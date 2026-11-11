package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
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

// TestStateCarriesLastSeq pins the snapshot's sequence: a client that
// streams after it sees only what changed since, not the whole history.
func TestStateCarriesLastSeq(t *testing.T) {
	e := newTestEnv(t)
	e.project(t)

	var st proto.State
	if code := e.request(t, http.MethodGet, "/api/v1/state", nil, e.token, &st); code != http.StatusOK {
		t.Fatalf("state = %d", code)
	}
	if want := e.server.Bus.LastSeq(context.Background()); st.Seq != want {
		t.Fatalf("state seq = %d, want %d", st.Seq, want)
	}
	if st.Seq == 0 {
		t.Fatal("state seq is 0 after a project's event")
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

// TestCloseEndsStreams checks that closing the environment ends an open
// SSE stream instead of waiting out the HTTP shutdown deadline.
func TestCloseEndsStreams(t *testing.T) {
	e := newTestEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
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

	drained := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, resp.Body)
		drained <- err
	}()

	start := time.Now()
	if err := e.server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
		t.Fatalf("the stream stayed open %v after Close", time.Since(start))
	}
}

// TestUploadRoundTrip stores a file, reads it back and deletes it.
func TestUploadRoundTrip(t *testing.T) {
	e := newTestEnv(t)
	const body = "hello attachment"

	req, _ := http.NewRequest(http.MethodPost, e.http.URL+"/api/v1/uploads?name=note.txt", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", resp.StatusCode)
	}
	var attachment proto.Attachment
	if err := json.NewDecoder(resp.Body).Decode(&attachment); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if attachment.ID == "" || attachment.Name != "note.txt" || attachment.MimeType != "text/plain" || attachment.Size != int64(len(body)) {
		t.Fatalf("attachment = %#v", attachment)
	}

	req, _ = http.NewRequest(http.MethodGet, e.http.URL+"/api/v1/uploads/"+attachment.ID, nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(raw) != body {
		t.Fatalf("download = %q", raw)
	}
	if disposition := resp.Header.Get("Content-Disposition"); !strings.Contains(disposition, "note.txt") {
		t.Fatalf("content disposition = %q", disposition)
	}

	if code := e.request(t, http.MethodGet, "/api/v1/uploads/"+attachment.ID, nil, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthorized download = %d", code)
	}
	if code := e.request(t, http.MethodDelete, "/api/v1/uploads/"+attachment.ID, nil, e.token, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/uploads/"+attachment.ID, nil, e.token, nil); code != http.StatusNotFound {
		t.Fatalf("missing upload = %d", code)
	}
}

// TestWebClientServed serves a built web client at the root without auth,
// and never lists the directory or serves outside it.
func TestWebClientServed(t *testing.T) {
	cfg, err := config.Load(t.TempDir(), "127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>t3go web</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.WebDir = webDir
	srv, err := New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer func() {
		ts.CloseClientConnections()
		ts.Close()
		srv.Close()
	}()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "t3go web") {
		t.Fatalf("get / = %d %q", resp.StatusCode, raw)
	}
	if code := status(t, http.MethodGet, ts.URL+"/missing.js"); code != http.StatusNotFound {
		t.Fatalf("missing = %d", code)
	}
	if code := status(t, http.MethodGet, ts.URL+"/../token"); code != http.StatusNotFound {
		t.Fatalf("traversal = %d", code)
	}
	if code := status(t, http.MethodPost, ts.URL+"/"); code != http.StatusMethodNotAllowed {
		t.Fatalf("post / = %d", code)
	}
}

func status(t *testing.T, method, url string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestApprovalEndpoints lets a helper wait for an approval and answers it
// from the REST API.
func TestApprovalEndpoints(t *testing.T) {
	e := newTestEnv(t)
	p := e.project(t)
	th := e.thread(t, p.ID)

	type answered struct {
		decision proto.ApprovalDecision
		status   int
	}
	done := make(chan answered, 1)
	go func() {
		var decision proto.ApprovalDecision
		code := e.request(t, http.MethodPost, "/api/v1/threads/"+th.ID+"/approvals",
			map[string]any{"runId": "run_x", "tool": "Bash", "input": map[string]any{"command": "ls"}}, e.token, &decision)
		done <- answered{decision: decision, status: code}
	}()

	var approval proto.ApprovalRequest
	deadline := time.Now().Add(5 * time.Second)
	for {
		var list struct {
			Approvals []proto.ApprovalRequest `json:"approvals"`
		}
		if code := e.request(t, http.MethodGet, "/api/v1/approvals?threadId="+th.ID, nil, e.token, &list); code != http.StatusOK {
			t.Fatalf("list = %d", code)
		}
		if len(list.Approvals) == 1 {
			approval = list.Approvals[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the approval never became pending")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if approval.Tool != "Bash" || approval.RunID != "run_x" {
		t.Fatalf("approval = %#v", approval)
	}
	if code := e.request(t, http.MethodPost, "/api/v1/approvals/"+approval.ID, proto.ApprovalDecision{Allow: true}, e.token, nil); code != http.StatusOK {
		t.Fatalf("resolve = %d", code)
	}
	select {
	case got := <-done:
		if got.status != http.StatusOK || !got.decision.Allow {
			t.Fatalf("helper answer = %#v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the helper never got an answer")
	}
	if code := e.request(t, http.MethodPost, "/api/v1/approvals/"+approval.ID, proto.ApprovalDecision{Allow: true}, e.token, nil); code != http.StatusNotFound {
		t.Fatalf("second resolve = %d", code)
	}
}

// TestUpstreamDiscoveryAndProbe checks the T3 Code compatibility surface:
// the discovery descriptor and the JSON RPC frames for a probe, the config
// snapshot and the shell stream.
func TestUpstreamDiscoveryAndProbe(t *testing.T) {
	e := newTestEnv(t)
	p := e.project(t)
	e.thread(t, p.ID)

	var descriptor struct {
		EnvironmentID                string         `json:"environmentId"`
		Label                        string         `json:"label"`
		ServerVersion                string         `json:"serverVersion"`
		OrchestrationProtocolVersion int            `json:"orchestrationProtocolVersion"`
		Capabilities                 map[string]any `json:"capabilities"`
	}
	if code := e.request(t, http.MethodGet, "/.well-known/t3/environment", nil, "", &descriptor); code != http.StatusOK {
		t.Fatalf("descriptor = %d", code)
	}
	if descriptor.EnvironmentID != e.server.ID || descriptor.OrchestrationProtocolVersion != 2 || descriptor.ServerVersion == "" || descriptor.Label == "" {
		t.Fatalf("descriptor = %#v", descriptor)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	send := func(v any) {
		t.Helper()
		if err := wsjson.Write(ctx, conn, v); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	send(map[string]any{"_tag": "Request", "id": 1, "tag": "server.probe", "payload": map[string]any{}, "headers": []any{}})
	frame := read()
	if frame["_tag"] != "Exit" || frame["exit"].(map[string]any)["_tag"] != "Success" {
		t.Fatalf("probe = %#v", frame)
	}

	send(map[string]any{"_tag": "Request", "id": 2, "tag": "server.getConfig", "payload": map[string]any{}, "headers": []any{}})
	frame = read()
	config, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
	if config["environment"] == nil || config["providers"] == nil {
		t.Fatalf("config = %#v", frame)
	}
	providers, _ := config["providers"].([]any)
	if len(providers) == 0 {
		t.Fatal("config lists no providers")
	}
	if provider := providers[0].(map[string]any); provider["instanceId"] == "" || provider["models"] == nil {
		t.Fatalf("provider = %#v", provider)
	}

	send(map[string]any{"_tag": "Request", "id": 3, "tag": "orchestration.subscribeShell", "payload": map[string]any{}, "headers": []any{}})
	frame = read()
	if frame["_tag"] != "Chunk" {
		t.Fatalf("shell = %#v", frame)
	}
	items, _ := frame["values"].([]any)
	if len(items) < 2 || items[0].(map[string]any)["kind"] != "snapshot" {
		t.Fatalf("shell items = %#v", items)
	}
	snapshot := items[0].(map[string]any)["snapshot"].(map[string]any)
	projects, _ := snapshot["projects"].([]any)
	threads, _ := snapshot["threads"].([]any)
	if len(projects) != 1 || len(threads) != 1 {
		t.Fatalf("shell projects = %d, threads = %d", len(projects), len(threads))
	}
	if project := projects[0].(map[string]any); project["title"] != "demo" || project["workspaceRoot"] == "" {
		t.Fatalf("project = %#v", project)
	}
	if thread := threads[0].(map[string]any); thread["title"] != "Test thread" || thread["providerInstanceId"] != "claude" || thread["projectId"] == "" {
		t.Fatalf("thread = %#v", thread)
	}

	// A new project reaches the open shell stream.
	created := e.project(t)
	frame = read()
	values, _ := frame["values"].([]any)
	if frame["_tag"] != "Chunk" || len(values) != 1 {
		t.Fatalf("shell update = %#v", frame)
	}
	update := values[0].(map[string]any)
	if update["kind"] != "project.updated" || update["project"].(map[string]any)["id"] != created.ID {
		t.Fatalf("shell update = %#v", update)
	}

	send(map[string]any{"_tag": "Ping"})
	if frame := read(); frame["_tag"] != "Pong" {
		t.Fatalf("ping = %#v", frame)
	}
}

// upstreamRunner answers a turn at once, so an upstream dispatch test does
// not need a provider CLI.
type upstreamRunner struct{}

func (upstreamRunner) ID() string   { return "upstream-fake" }
func (upstreamRunner) Name() string { return "Upstream Fake" }

func (upstreamRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "upstream-fake", Name: "Upstream Fake", Installed: true, Version: "1",
		Models: []proto.Model{{ID: "upstream-fake-1", Name: "One", Provider: "upstream-fake", Default: true}}}
}

func (upstreamRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	sink.Text("hello from the fake")
	return nil
}

// TestUpstreamThreadAndDispatch opens a thread stream, sends a command and
// sees the user and assistant messages arrive as v2 events.
func TestUpstreamThreadAndDispatch(t *testing.T) {
	e := newTestEnv(t)
	e.server.Service.Providers.Register(upstreamRunner{})

	var thread proto.Thread
	body := map[string]any{"projectId": e.project(t).ID, "title": "Upstream thread",
		"provider": "upstream-fake", "model": "upstream-fake-1", "permission": "full-access"}
	if code := e.request(t, http.MethodPost, "/api/v1/threads", body, e.token, &thread); code != http.StatusCreated {
		t.Fatalf("thread = %d", code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	write := func(v any) {
		t.Helper()
		if err := wsjson.Write(ctx, conn, v); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	write(map[string]any{"_tag": "Request", "id": 1, "tag": "orchestration.subscribeThread",
		"payload": map[string]any{"threadId": thread.ID}, "headers": []any{}})
	frame := read()
	values, _ := frame["values"].([]any)
	if frame["_tag"] != "Chunk" || len(values) != 1 {
		t.Fatalf("thread snapshot = %#v", frame)
	}
	snapshot := values[0].(map[string]any)
	projection := snapshot["projection"].(map[string]any)
	appThread := projection["thread"].(map[string]any)
	if snapshot["kind"] != "snapshot" || appThread["id"] != thread.ID {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	write(map[string]any{"_tag": "Request", "id": 2, "tag": "orchestration.dispatchCommand",
		"payload": map[string]any{"type": "message.dispatch", "commandId": "cmd_1", "threadId": thread.ID,
			"messageId": "msg_cmd", "text": "Run this", "createdBy": "user", "creationSource": "web", "attachments": []any{}},
		"headers": []any{}})
	var sawReceipt, sawUser, sawAssistant, sawRun bool
	observe := func(frame map[string]any) {
		if frame["_tag"] == "Exit" {
			sawReceipt = frame["requestId"].(float64) == 2 && frame["exit"].(map[string]any)["_tag"] == "Success"
			return
		}
		items, _ := frame["values"].([]any)
		if frame["_tag"] != "Chunk" || len(items) != 1 {
			return
		}
		event := items[0].(map[string]any)
		if event["kind"] != "event" {
			return
		}
		domain := event["event"].(map[string]any)
		if domain["type"] == "run.created" {
			payload := domain["payload"].(map[string]any)
			sawRun = payload["userMessageId"] != "" && payload["status"] == "running"
			return
		}
		if domain["type"] != "message.updated" {
			return
		}
		payload := domain["payload"].(map[string]any)
		switch payload["role"] {
		case "user":
			sawUser = sawUser || payload["text"] == "Run this"
		case "assistant":
			sawAssistant = sawAssistant || payload["text"] == "hello from the fake"
		}
	}
	for !sawReceipt || !sawAssistant || !sawRun {
		observe(read())
	}
	if !sawUser {
		t.Fatalf("no user message event (assistant = %v)", sawAssistant)
	}
}

// TestUpstreamBearerAuth walks the pairing token exchange, session and
// one-time socket ticket a T3 Code client uses.
func TestUpstreamBearerAuth(t *testing.T) {
	e := newTestEnv(t)

	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {e.token}, "subject_token_type": {"urn:t3:params:oauth:token-type:environment-bootstrap"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"}}
	resp, err := http.PostForm(e.http.URL+"/oauth/token", form)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token = %d", resp.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if token.AccessToken != e.token || token.TokenType != "Bearer" || token.ExpiresIn == 0 || token.Scope == "" {
		t.Fatalf("token = %#v", token)
	}

	bad := url.Values{"subject_token": {"wrong"}}
	resp, err = http.PostForm(e.http.URL+"/oauth/token", bad)
	if err != nil {
		t.Fatalf("bad token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token = %d", resp.StatusCode)
	}

	var session struct {
		Authenticated bool     `json:"authenticated"`
		Scopes        []string `json:"scopes"`
		SessionMethod string   `json:"sessionMethod"`
	}
	if code := e.request(t, http.MethodGet, "/api/auth/session", nil, e.token, &session); code != http.StatusOK {
		t.Fatalf("session = %d", code)
	}
	if !session.Authenticated || len(session.Scopes) == 0 || session.SessionMethod != "bearer-access-token" {
		t.Fatalf("session = %#v", session)
	}

	req, _ := http.NewRequest(http.MethodPost, e.http.URL+"/api/auth/websocket-ticket/rpc", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ticket: %v", err)
	}
	defer resp.Body.Close()
	var ticket struct {
		Ticket    string `json:"ticket"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ticket); err != nil {
		t.Fatalf("ticket decode: %v", err)
	}
	if ticket.Ticket == "" || ticket.ExpiresAt == "" {
		t.Fatalf("ticket = %#v", ticket)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2&wsTicket=" + url.QueryEscape(ticket.Ticket)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": 1, "tag": "server.probe",
		"payload": map[string]any{}, "headers": []any{}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read: %v", err)
	}
	if frame["_tag"] != "Exit" {
		t.Fatalf("probe = %#v", frame)
	}

	// The ticket is one-time.
	if _, _, err := websocket.Dial(ctx, wsURL, nil); err == nil {
		t.Fatal("a used ticket opened another socket")
	}
}

// TestUpstreamLaunchAndManage creates a thread through the upstream RPC,
// sends its first message, then archives and deletes it.
func TestUpstreamLaunchAndManage(t *testing.T) {
	e := newTestEnv(t)
	e.server.Service.Providers.Register(upstreamRunner{})
	project := e.project(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	write := func(v any) {
		t.Helper()
		if err := wsjson.Write(ctx, conn, v); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	const threadID = "thr_upstream_launch"
	write(map[string]any{"_tag": "Request", "id": 1, "tag": "orchestration.launchThread", "headers": []any{},
		"payload": map[string]any{"commandId": "cmd_launch", "threadId": threadID, "projectId": project.ID,
			"title": "Launched upstream", "runtimeMode": "full-access", "interactionMode": "default",
			"modelSelection":    map[string]any{"instanceId": "upstream-fake", "model": "upstream-fake-1"},
			"workspaceStrategy": map[string]any{"type": "root"},
			"initialMessage":    map[string]any{"text": "Start here", "attachments": []any{}}}})
	frame := read()
	value, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
	if frame["_tag"] != "Exit" || value["threadId"] != threadID {
		t.Fatalf("launch = %#v", frame)
	}
	projection, _ := value["projection"].(map[string]any)
	if thread := projection["thread"].(map[string]any); thread["title"] != "Launched upstream" || thread["id"] != threadID {
		t.Fatalf("projection thread = %#v", thread)
	}

	write(map[string]any{"_tag": "Request", "id": 2, "tag": "orchestration.dispatchCommand", "headers": []any{},
		"payload": map[string]any{"type": "thread.archive", "commandId": "cmd_archive", "threadId": threadID}})
	if frame := read(); frame["_tag"] != "Exit" {
		t.Fatalf("archive = %#v", frame)
	}
	archived, err := e.server.Store.Thread(ctx, threadID)
	if err != nil || !archived.Archived {
		t.Fatalf("thread after archive = %#v, %v", archived, err)
	}

	write(map[string]any{"_tag": "Request", "id": 3, "tag": "orchestration.dispatchCommand", "headers": []any{},
		"payload": map[string]any{"type": "thread.delete", "commandId": "cmd_delete", "threadId": threadID}})
	if frame := read(); frame["_tag"] != "Exit" {
		t.Fatalf("delete = %#v", frame)
	}
	if _, err := e.server.Store.Thread(ctx, threadID); err == nil {
		t.Fatal("the thread was not deleted")
	}
}

func upstreamGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t3go@example.com")
	run("config", "user.name", "T3Go")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

// TestUpstreamLaunchWorktree starts a thread in the worktree strategy a T3
// Code client asks for.
func TestUpstreamLaunchWorktree(t *testing.T) {
	e := newTestEnv(t)
	e.server.Service.Providers.Register(upstreamRunner{})
	repo := upstreamGitRepo(t)
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": 1, "tag": "orchestration.launchThread",
		"headers": []any{}, "payload": map[string]any{
			"commandId": "cmd_worktree", "threadId": "thr_worktree", "projectId": project.ID,
			"title": "Worktree thread", "runtimeMode": "full-access", "interactionMode": "default",
			"modelSelection":    map[string]any{"instanceId": "upstream-fake", "model": "upstream-fake-1"},
			"workspaceStrategy": map[string]any{"type": "worktree", "branch": "t3go/upstream"},
		}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read: %v", err)
	}
	if frame["_tag"] != "Exit" {
		t.Fatalf("launch = %#v", frame)
	}
	thread, err := e.server.Store.Thread(ctx, "thr_worktree")
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if thread.WorktreePath == "" || thread.Branch != "t3go/upstream" {
		t.Fatalf("thread = %#v", thread)
	}
	if _, err := os.Stat(thread.WorktreePath); err != nil {
		t.Fatalf("worktree: %v", err)
	}
}

// TestUpstreamDiffs answers turn and whole-thread diffs from run
// checkpoints.
func TestUpstreamDiffs(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects", map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	var thread proto.Thread
	body := map[string]any{"projectId": project.ID, "title": "Diff thread", "provider": "claude",
		"model": "test-model", "permission": "full-access"}
	if code := e.request(t, http.MethodPost, "/api/v1/threads", body, e.token, &thread); code != http.StatusCreated {
		t.Fatalf("thread = %d", code)
	}
	ctx := context.Background()
	ref, err := git.Checkpoint(ctx, repo, thread.ID, "run_diff")
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed by upstream\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := e.server.Store.CreateRun(ctx, proto.Run{ID: "run_diff", ThreadID: thread.ID, Status: proto.RunCompleted,
		Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess, Prompt: "change",
		StartedAt: started, Checkpoint: ref}); err != nil {
		t.Fatalf("run: %v", err)
	}

	wsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(wsCtx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	call := func(id int, tag string, payload map[string]any) map[string]any {
		t.Helper()
		if err := wsjson.Write(wsCtx, conn, map[string]any{"_tag": "Request", "id": id, "tag": tag,
			"payload": payload, "headers": []any{}}); err != nil {
			t.Fatalf("write: %v", err)
		}
		var frame map[string]any
		if err := wsjson.Read(wsCtx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	for id, tag := range map[int]string{1: "orchestration.getFullThreadDiff", 2: "orchestration.getTurnDiff"} {
		frame := call(id, tag, map[string]any{"threadId": thread.ID, "fromTurnCount": 0, "toTurnCount": 1})
		value, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
		if frame["_tag"] != "Exit" || value["threadId"] != thread.ID || !strings.Contains(value["diff"].(string), "changed by upstream") {
			t.Fatalf("%s = %#v", tag, frame)
		}
	}
}

// TestUpstreamAttachmentUpload mints an upload URL, posts bytes to it and
// sends the attachment with a message.
func TestUpstreamAttachmentUpload(t *testing.T) {
	e := newTestEnv(t)
	e.server.Service.Providers.Register(upstreamRunner{})
	project := e.project(t)
	var thread proto.Thread
	body := map[string]any{"projectId": project.ID, "title": "Files", "provider": "upstream-fake",
		"model": "upstream-fake-1", "permission": "full-access"}
	if code := e.request(t, http.MethodPost, "/api/v1/threads", body, e.token, &thread); code != http.StatusCreated {
		t.Fatalf("thread = %d", code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	write := func(v any) {
		t.Helper()
		if err := wsjson.Write(ctx, conn, v); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	write(map[string]any{"_tag": "Request", "id": 1, "tag": "attachments.createUploadUrl", "headers": []any{},
		"payload": map[string]any{"type": "file", "name": "notes.txt", "mimeType": "text/plain", "sizeBytes": 7}})
	frame := read()
	value, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
	attachmentID, _ := value["attachmentId"].(string)
	relativeURL, _ := value["relativeUrl"].(string)
	if frame["_tag"] != "Exit" || attachmentID == "" || relativeURL == "" {
		t.Fatalf("upload url = %#v", frame)
	}

	req, _ := http.NewRequest(http.MethodPost, e.http.URL+relativeURL, strings.NewReader("attached"))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("upload = %d", resp.StatusCode)
	}
	raw, err := os.ReadFile(e.server.Service.UploadPath(attachmentID))
	if err != nil || string(raw) != "attached" {
		t.Fatalf("stored = %q, %v", raw, err)
	}

	write(map[string]any{"_tag": "Request", "id": 10, "tag": "orchestration.subscribeThread",
		"payload": map[string]any{"threadId": thread.ID}, "headers": []any{}})
	read()

	write(map[string]any{"_tag": "Request", "id": 2, "tag": "orchestration.dispatchCommand", "headers": []any{},
		"payload": map[string]any{"type": "message.dispatch", "commandId": "cmd_files", "threadId": thread.ID,
			"messageId": "msg_files", "text": "Look", "createdBy": "user", "creationSource": "web",
			"attachments": []any{map[string]any{"type": "file", "id": attachmentID, "name": "notes.txt",
				"mimeType": "text/plain", "sizeBytes": 7}}}})
	var sawAttachment bool
	for !sawAttachment {
		frame = read()
		items, _ := frame["values"].([]any)
		if frame["_tag"] != "Chunk" || len(items) != 1 {
			continue
		}
		event, _ := items[0].(map[string]any)
		domain, _ := event["event"].(map[string]any)
		payload, _ := domain["payload"].(map[string]any)
		if domain["type"] != "message.updated" || payload["role"] != "user" {
			continue
		}
		attachments, _ := payload["attachments"].([]any)
		if len(attachments) == 1 && attachments[0].(map[string]any)["id"] == attachmentID {
			sawAttachment = true
		}
	}
}

// TestUpstreamPastedImage persists a data URL and serves it back through
// an asset URL.
func TestUpstreamPastedImage(t *testing.T) {
	e := newTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	call := func(id int, tag string, payload map[string]any) map[string]any {
		t.Helper()
		if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": id, "tag": tag,
			"payload": payload, "headers": []any{}}); err != nil {
			t.Fatalf("write: %v", err)
		}
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	frame := call(1, "assets.persistChatAttachments", map[string]any{
		"threadId": "thr_1", "messageId": "msg_1", "attachments": []any{map[string]any{
			"type": "image", "name": "pixel.png", "mimeType": "image/png", "sizeBytes": len(png),
			"dataUrl": "data:image/png;base64," + png,
		}},
	})
	value, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
	attachments, _ := value["attachments"].([]any)
	if frame["_tag"] != "Exit" || len(attachments) != 1 {
		t.Fatalf("persist = %#v", frame)
	}
	attachment := attachments[0].(map[string]any)
	if attachment["type"] != "image" || attachment["mimeType"] != "image/png" || attachment["id"] == "" {
		t.Fatalf("attachment = %#v", attachment)
	}

	frame = call(2, "assets.createUrl", map[string]any{"resource": map[string]any{
		"type": "attachment", "attachmentId": attachment["id"], "mimeType": "image/png", "disposition": "inline",
	}})
	value, _ = frame["exit"].(map[string]any)["value"].(map[string]any)
	relative, _ := value["relativeUrl"].(string)
	if frame["_tag"] != "Exit" || relative == "" {
		t.Fatalf("asset url = %#v", frame)
	}
	resp, err := http.Get(e.http.URL + relative)
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || len(raw) == 0 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("asset = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

// TestUpstreamVcsStatus reports a checkout's working tree and branch.
func TestUpstreamVcsStatus(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects", map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\nline\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	call := func(id int, tag string, payload map[string]any) map[string]any {
		t.Helper()
		if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": id, "tag": tag,
			"payload": payload, "headers": []any{}}); err != nil {
			t.Fatalf("write: %v", err)
		}
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	frame := call(1, "vcs.refreshStatus", map[string]any{"cwd": repo})
	value, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
	if frame["_tag"] != "Exit" || value["isRepo"] != true || value["refName"] != "main" {
		t.Fatalf("status = %#v", frame)
	}
	workingTree, _ := value["workingTree"].(map[string]any)
	files, _ := workingTree["files"].([]any)
	if len(files) != 1 || workingTree["insertions"].(float64) == 0 {
		t.Fatalf("working tree = %#v", workingTree)
	}

	frame = call(2, "subscribeVcsStatus", map[string]any{"cwd": repo})
	items, _ := frame["values"].([]any)
	if frame["_tag"] != "Chunk" || len(items) != 1 {
		t.Fatalf("status stream = %#v", frame)
	}
	event := items[0].(map[string]any)
	if event["_tag"] != "snapshot" || event["local"].(map[string]any)["isRepo"] != true {
		t.Fatalf("status event = %#v", event)
	}

	// A working tree change reaches the open stream without a refresh.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed again\nmore\nlines\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var updated map[string]any
	for updated == nil {
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read update: %v", err)
		}
		values, _ := frame["values"].([]any)
		if len(values) == 1 {
			if candidate, ok := values[0].(map[string]any); ok && candidate["_tag"] == "localUpdated" {
				updated = candidate
			}
		}
	}
}

// TestUpstreamVcsWrites creates and switches branches.
func TestUpstreamVcsWrites(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects", map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	call := func(id int, tag string, payload map[string]any) map[string]any {
		t.Helper()
		if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": id, "tag": tag,
			"payload": payload, "headers": []any{}}); err != nil {
			t.Fatalf("write: %v", err)
		}
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	frame := call(1, "vcs.createRef", map[string]any{"cwd": repo, "refName": "feature", "switchRef": true})
	if value := frame["exit"].(map[string]any)["value"].(map[string]any); value["refName"] != "feature" {
		t.Fatalf("createRef = %#v", frame)
	}
	if branch := git.Status(ctx, repo).Branch; branch != "feature" {
		t.Fatalf("branch = %q", branch)
	}
	frame = call(2, "vcs.switchRef", map[string]any{"cwd": repo, "refName": "main"})
	if value := frame["exit"].(map[string]any)["value"].(map[string]any); value["refName"] != "main" {
		t.Fatalf("switchRef = %#v", frame)
	}
}

// TestUpstreamSearch finds a thread by message text and finds within it.
func TestUpstreamSearch(t *testing.T) {
	e := newTestEnv(t)
	project := e.project(t)
	var thread proto.Thread
	body := map[string]any{"projectId": project.ID, "title": "Searchable", "provider": "claude",
		"model": "test-model", "permission": "full-access"}
	if code := e.request(t, http.MethodPost, "/api/v1/threads", body, e.token, &thread); code != http.StatusCreated {
		t.Fatalf("thread = %d", code)
	}
	ctx := context.Background()
	for _, m := range []proto.Message{
		{ID: "msg_s1", ThreadID: thread.ID, RunID: "run_s", Role: proto.RoleUser, Kind: proto.KindText, Content: "where is the needle", CreatedAt: time.Now().Add(-time.Second)},
		{ID: "msg_s2", ThreadID: thread.ID, RunID: "run_s", Role: proto.RoleAssistant, Kind: proto.KindText, Content: "the needle is here", CreatedAt: time.Now()},
	} {
		if _, err := e.server.Store.AppendMessage(ctx, m); err != nil {
			t.Fatalf("message: %v", err)
		}
	}

	wsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(wsCtx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	call := func(id int, tag string, payload map[string]any) map[string]any {
		t.Helper()
		if err := wsjson.Write(wsCtx, conn, map[string]any{"_tag": "Request", "id": id, "tag": tag,
			"payload": payload, "headers": []any{}}); err != nil {
			t.Fatalf("write: %v", err)
		}
		var frame map[string]any
		if err := wsjson.Read(wsCtx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	frame := call(1, "orchestration.searchThreads", map[string]any{"query": "needle", "limit": 10})
	value, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
	matches, _ := value["matches"].([]any)
	if frame["_tag"] != "Exit" || len(matches) != 2 {
		t.Fatalf("searchThreads = %#v", frame)
	}
	if match := matches[0].(map[string]any); match["projectId"] != project.ID || match["threadId"] != thread.ID {
		t.Fatalf("match = %#v", match)
	}

	frame = call(2, "orchestration.searchThread", map[string]any{"threadId": thread.ID, "query": "needle", "index": 0})
	value, _ = frame["exit"].(map[string]any)["value"].(map[string]any)
	if value["totalMatches"].(float64) != 2 || value["match"].(map[string]any)["entryId"] == "" {
		t.Fatalf("searchThread = %#v", value)
	}
}

// TestUpstreamUsage aggregates a run's usage into the summary.
func TestUpstreamUsage(t *testing.T) {
	e := newTestEnv(t)
	project := e.project(t)
	var thread proto.Thread
	body := map[string]any{"projectId": project.ID, "title": "Usage", "provider": "claude",
		"model": "test-model", "permission": "full-access"}
	if code := e.request(t, http.MethodPost, "/api/v1/threads", body, e.token, &thread); code != http.StatusCreated {
		t.Fatalf("thread = %d", code)
	}
	started := time.Now().UTC()
	if err := e.server.Store.CreateRun(context.Background(), proto.Run{
		ID: "run_usage", ThreadID: thread.ID, Status: proto.RunCompleted, Provider: "claude", Model: "test-model",
		Permission: proto.PermissionFullAccess, StartedAt: started,
		Usage: &proto.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CostUSD: 0.01},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	day := started.Format("2006-01-02")
	if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": 1, "tag": "server.getUsageSummary",
		"headers": []any{}, "payload": map[string]any{"sinceDay": day, "untilDay": day, "timeZone": "UTC"}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read: %v", err)
	}
	value, _ := frame["exit"].(map[string]any)["value"].(map[string]any)
	buckets, _ := value["buckets"].([]any)
	if frame["_tag"] != "Exit" || len(buckets) != 1 {
		t.Fatalf("usage = %#v", frame)
	}
	bucket := buckets[0].(map[string]any)
	totals := bucket["totals"].(map[string]any)
	if totals["uncachedInputTokens"].(float64) != 10 || totals["outputTokens"].(float64) != 5 || bucket["costUsd"].(float64) != 0.01 {
		t.Fatalf("bucket = %#v", bucket)
	}
}

// TestUpstreamGitCommit runs the commit action and checks the repository.
func TestUpstreamGitCommit(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects", map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("commit me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": 1, "tag": "git.runStackedAction",
		"headers": []any{}, "payload": map[string]any{"actionId": "act_1", "cwd": repo, "action": "commit",
			"commitMessage": "Test commit"}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var finished map[string]any
	for finished == nil {
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		values, _ := frame["values"].([]any)
		if len(values) != 1 {
			continue
		}
		event, _ := values[0].(map[string]any)
		if event["kind"] == "action_failed" {
			t.Fatalf("action failed: %#v", event)
		}
		if event["kind"] == "action_finished" {
			finished = event["result"].(map[string]any)
		}
	}
	if finished["commit"].(map[string]any)["status"] != "created" {
		t.Fatalf("result = %#v", finished)
	}
	cmd := exec.Command("git", "log", "-1", "--pretty=%s")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "Test commit" {
		t.Fatalf("git log = %q, %v", out, err)
	}
}

func TestGitActionEndpoint(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	thread := e.thread(t, project.ID)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("endpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var result proto.GitActionResult
	path := "/api/v1/threads/" + thread.ID + "/git"
	if code := e.request(t, http.MethodPost, path, map[string]string{
		"action": "commit", "message": "From the endpoint",
	}, e.token, &result); code != http.StatusOK {
		t.Fatalf("git action = %d", code)
	}
	if result.CommitSHA == "" || result.CommitSubject != "From the endpoint" || result.Status.Dirty {
		t.Fatalf("result = %#v", result)
	}
	if code := e.request(t, http.MethodPost, path, map[string]string{"action": "nope"}, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("bad action = %d", code)
	}
	if code := e.request(t, http.MethodPost, path, map[string]string{"action": "commit"}, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthorized action = %d", code)
	}
}

// fakeServerGH puts a gh that reports and creates pull request 7 on PATH.
func fakeServerGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "pr-open")
	script := `#!/bin/sh
case "$1 $2" in
"pr view")
  if [ -f "$PR_STATE" ]; then
    echo '{"url":"https://github.com/acme/repo/pull/7","number":7,"title":"Add feature","baseRefName":"main","headRefName":"feature","state":"OPEN"}'
  else
    echo "no pull requests found" >&2
    exit 1
  fi
  ;;
"pr create")
  touch "$PR_STATE"
  echo "https://github.com/acme/repo/pull/7"
  ;;
"pr list")
  echo '[{"number":7,"title":"Add feature","url":"https://github.com/acme/repo/pull/7","state":"OPEN","isDraft":false,"baseRefName":"main","headRefName":"feature"}]'
  ;;
*)
  echo "unexpected gh $*" >&2
  exit 2
  ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PR_STATE", state)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return state
}

// TestUpstreamGitPullRequest runs the stacked commit, push and pull
// request action, including the feature branch official clients ask for.
func TestUpstreamGitPullRequest(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "init", "-q", "--bare", remote)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bare remote: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"remote", "add", "origin", remote},
		{"push", "-qu", "origin", "main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	fakeServerGH(t)
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("pull request\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": 1, "tag": "git.runStackedAction",
		"headers": []any{}, "payload": map[string]any{"actionId": "act_pr", "cwd": repo,
			"action": "commit_push_pr", "commitMessage": "Add feature", "featureBranch": true}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var finished map[string]any
	for finished == nil {
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		values, _ := frame["values"].([]any)
		if len(values) != 1 {
			continue
		}
		event, _ := values[0].(map[string]any)
		if event["kind"] == "action_failed" {
			t.Fatalf("action failed: %#v", event)
		}
		if event["kind"] == "action_finished" {
			finished = event["result"].(map[string]any)
		}
	}
	branch := finished["branch"].(map[string]any)
	if branch["status"] != "created" || !strings.HasPrefix(branch["name"].(string), "t3go/add-feature") {
		t.Fatalf("branch = %#v", branch)
	}
	if finished["commit"].(map[string]any)["status"] != "created" {
		t.Fatalf("commit = %#v", finished["commit"])
	}
	if finished["push"].(map[string]any)["status"] != "pushed" {
		t.Fatalf("push = %#v", finished["push"])
	}
	pr := finished["pr"].(map[string]any)
	if pr["status"] != "created" || pr["url"] != "https://github.com/acme/repo/pull/7" {
		t.Fatalf("pr = %#v", pr)
	}
	toast := finished["toast"].(map[string]any)
	if toast["title"] != "Pull request ready" {
		t.Fatalf("toast = %#v", toast)
	}
}

func TestBranchEndpoints(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	cmd := exec.Command("git", "branch", "feature")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	thread := e.thread(t, project.ID)
	path := "/api/v1/threads/" + thread.ID + "/branches"
	var list struct {
		Branches []proto.Branch `json:"branches"`
	}
	if code := e.request(t, http.MethodGet, path, nil, e.token, &list); code != http.StatusOK {
		t.Fatalf("branches = %d", code)
	}
	if len(list.Branches) != 2 || !list.Branches[0].Current {
		t.Fatalf("branches = %#v", list.Branches)
	}
	if code := e.request(t, http.MethodPost, path, map[string]string{"name": "feature"}, e.token, &list); code != http.StatusOK {
		t.Fatalf("switch = %d", code)
	}
	if len(list.Branches) == 0 || list.Branches[0].Name != "feature" || !list.Branches[0].Current {
		t.Fatalf("switched branches = %#v", list.Branches)
	}
	if code := e.request(t, http.MethodPost, path, map[string]string{"name": "--force"}, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("bad branch = %d", code)
	}
	if code := e.request(t, http.MethodPost, path, map[string]any{"name": "toolbar", "create": true, "switch": true}, e.token, &list); code != http.StatusOK {
		t.Fatalf("create branch = %d", code)
	}
	if len(list.Branches) == 0 || list.Branches[0].Name != "toolbar" || !list.Branches[0].Current {
		t.Fatalf("created branches = %#v", list.Branches)
	}
}

// upstreamCall sends one request and returns its successful value.
func upstreamCall(t *testing.T, ctx context.Context, conn *websocket.Conn, id int, tag string, payload any) any {
	t.Helper()
	if err := wsjson.Write(ctx, conn, map[string]any{"_tag": "Request", "id": id, "tag": tag,
		"headers": []any{}, "payload": payload}); err != nil {
		t.Fatalf("write %s: %v", tag, err)
	}
	for {
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read %s: %v", tag, err)
		}
		if frame["_tag"] != "Exit" {
			continue
		}
		exit, _ := frame["exit"].(map[string]any)
		if exit["_tag"] != "Success" {
			t.Fatalf("%s failed: %#v", tag, exit)
		}
		return exit["value"]
	}
}

// TestUpstreamVcsRefsAndWorktrees lists branches and manages a worktree
// through the calls the official client makes.
func TestUpstreamVcsRefsAndWorktrees(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "init", "-q", "--bare", remote)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bare remote: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"remote", "add", "origin", remote}, {"push", "-qu", "origin", "main"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	value := upstreamCall(t, ctx, conn, 1, "vcs.listRefs", map[string]any{
		"cwd": repo, "includeMatchingRemoteRefs": true, "refKind": "all",
	})
	result := value.(map[string]any)
	if result["isRepo"] != true || result["hasPrimaryRemote"] != true {
		t.Fatalf("refs result = %#v", result)
	}
	refs := result["refs"].([]any)
	var current, remoteRef bool
	for _, raw := range refs {
		ref := raw.(map[string]any)
		if ref["name"] == "main" && ref["current"] == true && ref["isDefault"] == true {
			current = true
		}
		if ref["name"] == "origin/main" && ref["isRemote"] == true {
			remoteRef = true
		}
	}
	if !current || !remoteRef {
		t.Fatalf("refs = %#v", refs)
	}

	value = upstreamCall(t, ctx, conn, 2, "vcs.createWorktree", map[string]any{
		"cwd": repo, "refName": "main", "newRefName": "t3go/review", "path": nil,
	})
	worktree := value.(map[string]any)["worktree"].(map[string]any)
	path := worktree["path"].(string)
	if worktree["refName"] != "t3go/review" {
		t.Fatalf("worktree = %#v", worktree)
	}
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Fatalf("worktree checkout: %v", err)
	}

	upstreamCall(t, ctx, conn, 3, "vcs.removeWorktree", map[string]any{
		"cwd": repo, "path": path, "force": true,
	})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}

	plain := t.TempDir()
	var plainProject proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "plain", "path": plain}, e.token, &plainProject); code != http.StatusCreated {
		t.Fatalf("plain project = %d", code)
	}
	upstreamCall(t, ctx, conn, 4, "vcs.init", map[string]any{"cwd": plain})
	if !git.IsRepo(ctx, plain) {
		t.Fatal("vcs.init did not make a repository")
	}
}

// TestUpstreamReview serves the branch commits, diff preview and file
// contents the official review panel reads.
func TestUpstreamReview(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-qm", "Second commit"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	value := upstreamCall(t, ctx, conn, 1, "review.getBranchCommits", map[string]any{"cwd": repo, "baseRef": "HEAD~1"})
	commits := value.(map[string]any)
	if commits["truncated"] != false || len(commits["commits"].([]any)) != 1 {
		t.Fatalf("commits = %#v", commits)
	}
	first := commits["commits"].([]any)[0].(map[string]any)
	if first["subject"] != "Second commit" {
		t.Fatalf("commit = %#v", first)
	}
	sha := first["sha"].(string)

	// A metadata-only working-tree preview lists tracked and untracked
	// files but defers their patches.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("working\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	value = upstreamCall(t, ctx, conn, 2, "review.getDiffPreview", map[string]any{
		"cwd": repo, "sourceKind": "working-tree", "metadataOnly": true,
	})
	preview := value.(map[string]any)
	source := preview["sources"].([]any)[0].(map[string]any)
	if source["kind"] != "working-tree" || source["patchesDeferred"] != true || source["diff"] != "" {
		t.Fatalf("source = %#v", source)
	}
	files := source["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("files = %#v", files)
	}

	value = upstreamCall(t, ctx, conn, 3, "review.getDiffPreview", map[string]any{
		"cwd": repo, "sourceKind": "working-tree",
	})
	source = value.(map[string]any)["sources"].([]any)[0].(map[string]any)
	if !strings.Contains(source["diff"].(string), "+working") || !strings.Contains(source["diff"].(string), "new.txt") {
		t.Fatalf("diff = %q", source["diff"])
	}

	value = upstreamCall(t, ctx, conn, 4, "review.getDiffFileContents", map[string]any{
		"cwd": repo, "sourceKind": "working-tree", "baseRef": nil, "headRef": "main",
		"changeType": "new", "oldPath": "new.txt", "newPath": "new.txt",
	})
	contents := value.(map[string]any)
	if contents["oldContents"] != "" || contents["newContents"] != "untracked\n" {
		t.Fatalf("contents = %#v", contents)
	}

	// A commit preview compares the selected commit with its parent.
	value = upstreamCall(t, ctx, conn, 5, "review.getDiffPreview", map[string]any{
		"cwd": repo, "commitSha": sha,
	})
	source = value.(map[string]any)["sources"].([]any)[0].(map[string]any)
	if source["kind"] != "commit" || source["commitSubject"] != "Second commit" || source["baseRef"] == nil {
		t.Fatalf("commit source = %#v", source)
	}
}

