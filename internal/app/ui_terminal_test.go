package app

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUITerminalExitedShell opens the thread's terminal, ends the shell,
// and restarts it from the surface's banner.
func TestUITerminalExitedShell(t *testing.T) {
	if err := terminal.Load(); err != nil {
		t.Skipf("terminal library unavailable: %v", err)
	}
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Repo", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Shell", Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
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

	// The terminal surface opens the shell and attaches to it.
	session.showSurface("terminal")
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.terminalSession.ID != "" && !session.terminalBusy
	}, "the terminal session to open")
	attached := false
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		term := session.terminalHub.get(session.terminalSession.ID)
		attached = term != nil
		return attached
	}, "the terminal to attach")

	// Ending the shell shows the restart banner.
	tester.wait(t, 15*time.Second, func() bool {
		list := srv.Terminals.List(thread.ID)
		if len(list) == 0 {
			return false
		}
		list[0].Write([]byte("exit\n"))
		return true
	}, "the shell to end")
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.terminalHub.exited(session.terminalSession.ID)
	}, "the exit to show")

	// Restarting brings a fresh shell.
	session.restartTerminal()
	tester.wait(t, 20*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.terminalSession.ID != "" && !session.terminalSession.Exited && !session.terminalBusy
	}, "the shell to restart")
}
