# build-cleaner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go CLI (`it.kluth.buildcleaner`) that walks a project tree and reports (dry-run by default) or deletes regenerable build artifacts to shrink backups, with an opt-in `--global` cache mode and an optional Bubble Tea select-and-delete TUI.

**Architecture:** A pure-stdlib core split into three packages — `rules` (data + matching), `scanner` (tree walk → candidates), `cleaner` (size/report/delete) — plus a `tui` package that is a thin Bubble Tea view over that core. `main.go` parses flags and dispatches to the non-interactive CLI path or the TUI.

**Tech Stack:** Go 1.26, standard library for the core; `github.com/charmbracelet/bubbletea` v1.3.10, `bubbles` v1.0.0, `lipgloss` v1.1.0 for the TUI.

**Spec:** `docs/superpowers/specs/2026-05-31-build-cleaner-design.md`

---

## File Structure

- `go.mod` — module `it.kluth.buildcleaner`, go 1.26, Charm deps
- `internal/rules/rules.go` — `Rule`, `ProjectRules`, `Rule.Matches`, `GlobalCacheDef`, `GlobalCacheDefs`
- `internal/rules/rules_test.go`
- `internal/scanner/scanner.go` — `Scope`, `Candidate`, `ScanLocal`, `ScanGlobal`
- `internal/scanner/scanner_test.go`
- `internal/cleaner/cleaner.go` — `SizedCandidate`, `DirSize`, `HumanSize`, `ReportOpts`, `Report`, `Delete`
- `internal/cleaner/cleaner_test.go`
- `internal/tui/tui.go` — `Options`, `Model`, messages, `New`, `Init`, `Update`, helpers, `Run`
- `internal/tui/view.go` — `View`
- `internal/tui/tui_test.go`
- `main.go` — flag parsing, dispatch, `runCLI`, `isTTY`
- `main_test.go` — integration over a temp tree
- `README.md`

---

## Task 1: Module and dependencies

**Files:**
- Create: `go.mod` (via `go mod init`)

- [ ] **Step 1: Initialize the module**

Run:
```bash
cd /home/andreaskluth/Coding/go/build-cleaner
go mod init it.kluth.buildcleaner
```
Expected: creates `go.mod` with `module it.kluth.buildcleaner` and a `go` directive.

- [ ] **Step 2: Add the Charm dependencies**

Run:
```bash
go get github.com/charmbracelet/bubbletea@v1.3.10
go get github.com/charmbracelet/bubbles@v1.0.0
go get github.com/charmbracelet/lipgloss@v1.1.0
```
Expected: `go.mod` lists the three requires; `go.sum` is created. (No code imports them yet, so they may be marked `// indirect` until Task 6 — that is fine.)

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: init go module it.kluth.buildcleaner with Charm deps"
```

---

## Task 2: rules package

**Files:**
- Create: `internal/rules/rules.go`
- Test: `internal/rules/rules_test.go`

- [ ] **Step 1: Write the failing test**

`internal/rules/rules_test.go`:
```go
package rules

import "testing"

func TestRuleMatches(t *testing.T) {
	csharp := Rule{Name: "C#/.NET", Markers: []string{"*.csproj", "*.sln"}}
	if !csharp.Matches([]string{"App.csproj", "Program.cs"}) {
		t.Error("expected glob marker *.csproj to match App.csproj")
	}
	if csharp.Matches([]string{"main.go"}) {
		t.Error("did not expect C# rule to match a Go directory")
	}

	goRule := Rule{Name: "Go", Markers: []string{"go.mod"}}
	if !goRule.Matches([]string{"go.mod", "main.go"}) {
		t.Error("expected literal marker go.mod to match")
	}

	android := Rule{
		Name:        "Android",
		Markers:     []string{"settings.gradle", "settings.gradle.kts"},
		AlsoRequire: []string{"gradlew"},
	}
	if android.Matches([]string{"settings.gradle"}) {
		t.Error("Android must require gradlew too")
	}
	if !android.Matches([]string{"settings.gradle", "gradlew"}) {
		t.Error("Android should match with settings.gradle + gradlew")
	}
}

