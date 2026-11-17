package worldbook_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyu/rpskill/internal/worldbook"
)

const validEntry = "---\nenabled: true\nactivation: keyword\nkeys: [魔法, MAGIC]\npriority: 50\n---\n# 魔法体系\n魔法来自星辰。\n"

func tempWorkspace(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func write(t *testing.T, workspace, relative, content string) {
	t.Helper()
	target := filepath.Join(workspace, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func entry(id, activation string, priority int, enabled bool, keys string) string {
	return fmt.Sprintf("---\nenabled: %t\nactivation: %s\nkeys: %s\npriority: %d\n---\n秘密世界正文：%s\n", enabled, activation, keys, priority, id)
}
func load(t *testing.T, files map[string]string) (string, *worldbook.Index) {
	t.Helper()
	workspace := tempWorkspace(t)
	for relative, content := range files {
		write(t, workspace, relative, content)
	}
	index, err := worldbook.Scan(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return workspace, index
}
func refs(t *testing.T, index *worldbook.Index, input worldbook.Message, recent []worldbook.Message, settings worldbook.Settings) []worldbook.Reference {
	t.Helper()
	result, err := index.Activate(input, recent, settings)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func ids(references []worldbook.Reference) []string {
	result := []string{}
	for _, reference := range references {
		result = append(result, reference.ID)
	}
	return result
}
func director(text string) worldbook.Message {
	return worldbook.Message{Type: "input", Speaker: "director", Text: text}
}

func TestParseStrictWorldbook(t *testing.T) {
	cases := map[string]string{
		"duplicate":          strings.Replace(validEntry, "priority: 50", "priority: 50\npriority: 20", 1),
		"unknown":            strings.Replace(validEntry, "priority: 50", "priority: 50\nfoo: bar", 1),
		"quoted enabled":     strings.Replace(validEntry, "enabled: true", "enabled: \"true\"", 1),
		"quoted priority":    strings.Replace(validEntry, "priority: 50", "priority: \"50\"", 1),
		"float priority":     strings.Replace(validEntry, "priority: 50", "priority: 1.5", 1),
		"overflow priority":  strings.Replace(validEntry, "priority: 50", "priority: 999999999999999999999999999", 1),
		"map keys":           strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: {a: b}", 1),
		"scalar keys":        strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: MAGIC", 1),
		"null keys":          strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: null", 1),
		"integer key":        strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: [42]", 1),
		"null key":           strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: [null]", 1),
		"empty key":          strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: ['']", 1),
		"whitespace key":     strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: ['  ']", 1),
		"empty keyword list": strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: []", 1),
		"invalid activation": strings.Replace(validEntry, "activation: keyword", "activation: vector", 1),
		"invalid id":         strings.Replace(validEntry, "enabled: true", "id: entry\nenabled: true", 1),
		"invalid name":       strings.Replace(validEntry, "enabled: true", "name: old\nenabled: true", 1),
		"alias":              strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: &k [魔法, MAGIC]", 1),
		"alias item":         strings.Replace(validEntry, "keys: [魔法, MAGIC]", "keys: [魔法, &k MAGIC]", 1),
		"custom yaml tag":    strings.Replace(validEntry, "enabled: true", "enabled: !foo true", 1),
		"NUL body":           validEntry + "\x00",
		"invalid utf8":       validEntry + string([]byte{0xff}),
	}
	for _, field := range []string{"enabled", "activation", "keys", "priority"} {
		lines := strings.Split(validEntry, "\n")
		filtered := []string{}
		for _, line := range lines {
			if !strings.HasPrefix(line, field+":") {
				filtered = append(filtered, line)
			}
		}
		cases["missing "+field] = strings.Join(filtered, "\n")
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := worldbook.Parse([]byte(content), "entry"); err == nil {
				t.Fatal("accepted invalid entry")
			}
		})
	}
	for _, content := range []string{validEntry, strings.ReplaceAll(validEntry, "\n", "\r\n"), entry("world", "always", 0, true, "[]")} {
		parsed, err := worldbook.Parse([]byte(content), "entry")
		if err != nil {
			t.Fatal(err)
		}
		if parsed.ID == "" || parsed.Name == "" || parsed.Body == "" {
			t.Fatalf("incomplete parse: %+v", parsed)
		}
	}
}

func TestScanStrictStructureAndUniqueIDs(t *testing.T) {
	cases := map[string]map[string]string{
		"flat file":                {"worldbook/entry.md": validEntry},
		"third level":              {"worldbook/magic/nested/entry.md": validEntry},
		"directory named markdown": {"worldbook/magic/entry.md/nested.md": validEntry},
		"invalid category":         {"worldbook/Bad?/entry.md": validEntry},
		"invalid slug":             {"worldbook/magic/Bad?.md": validEntry},
		"wrong extension":          {"worldbook/magic/entry.txt": validEntry},
		"case collision category":  {"worldbook/magic/entry.md": validEntry, "worldbook/Magic/other.md": validEntry},
		"case collision entry":     {"worldbook/magic/entry.md": validEntry, "worldbook/magic/Entry.md": validEntry},
		"hidden root file":         {"worldbook/.cache": "data"},
		"hidden category file":     {"worldbook/magic/.entry.md": validEntry},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			workspace := tempWorkspace(t)
			for relative, content := range files {
				write(t, workspace, relative, content)
			}
			if _, err := worldbook.Scan(workspace); err == nil {
				t.Fatal("accepted invalid structure")
			}
		})
	}
	workspace, index := load(t, map[string]string{"worldbook/magic/different-filename.md": validEntry})
	documents := index.Documents()
	if len(documents) != 1 || documents[0].Metadata.ID != "different-filename" || documents[0].Slug != "different-filename" || documents[0].Category != "magic" || documents[0].Content != validEntry {
		t.Fatalf("invalid docs: %+v", documents)
	}
	sum := sha256.Sum256([]byte(validEntry))
	if documents[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("hash mismatch")
	}
	again, err := worldbook.Scan(workspace)
	if err != nil || !reflect.DeepEqual(documents, again.Documents()) {
		t.Fatalf("nondeterministic: %v", err)
	}
	documents[0].Metadata.Keys[0] = "corrupt"
	if index.Documents()[0].Metadata.Keys[0] == "corrupt" {
		t.Fatal("Documents leaks mutable metadata")
	}
	empty, err := worldbook.Scan(tempWorkspace(t))
	if err != nil || len(empty.Documents()) != 0 {
		t.Fatalf("empty scan: %v", err)
	}
}

func TestWorldbookSymlinkEscapes(t *testing.T) {
	for _, kind := range []string{"root", "category", "entry", "inbound-entry", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			workspace := tempWorkspace(t)
			outside := tempWorkspace(t)
			write(t, outside, "entry.md", validEntry)
			write(t, workspace, "worldbook/magic/entry.md", validEntry)
			var target, link string
			switch kind {
			case "root":
				target = filepath.Join(workspace, "worldbook")
				link = filepath.Join(workspace, "alias")
			case "category":
				target = outside
				link = filepath.Join(workspace, "worldbook/external")
			case "entry":
				target = filepath.Join(outside, "entry.md")
				link = filepath.Join(workspace, "worldbook/magic/link.md")
			case "inbound-entry":
				target = "entry.md"
				link = filepath.Join(workspace, "worldbook/magic/link.md")
			case "dangling":
				target = filepath.Join(outside, "missing.md")
				link = filepath.Join(workspace, "worldbook/magic/link.md")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if kind == "root" {
				if _, err := worldbook.Scan(link); err == nil {
					t.Fatal("followed workspace symlink")
				}
				return
			}
			if _, err := worldbook.Scan(workspace); err == nil {
				t.Fatal("followed worldbook symlink")
			}
		})
	}
}

