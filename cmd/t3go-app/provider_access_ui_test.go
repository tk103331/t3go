package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
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
	waitSheetGone(app, "id:settings-dialog")
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

func TestUIAccessUsesSignedInAccount(t *testing.T) {
	const userToken = "test-relay-administrator-token"
	srv, ts, cfg := startEnv(t)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/v1/relay/environments":
			if r.Header.Get("Authorization") != "Bearer "+userToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"connectToken":"test-relay-connect-token-123"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hub.Close)
	profile := client.EnvironmentProfile{
		ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: cfg.Token,
		Routes: []client.Route{{URL: ts.URL, Kind: "direct"}},
	}
	account, err := json.Marshal(savedRelayAccount{
		Version: 1, Server: hub.URL, AllowHTTP: true,
		Session: proto.RelayUserSession{
			Token: userToken, ExpiresAt: time.Now().Add(time.Hour),
			User: proto.RelayUser{ID: "usr_test", Email: "owner@example.com", Name: "Account Owner"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1":      workspaceRegistry(profile),
		relayAccountStorageKey: string(account),
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	app.Press("id:settings-nav-access")
	app.WaitFor("id:settings-access-enabled", 5*time.Second)
	app.Press("id:settings-access-enabled")
	app.WaitFor("id:settings-relay-account", 5*time.Second)
	if got := app.Find("id:settings-relay-account-name").Text; got != "Account Owner" {
		t.Fatalf("Relay account name = %q", got)
	}
	if got := app.Find("id:settings-relay-account-server").Text; !strings.Contains(got, hub.URL) {
		t.Fatalf("Relay account detail = %q", got)
	}
	for _, id := range []string{"settings-relay-server", "settings-relay-email", "settings-relay-password"} {
		if app.Exists("id:" + id) {
			t.Fatalf("Access still asks for account credentials in %s", id)
		}
	}
	app.WaitFor("id:settings-relay-submit", 5*time.Second)
	app.Press("id:settings-relay-submit")
	app.WaitUntil(15*time.Second, func() bool {
		if !app.Exists("id:settings-access-link") {
			return false
		}
		link, err := url.Parse(app.Find("id:settings-access-link").Text)
		return err == nil && link.Query().Get("url") == hub.URL+"/e/"+srv.Identity.ID
	})
	if !app.Exists("id:settings-access-relay-row") || app.Exists("id:settings-access-relay-unavailable") {
		t.Fatal("configured Relay route was not added to Access")
	}
	raw, _ := app.Storage().GetItem("environments:v1")
	if !strings.Contains(raw, hub.URL+"/e/"+srv.Identity.ID) {
		t.Fatalf("saved profile is missing Relay route: %s", raw)
	}
}

func TestUIAccessDirectsSignedOutUsersToAccount(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	profile := client.EnvironmentProfile{
		ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: cfg.Token,
		Routes: []client.Route{{URL: ts.URL, Kind: "direct"}},
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(profile),
	}))
	app.WaitFor("id:settings", 15*time.Second)
	app.Press("id:settings")
	app.Press("id:settings-nav-access")
	app.WaitFor("id:settings-access-enabled", 5*time.Second)
	app.Press("id:settings-access-enabled")
	app.WaitFor("id:settings-relay-account-required", 5*time.Second)
	for _, id := range []string{"settings-relay-server", "settings-relay-email", "settings-relay-password", "settings-relay-submit"} {
		if app.Exists("id:" + id) {
			t.Fatalf("signed-out Access exposes credential flow %s", id)
		}
	}
	app.Press("id:settings-relay-open-account")
	app.WaitFor("id:relay-account-mode-login", 5*time.Second)
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
	app.WaitFor("id:settings-provider-add", 10*time.Second)
	if app.Exists("id:settings-pi-provider") {
		t.Fatal("built-in Pi provider is still shown in settings")
	}
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
	if len(settings.ACPProviders) != 1 || settings.ACPProviders[0].Name != "Local Agent" ||
		settings.ACPProviders[0].CommandPath != "agent" ||
		len(settings.ACPProviders[0].CommandArgs) != 1 || settings.ACPProviders[0].CommandArgs[0] != "acp" {
		t.Fatalf("ACP settings = %#v", settings.ACPProviders)
	}
	providerIDs := srv.Service.Providers.Providers()
	found := false
	for _, id := range providerIDs {
		if id == "pi" {
			t.Fatalf("built-in Pi provider is still registered: %#v", providerIDs)
		}
		if id == settings.ACPProviders[0].ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("configured providers = %#v, missing %q", providerIDs, settings.ACPProviders[0].ID)
	}
}
