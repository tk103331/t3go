package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
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
				target.newThreadBranch.Set("feature/a-very-long-development-branch-name")
				target.newThreadModel.Set("provider/a-very-long-model-name-with-a-version")
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
	for _, size := range []struct{ width, height, keyboard core.Points }{
		{320, 740, 0}, {390, 844, 0}, {320, 740, 300}, {390, 844, 300},
	} {
		for _, dark := range []bool{false, true} {
			rendered.Resize(size.width, size.height)
			rendered.SetEnvironment(core.Environment{
				OS: core.OSAndroid, Idiom: core.IdiomPhone, Dark: dark, KeyboardHeight: size.keyboard,
				SafeArea: core.Edges{Top: 24, Bottom: 16},
			})
			for _, screen := range screens {
				t.Run(fmt.Sprintf("%g/%g/dark=%t/%s", size.width, size.keyboard, dark, screen.name), func(t *testing.T) {
					rendered.Post(func() {
						owner.selected.Set("")
						owner.showNewThread.Set(false)
						owner.showSettings.Set(false)
						owner.mobileSettingsDetail.Set("")
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
				})
			}
		}
	}
}
