package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/server"
)

// startEnv runs a real server for the app to connect to, closed with the
// test. Clients and SSE must be disconnected before the listener, or
// Close waits on the stream.
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

// TestUIListAndThread runs the app against a real environment: it waits
// for the project to arrive over HTTP and opens a thread's transcript.
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

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("text:Demo Project", 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("text:Hello from the agent", 15*time.Second)
}

// TestUICreateThreadInProject picks a project in the new-thread dialog,
// creates a thread and checks it landed in that project.
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

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("text:Beta", 15*time.Second)
	app.Press("id:new-thread")
	app.WaitFor("id:new-thread-project-"+beta.ID, 5*time.Second)
	app.Press("id:new-thread-project-" + beta.ID)
	app.Type("label:Thread title", "Second project thread")
	app.Press("id:create-thread")
	app.WaitFor("id:content-header >> text:Second project thread", 10*time.Second)

	var created proto.Thread
	app.WaitUntil(10*time.Second, func() bool {
		threads, err := srv.Store.Threads(ctx, beta.ID, false)
		if err != nil || len(threads) == 0 {
			return false
		}
		created = threads[0]
		return true
	})
	if created.ProjectID != beta.ID {
		t.Fatalf("thread project = %q, want %q", created.ProjectID, beta.ID)
	}
}

// TestUISettingsDefaults renames the environment, changes the default
// permission, and checks a new thread takes it; closing the dialog
// without saving drops the edit.
func TestUISettingsDefaults(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	if _, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir()); err != nil {
		t.Fatalf("project: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)

	app.Press("id:settings")
	app.WaitFor("id:settings-save", 5*time.Second)
	app.Clear("label:Environment name")
	app.Type("label:Environment name", "Team env")
	app.Press("id:settings-perm-accept-edits")
	app.Press("id:settings-theme-dark")
	app.Press("id:settings-save")
	app.WaitFor("id:content-header >> text:Team env", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		s := srv.Service.Settings(ctx)
		return s.Title == "Team env" && s.DefaultPermission == string(proto.PermissionAcceptEdits) && s.Theme == "dark"
	})

	// A closed dialog keeps its draft out of the environment.
	app.Press("id:settings")
	app.WaitFor("id:settings-save", 5*time.Second)
	app.Clear("label:Environment name")
	app.Type("label:Environment name", "Draft name")
	app.Press("text:Close")
	app.WaitForGone("id:settings-save", 5*time.Second)
	app.WaitFor("id:content-header >> text:Team env", 5*time.Second)

	// The new thread inherits the saved permission.
	app.Press("id:new-thread")
	app.WaitFor("id:create-thread", 5*time.Second)
	app.Type("label:Thread title", "Inherits defaults")
	app.Press("id:create-thread")
	app.WaitFor("id:content-header >> text:Inherits defaults", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		threads, err := srv.Store.Threads(ctx, "", false)
		if err != nil || len(threads) != 1 {
			return false
		}
		return threads[0].Permission == proto.PermissionAcceptEdits
	})
}

// gitRepo makes a temporary repository with one commit, for worktrees.
func gitRepo(t *testing.T) string {
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

// TestUICreateThreadInWorktree asks for an own worktree with a branch
// name and checks the environment made the checkout.
func TestUICreateThreadInWorktree(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("text:Repo", 15*time.Second)
	app.Press("id:new-thread")
	app.WaitFor("id:create-thread", 5*time.Second)
	app.Press("id:toggle-worktree") // Own worktree: on
	app.WaitUntil(8*time.Second, func() bool {
		if app.Exists("label:Branch (optional)") {
			return true
		}
		// A press can land while the dialog settles. While it still reads
		// Off, press again; once On, wait for the body to follow.
		if app.Exists(`text:"Off"`) {
			app.Press("id:toggle-worktree")
			app.Settle()
		}
		return false
	})
	app.Type("label:Thread title", "Worktree thread")
	app.Type("label:Branch (optional)", "t3go/ui-test")
	app.Press("id:create-thread")
	app.WaitFor("id:content-header >> text:Worktree thread", 10*time.Second)

	var created proto.Thread
	app.WaitUntil(10*time.Second, func() bool {
		threads, err := srv.Store.Threads(ctx, project.ID, false)
		if err != nil || len(threads) != 1 {
			return false
		}
		created = threads[0]
		return true
	})
	if created.Branch != "t3go/ui-test" {
		t.Fatalf("branch = %q", created.Branch)
	}
	if created.WorktreePath == "" {
		t.Fatal("thread has no worktree path")
	}
	if _, err := os.Stat(created.WorktreePath); err != nil {
		t.Fatalf("worktree: %v", err)
	}
}

// TestUIArchiveThread hides a finished thread from the sidebar, shows it
// again with the archived filter, and unarchives it from its menu.
func TestUIArchiveThread(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo Project", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Disposable thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)

	app.LongPress("id:thread-" + thread.ID)
	app.WaitFor("id:archive-thread-"+thread.ID, 5*time.Second)
	app.Press("id:archive-thread-" + thread.ID)
	app.WaitForGone("id:thread-"+thread.ID, 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		th, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && th.Archived
	})

	app.Press("id:toggle-archived")
	app.WaitFor("id:thread-"+thread.ID, 10*time.Second)
	app.WaitFor("text:Disposable thread", 5*time.Second)

	app.LongPress("id:thread-" + thread.ID)
	app.WaitFor("id:archive-thread-"+thread.ID, 5*time.Second)
	app.Press("id:archive-thread-" + thread.ID)
	app.WaitUntil(10*time.Second, func() bool {
		th, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && !th.Archived
	})
}

// TestUIRenameAndDelete renames a thread and a project, then removes
// both, checking the server kept each change.
func TestUIRenameAndDelete(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo Project", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Disposable thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:delete-thread", 15*time.Second)

	// Rename the thread from the header, then check the server kept it.
	app.Press("id:rename-thread")
	app.WaitFor("id:confirm-rename", 5*time.Second)
	app.Clear("label:Thread title")
	app.Type("label:Thread title", "Renamed thread")
	app.Press("id:confirm-rename")
	app.WaitFor("id:content-header >> text:Renamed thread", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		t, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && t.Title == "Renamed thread"
	})

	// Rename the project from its sidebar menu.
	app.Press("id:project-menu-" + project.ID)
	app.WaitFor("id:rename-project-"+project.ID, 5*time.Second)
	app.Press("id:rename-project-" + project.ID)
	app.WaitFor("id:confirm-rename", 5*time.Second)
	app.Clear("label:Project name")
	app.Type("label:Project name", "Renamed project")
	app.Press("id:confirm-rename")
	app.WaitFor("text:Renamed project", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		p, err := srv.Store.Project(ctx, project.ID)
		return err == nil && p.Name == "Renamed project"
	})

	app.Press("id:delete-thread")
	app.WaitFor("id:confirm-delete", 5*time.Second)
	app.Press("id:confirm-delete")
	app.WaitForGone("id:thread-"+thread.ID, 10*time.Second)

	app.Press("id:project-menu-" + project.ID)
	app.WaitFor("id:delete-project-"+project.ID, 5*time.Second)
	app.Press("id:delete-project-" + project.ID)
	app.WaitFor("id:confirm-delete", 5*time.Second)
	app.Press("id:confirm-delete")
	app.WaitForGone("text:Renamed project", 10*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		_, err := srv.Store.Project(ctx, project.ID)
		return err != nil
	})
}

// TestUIPalette searches a thread by name and opens it, then checks the
// new-thread shortcut.
func TestUIPalette(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo Project", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	for _, title := range []string{"First thread", "Second thread"} {
		if _, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
			ProjectID: project.ID, Title: title, Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
		}); err != nil {
			t.Fatalf("thread: %v", err)
		}
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("text:Demo Project", 15*time.Second)

	app.Press("id:open-palette")
	app.WaitFor("id:palette", 5*time.Second)
	app.Type("label:Search commands", "Second")
	app.Key("enter")
	app.WaitFor("id:content-header >> text:Second thread", 10*time.Second)
	app.WaitForGone("id:palette", 5*time.Second)

	app.Key("mod+n")
	app.WaitFor("id:create-thread", 5*time.Second)
	app.Press("text:Cancel")
	app.WaitForGone("id:create-thread", 5*time.Second)

	app.Key("mod+k")
	app.WaitFor("id:palette", 5*time.Second)
}

// TestUISidebarRemembers hides the sidebar and checks the client kept
// that choice for the next launch.
func TestUISidebarRemembers(t *testing.T) {
	_, ts, cfg := startEnv(t)

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)

	app.Key("mod+b")
	app.WaitUntil(5*time.Second, func() bool {
		v, ok := app.Storage().GetItem("sidebarOpen")
		return ok && v == "false"
	})
	app.Key("mod+b")
	app.WaitUntil(5*time.Second, func() bool {
		v, ok := app.Storage().GetItem("sidebarOpen")
		return ok && v == "true"
	})
}

// TestUIRunHistory lists a thread's runs and opens the diff of one from
// its checkpoint.
func TestUIRunHistory(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	repo := gitRepo(t)
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "History thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	const runID = "run_history"
	ref, err := git.Checkpoint(ctx, repo, thread.ID, runID)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed by the agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ended := time.Now()
	if err := srv.Store.CreateRun(ctx, proto.Run{
		ID: runID, ThreadID: thread.ID, Status: proto.RunCompleted, Provider: "claude", Model: "test-model",
		Permission: proto.PermissionFullAccess, Prompt: "Change the readme", StartedAt: ended.Add(-2 * time.Second),
		EndedAt: &ended, Checkpoint: ref, Usage: &proto.Usage{TotalTokens: 1200, CostUSD: 0.03},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:history", 10*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("text:Change the readme") {
			return true
		}
		if app.Exists("id:history") {
			app.Press("id:history")
			app.Settle()
		}
		return app.Exists("text:Change the readme")
	})
	app.WaitFor("text:completed", 5*time.Second)
	app.Press("id:run-diff-" + runID)
	app.WaitFor("text:changed by the agent", 10*time.Second)
}

