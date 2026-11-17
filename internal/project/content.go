package project

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/character/contentio"
	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
	"github.com/luoyu/rpskill/internal/worldbook"
)

// Characters returns immutable read models; formal content changes remain Store
// mutations (HTTP management is Phase 2). No parser writes workspace files.
func (s *Store) Characters() ([]character.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	return character.Scan(s.Root())
}
func (s *Store) Worldbook() (*worldbook.Index, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	return worldbook.Scan(s.Root())
}

// ActivatedWorldbook previews an input that has NOT yet been persisted. Callback
// dispatch after Append must use ActivatedWorldbookAtInput to exclude the
// current input from the recent-message window.
func (s *Store) ActivatedWorldbook(id string, input worldbook.Message) ([]worldbook.Reference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	history, state, err := s.view(id, "")
	if err != nil {
		return nil, err
	}
	return s.activateWorldbook(input, history, state.Settings)
}

// ActivatedWorldbookAtInput locates an input by its current history address,
// never by text comparison. The scan uses current story settings and excludes
// that input and any later messages from the recent-message window.
func (s *Store) ActivatedWorldbookAtInput(id, inputID string) ([]worldbook.Reference, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return nil, err
	}
	if inputID == "" {
		return nil, errors.New("input address is required")
	}
	history, state, err := s.view(id, inputID)
	if err != nil {
		return nil, err
	}
	input := history[len(history)-1]
	if input.Type != timeline.Input || input.Timeline != id {
		return nil, errors.New("address must identify an input in the current story")
	}
	message := worldbook.Message{Type: input.Type, Speaker: input.Speaker, Text: input.Text}
	return s.activateWorldbook(message, history[:len(history)-1], state.Settings)
}

func (s *Store) activateWorldbook(input worldbook.Message, history []timeline.Event, settings timeline.Settings) ([]worldbook.Reference, error) {
	index, err := worldbook.Scan(s.Root())
	if err != nil {
		return nil, err
	}
	recent := make([]worldbook.Message, 0, len(history))
	for _, e := range timeline.ResolveHistory(history) {
		recent = append(recent, worldbook.Message{Type: e.Type, Speaker: e.Speaker, Kind: e.Kind, Text: e.Text})
	}
	w := settings.Worldbook
	return index.Activate(input, recent, worldbook.Settings{Categories: w.Categories, Entries: w.Entries, ScanRecent: w.ScanRecent})
}
func (s *Store) ReadCharacterAsset(id, relative string) (character.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(false); err != nil {
		return character.Asset{}, err
	}
	return character.ReadAsset(s.Root(), id, relative)
}

func (s *Store) validateAppendContent(id string, state timeline.State, e timeline.Event) error {
	if e.Type == timeline.Input && timeline.OrdinaryCharacter(e.Speaker) {
		docs, err := character.Scan(s.Root())
		if err != nil {
			return err
		}
		found := false
		for _, c := range character.Effective(docs, state.Settings.Characters) {
			if c.Metadata.ID == e.Speaker {
				found = true
				if !c.Enabled {
					return errors.New("input character is disabled")
				}
			}
		}
		if !found {
			return errors.New("input character does not exist")
		}
	}
	if len(e.Characters) > 0 {
		docs, err := character.Scan(s.Root())
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, card := range docs {
			known[card.Metadata.ID] = true
		}
		for _, name := range e.Characters {
			if !known[name] {
				return errors.New("output character does not exist: " + name)
			}
		}
	}
	if len(e.Media) == 0 {
		return nil
	}
	events, _, err := s.view(id, "")
	if err != nil {
		return err
	}
	index, err := s.mediaIndex()
	if err != nil {
		return err
	}
	priorMedia := map[string]timeline.MediaReference{}
	for _, prior := range events {
		for _, ref := range prior.Media {
			priorMedia[ref.ID] = ref
		}
	}
	for _, m := range e.Media {
		if err := validateMediaBinding(m, id, index); err != nil {
			return err
		}
		prior, seen := priorMedia[m.ID]
		if seen && (prior.Path != m.Path || prior.MIME != m.MIME) {
			return errors.New("media ID conflicts with an existing reference")
		}
		// Attachment paths are resolved only within this story.
		if err := validateMediaPath(m); err != nil {
			return errors.New("media is outside timeline visibility")
		}
		if _, err := s.fs.ReadFile(timelinePath(id, m.Path)); err != nil {
			return fmt.Errorf("media %s: %w", m.ID, err)
		}
		priorMedia[m.ID] = m
	}
	return nil
}

