package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUIRightPanelFiles walks the files surface: the tree, the previews
// of code, Markdown and an image, and the way back.
func TestUIRightPanelFiles(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "app.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Preview title\n"), 0o644); err != nil {
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

	// The files surface lists the checkout.
	session.showSurface("files")
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		found := false
		for _, e := range session.filesEntries {
			if e.Name == "src" {
				found = true
			}
		}
		return found
	}, "the file tree to arrive")
	tester.pump()
	if !tester.tt.HasText("README.md") {
		t.Fatalf("the tree does not list the checkout: %v", tester.tt.Texts())
	}

	// A source file previews as code.
	session.openFile("src/app.go")
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.fileSelected == "src/app.go" && strings.Contains(session.file.Content, "package main")
	}, "the source file to load")
	tester.pump()
	if !tester.accessValueHas("package main") {
		t.Fatalf("the code preview does not show the source")
	}

	// Back, then a Markdown file previews rendered.
	session.fileSelected, session.file = "", proto.WorkspaceFile{}
	session.openFile("README.md")
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.fileSelected == "README.md" && strings.Contains(session.file.Content, "# Preview title")
	}, "the Markdown file to load")
	tester.pump()
	if !tester.tt.HasText("Preview title") {
		t.Fatalf("the Markdown preview does not show the heading: %v", tester.tt.Texts())
	}

	// Back, then an image previews as one.
	session.fileSelected, session.file = "", proto.WorkspaceFile{}
	session.openFile("pic.png")
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.fileSelected == "pic.png"
	}, "the image to load")

	// The Git graph lists the repository's commits.
	session.showSurface("graph")
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.graph) > 0
	}, "the commit graph to arrive")
	tester.pump()
	if !tester.tt.HasText("initial") {
		t.Fatalf("the graph does not list the commit: %v", tester.tt.Texts())
	}

	// The working diff lists the changed file with stage and discard.
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
	// The diff surface lists the changed files and stages one.
	session.showSurface("diff")
	session.loadWorkingDiff(thread.ID)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tester.post.drain()
		tester.tt.Frame()
		if len(session.workingFiles) == 1 {
			break
		}
		if session.rightErr != "" {
			t.Fatalf("working diff error: %v", session.rightErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(session.workingFiles) == 0 {
		t.Fatalf("working files = %#v patch=%q rightErr=%q", session.workingFiles, session.workingDiff, session.rightErr)
	}
	found := false
	for _, file := range session.workingFiles {
		if file.Path == "README.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the working diff misses README.md: %#v", session.workingFiles)
	}
	tester.pump()
	if !tester.tt.HasText("README.md") {
		t.Fatalf("the diff surface does not list the file: %v", tester.tt.Texts())
	}
	session.stagePath("README.md", true)
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return git.StagedPaths(ctx, repo)["README.md"]
	}, "the file to stage")
}

// TestUIPullRequestSurface opens the pull request surface and shows the
// branch's state.
func TestUIPullRequestSurface(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Plain", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "PR thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
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

	// Without gh and a PR, the surface offers to create one.
	session.showSurface("pr")
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.surface == "pr"
	}, "the pull request surface to open")
	tester.pump()
	if !tester.tt.HasText("No open pull request") {
		t.Fatalf("the surface does not offer a pull request: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText("Create PR") {
		t.Fatalf("the surface has no create action: %v", tester.tt.Texts())
	}
}
