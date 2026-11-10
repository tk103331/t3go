package main

import (
	"encoding/json"
	"fmt"
	"time"

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
			Sidebar: []core.Node{a.sidebar(cx)},
			Header:  []core.Node{a.contentHeader(cx)},
			Content: []core.Node{a.content(cx)},
			Open:    a.sidebarOpen.Get, OnOpenChange: a.setSidebarOpen,
			ToggleLabel: "Toggle sidebar",
			Width:       a.sidebarWidth.Get, OnWidthChange: a.setSidebarWidth,
			DefaultWidth: 260, MinWidth: 220, MaxWidth: 380,
			ResizeLabel: "Resize sidebar",
		}
	} else {
		layout = a.narrowView(cx)
	}
	return core.View{Style: core.Style{FlexGrow: 1, Direction: core.Column, Background: colors.Background()}, Children: []core.Node{
		core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node { return a.errorBanner(cx) }},
		core.Show{When: a.offline.Get, Then: func(*core.Scope) core.Node { return a.offlineBanner(cx) }},
		core.View{Style: core.Style{FlexGrow: 1}, Children: []core.Node{layout, a.dialogs(cx)}},
	}}
}

// envName is what the environment calls itself, the brand until it is
// renamed in settings.
func (a *app) envName() string {
	if name := a.settings.Get().Title; name != "" {
		return name
	}
	return "T3Go"
}

// offlineBanner says the event stream dropped and is retrying.
func (a *app) offlineBanner(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "offline", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
		Padding: core.Symmetric(6, 12), Background: colors.Inset()}, Children: []core.Node{
		ui.Spinner{Size: 12, Label: "Reconnecting"},
		core.Text{Content: "Connection lost. Reconnecting…", Style: core.Style{FontSize: 12},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
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
	showArchived := a.showArchived.Get()
	var sections []ui.SidebarSection
	for _, p := range projects {
		var items []ui.SidebarItem
		for _, t := range allThreads {
			if t.ProjectID != p.ID || (t.Archived && !showArchived) {
				continue
			}
			t := t
			title := t.Title
			if t.Status == proto.ThreadRunning {
				title = "● " + title
			} else if t.Status == proto.ThreadFailed {
				title = "! " + title
			}
			if t.Archived {
				title = "◌ " + title
			}
			archiveText := "Archive thread"
			if t.Archived {
				archiveText = "Unarchive thread"
			}
			items = append(items, ui.SidebarItem{ID: t.ID, Text: title, TestID: "thread-" + t.ID,
				Menu: []core.Node{
					ui.MenuItem{Text: "Rename thread…", TestID: "rename-thread-" + t.ID, OnSelect: func() { a.askRenameThread(t) }},
					ui.MenuItem{Text: archiveText, TestID: "archive-thread-" + t.ID, OnSelect: func() { a.setArchived(t, !t.Archived) }},
					ui.MenuItem{Text: "Delete thread", Variant: ui.MenuItemDanger,
						TestID: "delete-thread-" + t.ID, OnSelect: func() { a.askDeleteThread(t) }},
				}})
		}
		section := ui.SidebarSection{Title: p.Name, Items: items}
		section.Actions = ui.Menu{
			TestID:  "project-menu-" + p.ID,
			Trigger: ui.Button{Text: "⋯", Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Project actions", TestID: "project-actions-" + p.ID},
			Children: []core.Node{
				ui.MenuItem{Text: "New thread…", TestID: "new-thread-in-" + p.ID, OnSelect: func() { a.openNewThreadIn(p.ID) }},
				ui.MenuItem{Text: "Rename project…", TestID: "rename-project-" + p.ID, OnSelect: func() { a.askRenameProject(p) }},
				ui.MenuItem{Text: "Delete project", Variant: ui.MenuItemDanger,
					TestID: "delete-project-" + p.ID, OnSelect: func() { a.askDeleteProject(p) }},
			},
		}
		if len(items) == 0 {
			note := "No threads yet"
			for _, t := range allThreads {
				if t.ProjectID == p.ID {
					note = "No threads shown"
					break
				}
			}
			section.Note = ui.Text(note, 12, ui.Regular, colors.Faint)
		}
		sections = append(sections, section)
	}
	if len(sections) == 0 {
		sections = append(sections, ui.SidebarSection{Title: "Projects", Note: ui.Text("Add a project to start.", 12, ui.Regular, colors.Faint)})
	}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		core.View{Style: ui.FillRest, Children: []core.Node{
			ui.Sidebar{Sections: sections, Selected: a.selected.Get, OnSelect: a.selectThread},
		}},
		a.sidebarFooter(cx),
	}}
}

