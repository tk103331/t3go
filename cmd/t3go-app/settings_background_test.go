package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
)

// TestUISettingsKeepTheLocalServerInTheBackground shows the switch in
// Access, only for a package that ships an environment server, and saves
// the choice.
func TestUISettingsKeepTheLocalServerInTheBackground(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	profile := client.EnvironmentProfile{
		ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: cfg.Token,
		Routes: []client.Route{{URL: ts.URL, Kind: "direct"}},
	}
	storage := map[string]string{"environments:v1": workspaceRegistry(profile)}
	openAccess := func() *nsgotest.App {
		t.Helper()
		app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(storage))
		app.WaitFor("id:settings", 15*time.Second)
		app.Press("id:settings")
		app.WaitFor("id:settings-dialog", 5*time.Second)
		app.Press("id:settings-nav-access")
		app.WaitFor("id:settings-access-title", 5*time.Second)
		return app
	}

	// The test binary ships no server, so there is nothing to keep running.
	t.Setenv("T3GO_SERVER_BIN", "")
	if app := openAccess(); app.Exists("id:settings-background-server") {
		t.Fatal("a client without a packaged server offers the background switch")
	}

	// A packaged client offers it, and the choice reaches the storage a
	// later start reads.
	bin := filepath.Join(t.TempDir(), "t3go")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T3GO_SERVER_BIN", bin)
	t.Setenv("T3GO_SERVER", ts.URL)
	app := openAccess()
	if !app.Exists("id:settings-background-server") {
		t.Fatal("a packaged client does not offer the background switch")
	}
	app.Press("id:settings-background-server")
	app.WaitUntil(5*time.Second, func() bool {
		value, _ := app.Storage().GetItem(backgroundServerKey)
		return value == "true"
	})
	app.Press("id:settings-background-server")
	app.WaitUntil(5*time.Second, func() bool {
		value, _ := app.Storage().GetItem(backgroundServerKey)
		return value == "false"
	})
}
