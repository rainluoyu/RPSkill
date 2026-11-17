package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyu/rpskill/internal/project"
	"github.com/luoyu/rpskill/internal/timeline"
)

func p1Store(t *testing.T) (*project.Store, string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "中文 RP")
	rt := t.TempDir()
	t.Setenv("RPS_RUNTIME_DIR", rt)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	s, err := project.Init(dir, project.InitOptions{Name: "Independent validation", RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	p1Write(t, dir, "characters/alice/card.md", p1Card("alice"))
	p1Write(t, dir, "characters/bob/card.md", p1Card("bob"))
	s, err = project.Open(dir, project.OpenOptions{RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir, rt
}
func p1Append(t *testing.T, s *project.Store, id string, e timeline.Event) timeline.Event {
	t.Helper()
	got, err := s.Append(id, e)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func p1Input(speaker string) timeline.Event {
	return timeline.Event{Type: timeline.Input, Speaker: speaker, Text: "Original user input"}
}
func p1State(t *testing.T, s *project.Store, id string) timeline.State {
	t.Helper()
	v, err := s.State(id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func p1History(t *testing.T, s *project.Store, id string) []timeline.Event {
	t.Helper()
	v, err := s.History(id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPhase1StoreOutputKindAndStoryScope(t *testing.T) {
	s, _, _ := p1Store(t)
	director := p1Append(t, s, "main", p1Input("director"))
	emit := timeline.Event{Type: timeline.Output, InputID: director.ID, Kind: "story", Text: "Formal story output"}
	p1Append(t, s, "main", emit)
	// Outputs use the preceding input; the Store has no execution lifecycle.
	var err error
	chat := p1Append(t, s, "main", p1Input("chat"))
	bad := emit
	bad.InputID = chat.ID
	if _, err := s.Append("main", bad); err == nil {
		t.Fatal("Chat story accepted")
	}
	bad.Kind = "assistant"
	p1Append(t, s, "main", bad)
	_, err = s.CreateTimeline(project.TimelineOptions{ID: "other", Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := emit
	if _, err := s.Append("other", foreign); err == nil {
		t.Fatal("foreign request accepted")
	}
	own := p1Append(t, s, "other", p1Input("director"))
	emit.InputID = own.ID
	p1Append(t, s, "other", emit)
	if len(p1History(t, s, "main")) != 4 || len(p1History(t, s, "other")) != 2 {
		t.Fatal("failed/duplicate output changed event count")
	}
}
func TestPhase1StoreReadOnlyAndWriterLock(t *testing.T) {
	s, dir, rt := p1Store(t)
	if second, err := project.Open(dir, project.OpenOptions{RuntimeDir: rt}); err == nil {
		second.Close()
		t.Fatal("second writer opened")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := project.Open(dir, project.OpenOptions{RuntimeDir: rt, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err := ro.Append("main", p1Input("director")); !errors.Is(err, project.ErrReadOnly) {
		t.Fatalf("readonly append not rejected: %v", err)
	}
	if _, err := ro.CreateTimeline(project.TimelineOptions{ID: "bad", Name: "bad"}); !errors.Is(err, project.ErrReadOnly) {
		t.Fatalf("readonly creation not rejected: %v", err)
	}
}
func TestPhase1StoreConcurrentAppend(t *testing.T) {
	s, _, _ := p1Store(t)
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Append("main", p1Input("director"))
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	h := p1History(t, s, "main")
	if len(h) != 40 {
		t.Fatalf("lost or duplicated append: %d", len(h))
	}
	if err := timeline.ValidateHistory(h); err != nil {
		t.Fatal(err)
	}
}
func TestPhase1StoreExplicitTailRepair(t *testing.T) {
	for name, tail := range map[string][]byte{"JSON_cut": []byte(`{"version":1,"id":`), "UTF8_cut": {0x7b, 0x22, 0x74, 0x22, 0x3a, 0x22, 0xe4, 0xb8}} {
		t.Run(name, func(t *testing.T) {
			s, dir, rt := p1Store(t)
			saved := p1Append(t, s, "main", p1Input("director"))
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "chats/main/history.jsonl")
			prefix, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, append(append([]byte{}, prefix...), tail...), 0644); err != nil {
				t.Fatal(err)
			}
			if bad, err := project.Open(dir, project.OpenOptions{RuntimeDir: rt}); err == nil {
				bad.Close()
				t.Fatal("normal Open silently accepted unterminated tail")
			}
			before, err := os.ReadFile(file)
			if err != nil || len(before) != len(prefix)+len(tail) {
				t.Fatal("normal open changed corrupt data")
			}
			repaired, err := project.Open(dir, project.OpenOptions{RuntimeDir: rt, RepairTail: true})
			if err != nil {
				t.Fatal(err)
			}
			defer repaired.Close()
			h := p1History(t, repaired, "main")
			if len(h) != 1 || h[0].ID != saved.ID {
				t.Fatal("repair lost committed prefix")
			}
			records := repaired.Recoveries()
			if len(records) == 0 {
				t.Fatal("repair not reported")
			}
			found := false
			for _, r := range records {
				if r.Bytes == len(tail) {
					q := r.Quarantine
					if !filepath.IsAbs(q) {
						q = filepath.Join(rt, q)
					}
					if strings.HasPrefix(q, dir+string(os.PathSeparator)) {
						t.Fatal("quarantine within Workspace")
					}
					data, e := os.ReadFile(q)
					if e != nil {
						t.Fatalf("quarantine not accessible: %s: %v", q, e)
					}
					if bytes.Equal(data, tail) {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("exact tail bytes not preserved in quarantine")
			}
			p1Append(t, repaired, "main", p1Input("chat"))
			if len(p1History(t, repaired, "main")) != 2 {
				t.Fatal("append after repair failed")
			}
		})
	}
}
func TestPhase1StoreRepairRefusesInteriorAndSemanticCorruption(t *testing.T) {
	for name, suffix := range map[string][]byte{"middle_bad_JSON": []byte("BROKEN\n{}\n"), "complete_invalid_schema": []byte("{\"version\":1}\n")} {
		t.Run(name, func(t *testing.T) {
			s, dir, rt := p1Store(t)
			p1Append(t, s, "main", p1Input("director"))
			s.Close()
			file := filepath.Join(dir, "chats/main/history.jsonl")
			prefix, _ := os.ReadFile(file)
			before := append(prefix, suffix...)
			if err := os.WriteFile(file, before, 0644); err != nil {
				t.Fatal(err)
			}
			repaired, err := project.Open(dir, project.OpenOptions{RuntimeDir: rt, RepairTail: true})
			if err == nil {
				repaired.Close()
				t.Fatal("corruption silently repaired")
			}
			after, _ := os.ReadFile(file)
			if !bytes.Equal(after, before) {
				t.Fatal("failed repair changed history")
			}
		})
	}
}

func TestPhase1StoreCharacterWriteCannotInvalidateAvatar(t *testing.T) {
	s, dir, _ := p1Store(t)
	p1Write(t, dir, "characters/alice/notes.txt", []byte("not an image"))
	before, err := os.ReadFile(filepath.Join(dir, "characters/alice/card.md"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := bytes.Replace(before, []byte("enabled: true"), []byte("enabled: true\navatar: notes.txt"), 1)
	if err := s.WriteCharacter("alice", invalid); err == nil {
		t.Error("WriteCharacter accepted non-image avatar and invalidated Workspace")
	}
	after, err := os.ReadFile(filepath.Join(dir, "characters/alice/card.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("rejected avatar candidate changed formal card")
	}
}
func TestPhase1StoreRepairDoesNotModifyInvalidCompletePrefix(t *testing.T) {
	s, dir, rt := p1Store(t)
	input := p1Append(t, s, "main", p1Input("chat"))
	s.Close()
	invalid := timeline.Event{Version: 1, ID: "evt_bad", Type: timeline.Output, Time: input.Time, Timeline: "main", InputID: input.ID, Kind: "story", Text: "wrong kind"}
	line, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "chats/main/history.jsonl")
	prefix, _ := os.ReadFile(file)
	before := append(prefix, append(line, '\n')...)
	before = append(before, []byte(`{"id":`)...)
	if err := os.WriteFile(file, before, 0644); err != nil {
		t.Fatal(err)
	}
	repaired, err := project.Open(dir, project.OpenOptions{RuntimeDir: rt, RepairTail: true})
	if err == nil {
		repaired.Close()
		t.Fatal("invalid history accepted")
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) {
		t.Fatal("failed recovery truncated tail before detecting invalid complete history")
	}
}

func TestPhase1LargeHistoryStillAllowsSettingsUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("large JSONL regression omitted by -short")
	}
	s, dir, rt := p1Store(t)
	sample := p1Append(t, s, "main", p1Input("director"))
	s.Close()
	file, err := os.OpenFile(filepath.Join(dir, "chats/main/history.jsonl"), os.O_TRUNC|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		e := sample
		e.ID = fmt.Sprintf("evt_large_%d", i)
		e.InputID = e.ID
		e.Text = strings.Repeat("x", 4_000_000)
		raw, err := timeline.EncodeMessage(e)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := project.Open(dir, project.OpenOptions{RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.SetTimelineSettings("main", timeline.SettingsPatch{Polishing: &timeline.Polishing{Level: "off"}}); err != nil {
		t.Fatalf("valid 36MB history cannot update settings: %v", err)
	}
	if p1State(t, reopened, "main").Settings.Polishing.Level != "off" {
		t.Fatal("large history settings not updated")
	}
}

func TestPhase1StoreWriterLockIndependentOfRuntimeOverride(t *testing.T) {
	s, dir, _ := p1Store(t)
	defer s.Close()
	second, err := project.Open(dir, project.OpenOptions{RuntimeDir: t.TempDir()})
	if err == nil {
		second.Close()
		t.Fatal("second writer bypassed Workspace exclusivity by changing RuntimeDir")
	}
}

func TestPhase1StoreFailedInputDoesNotChangeLock(t *testing.T) {
	s, _, _ := p1Store(t)
	p1Append(t, s, "main", p1Input("alice"))
	if err := s.SetTimelineSettings("main", timeline.SettingsPatch{Characters: map[string]bool{"bob": false}}); err != nil {
		t.Fatal(err)
	}
	before := p1History(t, s, "main")
	for _, speaker := range []string{"bob", "absent", "assistant", "story"} {
		if _, err := s.Append("main", p1Input(speaker)); err == nil {
			t.Errorf("invalid/disabled input %s accepted", speaker)
		}
	}
	if got := p1State(t, s, "main"); got.ProtectedCharacter != "alice" {
		t.Fatal("rejected input changed lock")
	}
	if !reflect.DeepEqual(before, p1History(t, s, "main")) {
		t.Fatal("rejected input changed history")
	}
}

func TestPhase1RequestWorldbookSnapshotExcludesCurrentAndLaterEvents(t *testing.T) {
	s, _, _ := p1Store(t)
	if err := s.WriteWorldbook("magic", "system", p1Book("magic", "keyword", 50, "[MAGIC]")); err != nil {
		t.Fatal(err)
	}
	one := 1
	if err := s.SetTimelineSettings("main", timeline.SettingsPatch{Worldbook: &timeline.WorldbookPatch{ScanRecent: &one}}); err != nil {
		t.Fatal(err)
	}
	earlier := p1Input("director")
	earlier.Text = "MAGIC"
	p1Append(t, s, "main", earlier)
	p1Append(t, s, "main", p1Input("chat"))
	target := p1Append(t, s, "main", p1Input("director"))
	if err := s.SetTimelineSettings("main", timeline.SettingsPatch{Worldbook: &timeline.WorldbookPatch{Categories: map[string]bool{"magic": false}}}); err != nil {
		t.Fatal(err)
	}
	p1Append(t, s, "main", p1Input("director"))
	refs, err := s.ActivatedWorldbookAtInput("main", target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("current project category disable was ignored: %+v", refs)
	}
	if _, err := s.ActivatedWorldbookAtInput("main", ""); err == nil {
		t.Fatal("empty request accepted")
	}
	if _, err := s.CreateTimeline(project.TimelineOptions{ID: "branch", Name: "branch"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActivatedWorldbookAtInput("branch", target.ID); err == nil {
		t.Fatal("foreign/inherited input used as local callback request")
	}
}