func TestProjectRulesNonEmpty(t *testing.T) {
	if len(ProjectRules) < 9 {
		t.Fatalf("expected at least 9 built-in rules, got %d", len(ProjectRules))
	}
	for _, r := range ProjectRules {
		if r.Name == "" || len(r.Markers) == 0 || len(r.Artifacts) == 0 {
			t.Errorf("rule %+v is incompletely defined", r)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rules/`
Expected: FAIL — `undefined: Rule` / package has no Go files.

- [ ] **Step 3: Write the implementation**

`internal/rules/rules.go`:
```go
// Package rules holds the built-in table mapping project types to the build
// artifacts they produce, plus the global per-user cache locations.
package rules

import "path/filepath"

// Rule maps a kind of project to the build artifacts it produces. A rule
// matches a directory when at least one Marker is present in that directory
// and, if AlsoRequire is non-empty, at least one of those is present too.
type Rule struct {
	Name        string   // informational label, e.g. "C#/.NET"
	Markers     []string // filenames or globs identifying the project
	AlsoRequire []string // optional: at least one must also be present
	Artifacts   []string // child dirs to delete; may be a glob or contain "/"
}

// ProjectRules is the built-in rule table.
var ProjectRules = []Rule{
	{Name: "C#/.NET", Markers: []string{"*.csproj", "*.sln", "*.fsproj"}, Artifacts: []string{"bin", "obj"}},
	{Name: "JavaScript/TS", Markers: []string{"package.json"}, Artifacts: []string{"node_modules", "dist", "build", ".next", ".nuxt"}},
	{Name: "Rust", Markers: []string{"Cargo.toml"}, Artifacts: []string{"target"}},
	{Name: "Java", Markers: []string{"pom.xml", "build.gradle", "build.gradle.kts"}, Artifacts: []string{"target", "build", ".gradle"}},
	{Name: "Kotlin", Markers: []string{"build.gradle.kts", "*.kts", "settings.gradle", "settings.gradle.kts"}, Artifacts: []string{"build", ".gradle", "out"}},
	{Name: "Android", Markers: []string{"settings.gradle", "settings.gradle.kts"}, AlsoRequire: []string{"gradlew"}, Artifacts: []string{"build", ".gradle", "app/build", ".cxx"}},
	{Name: "Flutter/Dart", Markers: []string{"pubspec.yaml"}, Artifacts: []string{"build", ".dart_tool", ".flutter-plugins", ".packages"}},
	{Name: "Go", Markers: []string{"go.mod"}, Artifacts: []string{"bin"}},
	{Name: "Ruby", Markers: []string{"Gemfile", "*.gemspec"}, Artifacts: []string{"vendor/bundle", ".bundle"}},
	{Name: "Python", Markers: []string{"pyproject.toml", "setup.py", "requirements.txt"}, Artifacts: []string{"__pycache__", ".venv", "venv", "*.egg-info", "build", "dist", ".pytest_cache", ".mypy_cache"}},
	{Name: "Crystal", Markers: []string{"shard.yml"}, Artifacts: []string{"lib", ".shards", "bin"}},
}

// anyMatch reports whether any name matches any of the patterns (filepath.Match
// handles both literal names and globs).
func anyMatch(patterns, names []string) bool {
	for _, p := range patterns {
		for _, n := range names {
			if ok, _ := filepath.Match(p, n); ok {
				return true
			}
		}
	}
	return false
}

// Matches reports whether the rule applies to a directory whose immediate entry
// names are given.
func (r Rule) Matches(names []string) bool {
	if !anyMatch(r.Markers, names) {
		return false
	}
	if len(r.AlsoRequire) > 0 && !anyMatch(r.AlsoRequire, names) {
		return false
	}
	return true
}

// GlobalCacheDef defines a global per-user cache location.
type GlobalCacheDef struct {
	Name     string // informational label, e.g. "Maven"
	RelPath  string // path relative to the user's home directory
	GoEnvKey string // if set, resolved via `go env <key>`, RelPath as fallback
}

// GlobalCacheDefs is the built-in list of global cache locations.
var GlobalCacheDefs = []GlobalCacheDef{
	{Name: "Maven", RelPath: ".m2/repository"},
	{Name: "Ivy", RelPath: ".ivy2/cache"},
	{Name: "Gradle", RelPath: ".gradle/caches"},
	{Name: "NuGet", RelPath: ".nuget/packages"},
	{Name: "npm", RelPath: ".npm"},
	{Name: "Yarn", RelPath: ".cache/yarn"},
	{Name: "pip", RelPath: ".cache/pip"},
	{Name: "Cargo", RelPath: ".cargo/registry"},
	{Name: "Pub", RelPath: ".pub-cache"},
	{Name: "Gem", RelPath: ".gem"},
	{Name: "Go module cache", RelPath: "go/pkg/mod", GoEnvKey: "GOMODCACHE"},
	{Name: "Go build cache", RelPath: ".cache/go-build", GoEnvKey: "GOCACHE"},
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rules/`
Expected: PASS (ok).

- [ ] **Step 5: Commit**

```bash
git add internal/rules/
git commit -m "feat: add rules package with built-in project rules and cache defs"
```

---

## Task 3: scanner package

**Files:**
- Create: `internal/scanner/scanner.go`
- Test: `internal/scanner/scanner_test.go`

- [ ] **Step 1: Write the failing test**

`internal/scanner/scanner_test.go`:
```go
package scanner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// touch creates an empty file, making parent dirs as needed.
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

func paths(cands []Candidate) map[string]bool {
	m := map[string]bool{}
	for _, c := range cands {
		m[c.Path] = true
	}
	return m
}

func TestScanLocalDetectsArtifacts(t *testing.T) {
	root := t.TempDir()

	// A C# project with bin/ and obj/.
	touch(t, filepath.Join(root, "cs", "App.csproj"))
	mkdir(t, filepath.Join(root, "cs", "bin"))
	mkdir(t, filepath.Join(root, "cs", "obj"))

	// A JS-less Go project (markers only matter via rules) with bin/.
	touch(t, filepath.Join(root, "go", "go.mod"))
	mkdir(t, filepath.Join(root, "go", "bin"))

	// A nested project inside the tree (monorepo) — must still be found.
	touch(t, filepath.Join(root, "outer", "go.mod"))
	touch(t, filepath.Join(root, "outer", "inner", "App.csproj"))
	mkdir(t, filepath.Join(root, "outer", "inner", "obj"))

	cands, warnings := ScanLocal(root)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	got := paths(cands)
	for _, want := range []string{
		filepath.Join(root, "cs", "bin"),
		filepath.Join(root, "cs", "obj"),
		filepath.Join(root, "go", "bin"),
		filepath.Join(root, "outer", "inner", "obj"),
	} {
		if !got[want] {
			t.Errorf("expected candidate %q, missing", want)
		}
	}
}

func TestScanLocalDoesNotDescendIntoArtifacts(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "js", "package.json")) // not a rule marker here, but...
	// Use a real artifact: a Python project with a build/ dir that itself
	// contains a marker file that must NOT produce a nested candidate.
	touch(t, filepath.Join(root, "py", "pyproject.toml"))
	mkdir(t, filepath.Join(root, "py", "build"))
	touch(t, filepath.Join(root, "py", "build", "pyproject.toml")) // trap
	mkdir(t, filepath.Join(root, "py", "build", "dist"))           // must be skipped

	cands, _ := ScanLocal(root)
	got := paths(cands)
	if !got[filepath.Join(root, "py", "build")] {
		t.Error("expected py/build to be a candidate")
	}
	if got[filepath.Join(root, "py", "build", "dist")] {
		t.Error("must not descend into a matched artifact dir")
	}
}

func TestScanLocalDedupesAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	// Gradle markers match Java and Kotlin -> build/ should appear once.
	touch(t, filepath.Join(root, "j", "build.gradle"))
	mkdir(t, filepath.Join(root, "j", "build"))

	cands, _ := ScanLocal(root)
	count := 0
	for _, c := range cands {
		if c.Path == filepath.Join(root, "j", "build") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected build/ candidate exactly once, got %d", count)
	}

	if runtime.GOOS != "windows" {
		// A symlinked artifact dir must not be deleted/followed.
		touch(t, filepath.Join(root, "s", "go.mod"))
		realTarget := filepath.Join(root, "realbin")
		mkdir(t, realTarget)
		if err := os.Symlink(realTarget, filepath.Join(root, "s", "bin")); err != nil {
			t.Fatal(err)
		}
		cands, _ := ScanLocal(root)
		if paths(cands)[filepath.Join(root, "s", "bin")] {
			t.Error("symlinked artifact dir must not be a candidate")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/scanner/`
Expected: FAIL — `undefined: Candidate` / `undefined: ScanLocal`.

- [ ] **Step 3: Write the implementation**

`internal/scanner/scanner.go`:
```go
// Package scanner walks a directory tree and produces deletion candidates, and
// resolves the global per-user caches that currently exist.
package scanner

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"it.kluth.buildcleaner/internal/rules"
)

// Scope distinguishes per-project artifacts from shared global caches.
type Scope int

const (
	Local Scope = iota
	Global
)

// Candidate is one directory that may be deleted.
type Candidate struct {
	Path  string // absolute or root-relative path of the directory
	Type  string // informational label, e.g. "C#/.NET" or "Maven (global)"
	Scope Scope
}

// ScanLocal walks root, returning the project-artifact directories found.
// Per-directory errors (e.g. permission denied) are returned as warnings and do
// not abort the walk. Matched artifact dirs are not descended into, nested
// projects are still discovered, and symlinks are not followed.
func ScanLocal(root string) (cands []Candidate, warnings []error) {
	skip := map[string]bool{}
	seen := map[string]bool{}

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			warnings = append(warnings, err)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if skip[path] {
			return filepath.SkipDir
		}

		entries, e := os.ReadDir(path)
		if e != nil {
			warnings = append(warnings, e)
			return nil
		}
		var names, dirNames []string
		for _, en := range entries {
			names = append(names, en.Name())
			if en.IsDir() { // IsDir is false for symlinks -> they are excluded
				dirNames = append(dirNames, en.Name())
			}
		}

		for _, rule := range rules.ProjectRules {
			if !rule.Matches(names) {
				continue
			}
			for _, art := range rule.Artifacts {
				for _, ap := range resolveArtifact(path, art, dirNames) {
					if seen[ap] {
						continue
					}
					seen[ap] = true
					skip[ap] = true
					cands = append(cands, Candidate{Path: ap, Type: rule.Name, Scope: Local})
				}
			}
		}
		return nil
	})
	return cands, warnings
}

// resolveArtifact turns one artifact spec into the existing directory paths it
// refers to within dir. Specs containing "/" are treated as a relative path;
// specs with glob metacharacters are matched against immediate subdirectories;
// otherwise the spec is a literal directory name.
func resolveArtifact(dir, art string, dirNames []string) []string {
	if strings.Contains(art, "/") {
		p := filepath.Join(dir, art)
		if fi, err := os.Lstat(p); err == nil && fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
			return []string{p}
		}
		return nil
	}
	if strings.ContainsAny(art, "*?[") {
		var out []string
		for _, n := range dirNames {
			if ok, _ := filepath.Match(art, n); ok {
				out = append(out, filepath.Join(dir, n))
			}
		}
		return out
	}
	for _, n := range dirNames {
		if n == art {
			return []string{filepath.Join(dir, art)}
		}
	}
	return nil
}

