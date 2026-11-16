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
		{ID: "msg_reason", ThreadID: thread.ID, RunID: "run_cards", Role: proto.RoleAssistant, Kind: proto.KindReasoning,
			Content: longThought, Status: "completed", CreatedAt: time.Now()},
		{ID: "msg_tool", ThreadID: thread.ID, RunID: "run_cards", Role: proto.RoleAssistant, Kind: proto.KindTool, Title: "Bash",
			Detail: `{"command":"go test ./..."}`, Content: "ok github.com/tk/t3go", Status: "completed", CreatedAt: time.Now().Add(time.Second)},
		{ID: "msg_progress", ThreadID: thread.ID, RunID: "run_cards", Role: proto.RoleAssistant, Kind: proto.KindText,
			Content: "Checking the result.", Status: "completed", CreatedAt: time.Now().Add(2 * time.Second)},
		{ID: "msg_final", ThreadID: thread.ID, RunID: "run_cards", Role: proto.RoleAssistant, Kind: proto.KindText,
			Content: "All checks passed.", Status: "completed", CreatedAt: time.Now().Add(3 * time.Second)},
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
	app.WaitFor("id:turn-summary-run_cards", 10*time.Second)
	app.WaitFor("text:Thought once · 1 tool call", 5*time.Second)
	app.WaitFor("text:All checks passed.", 5*time.Second)
	if app.Exists("id:reasoning-msg_reason") || app.Exists("id:tool-msg_tool") {
		t.Fatal("completed process entries were visible before the summary was expanded")
	}
	app.Press("id:turn-summary-run_cards")
	app.WaitFor("id:reasoning-msg_reason", 10*time.Second)
	app.WaitFor("id:tool-msg_tool", 10*time.Second)
	app.WaitFor("text:Checking the result.", 5*time.Second)

	// Closed cards show a one-line summary and keep the full text hidden.
	app.Press("label:Thought")
	app.WaitFor("text:Thinking about the next step.", 5*time.Second)

	// A tool card opens to its command and output.
	app.Press("label:Bash")
	app.WaitUntil(5*time.Second, func() bool { return len(app.Query("text:go test ./...")) > 0 })
	app.WaitFor("text:ok github.com/tk/t3go", 5*time.Second)
}

func TestSplitTurnKeepsLiveProcessAndFoldsCompletedTurns(t *testing.T) {
	messages := []proto.Message{
		{Role: proto.RoleUser, Kind: proto.KindText, Content: "Ship it"},
		{Role: proto.RoleAssistant, Kind: proto.KindReasoning, Content: "Check the tree.", Status: "completed"},
		{Role: proto.RoleAssistant, Kind: proto.KindTool, Title: "Bash", Status: "completed"},
		{Role: proto.RoleAssistant, Kind: proto.KindText, Content: "Running checks.", Status: "completed"},
		{Role: proto.RoleAssistant, Kind: proto.KindReasoning, Content: "Review results.", Status: "completed"},
		{Role: proto.RoleAssistant, Kind: proto.KindText, Content: "All done.", Status: "completed"},
		{Role: proto.RoleSystem, Kind: proto.KindNotice, Content: "Reconnected."},
	}

	live := splitTurn(messages, true)
	if live.Final != nil {
		t.Fatalf("running turn has final = %#v", live.Final)
	}
	if len(live.Process) != 5 || live.Thoughts != 2 || live.Tools != 1 {
		t.Fatalf("running split = %#v", live)
	}
	if len(live.Retained) != 1 {
		t.Fatalf("retained = %#v", live.Retained)
	}

	done := splitTurn(messages, false)
	if done.Final == nil || done.Final.Content != "All done." {
		t.Fatalf("final = %#v", done.Final)
	}
	if len(done.Process) != 4 || done.Thoughts != 2 || done.Tools != 1 {
		t.Fatalf("completed split = %#v", done)
	}
	if got := turnSummary(done.Thoughts, done.Tools, len(done.Process)); got != "Thought 2 times · 1 tool call" {
		t.Fatalf("summary = %q", got)
	}
}
