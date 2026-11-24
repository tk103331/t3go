package app

import (
	"testing"
	"time"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
)

// TestUIMobileProvidersEnvironmentChooser walks the phone's providers
// page the way the old client did: the environments are named, the one
// that does not answer says so, and choosing another one shows its
// providers.
func TestUIMobileProvidersEnvironmentChooser(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})

	tester, session := startAppAt(t, ts, cfg, 390, 844, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")

	mgr := tester.manager
	// An environment the phone cannot reach, such as a machine on the
	// desk, becomes the session on screen.
	far := mgr.newSession(client.EnvironmentProfile{ID: "far-away", Name: "Far away"})
	mgr.sessions["far-away"] = far
	mgr.app, mgr.active, mgr.current = far, far, "far-away"
	far.narrow = true
	far.mobilePage = "settings"

	// The providers page names the environment that does not answer and
	// offers to bring it back, as the old client did.
	far.openSettings()
	tester.pump()
	if err := tester.tt.Click(i18n.T("Providers")); err != nil {
		t.Fatalf("open the providers page: %v", err)
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("Connect this environment to view or change its settings.")) {
		t.Fatalf("the page does not say the environment is not connected: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText(i18n.T("Reconnect")) {
		t.Fatalf("the page has no way to bring the environment back: %v", tester.tt.Texts())
	}
	// The environment picker names every environment.
	if !tester.tt.HasText("Far away") {
		t.Fatalf("the page does not name the environments: %v", tester.tt.Texts())
	}

	// Choosing the environment that answers shows its providers.
	name := mgr.environmentNames()[session.profile.ID]
	if name == "" {
		name = session.envName()
	}
	if err := tester.tt.Click(name); err != nil {
		t.Fatalf("choose the environment that answers: %v", err)
	}
	tester.pump()
	tester.wait(t, 15*time.Second, func() bool {
		tester.post.drain()
		tester.tt.Frame()
		return len(session.providers) > 0
	}, "the providers of the chosen environment")
	if !tester.tt.HasText(i18n.T("Detected providers")) {
		t.Fatalf("the providers page did not open: %v", tester.tt.Texts())
	}
}

// TestUISettingsEnvironmentChooserAndReconnect checks the settings on a
// desktop: the environments are named, and one that does not answer says
// so, with a way to bring it back.
func TestUISettingsEnvironmentChooserAndReconnect(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})

	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")

	// An environment that does not answer, chosen for the settings.
	far := tester.manager.newSession(client.EnvironmentProfile{ID: "far-away", Name: "Far away"})
	tester.manager.sessions["far-away"] = far
	session.openSettings()
	tester.pump()
	session.settingsEnv = "far-away"
	session.setSettingsSection("providers")
	tester.pump()

	if !tester.tt.HasText(i18n.T("Connect this environment to view or change its settings.")) {
		t.Fatalf("the settings do not say the environment is not connected: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText(i18n.T("Reconnect")) {
		t.Fatalf("the settings have no way to bring the environment back: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText("Far away") {
		t.Fatalf("the settings do not name the environments: %v", tester.tt.Texts())
	}
}
