package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func workspaceRegistry(profiles ...client.EnvironmentProfile) string {
	raw, _ := json.Marshal(savedEnvironments{Version: 1, Active: profiles[0].ID,
		Default: profiles[0].ID, Profiles: profiles})
	return string(raw)
}

func waitSheetGone(app *nsgotest.App, selector string) {
	// Sheet exit animations use the test clock, while HTTP completions use
	// real time. Advance frames as we wait for the asynchronous close.
	app.WaitUntil(5*time.Second, func() bool {
		app.Advance(250 * time.Millisecond)
		return !app.Exists(selector)
	})
}

func TestUISameNamedProjectsKeepIndependentGroups(t *testing.T) {
	alpha, ta, ca := startEnv(t)
	beta, tb, cb := startEnv(t)
	ctx := context.Background()
	for _, create := range []func(proto.Project, proto.Thread) error{
		func(p proto.Project, th proto.Thread) error {
			if err := alpha.Store.CreateProject(ctx, p); err != nil {
				return err
			}
			return alpha.Store.CreateThread(ctx, th)
		},
		func(p proto.Project, th proto.Thread) error {
			if err := beta.Store.CreateProject(ctx, p); err != nil {
				return err
			}
			return beta.Store.CreateThread(ctx, th)
		},
	} {
		p := proto.Project{ID: "same-project", Name: "Shared project", Path: t.TempDir(), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		th := proto.Thread{ID: "same-thread", ProjectID: p.ID, Title: "Shared thread", Provider: "claude", Model: "test",
			Permission: proto.PermissionFullAccess, Status: proto.ThreadIdle, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := create(p, th); err != nil {
			t.Fatal(err)
		}
	}
	pa := client.EnvironmentProfile{ID: alpha.Identity.ID, Alias: "Alpha", Token: ca.Token, PublicKey: alpha.Identity.PublicKey(),
		Routes: []client.Route{{URL: ta.URL, Kind: "direct"}}}
	pb := client.EnvironmentProfile{ID: beta.Identity.ID, Alias: "Beta", Token: cb.Token, PublicKey: beta.Identity.PublicKey(),
		Routes: []client.Route{{URL: tb.URL, Kind: "direct"}}}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(pa, pb),
	}))
	alphaRow, betaRow := "id:thread-"+pa.ID+"/same-thread", "id:thread-"+pb.ID+"/same-thread"
	app.WaitFor(alphaRow, 10*time.Second)
	app.WaitFor(betaRow, 10*time.Second)
	alphaHeader := "id:project-" + pa.ID + "/same-project >> label:Shared project"
	app.Press(alphaHeader)
	app.WaitForGone(alphaRow, 5*time.Second)
	app.WaitFor(betaRow, 5*time.Second)
	app.Press(alphaHeader)
	app.WaitFor(alphaRow, 5*time.Second)
	app.Press(betaRow)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:current-environment-name").Text == "Beta" })
	app.Key("up")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:current-environment-name").Text == "Alpha" })
	app.Key("down")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:current-environment-name").Text == "Beta" })
	app.Type("label:Search threads", "no such thread")
	app.WaitFor("text:No threads match your search.", 5*time.Second)
	app.Clear("label:Search threads")
	app.WaitFor(alphaRow, 5*time.Second)
	app.WaitFor(betaRow, 5*time.Second)
}

func TestUIMobileEnvironmentFormsKeepFocusWhenKeyboardChanges(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	p := client.EnvironmentProfile{ID: srv.Identity.ID, Alias: "Phone", Token: cfg.Token, PublicKey: srv.Identity.PublicKey(),
		Routes: []client.Route{{URL: ts.URL, Kind: "direct"}}}
	env := core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone, SafeArea: core.Edges{Top: 24, Bottom: 16}}
	app := nsgotest.Start(t, Root, nsgotest.Size(390, 844), nsgotest.Environment(env),
		nsgotest.Storage(map[string]string{"environments:v1": workspaceRegistry(p)}))
	app.WaitFor("id:settings", 10*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:settings-nav-environments", 5*time.Second)
	app.Press("id:settings-nav-environments")
	app.Press("id:environment-rename-" + p.ID)
	app.WaitFor("label:Environment alias", 5*time.Second)
	app.Clear("label:Environment alias")
	app.Type("label:Environment alias", "Office")
	aliasID := app.Find("label:Environment alias").ID
	env.KeyboardHeight = 300
	app.SetEnvironment(env)
	app.Settle()
	if field := app.Find("label:Environment alias"); field.ID != aliasID || !field.Focused || field.Text != "Office" {
		t.Fatalf("keyboard change disturbed alias input: id %d -> %d, focused=%v, text=%q", aliasID, field.ID, field.Focused, field.Text)
	}
	app.Press("id:environment-add")
	app.WaitFor("label:Server address", 5*time.Second)
	app.Type("label:Server address", "http://example.test:4870")
	addressID := app.Find("label:Server address").ID
	env.KeyboardHeight = 220
	app.SetEnvironment(env)
	app.Settle()
	if field := app.Find("label:Server address"); field.ID != addressID || !field.Focused || field.Text != "http://example.test:4870" {
		t.Fatalf("keyboard change disturbed connection input: id %d -> %d, focused=%v, text=%q", addressID, field.ID, field.Focused, field.Text)
	}
	dialog := app.Find("id:add-environment-dialog").Frame
	if dialog.Origin.Y+dialog.Size.Height > 844-env.KeyboardHeight+1 {
		t.Fatalf("add environment dialog is covered by the keyboard: %+v", dialog)
	}
	app.Press("id:environment-add-cancel")
	waitSheetGone(app, "id:add-environment-dialog")
}

