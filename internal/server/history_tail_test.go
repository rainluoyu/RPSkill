package server

import (
	"encoding/json"
	"testing"

	"github.com/luoyu/rpskill/internal/timeline"
)

func TestHistoryTailAndBefore(t *testing.T) {
	s := newTestServer(t)
	for i := 0; i < 7; i++ {
		if _, err := s.store.Append("main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "prompt"}); err != nil {
			t.Fatal(err)
		}
	}
	h, _ := s.store.History("main")
	for _, tt := range []struct {
		query     string
		positions []int
		status    int
	}{
		{"tail=true&limit=3", []int{5, 6, 7}, 200},
		{"tail=true&limit=3&before=" + h[4].ID, []int{2, 3, 4}, 200},
		{"limit=3&before=" + h[1].ID, []int{1}, 200},
		{"tail=true&before=" + h[0].ID, []int{}, 200},
		{"limit=2", []int{1, 2}, 200},
		{"after=" + h[0].ID + "&tail=true", nil, 400},
		{"before=missing", nil, 404},
	} {
		w := businessRequest(t, s, "GET", "/chats/main/events?view=rp&"+tt.query, "", nil)
		requireStatus(t, w, tt.status)
		if tt.status != 200 {
			continue
		}
		var result struct {
			Data []timeline.HistoryEvent `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Data) != len(tt.positions) {
			t.Fatal(tt.query, result.Data)
		}
		for i, pos := range tt.positions {
			if result.Data[i].ID != h[pos-1].ID {
				t.Fatal(tt.query, result.Data)
			}
		}
	}
}

func TestDeleteThroughLatestInclusive(t *testing.T) {
	for _, cut := range []int{1, 2, 4} {
		t.Run(string(rune('0'+cut)), func(t *testing.T) {
			s := newTestServer(t)
			seedOutput(t, s, "chat")
			seedOutput(t, s, "director")
			h := historyView(t, s, "main", "")
			if len(h) != 4 {
				t.Fatal(h)
			}
			path := "/chats/main/events/" + h[cut-1].ID
			body := map[string]any{"truncate": true}
			requireStatus(t, businessRequest(t, s, "DELETE", path, "truncate", body), 200)
			requireStatus(t, businessRequest(t, s, "DELETE", path, "truncate", body), 404)
			kept := historyView(t, s, "main", "")
			if len(kept) != cut-1 {
				t.Fatal(kept)
			}
			for i := range kept {
				if kept[i].Text != h[i].Text || kept[i].ID != h[i].ID {
					t.Fatal(kept)
				}
			}
		})
	}
}
