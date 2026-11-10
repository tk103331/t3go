package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tk/t3go/internal/proto"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitRepoWithRemote makes a checkout on main with origin pointing at a
// bare repository.
func gitRepoWithRemote(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "a.txt")
	runGit(t, repo, "commit", "-qm", "initial")

	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, filepath.Dir(remote), "init", "-q", "--bare", remote)
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-qu", "origin", "main")
	return repo
}

func fakeGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "pr-open")
	script := `#!/bin/sh
case "$1 $2" in
"pr view")
  if [ -f "$PR_STATE" ]; then
    echo '{"url":"https://github.com/acme/repo/pull/7","number":7,"title":"Add feature","baseRefName":"main","headRefName":"main","state":"OPEN"}'
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
}

func TestServiceGitAction(t *testing.T) {
	ctx := context.Background()
	repo := gitRepoWithRemote(t)
	svc := newTestService(t, &fakeRunner{})
	project, err := svc.CreateProject(ctx, "repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Title: "Git",
		Provider: "fake", Model: "fake-1", Permission: proto.PermissionFullAccess})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}

	// Commit records the change and leaves the checkout clean.
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := svc.GitAction(ctx, thread.ID, proto.GitActionInput{Action: proto.GitCommit, Message: "Second"})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if res.CommitSHA == "" || res.CommitSubject != "Second" || res.Status.Dirty {
		t.Fatalf("commit result = %#v", res)
	}
	if res.Action != proto.GitCommit || res.Pushed {
		t.Fatalf("commit did unrelated work: %#v", res)
	}

	// A clean commit reports no changes rather than failing.
	clean, err := svc.GitAction(ctx, thread.ID, proto.GitActionInput{Action: proto.GitCommit, Message: "Again"})
	if err != nil {
		t.Fatalf("clean commit: %v", err)
	}
	if !clean.NoChanges {
		t.Fatalf("clean commit = %#v", clean)
	}

	fakeGH(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushed, err := svc.GitAction(ctx, thread.ID, proto.GitActionInput{Action: proto.GitCommitPR, Message: "Third"})
	if err != nil {
		t.Fatalf("commit and PR: %v", err)
	}
	if !pushed.Pushed || pushed.PRURL != "https://github.com/acme/repo/pull/7" || pushed.PRNumber != 7 || pushed.PRExisted {
		t.Fatalf("commit and PR = %#v", pushed)
	}
	if pushed.Status.Ahead != 0 {
		t.Fatalf("branch still ahead after push: %#v", pushed.Status)
	}

	// A second PR action finds the open request instead of duplicating it.
	again, err := svc.GitAction(ctx, thread.ID, proto.GitActionInput{Action: proto.GitCreatePR})
	if err != nil {
		t.Fatalf("second PR: %v", err)
	}
	if !again.PRExisted || again.PRURL == "" {
		t.Fatalf("second PR = %#v", again)
	}
}

func TestServiceGitActionNeedsRepo(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, &fakeRunner{})
	project, err := svc.CreateProject(ctx, "plain", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := svc.CreateThread(ctx, CreateThreadInput{ProjectID: project.ID, Title: "Plain",
		Provider: "fake", Model: "fake-1", Permission: proto.PermissionFullAccess})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, err := svc.GitAction(ctx, thread.ID, proto.GitActionInput{Action: proto.GitCommit}); err == nil {
		t.Fatal("committed outside a repository")
	}
}