func TestActivateOrderingDedupAndSafeHints(t *testing.T) {
	_, index := load(t, map[string]string{
		"worldbook/world/background.md": entry("background", "always", 100, true, "[]"),
		"worldbook/magic/a.md":          entry("a", "keyword", 50, true, "[魔法, MAGIC, MAGIC]"),
		"worldbook/magic/b.md":          entry("b", "keyword", 50, true, "[魔法]"),
		"worldbook/world/disabled.md":   entry("disabled", "always", 200, false, "[]"),
	})
	references := refs(t, index, director("MAGIC 与魔法"), nil, worldbook.Settings{})
	if !reflect.DeepEqual(ids(references), []string{"background", "a", "b"}) {
		t.Fatalf("bad activation: %+v", references)
	}
	for _, reference := range references {
		if len(reference.SHA256) != 64 || !strings.HasPrefix(reference.Path, "worldbook/") {
			t.Fatalf("invalid hint: %+v", reference)
		}
	}
	raw, err := json.Marshal(references)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "秘密世界正文") || strings.Contains(string(raw), "body") || strings.Contains(string(raw), "content") {
		t.Fatalf("hint includes content: %s", raw)
	}
}

func TestActivationCategoryAndEntryOverrides(t *testing.T) {
	_, index := load(t, map[string]string{
		"worldbook/magic/enabled.md":  entry("enabled", "keyword", 1, true, "[magic]"),
		"worldbook/magic/disabled.md": entry("disabled", "keyword", 1, false, "[magic]"),
	})
	cases := []struct {
		name     string
		settings worldbook.Settings
		want     []string
	}{
		{"default", worldbook.Settings{}, []string{"enabled"}},
		{"project disabled", worldbook.Settings{Categories: map[string]bool{"magic": false}}, []string{}},
		{"entry disabled", worldbook.Settings{Entries: map[string]bool{"magic/enabled": false}}, []string{}},
		{"entry reenabled", worldbook.Settings{Entries: map[string]bool{"magic/disabled": true}}, []string{"disabled", "enabled"}},
		{"entry cannot bypass category", worldbook.Settings{Categories: map[string]bool{"magic": false}, Entries: map[string]bool{"magic/disabled": true}}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(refs(t, index, director("magic"), nil, tc.settings)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestChatUsesNormalActivation(t *testing.T) {
	_, index := load(t, map[string]string{"worldbook/world/always.md": entry("always", "always", 0, true, "[]"), "worldbook/magic/magic.md": validEntry})
	chat := worldbook.Message{Type: "input", Speaker: "chat", Text: "魔法"}
	settings := worldbook.Settings{ScanRecent: 5}
	if result := refs(t, index, chat, nil, settings); len(result) != 2 {
		t.Fatalf("Chat should use normal worldbook activation: %+v", result)
	}
}

func TestRecentFilteringAndCounting(t *testing.T) {
	_, index := load(t, map[string]string{"worldbook/magic/entry.md": validEntry})
	cases := []struct {
		name   string
		recent []worldbook.Message
		n      int
		want   bool
	}{
		{"current only", []worldbook.Message{director("魔法")}, 0, false},
		{"Director input", []worldbook.Message{director("魔法")}, 1, true},
		{"character input", []worldbook.Message{{Type: "input", Speaker: "alice", Text: "魔法"}}, 1, true},
		{"story", []worldbook.Message{{Type: "output", Kind: "story", Text: "魔法"}}, 1, true},
		{"assistant excluded", []worldbook.Message{{Type: "output", Kind: "assistant", Text: "魔法"}}, 10, false},
		{"Chat excluded", []worldbook.Message{{Type: "input", Speaker: "chat", Text: "魔法"}}, 10, false},
		{"state excluded", []worldbook.Message{{Type: "character.update", Text: "魔法"}}, 10, false},
		{"newest eligible", []worldbook.Message{director("魔法"), director("rain")}, 1, false},
		{"two eligible", []worldbook.Message{director("魔法"), director("rain")}, 2, true},
		{"discussion not counted", []worldbook.Message{director("魔法"), {Type: "input", Speaker: "chat", Text: "planning"}, {Type: "output", Kind: "assistant", Text: "advice"}}, 1, true},
		{"no cross message match", []worldbook.Message{director("ma"), director("gic")}, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := refs(t, index, director("nothing"), tc.recent, worldbook.Settings{ScanRecent: tc.n})
			if (len(result) > 0) != tc.want {
				t.Fatalf("activation: %+v", result)
			}
		})
	}
}

func TestUnicodeCaseInsensitiveSubstrings(t *testing.T) {
	_, index := load(t, map[string]string{"worldbook/magic/entry.md": entry("unicode", "keyword", 0, true, "[MAGIC, Σ, S, 魔法]")})
	for _, text := range []string{"magic", "UnMaGiCaL", "σ", "ς", "ſ", "古代魔法师"} {
		t.Run(text, func(t *testing.T) {
			if result := refs(t, index, director(text), nil, worldbook.Settings{}); len(result) != 1 {
				t.Fatalf("did not match %q", text)
			}
		})
	}
}

func TestRemovedManualActivationRejected(t *testing.T) {
	if _, err := worldbook.Parse([]byte(entry("old", "manual", 0, true, "[]")), "old"); err == nil {
		t.Fatal("manual activation accepted")
	}
}

func TestRefreshChangesAndFailurePreservesSnapshot(t *testing.T) {
	workspace, index := load(t, map[string]string{"worldbook/magic/entry.md": validEntry, "worldbook/world/remove.md": entry("remove", "always", 0, true, "[]")})
	before := index.Documents()
	write(t, workspace, "worldbook/magic/entry.md", validEntry+"新增正文。\n")
	write(t, workspace, "worldbook/world/new.md", entry("new", "always", 0, true, "[]"))
	if err := os.Remove(filepath.Join(workspace, "worldbook/world/remove.md")); err != nil {
		t.Fatal(err)
	}
	next, changes, err := index.Refresh(workspace)
	if err != nil {
		t.Fatal(err)
	}
	want := worldbook.Changes{Added: []string{"worldbook/world/new.md"}, Modified: []string{"worldbook/magic/entry.md"}, Removed: []string{"worldbook/world/remove.md"}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes %+v want %+v", changes, want)
	}
	if !reflect.DeepEqual(before, index.Documents()) {
		t.Fatal("Refresh mutated existing snapshot")
	}
	_, unchanged, err := next.Refresh(workspace)
	if err != nil || len(unchanged.Added)+len(unchanged.Modified)+len(unchanged.Removed) != 0 {
		t.Fatalf("unchanged refresh %+v %v", unchanged, err)
	}
	write(t, workspace, "worldbook/world/duplicate.md", "---\nenabled: null\n---\n")
	if _, _, err := next.Refresh(workspace); err == nil {
		t.Fatal("accepted malformed refresh")
	}
	if got := refs(t, next, director("magic"), nil, worldbook.Settings{}); len(got) != 2 {
		t.Fatalf("last good snapshot corrupted: %+v", got)
	}
}

func TestActivationInvalidSettingsAndInput(t *testing.T) {
	_, index := load(t, map[string]string{"worldbook/magic/entry.md": validEntry})
	for _, settings := range []worldbook.Settings{{ScanRecent: -1}, {Categories: map[string]bool{"../x": true}}, {Entries: map[string]bool{"../x": false}}} {
		if _, err := index.Activate(director("magic"), nil, settings); err == nil {
			t.Errorf("accepted %+v", settings)
		}
	}
	for _, input := range []worldbook.Message{{Type: "output", Kind: "story"}, {Type: "input", Speaker: "assistant"}, {Type: "input", Speaker: "narrator"}, {Type: "input", Speaker: "../alice"}, director(string([]byte{0xff}))} {
		if _, err := index.Activate(input, nil, worldbook.Settings{}); err == nil {
			t.Errorf("accepted %+v", input)
		}
	}
	if _, err := index.Activate(director("none"), []worldbook.Message{director(string([]byte{0xff}))}, worldbook.Settings{ScanRecent: 1}); err == nil {
		t.Fatal("accepted invalid recent UTF-8")
	}
	var empty *worldbook.Index
	if result, err := empty.Activate(director("magic"), nil, worldbook.Settings{}); err != nil || len(result) != 0 {
		t.Fatalf("nil snapshot: %+v %v", result, err)
	}
}

func TestConcurrentSnapshotReads(t *testing.T) {
	_, index := load(t, map[string]string{"worldbook/magic/entry.md": validEntry})
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 25; j++ {
				documents := index.Documents()
				documents[0].Metadata.Keys[0] = "mutated"
				result, err := index.Activate(director("magic"), nil, worldbook.Settings{})
				if err != nil || len(result) != 1 {
					t.Errorf("concurrent activation: %+v %v", result, err)
					return
				}
			}
		}()
	}
	group.Wait()
}

func TestValidateEntryDirect(t *testing.T) {
	base := worldbook.Entry{ID: "entry", Name: "Entry", Enabled: true, Activation: "keyword", Keys: []string{"key"}, Priority: 1}
	for _, mutate := range []func(*worldbook.Entry){func(e *worldbook.Entry) { e.ID = "../x" }, func(e *worldbook.Entry) { e.Name = "\n" }, func(e *worldbook.Entry) { e.Activation = "unknown" }, func(e *worldbook.Entry) { e.Keys = nil }, func(e *worldbook.Entry) { e.Keys = []string{""} }, func(e *worldbook.Entry) { e.Keys = []string{string([]byte{0xff})} }, func(e *worldbook.Entry) { e.Body = "\x00" }} {
		entry := base
		mutate(&entry)
		if err := worldbook.Validate(entry); err == nil {
			t.Errorf("accepted %+v", entry)
		}
	}
}

func TestNilIndexRefresh(t *testing.T) {
	workspace := tempWorkspace(t)
	write(t, workspace, "worldbook/magic/entry.md", validEntry)
	var previous *worldbook.Index
	next, changes, err := previous.Refresh(workspace)
	if err != nil || len(next.Documents()) != 1 || len(changes.Added) != 1 {
		t.Fatalf("nil refresh %+v %v", changes, err)
	}
	if documents := previous.Documents(); len(documents) != 0 {
		t.Fatal("nil Documents")
	}
}

func FuzzParseEntry(f *testing.F) {
	f.Add([]byte(validEntry))
	f.Add([]byte(entry("world", "always", 0, true, "[]")))
	f.Add([]byte("---\nkeys: [&a x, *a]\n---\n"))
	f.Fuzz(func(t *testing.T, content []byte) {
		parsed, err := worldbook.Parse(content, "entry")
		if err == nil {
			if err := worldbook.Validate(parsed); err != nil {
				t.Fatalf("Parse accepted invalid entry: %v", err)
			}
		}
	})
}

func TestWorldbookRootSymlinkRejected(t *testing.T) {
	workspace := tempWorkspace(t)
	source, _ := load(t, map[string]string{"worldbook/magic/entry.md": validEntry})
	if err := os.Symlink(filepath.Join(source, "worldbook"), filepath.Join(workspace, "worldbook")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := worldbook.Scan(workspace); err == nil {
		t.Fatal("followed worldbook root symlink")
	}
}
