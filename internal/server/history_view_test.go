package server

import (
	"encoding/json"
	"testing"

	"github.com/luoyu/rpskill/internal/timeline"
)

func TestHistoryViewFiltersBeforeLimitWithRawAnchor(t *testing.T) {
	s := newTestServer(t)
	for i := 0; i < 55; i++ {
		if _, err := s.store.Append("main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "input"}); err != nil {
			t.Fatal(err)
		}
	}
	history, _ := s.store.History("main")
	anchor := history[0].ID
	req := history[len(history)-1].ID
	story, err := s.store.Append("main", timeline.Event{Type: timeline.Output, Kind: "story", InputID: req, Text: "persisted story"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		query string
		count int
		first string
	}{
		{"view=story&limit=1", 1, story.ID}, {"view=story&limit=1&after=" + anchor, 1, story.ID},
		{"view=rp&limit=1", 1, anchor}, {"limit=1", 1, anchor}, {"view=diagnostic&limit=1", 0, ""},
	} {
		w := request(s, "GET", "/api/v1/chats/main/events?"+tt.query, map[string]string{"Authorization": "Bearer " + testToken})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var body struct {
			Data []timeline.Event `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		if len(body.Data) != tt.count || (tt.count > 0 && body.Data[0].ID != tt.first) {
			t.Fatalf("%s -> %+v", tt.query, body.Data)
		}
	}
	w := request(s, "GET", "/api/v1/chats/main/events?view=invalid", map[string]string{"Authorization": "Bearer " + testToken})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
