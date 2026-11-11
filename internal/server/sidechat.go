package server

import (
	"net/http"

	"github.com/tk/t3go/internal/proto"
)

// handleSideChat returns the thread's hidden side chat, creating it on
// first use.
func (s *Server) handleSideChat(w http.ResponseWriter, r *http.Request) {
	thread, err := s.Service.CreateSideChat(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "side chat", err.Error())
		return
	}
	writeJSON(w, 200, thread)
}

// visibleThreads leaves side chats out of the thread lists.
func visibleThreads(threads []proto.Thread) []proto.Thread {
	out := make([]proto.Thread, 0, len(threads))
	for _, thread := range threads {
		if !thread.SideChat {
			out = append(out, thread)
		}
	}
	return out
}