// TestUIOfflineBanner stops the environment and checks the client says it
// is reconnecting.
func TestUIOfflineBanner(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.WaitForGone("id:offline", 5*time.Second)

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	app.WaitFor("id:offline", 15*time.Second)
}

// uiRunner is a provider that holds its turn until it is stopped, so a
// test can queue prompts under a live run.
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

// TestUIQueuedPrompt queues a prompt under a live run, checks the client
// sends it when the run ends, and that a queued prompt can be removed.
func TestUIQueuedPrompt(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(uiRunner{started: make(chan struct{}, 4)})

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
	first, err := srv.Service.Send(ctx, thread.ID, "First prompt")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	defer srv.Service.Stop(thread.ID)

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("text:Stop", 10*time.Second)

	app.Type("label:Message", "Queued prompt")
	app.Press("text:Send")
	app.WaitFor("id:queued", 5*time.Second)
	app.WaitFor("text:Queued prompt", 5*time.Second)
	if runs, err := srv.Store.Runs(ctx, thread.ID); err != nil || len(runs) != 1 || runs[0].ID != first.ID {
		t.Fatalf("runs before the turn ended = %#v, %v", runs, err)
	}

	// Stopping the live run finishes it, so the client posts the prompt
	// that waited behind it.
	srv.Service.Stop(thread.ID)
	app.WaitUntil(10*time.Second, func() bool {
		runs, err := srv.Store.Runs(ctx, thread.ID)
		if err != nil || len(runs) != 2 {
			return false
		}
		return runs[1].Prompt == "Queued prompt"
	})
	app.WaitForGone("id:queued", 10*time.Second)
	app.WaitFor("text:Stop", 10*time.Second)

	// A queued prompt can be dropped instead.
	app.Type("label:Message", "Never sent")
	app.Press("text:Send")
	app.WaitFor("text:Never sent", 5*time.Second)
	app.Press("id:queued-remove-0")
	app.WaitForGone("text:Never sent", 5*time.Second)
	if runs, err := srv.Store.Runs(ctx, thread.ID); err != nil || len(runs) != 2 {
		t.Fatalf("runs after removing = %d, %v", len(runs), err)
	}
}

// TestUINewThreadInProject starts a thread from a project's own menu, so
// the project is chosen before the dialog opens.
func TestUINewThreadInProject(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	alpha, err := srv.Service.CreateProject(ctx, "Alpha", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	beta, err := srv.Service.CreateProject(ctx, "Beta", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("text:Beta", 15*time.Second)
	app.Press("id:project-menu-" + beta.ID)
	app.WaitFor("id:new-thread-in-"+beta.ID, 5*time.Second)
	app.Press("id:new-thread-in-" + beta.ID)
	app.WaitFor("id:create-thread", 5*time.Second)
	app.Type("label:Thread title", "From the menu")
	app.Press("id:create-thread")
	app.WaitFor("id:content-header >> text:From the menu", 10*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		threads, err := srv.Store.Threads(ctx, beta.ID, false)
		if err != nil || len(threads) != 1 {
			return false
		}
		if threads[0].ProjectID != beta.ID {
			t.Fatalf("thread project = %q, want %q", threads[0].ProjectID, beta.ID)
		}
		return true
	})
	if threads, err := srv.Store.Threads(ctx, alpha.ID, false); err != nil || len(threads) != 0 {
		t.Fatalf("alpha threads = %d, %v", len(threads), err)
	}
}

// TestUISettingsFromAnotherClient changes the environment's settings from
// outside the app and checks the event reaches it.
func TestUISettingsFromAnotherClient(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)

	settings := srv.Service.Settings(ctx)
	settings.Title = "Shared env"
	settings.Theme = "dark"
	if err := srv.Service.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	app.WaitFor("id:content-header >> text:Shared env", 10*time.Second)

	app.Press("id:settings")
	app.WaitFor("id:settings-save", 5*time.Second)
	if got := app.Find("label:Environment name").Text; got != "Shared env" {
		t.Fatalf("environment name field = %q", got)
	}
}

// TestUIConnectWithPairLink starts with only a pair link in storage and
// checks connecting fills in both the address and the token.
func TestUIConnectWithPairLink(t *testing.T) {
	_, ts, cfg := startEnv(t)
	link := "t3go://pair?url=" + ts.URL + "&token=" + cfg.Token

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{"token": link}))
	app.WaitFor(`text:"Connect"`, 5*time.Second)
	app.Press(`text:"Connect"`)
	app.WaitFor("id:settings", 15*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		v, ok := app.Storage().GetItem("serverUrl")
		return ok && v == ts.URL
	})
}

// TestUIComposerAttachment drops a file on the composer, sends it with a
// prompt, and checks the message kept the upload.
func TestUIComposerAttachment(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(uiRunner{started: make(chan struct{}, 1)})

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Files", Provider: "ui-fake", Model: "ui-fake-1", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	defer srv.Service.Stop(thread.ID)

	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello attachment"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:composer-drop", 10*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("label:Message").Text == "Read this" {
			return true
		}
		app.Clear("label:Message")
		app.Type("label:Message", "Read this")
		app.Settle()
		return app.Find("label:Message").Text == "Read this"
	})
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	app.DropFiles("id:composer-drop", uri)
	app.WaitFor("text:note.txt", 10*time.Second)

	// Removing the chip deletes the upload that was never sent.
	uploads := filepath.Join(cfg.Home, "uploads")
	entries, err := os.ReadDir(uploads)
	if err != nil || len(entries) != 1 {
		t.Fatalf("uploads = %v, %v", entries, err)
	}
	remove := "id:remove-attachment-" + entries[0].Name()
	app.WaitUntil(10*time.Second, func() bool {
		if !app.Exists("text:note.txt") {
			return true
		}
		// A press can land while the composer rebuilds after the upload.
		if app.Exists(remove) {
			app.Press(remove)
			app.Settle()
		}
		return false
	})
	app.WaitUntil(5*time.Second, func() bool {
		entries, err := os.ReadDir(uploads)
		return err == nil && len(entries) == 0
	})

	app.DropFiles("id:composer-drop", uri)
	app.WaitFor("text:note.txt", 10*time.Second)
	var sent proto.Message
	app.WaitUntil(10*time.Second, func() bool {
		detail, err := srv.Service.Detail(ctx, thread.ID)
		if err == nil {
			for _, m := range detail.Messages {
				if m.Role == proto.RoleUser && len(m.Attachments) == 1 {
					sent = m
					return true
				}
			}
		}
		// A press can land while the upload's final rebuild is running; try
		// again while the draft is still there.
		if len(app.Query("text:Send")) == 1 && app.Find("label:Message").Text != "" {
			app.Press("text:Send")
			app.Settle()
		}
		return false
	})
	if sent.Attachments[0].Name != "note.txt" || sent.Content != "Read this" {
		t.Fatalf("message = %#v", sent)
	}
	if _, err := os.Stat(srv.Service.UploadPath(sent.Attachments[0].ID)); err != nil {
		t.Fatalf("upload: %v", err)
	}
}

// TestSpeechDraftJoin checks what is dictated follows the draft.
func TestSpeechDraftJoin(t *testing.T) {
	cases := []struct{ base, text, want string }{
		{"", "hello", "hello"},
		{"write a test", "for this", "write a test for this"},
		{"write a test", "  ", "write a test"},
		{"  ", "hello", "hello"},
	}
	for _, c := range cases {
		if got := joinSpeech(c.base, c.text); got != c.want {
			t.Errorf("joinSpeech(%q, %q) = %q, want %q", c.base, c.text, got, c.want)
		}
	}
}

// TestUIDictationNeedsRecognizer hides the microphone where the platform
// recognizes no speech.
func TestUIDictationNeedsRecognizer(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Mic", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:attach", 10*time.Second)
	if app.Exists("id:dictate") {
		t.Fatal("the headless environment shows a microphone without a recognizer")
	}
}

