package server

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk/t3go/internal/proto"
)

// TestCommitDiffEndpointReadsOneCommit pins the endpoint the git graph
// opens a commit with, and that it refuses anything but a hash.
func TestCommitDiffEndpointReadsOneCommit(t *testing.T) {
	env := newTestEnv(t)
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt")
	git("commit", "-qm", "initial")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "second")
	hash := git("rev-parse", "HEAD")

	var project proto.Project
	if code := env.request(t, http.MethodPost, "/api/v1/projects",
		map[string]string{"name": "Repo", "path": repo}, env.token, &project); code != http.StatusCreated {
		t.Fatalf("create project = %d", code)
	}
	var thread proto.Thread
	if code := env.request(t, http.MethodPost, "/api/v1/threads", map[string]any{
		"projectId": project.ID, "title": "Commits", "provider": "claude", "model": "test-model",
		"permission": "full-access"}, env.token, &thread); code != http.StatusCreated {
		t.Fatalf("create thread = %d", code)
	}

	var out struct {
		Patch string `json:"patch"`
	}
	if code := env.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/commits/"+hash, nil, env.token, &out); code != http.StatusOK {
		t.Fatalf("commit = %d", code)
	}
	if !strings.Contains(out.Patch, "diff --git a/a.txt b/a.txt") || !strings.Contains(out.Patch, "+two") {
		t.Fatalf("patch = %q", out.Patch)
	}
	for _, bad := range []string{"HEAD~1", "--all"} {
		if code := env.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/commits/"+bad, nil, env.token, nil); code != http.StatusBadRequest {
			t.Fatalf("commit %q = %d, want 400", bad, code)
		}
	}
	if code := env.request(t, http.MethodGet, "/api/v1/threads/"+thread.ID+"/commits/deadbeef", nil, env.token, nil); code != http.StatusNotFound {
		t.Fatalf("an unknown hash = %d", code)
	}
}
