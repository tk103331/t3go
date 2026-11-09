package main

import (
	"github.com/oneo-me/nsgo"
	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/proto"
)

func main() {
	nsgo.Run(Root, nsgo.WindowSize(1100, 760))
}

// Root builds the app: a connection screen until an environment answers,
// then the projects and threads in it.
func Root(cx *core.Scope) core.Node {
	a := newApp(cx)
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if !a.connected.Get() {
			return a.connectView(cx)
		}
		return a.mainView(cx)
	})
}

func (a *app) connectView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		Padding: core.All(24), Background: colors.Background()}, Children: []core.Node{
		core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(400), Gap: 10}, Children: []core.Node{
			core.Text{Content: "T3Go", Style: core.Style{FontSize: 24, FontWeight: ui.Bold}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: "Connect to a T3Go environment to open its projects and threads.", Style: core.Style{FontSize: 13, LineHeight: 19},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.View{Style: core.Style{Height: core.Pt(6)}},
			ui.Input{Input: base.Input{Value: a.serverURL.Get, OnChange: a.serverURL.Set, Placeholder: "Server, e.g. http://127.0.0.1:4870",
				Label: "Server address", SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.connect(a.serverURL.Get(), a.token.Get()) }}},
			ui.Input{Input: base.Input{Value: a.token.Get, OnChange: a.token.Set, Placeholder: "Token, or paste a t3go:// pair link",
				Label: "Token", Masked: true, SubmitBehavior: core.SubmitBehaviorSubmit,
				OnSubmit: func(string) { a.connect(a.serverURL.Get(), a.token.Get()) }}},
			core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
			}},
			ui.Button{Text: func() string {
				if a.connecting.Get() {
					return "Connecting…"
				}
				return "Connect"
			}(), Variant: ui.ButtonPrimary, OnPress: func() { a.connect(a.serverURL.Get(), a.token.Get()) },
				Disabled: a.connecting.Get, Flex: 1},
			ui.Text("The token is in the environment's data directory, or run `t3go pair`.", 11.5, ui.Regular, colors.Faint),
		}},
	}}
}

func (a *app) mainView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	wide := cx.Env().Window().Width >= 760
	var layout core.Node
	if wide {
		layout = ui.SidebarShell{
			Sidebar:       []core.Node{a.sidebar(cx)},
			SidebarFooter: []core.Node{a.sidebarFooter(cx)},
			Header:        []core.Node{a.contentHeader(cx)},
			Content:       []core.Node{a.content(cx)},
			Open:          a.sidebarOpen.Get, OnOpenChange: a.sidebarOpen.Set,
			ToggleLabel: "Toggle sidebar",
			Width:       a.sidebarWidth.Get, OnWidthChange: a.sidebarWidth.Set,
			DefaultWidth: 260, MinWidth: 220, MaxWidth: 380,
			ResizeLabel: "Resize sidebar",
		}
	} else {
		layout = a.narrowView(cx)
	}
	return core.View{Style: core.Style{FlexGrow: 1, Direction: core.Column, Background: colors.Background()}, Children: []core.Node{
		core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node { return a.errorBanner(cx) }},
		core.View{Style: core.Style{FlexGrow: 1}, Children: []core.Node{layout, a.dialogs(cx)}},
	}}
}

// errorBanner shows an action's failure above the content, with a way to
// dismiss it.
func (a *app) errorBanner(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
		Padding: core.Symmetric(6, 12), Background: colors.Inset()}, Children: []core.Node{
		core.Text{Content: a.connErr.Get(), MaxLines: 3, Style: core.Style{FontSize: 12, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }},
		ui.Button{Text: "Dismiss", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: func() { a.connErr.Set("") }},
	}}
}

