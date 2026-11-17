package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/luoyu/rpskill/internal/media"
	"github.com/luoyu/rpskill/internal/project"
	"github.com/luoyu/rpskill/internal/timeline"
)

func mediaAPIError(w http.ResponseWriter, err error) {
	if errors.Is(err, project.ErrRecoveryRequired) {
		writeError(w, 503, "recovery_required", err.Error())
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.Is(err, media.ErrTooLarge) || errors.As(err, &tooLarge) {
		writeError(w, 413, "body_too_large", err.Error())
		return
	}
	apiError(w, err)
}

// mediaRecoveryBlocked must be called with Server.mu held.
func (s *Server) mediaRecoveryBlocked(w http.ResponseWriter) bool {
	pending, err := s.recoveryPending()
	if err != nil || pending {
		writeError(w, 503, "recovery_required", "transaction recovery required; restart and check saved content before resubmitting")
		return true
	}
	return false
}

// mediaArchiveAPI is called before business consumes JSON, with no lock held.
// The common host/origin/auth middleware still encloses this dispatcher.
func (s *Server) mediaArchiveAPI(w http.ResponseWriter, r *http.Request) bool {
	if strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f") {
		return false
	}
	p := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/"), "/")
	if len(p) > 0 && p[0] == "media" {
		s.mediaAPI(w, r, p)
		return true
	}
	return s.archiveAPI(w, r, p)
}

func parseMultipart(w http.ResponseWriter, r *http.Request, limit int64, allowed map[string]bool) (string, []byte, map[string]string, error) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" {
		return "", nil, nil, fail(415, "unsupported_media_type", "multipart/form-data required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return "", nil, nil, fail(400, "invalid_multipart", err.Error())
	}
	fields := map[string]string{}
	var file []byte
	filename := ""
	seen := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, nil, err
		}
		name := part.FormName()
		if !allowed[name] || seen[name] {
			part.Close()
			return "", nil, nil, fail(400, "invalid_multipart", "unknown or duplicate multipart field")
		}
		seen[name] = true
		if name == "file" {
			_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			if err != nil {
				part.Close()
				return "", nil, nil, err
			}
			filename = params["filename"]
			if err := media.Name(filename); err != nil {
				part.Close()
				return "", nil, nil, err
			}
			file, err = io.ReadAll(io.LimitReader(part, limit+1))
			part.Close()
			if err != nil {
				return "", nil, nil, err
			}
			if int64(len(file)) > limit {
				return "", nil, nil, media.ErrTooLarge
			}
		} else {
			if part.FileName() != "" {
				part.Close()
				return "", nil, nil, errors.New("metadata must not be a file")
			}
			value, err := io.ReadAll(io.LimitReader(part, 1025))
			part.Close()
			if err != nil {
				return "", nil, nil, err
			}
			if len(value) > 1024 {
				return "", nil, nil, media.ErrTooLarge
			}
			fields[name] = string(value)
		}
	}
	if !seen["file"] {
		return "", nil, nil, errors.New("file required")
	}
	return filename, file, fields, nil
}

func (s *Server) mediaMutation(w http.ResponseWriter, r *http.Request, run func(string) (any, int, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		writeError(w, 503, "server_closed", "server closed")
		return
	}
	if s.mediaRecoveryBlocked(w) {
		return
	}
	id, err := timeline.NewID("med")
	if err != nil {
		mediaAPIError(w, err)
		return
	}
	result, status, err := run(id)
	if err != nil {
		mediaAPIError(w, err)
		return
	}
	s.publish("workspace.changed", map[string]any{"path": r.URL.Path})
	writeJSON(w, status, result)
}
func (s *Server) mediaAPI(w http.ResponseWriter, r *http.Request, p []string) {
	if len(p) == 1 && r.Method == http.MethodPost {
		name, data, fields, err := parseMultipart(w, r, media.MaxUpload, map[string]bool{"file": true, "timeline": true, "character": true, "expression": true, "avatar": true})
		if err != nil {
			mediaAPIError(w, err)
			return
		}
		if fields["avatar"] != "" && fields["avatar"] != "true" && fields["avatar"] != "false" {
			writeError(w, 400, "invalid_avatar", "avatar must be true or false")
			return
		}
		upload := media.Upload{Name: name, Data: data, Timeline: fields["timeline"], Character: fields["character"], Expression: fields["expression"], Avatar: fields["avatar"] == "true"}
		if err := upload.Validate(); err != nil {
			mediaAPIError(w, err)
			return
		}
		s.mediaMutation(w, r, func(id string) (any, int, error) {
			upload.ID = id
			record, err := s.store.UploadMedia(upload)
			return record, 201, err
		})
		return
	}
	if len(p) == 2 && r.Method == http.MethodDelete {
		s.mediaMutation(w, r, func(_ string) (any, int, error) {
			err := s.store.DeleteMedia(p[1])
			return map[string]bool{"deleted": true}, 200, err
		})
		return
	}
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || (len(p) != 1 && len(p) != 2) {
		writeError(w, 404, "not_found", "unknown media endpoint")
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		writeError(w, 503, "server_closed", "server closed")
		return
	}
	if s.mediaRecoveryBlocked(w) {
		s.mu.Unlock()
		return
	}
	if len(p) == 1 {
		line, char := r.URL.Query().Get("timeline"), r.URL.Query().Get("character")
		records, err := s.store.ListMedia(line, char)
		s.mu.Unlock()
		if err != nil {
			mediaAPIError(w, err)
			return
		}
		writeJSON(w, 200, records)
		return
	}
	record, data, err := s.store.Media(p[1])
	s.mu.Unlock()
	if err != nil {
		mediaAPIError(w, err)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", fmt.Sprintf("\"%s\"", record.SHA256))
	disposition := "inline"
	mimeType := record.MIME
	if !record.Preview || r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": record.Name}))
	http.ServeContent(w, r, record.Name, time.Time{}, bytes.NewReader(data))
}
