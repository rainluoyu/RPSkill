// Package character parses and reads base character cards. It never writes RP
// data or replays timeline state; the Store owns those operations.
package character

import (
	"errors"
	"fmt"
	"go.yaml.in/yaml/v3"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/luoyu/rpskill/internal/character/contentio"
)

// Card is base metadata plus Markdown body, not dynamic timeline state.
type Card struct {
	ID      string `json:"id" yaml:"-"`
	Name    string `json:"name" yaml:"-"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Avatar  string `json:"avatar,omitempty" yaml:"avatar,omitempty"`
	Body    string `json:"body" yaml:"-"`
}

type Document struct {
	Metadata Card        `json:"metadata"`
	Path     string      `json:"path"`
	Content  string      `json:"content"`
	SHA256   string      `json:"sha256"`
	Assets   []AssetInfo `json:"assets"`
}

type View struct {
	Document
	Enabled bool `json:"enabled"`
}

var reserved = map[string]bool{"human": true, "god": true, "chat": true, "director": true, "story": true, "assistant": true, "narrator": true, "environment": true}

func ValidID(id string) bool { return contentio.ValidName(id) && !reserved[strings.ToLower(id)] }

func Parse(content []byte, folder string) (Card, error) {
	var metadata struct {
		Enabled bool   `yaml:"enabled"`
		Avatar  string `yaml:"avatar"`
	}
	body, err := contentio.Frontmatter(content, &metadata, map[string]string{"enabled": "bool", "avatar": "string"}, []string{"enabled"})
	if err != nil {
		return Card{}, err
	}
	card := Card{ID: folder, Name: folder, Enabled: metadata.Enabled, Avatar: metadata.Avatar, Body: body}
	return card, Validate(card)
}

func Encode(card Card) ([]byte, error) {
	if err := Validate(card); err != nil {
		return nil, err
	}
	metadata, err := yaml.Marshal(card)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(metadata) + "---\n" + card.Body), nil
}

func Validate(card Card) error {
	if !ValidID(card.ID) {
		return fmt.Errorf("invalid or reserved character ID %q", card.ID)
	}
	if strings.TrimSpace(card.Name) == "" || !utf8.ValidString(card.Name) || strings.ContainsAny(card.Name, "\x00\r\n") {
		return errors.New("character name must be a nonempty single line")
	}
	if !utf8.ValidString(card.Body) || strings.ContainsRune(card.Body, '\x00') {
		return errors.New("character body must be UTF-8 without NUL")
	}
	if card.Avatar != "" {
		if err := contentio.Relative(card.Avatar); err != nil {
			return fmt.Errorf("avatar: %w", err)
		}
	}
	return nil
}

// Scan validates all cards and assets, including disabled cards. Empty or absent
// character directories are valid; malformed/unknown root entries are not.
func Scan(workspace string) ([]Document, error) {
	root, err := contentio.Open(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	characters, err := contentio.Directory(root, "characters")
	if errors.Is(err, os.ErrNotExist) {
		return []Document{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer characters.Close()
	dirs, err := contentio.ReadDir(characters)
	if err != nil {
		return nil, err
	}
	result := make([]Document, 0, len(dirs))
	seen := map[string]bool{}
	for _, dir := range dirs {
		id := dir.Name()
		if seen[strings.ToLower(id)] {
			return nil, fmt.Errorf("conflicting character folder %q", id)
		}
		seen[strings.ToLower(id)] = true
		if !ValidID(id) {
			return nil, fmt.Errorf("invalid character directory %q", id)
		}
		folder, err := contentio.Directory(characters, id)
		if err != nil {
			return nil, fmt.Errorf("character %s: %w", id, err)
		}
		document, readErr := scanCard(folder, id)
		folder.Close()
		if readErr != nil {
			return nil, fmt.Errorf("character %s: %w", id, readErr)
		}
		result = append(result, document)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Metadata.ID < result[j].Metadata.ID })
	return result, nil
}

func scanCard(folder *os.Root, id string) (Document, error) {
	data, err := contentio.ReadFile(folder, "card.md", contentio.MaxDocumentSize)
	if err != nil {
		return Document{}, err
	}
	card, err := Parse(data, id)
	if err != nil {
		return Document{}, err
	}
	assets, err := scanAssets(folder, "", 0)
	if err != nil {
		return Document{}, err
	}
	if card.Avatar != "" {
		found := false
		for _, asset := range assets {
			if asset.RelativePath == card.Avatar {
				found = true
				if !imageMIME(asset.MIME) {
					return Document{}, errors.New("avatar must contain a supported raster image")
				}
				break
			}
		}
		if !found {
			return Document{}, fmt.Errorf("avatar file not found: %s", card.Avatar)
		}
	}
	for i := range assets {
		assets[i].Path = path.Join("characters", id, assets[i].RelativePath)
	}
	return Document{Metadata: card, Path: path.Join("characters", id, "card.md"), Content: string(data), SHA256: contentio.Hash(data), Assets: assets}, nil
}

// Effective applies explicit timeline booleans, preserving both disabled rows
// and base metadata for editors. Selecting a view never mutates the base card.
func Effective(documents []Document, overrides map[string]bool) []View {
	result := make([]View, 0, len(documents))
	for _, document := range documents {
		enabled := document.Metadata.Enabled
		if value, ok := overrides[document.Metadata.ID]; ok {
			enabled = value
		}
		document.Assets = append([]AssetInfo{}, document.Assets...)
		result = append(result, View{Document: document, Enabled: enabled})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Metadata.ID < result[j].Metadata.ID })
	return result
}