// TestUIApprovalDialog answers a supervised tool call, both one waiting
// before the app connected and one that arrives live.
func TestUIApprovalDialog(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Approvals", Provider: "claude", Model: "test-model", Permission: proto.PermissionSupervised,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	decision := make(chan proto.ApprovalDecision, 2)
	go func() {
		decision <- srv.Service.RequestApproval(ctx, proto.ApprovalRequest{ThreadID: thread.ID, RunID: "run_a",
			Tool: "Read", Input: map[string]any{"file_path": "/tmp/notes.txt"}})
	}()

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:approval-dialog", 15*time.Second)
	if !app.Exists("text:notes.txt") {
		t.Fatal("the dialog does not show the tool's input")
	}
	app.Press("id:approval-allow")
	select {
	case d := <-decision:
		if !d.Allow {
			t.Fatalf("first decision = %#v", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first approval was not allowed")
	}
	app.WaitForGone("id:approval-dialog", 5*time.Second)

	go func() {
		decision <- srv.Service.RequestApproval(ctx, proto.ApprovalRequest{ThreadID: thread.ID, RunID: "run_b",
			Tool: "Bash", Input: map[string]any{"command": "rm -rf build"}})
	}()
	app.WaitFor("id:approval-dialog", 10*time.Second)
	app.WaitFor("text:Bash", 5*time.Second)
	app.Press("id:approval-deny")
	select {
	case d := <-decision:
		if d.Allow || !strings.Contains(d.Message, "Denied") {
			t.Fatalf("second decision = %#v", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second approval was not denied")
	}
}

// TestUITranscriptScrolls keeps a long transcript inside the window and
// scrolls it.
func TestUITranscriptScrolls(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Long thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	long := strings.Repeat("A line of output that must stay inside the transcript.\n\n", 80)
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_long", ThreadID: thread.ID, Role: proto.RoleAssistant,
		Kind: proto.KindText, Content: long, Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("message: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:transcript", 10*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		transcript := app.Find("id:transcript")
		return len(transcript.Children) > 0 && transcript.Children[0].Content.Height > transcript.Children[0].Frame.Size.Height
	})
	transcript := app.Find("id:transcript")
	if transcript.Frame.Size.Height >= 760 {
		t.Fatalf("transcript grew past the window: %v", transcript.Frame)
	}
	if len(transcript.Children) == 0 {
		t.Fatal("transcript has no scroll")
	}
	scroll := transcript.Children[0]
	if scroll.Content.Height <= scroll.Frame.Size.Height {
		t.Fatalf("content %v fits the viewport %v, nothing to scroll", scroll.Content, scroll.Frame)
	}
	app.Scroll("id:transcript", -500)
	scroll = app.Find("id:transcript").Children[0]
	if scroll.Offset.Y >= scroll.Content.Height-scroll.Frame.Size.Height {
		t.Fatalf("the transcript did not scroll back: offset %v of %v", scroll.Offset.Y, scroll.Content.Height)
	}
}

// TestUIDialogsFit keeps the model-heavy settings and new-thread dialogs
// inside the window.
func TestUIDialogsFit(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Threshold", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	}); err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)

	check := func(selector string) {
		t.Helper()
		dialog := app.Find(selector)
		if bottom := dialog.Frame.Origin.Y + dialog.Frame.Size.Height; bottom > 760 {
			t.Fatalf("%s overflows the window: %v", selector, dialog.Frame)
		}
	}
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	app.WaitFor("id:settings-model", 5*time.Second)
	check("id:settings-dialog")
	// A button inside the dialog must not rebuild the dialog itself.
	save := app.Find("id:settings-save")
	app.Press("id:settings-perm-auto")
	app.Settle()
	if after := app.Find("id:settings-save"); after.ID != save.ID {
		t.Fatal("pressing a setting rebuilt the dialog")
	}
	app.Press("text:Close")
	app.WaitForGone("id:settings-dialog", 5*time.Second)

	app.Press("id:new-thread")
	app.WaitFor("id:new-thread-dialog", 5*time.Second)
	app.WaitFor("id:new-thread-model", 5*time.Second)
	check("id:new-thread-dialog")
	create := app.Find("id:create-thread")
	app.Press(`text:"supervised"`)
	app.Settle()
	if after := app.Find("id:create-thread"); after.ID != create.ID {
		t.Fatal("pressing a new-thread option rebuilt the dialog")
	}
}

// TestUIGitDialog commits the thread's checkout from the Changes dialog.
func TestUIGitDialog(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Git thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "branch", "feature")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:open-diff", 10*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:diff-dialog") {
			return true
		}
		if app.Exists("id:open-diff") {
			app.Press("id:open-diff")
			app.Settle()
		}
		return app.Exists("id:diff-dialog")
	})
	app.WaitFor("id:git-commit", 10*time.Second)

	// Typing a message updates the dialog's body, not the dialog.
	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("label:Commit message").Text == "Commit from the UI" {
			return true
		}
		app.Type("label:Commit message", "Commit from the UI")
		app.Settle()
		return app.Find("label:Commit message").Text == "Commit from the UI"
	})
	if !app.Exists("id:git-commit") {
		t.Fatal("typing a commit message closed the dialog")
	}
	app.Press("id:git-commit")
	app.WaitFor("text:Committed", 15*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		cmd := exec.Command("git", "log", "-1", "--pretty=%s")
		cmd.Dir = repo
		out, err := cmd.Output()
		return err == nil && strings.TrimSpace(string(out)) == "Commit from the UI"
	})
	if st := git.Status(ctx, repo); st.Dirty {
		t.Fatalf("checkout is still dirty after commit: %#v", st)
	}

	// The branch picker switches the same checkout.
	app.WaitFor("id:git-branch", 10*time.Second)
	app.Press("id:git-branch")
	app.WaitFor("id:git-branch-feature", 5*time.Second)
	app.Press("id:git-branch-feature")
	app.WaitFor("text:On feature", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool { return git.Status(ctx, repo).Branch == "feature" })
}

// TestUISidebarThreads covers the search, folding and unread markers of
// the thread list.
func TestUISidebarThreads(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Alpha", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	first, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "First thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("first thread: %v", err)
	}
	second, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Second thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("second thread: %v", err)
	}

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+first.ID, 15*time.Second)
	app.WaitFor("id:thread-"+second.ID, 15*time.Second)

	// Search narrows the list by title, prompt and branch.
	app.Type("label:Search threads", "Second")
	app.WaitFor("id:thread-"+second.ID, 5*time.Second)
	app.WaitForGone("id:thread-"+first.ID, 5*time.Second)
	app.Clear("label:Search threads")
	app.WaitFor("id:thread-"+first.ID, 5*time.Second)

	// A project's title folds its threads away.
	app.Press("label:Alpha")
	app.WaitForGone("id:thread-"+first.ID, 5*time.Second)
	app.Press("label:Alpha")
	app.WaitFor("id:thread-"+first.ID, 5*time.Second)

	// Work finished elsewhere marks the row until it is opened.
	updated := "Second thread updated"
	if _, err := srv.Service.UpdateThread(ctx, second.ID, orchestrator.ThreadPatch{Title: &updated}); err != nil {
		t.Fatalf("update thread: %v", err)
	}
	app.WaitFor("id:unread-"+second.ID, 10*time.Second)
	app.Press("id:thread-" + second.ID)
	app.WaitForGone("id:unread-"+second.ID, 10*time.Second)
}

// TestUITranscriptChrome covers the day separator, message time, copy
// action and a finished run's footer.
func TestUITranscriptChrome(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Chrome", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	start := time.Now().Add(-10 * time.Second)
	end := start.Add(10 * time.Second)
	if err := srv.Store.CreateRun(ctx, proto.Run{
		ID: "run_chrome", ThreadID: thread.ID, Status: proto.RunCompleted, Provider: "claude", Model: "test-model",
		Permission: proto.PermissionFullAccess, Prompt: "Say hi", StartedAt: start, EndedAt: &end,
		Usage: &proto.Usage{TotalTokens: 1530, CostUSD: 0.012},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, m := range []proto.Message{
		{ID: "msg_user", ThreadID: thread.ID, Role: proto.RoleUser, Kind: proto.KindText, Content: "Say hi", CreatedAt: start},
		{ID: "msg_reply", ThreadID: thread.ID, RunID: "run_chrome", Role: proto.RoleAssistant, Kind: proto.KindText,
			Content: "Hi there", Status: "completed", CreatedAt: end},
		{ID: "msg_code", ThreadID: thread.ID, Role: proto.RoleAssistant, Kind: proto.KindText,
			Content: "```go\nfmt.Println(\"hi\")\n```", Status: "completed", CreatedAt: end.Add(time.Second)},
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
	app.WaitFor("text:Today", 10*time.Second)
	app.WaitFor("text:1.5k tokens", 10*time.Second)
	app.WaitFor("text:10s", 10*time.Second)
	app.WaitFor("id:edit-message-msg_user", 5*time.Second)
	app.WaitFor("id:retry-message-msg_reply", 5*time.Second)
	app.WaitFor("id:branch-message-msg_reply", 5*time.Second)
	app.Press("id:edit-message-msg_user")
	app.WaitFor("id:edit-message-dialog", 5*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if !app.Exists("id:edit-message-dialog") {
			return true
		}
		if app.Exists("id:cancel-edit-message") {
			app.Press("id:cancel-edit-message")
			app.Settle()
		}
		return !app.Exists("id:edit-message-dialog")
	})
	app.WaitFor("id:composer-permission", 10*time.Second)
	app.WaitFor("text:198.5k left", 5*time.Second)
	app.WaitFor("text:Copy code", 5*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		thread, err := srv.Store.Thread(ctx, thread.ID)
		if err == nil && thread.Plan {
			return true
		}
		if app.Exists("id:composer-plan") {
			app.Press("id:composer-plan")
			app.Settle()
		}
		return false
	})
	app.WaitFor("id:plan-implement", 5*time.Second)
	app.Press("id:plan-implement")
	app.WaitUntil(10*time.Second, func() bool {
		thread, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && !thread.Plan
	})
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Implement the plan." })
	app.Clear("label:Message")

	app.Press("id:composer-permission")
	app.WaitFor("id:composer-perm-auto", 5*time.Second)
	app.Press("id:composer-perm-auto")
	app.WaitUntil(10*time.Second, func() bool {
		thread, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && thread.Permission == proto.PermissionAuto
	})

	app.Press("id:copy-message-msg_reply")
	app.WaitUntil(5*time.Second, func() bool { return app.Clipboard == "Hi there" })
}

// TestUISidebarDraftsAndPins keeps a draft per thread and sorts pinned
// threads to the top of their project.
func TestUISidebarDraftsAndPins(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Pins", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	first, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Older thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("first thread: %v", err)
	}
	second, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Newer thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("second thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+first.ID, 15*time.Second)
	app.WaitFor("id:thread-"+second.ID, 15*time.Second)

	// A draft follows its thread, and the sidebar preview names it.
	app.Press("id:thread-" + first.ID)
	app.WaitFor("id:composer-drop", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("label:Message").Text == "half written" {
			return true
		}
		app.Clear("label:Message")
		app.Type("label:Message", "half written")
		app.Settle()
		return app.Find("label:Message").Text == "half written"
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("text:Draft: half written") {
			return true
		}
		if app.Exists("id:thread-" + second.ID) {
			app.Press("id:thread-" + second.ID)
			app.Settle()
		}
		return app.Exists("text:Draft: half written")
	})
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "" })
	app.Press("id:thread-" + first.ID)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "half written" })
	app.Press("id:thread-" + second.ID)
	app.WaitFor("id:composer-drop", 5*time.Second)

	// Pinning moves the older thread above the newer one.
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:pin-thread-" + first.ID) {
			return true
		}
		app.LongPress("id:thread-" + first.ID)
		app.Settle()
		return app.Exists("id:pin-thread-" + first.ID)
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:pinned-" + first.ID) {
			return true
		}
		if app.Exists("id:pin-thread-" + first.ID) {
			app.Press("id:pin-thread-" + first.ID)
			app.Settle()
		}
		return app.Exists("id:pinned-" + first.ID)
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:pin-thread-" + first.ID) {
			return true
		}
		app.LongPress("id:thread-" + first.ID)
		app.Settle()
		return app.Exists("id:pin-thread-" + first.ID)
	})
	app.Settle()
	app.Press("id:pin-thread-" + first.ID)
	app.Settle()
	app.WaitForGone("id:pinned-"+first.ID, 5*time.Second)
}

