// Package worldbook provides a deterministic, read-only file index. Activation
// returns file hints, not injected instructions or complete worldbook text.
package worldbook

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

type Entry struct {
	ID         string   `json:"id" yaml:"-"`
	Name       string   `json:"name" yaml:"-"`
	Enabled    bool     `json:"enabled" yaml:"enabled"`
	Activation string   `json:"activation" yaml:"activation"`
	Keys       []string `json:"keys" yaml:"keys"`
	Priority   int      `json:"priority" yaml:"priority"`
	Body       string   `json:"body" yaml:"-"`
}

type Document struct {
	Metadata Entry  `json:"metadata"`
	Category string `json:"category"`
	Slug     string `json:"slug"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	SHA256   string `json:"sha256"`
}

// Index is an immutable snapshot. Publish a successful Refresh under the Store
// lock; readers may keep using the old snapshot while the new one is built.
type Index struct{ documents []Document }

func Parse(content []byte, filename string) (Entry, error) {
	var metadata struct {
		Enabled    bool     `yaml:"enabled"`
		Activation string   `yaml:"activation"`
		Keys       []string `yaml:"keys"`
		Priority   int      `yaml:"priority"`
	}
	body, err := contentio.Frontmatter(content, &metadata, map[string]string{"enabled": "bool", "activation": "string", "keys": "strings", "priority": "int"}, []string{"enabled", "activation", "keys", "priority"})
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{ID: filename, Name: filename, Enabled: metadata.Enabled, Activation: metadata.Activation, Keys: metadata.Keys, Priority: metadata.Priority, Body: body}
	return entry, Validate(entry)
}

func Encode(entry Entry) ([]byte, error) {
	if err := Validate(entry); err != nil {
		return nil, err
	}
	metadata, err := yaml.Marshal(entry)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(metadata) + "---\n" + entry.Body), nil
}

func Validate(entry Entry) error {
	if !contentio.ValidName(entry.ID) {
		return fmt.Errorf("invalid worldbook ID %q", entry.ID)
	}
	if strings.TrimSpace(entry.Name) == "" || !utf8.ValidString(entry.Name) || strings.ContainsAny(entry.Name, "\x00\r\n") {
		return errors.New("worldbook name must be a nonempty single line")
	}
	switch entry.Activation {
	case "always", "keyword":
	default:
		return fmt.Errorf("invalid activation %q", entry.Activation)
	}
	if entry.Activation == "keyword" && len(entry.Keys) == 0 {
		return errors.New("keyword activation requires at least one key")
	}
	for _, key := range entry.Keys {
		if strings.TrimSpace(key) == "" || !utf8.ValidString(key) || strings.ContainsRune(key, '\x00') {
			return errors.New("worldbook keys must be nonempty UTF-8 strings without NUL")
		}
	}
	if !utf8.ValidString(entry.Body) || strings.ContainsRune(entry.Body, '\x00') {
		return errors.New("worldbook body must be UTF-8 without NUL")
	}
	return nil
}

func Scan(workspace string) (*Index, error) {
	root, err := contentio.Open(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	worldbook, err := contentio.Directory(root, "worldbook")
	if errors.Is(err, os.ErrNotExist) {
		return &Index{documents: []Document{}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer worldbook.Close()
	categories, err := contentio.ReadDir(worldbook)
	if err != nil {
		return nil, err
	}
	documents := []Document{}
	ids := map[string]string{}
	categoryNames := map[string]bool{}
	for _, category := range categories {
		name := category.Name()
		if categoryNames[strings.ToLower(name)] {
			return nil, fmt.Errorf("conflicting category %q", name)
		}
		categoryNames[strings.ToLower(name)] = true
		if !contentio.ValidName(name) {
			return nil, fmt.Errorf("invalid worldbook category %q", name)
		}
		folder, err := contentio.Directory(worldbook, name)
		if err != nil {
			return nil, fmt.Errorf("worldbook category %s: %w", name, err)
		}
		entries, readErr := contentio.ReadDir(folder)
		if readErr != nil {
			folder.Close()
			return nil, readErr
		}
		for _, file := range entries {
			slug := strings.TrimSuffix(file.Name(), ".md")
			if file.Name() != slug+".md" || !contentio.ValidName(slug) {
				folder.Close()
				return nil, fmt.Errorf("worldbook must use <category>/<slug>.md: %s/%s", name, file.Name())
			}
			data, err := contentio.ReadFile(folder, file.Name(), contentio.MaxDocumentSize)
			if err != nil {
				folder.Close()
				return nil, err
			}
			metadata, err := Parse(data, slug)
			if err != nil {
				folder.Close()
				return nil, fmt.Errorf("worldbook %s/%s: %w", name, file.Name(), err)
			}
			relative := path.Join("worldbook", name, file.Name())
			key := strings.ToLower(name + "/" + slug)
			if previous, ok := ids[key]; ok {
				folder.Close()
				return nil, fmt.Errorf("duplicate worldbook ID %q at %s and %s", metadata.ID, previous, relative)
			}
			ids[key] = relative
			documents = append(documents, Document{Metadata: metadata, Category: name, Slug: slug, Path: relative, Content: string(data), SHA256: contentio.Hash(data)})
		}
		folder.Close()
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	return &Index{documents: documents}, nil
}

func (index *Index) Documents() []Document {
	result := []Document{}
	if index == nil {
		return result
	}
	for _, document := range index.documents {
		document.Metadata.Keys = append([]string{}, document.Metadata.Keys...)
		result = append(result, document)
	}
	return result
}

type Changes struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}

func (index *Index) Refresh(workspace string) (*Index, Changes, error) {
	next, err := Scan(workspace)
	if err != nil {
		return nil, Changes{}, err
	}
	changes := Changes{Added: []string{}, Modified: []string{}, Removed: []string{}}
	previous := map[string]string{}
	if index != nil {
		for _, document := range index.documents {
			previous[document.Path] = document.SHA256
		}
	}
	for _, document := range next.documents {
		hash, exists := previous[document.Path]
		if !exists {
			changes.Added = append(changes.Added, document.Path)
		} else if hash != document.SHA256 {
			changes.Modified = append(changes.Modified, document.Path)
		}
		delete(previous, document.Path)
	}
	for relative := range previous {
		changes.Removed = append(changes.Removed, relative)
	}
	sort.Strings(changes.Added)
	sort.Strings(changes.Modified)
	sort.Strings(changes.Removed)
	return next, changes, nil
}
