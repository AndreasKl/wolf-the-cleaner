# TUI: offer rescan-or-quit after deleting

Issue: [#2](https://github.com/AndreasKl/wolf-the-cleaner/issues/2) — *Interactive
mode should offer to continue after deleting some folders.*

## Problem

After the interactive TUI finishes deleting, the Done screen only offers to quit
(`internal/tui/view.go` `stateDone`, `internal/tui/tui.go` `handleKey`). A user
who wants to clean more must quit and relaunch `wolfe -i`. We want the Done
screen to also offer to continue.

## Behavior

The Done screen offers two actions:

- **`[r] rescan`** — discard the just-completed results and run a fresh scan:
  `wolf.Find` again, then re-measure sizes, landing back in the
  scanning → selecting flow. A fresh scan is the source of truth: deleted
  directories are gone, and any concurrent changes on disk are reflected.
  **`enter` also rescans** (treated the same as `r`).
- **`[q] quit`** — exit. `esc` and `ctrl+c` also quit.

Note this changes today's Done-screen behavior, where `enter` quits; after this
change `enter` rescans.

### Rescan from the selecting screen

The selecting screen also gains a **`[r] rescan`** key with the same effect:
re-run `wolf.Find` + re-measure and return to the scanning → selecting flow,
preserving the filter and `onlyGlobal` toggle. This lets the user refresh the
list (e.g. after building or deleting outside the tool) without first deleting
something. `enter` on the selecting screen still means *delete* — only `r`
rescans there. While the filter input is focused, `r` types into the filter
(same as `a`/`g`/`q` today); rescan is only bound in the non-filtering branch.

Pending selections are not carried across a rescan — the list is rebuilt from a
fresh scan, and (as today) every freshly scanned item starts selected.

### What rescan preserves vs. resets

Rescan re-runs the scan but keeps *how the user was viewing the list*:

- **Preserved:** the injected `findFn`/`sizeFn`/`deleteFn`, `opts`, the filter
  text (`filter` text input + its value), and the `onlyGlobal` toggle. Terminal
  `width`/`height`, the spinner, and the progress bar are also kept.
- **Reset:** all per-scan state — `items`, `byPath`, `view`, `allSized`,
  `cursor`, `offset`, the delete queue (`delQueue`, `delIndex`), `freed`,
  `failures`, the `sizeCh` channel, and the transient `filtering` input flag
  (filter value is kept, but we are not in filter-input mode while scanning).

Items that **failed** to delete still exist on disk, so a fresh scan will
naturally re-list them; no special handling is needed.

## Architecture

Approach: re-enter the existing scan flow rather than relaunch the program.

- A new helper `(*Model).resetForScan()` clears the per-scan fields above and
  sets `state = stateScanning`. `New` is refactored to call it (after building
  the spinner/filter/progress components), so the reset list cannot drift from
  construction.
- The `stateDone` branch of `handleKey` (`r`/`enter`) and the non-filtering
  branch of `handleSelectingKey` (`r`) both call `resetForScan` and return
  `tea.Batch(m.spinner.Tick, findCmd(m.findFn))` — the same command `Init`
  uses. A tiny `(m Model) rescan()` helper returns that
  `(tea.Model, tea.Cmd)` pair so both call sites share one implementation. The
  existing `scanMsg → startSizing → sizeMsg → sizingDoneMsg` pipeline then runs
  unchanged, and `rebuildView` re-applies the preserved filter and `onlyGlobal`
  toggle to the new items.

No changes to package `wolf`; this is entirely within `internal/tui`.

### Files touched

- `internal/tui/tui.go` — `resetForScan` + `rescan` helpers, the `r`/`enter`
  case in `stateDone`, the `r` case in `handleSelectingKey`, and `New`
  refactored to share the reset.
- `internal/tui/view.go` — Done-screen footer changes from `[q] quit` to
  `[r] rescan  [q] quit` (with `enter` also rescanning), and the selecting-screen
  footer gains `[r] rescan`.

## Testing (TDD)

Tests drive `Update` directly with synthetic messages and injected functions,
as `internal/tui/tui_test.go` already does (no TTY required):

1. **Rescan transition:** from `stateDone`, an `r` `KeyMsg` → `state ==
   stateScanning`, results cleared (`freed == 0`, `failures == nil`, `items`
   empty), and a non-nil command returned. A parallel case asserts `enter`
   triggers the same rescan transition.
2. **Real re-scan:** inject a `findFn` whose second call returns a *different*
   target set; after `r`, feed the resulting `scanMsg` and assert the model
   returns to `stateSelecting` with the new items (proves `findFn` ran again,
   not a cached list).
3. **Filter + globals preserved:** set a filter value and `onlyGlobal` before
   reaching Done; after rescan + `scanMsg`, assert both are still in effect
   (the rebuilt `view` reflects them).
4. **Quit still works:** from `stateDone`, `q` (and `esc`/`ctrl+c`) returns
   `tea.Quit`.
5. **Round-trip:** select → confirm (`y`) → delete → done → rescan (`r`) →
   select again, ending in `stateSelecting`.
6. **Rescan from selecting:** from `stateSelecting`, an `r` `KeyMsg` → `state ==
   stateScanning` and a non-nil command; feeding the `scanMsg` returns to
   `stateSelecting` with the (re-fetched) items. Also assert that while the
   filter input is focused, `r` does *not* rescan (it edits the filter).

## Out of scope

- Re-scanning automatically (without a keypress).
- Changing non-interactive CLI behavior.
- The unrelated TUI follow-ups in #7 (abort during scan) and #8 (goroutine
  leak).
