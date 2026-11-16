package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func TestEnvironmentDisplayNames(t *testing.T) {
	direct := func(address string) []client.Route {
		return []client.Route{{URL: address, Kind: "direct"}}
	}
	relay := func(id string) []client.Route {
		return []client.Route{{URL: "https://hub.example/e/" + id, Kind: "relay"}}
	}
	for _, test := range []struct {
		name     string
		profiles []client.EnvironmentProfile
		want     []string
	}{
		{
			name: "default names use execution hostnames",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Name: "T3Go", Hostname: " cloud-box ", Routes: direct("http://82.156.132.143:4870")},
				{ID: "desktop-one", Name: "t3go", Hostname: "desktop-box", Routes: relay("desktop-one")},
			},
			want: []string{"cloud-box", "desktop-box"},
		},
		{
			name: "default names use addresses",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Name: "T3Go", Routes: direct("http://82.156.132.143:4870")},
				{ID: "desktop-one", Name: "t3go", Routes: direct("http://192.168.1.20:4870")},
			},
			want: []string{"82.156.132.143:4870", "192.168.1.20:4870"},
		},
		{
			name: "unique custom names and aliases are kept",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Name: "Cloud", Hostname: "cloud-box", Routes: direct("http://cloud.example")},
				{ID: "desktop-one", Name: "T3Go", Hostname: "desktop-box", Alias: " My desktop ", Routes: direct("http://desktop.local")},
			},
			want: []string{"Cloud", "My desktop"},
		},
		{
			name: "same hostname gains addresses",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Name: "T3Go", Hostname: "ubuntu", Routes: direct("https://cloud.example")},
				{ID: "desktop-one", Name: "T3Go", Hostname: "ubuntu", Routes: direct("http://desktop.local:4870")},
			},
			want: []string{"ubuntu · cloud.example", "ubuntu · desktop.local:4870"},
		},
		{
			name: "same hostname sharing relay gains stable identities",
			profiles: []client.EnvironmentProfile{
				{ID: "desktop-one", Name: "T3Go", Hostname: "ubuntu", Routes: relay("desktop-one")},
				{ID: "desktop-two", Name: "T3Go", Hostname: "ubuntu", Routes: relay("desktop-two")},
			},
			want: []string{"ubuntu · hub.example · desktop-o", "ubuntu · hub.example · desktop-t"},
		},
		{
			name: "cached hostname replaces a legacy URL label",
			profiles: []client.EnvironmentProfile{
				{ID: "desktop-one", Name: "http://desktop.local:4870", Hostname: "desktop-box"},
				{ID: "cloud-one", Name: "T3Go", Hostname: "  ", Routes: direct("https://cloud.example")},
			},
			want: []string{"desktop-box", "cloud.example"},
		},
		{
			name: "local-only names fall back to system",
			profiles: []client.EnvironmentProfile{
				{ID: "desktop-one", Name: "localhost", Hostname: "127.0.0.1", Alias: "localhost",
					Routes: direct("http://localhost:4870"), System: &proto.SystemInfo{OS: "macOS"}},
				{ID: "desktop-two", Name: "T3Go", Hostname: "::1",
					Routes: relay("desktop-two"), System: &proto.SystemInfo{OS: "Linux"}},
			},
			want: []string{"macOS", "Linux"},
		},
		{
			name: "same custom names gain addresses",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Alias: "Office", Routes: direct("https://cloud.example")},
				{ID: "desktop-one", Name: "Office", Routes: direct("http://desktop.local:4870")},
			},
			want: []string{"Office · cloud.example", "Office · desktop.local:4870"},
		},
		{
			name: "relay and direct on the same hub remain distinct",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Name: "T3Go", Routes: direct("https://hub.example")},
				{ID: "desktop-one", Name: "T3Go", Routes: relay("desktop-one")},
				{ID: "desktop-two", Name: "T3Go", Routes: relay("desktop-two")},
			},
			want: []string{"hub.example · cloud-", "hub.example · desktop-o", "hub.example · desktop-t"},
		},
		{
			name: "direct address preferred over relay",
			profiles: []client.EnvironmentProfile{
				{ID: "desktop-one", Name: "T3Go", Routes: append(relay("desktop-one"), direct("http://desktop.local:4870")...)},
			},
			want: []string{"desktop.local:4870"},
		},
		{
			name: "addresses hide credentials and URL paths",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Name: "T3Go", Routes: direct("https://user:secret@cloud.example/private?token=secret#secret")},
				{ID: "desktop-one", Name: "https://desktop.local/e/secret?token=secret"},
			},
			want: []string{"cloud.example", "desktop.local"},
		},
		{
			name: "missing routes use short stable identities",
			profiles: []client.EnvironmentProfile{
				{ID: "cloud-one", Name: "T3Go"},
				{ID: "desktop-one"},
			},
			want: []string{"Environment · cloud-", "Environment · deskto"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := environmentDisplayNames(test.profiles)
			for i, p := range test.profiles {
				if got[p.ID] != test.want[i] {
					t.Errorf("%s: got %q, want %q", p.ID, got[p.ID], test.want[i])
				}
			}
		})
	}
}

