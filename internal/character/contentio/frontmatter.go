// Package contentio provides strict Markdown metadata and confined read helpers.
// It has no character, project, storage or timeline semantics.
package contentio

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const MaxDocumentSize = 4 << 20

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Frontmatter validates the YAML syntax and exact field types before decoding.
// kinds are string, bool, int, or strings. Every required field must be present.
func Frontmatter(data []byte, target any, kinds map[string]string, required []string) (string, error) {
	if len(data) > MaxDocumentSize {
		return "", errors.New("Markdown exceeds 4 MiB")
	}
	if !utf8.Valid(data) || bytes.ContainsRune(data, '\x00') {
		return "", errors.New("Markdown must be UTF-8 without NUL")
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return "", errors.New("Markdown must start with --- frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSuffix(lines[i], "\r") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", errors.New("frontmatter has no closing ---")
	}
	raw := strings.Join(lines[1:end], "\n")
	var tree yaml.Node
	dec := yaml.NewDecoder(strings.NewReader(raw))
	if err := dec.Decode(&tree); err != nil {
		return "", fmt.Errorf("frontmatter: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", errors.New("frontmatter must contain exactly one YAML document")
	}
	if len(tree.Content) != 1 || tree.Content[0].Kind != yaml.MappingNode || tree.Content[0].Tag != "!!map" {
		return "", errors.New("frontmatter must be a mapping")
	}
	node := tree.Content[0]
	if node.Anchor != "" {
		return "", errors.New("YAML anchors are not allowed")
	}
	seen := make(map[string]bool)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return "", errors.New("frontmatter keys must be strings; merges are not allowed")
		}
		name := key.Value
		if seen[name] {
			return "", fmt.Errorf("duplicate frontmatter key %q", name)
		}
		seen[name] = true
		kind, ok := kinds[name]
		if !ok {
			return "", fmt.Errorf("unknown frontmatter field %q", name)
		}
		if key.Anchor != "" || value.Anchor != "" || value.Kind == yaml.AliasNode {
			return "", fmt.Errorf("YAML aliases/anchors are not allowed: %s", name)
		}
		switch kind {
		case "string", "bool", "int":
			tag := map[string]string{"string": "!!str", "bool": "!!bool", "int": "!!int"}[kind]
			if value.Kind != yaml.ScalarNode || value.Tag != tag {
				return "", fmt.Errorf("field %s must be %s", name, kind)
			}
		case "strings":
			if value.Kind != yaml.SequenceNode || value.Tag != "!!seq" {
				return "", fmt.Errorf("field %s must be a string array", name)
			}
			for _, item := range value.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || item.Anchor != "" {
					return "", fmt.Errorf("field %s must contain only strings without aliases", name)
				}
			}
		default:
			return "", fmt.Errorf("unsupported field type %q", kind)
		}
	}
	for _, name := range required {
		if !seen[name] {
			return "", fmt.Errorf("missing required frontmatter field %q", name)
		}
	}
	strict := yaml.NewDecoder(strings.NewReader(raw))
	strict.KnownFields(true)
	if err := strict.Decode(target); err != nil {
		return "", fmt.Errorf("frontmatter: %w", err)
	}
	return strings.Join(lines[end+1:], "\n"), nil
}
