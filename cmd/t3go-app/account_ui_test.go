package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/relay"
	"github.com/tk/t3go/internal/server"
)

const relayAccountTestPassword = "relay" + "-account-test-password"

func startRelayAccountHub(t *testing.T) *httptest.Server {
	t.Helper()
	hub, err := relay.New(t.TempDir(), "relay-account-ui-administrator")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	hub.Mount(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Close()
		ts.Close()
	})
	return ts
}

func registerRelayAccountForUI(t *testing.T, hubURL, email string) proto.RelayUserSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := client.RegisterRelayAccount(ctx, hubURL, true, proto.RelayAccountRequest{
		Server: hubURL, Email: email, Password: relayAccountTestPassword, Name: "UI Owner", AllowHTTP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// registerRelayEnvironment registers a running environment with the hub and
// writes its credentials, without opening the tunnel: the environment then
// shows as an offline discovery.
func registerRelayEnvironment(t *testing.T, srv *server.Server, hubURL, token string) relay.Registered {
	t.Helper()
	registration := relay.Registration{
		ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Name: "Owner desktop",
		Token: srv.Config.Token,
	}
	if err := registration.Sign(srv.Identity.PrivateKey); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		hubURL+"/api/v1/relay/environments", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register environment = %d", resp.StatusCode)
	}
	var registered relay.Registered
	if err := json.NewDecoder(resp.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	if registered.ConnectToken == "" || registered.RelayKey == "" {
		t.Fatal("Relay did not return environment credentials")
	}
	credentials, err := json.Marshal(map[string]string{
		"server": hubURL, "environmentId": srv.Identity.ID,
		"connectToken": registered.ConnectToken, "relayKey": registered.RelayKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srv.Config.Home, "relay.json"), credentials, 0o600); err != nil {
		t.Fatal(err)
	}
	return registered
}

func registerRelayOwnedEnvironment(t *testing.T, srv *server.Server, environmentURL,
	hubURL, token string) relay.Registered {
	t.Helper()
	registered := registerRelayEnvironment(t, srv, hubURL, token)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connectorCtx, cancelConnector := context.WithCancel(context.Background())
	t.Cleanup(cancelConnector)
	connector := relay.Connector{
		Server: hubURL, EnvironmentID: srv.Identity.ID, Token: registered.ConnectToken,
		Target: strings.TrimPrefix(environmentURL, "http://"),
	}
	go connector.Run(connectorCtx)

	deadline := time.Now().Add(10 * time.Second)
	for {
		environments, err := client.RelayAccountEnvironments(ctx, hubURL, token, true)
		if err == nil {
			for _, environment := range environments {
				if environment.EnvironmentID == srv.Identity.ID && environment.Online {
					return registered
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Relay environment did not come online: %#v, %v", environments, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func storedRelayAccount(t *testing.T, app *nsgotest.App) savedRelayAccount {
	t.Helper()
	raw, ok := app.Storage().GetItem(relayAccountStorageKey)
	if !ok {
		t.Fatal("Relay account session was not saved")
	}
	var account savedRelayAccount
	if err := json.Unmarshal([]byte(raw), &account); err != nil {
		t.Fatal(err)
	}
	return account
}

func storedEnvironmentProfiles(t *testing.T, app *nsgotest.App) []client.EnvironmentProfile {
	t.Helper()
	raw, ok := app.Storage().GetItem("environments:v1")
	if !ok {
		return nil
	}
	var saved savedEnvironments
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	return saved.Profiles
}

func TestUIRelayAccountRegistrationDiscoveryRestartAndSignOut(t *testing.T) {
	srv, environment, _ := startEnv(t)
	hub := startRelayAccountHub(t)
	const email = "ui-owner@example.com"

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760))
	app.WaitFor("id:relay-account-open", 15*time.Second)
	app.Press("id:relay-account-open")
	app.WaitFor("id:relay-account-dialog", 5*time.Second)
	app.Press("id:relay-account-mode-register")
	app.Type("id:relay-account-server", hub.URL)
	app.Type("id:relay-account-email", email)
	app.Type("id:relay-account-password", relayAccountTestPassword)
	app.Type("id:relay-account-name", "UI Owner")
	app.Press("id:relay-account-allow-http")
	app.Press("id:relay-account-submit")
	app.WaitFor("id:relay-account-signed-in", 20*time.Second)
	app.WaitFor("text:No environments found", 5*time.Second)

	account := storedRelayAccount(t, app)
	if account.Session.Token == "" || account.Session.User.Email != email || account.Server != hub.URL {
		t.Fatalf("saved account = %#v", account)
	}
	registerRelayOwnedEnvironment(t, srv, environment.URL, hub.URL, account.Session.Token)
	app.Press("id:relay-account-refresh")
	app.WaitFor("id:relay-account-environment-"+srv.Identity.ID, 15*time.Second)
	app.Press("id:relay-account-open-environment-" + srv.Identity.ID)
	app.WaitFor("id:new-thread", 20*time.Second)

	profiles := storedEnvironmentProfiles(t, app)
	if len(profiles) != 1 || profiles[0].ID != srv.Identity.ID ||
		profiles[0].AccountServer != hub.URL || profiles[0].Token != account.Session.Token ||
		profiles[0].Preference != "relay-only" || len(profiles[0].Routes) != 1 ||
		profiles[0].Routes[0].Kind != "relay" {
		t.Fatalf("discovered profile = %#v", profiles)
	}
	accountRaw, _ := app.Storage().GetItem(relayAccountStorageKey)
	profilesRaw, _ := app.Storage().GetItem("environments:v1")

	restored := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		relayAccountStorageKey: accountRaw,
		"environments:v1":      profilesRaw,
	}))
	restored.WaitFor("id:settings", 15*time.Second)
	restored.Press("id:settings")
	restored.WaitFor("id:settings-nav-account", 5*time.Second)
	restored.Press("id:settings-nav-account")
	restored.WaitFor("id:relay-account-environment-"+srv.Identity.ID, 20*time.Second)
	restored.Press("id:relay-account-sign-out")
	restored.WaitUntil(10*time.Second, func() bool {
		if _, ok := restored.Storage().GetItem(relayAccountStorageKey); ok {
			return false
		}
		var saved savedEnvironments
		raw, ok := restored.Storage().GetItem("environments:v1")
		return ok && json.Unmarshal([]byte(raw), &saved) == nil && len(saved.Profiles) == 0
	})
}

func TestUIRelayAccountSignInDiscoversExistingEnvironment(t *testing.T) {
	srv, environment, _ := startEnv(t)
	hub := startRelayAccountHub(t)
	const email = "existing-owner@example.com"
	existing := registerRelayAccountForUI(t, hub.URL, email)
	registerRelayOwnedEnvironment(t, srv, environment.URL, hub.URL, existing.Token)

	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760))
	app.WaitFor("id:relay-account-open", 15*time.Second)
	app.Press("id:relay-account-open")
	app.WaitFor("id:relay-account-dialog", 5*time.Second)
	app.Type("id:relay-account-server", hub.URL)
	app.Type("id:relay-account-email", email)
	app.Type("id:relay-account-password", relayAccountTestPassword)
	app.Press("id:relay-account-allow-http")
	app.Press("id:relay-account-submit")
	app.WaitFor("id:relay-account-signed-in", 20*time.Second)
	app.WaitFor("id:relay-account-environment-"+srv.Identity.ID, 15*time.Second)
	account := storedRelayAccount(t, app)
	if account.Session.User.Email != email || account.Session.Token == "" ||
		account.Session.Token == existing.Token {
		t.Fatalf("signed-in account = %#v", account)
	}
	profiles := storedEnvironmentProfiles(t, app)
	if len(profiles) != 1 || profiles[0].AccountServer != hub.URL ||
		profiles[0].Token != account.Session.Token {
		t.Fatalf("signed-in profiles = %#v", profiles)
	}
}

