package skill_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyu/rpskill/internal/project"
	"github.com/luoyu/rpskill/internal/timeline"
)

type narrativeCase struct {
	ID, Name, Speaker, Perspective, Polishing, Prompt, Kind, Protected string
	PriorSpeaker                                                       string `json:"prior_speaker"`
	SelectedOnly                                                       string `json:"selected_only"`
	Tags, Review                                                       []string
	ChangePolishing                                                    string `json:"change_polishing"`
	Document                                                           *string
}

// Fixed inputs validate deterministic data rules, NOT model prose or behavior.
// All output below is explicitly marked fixture text, not a Harness response.
func TestNarrativeFixedInputs(t *testing.T) {
	data, err := os.ReadFile("narrative_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Version  int
		Evidence string
		Cases    []narrativeCase
	}
	if err := json.Unmarshal(data, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Version != 1 || len(suite.Cases) != 18 || suite.Evidence == "" {
		t.Fatal("expected 18 labelled fixed cases")
	}
	seen, tags := map[string]bool{}, map[string]bool{}
	for i, c := range suite.Cases {
		if c.ID != fmt.Sprintf("N%02d", i+1) || seen[c.ID] || len(c.Review) == 0 || c.Prompt == "" {
			t.Fatalf("invalid case: %+v", c)
		}
		seen[c.ID] = true
		for _, tag := range c.Tags {
			tags[tag] = true
		}
		t.Run(c.ID+"_"+c.Name, func(t *testing.T) {
			root := t.TempDir()
			s, err := project.Init(filepath.Join(root, "workspace"), project.InitOptions{Name: "fixed-input evidence only", RuntimeDir: filepath.Join(root, "runtime")})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			cards := map[string][]byte{}
			for _, id := range []string{"alice", "bob"} {
				cards[id] = []byte(fmt.Sprintf("---\nenabled: true\n---\nTest-only character background.\n"))
				if err := s.WriteCharacter(id, cards[id]); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetTimelineSettings("main", timeline.SettingsPatch{Narrative: &timeline.Narrative{Perspective: c.Perspective}, Polishing: &timeline.Polishing{Level: c.Polishing}}); err != nil {
				t.Fatal(err)
			}
			appendEvent := func(e timeline.Event) timeline.Event {
				t.Helper()
				got, err := s.Append("main", e)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			appendEvent(timeline.Event{Type: timeline.Input, Speaker: c.PriorSpeaker, Text: "Setup: the last ordinary-character input"})
			before, err := s.State("main")
			if err != nil {
				t.Fatal(err)
			}
			// UI-only selection is deliberately not a persisted input.
			if c.SelectedOnly != "" && before.ProtectedCharacter != c.PriorSpeaker {
				t.Fatal("selection changed lock")
			}
			input := appendEvent(timeline.Event{Type: timeline.Input, Speaker: c.Speaker, Text: c.Prompt})
			state, err := s.State("main")
			if err != nil {
				t.Fatal(err)
			}
			if state.ProtectedCharacter != c.Protected || state.Settings.Narrative.Perspective != c.Perspective || state.Settings.Polishing.Level != c.Polishing {
				t.Fatalf("unexpected state: %+v", state)
			}
			wrong := "assistant"
			if c.Kind == wrong {
				wrong = "story"
			}
			if _, err := s.Append("main", timeline.Event{Type: timeline.Output, InputID: input.ID, Kind: wrong, Text: "negative fixture"}); err == nil {
				t.Fatal("opposite kind accepted")
			}
			appendEvent(timeline.Event{Type: timeline.Output, InputID: input.ID, Kind: c.Kind, Text: "Fixed protocol fixture; not model output or literary acceptance."})
			if _, err := s.CreateTimeline(project.TimelineOptions{ID: "untouched", Name: "untouched"}); err != nil {
				t.Fatal(err)
			}

			if c.Document != nil {
				before := "Scraped left arm. Carrying an old key."
				if err := s.UpdateCharacterState("main", "alice", before, nil); err != nil {
					t.Fatal(err)
				}
				if err := s.UpdateCharacterState("main", "alice", *c.Document, &before); err != nil {
					t.Fatal(err)
				}
			}
			if c.ChangePolishing != "" {
				if err := s.SetTimelineSettings("main", timeline.SettingsPatch{Polishing: &timeline.Polishing{Level: c.ChangePolishing}}); err != nil {
					t.Fatal(err)
				}
			}
			state, err = s.State("main")
			if err != nil {
				t.Fatal(err)
			}

			if c.Document != nil && state.Characters["alice"] != *c.Document {
				t.Fatal("state document not replaced")
			}
			if c.ChangePolishing != "" && state.Settings.Polishing.Level != c.ChangePolishing {
				t.Fatal("config not applied")
			}
			other, err := s.State("untouched")
			if err != nil {
				t.Fatal(err)
			}
			if len(other.Characters) != 0 {
				t.Fatal("cross-timeline state leak")
			}
			cfg, err := s.StorySettings("main")
			if err != nil || cfg.Polishing.Level != state.Settings.Polishing.Level {
				t.Fatal("story setting differs from replayed state")
			}
			history, err := s.History("main")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range history {
				if e.ID == input.ID {
					found = true
					if e.Text != c.Prompt {
						t.Fatal("input overwritten")
					}
				}
			}
			if !found {
				t.Fatal("input lost")
			}
			for id, card := range cards {
				actual, err := os.ReadFile(filepath.Join(root, "workspace", "characters", id, "card.md"))
				if err != nil || string(actual) != string(card) {
					t.Fatal("base card changed")
				}
			}
			dirs, err := os.ReadDir(filepath.Join(root, "workspace", "characters"))
			if err != nil || len(dirs) != 2 {
				t.Fatal("unexpected NPC card")
			}
			if _, err := s.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tag := range []string{"first", "third", "off", "basic", "full", "lock", "director_authorization", "npc", "ensemble", "chat_discussion", "dynamic_state", "untrusted_data"} {
		if !tags[tag] {
			t.Errorf("missing coverage %s", tag)
		}
	}
}
