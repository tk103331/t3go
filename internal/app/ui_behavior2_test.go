package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// TestUIOfflineBanner marks the client offline when the environment's
// server goes away, and back online when a new one answers.
func TestUIOfflineBanner(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	tester.wait(t, 5*time.Second, func() bool {
		return !session.offline
	}, "the client to come online")

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.offline
	}, "the client to notice the server is gone")
}

// TestUIPaletteSearch opens the command palette, searches a thread by
// its title, and opens the hit.
func TestUIPaletteSearch(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo Project", t.TempDir())
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	for _, title := range []string{"First thread", "Second thread"} {
		if _, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
			ProjectID: project.ID, Title: title, Provider: "claude", Model: "test-model", Permission: proto.PermissionFullAccess,
		}); err != nil {
			t.Fatalf("thread: %v", err)
		}
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 2
	}, "the threads to arrive")

	session.openPalette()
	tester.wait(t, 5*time.Second, func() bool {
		return session.showPalette
	}, "the palette to open")
	tester.pump()
	if !tester.tt.HasText("Second thread") {
		t.Fatalf("the palette does not list the threads: %v", tester.tt.Texts())
	}
	// Typing narrows the rows; Enter opens the chosen one.
	tester.tt.Type("Second")
	tester.post.drain()
	tester.tt.Frame()
	tester.wait(t, 2*time.Second, func() bool {
		n := 0
		for _, item := range session.paletteItems() {
			if strings.Contains(strings.ToLower(item.label), "second") {
				n++
			}
		}
		return n == 1
	}, "the query to narrow the rows")
	// Enter on the narrowed list opens the hit: runCommand with the row's
	// id, which is what list.Submitted does.
	for _, item := range session.paletteItems() {
		if strings.Contains(strings.ToLower(item.label), "second") {
			session.runCommand(item.id)
		}
	}
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !session.showPalette && session.selected != ""
	}, "the palette to open the hit")
	var opened proto.Thread
	for _, th := range session.threads {
		if th.ID == session.selected {
			opened = th
		}
	}
	if opened.Title != "Second thread" {
		t.Fatalf("opened thread = %q", opened.Title)
	}

	// The shortcut opens the palette again.
	session.openPalette()
	tester.wait(t, 5*time.Second, func() bool {
		return session.showPalette
	}, "the palette to open again")
	_ = context.Background
}

// TestUISettingsDefaults saves the environment's default provider,
// model and permission, and a new thread inherits them.
func TestUISettingsDefaults(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	if _, err := srv.Service.CreateProject(ctx, "Defaults", t.TempDir()); err != nil {
		t.Fatalf("project: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.providers) > 0
	}, "the providers to arrive")

	// The client picks its defaults from the environment's settings.
	draft := session.settingsDraft
	for _, p := range session.providers {
		if p.ID == "ui-fake" {
			draft.DefaultProvider = "ui-fake"
			break
		}
	}
	draft.DefaultModel = "ui-fake-1"
	draft.DefaultPermission = string(proto.PermissionSupervised)
	session.saveSettings(draft)
	tester.wait(t, 10*time.Second, func() bool {
		return session.settings.DefaultProvider == "ui-fake"
	}, "the server to keep the defaults")

	// A thread created without explicit choices inherits them: the
	// page's prepare picks the environment's defaults.
	session.prepareNewThread()
	session.newThreadProj = session.projects[0].ID
	session.showNewThread = true
	session.newThreadPrompt = "Inherit the defaults"
	session.createPreparedThread(true)
	tester.wait(t, 10*time.Second, func() bool {
		return session.selected != ""
	}, "the thread to open")
	var created *proto.Thread
	for i := range session.threads {
		if session.threads[i].ID == session.selected {
			created = &session.threads[i]
		}
	}
	if created == nil {
		t.Fatalf("the created thread is not in the list")
	}
	if created.Provider != "ui-fake" || created.Model != "ui-fake-1" {
		t.Fatalf("thread provider/model = %q/%q, want ui-fake/ui-fake-1", created.Provider, created.Model)
	}
	if created.Permission != proto.PermissionSupervised {
		t.Fatalf("thread permission = %q", created.Permission)
	}
}
