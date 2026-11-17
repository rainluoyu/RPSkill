package server

import (
	"github.com/luoyu/rpskill/internal/timeline"
	"testing"
)

func TestStoryNameAPIAndOutputCastCorrection(t *testing.T) {
	s := newTestServer(t)
	requireStatus(t, businessRequest(t, s, "POST", "/chats", "create", map[string]any{"name": "另一条路"}), 201)
	requireStatus(t, businessRequest(t, s, "POST", "/chats", "legacy", map[string]any{"name": "Display", "id": "other"}), 400)
	if err := s.store.WriteCharacter("角色乙", []byte("---\nenabled: true\n---\nCard")); err != nil {
		t.Fatal(err)
	}
	seedOutput(t, s, "director")
	requireStatus(t, businessRequest(t, s, "PATCH", "/chats/main/events/2", "cast-edit", map[string]any{"characters": []string{"角色乙"}}), 200)
	h := historyView(t, s, "main", "")
	if h[1].Text != "original" || len(h[1].Characters) != 1 {
		t.Fatal(h)
	}
	rename := map[string]any{"name": "示例故事"}
	first := businessRequest(t, s, "PATCH", "/chats/main", "rename", rename)
	requireStatus(t, first, 200)
	h = historyView(t, s, "示例故事", "")
	if h[1].Text != "original" || h[1].Characters[0] != "角色乙" || h[0].Type != timeline.Input {
		t.Fatal(h)
	}
	requireStatus(t, businessRequest(t, s, "GET", "/chats/示例故事/requests", "", nil), 404)
	requireStatus(t, businessRequest(t, s, "GET", "/validate", "", nil), 200)
}
