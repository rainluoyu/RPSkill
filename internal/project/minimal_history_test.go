package project

import (
	"bytes"
	"github.com/luoyu/rpskill/internal/timeline"
	"os"
	"path/filepath"
	"testing"
)

func TestMinimalHistoryNoTransportFieldsAndReopen(t *testing.T) {
	s, _, rt := initStore(t)
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "one"})
	out := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "scene"})
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "chat", Text: "two"})
	raw, err := os.ReadFile(filepath.Join(s.Root(), "chats/main/history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"id":`, `"input_id":`, `"request_id":`, `"revision":`, `"part":`, `"final":`, `"key":`, `evt_`, "DO_NOT_SAVE_THIS"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatal("persisted runtime field", forbidden, string(raw))
		}
	}
	if _, err := s.AmendMessage("main", out.ID, nil); err != nil {
		t.Fatal(err)
	}
	root := s.Root()
	s.Close()
	reopened, err := Open(root, OpenOptions{RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	h, err := reopened.History("main")
	if err != nil || len(h) != 2 || h[1].Text != "two" {
		t.Fatal(h, err)
	}
	state, err := reopened.State("main")
	if err != nil || len(state.Characters) != 0 {
		t.Fatal(state, err)
	}
}
