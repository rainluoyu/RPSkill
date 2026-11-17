package server

import (
	"github.com/luoyu/rpskill/internal/timeline"
)

func (s *Server) branchChat(source string, raw []byte) (any, int, error) {
	var body struct {
		EventID  string `json:"event_id,omitempty"`
		Position int    `json:"position,omitempty"`
		Name     string `json:"name"`
	}
	if err := decodeBody(raw, &body); err != nil {
		return nil, 0, err
	}
	id := body.Name
	if body.Position > 0 {
		body.EventID = timeline.MessageID(source, body.Position)
	}
	chat, err := s.store.BranchChat(source, body.EventID, id, body.Name)
	if err != nil {
		return nil, 0, err
	}
	return chat, 201, nil
}
