package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// Double-clicking a commit in the graph opens a Diff tab with its patch,
// every changed file in the panel.
func TestUIGraphCommitDoubleClickDiff(t *testing.T) {
	repo := gitRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "second.txt"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "multi file change")

	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Graph", repo)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Graph thread", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 1200, 800, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "connect")
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool { return session.detail.Thread.ID == thread.ID }, "thread")
	session.showSurface("graph")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !session.rightBusy && len(session.graph) > 0
	}, "the graph to load")
	tester.pump()

	label := "multi file change"
	if err := tester.tt.Click(label); err != nil {
		t.Fatalf("graph row is not clickable: %v", err)
	}
	if err := tester.tt.Click(label); err != nil {
		t.Fatalf("graph row is not clickable twice: %v", err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.surface == "patch" && !session.patchDiffBusy &&
			strings.Contains(session.patchDiffPatch, "second.txt")
	}, "the commit's diff tab to open")
	if session.showDiff {
		t.Fatal("the commit opened the modal instead of the Diff tab")
	}
	if !strings.Contains(session.patchDiffPatch, "README.md") || !strings.Contains(session.patchDiffPatch, "second.txt") {
		t.Fatalf("the commit diff does not carry every file: %q", session.patchDiffPatch)
	}
	if session.patchDiffSubject != label {
		t.Fatalf("the diff tab names %q, want the commit", session.patchDiffSubject)
	}
	if want := i18n.T("Diff") + " · " + session.patchDiffShort; session.surfaceTitle("patch") != want {
		t.Fatalf("the tab is titled %q, want %q", session.surfaceTitle("patch"), want)
	}
	tester.pump()
	if !tester.accessValueHas("new file") || !tester.accessValueHas("changed") {
		t.Fatalf("the diff tab does not show the commit's changes: %v", session.patchDiffPatch)
	}
	if len(session.patchDiffs) == 0 || session.patchDiffs[0].Layout != editor.DiffSplit {
		t.Fatal("the Diff tab does not open side by side")
	}
	// The layout switch redraws every file's diff.
	if err := tester.tt.Click(i18n.T("Single column")); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 5*time.Second, func() bool {
		tester.tt.Frame()
		return !session.diffSplit && len(session.patchDiffs) > 0 &&
			session.patchDiffs[0].Layout == editor.DiffUnified
	}, "the diff to switch to one column")
	if err := tester.tt.Click(i18n.T("Two columns")); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 5*time.Second, func() bool {
		tester.tt.Frame()
		return session.diffSplit && len(session.patchDiffs) > 0 &&
			session.patchDiffs[0].Layout == editor.DiffSplit
	}, "the diff to switch back to two columns")
}
