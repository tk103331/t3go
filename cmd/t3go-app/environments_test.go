package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func TestUIMultipleEnvironmentsKeepDraftsAndThreadsSeparate(t *testing.T) {
	a, ta, ca := startEnv(t)
	b, tb, cb := startEnv(t)
	ctx := context.Background()
	pa, err := a.Service.CreateProject(ctx, "Environment Alpha", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.Service.CreateProject(ctx, "Environment Beta", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range []struct {
		project string
		create  func(proto.Thread) error
	}{
		{pa.ID, func(th proto.Thread) error { return a.Store.CreateThread(ctx, th) }},
		{pb.ID, func(th proto.Thread) error { return b.Store.CreateThread(ctx, th) }},
	} {
		th := proto.Thread{ID: "same-thread", ProjectID: item.project, Title: fmt.Sprintf("Thread %d", index),
			Provider: "claude", Model: "test", Permission: proto.PermissionFullAccess,
			Status: proto.ThreadIdle, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := item.create(th); err != nil {
			t.Fatal(err)
		}
	}
	profiles := []client.EnvironmentProfile{
		{ID: a.Identity.ID, Name: "Alpha", PublicKey: a.Identity.PublicKey(), Token: ca.Token, Routes: []client.Route{{URL: ta.URL, Kind: "direct"}}},
		{ID: b.Identity.ID, Name: "Beta", PublicKey: b.Identity.PublicKey(), Token: cb.Token, Routes: []client.Route{{URL: tb.URL, Kind: "direct"}}},
	}
	raw, _ := json.Marshal(savedEnvironments{Version: 1, Active: profiles[0].ID, Profiles: profiles})
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{"environments:v1": string(raw)}))
	app.WaitFor("text:Environment Alpha", 10*time.Second)
	app.Press("id:thread-same-thread")
	app.WaitFor("label:Message", 10*time.Second)
	app.Type("label:Message", "Alpha draft")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Alpha draft" })
	openEnvironments := func(target string) {
		t.Helper()
		app.WaitUntil(5*time.Second, func() bool {
			if app.Exists("id:environment-open-" + target) {
				return true
			}
			app.Press("id:environments")
			return false
		})
	}
	openEnvironments(b.Identity.ID)
	app.Press("id:environment-open-" + b.Identity.ID)
	app.WaitFor("text:Environment Beta", 10*time.Second)
	app.WaitForGone("text:Environment Alpha", 5*time.Second)
	app.Press("id:thread-same-thread")
	app.WaitFor("label:Message", 10*time.Second)
	app.Type("label:Message", "Beta draft")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Beta draft" })
	openEnvironments(a.Identity.ID)
	app.Press("id:environment-open-" + a.Identity.ID)
	app.WaitFor("text:Environment Alpha", 10*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		raw, _ := app.Storage().GetItem("env:" + a.Identity.ID + ":drafts")
		var drafts map[string]string
		_ = json.Unmarshal([]byte(raw), &drafts)
		return drafts["same-thread"] == "Alpha draft"
	})
	rawBeta, _ := app.Storage().GetItem("env:" + b.Identity.ID + ":drafts")
	var drafts map[string]string
	_ = json.Unmarshal([]byte(rawBeta), &drafts)
	if drafts["same-thread"] != "Beta draft" {
		t.Fatalf("drafts mixed: %q", rawBeta)
	}
	// Adding another connection to the existing identity merges routes without
	// replacing that environment's saved drafts with the connection screen's data.
	openEnvironments(a.Identity.ID)
	app.Press("id:environment-add")
	app.WaitFor("label:Server address", 5*time.Second)
	app.Type("label:Server address", ta.URL)
	app.Type("label:Token", ca.Token)
	app.Press("text:Connect")
	app.WaitFor("text:Environment Alpha", 10*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		return app.Exists("label:Message") && app.Find("label:Message").Text == "Alpha draft"
	})
}

