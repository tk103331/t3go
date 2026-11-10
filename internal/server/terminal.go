package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/terminal"
)

// handleEnsureTerminal starts or reuses the thread's shell.
func (s *Server) handleEnsureTerminal(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Thread(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	dir := s.Service.ThreadDir(r.Context(), t)
	if dir == "" {
		writeError(w, 400, "terminal", "this thread has no checkout")
		return
	}
	session, err := s.Terminals.Ensure(t.ID, dir, "")
	if err != nil {
		writeError(w, 500, "terminal", err.Error())
		return
	}
	writeJSON(w, 200, proto.TerminalSession{ID: session.ID, ThreadID: session.ThreadID, Shell: session.Shell,
		Cwd: session.Cwd, Cols: session.Cols(), Rows: session.Rows()})
}

// handleCloseTerminal ends one shell.
func (s *Server) handleCloseTerminal(w http.ResponseWriter, r *http.Request) {
	s.Terminals.Close(r.PathValue("id"))
	writeJSON(w, 200, map[string]bool{"closed": true})
}

// handleTerminalWS streams a shell's output and takes its input.
func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	session, ok := s.Terminals.Get(r.PathValue("id"))
	if !ok {
		writeError(w, 404, "terminal", terminal.ErrNotFound.Error())
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	snapshot, out, _, detach := session.Subscribe()
	defer detach()

	write := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
		defer writeCancel()
		return conn.Write(writeCtx, websocket.MessageText, data)
	}
	for len(snapshot) > 0 {
		chunk := snapshot
		if len(chunk) > 32<<10 {
			chunk = chunk[:32<<10]
		}
		snapshot = snapshot[len(chunk):]
		if err := write(map[string]any{"type": "output", "data": string(chunk)}); err != nil {
			return
		}
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for chunk := range out {
			if err := write(map[string]any{"type": "output", "data": string(chunk)}); err != nil {
				return
			}
		}
		_ = write(map[string]any{"type": "exit", "code": session.ExitCode()})
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			continue
		}
		var message struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		switch message.Type {
		case "input":
			if message.Data != "" {
				_ = session.Write([]byte(message.Data))
			}
		case "resize":
			_ = session.Resize(message.Cols, message.Rows)
		}
	}
	cancel()
	<-writerDone
}
