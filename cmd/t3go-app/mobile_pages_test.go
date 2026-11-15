package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

// Keep the fixture's real state/events/create endpoints while substituting
// external CLI operations and controlling delayed HTTP completions.
func interceptMobileFixture(t *testing.T, f *settingsFixture, intercept func(http.ResponseWriter, *http.Request) bool) {
	t.Helper()
	target, err := url.Parse(f.profile.Routes[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !intercept(w, r) {
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(func() { ts.CloseClientConnections(); ts.Close() })
	f.profile.Routes = []client.Route{{URL: ts.URL, Kind: "direct"}}
}

func startMobilePages(t *testing.T, fixtures ...*settingsFixture) (*nsgotest.App, *environmentManager) {
	t.Helper()
	var profiles []client.EnvironmentProfile
	for _, f := range fixtures {
		profiles = append(profiles, f.profile)
	}
	var manager *environmentManager
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		manager = newEnvironmentManager(cx)
		manager.start()
		return manager.view(cx)
	}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{
		OS: core.OSAndroid, Idiom: core.IdiomPhone, SafeArea: core.Edges{Top: 24, Bottom: 16},
	}), nsgotest.Storage(map[string]string{"environments:v1": workspaceRegistry(profiles...)}))
	for _, f := range fixtures {
		id := "same-thread"
		if len(fixtures) > 1 {
			id = f.profile.ID + "/" + id
		}
		app.WaitFor("id:thread-"+id, 10*time.Second)
	}
	return app, manager
}

func TestUIMobileNewThreadTargetsEnvironment(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	var alphaCreates, betaCreates, alphaSends atomic.Int32
	sent := make(chan string, 4)
	started, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	releaseCreate := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseCreate)
	interceptMobileFixture(t, alpha, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/threads" {
			alphaCreates.Add(1)
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") {
			alphaSends.Add(1)
			_ = json.NewEncoder(w).Encode(proto.Run{ID: "unexpected"})
			return true
		}
		return false
	})
	interceptMobileFixture(t, beta, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/threads" {
			betaCreates.Add(1)
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") {
			var body struct{ Content string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			sent <- body.Content
			_ = json.NewEncoder(w).Encode(proto.Run{ID: "fake-run"})
			return true
		}
		return false
	})
	app, manager := startMobilePages(t, alpha, beta)
	app.Press("id:thread-" + alpha.profile.ID + "/same-thread")
	app.WaitFor("label:Message", 5*time.Second)
	app.Type("label:Message", "Keep Alpha conversation")
	app.Press("label:Back to threads")
	app.Press("id:new-thread")
	app.WaitFor("label:New thread message", 5*time.Second)
	app.Type("label:New thread message", "Alpha new draft")
	fieldID := app.Find("label:New thread message").ID
	app.SetEnvironment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone, KeyboardHeight: 300,
		SafeArea: core.Edges{Top: 24, Bottom: 16}})
	if field := app.Find("label:New thread message"); field.ID != fieldID || !field.Focused || field.Text != "Alpha new draft" {
		t.Fatalf("IME replaced the new thread input: %+v", field)
	}
	app.Press("id:new-thread-environment")
	app.Press("id:new-thread-environment-" + beta.profile.ID)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:New thread message").Text == "" })
	app.Type("label:New thread message", "Build in Beta")
	app.Press("id:new-thread-environment")
	app.Press("id:new-thread-environment-" + alpha.profile.ID)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:New thread message").Text == "Alpha new draft" })
	app.Press("id:new-thread-environment")
	app.Press("id:new-thread-environment-" + beta.profile.ID)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:New thread message").Text == "Build in Beta" })
	app.Press("id:composer-send")
	app.WaitUntil(5*time.Second, func() bool { return len(started) > 0 })
	if app.Enabled("id:cancel-thread") || app.Enabled("label:New thread message") || app.Enabled("id:composer-send") {
		t.Fatal("creation allowed cancellation, editing, or duplicate submission")
	}
	releaseCreate()
	app.WaitForGone("id:new-thread-page", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool { return len(sent) > 0 })
	if got := <-sent; got != "Build in Beta" {
		t.Fatalf("sent prompt = %q", got)
	}
	if alphaCreates.Load() != 0 || alphaSends.Load() != 0 || betaCreates.Load() != 1 {
		t.Fatal("create or first message reached the wrong environment")
	}
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:current-environment-name").Text == "Beta" })
	app.Press("label:Back to threads")
	app.Press("id:thread-" + alpha.profile.ID + "/same-thread")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Keep Alpha conversation" })
	app.Press("label:Back to threads")
	app.Press("id:new-thread")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:New thread message").Text == "Alpha new draft" })
	app.Back()
	app.WaitForGone("id:new-thread-page", 5*time.Second)
	app.Post(func() {
		if manager.app.profile.ID != alpha.profile.ID {
			t.Error("cancel changed the active conversation environment")
		}
	})
	app.Settle()
	if alphaCreates.Load() != 0 || betaCreates.Load() != 1 {
		t.Fatal("cancel created a thread")
	}
}

