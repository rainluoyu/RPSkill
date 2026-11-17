package storage

import (
	"bytes"
	"testing"
)

func TestAtomicHistoryRewriteExceedsGeneralFileLimit(t *testing.T) {
	f, err := OpenFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := append([]byte(`{"text":"`), bytes.Repeat([]byte("x"), 1<<20)...)
	line = append(line, []byte("\"}\n")...)
	data := bytes.Repeat(line, 33)
	if err := f.AtomicWriteJSONL("history.jsonl", data); err != nil {
		t.Fatal(err)
	}
	log, err := f.ReadJSONL("history.jsonl")
	if err != nil || len(log.Lines) != 33 || len(log.Tail) != 0 {
		t.Fatal("large rewrite not recoverable", err)
	}
	if err := f.AtomicWriteJSONL("history.jsonl", data[:len(data)-1]); err == nil {
		t.Fatal("uncommitted tail accepted")
	}
	if err := f.AtomicWriteJSONL("history.jsonl", append(bytes.Repeat([]byte("x"), MaxEventSize+1), '\n')); err == nil {
		t.Fatal("oversized line accepted")
	}
	log, err = f.ReadJSONL("history.jsonl")
	if err != nil || len(log.Lines) != 33 {
		t.Fatal("rejected rewrite changed history", err)
	}
}