func TestUINewProjectUsesSelectedEnvironment(t *testing.T) {
	for _, width := range []core.Points{390, 1100} {
		t.Run(map[bool]string{true: "phone", false: "desktop"}[width < 760], func(t *testing.T) {
			alpha, _, ca := startEnv(t)
			beta, _, cb := startEnv(t)
			ctx := context.Background()
			// Project and thread IDs can both collide across machines.
			for _, srv := range []struct {
				create func(proto.Project) error
				path   string
			}{
				{func(p proto.Project) error { return alpha.Store.CreateProject(ctx, p) }, t.TempDir()},
				{func(p proto.Project) error { return beta.Store.CreateProject(ctx, p) }, t.TempDir()},
			} {
				if err := srv.create(proto.Project{ID: "same-project", Name: "Original project", Path: srv.path,
					CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			thread, err := alpha.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
				ProjectID: "same-project", Title: "Keep conversation", Provider: "claude", Model: "test",
			})
			if err != nil {
				t.Fatal(err)
			}
			var alphaDirs, alphaWrites, betaDirs, betaMkdir, betaProjects atomic.Int32
			alphaHandler, betaHandler := alpha.Handler(), beta.Handler()
			ta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/dirs" {
					alphaDirs.Add(1)
				}
				if r.Method == http.MethodPost {
					alphaWrites.Add(1)
				}
				alphaHandler.ServeHTTP(w, r)
			}))
			tb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/dirs" && r.Method == http.MethodGet:
					betaDirs.Add(1)
				case r.URL.Path == "/api/v1/dirs" && r.Method == http.MethodPost:
					betaMkdir.Add(1)
				case r.URL.Path == "/api/v1/projects" && r.Method == http.MethodPost:
					betaProjects.Add(1)
				}
				betaHandler.ServeHTTP(w, r)
			}))
			t.Cleanup(func() { ta.CloseClientConnections(); ta.Close(); tb.CloseClientConnections(); tb.Close() })
			pa := client.EnvironmentProfile{ID: alpha.Identity.ID, Alias: "Alpha", Token: ca.Token, PublicKey: alpha.Identity.PublicKey(),
				Routes: []client.Route{{URL: ta.URL, Kind: "direct"}}}
			pb := client.EnvironmentProfile{ID: beta.Identity.ID, Alias: "Beta", Token: cb.Token, PublicKey: beta.Identity.PublicKey(),
				Routes: []client.Route{{URL: tb.URL, Kind: "direct"}}}
			var manager *environmentManager
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				manager = newEnvironmentManager(cx)
				manager.start()
				return manager.view(cx)
			}, nsgotest.Size(width, 844), nsgotest.Storage(map[string]string{"environments:v1": workspaceRegistry(pa, pb)}))
			if width < 760 {
				openMobileProjects(app)
			}
			app.WaitFor("id:project-"+pa.ID+"/same-project", 10*time.Second)
			app.WaitFor("id:project-"+pb.ID+"/same-project", 10*time.Second)
			// Preserve an open conversation while using another environment's
			// file picker and mutation endpoints.
			app.Press("id:thread-" + pa.ID + "/" + thread.ID)
			app.WaitFor("label:Message", 5*time.Second)
			app.Type("label:Message", "Keep Alpha draft")
			app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Keep Alpha draft" })
			if width < 760 {
				app.Press("label:Back to threads")
				app.WaitFor("id:new-project", 5*time.Second)
			}
			app.Press("id:new-project")
			app.WaitFor("id:project-environment-"+pb.ID, 5*time.Second)
			app.Type("label:Project path", "/old-environment-path")
			app.Press("id:project-environment-" + pb.ID)
			app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Project path").Text == "" })
			folder := t.TempDir()
			app.Type("label:Project path", folder)
			app.Press("id:browse-project")
			app.WaitUntil(5*time.Second, func() bool {
				return app.Exists("id:dirs-path") && app.Find("id:dirs-path").Text == folder
			})
			if got := app.Find("id:dirs-environment").Text; got != "Environment: Beta" {
				t.Fatalf("wrong file picker environment: %q", got)
			}
			app.Press("id:dirs-new-folder")
			app.WaitFor("label:Folder name", 5*time.Second)
			app.Type("label:Folder name", "new-project")
			app.Press("id:create-dir")
			waitSheetGone(app, "id:new-dir-dialog")
			app.WaitFor("id:dirs-entry-new-project", 5*time.Second)
			app.Press("id:dirs-entry-new-project")
			app.WaitUntil(5*time.Second, func() bool { return app.Find("id:dirs-path").Text == filepath.Join(folder, "new-project") })
			app.Press("id:dirs-use")
			waitSheetGone(app, "id:dirs-dialog")
			app.Press("id:create-project")
			waitSheetGone(app, "id:new-project-dialog")
			app.WaitFor("text:new-project", 5*time.Second)
			if alphaDirs.Load() != 0 || alphaWrites.Load() != 0 || betaDirs.Load() < 2 ||
				betaMkdir.Load() != 1 || betaProjects.Load() != 1 {
				t.Fatalf("wrong target: alpha dirs=%d writes=%d; beta dirs=%d mkdir=%d projects=%d",
					alphaDirs.Load(), alphaWrites.Load(), betaDirs.Load(), betaMkdir.Load(), betaProjects.Load())
			}
			if _, err := os.Stat(filepath.Join(folder, "new-project")); err != nil {
				t.Fatal(err)
			}
			app.Post(func() {
				if manager.app.profile.ID != pa.ID {
					t.Error("creating a project changed the current conversation's environment")
				}
			})
			app.Settle()
			if width < 760 {
				app.Press("id:thread-" + pa.ID + "/" + thread.ID)
			}
			app.WaitUntil(5*time.Second, func() bool {
				return app.Exists("label:Message") && app.Find("label:Message").Text == "Keep Alpha draft"
			})
		})
	}
}

