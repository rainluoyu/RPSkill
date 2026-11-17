package project

import (
	"github.com/luoyu/rpskill/internal/timeline"
	"os"
	"path/filepath"
	"testing"
)

func TestCharacterStoryDocument(t *testing.T) {
	s, _, runtime := initStore(t)
	root := s.Root()
	card := []byte("---\nenabled: true\n---\nBase card")
	if err := s.WriteCharacter("alice", card); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTimeline(TimelineOptions{ID: "other", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "chats/main/history.jsonl"))
	doc := "# 当前状态\n\n左臂已康复。"
	if err := s.UpdateCharacterState("main", "alice", doc, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "chats/main/characters/alice.md"))
	if err != nil || string(raw) != doc {
		t.Fatal(string(raw), err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "chats/main/history.jsonl"))
	if string(before) != string(after) {
		t.Fatal("state changed history")
	}
	other, err := s.State("other")
	if err != nil || len(other.Characters) != 0 {
		t.Fatal(other, err)
	}
	stale := "old"
	if s.UpdateCharacterState("main", "alice", "wrong", &stale) == nil {
		t.Fatal("stale save accepted")
	}
	mustSettings(t, s, "main", timeline.SettingsPatch{Polishing: &timeline.Polishing{Level: "full"}, Worldbook: &timeline.WorldbookPatch{ScanRecent: ptr(5), Categories: map[string]bool{"world": false}}})
	config, _ := s.Config()
	config.LastChat = "other"
	if err := s.UpdateConfig(config); err != nil {
		t.Fatal(err)
	}
	rp, _ := os.ReadFile(filepath.Join(root, "rp.yaml"))
	if string(rp) != "last_chat: other\n" {
		t.Fatal(string(rp))
	}
	if err := s.RenameCharacter("alice", "艾莉丝", card); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root, OpenOptions{RuntimeDir: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state, err := s.State("main")
	if err != nil || state.Characters["艾莉丝"] != doc || state.Settings.Polishing.Level != "full" || state.Settings.Worldbook.ScanRecent != 5 {
		t.Fatal(state, err)
	}
	other, err = s.State("other")
	if err != nil || other.Settings.Polishing.Level != "basic" || other.Settings.Worldbook.ScanRecent != 0 {
		t.Fatal(other, err)
	}
	if err := s.UpdateCharacterState("main", "艾莉丝", "", &doc); err != nil {
		t.Fatal(err)
	}
}

func TestStoryFilesRejectLegacyAndEscape(t *testing.T) {
	for _, raw := range []string{"last_chat: main\nname: old\n", "last_chat: main\nnarrative:\n  perspective: first\n", "last_chat: main\nchats: {}\n"} {
		if _, err := ParseConfig([]byte(raw)); err == nil {
			t.Fatal("legacy config accepted", raw)
		}
	}
	for _, raw := range []string{`{"type":"input","time":"2026-11-16T00:00:00Z","speaker":"director","text":"x","meta":{"characters":[]}}`, `{"type":"character.update","time":"2026-11-16T00:00:00Z","document":"x"}`} {
		if _, err := timeline.ParseMessages([][]byte{[]byte(raw)}, "main"); err == nil {
			t.Fatal("legacy history accepted", raw)
		}
	}
	s, _, _ := initStore(t)
	if err := s.WriteCharacter("alice", []byte("---\nenabled: true\n---\nBase")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Root(), "chats/main/characters")); err != nil {
		t.Skip("symlink unavailable", err)
	}
	if err := s.UpdateCharacterState("main", "alice", "escape", nil); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "alice.md")); !os.IsNotExist(err) {
		t.Fatal("wrote outside workspace", err)
	}
}
