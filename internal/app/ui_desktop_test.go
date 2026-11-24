package app

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// Exercise the rendered controls and their positions, including a window
// resize: saved panel widths must describe the panel, not the transcript.
func TestUIDesktopLayout(t *testing.T) {
	_, ts, cfg := startEnv(t)
	tester, session := startAppAt(t, ts, cfg, 1280, 820, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")

	click := func(label string) {
		t.Helper()
		if err := tester.tt.Click(i18n.T(label)); err != nil {
			t.Fatalf("click %s: %v", label, err)
		}
		tester.pump()
	}
	if _, ok := tester.tt.Access(i18n.T("Search threads")); !ok {
		t.Fatal("sidebar search is missing")
	}
	click("Toggle sidebar")
	if session.sidebarOpen {
		t.Fatal("sidebar did not close")
	}
	if _, ok := tester.tt.Access(i18n.T("Search threads")); ok {
		t.Fatal("hidden sidebar still renders its search field")
	}
	click("Toggle sidebar")
	if _, ok := tester.tt.Access(i18n.T("Search threads")); !ok {
		t.Fatal("sidebar did not return")
	}

	// With no surface tabs, the panel's title marks its left edge.
	session.tabs = nil
	click("Toggle right panel")
	for _, width := range []int{900, 1280, 1600} {
		tester.tt.SetSize(width, 820)
		tester.pump()
		title, ok := tester.tt.Find(i18n.T("Surfaces"))
		if !ok {
			t.Fatal("panel title is missing")
		}
		panelWidth := float32(360)
		if width == 900 {
			panelWidth = 280
		}
		if want := float32(width) - panelWidth; math.Abs(float64(title.X-want)) > 24 {
			t.Fatalf("window %d: panel starts at %.1f, want near %.1f", width, title.X, want)
		}
	}
	if !tester.tt.HasText(i18n.T("Open a surface")) {
		t.Fatal("the empty panel does not offer a surface picker")
	}
	click("Files")
	if session.surface != "files" || len(session.tabs) != 1 {
		t.Fatal("the empty panel's Files action did not open a tab")
	}
	session.openSurface("diff")
	tester.pump()
	click("Close Files")
	if session.surface != "diff" || len(session.tabs) != 1 || session.tabs[0] != "diff" {
		t.Fatal("closing a background tab changed the active surface")
	}
	click("Close Diff")
	if !tester.tt.HasText(i18n.T("Open a surface")) {
		t.Fatal("closing the last surface did not restore the picker")
	}
	click("Close right panel")
	if session.rightOpen {
		t.Fatal("panel did not close")
	}
}

func TestUIDesktopNewThreadWorkspace(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	project, err := srv.Service.CreateProject(context.Background(), "Desktop workspace", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 1000, 760, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.projects) == 1 && len(session.providers) > 0
	}, "the project and providers to arrive")
	session.openNewThreadIn(project.ID)
	session.newThreadProv, session.newThreadModel = "side-fake", "side-fake-1"
	tester.wait(t, 10*time.Second, func() bool {
		return !session.newThreadBranchesBusy && len(session.newThreadBranches) > 0
	}, "the branches to arrive")
	field, ok := tester.tt.Access(i18n.T("New thread message"))
	if !ok || field.Bounds.H < 40 || field.Bounds.H > 90 {
		t.Fatalf("new thread field is not compact: %+v", field.Bounds)
	}
	if _, ok := tester.tt.Access(i18n.T("Project: ") + project.Name); !ok {
		t.Fatal("selected project is not named before opening its menu")
	}
	if _, ok := tester.tt.Access(i18n.T("Branch")); !ok {
		t.Fatal("Git project's branch picker is missing")
	}
	if err := tester.tt.Click(i18n.T("Worktree: ") + i18n.T("Current checkout")); err != nil {
		t.Fatal(err)
	}
	if err := tester.tt.ChooseMenuItem(i18n.T("New worktree")); err != nil {
		t.Fatal(err)
	}
	tester.pump()
	if !session.newThreadWT {
		t.Fatal("worktree selection did not change")
	}
	// The create action must stay inside the conversation even with the
	// sidebar and a right panel taking space on a smaller desktop.
	session.rightOpen, session.tabs = true, nil
	tester.pump()
	create, ok := tester.tt.Access(i18n.T("Create thread"))
	if !ok || create.Bounds.W < 26 || create.Bounds.H < 28 || create.Bounds.X+create.Bounds.W > 720 {
		t.Fatalf("create action is clipped by the right panel: %+v", create.Bounds)
	}
	for _, label := range []string{i18n.T("Agent") + " " + session.newThreadAgentLabel(), i18n.T("Model"), session.newThreadPermissionOption().pillLabel()} {
		control, ok := tester.tt.Access(label)
		sharesLine := control.Bounds.Y < create.Bounds.Y+create.Bounds.H && control.Bounds.Y+control.Bounds.H > create.Bounds.Y
		if !ok || control.Bounds.W < 40 || control.Bounds.X+control.Bounds.W > create.Bounds.X+create.Bounds.W ||
			(sharesLine && control.Bounds.X+control.Bounds.W > create.Bounds.X-4) {
			t.Fatalf("%s overlaps the create action: %+v, send %+v", label, control.Bounds, create.Bounds)
		}
	}
	// Close the panel and create an empty thread through the actual button.
	if err := tester.tt.Click(i18n.T("Close right panel")); err != nil {
		t.Fatal(err)
	}
	if err := tester.tt.Click(i18n.T("Create thread")); err != nil {
		t.Fatal(err)
	}
	tester.wait(t, 15*time.Second, func() bool {
		return !session.threadBusy && session.detail.Thread.ID != ""
	}, "the created thread to open")
	if session.detail.Thread.WorktreePath == "" || session.detail.Thread.ProjectID != project.ID {
		t.Fatalf("thread did not use the selected workspace: %+v", session.detail.Thread)
	}
}

