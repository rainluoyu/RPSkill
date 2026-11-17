package project

import (
	"bytes"
	"errors"
	"github.com/luoyu/rpskill/internal/timeline"
	"os"
)

func (s *Store) writeHistory(id string, events []timeline.Event) error {
	if err := timeline.ValidateHistory(events); err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, e := range events {
		raw, err := timeline.EncodeMessage(e)
		if err != nil {
			return err
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	return s.fs.AtomicWriteJSONL(timelinePath(id, "history.jsonl"), buf.Bytes())
}

// AmendMessage replaces a message, or deletes an input together with its replies.
// No audit events enter history; unrelated later messages and media files remain.
func (s *Store) AmendMessage(id, target string, text *string) (timeline.Event, error) {
	return s.amendMessage(id, target, text, nil, false)
}

// AmendMessageContent updates selected output metadata without rewriting prose.
func (s *Store) AmendMessageContent(id, target string, text *string, characters *[]string) (timeline.Event, error) {
	return s.amendMessage(id, target, text, characters, false)
}

// TruncateHistory removes the selected message and every later message.
// Character documents and media files are independent and remain untouched.
func (s *Store) TruncateHistory(id, target string) (timeline.Event, error) {
	return s.amendMessage(id, target, nil, nil, true)
}

func (s *Store) amendMessage(id, target string, text *string, characters *[]string, truncate bool) (timeline.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return timeline.Event{}, err
	}
	meta, err := s.metadata(id)
	if err != nil {
		return timeline.Event{}, err
	}
	if meta.Archived {
		return timeline.Event{}, errors.New("chat is archived")
	}
	h, _, err := s.view(id, "")
	if err != nil {
		return timeline.Event{}, err
	}
	for i, e := range h {
		if e.ID != target {
			continue
		}
		if e.Type != timeline.Input && e.Type != timeline.Output {
			return e, errors.New("only messages can be changed")
		}
		if truncate {
			return e, s.writeHistory(id, h[:i])
		}
		if text == nil && characters == nil {
			remaining := make([]timeline.Event, 0, len(h)-1)
			for _, m := range h {
				if m.ID == target || (e.Type == timeline.Input && m.InputID == target) {
					continue
				}
				remaining = append(remaining, m)
			}
			return e, s.writeHistory(id, remaining)
		}
		if text != nil {
			e.Text = *text
		}
		if characters != nil {
			if e.Type != timeline.Output {
				return e, errors.New("characters are only valid on outputs")
			}
			e.Characters = append([]string(nil), (*characters)...)
			for _, name := range e.Characters {
				if !timeline.OrdinaryCharacter(name) {
					return e, errors.New("invalid output character")
				}
				if _, err := s.fs.ReadFile("characters/" + name + "/card.md"); err != nil {
					return e, err
				}
			}
		}
		h[i] = e
		if err := s.writeHistory(id, h); err != nil {
			return e, err
		}
		fresh, err := s.ownHistory(id)
		if err != nil {
			return e, err
		}
		return fresh[i], nil
	}
	return timeline.Event{}, os.ErrNotExist
}