// TestTerminalScrollbarStyles checks the embedded terminal styles its
// scrollback bar.
func TestTerminalScrollbarStyles(t *testing.T) {
	if !strings.Contains(terminalPage, "xterm-viewport::-webkit-scrollbar") {
		t.Fatal("the terminal page does not style its scrollbar")
	}
}

// TestTerminalThemes checks the terminal follows the app's appearance.
func TestTerminalThemes(t *testing.T) {
	dark := terminalPageFor("ws://terminal", true)
	light := terminalPageFor("ws://terminal", false)
	for _, want := range []string{`"background":"#1e1e1e"`, `"foreground":"#d4d4d4"`, "rgba(255,255,255,.28)"} {
		if !strings.Contains(dark, want) {
			t.Fatalf("dark terminal is missing %q", want)
		}
	}
	for _, want := range []string{`"background":"#ffffff"`, `"foreground":"#24292f"`, "rgba(0,0,0,.25)"} {
		if !strings.Contains(light, want) {
			t.Fatalf("light terminal is missing %q", want)
		}
	}
	if strings.Contains(light, "#1e1e1e") {
		t.Fatal("the light terminal still uses the dark background")
	}
}

// TestUIRightPanel opens the surfaces rail, browses a file, reads the
// diff and finds the pull request surface.
func TestUIRightPanel(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "app.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Preview title\n\n![shot](pic.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "pic.png"), []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Surfaces", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	checkpoint, err := git.Checkpoint(ctx, repo, thread.ID, "run_surface")
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Preview title\n\nMore diff.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-time.Second)
	ended := time.Now()
	if err := srv.Store.CreateRun(ctx, proto.Run{ID: "run_surface", ThreadID: thread.ID, Status: proto.RunCompleted,
		Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess, Prompt: "edit",
		StartedAt: started, EndedAt: &ended, Checkpoint: checkpoint}); err != nil {
		t.Fatalf("run: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1300, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:toggle-right-panel", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:right-panel") {
			return true
		}
		if app.Exists("id:toggle-right-panel") {
			app.Press("id:toggle-right-panel")
			app.Settle()
		}
		return app.Exists("id:right-panel")
	})
	app.WaitFor("id:surface-files", 5*time.Second)
	beforeWidth := app.Find("id:right-panel").Frame.Size.Width
	app.Drag("id:right-resize", core.Point{X: -80})
	app.Settle()
	if afterWidth := app.Find("id:right-panel").Frame.Size.Width; afterWidth <= beforeWidth {
		t.Fatalf("right panel did not resize: %v to %v", beforeWidth, afterWidth)
	}
	openPicker := func(target string) {
		app.WaitUntil(10*time.Second, func() bool {
			if app.Exists("id:" + target) {
				return true
			}
			if app.Exists("id:surface-add") {
				app.Press("id:surface-add")
				app.Settle()
			}
			return app.Exists("id:" + target)
		})
	}
	pressUntil := func(press, target string) {
		app.WaitUntil(10*time.Second, func() bool {
			if app.Exists(target) {
				return true
			}
			if app.Exists(press) {
				app.Press(press)
				app.Settle()
			}
			return app.Exists(target)
		})
	}

	app.Press("id:surface-files")
	app.WaitFor("id:surface-tab-files", 5*time.Second)
	app.WaitFor("id:file-src", 5*time.Second)
	app.WaitFor("id:files-list-scrollbar", 5*time.Second)
	pressUntil("id:file-src", "id:file-src/app.go")
	pressUntil("id:file-src/app.go", "text:package main")
	app.WaitFor("id:file-code", 5*time.Second)
	app.WaitFor("id:files-scrollbar", 5*time.Second)
	pressUntil("id:file-back", "id:file-src/app.go")
	pressUntil("id:files-up", "id:file-README.md")
	pressUntil("id:file-README.md", "text:Preview title")
	pressUntil("id:file-back", "id:file-pic.png")
	pressUntil("id:file-pic.png", "id:file-image")

	openPicker("surface-browser")
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("label:Address") {
			return true
		}
		if app.Exists("id:surface-browser") {
			app.Press("id:surface-browser")
			app.Settle()
		}
		return app.Exists("label:Address")
	})
	app.Type("label:Address", "localhost:1")
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:browser-view") {
			return true
		}
		if app.Find("label:Address").Text != "localhost:1" {
			app.Clear("label:Address")
			app.Type("label:Address", "localhost:1")
			app.Settle()
		}
		if app.Exists("id:browser-go") {
			app.Press("id:browser-go")
			app.Settle()
		}
		return app.Exists("id:browser-view")
	})

	openPicker("surface-terminal")
	app.WaitUntil(20*time.Second, func() bool {
		if app.Exists("id:terminal-view") {
			return true
		}
		if app.Exists("id:surface-terminal") {
			app.Press("id:surface-terminal")
			app.Settle()
		}
		return app.Exists("id:terminal-view")
	})
	for _, gone := range []string{"terminal-session-menu", "terminal-new", "terminal-restart", "shells-dialog"} {
		if app.Exists("id:" + gone) {
			t.Fatalf("the terminal still has a toolbar item %q", gone)
		}
	}

	openPicker("surface-diff")
	app.Press("id:surface-diff")
	app.WaitFor("id:right-diff-refresh", 5*time.Second)
	app.WaitFor("id:surface-tab-diff", 5*time.Second)
	app.WaitFor("id:surface-tab-files", 5*time.Second)
	app.WaitFor("id:code-diff", 5*time.Second)
	app.WaitFor("id:diff-file-README.md", 5*time.Second)
	app.Press("id:diff-split")
	app.WaitFor("id:code-diff", 5*time.Second)
	app.Press("id:diff-stage-README.md")
	app.WaitUntil(10*time.Second, func() bool { return git.StagedPaths(ctx, repo)["README.md"] })
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:discard-dialog") {
			return true
		}
		if app.Exists("id:diff-discard-pic.png") {
			app.Press("id:diff-discard-pic.png")
			app.Settle()
		}
		return app.Exists("id:discard-dialog")
	})
	app.WaitUntil(15*time.Second, func() bool {
		if _, err := os.Stat(filepath.Join(repo, "pic.png")); os.IsNotExist(err) {
			return true
		}
		if app.Exists("id:confirm-discard") {
			app.Press("id:confirm-discard")
			app.Settle()
		}
		return false
	})
	openPicker("surface-graph")
	app.Press("id:surface-graph")
	app.WaitFor("id:git-graph", 5*time.Second)
	app.WaitFor("id:git-graph-scrollbar", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool { return len(app.Query("text:initial")) > 0 })
	openPicker("surface-pr")
	app.Press("id:surface-pr")
	app.WaitFor("id:right-pr-refresh", 5*time.Second)
	openPicker("surface-linked")
	app.Press("id:surface-linked")
	app.WaitFor("id:linked-pr-refresh", 5*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if !app.Exists("id:right-panel") {
			return true
		}
		if app.Exists("id:close-right-panel") {
			app.Press("id:close-right-panel")
			app.Settle()
		}
		return !app.Exists("id:right-panel")
	})
}

// TestUIComposerMentionsAndCommands covers @ file mentions and slash
// commands above the composer.
func TestUIComposerMentionsAndCommands(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claude", "commands", "review.md"),
		[]byte("---\nname: review\ndescription: Review the change\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Mentions", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:composer-drop", 10*time.Second)

	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:mention-file-README.md") {
			return true
		}
		if !strings.Contains(app.Find("label:Message").Text, "@READ") {
			app.Clear("label:Message")
			app.Type("label:Message", "Look at @READ")
			app.Settle()
		}
		return app.Exists("id:mention-file-README.md")
	})
	app.Press("id:mention-file-README.md")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Look at @README.md " })
	app.WaitFor("id:mention-chip-README.md", 5*time.Second)
	app.Press("id:mention-chip-README.md")
	app.WaitUntil(5*time.Second, func() bool { return !strings.Contains(app.Find("label:Message").Text, "README.md") })

	app.Clear("label:Message")
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:agent-command-claude-review") {
			return true
		}
		if !strings.Contains(app.Find("label:Message").Text, "/rev") {
			app.Clear("label:Message")
			app.Type("label:Message", "/rev")
			app.Settle()
		}
		return app.Exists("id:agent-command-claude-review")
	})
	app.Press("id:agent-command-claude-review")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "/review " })

	app.Clear("label:Message")
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:slash-command-settings") {
			return true
		}
		if !strings.Contains(app.Find("label:Message").Text, "/sett") {
			app.Clear("label:Message")
			app.Type("label:Message", "/sett")
			app.Settle()
		}
		return app.Exists("id:slash-command-settings")
	})
	app.Press("id:slash-command-settings")
	app.WaitFor("id:settings-dialog", 10*time.Second)
}

