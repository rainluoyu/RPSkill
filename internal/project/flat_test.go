package project

import (
	"bytes"
	"encoding/json"
	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/timeline"
	"os"
	"path/filepath"
	"testing"
)

func TestFlatStoriesConfigHistoryIsolationAndArchive(t *testing.T) {
	s, base, _ := initStore(t)
	if _, err := Open(s.Root(), OpenOptions{}); err == nil {
		t.Fatal("second writer accepted")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "chats/main/timeline.yaml")); !os.IsNotExist(err) {
		t.Fatal("unexpected timeline.yaml")
	}
	if _, err := s.CreateTimeline(TimelineOptions{ID: "other", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTimeline(TimelineOptions{ID: "a/b", Name: "nested"}); err == nil {
		t.Fatal("nested chat accepted")
	}
	input := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "original prompt"})
	out := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: input.ID, Kind: "story", Text: "old"})
	mustSettings(t, s, "main", timeline.SettingsPatch{Polishing: &timeline.Polishing{Level: "full"}})
	other, err := s.State("other")
	if err != nil || other.Settings.Polishing.Level != "basic" {
		t.Fatal(other, err)
	}
	h, err := s.History("main")
	if err != nil || len(h) != 2 {
		t.Fatal(h, err)
	}
	if len(other.Characters) != 0 {
		t.Fatal("metadata leaked")
	}
	data, err := os.ReadFile(filepath.Join(s.Root(), "chats/main/history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "input" && m["type"] != "output" {
			t.Fatal(m)
		}
		for _, key := range []string{"id", "input_id", "timeline", "version", "request_id", "patch"} {
			if _, ok := m[key]; ok {
				t.Fatal("redundant field", key)
			}
		}
	}
	revised, err := s.AmendMessage("main", out.ID, ptr("new"))
	if err != nil {
		t.Fatal(err)
	}
	if revised.Text != "new" {
		t.Fatal(revised)
	}
	zip, err := s.ExportArchive("chats", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	dest, err := Init(filepath.Join(base, "dest"), InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	// Import a project archive into the pristine destination.
	all, err := s.ExportArchive("project", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dest.ImportArchive("project", all); err != nil {
		t.Fatal(err)
	}
	copied, err := dest.History("main")
	if err != nil || copied[1].Text != "new" {
		t.Fatal(copied, err)
	}
	files, err := media.DecodeZIP(zip)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["chats/main/timeline.yaml"]; ok {
		t.Fatal("archive contains legacy config")
	}
	if _, err := s.AmendMessage("main", out.ID, nil); err != nil {
		t.Fatal(err)
	}
	h, err = s.History("main")
	if err != nil || len(h) != 1 || h[0].Text != "original prompt" {
		t.Fatal(h, err)
	}
	if err := s.DeleteTimeline("main"); err != nil {
		t.Fatal(err)
	}
	copied, err = dest.History("main")
	if err != nil || len(copied) != 2 {
		t.Fatal("independent copy affected", err)
	}
}
func TestMessageMetadataValidationAndDeleteSemantics(t *testing.T) {
	s, _, _ := initStore(t)
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "prompt"})
	out := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "story"})
	if err := s.WriteCharacter("alice", []byte("---\nenabled: true\n---\nCard")); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCharacterState("main", "alice", "伤势已恢复", nil); err != nil {
		t.Fatal(err)
	}
	state, err := s.State("main")
	if err != nil || state.Characters["alice"] != "伤势已恢复" {
		t.Fatal(state, err)
	}
	if _, err = s.AmendMessage("main", out.ID, nil); err != nil {
		t.Fatal(err)
	}
	state, err = s.State("main")
	if err != nil || state.Characters["alice"] != "伤势已恢复" {
		t.Fatal("history edit changed state file", state, err)
	}
}
