package app

import (
	"context"
	"fmt"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

func TestUIResponsiveMobileControls(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	project, err := srv.Service.CreateProject(context.Background(), "Phone workspace", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(context.Background(), orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "A conversation with a long title on a phone", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{320, 390, 744} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tester, session := startAppAt(t, ts, cfg, width, 568, nil)
			tester.wait(t, 15*time.Second, func() bool { return session != nil && session.connected && len(session.projects) == 1 }, "connect")
			app := &clientApp{manager: tester.manager}
			tester.tt = ui.NewTester(app.root, width, 568)
			tester.tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true, NoAnyHover: true})
			tester.tt.SetInsets(24, 0, 20, 0)
			visible := func(label string, minSize float32) {
				t.Helper()
				a, ok := tester.tt.Access(i18n.T(label))
				if !ok || a.Bounds.X < 0 || a.Bounds.Y < 24 || a.Bounds.X+a.Bounds.W > float32(width)+1 ||
					a.Bounds.Y+a.Bounds.H > 549 || a.Bounds.W < minSize || a.Bounds.H < minSize {
					t.Errorf("%s is clipped or too small to tap: %+v", label, a.Bounds)
				}
			}
			session.selectThread(thread.ID)
			tester.wait(t, 15*time.Second, func() bool { return session.detail.Thread.ID == thread.ID }, "thread")
			for _, label := range []string{"Back to the thread list", "Files", "Git controls", "Terminal", "Thread actions", "Attach files", i18n.T("Config"), "Send"} {
				visible(label, 44)
			}
			session.selected, session.mobilePage = "", "threads"
			tester.pump()
			for _, label := range []string{"Chats", "Pulls", "Usage", "Settings", "New thread"} {
				visible(label, 44)
			}
			session.mobilePage = "projects"
			tester.pump()
			if !tester.tt.HasText(project.Name) {
				t.Error("the projects page does not list its projects")
			}
			if err := tester.tt.Click(project.Name); err != nil {
				t.Fatal(err)
			}
			tester.wait(t, 15*time.Second, func() bool { return !session.newThreadBranchesBusy }, "branches")
			if !session.showNewThread || session.newThreadProj != project.ID {
				t.Fatal("the project row did not open a thread in its workspace")
			}
			visible("Create", 44)
			visible("Create thread", 44)
			visible("Attach files", 44)
			for _, label := range []string{i18n.T("Agent") + " " + session.newThreadAgentLabel(), i18n.T("Model"), session.newThreadPermissionOption().pillLabel()} {
				visible(label, 44)
			}
			// An empty thread is valid on both clients. The phone's
			// create action must not silently do nothing on an empty draft.
			session.newThreadProv, session.newThreadModel = "side-fake", "side-fake-1"
			if err := tester.tt.Click(i18n.T("Create")); err != nil {
				t.Fatal(err)
			}
			tester.wait(t, 15*time.Second, func() bool {
				return !session.showNewThread && session.selected != "" && session.selected != thread.ID
			}, "an empty thread to open")
			session.selected = ""
			session.openSettings()
			tester.pump()
			if err := tester.tt.Click(i18n.T("Appearance")); err != nil {
				t.Fatal(err)
			}
			tester.pump()
			if session.settingsSection != "appearance" || !tester.tt.HasText(i18n.T("Theme")) {
				t.Fatal("Appearance did not open the client's theme settings")
			}
			visible("Back to settings", 44)
			heading, ok := tester.tt.Find(i18n.T("Appearance"))
			if !ok || heading.Y > 80 || heading.W < 150 {
				t.Errorf("the settings heading is unnecessarily truncated: %+v", heading)
			}
		})
	}
}

func TestUIProjectDialogInShortViewport(t *testing.T) {
	for _, size := range []struct{ w, h int }{{320, 360}, {900, 480}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			s := &Session{showNewProject: true}
			tt := ui.NewTester(func(c *ui.Context) { newProjectDialog(c, s) }, size.w, size.h)
			if size.w < 760 {
				tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true})
			}
			tt.Frame()
			path, ok := tt.Access(i18n.T("Project path"))
			if !ok || path.Bounds.W < 160 {
				t.Fatalf("the path field is squeezed by Browse: %+v", path.Bounds)
			}
			// Scroll along the panel's padded edge, outside nested inputs.
			tt.Scroll(float32(size.w)/2+min(float32(size.w-32), 520)/2-8, float32(size.h)/2, 0, 2000)
			action, ok := tt.Access(i18n.T("Add project"))
			if !ok || action.Bounds.W < 80 || action.Bounds.H < 28 || action.Bounds.Y+action.Bounds.H > float32(size.h-16)+1 {
				t.Fatalf("the form's submit action is not reachable: %+v", action.Bounds)
			}
			if err := tt.Click(i18n.T("Cancel")); err != nil {
				t.Fatal(err)
			}
			if s.showNewProject {
				t.Fatal("the scrolled form did not close")
			}
		})
	}
}

