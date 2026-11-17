package server

import (
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Browsers share cookies across ports. Opening another project must not replace
// the first project's authenticated session, nor log it out.
func TestBrowserSessionsCoexistAcrossPorts(t *testing.T) {
	first, second := newTestServer(t), newTestServer(t)
	second.token = strings.Repeat("b", 64)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	send := func(s *Server, origin, method, path, bearer string) int {
		t.Helper()
		u, err := url.Parse(origin + path)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, u.String(), nil)
		r.Header.Set("Origin", origin)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		for _, c := range jar.Cookies(u) {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		jar.SetCookies(u, w.Result().Cookies())
		return w.Code
	}
	for _, pair := range []struct {
		s      *Server
		origin string
	}{{first, "http://127.0.0.1:8717"}, {second, "http://127.0.0.1:8718"}} {
		if got := send(pair.s, pair.origin, "POST", "/api/v1/auth/session", pair.s.token); got != 200 {
			t.Fatalf("login: %d", got)
		}
	}
	for _, pair := range []struct {
		s      *Server
		origin string
	}{{first, "http://127.0.0.1:8717"}, {second, "http://127.0.0.1:8718"}} {
		if got := send(pair.s, pair.origin, "GET", "/api/v1/status", ""); got != 200 {
			t.Fatalf("session overwritten at %s: %d", pair.origin, got)
		}
	}
	if got := send(second, "http://127.0.0.1:8718", "DELETE", "/api/v1/auth/session", ""); got != 200 {
		t.Fatal(got)
	}
	if got := send(first, "http://127.0.0.1:8717", "GET", "/api/v1/status", ""); got != 200 {
		t.Fatalf("logout removed other project's cookie: %d", got)
	}
}