func (a *app) archivedLabel() string {
	if a.showArchived.Get() {
		return "Hide archived"
	}
	return "Archived"
}

func (a *app) sidebarFooter(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, Width: core.Pct(100), Padding: core.Symmetric(6, 4), Gap: 4, Wrap: core.Wrap, AlignItems: core.AlignCenter}, Children: []core.Node{
		ui.Button{Text: "+ Project", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "new-project", OnPress: a.openNewProject},
		ui.Button{Text: "+ Thread", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "new-thread", OnPress: a.openNewThread},
		ui.Spacer{},
		ui.Button{Text: a.archivedLabel(), Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "toggle-archived", OnPress: a.toggleShowArchived},
		ui.Button{Text: "Search", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "open-palette", OnPress: a.openPalette},
		ui.Button{Text: "Settings", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "settings", OnPress: a.openSettings},
		core.Text{Content: "·", Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
	}}
}

func (a *app) contentHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	d := a.detail.Get()
	if d.Thread.ID == "" {
		return core.View{Style: core.Style{Height: core.Pt(32), AlignItems: core.AlignCenter}, Children: []core.Node{
			ui.Text(a.envName(), 13, ui.Semibold, colors.Muted),
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
		ui.Button{Text: "History", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "history", OnPress: a.openRuns},
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
			ui.Button{Text: "New project", Variant: ui.ButtonSecondary, TestID: "welcome-new-project", OnPress: a.openNewProject},
			ui.Button{Text: "New thread", Variant: ui.ButtonPrimary, TestID: "welcome-new-thread", OnPress: a.openNewThread},
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
			ui.Text(a.envName(), 16, ui.Bold, colors.Text), ui.Spacer{},
			ui.Button{Text: "Search", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "open-palette", OnPress: a.openPalette},
			ui.Button{Text: "Settings", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "settings", OnPress: a.openSettings},
		}},
		core.View{Style: ui.FillRest, Children: []core.Node{a.sidebar(cx)}},
	}}
}

func (a *app) dialogs(cx *core.Scope) core.Node {
	return core.Group{a.newProjectDialog(cx), a.newThreadDialog(cx), a.settingsDialog(cx), a.diffDialog(cx), a.runsDialog(cx),
		a.renameDialog(cx), a.deleteDialog(cx), a.paletteDialog(cx), a.approvalDialog(cx)}
}

