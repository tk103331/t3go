package main

import (
	"fmt"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

func (a *app) openMobileSettingsSection(section string) {
	a.setSettingsSection(section)
	a.mobileSettingsDetail.Set(section)
}

func (a *app) mobileSettings(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.mobileSettingsDetail.Get() != "" {
		return core.View{TestID: "settings-dialog", Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0),
			PaddingEnv: core.EnvEdges{Bottom: core.EnvKeyboard}}, Children: []core.Node{
			core.Scroll{Style: ui.FillRest, Children: []core.Node{
				core.View{Style: core.Style{Padding: core.All(18)}, Children: []core.Node{core.Dynamic(a.settingsPage)}},
			}},
			core.Dynamic(func(cx *core.Scope) core.Node {
				target := a.settingsTarget()
				if a.settingsSection.Get() != "general" || target == nil || !target.settingsConnectionReady() {
					return core.View{}
				}
				return core.View{Style: core.Style{Direction: core.Row, FlexShrink: 0, Gap: 8, Padding: core.Symmetric(8, 18)},
					Children: settingsFooter(cx, a, target)}
			}),
		}}
	}
	row := func(title, detail, id string, icon func() *core.SVG, onPress func()) core.Node {
		return base.Button{Label: title, TestID: id, OnPress: func(core.PressEvent) { onPress() },
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, MinHeight: core.Pt(58),
				Padding: core.Symmetric(12, 16)},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Hover()}, Focus: ui.FocusRing(cx, -2)}
			},
			Children: []core.Node{
				ui.Icon(icon, 24, colors.Text),
				core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4}, Children: []core.Node{
					ui.Text(title, 16, ui.Regular, colors.Text),
					core.Text{Content: detail, MaxLines: 2, Style: core.Style{FontSize: 12},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				}},
				ui.Icon(lucide.ChevronRight, 18, colors.Faint),
			}}
	}
	count := 1
	if a.manager != nil {
		count = len(a.manager.profiles.Get())
	}
	return core.Scroll{TestID: "mobile-settings-index", Style: ui.FillRest, Children: []core.Node{
		core.View{Style: core.Style{Padding: core.All(18), Gap: 22}, Children: []core.Node{
			core.View{Style: core.Style{CornerRadius: 24, Background: colors.Inset(), Overflow: core.Hidden}, Children: []core.Node{
				row("Environments", fmt.Sprintf("%d saved · manage connections and names", count), "settings-nav-environments", lucide.Monitor,
					func() { a.openMobileSettingsSection("environments") }),
				row("General & appearance", "Defaults, theme, accent and editor", "settings-nav-general", lucide.Settings,
					func() { a.openMobileSettingsSection("general") }),
				row("Providers", "CLI status, models and sign in by environment", "settings-nav-providers", lucide.Bot,
					func() { a.openMobileSettingsSection("providers") }),
				row("Worktrees", "Checkouts used by your threads", "settings-nav-worktrees", lucide.GitBranch,
					func() { a.openMobileSettingsSection("worktrees") }),
				row("Projects", "Folders and conversations across environments", "mobile-tab-projects", lucide.Folder,
					func() { a.mobilePage.Set("projects") }),
				row("About", "Server and protocol information", "settings-nav-about", lucide.Info,
					func() { a.openMobileSettingsSection("about") }),
			}},
		}},
	}}
}

func (a *app) mobileSessions(id string) []*app {
	if a.manager == nil {
		return []*app{a}
	}
	_ = a.manager.revision.Get()
	var sessions []*app
	for _, p := range a.manager.profiles.Get() {
		if session := a.manager.sessions[p.ID]; session != nil && (id == "" || id == p.ID) {
			sessions = append(sessions, session)
		}
	}
	return sessions
}

func (a *app) mobileEnvironmentMenu(cx *core.Scope, selected *core.Signal[string], id string, changed func(), iconOnly bool) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		label := "All environments"
		var choices []core.Node
		choose := func(env string) func() {
			return func() { selected.Set(env); changed() }
		}
		choices = append(choices, ui.MenuItem{Text: "All environments", TestID: id + "-all", OnSelect: choose("")})
		for _, session := range a.mobileSessions("") {
			env := session.profile.ID
			name := session.envName()
			if selected.Get() == env {
				label = name
			}
			choices = append(choices, ui.MenuItem{Text: name, TestID: id + "-" + env, OnSelect: choose(env)})
		}
		trigger := ui.Button{Text: label, Icon: lucide.ChevronDown, Variant: ui.ButtonGhost, Size: controlSize(cx)}
		var face core.Node = mobileMenuFace(cx, label, nil)
		if iconOnly {
			trigger = ui.Button{Icon: lucide.ListFilter, Label: "Filter environments", Side: 44, IconSize: 24, Variant: ui.ButtonGhost}
			face = nil
		}
		return ui.Menu{TestID: id, Trigger: trigger, Face: face, MaxHeight: 360, Children: choices}
	})
}
