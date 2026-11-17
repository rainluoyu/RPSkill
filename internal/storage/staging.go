package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	MaxTransactionBytes  = 256 << 20
	MaxTransactionWrites = 8192
)

// stageJournal writes each payload separately before publishing the durable
// intent. No workspace file is touched until the complete manifest is durable.
func stageJournal(workspace, runtime *FS, writes []Write) (Journal, error) {
	j := Journal{Version: 2, Project: workspace.Root(), Writes: append([]Write(nil), writes...)}
	if err := validateWrites(writes); err != nil {
		return j, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return j, err
	}
	j.Stage = "store-stage-" + hex.EncodeToString(nonce[:])
	if err := runtime.MkdirAll(j.Stage); err != nil {
		return j, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = runtime.RemoveTree(j.Stage)
		}
	}()
	for i := range j.Writes {
		w := &j.Writes[i]
		if w.RemoveTree || w.RemoveFile {
			continue
		}
		w.Staged = fmt.Sprintf("%s/%04d.bin", j.Stage, i)
		w.DataHash = SHA256(w.Data)
		w.DataSize = len(w.Data)
		if err := runtime.AtomicWrite(w.Staged, w.Data); err != nil {
			return j, err
		}
		w.Data = nil
	}
	ok = true
	return j, nil
}

func validStage(name string) bool {
	if !strings.HasPrefix(name, "store-stage-") || len(name) != len("store-stage-")+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(name, "store-stage-"))
	return err == nil
}

func hydrateJournal(j Journal, runtime *FS) (Journal, error) {
	if j.Version == 1 {
		if j.Stage != "" {
			return j, errors.New("legacy journal has staging metadata")
		}
		for _, w := range j.Writes {
			if w.Staged != "" || w.DataHash != "" || w.DataSize != 0 {
				return j, errors.New("legacy journal has staged payload")
			}
		}
		return j, nil
	}
	if j.Version != 2 || !validStage(j.Stage) {
		return j, errors.New("invalid staged journal")
	}
	if len(j.Writes) == 0 || len(j.Writes) > MaxTransactionWrites {
		return j, errors.New("invalid transaction write count")
	}
	j.Writes = append([]Write(nil), j.Writes...)
	total := 0
	// Validate every size/path before allocating or reading any payload.
	for i, w := range j.Writes {
		if len(w.Data) != 0 {
			return j, errors.New("staged journal contains inline payload")
		}
		if w.RemoveTree || w.RemoveFile {
			if w.Staged != "" || w.DataHash != "" || w.DataSize != 0 {
				return j, errors.New("deletion contains staged payload")
			}
			continue
		}
		if w.Staged != fmt.Sprintf("%s/%04d.bin", j.Stage, i) || w.DataSize < 0 || w.DataSize > MaxFileSize || len(w.DataHash) != 64 {
			return j, errors.New("invalid staged payload metadata")
		}
		total += w.DataSize
		if total > MaxTransactionBytes {
			return j, errors.New("transaction exceeds size limit")
		}
	}
	for i := range j.Writes {
		w := &j.Writes[i]
		if w.RemoveTree || w.RemoveFile {
			continue
		}
		data, err := runtime.ReadFile(w.Staged)
		if err != nil {
			return j, fmt.Errorf("read staged payload: %w", err)
		}
		if len(data) != w.DataSize || SHA256(data) != w.DataHash {
			return j, errors.New("staged payload hash/size mismatch")
		}
		w.Data = data
	}
	return j, nil
}

func saveStagedJournal(workspace, runtime *FS, writes []Write) (Journal, error) {
	j, err := stageJournal(workspace, runtime, writes)
	if err != nil {
		return j, err
	}
	data, err := json.Marshal(j)
	if err == nil {
		err = runtime.AtomicWrite(journalFile, data)
	}
	if err != nil {
		_ = runtime.RemoveTree(j.Stage)
	}
	return j, err
}

// discardOrphanStages is only called with the single-writer lock and no pending
// journal. A crash before publishing intent may leave payloads but no mutation.
func discardOrphanStages(runtime *FS) error {
	entries, err := runtime.ReadDir(".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if validStage(e.Name()) {
			if !e.IsDir() {
				return errors.New("transaction stage is not a directory")
			}
			if err := runtime.RemoveTree(e.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