// ScanGlobal returns the built-in global cache directories that currently
// exist, resolved against the user's home directory and `go env`.
func ScanGlobal() []Candidate {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []Candidate
	for _, def := range rules.GlobalCacheDefs {
		path := ""
		if def.GoEnvKey != "" {
			if p := goEnv(def.GoEnvKey); p != "" {
				path = p
			}
		}
		if path == "" {
			path = filepath.Join(home, def.RelPath)
		}
		if fi, err := os.Lstat(path); err == nil && fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
			out = append(out, Candidate{Path: path, Type: def.Name + " (global)", Scope: Global})
		}
	}
	return out
}

// goEnv returns the value of `go env <key>`, or "" if go is unavailable.
func goEnv(key string) string {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/scanner/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/scanner/
git commit -m "feat: add scanner with marker+artifact detection, no symlink follow"
```

---

## Task 4: cleaner package

**Files:**
- Create: `internal/cleaner/cleaner.go`
- Test: `internal/cleaner/cleaner_test.go`

- [ ] **Step 1: Write the failing test**

`internal/cleaner/cleaner_test.go`:
```go
package cleaner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"it.kluth.buildcleaner/internal/scanner"
)

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:                  "0 B",
		512:                "512 B",
		1024:               "1.0 KiB",
		1536:               "1.5 KiB",
		1024 * 1024:        "1.0 MiB",
		3 * 1024 * 1024 * 1024: "3.0 GiB",
	}
	for n, want := range cases {
		if got := HumanSize(n); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 150 {
		t.Errorf("DirSize = %d, want 150", got)
	}
}

func TestReportDryRunAndTotals(t *testing.T) {
	sized := []SizedCandidate{
		{Candidate: scanner.Candidate{Path: "/x/node_modules", Type: "JS/TS", Scope: scanner.Local}, Size: 200},
		{Candidate: scanner.Candidate{Path: "/home/.gradle/caches", Type: "Gradle (global)", Scope: scanner.Global}, Size: 1024},
	}
	var buf bytes.Buffer
	Report(&buf, sized, ReportOpts{DryRun: true})
	out := buf.String()
	if !strings.Contains(out, "[dry-run] would delete:") {
		t.Error("missing dry-run header")
	}
	if !strings.Contains(out, "(local:") || !strings.Contains(out, "global:") {
		t.Error("missing local/global breakdown when both scopes present")
	}
	if !strings.Contains(out, "Run with --delete to remove them.") {
		t.Error("missing dry-run hint")
	}
	// Largest first: gradle (1024) before node_modules (200).
	if strings.Index(out, "caches") > strings.Index(out, "node_modules") {
		t.Error("expected largest-first ordering")
	}
}

func TestReportQuietOmitsList(t *testing.T) {
	sized := []SizedCandidate{
		{Candidate: scanner.Candidate{Path: "/x/bin", Type: "Go", Scope: scanner.Local}, Size: 10},
	}
	var buf bytes.Buffer
	Report(&buf, sized, ReportOpts{DryRun: true, Quiet: true})
	out := buf.String()
	if strings.Contains(out, "/x/bin") {
		t.Error("quiet mode must not list individual directories")
	}
	if !strings.Contains(out, "Total reclaimable:") {
		t.Error("quiet mode must still print the total")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "bin")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	sized := []SizedCandidate{
		{Candidate: scanner.Candidate{Path: victim, Type: "Go", Scope: scanner.Local}, Size: 42},
	}
	freed, failures := Delete(sized)
	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %v", failures)
	}
	if freed != 42 {
		t.Errorf("freed = %d, want 42", freed)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("victim dir should be gone")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cleaner/`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Write the implementation**

`internal/cleaner/cleaner.go`:
```go
// Package cleaner sizes, reports, and deletes scan candidates.
package cleaner

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"it.kluth.buildcleaner/internal/scanner"
)

// SizedCandidate is a Candidate with its computed total size in bytes.
type SizedCandidate struct {
	scanner.Candidate
	Size int64
}

// DirSize returns the total size of regular files under path. Per-entry errors
// are skipped (best effort); a non-nil error indicates the root walk failed.
func DirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// HumanSize formats a byte count with binary (1024-based) IEC units.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	labels := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	v := float64(n)
	i := -1
	for v >= unit && i < len(labels)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", v, labels[i])
}

// ReportOpts controls Report's output.
type ReportOpts struct {
	DryRun bool // print the dry-run header and hint
	Quiet  bool // print only the totals line
}

// Report writes the candidate list (largest first) and totals to w. It sorts
// sized in place.
func Report(w io.Writer, sized []SizedCandidate, opts ReportOpts) {
	sort.SliceStable(sized, func(i, j int) bool { return sized[i].Size > sized[j].Size })

	var total, localTotal, globalTotal int64
	for _, c := range sized {
		total += c.Size
		if c.Scope == scanner.Global {
			globalTotal += c.Size
		} else {
			localTotal += c.Size
		}
	}

	if !opts.Quiet {
		if opts.DryRun {
			fmt.Fprintln(w, "[dry-run] would delete:")
		} else {
			fmt.Fprintln(w, "deleting:")
		}
		for _, c := range sized {
			fmt.Fprintf(w, "  %10s   %s   (%s)\n", HumanSize(c.Size), c.Path, c.Type)
		}
		fmt.Fprintln(w, "----")
	}

	line := fmt.Sprintf("Total reclaimable: %s across %d directories", HumanSize(total), len(sized))
	if localTotal > 0 && globalTotal > 0 {
		line += fmt.Sprintf("  (local: %s / global: %s)", HumanSize(localTotal), HumanSize(globalTotal))
	}
	fmt.Fprintln(w, line)

	if opts.DryRun && !opts.Quiet {
		fmt.Fprintln(w, "Run with --delete to remove them.")
	}
}

