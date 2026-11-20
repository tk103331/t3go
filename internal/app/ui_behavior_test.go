package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUIQueuedPrompt queues a prompt under a live run, checks the client
// sends it when the run ends, and that a queued prompt can be dropped.
func TestUIQueuedPrompt(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	runner := uiRunner{started: make(chan struct{}, 4)}
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
	first, err := srv.Service.Send(ctx, thread.ID, "First prompt")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	t.Cleanup(func() { srv.Service.Stop(thread.ID) })

	tester, session := startApp(t, ts, cfg, nil)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		tester.post.drain()
		tester.tt.Frame()
		if session != nil && session.connected {
			break
		}
		if session != nil && session.connErr != "" {
			t.Fatalf("connect failed: %v", session.connErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if session == nil || !session.connected {
		message := "no active session"
		if session != nil {
			message = fmt.Sprintf("connErr=%q connecting=%v", session.connErr, session.connecting)
		}
		t.Fatalf("the environment did not connect; sessions=%d %s", len(tester.manager.sessions), message)
	}
	tester.wait(t, 15*time.Second, func() bool {
		for _, th := range session.threads {
			if th.ID == thread.ID {
				return true
			}
		}
		return false
	}, "the thread list to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		return session.running
	}, "the live run to show")

	// A prompt typed under a live run queues.
	session.draft = "Queued prompt"
	session.send()
	tester.wait(t, 5*time.Second, func() bool {
		return len(session.queue) == 1 && session.queue[0].Text == "Queued prompt"
	}, "the prompt to queue")
	tester.pump()
	runs, err := srv.Store.Runs(ctx, thread.ID)
	if err != nil || len(runs) != 1 || runs[0].ID != first.ID {
		t.Fatalf("runs before the turn ended = %#v, %v", runs, err)
	}

	// Stopping the live run finishes it, so the client posts the prompt
	// that waited behind it.
	srv.Service.Stop(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		runs, err := srv.Store.Runs(ctx, thread.ID)
		return err == nil && len(runs) == 2 && runs[1].Prompt == "Queued prompt"
	}, "the queued prompt to send")
	tester.wait(t, 10*time.Second, func() bool {
		return len(session.queue) == 0
	}, "the queue to empty")

	// A queued prompt can be dropped instead.
	session.draft = "Never sent"
	session.send()
	tester.wait(t, 5*time.Second, func() bool {
		return len(session.queue) == 1
	}, "the prompt to queue again")
	session.dropQueued(0)
	tester.wait(t, 5*time.Second, func() bool {
		return len(session.queue) == 0
	}, "the prompt to leave the queue")
	runs, err = srv.Store.Runs(ctx, thread.ID)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs after removing = %d, %v", len(runs), err)
	}
}

// TestUIArchiveThread archives a thread from its menu and brings it back
// with the archived toggle, checking the server kept each change.
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

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	tester.wait(t, 15*time.Second, func() bool {
		for _, th := range session.threads {
			if th.ID == thread.ID {
				return true
			}
		}
		return false
	}, "the thread list to arrive")

	session.setArchived(thread, true)
	tester.wait(t, 10*time.Second, func() bool {
		th, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && th.Archived
	}, "the thread to archive")
	// The inbox keeps the thread in the snapshot but filters it at the
	// view, so it counts as gone there.
	tester.wait(t, 5*time.Second, func() bool {
		count := 0
		for _, row := range session.mobileThreads() {
			if row.thread.ID == thread.ID && !row.thread.Archived {
				count++
			}
		}
		return count == 0 && !session.showArchived
	}, "the thread to leave the inbox")

	session.toggleShowArchived()
	tester.wait(t, 10*time.Second, func() bool {
		for _, th := range session.threads {
			if th.ID == thread.ID {
				return th.Archived
			}
		}
		return false
	}, "the thread to come back archived")

	session.setArchived(thread, false)
	tester.wait(t, 10*time.Second, func() bool {
		th, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && !th.Archived
	}, "the thread to unarchive")
}

// TestUIRenameAndDelete renames a thread and a project, then removes
// both, checking the server kept each change.
func TestUIRenameAndDelete(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Rename Project", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Renamed thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
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

	session.askRenameThread(thread)
	session.renameName = "Better name"
	tester.pump()
	session.confirmRename()
	tester.wait(t, 10*time.Second, func() bool {
		th, err := srv.Store.Thread(ctx, thread.ID)
		return err == nil && th.Title == "Better name"
	}, "the thread to rename")

	session.askRenameProject(session.projects[0])
	session.renameName = "Renamed Project 2"
	tester.pump()
	session.confirmRename()
	tester.wait(t, 10*time.Second, func() bool {
		for _, p := range session.projects {
			if p.ID == project.ID {
				return p.Name == "Renamed Project 2"
			}
		}
		return false
	}, "the project to rename")

	session.askDeleteThread(thread)
	tester.pump()
	session.confirmDelete()
	tester.wait(t, 10*time.Second, func() bool {
		threads, err := srv.Store.Threads(ctx, project.ID, true)
		return err == nil && len(threads) == 0
	}, "the thread to delete")
	tester.wait(t, 10*time.Second, func() bool {
		return len(session.threads) == 0
	}, "the thread to leave the client")

	session.askDeleteProject(session.projects[0])
	tester.pump()
	session.confirmDelete()
	tester.wait(t, 10*time.Second, func() bool {
		return len(session.projects) == 0
	}, "the project to leave the client")
	projects, err := srv.Store.Projects(ctx)
	if err != nil || len(projects) != 0 {
		t.Fatalf("projects after delete = %#v, %v", projects, err)
	}
}
