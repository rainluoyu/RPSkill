package server

import (
	"encoding/json"

	"github.com/luoyu/rpskill/internal/timeline"

	"testing"
)

func seedOutput(t *testing.T, s *Server, speaker string) (timeline.Event, timeline.Event) {
	t.Helper()
	in, err := s.store.Append("main", timeline.Event{Type: timeline.Input, Speaker: speaker, Text: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	kind := "story"
	if speaker == "chat" {
		kind = "assistant"
	}
	out, err := s.store.Append("main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: kind, Text: "original"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.store.History("main")
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	return h[0], h[1]
}
func historyView(t *testing.T, s *Server, line, query string) []timeline.HistoryEvent {
	t.Helper()
	w := businessRequest(t, s, "GET", "/chats/"+line+"/events?"+query, "", nil)
	requireStatus(t, w, 200)
	var b struct {
		Data []timeline.HistoryEvent `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b.Data
}
func TestFlatChatAPIRejectsAncestryAndAcceptsStorySettings(t *testing.T) {
	s := newTestServer(t)
	requireStatus(t, businessRequest(t, s, "POST", "/chats", "branch", map[string]any{"id": "alt", "name": "Alt", "parent": "main", "fork_at": "evt_x"}), 400)
	requireStatus(t, businessRequest(t, s, "PATCH", "/chats/main", "override", map[string]any{"polishing": map[string]string{"level": "full"}}), 200)
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/regenerate", "regenerate", map[string]any{}), 404)
}
