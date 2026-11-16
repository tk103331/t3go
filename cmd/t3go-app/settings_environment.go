package main

import (
	"context"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

func (a *app) settingsTarget() *app {
	if a.manager == nil {
		return a
	}
	_ = a.manager.profiles.Get()
	_ = a.manager.revision.Get()
	if settingsUsesLocalTarget(a.settingsSection.Get()) {
		return a.localSettingsTarget()
	}
	return a.manager.sessions[a.settingsEnv.Get()]
}

func settingsUsesLocalTarget(section string) bool {
	return section == "access" || section == "about"
}

func (a *app) prepareLocalSettingsTarget(section string) {
	if !settingsUsesLocalTarget(section) {
		return
	}
	target := a.localSettingsTarget()
	if target == nil {
		return
	}
	if a.settingsVisited == nil {
		a.settingsVisited = map[*app]bool{}
	}
	if !a.settingsVisited[target] {
		target.beginSettingsDraft()
		a.settingsVisited[target] = true
	}
	a.settingsEnv.Set(target.profile.ID)
}

func (a *app) settingsConnectionReady() bool {
	return a.connected.Get() && !a.offline.Get() && !a.transportBlocked.Get() && a.client() != nil
}

func (a *app) beginSettingsDraft() {
	s := a.settings.GetUntracked()
	a.settingsDraft.Set(s)
	a.loadProviderTextDraft(s)
	a.settingsTitle.Set(s.Title)
}

func (a *app) selectSettingsEnvironment(id string) {
	if a.manager == nil || id == a.settingsEnv.GetUntracked() || a.settingsSaveBusy.GetUntracked() {
		return
	}
	target := a.manager.sessions[id]
	if target == nil {
		return
	}
	if a.settingsVisited == nil {
		a.settingsVisited = map[*app]bool{}
	}
	if !a.settingsVisited[target] {
		target.beginSettingsDraft()
		a.settingsVisited[target] = true
	}
	a.settingsEnv.Set(id)
	a.settingsSaveErr.Set("")
	a.loadSettingsSection(a.settingsSection.GetUntracked())
}

func (a *app) loadSettingsSection(section string) {
	target := a.settingsTarget()
	if target == nil {
		return
	}
	switch section {
	case "providers":
		target.refreshProviders()
	case "access":
		target.refreshAccess()
	case "usage":
		target.loadUsage(target.usageDays.GetUntracked())
	}
}

func (a *app) settingsEnvironmentSelector(cx *core.Scope) core.Node {
	if a.manager == nil {
		return a.environmentContext(cx, "settings-environment")
	}
	selected := a.settingsEnv.Get()
	_ = a.manager.revision.Get()
	names := a.manager.environmentNames()
	var choices []core.Node
	for _, p := range a.manager.profiles.Get() {
		p := p
		if a.manager.sessions[p.ID] == nil {
			continue
		}
		variant := ui.ButtonSecondary
		if p.ID == selected {
			variant = ui.ButtonPrimary
		}
		var choice core.Node = ui.Button{Text: names[p.ID], Variant: variant, Size: controlSize(cx),
			TestID: "settings-environment-" + p.ID, Disabled: a.settingsSaveBusy.Get,
			OnPress: func() { a.selectSettingsEnvironment(p.ID) }}
		if compactSettings(cx) && ui.Touch(cx) {
			colors := ui.ColorsOf(cx)
			choice = base.Button{Label: names[p.ID], TestID: "settings-environment-" + p.ID,
				Disabled: a.settingsSaveBusy.Get, Selected: func() bool { return p.ID == selected },
				OnPress: func(core.PressEvent) { a.selectSettingsEnvironment(p.ID) },
				Style: core.Style{MinHeight: core.Pt(40), MaxWidth: core.Pct(100), MinWidth: core.Pt(0),
					Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(8, 12), CornerRadius: 8},
				StyleFunc: func() core.Style {
					fill := colors.Inset()
					if p.ID == selected {
						fill = colors.AccentFill()
					}
					return core.Style{Background: fill, Focus: ui.FocusRing(cx, -2)}
				},
				Children: []core.Node{core.Text{Content: names[p.ID], MaxLines: 1,
					Style: core.Style{FontSize: 14, FlexShrink: 1, MinWidth: core.Pt(0)},
					StyleFunc: func() core.Style {
						color := colors.Text()
						if p.ID == selected {
							color = colors.OnAccent()
						}
						return core.Style{Color: color}
					}}}}
		}
		choices = append(choices, choice)
	}
	return core.View{TestID: "settings-environment", Style: core.Style{Gap: 6}, Children: []core.Node{
		ui.Text("Environment", 12, ui.Semibold, ui.ColorsOf(cx).Muted),
		core.Scroll{Style: core.Style{MaxHeight: core.Pt(100)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: choices},
		}},
	}}
}

