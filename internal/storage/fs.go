package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxFileSize = 32 << 20

// FS pins the canonical workspace directory; all access rejects symlinks and
// non-portable relative names. Root handles prevent escape after path checks.
type FS struct {
	root      *os.Root
	directory string
	fileMode  fs.FileMode
	dirMode   fs.FileMode
}

func OpenFS(directory string) (*FS, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, err
	}
	return &FS{root: r, directory: resolved, fileMode: 0644, dirMode: 0755}, nil
}
func (f *FS) Root() string { return f.directory }
func (f *FS) Close() error { return f.root.Close() }
func ValidPath(name string) error {
	if name == "" || !utf8.ValidString(name) || name == "." || strings.Contains(name, "\\") || strings.Contains(name, ":") || !fs.ValidPath(name) {
		return fmt.Errorf("invalid relative path %q", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("path contains a control character")
		}
	}
	for _, part := range strings.Split(name, "/") {
		if len(part) > 255 {
			return fmt.Errorf("path component exceeds portable 255-byte limit: %q", part)
		}
		if strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") || strings.ContainsAny(part, `<>"|?*`) {
			return fmt.Errorf("non-portable path %q", name)
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		suffix := strings.TrimPrefix(strings.TrimPrefix(stem, "COM"), "LPT")
		devicePort := (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && utf8.RuneCountInString(suffix) == 1 && strings.Contains("123456789¹²³", suffix)
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || stem == "CONIN$" || stem == "CONOUT$" || devicePort {
			return fmt.Errorf("reserved path %q", name)
		}
	}
	return nil
}
func (f *FS) check(name string, missing bool) error {
	if err := ValidPath(name); err != nil {
		return err
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		info, err := f.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && missing {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", current)
		}
		// Reject before Open: opening a FIFO can block before Stat is possible.
		// Methods still check the opened handle's type where applicable.
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("special file is not allowed: %s", current)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("not a directory: %s", current)
		}
	}
	return nil
}
func (f *FS) ReadFile(name string) ([]byte, error) {
	if err := f.check(name, false); err != nil {
		return nil, err
	}
	file, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err == nil && len(data) > MaxFileSize {
		return nil, errors.New("file exceeds size limit")
	}
	return data, err
}
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		if err := f.check(name, false); err != nil {
			return nil, err
		}
	}
	dir, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink is not allowed: %s", path.Join(name, e.Name()))
		}
	}
	return entries, nil
}
func (f *FS) MkdirAll(name string) error {
	if err := f.check(name, true); err != nil {
		return err
	}
	return f.root.MkdirAll(name, f.dirMode)
}
func (f *FS) AtomicWrite(name string, data []byte) error {
	if len(data) > MaxFileSize {
		return errors.New("file exceeds size limit")
	}
	return f.atomicWrite(name, data)
}

// AtomicWriteJSONL replaces validated history without imposing the small-file
// limit. Each record remains bounded; the rename is atomic and fsynced.
func (f *FS) AtomicWriteJSONL(name string, data []byte) error {
	if !strings.HasSuffix(name, ".jsonl") {
		return errors.New("history must be JSONL")
	}
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i-start > MaxEventSize {
				return errors.New("event exceeds size limit")
			}
			start = i + 1
		}
	}
	if start != len(data) {
		return errors.New("history requires final newline")
	}
	return f.atomicWrite(name, data)
}
func (f *FS) atomicWrite(name string, data []byte) error {
	if err := f.check(name, true); err != nil {
		return err
	}
	parent := path.Dir(name)
	if parent != "." {
		if err := f.MkdirAll(parent); err != nil {
			return err
		}
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	temp := path.Join(parent, ".rps-write-"+hex.EncodeToString(random))
	file, err := f.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.fileMode)
	if err != nil {
		return err
	}
	defer f.root.Remove(temp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := f.root.Rename(temp, name); err != nil {
		return err
	}
	return f.syncDir(parent)
}
func (f *FS) syncDir(name string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := f.root.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (f *FS) Remove(name string) error {
	if err := f.check(name, false); err != nil {
		return err
	}
	if err := f.root.Remove(name); err != nil {
		return err
	}
	return f.syncDir(path.Dir(name))
}
func SHA256(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// CopyFileFrom streams large history files without a fixed whole-file buffer.
func (f *FS) CopyFileFrom(source *FS, name string) error {
	if err := source.check(name, false); err != nil {
		return err
	}
	if err := f.check(name, true); err != nil {
		return err
	}
	src, err := source.root.Open(name)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("copy source must be a regular file")
	}
	parent := path.Dir(name)
	if parent != "." {
		if err := f.MkdirAll(parent); err != nil {
			return err
		}
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	temp := path.Join(parent, ".rps-copy-"+hex.EncodeToString(random))
	dst, err := f.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, f.fileMode)
	if err != nil {
		return err
	}
	defer f.root.Remove(temp)
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Sync(); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if err := f.root.Rename(temp, name); err != nil {
		return err
	}
	return f.syncDir(parent)
}

// RemoveTree preflights an entire subtree before unlinking any entries. Referenced
// business objects are rejected by Store before this low-level operation.
func (f *FS) RemoveTree(name string) error {
	if err := f.check(name, false); err != nil {
		return err
	}
	var files, dirs []string
	var walk func(string) error
	walk = func(dir string) error {
		entries, err := f.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			child := path.Join(dir, e.Name())
			if err := f.check(child, false); err != nil {
				return err
			}
			if e.IsDir() {
				if err := walk(child); err != nil {
					return err
				}
			} else {
				files = append(files, child)
			}
		}
		dirs = append(dirs, dir)
		return nil
	}
	if err := walk(name); err != nil {
		return err
	}
	for _, file := range files {
		if err := f.root.Remove(file); err != nil {
			return err
		}
	}
	for _, dir := range dirs {
		if err := f.root.Remove(dir); err != nil {
			return err
		}
	}
	return f.syncDir(path.Dir(name))
}

func (f *FS) PrepareTreeRemoval(name string) (Write, error) {
	if err := f.check(name, false); err != nil {
		return Write{}, err
	}
	if _, err := f.ReadDir(name); err != nil {
		return Write{}, err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return Write{}, err
	}
	return Write{Path: name, RemoveTree: true, Trash: ".rps-trash-" + hex.EncodeToString(random), Data: []byte{}}, nil
}
func (f *FS) verifyRemoval(w Write) error {
	original, err := f.root.Lstat(w.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	trash, e := f.root.Lstat(w.Trash)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if original != nil {
		if !original.IsDir() || original.Mode()&os.ModeSymlink != 0 {
			return errors.New("delete source is not a real directory")
		}
		if err := f.check(w.Path, false); err != nil {
			return err
		}
	}
	if trash != nil {
		if !trash.IsDir() || trash.Mode()&os.ModeSymlink != 0 {
			return errors.New("delete staging is not a real directory")
		}
		if err := f.check(w.Trash, false); err != nil {
			return err
		}
	}
	if original != nil && trash != nil {
		return errors.New("delete recovery conflict")
	}
	return nil
}
func (f *FS) finishRemoval(w Write) error {
	if err := f.verifyRemoval(w); err != nil {
		return err
	}
	if _, err := f.root.Lstat(w.Path); err == nil {
		if err := f.root.Rename(w.Path, w.Trash); err != nil {
			return err
		}
		if err := f.syncDir(path.Dir(w.Path)); err != nil {
			return err
		}
		if err := f.syncDir("."); err != nil {
			return err
		}
	}
	if _, err := f.root.Lstat(w.Trash); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return f.RemoveTree(w.Trash)
}
