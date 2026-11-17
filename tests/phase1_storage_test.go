package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyu/rpskill/internal/storage"
)

func p1FS(t *testing.T) (*storage.FS, string) {
	t.Helper()
	dir := t.TempDir()
	f, err := storage.OpenFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f, dir
}
func p1Write(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func TestPhase1StrictJSON(t *testing.T) {
	for name, data := range map[string][]byte{
		"duplicate": []byte(`{"a":1,"a":2}`), "nested": []byte(`{"a":{"b":1,"b":2}}`),
		"escaped_duplicate": []byte(`{"a":1,"\u0061":2}`), "trailing": []byte(`{} {}`),
		"invalid_UTF8": {0x7b, 0x22, 0x61, 0x22, 0x3a, 0x22, 0xff, 0x22, 0x7d},
	} {
		t.Run(name, func(t *testing.T) {
			var v any
			if err := storage.DecodeJSON(data, &v); err == nil {
				t.Fatal("malformed JSON accepted")
			}
		})
	}
	var v struct {
		N json.Number `json:"n"`
	}
	if err := storage.DecodeJSON([]byte(`{"n":9007199254740993}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.N.String() != "9007199254740993" {
		t.Fatal("number precision lost")
	}
	if err := storage.DecodeJSON([]byte(`{"n":1,"extra":true}`), &v); err == nil {
		t.Fatal("unknown struct field accepted")
	}
}
func TestPhase1StrictYAML(t *testing.T) {
	for name, data := range map[string]string{"duplicate": "a: 1\na: 2\n", "nested": "a:\n  b: 1\n  b: 2\n", "alias": "a: &x 1\nb: *x\n", "merge": "a: &x {b: 1}\nc: {<<: *x}\n", "multidocument": "a: 1\n---\na: 2\n"} {
		t.Run(name, func(t *testing.T) {
			var v any
			if err := storage.DecodeYAML([]byte(data), &v); err == nil {
				t.Fatal("malformed YAML accepted")
			}
		})
	}
}
func TestPhase1JSONLTailClassification(t *testing.T) {
	prefix := []byte("{\"n\":1}\n{\"n\":2}\n")
	for name, tail := range map[string][]byte{"none": nil, "complete_no_newline": []byte(`{"n":3}`), "partial": []byte(`{"n":`), "UTF8_cut": {0x7b, 0x22, 0x74, 0x22, 0x3a, 0x22, 0xe4, 0xb8}} {
		t.Run(name, func(t *testing.T) {
			f, dir := p1FS(t)
			all := append(append([]byte{}, prefix...), tail...)
			p1Write(t, dir, "history.jsonl", all)
			got, err := f.ReadJSONL("history.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Lines) != 2 || got.TailOffset != int64(len(prefix)) || !bytes.Equal(got.Tail, tail) {
				t.Fatalf("incorrect tail classification: %+v", got)
			}
			for i, line := range got.Lines {
				if line.Number != i+1 {
					t.Fatal("line numbers not 1-based")
				}
			}
			after, err := os.ReadFile(filepath.Join(dir, "history.jsonl"))
			if err != nil || !bytes.Equal(after, all) {
				t.Fatal("read modified input")
			}
		})
	}
	for name, body := range map[string][]byte{"interior": []byte("{}\nBROKEN\n{}\n"), "duplicate_key": []byte("{\"a\":1,\"a\":2}\n"), "blank": []byte("{}\n\n"), "UTF8": {0xff, '\n'}, "over_limit": append(bytes.Repeat([]byte{'x'}, storage.MaxEventSize+1), '\n')} {
		t.Run(name, func(t *testing.T) {
			f, dir := p1FS(t)
			p1Write(t, dir, "history.jsonl", body)
			if _, err := f.ReadJSONL("history.jsonl"); err == nil {
				t.Fatal("corrupt/oversized line accepted")
			}
		})
	}
}
func TestPhase1StorageRejectsEscapeAndSymlinks(t *testing.T) {
	f, dir := p1FS(t)
	for _, name := range []string{"../sentinel", "/tmp/sentinel", "a/../../sentinel", `C:\outside`, `\\server\share`, "a//b", "a/./b", "CON", "a/NUL.txt", "a/trailing.", "a/trailing "} {
		t.Run(name, func(t *testing.T) {
			if storage.ValidPath(name) == nil {
				t.Fatal("unsafe path accepted")
			}
			if err := f.AtomicWrite(name, []byte("bad")); err == nil {
				t.Fatal("unsafe write accepted")
			}
		})
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows link privileges not assumed; Unix escape tests above still apply")
	}
	outside := t.TempDir()
	p1Write(t, outside, "sentinel", []byte("unchanged"))
	for _, target := range []string{"file-link", "dir-link/sentinel"} {
		t.Run(target, func(t *testing.T) {
			if target == "file-link" {
				if err := os.Symlink(filepath.Join(outside, "sentinel"), filepath.Join(dir, "file-link")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(outside, filepath.Join(dir, "dir-link")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.ReadFile(target); err == nil {
				t.Error("symlink read accepted")
			}
			if err := f.AtomicWrite(target, []byte("bad")); err == nil {
				t.Error("symlink write accepted")
			}
			if err := f.Remove(target); err == nil {
				t.Error("symlink remove accepted")
			}
			after, err := os.ReadFile(filepath.Join(outside, "sentinel"))
			if err != nil || string(after) != "unchanged" {
				t.Fatalf("sentinel touched: %s %v", after, err)
			}
		})
	}
}
func TestPhase1AtomicReplacementNoTornReaders(t *testing.T) {
	f, _ := p1FS(t)
	old := bytes.Repeat([]byte("old\n"), 8192)
	newData := bytes.Repeat([]byte("new\n"), 16384)
	if err := f.AtomicWrite("config.md", old); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 60; j++ {
				data, err := f.ReadFile("config.md")
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(data, old) && !bytes.Equal(data, newData) {
					errs <- fmt.Errorf("torn read of %d bytes", len(data))
					return
				}
			}
		}()
	}
	for i := 0; i < 40; i++ {
		data := old
		if i%2 == 1 {
			data = newData
		}
		if err := f.AtomicWrite("config.md", data); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	entries, err := f.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".rps-write-") {
			t.Fatal("temporary file leaked")
		}
	}
}

// This worker is launched only by TestPhase1ConfirmedAppendsSurviveProcessKill.
// An ACK is printed only after AppendJSONL returns from fsync.
func TestPhase1CrashWorker(t *testing.T) {
	if os.Getenv("RPS_QA_CRASH_WORKER") != "1" {
		t.Skip("helper process only")
		return
	}
	dir := os.Getenv("RPS_QA_CRASH_DIR")
	f, err := storage.OpenFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for n := 1; ; n++ {
		data := []byte(fmt.Sprintf(`{"n":%d,"text":%q}`, n, strings.Repeat("中文正文", 500)))
		if err := f.AppendJSONL("history.jsonl", data); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("ACK %d\n", n)
	}
}
func TestPhase1ConfirmedAppendsSurviveProcessKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows process termination requires separate authorized run")
	}
	for round := 0; round < 3; round++ {
		t.Run(strconv.Itoa(round), func(t *testing.T) {
			f, dir := p1FS(t)
			p1Write(t, dir, "history.jsonl", nil)
			cmd := exec.Command(os.Args[0], "-test.run=^TestPhase1CrashWorker$")
			cmd.Env = append(os.Environ(), "RPS_QA_CRASH_WORKER=1", "RPS_QA_CRASH_DIR="+dir)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			ack := make(chan int, 1)
			go func() {
				s := bufio.NewScanner(stdout)
				n := 0
				for s.Scan() {
					if strings.HasPrefix(s.Text(), "ACK ") {
						n, _ = strconv.Atoi(strings.TrimPrefix(s.Text(), "ACK "))
						if n >= 20 {
							ack <- n
							return
						}
					}
				}
				ack <- n
			}()
			confirmed := 0
			select {
			case confirmed = <-ack:
			case <-time.After(10 * time.Second):
				t.Fatal("child stalled before ACK")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			if confirmed < 20 {
				t.Fatalf("child failed early: %s", stderr.String())
			}
			got, err := f.ReadJSONL("history.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Lines) < confirmed {
				t.Fatalf("lost acknowledged appends: confirmed %d, found %d", confirmed, len(got.Lines))
			}
			for i, line := range got.Lines {
				var v struct {
					N    int    `json:"n"`
					Text string `json:"text"`
				}
				if err := json.Unmarshal(line.Data, &v); err != nil {
					t.Fatal(err)
				}
				if v.N != i+1 {
					t.Fatal("append sequence corrupt")
				}
			}
			t.Logf("process-kill smoke only: %d confirmed, %d complete, %d tail bytes; not a power-loss proof", confirmed, len(got.Lines), len(got.Tail))
		})
	}
}

func TestPhase1JSONLSizeBoundaryRoundTrip(t *testing.T) {
	for _, size := range []int{storage.MaxEventSize - 1, storage.MaxEventSize} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			f, dir := p1FS(t)
			p1Write(t, dir, "history.jsonl", nil)
			data := []byte(`{"x":"` + strings.Repeat("x", size-len(`{"x":""}`)) + `"}`)
			if len(data) != size {
				t.Fatal("fixture size wrong")
			}
			if err := f.AppendJSONL("history.jsonl", data); err != nil {
				after, _ := os.ReadFile(filepath.Join(dir, "history.jsonl"))
				if len(after) != 0 {
					t.Fatal("rejected append changed file")
				}
				t.Logf("boundary safely rejected: %v", err)
				return
			}
			got, err := f.ReadJSONL("history.jsonl")
			if err != nil {
				t.Fatalf("successful append cannot be read back (%d payload bytes): %v", size, err)
			}
			if len(got.Lines) != 1 || !bytes.Equal(got.Lines[0].Data, data) {
				t.Fatal("accepted append did not roundtrip")
			}
		})
	}
}

// Journal fixtures model an interrupted Commit at its observable durable states.
// They are tied to the current storage layout, not fake power-loss recordings.
func TestPhase1JournalRollForwardAndConflict(t *testing.T) {
	for _, mode := range []string{"before_any_write", "after_first_write", "conflict_second_target", "wrong_project", "traversal"} {
		t.Run(mode, func(t *testing.T) {
			f, dir := p1FS(t)
			rt, _ := p1FS(t)
			p1Write(t, dir, "a.yaml", []byte("old-a"))
			p1Write(t, dir, "b.yaml", []byte("old-b"))
			a, err := f.PrepareWrite("a.yaml", []byte("new-a"))
			if err != nil {
				t.Fatal(err)
			}
			b, err := f.PrepareWrite("b.yaml", []byte("new-b"))
			if err != nil {
				t.Fatal(err)
			}
			j := storage.Journal{Version: 1, Project: f.Root(), Writes: []storage.Write{a, b}}
			switch mode {
			case "after_first_write":
				if err := f.AtomicWrite("a.yaml", a.Data); err != nil {
					t.Fatal(err)
				}
			case "conflict_second_target":
				p1Write(t, dir, "b.yaml", []byte("external-change"))
			case "wrong_project":
				j.Project = t.TempDir()
			case "traversal":
				j.Writes[0].Path = "../outside"
			}
			raw, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.AtomicWrite("store-transaction.json", raw); err != nil {
				t.Fatal(err)
			}
			beforeA, _ := f.ReadFile("a.yaml")
			beforeB, _ := f.ReadFile("b.yaml")
			err = storage.Recover(f, rt)
			if mode == "conflict_second_target" || mode == "wrong_project" || mode == "traversal" {
				if err == nil {
					t.Fatal("invalid/conflicting journal recovered")
				}
				afterA, _ := f.ReadFile("a.yaml")
				afterB, _ := f.ReadFile("b.yaml")
				if !bytes.Equal(beforeA, afterA) || !bytes.Equal(beforeB, afterB) {
					t.Fatal("rejected recovery partially changed targets")
				}
				pending, e := storage.Pending(rt)
				if e != nil || !pending {
					t.Fatal("failed recovery lost journal evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			afterA, _ := f.ReadFile("a.yaml")
			afterB, _ := f.ReadFile("b.yaml")
			if string(afterA) != "new-a" || string(afterB) != "new-b" {
				t.Fatal("roll-forward incomplete")
			}
			pending, e := storage.Pending(rt)
			if e != nil || pending {
				t.Fatal("completed journal still pending")
			}
			if err := storage.Recover(f, rt); err != nil {
				t.Fatal("repeat recovery not idempotent")
			}
		})
	}
}
