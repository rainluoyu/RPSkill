// Package callback delivers notifications to the conversation that started RPSkill.
// It never creates, resumes, supervises or cancels an Agent execution.
package callback

import "context"

type Provider string

const ProviderCodex Provider = "codex"

func (p Provider) Valid() bool { return p == ProviderCodex }

type FileHint struct {
	Path string `json:"path"`
}
type ContextHints struct {
	Project            string     `json:"project"`
	Chat               string     `json:"chat"`
	History            string     `json:"history"`
	InputLine          int        `json:"input_line"`
	TotalLines         int        `json:"total_lines"`
	Speaker            string     `json:"speaker"`
	StorySettings      FileHint   `json:"story_settings"`
	Characters         []FileHint `json:"characters"`
	CharacterStates    []FileHint `json:"character_states"`
	Worldbook          []FileHint `json:"worldbook"`
	ProtectedCharacter string     `json:"protected_character,omitempty"`
}
type Notification struct {
	Skill      string       `json:"skill"`
	Connection string       `json:"connection"`
	Context    ContextHints `json:"context"`
}

// Deliver returns only whether the launcher's inbox accepted a notification.
// It says nothing about model execution or whether a formal output was written.
type Dispatcher interface {
	Deliver(context.Context, Notification) error
}
