# Default Global Caches & Safer TUI Defaults — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make global caches part of the default scan in every mode (replacing the opt-in `--global` with an opt-out `--no-global`), start the interactive TUI with nothing selected, and hide the globals-only filter when a scan finds no globals.

**Architecture:** Three small, independent behavior flips plus their tests. The flag change lives entirely in `main.go`; the TUI changes live in `internal/tui` (`tui.go` for state, `view.go` for the footer). Package `wolf` is untouched. Each task is test-first and committed on its own.

**Tech Stack:** Go, Bubble Tea TUI (`charmbracelet/bubbletea`), stdlib `flag`, stdlib `testing`. Spec: `docs/superpowers/specs/2026-06-04-default-globals-and-tui-defaults-design.md`.

---

## File map

- `main.go` — flag definitions + `run()` wiring. `options.global bool` → `options.noGlobal bool`; drop `--global`, add `--no-global`; pass `!opts.noGlobal` to `runCLI` and `tui.Run`.
- `internal/tui/tui.go` — `scanMsg` builds items unselected; new `hasGlobals()`; force `onlyGlobal=false` on global-less scans; guard the `g` key.
- `internal/tui/view.go` — footer shows `[g] globals` only when `hasGlobals()`.
- `main_test.go` — `parseArgs` tests (rework `--global` cases → `--no-global`; reject `--global`).
- `internal/tui/tui_test.go` — selection-default tests + two new globals-filter tests.
- `e2e/e2e_test.go` — drop `--global`; assert globals appear by default.
- `README.md` + `docs/superpowers/specs/2026-06-03-tui-rescan-after-delete-design.md` — docs.

A note on TDD in Go: when a test references a renamed field/flag that doesn't exist yet, `go test` fails to **build**. That build failure is the "red" state — the implementation step makes it compile and pass.

---

## Task 1: Replace `--global` with default-on globals + `--no-global`

**Files:**
- Modify: `main.go:25` (struct field), `main.go:47` (flag), `main.go:99` (TUI wiring), `main.go:110` (CLI wiring)
- Test: `main_test.go:14-83`

- [ ] **Step 1: Rewrite the `parseArgs` tests to the new flag**

In `main_test.go`, inside `TestParseArgs`, replace the three cases that use `--global`/`global: true` so they use `--no-global`/`noGlobal: true` (they only ever used a representative flag to prove flag/positional interleaving). The exact replacements:

```go
		{
			name: "flags before the path",
			args: []string{"--no-global", "--delete", "/tmp/x"},
			want: options{path: "/tmp/x", noGlobal: true, del: true},
		},
		{
			// The stdlib flag package stops at the first non-flag argument, so
			// this is the case the re-parse loop exists to support.
			name: "flags after the path",
			args: []string{"/tmp/x", "--no-global", "--delete"},
			want: options{path: "/tmp/x", noGlobal: true, del: true},
		},
```

```go
		{
			name: "last positional wins",
			args: []string{"a", "--no-global", "b"},
			want: options{path: "b", noGlobal: true},
		},
```

Leave the other cases (`defaults to current dir…`, `flags on both sides…`, `interactive shorthand`) unchanged. Then add a new test below `TestParseArgsUnknownFlag` asserting the removed flag is rejected:

```go
func TestParseArgsRejectsRemovedGlobalFlag(t *testing.T) {
	if _, err := parseArgs([]string{"--global"}, io.Discard); err == nil {
		t.Error("--global was removed and must now be rejected")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail (build error)**

Run: `go test ./... -run TestParseArgs`
Expected: build failure — `unknown field 'noGlobal' in struct literal of type options` (the field doesn't exist yet).

- [ ] **Step 3: Implement the flag rework in `main.go`**

Rename the struct field (`main.go:25`):

```go
type options struct {
	path        string
	noGlobal    bool
	del         bool
	quiet       bool
	interactive bool
	noTrash     bool
}
```

Replace the `--global` registration (`main.go:47`) with `--no-global`:

```go
	fs.BoolVar(&opts.noGlobal, "no-global", false, "exclude global per-user caches (~/.m2, ~/.gradle, ...)")
```

Wire the resolved value in `run()`. Add, just before the `if opts.interactive` block (currently `main.go:98`):

```go
	includeGlobal := !opts.noGlobal
