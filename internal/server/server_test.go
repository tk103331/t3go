package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/proto"
)

type testEnv struct {
	server *Server
	http   *httptest.Server
	token  string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg, err := config.Load(t.TempDir(), "127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	srv, err := New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		srv.Close()
	})
	return &testEnv{server: srv, http: ts, token: cfg.Token}
}

func (e *testEnv) request(t *testing.T, method, path string, body any, token string, out any) int {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, e.http.URL+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 400 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

func (e *testEnv) project(t *testing.T) proto.Project {
	t.Helper()
	var p proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects", map[string]string{"name": "demo", "path": t.TempDir()}, e.token, &p); code != http.StatusCreated {
		t.Fatalf("create project: %d", code)
	}
	return p
}

func (e *testEnv) thread(t *testing.T, projectID string) proto.Thread {
	t.Helper()
	var th proto.Thread
	body := map[string]any{"projectId": projectID, "title": "Test thread", "provider": "claude", "model": "test-model", "permission": "full-access"}
	if code := e.request(t, http.MethodPost, "/api/v1/threads", body, e.token, &th); code != http.StatusCreated {
		t.Fatalf("create thread: %d", code)
	}
	return th
}

func TestHealthAndAuth(t *testing.T) {
	e := newTestEnv(t)
	if code := e.request(t, http.MethodGet, "/api/v1/health", nil, "", nil); code != http.StatusOK {
		t.Fatalf("health = %d", code)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/projects", nil, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthorized projects = %d", code)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/projects?token="+e.token, nil, "", nil); code != http.StatusOK {
		t.Fatalf("query token projects = %d", code)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/projects", nil, e.token, nil); code != http.StatusOK {
		t.Fatalf("authorized projects = %d", code)
	}
}

func TestProjectThreadAndDetail(t *testing.T) {
	e := newTestEnv(t)
	p := e.project(t)
	th := e.thread(t, p.ID)

	var detail proto.ThreadDetail
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+th.ID, nil, e.token, &detail); code != http.StatusOK {
		t.Fatalf("detail = %d", code)
	}
	if detail.Thread.ID != th.ID || detail.Messages == nil || detail.Runs == nil {
		t.Fatalf("detail = %#v", detail)
	}
	var updated proto.Thread
	if code := e.request(t, http.MethodPatch, "/api/v1/threads/"+th.ID, map[string]string{"title": "Renamed"}, e.token, &updated); code != http.StatusOK {
		t.Fatalf("patch = %d", code)
	}
	if updated.Title != "Renamed" {
		t.Fatalf("title = %q", updated.Title)
	}
	if code := e.request(t, http.MethodDelete, "/api/v1/threads/"+th.ID, nil, e.token, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+th.ID, nil, e.token, nil); code != http.StatusNotFound {
		t.Fatalf("missing thread = %d", code)
	}
}

func TestSSEStreamsAndReplays(t *testing.T) {
	e := newTestEnv(t)
	p := e.project(t)
	e.thread(t, p.ID) // events exist before the stream opens

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.http.URL+"/api/v1/events?after=0", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	var sawCreated bool
	for !sawCreated {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("sse read: %v", err)
		}
		if strings.TrimSpace(line) == "event: "+proto.EventThreadCreated {
			sawCreated = true
		}
	}
}

func TestWebSocketHelloAndEvent(t *testing.T) {
	e := newTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, e.http.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + e.token}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	var hello proto.WSFrame
	if err := wsjson.Read(ctx, c, &hello); err != nil {
		t.Fatalf("hello: %v", err)
	}
	if hello.Type != proto.WSHello || hello.ServerID != e.server.ID {
		t.Fatalf("hello = %#v", hello)
	}

	p := e.project(t)
	e.thread(t, p.ID)
	for {
		var frame proto.WSFrame
		if err := wsjson.Read(ctx, c, &frame); err != nil {
			t.Fatalf("event: %v", err)
		}
		if frame.Type == proto.WSEvent && frame.Event != nil && frame.Event.Type == proto.EventThreadCreated {
			if frame.Event.ThreadID == "" {
				t.Fatal("thread.created without a thread id")
			}
			return
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	e := newTestEnv(t)
	var settings proto.Settings
	if code := e.request(t, http.MethodGet, "/api/v1/settings", nil, e.token, &settings); code != http.StatusOK {
		t.Fatalf("get settings = %d", code)
	}
	settings.Title = "Test Environment"
	if code := e.request(t, http.MethodPatch, "/api/v1/settings", settings, e.token, nil); code != http.StatusOK {
		t.Fatalf("patch settings = %d", code)
	}
	if got := e.server.Service.Settings(context.Background()); got.Title != "Test Environment" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestListDir(t *testing.T) {
	e := newTestEnv(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Path string `json:"path"`
		Dirs []struct {
			Name string `json:"name"`
		} `json:"dirs"`
	}
	if code := e.request(t, http.MethodGet, "/api/v1/fs?path="+dir, nil, e.token, &out); code != http.StatusOK {
		t.Fatalf("fs = %d", code)
	}
	if len(out.Dirs) != 1 || out.Dirs[0].Name != "alpha" {
		t.Fatalf("dirs = %#v", out.Dirs)
	}
}
