package timeline

import (
	"strings"
	"testing"
)

func TestEventStrictSchema(t *testing.T) {
	base := `{"type":"input","time":"2026-11-12T08:00:00Z","speaker":"director","text":"prompt"}`
	for _, raw := range []string{
		strings.Replace(base, `"speaker":"director"`, `"speaker":null`, 1),
		strings.Replace(base, `"text":"prompt"`, `"text":null`, 1),
		strings.Replace(base, `"type":"input"`, `"Type":"input"`, 1),
		strings.Replace(base, `"speaker":"director"`, `"kind":"","speaker":"director"`, 1),
		strings.Replace(base, `"text":"prompt"`, `"characters":[],"text":"prompt"`, 1),
	} {
		if _, err := ParseMessages([][]byte{[]byte(raw)}, "main"); err == nil {
			t.Fatalf("malformed event accepted: %s", raw)
		}
	}
	if _, err := ParseMessages([][]byte{[]byte(base)}, "main"); err != nil {
		t.Fatal(err)
	}
}

func TestRejectLegacyCharacterOperations(t *testing.T) {
	for _, change := range []string{
		`{"character":"alice","operations":[{"op":"set","key":"injury","value":"scratch"}]}`,
		`{"character":"alice","document":"state","operations":[]}`,
		`{"character":"alice","document":null}`,
		`{"character":"alice"}`,
	} {
		raw := `{"type":"input","time":"2026-11-16T00:00:00Z","speaker":"director","text":"start","meta":{"characters":[` + change + `]}}`
		if _, err := ParseMessages([][]byte{[]byte(raw)}, "main"); err == nil {
			t.Fatalf("invalid character state accepted: %s", change)
		}
	}
}