// approvalDialog asks about the first tool call a supervised run is
// waiting on.
func (a *app) approvalDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	approvals := a.approvals.Get()
	if len(approvals) == 0 {
		return ui.Dialog{Open: func() bool { return false }}
	}
	request := approvals[0]
	description := "A supervised agent wants to use " + request.Tool + "."
	for _, t := range a.threads.Get() {
		if t.ID == request.ThreadID {
			description = "In " + t.Title + ", the agent wants to use " + request.Tool + "."
			break
		}
	}
	var children []core.Node
	if raw, err := json.MarshalIndent(request.Input, "", "  "); err == nil && len(request.Input) > 0 {
		children = append(children, core.Scroll{Style: core.Style{MaxHeight: core.Pt(280), Background: colors.Inset(),
			CornerRadius: 8, Padding: core.All(10)}, Children: []core.Node{
			core.Text{Content: string(raw), Selectable: core.SelectableOn, Style: core.Style{FontSize: 11.5, LineHeight: 16, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}})
	}
	if len(approvals) > 1 {
		children = append(children, ui.Text(fmt.Sprintf("%d more waiting", len(approvals)-1), 11.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{
		Open:         func() bool { return len(a.approvals.Get()) > 0 },
		OnOpenChange: func(bool, base.Reason) {},
		Title:        "Allow tool call?",
		Description:  description,
		Children:     children,
		TestID:       "approval-dialog",
		Footer: []core.Node{
			ui.Button{Text: "Deny", Variant: ui.ButtonGhost, TestID: "approval-deny", OnPress: func() {
				a.resolveApproval(request.ID, false, "Denied by the person running T3Go.")
			}},
			ui.Button{Text: "Allow", Variant: ui.ButtonPrimary, TestID: "approval-allow", OnPress: func() {
				a.resolveApproval(request.ID, true, "")
			}},
		},
	}
}

func closeDialog(sig *core.Signal[bool]) func(bool, base.Reason) {
	return func(open bool, _ base.Reason) { sig.Set(open) }
}

// paletteDialog searches the environment's threads and actions.
func (a *app) paletteDialog(cx *core.Scope) core.Node {
	return ui.CommandDialog{
		Open: a.showPalette.Get, OnOpenChange: closeDialog(a.showPalette),
		TestID: "palette",
		Command: ui.Command{
			Items: a.commandItems, OnSelect: a.runCommand,
			Placeholder: "Search threads and actions…", SearchLabel: "Search commands",
			Empty: "No threads or commands match.", Label: "Commands", TestID: "palette-command",
		},
	}
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
	projects := a.projects.Get()
	projectID := a.newThreadProj.Get()
	var projectButtons []core.Node
	for _, pr := range projects {
		pr := pr
		variant := ui.ButtonSecondary
		if pr.ID == projectID {
			variant = ui.ButtonPrimary
		}
		projectButtons = append(projectButtons, ui.Button{Text: pr.Name, Variant: variant, Size: ui.SizeSmall,
			TestID: "new-thread-project-" + pr.ID, OnPress: func() { a.newThreadProj.Set(pr.ID) }})
	}
	fields := []core.Node{
		ui.Input{Input: base.Input{Value: a.newThreadTitle.Get, OnChange: a.newThreadTitle.Set, Placeholder: "What is this thread about? (optional)", Label: "Thread title"}},
	}
	if len(projects) > 1 {
		fields = append(fields,
			ui.Text("Project", 11, ui.Semibold, colors.Muted),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: projectButtons})
	}
	fields = append(fields,
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
	)
	if a.newThreadWT.Get() {
		fields = append(fields, ui.Input{Input: base.Input{Value: a.newThreadBranch.Get, OnChange: a.newThreadBranch.Set,
			Placeholder: "t3go/<slug>", Label: "Branch (optional)"}})
	}
	fields = append(fields, core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
		return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
	}})
	return ui.Dialog{Open: a.showNewThread.Get, OnOpenChange: closeDialog(a.showNewThread), Title: "New thread",
		Description: "The agent runs in this project's directory, or in a fresh worktree.", Children: fields, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showNewThread.Set(false) }},
			ui.Button{Text: "Create thread", Variant: ui.ButtonPrimary, TestID: "create-thread", OnPress: a.createThread},
		}}
}

