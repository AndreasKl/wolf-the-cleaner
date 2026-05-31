// Package tui provides the interactive Bubble Tea front-end. (Stub — replaced
// in the TUI tasks.)
package tui

import "it.kluth.buildcleaner/internal/wolf"

// Options configures an interactive run.
type Options struct {
	Root   string
	Global bool
}

// Run launches the interactive TUI and returns any deletion failures.
func Run(opts Options) (failed []wolf.Failure, err error) {
	return nil, nil
}