```

Change the TUI launch (`main.go:99`) to use it:

```go
		failed, err := tui.Run(tui.Options{Root: opts.path, Global: includeGlobal, Permanent: opts.noTrash})
```

Change the CLI call (`main.go:110`) to use it:

```go
	return runCLI(opts.path, includeGlobal, opts.del, opts.quiet, how)
```

(`runCLI`'s signature already takes a resolved `global bool` — do not change it.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'TestParseArgs'`
Expected: PASS (all `TestParseArgs` subtests + `TestParseArgsRejectsRemovedGlobalFlag`).

Then the full suite still builds/passes (TUI unchanged this task):
Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat(cli)!: scan global caches by default; replace --global with --no-global"
```

---

## Task 2: TUI starts with nothing selected

**Files:**
- Modify: `internal/tui/tui.go:201` (`selected: true` → `false`)
- Test: `internal/tui/tui_test.go` — `reachDone` helper + four tests

- [ ] **Step 1: Update the tests to the new default**

In `internal/tui/tui_test.go`, make these edits.

(a) `reachDone` — press `a` (select-all) first, since nothing is selected by default. Replace the whole helper:

```go
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
```

(b) `TestRoundTripSelectDeleteRescan` — after rescan, items start unselected. Replace its post-rescan assertion block:

```go
	if m.state != stateSelecting {
		t.Fatalf("after rescan, state = %v, want selecting", m.state)
	}
	if m.selectedCount() != 0 {
		t.Errorf("freshly rescanned items should start unselected, got %d selected", m.selectedCount())
	}
```

(c) Rename and repurpose `TestModelDefaultsAllSelectedLargestFirst`. Replace the whole function:

```go
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
```

(d) `TestModelToggleAndFilter` — a `space` now *selects* the top item. Replace the toggle assertion:

```go
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggle top (gradle 600) on
	m = m2.(Model)
	if m.selectedSize() != 600 {
		t.Errorf("after toggle gradle on, selected = %d, want 600", m.selectedSize())
	}
```

(e) `TestDeleteFlowDeletesSelectedAfterConfirm` — select all, then deselect the top. Insert a select-all press immediately before the existing `space` (deselect gradle) line so the block reads:

```go
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // select all
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace}) // deselect gradle (top)
	m = m2.(Model)
```

(The remaining assertions — 2 deletions, `freed == 400`, gradle not deleted — stay as they are.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/`
Expected: FAIL — e.g. `TestModelDefaultsNoneSelectedLargestFirst` reports `size=1000 count=3, want 0/0`, and `reachDone` Fatals `state = selecting, want done` (with the old default, `a` *deselects* the pre-selected list, so `enter` is a no-op).

- [ ] **Step 3: Implement — build items unselected**

In `internal/tui/tui.go`, the `scanMsg` handler (currently `tui.go:201`), change:

```go
		it := &item{target: t, selected: false}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tui.go internal/tui/tui_test.go
git commit -m "feat(tui): start with nothing selected (press a to select all)"
```

---

## Task 3: Hide the globals filter when a scan has no globals

**Files:**
- Modify: `internal/tui/tui.go` — `scanMsg` handler, `handleSelectingKey` `g` case, new `hasGlobals` method
- Modify: `internal/tui/view.go:108` — conditional footer hint
- Test: `internal/tui/tui_test.go` — two new tests

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/tui_test.go`:

```go
func TestNoGlobalsHidesGlobalFilter(t *testing.T) {
	locals := []wolf.Target{
		{Path: "/a/node_modules", Kind: "JavaScript/TS"},
		{Path: "/b/target", Kind: "Rust"},
	}
	m := New(Options{Root: "/x"})
	m.findFn = func() []wolf.Target { return locals }
	m.sizeFn = func(string) int64 { return 1 }
	m.deleteFn = func(wolf.Target) error { return nil }
	m.width, m.height = 100, 10

	m2, _ := m.Update(scanMsg{targets: locals})
	m = m2.(Model)
	for _, lt := range locals {
		m2, _ = m.Update(sizeMsg{path: lt.Path, size: 1})
		m = m2.(Model)
	}
	m2, _ = m.Update(sizingDoneMsg{})
	m = m2.(Model)

	before := len(m.view)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = m2.(Model)
	if m.onlyGlobal {
		t.Error("g must not enable the globals-only filter when there are no globals")
	}
	if len(m.view) != before {
		t.Errorf("view changed after g with no globals: %d -> %d", before, len(m.view))
	}
	if strings.Contains(m.View(), "[g] globals") {
		t.Errorf("footer must omit the globals hint when there are no globals:\n%s", m.View())
	}
}

