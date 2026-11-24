package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func configTestOptions(picked *string) []configOption {
	return []configOption{
		{
			key:         "permission",
			label:       "Permission",
			title:       "Permission",
			description: "Set what the agent may change.",
			value:       "supervised",
			choices: []configChoice{
				{value: "supervised", label: "supervised"},
				{value: "full-access", label: "full-access"},
			},
			choose: func(value string) { *picked = "permission:" + value },
		},
		{
			key:         "plan",
			label:       "Plan",
			title:       "Plan mode",
			description: "Plan the work.",
			value:       "false",
			choices: []configChoice{
				{value: "false", label: "Off"},
				{value: "true", label: "On"},
			},
			choose: func(value string) { *picked = "plan:" + value },
		},
	}
}

// A wide middle pane shows each config option as its own pill, as the
// reference design does. There is no enclosing config popover.
func TestUIConfigOptionsWide(t *testing.T) {
	var picked string
	tt := ui.NewTester(func(c *ui.Context) {
		s := &Session{}
		s.configOptions(c, configTestOptions(&picked))
	}, 900, 640)
	if !tt.HasText("Permission: supervised") || !tt.HasText("Plan: Off") {
		t.Fatalf("individual config pills are missing: %v", tt.Texts())
	}
	if _, ok := tt.Access("Config"); ok {
		t.Fatal("a wide pane should not gather the config pills")
	}
	if err := tt.Click("Permission: supervised"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Set what the agent may change.") {
		t.Fatal("a config pill did not open the titled popover")
	}
	if err := tt.Click("full-access"); err != nil {
		t.Fatal(err)
	}
	if picked != "permission:full-access" {
		t.Fatalf("choosing a value did not reach the option: %q", picked)
	}
}

// A narrow middle pane gathers the options into one popover, without
// touching the model picker, which the caller draws next to it.
func TestUIConfigOptionsCollapseKeepsModelSeparate(t *testing.T) {
	var picked string
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Justify(ui.End).Padding(16).Children(func() {
			s := &Session{}
			s.configOptions(c, configTestOptions(&picked))
			ui.Button(c, "Choose model: Test Model")
		})
	}, 390, 640)
	tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true})
	if _, ok := tt.Access("Config"); !ok {
		t.Fatalf("a narrow pane did not gather the config options: %v", tt.Texts())
	}
	if tt.HasText("Permission: supervised") {
		t.Fatal("individual config pills are still shown in a narrow pane")
	}
	if err := tt.Click("Config"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Permission", "Set what the agent may change.",
		"full-access", "Plan mode", "Plan the work.", "On"} {
		if !tt.HasText(label) {
			t.Fatalf("the config popover is missing %q: %v", label, tt.Texts())
		}
	}
	// The model stays its own button and remains reachable while the
	// config popover is open: it is not one of the gathered options.
	if err := tt.Click("Choose model: Test Model"); err != nil {
		t.Fatalf("the config popover covered model selection: %v", err)
	}
	if picked != "" {
		t.Fatalf("clicking the model changed a config option: %q", picked)
	}
	if err := tt.Click("Config"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("On"); err != nil {
		t.Fatal(err)
	}
	if picked != "plan:true" {
		t.Fatalf("a gathered option did not apply its choice: %q", picked)
	}
	if tt.HasText("Set what the agent may change.") {
		t.Fatal("the config popover did not close after a choice")
	}
}

// The gathered panel scrolls inside a short viewport instead of pushing
// its last option off screen.
func TestUIConfigOptionsShortViewport(t *testing.T) {
	var picked string
	tt := ui.NewTester(func(c *ui.Context) {
		top, _, bottom, _ := c.SafeArea()
		ui.Column(c).Fill().Justify(ui.End).Padding(top+8, 8, bottom+8, 8).Children(func() {
			s := &Session{}
			s.configOptions(c, configTestOptions(&picked))
		})
	}, 320, 360)
	tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true})
	tt.SetInsets(24, 0, 180, 0)
	if err := tt.Click("Config"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Access("On"); !ok {
		t.Fatal("the last config choice is absent from the panel")
	}
	// The short panel scrolls: bring its last section into view.
	for range 6 {
		tt.Scroll(150, 140, 0, 60)
		tt.Frame()
	}
	if err := tt.Click("On"); err != nil {
		t.Fatalf("an option in the scrolling panel cannot be tapped: %v", err)
	}
	if picked != "plan:true" {
		t.Fatalf("the short-viewport panel did not apply the choice: %q", picked)
	}
}

// The real phone composer keeps the model button out of the config
// popover and keeps both reachable.
func TestUIThreadConfigOptionsIntegration(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Config phone", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Config thread", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 390, 844, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "connect")
	session.selectThread(thread.ID)
	tester.wait(t, 15*time.Second, func() bool { return session.detail.Thread.ID == thread.ID }, "thread")
	tester.pump()
	// More than the short-menu threshold, so the model button opens its
	// search picker instead of the native menu.
	for i := 0; i < 12; i++ {
		session.models = append(session.models, proto.Model{ID: fmt.Sprintf("extra-%d", i),
			Name: fmt.Sprintf("Extra %d", i), Provider: "side-fake"})
	}
	tester.pump()
	if !tester.tt.HasText(i18n.T("Config")) {
		t.Fatalf("the phone composer has no config pill: %v", tester.tt.Texts())
	}
	if err := tester.tt.Click(i18n.T("Config")); err != nil {
		t.Fatal(err)
	}
	if !tester.tt.HasText(i18n.T("Plan mode")) {
		t.Fatal("the config popover does not hold the plan option")
	}
	if _, ok := tester.tt.Access(i18n.T("Search models")); ok {
		t.Fatal("model selection leaked into the config popover")
	}
	if err := tester.tt.Click(i18n.T("Choose model: ") + session.modelDisplayName(thread.Provider, thread.Model)); err != nil {
		t.Fatalf("model selection is not reachable beside the config popover: %v", err)
	}
	if _, ok := tester.tt.Access(i18n.T("Search models")); !ok {
		t.Fatal("the model button did not open its own picker")
	}
}
