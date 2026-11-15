package main

import (
	"fmt"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

func mobileToolbarAction(cx *core.Scope, icon func() *core.SVG, title, subtitle, id string, action func()) core.Node {
	colors := ui.ColorsOf(cx)
	return base.Button{Label: title, TestID: id, OnPress: func(core.PressEvent) { action() },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, MinHeight: core.Pt(60),
			Padding: core.All(8), CornerRadius: 10},
		StyleFunc: func() core.Style {
			return core.Style{Hover: &core.Style{Background: colors.Hover()}, Focus: ui.FocusRing(cx, -2)}
		}, Children: []core.Node{
			ui.Icon(icon, 24, colors.Text),
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4}, Children: []core.Node{
				core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 16},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: subtitle, MaxLines: 2, Style: core.Style{FontSize: 12},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}},
		}}
}

func (a *app) mobileGitMenu(cx *core.Scope) core.Node {
	open := core.NewSignal(cx, false)
	choose := func(action func()) func() { return func() { open.Set(false); action() } }
	return ui.Popover{TestID: "branch-menu", Label: "Git controls", Open: open.Get,
		OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) },
		Trigger:      ui.Button{Icon: lucide.GitBranch, Label: "Git controls", Side: 44, IconSize: 24, Variant: ui.ButtonGhost},
		Style:        core.Style{Width: core.Pt(290), MaxWidth: core.Pct(94)}, Children: []core.Node{
			ui.Text("Git controls", 12, ui.Medium, ui.ColorsOf(cx).Muted),
			core.Dynamic(func(cx *core.Scope) core.Node {
				status := a.detail.Get().Git
				if status == nil || !status.Repo {
					return ui.Text("This thread is not in a Git repository.", 14, ui.Regular, ui.ColorsOf(cx).Muted)
				}
				branch, description := status.Branch, "Working tree clean"
				if branch == "" {
					branch = "Detached HEAD"
				}
				if status.Dirty {
					description = "Uncommitted changes"
				}
				if status.DiffStat != "" {
					description = oneLine(status.DiffStat, 80)
				}
				return core.Group{
					mobileToolbarAction(cx, lucide.GitBranch, branch, description, "mobile-git-branches", choose(func() {
						a.loadBranches(a.selected.GetUntracked())
						a.showBranches.Set(true)
					})),
					mobileToolbarAction(cx, lucide.ArrowUp, "Commit & push", "Review changes and enter a commit message",
						"mobile-git-commit-push", choose(a.openDiff)),
					mobileToolbarAction(cx, lucide.MessageSquare, "Review changes", "Turn diffs and worktree changes",
						"mobile-git-review", choose(a.openDiff)),
					mobileToolbarAction(cx, lucide.Ellipsis, "More", "Files, staging and diffs",
						"mobile-git-more", choose(func() { a.openMobileSurface("diff") })),
				}
			}),
		}}
}

func (a *app) mobileTerminalMenu(cx *core.Scope) core.Node {
	open := core.NewSignal(cx, false)
	return ui.Popover{TestID: "mobile-open-terminal", Label: "Terminal", Open: open.Get,
		OnOpenChange: func(value bool, _ base.Reason) {
			open.Set(value)
			if value {
				a.loadTerminalSessions()
			}
		},
		Trigger: ui.Button{Icon: lucide.Terminal, Label: "Terminal", Side: 44, IconSize: 24, Variant: ui.ButtonGhost},
		Style:   core.Style{Width: core.Pt(290), MaxWidth: core.Pct(94)}, Children: []core.Node{
			ui.Text("Terminal", 12, ui.Medium, ui.ColorsOf(cx).Muted),
			core.Dynamic(func(cx *core.Scope) core.Node {
				var rows []core.Node
				if err := a.terminalErr.Get(); err != "" {
					rows = append(rows, ui.Text(err, 12, ui.Regular, ui.ColorsOf(cx).Danger))
				}
				for i, session := range a.terminals.Get() {
					session := session
					title, status := session.Title, "Ready"
					if title == "" {
						title = fmt.Sprintf("Terminal %d", i+1)
					}
					if session.Exited {
						status = "Exited"
					}
					rows = append(rows, mobileToolbarAction(cx, lucide.Terminal, title, status+" · "+folderName(session.Cwd),
						"mobile-terminal-"+session.ID, func() {
							open.Set(false)
							a.terminalSession.Set(session)
							a.surface.Set("terminal")
							a.mobileSurface.Set("terminal")
						}))
				}
				if len(rows) == 0 {
					rows = append(rows, ui.Text("No terminal open", 14, ui.Regular, ui.ColorsOf(cx).Muted))
				}
				rows = append(rows, mobileToolbarAction(cx, lucide.Plus, "Open new terminal",
					"Start another shell for this thread", "mobile-terminal-new", func() {
						open.Set(false)
						a.surface.Set("terminal")
						a.mobileSurface.Set("terminal")
						a.newTerminal()
					}))
				return core.Scroll{Style: core.Style{MaxHeight: core.Pt(360)}, Children: rows}
			}),
		}}
}
