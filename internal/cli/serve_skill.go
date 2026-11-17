package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/luoyu/rpskill"
	"github.com/luoyu/rpskill/internal/storage"
)

// A generated Skill is versioned by the current executable, which already
// contains the exact embedded instruction texts. It is runtime data, never RP
// data, and never writes to the user's global skills directory.
type serveSkill struct {
	directory string
	source    string
	digest    string
	managed   bool
}

func planServeSkill(project, explicit, source string) (serveSkill, error) {
	if explicit != "" {
		directory, err := filepath.Abs(explicit)
		if err != nil {
			return serveSkill{}, err
		}
		if err := outsideWorkspace(project, directory); err != nil {
			return serveSkill{}, err
		}
		if err := validateSkillDirectory(directory); err != nil {
			return serveSkill{}, fmt.Errorf("--skill-directory: %w", err)
		}
		return serveSkill{directory: directory}, nil
	}
	// Installed Skills already carry this executable. Reuse that exact bundle.
	installed := filepath.Dir(filepath.Dir(source))
	if err := validateSkillDirectory(installed); err == nil {
		if err := outsideWorkspace(project, installed); err != nil {
			return serveSkill{}, err
		}
		return serveSkill{directory: installed}, nil
	}
	digest, err := hashRegularFile(source)
	if err != nil {
		return serveSkill{}, err
	}
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return serveSkill{}, err
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return serveSkill{}, err
	}
	if err := outsideWorkspace(project, base); err != nil {
		return serveSkill{}, err
	}
	dir, err := os.MkdirTemp(base, "rps-skill-")
	if err != nil {
		return serveSkill{}, err
	}
	return serveSkill{directory: filepath.Join(dir, "rp"), source: source, digest: digest, managed: true}, nil
}
func outsideWorkspace(project, directory string) error {
	inside, err := storage.WithinDirectory(project, directory)
	if err != nil {
		return err
	}
	if inside {
		return errors.New("trusted Skill directory must be outside the RP Workspace")
	}
	return nil
}
func nativeCLIName() string {
	if runtime.GOOS == "windows" {
		return "rps.exe"
	}
	return "rps"
}
func hashRegularFile(name string) (string, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	if err := rejectInstallLinks(filepath.Dir(absolute)); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.Base(absolute))
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("expected a regular non-symlink file")
	}
	file, err := root.Open(filepath.Base(absolute))
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", errors.New("file changed while opening")
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func validateSkillDirectory(directory string) error {
	if err := rejectInstallLinks(directory); err != nil {
		return err
	}
	content, err := readLocalFile(filepath.Join(directory, "SKILL.md"), maxTextSize)
	if err != nil {
		return err
	}

	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(normalized, []byte("---\n")) {
		return errors.New("SKILL.md requires YAML frontmatter")
	}
	end := bytes.Index(normalized[4:], []byte("\n---\n"))
	if end < 0 {
		return errors.New("SKILL.md has no closing frontmatter delimiter")
	}
	var metadata map[string]any
	if err := storage.DecodeYAML(normalized[4:4+end], &metadata); err != nil {
		return fmt.Errorf("invalid Skill frontmatter: %w", err)
	}
	if metadata["name"] != "rp" {
		return errors.New("Skill frontmatter name must be rp")
	}
	description, ok := metadata["description"].(string)
	if !ok || strings.TrimSpace(description) == "" {
		return errors.New("Skill frontmatter requires a nonempty description")
	}

	cli := filepath.Join(directory, "bin", nativeCLIName())
	if _, err := hashRegularFile(cli); err != nil {
		return fmt.Errorf("Skill CLI: %w", err)
	}
	info, err := os.Stat(cli)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return errors.New("Skill CLI must be executable")
	}
	return nil
}
func (p serveSkill) deploy(project string) error {
	if !p.managed {
		return validateSkillDirectory(p.directory)
	}
	if err := outsideWorkspace(project, p.directory); err != nil {
		return err
	}
	parent := filepath.Dir(p.directory)
	if _, err := os.Lstat(p.directory); errors.Is(err, os.ErrNotExist) {
		if _, err := installSkill(parent, p.source, false); err != nil {
			return fmt.Errorf("deploy bundled Skill: %w", err)
		}
	} else if err != nil {
		return err
	}
	if err := validateSkillDirectory(p.directory); err != nil {
		return fmt.Errorf("generated Skill cache is invalid: %w", err)
	}
	// Never silently execute a modified cache or overwrite a user's explicit
	// installation. A new binary gets a new directory, leaving active copies alone.
	binaryHash, err := hashRegularFile(filepath.Join(p.directory, "bin", nativeCLIName()))
	if err != nil {
		return err
	}
	if binaryHash != p.digest {
		return errors.New("generated Skill CLI hash mismatch; refusing modified cache")
	}
	err = fs.WalkDir(rpskill.SkillFiles(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		want, err := fs.ReadFile(rpskill.SkillFiles(), path)
		if err != nil {
			return err
		}
		got, err := readLocalFile(filepath.Join(p.directory, filepath.FromSlash(path)), maxTextSize)
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) {
			return fmt.Errorf("generated Skill text mismatch: %s", path)
		}
		return nil
	})
	return err
}