func TestUIMobileUsageMetricsAndEnvironment(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	var failBeta atomic.Bool
	for _, fixture := range []*settingsFixture{alpha, beta} {
		f := fixture
		interceptMobileFixture(t, f, func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path != "/api/v1/usage" {
				return false
			}
			if f == beta && failBeta.Load() {
				http.Error(w, "Beta usage unavailable", http.StatusServiceUnavailable)
				return true
			}
			report := proto.UsageReport{Since: "2026-11-01", DaysCount: 30,
				Totals: proto.UsageTotals{Runs: 1, Tokens: 1000, Cost: 2},
				Days: []proto.UsageDay{{Date: "2026-11-01", Tokens: 900, Cost: 0.5},
					{Date: "2026-11-02", Tokens: 100, Cost: 1.5}},
				Providers: []proto.UsageProvider{{Provider: "claude", Tokens: 1000, Cost: 2, Runs: 1}}}
			if f == beta {
				report.Totals.Tokens, report.Totals.Cost = 3000, 6
				report.Days = []proto.UsageDay{{Date: "2026-11-01", Tokens: 3000, Cost: 6}}
				report.Providers = []proto.UsageProvider{{Provider: "codex", Tokens: 3000, Cost: 6, Runs: 1}}
			}
			_ = json.NewEncoder(w).Encode(report)
			return true
		})
	}
	app, _ := startMobilePages(t, alpha, beta)
	app.Press("id:mobile-tab-usage")
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:mobile-usage-total") && app.Find("id:mobile-usage-total").Text == "$8.00"
	})
	if app.Exists("id:new-thread") {
		t.Fatal("Usage exposes the thread creation action")
	}
	costHeight := app.Find("id:mobile-usage-bar-2026-11-02").Frame.Size.Height
	app.Press("id:usage-metric-tokens")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:mobile-usage-total").Text == "4.0k" })
	if tokensHeight := app.Find("id:mobile-usage-bar-2026-11-02").Frame.Size.Height; tokensHeight >= costHeight {
		t.Fatal("token chart still uses the cost series")
	}
	app.WaitFor("id:usage-provider-codex >> text:75.0% · 3.0k tokens · 1 runs", 5*time.Second)
	app.Press("id:mobile-usage-environment")
	app.Press("id:mobile-usage-environment-" + alpha.profile.ID)
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("id:mobile-usage-total") && app.Find("id:mobile-usage-total").Text == "1.0k"
	})
	if app.Exists("id:usage-provider-codex") {
		t.Fatal("provider from another environment leaked into filtered report")
	}
	app.Press("id:usage-metric-limits")
	app.WaitFor("text:Provider limits", 5*time.Second)
	if app.Exists("id:mobile-usage-total") {
		t.Fatal("limits shows an invented account quota")
	}
	app.Press("id:usage-metric-cost")
	failBeta.Store(true)
	app.Press("id:mobile-usage-environment")
	app.Press("id:mobile-usage-environment-all")
	app.WaitFor("text:Beta: Beta usage unavailable", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:mobile-usage-total").Text == "$2.00" })
	app.Back()
	app.WaitFor("id:new-thread", 5*time.Second)
}

