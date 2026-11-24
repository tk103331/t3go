package app

import (
	"testing"
	"time"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// TestUIEnvironmentCredentialsAndAlias opens the environments section of
// the settings and uses its entries: the name an environment carries and
// the token that lets this client ask it for work.
func TestUIEnvironmentCredentialsAndAlias(t *testing.T) {
	_, ts, cfg := startEnv(t)
	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")

	session.openSettings()
	tester.pump()
	if err := tester.tt.Click(i18n.T("Environments")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("Add environment")) {
		t.Fatalf("the environments section did not open: %v", tester.tt.Texts())
	}

	// The alias editor names the environment on this client.
	if err := tester.tt.Click(i18n.T("Edit alias")); err != nil {
		t.Fatalf("open the alias editor: %v", err)
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("Save alias")) {
		t.Fatalf("the alias editor did not open: %v", tester.tt.Texts())
	}
	if err := tester.tt.Click(i18n.T("Alias")); err != nil {
		t.Fatalf("focus the alias field: %v", err)
	}
	tester.tt.Type("Work laptop")
	tester.pump()
	if err := tester.tt.Click(i18n.T("Save alias")); err != nil {
		t.Fatalf("save the alias: %v", err)
	}
	tester.pump()
	m := tester.manager
	profile := m.find(m.current)
	if profile.Alias != "Work laptop" {
		t.Fatalf("the environment's alias is %q, want %q", profile.Alias, "Work laptop")
	}
	if m.environmentNames()[profile.ID] != "Work laptop" {
		t.Fatalf("the environment is still named %q", m.environmentNames()[profile.ID])
	}

	// The token dialog changes the token the environment answers to.
	if err := tester.tt.Click(i18n.T("Update token")); err != nil {
		t.Fatalf("open the token dialog: %v", err)
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("Environment token")) {
		t.Fatalf("the token dialog did not open: %v", tester.tt.Texts())
	}
	if !tester.tt.HasText(i18n.T("Save and reconnect")) {
		t.Fatalf("the token dialog has no save action: %v", tester.tt.Texts())
	}
	// An empty token is refused, and the dialog stays open.
	if err := tester.tt.Click(i18n.T("Save and reconnect")); err != nil {
		t.Fatalf("save an empty token: %v", err)
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("Environment token")) {
		t.Fatal("the token dialog closed on an empty token")
	}
	if !tester.tt.HasText(i18n.T("Enter the environment token.")) {
		t.Fatalf("the token dialog did not report the empty token: %v", tester.tt.Texts())
	}
}

// TestGraphLanes lays a history out in lanes: a commit takes the lane
// that waited for it, and a merge opens a lane of its own.
func TestGraphLanes(t *testing.T) {
	commits := []proto.GraphCommit{
		{Hash: "c3", Parents: []string{"c2"}},
		{Hash: "c2", Parents: []string{"c1", "b1"}},
		{Hash: "c1", Parents: []string{"root"}},
		{Hash: "b1", Parents: []string{"root"}},
		{Hash: "root"},
	}
	rows, lanes := graphLanes(commits)
	if len(rows) != len(commits) {
		t.Fatalf("the graph has %d rows, want %d", len(rows), len(commits))
	}
	if lanes != 2 {
		t.Fatalf("the graph has %d lanes, want 2", lanes)
	}
	if rows[0].lane != 0 {
		t.Fatalf("the newest commit sits in lane %d, want 0", rows[0].lane)
	}
	if rows[1].lane != 0 {
		t.Fatalf("the merge sits in lane %d, want 0", rows[1].lane)
	}
	// The side branch takes a lane of its own and passes the merge.
	found := false
	for i := range rows {
		if rows[i].lane == 1 {
			found = true
			if i >= 2 && !rows[2].lanes[1] {
				t.Fatal("the side branch's lane does not pass the merge")
			}
		}
	}
	if !found {
		t.Fatal("the side branch took no lane of its own")
	}
}
