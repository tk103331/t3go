package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"

	"github.com/tk/t3go/internal/orchestrator"
)

// TestUIMobilePages walks the phone's pages: the thread inbox, the pull
// request page with its filters, and the usage page with its metric and
// range pickers.
func TestUIMobilePages(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if _, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Mobile thread", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	}); err != nil {
		t.Fatalf("thread: %v", err)
	}

	tester, session := startAppAt(t, ts, cfg, 390, 844, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "the threads to arrive")

	// The phone inbox lists the thread with the machine it runs on.
	session.mobilePage = "threads"
	tester.pump()
	if !tester.tt.HasText("Mobile thread") {
		t.Fatalf("the phone inbox does not list the thread: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText("Mobile") {
		t.Fatalf("the inbox row does not name the project")
	}

	// The pulls page shows its filters and settles its load.
	session.mobilePage = "prs"
	session.loadMobilePullRequests()
	tester.wait(t, 30*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !session.mobilePRBusy
	}, "the pull requests to settle")
	tester.pump()
	if !tester.tt.HasText(i18n.T("PR state")) || !tester.tt.HasText(i18n.T("Environments")) {
		t.Fatalf("the pulls page does not show its filters: %v", tester.tt.Texts())
	}
	// The state filter applies to the rows: whatever loads, a MERGED
	// filter only shows merged ones.
	session.mobilePRState = "MERGED"
	tester.pump()
	for _, row := range session.mobilePRRows {
		if strings.ToUpper(row.PR.State) != "MERGED" {
			t.Fatalf("the state filter leaked a %q PR", row.PR.State)
		}
	}
	session.mobilePRState = ""

	// The usage page loads the environment's report and picks a metric.
	session.mobilePage = "usage"
	session.loadMobileUsage()
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return !session.usageBusy && session.usageReport.Totals.Runs >= 0
	}, "the usage report to load")
	tester.pump()
	if !tester.tt.HasText(i18n.T("Recorded cost")) {
		t.Fatalf("the usage page does not show the totals: %v", tester.tt.Texts())
	}
	// The metric picker switches to tokens.
	session.mobileUsageMetric = "tokens"
	tester.pump()
	if !tester.tt.HasText(i18n.T("Recorded tokens")) {
		t.Fatalf("the usage page does not switch to tokens: %v", tester.tt.Texts())
	}
}

// TestUIMobileSettings opens the phone's settings index and its
// sections.
func TestUIMobileSettings(t *testing.T) {
	srv, ts, cfg := startEnv(t)

	ctx := context.Background()
	if _, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t)); err != nil {
		t.Fatalf("project: %v", err)
	}

	tester, session := startAppAt(t, ts, cfg, 390, 844, nil)

	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")

	// The phone's settings open as a page, not a dialog.
	session.openSettings()
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.mobilePage == "settings" && !session.showSettings
	}, "the settings page to open")
	tester.pump()
	if !tester.tt.HasText(i18n.T("Environments")) || !tester.tt.HasText(i18n.T("Appearance")) {
		t.Fatalf("the settings index is missing its rows: %v", tester.tt.Texts())
	}

	// A section's page opens and comes back.
	if err := tester.tt.Click(i18n.T("Account")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	if session.mobileSettingsDetail != "account" || !tester.tt.HasText(i18n.T("Relay account")) {
		t.Fatal("the phone's account settings did not open")
	}
	session.mobileSettingsDetail = ""
	tester.pump()

	session.openMobileSettingsSection("providers")
	tester.pump()
	if !tester.tt.HasText(i18n.T("Detected providers")) {
		t.Fatalf("the providers page did not open: %v", tester.tt.Texts())
	}
	session.mobileSettingsDetail = ""
	tester.pump()

	// The environments page lists the saved environment.
	session.openMobileSettingsSection("environments")
	tester.pump()
	if !tester.tt.HasText(session.envName()) {
		t.Fatalf("the environments page does not list the environment: %v", tester.tt.Texts())
	}
	session.mobileSettingsDetail = ""
	session.mobilePage = "threads"
}

