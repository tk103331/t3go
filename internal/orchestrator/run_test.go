package orchestrator

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/events"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/store"
)

type fakeRunner struct {
	fail bool
}

func (f *fakeRunner) ID() string   { return "fake" }
func (f *fakeRunner) Name() string { return "Fake Agent" }

func (f *fakeRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "fake", Name: "Fake Agent", Installed: true, Version: "1.0",
		Models: []proto.Model{{ID: "fake-1", Name: "Fake One", Provider: "fake", Default: true}}}
}

func (f *fakeRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	sink.Session("session-1")
	sink.Reasoning("thinking ")
	sink.Text("Hello ")
	sink.ToolStart("tool-1", "Bash", "echo hi")
	sink.ToolFinish("tool-1", "hi\n", false)
	sink.Text("world")
	sink.Usage(proto.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5})
	if f.fail {
		return context.Canceled
	}
	return nil
}

func newTestService(t *testing.T, runner provider.Provider) *Service {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	registry := provider.NewRegistry()
	registry.Register(runner)
	return New(st, registry, events.New(st), t.TempDir(), log.New(io.Discard, "", 0))
}

func TestRunStreamsAndFinishes(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, &fakeRunner{})
	project, err := svc.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Title: "Run test",
		Provider: "fake", Model: "fake-1", Permission: proto.PermissionFullAccess})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	run, err := svc.Send(ctx, thread.ID, "Say hello")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := svc.Wait(waitCtx, thread.ID); err != nil {
		t.Fatalf("wait: %v", err)
	}

	detail, err := svc.Detail(ctx, thread.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Thread.Status != proto.ThreadIdle {
		t.Fatalf("thread status = %q", detail.Thread.Status)
	}
	if detail.Thread.ProviderSession != "session-1" {
		t.Fatalf("session = %q", detail.Thread.ProviderSession)
	}
	if detail.Thread.Title != "Run test" {
		t.Fatalf("title = %q", detail.Thread.Title)
	}
	if detail.Thread.Summary != "Say hello" {
		t.Fatalf("summary = %q", detail.Thread.Summary)
	}

	var text string
	var reasoning bool
	var tool *proto.Message
	for i := range detail.Messages {
		m := detail.Messages[i]
		switch m.Kind {
		case proto.KindText:
			if m.Role == proto.RoleAssistant {
				text += m.Content
			}
		case proto.KindReasoning:
			reasoning = true
		case proto.KindTool:
			tool = &detail.Messages[i]
		}
	}
	if text != "Hello world" {
		t.Fatalf("assistant text = %q", text)
	}
	if !reasoning {
		t.Fatal("no reasoning message")
	}
	if tool == nil || tool.Status != "completed" || tool.Content != "hi\n" {
		t.Fatalf("tool = %#v", tool)
	}

	var finished *proto.Run
	for i := range detail.Runs {
		if detail.Runs[i].ID == run.ID {
			finished = &detail.Runs[i]
		}
	}
	if finished == nil || finished.Status != proto.RunCompleted {
		t.Fatalf("run = %#v", finished)
	}
	if finished.Usage == nil || finished.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", finished.Usage)
	}

	events, err := svc.Bus.Replay(ctx, 0, 100)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var sawDelta, sawFinished bool
	for _, e := range events {
		switch e.Type {
		case proto.EventMessageDelta:
			sawDelta = true
		case proto.EventRunFinished:
			sawFinished = true
		}
	}
	if !sawDelta || !sawFinished {
		t.Fatalf("events delta=%v finished=%v", sawDelta, sawFinished)
	}
}

func TestRunStopMarksCancelled(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, &fakeRunner{fail: true})
	project, _ := svc.CreateProject(ctx, "demo", t.TempDir())
	thread, _ := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Provider: "fake", Model: "fake-1",
		Permission: proto.PermissionFullAccess})
	if _, err := svc.Send(ctx, thread.ID, "hello"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := svc.Wait(context.Background(), thread.ID); err != nil {
		t.Fatalf("wait: %v", err)
	}
	detail, _ := svc.Detail(ctx, thread.ID)
	if detail.Runs[0].Status != proto.RunCancelled {
		t.Fatalf("run status = %q", detail.Runs[0].Status)
	}
	if detail.Thread.Status != proto.ThreadIdle {
		t.Fatalf("thread status = %q", detail.Thread.Status)
	}
	if strings.TrimSpace(detail.Messages[len(detail.Messages)-1].Content) == "" {
		t.Fatal("assistant returned no text")
	}
}

type blockingRunner struct {
	started chan struct{}
}

func (b *blockingRunner) ID() string   { return "blocking" }
func (b *blockingRunner) Name() string { return "Blocking Agent" }

func (b *blockingRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "blocking", Name: "Blocking Agent", Installed: true, Version: "1.0",
		Models: []proto.Model{{ID: "blocking-1", Name: "Blocking One", Provider: "blocking", Default: true}}}
}

