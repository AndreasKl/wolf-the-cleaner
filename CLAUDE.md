# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## ⚠️ Never run the `wolfe` binary against a real path

`wolfe` **deletes directories**, and `--delete --no-trash` is irreversible (`os.RemoveAll`). Do not build-and-run it to "try it out." Verify behavior with `go test ./...` (everything is sandboxed in `t.TempDir()`) or the Dockerized e2e suite — never by executing the binary on the host.

## Commands

```bash
go build -o wolfe .                                      # build (module path: it.kluth.buildcleaner)
gofmt -l .                                               # must print nothing
go vet ./...
go test ./...                                            # unit tests; does NOT include the e2e suite
go test ./internal/wolf -run TestFindNestedAndNoDescend  # a single test
golangci-lint run ./...                                  # config: .golangci.yml
./e2e/run.sh                                             # Dockerized e2e (build tag `e2e`); never run e2e outside Docker
```

CI gates merges on gofmt, vet, `go test ./...`, and golangci-lint.

## Architecture

The design is **hexagonal (ports and adapters)**: `internal/wolf` is the domain core, and `main.go` (CLI) and `internal/tui` (Bubble Tea) are adapters that depend inward on it — never the reverse. Both front-ends deal only in `wolf.Target` values; **all detection, sizing, and deletion live in `internal/wolf`** (unexported). Add filesystem logic to the core, not a front-end. External effects enter the core through injected ports (e.g. the TUI's `findFn`/`sizeFn`/`deleteFn`), keeping it testable; `internal/tui` is a thin presenter with no deletion/sizing of its own.

Non-obvious decisions — don't "simplify" these without understanding why:

- **Detection requires a project marker and the artifact directory present together**; symlinks are never followed; a matched artifact directory is never descended into. (Rule/cache tables: `internal/wolf/rules.go`; full flag and detection reference: `README.md`.)
- **Global caches are resolved *inside the scanned tree*, not against the real `$HOME`** — `GlobalCacheDef.RelPath` is joined onto every walked directory. Deliberate: scanning a *backup* of a home directory must find its caches.
- **Safety defaults:** dry-run unless `--delete`; move-to-trash unless `--no-trash`; global caches on unless `--no-global`. Trashing does not free disk space until the trash is emptied.
- `parseArgs` re-parses leftover args in a loop so flags may appear on either side of the positional path (`wolfe ~/x --delete`); `-i`/`--interactive` and `--quiet` are mutually exclusive.
- `internal/wolf/trash.go` implements the XDG trash spec from scratch: it writes `.trashinfo` *before* moving the directory (crash-safety) and falls back to copy-then-remove on `EXDEV`. Preserve that ordering.

## Code style

- **Write idiomatic Go, like the Go core team / standard library:** small focused functions, stdlib-style doc comments (a full sentence beginning with the identifier name), accept interfaces and return concrete types, wrap errors with `%w`, and no premature abstraction. Match the surrounding code.
- **Prefer the standard library.** Reach for a third-party dependency only when the stdlib genuinely cannot do the job (the TUI uses Bubble Tea/lipgloss; trashing is the from-scratch stdlib implementation above, not a dependency).
- **Apply SOLID principles.**
- **DRY is about concepts, not code.** Give each piece of *knowledge* a single source of truth, but don't merge code that merely looks similar. **A wrong abstraction is worse than duplication** — prefer incidental duplication of distinct concepts over a forced abstraction, and when an abstraction starts to fight the code, inline it back to duplication and re-derive the right one.

## Conventions

- **Test-first, outside-in — the _Growing Object-Oriented Software, Guided by Tests_ approach.** Write a failing test before the implementation, work from an acceptance/e2e test inward to the units, and treat test friction as design feedback to act on rather than work around. The `wolf` core's injected ports are a direct result of this pressure.
- **Commits:** Conventional Commits (`feat`/`fix`/`docs`/`test`/`refactor`/`chore`, `!` for breaking changes, scopes like `(tui)`/`(cli)`).
- **TUI changes:** `tui.Model` injects `findFn`/`sizeFn`/`deleteFn` (real `wolf` ops by default). Test new behavior by driving `Model.Update` with synthetic messages as `tui_test.go` does; don't reach into `wolf`.
- **Design-first:** non-trivial features get a dated spec in `docs/superpowers/specs/` and a plan in `docs/superpowers/plans/` before the code. Read the spec to learn *why* a default or invariant exists.
