package cli

import (
	"encoding/json"
	"testing"

	"github.com/luoyu/rpskill/internal/timeline"
)

func TestHistoryActionsThroughCLI(t *testing.T) {
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
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
	var input timeline.Event
	if err := json.Unmarshal(call("input", "--chat", "main", "--speaker", "director", "--text", "original"), &input); err != nil {
		t.Fatal(err)
	}

	call("chat", "message", "edit", "main", input.ID, "--text", "edited")
	var history []timeline.HistoryEvent
	if err := json.Unmarshal(call("chat", "history", "main"), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Text != "edited" {
		t.Fatal(history)
	}
	var branch timeline.Config
	args := []string{"chat", "branch", "main", input.ID, "--name", "Another"}
	if err := json.Unmarshal(call(args...), &branch); err != nil {
		t.Fatal(err)
	}
	call("chat", "message", "delete", "main", input.ID)
	if err := json.Unmarshal(call("chat", "history", branch.ID), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID == input.ID || history[0].Text != "edited" {
		t.Fatal(history)
	}
	call("chat", "message", "delete", branch.ID, history[0].ID)
	call("validate", "--online")

}