func TestUIRouteFailoverKeepsDraftAndPausesRestoredQueue(t *testing.T) {
	srv, direct, cfg := startEnv(t)
	alternate := httptest.NewServer(srv.Handler())
	t.Cleanup(alternate.Close)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Failover project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Failover thread", Provider: "claude", Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := client.EnvironmentProfile{ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: cfg.Token,
		Routes: []client.Route{{URL: direct.URL, Kind: "direct"}, {URL: alternate.URL, Kind: "relay"}}}
	registry, _ := json.Marshal(savedEnvironments{Version: 1, Active: profile.ID, Profiles: []client.EnvironmentProfile{profile}})
	pending, _ := json.Marshal(map[string][]queuedPrompt{thread.ID: {{Text: "Needs delivery check", Uncertain: true}}})
	prefix := "env:" + profile.ID + ":"
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": string(registry), prefix + "selectedThread": thread.ID, prefix + "pendingMessages": string(pending),
	}))
	app.WaitFor("text:Check delivery", 10*time.Second)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:connection-status").Text == "Direct" })
	app.WaitFor("id:queue-resume", 5*time.Second)
	app.Type("label:Message", "Keep this draft through failover")
	app.WaitUntil(5*time.Second, func() bool {
		if app.Find("label:Message").Text == "Keep this draft through failover" {
			return true
		}
		// The initial detail/branch responses may replace the input while
		// nsgotest is entering characters; finish typing once it settles.
		app.Clear("label:Message")
		app.Type("label:Message", "Keep this draft through failover")
		return false
	})
	inputID := app.Find("label:Message").ID
	direct.CloseClientConnections()
	direct.Close()
	app.WaitUntil(10*time.Second, func() bool {
		address, _ := app.Storage().GetItem(prefix + "serverUrl")
		return address == alternate.URL
	})
	app.WaitFor("text:Check delivery", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:connection-status").Text == "Relay" })
	if input := app.Find("label:Message"); input.ID != inputID || !input.Focused {
		t.Fatalf("route change replaced or unfocused composer: %+v", input)
	}
	if got := app.Find("label:Message").Text; got != "Keep this draft through failover" {
		t.Fatalf("draft lost during failover: %q", got)
	}
	// Even an explicit queue resume cannot replay a send whose outcome is unknown.
	app.Press("id:queue-resume")
	app.Settle()
	runs, err := srv.Store.Runs(ctx, thread.ID)
	if err != nil || len(runs) != 0 {
		t.Fatalf("uncertain prompt automatically sent: %d runs, %v", len(runs), err)
	}
}

func TestUIAuthorizationRecoveryKeepsDraftAndValidatesToken(t *testing.T) {
	const previousCredential = "expired-" + "test-credential"
	srv, _, cfg := startEnv(t)
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Credential recovery", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Recovery thread", Provider: "claude", Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	var initialState atomic.Bool
	var rejectedStreams atomic.Int32
	handler := srv.Handler()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+previousCredential {
			if r.URL.Path == "/api/v1/state" && initialState.CompareAndSwap(false, true) {
				r.Header.Set("Authorization", "Bearer "+cfg.Token)
			} else {
				if r.URL.Path == "/api/v1/events" {
					rejectedStreams.Add(1)
				}
				http.Error(w, "access rejected", http.StatusUnauthorized)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { proxy.CloseClientConnections(); proxy.Close() })
	profile := client.EnvironmentProfile{ID: srv.Identity.ID, Alias: "Desktop", PublicKey: srv.Identity.PublicKey(),
		Token: previousCredential, Routes: []client.Route{{URL: proxy.URL, Kind: "direct"}}}
	registry, _ := json.Marshal(savedEnvironments{Version: 1, Active: profile.ID, Profiles: []client.EnvironmentProfile{profile}})
	prefix := "env:" + profile.ID + ":"
	pending, _ := json.Marshal(map[string][]queuedPrompt{thread.ID: {{Text: "Check before replay", Uncertain: true}}})
	app := nsgotest.Start(t, Root, nsgotest.Environment(core.Environment{
		OS: core.OSAndroid, Idiom: core.IdiomPhone, SafeArea: core.Edges{Top: 24, Bottom: 16},
		KeyboardHeight: 300,
	}), nsgotest.Size(390, 844), nsgotest.Storage(map[string]string{
		"environments:v1": string(registry), prefix + "selectedThread": thread.ID,
		prefix + "pendingMessages": string(pending),
	}))
	app.WaitFor("id:authorization-required", 10*time.Second)
	app.WaitFor("label:Message", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		return app.Find("id:current-environment-name").Text == "Desktop" &&
			app.Find("id:connection-status").Text == "Authorization required"
	})
	if app.Exists("id:offline") || app.Exists("label:Reconnecting") {
		t.Fatal("authorization rejection still shows reconnecting")
	}
	app.Type("label:Message", "Keep my draft")
	app.Advance(10 * time.Second)
	if rejectedStreams.Load() != 1 {
		t.Fatal("authorization rejection retried the stream automatically")
	}
	app.Press("id:update-credentials")
	app.WaitFor("id:credentials-dialog", 5*time.Second)
	app.Type("label:New environment token", "incorrect-token")
	app.Press("id:credentials-save")
	app.WaitFor("text:That token was rejected. Check the token and try again.", 5*time.Second)
	var saved savedEnvironments
	raw, _ := app.Storage().GetItem("environments:v1")
	if err := json.Unmarshal([]byte(raw), &saved); err != nil || saved.Profiles[0].Token != previousCredential {
		t.Fatal("failed credential verification changed saved credentials")
	}
	app.Press("id:credentials-cancel")
	app.WaitForGone("id:credentials-dialog", 5*time.Second)
	app.Press("id:update-credentials")
	app.WaitFor("id:credentials-dialog", 5*time.Second)
	if app.Find("label:New environment token").Text != "" {
		t.Fatal("cancel retained temporary credentials")
	}
	app.Type("label:New environment token", cfg.Token)
	app.Press("id:credentials-save")
	app.WaitUntil(5*time.Second, func() bool {
		app.Advance(250 * time.Millisecond)
		return !app.Exists("id:credentials-dialog")
	})
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:connection-status").Text == "Direct" })
	app.WaitForGone("id:authorization-required", 5*time.Second)
	app.WaitFor("text:Check delivery", 5*time.Second)
	if app.Find("label:Message").Text != "Keep my draft" {
		t.Fatal("credential update lost the current draft")
	}
	raw, _ = app.Storage().GetItem("environments:v1")
	if err := json.Unmarshal([]byte(raw), &saved); err != nil || saved.Profiles[0].Token != cfg.Token {
		t.Fatal("verified replacement credentials were not saved")
	}
	// The environment switcher remains available in the phone's conversation.
	app.Press("id:environment-switcher")
	app.WaitFor("id:environment-open-"+profile.ID, 5*time.Second)
}

