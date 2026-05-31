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
- Trash support is **Linux-only** (FreeDesktop.org XDG home trash). No macOS/
  Windows recycle-bin integration; `--no-trash` (permanent `os.RemoveAll`) works
  on any platform.
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

  --delete        Actually dispose of the listed directories. Without this flag
                  the run is a dry-run (default) and changes nothing. Default
                  disposal moves directories to the trash (recoverable).

  --no-trash      Permanently delete (os.RemoveAll) instead of trashing. Frees
                  disk space immediately; irreversible. A mode modifier: alone
                  (no --delete) it only changes what the dry-run previews.

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

The design follows *A Philosophy of Software Design* (Ousterhout): a small number
of **deep modules** with simple interfaces hiding substantial implementation,
not many shallow ones. One core package, `wolf`, hides everything about *what*
is reclaimable and *how* it is found, measured, and removed. The CLI and the TUI
are thin **presenters** at a different layer — they decide how to show targets
and which to delete, and never see markers, `filepath.WalkDir`, or
`os.RemoveAll`. Complexity is pulled downward into `wolf`; errors are *defined
out of existence* where possible (an unreadable subtree is skipped, not an
error; deleting an already-absent directory is a no-op).

```
it.kluth.buildcleaner
├── main.go                  // CLI presenter: flags, text report, exit codes
├── internal/wolf/           // DEEP CORE: find + measure + delete reclaimable dirs
│   ├── wolf.go              //   Options, Target, Failure; Find, Measure, Delete, FormatSize
│   └── wolf_test.go
├── internal/rules/          // built-in data consumed only by wolf (hidden from callers)
│   ├── rules.go             //   ProjectRules, GlobalCacheDefs, Rule.Matches
│   └── rules_test.go
├── internal/tui/            // TUI presenter: Bubble Tea select-and-delete over wolf
│   ├── tui.go               //   Model, Init/Update; messages; Run
│   ├── view.go              //   View
│   └── tui_test.go
├── e2e/                     // Dockerized end-to-end tests (build tag `e2e`)
├── go.mod                   // module it.kluth.buildcleaner; requires bubbletea, bubbles, lipgloss
└── README.md
```

Why `wolf` is deep: its interface is four functions and two structs, but behind
them sit the rule table, the `WalkDir` traversal with skip/symlink/dedup logic,
`go env`/env-var cache resolution, `du`-style sizing, and `os.RemoveAll`. A
caller can reclaim space without knowing any of that.

### Core types (the `wolf` interface)

```go
// Options configures a scan.
type Options struct {
    Root          string // directory tree to scan for project artifacts
    IncludeGlobal bool   // also include the user's global package caches
}

// Target is one directory that can be reclaimed.
type Target struct {
    Path   string // absolute path of the directory
    Kind   string // informational label, e.g. "JavaScript/TS" or "Maven (global cache)"
    Global bool   // true for a shared per-user cache, false for a project artifact
    Size   int64  // measured size in bytes; 0 until Measure has been applied
}

// Failure records a directory that could not be deleted.
type Failure struct {
    Path string
    Err  error
}
```

### `wolf` (deep core)

```go
// Find walks opts.Root for project artifacts and, if opts.IncludeGlobal, adds
// the user's existing global caches. Returned Targets are unmeasured (Size 0).
// Find never returns an error: unreadable subtrees and missing caches are
// simply skipped. Matched artifact directories are not descended into, symlinks
// are not followed, nested projects are still found, and duplicates (e.g. a
// build/ matched by two rules) are removed.
func Find(opts Options) []Target

// Measure returns the total size in bytes of the regular files under path
// (best effort; unreadable entries contribute 0). Callers apply it to fill in
// Target.Size — eagerly (CLI) or lazily/streamed (TUI).
func Measure(path string) int64

// Disposal selects how Delete removes a target: ToTrash (default; move to the
// XDG home trash, recoverable) or Permanent (os.RemoveAll, frees space at once).
type Disposal int
const ( ToTrash Disposal = iota; Permanent )

// Delete disposes of each target per how, returning the total Size processed and
// any it could not dispose of. ToTrash follows the FreeDesktop.org Trash spec
// (move into ~/.local/share/Trash/files with a matching info/<name>.trashinfo;
// collision-safe names; cross-device copy+remove fallback). With Permanent,
// removing an absent directory is a no-op, not a failure.
func Delete(targets []Target, how Disposal) (processed int64, failed []Failure)

// FormatSize renders a byte count with binary (1024-based) IEC units —
// B, KiB, MiB, GiB, TiB — to one decimal place (e.g. "4.2 GiB"). Shared by both
// presenters; the GB/MB in this doc's examples are illustrative.
func FormatSize(n int64) string
```

