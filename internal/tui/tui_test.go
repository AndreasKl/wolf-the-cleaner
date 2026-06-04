package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"it.kluth.buildcleaner/internal/wolf"
)

func fixtureTargets() []wolf.Target {
	return []wolf.Target{
		{Path: "/a/node_modules", Kind: "JavaScript/TS"},
		{Path: "/b/target", Kind: "Rust"},
		{Path: "/home/.gradle/caches", Kind: "Gradle (global cache)", Global: true},
	}
}

// newTestModel builds a model with injected fakes (no filesystem, no TTY).
func newTestModel(sizes map[string]int64, deleted *[]string) Model {
	m := New(Options{Root: "/x", Global: true})
	m.findFn = func() []wolf.Target { return fixtureTargets() }
	m.sizeFn = func(p string) int64 { return sizes[p] }
	m.deleteFn = func(t wolf.Target) error {
		*deleted = append(*deleted, t.Path)
		return nil
	}
	return m
}

// drainSizing applies the scan result then feeds every size synchronously.
func drainSizing(t *testing.T, m Model, sizes map[string]int64) Model {
	t.Helper()
	m2, _ := m.Update(scanMsg{targets: fixtureTargets()})
	m = m2.(Model)
	for p, s := range sizes {
		m2, _ := m.Update(sizeMsg{path: p, size: s})
		m = m2.(Model)
	}
	m2, _ = m.Update(sizingDoneMsg{})
	return m2.(Model)
}

// applyScanCmd executes a (possibly batched) scan command and applies the
// scanMsg it produces, mirroring what the Bubble Tea runtime would do.
func applyScanCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil scan command")
	}
	scan := findScanMsg(t, cmd())
	m2, _ := m.Update(scan)
	return m2.(Model)
}

// findScanMsg digs the scanMsg out of a command result, flattening a tea.Batch.
func findScanMsg(t *testing.T, msg tea.Msg) scanMsg {
	t.Helper()
	switch v := msg.(type) {
	case scanMsg:
		return v
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if sm, ok := c().(scanMsg); ok {
				return sm
			}
		}
	}
	t.Fatalf("no scanMsg produced by command (got %T)", msg)
	return scanMsg{}
}

func TestRescanRerunsFind(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := reachDone(t, drainSizing(t, newTestModel(sizes, &deleted), sizes))

	// The next scan returns a different set; rescan must re-invoke findFn.
	m.findFn = func() []wolf.Target {
		return []wolf.Target{{Path: "/c/dist", Kind: "JavaScript/TS"}}
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = applyScanCmd(t, m2.(Model), cmd)

	if m.state != stateSelecting {
		t.Fatalf("state = %v, want selecting", m.state)
	}
	if len(m.items) != 1 || m.items[0].target.Path != "/c/dist" {
		t.Fatalf("expected the re-fetched item /c/dist, got %d items", len(m.items))
	}
}

func TestRescanPreservesFilterAndGlobals(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/target": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}) // globals only
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	for _, r := range "gradle" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // exit filter input
	m = m2.(Model)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}) // rescan
	m = applyScanCmd(t, m2.(Model), cmd)

	if m.filter.Value() != "gradle" {
		t.Errorf("filter not preserved across rescan: %q", m.filter.Value())
	}
	if !m.onlyGlobal {
		t.Error("onlyGlobal toggle not preserved across rescan")
	}
	if len(m.view) != 1 || m.items[m.view[0]].target.Path != "/home/.gradle/caches" {
		t.Errorf("view not re-filtered after rescan: %d items", len(m.view))
	}
}

func TestRoundTripSelectDeleteRescan(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := reachDone(t, drainSizing(t, newTestModel(sizes, &deleted), sizes))

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}) // rescan
	m = applyScanCmd(t, m2.(Model), cmd)
	if m.state != stateSelecting {
		t.Fatalf("after rescan, state = %v, want selecting", m.state)
	}
	if m.selectedCount() != 0 {
		t.Errorf("freshly rescanned items should start unselected, got %d selected", m.selectedCount())
	}
}

// reachDone drives a freshly-scanned model through select-all + confirm +
// delete into the Done state. Nothing is selected by default, so it presses
// `a` first to have something to delete.
func reachDone(t *testing.T, m Model) Model {
	t.Helper()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // select all
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // -> confirm
	m = m2.(Model)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}) // -> deleting
	m = m2.(Model)
	for cmd != nil {
		msg := cmd()
		dm, ok := msg.(delMsg)
		if !ok {
			break
		}
		m2, cmd = m.Update(dm)
		m = m2.(Model)
	}
	if m.state != stateDone {
		t.Fatalf("reachDone: state = %v, want done", m.state)
	}
	return m
}

func TestRescanFromDoneResetsAndScans(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := reachDone(t, drainSizing(t, newTestModel(sizes, &deleted), sizes))

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	if m.state != stateScanning {
		t.Fatalf("after r, state = %v, want scanning", m.state)
	}
	if cmd == nil {
		t.Fatal("expected a scan command after rescan")
	}
	if m.freed != 0 || m.failures != nil || len(m.items) != 0 {
		t.Errorf("rescan must clear results: freed=%d failures=%v items=%d", m.freed, m.failures, len(m.items))
	}
}

