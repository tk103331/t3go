package main

import (
	"fmt"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/proto"
)

type settingsNavItem struct {
	ID    string
	Label string
	Icon  func() *core.SVG
}

var settingsNav = []settingsNavItem{
	{ID: "general", Label: "General", Icon: lucide.Settings},
	{ID: "providers", Label: "Providers", Icon: lucide.Bot},
	{ID: "worktrees", Label: "Worktrees", Icon: lucide.GitBranch},
	{ID: "shortcuts", Label: "Shortcuts", Icon: lucide.Keyboard},
	{ID: "about", Label: "About", Icon: lucide.Info},
}

// settingsBody is the settings window's section navigation and page.
func (a *app) settingsBody(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	section := a.settingsSection.Get()
	var nav []core.Node
	for _, item := range settingsNav {
		item := item
		selected := item.ID == section
		nav = append(nav, base.Button{OnPress: func(core.PressEvent) { a.setSettingsSection(item.ID) },
			Label: item.Label, TestID: "settings-nav-" + item.ID,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pct(100),
				Padding: core.Symmetric(6, 8), CornerRadius: 7, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				style := core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
				if selected {
					style.Background = colors.Inset()
				}
				return style
			},
			Children: []core.Node{
				ui.Icon(item.Icon, 14, func() core.Color {
					if selected {
						return colors.Text()
					}
					return colors.Muted()
				}),
				core.Text{Content: item.Label, MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Medium, FlexGrow: 1},
					StyleFunc: func() core.Style {
						if selected {
							return core.Style{Color: colors.Text()}
						}
						return core.Style{Color: colors.Muted()}
					}},
			}})
	}
	var page core.Node
	switch section {
	case "providers":
		page = a.settingsProviders(cx)
	case "worktrees":
		page = a.settingsWorktrees(cx)
	case "shortcuts":
		page = a.settingsShortcuts(cx)
	case "about":
		page = a.settingsAbout(cx)
	default:
		page = a.settingsGeneral(cx)
	}
	return core.View{Style: core.Style{Direction: core.Row, Height: core.Pt(520), MinHeight: core.Pt(0)}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Width: core.Pt(150), FlexShrink: 0, Gap: 2,
			Padding: core.Edges{Top: 2, Right: 10, Bottom: 2, Left: 2}, BorderColor: colors.Border(), Borders: core.Edges{Right: 1}},
			Children: nav},
		core.Scroll{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), MinWidth: core.Pt(0),
			Padding: core.Edges{Left: 16, Right: 4}}, Children: []core.Node{page}},
	}}
}

// settingsSectionTitle is a page's heading.
func settingsSectionTitle(cx *core.Scope, title, detail string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Column, Gap: 2, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.Text{Content: title, Style: core.Style{FontSize: 14, FontWeight: ui.Semibold},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		core.Text{Content: detail, MaxLines: 2, Style: core.Style{FontSize: 11.5, LineHeight: 16},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
	}}
}

// settingsProviders shows each agent CLI's state and models.
func (a *app) settingsProviders(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	providers := a.providers.Get()
	models := a.models.Get()
	var cards []core.Node
	for _, provider := range providers {
		provider := provider
		status := "Installed"
		color := colors.Up
		switch {
		case !provider.Installed:
			status = "Not found on PATH"
			color = colors.Danger
		case provider.AuthStatus == "authenticated":
			status = "Installed · signed in"
		case provider.AuthStatus == "unauthenticated":
			status = "Installed · signed out"
			color = colors.Muted
		}
		var labels []core.Node
		for _, model := range models {
			if model.Provider != provider.ID {
				continue
			}
			label := model.Name
			if model.Default {
				label += " · default"
			}
			if model.ContextWindow > 0 {
				label += " · " + humanTokens(int64(model.ContextWindow))
			}
			labels = append(labels, core.View{Style: core.Style{Padding: core.Symmetric(2, 7), Background: colors.Inset(),
				CornerRadius: 5}, Children: []core.Node{core.Text{Content: label, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }}}})
		}
		meta := provider.Name + " · " + provider.ID
		if provider.Version != "" {
			meta += " · " + provider.Version
		}
		var body []core.Node
		body = append(body,
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
				ui.Text(provider.Name, 12.5, ui.Semibold, colors.Text),
				ui.Text(provider.ID, 10.5, ui.Regular, colors.Faint),
				ui.Spacer{},
				ui.Text(status, 10.5, ui.Semibold, color),
			}},
			core.Text{Content: meta, MaxLines: 1, Style: core.Style{FontSize: 10.5}, StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		)
		if provider.Path != "" {
			body = append(body, core.Text{Content: provider.Path, MaxLines: 1, Style: core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		}
		if provider.Error != "" {
			body = append(body, core.Text{Content: provider.Error, MaxLines: 2, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }})
		}
		if len(labels) > 0 {
			body = append(body, core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 3, Padding: core.Edges{Top: 2}}, Children: labels})
		} else if provider.Installed {
			body = append(body, ui.Text("No models reported yet.", 10.5, ui.Regular, colors.Faint))
		}
		if provider.AuthStatus == "unauthenticated" || provider.AuthStatus == "unknown" {
			if provider.LoginCommand != "" {
				body = append(body, core.Text{Content: provider.LoginCommand, MaxLines: 1,
					Style:     core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace"},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
			}
		}
		var actions []core.Node
		if !provider.Installed && provider.CanInstall && provider.InstallCommand != "" {
			body = append(body, core.Text{Content: provider.InstallCommand, MaxLines: 1,
				Style:     core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
			actions = append(actions, ui.Button{Text: "Install", Icon: lucide.Download, Variant: ui.ButtonSecondary,
				Size: ui.SizeSmall, TestID: "settings-install-" + provider.ID,
				OnPress: func() { a.runProviderCommand(provider.InstallCommand) }})
		}
		if provider.Installed && provider.CanAuthenticate && provider.AuthStatus != "authenticated" && provider.LoginCommand != "" {
			actions = append(actions, ui.Button{Text: "Sign in", Icon: lucide.LogIn, Variant: ui.ButtonSecondary,
				Size: ui.SizeSmall, TestID: "settings-login-" + provider.ID,
				OnPress: func() { a.runProviderCommand(provider.LoginCommand) }})
		}
		if len(actions) > 0 {
			body = append(body, core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4,
				Padding: core.Edges{Top: 2}}, Children: actions})
		}
		cards = append(cards, core.View{Style: core.Style{Direction: core.Column, Gap: 5, Padding: core.Symmetric(8, 10),
			Background: colors.Inset(), CornerRadius: 8}, Children: body})
	}
	refresh := ui.Button{Text: "Refresh", Icon: lucide.RefreshCw, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
		TestID: "settings-refresh-providers", OnPress: a.refreshProviders}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: []core.Node{
		settingsSectionTitle(cx, "Providers", "Agent CLIs T3Go can run. Install a CLI and keep it on PATH to use it."),
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter}, Children: []core.Node{
			ui.Text(fmt.Sprintf("%d providers · %d models", len(providers), len(models)), 11.5, ui.Regular, colors.Faint),
			ui.Spacer{}, refresh,
		}},
		core.View{Style: core.Style{Direction: core.Column, Gap: 6}, Children: cards},
	}}
}

