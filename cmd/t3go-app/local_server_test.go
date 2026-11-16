package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tk/t3go/internal/client"
)

func TestIsLoopbackURL(t *testing.T) {
	for _, tc := range []struct {
		address string
		want    bool
	}{
		{"http://127.0.0.1:4870", true},
		{"http://localhost:4870", true},
		{"127.0.0.1:4870", true},
		{"http://[::1]:4870", true},
		{"http://192.168.1.10:4870", false},
		{"https://t3.example.com", false},
		{"", false},
	} {
		if got := isLoopbackURL(tc.address); got != tc.want {
			t.Errorf("isLoopbackURL(%q) = %v, want %v", tc.address, got, tc.want)
		}
	}
}

func TestMergeLocalRoutesReplacesTheLoopbackAddress(t *testing.T) {
	routes := []client.Route{
		{URL: "http://127.0.0.1:4870", Kind: "direct"},
		{URL: "http://192.168.1.10:4870", Kind: "direct"},
		{URL: "https://relay.example.com/e/abc", Kind: "relay"},
	}
	got := mergeLocalRoutes(routes, "http://127.0.0.1:4871")
	want := []client.Route{
		{URL: "http://127.0.0.1:4871", Kind: "direct"},
		{URL: "http://192.168.1.10:4870", Kind: "direct"},
		{URL: "https://relay.example.com/e/abc", Kind: "relay"},
	}
	if len(got) != len(want) {
		t.Fatalf("routes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("routes = %v, want %v", got, want)
		}
	}
}

func TestServerAnswersAndAcceptsToken(t *testing.T) {
	var health, session int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			health++
			w.Write([]byte(`{"ok":true,"name":"t3go"}`))
		case "/api/auth/session":
			session++
			if r.Header.Get("Authorization") != "Bearer right" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"authenticated":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	if !serverAnswers(server.URL) {
		t.Fatal("the health endpoint did not answer")
	}
	if serverAcceptsToken(server.URL, "right") != true {
		t.Fatal("the token was refused")
	}
	if serverAcceptsToken(server.URL, "wrong") != false {
		t.Fatal("a wrong token was accepted")
	}
	if health == 0 || session == 0 {
		t.Fatalf("health=%d session=%d", health, session)
	}
}

func TestFetchLocalDescriptor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"environmentId":"env-1","publicKey":"key-1","name":"Studio"}`))
	}))
	defer server.Close()
	d, err := fetchLocalDescriptor(server.URL)
	if err != nil {
		t.Fatalf("fetch the descriptor: %v", err)
	}
	if d.ID != "env-1" || d.PublicKey != "key-1" || d.Name != "Studio" {
		t.Fatalf("descriptor = %+v", d)
	}
}

// TestStartLocalServerRunsThePackagedServer builds the environment server
// and lets the client start it, the way an installed package does.
func TestStartLocalServerRunsThePackagedServer(t *testing.T) {
	if testing.Short() {
		t.Skip("the packaged server is built for this test")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "t3go")
	build := exec.Command("go", "build", "-o", bin, "../t3go")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the server: %v\n%s", err, out)
	}
	t.Setenv("T3GO_SERVER_BIN", bin)
	t.Setenv("T3GO_HOME", filepath.Join(dir, "home"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := startLocalServer(ctx)
	if err != nil {
		t.Fatalf("start the local server: %v", err)
	}
	defer func() {
		if srv.stop != nil {
			srv.stop()
		}
	}()
	if !serverAnswers(srv.baseURL) {
		t.Fatalf("the server at %s did not answer", srv.baseURL)
	}
	if srv.token == "" {
		t.Fatal("the server kept no token")
	}
	if !serverAcceptsToken(srv.baseURL, srv.token) {
		t.Fatal("the server refused its own token")
	}
	desc, err := fetchLocalDescriptor(srv.baseURL)
	if err != nil {
		t.Fatalf("read the descriptor: %v", err)
	}
	if desc.ID == "" {
		t.Fatal("the environment has no identity")
	}
	// A second start finds the running server instead of a second one.
	again, err := startLocalServer(ctx)
	if err != nil {
		t.Fatalf("start the local server again: %v", err)
	}
	if again.baseURL != srv.baseURL {
		t.Fatalf("second start used %s, want %s", again.baseURL, srv.baseURL)
	}
}

func TestBundledServerPathNeedsTheBinary(t *testing.T) {
	t.Setenv("T3GO_SERVER_BIN", "")
	if path := bundledServerPath(); path != "" {
		if _, err := os.Stat(path); err == nil {
			t.Skip("this machine has a server next to the test binary")
		}
	}
}
