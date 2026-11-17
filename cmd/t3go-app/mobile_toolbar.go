package main

import (
	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/i18n"
)

func mobileToolbarAction(cx *core.Scope, icon func() *core.SVG, title, id string, action func()) core.Node {
	colors := ui.ColorsOf(cx)
	return base.Button{Label: title, TestID: id, OnPress: func(core.PressEvent) { action() },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, MinHeight: core.Pt(52),
			Padding: core.Symmetric(8, 8), CornerRadius: 10},
		StyleFunc: func() core.Style {
			return core.Style{Hover: &core.Style{Background: colors.Hover()}, Focus: ui.FocusRing(cx, -2)}
		}, Children: []core.Node{
			ui.Icon(icon, 24, colors.Text),
			core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 16, FlexGrow: 1, FlexShrink: 1,
				MinWidth: core.Pt(0)}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}}
}

func (a *app) mobileGitMenu(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	open := core.NewSignal(cx, false)
	choose := func(action func()) func() { return func() { open.Set(false); action() } }
	return ui.Popover{TestID: "branch-menu", Label: i18n.T("Git controls"), Open: open.Get,
		OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) },
		Trigger:      ui.Button{Icon: lucide.GitBranch, Label: i18n.T("Git controls"), Side: 44, IconSize: 24, Variant: ui.ButtonGhost},
		Style:        core.Style{Width: core.Pt(290), MaxWidth: core.Pct(94), Background: colors.Surface()}, Children: []core.Node{
			ui.Text(i18n.T("Git controls"), 12, ui.Medium, ui.ColorsOf(cx).Muted),
			core.Dynamic(func(cx *core.Scope) core.Node {
				status := a.detail.Get().Git
				if status == nil || !status.Repo {
					return ui.Text(i18n.T("This thread is not in a Git repository."), 14, ui.Regular, ui.ColorsOf(cx).Muted)
				}
				branch := status.Branch
				if branch == "" {
					branch = i18n.T("Detached HEAD")
				}
				return core.Group{
					mobileToolbarAction(cx, lucide.GitBranch, branch, "mobile-git-branches", choose(func() {
						a.loadBranches(a.selected.GetUntracked())
						a.showBranches.Set(true)
					})),
					mobileToolbarAction(cx, lucide.ArrowUp, i18n.T("Commit & push"), "mobile-git-commit-push",
						choose(func() { a.openMobileCommit(false) })),
					mobileToolbarAction(cx, lucide.MessageSquare, i18n.T("Review changes"), "mobile-git-review",
						choose(func() { a.openMobileSurface("diff") })),
					mobileToolbarAction(cx, lucide.Ellipsis, i18n.T("More"), "mobile-git-more",
						choose(func() { a.openMobileSurface("diff") })),
				}
			}),
		}}
}

func (a *app) mobileTerminalMenu(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	open := core.NewSignal(cx, false)
	return ui.Popover{TestID: "mobile-open-terminal", Label: i18n.T("Terminal"), Open: open.Get,
		OnOpenChange: func(value bool, _ base.Reason) {
			open.Set(value)
			if value {
				a.loadTerminalSessions()
			}
		},
		Trigger: ui.Button{Icon: lucide.Terminal, Label: i18n.T("Terminal"), Side: 44, IconSize: 24, Variant: ui.ButtonGhost},
		Style:   core.Style{Width: core.Pt(290), MaxWidth: core.Pct(94), Background: colors.Surface()}, Children: []core.Node{
			ui.Text(i18n.T("Terminal"), 12, ui.Medium, ui.ColorsOf(cx).Muted),
			core.Dynamic(func(cx *core.Scope) core.Node {
				var rows []core.Node
				if err := a.terminalErr.Get(); err != "" {
					rows = append(rows, ui.Text(err, 12, ui.Regular, ui.ColorsOf(cx).Danger))
				}
				for i, session := range a.terminals.Get() {
					session := session
					title := session.Title
					if title == "" {
						title = i18n.T("Terminal %d", i+1)
					}
					rows = append(rows, mobileToolbarAction(cx, lucide.Terminal, title,
						"mobile-terminal-"+session.ID, func() {
							open.Set(false)
							a.terminalSession.Set(session)
							a.surface.Set("terminal")
							a.mobileSurface.Set("terminal")
						}))
				}
				if len(rows) == 0 {
					rows = append(rows, ui.Text(i18n.T("No terminal open"), 14, ui.Regular, ui.ColorsOf(cx).Muted))
				}
				rows = append(rows, mobileToolbarAction(cx, lucide.Plus, i18n.T("Open new terminal"), "mobile-terminal-new", func() {
					open.Set(false)
					a.surface.Set("terminal")
					a.mobileSurface.Set("terminal")
					a.newTerminal()
				}))
				return core.Scroll{Style: core.Style{MaxHeight: core.Pt(360)}, Children: rows}
			}),
		}}
}
