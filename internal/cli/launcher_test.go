package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/timeline"
)

func TestLauncherCLIRealHTTPAndBinaryEmission(t *testing.T) {
	inbox := &testInbox{notices: make(chan callback.Notification, 4)}
	p, _ := actualServer(t, inbox)
	binary := buildCLI(t)
	out, err := run("input", "--project", p, "--chat", "main", "--speaker", "chat", "--text", "原始问题", "--json")
	if err != nil {
		t.Fatal(err, out)
	}
	n := <-inbox.notices
	args := []string{"emit", "--connection", n.Connection, "--chat", "main", "--input", strconv.Itoa(n.Context.InputLine), "--kind", "assistant", "--text", "正式答复", "--json"}
	stdout, stderr, code := processCLI(binary, nil, args...)
	if code != 0 {
		t.Fatal(code, stdout, stderr)
	}
	args[len(args)-2] = "补充答复"
	_, stderr, code = processCLI(binary, nil, args...)
	if code != 0 {
		t.Fatal(code, stderr)
	}
	raw, err := os.ReadFile(filepath.Join(p, "chats/main/history.jsonl"))
	if err != nil || strings.Count(string(raw), "\n") != 3 {
		t.Fatal(string(raw), err)
	}
	var lines []timeline.HistoryEvent
	history, err := run("chat", "history", "main", "--connection", n.Connection, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal([]byte(history), &reply)
	json.Unmarshal(reply.Data, &lines)
	if len(lines) != 3 || lines[1].Text != "正式答复" || len(lines[1].Characters) != 0 || lines[2].Text != "补充答复" {
		t.Fatal(history)
	}
	bad := append([]string{}, args...)
	bad = append(bad, "--project", t.TempDir())
	if _, err := run(bad...); err == nil {
		t.Fatal("cross-project connection accepted")
	}
	if _, err := run("chat", "request", "retry", "main", "x"); err == nil {
		t.Fatal("removed request API accepted")
	}
}
func TestLauncherConfigurationFailsClosed(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	o := serveOptions{codexExecutable: os.Args[0]}
	if _, err := o.dispatcher(callback.ProviderCodex); err == nil {
		t.Fatal("guessed an owner")
	}
	if _, err := o.dispatcher(callback.Provider("grok")); err == nil {
		t.Fatal("started standalone Grok")
	}
	t.Setenv("CODEX_THREAD_ID", "current-owner")
	d, err := o.dispatcher(callback.ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	if d.(callback.Codex).Thread != "current-owner" {
		t.Fatal(d)
	}
	o.ownerThread = "explicit-owner"
	d, err = o.dispatcher(callback.ProviderCodex)
	if err != nil || d.(callback.Codex).Thread != "explicit-owner" {
		t.Fatal(d, err)
	}
	o.dispatchCommand = os.Args[0]
	o.dispatchArgs = []string{"inbox"}
	if _, err := o.dispatcher(callback.Provider("grok")); err == nil {
		t.Fatal("bridge bypassed provider validation")
	}
	if _, err := o.dispatcher(callback.ProviderCodex); err != nil {
		t.Fatal(err)
	}
}