func (a *app) settingsDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	draft := a.settingsDraft.Get()
	update := func(fn func(*proto.Settings)) {
		a.settingsDraft.Update(func(s proto.Settings) proto.Settings {
			fn(&s)
			return s
		})
	}
	provider := draft.DefaultProvider
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
			Disabled: func() bool { return !p.Installed }, TestID: "settings-provider-" + p.ID, OnPress: func() {
				update(func(s *proto.Settings) {
					s.DefaultProvider = p.ID
					s.DefaultModel = ""
					for _, m := range a.models.GetUntracked() {
						if m.Provider == p.ID {
							s.DefaultModel = m.ID
							break
						}
					}
				})
			}})
	}
	var modelButtons []core.Node
	for _, m := range a.models.Get() {
		if m.Provider != provider {
			continue
		}
		m := m
		variant := ui.ButtonSecondary
		if m.ID == draft.DefaultModel {
			variant = ui.ButtonPrimary
		}
		modelButtons = append(modelButtons, ui.Button{Text: m.Name, Variant: variant, Size: ui.SizeSmall,
			TestID: "settings-model-" + m.ID, OnPress: func() {
				update(func(s *proto.Settings) { s.DefaultModel = m.ID })
			}})
	}
	perm := proto.PermissionMode(draft.DefaultPermission)
	if !perm.Valid() {
		perm = proto.PermissionFullAccess
	}
	permissions := []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits, proto.PermissionAuto, proto.PermissionFullAccess}
	var permButtons []core.Node
	for _, p := range permissions {
		p := p
		variant := ui.ButtonSecondary
		if p == perm {
			variant = ui.ButtonPrimary
		}
		permButtons = append(permButtons, ui.Button{Text: string(p), Variant: variant, Size: ui.SizeSmall,
			TestID: "settings-perm-" + string(p), OnPress: func() {
				update(func(s *proto.Settings) { s.DefaultPermission = string(p) })
			}})
	}
	fields := []core.Node{
		ui.Input{Input: base.Input{Value: a.settingsTitle.Get, OnChange: a.settingsTitle.Set,
			Placeholder: "Environment name", Label: "Environment name"}},
		ui.Text("Default provider", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: providerButtons},
	}
	if len(modelButtons) > 0 {
		fields = append(fields, ui.Text("Default model", 11, ui.Semibold, colors.Muted),
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: modelButtons})
	}
	var themeButtons []core.Node
	for _, theme := range []string{"system", "light", "dark"} {
		theme := theme
		variant := ui.ButtonSecondary
		if theme == draft.Theme {
			variant = ui.ButtonPrimary
		}
		themeButtons = append(themeButtons, ui.Button{Text: theme, Variant: variant, Size: ui.SizeSmall,
			TestID: "settings-theme-" + theme, OnPress: func() {
				update(func(s *proto.Settings) { s.Theme = theme })
			}})
	}
	fields = append(fields,
		ui.Text("Default permission", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: permButtons},
		ui.Text("Theme", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: themeButtons},
		core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Text{Content: "Server", Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.Text{Content: a.serverURL.Get(), MaxLines: 1, Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}},
	)
	return ui.Dialog{Open: a.showSettings.Get, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			a.closeSettings()
		}
	}, Title: "Settings", Description: "Defaults for new threads in this environment.",
		Children: fields, Footer: []core.Node{
			ui.Button{Text: "Disconnect", Variant: ui.ButtonGhost, TestID: "disconnect", OnPress: func() { a.closeSettings(); a.disconnect() }},
			ui.Spacer{},
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, OnPress: a.closeSettings},
			ui.Button{Text: "Save", Variant: ui.ButtonPrimary, TestID: "settings-save", OnPress: func() {
				a.closeSettings()
				a.saveSettingsDraft()
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

// runsDialog is the thread's run history, newest first, each with what it
// changed when it has a checkpoint.
func (a *app) runsDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	runs := a.detail.Get().Runs
	var rows []core.Node
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		meta := r.StartedAt.Local().Format("Jan 2 15:04")
		if r.EndedAt != nil {
			meta += " · " + r.EndedAt.Sub(r.StartedAt).Round(time.Second).String()
		}
		meta += " · " + r.Model
		if r.Usage != nil {
			if r.Usage.TotalTokens > 0 {
				meta += fmt.Sprintf(" · %d tokens", r.Usage.TotalTokens)
			}
			if r.Usage.CostUSD > 0 {
				meta += fmt.Sprintf(" · $%.2f", r.Usage.CostUSD)
			}
		}
		line := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.Text{Content: string(r.Status), MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: runColor(colors, r.Status)} }},
			core.Text{Content: meta, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}}
		if r.Checkpoint != "" {
			line.Children = append(line.Children, ui.Button{Text: "Diff", Variant: ui.ButtonGhost, Size: ui.SizeSmall,
				TestID: "run-diff-" + r.ID, OnPress: func() { a.openRunDiff(r.ID) }})
		}
		rows = append(rows, core.View{Style: core.Style{Gap: 3, Padding: core.Symmetric(8, 10), Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{
			line,
			core.Text{Content: r.Prompt, MaxLines: 2, Style: core.Style{FontSize: 12, LineHeight: 17},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No runs yet. Ask the agent to change something.", 12.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{Open: a.showRuns.Get, OnOpenChange: closeDialog(a.showRuns), Title: "History",
		Description: "Runs of this thread, newest first.", TestID: "runs-dialog",
		Children: []core.Node{
			core.Scroll{Style: core.Style{MaxHeight: core.Pt(420)}, Children: []core.Node{
				core.View{Style: core.Style{Gap: 6}, Children: rows},
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Close", Variant: ui.ButtonSecondary, OnPress: a.closeRuns},
		}}
}

func runColor(colors ui.Colors, status proto.RunStatus) core.Color {
	switch status {
	case proto.RunFailed:
		return colors.Danger()
	case proto.RunCancelled:
		return colors.Muted()
	case proto.RunRunning:
		return colors.Accent()
	}
	return colors.Text()
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
