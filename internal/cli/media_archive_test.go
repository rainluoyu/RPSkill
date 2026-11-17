package cli

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/character"
	"github.com/luoyu/rpskill/internal/timeline"
)

func phase6Call(t *testing.T, project string, args ...string) json.RawMessage {
	t.Helper()
	out, err := run(append(args, "--project", project, "--json")...)
	if err != nil {
		t.Fatalf("%v: %v; %s", args, err, out)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
func pngFixture(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "scene 中文.png")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	return file
}
func createPhase6Content(t *testing.T, project string) {
	t.Helper()
	dir := t.TempDir()
	card := filepath.Join(dir, "alice.md")
	os.WriteFile(card, []byte("---\nenabled: true\n---\n# Alice\n谨慎的剑士。\n"), 0600)
	phase6Call(t, project, "character", "create", "alice", "--file", card)
	entry := filepath.Join(dir, "history.md")
	os.WriteFile(entry, []byte("---\nenabled: true\nactivation: always\nkeys: []\npriority: 20\n---\n雾港拥有一座学院。\n"), 0600)
	phase6Call(t, project, "worldbook", "create", "world", "history", "--file", entry)
}
func parseMediaRef(t *testing.T, raw []byte) timeline.MediaReference {
	t.Helper()
	var ref timeline.MediaReference
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	if ref.ID == "" || ref.Path == "" || ref.MIME == "" {
		t.Fatal("missing media reference", string(raw))
	}
	return ref
}

func TestProjectCharacterAndWorldbookArchiveCommands(t *testing.T) {
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	source, _ := actualServer(t, nil)
	createPhase6Content(t, source)
	file := pngFixture(t)
	phase6Call(t, source, "character", "avatar", "alice", "--file", file)
	dir := t.TempDir()
	projectZIP, characterZIP, worldbookZIP := filepath.Join(dir, "project.zip"), filepath.Join(dir, "alice.zip"), filepath.Join(dir, "worldbook.zip")
	phase6Call(t, source, "project", "export", "--output", projectZIP)
	phase6Call(t, source, "character", "export", "alice", "--output", characterZIP)
	phase6Call(t, source, "worldbook", "export", "--category", "world", "--output", worldbookZIP)
	target, _ := actualServer(t, nil)
	phase6Call(t, target, "project", "import", "--file", projectZIP)
	var card character.Document
	json.Unmarshal(phase6Call(t, target, "character", "get", "alice"), &card)
	if card.Metadata.Avatar == "" || len(card.Assets) == 0 {
		t.Fatal("project ZIP lost character avatar")
	}
	phase6Call(t, target, "validate")
	onlyContent, _ := actualServer(t, nil)
	phase6Call(t, onlyContent, "character", "import", "--file", characterZIP)
	phase6Call(t, onlyContent, "worldbook", "import", "--file", worldbookZIP)
	phase6Call(t, onlyContent, "character", "get", "alice")
	phase6Call(t, onlyContent, "worldbook", "get", "world", "history")
	if _, err := run("project", "import", "--file", projectZIP, "--project", onlyContent); err == nil {
		t.Fatal("project import overwrote nonempty Workspace")
	}
	phase6Call(t, onlyContent, "validate")
}

func TestNativeMediaAndArchiveBinaryOutput(t *testing.T) {
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	p, _ := actualServer(t, nil)
	binary := buildCLI(t)
	file := pngFixture(t)
	invoke := func(args ...string) []byte {
		t.Helper()
		stdout, stderr, code := processCLI(binary, nil, append(args, "--project", p, "--json")...)
		if code != 0 {
			t.Fatalf("%v exit %d: %s %s", args, code, stdout, stderr)
		}
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
			t.Fatal("binary response polluted stdout", err)
		}
		return envelope.Data
	}
	ref := parseMediaRef(t, invoke("media", "upload", "--chat", "main", "--file", file))
	zip := filepath.Join(t.TempDir(), "native.zip")
	meta := invoke("project", "export", "--output", zip)
	if !strings.Contains(string(meta), `"mime":"application/zip"`) {
		t.Fatal("export did not return file receipt")
	}
	data, err := os.ReadFile(zip)
	if err != nil || !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatal("no actual ZIP file", err)
	}
	stdout, stderr, code := processCLI(binary, nil, "media", "download", ref.ID, "--project", p, "--output", zip, "--json")
	if code != 2 || stdout != "" || !strings.Contains(stderr, `"code":"usage_error"`) {
		t.Fatalf("unsafe output diagnostics: exit %d stdout=%q stderr=%s", code, stdout, stderr)
	}
	if after, _ := os.ReadFile(zip); !bytes.Equal(data, after) {
		t.Fatal("native download clobbered archive")
	}
	invoke("media", "delete", ref.ID)
	stdout, stderr, code = processCLI(binary, nil, "input", "--chat", "main", "--project", p, "--speaker", "director", "--media", ref.ID, "--json")
	if code != 1 || stdout != "" || !strings.Contains(stderr, `"code":"not_found"`) {
		t.Fatalf("missing media diagnostics: exit %d stdout=%q stderr=%s", code, stdout, stderr)
	}
}
