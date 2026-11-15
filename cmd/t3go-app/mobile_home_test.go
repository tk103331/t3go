package main

import (
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/proto"
)

func mobileScreenFrame(v *nsgotest.View) core.Bounds {
	frame := v.Frame
	for parent := v.Parent; parent != nil; parent = parent.Parent {
		frame.Origin.X += parent.Frame.Origin.X - parent.Offset.X
		frame.Origin.Y += parent.Frame.Origin.Y - parent.Offset.Y
	}
	return frame
}

func TestUIMobileUnifiedInbox(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	var manager *environmentManager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = newEnvironmentManager(cx)
		manager.start()
		return manager.view(cx)
	}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{
		OS: core.OSAndroid, Idiom: core.IdiomPhone, SafeArea: core.Edges{Top: 24, Bottom: 16},
	}), nsgotest.Storage(map[string]string{"environments:v1": workspaceRegistry(alpha.profile, beta.profile)}))
	alphaID, betaID := alpha.profile.ID+"/same-thread", beta.profile.ID+"/same-thread"
	app.WaitFor("id:thread-"+alphaID, 10*time.Second)
	app.WaitFor("id:thread-"+betaID, 10*time.Second)
	for id, want := range map[string]string{alphaID: "Alpha", betaID: "Beta"} {
		if got := app.Find("id:mobile-thread-environment-" + id).Text; got != want {
			t.Fatalf("thread machine = %q, want %q", got, want)
		}
	}
	app.Post(func() {
		b := manager.sessions[beta.profile.ID]
		rows := append([]proto.Thread(nil), b.threads.GetUntracked()...)
		rows[0].Status = proto.ThreadRunning
		b.threads.Set(rows)
	})
	app.Settle()
	app.WaitFor("id:mobile-group-working", 5*time.Second)
	if mobileScreenFrame(app.Find("id:thread-"+betaID)).Origin.Y >= mobileScreenFrame(app.Find("id:thread-"+alphaID)).Origin.Y {
		t.Fatal("working thread is not above settled threads")
	}
	app.WaitFor("text:Settled (1)", 5*time.Second)
	app.Press("id:mobile-group-settled")
	app.WaitForGone("id:thread-"+alphaID, 5*time.Second)
	app.WaitFor("id:thread-"+betaID, 5*time.Second)
	app.Press("id:mobile-group-settled")
	app.WaitFor("id:thread-"+alphaID, 5*time.Second)
	app.LongPress("id:thread-" + alphaID)
	app.WaitFor("id:pin-thread-"+alphaID, 5*time.Second)
	app.Press("id:pin-thread-" + alphaID)
	app.WaitFor("id:mobile-group-pinned", 5*time.Second)
	if mobileScreenFrame(app.Find("id:thread-"+alphaID)).Origin.Y >= mobileScreenFrame(app.Find("id:thread-"+betaID)).Origin.Y {
		t.Fatal("pinned thread is not above working threads")
	}
	// A smaller phone still has separate 44-point navigation targets.
	app.Resize(320, 700)
	app.Settle()
	for _, id := range []string{"new-project", "open-palette", "mobile-filter", "mobile-tab-threads",
		"mobile-tab-prs", "mobile-tab-usage", "settings", "new-thread"} {
		frame := mobileScreenFrame(app.Find("id:" + id))
		if frame.Size.Width < 44 || frame.Size.Height < 44 ||
			frame.Origin.X < 0 || frame.Origin.X+frame.Size.Width > 320 ||
			frame.Origin.Y < 24 || frame.Origin.Y+frame.Size.Height > 700-16 {
			t.Fatalf("%s is too small or outside the safe area: %+v", id, frame)
		}
	}
	openMobileProjects(app)
	app.WaitFor("id:project-"+beta.profile.ID+"/same-project", 5*time.Second)
	app.Press("id:mobile-tab-threads")
	app.WaitFor("id:mobile-group-working", 5*time.Second)
	app.Press("id:open-palette")
	app.WaitFor("id:palette-command", 5*time.Second)
	app.Type("label:Search commands", "Beta thread")
	app.WaitFor("id:palette-command >> role:option >> text:\"Beta thread\"", 5*time.Second)
	app.Press("id:palette-command >> role:option >> text:\"Beta thread\"")
	waitSheetGone(app, "id:palette")
	app.WaitFor("label:Message", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		return strings.Contains(app.Find("id:current-environment-name").Text, "Beta")
	})
	app.Type("label:Message", "Beta phone draft")
	app.Press("label:Back to threads")
	app.Press("id:mobile-tab-usage")
	app.WaitFor("id:mobile-usage-page", 5*time.Second)
	app.Press("id:usage-metric-tokens")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:mobile-usage-total").Text == "4.0k" })
	app.Press("id:mobile-usage-environment")
	app.Press("id:mobile-usage-environment-" + alpha.profile.ID)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:mobile-usage-total").Text == "1.5k" })
	app.Press("id:mobile-tab-threads")
	app.Press("id:thread-" + betaID)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Beta phone draft" })
}

func openMobileProjects(app *nsgotest.App) {
	app.WaitFor("id:mobile-filter", 10*time.Second)
	app.Press("id:mobile-filter")
	app.WaitFor("id:mobile-tab-projects", 5*time.Second)
	app.Press("id:mobile-tab-projects")
}
