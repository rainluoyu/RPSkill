package storage

import (
	"encoding/json"
	"testing"
)

func TestTypedJSONRejectsNullAndAlternateFieldNames(t *testing.T) {
	type payload struct {
		Patch struct {
			Entries map[string]bool `json:"entries"`
			Names   []string        `json:"names"`
		} `json:"patch"`
		Value json.RawMessage `json:"value"`
	}
	for _, raw := range []string{
		`null`, `{"Patch":{}}`, `{"patch":null}`, `{"patch":{"entries":{"magic":null}}}`,
		`{"patch":{"names":[null]}}`, `{"patch":{"entries":[]}}`, `{"patch":{"names":{}}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			var v payload
			if err := DecodeJSON([]byte(raw), &v); err == nil {
				t.Fatalf("invalid typed JSON accepted: %s", raw)
			}
		})
	}
	var v payload
	if err := DecodeJSON([]byte(`{"patch":{"entries":{"magic":false},"names":[]},"value":null}`), &v); err != nil {
		t.Fatal(err)
	}
	if string(v.Value) != "null" {
		t.Fatal("arbitrary state null was lost")
	}
}

func TestYAMLTypesDoNotCoerce(t *testing.T) {
	type payload struct {
		Name  string          `yaml:"name"`
		Flags map[string]bool `yaml:"flags"`
		Names []string        `yaml:"names"`
	}
	for _, raw := range []string{
		"name: 123\n", "name: true\n", "name: null\n", "name: name\nflags: {magic: 0}\n",
		"name: name\nnames: [false]\n", "name: name\nflags: []\n", "name: name\nnames: null\n",
	} {
		t.Run(raw, func(t *testing.T) {
			var v payload
			if err := DecodeYAML([]byte(raw), &v); err == nil {
				t.Fatalf("invalid YAML accepted: %s", raw)
			}
		})
	}
	var v payload
	if err := DecodeYAML([]byte("name: '123'\nflags: {magic: false}\nnames: []\n"), &v); err != nil {
		t.Fatal(err)
	}
}
