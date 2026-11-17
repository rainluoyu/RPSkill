//go:build linux || darwin

package character_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/luoyu/rpskill/internal/character"
)

func TestRejectSpecialAssetWithoutBlocking(t *testing.T) {
	workspace := setup(t)
	fifo := filepath.Join(workspace, "characters/alice/pipe")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := character.ReadAsset(workspace, "alice", "pipe"); err == nil {
		t.Fatal("accepted FIFO")
	}
	if _, err := character.Scan(workspace); err == nil {
		t.Fatal("scan accepted FIFO")
	}
}

func TestRelativeSymlinkInsideCharacterRejected(t *testing.T) {
	workspace := setup(t)
	link := filepath.Join(workspace, "characters/alice/copy.png")
	if err := os.Symlink("avatar.png", link); err != nil {
		t.Fatal(err)
	}
	if _, err := character.ReadAsset(workspace, "alice", "copy.png"); err == nil {
		t.Fatal("followed in-tree symlink")
	}
}
