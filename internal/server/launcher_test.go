package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/timeline"
)

type inboxFunc func(context.Context, callback.Notification) error

func (f inboxFunc) Deliver(ctx context.Context, n callback.Notification) error { return f(ctx, n) }
func TestLauncherNotificationAndEmission(t *testing.T) {
	s := newTestServer(t)
	var notices []callback.Notification
	s.dispatcher = inboxFunc(func(_ context.Context, n callback.Notification) error { notices = append(notices, n); return nil })
	requireStatus(t, businessRequest(t, s, "POST", "/characters", "", map[string]any{"id": "alice", "content": "---\nenabled: true\n---\nAlice"}), 201)
	requireStatus(t, businessRequest(t, s, "POST", "/worldbook", "", map[string]any{"category": "world", "slug": "magic", "content": "---\nenabled: true\nactivation: keyword\nkeys: [magic]\npriority: 50\n---\nPRIVATE BODY"}), 201)
	for _, speaker := range []string{"alice", "director", "chat"} {
		requireStatus(t, businessRequest(t, s, "POST", "/chats/main/inputs", "", map[string]any{"speaker": speaker, "text": "magic"}), 201)
		n := notices[len(notices)-1]
		h := n.Context
		protected := ""
		if speaker == "alice" {
			protected = "alice"
		}
		if h.Chat != "main" || h.InputLine != h.TotalLines || h.Speaker != speaker || h.ProtectedCharacter != protected || len(h.Worldbook) != 1 || h.Worldbook[0].Path != "worldbook/world/magic.md" {
			t.Fatal(h)
		}
		raw, _ := json.Marshal(n)
		for _, bad := range []string{s.token, "PRIVATE BODY", "request_id", "attempt_id", "session", "sha256", "revision"} {
			if strings.Contains(string(raw), bad) {
				t.Fatal("notification leaked", bad)
			}
		}
		info, err := os.Stat(n.Connection)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal(info, err)
		}
		kind := "story"
		wrong := "assistant"
		if speaker == "chat" {
			kind, wrong = wrong, kind
		}
		body := map[string]any{"input": h.InputLine, "kind": wrong, "text": "scene", "characters": []string{"alice"}}
		requireStatus(t, businessRequest(t, s, "POST", "/chats/main/outputs", "", body), 409)
		body["kind"] = kind
		requireStatus(t, businessRequest(t, s, "POST", "/chats/main/outputs", "", body), 201)
		body["text"] = "second segment"
		requireStatus(t, businessRequest(t, s, "POST", "/chats/main/outputs", "", body), 201)
	}
	for _, path := range []string{"/providers", "/chats/main/requests", "/chats/main/requests/x"} {
		requireStatus(t, businessRequest(t, s, "GET", path, "", nil), 404)
	}
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/requests/x/retry", "", map[string]any{}), 404)
	raw, _ := os.ReadFile(filepath.Join(s.project, "chats/main/history.jsonl"))
	for _, bad := range []string{"request_id", "attempt_id", "idempotency", "final", "part"} {
		if strings.Contains(string(raw), bad) {
			t.Fatal(string(raw))
		}
	}
}
func TestDeliveryFailureKeepsInputAndNoAutomaticReplay(t *testing.T) {
	s := newTestServer(t)
	count := 0
	s.dispatcher = inboxFunc(func(context.Context, callback.Notification) error { count++; return errors.New("owner unavailable") })
	w := businessRequest(t, s, "POST", "/chats/main/inputs", "", map[string]any{"speaker": "director", "text": "keep me"})
	requireStatus(t, w, 502)
	if !strings.Contains(w.Body.String(), "input_saved_delivery_failed") {
		t.Fatal(w.Body.String())
	}
	history, _ := s.store.History("main")
	if count != 1 || len(history) != 1 || history[0].Text != "keep me" {
		t.Fatal(count, history)
	}
	root, rt, temp := s.project, filepath.Dir(s.runtime.Root()), s.temporaryRoot
	s.Close()
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("connection file survived close", err)
	}
	next, err := New(Options{Project: root, RuntimeDir: rt, Token: testToken, Callback: callback.ProviderCodex, Dispatcher: s.dispatcher})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if count != 1 {
		t.Fatal("restart replayed input")
	}
	history, _ = next.store.History("main")
	if len(history) != 1 {
		t.Fatal(history)
	}
}
func TestNoRuntimeBusyGateAndStaleOutputCannotFollowNewInput(t *testing.T) {
	s := newTestServer(t)
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/inputs", "", map[string]any{"speaker": "director", "text": "one"}), 201)
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/inputs", "", map[string]any{"speaker": "chat", "text": "two"}), 201)
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/outputs", "", map[string]any{"input": 1, "kind": "story", "text": "stale"}), 422)
	requireStatus(t, businessRequest(t, s, "PATCH", "/chats/main/events/1", "", map[string]any{"text": "edit"}), 200)
	requireStatus(t, businessRequest(t, s, "POST", "/chats/main/branch", "", map[string]any{"position": 1, "name": "copy"}), 201)
	copy, _ := s.store.History("copy")
	if len(copy) != 1 || copy[0].Type != timeline.Input || copy[0].Text != "edit" {
		t.Fatal(copy)
	}
	requireStatus(t, businessRequest(t, s, "DELETE", "/chats/main/events/1", "", map[string]any{"truncate": true}), 200)
}

func TestConnectionFileRefusesWorkspaceTemp(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	s.endpoint = ""
	s.mu.Unlock()
	t.Setenv("TMPDIR", s.project)
	t.Setenv("TMP", s.project)
	t.Setenv("TEMP", s.project)
	if err := s.SetEndpoint("http://127.0.0.1:8717"); err == nil {
		t.Fatal("connection secret written into workspace")
	}
}
