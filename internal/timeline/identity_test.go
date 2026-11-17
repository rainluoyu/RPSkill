package timeline

import (
	"fmt"
	"testing"
)

func TestBuiltinIdentityProtocol(t *testing.T) {
	for _, tc := range []struct{ speaker, kind string }{
		{"director", "story"}, {"chat", "assistant"}, {"alice", "story"},
	} {
		t.Run(tc.speaker, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"type":"input","time":"2026-11-17T00:00:00Z","speaker":%q,"text":"prompt"}`, tc.speaker))
			output := []byte(fmt.Sprintf(`{"type":"output","time":"2026-11-17T00:00:01Z","kind":%q,"text":"reply"}`, tc.kind))
			events, err := ParseMessages([][]byte{raw, output}, "main")
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateHistory(events); err != nil {
				t.Fatal(err)
			}
			state := NewState(DefaultSettings())
			state.ProtectedCharacter = "bob"
			if err := state.Apply(events[0]); err != nil {
				t.Fatal(err)
			}
			expected := "bob"
			if tc.speaker == "director" {
				expected = ""
			}
			if state.LastSpeaker != tc.speaker {
				t.Fatal(state.LastSpeaker)
			}
			if tc.speaker == "alice" {
				expected = "alice"
			}
			if state.ProtectedCharacter != expected {
				t.Fatalf("protected %q, want %q", state.ProtectedCharacter, expected)
			}
			wrong := "assistant"
			if tc.kind == "assistant" {
				wrong = "story"
			}
			events[1].Kind = wrong
			if ValidateHistory(events) == nil {
				t.Fatal("mismatched output accepted")
			}
		})
	}
	for _, id := range []string{"human", "god", "Human", "God"} {
		raw := []byte(fmt.Sprintf(`{"type":"input","time":"2026-11-17T00:00:00Z","speaker":%q,"text":"prompt"}`, id))
		if _, err := ParseMessages([][]byte{raw}, "main"); err == nil {
			t.Fatalf("retired identity accepted: %s", id)
		}
	}
}

func TestProtectionAndLastSpeakerSequence(t *testing.T) {
	state := NewState(DefaultSettings())
	for _, tc := range []struct{ speaker, protected string }{
		{"alice", "alice"}, {"chat", "alice"}, {"bob", "bob"},
		{"director", ""}, {"chat", ""}, {"alice", "alice"},
	} {
		raw := []byte(fmt.Sprintf(`{"type":"input","time":"2026-11-17T00:00:00Z","speaker":%q,"text":"prompt"}`, tc.speaker))
		events, err := ParseMessages([][]byte{raw}, "main")
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Apply(events[0]); err != nil {
			t.Fatal(err)
		}
		if state.LastSpeaker != tc.speaker || state.ProtectedCharacter != tc.protected {
			t.Fatal(tc, state)
		}
	}
}
