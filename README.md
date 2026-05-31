# Wolf the Cleaner

> "I'm Winston Wolfe. I solve problems." — the cleaner from *Pulp Fiction*.

`wolfe` is a small Go CLI that walks a tree of programming projects and reports
— or deletes — regenerable build artifacts (`bin`/`obj`, `node_modules`,
`target`, `build`, …). It's built to **shrink backups**: run it over your
`Coding` directory (or a backup copy) so regenerable output is excluded and the
backup gets smaller. Everything it removes can be regenerated (dependencies
re-download, build output recompiles) — the Wolf cleans up the mess and leaves
no trace.

It can optionally also clean shared, per-user package caches (`~/.m2`,
`~/.gradle/caches`, the Go module cache, …), and offers a polished interactive
TUI for hands-on cleanup.

## Safety first

`wolfe` deletes directories permanently (`os.RemoveAll` — there is no trash), so
it is **dry-run by default**: a plain run only *lists* what it would remove and
how much space you'd reclaim. Nothing is deleted unless you pass `--delete` (or
confirm inside the TUI). Detection requires a real project marker next to the
artifact, symlinks are never followed, and matched artifact directories aren't
descended into.

## Install

```bash
git clone git@github.com:AndreasKl/wolf-the-cleaner.git
cd wolf-the-cleaner
go build -o wolfe .
# then put it on your PATH, e.g.
sudo mv wolfe /usr/local/bin/
```

## Usage

```
wolfe [path] [flags]
```

- `path` — directory to scan (default `.`).
- `--global` — also include global per-user caches in the same report/delete.
- `--delete` — actually delete (without it, the run is a **dry-run**).
- `--quiet` — print only the totals (handy in backup scripts).
- `--interactive`, `-i` — launch the interactive TUI (requires a terminal).

`--interactive` and `--quiet` are mutually exclusive. In interactive mode,
`--delete` is ignored — deletion is confirmed in the TUI.

### Examples

```bash
wolfe ~/Coding                 # dry-run: list artifacts + total size
wolfe ~/Coding --delete        # delete project artifacts
wolfe ~/Coding --global        # dry-run incl. global caches
wolfe ~/Coding --global --delete --quiet   # backup script form
wolfe ~/Coding -i              # interactive select-and-delete
```

Dry-run output:

```
[dry-run] would delete:
   4.2 GiB   /home/you/Coding/js/foo/node_modules   (JavaScript/TS)
   120 MiB   /home/you/Coding/cs/bar/bin            (C#/.NET)
----
Total reclaimable: 4.3 GiB across 2 directories
Run with --delete to remove them.
```

### Interactive TUI

Scans with a spinner, then shows a scrollable, filterable checklist sorted
largest-first (sizes fill in lazily as they're computed). Keys: `↑/↓` move,
`space` toggle, `a` toggle all, `g` globals-only, `/` filter, `enter` confirm &
delete, `q` quit. A confirmation step gates all deletion.

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

Global caches (with `--global`): Maven, Ivy, Gradle, NuGet, npm, Yarn, pnpm,
pip, Cargo (registry + git), Pub, Deno (`$DENO_DIR`), Gem, Crystal shards, and
the Go module/build caches (`go env GOMODCACHE`/`GOCACHE`). Each is resolved via
a relevant environment variable, then `go env`, then a `$HOME` fallback, and is
included only if it actually exists.

## Exit codes

- `0` — success (including dry-run).
- `1` — one or more deletions failed.
- `2` — invalid arguments or invalid path.

## Development

```bash
go test ./...          # unit tests (sandboxed in temp dirs)
./e2e/run.sh           # Dockerized end-to-end tests (build tag `e2e`)
```

End-to-end behavior — including the destructive `--delete`/`--global` paths — is
exercised only **inside a Docker container** against a throwaway tree and an
isolated `$HOME`, so it never touches your real files.
