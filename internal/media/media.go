// Package media validates portable assets and bounded ZIP containers. It does
// not write Workspace files; publication belongs exclusively to project.Store.
package media

import (
	"errors"
	"mime"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/luoyu/rpskill/internal/character/contentio"
	"github.com/luoyu/rpskill/internal/timeline"
)

const MaxUpload = 32 << 20
const MaxArchive = 64 << 20
const MaxExpanded = 128 << 20
const MaxFiles = 4096

var ErrTooLarge = errors.New("media or archive exceeds size limit")
var ErrConflict = errors.New("media/archive conflict")

type Upload struct {
	ID         string `json:"-"` // trusted server-generated stable ID for receipt recovery
	Timeline   string `json:"timeline,omitempty"`
	Character  string `json:"character,omitempty"`
	Expression string `json:"expression,omitempty"`
	Avatar     bool   `json:"avatar,omitempty"`
	Name       string `json:"name"`
	Data       []byte `json:"-"`
}
type Record struct {
	timeline.MediaReference
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Timeline   string `json:"timeline,omitempty"`
	Character  string `json:"character,omitempty"`
	Expression string `json:"expression,omitempty"`
	Preview    bool   `json:"preview"`
}

// FilePath resolves a server-owned record within its owning story/character.
// Public and persisted Path values remain relative to that owner.
func (r Record) FilePath() string {
	if r.Timeline != "" {
		return "chats/" + r.Timeline + "/" + r.Path
	}
	return "characters/" + r.Character + "/" + r.Path
}

func Image(m string) bool {
	return m == "image/png" || m == "image/jpeg" || m == "image/gif" || m == "image/webp"
}
func Preview(m string) bool {
	if Image(m) {
		return true
	}
	switch m {
	case "audio/mpeg", "audio/wave", "audio/wav", "audio/ogg", "video/ogg", "video/mp4", "video/webm", "audio/flac", "audio/aiff":
		return true
	}
	return false
}
func Detect(data []byte) string {
	m := http.DetectContentType(data)
	if base, _, err := mime.ParseMediaType(m); err == nil {
		m = base
	}
	// Ogg is a container; do not invent an audio/video codec from its extension.
	if m == "application/ogg" {
		return "audio/ogg"
	}
	return m
}
func Name(name string) error {
	if name == "" || len(name) > 180 || !utf8.ValidString(name) || path.Base(name) != name {
		return errors.New("invalid bounded asset filename")
	}
	return contentio.Relative(name)
}
func (u Upload) Validate() error {
	if (u.Timeline == "") == (u.Character == "") {
		return errors.New("exactly one timeline or character required")
	}
	if u.Timeline != "" && !timeline.ValidChatID(u.Timeline) {
		return errors.New("invalid timeline")
	}
	if u.Character != "" && !timeline.OrdinaryCharacter(u.Character) {
		return errors.New("invalid character")
	}
	if u.Expression != "" && (!timeline.ValidID(u.Expression) || u.Character == "") {
		return errors.New("expression requires character and valid ID")
	}
	if u.Avatar && (u.Character == "" || u.Expression != "") {
		return errors.New("avatar requires character and cannot also be expression")
	}
	if err := Name(u.Name); err != nil {
		return err
	}
	if len(u.Name)+len(u.Expression)+40 > 255 {
		return errors.New("asset filename plus identity exceeds portable limit")
	}
	if len(u.Data) > MaxUpload {
		return ErrTooLarge
	}
	if len(u.Data) == 0 {
		return errors.New("empty media")
	}
	if (u.Avatar || u.Expression != "") && !Image(Detect(u.Data)) {
		return errors.New("avatar/expression requires PNG, JPEG, GIF or WebP")
	}
	if u.ID != "" && (!timeline.ValidID(u.ID) || !strings.HasPrefix(u.ID, "med_")) {
		return errors.New("invalid media ID")
	}
	return nil
}
