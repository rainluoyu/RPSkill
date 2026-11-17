package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyu/rpskill/internal/timeline"
)

func TestCLIResourceNamesRemainLiteralURLSegments(t *testing.T) {
	project, _ := actualServer(t, nil)
	call := func(args ...string) json.RawMessage {
		t.Helper()
		out, err := run(append(args, "--project", project, "--json")...)
		if err != nil {
			t.Fatalf("%v: %v (%s)", args, err, out)
		}
		var reply struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &reply); err != nil {
			t.Fatal(err)
		}
		return reply.Data
	}
	card := filepath.Join(t.TempDir(), "card.md")
	if err := os.WriteFile(card, []byte("---\nenabled: true\n---\nCard"), 0600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(t.TempDir(), "entry.md")
	if err := os.WriteFile(entry, []byte("---\nenabled: true\nactivation: always\nkeys: []\npriority: 1\n---\nWorld"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"角色 & +", "equals=name;@", "逗号,空 格"} {
		call("chat", "create", name)
		call("chat", "get", name)
		call("character", "create", name, "--file", card)
		call("character", "get", name)
		call("character", "dynamic", "update", name, "--chat", name, "--file", card)
		call("worldbook", "create", name, name, "--file", entry)
		call("worldbook", "get", name, name)
		call("input", "--chat", name, "--speaker", "director", "--text", "scene")
		call("emit", "--chat", name, "--input", "1", "--kind", "story", "--text", "output")
		var history []timeline.HistoryEvent
		if err := json.Unmarshal(call("chat", "history", name), &history); err != nil || len(history) != 2 {
			t.Fatal(err, history)
		}
		call("chat", "message", "edit", name, "2", "--text", "edited")
		call("chat", "branch", name, "2", "--name", name+" copy")
		call("chat", "delete", name+" copy")
		call("chat", "delete", name)
		call("character", "delete", name)
		call("worldbook", "delete", name, name)
	}
	var stories []timeline.Config
	if err := json.Unmarshal(call("chat", "list"), &stories); err != nil || len(stories) != 1 || stories[0].ID != "main" {
		t.Fatal(err, stories)
	}
}
