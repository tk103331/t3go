package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// gitRepo makes a Git repository with one commit, the way the tests seed
// a project with history.
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

// TestUIRunHistory shows a finished run in the run-history dialog and
// opens its diff.
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

	// The run history lists the finished run with its diff.
	session.openRuns()
	tester.wait(t, 5*time.Second, func() bool {
		return session.showRuns
	}, "the run history to open")
	session.openRunDiff(runID)
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.showDiff && strings.Contains(session.diffText, "changed by the agent")
	}, "the run's diff to load")
	tester.pump()
	// The diff viewer is a pair of text fields: its lines live in their
	// accessibility values, not in the plain texts.
	shown := false
	for _, a := range tester.tt.AccessAll() {
		if strings.Contains(a.Value, "changed by the agent") {
			shown = true
		}
	}
	if !shown {
		var values []string
		for _, a := range tester.tt.AccessAll() {
			if a.Value != "" {
				values = append(values, a.Value)
			}
		}
		t.Fatalf("the diff dialog does not show the change: %q", values)
	}
}
