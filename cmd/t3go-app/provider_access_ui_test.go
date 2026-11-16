package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/nsgotest"
)

func TestUIAccessGeneratesCopyablePairingQRCode(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	app.Press("id:settings-nav-access")
	app.WaitFor("id:settings-access-enabled", 5*time.Second)
	app.Press("id:settings-access-enabled")
	app.WaitFor("id:settings-access-pairing", 5*time.Second)
	app.WaitFor("id:settings-access-qr", 5*time.Second)
	link := app.Find("id:settings-access-link").Text
	if !strings.HasPrefix(link, "t3go://pair?") || !strings.Contains(link, "environment=") ||
		!strings.Contains(link, "key=") {
		t.Fatalf("pairing link = %q", link)
	}
	app.Press("id:settings-access-copy")
	app.WaitUntil(5*time.Second, func() bool { return app.Clipboard == link })
	app.Press("id:settings-save")
	app.WaitUntil(10*time.Second, func() bool {
		return srv.Service.Settings(context.Background()).AccessEnabled
	})
	app.WaitForGone("id:settings-dialog", 5*time.Second)
}

func TestUIProviderSettingsPersistPerEnvironment(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"serverUrl": ts.URL,
		"token":     cfg.Token,
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	app.Press("id:settings-nav-providers")
	app.WaitFor("id:settings-pi-provider", 10*time.Second)
	app.Press("id:settings-pi-provider-enabled")
	setInput := func(id, want string) {
		app.WaitUntil(10*time.Second, func() bool {
			if app.Find(id).Text == want {
				return true
			}
			app.Clear(id)
			app.Type(id, want)
			app.Settle()
			return app.Find(id).Text == want
		})
	}
	setInput("id:settings-pi-binary", "pi")
	setInput("id:settings-pi-args", "provider")
	app.Press("id:settings-provider-add")
	app.WaitFor("id:settings-provider-add-panel", 5*time.Second)
	app.Press("id:settings-provider-mode-local")
	setInput("id:settings-acp-local-name", "Local Agent")
	setInput("id:settings-acp-local-path", "agent")
	setInput("id:settings-acp-local-args", "acp")
	app.Press("id:settings-acp-add-local")
	app.WaitFor("id:settings-acp-provider-acp-local-agent", 5*time.Second)
	app.Press("id:settings-save")
	app.WaitForGone("id:settings-dialog", 10*time.Second)

	settings := srv.Service.Settings(context.Background())
	if !settings.PiProvider.Enabled || settings.PiProvider.BinaryPath != "pi" ||
		len(settings.PiProvider.LaunchArgs) != 1 || settings.PiProvider.LaunchArgs[0] != "provider" {
		t.Fatalf("Pi settings = %#v", settings.PiProvider)
	}
	if len(settings.ACPProviders) != 1 || settings.ACPProviders[0].Name != "Local Agent" ||
		settings.ACPProviders[0].CommandPath != "agent" ||
		len(settings.ACPProviders[0].CommandArgs) != 1 || settings.ACPProviders[0].CommandArgs[0] != "acp" {
		t.Fatalf("ACP settings = %#v", settings.ACPProviders)
	}
	providerIDs := srv.Service.Providers.Providers()
	for _, want := range []string{"pi", settings.ACPProviders[0].ID} {
		found := false
		for _, id := range providerIDs {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("configured providers = %#v, missing %q", providerIDs, want)
		}
	}
}
