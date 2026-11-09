package main

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/config"
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
