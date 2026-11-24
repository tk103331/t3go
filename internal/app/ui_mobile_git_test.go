package app

import (
	"context"
	"testing"
	"time"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUIMobileGitControlsAndSurfaces walks the phone's entries to the
// checkout: the git controls that commit, and the surfaces that show the
// files and the shell.
func TestUIMobileGitControlsAndSurfaces(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Mobile git", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	tester, session := startAppAt(t, ts, cfg, 390, 844, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID
	}, "the thread to load")
	tester.pump()

	// The phone's header carries the entries to the checkout.
	for _, label := range []string{i18n.T("Files"), i18n.T("Git controls"), i18n.T("Terminal")} {
		if _, ok := tester.tt.Access(label); !ok {
			t.Fatalf("the phone's header has no %q entry: %v", label, tester.tt.Texts())
		}
	}

	// The git controls open the commit sheet.
	if err := tester.tt.Click(i18n.T("Git controls")); err != nil {
		t.Fatalf("open the git controls: %v", err)
	}
	if err := tester.tt.ChooseMenuItem(i18n.T("Commit & push")); err != nil {
		t.Fatalf("choose the commit: %v", err)
	}
	tester.pump()
	if !session.mobileCommitOpen {
		t.Fatal("the commit sheet did not open")
	}
	if !tester.tt.HasText(i18n.T("Changed files")) {
		t.Fatalf("the commit sheet does not list the changed files: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText(i18n.T("Commit message")) {
		t.Fatalf("the commit sheet has no message field: %v", tester.tt.Texts())
	}
	// The sheet closes again.
	if err := tester.tt.Click(i18n.T("Cancel")); err != nil {
		t.Fatalf("close the commit sheet: %v", err)
	}
	tester.pump()
	if session.mobileCommitOpen {
		t.Fatal("the commit sheet stayed open")
	}

	// The files surface opens over the conversation.
	if err := tester.tt.Click(i18n.T("Files")); err != nil {
		t.Fatalf("open the files: %v", err)
	}
	tester.pump()
	if session.mobileSurface != "files" {
		t.Fatalf("the phone opened the %q surface, want files", session.mobileSurface)
	}
	if !tester.tt.HasText(i18n.T("Back to the thread")) {
		t.Fatalf("the files surface has no way back: %v", tester.tt.Texts())
	}
	if err := tester.tt.Click(i18n.T("Back to the thread")); err != nil {
		t.Fatalf("leave the files surface: %v", err)
	}
	tester.pump()
	if session.mobileSurface != "" {
		t.Fatal("the files surface stayed open")
	}

	// The terminal opens as a surface too.
	if err := tester.tt.Click(i18n.T("Terminal")); err != nil {
		t.Fatalf("open the terminal: %v", err)
	}
	tester.pump()
	if session.mobileSurface != "terminal" {
		t.Fatalf("the phone opened the %q surface, want terminal", session.mobileSurface)
	}
	if err := tester.tt.Click(i18n.T("Back to the thread")); err != nil {
		t.Fatalf("leave the terminal surface: %v", err)
	}
	tester.pump()
	if session.mobileSurface != "" {
		t.Fatal("the terminal surface stayed open")
	}
}
