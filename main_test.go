package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"it.kluth.buildcleaner/internal/wolf"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want options
	}{
		{
			name: "defaults to current dir, dry-run, trash",
			args: nil,
			want: options{path: "."},
		},
		{
			name: "flags before the path",
			args: []string{"--global", "--delete", "/tmp/x"},
			want: options{path: "/tmp/x", global: true, del: true},
		},
		{
			// The stdlib flag package stops at the first non-flag argument, so
			// this is the case the re-parse loop exists to support.
			name: "flags after the path",
			args: []string{"/tmp/x", "--global", "--delete"},
			want: options{path: "/tmp/x", global: true, del: true},
		},
		{
			name: "flags on both sides of the path",
			args: []string{"--quiet", "/tmp/x", "--no-trash"},
			want: options{path: "/tmp/x", quiet: true, noTrash: true},
		},
		{
			name: "last positional wins",
			args: []string{"a", "--global", "b"},
			want: options{path: "b", global: true},
		},
		{
			name: "interactive shorthand",
			args: []string{"-i", "/tmp/x"},
			want: options{path: "/tmp/x", interactive: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseArgs(tc.args, io.Discard)
			if err != nil {
				t.Fatalf("parseArgs(%q) error = %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("parseArgs(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestParseArgsInteractiveQuietConflict(t *testing.T) {
	_, err := parseArgs([]string{"-i", "--quiet"}, io.Discard)
	if !errors.Is(err, errInteractiveQuiet) {
		t.Errorf("error = %v, want errInteractiveQuiet", err)
	}
}

func TestParseArgsUnknownFlag(t *testing.T) {
	if _, err := parseArgs([]string{"--bogus"}, io.Discard); err == nil {
		t.Error("expected an error for an unknown flag")
	}
}

func TestParseArgsHelp(t *testing.T) {
	if _, err := parseArgs([]string{"-h"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("error = %v, want flag.ErrHelp", err)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunCLIDryRunKeepsFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "p", "package.json"))
	mustMkdir(t, filepath.Join(root, "p", "node_modules"))

	if code := runCLI(root, false /*global*/, false /*delete*/, false /*quiet*/, wolf.ToTrash); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "p", "node_modules")); err != nil {
		t.Error("dry-run must not delete node_modules")
	}
}

func TestRunCLIDeleteToTrashMovesToSandboxedTrash(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg) // keep the real trash untouched
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "p", "package.json"))
	mustMkdir(t, filepath.Join(root, "p", "node_modules"))

	if code := runCLI(root, false, true /*delete*/, true /*quiet*/, wolf.ToTrash); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "p", "node_modules")); !os.IsNotExist(err) {
		t.Error("--delete should have moved node_modules out of the tree")
	}
	if _, err := os.Stat(filepath.Join(xdg, "Trash", "files", "node_modules")); err != nil {
		t.Errorf("expected node_modules in the sandboxed trash: %v", err)
	}
}

func TestRunCLIDeleteNoTrashPermanentlyRemoves(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "p", "package.json"))
	mustMkdir(t, filepath.Join(root, "p", "node_modules"))

	if code := runCLI(root, false, true /*delete*/, true /*quiet*/, wolf.Permanent); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "p", "node_modules")); !os.IsNotExist(err) {
		t.Error("--delete --no-trash should have permanently removed node_modules")
	}
}