func TestUIDesktopLongTitleActions(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Desktop", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: strings.Repeat("Long thread title ", 12),
		Provider: "side-fake", Model: "side-fake-1", Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	tester, session := startAppAt(t, ts, cfg, 900, 760, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected && len(session.threads) == 1
	}, "the thread to arrive")
	session.selectThread(thread.ID)
	tester.wait(t, 10*time.Second, func() bool {
		return session.detail.Thread.ID == thread.ID
	}, "the thread to open")
	session.rightOpen, session.tabs = true, nil
	tester.pump()
	for _, label := range []string{"Toggle sidebar", "Thread actions", "Toggle right panel", "Send"} {
		action, ok := tester.tt.Access(i18n.T(label))
		if !ok || action.Bounds.W < 28 || action.Bounds.H < 28 || action.Bounds.X+action.Bounds.W > 620 {
			t.Fatalf("%s is clipped with a long title: %+v", label, action.Bounds)
		}
	}
	if err := tester.tt.Click(i18n.T("Thread actions")); err != nil {
		t.Fatal(err)
	}
	if err := tester.tt.ChooseMenuItem(i18n.T("Rename thread…")); err != nil {
		t.Fatal(err)
	}
	if !session.renameOpen {
		t.Fatal("thread action did not open the rename dialog")
	}
}

func TestUIDesktopSettingsSections(t *testing.T) {
	_, ts, cfg := startEnv(t)
	tester, session := startAppAt(t, ts, cfg, 1000, 640, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	session.openSettings()
	tester.pump()
	for _, key := range []string{"appearance", "shortcuts", "general", "about"} {
		for _, section := range settingsSections() {
			if section.key != key {
				continue
			}
			if err := tester.tt.Click(section.label); err != nil {
				t.Fatalf("click %s: %v", key, err)
			}
			tester.pump()
			if session.settingsSection != key {
				t.Fatalf("section = %s, want %s", session.settingsSection, key)
			}
		}
		close, ok := tester.tt.Access(i18n.T("Close settings"))
		if !ok || close.Bounds.W < 28 || close.Bounds.H < 28 || close.Bounds.Y+close.Bounds.H > 640 {
			t.Fatalf("settings close button is not visible and usable: %+v", close.Bounds)
		}
	}
	if err := tester.tt.Click(i18n.T("Close settings")); err != nil {
		t.Fatal(err)
	}
	if session.showSettings {
		t.Fatal("settings did not close")
	}
}

func TestUIDesktopNavigationWhileTyping(t *testing.T) {
	_, ts, cfg := startEnv(t)
	tester, session := startApp(t, ts, cfg, nil)
	tester.wait(t, 15*time.Second, func() bool {
		return session != nil && session.connected
	}, "the environment to connect")
	app := &clientApp{manager: tester.manager}
	tt := ui.NewTester(app.root, 1280, 820)
	for _, key := range []ui.Key{ui.KeyComma, ui.KeyK} {
		if err := tt.Click(i18n.T("Search threads")); err != nil {
			t.Fatal(err)
		}
		tt.Type("draft")
		before := session.sidebarQuery
		tt.Key(ui.Cmd, key)
		if key == ui.KeyComma && !session.showSettings {
			t.Fatal("Cmd+, did not open settings while typing")
		}
		if key == ui.KeyK && !session.showPalette {
			t.Fatal("Cmd+K did not open the command palette while typing")
		}
		if session.sidebarQuery != before {
			t.Fatal("navigation changed the field's text")
		}
		tt.Key(0, ui.KeyEscape)
	}
}
