package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/timeline"
)

func TestInputAmendDeleteRepliesAndState(t *testing.T) {
	s, _, _ := initStore(t)
	for _, id := range []string{"alice", "bob"} {
		if err := s.WriteCharacter(id, []byte("---\nenabled: true\n---\nCard")); err != nil {
			t.Fatal(err)
		}
	}
	first := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "alice", Text: "first"})
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "bob", Text: "original"})
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "one"})
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "two"})
	if err := s.UpdateCharacterState("main", "bob", "scratch", nil); err != nil {
		t.Fatal(err)
	}
	chat := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "chat", Text: "discussion"})
	if _, err := s.Append("main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "late"}); err == nil {
		t.Fatal("late output accepted")
	}
	e, err := s.AmendMessage("main", in.ID, ptr("edited"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Text != "edited" || e.Speaker != "bob" {
		t.Fatal(e)
	}
	if _, err = s.AmendMessage("main", in.ID, ptr(" ")); err == nil {
		t.Fatal("empty input accepted")
	}
	if _, err = s.AmendMessage("main", in.ID, nil); err != nil {
		t.Fatal(err)
	}
	h, err := s.History("main")
	if err != nil || len(h) != 2 || h[0].ID != first.ID || h[1].Text != chat.Text {
		t.Fatal(h, err)
	}
	state, err := s.State("main")
	if err != nil || state.ProtectedCharacter != "alice" || state.Characters["bob"] != "scratch" {
		t.Fatal(state, err)
	}
	report, err := s.Validate()
	if err != nil || !report.Valid {
		t.Fatal(report, err)
	}
}

func TestBranchChatCopiesPrefixMetadataMediaAndSurvivesSourceDeletion(t *testing.T) {
	s, base, rt := initStore(t)
	if err := s.WriteCharacter("alice", []byte("---\nenabled: true\n---\nCard")); err != nil {
		t.Fatal(err)
	}
	asset, err := s.UploadMedia(media.Upload{Timeline: "main", Name: "note.txt", Data: []byte("portable media")})
	if err != nil {
		t.Fatal(err)
	}
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "alice", Text: "prompt", Media: []timeline.MediaReference{asset.MediaReference}})
	out := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "original", Media: []timeline.MediaReference{asset.MediaReference}})
	if err := s.UpdateCharacterState("main", "alice", "10", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AmendMessage("main", out.ID, ptr("edited")); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "future"})
	if err := s.UpdateCharacterState("main", "alice", "99", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UploadMedia(media.Upload{Timeline: "main", Name: "unused.txt", Data: []byte("not referenced")}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.Root(), "chats/main/history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range []struct {
		id, target string
		length     int
	}{{in.ID, "at_input", 1}, {out.ID, "at_output", 2}} {
		_, err := s.BranchChat("main", point.id, point.target, "Branch")
		if err != nil {
			t.Fatal(err)
		}
		h, err := s.History(point.target)
		if err != nil || len(h) != point.length {
			t.Fatal(h, err)
		}
		if h[0].ID == in.ID || h[0].InputID != h[0].ID || h[0].Timeline != point.target {
			t.Fatal(h[0])
		}
		if len(h) > 1 && (h[1].ID == out.ID || h[1].InputID != h[0].ID || h[1].Text != "edited") {
			t.Fatal(h[1])
		}
		assets, err := s.ListMedia(point.target, "")
		if err != nil || len(assets) != 1 {
			t.Fatal(assets, err)
		}
		if assets[0].ID == asset.ID || !strings.HasPrefix(assets[0].Path, "media/") {
			t.Fatal(assets)
		}
		_, data, err := s.Media(assets[0].ID)
		if err != nil || string(data) != "portable media" {
			t.Fatal(err)
		}
		state, err := s.State(point.target)
		if err != nil || state.ProtectedCharacter != "alice" {
			t.Fatal(state, err)
		}
		if len(h) == 2 && (state.Characters["alice"] != "99") {
			t.Fatal(state)
		}
		if len(h) == 1 && state.Characters["alice"] != "99" {
			t.Fatal(state)
		}
	}
	after, _ := os.ReadFile(filepath.Join(s.Root(), "chats/main/history.jsonl"))
	if !bytes.Equal(before, after) {
		t.Fatal("source changed")
	}
	if err = s.DeleteTimeline("main"); err != nil {
		t.Fatal(err)
	}
	report, err := s.Validate()
	if err != nil || !report.Valid {
		t.Fatal(report, err)
	}
	zip, err := s.ExportArchive("chats", "at_output", "")
	if err != nil {
		t.Fatal(err)
	}
	dest, err := Init(filepath.Join(base, "portable"), InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	if _, err = dest.ImportArchive("chats", zip); err != nil {
		t.Fatal(err)
	}
	report, err = dest.Validate()
	if err != nil || !report.Valid {
		t.Fatal(report, err)
	}
	root := s.Root()
	s.Close()
	reopened, err := Open(root, OpenOptions{RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if h, err := reopened.History("at_output"); err != nil || len(h) != 2 {
		t.Fatal(h, err)
	}
}

func TestBranchRejectsBadCutoffDestinationAndMediaWithoutPartialWrites(t *testing.T) {
	s, _, _ := initStore(t)
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "prompt"})
	for _, tt := range []struct{ source, until, id string }{{"main", "evt_missing", "bad"}, {"main", in.ID, "../bad"}, {"other", in.ID, "bad"}, {"main", in.ID, "main"}} {
		if _, err := s.BranchChat(tt.source, tt.until, tt.id, "Branch"); err == nil {
			t.Fatal("bad branch accepted", tt)
		}
	}
	report, err := s.Validate()
	if err != nil || !report.Valid || report.Timelines != 1 {
		t.Fatal(report, err)
	}
	// A forged name must never be joined into a destination path.
	asset, err := s.UploadMedia(media.Upload{Timeline: "main", Name: "note.txt", Data: []byte("note")})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := s.History("main")
	asset.Name = "../../main/history.jsonl"
	h[0].Media = []timeline.MediaReference{asset.MediaReference}
	if err = s.writeHistory("main", h); err != nil {
		t.Fatal(err)
	}
	before, _ := s.fs.ReadFile("chats/main/history.jsonl")
	if _, err = s.BranchChat("main", in.ID, "unsafe", "Unsafe"); err == nil {
		t.Fatal("unsafe media name accepted")
	}
	after, _ := s.fs.ReadFile("chats/main/history.jsonl")
	if !bytes.Equal(before, after) {
		t.Fatal("source was overwritten")
	}
	if _, err = s.fs.ReadDir("chats/unsafe"); !os.IsNotExist(err) {
		t.Fatal("partial branch", err)
	}
}

func TestBranchPartialSegmentsAndSymlinkRejection(t *testing.T) {
	s, base, _ := initStore(t)
	in := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "prompt"})
	one := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "first part"})
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: in.ID, Kind: "story", Text: "final part"})
	if _, err := s.BranchChat("main", one.ID, "partial", "Partial"); err != nil {
		t.Fatal(err)
	}
	h, err := s.History("partial")
	if err != nil || len(h) != 2 {
		t.Fatal(h, err)
	}
	mustAppend(t, s, "partial", timeline.Event{Type: timeline.Output, InputID: h[0].ID, Kind: "story", Text: "new ending"})
	if source, err := s.History("main"); err != nil || source[2].Text != "final part" {
		t.Fatal(source, err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.Root(), "chats/linked")); err != nil {
		t.Skip("symlink creation unavailable", err)
	}
	if _, err := s.BranchChat("main", one.ID, "linked", "Linked"); err == nil {
		t.Fatal("symlink branch destination accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside changed", entries, err)
	}
}

func TestLastSpeakerAndProtectionRebuiltFromHistory(t *testing.T) {
	s, _, rt := initStore(t)
	if err := s.WriteCharacter("alice", []byte("---\nenabled: true\n---\nCard")); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "alice", Text: "start"})
	director := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "direct"})
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "chat", Text: "discuss"})
	root := s.Root()
	s.Close()
	s, err := Open(root, OpenOptions{RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state, err := s.State("main")
	if err != nil || state.ProtectedCharacter != "" || state.LastSpeaker != "chat" {
		t.Fatal(state, err)
	}
	if _, err := s.TruncateHistory("main", director.ID); err != nil {
		t.Fatal(err)
	}
	state, err = s.State("main")
	if err != nil || state.ProtectedCharacter != "alice" || state.LastSpeaker != "alice" {
		t.Fatal(state, err)
	}
}
