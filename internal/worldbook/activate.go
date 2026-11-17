package worldbook

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/character/contentio"
)

type Message struct {
	Type    string `json:"type"`
	Speaker string `json:"speaker,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Text    string `json:"text"`
}

type Settings struct {
	Categories map[string]bool `json:"categories"`
	Entries    map[string]bool `json:"entries"`
	ScanRecent int             `json:"scan_recent"`
}

type Reference struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Activation string `json:"activation"`
	Priority   int    `json:"priority"`
}

func storyInput(message Message) bool {
	return message.Type == "input" && (message.Speaker == "director" || character.ValidID(message.Speaker))
}

func validateSettings(settings Settings) error {
	if settings.ScanRecent < 0 || settings.ScanRecent > 1000 {
		return errors.New("scan_recent must be between 0 and 1000")
	}
	for name := range settings.Categories {
		if !contentio.ValidName(name) {
			return fmt.Errorf("invalid category name %q", name)
		}
	}
	for id := range settings.Entries {
		if !contentio.ValidReference(id) {
			return fmt.Errorf("invalid worldbook settings ID %q", id)
		}
	}

	return nil
}

// Activate takes recent messages in chronological order. It first filters out
// Chat, assistant, and state/config events, then counts the last ScanRecent
// eligible messages. Text is matched per message, never across boundaries.
func (index *Index) Activate(input Message, recent []Message, settings Settings) ([]Reference, error) {
	if err := validateSettings(settings); err != nil {
		return nil, err
	}
	if input.Type != "input" || (!storyInput(input) && input.Speaker != "chat") {
		return nil, errors.New("activation requires a Chat, Director, or ordinary-character input")
	}
	if !utf8.ValidString(input.Text) {
		return nil, errors.New("input must be UTF-8")
	}
	result := []Reference{}
	if index == nil {
		return result, nil
	}
	texts := []string{fold(input.Text)}
	if settings.ScanRecent > 0 {
		selected := []string{}
		for i := len(recent) - 1; i >= 0 && len(selected) < settings.ScanRecent; i-- {
			message := recent[i]
			if storyInput(message) || (message.Type == "output" && message.Kind == "story") {
				if !utf8.ValidString(message.Text) {
					return nil, errors.New("recent story text must be UTF-8")
				}
				selected = append(selected, fold(message.Text))
			}
		}
		texts = append(texts, selected...)
	}
	for _, document := range index.documents {
		entry := document.Metadata
		enabled := entry.Enabled
		if override, ok := settings.Entries[document.Category+"/"+document.Slug]; ok {
			enabled = override
		}
		categoryEnabled := true
		if value, ok := settings.Categories[document.Category]; ok {
			categoryEnabled = value
		}
		if !enabled || !categoryEnabled {
			continue
		}
		active := false
		switch entry.Activation {
		case "always":
			active = true
		case "keyword":
			for _, key := range entry.Keys {
				for _, text := range texts {
					if strings.Contains(text, fold(key)) {
						active = true
						break
					}
				}
				if active {
					break
				}
			}
		}
		if active {
			result = append(result, Reference{ID: entry.ID, Path: document.Path, SHA256: document.SHA256, Activation: entry.Activation, Priority: entry.Priority})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		return result[i].Path < result[j].Path
	})
	return result, nil
}

// fold canonicalizes Unicode simple-fold equivalence classes, including Greek
// final sigma and long s that lowercasing alone would fail to match. It does not
// add linguistic normalization, stemming, or word boundaries.
func fold(text string) string {
	var output strings.Builder
	output.Grow(len(text))
	for _, r := range text {
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		output.WriteRune(minimum)
	}
	return output.String()
}
