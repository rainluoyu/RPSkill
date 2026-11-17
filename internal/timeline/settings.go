// Package timeline defines portable timeline metadata, events and pure replay.
package timeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/luoyu/rpskill/internal/character/contentio"
	"maps"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func ValidID(id string) bool     { return idPattern.MatchString(id) }
func ValidChatID(id string) bool { return contentio.ValidName(id) }
func OrdinaryCharacter(id string) bool {
	if !contentio.ValidName(id) {
		return false
	}
	switch strings.ToLower(id) {
	case "human", "god", "chat", "director", "story", "assistant", "narrator", "environment":
		return false
	}
	return true
}

type Narrative struct {
	Perspective string `json:"perspective" yaml:"perspective"`
}
type Polishing struct {
	Level string `json:"level" yaml:"level"`
}
type WorldbookSettings struct {
	ScanRecent int             `json:"scan_recent" yaml:"scan_recent"`
	Categories map[string]bool `json:"categories" yaml:"categories,omitempty"`
	Entries    map[string]bool `json:"entries" yaml:"entries,omitempty"`
}
type Settings struct {
	Narrative  Narrative         `json:"narrative" yaml:"narrative"`
	Polishing  Polishing         `json:"polishing" yaml:"polishing"`
	Worldbook  WorldbookSettings `json:"worldbook" yaml:"worldbook"`
	Characters map[string]bool   `json:"characters" yaml:"characters"`
	Provider   string            `json:"provider,omitempty" yaml:"provider,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{Narrative: Narrative{"third"}, Polishing: Polishing{"basic"}, Worldbook: WorldbookSettings{Categories: map[string]bool{}, Entries: map[string]bool{}}, Characters: map[string]bool{}}
}
func (s Settings) Clone() Settings {
	s.Characters = maps.Clone(s.Characters)
	s.Worldbook.Categories = maps.Clone(s.Worldbook.Categories)
	s.Worldbook.Entries = maps.Clone(s.Worldbook.Entries)
	return s
}
func (s Settings) Validate() error {
	if s.Narrative.Perspective != "first" && s.Narrative.Perspective != "third" {
		return errors.New("perspective must be first or third")
	}
	if s.Polishing.Level != "off" && s.Polishing.Level != "basic" && s.Polishing.Level != "full" {
		return errors.New("polishing.level must be off, basic or full")
	}
	if s.Provider != "" && s.Provider != "codex" {
		return errors.New("provider must be codex or omitted")
	}
	if s.Worldbook.ScanRecent < 0 || s.Worldbook.ScanRecent > 1000 {
		return errors.New("scan_recent must be between 0 and 1000")
	}
	for id := range s.Worldbook.Categories {
		if !contentio.ValidName(id) {
			return fmt.Errorf("invalid category name %q", id)
		}
	}
	for id := range s.Worldbook.Entries {
		if !contentio.ValidReference(id) {
			return fmt.Errorf("invalid worldbook id %q", id)
		}
	}
	for id := range s.Characters {
		if !OrdinaryCharacter(id) {
			return fmt.Errorf("invalid character override %q", id)
		}
	}

	return nil
}

type WorldbookPatch struct {
	ScanRecent *int            `json:"scan_recent,omitempty" yaml:"scan_recent,omitempty"`
	Categories map[string]bool `json:"categories,omitempty" yaml:"categories,omitempty"`
	Entries    map[string]bool `json:"entries,omitempty" yaml:"entries,omitempty"`
}
type SettingsPatch struct {
	Narrative  *Narrative      `json:"narrative,omitempty" yaml:"narrative,omitempty"`
	Polishing  *Polishing      `json:"polishing,omitempty" yaml:"polishing,omitempty"`
	Worldbook  *WorldbookPatch `json:"worldbook,omitempty" yaml:"worldbook,omitempty"`
	Characters map[string]bool `json:"characters,omitempty" yaml:"characters,omitempty"`
	Provider   *string         `json:"provider,omitempty" yaml:"provider,omitempty"`
}

func (p SettingsPatch) Empty() bool {
	return p.Narrative == nil && p.Polishing == nil && p.Worldbook == nil && len(p.Characters) == 0 && p.Provider == nil
}
func (p SettingsPatch) Apply(base Settings) (Settings, error) {
	s := base.Clone()
	if p.Narrative != nil {
		s.Narrative = *p.Narrative
	}
	if p.Polishing != nil {
		s.Polishing = *p.Polishing
	}
	if p.Provider != nil {
		s.Provider = *p.Provider
	}
	if s.Characters == nil {
		s.Characters = map[string]bool{}
	}
	for k, v := range p.Characters {
		s.Characters[k] = v
	}
	if p.Worldbook != nil {
		w := p.Worldbook
		if w.ScanRecent != nil {
			s.Worldbook.ScanRecent = *w.ScanRecent
		}
		if s.Worldbook.Categories == nil {
			s.Worldbook.Categories = map[string]bool{}
		}
		for k, v := range w.Categories {
			s.Worldbook.Categories[k] = v
		}
		if s.Worldbook.Entries == nil {
			s.Worldbook.Entries = map[string]bool{}
		}
		for k, v := range w.Entries {
			s.Worldbook.Entries[k] = v
		}
	}
	return s, s.Validate()
}

// Config is a filesystem-derived story listing, not a persisted configuration.
type Config struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived,omitempty"`
	SettingsPatch
}

// Chat listings expose persisted story settings without ancestry or runtime state.
func (c Config) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID         string          `json:"id"`
		Name       string          `json:"name"`
		Archived   bool            `json:"archived,omitempty"`
		Narrative  *Narrative      `json:"narrative"`
		Polishing  *Polishing      `json:"polishing"`
		Worldbook  *WorldbookPatch `json:"worldbook"`
		Characters map[string]bool `json:"characters,omitempty"`
	}{c.ID, c.Name, c.Archived, c.Narrative, c.Polishing, c.Worldbook, c.Characters})
}
