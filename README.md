# Wolf the Cleaner

> "I'm Winston Wolfe. I solve problems." - the cleaner from *Pulp Fiction*.

`wolfe` is a small Go CLI that walks a tree of programming projects and reports - or deletes - regenerable build artifacts (`bin`/`obj`, `node_modules`,
`target`, `build`, …). It's built to **shrink backups**: run it over your
`Coding` directory (or a backup copy) so regenerable output is excluded and the
backup gets smaller. Everything it removes can be regenerated (dependencies
re-download, build output recompiles) - the Wolf cleans up the mess and leaves
no trace.

It also cleans shared, per-user package caches (`./.m2`, `./.gradle/caches`) by
default (pass `--no-global` to skip), and offers a polished interactive
TUI for hands-on cleanup.

> **Platform:** `wolfe` is built and tested primarily for **Linux**. The trash
> support follows the FreeDesktop.org (XDG) Trash spec used by Linux desktops;
> on other platforms use `--no-trash` (permanent deletion), which works
> everywhere.

## Safety first

`wolfe` never touches anything unless you ask: it is **dry-run by default** — a
plain run only *lists* what it would remove and how much space it covers.
Detection requires a real project marker next to the artifact, symlinks are
never followed, and matched artifact directories aren't descended into.

When you do delete (`--delete`, or confirming in the TUI), the default is to
**move directories to the trash** (the XDG trash at `~/.local/share/Trash`), so
a mistake is recoverable. Note that trashing does **not** immediately free disk
space — items sit in the trash until it's emptied. To reclaim space for real
(especially for large global caches), add **`--no-trash`** to delete
permanently (`os.RemoveAll`, irreversible).

## Install

```bash
git clone git@github.com:AndreasKl/wolf-the-cleaner.git
cd wolf-the-cleaner
go build -o wolfe .
# then put it on your PATH, e.g.
sudo mv wolfe /usr/local/bin/
```

## Usage

```text
wolfe [path] [flags]
```

- `path` - directory to scan (default `.`).
- `--no-global` - exclude global per-user caches (`~/.m2`, `~/.gradle`, ...). Global caches are **included by default** in every mode.
- `--delete` - actually dispose of the listed directories (without it, the run is a **dry-run**). Default disposal is **move to trash**.
- `--no-trash` - permanently delete instead of trashing (irreversible; frees disk space immediately). A mode modifier — on its own it only changes what the dry-run *previews*.
- `--quiet` - print only the totals (handy in backup scripts).
- `--interactive`, `-i` - launch the interactive TUI (requires a terminal).

`--interactive` and `--quiet` are mutually exclusive. In interactive mode,
disposal is confirmed in the TUI (and `--no-trash` selects permanent deletion there too).

### Examples

```bash
wolfe ~/Coding                          # dry-run incl. global caches: would move to trash
wolfe ~/Coding --no-trash               # dry-run incl. global caches: would PERMANENTLY delete
wolfe ~/Coding --delete                 # move artifacts (incl. global caches) to the trash
wolfe ~/Coding --delete --no-trash      # permanently delete (frees space now)
wolfe ~/Coding --no-global              # dry-run of local build artifacts only
wolfe ~/Coding --delete --no-trash --quiet   # backup script: reclaim space (incl. caches)
wolfe ~/Coding -i                       # interactive select-and-delete
```

Dry-run output:

```text
[dry-run] would move to trash:
   4.2 GiB   /home/you/Coding/js/foo/node_modules   (JavaScript/TS)
   120 MiB   /home/you/Coding/cs/bar/bin            (C#/.NET)
----
Total to trash: 4.3 GiB across 2 directories
Run with --delete to move them to the trash (recoverable; use --no-trash to delete for good).
```

### Interactive TUI

