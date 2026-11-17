// Package storage provides bounded, symlink-safe file I/O and recoverable writes.
package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// DecodeJSON rejects duplicate keys at every depth, unknown struct fields,
// trailing values and invalid UTF-8. Numbers retain their exact representation.
func DecodeJSON(data []byte, target any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func() error
	value = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok {
					return errors.New("object key must be string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("JSON must contain one value")
	}
	typ := reflect.TypeOf(target)
	if typ == nil || typ.Kind() != reflect.Pointer || reflect.ValueOf(target).IsNil() {
		return errors.New("JSON target must be a non-nil pointer")
	}
	if err := validateJSONTypes(data, typ); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	d.UseNumber()
	return d.Decode(target)
}

// Typed protocol fields do not use JSON null as a substitute for omission.
// Arbitrary state values (RawMessage/interface) deliberately retain JSON null.
func validateJSONTypes(raw json.RawMessage, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() || typ.Kind() == reflect.Interface {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("typed JSON field must not be null")
	}
	// time.Time and other explicit codecs validate their own non-null wire type.
	if reflect.PointerTo(typ).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		fields := map[string]reflect.Type{}
		var collect func(reflect.Type)
		collect = func(t reflect.Type) {
			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				if !field.IsExported() {
					continue
				}
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if name == "-" {
					continue
				}
				ft := field.Type
				for ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if name == "" && field.Anonymous && ft.Kind() == reflect.Struct {
					collect(ft)
					continue
				}
				if name == "" {
					name = field.Name
				}
				fields[name] = field.Type
			}
		}
		collect(typ)
		for name, value := range object {
			ft, ok := fields[name]
			if !ok {
				return fmt.Errorf("unknown JSON field %q", name)
			}
			if err := validateJSONTypes(value, ft); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	case reflect.Map:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		for key, value := range object {
			if err := validateJSONTypes(value, typ.Elem()); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	case reflect.Slice, reflect.Array:
		if typ.Elem().Kind() == reflect.Uint8 { // encoding/json uses base64 for []byte.
			return nil
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for i, value := range items {
			if err := validateJSONTypes(value, typ.Elem()); err != nil {
				return fmt.Errorf("index %d: %w", i, err)
			}
		}
	}
	return nil
}

// DecodeYAML permits one document, no aliases/merge keys, no duplicate mapping
// keys, and no unknown struct fields. YAML implicit coercion is validated by the
// caller's typed model and node tags, not silently accepted as configuration.
func DecodeYAML(data []byte, target any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8")
	}
	var doc yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&doc); err != nil {
		return err
	}
	var extra yaml.Node
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("YAML must contain one document")
	}
	var walk func(*yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n.Kind == yaml.AliasNode {
			return errors.New("YAML aliases are not allowed")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Tag != "!!str" || k.Value == "<<" {
					return errors.New("YAML mapping keys must be plain strings; merge keys are not allowed")
				}
				if seen[k.Value] {
					return fmt.Errorf("duplicate YAML key %q", k.Value)
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(&doc); err != nil {
		return err
	}
	if err := validateYAMLTypes(&doc, reflect.TypeOf(target)); err != nil {
		return err
	}
	d = yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	return d.Decode(target)
}

// validateYAMLTypes blocks yaml.v3's convenient numeric/bool-to-string coercion
// for schema fields. Null is not a synonym for an omitted configuration field.
func validateYAMLTypes(node *yaml.Node, typ reflect.Type) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) != 1 {
			return errors.New("empty YAML document")
		}
		return validateYAMLTypes(node.Content[0], typ)
	}
	if node.Tag == "!!null" {
		return errors.New("explicit YAML null is not allowed")
	}
	switch typ.Kind() {
	case reflect.String:
		if node.Tag != "!!str" {
			return errors.New("YAML string field must be a string")
		}
	case reflect.Bool:
		if node.Tag != "!!bool" {
			return errors.New("YAML boolean field must be true or false")
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if node.Tag != "!!int" {
			return errors.New("YAML integer field must be an integer")
		}
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return errors.New("YAML object field must be a mapping")
		}
		fields := map[string]reflect.Type{}
		var collect func(reflect.Type)
		collect = func(t reflect.Type) {
			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				if !field.IsExported() {
					continue
				}
				tag := field.Tag.Get("yaml")
				parts := strings.Split(tag, ",")
				if parts[0] == "-" {
					continue
				}
				if len(parts) > 1 && parts[1] == "inline" {
					ft := field.Type
					for ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					if ft.Kind() == reflect.Struct {
						collect(ft)
					}
					continue
				}
				name := parts[0]
				if name == "" {
					name = strings.ToLower(field.Name)
				}
				fields[name] = field.Type
			}
		}
		collect(typ)
		for i := 0; i < len(node.Content); i += 2 {
			if ft, ok := fields[node.Content[i].Value]; ok {
				if err := validateYAMLTypes(node.Content[i+1], ft); err != nil {
					return fmt.Errorf("%s: %w", node.Content[i].Value, err)
				}
			}
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			return errors.New("YAML map field must be a mapping")
		}
		for i := 0; i < len(node.Content); i += 2 {
			if err := validateYAMLTypes(node.Content[i+1], typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			return errors.New("YAML array field must be a sequence")
		}
		for _, child := range node.Content {
			if err := validateYAMLTypes(child, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}
