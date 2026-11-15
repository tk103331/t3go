package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/orchestrator"
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
				{ID: "cloud-one", Name: "Cloud", Routes: direct("http://cloud.example")},
				{ID: "desktop-one", Name: "T3Go", Alias: " My desktop ", Routes: direct("http://desktop.local")},
			},
			want: []string{"Cloud", "My desktop"},
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
			names := []string{strings.TrimPrefix(ta.URL, "http://"), strings.TrimPrefix(tb.URL, "http://")}
			app.WaitFor("id:thread-"+pb.ID+"/"+thread.ID, 10*time.Second)
			if width < 760 {
				app.WaitUntil(5*time.Second, func() bool {
					return app.Find("id:mobile-thread-environment-"+pb.ID+"/"+thread.ID).Text == names[1]
				})
				openMobileProjects(app)
			}
			app.WaitUntil(5*time.Second, func() bool {
				return app.Find("id:environment-status-"+pa.ID).Text == "Direct" &&
					app.Find("id:environment-status-"+pb.ID).Text == "Relay"
			})
			for i, p := range []client.EnvironmentProfile{pa, pb} {
				app.WaitFor("id:sidebar-environment-"+p.ID+" >> text:"+names[i], 5*time.Second)
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
			app.Press("id:environment-rename-" + pb.ID)
			app.Type("label:Environment alias", "My desktop")
			app.Press("label:Save alias")
			app.WaitFor("id:environment-"+pb.ID+" >> text:My desktop", 5*time.Second)
			app.Press("id:settings-close")
			waitSheetGone(app, "id:settings-dialog")
			if width < 760 {
				openMobileProjects(app)
			}
			app.WaitFor("id:sidebar-environment-"+pb.ID+" >> text:My desktop", 5*time.Second)
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
