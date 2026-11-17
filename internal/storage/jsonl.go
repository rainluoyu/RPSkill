package storage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

const MaxEventSize = 4 << 20

type JSONLine struct {
	Number    int
	Data      []byte
	EndOffset int64
}
type JSONL struct {
	Lines      []JSONLine
	Tail       []byte
	TailOffset int64
}

// ReadJSONL streams with an explicit per-event limit. A trailing unterminated
// line is returned separately and is never silently accepted or discarded.
func (f *FS) ReadJSONL(name string) (JSONL, error) {
	var result JSONL
	if err := f.check(name, false); err != nil {
		return result, err
	}
	file, err := f.root.Open(name)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() {
		return result, errors.New("JSONL must be a regular file")
	}
	r := bufio.NewReaderSize(file, 64<<10)
	var buffer []byte
	var offset int64
	for {
		fragment, err := r.ReadSlice('\n')
		buffer = append(buffer, fragment...)
		if len(buffer) > MaxEventSize+2 {
			return result, fmt.Errorf("event line %d exceeds size limit", len(result.Lines)+1)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if len(buffer) > 0 && buffer[len(buffer)-1] == '\n' {
			raw := bytes.TrimSuffix(buffer, []byte{'\n'})
			raw = bytes.TrimSuffix(raw, []byte{'\r'})
			if len(raw) > MaxEventSize {
				return result, fmt.Errorf("event line %d exceeds size limit", len(result.Lines)+1)
			}
			if len(bytes.TrimSpace(raw)) == 0 || !utf8.Valid(raw) {
				return result, fmt.Errorf("invalid JSONL line %d", len(result.Lines)+1)
			}
			var value any
			if e := DecodeJSON(raw, &value); e != nil {
				return result, fmt.Errorf("line %d: %w", len(result.Lines)+1, e)
			}
			result.Lines = append(result.Lines, JSONLine{Number: len(result.Lines) + 1, Data: append([]byte(nil), raw...), EndOffset: offset + int64(len(buffer))})
			offset += int64(len(buffer))
			buffer = nil
		}
		if errors.Is(err, io.EOF) {
			result.Tail = append([]byte(nil), buffer...)
			result.TailOffset = offset
			return result, nil
		}
		if err != nil {
			return result, err
		}
	}
}
func (f *FS) AppendJSONL(name string, data []byte) error {
	if len(data) > MaxEventSize || bytes.ContainsAny(data, "\r\n") {
		return errors.New("JSONL append must be one bounded line")
	}
	var value any
	if err := DecodeJSON(data, &value); err != nil {
		return err
	}
	if err := f.check(name, false); err != nil {
		return err
	}
	file, err := f.root.OpenFile(name, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("JSONL must be a regular file")
	}
	if info.Size() > 0 {
		var last [1]byte
		if _, err := file.ReadAt(last[:], info.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			return errors.New("cannot append after an uncommitted JSONL tail")
		}
	}
	line := append(append([]byte(nil), data...), '\n')
	n, err := file.Write(line)
	if err != nil {
		return err
	}
	if n != len(line) {
		return io.ErrShortWrite
	}
	return file.Sync()
}
func (f *FS) TruncateJSONL(name string, size int64) error {
	if err := f.check(name, false); err != nil {
		return err
	}
	file, err := f.root.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Truncate(size); err != nil {
		return err
	}
	return file.Sync()
}

func (f *FS) PrepareAppend(name string, data []byte) (Write, error) {
	if len(data) > MaxEventSize+1 {
		return Write{}, errors.New("transaction append exceeds event limit")
	}
	if err := f.check(name, false); err != nil {
		return Write{}, err
	}
	file, err := f.root.Open(name)
	if err != nil {
		return Write{}, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return Write{}, err
	}
	return Write{Path: name, BeforeHash: hex.EncodeToString(hash.Sum(nil)), Data: append([]byte(nil), data...), Append: true, Offset: size}, nil
}
func (f *FS) verifyAppend(w Write) error {
	if err := f.check(w.Path, false); err != nil {
		return err
	}
	file, err := f.root.Open(w.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < w.Offset || info.Size() > w.Offset+int64(len(w.Data)) {
		return errors.New("append journal size conflict")
	}
	hash := sha256.New()
	if _, err := io.CopyN(hash, file, w.Offset); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != w.BeforeHash {
		return errors.New("append journal prefix changed")
	}
	suffix, err := io.ReadAll(io.LimitReader(file, int64(len(w.Data))+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(suffix, w.Data[:len(suffix)]) {
		return errors.New("append journal tail conflict")
	}
	return nil
}
func (f *FS) finishAppend(w Write) error {
	if err := f.verifyAppend(w); err != nil {
		return err
	}
	file, err := f.root.OpenFile(w.Path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	// Reset only the known, journaled suffix; never truncate acknowledged prefix.
	if err := file.Truncate(w.Offset); err != nil {
		return err
	}
	if _, err := file.Seek(w.Offset, io.SeekStart); err != nil {
		return err
	}
	if _, err := file.Write(w.Data); err != nil {
		return err
	}
	return file.Sync()
}
