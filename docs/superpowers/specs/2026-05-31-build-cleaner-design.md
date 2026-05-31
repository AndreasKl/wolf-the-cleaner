# build-cleaner — Design

**Date:** 2026-05-31
**Project name:** Wolf the Cleaner — after Winston "The Wolf" Wolfe, the cleaner
from *Pulp Fiction* ("I solve problems"). Output and `--help` carry a light touch
of that voice.
**Command / binary:** `wolfe`
**Module:** `it.kluth.buildcleaner` (Go import path; unchanged)
**Language:** Go 1.26
**Dependencies:** standard library for the core (scanning, sizing, deletion);
the [Charm](https://github.com/charmbracelet) stack for the optional interactive
TUI — `bubbletea` (event loop/model), `bubbles` (list/spinner/progress
components), and `lipgloss` (styling). The non-interactive CLI path never enters
the TUI and works without a TTY.

## Purpose

A CLI that walks a folder tree of programming projects and reports (and
optionally deletes) regenerable build artifacts — `bin`/`obj`, `node_modules`,
`target`, `build`, etc. The primary use case is **shrinking backups**: run it
over a `Coding` directory (or a backup copy of one) before/while backing up so
that regenerable output is excluded and the backup is smaller. Everything it
removes can be regenerated (dependencies re-download, build output recompiles).

It can optionally also clean **global, per-user package caches** (`~/.m2`,
`~/.gradle/caches`, the Go module cache, etc.) in the same run.

## Goals

- Safe by default: a **dry-run** that deletes nothing unless `--delete` is given.
- Report each deletable directory, its size, and a **total reclaimable** figure.
- **Scriptable by default** — the default and `--delete` paths are flag-driven
  with no prompts, so the tool drops into a backup script or cron job with clear
  exit codes for automation.
- **Optional polished TUI** (`--interactive`) for hands-on use: review
  candidates, cherry-pick what to delete, and delete from within the TUI.
- Correct detection: only flag a directory when there is a real project next to
  it (marker file + the artifact directory present together).
- Handle **nested projects** (monorepos) and large trees efficiently.

## Non-goals (v1)

- No user config file / rule overrides — the ruleset is built-in only.
- No trash/recycle bin — deletion is permanent (`os.RemoveAll`).
- No following of symlinks.
- No concurrency in the **non-interactive CLI path** — it scans and sizes
  sequentially (simple, deterministic output). The TUI path does run scanning
  and sizing as background `tea.Cmd`s for responsiveness; that asynchrony lives
  entirely in the `tui` package, not the core.

## CLI

```
wolfe [path] [flags]

  path            Root directory to scan for project artifacts.
                  Defaults to "." (current directory).

  --global        Also include global per-user package caches
                  (~/.m2, ~/.gradle/caches, Go module cache, ...) in the
                  same report and the same delete pass. Opt-in, because
                  these caches are shared across all of the user's projects.

  --delete        Actually remove the listed directories. Without this flag
                  the run is a dry-run (default) and deletes nothing.
                  (Ignored in --interactive mode, which confirms in the TUI.)

  --interactive   Launch the interactive TUI (Bubble Tea): scan, then pick
                  which candidates to delete from a checklist and delete them
                  in-TUI. Requires a TTY. Mutually exclusive with --quiet.
                  Short form: -i.

  --quiet         Suppress the per-directory listing; print only the final
                  total(s). Useful for scripted/backup runs. Mutually
                  exclusive with --interactive.

  --help          Show usage.
```

`--global` composes with `--interactive` (the global caches appear in the TUI
checklist alongside local artifacts). Passing both `--interactive` and `--quiet`
is an argument error (exit `2`). In `--interactive` mode the `--delete` flag is
redundant and ignored — deletion is gated by the in-TUI confirmation instead.

Examples:

```
wolfe ~/backup              # dry-run, local project artifacts only
wolfe ~/backup --global     # dry-run, local + global caches, one report
wolfe ~/backup --delete     # delete local project artifacts
wolfe ~/backup --global --delete   # delete local + global
wolfe --global              # dry-run of global caches only (path "." has no projects)
```

`--global` is **additive**: local project artifacts under `path` are always
scanned; `--global` adds the global caches to the same candidate list. The
report shows a combined total with a local/global breakdown.

## Architecture

Layered into focused packages so the core (scanning, sizing, deletion) is pure
standard library and unit-testable in isolation. The TUI is a thin
Bubble Tea view over that same core, kept in its own package so the core never
imports it.

```
it.kluth.buildcleaner
├── main.go                  // parse flags, pick CLI or TUI path, set exit code
├── internal/rules/          // built-in rule tables + matching logic
│   ├── rules.go             //   ProjectRules table, GlobalCaches table
│   └── rules_test.go
├── internal/scanner/        // walk a tree -> []Candidate; collect global caches
│   ├── scanner.go
│   └── scanner_test.go
├── internal/cleaner/        // size, report (CLI), and delete candidates
│   ├── cleaner.go
│   └── cleaner_test.go
├── internal/tui/            // Bubble Tea select-and-delete model (interactive)
│   ├── model.go             //   Model, Init/Update/View
│   └── model_test.go
├── go.mod                   // module it.kluth.buildcleaner, go 1.26
│                            //   requires bubbletea, bubbles, lipgloss
└── README.md
```

### Core type

```go
// Candidate is one directory that may be deleted.
type Candidate struct {
    Path  string // absolute path of the directory to delete
    Type  string // informational label, e.g. "C#/.NET", "Maven (global)"
    Scope Scope  // Local or Global
}

type Scope int
const ( Local Scope = iota; Global )
```

### `rules`

A static table mapping a project type to its marker files and the artifact
directories to remove when that project is detected. A marker may be a literal
filename or a glob (e.g. `*.csproj`). A rule matches a directory when **any**
marker is present **in that same directory**.

```go
type Rule struct {
    Name      string   // "C#/.NET"
    Markers   []string // filenames or globs that identify the project
    Artifacts []string // child dir names (relative) to delete, e.g. "bin", "app/build"
}
```

**Built-in project rules:**

| Type | Markers | Artifact dirs |
|---|---|---|
| C#/.NET | `*.csproj`, `*.sln`, `*.fsproj` | `bin`, `obj` |
| Java | `pom.xml`, `build.gradle`, `build.gradle.kts` | `target`, `build`, `.gradle` |
| Kotlin | `build.gradle.kts`, `*.kts`, `settings.gradle`, `settings.gradle.kts` | `build`, `.gradle`, `out` |
| Android | `settings.gradle`/`settings.gradle.kts` **and** `gradlew` | `build`, `.gradle`, `app/build`, `.cxx` |
| Flutter/Dart | `pubspec.yaml` | `build`, `.dart_tool`, `.flutter-plugins`, `.packages` |
| Go | `go.mod` | `bin` |
| Ruby | `Gemfile`, `*.gemspec` | `vendor/bundle`, `.bundle` |
| Python | `pyproject.toml`, `setup.py`, `requirements.txt` | `__pycache__`, `.venv`, `venv`, `*.egg-info`, `build`, `dist`, `.pytest_cache`, `.mypy_cache` |
| Crystal | `shard.yml` | `lib`, `.shards`, `bin` |

Note on overlap: Android/Kotlin/Java share Gradle markers. Multiple rules may
match the same directory; the scanner **dedupes candidates by absolute path**, so
a directory is queued at most once. The `Type` label is informational.

Artifact entries may contain a path separator (e.g. `app/build`) to target a
nested directory relative to the project root.

**Built-in global caches** (only included if the path exists):

| Tool | Path |
|---|---|
| Maven | `~/.m2/repository` |
| Ivy | `~/.ivy2/cache` |
| Gradle | `~/.gradle/caches` |
| NuGet | `~/.nuget/packages` |
| npm | `~/.npm` |
| Yarn | `~/.cache/yarn` |
| pip | `~/.cache/pip` |
| Cargo | `~/.cargo/registry` |
| Pub (Dart/Flutter) | `~/.pub-cache` |
| Gem | `~/.gem` |
| Go module cache | `go env GOMODCACHE` (fallback `~/go/pkg/mod`) |
| Go build cache | `go env GOCACHE` (fallback `~/.cache/go-build`) |

`$HOME` is resolved via `os.UserHomeDir()`. The Go cache paths are resolved by
shelling out to `go env GOMODCACHE` / `go env GOCACHE`; if `go` is unavailable
or returns empty, fall back to the documented default locations, and if those
don't exist, silently skip them.

### `scanner`

```go
func ScanLocal(root string) ([]Candidate, error)   // walk tree for project artifacts
func ScanGlobal() ([]Candidate, error)              // resolve existing global caches
```

`ScanLocal` uses `filepath.WalkDir` starting at `root`:

1. For each **directory** visited, check every `ProjectRule`: if any marker is
   present in the directory, then for each of that rule's artifact entries, if
   the artifact directory exists as a child, emit a `Candidate{Scope: Local}`.
2. **Do not descend into a matched artifact directory** — return
   `filepath.SkipDir` for it. This avoids the `node_modules`-full-of-
   `package.json` trap and is much faster. (Implementation: collect the set of
   matched artifact paths at each level and skip them as the walk reaches them.)
3. **Continue past a matched project** into its other subdirectories so nested
   projects are still found (monorepo support).
4. **Do not follow symlinks** — `WalkDir` does not follow them by default; dirs
   that are symlinks are not traversed into.
5. Dedupe emitted candidates by absolute path.

`ScanGlobal` builds candidates from the `GlobalCaches` table, including only
those whose resolved path exists.

Walk errors on an individual path (permission denied, etc.) are collected as
warnings and the walk continues; they do not abort the scan.

### `cleaner`

```go
func DirSize(path string) (int64, error)   // recursive sum of regular file sizes
func Report(w io.Writer, cands []SizedCandidate, opts ReportOpts)
func Delete(cands []Candidate) (freed int64, failures []error)
```

- Sizes each candidate via `DirSize` (a `filepath.WalkDir` summing `info.Size()`
  of regular files; symlinks not followed, size-of-symlink ignored).
- `Report` prints, per candidate, `<human-size>  <path>  (<type>)`, then a
  total. When both scopes are present, the total line includes a
  `(local: X / global: Y)` breakdown. `--quiet` prints only the total line(s).
- `Delete` removes each candidate with `os.RemoveAll`, summing freed bytes
  (pre-computed sizes) for successes and collecting failures.
- Human-readable sizes use **binary (1024-based) units** with labels
  `B`, `KiB`, `MiB`, `GiB`, `TiB`, formatted to 1 decimal place (e.g. `4.2 GiB`).
  The example outputs in this doc that show `GB`/`MB` are illustrative; the
  implementation uses the IEC labels above consistently.

### `tui` (interactive mode)

A Bubble Tea **select-and-delete** model that reuses `scanner` and `cleaner` —
it adds no deletion or sizing logic of its own, only presentation and selection.

Flow / states:

1. **Scanning** — show a `bubbles/spinner` with a live count while `scanner`
   runs in a background `tea.Cmd`. Candidates **stream into the list as they are
   found** (incremental `tea.Msg`s) rather than blocking on a full scan, so the
   UI is responsive immediately even on a huge tree. Sizing is **lazy and
   progressive**: `cleaner.DirSize` runs as background `tea.Cmd`s, prioritized
   for currently-visible rows; rows show `…` until their size lands. The
   running total updates as sizes arrive.
2. **Selecting** — a **scrollable, paginated** checklist built on
   `bubbles/list` (its viewport only renders visible rows, so it stays smooth at
   thousands of entries), **sorted largest-first** as sizes resolve. Each row:
   `[x|space] <size>  <path>  <type>`. Globals (when `--global`) are mixed in
   and tagged `(global)`. By default all rows start selected. A footer shows the
   **live selected total** (size + count), the visible range (e.g. `120–140 of
   3,184`), and key hints.
   - Keys: `↑/↓`/`j`/`k` move · `pgup/pgdn` page · `space` toggle row ·
     `a` toggle all · `/` **fuzzy filter by path/type** (list's built-in
     filter; toggles narrow huge lists fast) · `g` toggle only-globals ·
     `enter` proceed to confirm · `q`/`ctrl+c` quit without deleting.
   - `a` (toggle all) and the selected-total operate on the **full candidate
     set**, not just the visible page — and when a filter is active, on the
     filtered subset.
3. **Confirm** — a summary ("Delete N directories, X selected?") with
   `y` confirm / `n` back to selecting. (Non-bypassable; this is the only delete
   gate in interactive mode.)
4. **Deleting** — a `bubbles/progress` bar advancing per candidate as
   `cleaner.Delete` removes each (driven by `tea.Cmd`s emitting per-item msgs);
   failures collected and shown.
5. **Done** — final summary: freed total, count, and any failures. `q` exits.

Styling via `lipgloss`: a titled header, dimmed secondary text (type/path),
accent color on the selected total and progress bar, red on failures. Respects
`NO_COLOR` / non-TTY by degrading gracefully (and `main` refuses `-i` without a
TTY, exiting `2` with a hint to drop the flag).

The model takes its inputs (`[]SizedCandidate`, or a scan function) via
constructor injection so it can be driven in tests without a real terminal.

**Scalability (potentially thousands of candidates):** the list is virtualized
(only visible rows render), candidates stream in during scan, and sizing is
lazy/prioritized for visible rows — so neither a slow scan nor a slow `du`-style
sizing pass blocks interaction. Selection state is a `map[path]bool` (or a bit
on each item), so toggling, "select all", and the running total are O(n) over a
slice we already hold, never re-walking the filesystem. Sorting by size is
re-applied incrementally as sizes resolve (stable sort), avoiding visible
churn.

### Data flow

```
main
  ├─ parse flags (path, --global, --delete, --quiet, --interactive)
  ├─ validate (interactive+quiet -> exit 2; interactive without TTY -> exit 2)
  │
  ├─ if --interactive:
  │     run tui.Model (scans + sizes internally via scanner/cleaner,
  │     user selects, confirms, deletes); exit non-zero on delete failures
  │
  └─ else (CLI path):
        cands  = scanner.ScanLocal(path)
        if --global: cands += scanner.ScanGlobal()
        sized  = cleaner.DirSize for each candidate
        cleaner.Report(stdout, sized, opts)   // honors --quiet
        if --delete:
            freed, failures = cleaner.Delete(cands)
            print "Freed: <size>"; if failures -> stderr, exit non-zero
```

## Output

Dry-run (default):

```
[dry-run] would delete:
  4.2 GB   /home/.../Coding/javascript/foo/node_modules   (JS/TS)
  120 MB   /home/.../Coding/csharp/bar/bin                 (C#/.NET)
   80 MB   /home/.../Coding/csharp/bar/obj                 (C#/.NET)
  3.1 GB   /home/andreaskluth/.gradle/caches               (Gradle (global))
----
Total reclaimable: 7.5 GB across 4 directories  (local: 4.4 GB / global: 3.1 GB)
Run with --delete to remove them.
```

With `--delete`, the list prints as items are removed, ending with
`Freed: 7.5 GB across 4 directories`.

`--quiet` prints only the `Total reclaimable: ...` / `Freed: ...` line.

## Error handling

- **Invalid root path** (does not exist / not a directory): print to stderr,
  exit code `2`. (Exception: a bare `--global` run with default `.` still works
  even if `.` has no projects.)
- **Walk/permission error on an individual directory**: warn to stderr, skip,
  continue. Does not fail the run.
- **Delete failure on a candidate**: warn to stderr, exclude from freed total,
  continue. The run exits non-zero (`1`) if any delete failed.
- **Exit codes:** `0` success (incl. dry-run); `1` one or more delete failures;
  `2` invalid arguments / invalid root path.

## Testing

- **`rules`**: table tests for marker matching, including glob markers
  (`*.csproj`), the Android two-marker requirement, and that artifact lists are
  correct per type.
- **`scanner`**: build temp trees with `t.TempDir()` containing fixture
  projects; assert:
  - correct candidates for each project type,
  - nested project (monorepo) detection,
  - it does **not** descend into matched artifact dirs (e.g. a `package.json`
    placed inside `node_modules` must not produce a candidate),
  - symlinked directories are not traversed,
  - dedupe when multiple rules match one directory.
- **`cleaner`**: temp tree with files of known sizes → assert `DirSize` totals;
  assert dry-run `Report` deletes nothing; assert `Delete` removes the dirs and
  returns the correct freed total; assert a delete failure is reported and
  surfaced.
- **`tui`**: drive the Bubble Tea `Model` headlessly by calling
  `Update(msg)` with synthetic messages (key presses, scan-result msgs, sizing
  msgs, delete-result msgs) and asserting model state and `View()` output —
  no real terminal needed. Cover: streamed candidates appear; lazy sizes update
  the running total; `space`/`a` toggle selection correctly (incl. under an
  active filter and across pages); confirm gate must be passed before any
  `Delete` is invoked (use a fake cleaner to assert it isn't called on quit);
  delete failures surface in the Done state. A large synthetic set (e.g. 5,000
  candidates) verifies selection/total operations stay correct and the list
  paginates. Optionally use `charmbracelet/x/exp/teatest` for golden-frame
  coverage.
- **`main`** (light integration): run against a temp tree, assert exit codes and
  that dry-run / `--delete` / argument-validation (`-i` + `--quiet`,
  `-i` without TTY) behave correctly.

## Deliverables

- The Go module and packages above, with tests.
- `README.md` documenting purpose, install/build, usage (all flags incl.
  `--interactive`/`-i`), the interactive TUI (with a screenshot or asciinema),
  the built-in project rules and global caches, the backup use case, safety
  notes (dry-run default), and exit codes.