// Delete removes each candidate with os.RemoveAll, summing freed bytes for
// successes and collecting per-candidate failures.
func Delete(sized []SizedCandidate) (freed int64, failures []error) {
	for _, c := range sized {
		if err := os.RemoveAll(c.Path); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", c.Path, err))
			continue
		}
		freed += c.Size
	}
	return freed, failures
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cleaner/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cleaner/
git commit -m "feat: add cleaner with sizing, human-readable report, and delete"
```

---

## Task 5: CLI entrypoint (non-interactive)

**Files:**
- Create: `main.go`
- Test: `main_test.go`

> Note: `main.go` references `tui.Run`/`tui.Options`, built in Task 6+. To keep this task compiling and testable on its own, this step also creates a minimal `internal/tui/tui.go` stub that Task 6 replaces. The stub is committed here and fully implemented later.

- [ ] **Step 1: Create the tui stub so main compiles**

`internal/tui/tui.go`:
```go
// Package tui provides the interactive Bubble Tea front-end. (Stub — replaced
// in the TUI tasks.)
package tui

// Options configures an interactive run.
type Options struct {
	Root   string
	Global bool
}

// Run launches the interactive TUI and returns any deletion failures.
func Run(opts Options) (failures []error, err error) {
	return nil, nil
}
```

- [ ] **Step 2: Write the failing test**

`main_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCLIDryRunKeepsFiles(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "p", "bin"))
	mustWrite(t, filepath.Join(root, "p", "go.mod"))

	code := runCLI(root, false /*global*/, false /*delete*/, false /*quiet*/)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "p", "bin")); err != nil {
		t.Error("dry-run must not delete anything")
	}
}

func TestRunCLIDeleteRemovesArtifacts(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "p", "bin"))
	mustWrite(t, filepath.Join(root, "p", "go.mod"))

	code := runCLI(root, false, true /*delete*/, true /*quiet*/)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(root, "p", "bin")); !os.IsNotExist(err) {
		t.Error("--delete should have removed bin/")
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
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test .`
Expected: FAIL — `undefined: runCLI`.

- [ ] **Step 4: Write the implementation**

`main.go`:
```go
// Command wolfe (Wolf the Cleaner) reports or deletes regenerable build
// artifacts under a directory tree, with an opt-in global-cache mode and an
// interactive TUI. Module path: it.kluth.buildcleaner.
package main

import (
	"flag"
	"fmt"
	"os"

	"it.kluth.buildcleaner/internal/cleaner"
	"it.kluth.buildcleaner/internal/scanner"
	"it.kluth.buildcleaner/internal/tui"
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
		failures, err := tui.Run(tui.Options{Root: path, Global: global})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if len(failures) > 0 {
			return 1
		}
		return 0
	}

	return runCLI(path, global, del, quiet)
}

func runCLI(path string, global, del, quiet bool) int {
	cands, warnings := scanner.ScanLocal(path)
	if global {
		cands = append(cands, scanner.ScanGlobal()...)
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %v\n", w)
	}

	sized := make([]cleaner.SizedCandidate, 0, len(cands))
	for _, c := range cands {
		size, _ := cleaner.DirSize(c.Path)
		sized = append(sized, cleaner.SizedCandidate{Candidate: c, Size: size})
	}

	cleaner.Report(os.Stdout, sized, cleaner.ReportOpts{DryRun: !del, Quiet: quiet})

	if del {
		freed, failures := cleaner.Delete(sized)
		fmt.Fprintf(os.Stdout, "Freed: %s across %d directories\n", cleaner.HumanSize(freed), len(sized)-len(failures))
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "failed: %v\n", f)
		}
		if len(failures) > 0 {
			return 1
		}
	}
	return 0
}

// isTTY reports whether f is a character device (an interactive terminal).
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test .`
Expected: PASS.

- [ ] **Step 6: Build and smoke-test the CLI**

Run:
```bash
go build -o /tmp/wolfe . && /tmp/wolfe --help; echo "exit=$?"
```
Expected: usage text printed by the `flag` package; exit `2` (flag's default for `-h`/`--help`). This confirms the binary builds and flags are registered.

- [ ] **Step 7: Commit**

```bash
git add main.go main_test.go internal/tui/tui.go
git commit -m "feat: add CLI entrypoint with dry-run/delete/global/quiet"
```

---

## Task 6: TUI model — scan, lazy sizing, selection

**Files:**
- Modify (replace stub): `internal/tui/tui.go`
- Create: `internal/tui/view.go`
- Test: `internal/tui/tui_test.go`

This task builds the model state machine and the scan/size/selection logic, with a minimal `View` so the type satisfies `tea.Model`. Rendering detail is fleshed out in Task 7; delete flow and `Run` in Task 8.

- [ ] **Step 1: Write the failing test**

`internal/tui/tui_test.go`:
```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"it.kluth.buildcleaner/internal/cleaner"
	"it.kluth.buildcleaner/internal/scanner"
)

func fixtureCandidates() []scanner.Candidate {
	return []scanner.Candidate{
		{Path: "/a/node_modules", Type: "JS/TS", Scope: scanner.Local},
		{Path: "/b/bin", Type: "Go", Scope: scanner.Local},
		{Path: "/home/.gradle/caches", Type: "Gradle (global)", Scope: scanner.Global},
	}
}

// newTestModel builds a model with injected functions (no filesystem, no TTY).
func newTestModel(sizes map[string]int64, deleted *[]string) Model {
	m := New(Options{Root: "/x", Global: true})
	m.scanFn = func() ([]scanner.Candidate, []error) { return fixtureCandidates(), nil }
	m.sizeFn = func(p string) int64 { return sizes[p] }
	m.deleteFn = func(c cleaner.SizedCandidate) error {
		*deleted = append(*deleted, c.Path)
		return nil
	}
	return m
}

// drainSizing applies the scan result then feeds every size synchronously.
func drainSizing(t *testing.T, m Model, sizes map[string]int64) Model {
	t.Helper()
	m2, _ := m.Update(scanMsg{cands: fixtureCandidates()})
	m = m2.(Model)
	for p, s := range sizes {
		m2, _ := m.Update(sizeMsg{path: p, size: s})
		m = m2.(Model)
	}
	m2, _ = m.Update(sizingDoneMsg{})
	return m2.(Model)
}

