package media

import (
	"archive/zip"
	"bytes"
	"os"
	"testing"
)

func zipEntries(t *testing.T, names []string, symlink bool) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0644)
		if symlink {
			h.SetMode(os.ModeSymlink | 0777)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte("data"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestZIPRejectsUnsafeArchives(t *testing.T) {
	for _, names := range [][]string{{"../escape"}, {"/absolute"}, {"a\\b"}, {"C:/x"}, {"NUL.txt"}, {"a", "a"}, {"A", "a"}, {"a", "a/b"}, {"a/../b"}} {
		t.Run(names[0], func(t *testing.T) {
			if _, err := DecodeZIP(zipEntries(t, names, false)); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
		})
	}
	if _, err := DecodeZIP(zipEntries(t, []string{"link"}, true)); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestZIPRoundTripAndBounds(t *testing.T) {
	files := map[string][]byte{"worldbook/world/history.md": []byte("content"), "characters/alice/card.md": []byte("card")}
	raw, err := EncodeZIP(files)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeZIP(raw)
	if err != nil || !bytes.Equal(got["characters/alice/card.md"], files["characters/alice/card.md"]) {
		t.Fatalf("roundtrip %v", err)
	}
	if _, err := DecodeZIP([]byte("not a zip")); err == nil {
		t.Fatal("invalid zip accepted")
	}
	tooMany := map[string][]byte{}
	for i := 0; i < MaxFiles+1; i++ {
		tooMany[string(rune(1000+i))] = nil
	}
	if _, err := EncodeZIP(tooMany); err == nil {
		t.Fatal("too many files accepted")
	}
	corrupt := zipEntries(t, []string{"asset.bin"}, false)
	offset := bytes.Index(corrupt, []byte("data"))
	if offset < 0 {
		t.Fatal("fixture payload missing")
	}
	corrupt[offset] ^= 0xff
	if _, err := DecodeZIP(corrupt); err == nil {
		t.Fatal("CRC corrupted ZIP accepted")
	}

}
func TestMIMESafety(t *testing.T) {
	for _, data := range [][]byte{[]byte("<html><script>alert(1)</script></html>"), []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"), []byte("#!/bin/sh\necho unsafe")} {
		if Preview(Detect(data)) {
			t.Fatal("active content preview allowed")
		}
		if err := (Upload{Character: "alice", Avatar: true, Name: "image.png", Data: data}).Validate(); err == nil {
			t.Fatal("fake image accepted")
		}
	}
	for _, mime := range []string{"image/png", "audio/wave", "video/mp4", "video/webm"} {
		if !Preview(mime) {
			t.Fatalf("safe MIME %s disabled", mime)
		}
	}
}
func TestUploadValidation(t *testing.T) {
	for _, u := range []Upload{{Name: "a", Data: []byte("x")}, {Timeline: "main", Character: "alice", Name: "a", Data: []byte("x")}, {Timeline: "../x", Name: "a", Data: []byte("x")}, {Character: "director", Name: "a", Data: []byte("x")}, {Timeline: "main", Name: "../x", Data: []byte("x")}, {Timeline: "main", Name: "x", Expression: "happy", Data: []byte("x")}, {Timeline: "main", Name: "x", Avatar: true, Data: []byte("x")}} {
		if err := u.Validate(); err == nil {
			t.Errorf("invalid upload %+v", u)
		}
	}
}
