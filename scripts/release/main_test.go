package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVersionAndArchivePaths(t *testing.T) {
	for _, value := range []string{"1.0.0-rc.1", "1.2.3", "10.20.30-beta.2"} {
		if !versionPattern.MatchString(value) {
			t.Errorf("valid version rejected: %s", value)
		}
	}
	for _, value := range []string{"v1.0.0", "../1.0.0", "1.0.0;touch", "1.0", "1.0.0\n"} {
		if versionPattern.MatchString(value) {
			t.Errorf("unsafe version accepted: %q", value)
		}
	}
	for _, name := range []string{"", ".", "../escape", "a/../../b", "/root", "a//b", "a\\b", "C:/file", "a\x00b", "a\nb"} {
		if validArchivePath(name) {
			t.Errorf("unsafe member accepted: %q", name)
		}
	}
	if !validArchivePath("skill/rp/bin/rps.exe") {
		t.Fatal("normal member rejected")
	}
}
func testEntries(t target) []entry {
	var files []entry
	for _, name := range requiredDocs {
		files = append(files, entry{name, 0644, []byte("document")})
	}
	for _, name := range []string{"SKILL.md", "builtins/chat.md", "builtins/director.md", "references/workspace.md", "references/characters.md", "references/worldbook.md", "references/history.md", "references/narrative.md", "references/messages.md", "references/callback.md"} {
		files = append(files, entry{"skill/rp/" + name, 0644, []byte("skill")})
	}
	return append(files, entry{t.binary(), 0755, []byte("binary")}, entry{"skill/rp/bin/" + t.binary(), 0755, []byte("binary")}, entry{"VERSION", 0644, []byte(defaultVersion + "\n")}, entry{"MANIFEST.json", 0644, []byte("{}")}, entry{"examples/harbor-academy/rp.yaml", 0644, []byte("name: example")}, entry{"examples/harbor-academy/chats/main/history.jsonl", 0644, nil})
}
func TestPackageRequiredFilesAndModes(t *testing.T) {
	for _, platform := range platforms {
		t.Run(platform.key(), func(t *testing.T) {
			files := testEntries(platform)
			if err := validateEntries(files, platform, defaultVersion); err != nil {
				t.Fatal(err)
			}
			cases := map[string]func([]entry) []entry{
				"traversal":      func(f []entry) []entry { return append(f, entry{"../escape", 0644, nil}) },
				"case_collision": func(f []entry) []entry { return append(f, entry{"license", 0644, nil}) },
				"missing_license": func(f []entry) []entry {
					var out []entry
					for _, file := range f {
						if file.name != "LICENSE" {
							out = append(out, file)
						}
					}
					return out
				},
				"missing_exec_bit": func(f []entry) []entry {
					for i := range f {
						if f[i].name == platform.binary() {
							f[i].mode = 0644
						}
					}
					return f
				},
				"stale_skill_binary": func(f []entry) []entry {
					for i := range f {
						if f[i].name == "skill/rp/bin/"+platform.binary() {
							f[i].data = []byte("old binary")
						}
					}
					return f
				},
				"extra_skill_binary": func(f []entry) []entry { return append(f, entry{"skill/rp/bin/other.exe", 0755, []byte("other")}) },
				"node_modules":       func(f []entry) []entry { return append(f, entry{"web/node_modules/library.js", 0644, nil}) },
				"evidence":           func(f []entry) []entry { return append(f, entry{"docs/evidence/log.txt", 0644, nil}) },
				"cache":              func(f []entry) []entry { return append(f, entry{"examples/tools/__pycache__/asset.pyc", 0644, nil}) },
				"nonstandard_skill": func(f []entry) []entry {
					for i := range f {
						if f[i].name == "skill/rp/references/callback.md" {
							f[i].name = "skill/rp/assistant.md"
						}
					}
					return f
				},
				"version": func(f []entry) []entry {
					for i := range f {
						if f[i].name == "VERSION" {
							f[i].data = []byte("dev\n")
						}
					}
					return f
				},
			}
			for name, mutate := range cases {
				t.Run(name, func(t *testing.T) {
					if err := validateEntries(mutate(append([]entry{}, files...)), platform, defaultVersion); err == nil {
						t.Fatal("invalid package accepted")
					}
				})
			}
		})
	}
}
func TestArchiveRoundTripIsDeterministic(t *testing.T) {
	epoch := time.Unix(1700000000, 0).UTC()
	for _, windows := range []bool{false, true} {
		t.Run(map[bool]string{true: "zip", false: "tar.gz"}[windows], func(t *testing.T) {
			ext := ".tar.gz"
			if windows {
				ext = ".zip"
			}
			files := []entry{{"rps", 0755, []byte("binary")}, {"skill/rp/SKILL.md", 0644, []byte("instructions")}, {"history.jsonl", 0644, nil}}
			first := filepath.Join(t.TempDir(), "first"+ext)
			second := filepath.Join(t.TempDir(), "second"+ext)
			for _, name := range []string{first, second} {
				if err := writeArchive(name, "bundle", files, epoch, windows); err != nil {
					t.Fatal(err)
				}
			}
			a, _ := fileDigest(first)
			b, _ := fileDigest(second)
			if a != b {
				t.Fatal("same inputs produced different archive hash")
			}
			got, err := readArchive(first, "bundle")
			if err != nil {
				t.Fatal(err)
			}
			for i := range got {
				if len(got[i].data) == 0 && len(files[i].data) == 0 {
					got[i].data = files[i].data
				}
			}
			if !reflect.DeepEqual(got, files) {
				t.Fatalf("roundtrip mismatch: %#v", got)
			}
		})
	}
}
func TestRejectUnsafeArchiveMembers(t *testing.T) {
	for _, kind := range []string{"traversal", "root", "symlink", "duplicate", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			for _, windows := range []bool{false, true} {
				name := filepath.Join(t.TempDir(), "bad.tar.gz")
				if windows {
					name = filepath.Join(t.TempDir(), "bad.zip")
				}
				f, err := os.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				member := "bundle/file"
				mode := os.FileMode(0644)
				size := int64(1)
				switch kind {
				case "traversal":
					member = "bundle/../escape"
				case "root":
					member = "other/file"
				case "symlink":
					mode = os.ModeSymlink | 0777
				case "oversize":
					size = 33 << 20
				}
				if windows {
					w := zip.NewWriter(f)
					h := &zip.FileHeader{Name: member, Method: zip.Store}
					h.SetMode(mode)
					out, err := w.CreateHeader(h)
					if err != nil {
						t.Fatal(err)
					}
					if kind == "oversize" {
						_, err = out.Write(make([]byte, size))
					} else {
						_, err = out.Write([]byte("x"))
					}
					if err != nil {
						t.Fatal(err)
					}
					if kind == "duplicate" {
						duplicate := *h
						out, err = w.CreateHeader(&duplicate)
						if err != nil {
							t.Fatal(err)
						}
						out.Write([]byte("x"))
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					gz := gzip.NewWriter(f)
					w := tar.NewWriter(gz)
					h := &tar.Header{Name: member, Mode: int64(mode.Perm()), Typeflag: tar.TypeReg, Size: size}
					if kind == "symlink" {
						h.Typeflag = tar.TypeSymlink
						h.Linkname = "/etc/passwd"
						h.Size = 0
					}
					if err := w.WriteHeader(h); err != nil {
						t.Fatal(err)
					}
					if kind != "symlink" {
						if kind == "oversize" {
							w.Write(make([]byte, size))
						} else {
							w.Write([]byte("x"))
						}
					}
					if kind == "duplicate" {
						w.WriteHeader(h)
						w.Write([]byte("x"))
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
					if err := gz.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err := readArchive(name, "bundle"); err == nil {
					t.Fatal("unsafe archive accepted")
				}
			}
		})
	}
}
func TestBuildTimeAndPlatformEnvironment(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	got, err := buildTime()
	if err != nil || got.Unix() != 1700000000 {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"yesterday", "-1", "0", "99999999999"} {
		t.Setenv("SOURCE_DATE_EPOCH", bad)
		if _, err := buildTime(); err == nil {
			t.Errorf("invalid epoch accepted: %s", bad)
		}
	}
	t.Setenv("GOOS", "other")
	t.Setenv("GOARCH", "other")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GOAMD64", "v4")
	t.Setenv("GOARM64", "v9.5")
	env := platformEnvironment(target{"windows", "arm64"})
	var found = map[string]string{}
	for _, value := range env {
		key, value, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "GO") || key == "CGO_ENABLED" {
			if _, ok := found[key]; ok {
				t.Fatalf("duplicate env key %s", key)
			}
			found[key] = value
		}
	}
	if found["GOOS"] != "windows" || found["GOARCH"] != "arm64" || found["CGO_ENABLED"] != "0" || found["GOAMD64"] != "v1" || found["GOARM64"] != "v8.0" {
		t.Fatal(found)
	}
}
func TestExecutableArchitectureInspection(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateBinary(self, target{runtime.GOOS, runtime.GOARCH}); err != nil {
		t.Fatal(err)
	}
	wrong := "arm64"
	if runtime.GOARCH == "arm64" {
		wrong = "amd64"
	}
	if err := validateBinary(self, target{runtime.GOOS, wrong}); err == nil {
		t.Fatal("wrong architecture accepted")
	}
}
func TestMissingLicenseBlocksBeforeBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("README.md", []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}
	err := buildRelease(defaultVersion, "dist", "missing-go", "missing-npm")
	if err == nil || !strings.Contains(err.Error(), "LICENSE") {
		t.Fatalf("missing license did not fail before invoking build tool: %v", err)
	}
	if _, err := os.Stat("dist"); !os.IsNotExist(err) {
		t.Fatalf("failed preflight created dist: %v", err)
	}
}
func TestManifestRecordsHashModeAndSize(t *testing.T) {
	r := record(entry{"bin/rps", 0755, []byte("abc")})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != "0755" || r.Size != 3 || r.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || !strings.Contains(string(data), `"bytes":3`) {
		t.Fatal(string(data))
	}
}

func TestInstalledDependencyLicenseAudit(t *testing.T) {
	if os.Getenv("RPS_TEST_RELEASE_LICENSES") != "1" {
		t.Skip("set RPS_TEST_RELEASE_LICENSES=1 to audit installed Go/npm licensing texts")
	}
	t.Chdir("../..")
	files, err := dependencyLicenses("go", "npm")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		if !validArchivePath(file.name) || seen[strings.ToLower(file.name)] || len(file.data) == 0 {
			t.Fatalf("invalid licensing entry %s", file.name)
		}
		seen[strings.ToLower(file.name)] = true
	}
	if !seen["third_party_notices.md"] {
		t.Fatal("notices index missing")
	}
	for _, prefix := range []string{"licenses/go/github.com/inconshreveable/mousetrap@", "licenses/go/github.com/spf13/cobra@", "licenses/npm/dompurify@", "licenses/npm/vue@", "licenses/toolchain/go@"} {
		found := false
		for name := range seen {
			if strings.HasPrefix(name, prefix) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing dependency license %s", prefix)
		}
	}
	t.Logf("preserved %d dependency license/notice assets", len(files))
}
