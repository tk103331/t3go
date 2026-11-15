package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/server"
)

type settingsFixture struct {
	profile client.EnvironmentProfile
	server  *server.Server
	probes  atomic.Int32
	usage   atomic.Int32
	writes  atomic.Int32
	inputs  atomic.Int32
	reject  atomic.Bool
	hold    atomic.Bool
	release chan struct{}
}

func newSettingsFixture(t *testing.T, name string) *settingsFixture {
	t.Helper()
	srv, _, cfg := startEnv(t)
	f := &settingsFixture{server: srv, release: make(chan struct{}, 1)}
	now := time.Now()
	root := "/" + name + "/worktree"
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	p := proto.Project{ID: "same-project", Name: name + " project", Path: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	th := proto.Thread{ID: "same-thread", ProjectID: p.ID, Title: name + " thread", Provider: "claude", Model: "test",
		Status: proto.ThreadIdle, Permission: proto.PermissionFullAccess, WorktreePath: root, CreatedAt: now, UpdatedAt: now}
	if err := srv.Store.CreateProject(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := srv.Store.CreateThread(context.Background(), th); err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/state":
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				w.WriteHeader(rec.Code)
				_, _ = w.Write(rec.Body.Bytes())
				return
			}
			var state proto.State
			if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
				t.Error(err)
				return
			}
			state.Providers = []proto.Provider{{ID: "claude", Name: name + " CLI", Installed: true,
				CanAuthenticate: true, AuthStatus: "unauthenticated", LoginCommand: "test-login-" + name}}
			state.Models = []proto.Model{{ID: name + "-model", Name: name + " model", Provider: "claude"}}
			for i := range state.Threads {
				state.Threads[i].WorktreePath = root
			}
			_ = json.NewEncoder(w).Encode(state)
			return
		case r.URL.Path == "/api/v1/environment":
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				w.WriteHeader(rec.Code)
				_, _ = w.Write(rec.Body.Bytes())
				return
			}
			_, _ = w.Write(rec.Body.Bytes())
			return
		case r.URL.Path == "/api/v1/providers":
			f.probes.Add(1)
			if f.hold.Load() {
				select {
				case <-f.release:
				case <-r.Context().Done():
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": []proto.Provider{{ID: "claude", Name: name + " CLI", Installed: true,
					CanAuthenticate: true, AuthStatus: "unauthenticated", LoginCommand: "test-login-" + name}},
				"models": []proto.Model{{ID: name + "-model", Name: name + " model", Provider: "claude"}},
			})
			return
		case r.URL.Path == "/api/v1/usage":
			f.usage.Add(1)
			tokens := int64(1500)
			if name == "Beta" {
				tokens = 2500
			}
			_ = json.NewEncoder(w).Encode(proto.UsageReport{DaysCount: 30,
				Totals:    proto.UsageTotals{Runs: 1, Tokens: tokens},
				Providers: []proto.UsageProvider{{Provider: name, Tokens: tokens, Runs: 1}}})
			return
		case r.URL.Path == "/api/v1/threads/same-thread/working-diff":
			_ = json.NewEncoder(w).Encode(proto.WorkingDiff{
				Patch: "diff --git a/README.md b/README.md\n@@ -1 +1 @@\n-old\n+new\n",
				Files: []proto.DiffFile{{Path: "README.md", Additions: 1, Deletions: 1}},
			})
			return
		case r.URL.Path == "/api/v1/threads/same-thread/files" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": []proto.WorkspaceEntry{{Name: "long.txt", Path: "long.txt"}},
			})
			return
		case r.URL.Path == "/api/v1/threads/same-thread/file" && r.Method == http.MethodGet:
			lines := make([]string, 200)
			for i := range lines {
				lines[i] = fmt.Sprintf("line %d", i+1)
			}
			_ = json.NewEncoder(w).Encode(proto.WorkspaceFile{
				Path: "long.txt", Size: int64(len(strings.Join(lines, "\n"))), Content: strings.Join(lines, "\n"),
			})
			return
		case r.URL.Path == "/api/v1/threads/same-thread/branches" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"branches": []proto.Branch{{Name: "main", Current: true}}})
			return
		case r.URL.Path == "/api/v1/threads/same-thread" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(proto.ThreadDetail{
				Thread: th,
				Git:    &proto.GitStatus{Repo: true, Root: "/Alpha/repo", Branch: "main", Dirty: true},
			})
			return
		case r.URL.Path == "/api/v1/threads/same-thread/git" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") == "" {
				t.Error("git action was sent without credentials")
			}
			_ = json.NewEncoder(w).Encode(proto.GitActionResult{
				Action: proto.GitCommitPush, Branch: "main", CommitSHA: "1234567890", Pushed: true,
				Status: proto.GitStatus{Repo: true, Root: "/Alpha/repo", Branch: "main"},
			})
			return
		case r.URL.Path == "/api/v1/settings" && r.Method == http.MethodPatch:
			f.writes.Add(1)
			if f.reject.Load() {
				http.Error(w, "settings temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
		case r.URL.Path == "/api/v1/threads/same-thread/terminal":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "fake-terminal"})
			return
		case r.URL.Path == "/api/v1/threads/same-thread/terminals" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"sessions": []proto.TerminalSession{
				{ID: "fake-terminal", ThreadID: "same-thread", Title: "Terminal 1"},
			}})
			return
		case r.URL.Path == "/api/v1/threads/same-thread/terminals" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(proto.TerminalSession{ID: "fake-terminal", ThreadID: "same-thread", Title: "Terminal 1"})
			return
		case r.URL.Path == "/api/v1/terminals/fake-terminal/input":
			f.inputs.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { proxy.CloseClientConnections(); proxy.Close() })
	f.profile = client.EnvironmentProfile{ID: srv.Identity.ID, Alias: name, Token: cfg.Token,
		PublicKey: srv.Identity.PublicKey(), Routes: []client.Route{{URL: proxy.URL, Kind: "direct"}}}
	return f
}

