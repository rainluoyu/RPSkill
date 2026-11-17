package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/luoyu/rpskill"
	"github.com/luoyu/rpskill/internal/server"
	"github.com/spf13/cobra"
)

func installSkillCommand() *cobra.Command {
	var target string
	var asJSON, force bool
	cmd := &cobra.Command{Use: "install-skill --target SKILLS_DIRECTORY", Short: "Install embedded RP Skill and this executable for the actual platform", Args: noArgs, RunE: func(cmd *cobra.Command, args []string) error {

		if target == "" {
			return usage("--target is required")
		}
		source, err := os.Executable()
		if err != nil {
			return err
		}
		destination, err := installSkill(target, source, force)
		if err != nil {
			return err
		}
		if asJSON {
			return printData(cmd, map[string]any{"installed": true, "path": destination, "platform": runtime.GOOS + "/" + runtime.GOARCH})
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Installed RP Skill: %s\nPlatform: %s/%s\n", destination, runtime.GOOS, runtime.GOARCH)
		return err
	}}
	cmd.Flags().StringVar(&target, "target", "", "Existing or new skills parent directory (installs target/rp)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print structured installation result")
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing installation with rollback on failure")
	return cmd
}
func rejectInstallLinks(name string) error {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	for p := absolute; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return errors.New("installation path must use real directories, not symlinks or files")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func installSkill(target, executable string, force bool) (string, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if err := rejectInstallLinks(target); err != nil {
		return "", err
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(target)
	if err != nil {
		return "", err
	}
	defer root.Close()
	exists := false
	if info, err := root.Lstat("rp"); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("existing Skill target must be a real directory")
		}
		exists = true
		if !force {
			return "", errors.New("RP Skill already exists; use --force to replace it")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	nonce, err := server.NewToken()
	if err != nil {
		return "", err
	}
	staging := ".rp-install-" + nonce
	if err := root.Mkdir(staging, 0755); err != nil {
		return "", err
	}
	defer root.RemoveAll(staging)
	stage, err := root.OpenRoot(staging)
	if err != nil {
		return "", err
	}
	defer stage.Close()
	err = fs.WalkDir(rpskill.SkillFiles(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return stage.MkdirAll(path, 0755)
		}
		if !d.Type().IsRegular() {
			return errors.New("embedded Skill contains non-regular source")
		}
		content, err := fs.ReadFile(rpskill.SkillFiles(), path)
		if err != nil {
			return err
		}
		f, err := stage.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(content)
		syncErr := f.Sync()
		return errors.Join(writeErr, syncErr, f.Close())
	})
	if err != nil {
		return "", err
	}
	if err := stage.Mkdir("bin", 0755); err != nil {
		return "", err
	}
	// os.Executable may itself reside behind the OS's executable symlink. Opening
	// that trusted current image is deliberate; untrusted source paths are not used.
	source, err := os.Open(executable)
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("current executable is not a regular file")
	}
	name := "bin/rps"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary, err := stage.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(binary, source)
	syncErr := binary.Sync()
	if err := errors.Join(copyErr, syncErr, binary.Close()); err != nil {
		return "", err
	}
	backup := ".rp-backup-" + nonce
	if exists {
		if err := root.Rename("rp", backup); err != nil {
			return "", err
		}
	}
	if err := root.Rename(staging, "rp"); err != nil {
		if exists {
			return "", errors.Join(err, root.Rename(backup, "rp"))
		}
		return "", err
	}
	if exists {
		if err := root.RemoveAll(backup); err != nil {
			return "", fmt.Errorf("Skill installed but backup cleanup failed: %w", err)
		}
	}
	return filepath.Join(target, "rp"), nil
}