`wolf` is the only package that imports `rules`. Splitting `Find` from `Measure`
keeps the interface small while serving both the CLI's eager sizing and the
TUI's lazy, streamed sizing over the same data.

### Detection data (`rules`, internal to `wolf`)

A rule matches a directory when at least one **marker** is present in it (plus,
for Android, the extra `gradlew` requirement); then each existing **artifact**
directory beside the marker becomes a `Target`. Markers and artifacts may be
globs; an artifact may contain `/` to name a nested dir (`app/build`). Lists
follow the canonical `github/gitignore` templates per ecosystem (and the
Crystal/Deno docs).

**Built-in project rules:**

| Type | Markers | Artifact dirs |
|---|---|---|
| C#/.NET | `*.csproj`, `*.sln`, `*.fsproj`, `*.vbproj` | `bin`, `obj` |
| JavaScript/TS | `package.json` | `node_modules`, `dist`, `build`, `.next`, `.nuxt`, `out`, `.output`, `.svelte-kit`, `.parcel-cache`, `.turbo`, `.vite`, `coverage`, `.cache` |
| Deno | `deno.json`, `deno.jsonc`, `deno.lock` | `node_modules`, `vendor` |
| Rust | `Cargo.toml` | `target` |
| Maven | `pom.xml` | `target` |
| Gradle | `build.gradle`, `build.gradle.kts`, `settings.gradle`, `settings.gradle.kts` | `build`, `.gradle` |
| Android | `settings.gradle`/`settings.gradle.kts` **and** `gradlew` | `build`, `.gradle`, `app/build`, `.cxx`, `.externalNativeBuild`, `captures` |
| Flutter/Dart | `pubspec.yaml` | `build`, `.dart_tool` |
| Ruby | `Gemfile`, `*.gemspec` | `vendor/bundle`, `.bundle`, `.yardoc`, `coverage`, `pkg` |
| Python | `pyproject.toml`, `setup.py`, `setup.cfg`, `requirements.txt` | `__pycache__`, `.venv`, `venv`, `*.egg-info`, `.eggs`, `build`, `dist`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`, `.tox`, `.nox`, `htmlcov`, `.hypothesis` |
| Ruff | `ruff.toml`, `.ruff.toml` | `.ruff_cache` |
| Crystal | `shard.yml` | `lib`, `.shards`, `bin` |

There is deliberately **no local Go rule**: a Go checkout has no canonical
reclaimable directory (binaries are loose files, `vendor/` is usually
committed). Go's reclaimable space is its global module and build caches below.
Gradle/Maven/Android share markers; the dedup-by-path in `Find` ensures a
`build/` is queued once. The `Kind` label is informational. (`.flutter-plugins`
and `.packages` are files, not directories, so they are not listed.)

**Built-in global caches** (included only if the resolved path exists). Each is
resolved in priority order: a named environment variable, then `go env <key>`,
then `$HOME` + relative path (`$HOME` via `os.UserHomeDir()`; if `go` is
unavailable, the relative fallback is used).

| Tool | Path / resolution |
|---|---|
| Maven | `~/.m2/repository` |
| Ivy | `~/.ivy2/cache` |
| Gradle | `~/.gradle/caches` |
| NuGet | `~/.nuget/packages` |
| npm | `~/.npm` |
| Yarn | `~/.cache/yarn` |
| pnpm | `~/.local/share/pnpm/store` |
| pip | `~/.cache/pip` |
| Cargo registry | `~/.cargo/registry` |
| Cargo git | `~/.cargo/git` |
| Pub (Dart/Flutter) | `~/.pub-cache` |
| Deno | `$DENO_DIR`, else `~/.cache/deno` |
| Gem | `~/.gem` |
| Crystal shards | `~/.cache/shards` |
| Go module cache | `go env GOMODCACHE`, else `~/go/pkg/mod` |
| Go build cache | `go env GOCACHE`, else `~/.cache/go-build` |

### Traversal (inside `Find`)

`Find` uses `filepath.WalkDir` from `Root`. At each directory it reads the entry
names, asks each `rules.Rule` whether it matches, and for every matching rule
turns each existing artifact directory into a `Target{Global: false}`. It then
**does not descend** into a matched artifact dir (`filepath.SkipDir`) — avoiding
the `node_modules`-full-of-`package.json` trap and most of the walk cost — while
still descending into other subdirectories so **nested projects** are found.
Symlinked directories are **not** followed; results are **deduped by path**.
Unreadable directories are skipped silently. Global caches are appended from the
`rules.GlobalCacheDefs` table, resolving each path and keeping only those that
exist as real (non-symlink) directories.

### `tui` (interactive mode)

A Bubble Tea **select-and-delete** model that reuses `wolf` — it adds no
deletion or sizing logic of its own, only presentation and selection.

Flow / states:

1. **Scanning** — show a `bubbles/spinner` while `wolf.Find` runs in a
   background `tea.Cmd`. Sizing is **lazy and progressive**: `wolf.Measure` runs
   as background `tea.Cmd`s feeding a channel; rows show `…` until their size
   lands. The running total updates as sizes arrive, so a slow `du`-style sizing
   pass never blocks interaction.
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
4. **Deleting** — a `bubbles/progress` bar advancing per target as `wolf.Delete`
   removes each (driven by `tea.Cmd`s emitting per-item msgs); failures collected
   and shown.
5. **Done** — final summary: freed total, count, and any failures. `q` exits.

Styling via `lipgloss`: a titled header, dimmed secondary text (type/path),
accent color on the selected total and progress bar, red on failures. Respects
`NO_COLOR` / non-TTY by degrading gracefully (and `main` refuses `-i` without a
TTY, exiting `2` with a hint to drop the flag).

The model takes its `wolf` operations (`Find`, `Measure`, `Delete`) via
function fields defaulted in the constructor, so tests can inject fakes and
drive it without a real terminal or filesystem.

**Scalability (potentially thousands of targets):** the list is virtualized
(only visible rows render), and sizing is lazy via background `wolf.Measure`
commands so a slow `du`-style pass never blocks interaction. Selection is a bit
on each item, so toggling, "select all", and the running total are O(n) over a
slice we already hold, never re-walking the filesystem. Sorting by size is
re-applied as sizes resolve (stable sort) to avoid visible churn.

### Data flow

```
main
  ├─ parse flags (path, --global, --delete, --quiet, --interactive)
  ├─ validate (interactive+quiet -> exit 2; interactive without TTY -> exit 2;
  │            invalid root path -> exit 2)
  │
  ├─ if --interactive:
  │     tui.Run(opts): Model calls wolf.Find, lazily wolf.Measure, lets the user
  │     select, confirms, then wolf.Delete; exit non-zero on delete failures
  │
  └─ else (CLI path):
        targets = wolf.Find(Options{Root, IncludeGlobal: --global})
        for each: t.Size = wolf.Measure(t.Path)          // eager
        print report (sorted largest-first, local/global breakdown; honors --quiet)
        if --delete:
            reclaimed, failed = wolf.Delete(targets)
            print "Freed: <size>"; if failed -> stderr, exit non-zero
