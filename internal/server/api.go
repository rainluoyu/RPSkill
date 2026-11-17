package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/luoyu/rpskill/internal/callback"
	"github.com/luoyu/rpskill/internal/project"
	"github.com/luoyu/rpskill/internal/storage"
	"github.com/luoyu/rpskill/internal/timeline"
)

type apiFailure struct {
	Status        int
	Code, Message string
}

func (e *apiFailure) Error() string               { return e.Message }
func fail(status int, code, message string) error { return &apiFailure{status, code, message} }
func apiError(w http.ResponseWriter, err error) {
	var e *apiFailure
	if errors.As(err, &e) {
		writeError(w, e.Status, e.Code, e.Message)
		return
	}
	status, code := 422, "validation_error"
	if errors.Is(err, project.ErrRecoveryRequired) {
		status, code = 503, "recovery_required"
	}
	if errors.Is(err, os.ErrNotExist) {
		status = 404
		code = "not_found"
	}
	if errors.Is(err, project.ErrBusy) || strings.Contains(err.Error(), "conflict") || strings.Contains(err.Error(), "referenced") || strings.Contains(err.Error(), "already exists") {
		status = 409
		code = "conflict"
	}
	writeError(w, status, code, err.Error())
}
func decodeBody(raw []byte, v any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	if err := storage.DecodeJSON(raw, v); err != nil {
		return fail(400, "invalid_json", err.Error())
	}
	return nil
}
func (s *Server) business(w http.ResponseWriter, r *http.Request) {
	if s.mediaArchiveAPI(w, r) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/"), "/")
	if strings.Contains(r.URL.EscapedPath(), "%2f") || strings.Contains(r.URL.EscapedPath(), "%2F") {
		writeError(w, 400, "invalid_path", "encoded slash forbidden")
		return
	}
	mutation := r.Method != "GET" && r.Method != "HEAD"
	var raw []byte
	if mutation {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			writeError(w, 415, "unsupported_media_type", "application/json required")
			return
		}
		raw, err = io.ReadAll(io.LimitReader(r.Body, (5<<20)+1))
		if err != nil {
			apiError(w, err)
			return
		}
		if len(raw) > 5<<20 {
			writeError(w, 413, "body_too_large", "JSON body exceeds limit")
			return
		}
		var value any
		if err := decodeBody(raw, &value); err != nil {
			apiError(w, err)
			return
		}
		raw, _ = json.Marshal(value)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		writeError(w, 503, "server_closed", "server closed")
		return
	}
	if pending, err := s.recoveryPending(); err != nil || pending {
		s.mu.Unlock()
		writeError(w, 503, "recovery_required", "workspace recovery required")
		return
	}
	if !mutation {
		data, err := s.readAPI(parts, r)
		s.mu.Unlock()
		if err != nil {
			apiError(w, err)
			return
		}
		writeJSON(w, 200, data)
		return
	}
	data, status, err := s.mutateAPI(parts, r.Method, raw)
	var notice *callback.Notification
	if err == nil && r.Method == "POST" && len(parts) == 3 && parts[0] == "chats" && parts[2] == "inputs" {
		n, e := s.notification(parts[1])
		if e != nil {
			err = fail(502, "input_saved_delivery_failed", "Input saved, but launcher notification could not be prepared: "+e.Error())
		} else {
			notice = &n
		}
	}
	// Always notify readers after a mutation; an input remains saved on inbox failure.
	s.publish("workspace.changed", map[string]any{"path": r.URL.Path})
	s.mu.Unlock()
	if err != nil {
		apiError(w, err)
		return
	}
	if notice != nil {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		err = s.dispatcher.Deliver(ctx, *notice)
		cancel()
		if err != nil {
			writeError(w, 502, "input_saved_delivery_failed", "Input saved; launcher delivery failed (not automatically retried): "+err.Error())
			return
		}
	}
	writeJSON(w, status, data)
}
func (s *Server) readAPI(p []string, r *http.Request) (any, error) {
	switch p[0] {
	case "project":
		if len(p) == 1 {
			c, err := s.store.Config()
			c.Provider = string(s.provider)
			return c, err
		}
	case "validate":
		if len(p) == 1 {
			return s.store.Validate()
		}
	case "characters":
		docs, err := s.store.Characters()
		if err != nil {
			return nil, err
		}
		if len(p) == 1 {
			return docs, nil
		}
		if len(p) == 2 {
			for _, d := range docs {
				if d.Metadata.ID == p[1] {
					return d, nil
				}
			}
			return nil, os.ErrNotExist
		}
	case "worldbook":
		if len(p) == 2 && p[1] == "categories" {
			return s.store.WorldbookCategories()
		}
		index, err := s.store.Worldbook()
		if err != nil {
			return nil, err
		}
		docs := index.Documents()
		if len(p) == 1 {
			return docs, nil
		}
		if len(p) == 3 {
			for _, d := range docs {
				if d.Category == p[1] && d.Slug == p[2] {
					return d, nil
				}
			}
			return nil, os.ErrNotExist
		}
	case "chats":
		if len(p) == 1 {
			return s.store.Timelines()
		}
		id := p[1]
		if len(p) == 2 {
			metas, err := s.store.Timelines()
			if err != nil {
				return nil, err
			}
			for _, m := range metas {
				if m.ID == id {
					return m, nil
				}
			}
			return nil, os.ErrNotExist
		}
		if len(p) == 3 && p[2] == "events" {
			rawEvents, err := s.store.History(id)
			if err != nil {
				return nil, err
			}
			events := timeline.ResolveHistory(rawEvents)
			if r.URL.Query().Get("inherited") == "false" {
				own := []timeline.HistoryEvent{}
				for _, e := range events {
					if e.Timeline == id {
						own = append(own, e)
					}
				}
				events = own
			}
			after := r.URL.Query().Get("after")
			before := r.URL.Query().Get("before")
			tail := r.URL.Query().Get("tail") == "true"
			if after != "" && (before != "" || tail) {
				return nil, fail(400, "invalid_cursor", "after cannot be combined with before or tail")
			}
			if before != "" {
				found := false
				for i, e := range events {
					if e.ID == before {
						events = events[:i]
						found = true
						break
					}
				}
				if !found {
					return nil, fail(404, "event_not_found", "unknown before event")
				}
			}
			if after != "" {
				found := false
				for i, e := range events {
					if e.ID == after {
						events = events[i+1:]
						found = true
						break
					}
				}
				if !found {
					return nil, fail(404, "event_not_found", "unknown after event")
				}
			}

			view := r.URL.Query().Get("view")
			if view != "" && view != "rp" && view != "story" && view != "diagnostic" {
				return nil, fail(400, "invalid_view", "view must be rp, story or diagnostic")
			}
			if view != "" {
				visible := []timeline.HistoryEvent{}
				for _, e := range events {
					isRP := e.Type == "input" || (e.Type == "output" && (e.Kind == "story" || e.Kind == "assistant"))
					if (view == "rp" && isRP) || (view == "story" && e.Type == "output" && e.Kind == "story") || (view == "diagnostic" && !isRP) {
						visible = append(visible, e)
					}
				}
				events = visible
			}
			limit := 200
			if q := r.URL.Query().Get("limit"); q != "" {
				n, err := strconv.Atoi(q)
				if err != nil || n < 1 || n > 10000 {
					return nil, fail(400, "invalid_limit", "limit must be 1..10000")
				}
				limit = n
			}
			if len(events) > limit {
				if tail || before != "" {
					events = events[len(events)-limit:]
				} else {
					events = events[:limit]
				}
			}
			return events, nil
		}

		if len(p) == 3 && p[2] == "state" {
			state, err := s.store.State(id)
			state.Settings.Provider = string(s.provider)
			return state, err
		}
		state, err := s.store.State(id)
		if err != nil {
			return nil, err
		}

		if len(p) == 3 && p[2] == "settings" {
			return s.store.StorySettings(id)
		}
		if len(p) == 5 && p[2] == "characters" && p[4] == "state" {
			return state.Characters[p[3]], nil
		}
	}
	return nil, fail(404, "route_not_found", "API route does not exist")
}
func (s *Server) mutateAPI(p []string, method string, raw []byte) (any, int, error) {
	var err error
	if (method == "DELETE" && !(len(p) == 4 && p[0] == "chats" && p[2] == "events")) || (len(p) == 2 && p[0] == "worldbook" && p[1] == "refresh") {
		if err = decodeBody(raw, &struct{}{}); err != nil {
			return nil, 0, err
		}
	}

	switch p[0] {
	case "project":
		if len(p) == 1 && method == "PATCH" {
			config, e := s.store.Config()
			if e != nil {
				return nil, 0, e
			}
			var patch struct {
				LastChat *string `json:"last_chat,omitempty"`
			}
			if err = decodeBody(raw, &patch); err != nil {
				break
			}
			if patch.LastChat != nil {
				config.LastChat = *patch.LastChat
			}
			err = s.store.UpdateConfig(config)
			return config, 200, err
		}
	case "characters":
		if len(p) == 1 && method == "POST" {
			var b struct {
				ID      string `json:"id"`
				Content string `json:"content"`
			}
			if err = decodeBody(raw, &b); err != nil {
				break
			}
			exists, e := s.store.ContentExists("characters/" + b.ID + "/card.md")
			if e != nil {
				return nil, 0, e
			}
			if exists {
				return nil, 0, fail(409, "already_exists", "character exists")
			}
			err = s.store.WriteCharacter(b.ID, []byte(b.Content))
			if err == nil {
				docs, _ := s.store.Characters()
				for _, d := range docs {
					if d.Metadata.ID == b.ID {
						return d, 201, nil
					}
				}
			}
			break
		}
		if len(p) == 2 && method == "PUT" {
			var b struct {
				Content string `json:"content"`
				Name    string `json:"name,omitempty"`
			}
			if err = decodeBody(raw, &b); err != nil {
				break
			}
			exists, e := s.store.ContentExists("characters/" + p[1] + "/card.md")
			if e != nil {
				return nil, 0, e
			}
			if !exists {
				return nil, 0, os.ErrNotExist
			}
			name := p[1]
			if b.Name != "" && b.Name != name {
				err = s.store.RenameCharacter(name, b.Name, []byte(b.Content))
				name = b.Name
			} else {
				err = s.store.WriteCharacter(name, []byte(b.Content))
			}
			if err == nil {
				docs, _ := s.store.Characters()
				for _, d := range docs {
					if d.Metadata.ID == name {
						return d, 200, nil
					}
				}
			}
			break
		}
		if len(p) == 2 && method == "DELETE" {
			err = s.store.DeleteCharacter(p[1])
			return map[string]bool{"deleted": err == nil}, 200, err
		}
	case "worldbook":
		if len(p) == 2 && p[1] == "categories" && method == "POST" {
			var b struct {
				Name string `json:"name"`
			}
			if err := decodeBody(raw, &b); err != nil {
				return nil, 0, err
			}
			err := s.store.CreateWorldbookCategory(b.Name)
			return map[string]string{"name": b.Name}, 201, err
		}
		if len(p) == 2 && p[1] == "refresh" && method == "POST" {
			idx, e := s.store.Worldbook()
			if e != nil {
				return nil, 0, e
			}
			return idx.Documents(), 200, nil
		}
		if len(p) == 1 && method == "POST" {
			var b struct {
				Category string `json:"category"`
				Slug     string `json:"slug"`
				Content  string `json:"content"`
			}
			if err = decodeBody(raw, &b); err != nil {
				break
			}
			exists, e := s.store.ContentExists("worldbook/" + b.Category + "/" + b.Slug + ".md")
			if e != nil {
				return nil, 0, e
			}
			if exists {
				return nil, 0, fail(409, "already_exists", "worldbook entry exists")
			}
			err = s.store.WriteWorldbook(b.Category, b.Slug, []byte(b.Content))
			if err == nil {
				return s.worldbookDocument(b.Category, b.Slug, 201)
			}
			break
		}
		if len(p) == 3 && method == "PUT" {
			var b struct {
				Content string `json:"content"`
				Name    string `json:"name,omitempty"`
			}
			if err = decodeBody(raw, &b); err != nil {
				break
			}
			exists, e := s.store.ContentExists("worldbook/" + p[1] + "/" + p[2] + ".md")
			if e != nil {
				return nil, 0, e
			}
			if !exists {
				return nil, 0, os.ErrNotExist
			}
			name := p[2]
			if b.Name != "" && b.Name != name {
				err = s.store.RenameWorldbook(p[1], name, b.Name, []byte(b.Content))
				name = b.Name
			} else {
				err = s.store.WriteWorldbook(p[1], name, []byte(b.Content))
			}
			if err == nil {
				return s.worldbookDocument(p[1], name, 200)
			}
			break
		}
		if len(p) == 3 && method == "DELETE" {
			err = s.store.DeleteWorldbook(p[1], p[2])
			return map[string]bool{"deleted": err == nil}, 200, err
		}
	case "chats":
		if len(p) == 1 && method == "POST" {
			var b struct {
				Name       string                   `json:"name"`
				Narrative  *timeline.Narrative      `json:"narrative,omitempty"`
				Polishing  *timeline.Polishing      `json:"polishing,omitempty"`
				Worldbook  *timeline.WorldbookPatch `json:"worldbook,omitempty"`
				Characters map[string]bool          `json:"characters,omitempty"`
			}
			if err := decodeBody(raw, &b); err != nil {
				return nil, 0, err
			}
			meta, err := s.store.CreateTimeline(project.TimelineOptions{ID: b.Name, Overrides: timeline.SettingsPatch{Narrative: b.Narrative, Polishing: b.Polishing, Worldbook: b.Worldbook, Characters: b.Characters}})
			return meta, 201, err
		}

		if len(p) < 2 {
			break
		}
		id := p[1]
		if len(p) == 3 && p[2] == "branch" && method == "POST" {
			return s.branchChat(id, raw)
		}
		if len(p) == 4 && p[2] == "events" && (method == "PATCH" || method == "DELETE") {
			return s.reviseMessage(id, p[3], method, raw)
		}
		if len(p) == 2 && method == "PATCH" {
			var b struct {
				Name       string                   `json:"name,omitempty"`
				Archived   *bool                    `json:"archived,omitempty"`
				Narrative  *timeline.Narrative      `json:"narrative,omitempty"`
				Polishing  *timeline.Polishing      `json:"polishing,omitempty"`
				Worldbook  *timeline.WorldbookPatch `json:"worldbook,omitempty"`
				Characters map[string]bool          `json:"characters,omitempty"`
			}
			if err := decodeBody(raw, &b); err != nil {
				return nil, 0, err
			}
			meta, err := s.store.UpdateTimelineMetadata(id, b.Name, b.Archived, timeline.SettingsPatch{Narrative: b.Narrative, Polishing: b.Polishing, Worldbook: b.Worldbook, Characters: b.Characters})
			return meta, 200, err
		}

		if len(p) == 2 && method == "DELETE" {
			err = s.store.DeleteTimeline(id)
			return map[string]bool{"deleted": err == nil}, 200, err
		}
		if len(p) == 3 && p[2] == "settings" && method == "PATCH" {
			var b struct {
				Narrative  *timeline.Narrative      `json:"narrative,omitempty"`
				Polishing  *timeline.Polishing      `json:"polishing,omitempty"`
				Worldbook  *timeline.WorldbookPatch `json:"worldbook,omitempty"`
				Characters map[string]bool          `json:"characters,omitempty"`
			}
			if err := decodeBody(raw, &b); err != nil {
				return nil, 0, err
			}
			err := s.store.SetTimelineSettings(id, timeline.SettingsPatch{Narrative: b.Narrative, Polishing: b.Polishing, Worldbook: b.Worldbook, Characters: b.Characters})
			if err != nil {
				return nil, 0, err
			}
			settings, err := s.store.StorySettings(id)
			return settings, 200, err
		}
		if len(p) == 3 && p[2] == "inputs" && method == "POST" {
			var b struct {
				Speaker string                    `json:"speaker"`
				Text    string                    `json:"text"`
				Media   []timeline.MediaReference `json:"media,omitempty"`
			}
			if err = decodeBody(raw, &b); err != nil {
				break
			}
			event, e := s.store.Append(id, timeline.Event{Type: timeline.Input, Speaker: b.Speaker, Text: b.Text, Media: b.Media})
			if e != nil {
				return nil, 0, e
			}
			s.publish("timeline.event", event)
			return event, 201, nil
		}
		if len(p) == 3 && p[2] == "outputs" && method == "POST" {
			var b struct {
				Input      int                       `json:"input"`
				Kind       string                    `json:"kind"`
				Characters []string                  `json:"characters,omitempty"`
				Text       string                    `json:"text"`
				Media      []timeline.MediaReference `json:"media,omitempty"`
			}
			if err = decodeBody(raw, &b); err != nil {
				break
			}
			history, e := s.store.History(id)
			if e != nil {
				return nil, 0, e
			}
			if b.Input < 1 || b.Input > len(history) || history[b.Input-1].Type != timeline.Input {
				return nil, 0, fail(409, "input_conflict", "input position no longer exists")
			}
			event, e := s.store.Append(id, timeline.Event{Type: timeline.Output, InputID: history[b.Input-1].ID, Kind: b.Kind, Text: b.Text, Media: b.Media, Characters: b.Characters})
			if e != nil {
				return nil, 0, e
			}
			return timeline.HistoryEvent{Event: event, Position: len(history) + 1}, 201, nil
		}

		if len(p) == 5 && p[2] == "characters" && p[4] == "updates" && method == "POST" {
			var b struct {
				Document         *string `json:"document"`
				ExpectedDocument *string `json:"expected_document,omitempty"`
			}
			if err = decodeBody(raw, &b); err != nil {
				break
			}
			if b.Document == nil {
				return nil, 0, fail(422, "missing_document", "story state requires a complete document; submit an empty string to clear it")
			}
			err := s.store.UpdateCharacterState(id, p[3], *b.Document, b.ExpectedDocument)
			return map[string]string{"document": *b.Document}, 200, err
		}

	}
	if err != nil {
		return nil, 0, err
	}
	return nil, 0, fail(404, "route_not_found", "API route does not exist")
}
func (s *Server) worldbookDocument(category, slug string, status int) (any, int, error) {
	idx, err := s.store.Worldbook()
	if err != nil {
		return nil, 0, err
	}
	for _, d := range idx.Documents() {
		if d.Category == category && d.Slug == slug {
			return d, status, nil
		}
	}
	return nil, 0, os.ErrNotExist
}
