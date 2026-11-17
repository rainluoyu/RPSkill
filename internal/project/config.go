// Package project owns RP Workspace validation and the single-writer Store.
package project

import (
	"errors"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
)

type ChatInfo struct {
	Name     string `json:"name"`
	Archived bool   `json:"archived,omitempty"`
}

// Only LastChat is persisted. The other fields are filesystem-derived API data.
type Config struct {
	LastChat string              `json:"last_chat" yaml:"last_chat"`
	Name     string              `json:"name" yaml:"-"`
	Version  int                 `json:"version" yaml:"-"`
	Chats    map[string]ChatInfo `json:"chats" yaml:"-"`
	Provider string              `json:"provider,omitempty" yaml:"-"`
}

func DefaultConfig(name string) Config { return Config{LastChat: "main", Name: name, Version: 3} }
func (c Config) Validate() error {
	if !timeline.ValidChatID(c.LastChat) {
		return errors.New("last_chat requires a story ID")
	}
	if c.Provider != "" && c.Provider != "codex" {
		return errors.New("invalid provider")
	}
	return nil
}
func ParseConfig(data []byte) (Config, error) {
	var c Config
	if err := storage.DecodeYAML(data, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