func TestUISettingsEnvironmentIsolation(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	var manager *environmentManager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = newEnvironmentManager(cx)
		manager.start()
		return manager.view(cx)
	}, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(alpha.profile, beta.profile), "editorChoice": "zed",
	}))
	for _, f := range []*settingsFixture{alpha, beta} {
		app.WaitFor("id:thread-"+f.profile.ID+"/same-thread", 10*time.Second)
	}
	app.Press("id:thread-" + alpha.profile.ID + "/same-thread")
	app.WaitFor("label:Message", 5*time.Second)
	app.Type("label:Message", "Keep Alpha draft")
	app.Press("id:settings")
	app.WaitFor("label:Environment name", 5*time.Second)
	app.Clear("label:Environment name")
	app.Type("label:Environment name", "Alpha draft name")
	app.Press("id:settings-environment-" + beta.profile.ID)
	app.Clear("label:Environment name")
	app.Type("label:Environment name", "Beta saved name")
	app.Press("id:settings-editor-cursor")
	app.Press("id:settings-environment-" + alpha.profile.ID)
	if got := app.Find("label:Environment name").Text; got != "Alpha draft name" {
		t.Fatalf("Alpha settings draft lost: %q", got)
	}
	app.Press("id:settings-environment-" + beta.profile.ID)
	if got := app.Find("label:Environment name").Text; got != "Beta saved name" {
		t.Fatalf("Beta settings draft lost: %q", got)
	}
	// A failed B save stays visible in A's window and leaves stored values intact.
	beta.reject.Store(true)
	app.Press("id:settings-save")
	app.WaitFor("text:settings temporarily unavailable", 5*time.Second)
	if !app.Exists("id:settings-dialog") || beta.server.Service.Settings(context.Background()).Title == "Beta saved name" {
		t.Fatal("failed save dismissed the dialog or changed server defaults")
	}
	beta.reject.Store(false)
	app.Press("id:settings-save")
	waitSheetGone(app, "id:settings-dialog")
	if alpha.writes.Load() != 0 || beta.writes.Load() != 2 ||
		beta.server.Service.Settings(context.Background()).Title != "Beta saved name" {
		t.Fatal("settings writes reached the wrong environment")
	}
	if got := app.Find("label:Message").Text; got != "Keep Alpha draft" {
		t.Fatalf("settings target changed conversation: %q", got)
	}
	for _, f := range []*settingsFixture{alpha, beta} {
		want := "zed"
		if f == beta {
			want = "cursor"
		}
		if got, _ := app.Storage().GetItem("env:" + f.profile.ID + ":editorChoice"); got != want {
			t.Fatalf("editor choice for %s = %q, want %q", f.profile.Alias, got, want)
		}
	}
	app.Press("id:settings")
	app.Press("id:settings-nav-providers")
	app.WaitFor("text:\"Alpha CLI\"", 5*time.Second)
	app.WaitFor("id:settings-providers-environment-"+alpha.profile.ID, 5*time.Second)
	app.WaitFor("id:settings-providers-environment-"+beta.profile.ID, 5*time.Second)
	if app.Exists("id:settings-providers-show-" + beta.profile.ID) {
		t.Fatal("providers page repeats the environment switch")
	}
	app.Press("id:settings-environment-" + beta.profile.ID)
	app.WaitFor("text:\"Beta CLI\"", 5*time.Second)
	if app.Exists("text:Alpha CLI") || app.Exists("text:Alpha model") {
		t.Fatal("provider details leaked from Alpha")
	}
	app.WaitUntil(5*time.Second, func() bool { return app.Enabled("id:settings-refresh-providers") })
	before := alpha.probes.Load()
	app.Press("id:settings-refresh-providers")
	app.WaitUntil(5*time.Second, func() bool { return beta.probes.Load() == 2 && app.Enabled("id:settings-refresh-providers") })
	if alpha.probes.Load() != before {
		t.Fatal("refresh on Beta probed Alpha")
	}
	app.Press("id:settings-add-model-claude")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:custom-model-environment").Text == "Environment: Beta" })
	app.Type("id:custom-model-id", "beta-custom")
	app.Type("id:custom-model-name", "Beta custom")
	app.Press("id:confirm-custom-model")
	waitSheetGone(app, "id:custom-model-dialog")
	app.WaitFor("text:Beta custom", 5*time.Second)
	app.Press("id:settings-environment-" + alpha.profile.ID)
	if app.Exists("text:Beta custom") {
		t.Fatal("custom model leaked to Alpha")
	}
	app.Press("id:settings-nav-usage")
	app.WaitFor("text:1.5k tokens", 5*time.Second)
	app.Press("id:settings-environment-" + beta.profile.ID)
	app.WaitFor("text:2.5k tokens", 5*time.Second)
	if alpha.usage.Load() != 1 || beta.usage.Load() != 1 {
		t.Fatal("usage requested from incorrect environment")
	}
	app.Press("id:settings-nav-worktrees")
	app.WaitFor("text:\"/Beta/worktree\"", 5*time.Second)
	if app.Exists("text:\"/Alpha/worktree\"") {
		t.Fatal("worktrees leaked to Beta")
	}
	app.Press("id:settings-nav-about")
	app.WaitFor("text:\""+beta.profile.Routes[0].URL+"\"", 5*time.Second)
	// Inaccessible B cannot expose cached settings or send writes to healthy A.
	betaSession := manager.sessions[beta.profile.ID]
	app.Post(func() {
		t.Logf("before: blocked=%v connected=%v offline=%v", betaSession.transportBlocked.GetUntracked(),
			betaSession.connected.GetUntracked(), betaSession.offline.GetUntracked())
		betaSession.transportBlocked.Set(true)
		t.Logf("after: blocked=%v", betaSession.transportBlocked.GetUntracked())
	})
	app.Settle()
	app.WaitFor("id:settings-target-token", 5*time.Second)
	app.Press("id:settings-nav-general")
	if app.Exists("id:settings-save") || app.Exists("label:Environment name") {
		t.Fatal("blocked environment allowed editing cached settings")
	}
	app.Post(func() {
		if manager.app.profile.ID != alpha.profile.ID {
			t.Error("settings navigation switched current conversation")
		}
	})
	app.Settle()
}