func TestUIChangingProjectEnvironmentIgnoresStaleDirectoryResponse(t *testing.T) {
	alpha, _, ca := startEnv(t)
	beta, tb, cb := startEnv(t)
	requested, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := alpha.Handler()
	ta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/dirs" {
			close(requested)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			defer close(finished)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { ta.CloseClientConnections(); ta.Close() })
	pa := client.EnvironmentProfile{ID: alpha.Identity.ID, Alias: "Alpha", Token: ca.Token, PublicKey: alpha.Identity.PublicKey(),
		Routes: []client.Route{{URL: ta.URL, Kind: "direct"}}}
	pb := client.EnvironmentProfile{ID: beta.Identity.ID, Alias: "Beta", Token: cb.Token, PublicKey: beta.Identity.PublicKey(),
		Routes: []client.Route{{URL: tb.URL, Kind: "direct"}}}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(pa, pb),
	}))
	app.WaitUntil(10*time.Second, func() bool {
		return app.Exists("id:environment-status-"+pa.ID) && app.Find("id:environment-status-"+pa.ID).Text == "Direct" &&
			app.Exists("id:environment-status-"+pb.ID) && app.Find("id:environment-status-"+pb.ID).Text == "Direct"
	})
	app.Press("id:new-project")
	app.WaitFor("id:browse-project", 5*time.Second)
	app.Type("label:Project path", t.TempDir())
	app.Press("id:browse-project")
	app.WaitUntil(5*time.Second, func() bool {
		select {
		case <-requested:
			return true
		default:
			return false
		}
	})
	app.Press("id:dirs-cancel")
	waitSheetGone(app, "id:dirs-dialog")
	app.Press("id:project-environment-" + pb.ID)
	targetPath := t.TempDir()
	app.Type("label:Project path", targetPath)
	app.Press("id:browse-project")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:dirs-path").Text == targetPath })
	close(release)
	app.WaitUntil(5*time.Second, func() bool {
		select {
		case <-finished:
			return true
		default:
			return false
		}
	})
	app.Settle()
	if got := app.Find("id:dirs-path").Text; got != targetPath {
		t.Fatalf("stale response replaced the target's folders: %q", got)
	}
}