func TestGlobalLessScanForcesOnlyGlobalOff(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/target": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes) // fixture has a global

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}) // globals only
	m = m2.(Model)
	if !m.onlyGlobal {
		t.Fatal("setup: expected onlyGlobal on while globals exist")
	}

	m.findFn = func() []wolf.Target {
		return []wolf.Target{{Path: "/c/dist", Kind: "JavaScript/TS"}}
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}) // rescan into no-globals
	m = applyScanCmd(t, m2.(Model), cmd)

	if m.onlyGlobal {
		t.Error("a scan with no globals must force onlyGlobal off")
	}
	if len(m.view) == 0 {
		t.Error("view must not be empty after onlyGlobal is forced off")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestNoGlobalsHidesGlobalFilter|TestGlobalLessScanForcesOnlyGlobalOff'`
Expected: FAIL — with no guard, `g` sets `onlyGlobal=true` (view empties, footer still shows `[g] globals`), and the rescan leaves `onlyGlobal` stuck on.

- [ ] **Step 3: Implement `hasGlobals` + the guards**

In `internal/tui/tui.go`, add the method (next to `selectedSize`, after it):

```go
// hasGlobals reports whether the current scan produced any global-cache item.
// It drives whether the globals-only filter is offered at all.
func (m Model) hasGlobals() bool {
	for _, it := range m.items {
		if it.target.Global {
			return true
		}
	}
	return false
}
```

In the `scanMsg` handler, after `m.state = stateSelecting` and before `m.rebuildView()`, insert:

```go
		if !m.hasGlobals() {
			m.onlyGlobal = false // no globals to filter to; don't strand the view
		}
```

Guard the `g` case in `handleSelectingKey` (currently `tui.go:298-301`):

```go
	case "g":
		if m.hasGlobals() {
			m.onlyGlobal = !m.onlyGlobal
			m.cursor = 0
			m.rebuildView()
		}
```

In `internal/tui/view.go`, replace the single footer `Fprint` (currently `view.go:108`) with a conditional builder:

```go
	keys := "[space] toggle  [a] all  "
	if m.hasGlobals() {
		keys += "[g] globals  "
	}
	keys += "[/] filter  [r] rescan  [enter] delete  [q] quit"
	fmt.Fprint(&b, dimStyle.Render("  "+keys)+"\n")
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS (the two new tests plus all existing TUI tests — the globals-present fixture keeps the `g` filter working and `[g] globals` visible).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tui.go internal/tui/view.go internal/tui/tui_test.go
git commit -m "feat(tui): hide the globals filter when a scan has no globals"
```

---

## Task 4: Adapt the e2e tests to the new defaults

**Files:**
- Modify: `e2e/e2e_test.go:96` (drop `--global`), `e2e/e2e_test.go:84-86` region (assert globals by default)

> The `e2e` package is build-tagged `//go:build e2e` and deletes files; it is **never** run on the host. Verify only that it compiles here; the real run is the Docker harness (`./e2e/run.sh`).

- [ ] **Step 1: Drop `--global` from the delete test**

In `e2e/e2e_test.go`, `TestE2EDeleteRemovesArtifactsKeepsMarkers`, change the invocation (currently line 96) to:

```go
	out, code := run(t, bin, nil, root, "--delete")
```

(The expected-deleted list — including `.m2/repository` and `.gem` — stays unchanged; globals are now scanned by default.)

- [ ] **Step 2: Assert globals appear in the default dry-run**

In `TestE2EDryRunDeletesNothing`, after the existing `node_modules`/`Total to trash:` check, add:

```go
	if !strings.Contains(out, ".m2") {
		t.Errorf("globals should be scanned by default; expected a .m2 cache in output:\n%s", out)
	}
```

- [ ] **Step 3: Verify the e2e package compiles**

Run: `go vet -tags e2e ./e2e/`
Expected: clean (no output, exit 0). Do **not** run the e2e tests on the host.

- [ ] **Step 4: Commit**

```bash
git add e2e/e2e_test.go
git commit -m "test(e2e): adapt to global-by-default and --no-global"
```

---

## Task 5: Documentation

**Files:**
- Modify: `README.md` (flags list, examples, Global-caches paragraph, Interactive section)
- Modify: `docs/superpowers/specs/2026-06-03-tui-rescan-after-delete-design.md` (one sentence)

- [ ] **Step 1: Update the flags list**

In `README.md`, replace the `--global` bullet (currently line 52) with:

```markdown
- `--no-global` - exclude global per-user caches (`~/.m2`, `~/.gradle`, ...). Global caches are **included by default** in every mode.
```

- [ ] **Step 2: Update the examples block**

Replace the example lines that mention `--global` (currently lines 64-69) so the block reads:

```text
wolfe ~/Coding                          # dry-run incl. global caches: would move to trash
wolfe ~/Coding --no-trash               # dry-run: would PERMANENTLY delete
wolfe ~/Coding --delete                 # move artifacts (incl. global caches) to the trash
wolfe ~/Coding --delete --no-trash      # permanently delete (frees space now)
wolfe ~/Coding --no-global              # dry-run of local build artifacts only
wolfe ~/Coding --delete --no-trash --quiet   # backup script: reclaim space (incl. caches)
wolfe ~/Coding -i                       # interactive select-and-delete
```

- [ ] **Step 3: Update the Interactive TUI section**

Replace the Interactive-TUI paragraph (currently lines 86-89) with:

```markdown
Interactive mode scans local artifacts **and** global caches by default (pass
`--no-global` to skip the caches). The list starts with **nothing selected** —
pick what to remove, or press `a` to select all. While scanning shows a
scrollable, filterable checklist sorted largest-first (sizes fill in lazily as
they're computed). Keys: `↑/↓` move, `space` toggle, `a` toggle all, `g`
globals-only (shown only when the scan found global caches), `/` filter, `enter`
confirm & delete, `q` quit. A confirmation step gates all deletion.
```

- [ ] **Step 4: Update the Global-caches paragraph**

In `README.md`, change the opening of the global-caches paragraph (currently line 117) from `Global caches (with `--global`):` to:

```markdown
Global caches (included by default; use `--no-global` to skip): Maven
```

(Keep the rest of that sentence/list intact.)

- [ ] **Step 5: Correct the prior rescan spec**

In `docs/superpowers/specs/2026-06-03-tui-rescan-after-delete-design.md`, replace the sentence (currently lines 37-38):

```markdown
Pending selections are not carried across a rescan — the list is rebuilt from a
fresh scan, and every freshly scanned item starts unselected.
```

- [ ] **Step 6: Commit**

```bash
git add README.md docs/superpowers/specs/2026-06-03-tui-rescan-after-delete-design.md
git commit -m "docs: document global-by-default scanning and --no-global"
```

---

## Task 6: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Format check**

Run: `gofmt -l .`
Expected: no output. If any file is listed, run `gofmt -w .`, then `git add -u && git commit -m "style: gofmt"`.

- [ ] **Step 2: Vet (both build tags)**

Run: `go vet ./... && go vet -tags e2e ./e2e/`
Expected: clean.

- [ ] **Step 3: Unit tests**

Run: `go test ./...`
Expected: PASS (sandboxed; safe on host).

- [ ] **Step 4: Linters (if installed)**

Run: `golangci-lint run ./...`
Expected: clean. (Config: `.golangci.yml`.)

- [ ] **Step 5 (optional, Docker only): full e2e**

The destructive e2e suite runs only in Docker: `./e2e/run.sh`. Do not run the `wolfe` binary or `-tags e2e` tests directly on the host.

---

## Self-review

- **Spec coverage:** globals default-on all modes + `--no-global` (Task 1); `--global` removed/rejected (Task 1); nothing pre-selected (Task 2); `g` filter hidden + no-op + `onlyGlobal` forced off when no globals (Task 3); README + rescan-spec correction (Task 5); e2e adaptation (Task 4); host verification via `go test`, Docker-only e2e (Task 6). All spec sections map to a task.
- **No placeholders:** every code/test step shows the actual code and exact command + expected output.
- **Type consistency:** field `noGlobal` (Task 1) used consistently; `hasGlobals()` (Task 3) called identically in `tui.go` and `view.go`; `Options.Global` keeps its meaning (set from `!opts.noGlobal`).
