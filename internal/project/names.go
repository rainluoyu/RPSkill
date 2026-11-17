package project

import (
	"bytes"
	"errors"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/character/contentio"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"github.com/luoyu/rpskill/internal/worldbook"
)

// RenameCharacter moves the complete folder and rewrites structured references,
// never story text. The existing file transaction makes the change recoverable.
func (s *Store) RenameCharacter(old, name string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	if !character.ValidID(old) || !character.ValidID(name) || strings.EqualFold(old, name) {
		return errors.New("invalid or conflicting character name")
	}
	card, err := character.Parse(content, name)
	if err != nil {
		return err
	}
	docs, err := character.Scan(s.Root())
	if err != nil {
		return err
	}
	found := false
	for _, d := range docs {
		if strings.EqualFold(d.Metadata.ID, name) {
			return errors.New("character name already exists")
		}
		if d.Metadata.ID == old {
			found = true
		}
	}
	if !found {
		return os.ErrNotExist
	}
	if card.Avatar != "" {
		asset, e := character.ReadAsset(s.Root(), old, card.Avatar)
		if e != nil {
			return e
		}
		switch asset.MIME {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return errors.New("avatar must be PNG/JPEG/GIF/WebP image")
		}
	}
	data, err := character.Encode(card)
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	if err := archiveTree(s.fs, "characters/"+old, files); err != nil {
		return err
	}
	files["characters/"+old+"/card.md"] = data
	writes := []storage.Write{}
	names := make([]string, 0, len(files))
	for file := range files {
		names = append(names, file)
	}
	sort.Strings(names)
	for _, file := range names {
		target := "characters/" + name + strings.TrimPrefix(file, "characters/"+old)
		w, e := s.fs.PrepareWrite(target, files[file])
		if e != nil {
			return e
		}
		if w.BeforeHash != "" {
			return errors.New("character destination already exists")
		}
		writes = append(writes, w)
	}
	c, err := s.config()
	if err != nil {
		return err
	}
	for chat := range c.Chats {
		settings, err := s.storySettings(chat)
		if err != nil {
			return err
		}
		if enabled, ok := settings.Characters[old]; ok {
			delete(settings.Characters, old)
			settings.Characters[name] = enabled
			w, err := s.prepareStorySettings(chat, settings)
			if err != nil {
				return err
			}
			writes = append(writes, w)
		}
		oldState := timelinePath(chat, "characters/"+old+".md")
		if state, err := s.fs.ReadFile(oldState); err == nil {
			w, err := s.fs.PrepareWrite(timelinePath(chat, "characters/"+name+".md"), state)
			if err != nil {
				return err
			}
			if w.BeforeHash != "" {
				return errors.New("character state destination exists")
			}
			remove, err := s.fs.PrepareFileRemoval(oldState)
			if err != nil {
				return err
			}
			writes = append(writes, w, remove)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		events, e := s.ownHistory(chat)
		if e != nil {
			return e
		}
		changed := false
		var buf bytes.Buffer
		for _, event := range events {
			if event.Speaker == old {
				event.Speaker = name
				changed = true
			}
			for i, id := range event.Characters {
				if id == old {
					event.Characters[i] = name
					changed = true
				}
			}
			line, e := timeline.EncodeMessage(event)
			if e != nil {
				return e
			}
			buf.Write(line)
			buf.WriteByte('\n')
		}
		if changed {
			w, e := s.fs.PrepareWrite(timelinePath(chat, "history.jsonl"), buf.Bytes())
			if e != nil {
				return e
			}
			writes = append(writes, w)
		}
	}
	remove, err := s.fs.PrepareTreeRemoval("characters/" + old)
	if err != nil {
		return err
	}
	writes = append(writes, remove)
	return storage.Commit(s.fs, s.runtime, writes)
}

func (s *Store) RenameWorldbook(category, old, name string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	if !contentio.ValidName(category) || !contentio.ValidName(old) || !contentio.ValidName(name) || strings.EqualFold(old, name) {
		return errors.New("invalid or conflicting entry name")
	}
	entry, err := worldbook.Parse(content, name)
	if err != nil {
		return err
	}
	data, err := worldbook.Encode(entry)
	if err != nil {
		return err
	}
	index, err := worldbook.Scan(s.Root())
	if err != nil {
		return err
	}
	for _, d := range index.Documents() {
		if d.Category == category && strings.EqualFold(d.Slug, name) {
			return errors.New("entry name already exists")
		}
	}
	remove, err := s.fs.PrepareFileRemoval(path.Join("worldbook", category, old+".md"))
	if err != nil {
		return err
	}
	target, err := s.fs.PrepareWrite(path.Join("worldbook", category, name+".md"), data)
	if err != nil {
		return err
	}
	c, err := s.config()
	if err != nil {
		return err
	}
	before, after := category+"/"+old, category+"/"+name
	writes := []storage.Write{target, remove}
	for chat := range c.Chats {
		settings, err := s.storySettings(chat)
		if err != nil {
			return err
		}
		changed := false
		if enabled, ok := settings.Worldbook.Entries[before]; ok {
			delete(settings.Worldbook.Entries, before)
			settings.Worldbook.Entries[after] = enabled
			changed = true
		}

		if changed {
			w, err := s.prepareStorySettings(chat, settings)
			if err != nil {
				return err
			}
			writes = append(writes, w)
		}
	}
	return storage.Commit(s.fs, s.runtime, writes)
}
