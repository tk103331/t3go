package main

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
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
	app.Press("id:settings-save")
	app.WaitFor("id:content-header >> text:Team env", 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		s := srv.Service.Settings(ctx)
		return s.Title == "Team env" && s.DefaultPermission == string(proto.PermissionAcceptEdits)
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
	app.Press("text:Off") // Own worktree: on
	app.WaitFor("label:Branch (optional)", 5*time.Second)
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
	app.WaitFor("text:◌ Disposable thread", 5*time.Second)

	app.LongPress("id:thread-" + thread.ID)
	app.WaitFor("id:archive-thread-"+thread.ID, 5*time.Second)
	app.Press("id:archive-thread-" + thread.ID)
	app.WaitForGone("text:◌ Disposable thread", 10*time.Second)
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

	app.Press("id:history")
	app.WaitFor("text:Change the readme", 5*time.Second)
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
