package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUIPreviewServers lists the environment's local dev servers and
// opens one in the browser surface.
func TestUIPreviewServers(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'p123\\ncnode\\nn127.0.0.1:3000\\n'\n"
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

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	tester.wait(t, 15*time.Second, func() bool {
		return len(session.threads) == 1
	}, "the thread list to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID
	}, "the detail to load")

	session.showSurface("preview")
	session.loadPreviewServers()
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.previewServers) > 0
	}, "the local servers to arrive")
	tester.pump()
	if !tester.tt.HasText("node") {
		t.Fatalf("the preview surface does not list the server: %v", tester.tt.Texts())
	}

	// Opening one sends the browser surface to its address.
	session.openPreviewServer(session.previewServers[0].URL)
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.surface == "browser" && strings.Contains(session.browserURL, "localhost:3000")
	}, "the browser to open the server")
	tester.pump()
	if !tester.tt.HasText("localhost:3000") {
		t.Fatalf("the browser surface does not show the address: %v", tester.tt.Texts())
	}
}

// TestUIEditMessageAndFork edits a user message and forks before it.
func TestUIEditMessageAndFork(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Fork thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	asked := time.Now()
	answered := asked.Add(time.Second)
	for _, m := range []proto.Message{
		{ID: "msg_q", ThreadID: thread.ID, Role: proto.RoleUser, Kind: proto.KindText,
			Content: "Do the thing", Status: "completed", CreatedAt: asked},
		{ID: "msg_a", ThreadID: thread.ID, Role: proto.RoleAssistant, Kind: proto.KindText,
			Content: "Did the thing", Status: "completed", CreatedAt: answered},
	} {
		if _, err := srv.Store.AppendMessage(ctx, m); err != nil {
			t.Fatalf("message: %v", err)
		}
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	tester.wait(t, 15*time.Second, func() bool {
		return len(session.threads) == 1
	}, "the thread list to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.messages) == 2
	}, "the transcript to arrive")
	tester.pump()
	if !tester.tt.HasText("Do the thing") {
		t.Fatalf("the transcript does not show the message: %v", tester.tt.Texts())
	}

	// Editing a message opens the dialog with the message's text.
	var user proto.Message
	for _, m := range session.messages {
		if m.Role == proto.RoleUser {
			user = m
		}
	}
	session.openEditMessage(user)
	tester.pump()
	if !tester.tt.HasText("Edit message") {
		t.Fatalf("the edit dialog did not open: %v", tester.tt.Texts())
	}
	session.editDraft = "Do the thing better"
	tester.pump()
	session.confirmEditMessage()

	// The fork lands as a new thread whose first prompt is the edit.
	var forked proto.Thread
	tester.wait(t, 40*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		if len(session.threads) != 2 {
			return false
		}
		for _, th := range session.threads {
			if th.ID != thread.ID {
				forked = th
			}
		}
		return forked.ID != "" && session.selected == forked.ID
	}, "the fork to open")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.messages) >= 1
	}, "the fork's transcript to load")
	// The fork copies the transcript before the message, then sends the
	// edit as its first prompt.
	found := false
	for _, m := range session.messages {
		if m.Role == proto.RoleUser && strings.Contains(m.Content, "Do the thing better") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the fork does not carry the edit: %v", session.messages)
	}
}
