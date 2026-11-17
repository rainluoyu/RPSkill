package server

import (
	"errors"
	"path/filepath"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/timeline"
)

// notification is a snapshot, not a task record. No read cursor is retained.
func (s *Server) notification(chat string) (callback.Notification, error) {
	n := callback.Notification{Skill: filepath.Join(s.skillDirectory, "SKILL.md"), Connection: s.connectionFile}
	if n.Connection == "" {
		return n, errors.New("server endpoint is not ready")
	}
	history, err := s.store.History(chat)
	if err != nil {
		return n, err
	}
	if len(history) == 0 || history[len(history)-1].Type != timeline.Input {
		return n, errors.New("latest history item is not an input")
	}
	input := history[len(history)-1]
	state, err := s.store.State(chat)
	if err != nil {
		return n, err
	}
	h := callback.ContextHints{Project: s.project, Chat: chat, History: "chats/" + chat + "/history.jsonl", InputLine: len(history), TotalLines: len(history), Speaker: input.Speaker, ProtectedCharacter: state.ProtectedCharacter}
	h.StorySettings = callback.FileHint{Path: "chats/" + chat + "/settings.yaml"}
	refs, err := s.store.ActivatedWorldbookAtInput(chat, input.ID)
	if err != nil {
		return n, err
	}
	for _, r := range refs {
		h.Worldbook = append(h.Worldbook, callback.FileHint{Path: r.Path})
	}
	chars, err := s.store.Characters()
	if err != nil {
		return n, err
	}
	for _, c := range chars {
		enabled := c.Metadata.Enabled
		if v, ok := state.Settings.Characters[c.Metadata.ID]; ok {
			enabled = v
		}
		if !enabled {
			continue
		}
		h.Characters = append(h.Characters, callback.FileHint{Path: c.Path})
		if _, ok := state.Characters[c.Metadata.ID]; ok {
			h.CharacterStates = append(h.CharacterStates, callback.FileHint{Path: "chats/" + chat + "/characters/" + c.Metadata.ID + ".md"})
		}
	}
	n.Context = h
	return n, nil
}
