//go:build phase7

package tests

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type p7Package struct {
	Target    string `json:"target"`
	Archive   string `json:"archive"`
	Bytes     int64  `json:"bytes"`
	SHA       string `json:"sha256"`
	BinarySHA string `json:"binary_sha256"`
}
type p7File struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Mode  string `json:"mode"`
	SHA   string `json:"sha256"`
}
type p7Entry struct {
	data []byte
	mode os.FileMode
}

func p7Hash(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func p7Read(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func p7Decode(t *testing.T, b []byte, v any) {
	t.Helper()
	if e := json.Unmarshal(b, v); e != nil {
		t.Fatal(e)
	}
}
func p7Archive(t *testing.T, file, root string) map[string]p7Entry {
	t.Helper()
	entries := map[string]p7Entry{}
	add := func(name string, mode os.FileMode, r io.Reader) {
		if !strings.HasPrefix(name, root+"/") {
			t.Fatalf("wrong archive root %q", name)
		}
		name = strings.TrimPrefix(name, root+"/")
		if name == "" || path.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || !mode.IsRegular() {
			t.Fatalf("unsafe release member %q %v", name, mode)
		}
		if _, ok := entries[name]; ok {
			t.Fatalf("duplicate package member %s", name)
		}
		b, e := io.ReadAll(io.LimitReader(r, 64<<20+1))
		if e != nil || len(b) > 64<<20 {
			t.Fatalf("invalid member %s %v", name, e)
		}
		entries[name] = p7Entry{b, mode.Perm()}
	}
	if strings.HasSuffix(file, ".zip") {
		z, e := zip.OpenReader(file)
		if e != nil {
			t.Fatal(e)
		}
		defer z.Close()
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			add(f.Name, f.Mode(), r)
			r.Close()
		}
	} else {
		f, e := os.Open(file)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		g, e := gzip.NewReader(f)
		if e != nil {
			t.Fatal(e)
		}
		defer g.Close()
		tr := tar.NewReader(g)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal(e)
			}
			if h.Typeflag == tar.TypeDir {
				continue
			}
			if h.Typeflag != tar.TypeReg {
				t.Fatalf("nonregular member %s", h.Name)
			}
			add(h.Name, h.FileInfo().Mode(), tr)
		}
	}
	return entries
}
func TestPhase7DistributionArchiveIntegrityAndPlatforms(t *testing.T) {
	release := os.Getenv("RPS_P7_RELEASE")
	if release == "" {
		t.Skip("set RPS_P7_RELEASE to frozen release directory")
	}
	repo, e := filepath.Abs("..")
	if e != nil {
		t.Fatal(e)
	}
	raw := p7Read(t, filepath.Join(release, "release.json"))
	var manifest struct {
		Version  string      `json:"version"`
		Status   string      `json:"status"`
		Source   string      `json:"source_sha256"`
		Packages []p7Package `json:"packages"`
	}
	p7Decode(t, raw, &manifest)
	if manifest.Version != "1.0.0-rc.1" || !strings.Contains(manifest.Status, "candidate") {
		t.Fatalf("not a clearly labelled candidate: %+v", manifest)
	}
	inputs := p7Read(t, filepath.Join(release, "build-inputs.sha256"))
	if p7Hash(inputs) != manifest.Source {
		t.Fatal("release source manifest digest differs")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(inputs)), "\n") {
		hash, name, ok := strings.Cut(line, "  ")
		if !ok || len(hash) != 64 || !filepath.IsLocal(name) {
			t.Fatalf("invalid build source entry %q", line)
		}
		if p7Hash(p7Read(t, filepath.Join(repo, name))) != hash {
			t.Fatalf("release source no longer matches %s", name)
		}
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(p7Read(t, filepath.Join(release, "SHA256SUMS")))), "\n") {
		hash, name, ok := strings.Cut(line, "  ")
		if !ok || !filepath.IsLocal(name) {
			t.Fatal("invalid checksum line")
		}
		if _, ok := sums[name]; ok {
			t.Fatal("duplicate outer checksum")
		}
		sums[name] = hash
		if p7Hash(p7Read(t, filepath.Join(release, name))) != hash {
			t.Fatalf("outer SHA mismatch %s", name)
		}
	}
	if len(sums) != 8 || len(manifest.Packages) != 6 {
		t.Fatal("expected six packages plus release/source manifests")
	}
	wanted := map[string]bool{"linux-amd64": false, "linux-arm64": false, "darwin-amd64": false, "darwin-arm64": false, "windows-amd64": false, "windows-arm64": false}
	for _, pkg := range manifest.Packages {
		t.Run(pkg.Target, func(t *testing.T) {
			if seen, ok := wanted[pkg.Target]; !ok || seen {
				t.Fatal("unexpected or duplicate target")
			}
			wanted[pkg.Target] = true
			file := filepath.Join(release, pkg.Archive)
			b := p7Read(t, file)
			if int64(len(b)) != pkg.Bytes || p7Hash(b) != pkg.SHA || sums[pkg.Archive] != pkg.SHA {
				t.Fatal("package outer integrity mismatch")
			}
			root := "rpskill-" + manifest.Version + "-" + pkg.Target
			entries := p7Archive(t, file, root)
			var pm struct {
				Version string   `json:"version"`
				Target  string   `json:"target"`
				Source  string   `json:"source_sha256"`
				Files   []p7File `json:"files"`
			}
			p7Decode(t, entries["MANIFEST.json"].data, &pm)
			if pm.Version != manifest.Version || pm.Target != pkg.Target || pm.Source != manifest.Source || len(pm.Files)+1 != len(entries) {
				t.Fatal("package manifest association/count mismatch")
			}
			listed := map[string]bool{}
			for _, item := range pm.Files {
				entry, ok := entries[item.Path]
				if !ok || listed[item.Path] || int64(len(entry.data)) != item.Bytes || p7Hash(entry.data) != item.SHA || fmt.Sprintf("%04o", entry.mode) != item.Mode {
					t.Fatalf("manifest mismatch %s", item.Path)
				}
				listed[item.Path] = true
			}
			bin := "rps"
			if strings.HasPrefix(pkg.Target, "windows-") {
				bin += ".exe"
			}
			executable := entries[bin]
			if p7Hash(executable.data) != pkg.BinarySHA || !bytes.Equal(executable.data, entries["skill/rp/bin/"+bin].data) {
				t.Fatal("Skill/main/release executable hashes differ")
			}
			for _, name := range []string{"README.md", "LICENSE", "README.zh-CN.md", "docs/INSTALL.md", "docs/TROUBLESHOOTING.md", "skill/rp/SKILL.md", "skill/rp/builtins/chat.md", "skill/rp/builtins/director.md"} {
				if !bytes.Equal(entries[name].data, p7Read(t, filepath.Join(repo, name))) {
					t.Fatalf("package asset mismatch %s", name)
				}
			}
			for name, entry := range entries {
				if strings.HasPrefix(name, "skill/rp/") && !strings.Contains(name, "/bin/") {
					if !bytes.Equal(entry.data, p7Read(t, filepath.Join(repo, name))) {
						t.Fatalf("Skill text mismatch %s", name)
					}
				}
			}
			if string(entries["VERSION"].data) != manifest.Version+"\n" {
				t.Fatal("VERSION file mismatch")
			}
			info, err := buildinfo.Read(bytes.NewReader(executable.data))
			if err != nil {
				t.Fatal(err)
			}
			settings := map[string]string{}
			for _, s := range info.Settings {
				settings[s.Key] = s.Value
			}
			parts := strings.Split(pkg.Target, "-")
			if settings["GOOS"] != parts[0] || settings["GOARCH"] != parts[1] || settings["CGO_ENABLED"] != "0" || settings["-trimpath"] != "true" {
				t.Fatalf("binary build metadata differs %+v", settings)
			}
			// Go intentionally omits -ldflags from build info with -trimpath (issue
			// 52372). Prove the version/link flags by independently rebuilding every
			// target and comparing full binary bytes, not by dropping the assertion.
			// Non-native results are never executed; native runtime version is also
			// checked by the release smoke command using the extracted binary.
			out := filepath.Join(t.TempDir(), bin)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			args := []string{"build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X github.com/luoyu/rpskill/internal/server.Version=" + manifest.Version, "-o", out, "./cmd/rps"}
			rebuild := exec.CommandContext(ctx, "go", args...)
			rebuild.Dir = repo
			for _, value := range os.Environ() {
				key, _, _ := strings.Cut(value, "=")
				if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" && key != "GOAMD64" && key != "GOARM64" {
					rebuild.Env = append(rebuild.Env, value)
				}
			}
			rebuild.Env = append(rebuild.Env, "GOOS="+parts[0], "GOARCH="+parts[1], "CGO_ENABLED=0", "GOAMD64=v1", "GOARM64=v8.0")
			t.Logf("independent rebuild %s: go %q", pkg.Target, args)
			if log, err := rebuild.CombinedOutput(); err != nil {
				t.Fatalf("independent rebuild failed: %v\n%s", err, log)
			}
			if !bytes.Equal(executable.data, p7Read(t, out)) {
				t.Fatal("release differs from independent versioned rebuild; use the frozen release toolchain and source")
			}
			switch parts[0] {
			case "linux":
				f, err := elf.NewFile(bytes.NewReader(executable.data))
				if err != nil {
					t.Fatal(err)
				}
				want := elf.EM_X86_64
				if parts[1] == "arm64" {
					want = elf.EM_AARCH64
				}
				if f.Machine != want {
					t.Fatal("wrong ELF arch")
				}
				for _, p := range f.Progs {
					if p.Type == elf.PT_INTERP {
						t.Fatal("Linux release requires dynamic interpreter")
					}
				}
				f.Close()
			case "darwin":
				f, err := macho.NewFile(bytes.NewReader(executable.data))
				if err != nil {
					t.Fatal(err)
				}
				want := macho.CpuAmd64
				if parts[1] == "arm64" {
					want = macho.CpuArm64
				}
				if f.Cpu != want {
					t.Fatal("wrong Mach-O arch")
				}
				f.Close()
			case "windows":
				f, err := pe.NewFile(bytes.NewReader(executable.data))
				if err != nil {
					t.Fatal(err)
				}
				want := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
				if parts[1] == "arm64" {
					want = pe.IMAGE_FILE_MACHINE_ARM64
				}
				if f.Machine != want {
					t.Fatal("wrong PE arch")
				}
				f.Close()
			}
			if pkg.Target == "linux-amd64" {
				for _, p := range []string{"build/rps", "skill/rp/bin/rps"} {
					if !bytes.Equal(executable.data, p7Read(t, filepath.Join(repo, p))) {
						t.Fatalf("native package differs from %s", p)
					}
				}
				if extract := os.Getenv("RPS_P7_EXTRACT"); extract != "" {
					for name, entry := range entries {
						p := filepath.Join(extract, name)
						if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(p, entry.data, entry.mode); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			t.Logf("%s sha256=%s; format/metadata only for non-native targets", pkg.Target, pkg.BinarySHA)
		})
	}
}
