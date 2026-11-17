package project

import (
	"testing"

	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/timeline"
)

func TestUploadedReferenceMustMatchSniffedMIMEIDAndName(t *testing.T) {
	s, _, _ := initStore(t)
	record, err := s.UploadMedia(media.Upload{Timeline: "main", Name: "notes.txt", Data: []byte("plain text attachment")})
	if err != nil {
		t.Fatal(err)
	}
	if record.MIME != "text/plain" {
		t.Fatal("base MIME required for portable event", record.MIME)
	}
	for _, field := range []string{"mime", "id", "name", "path"} {
		t.Run(field, func(t *testing.T) {
			ref := record.MediaReference
			switch field {
			case "mime":
				ref.MIME = "image/png"
			case "id":
				ref.ID = "med_impostor"
			case "name":
				ref.Name = "other.txt"
			case "path":
				ref.Path = "rp.yaml"
			}
			if _, err := s.Append("main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "attachment", Media: []timeline.MediaReference{ref}}); err == nil {
				t.Fatal("forged media accepted", field)
			}
		})
	}
	if _, err := s.Append("main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "attachment", Media: []timeline.MediaReference{record.MediaReference}}); err != nil {
		t.Fatal("registered media rejected", err)
	}
	report, err := s.Validate()
	if err != nil || !report.Valid {
		t.Fatal(report, err)
	}
}
func TestLegacyMediaPathsAreRejected(t *testing.T) {
	s, _, _ := initStore(t)
	registered, err := s.UploadMedia(media.Upload{Timeline: "main", Name: "registered.txt", Data: []byte("registered")})
	if err != nil {
		t.Fatal(err)
	}
	fixtureFile(t, s.Root(), "chats/main/media/legacy.txt", []byte("legacy"))
	ref := timeline.MediaReference{ID: registered.ID, Path: "chats/main/media/legacy.txt", Name: "legacy.txt", MIME: "text/plain"}
	if _, err := s.Append("main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "legacy", Media: []timeline.MediaReference{ref}}); err == nil {
		t.Fatal("legacy path stole registered ID")
	}
	ref.ID = "med_legacy"
	if _, err := s.Append("main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "legacy", Media: []timeline.MediaReference{ref}}); err == nil {
		t.Fatal("legacy path accepted")
	}
	report, err := s.Validate()
	if err != nil || !report.Valid {
		t.Fatal(report, err)
	}
}