// WriteCharacter and WriteWorldbook are Store-owned content mutations for the
// server to call in Phase 2; no offline management CLI bypass is exposed.
func (s *Store) WriteCharacter(id string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	card, err := character.Parse(content, id)
	if err != nil {
		return err
	}
	if card.ID != id {
		return errors.New("character ID mismatch")
	}
	docs, err := character.Scan(s.Root())
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if strings.EqualFold(doc.Metadata.ID, id) && doc.Metadata.ID != id {
			return errors.New("character name conflicts with existing folder")
		}
	}
	// Validate the entire candidate snapshot in memory before writing. Referenced
	// avatar must already be an actual safe asset, never a path supplied blindly.
	if card.Avatar != "" {
		asset, err := character.ReadAsset(s.Root(), id, card.Avatar)
		if err != nil {
			return err
		}
		switch asset.MIME {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return errors.New("avatar must be PNG/JPEG/GIF/WebP image")
		}
	}
	content, err = character.Encode(card)
	if err != nil {
		return err
	}
	return s.fs.AtomicWrite("characters/"+id+"/card.md", content)
}
func (s *Store) WriteWorldbook(category, slug string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(true); err != nil {
		return err
	}
	if !contentio.ValidName(category) || !contentio.ValidName(slug) {
		return errors.New("invalid worldbook category/slug")
	}
	entry, err := worldbook.Parse(content, slug)
	if err != nil {
		return err
	}
	content, err = worldbook.Encode(entry)
	if err != nil {
		return err
	}
	categories, err := s.fs.ReadDir("worldbook")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, dir := range categories {
		if strings.EqualFold(dir.Name(), category) && dir.Name() != category {
			return errors.New("category name conflicts with existing folder")
		}
	}
	index, err := worldbook.Scan(s.Root())
	if err != nil {
		return err
	}
	for _, doc := range index.Documents() {
		if strings.EqualFold(doc.Category, category) && doc.Category != category {
			return errors.New("category name conflicts with existing folder")
		}
		if doc.Category == category && strings.EqualFold(doc.Slug, slug) && doc.Slug != slug {
			return errors.New("filename conflicts with existing entry")
		}
	}
	return s.fs.AtomicWrite("worldbook/"+category+"/"+slug+".md", content)
}

func validateMediaPath(m timeline.MediaReference) error {
	if err := storage.ValidPath(m.Path); err != nil {
		return err
	}
	if !strings.HasPrefix(m.Path, "media/") {
		return errors.New("story media path must be relative to its story: media/<file>")
	}
	return nil
}

// validateMediaBinding verifies bytes and generated portable IDs, rather than
// trusting the MIME/path pair supplied by a client. Legacy manually named media
// can receive its first stable ID in an event, but cannot steal an existing ID.
func validateMediaBinding(ref timeline.MediaReference, story string, index map[string]media.Record) error {
	var record *media.Record
	if existing, ok := index[ref.ID]; ok {
		if existing.Timeline != story || existing.Path != ref.Path {
			return errors.New("media ID belongs to another asset")
		}
		record = &existing
	} else {
		// Only a legacy asset's first assignment needs a path lookup. Normal
		// historical and uploaded references use the constant-time ID lookup.
		for _, r := range index {
			if r.Timeline == story && r.Path == ref.Path {
				copy := r
				record = &copy
				break
			}
		}
	}
	if record == nil {
		return fmt.Errorf("media file not indexed: %s", ref.Path)
	}
	if ref.MIME != record.MIME {
		return errors.New("media MIME does not match actual bytes")
	}
	pieces := strings.SplitN(path.Base(ref.Path), "--", 2)
	if len(pieces) == 2 && strings.HasPrefix(pieces[0], "med_") && timeline.ValidID(pieces[0]) {
		if ref.ID != pieces[0] || ref.Name != record.Name {
			return errors.New("media reference differs from registered upload")
		}
	}
	return nil
}
