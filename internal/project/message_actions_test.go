package project

import (
	"github.com/luoyu/rpskill/internal/timeline"
	"github.com/luoyu/rpskill/internal/worldbook"
	"testing"
)

func TestEditedAndDeletedOutputKeywordWindow(t *testing.T) {
	s, _, _ := initStore(t)
	if err := s.WriteWorldbook("world", "magic", []byte("---\nenabled: true\nactivation: keyword\nkeys: [魔法]\npriority: 1\n---\nMagic.")); err != nil {
		t.Fatal(err)
	}
	n := 1
	mustSettings(t, s, "main", timeline.SettingsPatch{Worldbook: &timeline.WorldbookPatch{ScanRecent: &n}})
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "prompt"})
	out := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "魔法"})
	refs, err := s.ActivatedWorldbook("main", worldbook.Message{Type: "input", Speaker: "director", Text: "continue"})
	checkRefs(t, refs, err, "magic")
	if _, err := s.AmendMessage("main", out.ID, ptr("sunshine")); err != nil {
		t.Fatal(err)
	}
	refs, err = s.ActivatedWorldbook("main", worldbook.Message{Type: "input", Speaker: "director", Text: "continue"})
	checkRefs(t, refs, err)
	if _, err := s.AmendMessage("main", out.ID, nil); err != nil {
		t.Fatal(err)
	}
	refs, err = s.ActivatedWorldbook("main", worldbook.Message{Type: "input", Speaker: "director", Text: "continue"})
	checkRefs(t, refs, err)
}
