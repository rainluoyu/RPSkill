package storage

import (
	"errors"
	"fmt"
	"os"
)

type Write struct {
	RemoveFile bool   `json:"remove_file,omitempty"`
	Staged     string `json:"staged,omitempty"`
	DataHash   string `json:"data_hash,omitempty"`
	DataSize   int    `json:"data_size,omitempty"`
	RemoveTree bool   `json:"remove_tree,omitempty"`
	Trash      string `json:"trash,omitempty"`
	Append     bool   `json:"append,omitempty"`
	Offset     int64  `json:"offset,omitempty"`
	Path       string `json:"path"`
	BeforeHash string `json:"before_hash"` // empty means the target did not exist
	Data       []byte `json:"data,omitempty"`
}
type Journal struct {
	Version int     `json:"version"`
	Stage   string  `json:"stage,omitempty"`
	Project string  `json:"project"`
	Writes  []Write `json:"writes"`
}

const journalFile = "store-transaction.json"

func (f *FS) PrepareWrite(name string, data []byte) (Write, error) {
	old, err := f.ReadFile(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Write{}, err
	}
	hash := ""
	if err == nil {
		hash = SHA256(old)
	}
	return Write{Path: name, BeforeHash: hash, Data: append([]byte{}, data...)}, nil
}
func Pending(runtime *FS) (bool, error) {
	_, err := runtime.ReadFile(journalFile)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Commit persists a write-ahead intent outside the Workspace before replacing
// any targets. Recovery rolls forward, accepting only the expected old/new bytes.
func Commit(workspace, runtime *FS, writes []Write) error {
	if len(writes) == 0 {
		return nil
	}
	if pending, err := Pending(runtime); err != nil {
		return err
	} else if pending {
		return errors.New("a transaction requires recovery before another mutation")
	}
	if err := discardOrphanStages(runtime); err != nil {
		return err
	}
	j, err := saveStagedJournal(workspace, runtime, writes)
	if err != nil {
		return err
	}
	j, err = hydrateJournal(j, runtime)
	if err != nil {
		return err
	}
	return finish(workspace, runtime, j)
}
func Recover(workspace, runtime *FS) error {
	data, err := runtime.ReadFile(journalFile)
	if errors.Is(err, os.ErrNotExist) {
		return discardOrphanStages(runtime)
	}
	if err != nil {
		return err
	}
	var j Journal
	if err := DecodeJSON(data, &j); err != nil {
		return fmt.Errorf("invalid recovery journal: %w", err)
	}
	if err := validateJournal(j, workspace.Root()); err != nil {
		return err
	}
	j, err = hydrateJournal(j, runtime)
	if err != nil {
		return err
	}
	if err := validateJournal(j, workspace.Root()); err != nil {
		return err
	}
	return finish(workspace, runtime, j)
}
func validateJournal(j Journal, project string) error {
	if (j.Version != 1 && j.Version != 2) || j.Project != project || len(j.Writes) == 0 {
		return errors.New("recovery journal project/version mismatch")
	}
	return validateWrites(j.Writes)
}
func validateWrites(writes []Write) error {
	if len(writes) == 0 || len(writes) > MaxTransactionWrites {
		return errors.New("invalid transaction write count")
	}
	total := 0
	seen := map[string]bool{}
	for _, w := range writes {
		if w.RemoveTree && w.RemoveFile || (w.RemoveTree || w.RemoveFile) && (w.Append || len(w.Data) != 0) {
			return errors.New("conflicting transaction operations")
		}
		if err := ValidPath(w.Path); err != nil {
			return err
		}
		if w.RemoveTree {
			if err := ValidPath(w.Trash); err != nil {
				return err
			}
			if w.Trash == w.Path || w.Trash == "" {
				return errors.New("invalid deletion staging path")
			}
		}
		if seen[w.Path] || len(w.Data) > MaxFileSize || w.Offset < 0 || (w.Append && len(w.Data) > MaxEventSize+1) {
			return errors.New("duplicate or oversized transaction write")
		}
		total += len(w.Data)
		if total > MaxTransactionBytes {
			return errors.New("transaction exceeds size limit")
		}
		seen[w.Path] = true
	}
	return nil
}
func finish(workspace, runtime *FS, j Journal) error {
	if err := validateJournal(j, workspace.Root()); err != nil {
		return err
	}
	// Preflight every target before any roll-forward, rather than partly applying
	// a journal whose later file was externally changed.
	for _, w := range j.Writes {
		if w.RemoveTree {
			if err := workspace.verifyRemoval(w); err != nil {
				return err
			}
			continue
		}
		if w.RemoveFile {
			if err := workspace.verifyFileRemoval(w); err != nil {
				return err
			}
			continue
		}
		if w.Append {
			if err := workspace.verifyAppend(w); err != nil {
				return fmt.Errorf("recovery conflict at %s: %w", w.Path, err)
			}
			continue
		}
		current, err := workspace.ReadFile(w.Path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		hash := ""
		if err == nil {
			hash = SHA256(current)
		}
		if hash != w.BeforeHash && hash != SHA256(w.Data) {
			return fmt.Errorf("recovery conflict at %s", w.Path)
		}
	}
	for _, w := range j.Writes {
		if w.RemoveTree {
			if err := workspace.finishRemoval(w); err != nil {
				return err
			}
			continue
		}
		if w.RemoveFile {
			if err := workspace.Remove(w.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if w.Append {
			if err := workspace.finishAppend(w); err != nil {
				return err
			}
			continue
		}
		if err := workspace.AtomicWrite(w.Path, w.Data); err != nil {
			return err
		}
	}
	if err := runtime.Remove(journalFile); err != nil {
		return err
	}
	if j.Stage != "" {
		return runtime.RemoveTree(j.Stage)
	}
	return nil
}

// PrepareFileRemoval captures the expected contents; recovery only unlinks those
// bytes (or accepts an already-unlinked file), never an external replacement.
func (f *FS) PrepareFileRemoval(name string) (Write, error) {
	data, err := f.ReadFile(name)
	if err != nil {
		return Write{}, err
	}
	return Write{Path: name, RemoveFile: true, BeforeHash: SHA256(data)}, nil
}
func (f *FS) verifyFileRemoval(w Write) error {
	data, err := f.ReadFile(w.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if w.BeforeHash == "" || SHA256(data) != w.BeforeHash {
		return fmt.Errorf("recovery conflict at %s", w.Path)
	}
	return nil
}
