package server

import (
	"github.com/luoyu/rpskill/internal/timeline"
	"strconv"
)

// Corrections atomically rewrite history under the server write lock.
func (s *Server) reviseMessage(line, target, method string, raw []byte) (any, int, error) {
	if position, err := strconv.Atoi(target); err == nil {
		if position < 1 {
			return nil, 0, fail(400, "invalid_position", "position must be positive")
		}
		target = timeline.MessageID(line, position)
	}
	var body struct {
		Characters *[]string `json:"characters,omitempty"`
		Truncate   bool      `json:"truncate,omitempty"`
		Text       *string   `json:"text,omitempty"`
	}
	if err := decodeBody(raw, &body); err != nil {
		return nil, 0, err
	}
	if (body.Truncate && method != "DELETE") || (method == "PATCH" && body.Text == nil && body.Characters == nil) || (method == "DELETE" && (body.Text != nil || body.Characters != nil)) {
		return nil, 0, fail(400, "invalid_message", "PATCH requires text or characters; DELETE must not contain either")
	}
	var text *string
	if method == "PATCH" {
		text = body.Text
	}
	var e timeline.Event
	var err error
	if body.Truncate {
		e, err = s.store.TruncateHistory(line, target)
	} else {
		e, err = s.store.AmendMessageContent(line, target, text, body.Characters)
	}
	if err != nil {
		return nil, 0, err
	}
	s.publish("timeline.event", e)
	if method == "DELETE" {
		return map[string]bool{"deleted": true}, 200, nil
	}
	return e, 200, nil
}
