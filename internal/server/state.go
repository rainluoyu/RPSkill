package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/luoyu/rpskill/internal/project"
	"github.com/luoyu/rpskill/internal/storage"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// One private connection file per server lifetime, never a per-input credential.
// The launcher already owns this server; it uses the same local control authority.
type Connection struct {
	Project string `json:"project"`
	URL     string `json:"url"`
	Token   string `json:"token"`
}

func newBusinessServer(root string, opts Options) (*Server, error) {
	if opts.Dispatcher == nil {
		return nil, errors.New("launcher dispatcher is required")
	}
	store, err := project.Open(root, project.OpenOptions{RuntimeDir: opts.RuntimeDir})
	if err != nil {
		return nil, err
	}
	rt, err := storage.OpenRuntimeFS(root, opts.RuntimeDir)
	if err != nil {
		store.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{project: root, provider: opts.Callback, token: opts.Token, started: time.Now().UTC(), store: store, runtime: rt, dispatcher: opts.Dispatcher, subscribers: map[chan streamEvent]bool{}, ctx: ctx, cancel: cancel, skillDirectory: opts.SkillDirectory}, nil
}
func (s *Server) SetEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || !localHost(u.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must be plain loopback HTTP")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("server is closed")
	}
	if s.endpoint != "" {
		if s.endpoint == endpoint {
			return nil
		}
		return errors.New("endpoint already set")
	}
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return err
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return err
	}
	inside, err := storage.WithinDirectory(s.project, base)
	if err != nil {
		return err
	}
	if inside {
		return errors.New("connection directory must be outside workspace")
	}
	temp, err := os.MkdirTemp(base, "rps-run-")
	if err != nil {
		return err
	}
	file := filepath.Join(temp, "connection.json")
	raw, _ := json.Marshal(Connection{Project: s.project, URL: endpoint, Token: s.token})
	if err = os.WriteFile(file, raw, 0600); err != nil {
		os.RemoveAll(temp)
		return err
	}
	s.temporaryRoot = temp
	s.connectionFile = file
	s.endpoint = endpoint
	return nil
}
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.cancel()
	return errors.Join(s.store.Close(), s.runtime.Close(), os.RemoveAll(s.temporaryRoot))
}
func (s *Server) businessAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.Header.Get("X-RPS-Project"); p != "" && p != s.project {
			writeError(w, 409, "project_mismatch", "connection belongs to a different project")
			return
		}
		s.authenticate(next).ServeHTTP(w, r)
	})
}
