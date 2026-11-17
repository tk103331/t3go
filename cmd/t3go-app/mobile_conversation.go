package main

import (
	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/i18n"
)

func (a *app) mobileConversationHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	thread := a.detail.Get().Thread
	project := ""
	for _, p := range a.projects.Get() {
		if p.ID == thread.ProjectID {
			project = p.Name
		}
	}
	title := thread.Title
	if title == "" {
		title = "Thread"
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexShrink: 0,
		Padding: core.Symmetric(8, 8), Gap: 2}, Children: []core.Node{
		ui.IconButton{Icon: lucide.ArrowLeft, Label: i18n.T("Back to threads"), Size: 44, IconSize: 24, OnPress: func() {
			a.persistView()
			a.selected.Set("")
		}},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			ui.Menu{TestID: "thread-actions", Open: a.threadMenuOpen.Get,
				OnOpenChange: func(open bool, _ base.Reason) {
					a.threadMenuGuard.change(open, a.threadMenuOpen.Set)
				},
				Trigger: ui.Button{Text: title, Label: i18n.T("Thread actions"), Variant: ui.ButtonGhost, Flex: 1},
				Face: core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4}, Children: []core.Node{
					core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 18, FontWeight: ui.Medium},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, MinWidth: core.Pt(0)}, Children: []core.Node{
						core.Text{Content: project + " ·", MaxLines: 1,
							Style:     core.Style{FontSize: 11.5, FlexShrink: 1, MinWidth: core.Pt(0)},
							StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
						core.Text{Content: a.envName(), MaxLines: 1, TestID: "current-environment-name",
							Style:     core.Style{FontSize: 11.5, FlexShrink: 1, MinWidth: core.Pt(0)},
							StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
						a.mobileConnectionIcon(cx),
					}},
				}},
				Children: []core.Node{
					a.settleMenuItem(thread, thread.ID),
					ui.MenuItem{Text: i18n.T("Rename thread…"), TestID: "rename-thread", OnSelect: func() { a.askRenameThread(thread) }},
					ui.MenuItem{Text: i18n.T("Run history"), TestID: "history", OnSelect: a.openRuns},
					ui.MenuItem{Text: i18n.T("Changes in this thread"), TestID: "open-diff", OnSelect: a.openDiff},
					ui.MenuItem{Text: i18n.T("Open in editor"), TestID: "open-editor", OnSelect: a.openInEditor},
					ui.MenuItem{Text: i18n.T("Settings"), TestID: "settings", OnSelect: a.openSettings},
					ui.MenuItem{Text: i18n.T("Delete thread"), TestID: "delete-thread", Variant: ui.MenuItemDanger,
						OnSelect: func() { a.askDeleteThread(thread) }},
				}},
		}},
		ui.IconButton{Icon: lucide.Folder, Label: i18n.T("Files"), TestID: "mobile-open-files", Size: 44, IconSize: 24,
			OnPress: func() { a.openMobileSurface("files") }},
		core.Func(a.mobileGitMenu),
		core.Func(a.mobileTerminalMenu),
	}}
}

func (a *app) mobileConnectionIcon(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	state, status := a.connectionStatus()
	icon, color := lucide.Cable, colors.Muted
	switch state {
	case "relay":
		icon = lucide.Cloud
	case "blocked":
		icon, color = lucide.LockKeyhole, colors.Danger
	case "offline":
		icon, color = lucide.CloudOff, colors.Danger
	}
	var glyph core.Node = ui.Icon(icon, 12, color)
	if state == "connecting" || state == "reconnecting" {
		glyph = ui.Spinner{Size: 12, Label: status}
	}
	return core.View{TestID: "connection-status", Label: status,
		Style: core.Style{FlexShrink: 0, Width: core.Pt(16), Height: core.Pt(14), AlignItems: core.AlignCenter,
			JustifyContent: core.JustifyCenter}, Children: []core.Node{glyph}}
}

func (a *app) openMobileSurface(name string) {
	a.openSurface(name)
	a.mobileSurface.Set(name)
}

func (a *app) mobileSurfaceView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "mobile-surface", Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0),
		Height: core.Pct(100), Overflow: core.Hidden, PaddingEnv: core.EnvEdges{Bottom: core.EnvKeyboard}}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(8, 12), Gap: 8}, Children: []core.Node{
			ui.IconButton{Icon: lucide.ArrowLeft, Label: i18n.T("Back to conversation"), TestID: "mobile-surface-back", Size: 44,
				OnPress: a.closeMobileSurface},
			core.Dynamic(func(cx *core.Scope) core.Node {
				return ui.Text(surfaceTitle(a.surface.Get()), 20, ui.Medium, colors.Text)
			}),
		}},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Overflow: core.Hidden, CornerRadius: 24,
			Background: colors.Surface()}, Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if a.mobileSurface.Get() == "diff" && compactSettings(cx) {
				return a.mobileChangesView(cx)
			}
			return a.rightBody(cx)
		})}},
	}}
}
