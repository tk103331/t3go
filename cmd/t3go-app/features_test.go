package main

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/uitest"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func TestUICommandPalette(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Palette Demo", projectDir)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Palette thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_palette", ThreadID: thread.ID, Role: proto.RoleAssistant,
		Kind: proto.KindText, Content: "Palette transcript", Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("message: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Palette thread")
	a.Tap("More")
	a.WaitFor("Command palette…")
	a.Tap("Command palette…")
	a.WaitFor("Search commands and threads")
	a.Expect("Command palette")
	a.Tap("Close palette")
	a.WaitGone("Search commands and threads")
}

func TestUISettingsAndSurfaces(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Surface Demo", projectDir)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Surface thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_surface", ThreadID: thread.ID, Role: proto.RoleAssistant,
		Kind: proto.KindText, Content: "Surface transcript", Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("message: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Surface thread")
	a.Tap("More")
	a.Tap("Settings…")
	a.WaitFor("Defaults for new threads and this environment's appearance.")
	a.Expect("Default agent")
	a.Tap("Close settings")
	a.WaitGone("Defaults for new threads and this environment's appearance.")

	a.Tap("Surface thread")
	a.WaitFor("Surface transcript")
	a.Tap("Surfaces")
	a.WaitFor("README.md")
	a.Type("Message the agent…", "@README")
	a.WaitFor("@README.md")
}

func TestUIEnvironmentsManagement(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	if _, err := srv.Service.CreateProject(context.Background(), "Environment Demo", t.TempDir()); err != nil {
		t.Fatalf("project: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Environment Demo")
	a.Tap("More")
	a.WaitFor("Environments…")
	a.Tap("Environments…")
	a.WaitFor("Saved machines, routes and credentials. Other settings tabs follow the active environment.")
	a.Expect("Active")
	a.Expect("Default")
	a.Expect("Add environment")
	a.Tap("Close settings")
	a.WaitGone("Saved machines, routes and credentials. Other settings tabs follow the active environment.")

	a.Tap("More")
	a.Tap("Settings…")
	a.Tap("Environments")
	a.WaitFor("Saved machines, routes and credentials. Other settings tabs follow the active environment.")
}

func TestUIPullRequestInbox(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Inbox Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Inbox thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	}); err != nil {
		t.Fatalf("thread: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Inbox thread")
	a.Tap("More")
	a.Tap("Pull requests…")
	a.WaitFor("Checking project branches…")
	a.WaitFor("No pull requests matched these filters.")
	a.Tap("Close pull requests")
	a.WaitGone("No pull requests matched these filters.")
}

func TestUIPairingDeepLink(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	if _, err := srv.Service.CreateProject(ctx, "Deep Link Project", t.TempDir()); err != nil {
		t.Fatalf("project: %v", err)
	}

	t.Setenv("ASUKA_DATA", t.TempDir())
	t.Setenv("T3GO_SERVER", "")
	t.Setenv("T3GO_TOKEN", "")
	a := uitest.Mount(t, Root, uitest.Platform(ui.PlatformMacOS), uitest.Size(1100, 760))
	a.WaitFor("Connect to T3Go")

	query := url.Values{"url": {ts.URL}, "token": {cfg.Token}}
	ui.Dispatch(ops.Event{Kind: ops.EventIncomingURL, Str: "t3go://pair?" + query.Encode()})
	a.WaitFor("Deep Link Project")
}