func TestUIMobilePullRequestsFiltersAndStaleResponse(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	now := time.Now()
	// A distinct checkout can report the same PR; the list should deduplicate it.
	if err := alpha.server.Store.CreateThread(context.Background(), proto.Thread{ID: "another-thread",
		ProjectID: "same-project", Title: "Another checkout", Provider: "claude", Model: "test", Branch: "other",
		Status: proto.ThreadIdle, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	var holdBeta atomic.Bool
	started, release := make(chan struct{}, 4), make(chan struct{})
	var releaseOnce sync.Once
	releaseBeta := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseBeta)
	var calls atomic.Int32
	for _, fixture := range []*settingsFixture{alpha, beta} {
		f := fixture
		interceptMobileFixture(t, f, func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasSuffix(r.URL.Path, "/pull-requests") {
				return false
			}
			calls.Add(1)
			if f == beta && holdBeta.Load() {
				started <- struct{}{}
				select {
				case <-release:
				case <-r.Context().Done():
					return true
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"pullRequests": []proto.PullRequest{
				{Number: 7, Title: f.profile.Alias + " open PR", State: "OPEN", URL: "https://example.test/repo/pull/7"},
				{Number: 8, Title: f.profile.Alias + " merged PR", State: "MERGED", URL: "https://example.test/repo/pull/8"},
			}})
			return true
		})
	}
	app, manager := startMobilePages(t, alpha, beta)
	app.WaitFor("id:thread-"+alpha.profile.ID+"/another-thread", 5*time.Second)
	app.Press("id:mobile-tab-prs")
	app.WaitFor("text:Alpha open PR", 5*time.Second)
	app.WaitFor("text:Beta open PR", 5*time.Second)
	app.Post(func() {
		if got := len(manager.app.mobilePRRows.GetUntracked()); got != 4 {
			t.Errorf("deduplicated PR rows = %d, want 4", got)
		}
	})
	app.Settle()
	if app.Exists("text:Alpha merged PR") || calls.Load() != 3 || app.Exists("id:new-thread") {
		t.Fatal("wrong PR scope, state filter or navigation action")
	}
	app.Press("id:mobile-pr-state")
	app.Press("id:mobile-pr-state-merged")
	app.WaitFor("text:Alpha merged PR", 5*time.Second)
	app.WaitForGone("text:Alpha open PR", 5*time.Second)
	holdBeta.Store(true)
	app.Press("id:mobile-pr-refresh")
	app.WaitUntil(5*time.Second, func() bool { return len(started) > 0 })
	app.Press("id:mobile-pr-environment")
	app.Press("id:mobile-pr-environment-" + alpha.profile.ID)
	app.WaitFor("text:Alpha merged PR", 5*time.Second)
	releaseBeta()
	// Wait for the first request batch's UI completion, then inspect its result.
	app.WaitUntil(5*time.Second, func() bool {
		var done bool
		app.Post(func() { done = !manager.app.mobilePRBusy.GetUntracked() })
		app.Settle()
		return done
	})
	app.Post(func() {
		for _, row := range manager.app.mobilePRRows.GetUntracked() {
			if row.EnvironmentID != alpha.profile.ID {
				t.Error("stale PR completion replaced the selected environment's rows")
			}
		}
	})
	app.Settle()
}

