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

func TestModelDefaultsAllSelectedLargestFirst(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/target": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	if m.state != stateSelecting {
		t.Fatalf("state = %v, want selecting", m.state)
	}
	if m.selectedSize() != 1000 || m.selectedCount() != 3 {
		t.Errorf("defaults: size=%d count=%d, want 1000/3", m.selectedSize(), m.selectedCount())
	}
	if m.items[m.view[0]].target.Path != "/home/.gradle/caches" {
		t.Errorf("expected largest (gradle) first, got %s", m.items[m.view[0]].target.Path)
	}
}

func TestModelToggleAndFilter(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/target": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggle top (gradle 600) off
	m = m2.(Model)
	if m.selectedSize() != 400 {
		t.Errorf("after toggle gradle off, selected = %d, want 400", m.selectedSize())
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

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace}) // deselect gradle (top)
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
