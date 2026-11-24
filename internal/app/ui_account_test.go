package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

func TestUISettingsAccount(t *testing.T) {
	var discoveries, logouts atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var in proto.RelayAccountRequest
			if json.NewDecoder(r.Body).Decode(&in) != nil || in.Email != "ui@example.com" || in.Password != "test-password" {
				http.Error(w, "invalid credentials", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(proto.RelayUserSession{
				User:  proto.RelayUser{ID: "ui-user", Email: in.Email, Name: "UI Account"},
				Token: "account-test-token", ExpiresAt: time.Now().Add(time.Hour),
			})
		case "/api/v1/relay/environments":
			if r.Header.Get("Authorization") != "Bearer account-test-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			discoveries.Add(1)
			_, _ = w.Write([]byte(`{"environments":[]}`))
		case "/api/v1/auth/logout":
			logouts.Add(1)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()
	_, ts, cfg := startEnv(t)
	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	session.openSettings()
	tester.pump()
	click := func(label string) {
		t.Helper()
		if err := tester.tt.Click(i18n.T(label)); err != nil {
			t.Fatalf("click %s: %v", label, err)
		}
		tester.pump()
	}
	click("Account")
	if session.settingsSection != "account" || !tester.tt.HasText(i18n.T("Relay account")) {
		t.Fatal("Account navigation did not open the account page")
	}
	click("Sign in and discover")
	if !tester.tt.HasText(i18n.T("Enter the Relay server URL.")) {
		t.Fatal("account form did not show validation feedback")
	}
	click("Register")
	if tester.manager.accountMode != "register" || !tester.tt.HasText(i18n.T("Create account")) {
		t.Fatal("registration mode did not open")
	}
	if _, ok := tester.tt.Access(i18n.T("Display name")); !ok {
		t.Fatal("registration name field is missing")
	}
	click("Sign in")
	for _, field := range []struct{ label, value string }{
		{"Relay server URL", relay.URL}, {"Email", "ui@example.com"}, {"Password", "test-password"},
	} {
		click(field.label)
		tester.tt.Type(field.value)
	}
	password, ok := tester.tt.AccessByRole(ui.RoleTextField, i18n.T("Password"))
	if !ok || !password.Password {
		t.Fatal("password field does not mask its contents")
	}
	click("Allow insecure HTTP")
	click("Sign in and discover")
	tester.wait(t, 10*time.Second, func() bool {
		return tester.manager.accountSessionValid() && !tester.manager.accountBusy
	}, "account login and discovery")
	if !tester.tt.HasText("ui@example.com") || !tester.tt.HasText(i18n.T("Discovered environments")) {
		t.Fatal("signed-in account details are missing")
	}
	if _, ok := tester.manager.store.GetItem(relayAccountStorageKey); !ok {
		t.Fatal("account session was not saved")
	}
	click("Refresh")
	tester.wait(t, 10*time.Second, func() bool {
		return discoveries.Load() >= 2 && !tester.manager.accountBusy
	}, "account environment refresh")
	click("Sign out")
	tester.wait(t, 10*time.Second, func() bool {
		return !tester.manager.accountSessionValid() && logouts.Load() == 1
	}, "account logout")
	if _, ok := tester.manager.store.GetItem(relayAccountStorageKey); ok {
		t.Fatal("signed-out account session is still saved")
	}
	if !session.connected || !tester.tt.HasText(i18n.T("Sign in and discover")) {
		t.Fatal("sign-out did not return to the account form with the direct environment connected")
	}
}

func TestUIAccountEnvironmentInformation(t *testing.T) {
	_, ts, cfg := startEnv(t)
	tester, session := startAppAt(t, ts, cfg, 1280, 820, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the direct environment connection")
	m := tester.manager
	profile := session.profile
	profile.Name, profile.Hostname = "T3Go", "Local-Mac"
	profile.System = &proto.SystemInfo{OS: "macOS", OSVersion: "15.0", Arch: "arm64"}
	m.update(profile)
	m.accountServer = "https://relay.example.test"
	m.accountSession = proto.RelayUserSession{
		Token: "private-account-test-token", ExpiresAt: time.Now().Add(time.Hour),
		User: proto.RelayUser{Email: "test@example.com"},
	}
	m.accountEnvs = []client.RelayEnvironment{
		{EnvironmentID: profile.ID, Name: "T3Go", Online: false, DirectURLs: []string{ts.URL, ts.URL}},
		{EnvironmentID: "unconnected-environment", Name: "Remote workstation", Online: true},
	}
	session.openSettings()
	tester.pump()
	if err := tester.tt.Click(i18n.T("Account")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	for _, want := range []string{
		"Local-Mac", "macOS 15.0 · arm64", profile.ID, ts.URL,
		"https://relay.example.test/e/" + profile.ID,
		i18n.T("Relay offline"), i18n.T("Connected") + " · " + i18n.T("Direct"),
		"Remote workstation", i18n.T("Relay online"), i18n.T("Not connected"),
	} {
		if !tester.tt.HasText(want) {
			t.Fatalf("account environment information is missing %q", want)
		}
	}
	directURLs := 0
	for _, text := range tester.tt.Texts() {
		if text == ts.URL {
			directURLs++
		}
		if text != "" && (text == m.accountSession.Token || text == profile.Token) {
			t.Fatal("environment information exposed credentials instead of connection metadata")
		}
	}
	if directURLs != 1 {
		t.Fatalf("direct address rendered %d times, want 1", directURLs)
	}
	// Opening the current environment from Account returns to its workspace.
	label := i18n.T("Open environment") + " Local-Mac"
	if err := tester.tt.Click(label); err != nil {
		tester.tt.Scroll(1000, 570, 0, 280)
		if err := tester.tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if session.showSettings || m.active != session {
		t.Fatal("Open environment did not return to the local workspace")
	}
}