func TestEnterRescansFromDone(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := reachDone(t, drainSizing(t, newTestModel(sizes, &deleted), sizes))

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.state != stateScanning {
		t.Fatalf("after enter on Done, state = %v, want scanning", m.state)
	}
	if cmd == nil {
		t.Fatal("expected a scan command after enter on Done")
	}
}

func TestQuitKeysStillQuitFromDone(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	} {
		deleted := []string{}
		m := reachDone(t, drainSizing(t, newTestModel(sizes, &deleted), sizes))
		m2, cmd := m.Update(key)
		m = m2.(Model)
		if cmd == nil {
			t.Errorf("key %v: expected tea.Quit command", key)
		}
		if m.state != stateDone {
			t.Errorf("key %v: state = %v, want done (quit shouldn't rescan)", key, m.state)
		}
	}
}

func TestRescanFromSelecting(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	if m.state != stateSelecting {
		t.Fatalf("setup: state = %v, want selecting", m.state)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = m2.(Model)
	if m.state != stateScanning {
		t.Fatalf("after r on selecting, state = %v, want scanning", m.state)
	}
	if cmd == nil {
		t.Fatal("expected a scan command")
	}
}

func TestRDoesNotRescanWhileFiltering(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")}) // enter filter mode
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}) // should type, not rescan
	m = m2.(Model)
	if m.state != stateSelecting {
		t.Fatalf("typing r while filtering must not rescan; state = %v", m.state)
	}
	if m.filter.Value() != "r" {
		t.Errorf("filter value = %q, want \"r\"", m.filter.Value())
	}
}

func TestDoneViewOffersRescanAndQuit(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := reachDone(t, drainSizing(t, newTestModel(sizes, &deleted), sizes))
	out := m.View()
	if !strings.Contains(out, "rescan") {
		t.Errorf("Done view should offer rescan:\n%s", out)
	}
	if !strings.Contains(out, "quit") {
		t.Errorf("Done view should still offer quit:\n%s", out)
	}
}

func TestSelectingViewOffersRescan(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	m.width, m.height = 100, 10
	m.rebuildView()
	if !strings.Contains(m.View(), "rescan") {
		t.Errorf("selecting view should offer rescan:\n%s", m.View())
	}
}

func TestModelDefaultsNoneSelectedLargestFirst(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/target": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	if m.state != stateSelecting {
		t.Fatalf("state = %v, want selecting", m.state)
	}
	if m.selectedSize() != 0 || m.selectedCount() != 0 {
		t.Errorf("defaults: size=%d count=%d, want 0/0 (nothing selected)", m.selectedSize(), m.selectedCount())
	}
	if m.items[m.view[0]].target.Path != "/home/.gradle/caches" {
		t.Errorf("expected largest (gradle) first, got %s", m.items[m.view[0]].target.Path)
	}
}

func TestModelToggleAndFilter(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/target": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggle top (gradle 600) on
	m = m2.(Model)
	if m.selectedSize() != 600 {
		t.Errorf("after toggle gradle on, selected = %d, want 600", m.selectedSize())
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	for _, r := range "target" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	if len(m.view) != 1 || m.items[m.view[0]].target.Path != "/b/target" {
		t.Fatalf("filter 'target' should show only /b/target, view=%d", len(m.view))
	}
}

func TestViewShowsRowsAndTotal(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/target": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	m.width, m.height = 100, 10
	m.rebuildView()
	out := m.View()
	if !strings.Contains(out, "node_modules") {
		t.Error("expected a row for node_modules")
	}
	if !strings.Contains(out, "Selected:") || !strings.Contains(out, "of 3") {
		t.Errorf("expected selected-total footer and count:\n%s", out)
	}
}

func TestConfirmGateBlocksDeleteOnQuit(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("expected tea.Quit on q")
	}
	if len(deleted) != 0 {
		t.Errorf("quit must not delete anything, deleted=%v", deleted)
	}
}

func TestDeleteFlowDeletesSelectedAfterConfirm(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/target": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // select all
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // deselect gradle (top)
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // -> confirm
	m = m2.(Model)
	if m.state != stateConfirm {
		t.Fatalf("state = %v, want confirm", m.state)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}) // -> deleting
	m = m2.(Model)
	if m.state != stateDeleting {
		t.Fatalf("state = %v, want deleting", m.state)
	}
	for cmd != nil {
		msg := cmd()
		dm, ok := msg.(delMsg)
		if !ok {
			break
		}
		m2, cmd = m.Update(dm)
		m = m2.(Model)
	}
	if m.state != stateDone {
		t.Fatalf("state = %v, want done", m.state)
	}
	if len(deleted) != 2 || m.freed != 400 {
		t.Fatalf("expected 2 deletions and freed 400, got deleted=%v freed=%d", deleted, m.freed)
	}
	for _, p := range deleted {
		if p == "/home/.gradle/caches" {
			t.Error("deselected item must not be deleted")
		}
	}
}
