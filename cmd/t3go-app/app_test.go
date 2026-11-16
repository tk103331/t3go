package main

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/uitest"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/server"
)

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
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
		srv.Close()
	})
	return srv, ts, cfg
}

func mountApp(t *testing.T, ts *httptest.Server, cfg config.Config, opts ...uitest.Option) *uitest.App {
	t.Helper()
	t.Setenv("ASUKA_DATA", t.TempDir())
	t.Setenv("T3GO_SERVER", ts.URL)
	t.Setenv("T3GO_TOKEN", cfg.Token)
	base := []uitest.Option{uitest.Platform(ui.PlatformMacOS), uitest.Size(1100, 760)}
	return uitest.Mount(t, Root, append(base, opts...)...)
}

// TestUIListAndThread connects to a real environment, finds the project
// and opens a thread's transcript.
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
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_1", ThreadID: thread.ID, Role: proto.RoleAssistant,
		Kind: proto.KindText, Content: "Hello from the agent", Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("message: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Demo Project")
	a.WaitFor("Chat thread")
	a.Tap("Chat thread")
	a.WaitFor("Hello from the agent")
}

type streamRunner struct {
	prompt chan string
}

func (streamRunner) ID() string   { return "ui-fake" }
func (streamRunner) Name() string { return "UI Fake" }

func (streamRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "ui-fake", Name: "UI Fake", Installed: true, Version: "1.0",
		Models: []proto.Model{{ID: "ui-fake-1", Name: "UI Fake One", Provider: "ui-fake", Default: true}}}
}

func (r streamRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	if r.prompt != nil {
		select {
		case r.prompt <- turn.Prompt:
		default:
		}
	}
	sink.Text("Hello from the agent")
	<-ctx.Done()
	return ctx.Err()
}

// TestUISendAndQueue sends a prompt, then queues another behind the live
// run and checks the client sends it once the run ends.
func TestUISendAndQueue(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	runner := streamRunner{prompt: make(chan string, 4)}
	srv.Service.Providers.Register(runner)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Queue thread", Provider: "ui-fake", Model: "ui-fake-1", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Queue thread")
	a.Tap("Queue thread")
	a.WaitFor("No messages yet")

	a.Type("Message the agent…", "First prompt")
	a.Tap("Send")
	a.WaitFor("Hello from the agent")
	a.WaitFor("Stop")

	a.Type("Message the agent…", "Second prompt")
	a.Tap("Queue")
	a.WaitFor("Waiting for the current run")
	a.WaitFor("Second prompt")

	srv.Service.Stop(thread.ID)
	a.Until("the queued prompt to be sent", func() bool {
		runs, err := srv.Store.Runs(ctx, thread.ID)
		if err != nil || len(runs) < 2 {
			return false
		}
		for _, run := range runs {
			if run.Prompt == "Second prompt" {
				return true
			}
		}
		return false
	})
}

// TestUIQueueStaysWithThread switches threads while one is running and
// checks that its queued prompt is not sent to the other thread.
func TestUIQueueStaysWithThread(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	runner := streamRunner{prompt: make(chan string, 4)}
	srv.Service.Providers.Register(runner)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Queue isolation", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	first, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "First thread", Provider: "ui-fake", Model: "ui-fake-1", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("first thread: %v", err)
	}
	second, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Second thread", Provider: "ui-fake", Model: "ui-fake-1", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("second thread: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("First thread")
	a.WaitFor("Second thread")
	a.Tap("First thread")
	a.Type("Message the agent…", "First prompt")
	a.Tap("Send")
	a.WaitFor("Stop")

	a.Type("Message the agent…", "Queued for first")
	a.Tap("Queue")
	a.WaitFor("Queued for first")

	a.Tap("Second thread")
	a.WaitFor("No messages yet")
	a.ExpectNo("Queued for first")
	time.Sleep(250 * time.Millisecond)
	if runs, err := srv.Store.Runs(ctx, second.ID); err != nil || len(runs) != 0 {
		t.Fatalf("second thread received a queued prompt: runs=%d err=%v", len(runs), err)
	}

	srv.Service.Stop(first.ID)
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	if err := srv.Service.Wait(waitCtx, first.ID); err != nil {
		t.Fatalf("wait for stop: %v", err)
	}
	a.Tap("First thread")
	a.Until("the queued prompt to be sent to its own thread", func() bool {
		runs, err := srv.Store.Runs(ctx, first.ID)
		if err != nil {
			return false
		}
		for _, run := range runs {
			if run.Prompt == "Queued for first" {
				return true
			}
		}
		return false
	})
	if runs, err := srv.Store.Runs(ctx, second.ID); err != nil || len(runs) != 0 {
		t.Fatalf("second thread received a queued prompt after switching back: runs=%d err=%v", len(runs), err)
	}
}

// TestUIMobileNavigation opens the list on a phone and returns from a
// thread with the back button.
func TestUIMobileNavigation(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Phone Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Phone thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_phone", ThreadID: thread.ID, Role: proto.RoleAssistant,
		Kind: proto.KindText, Content: "Mobile transcript", Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("message: %v", err)
	}
	a := mountApp(t, ts, cfg, uitest.Platform(ui.PlatformIOS), uitest.Size(390, 844))
	a.WaitFor("Phone thread")
	a.Tap("Phone thread")
	a.WaitFor("Mobile transcript")
	a.Tap("Threads")
	a.WaitFor("Phone Demo")
}

// TestUIApproval shows a waiting tool call and answers it from the
// transcript.
func TestUIApproval(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	project, err := srv.Service.CreateProject(ctx, "Approval Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Approval thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionSupervised,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	other, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Other thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionSupervised,
	})
	if err != nil {
		t.Fatalf("other thread: %v", err)
	}
	decisions := make(chan proto.ApprovalDecision, 1)
	go func() {
		decisions <- srv.Service.RequestApproval(ctx, proto.ApprovalRequest{
			ThreadID: thread.ID, Tool: "Bash", Input: map[string]any{"command": "ls"},
		})
	}()
	otherCtx, otherCancel := context.WithCancel(context.Background())
	t.Cleanup(otherCancel)
	go srv.Service.RequestApproval(otherCtx, proto.ApprovalRequest{
		ThreadID: other.ID, Tool: "Read", Input: map[string]any{"path": "README.md"},
	})

	a := mountApp(t, ts, cfg)
	a.WaitFor("Approval thread")
	a.Tap("Approval thread")
	a.WaitFor("Approval required · Bash")
	a.ExpectNo("Approval required · Read")
	a.Tap("Allow")
	select {
	case decision := <-decisions:
		if !decision.Allow {
			t.Fatal("approval was not allowed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approval was not resolved")
	}
}
