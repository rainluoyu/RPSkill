package contentio

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidName accepts one portable user-visible file or directory name, including Chinese.
func ValidName(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && len(name) <= 180 && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "/#%") && Relative(name) == nil
}

// ValidReference optionally scopes a filename by its category.
func ValidReference(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !ValidName(part) {
			return false
		}
	}
	return true
}

// Relative validates portable slash-separated paths without normalizing away
// traversal. Reject Windows device names even when running on Unix.
func Relative(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.ContainsAny(name, `\:`) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("invalid relative path %q", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("path contains a control character")
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, `<>"|?*`) {
			return fmt.Errorf("invalid path component %q", part)
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		suffix := strings.TrimPrefix(strings.TrimPrefix(base, "COM"), "LPT")
		devicePort := (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && utf8.RuneCountInString(suffix) == 1 && strings.Contains("123456789¹²³", suffix)
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || devicePort {
			return fmt.Errorf("reserved path component %q", part)
		}
	}
	return nil
}

// Open pins each directory component from the filesystem root down. It rejects
// workspace symlinks as well as links inside the workspace and never writes.
func Open(workspace string) (*os.Root, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	base := filepath.VolumeName(absolute) + string(filepath.Separator)
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(base, absolute)
	if err != nil {
		root.Close()
		return nil, err
	}
	if relative == "." {
		return root, nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		next, err := child(root, part)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = next
	}
	return root, nil
}

func child(root *os.Root, name string) (*os.Root, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("not a real directory: %s", name)
	}
	next, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	handle, err := next.Open(".")
	if err != nil {
		next.Close()
		return nil, err
	}
	pinned, err := handle.Stat()
	handle.Close()
	if err != nil {
		next.Close()
		return nil, err
	}
	if !os.SameFile(info, pinned) {
		next.Close()
		return nil, fmt.Errorf("directory changed while opening: %s", name)
	}
	return next, nil
}

func Directory(root *os.Root, name string) (*os.Root, error) {
	if err := Relative(name); err != nil {
		return nil, err
	}
	var opened *os.Root
	for _, part := range strings.Split(name, "/") {
		next, err := child(root, part)
		if opened != nil {
			opened.Close()
		}
		if err != nil {
			return nil, err
		}
		root, opened = next, next
	}
	return opened, nil
}

func ReadDir(root *os.Root) ([]fs.DirEntry, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

// ReadFile rejects links and nonregular files and confines the actual open to
// the pinned parent directory, so even a raced link cannot escape that root.
func ReadFile(root *os.Root, name string, limit int64) ([]byte, error) {
	if err := Relative(name); err != nil {
		return nil, err
	}
	parts := strings.Split(name, "/")
	if len(parts) > 1 {
		parent, err := Directory(root, strings.Join(parts[:len(parts)-1], "/"))
		if err != nil {
			return nil, err
		}
		defer parent.Close()
		root = parent
	}
	leaf := parts[len(parts)-1]
	before, err := root.Lstat(leaf)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", name)
	}
	if before.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d bytes: %s", limit, name)
	}
	f, err := root.Open(leaf)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, fmt.Errorf("file changed while opening: %s", name)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes: %s", limit, name)
	}
	return data, nil
}
