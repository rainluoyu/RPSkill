package timeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/luoyu/rpskill/internal/storage"
	"time"
)

// Message is a v3 portable list item. Identity and relationships are never stored.
type Message struct {
	Type       string           `json:"type"`
	Time       time.Time        `json:"time"`
	Speaker    string           `json:"speaker,omitempty"`
	Kind       string           `json:"kind,omitempty"`
	Text       string           `json:"text,omitempty"`
	Characters []string         `json:"characters,omitempty"`
	Media      []MediaReference `json:"media,omitempty"`
}

// MessageID is a read-time address, not an ID written to history.
func MessageID(chat string, position int) string {
	return "evt_" + storage.SHA256([]byte(fmt.Sprintf("%s/%d", chat, position)))[:32]
}

func EncodeMessage(e Event) ([]byte, error) {
	if e.Type != Input && e.Type != Output {
		return nil, errors.New("history only accepts input/output")
	}
	m := Message{Type: e.Type, Time: e.Time, Speaker: e.Speaker, Kind: e.Kind, Text: e.Text, Media: e.Media, Characters: e.Characters}
	return json.Marshal(m)
}

// ParseMessages derives temporary addresses and reply grouping from list order.
func ParseMessages(lines [][]byte, chat string) ([]Event, error) {
	result := make([]Event, 0, len(lines))
	input := ""
	for i, raw := range lines {
		var m Message
		if err := storage.DecodeJSON(raw, &m); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if m.Type != Input && m.Type != Output {
			return nil, errors.New("history only accepts input/output")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		for key, value := range fields {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, fmt.Errorf("line %d: %s must not be null", i+1, key)
			}
			if m.Type == Input && (key == "kind" || key == "characters") || m.Type == Output && key == "speaker" {
				return nil, fmt.Errorf("line %d: %s is invalid for %s", i+1, key, m.Type)
			}
		}

		e := Event{Version: Version, ID: MessageID(chat, i+1), Type: m.Type, Time: m.Time, Timeline: chat, Speaker: m.Speaker, Kind: m.Kind, Text: m.Text, Media: m.Media, Characters: m.Characters}

		if m.Type == Input {
			input = e.ID
			e.InputID = e.ID
		} else {
			if input == "" {
				return nil, errors.New("output must follow an input")
			}
			e.InputID = input
		}
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		result = append(result, e)
	}
	if err := ValidateHistory(result); err != nil {
		return nil, err
	}
	return result, nil
}
