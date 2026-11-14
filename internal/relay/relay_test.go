package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tk/t3go/internal/environment"
)

func TestRelayIsolationStreamingWebSocketAndRevocation(t *testing.T) {
	const admin = "administrator-token-for-relay-tests"
	home := t.TempDir()
	hub, err := New(home, admin)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	hub.Mount(mux)
	ts := httptest.NewServer(mux)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); hub.Close(); ts.Close() })
	hc := &http.Client{Timeout: 5 * time.Second}
	request := func(method, path, token string, body io.Reader) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, ts.URL+path, body)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	waitOnline := func(id string) {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			hub.mu.Lock()
			p := hub.peers[id]
			online := p != nil && !p.session.IsClosed()
			hub.mu.Unlock()
			if online {
				return
			}
			select {
			case <-tick.C:
			case <-deadline.C:
				t.Fatal("tunnel never became online")
			}
		}
	}
	type registeredEnv struct{ id, token string }
	var envs []registeredEnv
	for index := range 2 {
		identity, _ := environment.Generate()
		token := fmt.Sprintf("environment-%d-private-access-token", index)
		local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/environment" {
				_ = json.NewEncoder(w).Encode(identity.Describe("instance", "test", r.URL.Query().Get("challenge")))
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+token && r.URL.Query().Get("token") != token {
				w.WriteHeader(401)
				return
			}
			switch r.URL.Path {
			case "/ws":
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				for {
					kind, data, err := conn.Read(r.Context())
					if err != nil {
						return
					}
					if err := conn.Write(r.Context(), kind, data); err != nil {
						return
					}
				}
			case "/events":
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: first\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			case "/upload":
				_, _ = io.Copy(w, r.Body)
			default:
				_, _ = io.WriteString(w, fmt.Sprintf("%d:%s:%s", index, r.URL.Path, r.URL.RawQuery))
			}
		}))
		t.Cleanup(local.Close)
		raw, _ := json.Marshal(Registration{ID: identity.ID, PublicKey: identity.PublicKey(), Token: token, Name: fmt.Sprint(index)})
		resp := request("POST", "/api/v1/relay/environments", admin, bytes.NewReader(raw))
		if resp.StatusCode != 201 {
			t.Fatalf("register: %d", resp.StatusCode)
		}
		var registered Registered
		_ = json.NewDecoder(resp.Body).Decode(&registered)
		resp.Body.Close()
		c := Connector{Server: ts.URL, EnvironmentID: identity.ID, Token: registered.ConnectToken, Target: strings.TrimPrefix(local.URL, "http://")}
		go c.Run(ctx)
		waitOnline(identity.ID)
		envs = append(envs, registeredEnv{identity.ID, token})
	}
	for index, env := range envs {
		resp := request("GET", "/e/"+env.id+"/files/a%20b?q=x", env.token, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || string(body) != fmt.Sprintf("%d:/files/a b:q=x", index) {
			t.Fatalf("wrong environment/path: %d %q", resp.StatusCode, body)
		}
		resp = request("GET", "/e/"+env.id+"/files", envs[1-index].token, nil)
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatal("cross-environment token accepted")
		}
	}
	env := envs[0]
	upload := strings.Repeat("attachment-content", 1<<16)
	resp := request("POST", "/e/"+env.id+"/upload", env.token, strings.NewReader(upload))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != upload {
		t.Fatal("streaming upload corrupted")
	}
	hub.mu.Lock()
	previous := hub.peers[env.id]
	hub.mu.Unlock()
	_ = previous.session.Close()
	waitOnline(env.id)
	hub.mu.Lock()
	reconnected := hub.peers[env.id] != previous
	hub.mu.Unlock()
	if !reconnected {
		t.Fatal("connector did not replace the closed tunnel")
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	req, _ := http.NewRequestWithContext(streamCtx, "GET", ts.URL+"/e/"+env.id+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	resp, err = hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, len("data: first\n\n"))
	_, err = io.ReadFull(resp.Body, first)
	streamCancel()
	resp.Body.Close()
	if err != nil || string(first) != "data: first\n\n" {
		t.Fatal("SSE did not flush before EOF", err)
	}
	socketURL := strings.Replace(ts.URL, "http://", "ws://", 1) + "/e/" + env.id + "/ws?token=" + env.token
	socketCtx, socketCancel := context.WithTimeout(ctx, 5*time.Second)
	defer socketCancel()
	socket, _, err := websocket.Dial(socketCtx, socketURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	if err = socket.Write(socketCtx, websocket.MessageText, []byte("terminal input")); err != nil {
		t.Fatal(err)
	}
	_, data, err := socket.Read(socketCtx)
	if err != nil || string(data) != "terminal input" {
		t.Fatal("websocket proxy failed", err)
	}
	resp = request("DELETE", "/api/v1/relay/environments/"+env.id, admin, nil)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("revocation failed")
	}
	if _, _, err := socket.Read(socketCtx); err == nil {
		t.Fatal("revocation left an active websocket open")
	}
	resp = request("GET", "/e/"+env.id+"/files", env.token, nil)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("revoked environment still accessible")
	}
	raw, err := os.ReadFile(hub.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(envs[1].token)) || bytes.Contains(raw, []byte(admin)) {
		t.Fatal("raw API secret persisted by hub")
	}
	reloaded, err := New(home, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if len(reloaded.bindings) != 1 || reloaded.bindings[envs[1].id].ID == "" {
		t.Fatal("binding persistence lost")
	}
}
