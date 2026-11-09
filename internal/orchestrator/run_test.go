package orchestrator

import (
	"context"
	"io"
	"log"
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
