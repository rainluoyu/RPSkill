package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenRuntimeFS creates a private external runtime namespace without following
// symlinks in any component; sensitive file accesses still use pinned Roots.
func OpenRuntimeFS(workspace, override string) (*FS, error) {
	root := override
	if root == "" {
		root = os.Getenv("RPS_RUNTIME_DIR")
	}
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(base, "rpskill", "runtime")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	inside, err := WithinDirectory(workspace, absolute)
	if err != nil {
		return nil, err
	}
	if inside {
		return nil, errors.New("runtime must be outside workspace")
	}
	directory := filepath.Join(absolute, SHA256([]byte(workspace)))
	volume := filepath.VolumeName(directory)
	base := volume + string(filepath.Separator)
	handle, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(base, directory)
	if err != nil {
		handle.Close()
		return nil, err
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		info, err := handle.Lstat(component)
		if errors.Is(err, os.ErrNotExist) {
			if e := handle.Mkdir(component, 0700); e != nil && !errors.Is(e, os.ErrExist) {
				handle.Close()
				return nil, e
			}
			info, err = handle.Lstat(component)
		}
		if err != nil {
			handle.Close()
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			handle.Close()
			return nil, fmt.Errorf("runtime symlink/non-directory component %s", component)
		}
		next, err := handle.OpenRoot(component)
		handle.Close()
		if err != nil {
			return nil, err
		}
		handle = next
	}
	file, err := handle.Open(".")
	if err != nil {
		handle.Close()
		return nil, err
	}
	err = file.Chmod(0700)
	file.Close()
	if err != nil {
		handle.Close()
		return nil, err
	}
	return &FS{root: handle, directory: directory, fileMode: 0600, dirMode: 0700}, nil
}