// TestUIAttachmentDrafts restores staged files with their thread.
func TestUIAttachmentDrafts(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Files", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	first, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "First", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Second", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	path := filepath.Join(t.TempDir(), "draft-note.txt")
	if err := os.WriteFile(path, []byte("staged"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+first.ID, 15*time.Second)
	app.Press("id:thread-" + first.ID)
	app.WaitFor("id:composer-drop", 10*time.Second)
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	app.DropFiles("id:composer-drop", uri)
	app.WaitFor("text:draft-note.txt", 10*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if !app.Exists("text:draft-note.txt") {
			return true
		}
		if app.Exists("id:thread-" + second.ID) {
			app.Press("id:thread-" + second.ID)
			app.Settle()
		}
		return !app.Exists("text:draft-note.txt")
	})
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("text:draft-note.txt") {
			return true
		}
		if app.Exists("id:thread-" + first.ID) {
			app.Press("id:thread-" + first.ID)
			app.Settle()
		}
		return app.Exists("text:draft-note.txt")
	})
}

// TestUISettingsSections walks the settings navigation.
func TestUISettingsSections(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Worktree", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
		Worktree: true,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	_ = thread
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	app.WaitFor("id:settings-nav-general", 5*time.Second)

	app.Press("id:settings-nav-providers")
	app.WaitFor("id:settings-refresh-providers", 5*time.Second)
	app.Press("id:settings-nav-worktrees")
	app.WaitFor("text:managed worktrees", 5*time.Second)
	app.Press("id:settings-nav-shortcuts")
	app.WaitFor("text:Keyboard shortcuts", 5*time.Second)
	app.Press("id:settings-nav-about")
	app.WaitFor("text:About T3Go", 5*time.Second)
	app.Press("id:settings-nav-general")
	app.WaitFor("id:settings-model", 5*time.Second)
}

// TestUIBranchToolbar switches and creates branches from the header.
func TestUIBranchToolbar(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	cmd := exec.Command("git", "branch", "feature")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Branches", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:branch-menu", 10*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:header-branch-feature") {
			return true
		}
		if app.Exists("id:branch-menu") {
			app.Press("id:branch-menu")
			app.Settle()
		}
		return app.Exists("id:header-branch-feature")
	})
	app.Press("id:header-branch-feature")
	app.WaitUntil(10*time.Second, func() bool { return git.Status(ctx, repo).Branch == "feature" })

	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:branch-new") {
			return true
		}
		if app.Exists("id:branch-menu") {
			app.Press("id:branch-menu")
			app.Settle()
		}
		return app.Exists("id:branch-new")
	})
	app.Press("id:branch-new")
	app.WaitFor("id:new-branch-dialog", 5*time.Second)
	app.Type("label:Branch name", "toolbar")
	app.Press("id:confirm-new-branch")
	app.WaitForGone("id:new-branch-dialog", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool { return git.Status(ctx, repo).Branch == "toolbar" })
}

// TestUILinkedPullRequest opens a linked pull request, reads its checks
// and merges it through a fake gh.
func TestUILinkedPullRequest(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "gh.log")
	script := `#!/bin/sh
case "$1 $2" in
"pr list") echo '[{"number":7,"title":"Add feature","url":"https://github.com/acme/repo/pull/7","state":"OPEN","isDraft":false,"baseRefName":"main","headRefName":"feature"}]' ;;
"pr view") echo '{"number":7,"title":"Add feature","url":"https://github.com/acme/repo/pull/7","state":"OPEN","isDraft":false,"baseRefName":"main","headRefName":"feature","body":"Body","author":{"login":"tk"},"reviewDecision":"APPROVED","mergeable":"MERGEABLE","additions":3,"deletions":1,"changedFiles":2,"statusCheckRollup":[{"__typename":"CheckRun","name":"test","conclusion":"SUCCESS","detailsUrl":"https://ci/1"}],"reviews":[{"author":{"login":"reviewer"},"state":"APPROVED","body":"LGTM"}],"comments":[{"author":{"login":"tk"},"body":"Please add tests","createdAt":"2026-11-10T10:00:00Z"}]}' ;;
*) echo "$@" >> "$GH_LOG" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "PR", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1300, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:toggle-right-panel", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:surface-add") {
			return true
		}
		if app.Exists("id:toggle-right-panel") {
			app.Press("id:toggle-right-panel")
			app.Settle()
		}
		return app.Exists("id:surface-add")
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:surface-linked") {
			return true
		}
		if app.Exists("id:surface-add") {
			app.Press("id:surface-add")
			app.Settle()
		}
		return app.Exists("id:surface-linked")
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:open-linked-pr-7") {
			return true
		}
		if app.Exists("id:surface-linked") {
			app.Press("id:surface-linked")
			app.Settle()
		}
		return app.Exists("id:open-linked-pr-7")
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:pr-merge-squash") {
			return true
		}
		if app.Exists("id:open-linked-pr-7") {
			app.Press("id:open-linked-pr-7")
			app.Settle()
		}
		return app.Exists("id:pr-merge-squash")
	})
	app.WaitUntil(5*time.Second, func() bool { return len(app.Query("text:test")) > 0 })
	app.WaitUntil(5*time.Second, func() bool { return len(app.Query("text:Please add tests")) > 0 })

	steps := []struct{ note, button, want string }{
		{note: "Looks good", button: "id:pr-review-approve", want: "pr review 7 --approve --body Looks good"},
		{note: "Ship it", button: "id:pr-comment", want: "pr comment 7 --body Ship it"},
	}
	for _, step := range steps {
		note, button, want := step.note, step.button, step.want
		app.WaitUntil(10*time.Second, func() bool {
			if app.Find("id:pr-note").Text == note {
				return true
			}
			app.Clear("id:pr-note")
			app.Type("id:pr-note", note)
			app.Settle()
			return app.Find("id:pr-note").Text == note
		})
		app.WaitUntil(15*time.Second, func() bool {
			raw, err := os.ReadFile(logPath)
			if err == nil && strings.Contains(string(raw), want) {
				return true
			}
			if app.Exists(button) {
				app.Press(button)
				app.Settle()
			}
			return false
		})
	}

	app.Press("id:pr-merge-squash")
	app.WaitUntil(10*time.Second, func() bool {
		raw, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(raw), "pr merge 7 --squash")
	})
}

// TestUIMCPPromptArguments asks for an MCP prompt's arguments before
// inserting it into the composer.
func TestUIMCPPromptArguments(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	mcp := filepath.Join(repo, "mcp.sh")
	script := `#!/bin/sh
while IFS= read -r line; do
case "$line" in
*'"method":"initialize"'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{}},"serverInfo":{"name":"fake","version":"1"}}}';;
*'"method":"prompts/list"'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"prompts":[{"name":"summarize","description":"Summarize with MCP","arguments":[{"name":"topic","description":"What to summarize","required":true},{"name":"style"}]}]}}';;
esac
done
`
	if err := os.WriteFile(mcp, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".mcp.json"),
		[]byte(`{"mcpServers":{"fake":{"command":"`+mcp+`"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Prompts", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:composer-drop", 10*time.Second)

	app.WaitUntil(20*time.Second, func() bool {
		if app.Exists("id:agent-command-mcp-mcp__fake__summarize") {
			return true
		}
		if !strings.Contains(app.Find("label:Message").Text, "/mcp") {
			app.Clear("label:Message")
			app.Type("label:Message", "/mcp")
			app.Settle()
		}
		return app.Exists("id:agent-command-mcp-mcp__fake__summarize")
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:prompt-args-dialog") {
			return true
		}
		if app.Exists("id:agent-command-mcp-mcp__fake__summarize") {
			app.Press("id:agent-command-mcp-mcp__fake__summarize")
			app.Settle()
		}
		return app.Exists("id:prompt-args-dialog")
	})

	for label, want := range map[string]string{"id:prompt-arg-topic": "release notes", "id:prompt-arg-style": "brief"} {
		label, want := label, want
		app.WaitUntil(10*time.Second, func() bool {
			if app.Find(label).Text == want {
				return true
			}
			app.Clear(label)
			app.Type(label, want)
			app.Settle()
			return app.Find(label).Text == want
		})
	}
	app.WaitUntil(10*time.Second, func() bool {
		if !app.Exists("id:prompt-args-dialog") {
			return true
		}
		if app.Exists("id:confirm-prompt-args") {
			app.Press("id:confirm-prompt-args")
			app.Settle()
		}
		return !app.Exists("id:prompt-args-dialog")
	})
	app.WaitUntil(5*time.Second, func() bool {
		return app.Find("label:Message").Text == "/mcp__fake__summarize release notes brief"
	})
}

// TestContextUsagePrefersReportedWindow checks the composer's remaining
// context uses the provider's window, then the catalog, then the name.
func TestContextUsagePrefersReportedWindow(t *testing.T) {
	runs := []proto.Run{{Usage: &proto.Usage{InputTokens: 1_000, OutputTokens: 500, ContextWindow: 1_000_000}}}
	models := []proto.Model{{ID: "claude-sonnet-5-5", ContextWindow: 200_000}}
	if used, limit := contextUsage("claude-sonnet-5-5", runs, models); used != 1_500 || limit != 1_000_000 {
		t.Fatalf("reported window: used=%d limit=%d", used, limit)
	}
	runs = []proto.Run{{Usage: &proto.Usage{InputTokens: 1_000, OutputTokens: 500}}}
	if _, limit := contextUsage("claude-sonnet-5-5", runs, models); limit != 200_000 {
		t.Fatalf("catalog window = %d", limit)
	}
	if _, limit := contextUsage("gemini-3-pro", nil, nil); limit != 1_000_000 {
		t.Fatalf("fallback window = %d", limit)
	}
}

