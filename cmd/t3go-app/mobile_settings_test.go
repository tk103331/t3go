package main

import (
	"strings"
	"testing"
	"time"
)

// TestUIMobileSettingsPage keeps the phone's settings a page of their own:
// no close button (the toolbar has a back arrow) and no keyboard shortcuts
// (the phone has no hardware keyboard).
func TestUIMobileSettingsPage(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	app, _ := startMobilePages(t, alpha, beta)
	app.Press("id:settings")
	app.WaitFor("id:mobile-settings-index", 5*time.Second)
	if app.Exists("id:settings-nav-shortcuts") {
		t.Fatal("mobile settings still lists the keyboard shortcuts")
	}
	app.Press("id:settings-nav-providers")
	app.WaitFor("id:settings-provider-card-claude", 10*time.Second)
	if app.Exists("id:settings-providers-environments") {
		t.Fatal("providers page repeats the environment summary")
	}
	if app.Exists("id:settings-close") {
		t.Fatal("mobile settings page still has a close button")
	}
	// The card names the CLI once; it used to repeat the name and the id
	// under the heading.
	dump := app.Dump()
	if strings.Contains(dump, "Alpha CLI · claude") {
		t.Fatalf("provider card repeats its name and id:\n%s", dump)
	}
}