func (b *blockingRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	close(b.started)
	<-ctx.Done()
	return ctx.Err()
}

// TestShutdownStopsRuns checks that a shutdown cancels the live run and
// records it before the database closes.
func TestShutdownStopsRuns(t *testing.T) {
	ctx := context.Background()
	runner := &blockingRunner{started: make(chan struct{})}
	svc := newTestService(t, runner)
	project, err := svc.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Title: "Long run",
		Provider: "blocking", Model: "blocking-1", Permission: proto.PermissionFullAccess})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	run, err := svc.Send(ctx, thread.ID, "Keep working")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the run never started")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	svc.Shutdown(shutdownCtx)

	detail, err := svc.Detail(ctx, thread.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Thread.Status != proto.ThreadIdle {
		t.Fatalf("thread status = %q, want idle", detail.Thread.Status)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].ID != run.ID || detail.Runs[0].Status != proto.RunCancelled {
		t.Fatalf("runs = %#v", detail.Runs)
	}
}

// TestRecoverInterrupted marks what a crash left running as failed.
func TestRecoverInterrupted(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, &fakeRunner{})
	project, err := svc.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Title: "Crashed",
		Provider: "fake", Model: "fake-1", Permission: proto.PermissionFullAccess})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	now := time.Now()
	if err := svc.Store.CreateRun(ctx, proto.Run{ID: "run_crashed", ThreadID: thread.ID, Status: proto.RunRunning,
		Provider: "fake", Model: "fake-1", Permission: proto.PermissionFullAccess, Prompt: "work", StartedAt: now}); err != nil {
		t.Fatalf("run: %v", err)
	}
	thread.Status = proto.ThreadRunning
	thread.UpdatedAt = now
	if err := svc.Store.UpdateThread(ctx, thread); err != nil {
		t.Fatalf("update thread: %v", err)
	}

	runs, threads, err := svc.RecoverInterrupted(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if runs != 1 || threads != 1 {
		t.Fatalf("recovered %d runs and %d threads, want 1 and 1", runs, threads)
	}
	detail, err := svc.Detail(ctx, thread.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Thread.Status != proto.ThreadFailed || detail.Thread.LastError != interruptedReason {
		t.Fatalf("thread = %#v", detail.Thread)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].Status != proto.RunFailed || detail.Runs[0].Error != interruptedReason || detail.Runs[0].EndedAt == nil {
		t.Fatalf("runs = %#v", detail.Runs)
	}

	if runs, threads, err := svc.RecoverInterrupted(ctx); err != nil || runs != 0 || threads != 0 {
		t.Fatalf("second recover = %d, %d, %v", runs, threads, err)
	}
}

type capturingRunner struct {
	prompts chan string
}

func (c *capturingRunner) ID() string   { return "capture" }
func (c *capturingRunner) Name() string { return "Capture Agent" }

func (c *capturingRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "capture", Name: "Capture Agent", Installed: true, Version: "1.0",
		Models: []proto.Model{{ID: "capture-1", Name: "Capture One", Provider: "capture", Default: true}}}
}

func (c *capturingRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	c.prompts <- turn.Prompt
	sink.Text("done")
	return nil
}

// TestSendWithAttachments lets the provider read the uploaded file's path
// and keeps the attachment on the user message.
func TestSendWithAttachments(t *testing.T) {
	ctx := context.Background()
	runner := &capturingRunner{prompts: make(chan string, 1)}
	svc := newTestService(t, runner)
	project, err := svc.CreateProject(ctx, "demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Title: "Files",
		Provider: "capture", Model: "capture-1", Permission: proto.PermissionFullAccess})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	attachment, err := svc.SaveUpload(ctx, "notes.txt", "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	path := svc.UploadPath(attachment.ID)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("upload file: %v", err)
	}

	run, err := svc.SendWith(ctx, thread.ID, "Read the notes", []string{attachment.ID})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := svc.Wait(waitCtx, thread.ID); err != nil {
		t.Fatalf("wait: %v", err)
	}
	prompt := <-runner.prompts
	if !strings.Contains(prompt, path) || !strings.Contains(prompt, "notes.txt") {
		t.Fatalf("provider prompt = %q", prompt)
	}

	detail, err := svc.Detail(ctx, thread.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	var user *proto.Message
	for i := range detail.Messages {
		if detail.Messages[i].RunID == run.ID && detail.Messages[i].Role == proto.RoleUser {
			user = &detail.Messages[i]
		}
	}
	if user == nil {
		t.Fatal("no user message")
	}
	if user.Content != "Read the notes" || len(user.Attachments) != 1 || user.Attachments[0].ID != attachment.ID {
		t.Fatalf("user message = %#v", user)
	}

	if _, err := svc.SendWith(ctx, thread.ID, "Another", []string{"upl_missing"}); !errors.Is(err, ErrBadAttachment) {
		t.Fatalf("missing attachment error = %v", err)
	}

	if err := svc.DeleteThread(ctx, thread.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("upload file still there: %v", err)
	}
}
