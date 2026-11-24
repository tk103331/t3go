package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/relay"
)

// Use the real account service, signed environment registration, reverse
// tunnel and environment APIs. Only the model provider is deterministic.
func TestUIRelayAccountToConversation(t *testing.T) {
	t.Setenv("T3GO_SERVER_BIN", filepath.Join(t.TempDir(), "no-bundled-server"))
	hub, err := relay.New(t.TempDir(), "local-flow-administrator-token")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	hub.Mount(mux)
	var registrations, logins, routed, sockets, streams, creates, sends atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/register":
			registrations.Add(1)
		case "/api/v1/auth/login":
			logins.Add(1)
		}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			sockets.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/e/") {
			routed.Add(1)
			if strings.HasSuffix(r.URL.Path, "/api/v1/events") {
				streams.Add(1)
			}
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/v1/threads") {
				creates.Add(1)
			}
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") {
				sends.Add(1)
			}
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { hub.Close(); cloud.CloseClientConnections(); cloud.Close() })

	srv, localHTTP, cfg := startEnv(t)
	// ConfigureRelay targets the server's own listener, just as in the app.
	if err := srv.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	settings := srv.Service.Settings(ctx)
	settings.AccessEnabled = true
	settings.DefaultProvider, settings.DefaultModel = "side-fake", "side-fake-1"
	if err := srv.Service.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	srv.Service.Providers.Register(sideRunner{})
	project, err := srv.Service.CreateProject(ctx, "Relay flow project", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("isolated local services: Relay %s; environment http://%s", cloud.URL, srv.Addr())

	click := func(tester *Tester, label string) {
		t.Helper()
		if err := tester.tt.Click(i18n.T(label)); err != nil {
			t.Fatalf("click %s: %v", label, err)
		}
		tester.pump()
	}
	fill := func(tester *Tester, label, value string) {
		t.Helper()
		click(tester, label)
		tester.tt.Type(value)
	}
	const email = "local-flow@example.com"
	const password = "local-flow-test-password"
	host, local := startAppAt(t, localHTTP, cfg, 1440, 1000, nil)
	host.wait(t, 15*time.Second, func() bool { return local.connected && len(local.projects) == 1 }, "local environment")
	local.openSettings()
	host.pump()
	click(host, "Account")
	click(host, "Register")
	fill(host, "Relay server URL", cloud.URL)
	fill(host, "Email", email)
	fill(host, "Password", password)
	fill(host, "Display name", "Local flow tester")
	click(host, "Allow insecure HTTP")
	click(host, "Create account")
	host.wait(t, 15*time.Second, func() bool {
		return host.manager.accountSessionValid() && !host.manager.accountBusy
	}, "real Relay account registration")
	if registrations.Load() != 1 {
		t.Fatal("account registration did not reach Relay")
	}
	click(host, "Sign out")
	click(host, "Sign in")
	fill(host, "Password", password)
	click(host, "Sign in and discover")
	host.wait(t, 15*time.Second, func() bool {
		return host.manager.accountSessionValid() && !host.manager.accountBusy
	}, "real Relay account login")
	t.Log("PASS: registered a test account, signed out, and signed back in through Account settings")

	click(host, "Access")
	host.tt.Scroll(1000, 650, 0, 300)
	click(host, "Connect Relay")
	host.wait(t, 15*time.Second, func() bool {
		return !local.relayBusy && local.relayAccess.Configured && local.relayAccess.State == "active"
	}, "signed environment registration and online reverse tunnel")
	credentials, err := os.ReadFile(filepath.Join(cfg.Home, "relay.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(credentials), host.manager.accountSession.Token) {
		t.Fatal("environment credentials contain the account token")
	}
	t.Log("PASS: Connect Relay registered this environment and brought the real WebSocket tunnel online")

	// A second client starts with no profiles, account session or environment
	// token. Its only way to find this machine is account discovery via Relay.
	post := &testPost{}
	mgr := newManagerForTest(post, memoryStorage(nil))
	mgr.start()
	remote := &Tester{post: post, manager: mgr}
	remote.tt = ui.NewTester(func(c *ui.Context) {
		compactView = false
		mgr.view(c)
	}, 1440, 1000)
	t.Cleanup(func() {
		for _, session := range mgr.sessions {
			session.disposeEnvironmentSession()
		}
	})
	click(remote, "Sign in to Relay")
	fill(remote, "Relay server URL", cloud.URL)
	fill(remote, "Email", email)
	fill(remote, "Password", password)
	click(remote, "Allow insecure HTTP")
	click(remote, "Sign in and discover")
	remote.wait(t, 20*time.Second, func() bool {
		return mgr.active != nil && mgr.active.connected && mgr.active.profile.ID == srv.Identity.ID && !mgr.accountBusy
	}, "fresh client to discover and connect the environment through Relay")
	connected := mgr.active
	if connected.profile.AccountServer != cloud.URL || connected.profile.Preference != "relay-only" || connected.profile.Token == cfg.Token {
		t.Fatal("fresh client did not use account credentials and a Relay-only profile")
	}
	if !strings.HasPrefix(connected.client().BaseURL, cloud.URL+"/e/") || len(mgr.profiles) != 1 {
		t.Fatal("fresh client connected outside the discovered Relay route")
	}
	if !remote.tt.HasText(i18n.T("Discovered environments")) {
		t.Fatal("account dialog does not show its discovered environments")
	}
	openLabel := i18n.T("Open environment") + " " + mgr.environmentNames()[srv.Identity.ID]
	if err := remote.tt.Click(openLabel); err != nil {
		remote.tt.Scroll(800, 650, 0, 240)
		if err := remote.tt.Click(openLabel); err != nil {
			t.Fatalf("open discovered environment: %v", err)
		}
	}
	remote.pump()
	if mgr.accountOpen || mgr.active != connected {
		t.Fatal("opening the environment did not close Account and show its workspace")
	}
	t.Log("PASS: fresh client logged in, discovered the owned environment, and connected using Relay-only account access")

	click(remote, "New thread")
	remote.wait(t, 10*time.Second, func() bool {
		return connected.showNewThread && connected.newThreadProj == project.ID && len(connected.providers) > 0
	}, "new-thread page with the discovered project and providers")
	fill(remote, "New thread message", "Verify the full Relay conversation flow")
	t.Logf("new thread: provider=%q, model=%q, promptLength=%d", connected.newThreadProv, connected.newThreadModel, len(connected.newThreadPrompt))
	click(remote, "Create thread")
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("conversation failure: selected=%q, busy=%v, connected=%v, error=%q, creates=%d, sends=%d, messages=%d",
				connected.selected, connected.threadBusy, connected.connected, connected.connErr, creates.Load(), sends.Load(), len(connected.messages))
			for _, message := range connected.messages {
				t.Logf("visible message: role=%s status=%s content=%q", message.Role, message.Status, message.Content)
			}
			stored, _ := srv.Store.Messages(ctx, connected.selected, 100)
			t.Logf("stored messages=%d, eventSeq=%d, running=%v", len(stored), connected.eventSeq, connected.running)
		}
	})
	remote.wait(t, 20*time.Second, func() bool {
		return connected.connErr != "" || connected.detail.Thread.ID != "" && !connected.threadBusy && remote.tt.HasText("side reply")
	}, "thread creation and streamed model reply over Relay")
	if connected.connErr != "" {
		t.Fatalf("new thread: %s", connected.connErr)
	}
	threadID := connected.detail.Thread.ID
	fill(remote, "Message", "Verify the second message over Relay")
	click(remote, "Send")
	remote.wait(t, 15*time.Second, func() bool {
		answers := 0
		for _, message := range connected.messages {
			if message.Role == proto.RoleAssistant && message.Content == "side reply" {
				answers++
			}
		}
		return answers == 2 && !connected.running
	}, "second message and streamed reply")
	threads, err := srv.Store.Threads(ctx, project.ID, false)
	if err != nil || len(threads) != 1 || threads[0].ID != threadID {
		t.Fatal("the environment did not persist exactly one new thread")
	}
	messages, err := srv.Store.Messages(ctx, threadID, 100)
	if err != nil || len(messages) != 4 {
		t.Fatalf("persisted message count = %d, want two user/assistant exchanges", len(messages))
	}
	if creates.Load() != 1 || sends.Load() != 2 || sockets.Load() < 1 || streams.Load() < 1 || logins.Load() != 2 {
		t.Fatalf("unexpected Relay requests: logins=%d, creates=%d, sends=%d, WebSockets=%d, event streams=%d",
			logins.Load(), creates.Load(), sends.Load(), sockets.Load(), streams.Load())
	}
	t.Logf("PASS: persisted thread %s, 4 messages; Relay observed %d routed requests, %d WebSocket tunnels, %d event streams, 1 thread create and 2 sends",
		threadID, routed.Load(), sockets.Load(), streams.Load())
}
