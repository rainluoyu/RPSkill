package project

import (
	"bytes"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/timeline"
)

func TestOwnerRelativeMediaSurvivesRenameAndProjectCopy(t *testing.T) {
	s, _, _ := initStore(t)
	if err := s.WriteCharacter("角色乙", []byte("---\nenabled: true\n---\nCard")); err != nil {
		t.Fatal(err)
	}
	avatar, err := s.UploadMedia(media.Upload{Character: "角色乙", Avatar: true, Name: "avatar.png", Data: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(avatar.Path, "/") {
		t.Fatal(avatar.Path)
	}
	story, err := s.UploadMedia(media.Upload{Timeline: "main", Name: "scene.png", Data: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(story.Path, "media/") || strings.Contains(story.Path, "chats/") {
		t.Fatal(story.Path)
	}
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "scene", Media: []timeline.MediaReference{story.MediaReference}})
	before, err := s.fs.ReadFile("chats/main/history.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTimelineMetadata("main", "旅途", nil); err != nil {
		t.Fatal(err)
	}
	after, err := s.fs.ReadFile("chats/旅途/history.jsonl")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rename rewrote history", err)
	}
	if err := s.RenameCharacter("角色乙", "角色丙", []byte("---\nenabled: true\navatar: "+avatar.Path+"\n---\nCard")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{story.ID, avatar.ID} {
		record, data, err := s.Media(id)
		if err != nil || len(data) == 0 || strings.HasPrefix(record.Path, "chats/") || strings.HasPrefix(record.Path, "characters/") {
			t.Fatal(record, err)
		}
	}
	if _, err := s.CreateTimeline(TimelineOptions{ID: "其他"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append("其他", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "wrong scope", Media: []timeline.MediaReference{story.MediaReference}}); err == nil {
		t.Fatal("cross-story ID allowed")
	}
	bad := story.MediaReference
	bad.Path = "../旅途/" + bad.Path
	if _, err := s.Append("旅途", timeline.Event{Type: timeline.Output, Kind: "story", InputID: in.ID, Text: "bad", Media: []timeline.MediaReference{bad}}); err == nil {
		t.Fatal("traversal allowed")
	}
	report, err := s.Validate()
	if err != nil || !report.Valid {
		t.Fatal(report, err)
	}
	files, err := s.archiveFiles()
	if err != nil {
		t.Fatal(err)
	}
	cleanup, copied, err := candidate(files)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	restored, err := Open(copied.Root(), OpenOptions{RuntimeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	for _, id := range []string{story.ID, avatar.ID} {
		_, data, err := restored.Media(id)
		if err != nil || len(data) == 0 {
			t.Fatal("copied project missing media", err)
		}
	}
}