// TestUISettingsNavSwitches clicks the settings dialog's nav and checks
// the section page follows.
func TestUISettingsNavSwitches(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	ctx := context.Background()
	if _, err := srv.Service.CreateProject(ctx, "Nav", gitRepo(t)); err != nil {
		t.Fatalf("project: %v", err)
	}

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	session.setSettingsSection("general")
	session.openSettings()
	tester.pump()

	// Click the Providers row by its label, as a reader does.
	if err := tester.tt.Click(i18n.T("Providers")); err != nil {
		t.Fatalf("click: %v", err)
	}
	tester.wait(t, 5*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return session.settingsSection == "providers" && tester.tt.HasText(i18n.T("Detected providers"))
	}, "the providers page to open")
}

// TestUIMobileComposerFocusGrowth checks the phone's message field: one
// line at rest with a tight frame, room for three lines while it has the
// focus, and one line again once it loses it.
func TestUIMobileComposerFocusGrowth(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})

	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Mobile", gitRepo(t))
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Focused composer", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	tester, session := startAppAt(t, ts, cfg, 390, 844, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID
	}, "the thread to load")
	tester.pump()

	label := i18n.T("Message")
	rest, ok := tester.tt.Find(label)
	if !ok {
		t.Fatalf("the phone composer has no message field: %v", tester.tt.Texts())
	}
	if tester.tt.Focused(label) {
		t.Fatal("the message field starts focused")
	}
	if rest.H > 40 {
		t.Fatalf("the field at rest is %v high, want one tight line", rest.H)
	}
	if err := tester.tt.Click(label); err != nil {
		t.Fatalf("focus the message field: %v", err)
	}
	tester.pump()
	if !tester.tt.Focused(label) {
		t.Fatal("clicking the message field did not focus it")
	}
	focused, ok := tester.tt.Find(label)
	if !ok {
		t.Fatal("the focused message field is gone")
	}
	// Three lines against one: the field's own padding is in both.
	if focused.H < rest.H*2.2 || focused.H > rest.H*2.6 {
		t.Fatalf("the focused field is %v high, want about three lines of %v", focused.H, rest.H)
	}
	// A press outside the field takes the focus and the field shrinks
	// back to a single line.
	if err := tester.tt.Click("Focused composer"); err != nil {
		t.Fatalf("click outside the message field: %v", err)
	}
	tester.pump()
	if tester.tt.Focused(label) {
		t.Fatal("the message field kept the focus after a press outside it")
	}
	blurred, ok := tester.tt.Find(label)
	if !ok {
		t.Fatal("the blurred message field is gone")
	}
	if blurred.H != rest.H {
		t.Fatalf("the field is %v high after losing the focus, want %v", blurred.H, rest.H)
	}

	// The phone's new thread has the same field: one line at rest, three
	// while it is written in.
	session.showNewThread = true
	tester.pump()
	newLabel := i18n.T("New thread message")
	newRest, ok := tester.tt.Find(newLabel)
	if !ok {
		t.Fatalf("the phone's new thread has no message field: %v", tester.tt.Texts())
	}
	if newRest.H > 40 {
		t.Fatalf("the new thread field at rest is %v high, want one tight line", newRest.H)
	}
	if err := tester.tt.Click(newLabel); err != nil {
		t.Fatalf("focus the new thread field: %v", err)
	}
	tester.pump()
	if !tester.tt.Focused(newLabel) {
		t.Fatal("clicking the new thread field did not focus it")
	}
	newFocused, ok := tester.tt.Find(newLabel)
	if !ok {
		t.Fatal("the focused new thread field is gone")
	}
	if newFocused.H < newRest.H*2.2 || newFocused.H > newRest.H*2.6 {
		t.Fatalf("the focused new thread field is %v high, want about three lines of %v", newFocused.H, newRest.H)
	}
}
