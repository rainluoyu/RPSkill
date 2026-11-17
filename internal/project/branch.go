package project

import (
	"errors"
	"os"
	"path"
	"strings"

	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
)

// BranchChat materializes a prefix as an independent chat, never an ancestry link.
// IDs and media paths are remapped so either chat can be deleted or exported alone.
func (s *Store) BranchChat(source, until, id, name string) (timeline.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return timeline.Config{}, err
	}
	if !timeline.ValidChatID(source) || !timeline.ValidChatID(id) || until == "" {
		return timeline.Config{}, errors.New("branch requires source, message, valid ID and name")
	}
	c, err := s.config()
	if err != nil {
		return timeline.Config{}, err
	}
	if _, ok := c.Chats[id]; ok {
		return timeline.Config{}, errors.New("chat already exists")
	}
	// Do not adopt an orphan directory or an external/symlink destination.
	if _, err := s.fs.ReadDir("chats/" + id); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = errors.New("chat directory already exists")
		}
		return timeline.Config{}, err
	}
	history, _, err := s.view(source, until)
	if err != nil {
		return timeline.Config{}, err
	}
	remap := func(prefix, old string) string { return prefix + "_" + storage.SHA256([]byte(id + "/" + old))[:32] }
	copies := make([]timeline.Event, 0, len(history))
	writes := []storage.Write{}
	resources := map[string]timeline.MediaReference{}
	total := 0
	for _, item := range history {
		e := item
		e.Timeline = id
		e.Media = append([]timeline.MediaReference(nil), item.Media...)

		for i, ref := range e.Media {
			if err := media.Name(ref.Name); err != nil {
				return timeline.Config{}, err
			}
			copied, ok := resources[ref.Path]
			if !ok {
				if !strings.HasPrefix(ref.Path, "media/") {
					return timeline.Config{}, errors.New("media outside source chat")
				}
				data, err := s.fs.ReadFile(timelinePath(source, ref.Path))
				if err != nil {
					return timeline.Config{}, err
				}
				if media.Detect(data) != ref.MIME {
					return timeline.Config{}, errors.New("media type changed")
				}
				copied = ref
				copied.ID = remap("med", ref.ID)
				copied.Path = path.Join("media", copied.ID+"--"+ref.Name)
				total += len(data)
				if total > storage.MaxTransactionBytes || len(writes)+3 > storage.MaxTransactionWrites {
					return timeline.Config{}, media.ErrTooLarge
				}
				w, err := s.fs.PrepareWrite(timelinePath(id, copied.Path), data)
				if err != nil {
					return timeline.Config{}, err
				}
				writes = append(writes, w)
				resources[ref.Path] = copied
			}
			e.Media[i] = copied
		}
		copies = append(copies, e)
	}
	sourceSettings, err := s.storySettings(source)
	if err != nil {
		return timeline.Config{}, err
	}
	states, err := s.characterStates(source)
	if err != nil {
		return timeline.Config{}, err
	}
	// State documents are current snapshots, not historical revisions.
	for name, document := range states {
		w, err := s.fs.PrepareWrite(timelinePath(id, "characters/"+name+".md"), []byte(document))
		if err != nil {
			return timeline.Config{}, err
		}
		writes = append(writes, w)
	}
	return s.createTimeline(TimelineOptions{ID: id, Name: name, Overrides: sourceSettings.Patch()}, copies, writes)
}
