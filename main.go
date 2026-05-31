// Command wolfe (Wolf the Cleaner) reports or deletes regenerable build
// artifacts under a directory tree, with an opt-in global-cache mode and an
// interactive TUI. Module path: it.kluth.buildcleaner.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"it.kluth.buildcleaner/internal/tui"
	"it.kluth.buildcleaner/internal/wolf"
)

func main() {
	os.Exit(run())
}

func run() int {
	var global, del, quiet, interactive, noTrash bool
	flag.BoolVar(&global, "global", false, "also include global per-user caches (~/.m2, ~/.gradle, ...)")
	flag.BoolVar(&del, "delete", false, "actually dispose of the listed directories (default: dry-run)")
	flag.BoolVar(&noTrash, "no-trash", false, "permanently delete instead of moving to the trash")
	flag.BoolVar(&quiet, "quiet", false, "print only the totals")
	flag.BoolVar(&interactive, "interactive", false, "launch the interactive TUI")
	flag.BoolVar(&interactive, "i", false, "launch the interactive TUI (shorthand)")
	flag.Parse()

	// The stdlib flag package stops at the first non-flag argument, so
	// `wolfe ~/Coding --delete` (path before flags) would otherwise ignore
	// --delete. Re-parse the leftovers so flags may appear on either side of the
	// positional path; the last positional wins.
	path := "."
	for args := flag.Args(); len(args) > 0; args = flag.Args() {
		path = args[0]
		flag.CommandLine.Parse(args[1:])
	}

	if interactive && quiet {
		fmt.Fprintln(os.Stderr, "error: --interactive and --quiet are mutually exclusive")
		return 2
	}
	if interactive && !isTTY(os.Stdout) {
		fmt.Fprintln(os.Stderr, "error: --interactive requires a terminal; drop -i for non-interactive output")
		return 2
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "error: %q is not a directory\n", path)
		return 2
	}

	how := wolf.ToTrash
	if noTrash {
		how = wolf.Permanent
	}

	if interactive {
		failed, err := tui.Run(tui.Options{Root: path, Global: global, Permanent: noTrash})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if len(failed) > 0 {
			return 1
		}
		return 0
	}

	return runCLI(path, global, del, quiet, how)
}

func runCLI(path string, global, del, quiet bool, how wolf.Disposal) int {
	targets := wolf.Find(wolf.Options{Root: path, IncludeGlobal: global})
	for i := range targets {
		targets[i].Size = wolf.Measure(targets[i].Path)
	}

	report(os.Stdout, targets, !del, quiet, how)

	if del {
		processed, failed := wolf.Delete(targets, how)
		verb := "Moved to trash"
		if how == wolf.Permanent {
			verb = "Freed"
		}
		fmt.Fprintf(os.Stdout, "%s: %s across %d directories\n",
			verb, wolf.FormatSize(processed), len(targets)-len(failed))
		for _, f := range failed {
			fmt.Fprintf(os.Stderr, "failed: %s: %v\n", f.Path, f.Err)
		}
		if len(failed) > 0 {
			return 1
		}
	}
	return 0
}

// report renders the target listing and totals — CLI presentation, kept out of
// wolf. Targets are sorted largest-first; the total shows a local/global
// breakdown when both are present. quiet prints only the totals line. The
// wording reflects the disposal mode (trash vs permanent).
func report(w io.Writer, targets []wolf.Target, dryRun, quiet bool, how wolf.Disposal) {
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].Size > targets[j].Size })

	var total, local, global int64
	for _, t := range targets {
		total += t.Size
		if t.Global {
			global += t.Size
		} else {
			local += t.Size
		}
	}

	permanent := how == wolf.Permanent
	if !quiet {
		switch {
		case dryRun && permanent:
			fmt.Fprintln(w, "[dry-run] would permanently delete:")
		case dryRun:
			fmt.Fprintln(w, "[dry-run] would move to trash:")
		case permanent:
			fmt.Fprintln(w, "deleting permanently:")
		default:
			fmt.Fprintln(w, "moving to trash:")
		}
		for _, t := range targets {
			fmt.Fprintf(w, "  %10s   %s   (%s)\n", wolf.FormatSize(t.Size), t.Path, t.Kind)
		}
		fmt.Fprintln(w, "----")
	}

	noun := "reclaimable"
	if !permanent {
		noun = "to trash" // not freed from disk until the trash is emptied
	}
	line := fmt.Sprintf("Total %s: %s across %d directories", noun, wolf.FormatSize(total), len(targets))
	if local > 0 && global > 0 {
		line += fmt.Sprintf("  (local: %s / global: %s)", wolf.FormatSize(local), wolf.FormatSize(global))
	}
	fmt.Fprintln(w, line)

	if dryRun && !quiet {
		if permanent {
			fmt.Fprintln(w, "Run with --delete --no-trash to permanently delete them.")
		} else {
			fmt.Fprintln(w, "Run with --delete to move them to the trash (recoverable; use --no-trash to delete for good).")
		}
	}
}

// isTTY reports whether f is an interactive terminal (a character device).
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
