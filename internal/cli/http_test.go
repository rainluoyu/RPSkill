package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type capturedRequest struct {
	method, path, key, auth, attempt string
	body                             map[string]any
}

func hostFixture(t *testing.T) (string, *[]capturedRequest) {
	t.Helper()
	project := t.TempDir()
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	requests := &[]capturedRequest{}
	token := strings.Repeat("h", 64)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("wrong authorization")
		}
		body := map[string]any{}
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Error(err)
				}
			}
		}
		*requests = append(*requests, capturedRequest{r.Method, r.URL.RequestURI(), r.Header.Get("Idempotency-Key"), r.Header.Get("Authorization"), r.Header.Get("X-RPS-Attempt-ID"), body})
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"data":{"saved":true}}`)
	}))
	t.Cleanup(ts.Close)

	t.Setenv("RPS_SERVER_URL", ts.URL)
	t.Setenv("RPS_SERVER_TOKEN", token)
	return project, requests
}
func TestHostCommandsUseHTTP(t *testing.T) {
	p, requests := hostFixture(t)
	card := filepath.Join(t.TempDir(), "card.md")
	if err := os.WriteFile(card, []byte("---\nenabled: true\n---\n正文\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args         []string
		method, path string
	}{
		{[]string{"input", "--chat", "main", "--speaker", "chat", "--text", " original\n"}, "POST", "/api/v1/chats/main/inputs"},
		{[]string{"chat", "list"}, "GET", "/api/v1/chats"},
		{[]string{"chat", "create", "新世界"}, "POST", "/api/v1/chats"},
		{[]string{"chat", "get", "main"}, "GET", "/api/v1/chats/main"},
		{[]string{"chat", "rename", "main", "新名字"}, "PATCH", "/api/v1/chats/main"},
		{[]string{"chat", "delete", "alt"}, "DELETE", "/api/v1/chats/alt"},
		{[]string{"config", "set", "last_chat", `"main"`}, "PATCH", "/api/v1/project"},
		{[]string{"chat", "state", "main"}, "GET", "/api/v1/chats/main/state"},
		{[]string{"character", "list"}, "GET", "/api/v1/characters"},
		{[]string{"character", "create", "alice", "--file", card}, "POST", "/api/v1/characters"},
		{[]string{"character", "update", "alice", "--file", card}, "PUT", "/api/v1/characters/alice"},
		{[]string{"character", "get", "alice"}, "GET", "/api/v1/characters/alice"},
		{[]string{"character", "delete", "alice"}, "DELETE", "/api/v1/characters/alice"},
		{[]string{"character", "state", "alice", "--chat", "alt"}, "GET", "/api/v1/chats/alt/characters/alice/state"},
		{[]string{"character", "dynamic", "delete", "alice", "--chat", "main"}, "POST", "/api/v1/chats/main/characters/alice/updates"},
		{[]string{"worldbook", "list"}, "GET", "/api/v1/worldbook"},
		{[]string{"worldbook", "create", "world", "history", "--file", card}, "POST", "/api/v1/worldbook"},
		{[]string{"worldbook", "get", "world", "history"}, "GET", "/api/v1/worldbook/world/history"},
		{[]string{"worldbook", "update", "world", "history", "--file", card}, "PUT", "/api/v1/worldbook/world/history"},
		{[]string{"worldbook", "delete", "world", "history"}, "DELETE", "/api/v1/worldbook/world/history"},
		{[]string{"worldbook", "category", "world", "--enabled=false", "--chat", "main"}, "PATCH", "/api/v1/chats/main/settings"},
		{[]string{"worldbook", "refresh"}, "POST", "/api/v1/worldbook/refresh"},
		{[]string{"config", "get"}, "GET", "/api/v1/project"},
		{[]string{"config", "get"}, "GET", "/api/v1/project"},
		{[]string{"config", "set", "name", `"新名称"`}, "PATCH", "/api/v1/project"},
		{[]string{"config", "set", "polishing.level", `"off"`}, "PATCH", "/api/v1/project"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			args := append(append([]string{}, c.args...), "--project", p, "--json")
			out, err := run(args...)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid([]byte(out)) {
				t.Fatalf("invalid response %q", out)
			}
			got := (*requests)[len(*requests)-1]
			if got.method != c.method || got.path != c.path {
				t.Fatalf("got %s %s", got.method, got.path)
			}
			if c.args[0] == "input" && got.body["text"] != " original\n" {
				t.Fatal("original text changed")
			}
		})
	}
}
func TestUsageAndFileValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(file, bytes.Repeat([]byte("x"), maxTextSize+1), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(file, link)
	cases := [][]string{
		{"input", "--chat", "main", "--speaker", "assistant", "--text", "x"}, {"input", "--chat", "main", "--speaker", "director"},
		{"chat", "get", "../bad"}, {"worldbook", "get", "world", "../bad"},
		{"chat", "create", "alt", "--parent", "main"}, {"chat", "plans", "set", "main", "plan_x", "--text", "x"},
		{"config", "set", "name", "unquoted"}, {"config", "set", "a..b", "true"},
		{"emit", "--kind", "invalid", "--text", "x"}, {"emit", "--kind", "story", "--file", file},
		{"input", "--chat", "main", "--speaker", "chat", "--text", "x", "--file", file},
		{"worldbook", "category", "world"}, {"chat", "history", "main", "--limit", "0"},
	}
	for _, args := range cases {
		_, err := run(args...)
		if ExitCode(err) != 2 {
			t.Errorf("%v: expected usage error got %v", args, err)
		}
	}
	if _, err := readLocalFile(link, maxTextSize); err == nil {
		t.Fatal("followed symlink")
	}
	var target any
	if _, err := parseJSON([]byte(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("duplicate JSON accepted")
	}
	_ = target
}
func TestHTTPErrorAndJSONDiagnostics(t *testing.T) {
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	p := t.TempDir()
	token := strings.Repeat("t", 64)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"error":{"code":"stale_attempt","message":"old attempt","details":{"retry":false}}}`)
	}))
	defer ts.Close()
	t.Setenv("RPS_SERVER_URL", ts.URL)
	t.Setenv("RPS_SERVER_TOKEN", token)
	cmd := NewRoot()
	var out, errs bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetArgs([]string{"input", "--chat", "main", "--project", p, "--speaker", "director", "--text", "x", "--json"})
	if code := Execute(cmd); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Fatal("printed false success")
	}
	var result map[string]any
	if err := json.Unmarshal(errs.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["error"].(map[string]any)["code"] != "stale_attempt" {
		t.Fatal(errs.String())
	}
}
func TestHTTPDoesNotFollowRedirectOrProxy(t *testing.T) {
	var contacted bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true }))
	defer evil.Close()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL, http.StatusTemporaryRedirect)
	}))
	defer ts.Close()
	t.Setenv("HTTP_PROXY", evil.URL)
	t.Setenv("http_proxy", evil.URL)
	t.Setenv("ALL_PROXY", evil.URL)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	c := &apiClient{endpoint: ts.URL, token: strings.Repeat("x", 64)}
	_, err := c.do(cmd, "POST", "/test", map[string]any{})
	if err == nil {
		t.Fatal("accepted redirect")
	}
	if contacted {
		t.Fatal("redirected/proxied secret")
	}
}
func TestLocalEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"https://127.0.0.1:1", "http://localhost:1", "http://example.com:1", "http://127.0.0.1:1/", "http://127.0.0.1:1?", "http://127.0.0.1:1/#x", "http://user@127.0.0.1:1", "http://127.0.0.1"} {
		if localEndpoint(endpoint) == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:8717", "http://[::1]:8717"} {
		if err := localEndpoint(endpoint); err != nil {
			t.Fatal(err)
		}
	}
}
func TestMediaAndPatchFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "refs.json")
	os.WriteFile(file, []byte(`[{"id":"med_1","path":"media/a.png","mime":"image/png","name":"a.png"}]`), 0600)
	refs, err := readMedia(file)
	if err != nil || len(refs) != 1 {
		t.Fatalf("%v %v", refs, err)
	}
	for _, s := range []string{`null`, `["med_1"]`, `[{"id":"med_1","unknown":true}]`} {
		os.WriteFile(file, []byte(s), 0600)
		if _, err := readMedia(file); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	os.WriteFile(file, []byte(`{"polishing":{"level":"full"}}`), 0600)
	patch, err := patchFromArgs(nil, file)
	if err != nil {
		t.Fatal(err)
	}
	if patch["polishing"].(map[string]any)["level"] != "full" {
		t.Fatal(patch)
	}
	_, err = patchFromArgs([]string{"name", `"x"`}, file)
	var usageError *UsageError
	if !errors.As(err, &usageError) {
		t.Fatal(err)
	}
}

func TestStoryNameAndCastEditFlags(t *testing.T) {
	project, requests := hostFixture(t)
	for _, args := range [][]string{
		{"chat", "create", "示例故事"},
		{"chat", "message", "edit", "示例故事", "2", "--character", "角色乙", "--character", "角色甲"},
	} {
		if out, err := run(append(args, "--project", project, "--json")...); err != nil {
			t.Fatal(err, out)
		}
	}
	if (*requests)[0].body["name"] != "示例故事" || (*requests)[0].body["id"] != nil {
		t.Fatal((*requests)[0])
	}
	body := (*requests)[1].body
	if body["text"] != nil || len(body["characters"].([]any)) != 2 {
		t.Fatal(body)
	}
}
