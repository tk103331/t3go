package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// handleSSE streams recorded and live events as Server-Sent Events.
//
// Clients pass ?after=<seq> to resume after the last sequence they saw.
// Without it the stream starts at the newest recorded event so a client
// that has just fetched its state does not replay the whole history.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream", "streaming unsupported")
		return
	}

	after := s.Bus.LastSeq(r.Context())
	if raw := r.URL.Query().Get("after"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			after = v
		}
	} else if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			after = v
		}
	}

	// Subscribe before replaying so nothing published during the replay is
	// lost. Events seen in both paths are skipped by sequence number.
	live, unsubscribe := s.Bus.Subscribe()
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 2000\n\n")
	flusher.Flush()

	sent := after
	writeEvent := func(e proto.Event) bool {
		if e.Seq > 0 && e.Seq <= sent {
			return true
		}
		data, err := json.Marshal(e)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, data); err != nil {
			return false
		}
		if e.Seq > sent {
			sent = e.Seq
		}
		return true
	}

	// Replay what the client missed, in bounded pages.
	for {
		events, err := s.Bus.Replay(r.Context(), sent, 500)
		if err != nil {
			s.Log.Printf("sse: replay: %v", err)
			return
		}
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			if !writeEvent(e) {
				return
			}
		}
		if len(events) < 500 {
			break
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.closing:
			return
		case e, ok := <-live:
			if !ok {
				return
			}
			if !writeEvent(e) {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": keepalive %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
