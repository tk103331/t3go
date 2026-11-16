package main

import (
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/proto"
)

func TestUIDesktopInboxGroupsByStatus(t *testing.T) {
	fixture := newSettingsFixture(t, "Alpha")
	var manager *environmentManager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = newEnvironmentManager(cx)
		manager.start()
		return manager.view(cx)
	}, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(fixture.profile),
	}))
	app.WaitFor("id:thread-same-thread", 10*time.Second)

	app.Post(func() {
		session := manager.sessions[fixture.profile.ID]
		base := session.threads.GetUntracked()[0]
		now := time.Now()
		recent := base
		recent.ID, recent.Title, recent.UpdatedAt = "recent", "Recent thread", now.Add(-2*time.Minute)
		running := base
		running.ID, running.Title, running.Status, running.UpdatedAt = "running", "Working thread", proto.ThreadRunning, now.Add(-time.Minute)
		settled := base
		settled.ID, settled.Title, settled.Settled, settled.UpdatedAt = "settled", "Settled thread", true, now
		session.threads.Set([]proto.Thread{recent, running, settled})
	})

	app.WaitFor("id:thread-recent", 5*time.Second)
	app.WaitFor("id:thread-running", 5*time.Second)
	workingHeader := "id:sidebar-status-working-toggle"
	settledHeader := "id:sidebar-status-settled-toggle"
	app.WaitFor(workingHeader, 5*time.Second)
	app.WaitFor(settledHeader, 5*time.Second)
	app.WaitForGone("id:thread-settled", 5*time.Second)

	if mobileScreenFrame(app.Find("id:thread-recent")).Origin.Y >= mobileScreenFrame(app.Find("id:thread-running")).Origin.Y {
		t.Fatal("normal conversations are not above Working")
	}
	if mobileScreenFrame(app.Find("id:thread-running")).Origin.Y >= mobileScreenFrame(app.Find(settledHeader)).Origin.Y {
		t.Fatal("Working is not above Settled")
	}

	app.Press(settledHeader)
	app.WaitFor("id:thread-settled", 5*time.Second)
}

func TestUIDesktopInboxDoesNotGroupByEnvironment(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(alpha.profile, beta.profile),
	}))
	alphaRow := "id:thread-" + alpha.profile.ID + "/same-thread"
	betaRow := "id:thread-" + beta.profile.ID + "/same-thread"
	app.WaitFor(alphaRow, 10*time.Second)
	app.WaitFor(betaRow, 10*time.Second)

	if app.Exists("id:sidebar-environment-"+alpha.profile.ID) ||
		app.Exists("id:sidebar-environment-"+beta.profile.ID) {
		t.Fatal("the conversation list still has environment headings")
	}
	if !app.Exists("id:sidebar-status-recent >> "+alphaRow) ||
		!app.Exists("id:sidebar-status-recent >> "+betaRow) {
		t.Fatal("threads from different environments are not in the same status group")
	}
	for id, want := range map[string]string{
		alpha.profile.ID + "/same-thread": "Alpha",
		beta.profile.ID + "/same-thread":  "Beta",
	} {
		text := app.Find("id:thread-context-" + id).Text
		if !containsAll(text, want) {
			t.Fatalf("thread context %q does not name environment %q", text, want)
		}
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