func TestUIRestoredUnknownSendUsesOriginalRequest(t *testing.T) {
	for _, action := range []string{"check", "retry", "missing"} {
		t.Run(action, func(t *testing.T) {
			srv, ts, cfg := startEnv(t)
			srv.Service.Providers.Register(sideRunner{})
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			project, err := srv.Service.CreateProject(ctx, "Delivery project", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
				ProjectID: project.ID, Title: "Delivery thread", Provider: "side-fake", Model: "side-fake-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			const requestID = "send_restored"
			if action != "missing" {
				if _, err := srv.Service.SendWithRequest(ctx, thread.ID, "Unknown delivery", nil, requestID); err != nil {
					t.Fatal(err)
				}
				if err := srv.Service.Wait(ctx, thread.ID); err != nil {
					t.Fatal(err)
				}
			}
			profile := client.EnvironmentProfile{ID: srv.Identity.ID, PublicKey: srv.Identity.PublicKey(), Token: cfg.Token,
				Routes: []client.Route{{URL: ts.URL, Kind: "direct"}}}
			registry, _ := json.Marshal(savedEnvironments{Version: 1, Active: profile.ID, Profiles: []client.EnvironmentProfile{profile}})
			pending, _ := json.Marshal(map[string][]queuedPrompt{thread.ID: {
				{Text: "Unknown delivery", Uncertain: true, RequestID: requestID},
				{Text: "Keep paused"},
			}})
			drafts, _ := json.Marshal(map[string]string{thread.ID: "Keep this draft"})
			prefix := "env:" + profile.ID + ":"
			app := nsgotest.Start(t, Root, nsgotest.Size(420, 820), nsgotest.Storage(map[string]string{
				"environments:v1": string(registry), prefix + "selectedThread": thread.ID,
				prefix + "pendingMessages": string(pending), prefix + "drafts": string(drafts),
			}))
			app.WaitFor("id:queued-check-0", 10*time.Second)
			if app.Exists("id:queued-edit-0") {
				t.Fatal("unknown keyed request can be edited into a new send")
			}
			if action == "missing" {
				app.Press("id:queued-check-0")
				app.WaitFor("text:No receipt yet. Retry the same request to confirm delivery without starting it twice.", 5*time.Second)
				// A 404 must retain the original request and never send automatically.
				runs, err := srv.Store.Runs(ctx, thread.ID)
				if err != nil || len(runs) != 0 {
					t.Fatalf("receipt check sent a message: %#v, %v", runs, err)
				}
				app.Press("id:queued-retry-0")
			} else {
				app.Press("id:queued-" + action + "-0")
			}
			app.WaitForGone("text:Check delivery", 10*time.Second)
			app.WaitUntil(5*time.Second, func() bool {
				raw, _ := app.Storage().GetItem(prefix + "pendingMessages")
				var saved map[string][]queuedPrompt
				_ = json.Unmarshal([]byte(raw), &saved)
				return len(saved[thread.ID]) == 1 && saved[thread.ID][0].Text == "Keep paused"
			})
			if err := srv.Service.Wait(ctx, thread.ID); err != nil {
				t.Fatal(err)
			}
			runs, err := srv.Store.Runs(ctx, thread.ID)
			if err != nil || len(runs) != 1 {
				t.Fatalf("retry or paused queue executed twice: %#v, %v", runs, err)
			}
			if got := app.Find("label:Message").Text; got != "Keep this draft" {
				t.Fatalf("receipt action replaced draft: %q", got)
			}
		})
	}
}

func TestUIThreadUpdatesPreserveComposerAndFocus(t *testing.T) {
	for _, width := range []core.Points{420, 1100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			srv, ts, cfg := startEnv(t)
			ctx := context.Background()
			project, err := srv.Service.CreateProject(ctx, "Stable composer", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
				ProjectID: project.ID, Title: "Before update", Provider: "claude", Model: "test",
			})
			if err != nil {
				t.Fatal(err)
			}
			app := nsgotest.Start(t, Root, nsgotest.Size(width, 820), nsgotest.Storage(map[string]string{
				"serverUrl": ts.URL, "token": cfg.Token, "selectedThread": thread.ID,
			}))
			header := ""
			if width >= 760 {
				header = "id:content-header >> "
			}
			app.WaitFor(header+"text:Before update", 10*time.Second)
			app.WaitFor("label:Message", 5*time.Second)
			app.Type("label:Message", "Keep typing")
			inputID := app.Find("label:Message").ID
			for i := range 3 {
				title := fmt.Sprintf("Updated title %d", i)
				if _, err := srv.Service.UpdateThread(ctx, thread.ID, orchestrator.ThreadPatch{Title: &title}); err != nil {
					t.Fatal(err)
				}
				app.WaitFor(header+"text:"+title, 5*time.Second)
				input := app.Find("label:Message")
				if input.ID != inputID || !input.Focused || input.Text != "Keep typing" {
					t.Fatalf("thread event replaced active composer: ID %v -> %v, focused=%v, text=%q", inputID, input.ID, input.Focused, input.Text)
				}
			}
			app.Type("label:Message", " after updates")
			if got := app.Find("label:Message").Text; got != "Keep typing after updates" {
				t.Fatalf("lost input: %q", got)
			}
		})
	}
}

