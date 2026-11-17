package project

import (
	"crypto/sha256"

	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luoyu/rpskill/internal/timeline"
	"github.com/luoyu/rpskill/internal/worldbook"
)

func initStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("APPDATA", filepath.Join(base, "config"))
	rt := filepath.Join(base, "runtime")
	s, err := Init(filepath.Join(base, "小说 项目"), InitOptions{Name: "港湾学院", RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, base, rt
}
func mustAppend(t *testing.T, s *Store, id string, e timeline.Event) timeline.Event {
	t.Helper()
	actual, err := s.Append(id, e)
	if err != nil {
		t.Fatal(err)
	}
	return actual
}
func mustSettings(t *testing.T, s *Store, id string, patch timeline.SettingsPatch) {
	t.Helper()
	if err := s.SetTimelineSettings(id, patch); err != nil {
		t.Fatal(err)
	}
}
func checkRefs(t *testing.T, refs []worldbook.Reference, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, ref := range refs {
		got = append(got, ref.ID)
	}
	if !reflect.DeepEqual(got, append([]string{}, want...)) {
		t.Fatalf("active: got %v want %v", got, want)
	}
}

func TestWorldbookRequestWindowExcludesCurrentAndFutureEvents(t *testing.T) {
	s, _, _ := initStore(t)
	entry := []byte("---\nenabled: true\nactivation: keyword\nkeys: [暴雨]\npriority: 1\n---\nWeather.\n")
	if err := s.WriteWorldbook("world", "weather", entry); err != nil {
		t.Fatal(err)
	}
	n := 1
	mustSettings(t, s, "main", timeline.SettingsPatch{Worldbook: &timeline.WorldbookPatch{ScanRecent: &n}})
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: "暴雨将至"})
	// Chat discussion is not eligible for the recent story budget.
	mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "chat", Text: "讨论"})
	input := worldbook.Message{Type: timeline.Input, Speaker: "director", Text: "继续"}
	refs, err := s.ActivatedWorldbook("main", input)
	checkRefs(t, refs, err, "weather")
	current := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: input.Text})
	// Project settings are shared and current; no configuration history is kept.
	mustSettings(t, s, "main", timeline.SettingsPatch{Worldbook: &timeline.WorldbookPatch{Categories: map[string]bool{"world": false}}})
	refs, err = s.ActivatedWorldbookAtInput("main", current.ID)
	checkRefs(t, refs, err)
	mustSettings(t, s, "main", timeline.SettingsPatch{Worldbook: &timeline.WorldbookPatch{Categories: map[string]bool{"world": true}}})
	duplicateText := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "director", Text: input.Text})
	refs, err = s.ActivatedWorldbookAtInput("main", duplicateText.ID)
	checkRefs(t, refs, err) // Previous identical input really consumes the one-message window.
	chat := mustAppend(t, s, "main", timeline.Event{Type: timeline.Input, Speaker: "chat", Text: "暴雨"})
	refs, err = s.ActivatedWorldbookAtInput("main", chat.ID)
	checkRefs(t, refs, err, "weather")
	if _, err := s.ActivatedWorldbookAtInput("main", ""); err == nil {
		t.Fatal("empty request accepted")
	}
	output := mustAppend(t, s, "main", timeline.Event{Type: timeline.Output, InputID: chat.ID, Kind: "assistant", Text: "讨论"})
	if _, err := s.ActivatedWorldbookAtInput("main", output.ID); err == nil {
		t.Fatal("output used as input")
	}
}
func ptr[T any](v T) *T { return &v }

func fixtureFile(t *testing.T, root, name string, data []byte) {
	t.Helper()
	name = filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func snapshotFiles(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = sha256.Sum256(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInitRejectsInternalRuntimeBeforePublishing(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "project")
	if s, err := Init(target, InitOptions{RuntimeDir: filepath.Join(target, "cache")}); err == nil {
		s.Close()
		t.Fatal("internal runtime accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed init published workspace: %v", err)
	}
}
