package project

import (
	"errors"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/character/contentio"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"go.yaml.in/yaml/v3"
)

type StorySettings struct {
	Archived   bool                       `json:"archived,omitempty" yaml:"archived,omitempty"`
	Narrative  timeline.Narrative         `json:"narrative" yaml:"narrative"`
	Polishing  timeline.Polishing         `json:"polishing" yaml:"polishing"`
	Worldbook  timeline.WorldbookSettings `json:"worldbook" yaml:"worldbook"`
	Characters map[string]bool            `json:"characters,omitempty" yaml:"characters,omitempty"`
}

func DefaultStorySettings() StorySettings {
	s := timeline.DefaultSettings()
	return StorySettings{Narrative: s.Narrative, Polishing: s.Polishing, Worldbook: s.Worldbook, Characters: s.Characters}
}
func (s StorySettings) Effective() timeline.Settings {
	return timeline.Settings{Narrative: s.Narrative, Polishing: s.Polishing, Worldbook: s.Worldbook, Characters: s.Characters}
}
func (s StorySettings) Patch() timeline.SettingsPatch {
	settings := s.Effective().Clone()
	return timeline.SettingsPatch{Narrative: &settings.Narrative, Polishing: &settings.Polishing, Characters: settings.Characters,
		Worldbook: &timeline.WorldbookPatch{ScanRecent: &settings.Worldbook.ScanRecent, Categories: settings.Worldbook.Categories, Entries: settings.Worldbook.Entries}}
}
func (s StorySettings) Validate() error {
	return s.Effective().Validate()
}
func (s StorySettings) apply(p timeline.SettingsPatch) (StorySettings, error) {
	if p.Provider != nil {
		return s, errors.New("provider is a runtime setting")
	}
	next, err := p.Apply(s.Effective())
	if err != nil {
		return s, err
	}
	s.Narrative = next.Narrative
	s.Polishing = next.Polishing
	s.Worldbook = next.Worldbook
	s.Characters = next.Characters
	return s, s.Validate()
}
func (s *Store) storySettings(id string) (StorySettings, error) {
	var settings StorySettings
	if !timeline.ValidChatID(id) {
		return settings, errors.New("invalid story ID")
	}
	raw, err := s.fs.ReadFile(timelinePath(id, "settings.yaml"))
	if err != nil {
		return settings, err
	}
	if err = storage.DecodeYAML(raw, &settings); err != nil {
		return settings, err
	}
	return settings, settings.Validate()
}
func (s *Store) StorySettings(id string) (StorySettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return StorySettings{}, err
	}
	return s.storySettings(id)
}
func (s *Store) prepareStorySettings(id string, settings StorySettings) (storage.Write, error) {
	if err := settings.Validate(); err != nil {
		return storage.Write{}, err
	}
	raw, err := yaml.Marshal(settings)
	if err != nil {
		return storage.Write{}, err
	}
	return s.fs.PrepareWrite(timelinePath(id, "settings.yaml"), raw)
}
func (s *Store) SetTimelineSettings(id string, p timeline.SettingsPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	if p.Empty() {
		return errors.New("empty story settings")
	}
	settings, err := s.storySettings(id)
	if err != nil {
		return err
	}
	settings, err = settings.apply(p)
	if err != nil {
		return err
	}
	w, err := s.prepareStorySettings(id, settings)
	if err != nil {
		return err
	}
	return storage.Commit(s.fs, s.runtime, []storage.Write{w})
}
func (s *Store) characterStates(id string) (map[string]string, error) {
	states := map[string]string{}
	entries, err := s.fs.ReadDir(timelinePath(id, "characters"))
	if errors.Is(err, os.ErrNotExist) {
		return states, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".md")
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || !character.ValidID(name) {
			return nil, errors.New("story characters must be named Markdown files")
		}
		if _, err := s.fs.ReadFile("characters/" + name + "/card.md"); err != nil {
			return nil, err
		}
		raw, err := s.fs.ReadFile(timelinePath(id, "characters/"+entry.Name()))
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(raw) || len(raw) > 4<<20 {
			return nil, errors.New("invalid character-state document")
		}
		states[name] = string(raw)
	}
	return states, nil
}

// UpdateCharacterState never touches history or the shared base card.
func (s *Store) UpdateCharacterState(id, name, document string, expected *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	settings, err := s.storySettings(id)
	if err != nil {
		return err
	}
	if settings.Archived {
		return errors.New("story is archived")
	}
	if !character.ValidID(name) || !utf8.ValidString(document) || len(document) > 4<<20 {
		return errors.New("invalid character-state document")
	}
	if _, err := s.fs.ReadFile("characters/" + name + "/card.md"); err != nil {
		return err
	}
	file := timelinePath(id, "characters/"+name+".md")
	current, err := s.fs.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if expected != nil && string(current) != *expected {
		return errors.New("state_conflict: story state changed; reload before saving")
	}
	if document == "" {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		w, err := s.fs.PrepareFileRemoval(file)
		if err != nil {
			return err
		}
		return storage.Commit(s.fs, s.runtime, []storage.Write{w})
	}
	return s.fs.AtomicWrite(file, []byte(document))
}
func (s *Store) WorldbookCategories() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	return s.worldbookCategories()
}
func (s *Store) worldbookCategories() ([]string, error) {
	entries, err := s.fs.ReadDir("worldbook")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
func (s *Store) CreateWorldbookCategory(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	if !contentio.ValidName(name) {
		return errors.New("invalid category name")
	}
	names, err := s.worldbookCategories()
	if err != nil {
		return err
	}
	for _, existing := range names {
		if strings.EqualFold(existing, name) {
			if existing == name {
				return nil
			}
			return errors.New("category name conflicts with existing folder")
		}
	}
	return s.fs.MkdirAll("worldbook/" + name)
}
