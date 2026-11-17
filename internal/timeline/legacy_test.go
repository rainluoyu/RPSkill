package timeline

import (
	"testing"
	"time"
)

func TestLegacyEventsAreRejected(t *testing.T) {
	for _, kind := range []string{"output.edit", "output.delete", "timeline.config", "character.update", "plot.plan"} {
		e := Event{Version: 1, ID: "evt_old", Type: kind, Timeline: "main", Time: time.Now().UTC()}
		if err := ValidateHistory([]Event{e}); err == nil {
			t.Errorf("accepted legacy event %s", kind)
		}
	}
	for _, raw := range []string{
		`{"version":1,"id":"evt_input","type":"input","timeline":"main","time":"2026-11-15T00:00:00Z","request_id":"evt_input","speaker":"director","text":"prompt","origin":{"timeline":"other","event_id":"evt_parent"}}`,
		`{"version":1,"id":"evt_del","type":"output.delete","timeline":"main","time":"2026-11-15T00:00:00Z","target_id":"evt_output","previous_id":"evt_output"}`,
	} {
		if _, err := ParseMessages([][]byte{[]byte(raw)}, "main"); err == nil {
			t.Fatalf("accepted legacy payload: %s", raw)
		}
	}
}
