// Package server implements the local HTTP control plane and embedded web UI.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/project"
	"github.com/luoyu/rpskill/internal/storage"
)

// Version can be overridden by the release builder with -ldflags -X.
// A candidate version describes engineering delivery, not model or platform QA.
var Version = "1.0.0-rc.1"

const ReleasePhase = "v1.0-candidate"

const ProtocolVersion = 3

// Cookies have no port scope. Namespace sessions so simultaneous local projects
// do not overwrite one another's login or logout state.
func sessionCookieName(r *http.Request) string {
	port := r.URL.Port()
	if _, p, err := net.SplitHostPort(r.Host); err == nil {
		port = p
	}
	if port == "" {
		port = "80"
		if r.TLS != nil {
			port = "443"
		}
	}
	return "rps_session_" + port
}

type Options struct {
	Project        string
	Callback       callback.Provider
	Token          string
	RuntimeDir     string
	Dispatcher     callback.Dispatcher
	SkillDirectory string
}
type Server struct {
	project        string
	provider       callback.Provider
	token          string
	started        time.Time
	handler        http.Handler
	mu             sync.Mutex
	store          *project.Store
	runtime        *storage.FS
	temporaryRoot  string
	connectionFile string
	endpoint       string
	dispatcher     callback.Dispatcher
	subscribers    map[chan streamEvent]bool
	sequence       uint64
	ctx            context.Context
	cancel         context.CancelFunc
	closed         bool
	skillDirectory string
}
type Status struct {
	Version         string            `json:"version"`
	ProtocolVersion int               `json:"protocol_version"`
	Phase           string            `json:"phase"`
	Project         string            `json:"project"`
	Callback        callback.Provider `json:"callback"`
	StartedAt       time.Time         `json:"started_at"`
	Capabilities    []string          `json:"capabilities"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func CanonicalProject(project string) (string, error) {
	absolute, err := filepath.Abs(project)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("project must be a directory")
	}
	return resolved, nil
}

func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func New(opts Options) (*Server, error) {
	if !opts.Callback.Valid() {
		return nil, errors.New("callback must be explicitly set to codex")
	}
	project, err := CanonicalProject(opts.Project)
	if err != nil {
		return nil, err
	}
	if len(opts.Token) < 32 {
		return nil, errors.New("control token must contain at least 32 characters")
	}
	s, err := newBusinessServer(project, opts)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.Handle("GET /api/v1/status", s.authenticate(http.HandlerFunc(s.status)))
	mux.Handle("POST /api/v1/auth/session", s.authenticateBearer(http.HandlerFunc(s.login)))
	mux.Handle("DELETE /api/v1/auth/session", s.authenticate(http.HandlerFunc(s.logout)))
	mux.Handle("GET /api/v1/events", s.authenticate(http.HandlerFunc(s.events)))
	mux.Handle("/api/v1/", s.businessAuth(http.HandlerFunc(s.business)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "route_not_found", "API route is not implemented or does not exist")
	})
	mux.Handle("/", staticUI(embeddedUI()))
	s.handler = s.localOnly(mux)
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) Snapshot() Status {
	state := Status{
		Version: Version, ProtocolVersion: ProtocolVersion, Phase: ReleasePhase, Project: s.project,
		Callback: s.provider, StartedAt: s.started,
		Capabilities: []string{"health", "status", "auth.session", "sse.resync", "embedded.ui", "workspace.crud", "launcher.inbox", "history.edit", "media", "archives", "chats.flat", "provider.codex"},
	}
	return state
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Data any `json:"data"`
	}{data})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error APIError `json:"error"`
	}{APIError{code, message}})
}

// localHost forbids arbitrary DNS names to prevent browser DNS rebinding.
func localHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
		if !localHost(r.Host) {
			writeError(w, http.StatusForbidden, "host_forbidden", "only loopback hosts are allowed")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusForbidden, "origin_forbidden", "cross-site requests are not allowed")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != scheme || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				writeError(w, http.StatusForbidden, "origin_forbidden", "request must have the same origin")
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validToken(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

func (s *Server) bearer(r *http.Request) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	return strings.HasPrefix(auth, prefix) && s.validToken(strings.TrimPrefix(auth, prefix))
}

func (s *Server) authenticateBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.bearer(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid local Bearer token is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		valid := s.bearer(r)
		if !valid && r.Header.Get("Authorization") == "" {
			if c, err := r.Cookie(sessionCookieName(r)); err == nil {
				valid = s.validToken(c.Value)
				if valid && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && r.Header.Get("Origin") == "" {
					writeError(w, http.StatusForbidden, "origin_required", "browser session mutations require an explicit same-origin Origin header")
					return
				}
			}
		}
		if !valid {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid local token or browser session is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "protocol_version": ProtocolVersion, "phase": ReleasePhase})
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Snapshot())
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName(r), Value: s.token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName(r), Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func staticUI(files fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "static resources only support GET and HEAD")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(files, name)
		if err == nil && !info.IsDir() {
			if name == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// Only HTML navigation falls back; missing assets/API never become HTML 200.
		if path.Ext(name) == "" && strings.Contains(r.Header.Get("Accept"), "text/html") {
			body, err := fs.ReadFile(files, "index.html")
			if err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-cache")
				w.WriteHeader(http.StatusOK)
				if r.Method != http.MethodHead {
					_, _ = w.Write(body)
				}
				return
			}
		}
		http.NotFound(w, r)
	})
}
