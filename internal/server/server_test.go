package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/project"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "workspace")
	rt := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := project.Init(directory, project.InitOptions{RuntimeDir: rt})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	s, err := New(Options{Project: directory, RuntimeDir: rt, Callback: callback.ProviderCodex, Token: testToken, Dispatcher: inboxFunc(func(context.Context, callback.Notification) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEndpoint("http://127.0.0.1:8717"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func request(s *Server, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8717"+path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	project := t.TempDir()
	for _, tt := range []Options{
		{Project: project, Token: testToken},
		{Project: project, Token: testToken, Callback: "mock"},
		{Project: project, Token: "short", Callback: callback.ProviderCodex},
		{Project: project + "/missing", Token: testToken, Callback: callback.ProviderCodex},
	} {
		if _, err := New(tt); err == nil {
			t.Errorf("accepted invalid options: %#v", tt)
		}
	}
}

func TestHealthAndHonestStatus(t *testing.T) {
	s := newTestServer(t)
	w := request(s, "GET", "/api/v1/health", nil)
	if w.Code != 200 {
		t.Fatalf("health=%d %s", w.Code, w.Body.String())
	}
	var health struct {
		Data struct {
			Status  string `json:"status"`
			Phase   string `json:"phase"`
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Data.Status != "ok" || health.Data.Phase != ReleasePhase || health.Data.Version != Version {
		t.Fatalf("invalid health: %+v", health)
	}
	w = request(s, "GET", "/api/v1/status", map[string]string{"Authorization": "Bearer " + testToken})
	var status struct {
		Data Status `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || status.Data.Callback != "codex" || status.Data.Version != Version || status.Data.Phase != ReleasePhase {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), testToken) {
		t.Fatal("status leaked control token")
	}
}

func TestAuthenticationAndOrigin(t *testing.T) {
	s := newTestServer(t)
	cases := []struct {
		name, method, path string
		headers            map[string]string
		want               int
	}{
		{"missing token", "GET", "/api/v1/status", nil, 401},
		{"wrong token", "GET", "/api/v1/status", map[string]string{"Authorization": "Bearer wrong"}, 401},
		{"valid token", "GET", "/api/v1/status", map[string]string{"Authorization": "Bearer " + testToken}, 200},
		{"token in query refused", "GET", "/api/v1/status?token=" + testToken, nil, 401},
		{"cross origin", "GET", "/api/v1/status", map[string]string{"Authorization": "Bearer " + testToken, "Origin": "https://evil.example"}, 403},
		{"cross site", "GET", "/api/v1/status", map[string]string{"Authorization": "Bearer " + testToken, "Sec-Fetch-Site": "cross-site"}, 403},
		{"null origin", "POST", "/api/v1/auth/session", map[string]string{"Authorization": "Bearer " + testToken, "Origin": "null"}, 403},
		{"same origin", "POST", "/api/v1/auth/session", map[string]string{"Authorization": "Bearer " + testToken, "Origin": "http://127.0.0.1:8717"}, 200},
		{"cookie login refused", "POST", "/api/v1/auth/session", map[string]string{"Cookie": "rps_session_8717=" + testToken}, 401},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			w := request(s, tt.method, tt.path, tt.headers)
			if w.Code != tt.want {
				t.Fatalf("got %d want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
	w := request(s, "POST", "/api/v1/auth/session", map[string]string{"Authorization": "Bearer " + testToken})
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatalf("unsafe cookie: %v", cookies)
	}
	cookieHeader := cookies[0].String()
	w = request(s, "GET", "/api/v1/status", map[string]string{"Cookie": cookieHeader})
	if w.Code != 200 {
		t.Fatalf("cookie status=%d", w.Code)
	}
	w = request(s, "DELETE", "/api/v1/auth/session", map[string]string{"Cookie": cookieHeader, "Origin": "http://127.0.0.1:8717"})
	if w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout failed: %d %s", w.Code, w.Body.String())
	}
}

func TestHostRebindingIsRejected(t *testing.T) {
	s := newTestServer(t)
	for _, host := range []string{"evil.example:8717", "127.0.0.1.evil.example", "0.0.0.0:8717", "192.168.1.1:8717"} {
		r := httptest.NewRequest("GET", "http://"+host+"/api/v1/health", nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("host %q accepted (%d)", host, w.Code)
		}
	}
	for _, host := range []string{"127.0.0.1:8717", "localhost:8717", "[::1]:8717"} {
		if !localHost(host) {
			t.Errorf("loopback %q rejected", host)
		}
	}
}

func TestStaticAndUnsupportedAPIBoundaries(t *testing.T) {
	s := newTestServer(t)
	for _, tt := range []struct {
		method, path, accept string
		want                 int
	}{
		{"GET", "/", "", 200},
		{"HEAD", "/", "", 200},
		{"GET", "/chats/main", "text/html", 200},
		{"GET", "/missing.js", "text/html", 404},
		{"GET", "/missing", "application/json", 404},
		{"GET", "/api/v1/chats", "text/html", 401},
		{"POST", "/api/v1/chats", "", 401},
		{"POST", "/", "", 405},
	} {
		w := request(s, tt.method, tt.path, map[string]string{"Accept": tt.accept})
		if w.Code != tt.want {
			t.Errorf("%s %s=%d want %d: %s", tt.method, tt.path, w.Code, tt.want, w.Body.String())
		}
		if strings.HasPrefix(tt.path, "/api/") && strings.Contains(w.Body.String(), "<!doctype html>") {
			t.Fatal("API became SPA success")
		}
	}
}

func TestSSEInitialStatusAndDisconnect(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("bad stream: %+v", response)
	}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "id: ") {
		t.Fatalf("missing SSE id: %q", first)
	}
	first, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if first != "event: server.status\n" {
		t.Fatalf("bad SSE event: %q", first)
	}
	data, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, `"launcher.inbox"`) {
		t.Fatalf("bad status: %s", data)
	}
	cancel()
	_, _ = io.Copy(io.Discard, response.Body)
}

func TestCookieMutationRequiresExplicitSameOrigin(t *testing.T) {
	s := newTestServer(t)
	for _, tt := range []struct {
		origin, fetch string
		want          int
	}{
		{"", "", 403}, {"", "same-origin", 403}, {"null", "same-origin", 403},
		{"http://127.0.0.1:9999", "same-site", 403},
		{"https://127.0.0.1:8717", "same-site", 403},
		{"http://127.0.0.1:8717", "same-origin", 200},
	} {
		w := request(s, "DELETE", "/api/v1/auth/session", map[string]string{"Cookie": "rps_session_8717=" + testToken, "Origin": tt.origin, "Sec-Fetch-Site": tt.fetch})
		if w.Code != tt.want {
			t.Errorf("Origin=%q fetch=%q got %d want %d", tt.origin, tt.fetch, w.Code, tt.want)
		}
	}
	// Non-browser CLI Bearer clients do not need to synthesize Origin headers.
	w := request(s, "DELETE", "/api/v1/auth/session", map[string]string{"Authorization": "Bearer " + testToken})
	if w.Code != 200 {
		t.Fatalf("Bearer CLI refused without Origin: %d", w.Code)
	}
}
