package server

import "net/http"

func (s *Server) handleEnvironment(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("challenge")
	if len(challenge) > 128 {
		writeError(w, http.StatusBadRequest, "challenge", "challenge is too long")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.Identity.Describe(s.ID, s.Service.Settings(r.Context()).Title, challenge))
}
