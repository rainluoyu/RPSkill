package project

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"go.yaml.in/yaml/v3"
)

// renameStory runs under the Store lock. File publication, old-folder removal
// and last_chat updates use one recoverable transaction; prose stays untouched.
func (s *Store) renameStory(old, name string, settings StorySettings) (timeline.Config, error) {
	meta := timeline.Config{ID: name, Name: name, Archived: settings.Archived, SettingsPatch: settings.Patch()}
	if !timeline.ValidChatID(name) {
		return meta, errors.New("invalid story folder name")
	}
	config, err := s.config()
	if err != nil {
		return meta, err
	}
	for id := range config.Chats {
		if strings.EqualFold(id, name) {
			return meta, errors.New("story name already exists")
		}
	}
	if _, err := s.fs.ReadDir("chats/" + name); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = errors.New("story destination already exists")
		}
		return meta, err
	}
	files := map[string][]byte{}
	if err := archiveTree(s.fs, "chats/"+old, files); err != nil {
		return meta, err
	}
	raw, err := yaml.Marshal(settings)
	if err != nil {
		return meta, err
	}
	files[timelinePath(old, "settings.yaml")] = raw
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writes := []storage.Write{}
	for _, key := range keys {
		target := "chats/" + name + strings.TrimPrefix(key, "chats/"+old)
		w, err := s.fs.PrepareWrite(target, files[key])
		if err != nil {
			return meta, err
		}
		if w.BeforeHash != "" {
			return meta, errors.New("story destination exists")
		}
		writes = append(writes, w)
	}
	if config.LastChat == old {
		config.LastChat = name
		raw, err = yaml.Marshal(config)
		if err != nil {
			return meta, err
		}
		w, err := s.fs.PrepareWrite("rp.yaml", raw)
		if err != nil {
			return meta, err
		}
		writes = append(writes, w)
	}
	remove, err := s.fs.PrepareTreeRemoval("chats/" + old)
	if err != nil {
		return meta, err
	}
	writes = append(writes, remove)
	return meta, storage.Commit(s.fs, s.runtime, writes)
}
