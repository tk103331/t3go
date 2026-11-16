package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/relay"
)

func TestRelayAccessConfigurationRegistersAndStoresCredentials(t *testing.T) {
	const userToken = "relay-admin-token-12345"
	e := newTestEnv(t)
	var registration relay.Registration
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/relay/environments" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+userToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"connectToken":"connect-token-abcdefghijklmnopqrstuvwxyz"}`)
	}))
	t.Cleanup(hub.Close)

	var initial proto.RelayAccessStatus
	if code := e.request(t, http.MethodGet, "/api/v1/access/relay", nil, e.token, &initial); code != http.StatusOK {
		t.Fatalf("initial relay status = %d", code)
	}
	if initial.Configured {
		t.Fatalf("initial relay status = %#v", initial)
	}

	var status proto.RelayAccessStatus
	code := e.request(t, http.MethodPost, "/api/v1/access/relay", proto.RelayAccessConfig{
		Server: hub.URL, UserToken: userToken, DirectURL: "http://192.0.2.10:4870",
		AllowHTTP: true,
	}, e.token, &status)
	if code != http.StatusOK {
		t.Fatalf("configure relay = %d", code)
	}
	if !status.Configured || status.Server != hub.URL ||
		status.Route != hub.URL+"/e/"+e.server.Identity.ID || status.DirectURL != "http://192.0.2.10:4870" {
		t.Fatalf("relay status = %#v", status)
	}
	if registration.ID != e.server.Identity.ID || registration.PublicKey != e.server.Identity.PublicKey() ||
		registration.Token != e.token || registration.Name == "" ||
		len(registration.DirectURLs) != 1 || registration.DirectURLs[0] != "http://192.0.2.10:4870" {
		t.Fatalf("registration = %#v", registration)
	}
	raw, err := os.ReadFile(filepath.Join(e.server.Config.Home, "relay.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), userToken) {
		t.Fatal("Relay user token was persisted in relay.json")
	}
	info, err := os.Stat(filepath.Join(e.server.Config.Home, "relay.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("relay.json mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRelayAccessRejectsPublicHTTPWithoutExplicitPermission(t *testing.T) {
	e := newTestEnv(t)
	code := e.request(t, http.MethodPost, "/api/v1/access/relay", proto.RelayAccessConfig{
		Server: "http://relay.example.com", AdminToken: "relay-admin-token-12345",
	}, e.token, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("insecure relay configure = %d", code)
	}
}

func TestRelayUserAccountConfiguresSignedEnvironment(t *testing.T) {
	e := newTestEnv(t)
	hub, err := relay.New(t.TempDir(), "relay-admin-token-12345")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	hub.Mount(mux)
	hubServer := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Close()
		hubServer.Close()
	})

	var session proto.RelayUserSession
	code := e.request(t, http.MethodPost, "/api/v1/access/relay/register", proto.RelayAccountRequest{
		Server: hubServer.URL, Email: "desktop@example.com", Password: "alice-password",
		Name: "Desktop", AllowHTTP: true,
	}, e.token, &session)
	if code != http.StatusOK || len(session.Token) < 20 || session.User.Email != "desktop@example.com" {
		t.Fatalf("relay user registration = %d, session = %#v", code, session)
	}
	var status proto.RelayAccessStatus
	code = e.request(t, http.MethodPost, "/api/v1/access/relay", proto.RelayAccessConfig{
		Server: hubServer.URL, UserToken: session.Token, AllowHTTP: true,
	}, e.token, &status)
	if code != http.StatusOK || !status.Configured {
		t.Fatalf("configure user-owned relay = %d, status = %#v", code, status)
	}
	req, _ := http.NewRequest(http.MethodGet, hubServer.URL+"/api/v1/relay/environments", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listed struct {
		Environments []map[string]any `json:"environments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Environments) != 1 || listed.Environments[0]["environmentId"] != e.server.Identity.ID {
		t.Fatalf("user-owned environments = %#v", listed.Environments)
	}
}

func TestSavedRelayConfigurationReconnectsOnRestart(t *testing.T) {
	const adminToken = "relay-admin-token-12345"
	e := newTestEnv(t)
	if started, err := e.server.StartSavedRelayConnector(context.Background(), e.server.Addr()); err != nil || started {
		t.Fatalf("missing saved relay started = %v, err = %v", started, err)
	}

	hub, err := relay.New(t.TempDir(), adminToken)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	hub.Mount(mux)
	hubServer := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Close()
		hubServer.Close()
	})
	raw, _ := json.Marshal(relay.Registration{
		ID: e.server.Identity.ID, PublicKey: e.server.Identity.PublicKey(), Name: "restart-test", Token: e.token,
	})
	req, _ := http.NewRequest(http.MethodPost, hubServer.URL+"/api/v1/relay/environments", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register relay = %d", resp.StatusCode)
	}
	var registered relay.Registered
	if err := json.NewDecoder(resp.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	if err := saveRelayCredentials(e.server.Config.Home, relayCredentials{
		Server: hubServer.URL, EnvironmentID: e.server.Identity.ID, ConnectToken: registered.ConnectToken,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	target := strings.TrimPrefix(e.http.URL, "http://")
	started, err := e.server.StartSavedRelayConnector(ctx, target)
	if err != nil || !started {
		t.Fatalf("restore saved relay = %v, err = %v", started, err)
	}
	upgradeDeadline := time.Now().Add(10 * time.Second)
	for {
		saved, err := loadRelayCredentials(e.server.Config.Home)
		if err == nil && saved.RelayKey != "" {
			break
		}
		if time.Now().After(upgradeDeadline) {
			t.Fatalf("saved Relay credentials were not upgraded: %#v, %v", saved, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.server.relayConnMu.Lock()
		state := e.server.relayState
		e.server.relayConnMu.Unlock()
		if state == "active" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved relay state = %q, want active", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSavedRelayUpgradeDoesNotBlockStartup(t *testing.T) {
	e := newTestEnv(t)
	if err := saveRelayCredentials(e.server.Config.Home, relayCredentials{
		Server: "http://127.0.0.1:1", EnvironmentID: e.server.Identity.ID,
		ConnectToken: strings.Repeat("connect-", 4),
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	startedAt := time.Now()
	started, err := e.server.StartSavedRelayConnector(ctx, strings.TrimPrefix(e.http.URL, "http://"))
	if err != nil || !started {
		t.Fatalf("saved Relay startup = %v, %v", started, err)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("saved Relay upgrade blocked startup for %s", elapsed)
	}
}
