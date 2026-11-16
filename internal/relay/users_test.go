package relay

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

	"github.com/tk/t3go/internal/environment"
)

func TestUserAccountsOwnEnvironmentsAndAuthorizeRelayAccess(t *testing.T) {
	const admin = "relay-user-system-administrator"
	const alicePassword = "alice-password"
	const environmentToken = "environment-access-token-for-user-test"
	home := t.TempDir()
	hub, err := New(home, admin)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	hub.Mount(mux)
	server := httptest.NewServer(mux)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		hub.Close()
		server.Close()
	})
	request := func(method, path, token string, body any) *http.Response {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequest(method, server.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	account := func(email, password, name string) UserSession {
		t.Helper()
		resp := request(http.MethodPost, "/api/v1/auth/register", "",
			UserCredentials{Email: email, Password: password, Name: name})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("register %s = %d: %s", email, resp.StatusCode, raw)
		}
		var session UserSession
		if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
			t.Fatal(err)
		}
		if len(session.Token) < 20 || session.User.ID == "" || session.User.Email != email {
			t.Fatalf("invalid user session: %#v", session)
		}
		return session
	}

	alice := account("alice@example.com", alicePassword, "Alice")
	bob := account("bob@example.com", "bob-password", "Bob")
	resp := request(http.MethodPost, "/api/v1/auth/register", "",
		UserCredentials{Email: "ALICE@example.com", Password: "another-password"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate account = %d", resp.StatusCode)
	}
	resp = request(http.MethodPost, "/api/v1/auth/login", "",
		UserCredentials{Email: "alice@example.com", Password: "wrong-password"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d", resp.StatusCode)
	}
	for range maxLoginFailures {
		resp = request(http.MethodPost, "/api/v1/auth/login", "",
			UserCredentials{Email: "missing@example.com", Password: "wrong-password"})
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failed login before limit = %d", resp.StatusCode)
		}
	}
	resp = request(http.MethodPost, "/api/v1/auth/login", "",
		UserCredentials{Email: "missing@example.com", Password: "wrong-password"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate-limited login = %d", resp.StatusCode)
	}
	resp = request(http.MethodPost, "/api/v1/auth/login", "",
		UserCredentials{Email: "alice@example.com", Password: alicePassword})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	var login UserSession
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if login.User.ID != alice.User.ID || login.Token == alice.Token {
		t.Fatal("login did not issue a fresh session for the same user")
	}

	identity, err := environment.Generate()
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "registered through user token")
	}))
	t.Cleanup(local.Close)
	unsigned := Registration{
		ID: identity.ID, PublicKey: identity.PublicKey(), Name: "Alice laptop",
		Token: environmentToken,
	}
	resp = request(http.MethodPost, "/api/v1/relay/environments", login.Token, unsigned)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsigned user registration = %d", resp.StatusCode)
	}
	if err := unsigned.Sign(identity.PrivateKey); err != nil {
		t.Fatal(err)
	}
	resp = request(http.MethodPost, "/api/v1/relay/environments", login.Token, unsigned)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("user environment registration = %d: %s", resp.StatusCode, raw)
	}
	var registered Registered
	if err := json.NewDecoder(resp.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp = request(http.MethodGet, "/api/v1/relay/environments", bob.Token, nil)
	var bobList struct {
		Environments []map[string]any `json:"environments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bobList); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(bobList.Environments) != 0 {
		t.Fatalf("another user can list the environment: %#v", bobList.Environments)
	}
	resp = request(http.MethodGet, "/api/v1/relay/environments", login.Token, nil)
	var aliceList struct {
		Environments []map[string]any `json:"environments"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&aliceList); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(aliceList.Environments) != 1 {
		t.Fatalf("owner environment list = %#v", aliceList.Environments)
	}

	connector := Connector{Server: server.URL, EnvironmentID: identity.ID,
		Token: registered.ConnectToken, Target: strings.TrimPrefix(local.URL, "http://")}
	go connector.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		hub.mu.Lock()
		online := hub.peers[identity.ID] != nil && !hub.peers[identity.ID].session.IsClosed()
		hub.mu.Unlock()
		if online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("user-owned environment tunnel never connected")
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp = request(http.MethodGet, "/e/"+identity.ID+"/hello", bob.Token, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("relay user token unexpectedly accessed an environment API = %d", resp.StatusCode)
	}
	resp = request(http.MethodGet, "/e/"+identity.ID+"/hello", environmentToken, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "registered through user token" {
		t.Fatalf("environment token proxy access = %d %q", resp.StatusCode, body)
	}
	resp = request(http.MethodDelete, "/api/v1/relay/environments/"+identity.ID, bob.Token, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-owner delete = %d", resp.StatusCode)
	}
	resp = request(http.MethodDelete, "/api/v1/relay/environments/"+identity.ID, login.Token, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner delete = %d", resp.StatusCode)
	}

	raw, err := os.ReadFile(filepath.Join(home, "relay-users.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, "relay-users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("relay-users.json mode = %o, want 600", info.Mode().Perm())
	}
	for _, secret := range []string{alicePassword, alice.Token, login.Token} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("relay-users.json persisted plaintext secret %q", secret)
		}
	}
}