// TestUIProviderSignIn runs a provider's login command in a thread's
// terminal from Settings.
func TestUIProviderSignIn(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in
--version) echo 1.0.0 ;;
auth) echo "Not logged in" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Sign in", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:settings", 10*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:settings-nav-providers") {
			return true
		}
		if app.Exists("id:settings") {
			app.Press("id:settings")
			app.Settle()
		}
		return app.Exists("id:settings-nav-providers")
	})
	app.Press("id:settings-nav-providers")
	app.WaitUntil(20*time.Second, func() bool { return app.Exists("id:settings-login-claude") })
	app.Press("id:settings-login-claude")
	app.WaitUntil(20*time.Second, func() bool {
		if len(srv.Terminals.List(thread.ID)) > 0 && app.Exists("id:surface-tab-terminal") {
			return true
		}
		if app.Exists("id:settings-login-claude") {
			app.Press("id:settings-login-claude")
			app.Settle()
		}
		return len(srv.Terminals.List(thread.ID)) > 0 && app.Exists("id:surface-tab-terminal")
	})
}

// TestUITerminalExitedShell offers a restart when the active shell exits.
func TestUITerminalExitedShell(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Shell", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:toggle-right-panel", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:surface-terminal") {
			return true
		}
		if app.Exists("id:toggle-right-panel") {
			app.Press("id:toggle-right-panel")
			app.Settle()
		}
		if app.Exists("id:surface-add") {
			app.Press("id:surface-add")
			app.Settle()
		}
		return app.Exists("id:surface-terminal")
	})
	app.Press("id:surface-terminal")
	app.WaitFor("id:terminal-view", 15*time.Second)

	app.WaitUntil(15*time.Second, func() bool {
		list := srv.Terminals.List(thread.ID)
		if len(list) == 0 {
			return false
		}
		_ = list[0].Write([]byte("exit\n"))
		return true
	})
	app.WaitFor("id:terminal-restart-exited", 15*time.Second)
	app.Press("id:terminal-restart-exited")
	app.WaitForGone("id:terminal-restart-exited", 15*time.Second)
	app.WaitFor("id:terminal-view", 15*time.Second)
}

// TestUIDevices lists a simulator, boots it and shows its screen.
func TestUIDevices(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "booted")
	logPath := filepath.Join(dir, "device.log")
	script := `#!/bin/sh
case "$2" in
list)
  if [ -f "$DEVICE_STATE" ]; then state=Booted; else state=Shutdown; fi
  printf '{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-17-0":[{"udid":"7E99F39F-299A-4E23-A2D0-7948BC3EE5F4","name":"iPhone 15 Pro","state":"%s","isAvailable":true}]}}\n' "$state"
  ;;
boot) touch "$DEVICE_STATE"; echo "boot $3" >> "$DEVICE_LOG" ;;
shutdown) rm -f "$DEVICE_STATE"; echo "shutdown $3" >> "$DEVICE_LOG" ;;
io) echo io >> "$DEVICE_LOG"; printf 'PNGDATA' > "$5" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "xcrun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVICE_STATE", state)
	t.Setenv("DEVICE_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Device", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:toggle-right-panel", 10*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:surface-device") {
			return true
		}
		if app.Exists("id:surface-add") {
			app.Press("id:surface-add")
			app.Settle()
		} else if app.Exists("id:toggle-right-panel") {
			app.Press("id:toggle-right-panel")
			app.Settle()
		}
		return app.Exists("id:surface-device")
	})
	app.Press("id:surface-device")

	app.WaitFor("id:device-select", 15*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:devices-dialog") {
			return true
		}
		if app.Exists("id:device-select") {
			app.Press("id:device-select")
			app.Settle()
		}
		return app.Exists("id:devices-dialog")
	})
	app.WaitFor("id:device-7E99F39F-299A-4E23-A2D0-7948BC3EE5F4-0", 5*time.Second)
	app.Press("id:device-7E99F39F-299A-4E23-A2D0-7948BC3EE5F4-0")
	app.WaitFor("id:device-boot", 5*time.Second)
	app.Press("id:device-boot")
	app.WaitFor("id:device-screen", 15*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		raw, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(raw), "boot 7E99F39F")
	})
	app.WaitUntil(15*time.Second, func() bool {
		raw, err := os.ReadFile(logPath)
		if err == nil && strings.Count(string(raw), "io") >= 2 {
			return true
		}
		app.Advance(2100 * time.Millisecond)
		app.Settle()
		return false
	})
	app.WaitFor("id:device-live", 5*time.Second)
	app.Press("id:device-live")
	app.WaitUntil(15*time.Second, func() bool {
		if _, err := os.Stat(state); os.IsNotExist(err) {
			return true
		}
		if app.Exists("id:device-shutdown") {
			app.Press("id:device-shutdown")
			app.Settle()
		}
		return false
	})
}

// sideRunner answers a side question at once.
type sideRunner struct{}

func (sideRunner) ID() string   { return "side-fake" }
func (sideRunner) Name() string { return "Side Fake" }

func (sideRunner) Snapshot(context.Context) proto.Provider {
	return proto.Provider{ID: "side-fake", Name: "Side Fake", Installed: true, Version: "1",
		Models: []proto.Model{{ID: "side-fake-1", Name: "Side One", Provider: "side-fake", Default: true},
			{ID: "side-fake-2", Name: "Side Two", Provider: "side-fake"}}}
}

func (sideRunner) Run(ctx context.Context, turn provider.Turn, sink provider.Sink) error {
	sink.Text("side reply")
	return nil
}

// TestUISideChat opens the Side chat surface and asks a question.
func TestUISideChat(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Parent", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:toggle-right-panel", 10*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:surface-chat") {
			return true
		}
		if app.Exists("id:surface-add") {
			app.Press("id:surface-add")
			app.Settle()
		} else if app.Exists("id:toggle-right-panel") {
			app.Press("id:toggle-right-panel")
			app.Settle()
		}
		return app.Exists("id:surface-chat")
	})
	app.Press("id:surface-chat")
	app.WaitFor("id:side-input", 20*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("id:side-input").Text == "quick question" {
			return true
		}
		app.Clear("id:side-input")
		app.Type("id:side-input", "quick question")
		app.Settle()
		return app.Find("id:side-input").Text == "quick question"
	})
	app.WaitUntil(15*time.Second, func() bool {
		if len(app.Query("text:side reply")) > 0 {
			return true
		}
		if app.Exists("id:side-send") {
			app.Press("id:side-send")
			app.Settle()
		}
		return len(app.Query("text:side reply")) > 0
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:side-model-side-fake-2") {
			return true
		}
		if app.Exists("id:side-model") {
			app.Press("id:side-model")
			app.Settle()
		}
		return app.Exists("id:side-model-side-fake-2")
	})
	app.Press("id:side-model-side-fake-2")
	app.WaitUntil(10*time.Second, func() bool {
		side, ok, err := srv.Store.SideChat(ctx, thread.ID)
		return err == nil && ok && side.Model == "side-fake-2"
	})

	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:side-perm-supervised") {
			return true
		}
		if app.Exists("id:side-permission") {
			app.Press("id:side-permission")
			app.Settle()
		}
		return app.Exists("id:side-perm-supervised")
	})
	app.Press("id:side-perm-supervised")
	app.WaitUntil(10*time.Second, func() bool {
		side, ok, err := srv.Store.SideChat(ctx, thread.ID)
		return err == nil && ok && side.Permission == proto.PermissionSupervised
	})

	path := filepath.Join(t.TempDir(), "side-note.txt")
	if err := os.WriteFile(path, []byte("attached"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.DropFiles("id:side-composer", (&url.URL{Scheme: "file", Path: path}).String())
	app.WaitFor("id:side-attachment-side-note.txt", 15*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("id:side-input").Text == "with a file" {
			return true
		}
		app.Clear("id:side-input")
		app.Type("id:side-input", "with a file")
		app.Settle()
		return app.Find("id:side-input").Text == "with a file"
	})
	app.WaitUntil(15*time.Second, func() bool {
		if !app.Exists("id:side-attachment-side-note.txt") {
			return true
		}
		if app.Exists("id:side-send") {
			app.Press("id:side-send")
			app.Settle()
		}
		return !app.Exists("id:side-attachment-side-note.txt")
	})

	if app.Exists("text:Side chat · Parent") {
		t.Fatal("the side chat is listed in the sidebar")
	}
}

// TestUINewProject validates the folder and names a project after it.
func TestUINewProject(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	repo := gitRepo(t)
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:welcome-new-project", 15*time.Second)
	app.Press("id:welcome-new-project")
	app.WaitFor("id:new-project-dialog", 5*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("label:Project path").Text == "/definitely/not/here" {
			return true
		}
		app.Clear("label:Project path")
		app.Type("label:Project path", "/definitely/not/here")
		app.Settle()
		return app.Find("label:Project path").Text == "/definitely/not/here"
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("text:That directory does not exist.") {
			return true
		}
		if app.Exists("id:create-project") {
			app.Press("id:create-project")
			app.Settle()
		}
		return app.Exists("text:That directory does not exist.")
	})
	if !app.Exists("id:new-project-dialog") {
		t.Fatal("the dialog closed on an invalid path")
	}

	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("label:Project path").Text == repo {
			return true
		}
		app.Clear("label:Project path")
		app.Type("label:Project path", repo)
		app.Settle()
		return app.Find("label:Project path").Text == repo
	})
	ctx := context.Background()
	app.WaitUntil(15*time.Second, func() bool {
		projects, err := srv.Store.Projects(ctx)
		if err == nil && len(projects) == 1 {
			return true
		}
		if app.Exists("id:create-project") && app.Enabled("id:create-project") &&
			app.Find("label:Project path").Text == repo {
			app.Press("id:create-project")
			app.Settle()
			time.Sleep(150 * time.Millisecond)
		}
		return false
	})
	if app.Exists("id:new-project-dialog") {
		app.Press("id:cancel-project")
		app.Settle()
	}
	projects, err := srv.Store.Projects(ctx)
	if err != nil || len(projects) != 1 || projects[0].Name != filepath.Base(repo) {
		t.Fatalf("projects = %#v, %v", projects, err)
	}
}

// TestUINewThreadOptions creates a worktree thread from the dialog.
func TestUINewThreadOptions(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	repo := gitRepo(t)
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:new-thread", 15*time.Second)
	app.Press("id:new-thread")
	app.WaitFor("id:new-thread-dialog", 5*time.Second)
	app.Press("id:new-thread-project-" + project.ID)
	app.Press("id:new-thread-provider-side-fake")
	app.Press("id:new-thread-perm-auto")
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:new-thread-branch") {
			return true
		}
		if app.Exists("id:toggle-worktree") {
			app.Press("id:toggle-worktree")
			app.Settle()
		}
		return app.Exists("id:new-thread-branch")
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("id:new-thread-branch").Text == "t3go/test" {
			return true
		}
		app.Clear("id:new-thread-branch")
		app.Type("id:new-thread-branch", "t3go/test")
		app.Settle()
		return app.Find("id:new-thread-branch").Text == "t3go/test"
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Find("id:new-thread-base").Text == "HEAD" {
			return true
		}
		app.Clear("id:new-thread-base")
		app.Type("id:new-thread-base", "HEAD")
		app.Settle()
		return app.Find("id:new-thread-base").Text == "HEAD"
	})
	app.Press("id:create-thread")
	app.WaitUntil(15*time.Second, func() bool {
		threads, err := srv.Store.Threads(ctx, project.ID, false)
		if err != nil || len(threads) == 0 {
			return false
		}
		created := threads[0]
		return created.Branch == "t3go/test" && created.WorktreePath != "" &&
			created.Provider == "side-fake" && created.Permission == proto.PermissionAuto
	})
}

// TestUIPreviewServers lists a local server and opens it in the browser.
func TestUIPreviewServers(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
printf 'p123\ncnode\nn127.0.0.1:3000\n'
`
	if err := os.WriteFile(filepath.Join(dir, "lsof"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Preview", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:toggle-right-panel", 10*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:surface-preview") {
			return true
		}
		if app.Exists("id:surface-add") {
			app.Press("id:surface-add")
			app.Settle()
		} else if app.Exists("id:toggle-right-panel") {
			app.Press("id:toggle-right-panel")
			app.Settle()
		}
		return app.Exists("id:surface-preview")
	})
	app.WaitUntil(20*time.Second, func() bool {
		if app.Exists("id:preview-open-3000") {
			return true
		}
		if app.Exists("id:surface-preview") {
			app.Press("id:surface-preview")
			app.Settle()
		}
		return app.Exists("id:preview-open-3000")
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:browser-view") {
			return true
		}
		if app.Exists("id:preview-open-3000") {
			app.Press("id:preview-open-3000")
			app.Settle()
		}
		return app.Exists("id:browser-view")
	})
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Address").Text == "http://localhost:3000" })
}

