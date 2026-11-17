package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type streamEvent struct {
	ID   uint64
	Kind string
	Data any
}

// Events are notifications, not the durable data source. Every reconnect gets
// an explicit resync marker, even if an in-memory notification has been lost.
func (s *Server) publish(kind string, data any) {
	s.sequence++
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	e := streamEvent{s.sequence, kind, json.RawMessage(raw)}
	for ch := range s.subscribers {
		select {
		case ch <- e:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- streamEvent{s.sequence, "resync.required", map[string]any{"reason": "slow_consumer"}}:
			default:
			}
		}
	}
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_unavailable", "streaming unavailable")
		return
	}
	ch := make(chan streamEvent, 64)
	s.mu.Lock()
	s.subscribers[ch] = true
	sequence := s.sequence
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, ch); s.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(e streamEvent) bool {
		raw, err := json.Marshal(e.Data)
		if err != nil {
			return false
		}
		_, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", strconv.FormatUint(e.ID, 10), e.Kind, raw)
		flusher.Flush()
		return err == nil
	}
	if !send(streamEvent{sequence, "server.status", s.Snapshot()}) {
		return
	}
	if !send(streamEvent{sequence, "resync.required", map[string]any{"reason": "refresh_from_persisted_workspace", "last_event_id": r.Header.Get("Last-Event-ID")}}) {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case e := <-ch:
			if !send(e) {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
