package app

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// terminalBridge owns the native terminal WebSocket. The xterm page
// receives server frames through the view's web-view bridge and sends
// input back through it. This avoids Android WebView's cleartext policy.
type terminalBridge struct {
	mu         sync.Mutex
	writeMu    sync.Mutex
	generation uint64
	sessionID  string
	cancel     context.CancelFunc
	conn       *websocket.Conn
}

func newTerminalBridge() *terminalBridge {
	return &terminalBridge{}
}

// connect opens the environment's terminal WebSocket for sessionID and
// delivers every frame to emit. emit is called from other goroutines; the
// view's emit must hop to the main thread with the window's Update.
func (b *terminalBridge) connect(emit func(string), sessionID, rawURL string) {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	if b.conn != nil {
		_ = b.conn.CloseNow()
	}
	b.generation++
	generation := b.generation
	b.sessionID = sessionID
	b.cancel = cancel
	b.conn = nil
	b.mu.Unlock()

	go func() {
		defer cancel()
		dialCtx, dialCancel := context.WithTimeout(ctx, 15*time.Second)
		conn, resp, err := websocket.Dial(dialCtx, rawURL, nil)
		dialCancel()
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			b.finish(generation, emit)
			return
		}
		conn.SetReadLimit(1 << 20)
		b.mu.Lock()
		if b.generation != generation {
			b.mu.Unlock()
			_ = conn.CloseNow()
			return
		}
		b.conn = conn
		b.mu.Unlock()
		emitJSON(emit, map[string]any{"type": "bridge", "state": "connected"})
		for {
			typ, data, readErr := conn.Read(ctx)
			if readErr != nil {
				break
			}
			if typ == websocket.MessageText {
				emit(string(data))
			}
		}
		_ = conn.CloseNow()
		b.finish(generation, emit)
	}()
}

func (b *terminalBridge) finish(generation uint64, emit func(string)) {
	b.mu.Lock()
	if b.generation != generation {
		b.mu.Unlock()
		return
	}
	b.conn = nil
	b.cancel = nil
	b.mu.Unlock()
	emitJSON(emit, map[string]any{"type": "bridge", "state": "disconnected"})
}

func (b *terminalBridge) send(sessionID string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	b.mu.Lock()
	conn := b.conn
	matches := conn != nil && b.sessionID == sessionID
	b.mu.Unlock()
	if !matches {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return conn.Write(ctx, websocket.MessageText, data) == nil
}

func (b *terminalBridge) close() {
	b.mu.Lock()
	b.generation++
	cancel := b.cancel
	conn := b.conn
	b.cancel, b.conn, b.sessionID = nil, nil, ""
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.CloseNow()
	}
}

func emitJSON(emit func(string), value any) {
	data, err := json.Marshal(value)
	if err == nil {
		emit(string(data))
	}
}

// terminalPageMessage feeds one message from the terminal's xterm page to
// the bridge. emit delivers a JSON frame to the page; the view owns the
// transport and must hop emit back to the main thread.
func (s *Session) terminalPageMessage(sessionID, data string, emit func(string)) {
	var message struct {
		Type string `json:"type"`
		Data string `json:"data"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if json.Unmarshal([]byte(data), &message) != nil {
		return
	}
	switch message.Type {
	case "ready":
		c := s.client()
		if c == nil {
			return
		}
		s.terminalBridge.connect(emit, sessionID, c.TerminalURL(sessionID))
	case "input":
		s.terminalBridge.send(sessionID, map[string]any{"type": "input", "data": message.Data})
	case "resize":
		s.terminalBridge.send(sessionID, map[string]any{"type": "resize", "cols": message.Cols, "rows": message.Rows})
	case "exit":
		s.terminalBridge.close()
		s.loadTerminalSessions()
	case "close":
		s.terminalBridge.close()
	}
}

// TODO(view): the terminal surface builds the xterm page (the embedded
// xterm.js assets and terminalPageFor template of the old client), feeds
// its messages to terminalPageMessage, and reloads the page when the
// environment's serverURL changes routes.
