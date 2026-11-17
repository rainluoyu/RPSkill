package media

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/luoyu/rpskill/internal/character/contentio"
)

// DecodeZIP checks the central directory and actual decompressed bytes. No path
// is extracted before all entries, CRCs and total expanded size are validated.
func DecodeZIP(data []byte) (map[string][]byte, error) {
	if len(data) > MaxArchive {
		return nil, ErrTooLarge
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(reader.File) > MaxFiles {
		return nil, ErrTooLarge
	}
	files := map[string][]byte{}
	seen := map[string]bool{}
	dirs := map[string]bool{}
	total := uint64(0)
	for _, f := range reader.File {
		name := strings.TrimSuffix(f.Name, "/")
		if err := contentio.Relative(name); err != nil {
			return nil, err
		}
		folded := strings.ToLower(name)
		if seen[folded] {
			return nil, fmt.Errorf("duplicate ZIP path: %s", name)
		}
		seen[folded] = true
		if !f.Mode().IsRegular() && !f.Mode().IsDir() {
			return nil, errors.New("ZIP links/special files forbidden")
		}
		if f.Flags&1 != 0 {
			return nil, errors.New("encrypted ZIP forbidden")
		}
		if f.Mode().IsDir() {
			if !strings.HasSuffix(f.Name, "/") || f.UncompressedSize64 != 0 {
				return nil, errors.New("invalid ZIP directory")
			}
			dirs[folded] = true
			continue
		}
		if f.UncompressedSize64 > MaxUpload {
			return nil, ErrTooLarge
		}
		total += f.UncompressedSize64
		if total > MaxExpanded {
			return nil, ErrTooLarge
		}
		stream, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(stream, MaxUpload+1))
		closeErr := stream.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(body) > MaxUpload || uint64(len(body)) != f.UncompressedSize64 {
			return nil, ErrTooLarge
		}
		files[name] = body
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if seen[parent] && !dirs[parent] {
				return nil, fmt.Errorf("ZIP file/directory conflict: %s", parent)
			}
		}
	}
	if len(files) == 0 {
		return nil, errors.New("empty ZIP")
	}
	return files, nil
}
func EncodeZIP(files map[string][]byte) ([]byte, error) {
	if len(files) > MaxFiles {
		return nil, ErrTooLarge
	}
	names := make([]string, 0, len(files))
	total := 0
	for name, data := range files {
		if err := contentio.Relative(name); err != nil {
			return nil, err
		}
		if len(data) > MaxUpload {
			return nil, ErrTooLarge
		}
		total += len(data)
		if total > MaxExpanded {
			return nil, ErrTooLarge
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0644)
		out, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = out.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > MaxArchive {
		return nil, ErrTooLarge
	}
	return buffer.Bytes(), nil
}