// TestUICustomModel adds a model id in Settings and picks it in the composer.
func TestUICustomModel(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Custom", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.WaitFor("id:settings", 10*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 10*time.Second)
	app.Press("id:settings-nav-providers")
	app.WaitFor("id:settings-add-model-side-fake", 10*time.Second)
	app.Press("id:settings-add-model-side-fake")
	app.WaitFor("id:custom-model-dialog", 5*time.Second)

	for id, want := range map[string]string{"id:custom-model-id": "side-fake-custom", "id:custom-model-name": "Custom Side"} {
		id, want := id, want
		app.WaitUntil(10*time.Second, func() bool {
			if app.Find(id).Text == want {
				return true
			}
			app.Clear(id)
			app.Type(id, want)
			app.Settle()
			return app.Find(id).Text == want
		})
	}
	app.Press("id:confirm-custom-model")
	app.WaitForGone("id:custom-model-dialog", 10*time.Second)
	app.WaitFor("text:Custom Side", 5*time.Second)

	app.Press("text:Close")
	app.WaitForGone("id:settings-dialog", 5*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:composer-model", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:composer-model-side-fake-custom") {
			return true
		}
		if app.Exists("id:composer-model") {
			app.Press("id:composer-model")
			app.Settle()
		}
		return app.Exists("id:composer-model-side-fake-custom")
	})
	app.Press("id:composer-model-side-fake-custom")
	app.WaitUntil(10*time.Second, func() bool {
		updated, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && updated.Model == "side-fake-custom"
	})
}

// TestUIRestoreRun restores tracked files from a run's checkpoint.
func TestUIRestoreRun(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	before, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Restore", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	const runID = "run_restore_ui"
	ref, err := git.Checkpoint(ctx, repo, thread.ID, runID)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("agent wrote this\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ended := time.Now()
	if err := srv.Store.CreateRun(ctx, proto.Run{ID: runID, ThreadID: thread.ID, Status: proto.RunCompleted,
		Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess, Prompt: "Change files",
		StartedAt: ended.Add(-time.Second), EndedAt: &ended, Checkpoint: ref}); err != nil {
		t.Fatalf("run: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:run-restore-" + runID) {
			return true
		}
		if app.Exists("id:history") {
			app.Press("id:history")
			app.Settle()
		}
		return app.Exists("id:run-restore-" + runID)
	})
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:restore-dialog") {
			return true
		}
		if app.Exists("id:run-restore-" + runID) {
			app.Press("id:run-restore-" + runID)
			app.Settle()
		}
		return app.Exists("id:restore-dialog")
	})
	app.WaitUntil(10*time.Second, func() bool {
		after, err := os.ReadFile(filepath.Join(repo, "README.md"))
		if err == nil && string(after) == string(before) {
			return true
		}
		if app.Exists("id:confirm-restore-run") {
			app.Press("id:confirm-restore-run")
			app.Settle()
		}
		return false
	})
}

// TestUISettingsUsage shows token and cost totals.
func TestUISettingsUsage(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Usage", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	ended := time.Now()
	if err := srv.Store.CreateRun(ctx, proto.Run{ID: "run_usage_ui", ThreadID: thread.ID, Status: proto.RunCompleted,
		Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess, StartedAt: ended.Add(-time.Second),
		EndedAt: &ended, Usage: &proto.Usage{TotalTokens: 1500, CostUSD: 0.05}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-nav-usage", 10*time.Second)
	app.Press("id:settings-nav-usage")
	app.WaitFor("id:usage-chart", 10*time.Second)
	app.WaitFor("id:usage-provider-claude", 5*time.Second)
	app.WaitFor("text:1.5k tokens", 5*time.Second)
	app.WaitFor("text:Cost", 5*time.Second)
}

// TestUIOpenEditor sets the editor and opens the thread's checkout.
func TestUIOpenEditor(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "editor.log")
	script := `#!/bin/sh
echo "$@" >> "$EDITOR_LOG"
`
	if err := os.WriteFile(filepath.Join(dir, "zed"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Editor", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:settings", 10*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:settings-nav-general") {
			return true
		}
		if app.Exists("id:settings") {
			app.Press("id:settings")
			app.Settle()
		}
		return app.Exists("id:settings-nav-general")
	})
	app.WaitFor("id:settings-editor-zed", 5*time.Second)
	app.Press("id:settings-editor-zed")
	app.Press("text:Close")
	app.WaitForGone("id:settings-dialog", 5*time.Second)
	app.WaitUntil(30*time.Second, func() bool {
		raw, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(raw), repo) {
			return true
		}
		if app.Exists("id:open-editor") {
			app.Press("id:open-editor")
			app.Settle()
		}
		return false
	})
}

// TestUISearchMessages finds a transcript message from the palette.
func TestUISearchMessages(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Search Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Search thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_purple", ThreadID: thread.ID, Role: proto.RoleUser,
		Kind: proto.KindText, Content: "find the purple elephant", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:open-palette", 15*time.Second)
	app.Press("id:open-palette")
	app.WaitFor("id:palette", 5*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("text:/purple elephant/") {
			return true
		}
		if app.Exists("label:Search commands") {
			app.Clear("label:Search commands")
			app.Type("label:Search commands", "purple elephant")
			app.Settle()
			app.Advance(300 * time.Millisecond)
			app.Settle()
		}
		return app.Exists("text:/purple elephant/")
	})
	app.Key("enter")
	app.WaitFor("id:content-header >> text:Search thread", 10*time.Second)
}

// TestUISettingsAccent saves a custom accent color.
func TestUISettingsAccent(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:settings-accent-violet") {
			return true
		}
		if app.Exists("id:settings") {
			app.Press("id:settings")
			app.Settle()
		}
		return app.Exists("id:settings-accent-violet")
	})
	app.Press("id:settings-accent-violet")
	app.Press("id:settings-save")
	app.WaitUntil(10*time.Second, func() bool {
		return srv.Service.Settings(ctx).Accent == "#7c3aed"
	})
}

// TestUIMobileComposerStaysVisible keeps the composer above the keyboard
// with a long transcript on a phone-sized window.
func TestUIMobileComposerStaysVisible(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Long thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	for i := 0; i < 40; i++ {
		if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "msg_long_" + fmt.Sprint(i), ThreadID: thread.ID,
			Role: proto.RoleAssistant, Kind: proto.KindText, Content: "Line " + fmt.Sprint(i) + "\n\nA long answer that wraps over several lines in a narrow window.",
			CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{OS: core.OSAndroid,
		Idiom: core.IdiomPhone, KeyboardHeight: 300, SafeArea: core.Edges{Top: 24, Bottom: 16}}),
		nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
			"serverUrl": ts.URL,
			"token":     cfg.Token,
		}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:composer-card", 10*time.Second)
	composer := app.Find("id:composer-card")
	if bottom := composer.Frame.Origin.Y + composer.Frame.Size.Height; bottom > 844-300 {
		t.Fatalf("the composer is covered by the keyboard: %v", composer.Frame)
	}
}

