package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTerminalPageUsesNativeBridge(t *testing.T) {
	page := terminalPageFor("http://example.test", false)
	if strings.Contains(page, "new WebSocket") {
		t.Fatal("the terminal page still opens a WebView WebSocket")
	}
	for _, want := range []string{"window.nsgo.postMessage", "type: 'ready'", "type: 'input'"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the terminal page is missing %q", want)
		}
	}
}

func TestTerminalBridgeForwardsFrames(t *testing.T) {
	messages := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		payload, _ := json.Marshal(map[string]any{"type": "output", "data": "echo:" + string(data)})
		_ = conn.Write(ctx, websocket.MessageText, payload)
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()

	bridge := newTerminalBridge()
	defer bridge.close()
	bridge.connect(func(message string) { messages <- message }, "term-1",
		"ws"+strings.TrimPrefix(server.URL, "http"))

	waitTerminalBridgeMessage(t, messages, func(message map[string]any) bool {
		return message["type"] == "bridge" && message["state"] == "connected"
	})
	if bridge.send("other-term", map[string]any{"type": "input", "data": "wrong"}) {
		t.Fatal("the bridge sent input for another session")
	}
	if !bridge.send("term-1", map[string]any{"type": "input", "data": "hello"}) {
		t.Fatal("the bridge did not send terminal input")
	}
	waitTerminalBridgeMessage(t, messages, func(message map[string]any) bool {
		data, _ := message["data"].(string)
		return message["type"] == "output" && strings.HasPrefix(data, "echo:") && strings.Contains(data, `"hello"`)
	})
}

func waitTerminalBridgeMessage(t *testing.T, messages <-chan string, match func(map[string]any) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw := <-messages:
			var message map[string]any
			if json.Unmarshal([]byte(raw), &message) == nil && match(message) {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for a terminal bridge message")
		}
	}
}
