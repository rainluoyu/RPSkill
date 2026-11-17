package server

import (
	"bytes"
	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/timeline"
	"os"
	"path/filepath"
	"testing"
)

func TestStoryFilesAPIAndHints(t *testing.T) {
	s := newTestServer(t)
	requireStatus(t, businessRequest(t, s, "POST", "/characters", "card", map[string]any{"id": "alice", "content": "---\nenabled: true\n---\nBase"}), 201)
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/characters/alice/updates", "state", map[string]any{"document": "# Healthy", "expected_document": ""}), 200)
	raw, err := os.ReadFile(filepath.Join(s.project, "chats/main/history.jsonl"))
	if err != nil || len(raw) != 0 {
		t.Fatal(string(raw), err)
	}
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/characters/alice/updates", "stale", map[string]any{"document": "wrong", "expected_document": ""}), 409)
	requireStatus(t, businessRequest(t, s, "POST", "/chats", "alt", map[string]any{"name": "alt"}), 201)
	requireStatus(t, businessRequest(t, s, "PATCH", "/chats/main/settings", "settings", map[string]any{"polishing": map[string]any{"level": "full"}, "worldbook": map[string]any{"scan_recent": 4, "categories": map[string]bool{"magic": false}}}), 200)
	requireStatus(t, businessRequest(t, s, "PATCH", "/project", "old-settings", map[string]any{"polishing": map[string]any{"level": "off"}}), 400)
	requireStatus(t, businessRequest(t, s, "PATCH", "/project", "select", map[string]any{"last_chat": "alt"}), 200)
	rp, _ := os.ReadFile(filepath.Join(s.project, "rp.yaml"))
	if string(rp) != "last_chat: alt\n" {
		t.Fatal(string(rp))
	}
	main, _ := s.store.State("main")
	alt, _ := s.store.State("alt")
	if main.Settings.Polishing.Level != "full" || alt.Settings.Polishing.Level != "basic" || len(alt.Characters) != 0 {
		t.Fatal(main, alt)
	}
	_, err = s.store.Append("main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "start"})
	if err != nil {
		t.Fatal(err)
	}
	notice, err := s.notification("main")
	if err != nil {
		t.Fatal(err)
	}
	hints := notice.Context
	if len(hints.CharacterStates) != 1 || hints.CharacterStates[0].Path != "chats/main/characters/alice.md" || hints.StorySettings.Path != "chats/main/settings.yaml" {
		t.Fatal(hints)
	}
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/characters/alice/updates", "clear", map[string]any{"document": ""}), 200)
	if _, err := os.Stat(filepath.Join(s.project, "chats/main/characters/alice.md")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(s.project, "chats/main/history.jsonl"))
	if bytes.Contains(raw, []byte("meta")) {
		t.Fatal(string(raw))
	}
	requireStatus(t, businessRequest(t, s, "GET", "/chats/main/plans", "", nil), 404)
	root, rt := s.project, filepath.Dir(s.runtime.Root())
	s.Close()
	restored, err := New(Options{Project: root, RuntimeDir: rt, Callback: callback.ProviderCodex, Token: testToken, Dispatcher: s.dispatcher})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	config, err := restored.store.Config()
	if err != nil || config.LastChat != "alt" {
		t.Fatal(config, err)
	}
}
