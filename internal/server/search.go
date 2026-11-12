package server

import (
	"net/http"
	"strconv"
)

// handleSearchMessages finds message text across visible threads.
func (s *Server) handleSearchMessages(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			limit = value
		}
	}
	hits, err := s.Service.SearchMessages(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		writeError(w, statusFor(err), "search", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"messages": nonNil(hits)})
}
