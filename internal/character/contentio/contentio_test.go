package contentio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableRelative(t *testing.T) {
	for _, name := range []string{"", "/absolute", "../x", "a/../x", "a/./x", "a//x", `a\b`, "a:b", "a.", "a ", "a\x00b", "a\nb", "CON", "NUL.png", "PRN.txt", "COM1", "LPT9.png", "COM¹.txt", "LPT²", "a?b", "a*b", "a|b", "a<b", "a>b", "a\"b"} {
		if err := Relative(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"avatar.png", "expressions/快乐.png", "readme.txt", "company.txt", "com10.txt", "0/a_b-c.jpg"} {
		if err := Relative(name); err != nil {
			t.Errorf("rejected %q: %v", name, err)
		}
	}
}

func TestReadFileBoundedAndConfined(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := ReadFile(root, "file", 4); err == nil {
		t.Fatal("accepted oversized file")
	}
	data, err := ReadFile(root, "file", 5)
	if err != nil || string(data) != "12345" {
		t.Fatalf("read: %q %v", data, err)
	}
	for _, name := range []string{"../file", "/etc/passwd", ".", "missing"} {
		if _, err := ReadFile(root, name, 100); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
}

func TestFrontmatterRejectsShapeAndAliases(t *testing.T) {
	cases := []string{
		"---\n[]\n---\n",
		"---\n&root\nid: a\n---\n",
		"---\nid: &id a\n---\n",
		"---\nid: !custom a\n---\n",
		"---\nid: null\n---\n",
		"---\n1: x\n---\n",
		"---\nid: a\nid: b\n---\n",
		"---\nid: a\nkeys: [x, 1]\n---\n",
		"---\nid: a\nkeys: [x, &k y]\n---\n",
		"---\nid: a\nkeys: [x, *k]\n---\n",
	}
	for _, source := range cases {
		var target struct {
			ID   string   `yaml:"id"`
			Keys []string `yaml:"keys"`
		}
		if _, err := Frontmatter([]byte(source), &target, map[string]string{"id": "string", "keys": "strings"}, []string{"id"}); err == nil {
			t.Errorf("accepted: %s", strings.ReplaceAll(source, "\n", " / "))
		}
	}
}

func TestDirectoryAndHash(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "a", "b", "file"), []byte("text"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	nested, err := Directory(root, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ReadDir(nested)
	if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
		t.Fatalf("ReadDir: %v %v", entries, err)
	}
	nested.Close()
	if _, err := Directory(root, "../a"); err == nil {
		t.Fatal("accepted directory traversal")
	}
	if _, err := Directory(root, "a/missing"); err == nil {
		t.Fatal("accepted missing directory")
	}
	if _, err := Directory(root, "a/b/file"); err == nil {
		t.Fatal("accepted file as directory")
	}
	if _, err := ReadFile(root, "a/b/file", 4); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(root, "a/missing/file", 4); err == nil {
		t.Fatal("accepted missing parent")
	}
	if _, err := ReadFile(root, "a", 4); err == nil {
		t.Fatal("accepted directory as file")
	}
	if !ValidID("valid-id") || ValidID("Invalid") {
		t.Fatal("ID validation")
	}
	if hash := Hash([]byte("abc")); hash != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("SHA256 %s", hash)
	}
}

func TestStrictDecodeAndMissingFields(t *testing.T) {
	tests := []struct {
		source   string
		kinds    map[string]string
		required []string
	}{
		{"---\nid: [\n---\n", map[string]string{"id": "string"}, []string{"id"}},
		{"---\nid: a\nunknown: true\n---\n", map[string]string{"id": "string"}, []string{"id"}},
		{"---\nname: a\n---\n", map[string]string{"name": "string"}, []string{"id"}},
		{"---\nid: a\n---\n", map[string]string{"id": "unsupported"}, []string{"id"}},
		{"---\nid: a\n---\n", map[string]string{"id": "string"}, []string{"id"}},
	}
	for i, tc := range tests {
		var target struct {
			Other string `yaml:"other"`
		}
		if _, err := Frontmatter([]byte(tc.source), &target, tc.kinds, tc.required); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}
