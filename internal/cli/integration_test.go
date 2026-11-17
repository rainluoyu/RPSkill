package cli

import (
	"bytes"
	"context"

	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"testing"
	"time"

	"github.com/luoyu/rpskill/internal/callback"

	"github.com/luoyu/rpskill/internal/server"
)

type testInbox struct{ notices chan callback.Notification }

func (i *testInbox) Deliver(_ context.Context, n callback.Notification) error {
	if i.notices != nil {
		i.notices <- n
	}
	return nil
}

func actualServer(t *testing.T, dispatcher callback.Dispatcher) (string, *server.Server) {
	t.Helper()
	p := cliProject(t)
	token, _ := server.NewToken()
	if dispatcher == nil {
		dispatcher = &testInbox{}
	}
	opts := server.Options{Project: p, Callback: callback.ProviderCodex, Token: token, Dispatcher: dispatcher}
	srv, err := server.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	if err := srv.SetEndpoint(ts.URL); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RPS_SERVER_URL", ts.URL)
	t.Setenv("RPS_SERVER_TOKEN", token)
	return p, srv
}
func buildCLI(t *testing.T) string {
	t.Helper()
	name := "rps"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", path, "../../cmd/rps")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return path
}
func processCLI(binary string, env []string, args ...string) (string, string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), env...)
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
	}
	return out.String(), errs.String(), code
}
func TestInstallRejectsSymlinkAndPreservesExisting(t *testing.T) {
	target := t.TempDir()
	source := filepath.Join(t.TempDir(), "binary")
	os.WriteFile(source, []byte("real-current-test-image"), 0700)
	installed, err := installSkill(target, source, false)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(installed, "user-marker")
	os.WriteFile(marker, []byte("keep"), 0600)
	if _, err := installSkill(target, "/does/not/exist", true); err == nil {
		t.Fatal("installed missing executable")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("failed install destroyed original")
	}
	linked := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(target, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := installSkill(linked, source, false); err == nil {
		t.Fatal("accepted linked target")
	}
	if _, err := installSkill(target, source, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("did not replace original")
	}
}