func TestUIUnavailableEnvironmentDoesNotBlockHealthySessions(t *testing.T) {
	healthy, ta, ca := startEnv(t)
	unavailable, tb, cb := startEnv(t)
	ctx := context.Background()
	project, err := healthy.Service.CreateProject(ctx, "Healthy project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	thread, err := healthy.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Healthy thread", Provider: "claude", Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := unavailable.Service.CreateProject(ctx, "Recovered project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pa := client.EnvironmentProfile{ID: healthy.Identity.ID, Alias: "Healthy", Token: ca.Token, PublicKey: healthy.Identity.PublicKey(),
		Routes: []client.Route{{URL: ta.URL, Kind: "direct"}}}
	pb := client.EnvironmentProfile{ID: unavailable.Identity.ID, Alias: "Unavailable", Token: "wrong", PublicKey: unavailable.Identity.PublicKey(),
		Routes: []client.Route{{URL: tb.URL, Kind: "direct"}}}
	app := nsgotest.Start(t, Root, nsgotest.Size(1100, 760), nsgotest.Storage(map[string]string{
		"environments:v1": workspaceRegistry(pb, pa),
	}))
	app.WaitFor("text:Healthy project", 10*time.Second)
	app.WaitUntil(5*time.Second, func() bool {
		return app.Find("id:environment-status-"+pb.ID).Text == "Authorization required"
	})
	app.Press("id:thread-" + pa.ID + "/" + thread.ID)
	app.WaitFor("label:Message", 5*time.Second)
	app.Type("label:Message", "Healthy draft")
	app.Press("id:settings")
	app.WaitFor("id:settings-nav-environments", 5*time.Second)
	app.Press("id:settings-nav-environments")
	if app.Exists("id:settings-save") {
		t.Fatal("environment management offers to save unrelated server settings")
	}
	app.Press("id:environment-token-" + pb.ID)
	app.WaitFor("label:New environment token", 5*time.Second)
	app.Type("label:New environment token", cb.Token)
	app.Press("id:credentials-save")
	waitSheetGone(app, "id:credentials-dialog")
	app.WaitFor("id:project-"+pb.ID+"/"+remote.ID, 5*time.Second)
	app.Press("id:environment-forget-" + pb.ID)
	app.WaitFor("id:environment-confirm-forget", 5*time.Second)
	app.Press("id:environment-confirm-forget")
	app.WaitForGone("id:environment-"+pb.ID, 5*time.Second)
	app.Press("id:settings-close")
	waitSheetGone(app, "id:settings-dialog")
	app.WaitUntil(5*time.Second, func() bool { return app.Find("label:Message").Text == "Healthy draft" })
	if _, err := healthy.Service.CreateProject(ctx, "Still connected", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	app.WaitFor("text:Still connected", 5*time.Second)
	raw, _ := app.Storage().GetItem("environments:v1")
	var saved savedEnvironments
	if err := json.Unmarshal([]byte(raw), &saved); err != nil || len(saved.Profiles) != 1 || saved.Active != pa.ID {
		t.Fatalf("forgetting a background environment changed the workspace: %v", err)
	}
	inputID := app.Find("label:Message").ID
	// Adding a new connection must preserve the visible thread and its scope.
	app.Press("id:settings")
	app.WaitFor("id:environment-add", 5*time.Second)
	app.Press("id:environment-add")
	app.WaitFor("label:Server address", 5*time.Second)
	app.Type("label:Server address", tb.URL)
	app.Type("label:Token", cb.Token)
	app.Press("id:connect-environment")
	waitSheetGone(app, "id:add-environment-dialog")
	app.WaitFor("id:project-"+pb.ID+"/"+remote.ID, 5*time.Second)
	app.Press("id:settings-close")
	waitSheetGone(app, "id:settings-dialog")
	if input := app.Find("label:Message"); input.ID != inputID || input.Text != "Healthy draft" {
		t.Fatalf("adding an environment replaced the current conversation: %+v", input)
	}
	// Forgetting the visible environment selects an existing session; forgetting
	// the last one returns to the connection form without reconnecting it.
	app.Press("id:settings")
	app.WaitFor("id:environment-forget-"+pa.ID, 5*time.Second)
	app.Press("id:environment-forget-" + pa.ID)
	app.WaitFor("id:environment-confirm-forget", 5*time.Second)
	app.Press("id:environment-confirm-forget")
	waitSheetGone(app, "id:settings-dialog")
	app.WaitFor("id:project-"+remote.ID, 5*time.Second)
	if _, err := unavailable.Service.CreateProject(ctx, "Remaining session live", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	app.WaitFor("text:Remaining session live", 5*time.Second)
	app.Press("id:settings")
	app.WaitFor("id:environment-forget-"+pb.ID, 5*time.Second)
	app.Press("id:environment-forget-" + pb.ID)
	app.WaitFor("id:environment-confirm-forget", 5*time.Second)
	app.Press("id:environment-confirm-forget")
	app.WaitFor("label:Server address", 5*time.Second)
}
