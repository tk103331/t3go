package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
)

// Opt-in images make layout changes inspectable without writing build
// artifacts into the repository during a normal test run.
func TestUILayoutSnapshots(t *testing.T) {
	out := os.Getenv("T3GO_UI_SNAPSHOT_DIR")
	if out == "" {
		t.Skip("set T3GO_UI_SNAPSHOT_DIR to save rendered layouts")
	}
	srv, ts, cfg := startEnv(t)
	srv.Service.Providers.Register(sideRunner{})
	ctx := context.Background()
	project, err := srv.Service.CreateProject(ctx, "Demo", gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	thread, err := srv.Service.CreateThread(ctx, orchestrator.CreateThreadInput{
		ProjectID: project.ID, Title: "Layout review", Provider: "side-fake", Model: "side-fake-1",
		Permission: proto.PermissionFullAccess,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "m1", ThreadID: thread.ID, Role: proto.RoleUser, Kind: proto.KindText, Content: "hello", Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Store.AppendMessage(ctx, proto.Message{ID: "m2", ThreadID: thread.ID, Role: proto.RoleAssistant, Kind: proto.KindText, Content: "Hello! 👋 How can I help you today?", Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, size := range []struct {
		name string
		w, h int
		dark bool
	}{{"desktop", 1440, 960, false}, {"desktop-dark", 1440, 960, true}, {"narrow-desktop", 1000, 760, false},
		{"short-desktop", 900, 600, false}, {"phone", 390, 844, false}, {"small-phone", 320, 568, false}, {"phone-dark", 390, 844, true}} {
		t.Run(size.name, func(t *testing.T) {
			tester, session := startAppAt(t, ts, cfg, size.w, size.h, nil)
			tester.wait(t, 15*time.Second, func() bool { return session != nil && session.connected }, "connect")
			if size.dark {
				tester.manager.appearance.Theme = "dark"
			}
			app := &clientApp{manager: tester.manager}
			tester.tt = ui.NewTester(app.root, size.w, size.h)
			if size.w < 760 {
				tester.tt.SetPreferences(ui.Preferences{Pointer: ui.PointerCoarse, NoHover: true, NoAnyHover: true})
			}
			session.selectThread(thread.ID)
			tester.wait(t, 15*time.Second, func() bool { return len(session.messages) >= 2 }, "messages")
			session.rightOpen, session.surface, session.tabs = true, "", nil
			tester.pump()
			body, ok := tester.tt.Find("hello")
			if !ok || body.H != 19 {
				t.Fatalf("user message is not one line high: %+v", body)
			}
			save := func(name string) {
				t.Helper()
				file, err := os.Create(filepath.Join(out, size.name+"-"+name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				if err := png.Encode(file, tester.tt.Image()); err != nil {
					t.Fatal(err)
				}
			}
			save("conversation")
			if size.w >= 760 {
				session.openSurface("diff")
				tester.wait(t, 15*time.Second, func() bool { return !session.rightBusy }, "diff")
				tester.pump()
				save("diff")
				session.openNewThreadIn(project.ID)
				tester.wait(t, 15*time.Second, func() bool { return !session.newThreadBranchesBusy }, "branches")
				tester.pump()
				if _, ok := tester.tt.Access(i18n.T("Create thread")); !ok {
					t.Fatal("new thread action is missing")
				}
				save("new-thread")
			} else {
				session.openMobileSurface("files")
				tester.wait(t, 15*time.Second, func() bool { return !session.rightBusy }, "files")
				save("files")
				session.closeMobileSurface()
				session.selected, session.mobilePage = "", "threads"
				tester.pump()
				save("inbox")
				session.mobilePage = "projects"
				tester.pump()
				save("projects")
				session.openNewThreadIn(project.ID)
				tester.wait(t, 15*time.Second, func() bool { return !session.newThreadBranchesBusy }, "branches")
				save("new-thread")
			}
			session.showNewThread = false
			session.openSettings()
			tester.pump()
			save("settings")
			if size.w < 760 {
				session.openMobileSettingsSection("appearance")
			} else {
				session.setSettingsSection("appearance")
			}
			tester.pump()
			save("appearance")
			if size.w < 760 {
				session.openMobileSettingsSection("providers")
			} else {
				session.setSettingsSection("providers")
			}
			session.providerAddOpen, session.providerAddMode = true, "local"
			tester.pump()
			save("providers")
			session.closeSettings()
			session.openNewProject()
			tester.pump()
			save("new-project")
			session.showNewProject = false
			session.showDirs = true
			session.loadDirs(project.Path)
			tester.wait(t, 15*time.Second, func() bool { return !session.dirsBusy }, "folders")
			save("folders")
		})
	}
}
