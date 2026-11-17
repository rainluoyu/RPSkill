package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
)

func mediaStore(t *testing.T) (*Store, string) {
	t.Helper()
	base := t.TempDir()
	s, err := Init(filepath.Join(base, "RP"), InitOptions{RuntimeDir: filepath.Join(base, "runtime")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, base
}
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func mediaInput(t *testing.T, s *Store, line, text string, refs ...timeline.MediaReference) timeline.Event {
	t.Helper()
	id, err := timeline.NewID("evt")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Append(line, timeline.Event{Version: 1, ID: id, InputID: id, Time: time.Now().UTC(), Timeline: line, Type: timeline.Input, Speaker: "director", Text: text, Media: refs})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAvatarExpressionAtomicPublication(t *testing.T) {
	s, _ := mediaStore(t)
	if err := s.WriteCharacter("alice", []byte("---\nenabled: true\n---\nBase body\n")); err != nil {
		t.Fatal(err)
	}
	avatar, err := s.UploadMedia(media.Upload{Character: "alice", Avatar: true, Name: "avatar.png", Data: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	cards, err := character.Scan(s.Root())
	if err != nil || len(cards) != 1 || cards[0].Metadata.Avatar != filepath.Base(avatar.Path) || cards[0].Metadata.Body != "Base body\n" {
		t.Fatalf("avatar card %+v %v", cards, err)
	}
	if err := s.DeleteMedia(avatar.ID); err == nil {
		t.Fatal("deleted active avatar")
	}
	exp, err := s.UploadMedia(media.Upload{Character: "alice", Expression: "happy", Name: "happy.png", Data: pngBytes(t)})
	if err != nil {
		t.Fatal(err)
	}
	if exp.Expression != "happy" {
		t.Fatal("expression lost")
	}
	if err := s.DeleteMedia(exp.ID); err != nil {
		t.Fatal(err)
	}
}

func TestProjectCharacterWorldbookArchivesAndAtomicConflict(t *testing.T) {
	source, _ := mediaStore(t)
	if err := source.WriteCharacter("alice", []byte("---\nenabled: true\n---\nBody\n")); err != nil {
		t.Fatal(err)
	}
	if err := source.WriteWorldbook("world", "history", []byte("---\nenabled: true\nactivation: always\nkeys: []\npriority: 1\n---\nBody\n")); err != nil {
		t.Fatal(err)
	}
	whole, err := source.ExportArchive("project", "", "")
	if err != nil {
		t.Fatal(err)
	}
	target, _ := mediaStore(t)
	if _, err := target.ImportArchive("project", whole); err != nil {
		t.Fatal(err)
	}
	if _, err := target.ImportArchive("project", whole); err == nil {
		t.Fatal("nonpristine project replaced")
	}
	for _, kind := range []string{"characters", "worldbook"} {
		raw, err := source.ExportArchive(kind, "alice", "")
		if err != nil {
			t.Fatal(err)
		}
		receiver, _ := mediaStore(t)
		if _, err := receiver.ImportArchive(kind, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := receiver.ImportArchive(kind, raw); err != nil {
			t.Fatalf("identical base should be reusable: %v", err)
		}
		files, _ := media.DecodeZIP(raw)
		for name := range files {
			files[name] = append(files[name], []byte("changed")...)
		}
		altered, _ := media.EncodeZIP(files)
		before, _ := receiver.ExportArchive("project", "", "")
		if _, err := receiver.ImportArchive(kind, altered); err == nil {
			t.Fatal("conflicting base overwritten")
		}
		after, _ := receiver.ExportArchive("project", "", "")
		if !bytes.Equal(before, after) {
			t.Fatal("failed import changed project")
		}
	}
	malformed, _ := media.EncodeZIP(map[string][]byte{"worldbook/nested/deep/entry.md": []byte("invalid")})
	before, _ := target.ExportArchive("project", "", "")
	if _, err := target.ImportArchive("worldbook", malformed); err == nil {
		t.Fatal("bad hierarchy accepted")
	}
	after, _ := target.ExportArchive("project", "", "")
	if storage.SHA256(before) != storage.SHA256(after) {
		t.Fatal("failed import was not atomic")
	}
}

func TestMediaLargeTransactionAndUnreferencedMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("32MiB transaction")
	}
	s, _ := mediaStore(t)
	payload := bytes.Repeat([]byte{0x42}, media.MaxUpload)
	record, err := s.UploadMedia(media.Upload{Timeline: "main", Name: "large.bin", Data: payload})
	if err != nil {
		t.Fatal(err)
	}
	if record.Size != media.MaxUpload {
		t.Fatal("size lost")
	}
	raw, err := s.ExportArchive("chats", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	target, _ := mediaStore(t)
	if _, err := target.ImportArchive("project", raw); err != nil {
		t.Fatal(err)
	}
	got, data, err := target.Media(record.ID)
	if err != nil || got.Size != record.Size || !bytes.Equal(data, payload) {
		t.Fatalf("unreferenced migration: %v", err)
	}
}

func TestArchiveValidatesReconstructedIndexAndMissingAssets(t *testing.T) {
	s, _ := mediaStore(t)
	files := map[string][]byte{"characters/alice/card.md": []byte("---\nenabled: true\navatar: missing.png\n---\n"), "characters/alice/med_0123456789abcdef0123456789abcdef--a.png": pngBytes(t)}
	raw, err := media.EncodeZIP(files)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportArchive("characters", raw); err == nil {
		t.Fatal("missing avatar accepted")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "characters/alice")); !os.IsNotExist(err) {
		t.Fatal("bad import left partial files")
	}
}

func TestArchivePartialImportJournalRecovery(t *testing.T) {
	s, base := mediaStore(t)
	card := []byte("---\nenabled: true\navatar: avatar.png\n---\nRecovered\n")
	payloads := [][]byte{card, pngBytes(t)}
	paths := []string{"characters/alice/card.md", "characters/alice/avatar.png"}
	stage := "store-stage-0123456789abcdef0123456789abcdef"
	journal := storage.Journal{Version: 2, Stage: stage, Project: s.Root()}
	// Reproduce the actual v2 durable transaction format at the crash point after
	// one file was published. Reopen executes production recovery, not a mock.
	s.mu.Lock()
	for i, data := range payloads {
		name := fmt.Sprintf("%s/%04d.bin", stage, i)
		if err := s.runtime.AtomicWrite(name, data); err != nil {
			s.mu.Unlock()
			t.Fatal(err)
		}
		journal.Writes = append(journal.Writes, storage.Write{Path: paths[i], Staged: name, DataSize: len(data), DataHash: storage.SHA256(data)})
	}
	raw, _ := json.Marshal(journal)
	if err := s.runtime.AtomicWrite("store-transaction.json", raw); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	if err := s.fs.AtomicWrite(paths[0], payloads[0]); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	root := s.Root()
	s.Close()
	reopened, err := Open(root, OpenOptions{RuntimeDir: filepath.Join(base, "runtime")})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cards, err := reopened.Characters()
	if err != nil || len(cards) != 1 || cards[0].Metadata.Body != "Recovered\n" {
		t.Fatalf("partial import recovery %+v %v", cards, err)
	}
	assets, err := reopened.ListMedia("", "alice")
	if err != nil || len(assets) != 1 {
		t.Fatalf("recovered asset %+v %v", assets, err)
	}
	if pending, err := storage.Pending(reopened.runtime); err != nil || pending {
		t.Fatalf("journal not finalized: %v %v", pending, err)
	}
}