func TestUIDefaultEnvironmentNamesStayConsistent(t *testing.T) {
	for _, width := range []core.Points{390, 1100} {
		t.Run(map[bool]string{true: "phone", false: "desktop"}[width < 760], func(t *testing.T) {
			alpha, ta, ca := startEnv(t)
			beta, tb, cb := startEnv(t)
			alpha.Hostname, beta.Hostname = "cloud-box", "desktop-box"
			ctx := context.Background()
			project, err := beta.Service.CreateProject(ctx, "Desktop project", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			thread, err := beta.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
				ProjectID: project.ID, Title: "Desktop thread", Provider: "claude", Model: "test",
			})
			if err != nil {
				t.Fatal(err)
			}
			pa := client.EnvironmentProfile{ID: alpha.Identity.ID, Name: "T3Go", Token: ca.Token,
				PublicKey: alpha.Identity.PublicKey(), Routes: []client.Route{{URL: ta.URL, Kind: "direct"}}}
			pb := client.EnvironmentProfile{ID: beta.Identity.ID, Name: "T3Go", Token: cb.Token,
				PublicKey: beta.Identity.PublicKey(), Routes: []client.Route{{URL: tb.URL, Kind: "relay"}}}
			app := nsgotest.Start(t, Root, nsgotest.Size(width, 844),
				nsgotest.Storage(map[string]string{"environments:v1": workspaceRegistry(pa, pb)}))
			names := []string{"cloud-box", "desktop-box"}
			app.WaitFor("id:thread-"+pb.ID+"/"+thread.ID, 10*time.Second)
			if width < 760 {
				app.WaitUntil(5*time.Second, func() bool {
					return app.Find("id:mobile-thread-environment-"+pb.ID+"/"+thread.ID).Text == names[1]
				})
				openMobileProjects(app)
				app.WaitUntil(5*time.Second, func() bool {
					return app.Find("id:environment-status-"+pa.ID).Text == "Direct" &&
						app.Find("id:environment-status-"+pb.ID).Text == "Relay"
				})
				for i, p := range []client.EnvironmentProfile{pa, pb} {
					app.WaitFor("id:sidebar-environment-"+p.ID+" >> text:"+names[i], 5*time.Second)
				}
			} else {
				app.WaitUntil(5*time.Second, func() bool {
					context := app.Find("id:thread-context-" + pb.ID + "/" + thread.ID).Text
					return strings.Contains(context, names[1])
				})
				if app.Exists("id:sidebar-environment-"+pa.ID) || app.Exists("id:sidebar-environment-"+pb.ID) {
					t.Fatal("desktop inbox still groups conversations by environment")
				}
				app.WaitUntil(5*time.Second, func() bool {
					return app.Find("id:project-environment-"+pb.ID+"/"+project.ID).Text == names[1]
				})
			}
			var saved savedEnvironments
			app.WaitUntil(5*time.Second, func() bool {
				raw, _ := app.Storage().GetItem("environments:v1")
				if json.Unmarshal([]byte(raw), &saved) != nil {
					return false
				}
				offline := environmentDisplayNames(saved.Profiles)
				return offline[pa.ID] == names[0] && offline[pb.ID] == names[1]
			})
			offlineNames := environmentDisplayNames(saved.Profiles)
			for i, p := range []client.EnvironmentProfile{pa, pb} {
				if got := offlineNames[p.ID]; got != names[i] {
					t.Errorf("cached environment %s: got %q, want %q", p.ID, got, names[i])
				}
			}
			app.Press("id:new-project")
			for i, p := range []client.EnvironmentProfile{pa, pb} {
				app.WaitFor("id:project-environment-"+p.ID+" >> text:"+names[i], 5*time.Second)
			}
			app.Press("id:project-environment-" + pb.ID)
			app.Press("id:browse-project")
			app.WaitFor("id:dirs-environment", 5*time.Second)
			if got := app.Find("id:dirs-environment").Text; got != "Environment: "+names[1] {
				t.Fatalf("directory picker name = %q", got)
			}
			app.Press("id:dirs-cancel")
			waitSheetGone(app, "id:dirs-dialog")
			app.Press("id:cancel-project")
			waitSheetGone(app, "id:new-project-dialog")
			app.Press("id:settings")
			app.WaitFor("id:settings-nav-environments", 5*time.Second)
			app.Press("id:settings-nav-environments")
			app.WaitFor("id:environment-"+pb.ID+" >> text:\""+names[1]+"\"", 5*time.Second)
			app.WaitFor("id:environment-system-"+pb.ID, 5*time.Second)
			app.WaitFor("id:environment-cpu-"+pb.ID, 5*time.Second)
			app.Press("id:environment-rename-" + pb.ID)
			app.WaitFor("id:environment-alias-editor-"+pb.ID, 5*time.Second)
			app.Clear("label:Environment alias")
			app.Type("label:Environment alias", "localhost")
			app.WaitFor("text:Use a device name instead of localhost or a loopback address.", 5*time.Second)
			app.Clear("label:Environment alias")
			app.Type("label:Environment alias", "My desktop")
			app.Press("id:environment-alias-save-" + pb.ID)
			app.WaitFor("id:environment-"+pb.ID+" >> text:My desktop", 5*time.Second)
			if width < 760 {
				// The phone settings are a page with a back arrow; the
				// desktop keeps its settings dialog.
				app.Press("id:mobile-settings-back")
				app.Press("id:mobile-tab-threads")
			} else {
				app.Press("id:settings-close")
			}
			waitSheetGone(app, "id:settings-dialog")
			if width < 760 {
				openMobileProjects(app)
				app.WaitFor("id:sidebar-environment-"+pb.ID+" >> text:My desktop", 5*time.Second)
			} else {
				app.WaitUntil(5*time.Second, func() bool {
					return app.Find("id:project-environment-"+pb.ID+"/"+project.ID).Text == "My desktop"
				})
			}
			app.Press("id:new-project")
			app.WaitFor("id:project-environment-"+pb.ID+" >> text:My desktop", 5*time.Second)
			app.Press("id:cancel-project")
			waitSheetGone(app, "id:new-project-dialog")
			app.Press("id:thread-" + pb.ID + "/" + thread.ID)
			app.WaitUntil(5*time.Second, func() bool {
				return app.Exists("id:current-environment-name") && app.Find("id:current-environment-name").Text == "My desktop"
			})
		})
	}
}