func (a *app) sidebar(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	projects := a.projects.Get()
	allThreads := a.threads.Get()
	var sections []ui.SidebarSection
	for _, p := range projects {
		var items []ui.SidebarItem
		for _, t := range allThreads {
			if t.ProjectID != p.ID {
				continue
			}
			t := t
			title := t.Title
			if t.Status == proto.ThreadRunning {
				title = "● " + title
			} else if t.Status == proto.ThreadFailed {
				title = "! " + title
			}
			items = append(items, ui.SidebarItem{ID: t.ID, Text: title, TestID: "thread-" + t.ID,
				Menu: []core.Node{
					ui.MenuItem{Text: "Rename thread…", TestID: "rename-thread-" + t.ID, OnSelect: func() { a.askRenameThread(t) }},
					ui.MenuItem{Text: "Delete thread", Variant: ui.MenuItemDanger,
						TestID: "delete-thread-" + t.ID, OnSelect: func() { a.askDeleteThread(t) }},
				}})
		}
		section := ui.SidebarSection{Title: p.Name, Items: items}
		section.Actions = ui.Menu{
			TestID:  "project-menu-" + p.ID,
			Trigger: ui.Button{Text: "⋯", Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Project actions", TestID: "project-actions-" + p.ID},
			Children: []core.Node{
				ui.MenuItem{Text: "Rename project…", TestID: "rename-project-" + p.ID, OnSelect: func() { a.askRenameProject(p) }},
				ui.MenuItem{Text: "Delete project", Variant: ui.MenuItemDanger,
					TestID: "delete-project-" + p.ID, OnSelect: func() { a.askDeleteProject(p) }},
			},
		}
		if len(items) == 0 {
			section.Note = ui.Text("No threads yet", 12, ui.Regular, colors.Faint)
		}
		sections = append(sections, section)
	}
	if len(sections) == 0 {
		sections = append(sections, ui.SidebarSection{Title: "Projects", Note: ui.Text("Add a project to start.", 12, ui.Regular, colors.Faint)})
	}
	return core.View{Style: ui.FillRest, Children: []core.Node{
		ui.Sidebar{Sections: sections, Selected: a.selected.Get, OnSelect: a.selectThread},
	}}
}

func (a *app) sidebarFooter(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, Width: core.Pct(100), Padding: core.Symmetric(6, 4), Gap: 4, AlignItems: core.AlignCenter}, Children: []core.Node{
		ui.Button{Text: "+ Project", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: a.openNewProject},
		ui.Button{Text: "+ Thread", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: a.openNewThread},
		ui.Spacer{},
		ui.Button{Text: "Settings", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: func() { a.showSettings.Set(true) }},
		core.Text{Content: "·", Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
	}}
}

func (a *app) contentHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	d := a.detail.Get()
	if d.Thread.ID == "" {
		return core.View{Style: core.Style{Height: core.Pt(32), AlignItems: core.AlignCenter}, Children: []core.Node{
			ui.Text("T3Go", 13, ui.Semibold, colors.Muted),
		}}
	}
	meta := d.Thread.Provider
	if d.Thread.Model != "" {
		meta += " · " + d.Thread.Model
	}
	meta += " · " + string(d.Thread.Permission)
	if g := d.Git; g != nil && g.Branch != "" {
		meta += " · " + g.Branch
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinWidth: core.Pt(0)}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, MinWidth: core.Pt(0), FlexGrow: 1, Gap: 1}, Children: []core.Node{
			core.Text{Content: d.Thread.Title, MaxLines: 1, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: meta, MaxLines: 1, Style: core.Style{FontSize: 11},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}},
		ui.Button{Text: "Rename", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "rename-thread",
			OnPress: func() { a.askRenameThread(d.Thread) }},
		ui.Button{Text: "Delete", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "delete-thread",
			OnPress: func() { a.askDeleteThread(d.Thread) }},
		ui.Button{Text: "Diff", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "open-diff", OnPress: a.openDiff},
	}}
}

func (a *app) content(cx *core.Scope) core.Node {
	if a.selected.Get() == "" {
		return a.welcome(cx)
	}
	return a.threadPane(cx)
}

func (a *app) welcome(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Padding: core.All(24), Gap: 12}, Children: []core.Node{
		ui.Text("T3Go", 20, ui.Bold, colors.Text),
		ui.Text("Pick a thread, or start one in a project.", 13, ui.Regular, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Gap: 8, JustifyContent: core.JustifyCenter}, Children: []core.Node{
			ui.Button{Text: "New project", Variant: ui.ButtonSecondary, OnPress: a.openNewProject},
			ui.Button{Text: "New thread", Variant: ui.ButtonPrimary, OnPress: a.openNewThread},
		}},
	}}
}

