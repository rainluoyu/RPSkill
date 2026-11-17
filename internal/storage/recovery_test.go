package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testFS(t *testing.T) *FS {
	t.Helper()
	f, err := OpenFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
func put(t *testing.T, f *FS, name string, data []byte) {
	t.Helper()
	if err := f.AtomicWrite(name, data); err != nil {
		t.Fatal(err)
	}
}
func assertFile(t *testing.T, f *FS, name string, want []byte) {
	t.Helper()
	got, err := f.ReadFile(name)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q, want %q (%v)", name, got, want, err)
	}
}
func saveJournal(t *testing.T, workspace, runtime *FS, writes ...Write) {
	t.Helper()
	raw, err := json.Marshal(Journal{Version: 1, Project: workspace.Root(), Writes: writes})
	if err != nil {
		t.Fatal(err)
	}
	put(t, runtime, journalFile, raw)
}

func TestRecoveryCompletesPartialAppendAndEmptyFile(t *testing.T) {
	for _, suffix := range []string{"", "{", "{\"n\":2}\n"} {
		t.Run(suffix, func(t *testing.T) {
			workspace, runtime := testFS(t), testFS(t)
			prefix, next := []byte("{\"n\":1}\n"), []byte("{\"n\":2}\n")
			put(t, workspace, "history.jsonl", prefix)
			appendWrite, err := workspace.PrepareAppend("history.jsonl", next)
			if err != nil {
				t.Fatal(err)
			}
			emptyWrite, err := workspace.PrepareWrite("child/history.jsonl", []byte{})
			if err != nil {
				t.Fatal(err)
			}
			saveJournal(t, workspace, runtime, appendWrite, emptyWrite)
			put(t, workspace, "history.jsonl", append(append([]byte{}, prefix...), suffix...))
			if err := Recover(workspace, runtime); err != nil {
				t.Fatal(err)
			}
			assertFile(t, workspace, "history.jsonl", append(prefix, next...))
			assertFile(t, workspace, "child/history.jsonl", []byte{})
			if pending, err := Pending(runtime); err != nil || pending {
				t.Fatalf("journal not cleared: %v %v", pending, err)
			}
			if err := Recover(workspace, runtime); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecoveryPreflightsAllFilesBeforeAnyMutation(t *testing.T) {
	workspace, runtime := testFS(t), testFS(t)
	put(t, workspace, "a.yaml", []byte("old-a"))
	put(t, workspace, "b.yaml", []byte("old-b"))
	a, _ := workspace.PrepareWrite("a.yaml", []byte("new-a"))
	b, _ := workspace.PrepareWrite("b.yaml", []byte("new-b"))
	saveJournal(t, workspace, runtime, a, b)
	put(t, workspace, "b.yaml", []byte("external-edit"))
	if err := Recover(workspace, runtime); err == nil {
		t.Fatal("external edit ignored")
	}
	assertFile(t, workspace, "a.yaml", []byte("old-a"))
	assertFile(t, workspace, "b.yaml", []byte("external-edit"))
	if pending, _ := Pending(runtime); !pending {
		t.Fatal("conflicting intent discarded")
	}
	// After an explicit external conflict resolution, retry safely rolls forward.
	put(t, workspace, "b.yaml", []byte("old-b"))
	if err := Recover(workspace, runtime); err != nil {
		t.Fatal(err)
	}
	assertFile(t, workspace, "a.yaml", []byte("new-a"))
	assertFile(t, workspace, "b.yaml", []byte("new-b"))
}

func TestAppendRefusesUncommittedTailWithoutMutation(t *testing.T) {
	f := testFS(t)
	original := []byte("{\"n\":1}\n{\"n\":")
	put(t, f, "history.jsonl", original)
	if err := f.AppendJSONL("history.jsonl", []byte(`{"n":2}`)); err == nil {
		t.Fatal("append merged with an incomplete line")
	}
	assertFile(t, f, "history.jsonl", original)
}

func TestRuntimeWritesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits; Windows ACL tested natively later")
	}
	workspace := testFS(t)
	root, err := OpenRuntimeFS(workspace.Root(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	put(t, root, "recovery/tail.bin", []byte("private"))
	for path, mode := range map[string]os.FileMode{".": 0700, "recovery": 0700, "recovery/tail.bin": 0600} {
		info, err := os.Stat(filepath.Join(root.Root(), path))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("private permissions %s: %v %v", path, info, err)
		}
	}
}

func TestRemovalJournalRecoversBeforeAndAfterStaging(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(fmt.Sprint(staged), func(t *testing.T) {
			workspace, runtime := testFS(t), testFS(t)
			put(t, workspace, "characters/alice/card.md", []byte("card"))
			put(t, workspace, "characters/alice/nested/avatar.png", []byte("media"))
			put(t, workspace, "sentinel", []byte("unchanged"))
			w, err := workspace.PrepareTreeRemoval("characters/alice")
			if err != nil {
				t.Fatal(err)
			}
			saveJournal(t, workspace, runtime, w)
			if staged {
				if err := workspace.root.Rename(w.Path, w.Trash); err != nil {
					t.Fatal(err)
				}
				if err := workspace.root.Remove(w.Trash + "/card.md"); err != nil {
					t.Fatal(err)
				}
			}
			if err := Recover(workspace, runtime); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{w.Path, w.Trash} {
				if _, err := workspace.root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("deletion was not recovered: %s %v", name, err)
				}
			}
			assertFile(t, workspace, "sentinel", []byte("unchanged"))
			if pending, _ := Pending(runtime); pending {
				t.Fatal("removal journal remains")
			}
		})
	}
}