func TestModelTotalsAndSelectionDefaults(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/bin": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	if m.state != stateSelecting {
		t.Fatalf("state = %v, want selecting", m.state)
	}
	if got := m.selectedSize(); got != 1000 {
		t.Errorf("default selected size = %d, want 1000 (all selected)", got)
	}
	if got := m.selectedCount(); got != 3 {
		t.Errorf("default selected count = %d, want 3", got)
	}
	// Largest-first once all sizes are known: gradle(600) then node_modules(300).
	if m.items[m.view[0]].cand.Path != "/home/.gradle/caches" {
		t.Errorf("expected largest first, got %s", m.items[m.view[0]].cand.Path)
	}
}

func TestModelToggleAndFilter(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/bin": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	// Toggle the top row (gradle) off.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(Model)
	if m.selectedSize() != 400 {
		t.Errorf("after toggling gradle off, selected = %d, want 400", m.selectedSize())
	}

	// Filter to "bin": only /b/bin matches.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = m2.(Model)
	for _, r := range "bin" {
		m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	if len(m.view) != 1 || m.items[m.view[0]].cand.Path != "/b/bin" {
		t.Fatalf("filter 'bin' should show only /b/bin, view=%d", len(m.view))
	}
}

func TestModelConfirmGateBlocksDeleteOnQuit(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 1, "/b/bin": 1, "/home/.gradle/caches": 1}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	// Quit from selecting -> nothing deleted.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = m2.(Model)
	if cmd == nil {
		t.Error("expected tea.Quit command on q")
	}
	if len(deleted) != 0 {
		t.Errorf("quit must not delete anything, deleted=%v", deleted)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/`
Expected: FAIL — `undefined: New` / `scanMsg` / etc. (the stub has none of these).

- [ ] **Step 3: Replace the stub with the model**

`internal/tui/tui.go`:
```go
// Package tui provides the interactive Bubble Tea select-and-delete front-end.
// It is a thin view over the scanner and cleaner packages and contains no
// deletion or sizing logic of its own.
package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"it.kluth.buildcleaner/internal/cleaner"
	"it.kluth.buildcleaner/internal/scanner"
)

// Options configures an interactive run.
type Options struct {
	Root   string
	Global bool
}

type state int

const (
	stateScanning state = iota
	stateSelecting
	stateConfirm
	stateDeleting
	stateDone
)

type item struct {
	cand     scanner.Candidate
	size     int64
	sized    bool
	selected bool
}

// Messages flowing through the Bubble Tea event loop.
type scanMsg struct {
	cands    []scanner.Candidate
	warnings []error
}
type sizeMsg struct {
	path string
	size int64
}
type sizingDoneMsg struct{}
type delMsg struct {
	path string
	size int64
	err  error
}

// Model is the Bubble Tea model for the select-and-delete TUI.
type Model struct {
	opts Options

	state   state
	spinner spinner.Model
	filter  textinput.Model
	prog    progress.Model

	items    []*item
	byPath   map[string]*item
	view     []int // indices into items after filtering/sorting
	allSized bool

	filtering  bool
	onlyGlobal bool

	cursor int
	offset int
	height int // visible list rows
	width  int

	// Injected for testability; defaulted to real implementations in New.
	scanFn   func() ([]scanner.Candidate, []error)
	sizeFn   func(string) int64
	deleteFn func(cleaner.SizedCandidate) error

	sizeCh   chan sizeMsg
	delQueue []int
	delIndex int
	freed    int64
	failures []error
	warnings []error
}

// New constructs a Model with real scan/size/delete implementations.
func New(opts Options) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	ti := textinput.New()
	ti.Placeholder = "filter by path or type"

	return Model{
		opts:    opts,
		state:   stateScanning,
		spinner: sp,
		filter:  ti,
		prog:    progress.New(progress.WithDefaultGradient()),
		byPath:  map[string]*item{},
		height:  20,
		width:   80,
		scanFn:  func() ([]scanner.Candidate, []error) { return scan(opts) },
		sizeFn:  func(p string) int64 { s, _ := cleaner.DirSize(p); return s },
		deleteFn: func(c cleaner.SizedCandidate) error {
			_, f := cleaner.Delete([]cleaner.SizedCandidate{c})
			if len(f) > 0 {
				return f[0]
			}
			return nil
		},
	}
}

func scan(opts Options) ([]scanner.Candidate, []error) {
	cands, warnings := scanner.ScanLocal(opts.Root)
	if opts.Global {
		cands = append(cands, scanner.ScanGlobal()...)
	}
	return cands, warnings
}

// Init starts the spinner and kicks off the background scan.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, scanCmd(m.scanFn))
}

func scanCmd(fn func() ([]scanner.Candidate, []error)) tea.Cmd {
	return func() tea.Msg {
		cands, warnings := fn()
		return scanMsg{cands: cands, warnings: warnings}
	}
}

// startSizing launches a producer goroutine that sizes every item in order and
// pushes results onto a channel; returns the command that reads the first one.
func (m *Model) startSizing() tea.Cmd {
	m.sizeCh = make(chan sizeMsg, 64)
	items := m.items
	sizeFn := m.sizeFn
	ch := m.sizeCh
	go func() {
		for _, it := range items {
			ch <- sizeMsg{path: it.cand.Path, size: sizeFn(it.cand.Path)}
		}
		close(ch)
	}()
	return listenSize(ch)
}

func listenSize(ch chan sizeMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return sizingDoneMsg{}
		}
		return msg
	}
}

// Update is the Bubble Tea state-transition function.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = max(3, msg.Height-6) // leave room for header/footer
		m.prog.Width = max(10, msg.Width-20)
		m.clampOffset()
		return m, nil

	case spinner.TickMsg:
		if m.state == stateScanning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case scanMsg:
		m.warnings = msg.warnings
		m.items = make([]*item, 0, len(msg.cands))
		for _, c := range msg.cands {
			it := &item{cand: c, selected: true}
			m.items = append(m.items, it)
			m.byPath[c.Path] = it
		}
		m.state = stateSelecting
		m.rebuildView()
		return m, m.startSizing()

	case sizeMsg:
		if it, ok := m.byPath[msg.path]; ok {
			it.size = msg.size
			it.sized = true
		}
		return m, listenSize(m.sizeCh)

	case sizingDoneMsg:
		m.allSized = true
		m.rebuildView()
		return m, nil

	case delMsg:
		if msg.err != nil {
			m.failures = append(m.failures, msg.err)
		} else {
			m.freed += msg.size
		}
		m.delIndex++
		if m.delIndex < len(m.delQueue) {
			return m, m.deleteNext()
		}
		m.state = stateDone
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateSelecting:
		return m.handleSelectingKey(msg)
	case stateConfirm:
		switch msg.String() {
		case "y":
			return m.beginDelete()
		case "n", "esc", "q":
			m.state = stateSelecting
			return m, nil
		}
		return m, nil
	case stateDone:
		switch msg.String() {
		case "q", "enter", "esc", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleSelectingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.Type {
		case tea.KeyEnter:
			m.filtering = false
			m.filter.Blur()
			return m, nil
		case tea.KeyEsc:
			m.filtering = false
			m.filter.SetValue("")
			m.filter.Blur()
			m.rebuildView()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildView()
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.height)
	case "pgdown":
		m.moveCursor(m.height)
	case " ":
		if len(m.view) > 0 {
			it := m.items[m.view[m.cursor]]
			it.selected = !it.selected
		}
	case "a":
		m.toggleAll()
	case "g":
		m.onlyGlobal = !m.onlyGlobal
		m.cursor = 0
		m.rebuildView()
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "enter":
		if m.selectedCount() > 0 {
			m.state = stateConfirm
		}
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	if len(m.view) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	m.clampOffset()
}

