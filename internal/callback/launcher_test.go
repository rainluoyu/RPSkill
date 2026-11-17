package callback

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCodexQueuesOnlyToExplicitOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "args")
	binary := filepath.Join(dir, "codex")
	t.Setenv("RPS_QUEUE_TEST", file)
	os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RPS_QUEUE_TEST\"\n"), 0700)
	n := Notification{Skill: "/tmp/skill/SKILL.md", Connection: "/tmp/private/connection.json", Context: ContextHints{Chat: "中文", InputLine: 4}}
	if err := (Codex{Executable: binary, Thread: "owner;not-a-shell"}).Deliver(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	lines := strings.Split(string(raw), "\n")
	if len(lines) < 5 || lines[0] != "queue" || lines[1] != "--thread" || lines[2] != "owner;not-a-shell" || lines[3] != "--message" {
		t.Fatal(string(raw))
	}
	for _, forbidden := range []string{"--resume", "app-server", "thread/start", "session/new", "--yolo"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal(string(raw))
		}
	}
	if err := (Codex{Executable: binary}).Deliver(context.Background(), n); err == nil {
		t.Fatal("accepted missing owner")
	}
}
func TestExplicitBridgeGetsOnlyNotificationAndHonorsTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "bridge")
	output := filepath.Join(dir, "message")
	os.WriteFile(binary, []byte("#!/bin/sh\ncat > \"$1\"\n"), 0700)
	n := Notification{Skill: "skill", Connection: "connection", Context: ContextHints{Chat: "main"}}
	if err := (Command{Executable: binary, Args: []string{output}}).Deliver(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	var got Notification
	if json.Unmarshal(raw, &got) != nil || got.Context.Chat != "main" {
		t.Fatal(string(raw))
	}
	os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 20\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := (Command{Executable: binary}).Deliver(ctx, n); err == nil {
		t.Fatal("timeout accepted")
	}
}
