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
