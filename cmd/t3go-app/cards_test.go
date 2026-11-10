package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func TestUITranscriptCards(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Cards", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	longThought := strings.Repeat("Thinking about the next step. ", 8)
	for _, m := range []proto.Message{
		{ID: "msg_reason", ThreadID: thread.ID, Role: proto.RoleAssistant, Kind: proto.KindReasoning,
			Content: longThought, Status: "completed", CreatedAt: time.Now()},
		{ID: "msg_tool", ThreadID: thread.ID, Role: proto.RoleAssistant, Kind: proto.KindTool, Title: "Bash",
			Detail: `{"command":"go test ./..."}`, Content: "ok github.com/tk/t3go", Status: "completed", CreatedAt: time.Now().Add(time.Second)},
	} {
		if _, err := srv.Store.AppendMessage(ctx, m); err != nil {
			t.Fatalf("message: %v", err)
		}
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:reasoning-msg_reason", 10*time.Second)
	app.WaitFor("id:tool-msg_tool", 10*time.Second)

	// Closed cards show a one-line summary and keep the full text hidden.
	app.Press("label:Thought")
	app.WaitFor("text:Thinking about the next step.", 5*time.Second)

	// A tool card opens to its command and output.
	app.Press("label:Bash")
	app.WaitUntil(5*time.Second, func() bool { return len(app.Query("text:go test ./...")) > 0 })
	app.WaitFor("text:ok github.com/tk/t3go", 5*time.Second)
}