// TestUIMobileSettings fits settings into a phone-sized window.
func TestUIMobileSettings(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Mobile thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{OS: core.OSAndroid,
		Idiom: core.IdiomPhone, SafeArea: core.Edges{Top: 24, Bottom: 16}}),
		nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
			"serverUrl": ts.URL,
			"token":     cfg.Token,
		}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.WaitFor("id:settings", 10*time.Second)
	app.WaitUntil(15*time.Second, func() bool {
		if app.Exists("id:settings-nav-general") {
			return true
		}
		if app.Exists("id:settings") {
			app.Press("id:settings")
			app.Settle()
		}
		return app.Exists("id:settings-nav-general")
	})
	app.Press("id:settings-nav-general")
	app.WaitFor("id:settings-editor-auto", 5*time.Second)
	save := app.Find("id:settings-save")
	if save.Frame.Origin.X < 0 || save.Frame.Origin.X+save.Frame.Size.Width > 390 ||
		save.Frame.Origin.Y+save.Frame.Size.Height > 844 {
		t.Fatalf("save is outside the phone window: %v dialog=%v nav=%v", save.Frame,
			app.Find("id:settings-dialog").Frame, app.Find("id:settings-nav-general").Frame)
	}
	app.Press("id:settings-nav-about")
	app.WaitFor("id:settings-disconnect", 5*time.Second)
}

// TestUIMobileDialogsFit keeps the dialogs inside a phone and above the
// software keyboard.
func TestUIMobileDialogsFit(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	if _, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t)); err != nil {
		t.Fatalf("project: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{OS: core.OSAndroid,
		Idiom: core.IdiomPhone, KeyboardHeight: 300, SafeArea: core.Edges{Top: 24, Bottom: 16}}),
		nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
			"serverUrl": ts.URL,
			"token":     cfg.Token,
		}))
	app.WaitFor("id:new-thread", 15*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:new-thread-dialog") {
			return true
		}
		app.Press("id:new-thread")
		app.Settle()
		return app.Exists("id:new-thread-dialog")
	})
	dialog := app.Find("id:new-thread-dialog").Frame
	if dialog.Origin.X < 0 || dialog.Origin.X+dialog.Size.Width > 390 {
		t.Fatalf("new thread dialog overflows: %v", dialog)
	}
	create := app.Find("id:create-thread").Frame
	if create.Origin.X < 0 || create.Origin.X+create.Size.Width > 390 {
		t.Fatalf("create button overflows: %v", create)
	}
	if create.Size.Height < 32 {
		t.Fatalf("create button is still using a dense touch size: %v", create)
	}
	if create.Origin.Y+create.Size.Height > 844-300 {
		t.Fatalf("create button is covered by the keyboard: %v", create)
	}
	if dialog.Origin.Y+dialog.Size.Height > 844-300 {
		t.Fatalf("new thread dialog is covered by the keyboard: %v", dialog)
	}
	threadTitle := app.Find("label:Thread title").Frame
	if threadTitle.Size.Height < 44 {
		t.Fatalf("thread title input is still using a dense touch size: %v", threadTitle)
	}
	app.Press("id:cancel-thread")
	app.WaitForGone("id:new-thread-dialog", 5*time.Second)

	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:new-project-dialog") {
			return true
		}
		app.Press("id:new-project")
		app.Settle()
		return app.Exists("id:new-project-dialog")
	})
	projectDialog := app.Find("id:new-project-dialog").Frame
	if projectDialog.Origin.X < 0 || projectDialog.Origin.X+projectDialog.Size.Width > 390 {
		t.Fatalf("new project dialog overflows: %v", projectDialog)
	}
	if projectDialog.Origin.Y+projectDialog.Size.Height > 844-300 {
		t.Fatalf("new project dialog is covered by the keyboard: %v", projectDialog)
	}
}

// TestUIMobileThreadActions keeps the phone's thread header compact: a
// branch button and one menu rather than every action in a row.
func TestUIMobileThreadActions(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Mobile thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{OS: core.OSAndroid,
		Idiom: core.IdiomPhone, SafeArea: core.Edges{Top: 24, Bottom: 16}}),
		nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
			"serverUrl": ts.URL,
			"token":     cfg.Token,
		}))
	app.WaitFor("id:thread-"+thread.ID, 15*time.Second)
	app.Press("id:thread-" + thread.ID)
	app.WaitFor("id:composer-card", 10*time.Second)
	app.WaitFor("id:branch-menu", 5*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:rename-thread") {
			return true
		}
		if app.Exists("id:thread-actions") {
			app.Press("id:thread-actions")
			app.Settle()
		}
		return app.Exists("id:rename-thread")
	})
	app.WaitFor("id:history", 5*time.Second)
	app.WaitFor("id:open-diff", 5*time.Second)
}

// TestUIMobilePalette keeps the phone palette above the keyboard and
// gives it a visible way to close when the sheet cannot be swiped.
func TestUIMobilePalette(t *testing.T) {
	_, ts, cfg := startEnv(t)
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{OS: core.OSAndroid,
		Idiom: core.IdiomPhone, KeyboardHeight: 300, SafeArea: core.Edges{Top: 24, Bottom: 16}}),
		nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
			"serverUrl": ts.URL,
			"token":     cfg.Token,
		}))
	app.WaitFor("id:open-palette", 15*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:palette") {
			return true
		}
		app.Press("id:open-palette")
		app.Settle()
		return app.Exists("id:palette")
	})
	app.WaitFor("id:palette-close", 5*time.Second)
	app.Press("id:palette-close")
	app.WaitForGone("id:palette", 5*time.Second)

	// The Android back action must dismiss the palette itself, even though
	// its search field has opened the software keyboard.
	app.Press("id:open-palette")
	app.WaitFor("id:palette", 5*time.Second)
	app.Back()
	app.WaitForGone("id:palette", 5*time.Second)
}

// TestUIMobileBrowseHostDirs browses the connected environment's folders
// from a phone, whose own file system cannot see them.
func TestUIMobileBrowseHostDirs(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{OS: core.OSAndroid,
		Idiom: core.IdiomPhone, SafeArea: core.Edges{Top: 24, Bottom: 16}}),
		nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
			"serverUrl": ts.URL,
			"token":     cfg.Token,
		}))
	app.WaitFor("id:new-project", 15*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:new-project-dialog") {
			return true
		}
		app.Press("id:new-project")
		app.Settle()
		return app.Exists("id:new-project-dialog")
	})
	app.Clear("label:Project path")
	app.Type("label:Project path", root)
	app.WaitUntil(10*time.Second, func() bool {
		if app.Exists("id:dirs-dialog") {
			return true
		}
		if app.Exists("id:browse-project") {
			app.Press("id:browse-project")
			app.Settle()
		}
		return app.Exists("id:dirs-dialog")
	})
	app.WaitFor("id:dirs-entry-alpha", 10*time.Second)
	app.WaitFor("id:dirs-entry-beta", 10*time.Second)
	app.Press("id:dirs-new-folder")
	app.WaitFor("id:new-dir-dialog", 5*time.Second)
	app.Type("label:Folder name", "created")
	app.Press("id:create-dir")
	app.WaitForGone("id:new-dir-dialog", 5*time.Second)
	app.WaitFor("id:dirs-entry-created", 10*time.Second)
	if info, err := os.Stat(filepath.Join(root, "created")); err != nil || !info.IsDir() {
		t.Fatalf("created folder missing: %v", err)
	}
	app.WaitUntil(10*time.Second, func() bool {
		if app.Enabled("id:dirs-use") && app.Find("id:dirs-path").Text == filepath.Join(root, "alpha") {
			return true
		}
		if app.Exists("id:dirs-entry-alpha") {
			app.Press("id:dirs-entry-alpha")
			app.Settle()
		}
		return false
	})
	app.Press("id:dirs-use")
	app.WaitForGone("id:dirs-dialog", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		return app.Find("label:Project path").Text == filepath.Join(root, "alpha")
	})
	if name := app.Find("label:Project name").Text; name != "alpha" {
		t.Fatalf("project name = %q", name)
	}
	app.Press("id:create-project")
	ctx := context.Background()
	app.WaitUntil(10*time.Second, func() bool {
		projects, err := srv.Store.Projects(ctx)
		return err == nil && len(projects) == 1 && projects[0].Path == filepath.Join(root, "alpha")
	})
}

// TestParseLoginFragment reads a login from a web link's fragment.
func TestParseLoginFragment(t *testing.T) {
	cases := []struct {
		fragment, url, token string
	}{
		{"", "", ""},
		{"#", "", ""},
		{"#token=abc", "", "abc"},
		{"#token=a%2Bb", "", "a+b"},
		{"#url=http://h:1&token=abc", "http://h:1", "abc"},
		{"#t3go://pair?url=http://h:1&token=abc", "http://h:1", "abc"},
		{"#t3go%3A%2F%2Fpair%3Furl%3Dhttp%3A%2F%2Fh%3A1%26token%3Dabc", "http://h:1", "abc"},
	}
	for _, tc := range cases {
		u, tok := parseLoginFragment(tc.fragment)
		if u != tc.url || tok != tc.token {
			t.Errorf("%q = (%q, %q), want (%q, %q)", tc.fragment, u, tok, tc.url, tc.token)
		}
	}
}
