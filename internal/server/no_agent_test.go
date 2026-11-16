package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/proto"
)

func newNoAgentEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg, err := config.Load(t.TempDir(), "127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.NoAgent = true
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

func TestNoAgentEnvironmentRefusesRuns(t *testing.T) {
	e := newNoAgentEnv(t)
	thread := e.thread(t, e.project(t).ID)

	code, raw := e.rawRequest(t, http.MethodPost, "/api/v1/threads/"+thread.ID+"/messages",
		map[string]any{"content": "run something"}, e.token)
	if code != http.StatusForbidden {
		t.Fatalf("send = %d, want 403", code)
	}
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &failure); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, raw)
	}
	if failure.Error.Code != "run" || failure.Error.Message != "this environment does not run agent tasks" {
		t.Fatalf("send error = %#v", failure.Error)
	}

	if code := e.request(t, http.MethodPost, "/api/v1/threads/"+thread.ID+"/retry", map[string]any{}, e.token, nil); code == http.StatusAccepted {
		t.Fatalf("retry = %d, want a refusal", code)
	}
	var side proto.Thread
	if code := e.request(t, http.MethodPost, "/api/v1/threads/"+thread.ID+"/side-chat", map[string]any{}, e.token, &side); code != http.StatusOK {
		t.Fatalf("side chat = %d", code)
	}
	sideCode, _ := e.rawRequest(t, http.MethodPost, "/api/v1/threads/"+side.ID+"/messages",
		map[string]any{"content": "run something"}, e.token)
	if sideCode != http.StatusForbidden {
		t.Fatalf("side chat message = %d, want 403", sideCode)
	}

	runs, err := e.server.Store.Runs(t.Context(), thread.ID)
	if err != nil {
		t.Fatalf("runs: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %d, want none", len(runs))
	}
}

func TestNoAgentEnvironmentStillServesThreadsAndFiles(t *testing.T) {
	e := newNoAgentEnv(t)
	project := e.project(t)
	thread := e.thread(t, project.ID)

	var state proto.State
	if code := e.request(t, http.MethodGet, "/api/v1/state", nil, e.token, &state); code != http.StatusOK {
		t.Fatalf("state = %d", code)
	}
	if len(state.Threads) != 1 || state.Threads[0].ID != thread.ID {
		t.Fatalf("threads = %#v", state.Threads)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("empty state")
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID, nil, e.token, nil); code != http.StatusOK {
		t.Fatalf("thread = %d", code)
	}
}

// rawRequest sends a request and returns the status and the body, so a
// test can read an error payload.
func (e *testEnv) rawRequest(t *testing.T, method, path string, body any, token string) (int, []byte) {
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
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, raw
}
