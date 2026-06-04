# Default to scanning global caches; safer TUI defaults

Motivated by a direct request: make global caches part of the default run in
every mode, stop pre-selecting everything in the interactive TUI, and stop
offering the globals-only filter when a scan turns up no globals.

## Problem

Three defaults are wrong today:

1. **Global caches are opt-in.** `--global` defaults to `false` (`main.go`
   `parseArgs`), so `wolfe` and `wolfe -i` ignore global per-user caches
   (`~/.m2`, `.cache/go-build`, …) unless the user remembers the flag. These
   caches are usually the largest reclaimable space, so the most useful run is
   the one people forget to ask for.
2. **The TUI pre-selects everything.** Each scanned item is created with
   `selected: true` (`internal/tui/tui.go`, the `scanMsg` handler), so a stray
   `enter` → `y` deletes the whole list. The tool is destructive; the safe
   default is to select nothing and make the user choose.
3. **The globals-only filter is offered even when there are no globals.** The
   `g` key toggles `onlyGlobal` and the footer always advertises `[g] globals`
   (`internal/tui/view.go`). When a scan produced no global items, toggling
   filters the list to empty with no explanation. With globals on by default
   this case is common — scanning a single project subtree finds local
   artifacts but no caches.

## Behavior

### Global caches on by default (all modes)

`wolfe <path>`, `wolfe <path> --delete`, and `wolfe <path> -i` all include
global caches by default. The `--global` flag is **removed**. A new
**`--no-global`** flag excludes global caches; it mirrors `--no-trash` and
applies to every mode, including the TUI launch — `wolfe -i --no-global` scans
local artifacts only.

`--no-global` composes with everything; there is no new mutually-exclusive
combination. `--no-global --quiet`, `--no-global --delete`, and so on behave as
their names suggest.

### TUI: nothing selected by default

A fresh scan lists every item **unselected**. The selected-total footer opens at
`Selected: 0 B / 0 dirs`. `enter` stays gated on a non-empty selection (already
true in `handleSelectingKey`), so it is a no-op until the user checks something.
`a` (toggle-all) selects the whole visible list — the one-keystroke path to the
old behavior. This also applies after a rescan: re-listed items start unselected
(the rescan spec already says fresh items "start selected"; that sentence is now
the opposite and the spec text is corrected there too).

### TUI: globals filter only when globals exist

When the current scan contains at least one global item, the `g` key and the
`[g] globals` footer hint behave exactly as today. When the scan contains **no**
global items:

- the `g` key is a no-op (it does not toggle `onlyGlobal`),
- the footer omits the `[g] globals` hint,
- `onlyGlobal` is forced to `false` for the new result set.

The last point matters because rescan preserves `onlyGlobal` across scans.
Without forcing it off, rescanning from a globals-present list (with
`onlyGlobal` on) into a no-globals result would strand the user on an empty,
un-toggleable view. Forcing `onlyGlobal = false` whenever a scan yields no
globals closes that trap.

"Has globals" is derived from the scan results — any item whose `target.Global`
is set — not from the `--no-global` flag, so `--no-global` (which removes
globals from the results) hides the filter too, for free.

## Architecture

### CLI / flags (`main.go`)

- `options.global bool` → `options.noGlobal bool`.
- Drop the `--global` registration; register `--no-global`
  (`fs.BoolVar(&opts.noGlobal, "no-global", false, "exclude global per-user caches (~/.m2, ~/.gradle, ...)")`).
- `run()` computes `includeGlobal := !opts.noGlobal` once and passes it to both
  `runCLI(opts.path, includeGlobal, opts.del, opts.quiet, how)` and
  `tui.Run(tui.Options{Root: opts.path, Global: includeGlobal, Permanent: opts.noTrash})`.
- `runCLI`'s signature (already a resolved `global bool`) and package `wolf`
  are unchanged.

### TUI (`internal/tui`)

- `scanMsg` handler builds items with `selected: false`.
- New method `(Model) hasGlobals() bool` — true if any item's `target.Global`
  is set.
- `scanMsg` handler, after populating items: `if !m.hasGlobals() { m.onlyGlobal = false }`,
  then `rebuildView()` as today.