Interactive mode scans local artifacts **and** global caches by default (pass
`--no-global` to skip the caches). The list starts with **nothing selected** —
pick what to remove, or press `a` to select all. While scanning shows a
scrollable, filterable checklist sorted largest-first (sizes fill in lazily as
they're computed). Keys: `↑/↓` move, `space` toggle, `a` toggle all, `g`
globals-only (shown only when the scan found global caches), `/` filter, `enter`
confirm & delete, `q` quit. A confirmation step gates all deletion.

## Detection

A directory is only flagged when a **project marker** and the **artifact
directory** are present together. Artifact lists follow the canonical
[`github/gitignore`](https://github.com/github/gitignore) templates per
ecosystem (and the Crystal/Deno docs).

| Type | Markers | Artifacts |
|---|---|---|
| C#/.NET | `*.csproj`, `*.sln`, `*.fsproj`, `*.vbproj` | `bin`, `obj` |
| JavaScript/TS | `package.json` | `node_modules`, `dist`, `build`, `.next`, `.nuxt`, `out`, `.output`, `.svelte-kit`, `.parcel-cache`, `.turbo`, `.vite`, `coverage`, `.cache` |
| Deno | `deno.json`, `deno.jsonc`, `deno.lock` | `node_modules`, `vendor` |
| Rust | `Cargo.toml` | `target` |
| Maven | `pom.xml` | `target` |
| Gradle | `build.gradle(.kts)`, `settings.gradle(.kts)` | `build`, `.gradle` |
| Android | `settings.gradle(.kts)` + `gradlew` | `build`, `.gradle`, `app/build`, `.cxx`, `.externalNativeBuild`, `captures` |
| Flutter/Dart | `pubspec.yaml` | `build`, `.dart_tool` |
| Ruby | `Gemfile`, `*.gemspec` | `vendor/bundle`, `.bundle`, `.yardoc`, `coverage`, `pkg` |
| Python | `pyproject.toml`, `setup.py`, `setup.cfg`, `requirements.txt` | `__pycache__`, `.venv`, `venv`, `*.egg-info`, `.eggs`, `build`, `dist`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`, `.tox`, `.nox`, `htmlcov`, `.hypothesis` |
| Ruff | `ruff.toml`, `.ruff.toml` | `.ruff_cache` |
| Crystal | `shard.yml` | `lib`, `.shards`, `bin` |

> **Go** has no local rule on purpose: a Go checkout has no canonical
> reclaimable directory (binaries are loose files, `vendor/` is usually
> committed). Go's reclaimable space is its global module/build caches below.

Global caches (included by default; use `--no-global` to skip): Maven (`.m2/repository`), Ivy (`.ivy2/cache`),
Gradle (`.gradle/caches`), NuGet (`.nuget/packages`), npm (`.npm`), Yarn
(`.cache/yarn`), pnpm (`.local/share/pnpm/store`), pip (`.cache/pip`), Cargo
(`.cargo/registry`, `.cargo/git`), Pub (`.pub-cache`), Deno (`.cache/deno`), Gem
(`.gem`), Crystal shards (`.cache/shards`), and the Go module/build caches
(`go/pkg/mod`, `.cache/go-build`). Each is identified by its conventional path
relative to a home directory and is looked for **inside the scanned tree** — so
pointing `wolfe` at a backup of your home directory finds the backed-up caches.
A cache is included only if it actually exists.

## Exit codes

- `0` - success (including dry-run).
- `1` - one or more deletions failed.
- `2` - invalid arguments or invalid path.

## Development

```bash
gofmt -l .             # formatting check (should print nothing)
go vet ./...           # vet
go test ./...          # unit tests (sandboxed in temp dirs)
golangci-lint run ./...# linters (config in .golangci.yml)
./e2e/run.sh           # Dockerized end-to-end tests (build tag `e2e`)
```

CI (`.github/workflows/ci.yml`) runs gofmt, vet, tests, and golangci-lint on
every push and PR.

End-to-end behavior - including the destructive `--delete` path - is
exercised only **inside a Docker container** against a throwaway tree and an
isolated `$HOME`, so it never touches your real files.