type fullPendingStorage struct{ appStorage }

func (s fullPendingStorage) SetItem(key, value string) error {
	if key == "pendingMessages" {
		return errors.New("storage full")
	}
	return s.appStorage.SetItem(key, value)
}

func TestUIStorageFailureDoesNotSubmitOrReplaceUnknownRequest(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			srv, ts, cfg := startEnv(t)
			srv.Service.Providers.Register(sideRunner{})
			ctx := context.Background()
			project, err := srv.Service.CreateProject(ctx, "Storage failure", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
				ProjectID: project.ID, Title: "Pending storage", Provider: "side-fake", Model: "side-fake-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			storage := map[string]string{"serverUrl": ts.URL, "token": cfg.Token, "selectedThread": thread.ID}
			if retry {
				pending, _ := json.Marshal(map[string][]queuedPrompt{thread.ID: {
					{Text: "Preserve original request", Uncertain: true, RequestID: "send_original"},
				}})
				storage["pendingMessages"] = string(pending)
			}
			var instance *app
			testApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.Untrack(func() { instance = newApp(cx) })
				instance.store = fullPendingStorage{instance.store}
				return core.Dynamic(func(cx *core.Scope) core.Node {
					if !instance.connected.Get() {
						return instance.connectView(cx)
					}
					return instance.mainView(cx)
				})
			}, nsgotest.Size(1100, 820), nsgotest.Storage(storage))
			testApp.WaitFor("label:Message", 10*time.Second)
			if retry {
				testApp.WaitFor("id:queued-retry-0", 5*time.Second)
				testApp.Press("id:queued-retry-0")
			} else {
				testApp.Type("label:Message", "Preserve original request")
				testApp.Press("text:Send")
			}
			testApp.WaitFor("text:Could not save the pending message. It has not been sent: storage full", 5*time.Second)
			runs, err := srv.Store.Runs(ctx, thread.ID)
			if err != nil || len(runs) != 0 {
				t.Fatalf("submitted before saving request identity: %#v, %v", runs, err)
			}
			testApp.Post(func() {
				queue := instance.queued[thread.ID]
				if len(queue) != 1 || queue[0].Text != "Preserve original request" {
					t.Errorf("lost pending message: %#v", queue)
				} else if retry && (!queue[0].Uncertain || queue[0].RequestID != "send_original") {
					t.Errorf("storage failure made an unknown request executable with a new ID: %#v", queue[0])
				}
			})
			testApp.Settle()
		})
	}
}
