package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
)

func TestUIAccessGeneratesCopyablePairingQRCode(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	relay := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		relay.CloseClientConnections()
		relay.Close()
	})
	profile := client.EnvironmentProfile{
		ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: cfg.Token,
		Routes: []client.Route{
			{URL: ts.URL, Kind: "direct"},
			{URL: relay.URL, Kind: "relay"},
		},
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(profile),
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	app.Press("id:settings-nav-access")
	app.WaitFor("id:settings-access-enabled", 5*time.Second)
	app.Press("id:settings-access-enabled")
	app.WaitFor("id:settings-access-pairing", 5*time.Second)
	app.WaitFor("id:settings-access-qr", 5*time.Second)
	linkQuery := func() url.Values {
		link := app.Find("id:settings-access-link").Text
		parsed, err := url.Parse(link)
		if err != nil || !strings.HasPrefix(link, "t3go://pair?") {
			t.Fatalf("pairing link = %q: %v", link, err)
		}
		return parsed.Query()
	}
	query := linkQuery()
	if query.Get("url") != relay.URL || query.Get("direct") != ts.URL ||
		query.Get("environment") == "" || query.Get("key") == "" {
		t.Fatalf("pairing link query = %#v", query)
	}
	app.Press("id:settings-access-direct")
	app.WaitUntil(5*time.Second, func() bool {
		query := linkQuery()
		return query.Get("url") == relay.URL && query.Get("direct") == ""
	})
	if query := linkQuery(); query.Get("url") != relay.URL {
		t.Fatalf("relay-only pairing changed unexpectedly: %#v", query)
	}
	app.Press("id:settings-access-direct")
	app.WaitUntil(5*time.Second, func() bool { return linkQuery().Get("direct") == ts.URL })
	app.Press("id:settings-access-relay")
	app.WaitUntil(5*time.Second, func() bool {
		query := linkQuery()
		return query.Get("url") == ts.URL && query.Get("direct") == ""
	})
	link := app.Find("id:settings-access-link").Text
	app.Press("id:settings-access-copy")
	app.WaitUntil(5*time.Second, func() bool { return app.Clipboard == link })
	app.Press("id:settings-save")
	app.WaitUntil(10*time.Second, func() bool {
		settings := srv.Service.Settings(context.Background())
		return settings.AccessEnabled && settings.AccessRoutes == "direct"
	})
	app.WaitForGone("id:settings-dialog", 5*time.Second)
}

func TestUIAccessAndAboutOnlyUseLocalEnvironment(t *testing.T) {
	local, localTS, localCfg := startEnv(t)
	remote, remoteTS, remoteCfg := startEnv(t)
	localProfile := client.EnvironmentProfile{
		ID: local.Identity.ID, Alias: "Local machine", PublicKey: local.Identity.PublicKey(), Token: localCfg.Token,
		Routes: []client.Route{{URL: localTS.URL, Kind: "direct"}},
	}
	remoteProfile := client.EnvironmentProfile{
		ID: remote.Identity.ID, Alias: "Remote machine", PublicKey: remote.Identity.PublicKey(), Token: remoteCfg.Token,
		Routes: []client.Route{{URL: remoteTS.URL, Kind: "relay"}},
	}
	registry, _ := json.Marshal(savedEnvironments{
		Version: 1, Active: remoteProfile.ID, Default: remoteProfile.ID,
		Profiles: []client.EnvironmentProfile{localProfile, remoteProfile},
	})
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": string(registry),
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	app.Press("id:settings-nav-access")
	app.WaitFor("id:settings-access-title", 5*time.Second)
	if app.Exists("id:settings-environment") {
		t.Fatal("Access exposed an environment selector")
	}
	app.Press("id:settings-access-enabled")
	app.WaitFor("id:settings-access-pairing", 5*time.Second)
	link, err := url.Parse(app.Find("id:settings-access-link").Text)
	if err != nil {
		t.Fatal(err)
	}
	if got := link.Query().Get("url"); got != localTS.URL {
		t.Fatalf("Access paired %q, want local %q", got, localTS.URL)
	}
	app.Press("id:settings-nav-about")
	app.WaitFor("id:settings-about-server", 5*time.Second)
	if app.Exists("id:settings-environment") {
		t.Fatal("About exposed an environment selector")
	}
	if got := app.Find("id:settings-about-url").Text; got != localTS.URL {
		t.Fatalf("About local URL = %q, want %q", got, localTS.URL)
	}
	for _, id := range []string{"settings-about-client", "settings-about-build", "settings-about-go", "settings-about-nsgo", "settings-about-server"} {
		if strings.TrimSpace(app.Find("id:"+id).Text) == "" {
			t.Fatalf("%s is empty", id)
		}
	}
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
