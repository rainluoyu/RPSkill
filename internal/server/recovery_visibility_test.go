package server

import (
	"encoding/json"
	"testing"

	"github.com/luoyu/rpskill/internal/storage"
)

func TestPendingStoreIntentDoesNotExposePartialReadsOrCacheRejection(t *testing.T) {
	s := newTestServer(t)
	raw, _ := json.Marshal(storage.Journal{Version: 1, Project: s.project, Writes: []storage.Write{{Path: "rp.yaml", Data: []byte("invalid")}}})
	if err := s.runtime.AtomicWrite("store-transaction.json", raw); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, businessRequest(t, s, "GET", "/project", "", nil), 503)
	requireStatus(t, businessRequest(t, s, "POST", "/chats", "recover-create", map[string]any{"name": "after"}), 503)
	if err := s.runtime.Remove("store-transaction.json"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, businessRequest(t, s, "POST", "/chats", "recover-create", map[string]any{"name": "after"}), 201)
}
