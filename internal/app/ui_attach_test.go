package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUIComposerAttachment drops a file on the composer, removes the
// chip (which deletes the upload), then sends the file with a message.
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
	t.Cleanup(func() { srv.Service.Stop(thread.ID) })

	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello attachment"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
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

	session.draft = "Read this"
	session.attachPaths([]string{path})
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.attachments) == 1 && session.attachments[0].Name == "note.txt"
	}, "the dropped file to arrive")
	tester.pump()
	if !tester.tt.HasText("note.txt") {
		t.Fatalf("the chip does not show the file: %v", tester.tt.Texts())
	}

	// Removing the chip deletes the upload that was never sent.
	uploads := filepath.Join(cfg.Home, "uploads")
	entries, err := os.ReadDir(uploads)
	if err != nil || len(entries) != 1 {
		t.Fatalf("uploads = %v, %v", entries, err)
	}
	session.removeAttachment(session.attachments[0].ID)
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		remaining, err := os.ReadDir(uploads)
		return err == nil && len(remaining) == 0 && len(session.attachments) == 0
	}, "the upload to be deleted")

	// Dropping again and sending carries the file with the message.
	session.attachPaths([]string{path})
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.attachments) == 1
	}, "the dropped file to arrive again")
	ids := make([]string, 0, len(session.attachments))
	for _, attachment := range session.attachments {
		ids = append(ids, attachment.ID)
	}
	session.sendPrompt(thread.ID, "Read this", session.attachments)
	_ = ids
	var sent proto.Message
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		detail, err := srv.Service.Detail(ctx, thread.ID)
		if err != nil {
			return false
		}
		for _, m := range detail.Messages {
			if m.Role == proto.RoleUser && len(m.Attachments) == 1 {
				sent = m
				return true
			}
		}
		return false
	}, "the message with the attachment to land")
	if sent.Attachments[0].Name != "note.txt" || sent.Content != "Read this" {
		t.Fatalf("message = %#v", sent)
	}
	if _, err := os.Stat(srv.Service.UploadPath(sent.Attachments[0].ID)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	_ = strings.TrimSpace
	_ = data
}

// TestUIRestoreRun restores a run's checkpoint through the run history's
// dialog, and the tracked files go back.
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

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	tester.wait(t, 15*time.Second, func() bool {
		return len(session.threads) == 1
	}, "the thread list to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID && len(session.detail.Runs) == 1
	}, "the detail with its run to load")

	// The restore dialog asks, and confirming puts the files back.
	session.pendingRestore = runID
	session.showRestore = true
	tester.pump()
	if !tester.tt.HasText("Restore this run?") {
		t.Fatalf("the restore dialog did not open: %v", tester.tt.Texts())
	}
	session.restoreRun()
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		after, err := os.ReadFile(filepath.Join(repo, "README.md"))
		return err == nil && string(after) == string(before)
	}, "the files to go back")
}

// TestUIConnectWithPairLink connects from a pasted pairing link: the
// token field takes the whole t3go:// link and the connect action keeps
// the environment.
func TestUIConnectWithPairLink(t *testing.T) {
	_, ts, cfg := startEnv(t)
	link := "t3go://pair?url=" + ts.URL + "&token=" + cfg.Token

	entries := map[string]string{"token": link}
	post := &testPost{}
	mgr := newManagerPost(nil, post.Update, memoryStorage(entries))
	mgr.start()
	tt := ui.NewTester(mgr.view, 1100, 760)
	tester := &Tester{tt: tt, post: post, manager: mgr}
	session := mgr.active

	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session != nil && strings.Contains(session.token, "t3go://pair")
	}, "the connect screen to load the link")
	tester.pump()
	if !tester.tt.HasText("Connect") {
		t.Fatalf("the connect screen is missing its button: %v", tester.tt.Texts())
	}

	// The connect action parses the link and connects.
	session.connect(session.serverURL, session.token)
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.connected
	}, "the environment to connect")
	if session.serverURL != ts.URL {
		t.Fatalf("serverURL = %q, want %q", session.serverURL, ts.URL)
	}
	if v, _ := tester.manager.store.GetItem("serverUrl"); v != ts.URL {
		t.Fatalf("the storage did not keep the address: %q", v)
	}
}
