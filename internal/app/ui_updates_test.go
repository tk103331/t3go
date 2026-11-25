package app

import (
	"testing"
	"time"

	"github.com/tk/t3go/internal/i18n"
)

// The About page offers client updates, and says where they apply when
// this is a development build.
func TestUISettingsAboutUpdates(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	_ = srv
	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	session.openSettings()
	session.setSettingsSection("about")
	tester.pump()
	if !tester.tt.HasText(i18n.T("Client updates")) {
		t.Fatalf("the About page has no update card: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText(i18n.T("Check for updates")) {
		t.Fatalf("the update card has no check action: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText(i18n.T("Updates are available in installed builds. Development builds update by rebuilding.")) {
		t.Fatalf("the development-build note is missing: %v", tester.tt.Texts())
	}
}
