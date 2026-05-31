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
	var global, del, quiet, interactive bool
	flag.BoolVar(&global, "global", false, "also include global per-user caches (~/.m2, ~/.gradle, ...)")
	flag.BoolVar(&del, "delete", false, "actually delete the listed directories (default: dry-run)")
	flag.BoolVar(&quiet, "quiet", false, "print only the totals")
	flag.BoolVar(&interactive, "interactive", false, "launch the interactive TUI")
	flag.BoolVar(&interactive, "i", false, "launch the interactive TUI (shorthand)")
	flag.Parse()

	path := "."
	if args := flag.Args(); len(args) > 0 {
		path = args[0]
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

	if interactive {
		failed, err := tui.Run(tui.Options{Root: path, Global: global})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if len(failed) > 0 {
			return 1
		}
		return 0
	}

	return runCLI(path, global, del, quiet)
}

func runCLI(path string, global, del, quiet bool) int {
	targets := wolf.Find(wolf.Options{Root: path, IncludeGlobal: global})
	for i := range targets {
		targets[i].Size = wolf.Measure(targets[i].Path)
	}

	report(os.Stdout, targets, !del, quiet)

	if del {
		reclaimed, failed := wolf.Delete(targets)
		fmt.Fprintf(os.Stdout, "Freed: %s across %d directories\n",
			wolf.FormatSize(reclaimed), len(targets)-len(failed))
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
// breakdown when both are present. quiet prints only the totals line.
func report(w io.Writer, targets []wolf.Target, dryRun, quiet bool) {
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

	if !quiet {
		if dryRun {
			fmt.Fprintln(w, "[dry-run] would delete:")
		} else {
			fmt.Fprintln(w, "deleting:")
		}
		for _, t := range targets {
			fmt.Fprintf(w, "  %10s   %s   (%s)\n", wolf.FormatSize(t.Size), t.Path, t.Kind)
		}
		fmt.Fprintln(w, "----")
	}

	line := fmt.Sprintf("Total reclaimable: %s across %d directories", wolf.FormatSize(total), len(targets))
	if local > 0 && global > 0 {
		line += fmt.Sprintf("  (local: %s / global: %s)", wolf.FormatSize(local), wolf.FormatSize(global))
	}
	fmt.Fprintln(w, line)

	if dryRun && !quiet {
		fmt.Fprintln(w, "Run with --delete to remove them.")
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
