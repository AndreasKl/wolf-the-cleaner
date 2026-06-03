// Package wolf is the deep core of Wolf the Cleaner: it finds reclaimable build
// artifacts and global caches, measures them, and deletes them. The rule table,
// tree traversal, cache resolution, sizing, and removal are all hidden here so
// that callers (the CLI and the TUI) deal only with Targets.
package wolf

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Options configures a scan.
type Options struct {
	Root          string // directory tree to scan for project artifacts
	IncludeGlobal bool   // also include the user's global package caches
}

// Target is one directory that can be reclaimed.
type Target struct {
	Path   string // absolute or root-relative path of the directory
	Kind   string // informational label, e.g. "JavaScript/TS" or "Maven (gobal)"
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
	targets := findFiles(opts.Root, opts.IncludeGlobal)
	return targets
}

func findFiles(root string, includeGlobal bool) []Target {
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
		for _, rule := range ProjectRules {
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

		if includeGlobal {
			for _, def := range GlobalCacheDefs {
				for _, ap := range resolveArtifact(path, def.RelPath, dirNames) {
					if seen[ap] {
						continue
					}
					seen[ap] = true
					skip[ap] = true
					targets = append(targets, Target{Path: ap, Kind: def.Name + " (global cache)"})
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
	if slices.Contains(dirNames, art) {
		return []string{filepath.Join(dir, art)}
	}
	return nil
}

// isRealDir reports whether path is a directory and not a symlink.
func isRealDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir() && fi.Mode()&os.ModeSymlink == 0
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

// Disposal selects how Delete gets rid of a target.
type Disposal int

const (
	// ToTrash moves the directory to the user's trash (recoverable). This is
	// the default; note it does not free disk space until the trash is emptied.
	ToTrash Disposal = iota
	// Permanent removes the directory outright with os.RemoveAll, freeing space
	// immediately and irreversibly.
	Permanent
)

// Delete disposes of each target according to how, returning the total Size of
// the targets it processed and any it could not dispose of. With Permanent,
// removing an absent directory is a no-op, not a failure.
func Delete(targets []Target, how Disposal) (processed int64, failed []Failure) {
	for _, t := range targets {
		var err error
		if how == ToTrash {
			err = moveToTrash(t.Path)
		} else {
			err = os.RemoveAll(t.Path)
		}
		if err != nil {
			failed = append(failed, Failure{Path: t.Path, Err: err})
			continue
		}
		processed += t.Size
	}
	return processed, failed
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
