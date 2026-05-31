//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildWolfe compiles the wolfe binary once and returns its path.
func buildWolfe(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "wolfe")
	cmd := exec.Command("go", "build", "-o", bin, "it.kluth.buildcleaner")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build wolfe: %v", err)
	}
	return bin
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureTree builds a tree of projects with artifacts and returns its root.
func fixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "cs", "App.csproj"), 1)
	writeFile(t, filepath.Join(root, "cs", "bin", "a.dll"), 2048)
	writeFile(t, filepath.Join(root, "cs", "obj", "a.o"), 1024)
	writeFile(t, filepath.Join(root, "js", "package.json"), 1)
	writeFile(t, filepath.Join(root, "js", "node_modules", "left-pad", "i.js"), 4096)
	writeFile(t, filepath.Join(root, "rs", "Cargo.toml"), 1)
	writeFile(t, filepath.Join(root, "rs", "target", "bin"), 8192)
	writeFile(t, filepath.Join(root, "py", "pyproject.toml"), 1)
	writeFile(t, filepath.Join(root, "py", "__pycache__", "m.pyc"), 512)
	writeFile(t, filepath.Join(root, ".m2", "repository", "a.jar"), 2048)
	writeFile(t, filepath.Join(root, ".gem", "b.irb"), 2048)
	return root
}

func run(t *testing.T, bin string, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	t.Fatalf("run %v: %v", args, err)
	return "", -1
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestE2EDryRunDeletesNothing(t *testing.T) {
	bin := buildWolfe(t)
	root := fixtureTree(t)

	out, code := run(t, bin, nil, root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "[dry-run] would move to trash:") {
		t.Errorf("missing dry-run header:\n%s", out)
	}
	if !strings.Contains(out, "node_modules") || !strings.Contains(out, "Total to trash:") {
		t.Errorf("dry-run output missing expected content:\n%s", out)
	}
	if !exists(filepath.Join(root, "js", "node_modules")) {
		t.Error("dry-run must not delete node_modules")
	}
}

func TestE2EDeleteRemovesArtifactsKeepsMarkers(t *testing.T) {
	bin := buildWolfe(t)
	root := fixtureTree(t)

	out, code := run(t, bin, nil, root, "--global", "--delete")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	for _, gone := range []string{
		filepath.Join(root, "cs", "bin"),
		filepath.Join(root, "cs", "obj"),
		filepath.Join(root, "js", "node_modules"),
		filepath.Join(root, "rs", "target"),
		filepath.Join(root, "py", "__pycache__"),
		filepath.Join(root, ".m2", "repository"),
		filepath.Join(root, ".gem"),
	} {
		if exists(gone) {
			t.Errorf("expected %s to be deleted", gone)
		}
	}
	for _, keep := range []string{
		filepath.Join(root, "cs", "App.csproj"),
		filepath.Join(root, "js", "package.json"),
		filepath.Join(root, "rs", "Cargo.toml"),
	} {
		if !exists(keep) {
			t.Errorf("marker %s must be kept", keep)
		}
	}
}

func TestE2EExitCodes(t *testing.T) {
	bin := buildWolfe(t)
	root := fixtureTree(t)

	if _, code := run(t, bin, nil, filepath.Join(root, "does-not-exist")); code != 2 {
		t.Errorf("invalid path: exit = %d, want 2", code)
	}
	if _, code := run(t, bin, nil, root, "-i", "--quiet"); code != 2 {
		t.Errorf("-i + --quiet: exit = %d, want 2", code)
	}
	// A subprocess pipe is not a TTY, so -i must refuse with exit 2.
	if _, code := run(t, bin, nil, root, "-i"); code != 2 {
		t.Errorf("-i without TTY: exit = %d, want 2", code)
	}
}

func TestE2EDeleteDefaultMovesToTrash(t *testing.T) {
	bin := buildWolfe(t)
	root := fixtureTree(t)
	xdg := filepath.Join(t.TempDir(), "data")

	out, code := run(t, bin, []string{"XDG_DATA_HOME=" + xdg}, root, "--delete")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if exists(filepath.Join(root, "js", "node_modules")) {
		t.Error("--delete should have moved node_modules out of the tree")
	}
	entries, err := os.ReadDir(filepath.Join(xdg, "Trash", "files"))
	if err != nil || len(entries) == 0 {
		t.Errorf("expected the XDG trash to receive items, got err=%v entries=%d", err, len(entries))
	}
}

func TestE2EDeleteNoTrashPermanentlyDeletes(t *testing.T) {
	bin := buildWolfe(t)
	root := fixtureTree(t)
	xdg := filepath.Join(t.TempDir(), "data")

	out, code := run(t, bin, []string{"XDG_DATA_HOME=" + xdg}, root, "--delete", "--no-trash")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	for _, gone := range []string{
		filepath.Join(root, "js", "node_modules"),
		filepath.Join(root, "rs", "target"),
	} {
		if exists(gone) {
			t.Errorf("expected %s to be permanently deleted", gone)
		}
	}
	// Permanent deletion must not populate the trash.
	if entries, err := os.ReadDir(filepath.Join(xdg, "Trash", "files")); err == nil && len(entries) > 0 {
		t.Errorf("--no-trash must not create trash entries, found %d", len(entries))
	}
}
