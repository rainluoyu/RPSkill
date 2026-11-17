package project

import (
	"errors"
	"os"
	"path"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/character/contentio"
	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/worldbook"
	"go.yaml.in/yaml/v3"
)

type ArchiveResult struct {
	Kind  string   `json:"kind"`
	IDs   []string `json:"ids"`
	Files int      `json:"files"`
}

func archiveTree(fs *storage.FS, dir string, files map[string][]byte) error {
	entries, err := fs.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if dir != "." {
			name = path.Join(dir, name)
		}
		if entry.IsDir() {
			if err := archiveTree(fs, name, files); err != nil {
				return err
			}
		} else {
			data, err := fs.ReadFile(name)
			if err != nil {
				return err
			}
			files[name] = data
			total := 0
			for _, body := range files {
				total += len(body)
			}
			if total > media.MaxExpanded || len(files) > media.MaxFiles {
				return media.ErrTooLarge
			}
		}
	}
	return nil
}
func (s *Store) archiveFiles() (map[string][]byte, error) {
	files := map[string][]byte{}
	raw, err := s.fs.ReadFile("rp.yaml")
	if err != nil {
		return nil, err
	}
	files["rp.yaml"] = raw
	for _, dir := range []string{"worldbook", "characters", "chats"} {
		if err := archiveTree(s.fs, dir, files); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (s *Store) ExportArchive(kind, id, category string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	switch kind {
	case "project":
		if report := s.validate(); !report.Valid {
			return nil, errors.New("invalid project cannot be exported")
		}
		var err error
		files, err = s.archiveFiles()
		if err != nil {
			return nil, err
		}
	case "characters":
		if !character.ValidID(id) {
			return nil, errors.New("invalid character")
		}
		if _, err := s.fs.ReadFile("characters/" + id + "/card.md"); err != nil {
			return nil, err
		}
		if _, err := character.Scan(s.Root()); err != nil {
			return nil, err
		}
		if err := archiveTree(s.fs, "characters/"+id, files); err != nil {
			return nil, err
		}
	case "worldbook":
		if _, err := worldbook.Scan(s.Root()); err != nil {
			return nil, err
		}
		dir := "worldbook"
		if category != "" {
			if !contentio.ValidName(category) {
				return nil, errors.New("invalid category")
			}
			dir += "/" + category
		}
		if err := archiveTree(s.fs, dir, files); err != nil {
			return nil, err
		}
	case "chats":
		c, err := s.config()
		if err != nil {
			return nil, err
		}
		info, ok := c.Chats[id]
		if !ok {
			return nil, os.ErrNotExist
		}
		c.Chats = map[string]ChatInfo{id: info}
		c.LastChat = id
		raw, err := yaml.Marshal(c)
		if err != nil {
			return nil, err
		}
		files["rp.yaml"] = raw
		for _, dir := range []string{"characters", "worldbook", "chats/" + id} {
			if err := archiveTree(s.fs, dir, files); err != nil {
				return nil, err
			}
		}
		cleanup, _, err := candidate(files)
		if err != nil {
			return nil, err
		}
		cleanup()

	default:
		return nil, errors.New("unknown archive kind")
	}
	if len(files) == 0 {
		return nil, os.ErrNotExist
	}
	return media.EncodeZIP(files)
}
