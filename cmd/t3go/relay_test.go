package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/relay"
	"github.com/tk/t3go/internal/store"
)

func TestRelayCLIRegistrationPairingAndRevocation(t *testing.T) {
	t.Setenv("T3GO_TOKEN", "")
	const admin = "relay-cli-administrator-token"
	hub, err := relay.New(t.TempDir(), admin)
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	mux := http.NewServeMux()
	hub.Mount(mux)
	cloud := httptest.NewServer(mux)
	defer cloud.Close()
	home := t.TempDir()
	adminFile := filepath.Join(t.TempDir(), "admin-token")
	if err := os.WriteFile(adminFile, []byte(admin), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--home", home, "--server", cloud.URL, "--admin-token-file", adminFile}
	if code := cmdRelay(args); code != 1 {
		t.Fatal("HTTP registration did not require an explicit override")
	}
	args = append(args, "--allow-http", "--direct-url", "http://192.168.1.20:4870")
	if code := cmdRelay(args); code != 0 {
		t.Fatalf("registration exited %d", code)
	}
	credentials, err := loadRelay(home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(config.DBPath(home))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := st.EnvironmentIdentity(context.Background())
	st.Close()
	if err != nil || credentials.EnvironmentID != identity.ID {
		t.Fatal("registration did not use the persisted identity", err)
	}
	cfg, err := config.Load(home, "", false)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := client.PairingProfile(relayPairing(credentials, cfg.Token), "")
	if err != nil || profile.ID != identity.ID || profile.PublicKey != identity.PublicKey() ||
		len(profile.Routes) != 2 || !strings.Contains(profile.Routes[1].URL, "/e/"+identity.ID) {
		t.Fatal("invalid multi-route CLI pairing", err)
	}
	if err := startRelayConnector(context.Background(), home, "different-identity", "127.0.0.1:4870", nil); err == nil {
		t.Fatal("connector accepted another environment's saved credentials")
	}
	if code := cmdRelay(append(args, "--revoke", identity.ID)); code != 0 {
		t.Fatalf("revoke exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(home, "relay.json")); !os.IsNotExist(err) {
		t.Fatal("revoked local credentials were retained", err)
	}
	// A revocation leaves the environment available for a fresh registration.
	if code := cmdRelay(args); code != 0 {
		t.Fatalf("fresh registration after revoke exited %d", code)
	}
}

func TestRelayCannotExposeUnauthenticatedEnvironment(t *testing.T) {
	for _, mode := range []string{"--relay", "--connect-relay"} {
		if code := cmdServe([]string{"--home", t.TempDir(), "--no-auth", mode}); code != 1 {
			t.Fatalf("%s accepted --no-auth", mode)
		}
	}
}

func TestRelayCLIUsesUserToken(t *testing.T) {
	hub, err := relay.New(t.TempDir(), "relay-user-cli-administrator")
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	mux := http.NewServeMux()
	hub.Mount(mux)
	cloud := httptest.NewServer(mux)
	defer cloud.Close()

	body := strings.NewReader(`{"email":"cli@example.com","password":"alice-password","name":"CLI user"}`)
	req, _ := http.NewRequest(http.MethodPost, cloud.URL+"/api/v1/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register user = %d", resp.StatusCode)
	}
	var session relay.UserSession
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil || len(session.Token) < 20 {
		t.Fatalf("decode user session: %v", err)
	}
	tokenFile := filepath.Join(t.TempDir(), "user-token")
	if err := os.WriteFile(tokenFile, []byte(session.Token), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if code := cmdRelay([]string{"--home", home, "--server", cloud.URL,
		"--user-token-file", tokenFile, "--allow-http"}); code != 0 {
		t.Fatalf("user-token registration exited %d", code)
	}
	if credentials, err := loadRelay(home); err != nil || credentials.ConnectToken == "" {
		t.Fatalf("user-token relay credentials = %#v, %v", credentials, err)
	}
}
