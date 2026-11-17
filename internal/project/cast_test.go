package project

import (
	"github.com/luoyu/rpskill/internal/timeline"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutputCharactersPersistBranchRenameAndValidate(t *testing.T) {
	s, _, _ := initStore(t)
	for _, name := range []string{"alice", "bob"} {
		if err := s.WriteCharacter(name, []byte("---\nenabled: true\n---\nCard")); err != nil {
			t.Fatal(err)
		}
	}
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "prompt"})
	for _, names := range [][]string{{"missing"}, {"alice", "alice"}, {"chat"}} {
		if _, err := s.Append("main", timeline.Event{Type: timeline.Output, Kind: "story", InputID: in.ID, Text: "story", Characters: names}); err == nil {
			t.Fatal("accepted", names)
		}
	}
	out := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, Kind: "story", InputID: in.ID, Text: "story", Characters: []string{"alice", "bob"}})
	h, err := s.History("main")
	if err != nil || len(h[1].Characters) != 2 {
		t.Fatal(h, err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root(), "chats/main/history.jsonl"))
	if err != nil || !strings.Contains(string(raw), `"characters":["alice","bob"]`) {
		t.Fatal(string(raw), err)
	}
	if _, err := s.BranchChat("main", out.ID, "copy", "Copy"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCharacter("alice"); err == nil {
		t.Fatal("deleted referenced cast")
	}
	if err := s.RenameCharacter("alice", "alicia", []byte("---\nenabled: true\n---\nCard")); err != nil {
		t.Fatal(err)
	}
	for _, chat := range []string{"main", "copy"} {
		h, err := s.History(chat)
		if err != nil || h[1].Characters[0] != "alicia" {
			t.Fatal(h, err)
		}
	}
}