func TestUIHostnameRefreshKeepsDraftAndAlias(t *testing.T) {
	fixture := newSettingsFixture(t, "Alpha")
	fixture.profile.Alias = ""
	rendered, manager := startMobilePages(t, fixture)
	owner := manager.app
	rendered.Press("id:thread-same-thread")
	rendered.WaitFor("label:Message", 5*time.Second)
	rendered.Type("label:Message", "Keep this draft")
	inputID := rendered.Find("label:Message").ID
	rendered.Post(func() {
		st := proto.State{Hostname: "renamed-desktop", Settings: owner.settings.GetUntracked(),
			Projects: owner.projects.GetUntracked(), Threads: owner.threads.GetUntracked(),
			Providers: owner.providers.GetUntracked(), Models: owner.models.GetUntracked()}
		owner.applyState(st)
	})
	rendered.WaitUntil(5*time.Second, func() bool {
		return rendered.Find("id:current-environment-name").Text == "renamed-desktop"
	})
	if field := rendered.Find("label:Message"); field.ID != inputID || field.Text != "Keep this draft" || !field.Focused {
		t.Fatal("hostname refresh disturbed the composer")
	}
	raw, _ := rendered.Storage().GetItem("environments:v1")
	var saved savedEnvironments
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if got := environmentDisplayNames(saved.Profiles)[fixture.profile.ID]; got != "renamed-desktop" {
		t.Fatalf("hostname refresh did not persist: %q", got)
	}
	rendered.Post(func() {
		profile := manager.find(fixture.profile.ID)
		profile.Alias = "Office"
		manager.update(profile)
		st := proto.State{Hostname: "another-hostname", Settings: owner.settings.GetUntracked(),
			Projects: owner.projects.GetUntracked(), Threads: owner.threads.GetUntracked(),
			Providers: owner.providers.GetUntracked(), Models: owner.models.GetUntracked()}
		owner.applyState(st)
	})
	rendered.WaitUntil(5*time.Second, func() bool {
		return rendered.Find("id:current-environment-name").Text == "Office"
	})
}
