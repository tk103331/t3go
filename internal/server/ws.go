package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/tk/t3go/internal/proto"
)

// handleWS streams events over a WebSocket and accepts tiny control frames
// (ping, subscribe, unsubscribe). All mutations go through the REST API;
// the socket is a push channel with client-side filtering.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	defer c.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var writeMu sync.Mutex
	write := func(v any) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
		defer writeCancel()
		return c.Write(writeCtx, websocket.MessageText, data)
	}

	var filterMu sync.RWMutex
	var subscribed map[string]bool

	allowed := func(e proto.Event) bool {
		filterMu.RLock()
		defer filterMu.RUnlock()
		if len(subscribed) == 0 || e.ThreadID == "" {
			return true
		}
		return subscribed[e.ThreadID]
	}

	hello := proto.WSFrame{
		Type:     proto.WSHello,
		Protocol: proto.ProtocolVersion,
		ServerID: s.ID,
		Result: map[string]any{
			"lastSeq":  s.Bus.LastSeq(ctx),
			"serverId": s.ID,
			"name":     s.Service.Settings(ctx).Title,
		},
	}
	if err := write(hello); err != nil {
		return
	}

	live, unsubscribe := s.Bus.Subscribe()
	defer unsubscribe()

	go func() {
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var frame proto.WSFrame
			if err := json.Unmarshal(data, &frame); err != nil {
				_ = write(proto.WSFrame{Type: proto.WSResponse, Error: "invalid frame"})
				continue
			}
			switch frame.Type {
			case proto.WSPing:
				_ = write(proto.WSFrame{Type: proto.WSPong, ID: frame.ID})
			case proto.WSSubscribe:
				filterMu.Lock()
				if subscribed == nil {
					subscribed = map[string]bool{}
				}
				if frame.ThreadID != "" {
					subscribed[frame.ThreadID] = true
				} else {
					subscribed = nil
				}
				filterMu.Unlock()
				_ = write(proto.WSFrame{Type: proto.WSResponse, ID: frame.ID, Result: map[string]any{"ok": true}})
			case proto.WSUnsubscribe:
				filterMu.Lock()
				if frame.ThreadID == "" || len(subscribed) == 0 {
					subscribed = nil
				} else {
					delete(subscribed, frame.ThreadID)
				}
				filterMu.Unlock()
				_ = write(proto.WSFrame{Type: proto.WSResponse, ID: frame.ID, Result: map[string]any{"ok": true}})
			case proto.WSRequest:
				_ = write(proto.WSFrame{Type: proto.WSResponse, ID: frame.ID, Error: "requests are not supported over the event socket; use the REST API"})
			default:
				_ = write(proto.WSFrame{Type: proto.WSResponse, ID: frame.ID, Error: "unknown frame type: " + frame.Type})
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closing:
			return
		case e, ok := <-live:
			if !ok {
				return
			}
			if !allowed(e) {
				continue
			}
			ev := e
			if err := write(proto.WSFrame{Type: proto.WSEvent, Event: &ev}); err != nil {
				return
			}
		}
	}
}
