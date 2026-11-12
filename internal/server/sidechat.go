package server

import (
	"net/http"

	"github.com/tk/t3go/internal/editor"

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

// handleOpenEditor opens a thread's checkout in an editor on the
// environment's machine.
func (s *Server) handleOpenEditor(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Editor string `json:"editor"`
	}
	_ = decodeJSON(r, &input)
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	dir := s.Service.ThreadDir(r.Context(), t)
	if err := editor.Open(r.Context(), dir, input.Editor); err != nil {
		writeError(w, 400, "editor", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