// The settings window owns navigation, while every environment owns its data,
// drafts and requests. Choosing a settings target leaves the conversation open.
func (a *app) settingsPage(cx *core.Scope) core.Node {
	switch a.settingsSection.Get() {
	case "general":
		return a.settingsAppearance(cx)
	case "environments":
		if a.manager != nil {
			return a.environmentManagement(cx)
		}
		return ui.Text("Connect an environment to manage its routes.", 12, ui.Regular, ui.ColorsOf(cx).Muted)
	case "shortcuts":
		return a.settingsShortcuts(cx)
	case "access":
		return a.settingsTargetPage(cx, false, func(target *app) core.Node {
			return target.settingsAccess(cx)
		})
	case "about":
		return a.settingsAbout(cx)
	}
	return a.settingsTargetPage(cx, true, func(target *app) core.Node {
		switch a.settingsSection.Get() {
		case "providers":
			return target.settingsProviders(cx, a)
		case "usage":
			return target.settingsUsage(cx)
		case "worktrees":
			return target.settingsWorktrees(cx)
		default:
			return target.settingsEnvironmentDefaults(cx)
		}
	})
}

func (a *app) settingsTargetPage(cx *core.Scope, showSelector bool, render func(*app) core.Node) core.Node {
	return core.View{Style: core.Style{Gap: 12}, Children: []core.Node{
		core.Show{When: func() bool { return a.settingsSaveErr.Get() != "" }, Then: func(cx *core.Scope) core.Node {
			return ui.Text(a.settingsSaveErr.Get(), 12, ui.Regular, ui.ColorsOf(cx).Danger)
		}},
		core.Show{When: func() bool { return showSelector }, Then: func(cx *core.Scope) core.Node {
			return core.Dynamic(a.settingsEnvironmentSelector)
		}},
		core.Dynamic(func(cx *core.Scope) core.Node {
			target := a.settingsTarget()
			if target == nil {
				if !showSelector {
					return core.View{TestID: "settings-local-unavailable", Style: core.Style{Gap: 6}, Children: []core.Node{
						ui.Text("No local environment was found.", 13, ui.Semibold, ui.ColorsOf(cx).Text),
						ui.Text("Connect to the T3Go server running on this machine, then reopen settings.", 12, ui.Regular, ui.ColorsOf(cx).Muted),
					}}
				}
				return ui.Text("This environment is no longer available. Select another environment.", 12, ui.Regular, ui.ColorsOf(cx).Muted)
			}
			if !target.settingsConnectionReady() {
				action := ui.Button{Text: "Reconnect", Size: controlSize(cx), TestID: "settings-target-reconnect",
					Disabled: target.connecting.Get, OnPress: func() { target.connectProfile(target.profile) }}
				if target.transportBlocked.Get() {
					action.Text, action.TestID, action.OnPress = "Update token", "settings-target-token", target.openCredentials
				}
				return core.View{TestID: "settings-environment-unavailable", Style: core.Style{Gap: 8},
					Children: []core.Node{
						ui.Text(target.envName()+" · "+target.connectionLabel(), 12, ui.Semibold, ui.ColorsOf(cx).Muted),
						ui.Text("Connect this environment to view or change its settings.", 12, ui.Regular, ui.ColorsOf(cx).Muted),
						action,
					}}
			}
			return render(target)
		}),
	}}
}

// Keep errors with the window that initiated the request, even when its target
// is another environment. Only apply defaults after the server accepted them.
func (a *app) saveTargetSettings(target *app) {
	if a.settingsSaveBusy.GetUntracked() || !target.settingsConnectionReady() {
		return
	}
	target.syncProviderTextDraft()
	s := target.settingsDraft.GetUntracked()
	s.Title = target.settingsTitle.GetUntracked()
	a.settingsSaveBusy.Set(true)
	a.settingsSaveErr.Set("")
	var saved proto.Settings
	target.call(func(ctx context.Context, c *client.Client) error {
		var err error
		saved, err = c.SaveSettings(ctx, s)
		return err
	}, func(err error) {
		a.settingsSaveBusy.Set(false)
		if err != nil {
			a.settingsSaveErr.Set(target.envName() + ": " + friendlyError(err))
			return
		}
		target.settings.Set(saved)
		a.closeSettings()
	})
}

func (a *app) openEnvironmentSettings(id string) {
	if target := a.manager.sessions[id]; target != nil {
		if a.settingsVisited == nil {
			a.settingsVisited = map[*app]bool{}
		}
		if !a.settingsVisited[target] {
			target.beginSettingsDraft()
			a.settingsVisited[target] = true
		}
		a.selectSettingsEnvironment(id)
		a.setSettingsSection("environment-defaults")
		if compactSettings(a.cx) {
			a.mobileSettingsDetail.Set("environment-defaults")
		}
	}
}

func (a *app) runSettingsProviderCommand(target *app, command string) {
	if !target.settingsConnectionReady() {
		target.providersErr.Set("Connect this environment before running its setup command.")
		return
	}
	if target.selected.GetUntracked() == "" {
		a.cx.SetClipboard(command)
		target.providersErr.Set("Command copied. Open a thread in " + target.envName() + " to run it in that environment's terminal.")
		return
	}
	a.closeSettings()
	target.inEnvironment(func() { target.runProviderCommand(command) })()
}