// settingsWorktrees lists threads that run in their own checkouts.
func (a *app) settingsWorktrees(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	var rows []core.Node
	for _, thread := range a.threads.Get() {
		if thread.WorktreePath == "" {
			continue
		}
		meta := thread.WorktreePath
		if thread.Branch != "" {
			meta = thread.Branch + " · " + meta
		}
		rows = append(rows, core.View{Style: core.Style{Direction: core.Column, Gap: 2, Padding: core.Symmetric(7, 9),
			Background: colors.Inset(), CornerRadius: 7}, Children: []core.Node{
			core.Text{Content: thread.Title, MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: meta, MaxLines: 1, Style: core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}})
	}
	note := "New threads can run in the project directory or in their own worktree. T3Go keeps managed worktrees until their thread is deleted."
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No thread is using a worktree.", 11.5, ui.Regular, colors.Faint))
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: []core.Node{
		settingsSectionTitle(cx, "Worktrees", note),
		core.View{Style: core.Style{Direction: core.Column, Gap: 6}, Children: rows},
	}}
}

// settingsShortcuts lists the app's keyboard shortcuts.
func (a *app) settingsShortcuts(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	shortcuts := [][2]string{
		{"⌘K", "Search threads and actions"},
		{"⌘N", "New thread"},
		{"⌘⇧N", "New project"},
		{"⌘,", "Settings"},
		{"⌘B", "Toggle sidebar"},
		{"↑ / ↓", "Move through lists"},
		{"Return", "Open the selected item"},
		{"Esc", "Close a dialog"},
	}
	var rows []core.Node
	for _, shortcut := range shortcuts {
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
			Padding: core.Symmetric(6, 4)}, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(64), FlexShrink: 0, Padding: core.Symmetric(2, 6), Background: colors.Inset(),
				CornerRadius: 5, AlignItems: core.AlignCenter}, Children: []core.Node{
				core.Text{Content: shortcut[0], Style: core.Style{FontSize: 10.5, FontWeight: ui.Medium},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			}},
			core.Text{Content: shortcut[1], MaxLines: 1, Style: core.Style{FontSize: 12, FlexGrow: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 4}, Children: []core.Node{
		settingsSectionTitle(cx, "Shortcuts", "Keyboard shortcuts on this platform; Ctrl replaces ⌘ outside Apple platforms."),
		core.View{Style: core.Style{Direction: core.Column, Gap: 1}, Children: rows},
	}}
}

// settingsAbout describes the environment this window is connected to.
func (a *app) settingsAbout(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	rows := []core.Node{
		a.aboutRow(cx, "Server", a.serverURL.Get()),
		a.aboutRow(cx, "Protocol", fmt.Sprintf("v%d", proto.ProtocolVersion)),
		a.aboutRow(cx, "Environment", a.settings.Get().Title),
	}
	if a.offline.Get() {
		rows = append(rows, ui.Text("The event stream is reconnecting.", 11.5, ui.Regular, colors.Danger))
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: []core.Node{
		settingsSectionTitle(cx, "About T3Go", "A Go environment and native client for T3 Code's protocol."),
		core.View{Style: core.Style{Direction: core.Column, Gap: 4}, Children: rows},
		core.Text{Content: "Agent CLIs run locally on the environment machine. The client never sees a provider's tokens or process output beyond the thread.",
			MaxLines: 4, Style: core.Style{FontSize: 11, LineHeight: 16}, StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
	}}
}

func (a *app) aboutRow(cx *core.Scope, label, value string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Symmetric(4, 0)}, Children: []core.Node{
		core.Text{Content: label, Style: core.Style{FontSize: 12, Width: core.Pt(90), FlexShrink: 0},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		core.Text{Content: value, MaxLines: 2, Selectable: core.SelectableOn, Style: core.Style{FontSize: 12, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
	}}
}