// TestWorkspaceFilesAndPullRequest browses a checkout and reads its pull
// request through the right panel's endpoints.
func TestWorkspaceFilesAndPullRequest(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "app.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "blob.bin"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "pic.png"), []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	thread := e.thread(t, project.ID)
	filesPath := "/api/v1/threads/" + thread.ID + "/files"

	var listing struct {
		Path    string                 `json:"path"`
		Entries []proto.WorkspaceEntry `json:"entries"`
	}
	if code := e.request(t, http.MethodGet, filesPath, nil, e.token, &listing); code != http.StatusOK {
		t.Fatalf("files = %d", code)
	}
	if len(listing.Entries) < 2 || listing.Entries[0].Name != "src" || !listing.Entries[0].Directory {
		t.Fatalf("entries = %#v", listing.Entries)
	}
	for _, entry := range listing.Entries {
		if entry.Name == ".git" {
			t.Fatalf(".git is listed: %#v", listing.Entries)
		}
	}
	if code := e.request(t, http.MethodGet, filesPath+"?path=src", nil, e.token, &listing); code != http.StatusOK {
		t.Fatalf("src files = %d", code)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Path != "src/app.go" {
		t.Fatalf("src entries = %#v", listing.Entries)
	}
	if err := os.MkdirAll(filepath.Join(repo, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "node_modules", "pkg", "index.js"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := e.request(t, http.MethodGet, filesPath+"?recursive=1", nil, e.token, &listing); code != http.StatusOK {
		t.Fatalf("recursive files = %d", code)
	}
	foundSource := false
	for _, entry := range listing.Entries {
		if entry.Path == "src/app.go" {
			foundSource = true
		}
		if strings.Contains(entry.Path, "node_modules") {
			t.Fatalf("recursive files include node_modules: %#v", entry)
		}
	}
	if !foundSource {
		t.Fatalf("recursive files = %#v", listing.Entries)
	}

	var file proto.WorkspaceFile
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/file?path=src/app.go", nil, e.token, &file); code != http.StatusOK {
		t.Fatalf("file = %d", code)
	}
	if file.Binary || file.Content != "package main\n" || !strings.Contains(file.Mime, "text/plain") {
		t.Fatalf("file = %#v", file)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/file?path=blob.bin", nil, e.token, &file); code != http.StatusOK || !file.Binary {
		t.Fatalf("binary file = %d, %#v", code, file)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/file?path=../../etc/passwd", nil, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("traversal = %d", code)
	}
	rawResp, err := http.Get(e.http.URL + "/api/v1/threads/" + thread.ID + "/raw?path=pic.png&token=" + e.token)
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	defer rawResp.Body.Close()
	if rawResp.StatusCode != http.StatusOK || rawResp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("raw = %d, %q", rawResp.StatusCode, rawResp.Header.Get("Content-Type"))
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/raw?path=../../etc/passwd", nil, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("raw traversal = %d", code)
	}

	prPath := "/api/v1/threads/" + thread.ID + "/pull-request"
	var prResult struct {
		PullRequest *proto.PullRequest `json:"pullRequest"`
		Detail      string             `json:"detail"`
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	if code := e.request(t, http.MethodGet, prPath, nil, e.token, &prResult); code != http.StatusOK {
		t.Fatalf("no gh pull request = %d", code)
	}
	if prResult.PullRequest != nil || !strings.Contains(prResult.Detail, "not installed") {
		t.Fatalf("no gh result = %#v", prResult)
	}
	t.Setenv("PATH", oldPath)
	state := fakeServerGH(t)
	if err := os.WriteFile(state, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := e.request(t, http.MethodGet, prPath, nil, e.token, &prResult); code != http.StatusOK {
		t.Fatalf("pull request = %d", code)
	}
	if prResult.PullRequest == nil || prResult.PullRequest.Number != 7 {
		t.Fatalf("pull request = %#v", prResult)
	}
	var listResult struct {
		PullRequests []proto.PullRequest `json:"pullRequests"`
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/pull-requests", nil, e.token, &listResult); code != http.StatusOK {
		t.Fatalf("pull requests = %d", code)
	}
	if len(listResult.PullRequests) != 1 || listResult.PullRequests[0].Number != 7 {
		t.Fatalf("pull requests = %#v", listResult.PullRequests)
	}

	var graph struct {
		Commits []proto.GraphCommit `json:"commits"`
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/graph", nil, e.token, &graph); code != http.StatusOK {
		t.Fatalf("graph = %d", code)
	}
	if len(graph.Commits) == 0 || !graph.Commits[0].Head || graph.Commits[0].Short == "" {
		t.Fatalf("graph = %#v", graph.Commits)
	}
}

// TestUpstreamRuntimeApproval projects a pending approval and answers it
// through the runtime-request command.
func TestUpstreamRuntimeApproval(t *testing.T) {
	e := newTestEnv(t)
	project := e.project(t)
	var thread proto.Thread
	body := map[string]any{"projectId": project.ID, "title": "Approval", "provider": "claude",
		"model": "test-model", "permission": "supervised"}
	if code := e.request(t, http.MethodPost, "/api/v1/threads", body, e.token, &thread); code != http.StatusCreated {
		t.Fatalf("thread = %d", code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	decisions := make(chan proto.ApprovalDecision, 1)
	go func() {
		decisions <- e.server.Service.RequestApproval(ctx, proto.ApprovalRequest{
			ThreadID: thread.ID, RunID: "run_approval", Tool: "Bash", Input: map[string]any{"command": "ls"},
		})
	}()

	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/ws?orchestrationProtocol=2"
	conn, _, err := websocket.Dial(wsCtx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + e.token},
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	call := func(id int, tag string, payload map[string]any) map[string]any {
		t.Helper()
		if err := wsjson.Write(wsCtx, conn, map[string]any{"_tag": "Request", "id": id, "tag": tag,
			"payload": payload, "headers": []any{}}); err != nil {
			t.Fatalf("write: %v", err)
		}
		var frame map[string]any
		if err := wsjson.Read(wsCtx, conn, &frame); err != nil {
			t.Fatalf("read: %v", err)
		}
		return frame
	}

	// Subscribe after the request exists to see it in the snapshot.
	var approvalID string
	for approvalID == "" {
		if pending := e.server.Service.PendingApprovals(thread.ID); len(pending) == 1 {
			approvalID = pending[0].ID
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	frame := call(1, "orchestration.subscribeThread", map[string]any{"threadId": thread.ID})
	values, _ := frame["values"].([]any)
	projection := values[0].(map[string]any)["projection"].(map[string]any)
	requests, _ := projection["runtimeRequests"].([]any)
	if len(requests) != 1 || requests[0].(map[string]any)["id"] != approvalID {
		t.Fatalf("runtime requests = %#v", requests)
	}

	frame = call(2, "orchestration.dispatchCommand", map[string]any{
		"type": "runtime-request.respond", "commandId": "cmd_approve", "threadId": thread.ID,
		"requestId": approvalID, "decision": "accept",
	})
	if frame["_tag"] != "Exit" {
		t.Fatalf("respond = %#v", frame)
	}
	select {
	case decision := <-decisions:
		if !decision.Allow {
			t.Fatalf("decision = %#v", decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the approval was not resolved")
	}
}

func TestTerminalSession(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	e := newTestEnv(t)
	project := e.project(t)
	thread := e.thread(t, project.ID)

	var session proto.TerminalSession
	path := "/api/v1/threads/" + thread.ID + "/terminal"
	if code := e.request(t, http.MethodPost, path, map[string]any{}, e.token, &session); code != http.StatusOK {
		t.Fatalf("terminal = %d", code)
	}
	if session.ID == "" || session.Cwd == "" || session.Shell == "" || session.Title == "" {
		t.Fatalf("session = %#v", session)
	}
	var listed struct {
		Sessions []proto.TerminalSession `json:"sessions"`
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/terminals", nil, e.token, &listed); code != http.StatusOK || len(listed.Sessions) != 1 {
		t.Fatalf("terminal list = %d, %#v", code, listed.Sessions)
	}
	var extra proto.TerminalSession
	if code := e.request(t, http.MethodPost, "/api/v1/threads/"+thread.ID+"/terminals", map[string]any{}, e.token, &extra); code != http.StatusOK {
		t.Fatalf("new terminal = %d", code)
	}
	if extra.ID == "" || extra.ID == session.ID || extra.Title == "" {
		t.Fatalf("extra = %#v", extra)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/terminals", nil, e.token, &listed); code != http.StatusOK || len(listed.Sessions) != 2 {
		t.Fatalf("terminal list after new = %d, %#v", code, listed.Sessions)
	}
	if code := e.request(t, http.MethodDelete, "/api/v1/terminals/"+extra.ID, nil, e.token, nil); code != http.StatusOK {
		t.Fatalf("close extra = %d", code)
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/terminals", nil, e.token, &listed); code != http.StatusOK || len(listed.Sessions) != 1 {
		t.Fatalf("terminal list after close = %d, %#v", code, listed.Sessions)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(e.http.URL, "http") + "/api/v1/terminals/" + session.ID + "/ws?token=" + e.token
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	_ = wsjson.Write(ctx, conn, map[string]any{"type": "resize", "cols": 100, "rows": 30})
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "input", "data": "echo T3GO_TERM_OK\n"}); err != nil {
		t.Fatalf("input: %v", err)
	}
	var output strings.Builder
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(output.String(), "T3GO_TERM_OK") && time.Now().Before(deadline) {
		var frame struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("read: %v, output %q", err, output.String())
		}
		if frame.Type == "output" {
			output.WriteString(frame.Data)
		}
	}
	if !strings.Contains(output.String(), "T3GO_TERM_OK") {
		t.Fatalf("shell output = %q", output.String())
	}
	if code := e.request(t, http.MethodDelete, "/api/v1/terminals/"+session.ID, nil, e.token, nil); code != http.StatusOK {
		t.Fatalf("close = %d", code)
	}
}

func TestWorkingDiffStageAndDiscard(t *testing.T) {
	e := newTestEnv(t)
	repo := upstreamGitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(repo, "scratch.txt")
	if err := os.WriteFile(untracked, []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	thread := e.thread(t, project.ID)
	path := "/api/v1/threads/" + thread.ID + "/working-diff"
	var diff proto.WorkingDiff
	if code := e.request(t, http.MethodGet, path, nil, e.token, &diff); code != http.StatusOK {
		t.Fatalf("working diff = %d", code)
	}
	if !strings.Contains(diff.Patch, "+changed") || len(diff.Files) != 2 {
		t.Fatalf("diff = %#v", diff)
	}
	stage := "/api/v1/threads/" + thread.ID + "/git/stage"
	if code := e.request(t, http.MethodPost, stage, map[string]any{"paths": []string{"README.md"}}, e.token, nil); code != http.StatusOK {
		t.Fatalf("stage = %d", code)
	}
	if !git.StagedPaths(context.Background(), repo)["README.md"] {
		t.Fatal("README.md was not staged")
	}
	discard := "/api/v1/threads/" + thread.ID + "/git/discard"
	if code := e.request(t, http.MethodPost, discard, map[string]any{"paths": []string{"scratch.txt"}}, e.token, nil); code != http.StatusOK {
		t.Fatalf("discard = %d", code)
	}
	if _, err := os.Stat(untracked); !os.IsNotExist(err) {
		t.Fatalf("untracked file still exists: %v", err)
	}
	if code := e.request(t, http.MethodPost, discard, map[string]any{"paths": []string{"../escape"}}, e.token, nil); code != http.StatusBadRequest {
		t.Fatalf("escape = %d", code)
	}
}

func TestAgentCommands(t *testing.T) {
	e := newTestEnv(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claude", "commands", "review.md"),
		[]byte("---\nname: review\ndescription: Review the change\n---\n\nLook closely.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".opencode", "skills", "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".opencode", "skills", "audit", "SKILL.md"),
		[]byte("---\ndescription: Audit a change\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := filepath.Join(repo, "mcp.sh")
	script := `#!/bin/sh
while IFS= read -r line; do
case "$line" in
*'"method":"initialize"'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{}},"serverInfo":{"name":"fake","version":"1"}}}';;
*'"method":"prompts/list"'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"prompts":[{"name":"summarize","description":"Summarize with MCP"}]}}';;
esac
done
`
	if err := os.WriteFile(mcp, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-1")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{}},"serverInfo":{"name":"remote","version":"1"}}}`))
		case "prompts/list":
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				w.WriteHeader(400)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"prompts\":[{\"name\":\"inspect\",\"description\":\"Inspect with remote MCP\"}]}}\n\n"))
		default:
			w.WriteHeader(202)
		}
	}))
	t.Cleanup(remote.Close)
	if err := os.WriteFile(filepath.Join(repo, ".mcp.json"),
		[]byte(`{"mcpServers":{"fake":{"command":"`+mcp+`"},"remote":{"type":"http","url":"`+remote.URL+`"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var project proto.Project
	if code := e.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "repo", "path": repo}, e.token, &project); code != http.StatusCreated {
		t.Fatalf("project = %d", code)
	}
	thread := e.thread(t, project.ID)
	var result struct {
		Commands []proto.AgentCommand `json:"commands"`
	}
	if code := e.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/commands", nil, e.token, &result); code != http.StatusOK {
		t.Fatalf("commands = %d", code)
	}
	var review, audit, prompt, remotePrompt bool
	for _, command := range result.Commands {
		if command.Name == "review" && command.Source == "claude" && command.Kind == "command" {
			review = command.Description == "Review the change"
		}
		if command.Name == "audit" && command.Source == "opencode" && command.Kind == "skill" {
			audit = command.Description == "Audit a change"
		}
		if command.Name == "mcp__fake__summarize" && command.Source == "mcp" && command.Kind == "prompt" {
			prompt = command.Description == "Summarize with MCP"
		}
		if command.Name == "mcp__remote__inspect" && command.Source == "mcp" {
			remotePrompt = command.Description == "Inspect with remote MCP"
		}
	}
	if !review || !audit || !prompt || !remotePrompt {
		t.Fatalf("commands = %#v", result.Commands)
	}

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[mcp_servers.codexfake]\ncommand = \"" + mcp + "\"\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	var codexPrompt bool
	for _, command := range discoverMCPPromptsAt(repo, home) {
		if command.Name == "mcp__codexfake__summarize" && command.Source == "mcp" {
			codexPrompt = true
		}
	}
	if !codexPrompt {
		t.Fatalf("codex MCP prompt missing")
	}
}
