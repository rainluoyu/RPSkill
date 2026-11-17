package timeline

import (
	"errors"
	"fmt"
)

type State struct {
	Settings           Settings          `json:"settings"`
	LastSpeaker        string            `json:"last_speaker,omitempty"`
	ProtectedCharacter string            `json:"protected_character,omitempty"`
	Characters         map[string]string `json:"characters"`
}

func NewState(settings Settings) State {
	return State{Settings: settings.Clone(), Characters: map[string]string{}}
}
func (s *State) Apply(e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	switch e.Type {
	case Input:
		s.LastSpeaker = e.Speaker
		if e.Speaker == "director" {
			s.ProtectedCharacter = ""
		} else if OrdinaryCharacter(e.Speaker) {
			s.ProtectedCharacter = e.Speaker
		}

	}
	return nil
}

// ValidateHistory validates a flat input/output list, not an execution log.
func ValidateHistory(events []Event) error {
	ids := map[string]bool{}
	var input Event
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("message %s: %w", e.ID, err)
		}
		if ids[e.ID] {
			return fmt.Errorf("duplicate message address %s", e.ID)
		}
		ids[e.ID] = true
		if e.Type == Input {
			input = e
			continue
		}
		if input.ID == "" || e.InputID != input.ID || e.Timeline != input.Timeline {
			return errors.New("output must follow its input in the same story")
		}
		expected := "story"
		if input.Speaker == "chat" {
			expected = "assistant"
		}
		if e.Kind != expected {
			return errors.New("output kind conflicts with input speaker")
		}
	}
	return nil
}

// HistoryEvent is a read projection, never a serialized replacement for JSONL.
type HistoryEvent struct {
	Event
	Position int `json:"position"`
}

func ResolveHistory(events []Event) []HistoryEvent {
	result := make([]HistoryEvent, len(events))
	for i, e := range events {
		result[i] = HistoryEvent{Event: e, Position: i + 1}
	}
	return result
}