func TestUIMobileConversationSurfacesPreserveDraft(t *testing.T) {
	alpha := newSettingsFixture(t, "Alpha")
	app, _ := startMobilePages(t, alpha)
	app.Press("id:thread-same-thread")
	app.WaitFor("label:Message", 5*time.Second)
	app.Type("label:Message", "Keep this draft")
	for _, width := range []core.Points{390, 320} {
		app.Resize(width, 844)
		for _, id := range []string{"mobile-open-files", "branch-menu", "mobile-open-terminal"} {
			frame := mobileScreenFrame(app.Find("id:" + id))
			if frame.Size.Width < 44 || frame.Size.Height < 44 || frame.Origin.X < 0 || frame.Origin.X+frame.Size.Width > width {
				t.Fatalf("conversation action %s outside %v phone: %+v", id, width, frame)
			}
		}
	}
	app.Press("id:mobile-open-files")
	app.WaitFor("id:mobile-surface", 5*time.Second)
	app.Press("id:mobile-surface-back")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Keep this draft" })
	app.Press("id:mobile-open-terminal")
	app.WaitFor("id:mobile-terminal-new", 5*time.Second)
	app.Press("id:mobile-terminal-new")
	app.WaitFor("id:mobile-surface", 5*time.Second)
	app.Back()
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Keep this draft" })
}

func TestUIMobileVisibleModelAndSettingsNavigation(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	app, _ := startMobilePages(t, alpha, beta)
	app.Press("id:settings")
	app.WaitFor("id:mobile-settings-index", 5*time.Second)
	if app.Exists("id:settings-nav-usage") {
		t.Fatal("mobile Settings still duplicates the standalone Usage page")
	}
	app.Press("id:mobile-tab-threads")
	app.Press("id:thread-" + beta.profile.ID + "/same-thread")
	app.WaitFor("id:composer-model-name", 5*time.Second)
	app.Type("label:Message", "Keep draft while choosing a model")
	for _, width := range []core.Points{390, 320} {
		app.Resize(width, 844)
		app.SetEnvironment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone, KeyboardHeight: 300,
			SafeArea: core.Edges{Top: 24, Bottom: 16}})
		picker := mobileScreenFrame(app.Find("id:composer-model"))
		send := mobileScreenFrame(app.Find("id:composer-send"))
		if picker.Size.Height < 44 || picker.Origin.X < 0 || picker.Origin.X+picker.Size.Width > width ||
			send.Origin.Y+send.Size.Height > 844-300 {
			t.Fatalf("picker or send button outside the phone/keyboard area: picker=%v send=%v", picker, send)
		}
	}
	app.Press("id:composer-model")
	app.WaitFor("id:composer-model-Beta-model", 5*time.Second)
	app.Advance(300 * time.Millisecond)
	if app.Exists("id:composer-model-Alpha-model") {
		t.Fatal("models leaked from another environment")
	}
	app.Press("id:composer-model-Beta-model")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:composer-model-name").Text == "Beta model" })
	if input := app.Find("label:Message"); input.Text != "Keep draft while choosing a model" || !app.Exists("id:composer") {
		t.Fatal("model selection replaced the editor or lost its draft")
	}
	thread, err := beta.server.Store.Thread(context.Background(), "same-thread")
	if err != nil || thread.Model != "Beta-model" {
		t.Fatalf("model was not saved on Beta: %+v, %v", thread, err)
	}
	thread, err = alpha.server.Store.Thread(context.Background(), "same-thread")
	if err != nil || thread.Model != "test" {
		t.Fatal("choosing a model changed Alpha")
	}
	// Only two lines in the title block; connection mode is an icon on
	// the environment line rather than a third line of text.
	env := mobileScreenFrame(app.Find("id:current-environment-name"))
	status := app.Find("id:connection-status")
	statusFrame := mobileScreenFrame(status)
	if status.Text != "" || status.Props.Label != "Direct" || statusFrame.Origin.Y >= env.Origin.Y+env.Size.Height ||
		statusFrame.Origin.Y+statusFrame.Size.Height <= env.Origin.Y {
		t.Fatalf("connection is not an icon on the environment line: %+v, %v", status, statusFrame)
	}
	app.Press("id:thread-actions")
	app.Advance(300 * time.Millisecond)
	app.Press("id:settings")
	app.WaitFor("id:settings-dialog", 5*time.Second)
	if app.Exists("id:settings-nav-usage") {
		t.Fatal("conversation Settings still duplicates Usage")
	}
}