func TestUIRelayAccountMergePreservesLocalDirectProfile(t *testing.T) {
	srv, environment, cfg := startEnv(t)
	hub := startRelayAccountHub(t)
	account := registerRelayAccountForUI(t, hub.URL, "merge-owner@example.com")
	registerRelayOwnedEnvironment(t, srv, environment.URL, hub.URL, account.Token)

	local := client.EnvironmentProfile{
		ID: srv.Identity.ID, Name: "Local desktop", Alias: "This Mac",
		PublicKey: srv.Identity.PublicKey(), Token: cfg.Token, Preference: "auto",
		Routes: []client.Route{{URL: environment.URL, Kind: "direct"}},
	}
	accountRaw, err := json.Marshal(savedRelayAccount{
		Version: 1, Server: hub.URL, AllowHTTP: true, Session: account,
	})
	if err != nil {
		t.Fatal(err)
	}
	var manager *environmentManager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = newEnvironmentManager(cx)
		manager.start()
		return manager.view(cx)
	}, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		relayAccountStorageKey: string(accountRaw),
		"environments:v1":      workspaceRegistry(local),
	}))
	app.WaitFor("id:new-thread", 20*time.Second)
	app.WaitUntil(20*time.Second, func() bool {
		return len(manager.accountEnvs.GetUntracked()) == 1 &&
			len(manager.profiles.GetUntracked()) == 1 &&
			len(manager.profiles.GetUntracked()[0].Routes) == 2
	})
	profile := manager.profiles.GetUntracked()[0]
	if profile.Token != cfg.Token || profile.AccountServer != "" || profile.Preference != "auto" ||
		profile.Alias != "This Mac" || profile.Routes[0].Kind != "direct" ||
		profile.Routes[0].URL != environment.URL || profile.Routes[1].Kind != "relay" {
		t.Fatalf("merged local profile = %#v", profile)
	}
	manager.refreshRelayAccount(false)
	app.WaitUntil(20*time.Second, func() bool {
		return !manager.accountBusy.GetUntracked() && len(manager.accountEnvs.GetUntracked()) == 1
	})
	if routes := manager.profiles.GetUntracked()[0].Routes; len(routes) != 2 ||
		routes[0].Kind != "direct" || routes[1].Kind != "relay" {
		t.Fatalf("repeated discovery duplicated routes: %#v", routes)
	}
}

