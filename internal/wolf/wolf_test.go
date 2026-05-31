package wolf

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func pathsOf(ts []Target) map[string]bool {
	m := map[string]bool{}
	for _, x := range ts {
		m[x.Path] = true
	}
	return m
}

func TestFindDetectsArtifactsIncludingDenoAndRuff(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "cs", "App.csproj"))
	mkdir(t, filepath.Join(root, "cs", "bin"))
	mkdir(t, filepath.Join(root, "cs", "obj"))
	touch(t, filepath.Join(root, "js", "package.json"))
	mkdir(t, filepath.Join(root, "js", "node_modules"))
	touch(t, filepath.Join(root, "dn", "deno.json"))
	mkdir(t, filepath.Join(root, "dn", "vendor"))
	touch(t, filepath.Join(root, "rf", "ruff.toml"))
	mkdir(t, filepath.Join(root, "rf", ".ruff_cache"))
	touch(t, filepath.Join(root, "rs", "Cargo.toml"))
	mkdir(t, filepath.Join(root, "rs", "target"))

	got := pathsOf(Find(Options{Root: root}))
	for _, want := range []string{
		filepath.Join(root, "cs", "bin"), filepath.Join(root, "cs", "obj"),
		filepath.Join(root, "js", "node_modules"),
		filepath.Join(root, "dn", "vendor"),
		filepath.Join(root, "rf", ".ruff_cache"),
		filepath.Join(root, "rs", "target"),
	} {
		if !got[want] {
			t.Errorf("missing target %q", want)
		}
	}
}

func TestFindNestedAndNoDescend(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "outer", "inner", "App.csproj"))
	mkdir(t, filepath.Join(root, "outer", "inner", "obj"))
	touch(t, filepath.Join(root, "py", "pyproject.toml"))
	mkdir(t, filepath.Join(root, "py", "build"))
	touch(t, filepath.Join(root, "py", "build", "pyproject.toml")) // trap
	mkdir(t, filepath.Join(root, "py", "build", "dist"))           // must be skipped

	got := pathsOf(Find(Options{Root: root}))
	if !got[filepath.Join(root, "outer", "inner", "obj")] {
		t.Error("nested project artifact not found")
	}
	if !got[filepath.Join(root, "py", "build")] {
		t.Error("py/build should be a target")
	}
	if got[filepath.Join(root, "py", "build", "dist")] {
		t.Error("must not descend into a matched artifact dir")
	}
}

func TestFindDedupAndSymlink(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "g", "build.gradle"))
	mkdir(t, filepath.Join(root, "g", "build"))
	n := 0
	for _, x := range Find(Options{Root: root}) {
		if x.Path == filepath.Join(root, "g", "build") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected build/ exactly once, got %d", n)
	}

	if runtime.GOOS != "windows" {
		touch(t, filepath.Join(root, "s", "Cargo.toml"))
		real := filepath.Join(root, "realtarget")
		mkdir(t, real)
		if err := os.Symlink(real, filepath.Join(root, "s", "target")); err != nil {
			t.Fatal(err)
		}
		if pathsOf(Find(Options{Root: root}))[filepath.Join(root, "s", "target")] {
			t.Error("symlinked artifact dir must not be a target")
		}
	}
}

func TestFindGlobalUsesScannedRoot(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, ".m2", "repository"))
	mkdir(t, filepath.Join(root, ".nuget", "packages"))

	// Caches in the real home directory must never be touched.
	home := t.TempDir()
	t.Setenv("HOME", home)
	mkdir(t, filepath.Join(home, ".ivy2", "cache"))

	got := pathsOf(Find(Options{Root: root, IncludeGlobal: true}))
	if !got[filepath.Join(root, ".m2", "repository")] {
		t.Error("expected <root>/.m2/repository global cache")
	}
	if !got[filepath.Join(root, ".nuget", "packages")] {
		t.Error("expected <root>/.nuget/packages global cache")
	}
	if got[filepath.Join(home, ".ivy2", "cache")] {
		t.Error("home-directory caches must not be discovered")
	}
}

func TestMeasure(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "a"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Join(d, "sub"))
	if err := os.WriteFile(filepath.Join(d, "sub", "b"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Measure(d); got != 150 {
		t.Errorf("Measure = %d, want 150", got)
	}
}

func TestDeletePermanentRemovesAndTreatsAbsentAsNoOp(t *testing.T) {
	d := t.TempDir()
	victim := filepath.Join(d, "bin")
	mkdir(t, victim)
	processed, failed := Delete([]Target{{Path: victim, Size: 42}}, Permanent)
	if len(failed) != 0 {
		t.Fatalf("unexpected failures %v", failed)
	}
	if processed != 42 {
		t.Errorf("processed = %d, want 42", processed)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("victim should be gone")
	}
	if _, failed := Delete([]Target{{Path: filepath.Join(d, "nope"), Size: 1}}, Permanent); len(failed) != 0 {
		t.Errorf("permanently deleting an absent dir must be a no-op, got %v", failed)
	}
}

func TestDeleteToTrashMovesAndRecordsTrashinfo(t *testing.T) {
	// Sandbox the trash so the test never touches the real ~/.local/share/Trash.
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	d := t.TempDir()
	victim := filepath.Join(d, "node_modules")
	mkdir(t, victim)
	if err := os.WriteFile(filepath.Join(victim, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	processed, failed := Delete([]Target{{Path: victim, Size: 99}}, ToTrash)
	if len(failed) != 0 {
		t.Fatalf("unexpected failures %v", failed)
	}
	if processed != 99 {
		t.Errorf("processed = %d, want 99", processed)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("victim should have left its original location")
	}
	// It should now live under the trash files/ dir, with a .trashinfo recording
	// the original path.
	if _, err := os.Stat(filepath.Join(xdg, "Trash", "files", "node_modules")); err != nil {
		t.Errorf("expected node_modules under trash files/: %v", err)
	}
	infoBytes, err := os.ReadFile(filepath.Join(xdg, "Trash", "info", "node_modules.trashinfo"))
	if err != nil {
		t.Fatalf("expected a .trashinfo: %v", err)
	}
	info := string(infoBytes)
	if !strings.HasPrefix(info, "[Trash Info]") || !strings.Contains(info, "Path=") || !strings.Contains(info, "DeletionDate=") {
		t.Errorf("malformed trashinfo:\n%s", info)
	}

	// A second trash of the same basename must not collide.
	victim2 := filepath.Join(t.TempDir(), "node_modules")
	mkdir(t, victim2)
	if _, failed := Delete([]Target{{Path: victim2, Size: 1}}, ToTrash); len(failed) != 0 {
		t.Fatalf("second trash failed: %v", failed)
	}
	if _, err := os.Stat(filepath.Join(xdg, "Trash", "files", "node_modules.1")); err != nil {
		t.Errorf("expected collision-suffixed node_modules.1 in trash: %v", err)
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 512: "512 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 1024 * 1024: "1.0 MiB"}
	for n, want := range cases {
		if got := FormatSize(n); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", n, got, want)
		}
	}
}