- `handleSelectingKey` `case "g":` is guarded by `m.hasGlobals()`; otherwise the
  key is ignored.
- `selectingView` builds the footer hint string with `[g] globals` included only
  when `m.hasGlobals()`.

No changes to the scan/size pipeline or to `resetForScan`/`rescan` (which
already preserve `onlyGlobal`; the forced-off rule lives in the post-scan
handler so it applies to both the first scan and rescans).

### Files touched

- `main.go` — flag rework (`--global` → `--no-global`, `options` field, `run`
  wiring).
- `internal/tui/tui.go` — `selected: false`, `hasGlobals`, the `g`-key guard,
  forcing `onlyGlobal=false` on global-less scans.
- `internal/tui/view.go` — conditional `[g] globals` footer hint.
- `README.md` — flags list, the "Global caches" paragraph, examples, and the
  interactive section.
- `docs/superpowers/specs/2026-06-03-tui-rescan-after-delete-design.md` — correct
  the one sentence that says fresh items "start selected".
- `main_test.go`, `internal/tui/tui_test.go`, `e2e/e2e_test.go` — see Testing.

## Testing (TDD)

Unit tests drive `parseArgs`/`Update` directly with synthetic input, as the
existing tests do (no TTY required).

**`main_test.go` (`parseArgs`):**

1. Default args → `options{path: "."}` — globals-on is the zero value
   (`noGlobal` false); re-confirms the new default.
2. The three existing ordering cases that used `--global` ("flags before the
   path", "flags after the path", "last positional wins") are reworked to use
   `--no-global`, asserting `noGlobal: true` plus the same path/ordering
   outcomes — they only ever used a representative flag to prove
   flag/positional interleaving.
3. `--global` is no longer accepted: extend `TestParseArgsUnknownFlag` (or add a
   case) asserting `parseArgs([]string{"--global"})` returns an error.

**`internal/tui/tui_test.go`:**

4. `TestModelDefaultsAllSelectedLargestFirst` → repurposed (and renamed): after
   scan+sizing, `selectedCount() == 0` and `selectedSize() == 0`, while the
   largest-first view ordering still holds.
5. The `reachDone` helper presses `a` (select-all) before `enter`, so the
   confirm→delete path still has a selection; the round-trip and rescan tests
   that depend on it keep working.
6. `TestRoundTripSelectDeleteRescan`: after rescan, freshly listed items are
   **unselected** (`selectedCount() == 0`).
7. `TestModelToggleAndFilter` / `TestDeleteFlowDeletesSelectedAfterConfirm`: a
   `space` press now **selects** rather than deselects; expected sizes/sets are
   updated accordingly.
8. **New — no globals hides the filter:** a `findFn` returning only local
   targets; after scan, pressing `g` leaves `onlyGlobal` false and the view
   full, and `View()` omits `[g] globals`.
9. **New — global-less scan forces `onlyGlobal` off:** start from a
   globals-present scan with `onlyGlobal == true`, rescan into a no-globals
   result, then assert `onlyGlobal == false` and a non-empty view. (The existing
   globals-present fixture already covers the filter-present path.)

**`e2e/e2e_test.go`:**

10. `TestE2EDeleteRemovesArtifactsKeepsMarkers`: drop `--global` from the args
    (globals are the default now); assertions unchanged.
11. `TestE2EDryRunDeletesNothing`: add an assertion that a global cache path from
    the fixture (e.g. `.m2`/`.gem`) appears in the default dry-run output,
    locking in the new default.

Host verification is `gofmt -l .`, `go vet ./...`, and `go test ./...`
(sandboxed, non-destructive). The destructive, `e2e`-tagged paths run only in
the Docker harness (`./e2e/run.sh`), never against real files on the host.

## Out of scope

- An in-TUI keypress to toggle global *inclusion* (the `g` key stays a view
  filter; `--no-global` is the inclusion control).
- A `--local-only` alias for `--no-global`.
- Any change to global-cache *detection* (which paths count as global) in
  package `wolf`.
- Backwards-compatible acceptance of `--global` — it is removed, not deprecated.
