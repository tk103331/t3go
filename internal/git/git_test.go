package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk/t3go/internal/proto"
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

// fakeGH puts a gh that reports and creates pull request 7 on PATH.
func fakeGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "pr-open")
	script := `#!/bin/sh
case "$1 $2" in
"pr view")
  if [ -f "$PR_STATE" ]; then
    echo '{"url":"https://github.com/acme/repo/pull/7","number":7,"title":"Add feature","baseRefName":"main","headRefName":"feature","state":"OPEN"}'
  else
    echo "no pull requests found" >&2
    exit 1
  fi
  ;;
"pr create")
  touch "$PR_STATE"
  echo "https://github.com/acme/repo/pull/7"
  ;;
*)
  echo "unexpected gh $*" >&2
  exit 2
  ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PR_STATE", state)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return state
}

func TestCreatePullRequest(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)
	fakeGH(t)

	pr, err := CreatePullRequest(ctx, dir, "main")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if pr.URL != "https://github.com/acme/repo/pull/7" || pr.Number != 7 || pr.Existed {
		t.Fatalf("pr = %#v", pr)
	}
	if pr.Title != "Add feature" || pr.BaseBranch != "main" || pr.HeadBranch != "feature" {
		t.Fatalf("pr details = %#v", pr)
	}

	// A second call finds the open pull request instead of making another.
	again, err := CreatePullRequest(ctx, dir, "main")
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !again.Existed || again.Number != 7 {
		t.Fatalf("second pr = %#v", again)
	}
}

func TestCreatePullRequestNeedsGH(t *testing.T) {
	dir := initRepo(t)
	t.Setenv("PATH", t.TempDir())
	if _, err := CreatePullRequest(context.Background(), dir, ""); !errors.Is(err, ErrGHMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestListRefs(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)
	gitRun(t, dir, "branch", "feature")
	path := filepath.Join(t.TempDir(), "wt")
	if err := CreateWorktree(ctx, dir, path, "t3go/wt", ""); err != nil {
		t.Fatalf("worktree: %v", err)
	}
	refs, err := ListRefs(ctx, dir, false)
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	var current, feature, wt *proto.Branch
	for i := range refs {
		switch refs[i].Name {
		case Status(ctx, dir).Branch:
			current = &refs[i]
		case "feature":
			feature = &refs[i]
		case "t3go/wt":
			wt = &refs[i]
		}
	}
	if current == nil || !current.Current || !current.Default {
		t.Fatalf("current/default refs = %#v", refs)
	}
	if feature == nil || feature.Current {
		t.Fatalf("feature ref = %#v", feature)
	}
	if wt == nil {
		t.Fatalf("worktree branch missing: %#v", refs)
	}
	want, _ := filepath.EvalSymlinks(path)
	got, _ := filepath.EvalSymlinks(wt.WorktreePath)
	if got != want {
		t.Fatalf("worktree ref = %#v, want %s", wt, want)
	}
	if refs[0].Name != current.Name {
		t.Fatalf("current branch is not first: %#v", refs)
	}
	if err := RemoveWorktree(ctx, dir, path); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}
}

func TestReviewData(t *testing.T) {
	ctx := context.Background()
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-qm", "second")

	commits, truncated, err := BranchCommits(ctx, dir, "HEAD~1", 10)
	if err != nil {
		t.Fatalf("commits: %v", err)
	}
	if truncated || len(commits) != 1 || commits[0].Subject != "second" || commits[0].CommittedAt == "" {
		t.Fatalf("commits = %#v, truncated = %v", commits, truncated)
	}

	// A tracked and an untracked change share one working-tree preview.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stats, err := ReviewDiff(ctx, dir, "HEAD", "", nil, false, false, false)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats.Files) != 2 || stats.Patch != "" {
		t.Fatalf("stats = %#v", stats)
	}
	diff, err := ReviewDiff(ctx, dir, "HEAD", "", nil, false, false, true)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(diff.Patch, "+three") || !strings.Contains(diff.Patch, "b.txt") {
		t.Fatalf("patch = %q", diff.Patch)
	}
	if DiffHash(diff.Patch, diff.Files) == "" {
		t.Fatal("no diff hash")
	}

	old, current, err := FileContents(ctx, dir, "working-tree", "HEAD", "", "a.txt", "a.txt")
	if err != nil || old != "two\n" || current != "three\n" {
		t.Fatalf("contents = %q, %q, %v", old, current, err)
	}
	oldNew, newNew, err := FileContents(ctx, dir, "working-tree", "HEAD", "", "b.txt", "b.txt")
	if err != nil || oldNew != "" || newNew != "new\n" {
		t.Fatalf("new file contents = %q, %q, %v", oldNew, newNew, err)
	}

	// A commit preview compares against its parent.
	sha := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD"))
	subject, parent, err := CommitMeta(ctx, dir, sha)
	if err != nil || subject != "second" || parent == "" {
		t.Fatalf("meta = %q, %q, %v", subject, parent, err)
	}
	commitDiff, err := ReviewDiff(ctx, dir, parent, sha, nil, false, false, true)
	if err != nil || !strings.Contains(commitDiff.Patch, "+two") {
		t.Fatalf("commit diff = %q, %v", commitDiff.Patch, err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
