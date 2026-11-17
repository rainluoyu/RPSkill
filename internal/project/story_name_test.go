package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/timeline"
)

func TestStoryFolderNameRenameAndCastCorrection(t *testing.T) {
	s, _, _ := initStore(t)
	if err := s.WriteCharacter("角色乙", []byte("---\nenabled: true\n---\nPriest")); err != nil {
		t.Fatal(err)
	}
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "prompt"})
	m, err := s.UploadMedia(media.Upload{Timeline: "main", Name: "note.txt", Data: []byte("A travel note")})
	if err != nil {
		t.Fatal(err)
	}
	out := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, Kind: "story", InputID: in.ID, Text: "unchanged prose", Media: []timeline.MediaReference{m.MediaReference}})
	names := []string{"角色乙"}
	corrected, err := s.AmendMessageContent("main", out.ID, nil, &names)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Text != out.Text || len(corrected.Characters) != 1 {
		t.Fatal(corrected)
	}
	bad := []string{"角色乙", "角色乙"}
	if _, err := s.AmendMessageContent("main", corrected.ID, nil, &bad); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := s.AmendMessageContent("main", in.ID, nil, &names); err == nil {
		t.Fatal("input accepted cast")
	}
	if err := s.UpdateCharacterState("main", "角色乙", "current state", nil); err != nil {
		t.Fatal(err)
	}
	renamed, err := s.UpdateTimelineMetadata("main", "示例故事", nil)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != "示例故事" || renamed.Name != renamed.ID {
		t.Fatal(renamed)
	}
	cfg, err := s.Config()
	if err != nil || cfg.LastChat != renamed.ID {
		t.Fatal(cfg, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "chats/main")); !os.IsNotExist(err) {
		t.Fatal("old folder remains", err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root(), "chats/示例故事/settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "name:") || strings.Contains(string(raw), "id:") {
		t.Fatal(string(raw))
	}
	history, err := s.History(renamed.ID)
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	if history[1].Text != out.Text || history[1].Time != out.Time || len(history[1].Characters) != 1 || history[1].Media[0].ID != m.ID || !strings.HasPrefix(history[1].Media[0].Path, "media/") {
		t.Fatal(history)
	}
	state, err := s.State(renamed.ID)
	if err != nil || state.Characters["角色乙"] != "current state" {
		t.Fatal(state, err)
	}
	if _, err := s.BranchChat(renamed.ID, history[1].ID, "另一条路", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTimelineMetadata(renamed.ID, "另一条路", nil); err == nil {
		t.Fatal("overwrote story")
	}
	if _, err := s.UpdateTimelineMetadata(renamed.ID, "../escape", nil); err == nil {
		t.Fatal("accepted traversal")
	}
}
