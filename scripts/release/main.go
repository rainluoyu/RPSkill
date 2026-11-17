// Command release builds and verifies portable candidate distribution archives.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/luoyu/rpskill"
)

const defaultVersion = "1.0.0-rc.1"
const maxPackageBytes = 128 << 20

var platforms = []target{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}}
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)
var standardSkillTexts = []string{"SKILL.md", "builtins/chat.md", "builtins/director.md", "references/workspace.md", "references/characters.md", "references/worldbook.md", "references/history.md", "references/narrative.md", "references/messages.md", "references/callback.md"}
var requiredDocs = []string{"README.md", "LICENSE", "README.zh-CN.md", "docs/INSTALL.md", "docs/TROUBLESHOOTING.md"}

type target struct{ OS, Arch string }

func (t target) key() string { return t.OS + "-" + t.Arch }
func (t target) binary() string {
	if t.OS == "windows" {
		return "rps.exe"
	}
	return "rps"
}
func (t target) archive(version string) string {
	ext := ".tar.gz"
	if t.OS == "windows" {
		ext = ".zip"
	}
	return "rpskill-" + version + "-" + t.key() + ext
}

type fileRecord struct {
	Path   string `json:"path"`
	Size   int64  `json:"bytes"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}
type packageRecord struct {
	Target       string `json:"target"`
	Archive      string `json:"archive"`
	Size         int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	BinarySHA256 string `json:"binary_sha256"`
}
type packageManifest struct {
	Version      string       `json:"version"`
	Status       string       `json:"status"`
	Target       string       `json:"target"`
	BuiltAt      string       `json:"built_at"`
	SourceSHA256 string       `json:"source_sha256"`
	Files        []fileRecord `json:"files"`
}
type releaseManifest struct {
	Version      string          `json:"version"`
	Status       string          `json:"status"`
	BuiltAt      string          `json:"built_at"`
	GoVersion    string          `json:"go_version"`
	SourceSHA256 string          `json:"source_sha256"`
	Packages     []packageRecord `json:"packages"`
}
type entry struct {
	name string
	mode fs.FileMode
	data []byte
}

func main() {
	if os.Getenv("RPS_RELEASE_PROTOCOL_FIXTURE") == "1" {
		protocolFixture()
		return
	}
	version := flag.String("version", defaultVersion, "Candidate version (no leading v)")
	output := flag.String("output", "dist", "Parent directory; the version subdirectory must not exist")
	goBin := flag.String("go", "go", "Go executable")
	npmBin := flag.String("npm", "npm", "npm executable for installed dependency license discovery")
	verify := flag.String("verify", "", "Verify an existing version directory instead of building")
	smoke := flag.String("smoke", "", "Verify and smoke-test a Linux package outside the source tree")
	flag.Parse()
	var err error
	switch {
	case flag.NArg() != 0:
		err = errors.New("unexpected positional arguments")
	case *verify != "" && *smoke != "":
		err = errors.New("use only one of --verify or --smoke")
	case *verify != "":
		err = verifyRelease(*verify)
	case *smoke != "":
		err = smokeRelease(*smoke)
	default:
		err = buildRelease(*version, *output, *goBin, *npmBin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func buildRelease(version, output, goBin, npmBin string) error {
	if !versionPattern.MatchString(version) {
		return errors.New("version must be a safe semantic version without a leading v")
	}
	for _, name := range requiredDocs {
		info, err := os.Lstat(name)
		if err != nil {
			return fmt.Errorf("required distribution document %s: %w", name, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("required document %s must be a nonempty regular file", name)
		}
	}
	if _, err := os.Stat("internal/server/ui/index.html"); err != nil {
		return errors.New("missing embedded frontend; run make release to build it first")
	}
	assets, err := distributionAssets()
	if err != nil {
		return err
	}
	licenses, err := dependencyLicenses(goBin, npmBin)
	if err != nil {
		return err
	}
	inputs, err := buildInputs(assets)
	if err != nil {
		return err
	}
	sourceHash := digest(inputs)
	epoch, err := buildTime()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	destination := filepath.Join(output, version)
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release destination must not exist: %s", destination)
	}
	stage, err := os.MkdirTemp(output, ".release-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	goVersion, err := exec.Command(goBin, "version").Output()
	if err != nil {
		return fmt.Errorf("Go version: %w", err)
	}
	manifest := releaseManifest{Version: version, Status: "candidate; not proof of real Harness authentication, literary adherence or native OS validation", BuiltAt: epoch.Format(time.RFC3339), GoVersion: strings.TrimSpace(string(goVersion)), SourceSHA256: sourceHash}
	for _, platform := range platforms {
		binaryPath := filepath.Join(stage, "build-"+platform.key())
		args := []string{"build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X github.com/luoyu/rpskill/internal/server.Version=" + version, "-o", binaryPath, "./cmd/rps"}
		cmd := exec.Command(goBin, args...)
		cmd.Env = platformEnvironment(platform)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("Building %s %s\n", version, platform.key())
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build %s: %w", platform.key(), err)
		}
		binary, err := os.ReadFile(binaryPath)
		if err != nil {
			return err
		}
		if err := validateBinary(binaryPath, platform); err != nil {
			return err
		}
		files := append([]entry{}, assets...)
		files = append(files, licenses...)
		files = append(files, entry{platform.binary(), 0755, binary}, entry{"skill/rp/bin/" + platform.binary(), 0755, binary})
		files = append(files, entry{"VERSION", 0644, []byte(version + "\n")})
		sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
		pm := packageManifest{Version: version, Status: manifest.Status, Target: platform.key(), BuiltAt: manifest.BuiltAt, SourceSHA256: sourceHash}
		for _, file := range files {
			pm.Files = append(pm.Files, record(file))
		}
		pmBytes, err := json.MarshalIndent(pm, "", "  ")
		if err != nil {
			return err
		}
		files = append(files, entry{"MANIFEST.json", 0644, append(pmBytes, '\n')})
		if err := validateEntries(files, platform, version); err != nil {
			return err
		}
		archiveName := platform.archive(version)
		if err := writeArchive(filepath.Join(stage, archiveName), strings.TrimSuffix(archiveName, archiveExt(platform)), files, epoch, platform.OS == "windows"); err != nil {
			return err
		}
		info, err := os.Stat(filepath.Join(stage, archiveName))
		if err != nil {
			return err
		}
		hash, err := fileDigest(filepath.Join(stage, archiveName))
		if err != nil {
			return err
		}
		manifest.Packages = append(manifest.Packages, packageRecord{platform.key(), archiveName, info.Size(), hash, digest(binary)})
		if err := os.Remove(binaryPath); err != nil {
			return err
		}
	}
	after, err := buildInputs(assets)
	if err != nil {
		return err
	}
	// Re-read packaged files too: a concurrent documentation/Skill update must
	// never produce packages that disagree with the captured build inputs.
	assetsAfter, err := distributionAssets()
	if err != nil {
		return err
	}
	for i := range assets {
		if i >= len(assetsAfter) || assets[i].name != assetsAfter[i].name || digest(assets[i].data) != digest(assetsAfter[i].data) {
			return errors.New("distribution assets changed during build; freeze inputs and rebuild")
		}
	}
	if len(assets) != len(assetsAfter) || string(after) != string(inputs) {
		return errors.New("build inputs changed during cross build; freeze source and rebuild")
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "release.json"), append(manifestBytes, '\n'), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "build-inputs.sha256"), inputs, 0644); err != nil {
		return err
	}
	var sums strings.Builder
	for _, pkg := range manifest.Packages {
		fmt.Fprintf(&sums, "%s  %s\n", pkg.SHA256, pkg.Archive)
	}
	for _, name := range []string{"release.json", "build-inputs.sha256"} {
		hash, err := fileDigest(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%s  %s\n", hash, name)
	}
	if err := os.WriteFile(filepath.Join(stage, "SHA256SUMS"), []byte(sums.String()), 0644); err != nil {
		return err
	}
	if err := verifyRelease(stage); err != nil {
		return err
	}
	if err := os.Rename(stage, destination); err != nil {
		return err
	}
	fmt.Printf("Release ready: %s\n", destination)
	return nil
}
func archiveExt(t target) string {
	if t.OS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}
func buildTime() (time.Time, error) {
	value := os.Getenv("SOURCE_DATE_EPOCH")
	if value == "" {
		return time.Now().UTC().Truncate(time.Second), nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 315532800 || n > 4354819199 {
		return time.Time{}, errors.New("SOURCE_DATE_EPOCH must be Unix seconds in ZIP-compatible years 1980–2107")
	}
	return time.Unix(n, 0).UTC(), nil
}
func platformEnvironment(t target) []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" && key != "GOAMD64" && key != "GOARM64" {
			env = append(env, value)
		}
	}
	return append(env, "GOOS="+t.OS, "GOARCH="+t.Arch, "CGO_ENABLED=0", "GOAMD64=v1", "GOARM64=v8.0")
}
func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func fileDigest(name string) (string, error) {
	f, e := os.Open(name)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func record(file entry) fileRecord {
	return fileRecord{file.name, int64(len(file.data)), fmt.Sprintf("%04o", file.mode.Perm()), digest(file.data)}
}
func readRegular(name string) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("non-regular source: %s", name)
	}
	if info.Size() > 32<<20 {
		return nil, fmt.Errorf("source too large: %s", name)
	}
	return os.ReadFile(name)
}
func distributionAssets() ([]entry, error) {
	var files []entry
	for _, name := range requiredDocs {
		data, err := readRegular(name)
		if err != nil {
			return nil, err
		}
		files = append(files, entry{name, 0644, data})
	}
	for _, root := range []string{"examples/harbor-academy", "examples/assets", "examples/reading", "examples/tools"} {
		err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || strings.HasSuffix(name, ".pyc") {
				return nil
			}
			data, err := readRegular(name)
			if err != nil {
				return err
			}
			files = append(files, entry{filepath.ToSlash(name), 0644, data})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	data, err := readRegular("examples/README.md")
	if err != nil {
		return nil, err
	}
	files = append(files, entry{"examples/README.md", 0644, data})
	err = fs.WalkDir(rpskill.SkillFiles(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(rpskill.SkillFiles(), name)
		if err != nil {
			return err
		}
		onDisk, err := readRegular(filepath.Join("skill/rp", name))
		if err != nil {
			return err
		}
		if digest(data) != digest(onDisk) {
			return errors.New("embedded Skill differs from source; restart release command after source freeze")
		}
		files = append(files, entry{"skill/rp/" + name, 0644, data})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}
func buildInputs(assets []entry) ([]byte, error) {
	names := map[string]bool{"assets.go": true, "go.mod": true, "go.sum": true, "Makefile": true, "web/package.json": true, "web/package-lock.json": true, "web/index.html": true, "web/vite.config.ts": true, "web/tsconfig.json": true, "scripts/sync-web.sh": true}
	for _, file := range assets {
		names[file.name] = true
	}
	for _, root := range []string{"cmd", "internal", "skill", "scripts/release", "web/src", "web/config"} {
		err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if filepath.ToSlash(name) == "skill/rp/bin" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(name, ".go") || strings.HasPrefix(filepath.ToSlash(name), "internal/server/ui/") || strings.HasPrefix(filepath.ToSlash(name), "web/src/") || strings.HasPrefix(filepath.ToSlash(name), "web/config/") {
				names[filepath.ToSlash(name)] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	var sorted []string
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	var result strings.Builder
	for _, name := range sorted {
		data, err := readRegular(name)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&result, "%s  %s\n", digest(data), name)
	}
	return []byte(result.String()), nil
}
func validArchivePath(name string) bool {
	return name != "" && name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00\r\n")
}
func validateEntries(files []entry, t target, version string) error {
	seen := map[string]bool{}
	exact := map[string]bool{}
	var total int
	for _, file := range files {
		if !validArchivePath(file.name) || seen[strings.ToLower(file.name)] {
			return fmt.Errorf("unsafe or duplicate archive path %q", file.name)
		}
		seen[strings.ToLower(file.name)] = true
		exact[file.name] = true
		total += len(file.data)
		if total > maxPackageBytes {
			return errors.New("distribution exceeds 128MiB expanded limit")
		}
		if file.mode.Perm() != 0644 && file.mode.Perm() != 0755 {
			return fmt.Errorf("unexpected mode %s", file.name)
		}
	}
	for _, name := range append(append([]string{}, requiredDocs...), t.binary(), "skill/rp/SKILL.md", "skill/rp/builtins/chat.md", "skill/rp/builtins/director.md", "skill/rp/bin/"+t.binary(), "examples/harbor-academy/rp.yaml", "examples/harbor-academy/chats/main/history.jsonl", "VERSION", "MANIFEST.json") {
		if !exact[name] {
			return fmt.Errorf("missing required package member %s", name)
		}
	}
	for _, name := range standardSkillTexts {
		if !exact["skill/rp/"+name] {
			return fmt.Errorf("missing standard Skill text %s", name)
		}
	}
	var bin []byte
	var skillBin []byte
	skillTexts := 0
	executables := 0
	for _, file := range files {
		if file.name == "VERSION" && string(file.data) != version+"\n" {
			return errors.New("package VERSION mismatch")
		}
		if file.name == t.binary() {
			bin = file.data
		}
		if file.name == "skill/rp/bin/"+t.binary() {
			skillBin = file.data
		}
		if file.mode.Perm() == 0755 {
			executables++
			if file.name != t.binary() && file.name != "skill/rp/bin/"+t.binary() {
				return fmt.Errorf("unexpected executable %s", file.name)
			}
		}
		if strings.HasPrefix(file.name, "skill/rp/") && !strings.HasPrefix(file.name, "skill/rp/bin/") {
			skillTexts++
			if !strings.HasSuffix(file.name, ".md") {
				return errors.New("unexpected Skill source asset")
			}
		}
		if strings.HasPrefix(file.name, "skill/rp/bin/") && file.name != "skill/rp/bin/"+t.binary() {
			return errors.New("wrong-platform or extra Skill binary")
		}
		for _, part := range strings.Split(file.name, "/") {
			if part == "node_modules" || part == "__pycache__" || part == "tests" || strings.Contains(part, "evidence") {
				return fmt.Errorf("development material in package: %s", file.name)
			}
		}
	}
	if executables != 2 || len(bin) == 0 || digest(bin) != digest(skillBin) {
		return errors.New("top-level and Skill executables must be identical and executable")
	}
	if skillTexts != 10 {
		return fmt.Errorf("expected 10 standard Skill text files, got %d", skillTexts)
	}
	return nil
}
func validateBinary(name string, t target) error {
	switch t.OS {
	case "linux":
		f, err := elf.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		want := elf.EM_X86_64
		if t.Arch == "arm64" {
			want = elf.EM_AARCH64
		}
		if f.Machine != want || f.Class != elf.ELFCLASS64 {
			return fmt.Errorf("wrong ELF architecture for %s", t.key())
		}
	case "darwin":
		f, err := macho.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		want := macho.CpuAmd64
		if t.Arch == "arm64" {
			want = macho.CpuArm64
		}
		if f.Cpu != want {
			return fmt.Errorf("wrong Mach-O architecture for %s", t.key())
		}
	case "windows":
		f, err := pe.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		want := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if t.Arch == "arm64" {
			want = pe.IMAGE_FILE_MACHINE_ARM64
		}
		if f.Machine != want {
			return fmt.Errorf("wrong PE architecture for %s", t.key())
		}
	default:
		return errors.New("unsupported target")
	}
	return nil
}
func writeArchive(name, root string, files []entry, epoch time.Time, windows bool) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if windows {
		w := zip.NewWriter(f)
		for _, file := range files {
			h := &zip.FileHeader{Name: root + "/" + file.name, Method: zip.Deflate}
			h.SetMode(file.mode)
			h.SetModTime(epoch)
			out, err := w.CreateHeader(h)
			if err != nil {
				return err
			}
			if _, err := out.Write(file.data); err != nil {
				return err
			}
		}
		return errors.Join(w.Close(), f.Sync())
	}
	gz := gzip.NewWriter(f)
	gz.Header.ModTime = epoch
	tw := tar.NewWriter(gz)
	for _, file := range files {
		h := &tar.Header{Name: root + "/" + file.name, Mode: int64(file.mode.Perm()), Size: int64(len(file.data)), ModTime: epoch, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(file.data); err != nil {
			return err
		}
	}
	return errors.Join(tw.Close(), gz.Close(), f.Sync())
}

// readArchive refuses links, duplicate paths and oversized content before any
// extraction. Every member stays under the single, expected package root.
func readArchive(name, root string) ([]entry, error) {
	var files []entry
	total := int64(0)
	seen := map[string]bool{}
	add := func(name string, mode fs.FileMode, size int64, r io.Reader) error {
		if !strings.HasPrefix(name, root+"/") {
			return errors.New("archive has unexpected top-level root")
		}
		name = strings.TrimPrefix(name, root+"/")
		if !validArchivePath(name) || seen[strings.ToLower(name)] || !mode.IsRegular() {
			return fmt.Errorf("unsafe or duplicate archive member %q", name)
		}
		if size < 0 || size > 32<<20 || total+size > maxPackageBytes {
			return errors.New("archive member exceeds release limit")
		}
		if len(files) >= 4096 {
			return errors.New("too many archive files")
		}
		data, err := io.ReadAll(io.LimitReader(r, size+1))
		if err != nil {
			return err
		}
		if int64(len(data)) != size {
			return errors.New("archive size mismatch")
		}
		total += size
		seen[strings.ToLower(name)] = true
		files = append(files, entry{name, mode.Perm(), data})
		return nil
	}
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.OpenReader(name)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		for _, file := range zr.File {
			r, err := file.Open()
			if err != nil {
				return nil, err
			}
			err = add(file.Name, file.Mode(), int64(file.UncompressedSize64), r)
			closeErr := r.Close()
			if err = errors.Join(err, closeErr); err != nil {
				return nil, err
			}
		}
		return files, nil
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			return nil, errors.New("non-regular tar member")
		}
		if err := add(h.Name, fs.FileMode(h.Mode), h.Size, tr); err != nil {
			return nil, err
		}
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return nil, err
	}
	return files, nil
}
func verifyRelease(directory string) error {
	data, err := readRegular(filepath.Join(directory, "release.json"))
	if err != nil {
		return err
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if !versionPattern.MatchString(manifest.Version) || len(manifest.Packages) != len(platforms) {
		return errors.New("invalid release version/platform count")
	}
	inputs, err := readRegular(filepath.Join(directory, "build-inputs.sha256"))
	if err != nil {
		return err
	}
	if digest(inputs) != manifest.SourceSHA256 {
		return errors.New("build input manifest hash mismatch")
	}
	sums, err := readRegular(filepath.Join(directory, "SHA256SUMS"))
	if err != nil {
		return err
	}
	checks := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		hash, name, ok := strings.Cut(line, "  ")
		if !ok || !validArchivePath(name) || path.Base(name) != name || checks[name] != "" || len(hash) != 64 {
			return errors.New("invalid checksum line")
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return err
		}
		checks[name] = hash
		actual, err := fileDigest(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if actual != hash {
			return fmt.Errorf("checksum mismatch: %s", name)
		}
	}
	if len(checks) != 8 || checks["release.json"] == "" || checks["build-inputs.sha256"] == "" {
		return errors.New("checksum file must cover six archives and both manifests")
	}
	for i, pkg := range manifest.Packages {
		t := platforms[i]
		if pkg.Target != t.key() || pkg.Archive != t.archive(manifest.Version) || checks[pkg.Archive] != pkg.SHA256 {
			return errors.New("release target/name/hash mismatch")
		}
		archive := filepath.Join(directory, pkg.Archive)
		info, err := os.Lstat(archive)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != pkg.Size {
			return errors.New("release archive size/type mismatch")
		}
		files, err := readArchive(archive, strings.TrimSuffix(pkg.Archive, archiveExt(t)))
		if err != nil {
			return fmt.Errorf("%s: %w", pkg.Archive, err)
		}
		if err := validateEntries(files, t, manifest.Version); err != nil {
			return err
		}
		var pm packageManifest
		for _, file := range files {
			if file.name == "MANIFEST.json" {
				if err := json.Unmarshal(file.data, &pm); err != nil {
					return err
				}
			}
		}
		if pm.Version != manifest.Version || pm.Target != t.key() || pm.SourceSHA256 != manifest.SourceSHA256 || pm.BuiltAt != manifest.BuiltAt || len(pm.Files) != len(files)-1 {
			return errors.New("package manifest mismatch")
		}
		records := map[string]fileRecord{}
		for _, r := range pm.Files {
			if records[r.Path].Path != "" {
				return errors.New("duplicate manifest file")
			}
			records[r.Path] = r
		}
		for _, file := range files {
			if file.name == "MANIFEST.json" {
				continue
			}
			if records[file.name] != record(file) {
				return fmt.Errorf("package file manifest mismatch: %s", file.name)
			}
			if file.name == t.binary() {
				if digest(file.data) != pkg.BinarySHA256 {
					return errors.New("binary checksum mismatch")
				}
				temp, err := os.CreateTemp("", "rps-release-binary-")
				if err != nil {
					return err
				}
				_, writeErr := temp.Write(file.data)
				closeErr := temp.Close()
				if writeErr == nil && closeErr == nil {
					err = validateBinary(temp.Name(), t)
				} else {
					err = errors.Join(writeErr, closeErr)
				}
				os.Remove(temp.Name())
				if err != nil {
					return err
				}
			}
		}
		fmt.Printf("Verified %s (%d bytes)\n", pkg.Archive, pkg.Size)
	}
	return nil
}
