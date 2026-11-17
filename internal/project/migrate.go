package project

import (
	"errors"
	"os"
	"path"
	"path/filepath"

	"github.com/luoyu/rpskill/internal/storage"
)

func copyWorkspace(source *storage.FS, destination string) error {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	inside, err := storage.WithinDirectory(source.Root(), absolute)
	if err != nil {
		return err
	}
	if inside {
		return errors.New("migration destination must be outside source workspace")
	}
	if _, err := os.Lstat(absolute); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("migration destination already exists")
		}
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	// Resolve the parent before repeating containment so a symlinked parent
	// cannot turn an apparently external destination into a nested source copy.
	absolute = filepath.Join(parent, filepath.Base(absolute))
	inside, err = storage.WithinDirectory(source.Root(), absolute)
	if err != nil {
		return err
	}
	if inside {
		return errors.New("resolved migration destination is inside source")
	}
	temp, err := os.MkdirTemp(parent, ".rps-copy-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	target, err := storage.OpenFS(temp)
	if err != nil {
		return err
	}
	defer target.Close()
	var copyDir func(string) error
	copyDir = func(dir string) error {
		entries, err := source.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if dir != "." {
				name = path.Join(dir, name)
			}
			if entry.IsDir() {
				if err := target.MkdirAll(name); err != nil {
					return err
				}
				if err := copyDir(name); err != nil {
					return err
				}
			} else {
				if err := target.CopyFileFrom(source, name); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := copyDir("."); err != nil {
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	return os.Rename(temp, absolute)
}