// narrowView is the phone layout: a list of threads, or the open thread
// with a way back to the list.
func (a *app) narrowView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() != "" {
		return core.View{Style: core.Style{FlexGrow: 1}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(8, 8), Gap: 8}, Children: []core.Node{
				ui.Button{Text: "Back", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: func() { a.selected.Set("") }},
				core.View{Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{a.contentHeader(cx)}},
			}},
			core.View{Style: ui.FillRest, Children: []core.Node{a.threadPane(cx)}},
		}}
	}
	return core.View{Style: core.Style{FlexGrow: 1, Direction: core.Column}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(8, 12), Gap: 8}, Children: []core.Node{
			ui.Text("T3Go", 16, ui.Bold, colors.Text), ui.Spacer{},
			ui.Button{Text: "+ Project", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: a.openNewProject},
			ui.Button{Text: "Settings", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: func() { a.showSettings.Set(true) }},
		}},
		core.View{Style: ui.FillRest, Children: []core.Node{a.sidebar(cx)}},
	}}
}

func (a *app) dialogs(cx *core.Scope) core.Node {
	return core.Group{a.newProjectDialog(cx), a.newThreadDialog(cx), a.settingsDialog(cx), a.diffDialog(cx), a.renameDialog(cx), a.deleteDialog(cx)}
}

func closeDialog(sig *core.Signal[bool]) func(bool, base.Reason) {
	return func(open bool, _ base.Reason) { sig.Set(open) }
}

func (a *app) newProjectDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return ui.Dialog{Open: a.showNewProject.Get, OnOpenChange: closeDialog(a.showNewProject), Title: "New project",
		Description: "Register a directory the agent can work in.", Children: []core.Node{
			ui.Input{Input: base.Input{Value: a.newProjectName.Get, OnChange: a.newProjectName.Set, Placeholder: "Name (optional)", Label: "Project name"}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
				core.View{Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
					ui.Input{Input: base.Input{Value: a.newProjectPath.Get, OnChange: a.newProjectPath.Set, Placeholder: "/path/to/project", Label: "Project path"}},
				}},
				ui.Button{Text: "Browse…", Variant: ui.ButtonSecondary, OnPress: a.browseProject},
			}},
			core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showNewProject.Set(false) }},
			ui.Button{Text: "Add project", Variant: ui.ButtonPrimary, OnPress: a.createProject},
		}}
}

func (a *app) newThreadDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	provider := a.newThreadProv.Get()
	var providerButtons []core.Node
	for _, p := range a.providers.Get() {
		p := p
		variant := ui.ButtonSecondary
		if p.ID == provider {
			variant = ui.ButtonPrimary
		}
		label := p.Name
		if !p.Installed {
			label += " (missing)"
		}
		providerButtons = append(providerButtons, ui.Button{Text: label, Variant: variant, Size: ui.SizeSmall,
			Disabled: func() bool { return !p.Installed }, OnPress: func() {
				a.newThreadProv.Set(p.ID)
				for _, m := range a.models.Get() {
					if m.Provider == p.ID {
						a.newThreadModel.Set(m.ID)
						break
					}
				}
			}})
	}
	var modelButtons []core.Node
	for _, m := range a.models.Get() {
		if m.Provider != provider {
			continue
		}
		m := m
		variant := ui.ButtonSecondary
		if m.ID == a.newThreadModel.Get() {
			variant = ui.ButtonPrimary
		}
		modelButtons = append(modelButtons, ui.Button{Text: m.Name, Variant: variant, Size: ui.SizeSmall, OnPress: func() { a.newThreadModel.Set(m.ID) }})
	}
	permissions := []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits, proto.PermissionAuto, proto.PermissionFullAccess}
	var permButtons []core.Node
	for _, p := range permissions {
		p := p
		variant := ui.ButtonSecondary
		if p == a.newThreadPerm.Get() {
			variant = ui.ButtonPrimary
		}
		permButtons = append(permButtons, ui.Button{Text: string(p), Variant: variant, Size: ui.SizeSmall, OnPress: func() { a.newThreadPerm.Set(p) }})
	}
	worktree := "Off"
	if a.newThreadWT.Get() {
		worktree = "On"
	}
	return ui.Dialog{Open: a.showNewThread.Get, OnOpenChange: closeDialog(a.showNewThread), Title: "New thread",
		Description: "The agent runs in this project's directory, or in a fresh worktree.", Children: []core.Node{
			ui.Input{Input: base.Input{Value: a.newThreadTitle.Get, OnChange: a.newThreadTitle.Set, Placeholder: "What is this thread about? (optional)", Label: "Thread title"}},
			ui.Text("Provider", 11, ui.Semibold, colors.Muted),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: providerButtons},
			ui.Text("Model", 11, ui.Semibold, colors.Muted),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: modelButtons},
			ui.Text("Permission", 11, ui.Semibold, colors.Muted),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: permButtons},
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				ui.Text("Own worktree", 12, ui.Regular, colors.Muted), ui.Spacer{},
				ui.Button{Text: worktree, Variant: ui.ButtonSecondary, Size: ui.SizeSmall, OnPress: func() { a.newThreadWT.Set(!a.newThreadWT.Get()) }},
			}},
			core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showNewThread.Set(false) }},
			ui.Button{Text: "Create thread", Variant: ui.ButtonPrimary, OnPress: a.createThread},
		}}
}