func TestUIResponsiveDesktopPanes(t *testing.T) {
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	project, err := srv.Service.CreateProject(context.Background(), "A project with a longer workspace name", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []struct {
		w, h int
		bar  ui.TitleBar
	}{{800, 600, ui.TitleBar{Height: 56, Left: 80}}, {900, 600, ui.TitleBar{Height: 56, Right: 138}}, {1440, 900, ui.TitleBar{}}} {
		t.Run(fmt.Sprint(size.w), func(t *testing.T) {
			tester, session := startAppAt(t, ts, cfg, size.w, size.h, nil)
			tester.wait(t, 15*time.Second, func() bool { return session != nil && session.connected && len(session.projects) == 1 }, "connect")
			session.rightOpen, session.surface, session.tabs = true, "", nil
			tester.tt.SetTitleBar(size.bar)
			session.openNewThreadIn(project.ID)
			tester.wait(t, 15*time.Second, func() bool { return !session.newThreadBranchesBusy }, "branches")
			projectBounds, ok := tester.tt.Access(i18n.T("Project: ") + project.Name)
			if !ok || projectBounds.Bounds.W < 80 {
				t.Fatal("the project's picker is not visible")
			}
			// A short window may need to scroll its new-thread form.
			sidebar, room, _ := session.desktopPaneWidths(float32(size.w))
			tester.tt.Scroll(sidebar+room/2, float32(size.h)/2, 0, 1200)
			for _, label := range []string{"Toggle sidebar", "Toggle right panel", "Close right panel", "Create thread"} {
				bounds, ok := tester.tt.Find(i18n.T(label))
				if !ok || bounds.W < 28 || bounds.H < 28 || bounds.X < 0 || bounds.Y < 0 ||
					bounds.X+bounds.W > float32(size.w)-size.bar.Right+1 || bounds.Y+bounds.H > float32(size.h) {
					t.Errorf("%s overflows the desktop: %+v", label, bounds)
				}
			}
			session.showNewThread = false
			session.openSettings()
			tester.pump()
			close, ok := tester.tt.Access(i18n.T("Close settings"))
			if !ok || close.Bounds.W < 28 || close.Bounds.Y+close.Bounds.H > float32(size.h) {
				t.Fatalf("settings cannot be closed: %+v", close.Bounds)
			}
		})
	}
}

func TestUIProviderModeSwitchHasOneButtonPerMode(t *testing.T) {
	for _, mode := range []string{"registry", "local"} {
		t.Run(mode, func(t *testing.T) {
			s := &Session{providerAddOpen: true, providerAddMode: mode}
			tt := ui.NewTester(func(c *ui.Context) { s.providerAddPanel(c) }, 320, 568)
			tt.Frame()
			for _, label := range []string{"ACP Registry", "Local command"} {
				count := 0
				for _, a := range tt.AccessAll() {
					if a.Name == i18n.T(label) && a.Role == ui.RoleButton {
						count++
					}
				}
				if count != 1 {
					t.Errorf("%s renders %d controls, want one", label, count)
				}
			}
		})
	}
}

// A finger press and release arrive in separate frames. Taking focus
// from the editor on press used to collapse it and move Send before the
// release, so the first tap only dismissed the keyboard.
func TestUIMobileComposerSendWhileTyping(t *testing.T) {
	draft, sent := "", ""
	s := &Session{}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Justify(ui.End).Padding(16).Children(func() {
			s.composerCard(c, composerSpec{Draft: &draft, Label: "Message", FocusLines: 3,
				OnSend: func() { sent, draft = draft, "" }})
		})
	}, 390, 600)
	tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true})
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("tap once")
	button, ok := tt.Find(i18n.T("Send"))
	if !ok {
		t.Fatal("Send is missing")
	}
	x, y := button.X+button.W/2, button.Y+button.H/2
	tt.Press(x, y)
	tt.Frame()
	if !tt.Focused("Message") {
		t.Fatal("pressing Send took focus from the editor")
	}
	tt.Release(x, y)
	tt.Frame()
	if sent != "tap once" {
		t.Fatalf("a single tap did not send the draft: %q", sent)
	}
}

func TestUIMobileComposerWaitsForMarkedText(t *testing.T) {
	for _, tc := range []struct{ name, prefix, marked string }{
		{"word only", "", "hello"},
		{"last word", "android native ", "smoke"},
		{"Chinese input", "请检查", "布局"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft, sent := "", ""
			s := &Session{}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Justify(ui.End).Padding(16).Children(func() {
					s.composerCard(c, composerSpec{Draft: &draft, Label: "Message", FocusLines: 3,
						OnSend: func() { sent, draft = draft, "" }})
				})
			}, 390, 600)
			tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true})
			if err := tt.Click("Message"); err != nil {
				t.Fatal(err)
			}
			tt.Type(tc.prefix)
			tt.Compose(tc.marked, utf8.RuneCountInString(tc.marked))
			button, ok := tt.Find(i18n.T("Send"))
			if !ok {
				t.Fatal("Send is missing")
			}
			x, y := button.X+button.W/2, button.Y+button.H/2
			tt.Press(x, y)
			tt.Frame()
			tt.Release(x, y)
			tt.Frame()
			if sent != "" || !tt.Focused("Message") {
				t.Fatal("Send submitted partial text or discarded the active candidate")
			}
			tt.Type(tc.marked)
			if err := tt.Click(i18n.T("Send")); err != nil {
				t.Fatal(err)
			}
			if sent != tc.prefix+tc.marked {
				t.Fatalf("the composed word was lost: got %q, want %q", sent, tc.prefix+tc.marked)
			}
		})
	}
}
