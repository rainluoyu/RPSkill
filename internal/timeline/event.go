package timeline

import (
	"crypto/rand"
	"encoding/hex"

	"errors"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/luoyu/rpskill/internal/storage"
)

const Version = 1
const (
	Input  = "input"
	Output = "output"
)

type MediaReference struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	MIME string `json:"mime"`
	Name string `json:"name"`
}
type Event struct {
	Version    int              `json:"version"`
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Time       time.Time        `json:"time"`
	Timeline   string           `json:"timeline"`
	InputID    string           `json:"-"`
	Speaker    string           `json:"speaker,omitempty"`
	Text       string           `json:"text,omitempty"`
	Kind       string           `json:"kind,omitempty"`
	Characters []string         `json:"characters,omitempty"`
	Media      []MediaReference `json:"media,omitempty"`
}

func NewID(prefix string) (string, error) {
	if prefix == "" {
		prefix = "evt"
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}
func (e Event) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("unsupported event version %d", e.Version)
	}
	if !ValidID(e.ID) || !strings.HasPrefix(e.ID, "evt_") || !ValidChatID(e.Timeline) || e.Time.IsZero() {
		return errors.New("event requires valid evt_ id, timeline and time")
	}
	_, offset := e.Time.Zone()
	if offset != 0 {
		return errors.New("event time must be UTC")
	}
	if e.InputID != "" && (!ValidID(e.InputID) || !strings.HasPrefix(e.InputID, "evt_")) {
		return errors.New("invalid input address")
	}
	switch e.Type {
	case Input:
		if e.Kind != "" || len(e.Characters) != 0 {
			return errors.New("input contains output-only fields")
		}
		if e.InputID != e.ID {
			return errors.New("input address must equal its message address")
		}
		if e.Speaker != "chat" && e.Speaker != "director" && !OrdinaryCharacter(e.Speaker) {
			return errors.New("input speaker must be chat, director or an ordinary character")
		}
		if strings.TrimSpace(e.Text) == "" && len(e.Media) == 0 {
			return errors.New("input requires text or media")
		}
	case Output:
		if e.Speaker != "" {
			return errors.New("output must not contain speaker")
		}
		if e.InputID == "" || (e.Kind != "story" && e.Kind != "assistant") {
			return errors.New("output requires preceding input and story/assistant kind")
		}
		if strings.TrimSpace(e.Text) == "" && len(e.Media) == 0 {
			return errors.New("output requires text or media")
		}
	default:
		return fmt.Errorf("unsupported event type %q", e.Type)
	}
	seenCharacters := map[string]bool{}
	if len(e.Characters) > 100 {
		return errors.New("too many output characters")
	}
	for _, id := range e.Characters {
		if !OrdinaryCharacter(id) || seenCharacters[id] {
			return errors.New("output characters must be unique ordinary character names")
		}
		seenCharacters[id] = true
	}
	for _, m := range e.Media {
		if !ValidID(m.ID) || !strings.HasPrefix(m.ID, "med_") || m.Name == "" {
			return errors.New("invalid media reference")
		}
		if err := storage.ValidPath(m.Path); err != nil {
			return err
		}
		if !strings.HasPrefix(m.Path, "media/") {
			return errors.New("story attachment paths must be relative to the story media directory")
		}
		mediaType, params, err := mime.ParseMediaType(m.MIME)
		if err != nil || len(params) != 0 || mediaType != m.MIME {
			return errors.New("invalid media MIME")
		}
	}
	return nil
}
