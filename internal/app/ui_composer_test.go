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

// TestUIComposerMentionsAndCommands covers @ file mentions and slash
// commands above the composer.
func TestUIComposerMentionsAndCommands(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claude", "commands", "review.md"),
		[]byte("---\nname: review\ndescription: Review the change\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Mentions", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
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
	}, "the detail to load")

	// Typing an @ suggestion loads the file index and shows the files.
	session.ensureFileIndex()
	session.setDraft("Look at @READ")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		if len(session.fileIndex) == 0 {
			return false
		}
		tester.pump()
		return tester.tt.HasText("README.md")
	}, "the mention suggestions to arrive")
	tester.pump()

	// Picking one completes the mention, and its chip can go away.
	session.insertMention("README.md")
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.draft == "Look at @README.md "
	}, "the mention to complete")
	tester.pump()
	if !tester.tt.HasText("README.md") || !strings.Contains(strings.Join(tester.tt.Texts(), " "), "Remove") {
		t.Fatalf("the mention chip is missing: %v", tester.tt.Texts())
	}
	session.removeMention("README.md")
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !strings.Contains(session.draft, "README.md")
	}, "the mention to leave the draft")

	// A slash after nothing shows the workspace's skills first.
	session.ensureAgentCommands()
	session.setDraft("/rev")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.agentCommands) > 0
	}, "the agent commands to arrive")
	tester.pump()
	if !tester.tt.HasText("/review") {
		t.Fatalf("the suggestions do not list the skill: %v", tester.tt.Texts())
	}

	// Picking it fills the draft with the command.
	var review proto.AgentCommand
	for _, command := range session.agentCommands {
		if command.Name == "review" {
			review = command
		}
	}
	if review.Name == "" {
		t.Fatalf("the review skill is not in the commands")
	}
	session.pickAgentCommand(review)
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.draft == "/review "
	}, "the command to fill the draft")

	// The app's own slash commands are there too, and run.
	session.setDraft("/sett")
	tester.pump()
	if !tester.tt.HasText("/settings") {
		t.Fatalf("the suggestions do not list the app command: %v", tester.tt.Texts())
	}
	session.setDraft("")
	session.openSettings()
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.showSettings
	}, "the settings to open")
	session.showSettings = false
}

// TestUIGitDialog commits through the Changes dialog and switches the
// checkout's branch.
func TestUIGitDialog(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	repo := gitRepo(t)
	project, err := srv.Service.CreateProject(ctx, "Repo", repo)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Git thread", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "branch", "feature")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
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

	// The Changes dialog commits the working tree.
	session.showDiff = true
	session.loadDiff(thread.ID, "")
	session.gitMessage = "Commit from the UI"
	tester.pump()
	if !tester.tt.HasText("Changes") {
		t.Fatalf("the dialog is not open: %v", tester.tt.Texts())
	}
	session.runGitAction(proto.GitCommit)
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		cmd := exec.Command("git", "log", "-1", "--pretty=%s")
		cmd.Dir = repo
		out, err := cmd.Output()
		return err == nil && strings.TrimSpace(string(out)) == "Commit from the UI" &&
			!session.gitBusy && strings.Contains(session.gitNote, "Committed")
	}, "the commit to land")
	if st := git.Status(ctx, repo); st.Dirty {
		t.Fatalf("checkout is still dirty after commit: %#v", st)
	}
	tester.pump()
	if !tester.tt.HasText("Committed") {
		t.Fatalf("the dialog does not say it committed: %v", tester.tt.Texts())
	}

	// The branch picker switches the same checkout.
	session.showBranches = true
	session.loadBranches(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.branches) >= 2
	}, "the branches to arrive")
	session.switchBranch("feature")
	tester.wait(t, 10*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return git.Status(ctx, repo).Branch == "feature"
	}, "the branch to switch")
	tester.pump()
	if !tester.tt.HasText("feature") {
		t.Fatalf("the dialog does not show the branch: %v", tester.tt.Texts())
	}
}
