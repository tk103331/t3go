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
	"github.com/tk/t3go/internal/provider"
)

// sideRunner answers every turn with one text.
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

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "the threads to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID
	}, "the thread's detail to load")

	// The side chat opens as a surface of the thread.
	session.showSurface("sidechat")
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.sideChat.ID != "" && !session.sideBusy
	}, "the side chat to start")

	// Asking a question streams the fake provider's reply.
	session.sideDraft = "quick question"
	tester.pump()
	session.sendSideChat()
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		replied := false
		for _, m := range session.sideMessages {
			if strings.Contains(m.Content, "side reply") {
				replied = true
			}
		}
		return replied
	}, "the side chat to reply")
	tester.pump()
	if !tester.tt.HasText("side reply") {
		t.Fatalf("the surface does not show the reply: %v", tester.tt.Texts())
	}

	// The model and the permission persist on the side chat's thread.
	session.setSideModel(proto.Model{ID: "side-fake-2", Provider: "side-fake"})
	tester.wait(t, 10*time.Second, func() bool {
		side, ok, err := srv.Store.SideChat(ctx, thread.ID)
		return err == nil && ok && side.Model == "side-fake-2"
	}, "the side chat's model to persist")
	session.setSidePermission(proto.PermissionSupervised)
	tester.wait(t, 10*time.Second, func() bool {
		side, ok, err := srv.Store.SideChat(ctx, thread.ID)
		return err == nil && ok && side.Permission == proto.PermissionSupervised
	}, "the side chat's permission to persist")

	// A file attached to the side chat goes with the next question.
	path := filepath.Join(t.TempDir(), "side-note.txt")
	if err := os.WriteFile(path, []byte("attached"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session.attachSideFiles([]pickedFile{{Name: "side-note.txt", MIME: "text/plain", Size: int64(len(data)), Data: data}})
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.sideAttachments) == 1
	}, "the attachment to arrive")
	session.sideDraft = "with a file"
	tester.pump()
	session.sendSideChat()
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.sideAttachments) == 0
	}, "the question with the file to send")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		replied := false
		for _, m := range session.sideMessages {
			if strings.Contains(m.Content, "side reply") {
				replied = true
			}
		}
		return replied
	}, "the second question to reply")

	// The hidden thread stays out of the sidebar.
	tester.pump()
	for _, row := range session.mobileThreads() {
		if row.thread.ID == session.sideChat.ID {
			t.Fatalf("the side chat is listed in the inbox: %q", row.thread.Title)
		}
	}
}
