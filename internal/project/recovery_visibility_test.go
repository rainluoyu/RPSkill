package project

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/luoyu/rpskill/internal/storage"
)

func TestPendingTransactionBlocksAllPublicWorkspaceReads(t *testing.T) {
	s, _, rt := initStore(t)
	write, err := s.fs.PrepareWrite("rp.yaml", []byte("uncommitted replacement"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(storage.Journal{Version: 1, Project: s.Root(), Writes: []storage.Write{write}})
	if err := s.runtime.AtomicWrite("store-transaction.json", raw); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func() error{
		"config":     func() error { _, err := s.Config(); return err },
		"chats":      func() error { _, err := s.Timelines(); return err },
		"state":      func() error { _, err := s.State("main"); return err },
		"history":    func() error { _, err := s.History("main"); return err },
		"characters": func() error { _, err := s.Characters(); return err },
		"worldbook":  func() error { _, err := s.Worldbook(); return err },
		"validate":   func() error { _, err := s.Validate(); return err },
	}
	for name, read := range tests {
		t.Run(name, func(t *testing.T) {
			if err := read(); !errors.Is(err, ErrRecoveryRequired) {
				t.Fatal("pending intermediate view exposed", err)
			}
		})
	}
	// Clear the deliberately uncommitted fixture, then close/reopen normally.
	if err := s.runtime.Remove("store-transaction.json"); err != nil {
		t.Fatal(err)
	}
	root := s.Root()
	s.Close()
	next, err := Open(root, OpenOptions{RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}
