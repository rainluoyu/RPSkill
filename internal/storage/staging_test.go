package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLargeTransactionUsesSmallExternalManifest(t *testing.T) {
	workspace, runtime := testFS(t), testFS(t)
	// A payload at the existing per-file limit must not be base64-expanded into
	// a journal that exceeds that same limit.
	data := bytes.Repeat([]byte{0x6a}, MaxFileSize)
	w, err := workspace.PrepareWrite("chats/main/media/full.bin", data)
	if err != nil {
		t.Fatal(err)
	}
	j, err := saveStagedJournal(workspace, runtime, []Write{w})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := runtime.ReadFile(journalFile)
	if err != nil || len(manifest) > 4096 {
		t.Fatalf("manifest size=%d err=%v", len(manifest), err)
	}
	if _, err := workspace.ReadFile(w.Path); !os.IsNotExist(err) {
		t.Fatal("workspace changed before recovery", err)
	}
	if err := Recover(workspace, runtime); err != nil {
		t.Fatal(err)
	}
	got, err := workspace.ReadFile(w.Path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("large payload recovery", err)
	}
	if _, err := runtime.ReadDir(j.Stage); !os.IsNotExist(err) {
		t.Fatal("stage not removed", err)
	}
	if err := Recover(workspace, runtime); err != nil {
		t.Fatal(err)
	}
}
func TestStagedRecoveryPreflightsPayloadsAndTargets(t *testing.T) {
	for _, failure := range []string{"missing", "tampered", "target", "symlink"} {
		t.Run(failure, func(t *testing.T) {
			workspace, runtime := testFS(t), testFS(t)
			put(t, workspace, "a", []byte("old-a"))
			put(t, workspace, "b", []byte("old-b"))
			a, _ := workspace.PrepareWrite("a", []byte("new-a"))
			b, _ := workspace.PrepareWrite("b", []byte("new-b"))
			j, err := saveStagedJournal(workspace, runtime, []Write{a, b})
			if err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "missing":
				if err := runtime.Remove(j.Writes[1].Staged); err != nil {
					t.Fatal(err)
				}
			case "tampered":
				put(t, runtime, j.Writes[1].Staged, []byte("evil!"))
			case "target":
				put(t, workspace, "b", []byte("external"))
			case "symlink":
				if err := runtime.Remove(j.Writes[1].Staged); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(workspace.Root(), "b"), filepath.Join(runtime.Root(), j.Writes[1].Staged)); err != nil {
					t.Skip(err)
				}
			}
			if err := Recover(workspace, runtime); err == nil {
				t.Fatal("bad stage/target accepted")
			}
			assertFile(t, workspace, "a", []byte("old-a"))
			if pending, _ := Pending(runtime); !pending {
				t.Fatal("intent discarded")
			}
		})
	}
}
func TestStagedRecoveryAfterPartialApplyAndOrphanCleanup(t *testing.T) {
	workspace, runtime := testFS(t), testFS(t)
	a, _ := workspace.PrepareWrite("a", []byte("new-a"))
	b, _ := workspace.PrepareWrite("b", []byte("new-b"))
	j, err := saveStagedJournal(workspace, runtime, []Write{a, b})
	if err != nil {
		t.Fatal(err)
	}
	put(t, workspace, "a", a.Data)
	if err := Recover(workspace, runtime); err != nil {
		t.Fatal(err)
	}
	assertFile(t, workspace, "b", b.Data)
	// Crash before the intent was published: safe to drop staging, never apply it.
	orphan, err := stageJournal(workspace, runtime, []Write{b})
	if err != nil {
		t.Fatal(err)
	}
	if err := Recover(workspace, runtime); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{j.Stage, orphan.Stage} {
		if _, err := runtime.ReadDir(dir); !os.IsNotExist(err) {
			t.Fatal("orphan remains", err)
		}
	}
}
func TestStageMetadataCannotReadArbitraryRuntimeFiles(t *testing.T) {
	workspace, runtime := testFS(t), testFS(t)
	put(t, runtime, "secret", []byte("secret"))
	w, _ := workspace.PrepareWrite("a", []byte("value"))
	j, err := saveStagedJournal(workspace, runtime, []Write{w})
	if err != nil {
		t.Fatal(err)
	}
	j.Writes[0].Staged = "secret"
	raw, _ := json.Marshal(j)
	put(t, runtime, journalFile, raw)
	if err := Recover(workspace, runtime); err == nil {
		t.Fatal("arbitrary staged path accepted")
	}
	if _, err := workspace.ReadFile("a"); !os.IsNotExist(err) {
		t.Fatal("workspace mutated")
	}
}
func TestFileRemovalJournalRecoversAndRejectsReplacements(t *testing.T) {
	for _, mode := range []string{"before", "after", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			workspace, runtime := testFS(t), testFS(t)
			put(t, workspace, "asset", []byte("old"))
			put(t, workspace, "meta", []byte("before"))
			del, err := workspace.PrepareFileRemoval("asset")
			if err != nil {
				t.Fatal(err)
			}
			meta, _ := workspace.PrepareWrite("meta", []byte("after"))
			if _, err := saveStagedJournal(workspace, runtime, []Write{del, meta}); err != nil {
				t.Fatal(err)
			}
			if mode == "after" {
				if err := workspace.Remove("asset"); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "conflict" {
				put(t, workspace, "asset", []byte("external"))
			}
			err = Recover(workspace, runtime)
			if mode == "conflict" {
				if err == nil {
					t.Fatal("replacement deleted")
				}
				assertFile(t, workspace, "meta", []byte("before"))
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := workspace.ReadFile("asset"); !os.IsNotExist(err) {
				t.Fatal("file not removed", err)
			}
			assertFile(t, workspace, "meta", []byte("after"))
		})
	}
}
func TestTransactionLimitsBeforeWorkspaceMutation(t *testing.T) {
	workspace, runtime := testFS(t), testFS(t)
	if err := Commit(workspace, runtime, make([]Write, MaxTransactionWrites+1)); err == nil {
		t.Fatal("write count not limited")
	}
	large := bytes.Repeat([]byte{1}, MaxFileSize)
	writes := make([]Write, 9)
	for i := range writes {
		writes[i] = Write{Path: string(rune('a' + i)), Data: large}
	}
	if err := Commit(workspace, runtime, writes); err == nil {
		t.Fatal("aggregate size not limited")
	}
	entries, err := workspace.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Fatal("workspace changed", err)
	}
}

func TestOversizedPathComponentRejectedBeforeJournalPublication(t *testing.T) {
	workspace, runtime := testFS(t), testFS(t)
	name := strings.Repeat("a", 256)
	if _, err := workspace.PrepareWrite(name, []byte("x")); err == nil {
		t.Fatal("oversized component accepted")
	}
	if err := Commit(workspace, runtime, []Write{{Path: name, Data: []byte("x")}}); err == nil {
		t.Fatal("unrecoverable target published")
	}
	if pending, err := Pending(runtime); err != nil || pending {
		t.Fatal("invalid target left pending journal", pending, err)
	}
	if err := ValidPath(strings.Repeat("a", 255)); err != nil {
		t.Fatal("portable boundary rejected", err)
	}
	if err := ValidPath(strings.Repeat("é", 128)); err == nil {
		t.Fatal("byte limit bypassed by unicode")
	}
}