func (m *Model) clampOffset() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// toggleAll selects or deselects every item currently in view. If all visible
// items are selected, it deselects them; otherwise it selects them all.
func (m *Model) toggleAll() {
	allSelected := true
	for _, idx := range m.view {
		if !m.items[idx].selected {
			allSelected = false
			break
		}
	}
	for _, idx := range m.view {
		m.items[idx].selected = !allSelected
	}
}

// rebuildView recomputes the filtered/sorted view slice.
func (m *Model) rebuildView() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.view = m.view[:0]
	for i, it := range m.items {
		if m.onlyGlobal && it.cand.Scope != scanner.Global {
			continue
		}
		if q != "" {
			hay := strings.ToLower(it.cand.Path + " " + it.cand.Type)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		m.view = append(m.view, i)
	}
	if m.allSized {
		sort.SliceStable(m.view, func(a, b int) bool {
			return m.items[m.view[a]].size > m.items[m.view[b]].size
		})
	}
	if m.cursor >= len(m.view) {
		m.cursor = max(0, len(m.view)-1)
	}
	m.clampOffset()
}

func (m Model) selectedCount() int {
	n := 0
	for _, it := range m.items {
		if it.selected {
			n++
		}
	}
	return n
}

func (m Model) selectedSize() int64 {
	var s int64
	for _, it := range m.items {
		if it.selected {
			s += it.size
		}
	}
	return s
}
```

> Note: `beginDelete`, `deleteNext`, and `Run` are added in Task 8. This task's tests do not reach the delete path (they quit before confirming), so they pass without those methods — **but the package must compile**, so add the following minimal stubs to `tui.go` now and replace them in Task 8:

```go
func (m Model) beginDelete() (tea.Model, tea.Cmd) { m.state = stateDeleting; return m, nil }
func (m Model) deleteNext() tea.Cmd               { return nil }
```

- [ ] **Step 4: Add a minimal View so Model satisfies tea.Model**

`internal/tui/view.go`:
```go
package tui

import "fmt"

