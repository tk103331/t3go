package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-qm", "initial")
	return dir
}

func TestStatusCheckpointAndDiff(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)

	if !IsRepo(ctx, dir) {
		t.Fatal("not a repo")
	}
	st := Status(ctx, dir)
	if !st.Repo || st.Branch == "" || st.Dirty {
		t.Fatalf("status = %#v", st)
	}

	// A clean checkpoint points at HEAD.
	ref, err := Checkpoint(ctx, dir, "thr_1", "run_1")
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if !strings.HasPrefix(ref, "refs/t3go/checkpoints/thr_1/run_1") {
		t.Fatalf("ref = %q", ref)
	}

	// Modify a tracked file: the checkpoint stash includes it, and a later
	// change shows in the diff against that checkpoint.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref2, err := Checkpoint(ctx, dir, "thr_1", "run_2")
	if err != nil {
		t.Fatalf("checkpoint 2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := Diff(ctx, dir, ref2)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(diff, "+three") || strings.Contains(diff, "+two") {
		t.Fatalf("diff = %q", diff)
	}
	if stat := DiffStat(ctx, dir, ref2); !strings.Contains(stat, "a.txt") {
		t.Fatalf("stat = %q", stat)
	}
	if st := Status(ctx, dir); !st.Dirty {
		t.Fatal("dirty work tree reported clean")
	}
}

func TestWorktree(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)
	path := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(ctx, dir, path, "t3go/test", ""); err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(path, "a.txt")); err != nil || fi.IsDir() {
		t.Fatalf("worktree file: %v", err)
	}
	if branches, err := Branches(ctx, dir); err != nil || len(branches) != 2 {
		t.Fatalf("branches = %#v, %v", branches, err)
	}
	if err := RemoveWorktree(ctx, dir, path); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
}
