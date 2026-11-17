package project

import (
	"errors"
	"os"
	"path"
	"sort"

	"github.com/luoyu/rpskill/internal/character/contentio"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"go.yaml.in/yaml/v3"
)

// UpdateTimelineMetadata applies story settings and optionally renames its folder.
func (s *Store) UpdateTimelineMetadata(id, name string, archived *bool, settings ...timeline.SettingsPatch) (timeline.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return timeline.Config{}, err
	}
	meta, err := s.metadata(id)
	if err != nil {
		return meta, err
	}
	if name != "" {
		meta.Name = name
	}
	if archived != nil {
		meta.Archived = *archived
	}
	current, err := s.storySettings(id)
	if err != nil {
		return meta, err
	}
	if len(settings) > 0 {
		current, err = current.apply(settings[0])
		if err != nil {
			return meta, err
		}
	}
	current.Archived = meta.Archived
	if name != "" && name != id {
		return s.renameStory(id, name, current)
	}
	w, err := s.prepareStorySettings(id, current)
	if err != nil {
		return meta, err
	}
	meta.SettingsPatch = current.Patch()
	return meta, storage.Commit(s.fs, s.runtime, []storage.Write{w})
}
func (s *Store) DeleteTimeline(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	c, err := s.config()
	if err != nil {
		return err
	}
	if _, ok := c.Chats[id]; !ok {
		return os.ErrNotExist
	}
	if len(c.Chats) <= 1 {
		return errors.New("cannot delete the last chat")
	}
	delete(c.Chats, id)
	if c.LastChat == id {
		names := make([]string, 0, len(c.Chats))
		for name := range c.Chats {
			names = append(names, name)
		}
		sort.Strings(names)
		c.LastChat = names[0]
	}
	raw, _ := yaml.Marshal(c)
	a, err := s.fs.PrepareWrite("rp.yaml", raw)
	if err != nil {
		return err
	}
	b, err := s.fs.PrepareTreeRemoval(path.Join("chats", id))
	if err != nil {
		return err
	}
	return storage.Commit(s.fs, s.runtime, []storage.Write{a, b})
}

func (s *Store) DeleteCharacter(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	if !timeline.OrdinaryCharacter(id) {
		return errors.New("invalid character id")
	}
	metas, err := s.timelines()
	if err != nil {
		return err
	}
	for _, m := range metas {
		if _, err := s.fs.ReadFile(timelinePath(m.ID, "characters/"+id+".md")); err == nil {
			return errors.New("character is referenced by story state")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		events, err := s.ownHistory(m.ID)
		if err != nil {
			return err
		}
		for _, e := range events {
			for _, name := range e.Characters {
				if name == id {
					return errors.New("character is referenced by history")
				}
			}
			if e.Speaker == id {
				return errors.New("character is referenced by history")
			}
		}
	}
	w, err := s.fs.PrepareTreeRemoval(path.Join("characters", id))
	if err != nil {
		return err
	}
	return storage.Commit(s.fs, s.runtime, []storage.Write{w})
}
func (s *Store) DeleteWorldbook(category, slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	if !contentio.ValidName(category) || !contentio.ValidName(slug) {
		return errors.New("invalid worldbook path")
	}
	return s.fs.Remove(path.Join("worldbook", category, slug+".md"))
}

// ContentExists distinguishes creation from replacement without ignoring read errors.
func (s *Store) ContentExists(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return false, err
	}
	_, err := s.fs.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
