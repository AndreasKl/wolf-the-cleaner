// Package wolf is the deep core of Wolf the Cleaner: it finds reclaimable build
// artifacts and global caches, measures them, and deletes them. The rule table,
// tree traversal, cache resolution, sizing, and removal are all hidden here so
// that callers (the CLI and the TUI) deal only with Targets.
package wolf

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"it.kluth.buildcleaner/internal/rules"
)

// Options configures a scan.
type Options struct {
	Root          string // directory tree to scan for project artifacts
	IncludeGlobal bool   // also include the user's global package caches
}

// Target is one directory that can be reclaimed.
type Target struct {
	Path   string // absolute or root-relative path of the directory
	Kind   string // informational label, e.g. "JavaScript/TS" or "Maven (global cache)"
	Global bool   // true for a shared per-user cache, false for a project artifact
	Size   int64  // measured size in bytes; 0 until Measure has been applied
}

// Failure records a directory that could not be deleted.
type Failure struct {
	Path string
	Err  error
}

// Find returns every reclaimable directory under opts.Root and, when
// opts.IncludeGlobal is set, the user's existing global caches. Targets come
// back unmeasured (Size 0); apply Measure to fill in sizes. Find never fails:
// unreadable subtrees and missing caches are simply skipped.
func Find(opts Options) []Target {
	targets := findLocal(opts.Root)
	if opts.IncludeGlobal {
		targets = append(targets, findGlobal()...)
	}
	return targets
}

func findLocal(root string) []Target {
	var targets []Target
	skip := map[string]bool{} // artifact dirs we must not descend into
	seen := map[string]bool{} // dedup by path

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
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
					targets = append(targets, Target{Path: ap, Kind: rule.Name})
				}
			}
		}
		return nil
	})
	return targets
}

// resolveArtifact turns one artifact spec into the existing directory paths it
// names within dir: a spec with "/" is a relative path, a spec with glob
// metacharacters is matched against immediate subdirectories, otherwise it is a
// literal directory name.
func resolveArtifact(dir, art string, dirNames []string) []string {
	if strings.Contains(art, "/") {
		p := filepath.Join(dir, art)
		if isRealDir(p) {
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

// isRealDir reports whether path is a directory and not a symlink.
func isRealDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir() && fi.Mode()&os.ModeSymlink == 0
}

func findGlobal() []Target {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []Target
	for _, def := range rules.GlobalCacheDefs {
		path := resolveCache(def, home)
		if path != "" && isRealDir(path) {
			out = append(out, Target{Path: path, Kind: def.Name + " (global cache)", Global: true})
		}
	}
	return out
}

// resolveCache resolves a cache path: a named env var wins, then `go env <key>`,
// then $HOME joined with the relative fallback.
func resolveCache(def rules.GlobalCacheDef, home string) string {
	if def.EnvVar != "" {
		if v := os.Getenv(def.EnvVar); v != "" {
			return v
		}
	}
	if def.GoEnvKey != "" {
		if v := goEnv(def.GoEnvKey); v != "" {
			return v
		}
	}
	return filepath.Join(home, def.RelPath)
}

func goEnv(key string) string {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Measure returns the total size of regular files under path (best effort;
// unreadable entries contribute 0).
func Measure(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// Delete removes each target with os.RemoveAll, returning the bytes reclaimed
// (summed from each target's Size) and any directories it could not remove.
// Removing an absent directory is a no-op, not a failure.
func Delete(targets []Target) (reclaimed int64, failed []Failure) {
	for _, t := range targets {
		if err := os.RemoveAll(t.Path); err != nil {
			failed = append(failed, Failure{Path: t.Path, Err: err})
			continue
		}
		reclaimed += t.Size
	}
	return reclaimed, failed
}

// FormatSize renders a byte count with binary (1024-based) IEC units.
func FormatSize(n int64) string {
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
