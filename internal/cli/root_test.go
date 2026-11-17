package cli

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/project"
	"github.com/luoyu/rpskill/internal/server"
)

func run(args ...string) (string, error) {
	cmd := NewRoot()
	var out lockedBuffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"serve", "--help"}, {"status", "--help"}, {"version"}} {
		out, err := run(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out == "" {
			t.Fatalf("empty output: %v", args)
		}
	}
}

func TestRequiredArgumentsAndExitCodes(t *testing.T) {
	for _, args := range [][]string{
		{"serve"}, {"serve", t.TempDir()}, {"serve", t.TempDir(), "--callback", "mock"}, {"serve", t.TempDir(), "--callback", "grok"},
		{"serve", t.TempDir(), "--callback", "codex", "--listen", "0.0.0.0:8717"},
		{"serve", t.TempDir(), "--callback", "codex", "--listen", "not-a-listener"},
		{"status"}, {"status", "extra"}, {"version", "extra"}, {"serve", "--unknown"},
	} {
		_, err := run(args...)
		if ExitCode(err) != 2 {
			t.Errorf("%v: got %v, code %d", args, err, ExitCode(err))
		}
	}
	if ExitCode(nil) != 0 {
		t.Fatal("success exit code")
	}
}

func TestStatusUsesExternalDescriptor(t *testing.T) {
	project := cliProject(t)
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	token, err := server.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(server.Options{Project: project, Callback: callback.ProviderCodex, Token: token, Dispatcher: &testInbox{}})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	t.Setenv("RPS_SERVER_URL", httpServer.URL)
	t.Setenv("RPS_SERVER_TOKEN", token)
	out, err := run("status", "--project", project, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Data server.Status `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Callback != callback.ProviderCodex {
		t.Fatalf("unexpected status: %s", out)
	}
	if strings.Contains(out, token) {
		t.Fatal("status leaked token")
	}
	out, err = run("status", "--project", project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "codex (launcher inbox)") {
		t.Fatal(out)
	}
}

func TestStatusRejectsNonLocalRuntime(t *testing.T) {
	project := cliProject(t)
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	token, _ := server.NewToken()
	for _, endpoint := range []string{"http://evil.example", "https://127.0.0.1:8717", "http://127.0.0.1:8717/path", "http://user@127.0.0.1:8717", "http://127.0.0.1:8717?token=bad"} {

		t.Setenv("RPS_SERVER_URL", endpoint)
		t.Setenv("RPS_SERVER_TOKEN", token)
		_, err := run("status", "--project", project)
		if err == nil {
			t.Errorf("accepted unsafe runtime URL %q", endpoint)
		}
	}
}

func cliProject(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	store, err := project.Init(path, project.InitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
