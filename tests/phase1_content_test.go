package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/worldbook"
)

func p1Card(id string) []byte {
	return []byte(fmt.Sprintf("---\nenabled: true\n---\n\n# Background\nA cautious traveler.\n"))
}
func p1Book(id, activation string, priority int, keys string) []byte {
	return []byte(fmt.Sprintf("---\nenabled: true\nactivation: %s\nkeys: %s\npriority: %d\n---\n\nPRIVATE_BODY_MUST_NOT_BE_IN_HINTS\n", activation, keys, priority))
}
func p1PNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestPhase1WorldbookActivationAndIsolation(t *testing.T) {
	dir := t.TempDir()
	p1Write(t, dir, "worldbook/world/history.md", p1Book("history", "always", 100, "[]"))
	p1Write(t, dir, "worldbook/magic/system.md", p1Book("magic", "keyword", 50, "[魔法, MAGIC]"))
	index, err := worldbook.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, speaker, text string
		settings            worldbook.Settings
		want                []string
	}{
		{"keyword_dedup", "director", "MAGIC 魔法 MAGIC", worldbook.Settings{}, []string{"history", "system"}},
		{"ordinary_character", "alice", "magic", worldbook.Settings{}, []string{"history", "system"}},
		{"Human_default", "chat", "魔法", worldbook.Settings{}, []string{"history", "system"}},
		{"category_disabled", "director", "魔法", worldbook.Settings{Categories: map[string]bool{"magic": false}}, []string{"history"}},
		{"always_category_disabled", "director", "魔法", worldbook.Settings{Categories: map[string]bool{"world": false}}, []string{"system"}},
		{"entry_disabled", "director", "魔法", worldbook.Settings{Entries: map[string]bool{"magic/system": false}}, []string{"history"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refs, err := index.Activate(worldbook.Message{Type: "input", Speaker: c.speaker, Text: c.text}, nil, c.settings)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, r := range refs {
				ids = append(ids, r.ID)
				if len(r.SHA256) != 64 || !strings.HasPrefix(r.Path, "worldbook/") {
					t.Fatalf("bad hint: %+v", r)
				}
			}
			if !reflect.DeepEqual(ids, c.want) {
				t.Fatalf("got %v want %v", ids, c.want)
			}
			data, _ := json.Marshal(refs)
			if bytes.Contains(data, []byte("PRIVATE_BODY")) {
				t.Fatal("full body leaked into hints")
			}
		})
	}
	recent := []worldbook.Message{{Type: "output", Kind: "story", Text: "魔法"}, {Type: "input", Speaker: "chat", Text: "discussion"}, {Type: "output", Kind: "assistant", Text: "discussion"}}
	refs, err := index.Activate(worldbook.Message{Type: "input", Speaker: "director", Text: "quiet"}, recent, worldbook.Settings{ScanRecent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatal("recent story scan incorrectly counted OOC messages")
	}
	refs, err = index.Activate(worldbook.Message{Type: "input", Speaker: "director", Text: "quiet"}, recent[1:], worldbook.Settings{ScanRecent: 10})
	if err != nil || len(refs) != 1 {
		t.Fatal("OOC activated worldbook")
	}
	// Reusing the immutable index after a disabled-line lookup must not leak it to another line.
	refs, err = index.Activate(worldbook.Message{Type: "input", Speaker: "director", Text: "magic"}, nil, worldbook.Settings{})
	if err != nil || len(refs) != 2 {
		t.Fatal("per-line override mutated global index")
	}
}
func TestPhase1WorldbookRefreshAndFrontmatter(t *testing.T) {
	dir := t.TempDir()
	p1Write(t, dir, "worldbook/world/a.md", p1Book("a", "always", 1, "[]"))
	index, err := worldbook.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := index.Documents()
	p1Write(t, dir, "worldbook/world/a.md", append(p1Book("a", "always", 1, "[]"), []byte("changed")...))
	p1Write(t, dir, "worldbook/world/b.md", p1Book("b", "always", 2, "[]"))
	next, changes, err := index.Refresh(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes.Modified, []string{"worldbook/world/a.md"}) || !reflect.DeepEqual(changes.Added, []string{"worldbook/world/b.md"}) {
		t.Fatalf("bad refresh %+v", changes)
	}
	if !reflect.DeepEqual(index.Documents(), old) {
		t.Fatal("refresh mutated previous snapshot")
	}
	if err := os.Remove(filepath.Join(dir, "worldbook/world/a.md")); err != nil {
		t.Fatal(err)
	}
	_, changes, err = next.Refresh(dir)
	if err != nil || !reflect.DeepEqual(changes.Removed, []string{"worldbook/world/a.md"}) {
		t.Fatalf("bad removal %+v %v", changes, err)
	}
	for name, data := range map[string][]byte{
		"duplicate_field": bytes.Replace(p1Book("a", "always", 1, "[]"), []byte("enabled: true"), []byte("enabled: true\nenabled: false"), 1),
		"wrong_type":      bytes.Replace(p1Book("a", "always", 1, "[]"), []byte("enabled: true"), []byte("enabled: \"true\""), 1),
		"missing_keys":    bytes.Replace(p1Book("a", "always", 1, "[]"), []byte("keys: []\n"), nil, 1),
		"empty_keyword":   p1Book("a", "keyword", 1, "[\"\"]"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := worldbook.Parse(data, "entry"); err == nil {
				t.Fatal("invalid frontmatter accepted")
			}
		})
	}
}
func TestPhase1CharacterAvatarAndOverrides(t *testing.T) {
	dir := t.TempDir()
	card := bytes.Replace(p1Card("alice"), []byte("enabled: true"), []byte("enabled: true\navatar: avatar.png"), 1)
	p1Write(t, dir, "characters/alice/card.md", card)
	avatar := p1PNG(t)
	p1Write(t, dir, "characters/alice/avatar.png", avatar)
	p1Write(t, dir, "characters/alice/expressions/happy.png", avatar)
	docs, err := character.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Metadata.ID != "alice" || len(docs[0].Assets) != 2 {
		t.Fatalf("bad documents %+v", docs)
	}
	got, err := character.ReadAsset(dir, "alice", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if got.MIME != "image/png" || !bytes.Equal(got.Data, avatar) {
		t.Fatal("avatar bytes/MIME changed")
	}
	off := character.Effective(docs, map[string]bool{"alice": false})
	on := character.Effective(docs, nil)
	if off[0].Enabled || !on[0].Enabled || !docs[0].Metadata.Enabled {
		t.Fatal("timeline override leaked to base/other line")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "characters/alice/card.md"))
	if !bytes.Equal(after, card) {
		t.Fatal("read/effective mutated card")
	}
}
func TestPhase1ContentSafety(t *testing.T) {
	dir := t.TempDir()
	p1Write(t, dir, "characters/alice/card.md", p1Card("alice"))
	p1Write(t, dir, "characters/alice/avatar.png", p1PNG(t))
	for _, name := range []string{"../card.md", "../../rp.yaml", "/tmp/outside", `C:\outside`, "card.md"} {
		t.Run(name, func(t *testing.T) {
			if _, err := character.ReadAsset(dir, "alice", name); err == nil {
				t.Fatal("unsafe asset read accepted")
			}
		})
	}
	p1Write(t, dir, "characters/alice/expressions/fake.png", []byte("<svg onload='alert(1)'/>"))
	if _, err := character.Scan(dir); err == nil {
		t.Fatal("active fake expression accepted")
	}
	if err := os.Remove(filepath.Join(dir, "characters/alice/expressions/fake.png")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"chat", "director", "story", "assistant", "narrator", "environment"} {
		if _, err := character.Parse(p1Card(id), id); err == nil {
			t.Errorf("reserved role %s accepted", id)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	sentinel := filepath.Join(t.TempDir(), "sentinel.png")
	if err := os.WriteFile(sentinel, p1PNG(t), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(dir, "characters/alice/escape.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := character.ReadAsset(dir, "alice", "escape.png"); err == nil {
		t.Fatal("symbolic asset read allowed")
	}
	if _, err := character.Scan(dir); err == nil {
		t.Fatal("symbolic asset scan allowed")
	}
}
