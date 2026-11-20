package app

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tk/t3go/internal/provider"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/server"
)

// testPost is the test's main-thread queue. The client's goroutines reach
// the state through it; the test drains it onto its own goroutine before
// every frame, as the window would.
type testPost struct {
	fns []func()
}

func (p *testPost) Update(fn func()) {
	p.fns = append(p.fns, fn)
}

func (p *testPost) drain() {
	for len(p.fns) > 0 {
		fns := p.fns
		p.fns = nil
		for _, fn := range fns {
			fn()
		}
	}
}

// uiRunner is a provider that holds its turn until it is stopped, so a
// test can create threads and queue prompts under a live run.
type uiRunner struct {
	started chan struct{}
}

func (uiRunner) ID() string   { return "ui-fake" }
func (uiRunner) Name() string { return "UI Fake" }

func (uiRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "ui-fake", Name: "UI Fake", Installed: true, Version: "1.0",
		Models: []proto.Model{{ID: "ui-fake-1", Name: "UI Fake One", Provider: "ui-fake", Default: true}}}
}

func (r uiRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	if r.started != nil {
		select {
		case r.started <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// startEnv runs a real environment server for the app to connect to.
func startEnv(t *testing.T) (*server.Server, *httptest.Server, config.Config) {
	t.Helper()
	cfg, err := config.Load(t.TempDir(), "127.0.0.1:0", false)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	srv, err := server.New(cfg, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	srv.Service.Providers.Register(uiRunner{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
		srv.Close()
	})
	return srv, ts, cfg
}

// startApp runs the client's root view against the environment, with a
// memory store that already knows the address and the token.
func startApp(t *testing.T, ts *httptest.Server, cfg config.Config, entries map[string]string) (*Tester, *Session) {
	t.Helper()
	if entries == nil {
		entries = map[string]string{}
	}
	entries["serverUrl"] = ts.URL
	entries["token"] = cfg.Token
	entries["environments:v1"] = ""
	post := &testPost{}
	store := memoryStorage(entries)
	// The window is nil in tests; the session posts through the queue.
	// Manager and session construct lazily inside the view, so the queue
	// has to exist first: build them here and give the root a manager.
	mgr := newManagerForTest(post, store)
	mgr.start()
	tt := ui.NewTester(mgr.view, 1100, 760)
	t.Cleanup(func() {
		for _, s := range mgr.sessions {
			s.disposeEnvironmentSession()
		}
	})
	session := mgr.active
	if session != nil {
		session.post = post.Update
	}
	return &Tester{tt: tt, post: post, manager: mgr}, session
}

// Tester bundles the headless view with the post queue it drains.
type Tester struct {
	tt      *ui.Tester
	post    *testPost
	manager *Manager
}

// pump drains the client's queue and draws until settle.
func (t *Tester) pump() {
	t.post.drain()
	t.tt.Frame()
}

// wait runs frames until want holds, or fails after limit.
func (t *Tester) wait(t2 *testing.T, limit time.Duration, want func() bool, msg string) {
	t2.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		t.pump()
		if want() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t2.Fatalf("timeout waiting: %s", msg)
}

// newManagerForTest builds a manager whose window is nil, posting onto
// the test's queue.
func newManagerForTest(post *testPost, store appStorage) *Manager {
	return newManager(nil, store)
}

func TestUIListAndThread(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo Project", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Chat thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	msg := proto.Message{ID: "msg_1", ThreadID: thread.ID, Role: proto.RoleAssistant, Kind: proto.KindText,
		Content: "Hello from the agent", Status: "completed", CreatedAt: time.Now()}
	if _, err := srv.Store.AppendMessage(ctx, msg); err != nil {
		t.Fatalf("message: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		tester.post.drain()
		tester.tt.Frame()
		if session != nil && session.connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	active := tester.manager.active
	if active == nil {
		t.Logf("after wait: no active session; sessions=%d", len(tester.manager.sessions))
	} else {
		t.Logf("after wait: active=%v connected=%v connErr=%q connecting=%v",
			active != nil, active.connected, active.connErr, active.connecting)
	}
	tester.wait(t, 15*time.Second, func() bool {
		for _, th := range session.threads {
			if th.ID == thread.ID {
				return true
			}
		}
		return false
	}, "the thread list to arrive")
	// Open the thread through the model, as the row's click does.
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool {
		for _, m := range session.messages {
			if m.ID == "msg_1" {
				return true
			}
		}
		return false
	}, "the transcript to load")
	tester.pump()
	if !tester.tt.HasText("Hello from the agent") {
		t.Fatalf("transcript does not show the message: %v", tester.tt.Texts())
	}
}

func TestUICreateThreadInProject(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	if _, err := srv.Service.CreateProject(ctx, "Alpha", t.TempDir()); err != nil {
		t.Fatalf("project: %v", err)
	}
	beta, err := srv.Service.CreateProject(ctx, "Beta", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.projects) == 2
	}, "the projects to arrive")

	session.newThreadProj = beta.ID
	session.showNewThread = true
	session.newThreadPrompt = "Second project thread"
	session.newThreadProv = "ui-fake"
	session.newThreadModel = "ui-fake-1"
	session.createPreparedThread(true)
	created := ""
	tester.wait(t, 10*time.Second, func() bool {
		threads, err := srv.Store.Threads(ctx, beta.ID, false)
		if err != nil || len(threads) == 0 {
			return false
		}
		created = threads[0].ID
		return true
	}, "the thread to land in Beta")
	tester.wait(t, 10*time.Second, func() bool {
		return session.selected == created
	}, "the client to open the created thread")
}