func TestUISettingsDelayedProvidersAndSetupTarget(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	alpha.hold.Store(true)
	t.Cleanup(func() { close(alpha.release) })
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(alpha.profile, beta.profile),
	}))
	app.WaitFor("id:thread-"+beta.profile.ID+"/same-thread", 10*time.Second)
	// Remember a B thread, then return to A before opening settings for B.
	app.Press("id:thread-" + beta.profile.ID + "/same-thread")
	app.WaitFor("label:Message", 5*time.Second)
	app.Press("id:thread-" + alpha.profile.ID + "/same-thread")
	app.WaitFor("label:Message", 5*time.Second)
	app.Press("id:settings")
	app.Press("id:settings-nav-providers")
	app.WaitUntil(5*time.Second, func() bool { return alpha.probes.Load() == 1 })
	app.Press("id:settings-environment-" + beta.profile.ID)
	app.WaitFor("text:\"Beta CLI\"", 5*time.Second)
	alpha.hold.Store(false)
	// The delayed response belongs only to A, regardless of the selected page.
	alpha.release <- struct{}{}
	app.WaitUntil(5*time.Second, func() bool { return app.Enabled("id:settings-refresh-providers") })
	if app.Exists("text:Alpha CLI") {
		t.Fatal("late Alpha response replaced Beta provider details")
	}
	app.Press("id:settings-login-claude")
	waitSheetGone(app, "id:settings-dialog")
	app.WaitUntil(5*time.Second, func() bool { return beta.inputs.Load() == 1 })
	if alpha.inputs.Load() != 0 {
		t.Fatal("Beta login command ran on Alpha")
	}
	app.WaitUntil(5*time.Second, func() bool {
		return strings.Contains(app.Find("id:current-environment-name").Text, "Beta")
	})
}