// View renders the model. (Expanded with full styling in the next task.)
func (m Model) View() string {
	switch m.state {
	case stateScanning:
		return fmt.Sprintf("%s scanning %s ...\n", m.spinner.View(), m.opts.Root)
	default:
		return fmt.Sprintf("%d directories, %d selected\n", len(m.items), m.selectedCount())
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): model with scan, lazy sizing, selection and filter"
```

---

## Task 7: TUI rendering

**Files:**
- Modify: `internal/tui/view.go`
- Test: `internal/tui/tui_test.go` (add a render test)

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/tui_test.go`:
```go
import "strings" // add to the existing import block if not present

func TestViewShowsRowsAndTotal(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/bin": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)
	m.width, m.height = 100, 10
	m.rebuildView()

	out := m.View()
	if !strings.Contains(out, "node_modules") {
		t.Error("expected a row for node_modules")
	}
	if !strings.Contains(out, "Selected:") {
		t.Error("expected a selected-total footer")
	}
	if !strings.Contains(out, "of 3") {
		t.Error("expected a visible-range / count indicator")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestViewShowsRowsAndTotal`
Expected: FAIL — footer/"of 3" not present in the minimal View.

- [ ] **Step 3: Implement the full View**

Replace `internal/tui/view.go`:
```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"it.kluth.buildcleaner/internal/cleaner"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	selMarkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("84"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	failureStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// View renders the model for the current state.
func (m Model) View() string {
	switch m.state {
	case stateScanning:
		return fmt.Sprintf("\n  %s scanning %s ...\n", m.spinner.View(), m.opts.Root)
	case stateConfirm:
		return fmt.Sprintf(
			"\n  %s\n\n  Delete %d directories (%s)?  [y] yes  [n] back\n",
			titleStyle.Render("Confirm deletion"),
			m.selectedCount(), cleaner.HumanSize(m.selectedSize()),
		)
	case stateDeleting:
		done := m.delIndex
		total := len(m.delQueue)
		pct := 0.0
		if total > 0 {
			pct = float64(done) / float64(total)
		}
		return fmt.Sprintf("\n  Deleting %d/%d\n\n  %s\n", done, total, m.prog.ViewAs(pct))
	case stateDone:
		var b strings.Builder
		fmt.Fprintf(&b, "\n  %s\n\n", titleStyle.Render("Done"))
		fmt.Fprintf(&b, "  Freed: %s across %d directories\n", cleaner.HumanSize(m.freed), len(m.delQueue)-len(m.failures))
		for _, f := range m.failures {
			fmt.Fprintf(&b, "  %s %v\n", failureStyle.Render("failed:"), f)
		}
		fmt.Fprint(&b, "\n  [q] quit\n")
		return b.String()
	default:
		return m.selectingView()
	}
}

func (m Model) selectingView() string {
	var b strings.Builder
	header := titleStyle.Render("Wolf the Cleaner")
	if m.onlyGlobal {
		header += dimStyle.Render("  [globals only]")
	}
	fmt.Fprintf(&b, "\n  %s\n\n", header)

	end := min(m.offset+m.height, len(m.view))
	for i := m.offset; i < end; i++ {
		idx := m.view[i]
		it := m.items[idx]

		mark := " "
		if it.selected {
			mark = selMarkStyle.Render("x")
		}
		size := "    …"
		if it.sized {
			size = cleaner.HumanSize(it.size)
		}
		row := fmt.Sprintf("[%s] %10s  %s  %s", mark, size, it.cand.Path, dimStyle.Render("("+it.cand.Type+")"))
		if i == m.cursor {
			row = cursorStyle.Render("> ") + row
		} else {
			row = "  " + row
		}
		fmt.Fprintln(&b, "  "+row)
	}

	if m.filtering {
		fmt.Fprintf(&b, "\n  /%s\n", m.filter.View())
	}

	lo := 0
	if len(m.view) > 0 {
		lo = m.offset + 1
	}
	fmt.Fprintf(&b, "\n  %s   %s\n",
		accentStyle.Render(fmt.Sprintf("Selected: %s / %d dirs", cleaner.HumanSize(m.selectedSize()), m.selectedCount())),
		dimStyle.Render(fmt.Sprintf("%d–%d of %d", lo, end, len(m.view))),
	)
	fmt.Fprint(&b, dimStyle.Render("  [space] toggle  [a] all  [g] globals  [/] filter  [enter] delete  [q] quit")+"\n")
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS (all tui tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/tui_test.go
git commit -m "feat(tui): full styled view with virtualized list and footer"
```

---

## Task 8: TUI delete flow and Run

**Files:**
- Modify: `internal/tui/tui.go` (replace `beginDelete`/`deleteNext` stubs, add `Run`)
- Test: `internal/tui/tui_test.go` (add a delete-flow test)

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/tui_test.go`:
```go
func TestModelDeleteFlowDeletesSelectedAfterConfirm(t *testing.T) {
	sizes := map[string]int64{"/a/node_modules": 300, "/b/bin": 100, "/home/.gradle/caches": 600}
	deleted := []string{}
	m := drainSizing(t, newTestModel(sizes, &deleted), sizes)

	// Deselect the top row (gradle, 600) so only two remain selected.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = m2.(Model)

	// Enter -> confirm, then 'y' -> deleting.
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.state != stateConfirm {
		t.Fatalf("state = %v, want confirm", m.state)
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m2.(Model)
	if m.state != stateDeleting {
		t.Fatalf("state = %v, want deleting", m.state)
	}

	// Drive the delete commands to completion.
	for cmd != nil {
		msg := cmd()
		dm, ok := msg.(delMsg)
		if !ok {
			break
		}
		m2, cmd = m.Update(dm)
		m = m2.(Model)
	}

	if m.state != stateDone {
		t.Fatalf("state = %v, want done", m.state)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deletions, got %v", deleted)
	}
	if m.freed != 400 {
		t.Errorf("freed = %d, want 400", m.freed)
	}
	for _, p := range deleted {
		if p == "/home/.gradle/caches" {
			t.Error("deselected item must not be deleted")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestModelDeleteFlow`
Expected: FAIL — the `beginDelete`/`deleteNext` stubs never delete, so `deleted` stays empty and state never reaches done with deletions.

- [ ] **Step 3: Replace the stubs and add Run**

In `internal/tui/tui.go`, replace the two stub methods:
```go
func (m Model) beginDelete() (tea.Model, tea.Cmd) { m.state = stateDeleting; return m, nil }
func (m Model) deleteNext() tea.Cmd               { return nil }
```
with:
```go
// beginDelete snapshots the selected items into the delete queue and starts the
// first deletion.
func (m Model) beginDelete() (tea.Model, tea.Cmd) {
	m.delQueue = m.delQueue[:0]
	for i, it := range m.items {
		if it.selected {
			m.delQueue = append(m.delQueue, i)
		}
	}
	m.delIndex = 0
	m.freed = 0
	m.failures = nil
	m.state = stateDeleting
	if len(m.delQueue) == 0 {
		m.state = stateDone
		return m, nil
	}
	return m, m.deleteNext()
}

// deleteNext returns a command that deletes the current queued item.
func (m Model) deleteNext() tea.Cmd {
	it := m.items[m.delQueue[m.delIndex]]
	fn := m.deleteFn
	return func() tea.Msg {
		err := fn(cleaner.SizedCandidate{Candidate: it.cand, Size: it.size})
		return delMsg{path: it.cand.Path, size: it.size, err: err}
	}
}
```

Add the `Run` entrypoint at the end of `internal/tui/tui.go`:
```go
// Run launches the interactive TUI and blocks until the user exits. It returns
// any deletion failures that occurred.
func Run(opts Options) (failures []error, err error) {
	m := New(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	if fm, ok := final.(Model); ok {
		return fm.failures, nil
	}
	return nil, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Verify the whole module builds and vets cleanly**

Run:
```bash
go build ./... && go vet ./... && go test ./...
```
Expected: build succeeds, vet is silent, all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): confirm-gated delete flow with progress and Run entrypoint"
```

---

## Task 9: README and final verification

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write the README**

`README.md`:
````markdown
# Wolf the Cleaner

> "I'm Winston Wolfe. I solve problems." — the cleaner from *Pulp Fiction*.

`wolfe` is a small Go CLI that walks a tree of programming projects and reports
— or deletes — regenerable build artifacts (`bin`/`obj`, `node_modules`,
`target`, `build`, …). Built to **shrink backups**: run it over your `Coding`
directory (or a backup copy) so regenerable output is excluded and the backup is
smaller. Everything it removes can be regenerated (dependencies re-download,
build output recompiles) — the Wolf cleans up the mess and leaves no trace.

It can optionally also clean shared, per-user package caches (`~/.m2`,
`~/.gradle/caches`, the Go module cache, …), and offers an interactive TUI for
hands-on cleanup.

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
   4.2 GiB   /home/you/Coding/js/foo/node_modules   (JS/TS)
   120 MiB   /home/you/Coding/cs/bar/bin            (C#/.NET)
----
Total reclaimable: 4.3 GiB across 2 directories
Run with --delete to remove them.
```

### Interactive TUI

Scans with a spinner, then shows a scrollable, filterable checklist sorted
largest-first (sizes fill in as they compute). Keys: `↑/↓` move, `space` toggle,
`a` toggle all, `g` globals-only, `/` filter, `enter` confirm & delete, `q` quit.
A confirmation step gates all deletion.

## Detection

A directory is only flagged when a **project marker** and the **artifact
directory** are present together. Built-in rules:

| Type | Markers | Artifacts |
|---|---|---|
| C#/.NET | `*.csproj`, `*.sln`, `*.fsproj` | `bin`, `obj` |
| JavaScript/TS | `package.json` | `node_modules`, `dist`, `build`, `.next`, `.nuxt` |
| Rust | `Cargo.toml` | `target` |
| Java | `pom.xml`, `build.gradle`, `build.gradle.kts` | `target`, `build`, `.gradle` |
| Kotlin | `build.gradle.kts`, `*.kts`, `settings.gradle(.kts)` | `build`, `.gradle`, `out` |
| Android | `settings.gradle(.kts)` + `gradlew` | `build`, `.gradle`, `app/build`, `.cxx` |
| Flutter/Dart | `pubspec.yaml` | `build`, `.dart_tool`, `.flutter-plugins`, `.packages` |
| Go | `go.mod` | `bin` |
| Ruby | `Gemfile`, `*.gemspec` | `vendor/bundle`, `.bundle` |
| Python | `pyproject.toml`, `setup.py`, `requirements.txt` | `__pycache__`, `.venv`, `venv`, `*.egg-info`, `build`, `dist`, `.pytest_cache`, `.mypy_cache` |
| Crystal | `shard.yml` | `lib`, `.shards`, `bin` |

Global caches (with `--global`): Maven, Ivy, Gradle, NuGet, npm, Yarn, pip,
Cargo, Pub, Gem, and the Go module/build caches.

Symlinks are never followed, matched artifact directories are not descended
into, and nested (monorepo) projects are all discovered.

## Exit codes

- `0` — success (including dry-run).
- `1` — one or more deletions failed.
- `2` — invalid arguments or invalid path.

## Safety

Dry-run is the default; nothing is deleted without `--delete` (or confirming in
the TUI). Deletion is permanent (`os.RemoveAll`) — there is no trash.
````

- [ ] **Step 2: Final verification**

Run:
```bash
go build ./... && go vet ./... && go test ./...
go build -o /tmp/wolfe . && cd $(mktemp -d) && mkdir -p p/bin && touch p/go.mod && /tmp/wolfe .; echo "exit=$?"
```
Expected: all green; the smoke run prints a dry-run listing `p/bin` with a total and the "Run with --delete" hint, exit `0`, and `p/bin` still exists.

- [ ] **Step 3: Commit**

```bash
cd /home/andreaskluth/Coding/go/build-cleaner
git add README.md
git commit -m "docs: add README"
```

---

## Task 10: Dockerized end-to-end tests

**Files:**
- Create: `e2e/doc.go` (untagged, so `go build ./...` always sees a package)
- Create: `e2e/e2e_test.go` (build tag `e2e`)
- Create: `e2e/Dockerfile`
- Create: `e2e/run.sh`

This task builds the real `wolfe` binary and drives it end-to-end inside a
container, including the destructive `--delete` and `--global` paths, against an
isolated fake `$HOME`. It runs only with the `e2e` build tag, so normal
`go test ./...` is unaffected.

- [ ] **Step 1: Create the always-present package doc**

`e2e/doc.go`:
```go
// Package e2e contains Dockerized end-to-end tests for the wolfe binary. The
// tests are guarded by the `e2e` build tag so they do not run during a normal
// `go test ./...`; run them via e2e/run.sh (docker build + docker run).
package e2e
```

- [ ] **Step 2: Write the end-to-end test**

`e2e/e2e_test.go`:
```go
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
	if !strings.Contains(out, "[dry-run] would delete:") {
		t.Errorf("missing dry-run header:\n%s", out)
	}
	if !strings.Contains(out, "node_modules") || !strings.Contains(out, "Total reclaimable:") {
		t.Errorf("dry-run output missing expected content:\n%s", out)
	}
	if !exists(filepath.Join(root, "js", "node_modules")) {
		t.Error("dry-run must not delete node_modules")
	}
}

func TestE2EDeleteRemovesArtifactsKeepsMarkers(t *testing.T) {
	bin := buildWolfe(t)
	root := fixtureTree(t)

	out, code := run(t, bin, nil, root, "--delete")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	for _, gone := range []string{
		filepath.Join(root, "cs", "bin"),
		filepath.Join(root, "cs", "obj"),
		filepath.Join(root, "js", "node_modules"),
		filepath.Join(root, "rs", "target"),
		filepath.Join(root, "py", "__pycache__"),
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

func TestE2EGlobalDeleteInIsolatedHome(t *testing.T) {
	bin := buildWolfe(t)
	root := fixtureTree(t)

	home := t.TempDir()
	gomod := filepath.Join(home, "gomodcache")
	gocache := filepath.Join(home, "gocache")
	writeFile(t, filepath.Join(home, ".m2", "repository", "x.jar"), 4096)
	writeFile(t, filepath.Join(home, ".gradle", "caches", "x.bin"), 4096)
	writeFile(t, filepath.Join(gomod, "mod", "x.go"), 4096)
	writeFile(t, filepath.Join(gocache, "ab", "x"), 4096)

	env := []string{
		"HOME=" + home,
		"GOMODCACHE=" + gomod,
		"GOCACHE=" + gocache,
	}
	out, code := run(t, bin, env, root, "--global", "--delete")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	for _, gone := range []string{
		filepath.Join(home, ".m2", "repository"),
		filepath.Join(home, ".gradle", "caches"),
		gomod,
		gocache,
		filepath.Join(root, "js", "node_modules"),
	} {
		if exists(gone) {
			t.Errorf("expected %s to be deleted under --global --delete", gone)
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
	// No TTY is attached to a subprocess pipe, so -i must refuse with exit 2.
	if _, code := run(t, bin, nil, root, "-i"); code != 2 {
		t.Errorf("-i without TTY: exit = %d, want 2", code)
	}
}
```

- [ ] **Step 3: Write the Dockerfile**

`e2e/Dockerfile`:
```dockerfile
FROM golang:1.26

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build the binary once (fail fast on compile errors), then run the e2e suite.
RUN go build -o /usr/local/bin/wolfe it.kluth.buildcleaner
CMD ["go", "test", "-tags", "e2e", "-v", "./e2e/..."]
```

- [ ] **Step 4: Write the runner script**

`e2e/run.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail

# Build and run the Dockerized end-to-end tests from the repo root.
cd "$(dirname "$0")/.."
docker build -f e2e/Dockerfile -t wolf-the-cleaner-e2e .
docker run --rm wolf-the-cleaner-e2e
```

Make it executable:
```bash
chmod +x e2e/run.sh
```

- [ ] **Step 5: Verify the package builds without the tag, and run the suite**

Run:
```bash
go build ./... && go vet ./...        # e2e/doc.go keeps the package buildable
go test ./...                          # unaffected: e2e tests excluded (no tag)
./e2e/run.sh                           # builds image, runs e2e tests in Docker
```
Expected: `go build`/`vet`/`test` all pass; `./e2e/run.sh` ends with the e2e
package's tests all `--- PASS` / `ok`. (Requires Docker installed and running.)

- [ ] **Step 6: Commit**

```bash
git add e2e/
git commit -m "test: add Dockerized end-to-end tests"
```

---

## Self-Review Notes (for the implementer)

- **Spec coverage:** dry-run default (Task 5), `--delete`/`--quiet`/`--global` (Task 5), marker+sibling-artifact detection with dedup and no-symlink-follow (Tasks 2–3), nested projects (Task 3 test), per-dir size + total + local/global breakdown (Task 4), global caches incl. `go env` resolution (Task 3), interactive select-and-delete TUI with streaming-ready scan, lazy sizing, virtualized list, filter, confirm gate, progress, and large-list correctness (Tasks 6–8), README (Task 9), exit codes (Task 5).
- **Type consistency:** `Candidate{Path,Type,Scope}`, `Scope{Local,Global}`, `SizedCandidate{Candidate,Size}`, `ReportOpts{DryRun,Quiet}`, and `tui.Options{Root,Global}` / `tui.Run` are used identically across tasks.
- **Sizing concurrency** lives only in `tui` (channel + producer goroutine); the CLI path sizes sequentially, matching the spec's non-goal.
- **Simplification vs spec:** sizing uses one background producer (sequential) rather than a prioritized worker pool — responsive and far simpler; a pool can be added later if needed without changing the message protocol.
````
