package server

import (
	"net/http"
	"strconv"
)

// handleUsage summarizes runs over the last days.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			days = value
		}
	}
	report, err := s.Service.Usage(r.Context(), days)
	if err != nil {
		writeError(w, statusFor(err), "usage", err.Error())
		return
	}
	writeJSON(w, 200, report)
}