```

The text report (formatting, `--quiet`, the dry-run hint) lives in `main` — it
is CLI presentation, a different layer from `wolf`'s data. `wolf.FormatSize`
renders the sizes for both presenters.

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
- **`wolf`**: build temp trees with `t.TempDir()` and assert against `Find`,
  `Measure`, and `Delete`:
  - `Find` returns the correct targets per project type (incl. Deno/Ruff),
  - nested project (monorepo) detection,
  - it does **not** descend into matched artifact dirs (e.g. a `package.json`
    placed inside `node_modules` must not produce a target),
  - symlinked directories are neither traversed nor returned,
  - dedupe when multiple rules match one directory (e.g. Gradle `build/`),
  - global caches resolved via env var / `go env` / home fallback (use a fake
    `$HOME` and `DENO_DIR`/`GOMODCACHE` env to avoid touching the real ones),
  - `Measure` totals files of known sizes; unreadable entries contribute 0,
  - `Delete` removes the dirs, returns the correct reclaimed total, treats an
    absent dir as a no-op, and surfaces a failure when removal is blocked,
  - `FormatSize` renders IEC units (`0 B`, `1.0 KiB`, `1.5 KiB`, `1.0 MiB`).
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
- **End-to-end (Docker)**: an `e2e` package guarded by the `//go:build e2e` tag,
  run inside a `golang:1.26` container (`e2e/run.sh` → `docker build` + `docker
  run`). It builds the real `wolfe` binary and drives it against a fixture
  project tree **and an isolated fake `$HOME`** (with `GOMODCACHE`/`GOCACHE`
  redirected into that sandbox), asserting: dry-run deletes nothing and reports
  correct totals; `--delete` removes artifacts but keeps marker files;
  `--global --delete` removes the seeded caches (`~/.m2`, `~/.gradle`, the
  redirected Go caches); and exit codes (`2` for bad path and for `-i`+`--quiet`
  / `-i` without a TTY). The container isolates destructive and global-cache
  behavior from the host. Opt-in: normal `go test ./...` does not run it.

## Deliverables

- The Go module and packages above, with tests.
- An `e2e/` directory: `e2e_test.go` (tag `e2e`), `Dockerfile`, and `run.sh` for
  Dockerized end-to-end testing.
- `README.md` documenting purpose, install/build, usage (all flags incl.
  `--interactive`/`-i`), the interactive TUI (with a screenshot or asciinema),
  the built-in project rules and global caches, the backup use case, safety
  notes (dry-run default), and exit codes.
