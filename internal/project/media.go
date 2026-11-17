package project

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"go.yaml.in/yaml/v3"
)

// UploadMedia uses a stable generated ID in the portable filename, allowing an
// unreferenced upload to survive loss of all runtime indices and receipts.
func (s *Store) UploadMedia(u media.Upload) (media.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return media.Record{}, err
	}
	if err := u.Validate(); err != nil {
		return media.Record{}, err
	}
	if u.ID == "" {
		id, err := timeline.NewID("med")
		if err != nil {
			return media.Record{}, err
		}
		u.ID = id
	}
	var card character.Card
	folder := ""
	if u.Timeline != "" {
		if _, err := s.metadata(u.Timeline); err != nil {
			return media.Record{}, err
		}
		folder = timelinePath(u.Timeline, "media")
	} else {
		raw, err := s.fs.ReadFile("characters/" + u.Character + "/card.md")
		if err != nil {
			return media.Record{}, err
		}
		card, err = character.Parse(raw, u.Character)
		if err != nil {
			return media.Record{}, err
		}
		folder = "characters/" + u.Character
		if u.Expression != "" {
			folder += "/expressions"
		}
	}
	filename := u.ID + "--" + u.Name
	if u.Expression != "" {
		filename = u.ID + "--" + u.Expression + "--" + u.Name
	}
	target := path.Join(folder, filename)
	existing, err := s.fs.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return media.Record{}, err
	}
	if err == nil && !bytes.Equal(existing, u.Data) {
		return media.Record{}, media.ErrConflict
	}
	writes := []storage.Write{}
	if err != nil {
		w, e := s.fs.PrepareWrite(target, u.Data)
		if e != nil {
			return media.Record{}, e
		}
		writes = append(writes, w)
	}
	if u.Avatar {
		card.Avatar = filename
		metadata, e := yaml.Marshal(card)
		if e != nil {
			return media.Record{}, e
		}
		content := append([]byte("---\n"), metadata...)
		content = append(content, []byte("---\n"+card.Body)...)
		w, e := s.fs.PrepareWrite("characters/"+u.Character+"/card.md", content)
		if e != nil {
			return media.Record{}, e
		}
		writes = append(writes, w)
	}
	if err := storage.Commit(s.fs, s.runtime, writes); err != nil {
		return media.Record{}, err
	}
	relative := strings.TrimPrefix(target, "characters/"+u.Character+"/")
	if u.Timeline != "" {
		relative = strings.TrimPrefix(target, "chats/"+u.Timeline+"/")
	}
	return media.Record{MediaReference: timeline.MediaReference{ID: u.ID, Path: relative, Name: u.Name, MIME: media.Detect(u.Data)}, Timeline: u.Timeline, Character: u.Character, Expression: u.Expression, Size: int64(len(u.Data)), SHA256: storage.SHA256(u.Data), Preview: media.Preview(media.Detect(u.Data))}, nil
}
func (s *Store) mediaIndex() (map[string]media.Record, error) {
	references := map[string]timeline.MediaReference{}
	metas, err := s.timelines()
	if err != nil {
		return nil, err
	}
	for _, m := range metas {
		events, e := s.ownHistory(m.ID)
		if e != nil {
			return nil, e
		}
		for _, event := range events {
			for _, ref := range event.Media {
				file := timelinePath(m.ID, ref.Path)
				if prior, ok := references[file]; ok && prior.ID != ref.ID {
					return nil, fmt.Errorf("media conflict at %s", ref.Path)
				}
				references[file] = ref
			}
		}
	}
	records := map[string]media.Record{}
	var walk func(string, string, string) error
	walk = func(folder, line, char string) error {
		entries, err := s.fs.ReadDir(folder)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			file := path.Join(folder, entry.Name())
			if entry.IsDir() {
				if err := walk(file, line, char); err != nil {
					return err
				}
				continue
			}
			if char != "" && entry.Name() == "card.md" {
				continue
			}
			data, err := s.fs.ReadFile(file)
			if err != nil {
				return err
			}
			id := "med_" + storage.SHA256([]byte(file))[:32]
			name := entry.Name()
			expression := ""
			pieces := strings.SplitN(name, "--", 3)
			if len(pieces) >= 2 && timeline.ValidID(pieces[0]) && strings.HasPrefix(pieces[0], "med_") {
				id = pieces[0]
				name = strings.Join(pieces[1:], "--")
				if strings.Contains(file, "/expressions/") && len(pieces) == 3 {
					expression = pieces[1]
					name = pieces[2]
				}
			}
			mime := media.Detect(data)
			relative := strings.TrimPrefix(file, "characters/"+char+"/")
			if line != "" {
				relative = strings.TrimPrefix(file, "chats/"+line+"/")
			}
			ref := timeline.MediaReference{ID: id, Path: relative, Name: name, MIME: mime}
			if existing, ok := references[file]; ok {
				ref.ID = existing.ID
				ref.Name = existing.Name
			}
			if char != "" && strings.Contains(file, "/expressions/") && expression == "" {
				expression = strings.TrimSuffix(entry.Name(), path.Ext(entry.Name()))
			}
			record := media.Record{MediaReference: ref, Timeline: line, Character: char, Expression: expression, Size: int64(len(data)), SHA256: storage.SHA256(data), Preview: media.Preview(mime)}
			if prior, ok := records[ref.ID]; ok && prior.FilePath() != file {
				return fmt.Errorf("media ID conflict: %s", ref.ID)
			}
			records[ref.ID] = record
		}
		return nil
	}
	for _, m := range metas {
		if err := walk(timelinePath(m.ID, "media"), m.ID, ""); err != nil {
			return nil, err
		}
	}
	chars, err := s.fs.ReadDir("characters")
	if err != nil {
		return nil, err
	}
	for _, char := range chars {
		if !char.IsDir() {
			return nil, errors.New("invalid character directory")
		}
		if err := walk("characters/"+char.Name(), "", char.Name()); err != nil {
			return nil, err
		}
	}
	return records, nil
}
func (s *Store) ListMedia(line, char string) ([]media.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	if line != "" && !timeline.ValidChatID(line) || char != "" && !timeline.OrdinaryCharacter(char) {
		return nil, errors.New("invalid media filter")
	}
	if line != "" {
		if _, err := s.metadata(line); err != nil {
			return nil, err
		}
	}
	index, err := s.mediaIndex()
	if err != nil {
		return nil, err
	}
	result := []media.Record{}
	for _, record := range index {
		if (line == "" || record.Timeline == line) && (char == "" || record.Character == char) {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
func (s *Store) Media(id string) (media.Record, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return media.Record{}, nil, err
	}
	index, err := s.mediaIndex()
	if err != nil {
		return media.Record{}, nil, err
	}
	record, ok := index[id]
	if !ok {
		return media.Record{}, nil, os.ErrNotExist
	}
	data, err := s.fs.ReadFile(record.FilePath())
	return record, data, err
}
func (s *Store) DeleteMedia(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	index, err := s.mediaIndex()
	if err != nil {
		return err
	}
	record, ok := index[id]
	if !ok {
		return os.ErrNotExist
	}
	metas, err := s.timelines()
	if err != nil {
		return err
	}
	for _, m := range metas {
		events, err := s.ownHistory(m.ID)
		if err != nil {
			return err
		}
		for _, e := range events {
			for _, ref := range e.Media {
				if ref.ID == id || (m.ID == record.Timeline && ref.Path == record.Path) {
					return errors.New("media is referenced by preserved history")
				}
			}
		}
	}
	cards, err := character.Scan(s.Root())
	if err != nil {
		return err
	}
	for _, card := range cards {
		if card.Metadata.Avatar != "" && record.Character == card.Metadata.ID && card.Metadata.Avatar == record.Path {
			return errors.New("media is referenced by character avatar")
		}
	}
	write, err := s.fs.PrepareFileRemoval(record.FilePath())
	if err != nil {
		return err
	}
	return storage.Commit(s.fs, s.runtime, []storage.Write{write})
}
