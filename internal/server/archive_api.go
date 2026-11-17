package server

import (
	"bytes"
	"mime"
	"net/http"
	"time"

	"github.com/luoyu/rpskill/internal/media"
)

func (s *Server) archiveAPI(w http.ResponseWriter, r *http.Request, p []string) bool {
	if len(p) < 2 {
		return false
	}
	kind := p[0]
	if kind != "project" && kind != "chats" && kind != "characters" && kind != "worldbook" {
		return false
	}
	export := p[len(p)-1] == "export"
	importing := len(p) == 2 && p[1] == "import"
	if !export && !importing {
		return false
	}
	if importing {
		if r.Method != http.MethodPost {
			writeError(w, 405, "method_not_allowed", "POST required")
			return true
		}
		_, data, _, err := parseMultipart(w, r, media.MaxArchive, map[string]bool{"file": true})
		if err != nil {
			mediaAPIError(w, err)
			return true
		}
		s.mediaMutation(w, r, func(_ string) (any, int, error) {
			result, err := s.store.ImportArchive(kind, data)
			return result, 201, err
		})
		return true
	}
	id := ""
	valid := false
	if kind == "project" && len(p) == 2 && r.Method == http.MethodPost {
		valid = true
	}
	if kind == "chats" && len(p) == 3 && r.Method == http.MethodPost {
		valid = true
		id = p[1]
	}
	if kind == "characters" && len(p) == 3 && r.Method == http.MethodGet {
		valid = true
		id = p[1]
	}
	if kind == "worldbook" && len(p) == 2 && r.Method == http.MethodGet {
		valid = true
	}
	if !valid {
		writeError(w, 404, "not_found", "unknown archive endpoint")
		return true
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		writeError(w, 503, "server_closed", "server closed")
		return true
	}
	if s.mediaRecoveryBlocked(w) {
		s.mu.Unlock()
		return true
	}
	data, err := s.store.ExportArchive(kind, id, r.URL.Query().Get("category"))
	s.mu.Unlock()
	if err != nil {
		mediaAPIError(w, err)
		return true
	}
	filename := kind + ".zip"
	if id != "" {
		filename = id + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
	return true
}
