package main

import (
	"os"
	"path/filepath"
	"testing"
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

	if code := runCLI(root, false /*global*/, false /*delete*/, false /*quiet*/); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "p", "node_modules")); err != nil {
		t.Error("dry-run must not delete node_modules")
	}
}

func TestRunCLIDeleteRemovesArtifacts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "p", "package.json"))
	mustMkdir(t, filepath.Join(root, "p", "node_modules"))

	if code := runCLI(root, false, true /*delete*/, true /*quiet*/); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "p", "node_modules")); !os.IsNotExist(err) {
		t.Error("--delete should have removed node_modules")
	}
}
