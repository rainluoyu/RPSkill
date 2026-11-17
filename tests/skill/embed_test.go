package skill_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"testing/fstest"

	"github.com/luoyu/rpskill"
)

func TestFilesContainsOnlyInstallableText(t *testing.T) {
	want := []string{
		"SKILL.md",
		"builtins/director.md",
		"builtins/chat.md",
		"references/callback.md",
		"references/characters.md",
		"references/messages.md",
		"references/narrative.md",
		"references/history.md",
		"references/workspace.md",
		"references/worldbook.md",
	}
	files := rpskill.SkillFiles()
	if err := fstest.TestFS(files, want...); err != nil {
		t.Fatal(err)
	}
	var got []string
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			t.Errorf("embedded path is not a regular file: %s", name)
		}
		got = append(got, name)
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(data)) == 0 {
			t.Errorf("empty instructions: %s", name)
		}
		source, err := os.ReadFile(filepath.Join("..", "..", "skill", "rp", filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if !bytes.Equal(data, source) {
			t.Errorf("embedded text differs from source: %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("embedded files = %v; want only %v", got, want)
	}
}

func TestFilesDoesNotExposeBinaryOrDevelopmentFiles(t *testing.T) {
	for _, name := range []string{
		"bin", "bin/rps", "bin/rps.exe", "bin/rps.here",
		"rp/SKILL.md", "PHASE0.md", "PHASE1.md", "PHASE2.md", "PHASE6.md",
		"embed.go", "embed_test.go", "narrative_test.go", "tests/test_structure.py",
		"tests/narrative_cases.json", "tests/wirepeer/main.go",
		"../go.mod", "../../go.mod", "/rp/SKILL.md",
	} {
		t.Run(name, func(t *testing.T) {
			if file, err := rpskill.SkillFiles().Open(name); err == nil {
				file.Close()
				t.Fatalf("unexpected embedded path exposed: %s", name)
			}
		})
	}
}
