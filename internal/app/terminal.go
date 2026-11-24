package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// terminalSession is a shell attached to a thread's checkout, as the
// platform's terminal library offers it.
type terminalSession interface {
	// Done is closed when the shell is gone.
	Done() <-chan struct{}
	// Close ends the shell.
	Close() error
}

// terminalHub owns the open terminal sessions. Each one connects to the
// environment's terminal WebSocket and shows through the terminal
// plugin's view, which draws the shell itself.
//
// The terminal plugin needs the libghostty-vt library, which ships with
// desktop apps; a phone has no terminal surface, and its hub is a stub.
type terminalHub struct {
	mu    sync.Mutex
	terms map[string]terminalSession
	conns map[string]*terminalConn
}

func newTerminalHub() *terminalHub {
	return &terminalHub{terms: map[string]terminalSession{}, conns: map[string]*terminalConn{}}
}

// get returns the open terminal of one session, opening it on first use.
func (h *terminalHub) get(sessionID string) terminalSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.terms[sessionID]
}

// open attaches a terminal to the session's WebSocket.
func (h *terminalHub) open(sessionID, rawURL string) terminalSession {
	h.mu.Lock()
	if t := h.terms[sessionID]; t != nil {
		h.mu.Unlock()
		return t
	}
	conn := &terminalConn{url: rawURL}
	t, err := newTerminalSession(conn)
	if err != nil {
		h.mu.Unlock()
		return nil
	}
	h.terms[sessionID] = t
	h.conns[sessionID] = conn
	h.mu.Unlock()
	return t
}

// close hangs every terminal up.
func (h *terminalHub) close() {
	h.mu.Lock()
	terms, conns := h.terms, h.conns
	h.terms, h.conns = map[string]terminalSession{}, map[string]*terminalConn{}
	h.mu.Unlock()
	for _, t := range terms {
		t.Close()
	}
	for _, c := range conns {
		c.hangUp()
	}
}

// closeSession hangs one terminal up and forgets it.
func (h *terminalHub) closeSession(sessionID string) {
	h.mu.Lock()
	t, c := h.terms[sessionID], h.conns[sessionID]
	delete(h.terms, sessionID)
	delete(h.conns, sessionID)
	h.mu.Unlock()
	if t != nil {
		t.Close()
	}
	if c != nil {
		c.hangUp()
	}
}

// exited reports whether the session's terminal is gone.
func (h *terminalHub) exited(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	t := h.terms[sessionID]
	if t == nil {
		return false
	}
	select {
	case <-t.Done():
		return true
	default:
		return false
	}
}

// terminalConn is a terminal plugin Conn over the environment's terminal
// WebSocket. The server sends {"type":"output","data":...} frames and
// takes {"type":"input","data":...} and {"type":"resize",...} ones.
type terminalConn struct {
	url string

	mu      sync.Mutex
	cond    *sync.Cond
	pending []byte
	closed  bool
	conn    *websocket.Conn
	dialing bool
	readErr error
	writeMu sync.Mutex
}

func (c *terminalConn) dial() {
	c.mu.Lock()
	if c.conn != nil || c.dialing || c.closed {
		c.mu.Unlock()
		return
	}
	c.dialing = true
	c.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		conn, resp, err := websocket.Dial(ctx, c.url, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		c.mu.Lock()
		c.dialing = false
		if err != nil {
			c.readErr = err
			c.closed = true
			if c.cond != nil {
				c.cond.Broadcast()
			}
			c.mu.Unlock()
			return
		}
		conn.SetReadLimit(1 << 20)
		c.conn = conn
		if c.cond != nil {
			c.cond.Broadcast()
		}
		c.mu.Unlock()
		// The pump moves every frame's payload into the pending buffer
		// the terminal's reads drain.
		for {
			typ, data, readErr := conn.Read(ctx)
			if readErr != nil {
				c.mu.Lock()
				c.readErr = readErr
				c.closed = true
				if c.cond != nil {
					c.cond.Broadcast()
				}
				c.mu.Unlock()
				_ = conn.CloseNow()
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var frame struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if json.Unmarshal(data, &frame) != nil || frame.Data == "" {
				continue
			}
			c.mu.Lock()
			c.pending = append(c.pending, frame.Data...)
			if c.cond != nil {
				c.cond.Broadcast()
			}
			c.mu.Unlock()
		}
	}()
}

func (c *terminalConn) Read(p []byte) (int, error) {
	c.dial()
	c.mu.Lock()
	if c.cond == nil {
		c.cond = sync.NewCond(&c.mu)
	}
	for len(c.pending) == 0 {
		if c.closed {
			err := c.readErr
			c.mu.Unlock()
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		c.cond.Wait()
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	c.mu.Unlock()
	return n, nil
}

func (c *terminalConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return 0, io.ErrClosedPipe
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"input","data":`+quoteJSON(p)+`}`))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// quoteJSON encodes p as one JSON string, whatever bytes it holds.
func quoteJSON(p []byte) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(string(p))
	s := buf.String()
	return s[:len(s)-1] // the encoder's newline
}

// Resize tells the shell the terminal's size.
func (c *terminalConn) Resize(cols, rows int) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return nil
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func (c *terminalConn) hangUp() {
	c.mu.Lock()
	c.closed = true
	conn := c.conn
	c.conn = nil
	cond := c.cond
	c.mu.Unlock()
	if conn != nil {
		_ = conn.CloseNow()
	}
	if cond != nil {
		cond.Broadcast()
	}
}

func (c *terminalConn) Close() error {
	c.hangUp()
	return nil
}
