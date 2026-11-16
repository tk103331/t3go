package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
	"github.com/tk/t3go/internal/proto"
)

func checkMobileControlWidths(t *testing.T, rendered *nsgotest.App, width core.Points) {
	t.Helper()
	for _, selector := range []string{"role:button", "role:textbox"} {
		for _, view := range rendered.Query(selector) {
			frame := mobileScreenFrame(view)
			if frame.Size.Width <= 0 || frame.Size.Height <= 0 {
				continue
			}
			if frame.Origin.X < -1 || frame.Origin.X+frame.Size.Width > width+1 {
				t.Errorf("%s %q overflows %g-point screen: %v", selector, view.Props.TestID, width, frame)
			}
		}
	}
}

func TestUIMobileLayoutMatrix(t *testing.T) {
	alpha, beta := newSettingsFixture(t, "Alpha"), newSettingsFixture(t, "Beta")
	alpha.profile.Alias = "Desktop with a very long environment name"
	beta.profile.Alias = "Remote development server with a very long environment name"
	rendered, manager := startMobilePages(t, alpha, beta)
	owner := manager.app
	screens := []struct {
		name string
		open func()
	}{
		{"chats", func() { owner.mobilePage.Set("threads") }},
		{"projects", func() { owner.mobilePage.Set("projects") }},
		{"usage", func() { owner.mobilePage.Set("usage"); owner.loadMobileUsage() }},
		{"pulls", func() { owner.mobilePage.Set("prs") }},
		{"settings", func() { owner.openSettings() }},
		{"new-thread", func() {
			owner.openNewThread()
			if target := owner.newThreadTarget(); target != nil {
				target.newThreadModel.Set("provider/a-very-long-model-name-with-a-version")
				projects := target.projects.GetUntracked()
				for i := range projects {
					if projects[i].ID == target.newThreadProj.GetUntracked() {
						projects[i].IsGit = true
					}
				}
				target.projects.Set(projects)
				target.newThreadWT.Set(true)
				target.newThreadBranches.Set([]proto.Branch{{
					Name: "feature/a-very-long-development-branch-name", Current: true,
				}})
				target.newThreadBase.Set("feature/a-very-long-development-branch-name")
			}
		}},
		{"conversation", func() { owner.selectThread("same-thread") }},
	}
	for _, section := range []string{"general", "providers", "environments", "worktrees", "about"} {
		screens = append(screens, struct {
			name string
			open func()
		}{"settings/" + section, func() {
			owner.openSettings()
			owner.openMobileSettingsSection(section)
		}})
	}
	screens = append(screens, struct {
		name string
		open func()
	}{"settings/environment-defaults", func() {
		owner.openSettings()
		owner.openEnvironmentSettings(owner.profile.ID)
	}})
	if nsgotest.Target() != "" {
		t.Skip("the phone's real size is fixed; live iOS flows run in the target tests")
	}
	platforms := []struct {
		name string
		os   core.OS
		safe core.Edges
	}{
		{"android", core.OSAndroid, core.Edges{Top: 24, Bottom: 16}},
		{"ios", core.OSIOS, core.Edges{Top: 47, Bottom: 34}},
	}
	for _, platform := range platforms {
		for _, size := range []struct{ width, height, keyboard core.Points }{
			{320, 740, 0}, {390, 844, 0}, {320, 740, 300}, {390, 844, 300},
		} {
			for _, dark := range []bool{false, true} {
				rendered.Resize(size.width, size.height)
				rendered.SetEnvironment(core.Environment{
					OS: platform.os, Idiom: core.IdiomPhone, Dark: dark, KeyboardHeight: size.keyboard,
					SafeArea: platform.safe,
				})
				for _, screen := range screens {
					t.Run(fmt.Sprintf("%s/%g/%g/dark=%t/%s", platform.name, size.width, size.keyboard, dark, screen.name), func(t *testing.T) {
						rendered.Post(func() {
							owner.closeMobileThreadSearch()
							owner.selected.Set("")
							owner.showNewThread.Set(false)
							owner.showSettings.Set(false)
							owner.mobileSettingsDetail.Set("")
							owner.mobilePage.Set("threads")
							screen.open()
						})
						switch screen.name {
						case "new-thread":
							rendered.WaitFor("id:new-thread-page", 5*time.Second)
						case "conversation":
							rendered.WaitFor("id:composer", 5*time.Second)
						default:
							rendered.WaitFor("id:mobile-home", 5*time.Second)
						}
						rendered.Settle()
						checkMobileControlWidths(t, rendered, size.width)
						if screen.name == "settings" && rendered.Exists("id:settings-nav-shortcuts") {
							t.Error("mobile settings includes shortcuts")
						}
						if rendered.Exists("id:settings-close") {
							t.Error("mobile settings includes a Close button")
						}
						if screen.name == "new-thread" || screen.name == "conversation" {
							cardID := "composer-card"
							if screen.name == "new-thread" {
								cardID = "new-thread-composer"
							}
							card := mobileScreenFrame(rendered.Find("id:" + cardID))
							send := mobileScreenFrame(rendered.Find("id:composer-send"))
							if send.Size.Width != 38 || send.Size.Height != 38 {
								t.Errorf("send button has inconsistent size: %v", send)
							}
							if gap := card.Origin.X + card.Size.Width - send.Origin.X - send.Size.Width; gap < 0 || gap > 16 {
								t.Errorf("send button is not right aligned: card=%v send=%v", card, send)
							}
							if send.Origin.Y+send.Size.Height > size.height-size.keyboard {
								t.Errorf("send button is below the keyboard: %v", send)
							}
						}
						if screen.name == "chats" {
							if rendered.Exists("id:new-project") || rendered.Exists("id:open-palette") {
								t.Error("mobile thread inbox exposes desktop creation or search")
							}
							rendered.Find("id:mobile-thread-search-open")
						}
						if screen.name == "new-thread" {
							rendered.Find("id:new-thread-project-menu")
						}
					})
				}
			}
		}
	}
}
