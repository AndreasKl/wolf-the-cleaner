package wolf

// Trashing follows the FreeDesktop.org Trash specification (the "home trash" at
// $XDG_DATA_HOME/Trash, with files/ and info/ subdirectories). The concept is
// borrowed from github.com/Kei-K23/trashbox, but that project's Linux path is a
// proof of concept (no .trashinfo, no collision handling, no cross-device
// fallback); this is a spec-compliant reimplementation.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// moveToTrash moves path into the user's home trash, recording its original
// location and deletion time in a .trashinfo file. Cross-filesystem moves fall
// back to a recursive copy followed by removal of the original.
func moveToTrash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	trash, err := trashDir()
	if err != nil {
		return err
	}

	name := uniqueTrashName(trash, filepath.Base(abs))
	infoPath := filepath.Join(trash, "info", name+".trashinfo")
	filesPath := filepath.Join(trash, "files", name)

	// Write the .trashinfo first so a crash can never leave an orphaned file in
	// files/ with no record of where it came from.
	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		encodeTrashPath(abs), time.Now().Format("2006-01-02T15:04:05"))
	if err := os.WriteFile(infoPath, []byte(info), 0o600); err != nil {
		return err
	}

	if err := os.Rename(abs, filesPath); err != nil {
		if errors.Is(err, syscall.EXDEV) { // different filesystem: copy then remove
			if cerr := copyTree(abs, filesPath); cerr != nil {
				_ = os.RemoveAll(filesPath)
				_ = os.Remove(infoPath)
				return cerr
			}
			if rerr := os.RemoveAll(abs); rerr != nil {
				return rerr
			}
			return nil
		}
		_ = os.Remove(infoPath) // roll back the orphaned info file
		return err
	}
	return nil
}

// trashDir returns the home trash directory ($XDG_DATA_HOME/Trash, else
// ~/.local/share/Trash), ensuring its files/ and info/ subdirectories exist.
func trashDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	trash := filepath.Join(base, "Trash")
	for _, sub := range []string{"files", "info"} {
		if err := os.MkdirAll(filepath.Join(trash, sub), 0o700); err != nil {
			return "", err
		}
	}
	return trash, nil
}

// uniqueTrashName returns a name not already taken under files/ or info/,
// appending ".N" on collision as the spec's uniqueness rule requires.
func uniqueTrashName(trash, base string) string {
	candidate := base
	for i := 1; ; i++ {
		_, fErr := os.Lstat(filepath.Join(trash, "files", candidate))
		_, iErr := os.Lstat(filepath.Join(trash, "info", candidate+".trashinfo"))
		if os.IsNotExist(fErr) && os.IsNotExist(iErr) {
			return candidate
		}
		candidate = fmt.Sprintf("%s.%d", base, i)
	}
}

// encodeTrashPath percent-encodes an absolute path for a .trashinfo Path field,
// leaving the '/' separators intact as the spec requires.
func encodeTrashPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// copyTree recursively copies src to dst, recreating directories and symlinks
// (symlinks are copied as links, never followed).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			return copyFile(p, target)
		}
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