func TestUISettledThreads(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Settled Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Settled thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	settled := true
	if _, err := srv.Service.UpdateThread(ctx, thread.ID, orchestrator.ThreadPatch{Settled: &settled}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Settled (1)")
	a.WaitFor("Settled thread")
	a.Tap("Hide settled threads")
	a.WaitGone("Settled thread")
}

func TestUIToolChangedFiles(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("Demo file content"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Tool Demo", projectDir)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Tool thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_tool", ThreadID: thread.ID, Role: proto.RoleAssistant,
		Kind: proto.KindTool, Title: "Edit file", Detail: `{"file_path":"README.md"}`,
		Content: "updated README.md", Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("message: %v", err)
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Tool thread")
	a.Tap("Tool thread")
	a.WaitFor("Edit file · README.md")
	a.Tap("Edit file · README.md")
	a.WaitFor("README.md")
	a.Tap("README.md")
	a.WaitFor("Demo file content")
}

func TestUITranscriptCompletedRunSummary(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Summary Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Summary thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	now := time.Now()
	for _, message := range []proto.Message{
		{ID: "msg_summary_user", ThreadID: thread.ID, RunID: "run_summary", Role: proto.RoleUser,
			Kind: proto.KindText, Content: "Inspect the project", Status: "completed", CreatedAt: now},
		{ID: "msg_summary_reasoning", ThreadID: thread.ID, RunID: "run_summary", Role: proto.RoleAssistant,
			Kind: proto.KindReasoning, Content: "reasoning body", Status: "completed", CreatedAt: now.Add(time.Millisecond)},
		{ID: "msg_summary_tool", ThreadID: thread.ID, RunID: "run_summary", Role: proto.RoleAssistant,
			Kind: proto.KindTool, Title: "Read", Detail: `{"path":"README.md"}`, Content: "tool output",
			Status: "completed", CreatedAt: now.Add(2 * time.Millisecond)},
		{ID: "msg_summary_middle", ThreadID: thread.ID, RunID: "run_summary", Role: proto.RoleAssistant,
			Kind: proto.KindText, Content: "intermediate answer", Status: "completed", CreatedAt: now.Add(3 * time.Millisecond)},
		{ID: "msg_summary_final", ThreadID: thread.ID, RunID: "run_summary", Role: proto.RoleAssistant,
			Kind: proto.KindText, Content: "final answer", Status: "completed", CreatedAt: now.Add(4 * time.Millisecond)},
	} {
		if _, err := srv.Store.AppendMessage(ctx, message); err != nil {
			t.Fatalf("message %s: %v", message.ID, err)
		}
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Summary thread")
	a.Tap("Summary thread")
	a.WaitFor("final answer")
	a.ExpectNo("intermediate answer")
	a.ExpectNo("reasoning body")
	a.WaitFor("1 thought · 1 tool call")
	a.Tap("1 thought · 1 tool call")
	a.WaitFor("intermediate answer")
	a.WaitFor("Read · README.md")
	a.ExpectNo("tool output")
	a.Tap("Read · README.md")
	a.WaitFor("tool output")
}

func TestUITranscriptRunningStepsCollapsed(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Running Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Running thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	now := time.Now()
	for _, message := range []proto.Message{
		{ID: "msg_running_user", ThreadID: thread.ID, RunID: "run_running", Role: proto.RoleUser,
			Kind: proto.KindText, Content: "Keep working", CreatedAt: now},
		{ID: "msg_running_reasoning", ThreadID: thread.ID, RunID: "run_running", Role: proto.RoleAssistant,
			Kind: proto.KindReasoning, Content: "reasoning body", Status: "running", CreatedAt: now.Add(time.Millisecond)},
		{ID: "msg_running_tool", ThreadID: thread.ID, RunID: "run_running", Role: proto.RoleAssistant,
			Kind: proto.KindTool, Title: "Read", Detail: `{"path":"README.md"}`, Content: "tool output",
			Status: "running", CreatedAt: now.Add(2 * time.Millisecond)},
		{ID: "msg_running_text", ThreadID: thread.ID, RunID: "run_running", Role: proto.RoleAssistant,
			Kind: proto.KindText, Content: "streaming answer", Status: "running", CreatedAt: now.Add(3 * time.Millisecond)},
	} {
		if _, err := srv.Store.AppendMessage(ctx, message); err != nil {
			t.Fatalf("message %s: %v", message.ID, err)
		}
	}

	a := mountApp(t, ts, cfg)
	a.WaitFor("Running thread")
	a.Tap("Running thread")
	a.WaitFor("streaming answer")
	a.WaitFor("Thinking")
	a.WaitFor("Read · README.md")
	a.WaitFor("Working · 1 thought · 1 tool call")
	a.ExpectNo("reasoning body")
	a.ExpectNo("tool output")
	a.Tap("Thinking")
	a.WaitFor("reasoning body")
	a.Tap("Read · README.md")
	a.WaitFor("tool output")
}

func TestToolChangedFiles(t *testing.T) {
	files := toolChangedFiles("Edit", `{"file_path":"README.md","nested":{"path":"cmd/main.go"}}`)
	if len(files) != 2 || files[0] != "README.md" || files[1] != "cmd/main.go" {
		t.Fatalf("files = %v", files)
	}
	if got := toolFileRelative("/work/tree", "/work/tree/cmd/main.go"); got != "cmd/main.go" {
		t.Fatalf("relative = %q", got)
	}
	if got := toolFileRelative("/work/tree", "/etc/passwd"); got != "" {
		t.Fatalf("outside = %q", got)
	}
}

func TestCustomModelOptions(t *testing.T) {
	providers := []proto.Provider{{ID: "claude", Name: "Claude Code", Installed: true}}
	models := []proto.Model{{ID: "sonnet", Name: "Sonnet", Provider: "claude"}}
	custom := map[string][]proto.Model{
		"claude": {
			{ID: "sonnet", Name: "Duplicate", Provider: "claude", Custom: true},
			{ID: "custom-large", Name: "Custom Large", Provider: "claude", Custom: true},
		},
	}
	options := modelOptionsOf(providers, models, custom, proto.Settings{})
	if len(options) != 2 {
		t.Fatalf("options = %+v", options)
	}
	if options[1].Provider != "claude" || options[1].Model != "custom-large" {
		t.Fatalf("custom option = %+v", options[1])
	}
}
