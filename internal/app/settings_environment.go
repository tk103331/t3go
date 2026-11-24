package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/i18n"
)

// settingsEnvironmentSelector chooses the environment the settings are
// about. The old client showed it above every section that reads from
// an environment.
func (s *Session) settingsEnvironmentSelector(c *ui.Context) {
	if s.manager == nil {
		return
	}
	t := c.Theme()
	names := s.manager.environmentNames()
	ui.Column(c).Gap(6).Children(func() {
		faint(c, i18n.T("Environment"), 12)
		ui.Scroll(c).MaxHeight(100).Children(func() {
			ui.Row(c).Wrap().Gap(6).Children(func() {
				for _, p := range s.manager.profiles {
					p := p
					session := s.manager.sessions[p.ID]
					if session == nil {
						continue
					}
					label := names[p.ID]
					if label == "" {
						label = session.envName()
					}
					button := ui.Button(c, label).MaxWidthPercent(100).MinHeight(controlMinHeight(c))
					if p.ID == s.settingsEnv {
						button.Background(t.Accent.Alpha(0.12))
					}
					button.OnClick(func() { s.selectSettingsEnvironment(p.ID) })
				}
			})
		})
	})
}

// settingsTargetPage draws a section that reads from an environment: the
// environment picker, and the section itself when that environment
// answers. An environment that does not is named, with a way to bring it
// back, as the old client showed.
func (s *Session) settingsTargetPage(c *ui.Context, showSelector bool, render func(target *Session)) {
	t := c.Theme()
	ui.Column(c).Gap(12).Children(func() {
		if s.settingsSaveErr != "" {
			ui.Text(c, s.settingsSaveErr).FontSize(12).TextColor(t.Danger)
		}
		if showSelector {
			s.settingsEnvironmentSelector(c)
		}
		target := s.settingsTarget()
		if target == nil {
			if !showSelector {
				ui.Column(c).Gap(6).Children(func() {
					ui.Text(c, i18n.T("No local environment was found.")).FontSize(13).FontWeight(600).TextColor(t.Text)
					ui.Text(c, i18n.T("Connect to the T3Go server running on this machine, then reopen settings.")).
						FontSize(12).TextColor(t.TextMuted)
				})
				return
			}
			ui.Text(c, i18n.T("This environment is no longer available. Select another environment.")).
				FontSize(12).TextColor(t.TextMuted)
			return
		}
		if !target.settingsConnectionReady() {
			ui.Column(c).Gap(8).Children(func() {
				ui.Text(c, target.envName()+" · "+target.connectionLabel()).
					FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				ui.Text(c, i18n.T("Connect this environment to view or change its settings.")).
					FontSize(12).TextColor(t.TextMuted)
				if target.transportBlocked {
					ui.Button(c, i18n.T("Update token")).OnClick(target.openCredentials)
					return
				}
				reconnect := ui.Button(c, i18n.T("Reconnect"))
				if target.connecting {
					reconnect.Disabled(true)
				}
				reconnect.OnClick(func() { target.connectProfile(target.profile) })
			})
			return
		}
		render(target)
	})
}

// projectEnvironmentSelector chooses the environment a new project
// belongs to. The old client named the connection state of each, so a
// machine that does not answer was visible before the folder was
// chosen.
func (s *Session) projectEnvironmentSelector(c *ui.Context) {
	if s.manager == nil {
		return
	}
	t := c.Theme()
	ui.Column(c).Gap(6).Children(func() {
		faint(c, i18n.T("Environment"), 12)
		ui.Scroll(c).MaxHeight(120).Children(func() {
			ui.Row(c).Wrap().Gap(6).Children(func() {
				for _, p := range s.manager.profiles {
					p := p
					session := s.manager.sessions[p.ID]
					if session == nil {
						continue
					}
					label := session.envName()
					if !session.connected || session.offline || session.transportBlocked {
						label += " · " + session.connectionLabel()
					}
					button := ui.Button(c, label).MaxWidthPercent(100).MinHeight(controlMinHeight(c))
					if p.ID == s.newProjectEnv {
						button.Background(t.Accent.Alpha(0.12))
					}
					button.OnClick(func() { s.selectProjectEnvironment(p.ID) })
				}
			})
		})
	})
}
