package project

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyu/rpskill/internal/timeline"
	"github.com/luoyu/rpskill/internal/worldbook"
)

func TestFileNamedContent(t *testing.T) {
	s, _, _ := initStore(t)
	card := []byte("---\nenabled: true\n---\n角色正文。\n")
	if err := s.WriteCharacter("魔法师", card); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Characters()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range cards {
		if d.Metadata.ID == "魔法师" {
			found = d.Metadata.Name == "魔法师" && !bytes.Contains([]byte(d.Content), []byte("\nid:")) && !bytes.Contains([]byte(d.Content), []byte("\nname:"))
		}
	}
	if !found {
		t.Fatal("character name must derive from folder")
	}
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "魔法师", Text: "我走进协会。"})
	if err := s.RenameCharacter("魔法师", "角色甲", card); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root(), "chats/main/history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"speaker":"角色甲"`)) || !bytes.Contains(raw, []byte("我走进协会。")) {
		t.Fatalf("history reference not renamed: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "characters/魔法师")); !os.IsNotExist(err) {
		t.Fatal("old folder remains")
	}
	book := []byte("---\nenabled: true\nactivation: keyword\nkeys: [魔法]\npriority: 1\n---\n世界书正文。\n")
	for _, category := range []string{"学院", "王城"} {
		if err := s.WriteWorldbook(category, "历史", book); err != nil {
			t.Fatal(err)
		}
	}
	config, err := s.StorySettings("main")
	if err != nil {
		t.Fatal(err)
	}
	config.Worldbook.Entries = map[string]bool{"学院/历史": false}
	if err := s.SetTimelineSettings("main", config.Patch()); err != nil {
		t.Fatal(err)
	}
	refs, err := s.ActivatedWorldbook("main", worldbook.Message{Type: "input", Speaker: "角色甲", Text: "魔法"})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Path != "worldbook/王城/历史.md" {
		t.Fatalf("cross-category activation: %+v", refs)
	}
	if err := s.RenameWorldbook("学院", "历史", "往事", book); err != nil {
		t.Fatal(err)
	}
	config, err = s.StorySettings("main")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := config.Worldbook.Entries["学院/往事"]; !ok || v {
		t.Fatal("rename lost override")
	}
}