func TestRelayAccountDiscoveryPreservesPairedLANAccess(t *testing.T) {
	const (
		environmentID    = "environment-lan-owner"
		environmentKey   = "environment-lan-public-key"
		environmentToken = "environment-lan-access-token"
		accountToken     = "relay-account-token"
	)
	profile := client.EnvironmentProfile{
		ID: environmentID, PublicKey: environmentKey, Token: environmentToken,
		Preference: "direct-only",
		Routes:     []client.Route{{URL: "http://192.168.1.20:4870", Kind: "direct"}},
	}
	var manager *environmentManager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = newEnvironmentManager(cx)
		return core.Text{Content: "account merge test"}
	}, nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(profile),
	}))
	app.WaitUntil(5*time.Second, func() bool { return manager != nil })
	environments := []client.RelayEnvironment{{
		EnvironmentID: environmentID, PublicKey: environmentKey,
		Name: "Desk", Online: true,
	}}
	manager.mergeAccountEnvironments("https://relay.example.com", accountToken, environments, false)
	manager.mergeAccountEnvironments("https://relay.example.com", accountToken, environments, false)
	got := manager.profiles.GetUntracked()
	if len(got) != 1 || got[0].Token != environmentToken ||
		got[0].AccountServer != "" || got[0].Preference != "direct-only" ||
		len(got[0].Routes) != 2 || got[0].Routes[0].Kind != "direct" ||
		got[0].Routes[0].URL != "http://192.168.1.20:4870" ||
		got[0].Routes[1].Kind != "relay" {
		t.Fatalf("LAN profile was replaced by account discovery: %#v", got)
	}
}

// TestUIAccountShowsDiscoveriesApartAndRemovesStaleOnes pins the account's
// environment list: registrations a machine left behind read apart from the
// one that answers, and a stale one can be removed from the account.
func TestUIAccountShowsDiscoveriesApartAndRemovesStaleOnes(t *testing.T) {
	live, liveEnvironment, _ := startEnv(t)
	stale, _, _ := startEnv(t)
	hub := startRelayAccountHub(t)
	account := registerRelayAccountForUI(t, hub.URL, "stale-owner@example.com")
	registerRelayEnvironment(t, stale, hub.URL, account.Token)
	registerRelayOwnedEnvironment(t, live, liveEnvironment.URL, hub.URL, account.Token)

	accountRaw, err := json.Marshal(savedRelayAccount{Version: 1, Server: hub.URL, AllowHTTP: true, Session: account})
	if err != nil {
		t.Fatal(err)
	}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		relayAccountStorageKey: string(accountRaw),
	}))
	app.WaitFor("id:relay-account-open", 15*time.Second)
	app.Press("id:relay-account-open")
	app.WaitFor("id:relay-account-dialog", 5*time.Second)
	app.WaitFor("id:relay-account-environment-"+live.Identity.ID, 20*time.Second)
	app.WaitFor("id:relay-account-environment-"+stale.Identity.ID, 5*time.Second)

	liveDetail := app.Find("id:relay-account-environment-detail-" + live.Identity.ID).Text
	staleDetail := app.Find("id:relay-account-environment-detail-" + stale.Identity.ID).Text
	if !strings.Contains(liveDetail, "Online") || !strings.Contains(staleDetail, "Offline") {
		t.Fatalf("details = %q, %q", liveDetail, staleDetail)
	}
	if liveDetail == staleDetail {
		t.Fatalf("two discoveries read the same: %q", liveDetail)
	}
	liveRow := mobileScreenFrame(app.Find("id:relay-account-environment-" + live.Identity.ID))
	staleRow := mobileScreenFrame(app.Find("id:relay-account-environment-" + stale.Identity.ID))
	if liveRow.Origin.Y >= staleRow.Origin.Y {
		t.Fatalf("the offline discovery %v is not below the online one %v", staleRow, liveRow)
	}

	app.Press("id:relay-account-remove-environment-" + stale.Identity.ID)
	app.WaitFor("id:relay-account-remove-dialog", 5*time.Second)
	app.Press("id:relay-account-remove-confirm")
	app.WaitForGone("id:relay-account-environment-"+stale.Identity.ID, 10*time.Second)
	app.WaitUntil(10*time.Second, func() bool {
		environments, err := client.RelayAccountEnvironments(context.Background(), hub.URL, account.Token, true)
		return err == nil && len(environments) == 1 && environments[0].EnvironmentID == live.Identity.ID
	})
	if !app.Exists("id:relay-account-environment-" + live.Identity.ID) {
		t.Fatal("removing the stale discovery removed the live one")
	}
}
