package main

import (
	"os"
	"path/filepath"
	"testing"

	"it.kluth.buildcleaner/internal/wolf"
)

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
