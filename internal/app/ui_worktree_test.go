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

// TestUICreateThreadInWorktree asks for a worktree from a selected
// branch and checks the environment made the checkout.
func TestUICreateThreadInWorktree(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	repo := gitRepo(t)
	cmd := exec.Command("git", "branch", "feature/ui")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.projects) == 1
	}, "the projects to arrive")

	// The new-thread page picks the worktree and its base branch.
	session.showNewThread = true
	session.newThreadProj = project.ID
	session.loadNewThreadBranches(project.ID)
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.newThreadBranches) >= 2
	}, "the branches to arrive")
	session.newThreadWT = true
	session.newThreadBase = "feature/ui"
	session.newThreadPrompt = "Worktree thread"
	session.newThreadProv = "claude"
	session.newThreadModel = "test-model"
	session.createPreparedThread(true)

	created := ""
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		threads, err := srv.Store.Threads(ctx, project.ID, false)
		if err != nil || len(threads) != 1 {
			return false
		}
		created = threads[0].ID
		// The store commits before the websocket update reaches the UI.
		for _, thread := range session.threads {
			if thread.ID == created {
				return true
			}
		}
		return false
	}, "the thread to land")
	var made proto.Thread
	for _, th := range session.threads {
		if th.ID == created {
			made = th
		}
	}
	if made.Branch != "t3go/worktree-thread" {
		t.Fatalf("branch = %q", made.Branch)
	}
	if made.WorktreePath == "" {
		t.Fatal("thread has no worktree path")
	}
	if _, err := os.Stat(made.WorktreePath); err != nil {
		t.Fatalf("worktree: %v", err)
	}
}

// TestUICreateThreadInCurrentCheckoutSelectsBranch creates the thread in
// the project's checkout at a chosen branch.
func TestUICreateThreadInCurrentCheckoutSelectsBranch(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	cmd := exec.Command("git", "branch", "feature/current")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.projects) == 1
	}, "the projects to arrive")

	session.showNewThread = true
	session.newThreadProj = project.ID
	session.loadNewThreadBranches(project.ID)
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.newThreadBranches) >= 2
	}, "the branches to arrive")
	session.newThreadWT = false
	session.newThreadBase = "feature/current"
	session.newThreadPrompt = "Current checkout thread"
	session.newThreadProv = "claude"
	session.newThreadModel = "test-model"
	session.createPreparedThread(true)

	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		threads, err := srv.Store.Threads(ctx, project.ID, false)
		return err == nil && len(threads) == 1
	}, "the thread to land")
	threads, err := srv.Store.Threads(ctx, project.ID, false)
	if err != nil || len(threads) != 1 {
		t.Fatalf("threads = %#v, %v", threads, err)
	}
	if threads[0].Branch != "feature/current" || threads[0].WorktreePath != "" {
		t.Fatalf("thread checkout = %#v", threads[0])
	}
	if branch := git.Status(ctx, repo).Branch; branch != "feature/current" {
		t.Fatalf("current checkout branch = %q", branch)
	}
}

// TestUIMCPPromptArguments fills the arguments of an MCP prompt command.
func TestUIMCPPromptArguments(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	mcp := filepath.Join(repo, "mcp.sh")
	script := `#!/bin/sh
while IFS= read -r line; do
case "$line" in
*'"method":"initialize"'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{}},"serverInfo":{"name":"fake","version":"1"}}}';;
*'"method":"prompts/list"'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"prompts":[{"name":"summarize","description":"Summarize with MCP","arguments":[{"name":"topic","description":"What to summarize","required":true},{"name":"style"}]}]}}';;
esac
done
`
	if err := os.WriteFile(mcp, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".mcp.json"),
		[]byte(`{"mcpServers":{"fake":{"command":"`+mcp+`"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Prompts", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
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

	// The MCP prompt arrives among the agent commands.
	session.ensureAgentCommands()
	var summarize proto.AgentCommand
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		for _, command := range session.agentCommands {
			if strings.Contains(command.Name, "summarize") {
				summarize = command
			}
		}
		return summarize.Name != ""
	}, "the MCP prompt to arrive")

	// Picking it asks for the prompt's arguments.
	session.pickAgentCommand(summarize)
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.showPromptArgs && len(session.promptArgsDefs) == 2
	}, "the argument dialog to open")
	tester.pump()
	if !tester.tt.HasText(session.promptName) {
		t.Fatalf("the dialog does not name the command: %v", tester.tt.Texts())
	}

	// Filling the arguments inserts the command with them.
	state := session.promptArgValues
	state.Values[0] = "release notes"
	state.Values[1] = "brief"
	session.promptArgValues = state
	tester.pump()
	session.submitPromptArgs()
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !session.showPromptArgs && strings.Contains(session.draft, "release notes")
	}, "the command to fill the draft")
	if !strings.Contains(session.draft, "brief") {
		t.Fatalf("draft = %q", session.draft)
	}
}