func (a *app) settingsDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	settings := a.settings.Get()
	title := settings.Title
	return ui.Dialog{Open: a.showSettings.Get, OnOpenChange: closeDialog(a.showSettings), Title: "Settings",
		Description: "Defaults for new threads in this environment.", Children: []core.Node{
			ui.Input{Input: base.Input{Value: func() string { return a.settings.Get().Title }, OnChange: func(v string) {
				a.settings.Update(func(s proto.Settings) proto.Settings { s.Title = v; return s })
			}, Placeholder: "Environment name", Label: "Environment name"}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
				core.Text{Content: "Server", Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				core.Text{Content: a.serverURL.Get(), MaxLines: 1, Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
				core.Text{Content: "Environment", Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Disconnect", Variant: ui.ButtonGhost, OnPress: func() { a.showSettings.Set(false); a.disconnect() }},
			ui.Spacer{},
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, OnPress: func() { a.showSettings.Set(false) }},
			ui.Button{Text: "Save", Variant: ui.ButtonPrimary, OnPress: func() {
				a.saveSettings(a.settings.Get())
				a.showSettings.Set(false)
			}},
		}}
}

func (a *app) diffDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return ui.Dialog{Open: a.showDiff.Get, OnOpenChange: closeDialog(a.showDiff), Title: "Changes", Description: "The working tree against the run's checkpoint.",
		Children: []core.Node{
			core.Scroll{Style: core.Style{MaxHeight: core.Pt(420), Background: colors.Inset(), CornerRadius: 8, Padding: core.All(10)}, Children: []core.Node{
				core.Text{Content: a.diffText.Get(), Selectable: core.SelectableOn, Style: core.Style{FontSize: 11.5, LineHeight: 16, FontFamily: "Menlo, monospace"},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Close", Variant: ui.ButtonSecondary, OnPress: func() { a.showDiff.Set(false) }},
		}}
}

func (a *app) renameDialog(cx *core.Scope) core.Node {
	target := a.pendingRename.Get()
	title, label := "Rename", "Name"
	switch target.Kind {
	case "thread":
		title, label = "Rename thread", "Thread title"
	case "project":
		title, label = "Rename project", "Project name"
	}
	return ui.Dialog{
		Open: func() bool { return target.Kind != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				a.cancelRename()
			}
		},
		Title:       title,
		Description: "The new name is saved for everyone connected to this environment.",
		Children: []core.Node{
			ui.Input{Input: base.Input{Value: a.renameName.Get, OnChange: a.renameName.Set, Placeholder: label, Label: label,
				SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.confirmRename() }}},
		},
		Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: a.cancelRename},
			ui.Button{Text: "Save", Variant: ui.ButtonPrimary, TestID: "confirm-rename", OnPress: a.confirmRename},
		},
	}
}

func (a *app) deleteDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	target := a.pendingDelete.Get()
	title, description := "Delete", ""
	switch target.Kind {
	case "thread":
		title = "Delete thread"
		name := target.Name
		if name == "" {
			name = "This thread"
		}
		description = name + " and its transcript will be removed. This cannot be undone."
	case "project":
		title = "Delete project"
		name := target.Name
		if name == "" {
			name = "This project"
		}
		description = name + " and all of its threads will be removed. This cannot be undone."
	}
	return ui.AlertDialog{
		Open: func() bool { return target.Kind != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				a.cancelDelete()
			}
		},
		Title:       title,
		Description: description,
		Actions: []ui.AlertAction{
			{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: a.cancelDelete, TestID: "cancel-delete"},
			{Text: "Delete", Variant: ui.ButtonPrimary, Color: func() core.Color { return colors.Danger() },
				OnPress: a.confirmDelete, TestID: "confirm-delete"},
		},
		TestID: "delete-dialog",
	}
}
