package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oneo-me/nsgo"
	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/proto"
)

func main() {
	nsgo.Run(Root, nsgo.WindowSize(1100, 760))
}

// Root keeps every saved environment connected while rendering one conversation.
func Root(cx *core.Scope) core.Node {
	// NSGo's theme carries the role colors the ui reads (accent fills,
	// insets, faint text). Set it before the environments answer, so the
	// first frame is not painted without them.
	cx.SetTheme(ui.DefaultTheme())
	manager := newEnvironmentManager(cx)
	manager.start()
	return manager.view(cx)
}

func (manager *environmentManager) view(cx *core.Scope) core.Node {
	return core.Group{
		core.Dynamic(func(cx *core.Scope) core.Node {
			a := manager.active.Get()
			a.bindKeysOn(cx)
			return core.Group{core.Dynamic(a.environmentDialog), core.Dynamic(func(cx *core.Scope) core.Node {
				if !a.connected.Get() {
					if a.profile.ID == "" {
						return a.connectView(cx)
					}
					if a.connecting.Get() {
						ready := false
						for _, session := range manager.sessions {
							ready = session.connected.Get() || ready
						}
						if !ready {
							return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
								Children: []core.Node{ui.Spinner{Size: 20, Label: "Connecting environments"}}}
						}
					}
				}
				return a.mainView(cx)
			})}
		}),
		core.Each(func() []string {
			_ = manager.revision.Get()
			var ids []string
			for _, p := range manager.profiles.Get() {
				if manager.sessions[p.ID] != nil {
					ids = append(ids, p.ID)
				}
			}
			return ids
		}, func(id string) string { return id }, func(_ *core.Scope, item func() string, _ func() int) core.Node {
			session := manager.sessions[item()]
			return core.Group{core.Func(session.credentialsDialog), core.Dynamic(session.customModelDialog)}
		}),
		core.Dynamic(manager.addEnvironmentDialog),
		core.Dynamic(manager.relayAccountDialog),
	}
}

func (a *app) connectView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		Padding: core.All(24), Background: colors.Background()}, Children: []core.Node{
		core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(420), Gap: 14,
			Padding: core.All(22), CornerRadius: 14, BorderWidth: 1, BorderColor: ui.Alpha(colors.Muted(), 0.18),
			Background: colors.Inset()}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12}, Children: []core.Node{
				brandWordmark(cx, 21),
				core.View{Style: core.Style{Gap: 3}, Children: []core.Node{
					core.Text{Content: "T3Go", Style: core.Style{FontSize: 21, FontWeight: ui.Bold},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					core.Text{Content: "Your coding workspace", Style: core.Style{FontSize: 12},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
				}},
			}},
			core.Text{Content: "Connect to an environment", Style: core.Style{FontSize: 15, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: "Enter the server address and access token to open your projects and threads.", Style: core.Style{FontSize: 12.5, LineHeight: 18},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.Show{When: func() bool { return a.manager != nil }, Then: func(cx *core.Scope) core.Node {
				return ui.Button{Text: "Sign in to Relay", Icon: lucide.UserRound, Variant: ui.ButtonSecondary,
					Size: controlSize(cx), TestID: "relay-account-open",
					OnPress: func() { a.manager.accountOpen.Set(true) }}
			}},
			core.Show{When: func() bool { return a.manager != nil && a.manager.localStarting.Get() }, Then: func(*core.Scope) core.Node {
				return ui.Text("Starting the local environment…", 12, ui.Regular, colors.Muted)
			}},
			core.Show{When: func() bool { return a.manager != nil && a.manager.localErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return ui.Text(a.manager.localErr.Get(), 12, ui.Regular, colors.Danger)
			}},
			mobileInput{Scope: cx, Input: base.Input{Value: a.serverURL.Get, OnChange: a.serverURL.Set, Placeholder: "Server, e.g. http://127.0.0.1:4870",
				Label: "Server address", SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.connect(a.serverURL.Get(), a.token.Get()) }}},
			mobileInput{Scope: cx, Input: base.Input{Value: a.token.Get, OnChange: a.token.Set, Placeholder: "Token, or paste a t3go:// pair link",
				Label: "Token", Masked: true, SubmitBehavior: core.SubmitBehaviorSubmit,
				OnSubmit: func(string) { a.connect(a.serverURL.Get(), a.token.Get()) }}},
			core.Show{
				When: func() bool { return ui.Touch(cx) && cx.Env().CanTakePhoto() },
				Then: func(cx *core.Scope) core.Node {
					return ui.Button{Text: "Scan QR code", Icon: lucide.ScanQrCode, Variant: ui.ButtonSecondary,
						Size: controlSize(cx), TestID: "environment-scan-qr", Flex: 1,
						OnPress: func() { a.scanEnvironmentQR(cx) }}
				},
			},
			core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return core.View{Style: core.Style{Padding: core.Symmetric(8, 10), CornerRadius: 7,
					Background: ui.Alpha(colors.Danger(), 0.1)}, Children: []core.Node{
					ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger),
				}}
			}},
			ui.Button{Text: func() string {
				if a.connecting.Get() {
					return "Connecting…"
				}
				return "Connect"
			}(), Icon: lucide.LogIn, Variant: ui.ButtonPrimary, Size: controlSize(cx),
				TestID:   "connect-environment",
				OnPress:  func() { a.connect(a.serverURL.Get(), a.token.Get()) },
				Disabled: a.connecting.Get, Flex: 1},
			core.Text{Content: "Find the token in the environment data directory, or run `t3go pair`.",
				Style: core.Style{FontSize: 11, LineHeight: 16}, StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			core.Show{When: func() bool { return a.manager == nil || a.manager.pending.Get() != a }, Then: func(*core.Scope) core.Node {
				return ui.Button{Text: "Saved environments", Icon: lucide.Network, Variant: ui.ButtonGhost,
					TestID: "environments", Size: controlSize(cx), OnPress: func() { a.showEnvironments.Set(true) }}
			}},
		}},
	}}
}

func (a *app) mainView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	wide := cx.Env().Window().Width >= 760
	var layout core.Node
	if wide {
		layout = ui.SidebarShell{
			// Keep asynchronous detail/header updates from replacing the input
			// while the user types in the conversation.
			Sidebar: []core.Node{core.Dynamic(a.sidebar)},
			Header:  []core.Node{core.Dynamic(a.contentHeader)},
			Content: []core.Node{core.Dynamic(a.content)},
			// The conversation header owns both panel toggles. SidebarShell
			// renders another leading toggle whenever OnOpenChange is set,
			// which would duplicate the conversation-header control.
			Open:  a.sidebarOpen.Get,
			Width: a.sidebarWidth.Get, OnWidthChange: a.setSidebarWidth,
			DefaultWidth: 260, MinWidth: 220, MaxWidth: 380,
			ResizeLabel: "Resize sidebar",
		}
	} else {
		layout = core.Dynamic(a.narrowView)
	}
	body := layout
	if wide {
		body = core.View{Style: core.Style{Direction: core.Row, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0),
			MinHeight: core.Pt(0), Overflow: core.Hidden}, Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
				Overflow: core.Hidden}, Children: []core.Node{layout}},
			core.Show{When: a.rightOpen.Get, Then: func(cx *core.Scope) core.Node { return a.rightRail(cx) }},
		}}
	}
	style := core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
		Width: core.Pct(100), Height: core.Pct(100), Overflow: core.Hidden, Direction: core.Column,
		Background: colors.Background()}
	if ui.Touch(cx) {
		style.PaddingEnv = core.EnvAll(core.EnvSafeArea)
	}
	return core.View{Style: style, Children: []core.Node{
		core.Show{When: func() bool { return a.connErr.Get() != "" && !a.transportBlocked.Get() },
			Then: func(*core.Scope) core.Node { return core.Dynamic(a.errorBanner) }},
		core.Dynamic(a.connectionBanner),
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
			Overflow: core.Hidden}, Children: []core.Node{body, core.Dynamic(a.dialogs)}},
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
		ui.Button{Text: "Dismiss", Variant: ui.ButtonGhost, Size: controlSize(cx), OnPress: func() { a.connErr.Set("") }},
	}}
}

func (a *app) sidebar(cx *core.Scope) core.Node {
	var takingKeys *core.Signal[bool]
	var offset *core.Points
	if a.manager != nil {
		takingKeys, offset = a.manager.sidebarKeys, &a.manager.sidebarOffset
	}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		core.View{Style: core.Style{Padding: core.Edges{Top: 4, Right: 10, Bottom: 2, Left: 10}}, Children: []core.Node{
			mobileInput{Scope: cx, Input: base.Input{Value: a.sidebarQuery.Get, OnChange: a.sidebarQuery.Set,
				Placeholder: "Search threads", Label: "Search threads"}},
		}},
		core.View{Style: ui.FillRest, Children: []core.Node{
			scopedSidebar{Sections: func() []ui.SidebarSection { return a.sidebarInboxSections(cx) },
				TakingKeys: takingKeys, Offset: offset,
				Selected: func() string { return a.threadKey(a.selected.Get()) }, OnSelect: a.selectSidebarThread,
				CanFocus: func(id string) bool {
					return !strings.HasPrefix(id, "project:") && !strings.Contains(id, "/project:")
				}},
		}},
		a.sidebarFooter(cx),
	}}
}

type inboxThread struct {
	owner   *app
	thread  proto.Thread
	project string
	pinned  bool
}

type inboxProject struct {
	owner   *app
	project proto.Project
}

// sidebarInboxSections is one desktop and web inbox across every connected
// environment. Environment names stay on each row, while the list itself is
// grouped only by operational state.
func (a *app) sidebarInboxSections(cx *core.Scope) []ui.SidebarSection {
	colors := ui.ColorsOf(cx)
	query := strings.ToLower(strings.TrimSpace(a.sidebarQuery.Get()))
	showArchived := a.showArchived.Get()
	sessions := []*app{a}
	if a.manager != nil {
		_ = a.manager.revision.Get()
		sessions = nil
		for _, p := range a.manager.profiles.Get() {
			if session := a.manager.sessions[p.ID]; session != nil {
				sessions = append(sessions, session)
			}
		}
	}

	var recent, working, settled []inboxThread
	var projects []inboxProject
	for _, session := range sessions {
		projectNames := make(map[string]string)
		for _, project := range session.projects.Get() {
			projectNames[project.ID] = project.Name
			projects = append(projects, inboxProject{owner: session, project: project})
		}
		pinned := session.pinned.Get()
		for _, thread := range session.threads.Get() {
			if !showArchived && thread.Archived {
				continue
			}
			if query != "" && !threadMatches(thread, query) {
				continue
			}
			entry := inboxThread{
				owner: session, thread: thread, project: projectNames[thread.ProjectID],
				pinned: pinned[thread.ID],
			}
			switch {
			case thread.Status == proto.ThreadRunning:
				working = append(working, entry)
			case thread.Settled:
				settled = append(settled, entry)
			default:
				recent = append(recent, entry)
			}
		}
	}
	sortInboxThreads := func(threads []inboxThread) {
		sort.SliceStable(threads, func(i, j int) bool {
			if threads[i].pinned != threads[j].pinned {
				return threads[i].pinned
			}
			return threads[i].thread.UpdatedAt.After(threads[j].thread.UpdatedAt)
		})
	}
	sortInboxThreads(recent)
	sortInboxThreads(working)
	sortInboxThreads(settled)

	itemsFor := func(threads []inboxThread) []ui.SidebarItem {
		items := make([]ui.SidebarItem, 0, len(threads))
		for _, entry := range threads {
			item := entry.owner.sidebarThreadItem(entry.thread)
			context := strings.TrimSpace(entry.project)
			if name := strings.TrimSpace(entry.owner.envName()); name != "" {
				if context != "" {
					context += " · "
				}
				context += name
			}
			if !entry.owner.settingsConnectionReady() {
				context += " · " + entry.owner.connectionLabel()
			}
			item.Content = entry.owner.threadRow(cx, entry.thread, colors, context)
			items = append(items, item)
		}
		return items
	}

	var sections []ui.SidebarSection
	if items := itemsFor(recent); len(items) > 0 {
		sections = append(sections, ui.SidebarSection{
			Items: items, TestID: "sidebar-status-recent",
		})
	}
	for _, group := range []struct {
		name    string
		threads []inboxThread
	}{
		{name: "Working", threads: working},
		{name: "Settled", threads: settled},
	} {
		items := itemsFor(group.threads)
		if len(items) == 0 {
			continue
		}
		status := strings.ToLower(group.name)
		sections = append(sections, ui.SidebarSection{
			Title: group.name, Items: items, Collapsible: true,
			Open: func() bool {
				return !a.statusSectionCollapsed(status)
			},
			OnOpenChange: func(open bool) {
				a.setStatusSectionCollapsed(status, !open)
			},
			Trailing: ui.Text(strconv.Itoa(len(items)), 10.5, ui.Regular, colors.Faint),
			TestID:   "sidebar-status-" + status,
		})
	}

	if len(projects) > 0 && query == "" {
		items := make([]ui.SidebarItem, 0, len(projects))
		for _, entry := range projects {
			owner, project := entry.owner, entry.project
			rowID := owner.sidebarID(project.ID)
			menu := ui.Menu{
				TestID: "project-menu-" + rowID,
				Trigger: ui.Button{Text: "⋯", Variant: ui.ButtonGhost, Size: controlSize(cx),
					Label: "Project actions", TestID: "project-actions-" + rowID},
				Children: []core.Node{
					ui.MenuItem{Text: "New thread…", TestID: "new-thread-in-" + rowID, OnSelect: owner.inEnvironment(func() { owner.openNewThreadIn(project.ID) })},
					ui.MenuItem{Text: "Rename project…", TestID: "rename-project-" + rowID, OnSelect: owner.inEnvironment(func() { owner.askRenameProject(project) })},
					ui.MenuItem{Text: "Delete project", Variant: ui.MenuItemDanger,
						TestID: "delete-project-" + rowID, OnSelect: owner.inEnvironment(func() { owner.askDeleteProject(project) })},
				},
			}
			items = append(items, ui.SidebarItem{
				ID: owner.threadKey("project:" + project.ID), Text: project.Name,
				Content: core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 7,
					MinWidth: core.Pt(0), FlexGrow: 1}, Children: []core.Node{
					ui.Icon(lucide.Folder, 14, colors.Muted),
					core.Text{Content: project.Name, TestID: "project-name-" + rowID,
						Style: core.Style{FontSize: 12.5, FontWeight: ui.Medium, FlexShrink: 1},
						StyleFunc: func() core.Style {
							return core.Style{Color: colors.Text()}
						}},
					core.Text{Content: projectEnvironmentLabel(owner), TestID: "project-environment-" + rowID, MaxLines: 1,
						Style:     core.Style{FontSize: 10, FlexShrink: 1},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
					ui.Spacer{},
					menu,
				}},
				TestID: "project-" + rowID,
			})
		}
		sections = append(sections, ui.SidebarSection{
			Title: "Projects", Items: items, Collapsible: true,
			Open: func() bool {
				return !a.statusSectionCollapsed("projects")
			},
			OnOpenChange: func(open bool) {
				a.setStatusSectionCollapsed("projects", !open)
			},
			Trailing: ui.Text(strconv.Itoa(len(items)), 10.5, ui.Regular, colors.Faint),
			TestID:   "sidebar-project-management",
		})
	}
	if len(sections) == 0 && query != "" {
		sections = append(sections, ui.SidebarSection{Title: "Threads", TestID: "sidebar-no-results",
			Note: ui.Text("No threads match your search.", 12, ui.Regular, colors.Faint)})
	}
	return sections
}

// sidebarSections builds the project and thread groups under the current
// search, inside the sidebar's own scope so typing keeps the field still.
func (a *app) sidebarSections(cx *core.Scope) []ui.SidebarSection {
	query := strings.ToLower(strings.TrimSpace(a.sidebarQuery.Get()))
	showArchived := a.showArchived.Get()
	if a.manager == nil {
		return a.projectSections(cx, query, showArchived)
	}
	_ = a.manager.revision.Get()
	var sections []ui.SidebarSection
	for _, p := range a.manager.profiles.Get() {
		session := a.manager.sessions[p.ID]
		if session == nil {
			continue
		}
		groups := session.projectSections(cx, query, showArchived)
		if query != "" && len(groups) == 0 {
			continue
		}
		colors := ui.ColorsOf(cx)
		name := session.envName()
		sections = append(sections, ui.SidebarSection{
			Title: name, TestID: "sidebar-environment-" + p.ID,
			Trailing: core.Text{Content: session.connectionLabel(), TestID: "environment-status-" + p.ID,
				Style: core.Style{FontSize: 10}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		})
		sections = append(sections, groups...)
	}
	if len(sections) == 0 && query != "" {
		sections = append(sections, ui.SidebarSection{Title: "Projects", TestID: "sidebar-no-results",
			Note: ui.Text("No threads match your search.", 12, ui.Regular, ui.ColorsOf(cx).Faint)})
	}
	return sections
}

func (a *app) threadKey(id string) string {
	if id == "" || a.manager == nil {
		return id
	}
	return a.profile.ID + "/" + id
}

func (a *app) sidebarID(id string) string {
	if a.manager != nil && len(a.manager.profiles.Get()) > 1 {
		return a.profile.ID + "/" + id
	}
	return id
}

func (a *app) selectSidebarThread(key string) {
	if a.manager == nil {
		if projectID, ok := strings.CutPrefix(key, "project:"); ok {
			a.openNewThreadIn(projectID)
			return
		}
		a.showNewThread.Set(false)
		a.selectThread(key)
		return
	}
	env, target, ok := strings.Cut(key, "/")
	if session := a.manager.sessions[env]; ok && session != nil {
		a.manager.activate(env)
		if projectID, isProject := strings.CutPrefix(target, "project:"); isProject {
			session.openNewThreadIn(projectID)
			return
		}
		a.showNewThread.Set(false)
		session.selectThread(target)
	}
}

func (a *app) inEnvironment(action func()) func() {
	return func() {
		if a.manager != nil {
			a.manager.activate(a.profile.ID)
		}
		action()
	}
}

func (a *app) projectSections(cx *core.Scope, query string, showArchived bool) []ui.SidebarSection {
	colors := ui.ColorsOf(cx)
	projects := a.projects.Get()
	allThreads := a.threads.Get()
	pinned := a.pinned.Get()
	var sections []ui.SidebarSection
	for _, p := range projects {
		p := p
		var threads []proto.Thread
		for _, t := range allThreads {
			if t.ProjectID != p.ID || (t.Archived && !showArchived) {
				continue
			}
			if query != "" && !threadMatches(t, query) {
				continue
			}
			threads = append(threads, t)
		}
		sort.SliceStable(threads, func(i, j int) bool {
			iPinned, jPinned := pinned[threads[i].ID], pinned[threads[j].ID]
			if iPinned != jPinned {
				return iPinned
			}
			return threads[i].UpdatedAt.After(threads[j].UpdatedAt)
		})
		var items []ui.SidebarItem
		for _, t := range threads {
			t := t
			item := a.sidebarThreadItem(t)
			item.Content = a.threadRow(cx, t, colors, "")
			items = append(items, item)
		}
		if query != "" && len(items) == 0 {
			continue
		}
		section := ui.SidebarSection{Title: p.Name, Items: items, Collapsible: true,
			Open:         func() bool { return !a.projectCollapsed(p.ID) },
			OnOpenChange: func(open bool) { a.setProjectCollapsed(p.ID, !open) },
			Trailing:     ui.Text(strconv.Itoa(len(items)), 10.5, ui.Regular, colors.Faint),
			TestID:       "project-" + a.sidebarID(p.ID),
		}
		section.Actions = ui.Menu{
			TestID:  "project-menu-" + a.sidebarID(p.ID),
			Trigger: ui.Button{Text: "⋯", Variant: ui.ButtonGhost, Size: controlSize(cx), Label: "Project actions", TestID: "project-actions-" + a.sidebarID(p.ID)},
			Children: []core.Node{
				ui.MenuItem{Text: "New thread…", TestID: "new-thread-in-" + a.sidebarID(p.ID), OnSelect: a.inEnvironment(func() { a.openNewThreadIn(p.ID) })},
				ui.MenuItem{Text: "Rename project…", TestID: "rename-project-" + a.sidebarID(p.ID), OnSelect: a.inEnvironment(func() { a.askRenameProject(p) })},
				ui.MenuItem{Text: "Delete project", Variant: ui.MenuItemDanger,
					TestID: "delete-project-" + a.sidebarID(p.ID), OnSelect: a.inEnvironment(func() { a.askDeleteProject(p) })},
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
		if query != "" && a.manager != nil {
			return nil
		}
		note := "Add a project to start."
		switch {
		case query != "":
			note = "No threads match your search."
		case len(projects) > 0:
			note = "No threads yet."
		}
		sections = append(sections, ui.SidebarSection{Title: "Projects", TestID: "projects-empty-" + a.profile.ID,
			Note: ui.Text(note, 12, ui.Regular, colors.Faint)})
	}
	return sections
}

func (a *app) sidebarThreadItem(t proto.Thread) ui.SidebarItem {
	archiveText := "Archive thread"
	if t.Archived {
		archiveText = "Unarchive thread"
	}
	pinText := "Pin thread"
	if a.threadPinned(t.ID) {
		pinText = "Unpin thread"
	}
	rowID := a.sidebarID(t.ID)
	return ui.SidebarItem{ID: a.threadKey(t.ID), Text: t.Title, TestID: "thread-" + rowID,
		Menu: []core.Node{
			ui.MenuItem{Text: pinText, TestID: "pin-thread-" + rowID, OnSelect: func() { a.togglePin(t) }},
			a.settleMenuItem(t, rowID),
			ui.MenuItem{Text: "Rename thread…", TestID: "rename-thread-" + rowID, OnSelect: a.inEnvironment(func() { a.askRenameThread(t) })},
			ui.MenuItem{Text: archiveText, TestID: "archive-thread-" + rowID, OnSelect: func() { a.setArchived(t, !t.Archived) }},
			ui.MenuItem{Text: "Delete thread", Variant: ui.MenuItemDanger,
				TestID: "delete-thread-" + rowID, OnSelect: a.inEnvironment(func() { a.askDeleteThread(t) })},
		}}
}

// threadRow is one thread's line in the sidebar: its state, title, the
// last prompt and when it changed.
func (a *app) threadRow(cx *core.Scope, t proto.Thread, colors ui.Colors, projectName string) core.Node {
	title := t.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled thread"
	}
	titleColor := colors.Text
	if t.Archived || t.Settled {
		titleColor = colors.Muted
	}
	head := []core.Node{}
	switch {
	case t.Status == proto.ThreadRunning:
		head = append(head, ui.Spinner{Size: 11, Label: "Running"})
	case t.Status == proto.ThreadFailed:
		head = append(head, ui.Icon(lucide.CircleAlert, 12, colors.Danger))
	case a.threadUnread(t):
		head = append(head, core.View{TestID: "unread-" + t.ID,
			Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3, Background: colors.Accent()}})
	case t.Archived:
		head = append(head, ui.Icon(lucide.Archive, 11, colors.Faint))
	}
	if a.threadPinned(t.ID) {
		head = append(head, core.View{TestID: "pinned-" + t.ID, Children: []core.Node{ui.Icon(lucide.Pin, 10, colors.Faint)}})
	}
	head = append(head,
		core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 12.5, FontWeight: ui.Semibold, FlexGrow: 1, FlexShrink: 1},
			StyleFunc: func() core.Style { return core.Style{Color: titleColor()} }},
		core.Text{Content: relativeTime(t.UpdatedAt), MaxLines: 1, Style: core.Style{FontSize: 10.5, FlexShrink: 0},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
	)
	// The list carries the thread's own actions, so the header stays
	// free of them. They show for the thread the list has open.
	head = append(head, core.Show{When: func() bool { return a.selected.GetUntracked() == t.ID },
		Then: func(*core.Scope) core.Node {
			return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2,
				FlexShrink: 0}, Children: []core.Node{
				ui.IconButton{Icon: lucide.Pencil, Label: "Rename thread", TestID: "rename-thread-" + a.sidebarID(t.ID),
					Size: 22, IconSize: 13, Variant: ui.IconButtonGhost,
					OnPress: func() { a.inEnvironment(func() { a.askRenameThread(t) })() }},
				ui.IconButton{Icon: lucide.Trash, Label: "Delete thread", TestID: "delete-thread-" + a.sidebarID(t.ID),
					Size: 22, IconSize: 13, Variant: ui.IconButtonGhost,
					OnPress: func() { a.inEnvironment(func() { a.askDeleteThread(t) })() }},
			}}
		}})
	rows := []core.Node{core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, MinWidth: core.Pt(0)}, Children: head}}
	draft := a.draftFor(t.ID)
	preview := oneLine(t.Summary, 80)
	previewColor := colors.Faint
	if draft != "" {
		preview, previewColor = "Draft: "+oneLine(draft, 72), colors.Accent
	} else if preview == "" {
		preview = t.Provider
	}
	if projectName = strings.TrimSpace(projectName); projectName != "" && draft == "" {
		if preview == "" {
			preview = projectName
		} else {
			preview = projectName + " · " + preview
		}
	}
	line := []core.Node{}
	switch {
	case draft != "":
		line = append(line, ui.Icon(lucide.SquarePen, 10, colors.Accent))
	case t.WorktreePath != "":
		line = append(line, ui.Icon(lucide.GitBranch, 10, colors.Faint))
	}
	if strings.TrimSpace(preview) != "" {
		line = append(line, core.Text{Content: preview, TestID: "thread-context-" + a.sidebarID(t.ID),
			MaxLines: 1, Style: core.Style{FontSize: 11, FlexGrow: 1, FlexShrink: 1},
			StyleFunc: func() core.Style { return core.Style{Color: previewColor()} }})
	}
	if len(line) > 0 {
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, MinWidth: core.Pt(0)}, Children: line})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Padding: core.Symmetric(3, 0)},
		Children: rows}
}

func projectEnvironmentLabel(owner *app) string {
	label := owner.envName()
	if owner.settingsConnectionReady() {
		return label
	}
	return label + " · " + owner.connectionLabel()
}

// threadMatches reports whether a thread's title, prompt or branch matches
// the sidebar's search.
func threadMatches(t proto.Thread, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{t.Title, t.Summary, t.Branch, t.Provider, t.Model}, " "))
	return strings.Contains(haystack, query)
}

// relativeTime writes the compact age the sidebar shows.
func relativeTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	age := time.Since(at)
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	case at.Year() == time.Now().Year():
		return at.Local().Format("Jan 2")
	}
	return at.Local().Format("Jan 2006")
}

func (a *app) archivedLabel() string {
	if a.showArchived.Get() {
		return "Hide archived"
	}
	return "Archived"
}

func (a *app) sidebarFooter(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, Width: core.Pct(100), Padding: core.Symmetric(6, 4), Gap: 4, AlignItems: core.AlignCenter}, Children: []core.Node{
		ui.IconButton{Icon: lucide.FolderPlus, Label: "New project", Size: iconButtonSize(cx), TestID: "new-project", OnPress: a.openNewProject},
		ui.IconButton{Icon: lucide.MessageSquarePlus, Label: "New thread", Size: iconButtonSize(cx), TestID: "new-thread", OnPress: a.openNewThread},
		ui.Spacer{},
		ui.IconButton{IconFunc: func() *core.SVG {
			if a.showArchived.Get() {
				return lucide.ArchiveRestore()
			}
			return lucide.Archive()
		}, LabelFunc: a.archivedLabel, Size: iconButtonSize(cx), TestID: "toggle-archived", OnPress: a.toggleShowArchived},
		ui.IconButton{Icon: lucide.Search, Label: "Search", Size: iconButtonSize(cx), TestID: "open-palette", OnPress: a.openPalette},
		ui.IconButton{Icon: lucide.Settings, Label: "Settings", Size: iconButtonSize(cx), TestID: "settings", OnPress: a.openSettings},
	}}
}

func (a *app) contentHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.showNewThread.Get() && !compactSettings(cx) {
		return core.View{TestID: "new-thread-header", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
			Gap: 7, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			a.sidebarToggle(),
			brandWordmark(cx, 17),
			ui.Text("/", 12, ui.Regular, colors.Faint),
			ui.Text("New thread", 12.5, ui.Medium, colors.Text),
			ui.Spacer{},
			ui.IconButton{Icon: lucide.X, Label: "Cancel new thread", TestID: "cancel-thread",
				Size: 34, IconSize: 17, Disabled: a.threadBusy.Get,
				OnPress: func() { a.showNewThread.Set(false) }},
			a.rightToggle(),
		}}
	}
	d := a.detail.Get()
	if d.Thread.ID == "" {
		return core.View{Style: core.Style{Height: core.Pt(32), Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6,
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			a.sidebarToggle(), core.Dynamic(a.environmentStatus), ui.Spacer{}, a.rightToggle(),
		}}
	}
	// Two lines only: the thread's title, then the environment it runs on
	// and how it is reached. The agent, model and permission mode are in
	// the composer already.
	title := core.View{Style: core.Style{Direction: core.Column, MinWidth: core.Pt(0), FlexGrow: 1, Gap: 1}, Children: []core.Node{
		core.Text{Content: d.Thread.Title, MaxLines: 1, Style: core.Style{FontSize: 13.5, FontWeight: ui.Semibold},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		core.Dynamic(a.environmentStatus),
	}}
	if compactSettings(cx) {
		// A phone row has no space for every action beside the title.
		return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, MinWidth: core.Pt(0)}, Children: []core.Node{
			title,
			ui.IconButton{Icon: lucide.GitBranch, Label: "Branches", TestID: "branch-menu",
				OnPress: func() { a.showBranches.Set(true) }},
			ui.Menu{TestID: "thread-actions", Open: a.threadMenuOpen.Get,
				OnOpenChange: func(open bool, _ base.Reason) {
					a.threadMenuGuard.change(open, a.threadMenuOpen.Set)
				},
				Trigger: ui.Button{Text: "⋯", Variant: ui.ButtonGhost,
					Size: controlSize(cx), Label: "Thread actions", TestID: "thread-actions"},
				Children: []core.Node{
					ui.MenuItem{Text: "Rename thread…", TestID: "rename-thread", OnSelect: func() { a.askRenameThread(d.Thread) }},
					ui.MenuItem{Text: "Run history", TestID: "history", OnSelect: a.openRuns},
					ui.MenuItem{Text: "Open in editor", TestID: "open-editor", OnSelect: a.openInEditor},
					ui.MenuItem{Text: "Changes in this thread", TestID: "open-diff", OnSelect: a.openDiff},
					ui.MenuItem{Text: "Delete thread", Variant: ui.MenuItemDanger, TestID: "delete-thread",
						OnSelect: func() { a.askDeleteThread(d.Thread) }},
				}},
		}}
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
		FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
		a.sidebarToggle(),
		title,
		a.branchMenu(cx, d),
		ui.Menu{TestID: "thread-menu", Open: a.threadMenuOpen.Get,
			OnOpenChange: func(open bool, _ base.Reason) {
				a.threadMenuGuard.change(open, a.threadMenuOpen.Set)
			},
			Trigger: ui.Button{Icon: lucide.Ellipsis, Label: "Thread actions",
				Side: 34, IconSize: 18, Variant: ui.ButtonGhost},
			Children: []core.Node{
				ui.MenuItem{Text: "Run history", TestID: "history", OnSelect: a.openRuns},
				ui.MenuItem{Text: "Open in editor", TestID: "open-editor", OnSelect: a.openInEditor},
				ui.MenuItem{Text: "Changes in this thread", TestID: "open-diff", OnSelect: a.openDiff},
			}},
		a.rightToggle(),
	}}
}

// sidebarToggle sits at the leading edge of the conversation header, so
// both panel controls stay in the workspace rather than the window chrome.
func (a *app) sidebarToggle() core.Node {
	if a.cx.Env().Window().Width < 760 {
		return core.View{}
	}
	return ui.IconButton{Icon: lucide.PanelLeft, Label: "Toggle sidebar", TestID: "toggle-sidebar",
		Selected: a.sidebarOpen.Get, OnPress: func() { a.setSidebarOpen(!a.sidebarOpen.GetUntracked()) }}
}

// rightToggle shows or hides the surfaces panel.
func (a *app) rightToggle() core.Node {
	if a.cx.Env().Window().Width < 760 {
		return core.View{}
	}
	return ui.IconButton{Icon: lucide.PanelRight, Label: "Toggle right panel", TestID: "toggle-right-panel",
		Selected: a.rightOpen.Get, OnPress: func() { a.setRightOpen(!a.rightOpen.GetUntracked()) }}
}

// branchMenu is the header's branch control.
func (a *app) branchMenu(cx *core.Scope, detail proto.ThreadDetail) core.Node {
	branch := detail.Thread.Branch
	if detail.Git != nil && detail.Git.Branch != "" {
		branch = detail.Git.Branch
	}
	if branch == "" {
		branch = "detached"
	}
	return ui.Button{Text: branch, Icon: lucide.GitBranch, Variant: ui.ButtonGhost, Size: controlSize(cx),
		Label: "Branch", TestID: "branch-menu", OnPress: func() { a.showBranches.Set(true) }}
}

func (a *app) content(cx *core.Scope) core.Node {
	if a.showNewThread.Get() && !compactSettings(cx) {
		return a.desktopNewThreadPage(cx)
	}
	if a.selected.Get() == "" {
		return a.welcome(cx)
	}
	return a.threadPane(cx)
}

func (a *app) welcome(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	phone := cx.Env().Window().Width < 520
	projectCount := len(a.projects.Get())
	message := "Create a project to give your agent a workspace, or start a thread in one you already have."
	if projectCount > 0 {
		message = fmt.Sprintf("You have %d %s ready. Pick a thread from the sidebar or start a fresh one.",
			projectCount, plural(projectCount, "project", "projects"))
	}
	buttons := []core.Node{
		ui.Button{Text: "New thread", Icon: lucide.MessageSquarePlus, Variant: ui.ButtonPrimary,
			TestID: "welcome-new-thread", Flex: 1, OnPress: a.openNewThread},
		ui.Button{Text: "New project", Icon: lucide.FolderPlus, Variant: ui.ButtonSecondary,
			TestID: "welcome-new-project", Flex: 1, OnPress: a.openNewProject},
	}
	buttonStyle := core.Style{Direction: core.Row, Gap: 8, Width: core.Pct(100)}
	if phone {
		buttonStyle.Direction = core.Column
	}
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		Padding: core.All(24), Background: colors.Background()}, Children: []core.Node{
		core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(420), AlignItems: core.AlignCenter,
			Gap: 12, Padding: core.All(24), CornerRadius: 14, BorderWidth: 1,
			BorderColor: ui.Alpha(colors.Muted(), 0.16), Background: colors.Inset()}, Children: []core.Node{
			brandWordmark(cx, 24),
			core.Text{Content: "Welcome to " + a.envName(), MaxLines: 1,
				Style:     core.Style{FontSize: 19, FontWeight: ui.Bold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: message, Style: core.Style{FontSize: 12.5, LineHeight: 19, TextAlign: core.TextAlignCenter},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.View{Style: buttonStyle, Children: buttons},
		}},
	}}
}

func plural(n int, singular, multiple string) string {
	if n == 1 {
		return singular
	}
	return multiple
}

// narrowView is the phone layout: a list of threads, or the open thread
// with a way back to the list.
func (a *app) narrowView(cx *core.Scope) core.Node {
	cx.OnBackPress(func() bool {
		if a.showNewThread.GetUntracked() {
			if target := a.newThreadTarget(); target == nil || !target.threadBusy.GetUntracked() {
				a.showNewThread.Set(false)
			}
		} else if a.mobileSurface.GetUntracked() != "" {
			a.closeMobileSurface()
		} else if a.selected.GetUntracked() != "" {
			a.persistView()
			a.selected.Set("")
		} else if a.mobilePage.GetUntracked() == "settings" && a.mobileSettingsDetail.GetUntracked() != "" {
			a.mobileSettingsDetail.Set("")
		} else if a.mobilePage.GetUntracked() == "threads" && a.mobileThreadSearch.GetUntracked() {
			a.closeMobileThreadSearch()
		} else if a.mobilePage.GetUntracked() != "threads" {
			a.mobilePage.Set("threads")
		} else {
			return false
		}
		return true
	})
	if a.showNewThread.Get() {
		return core.Func(a.mobileNewThread)
	}
	if a.selected.Get() != "" {
		if a.mobileSurface.Get() != "" {
			return core.Func(a.mobileSurfaceView)
		}
		return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Overflow: core.Hidden},
			Children: []core.Node{
				core.Dynamic(a.mobileConversationHeader),
				core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Overflow: core.Hidden,
					CornerRadius: 24, Background: ui.ColorsOf(cx).Surface()}, Children: []core.Node{a.threadPane(cx)}},
			}}
	}
	return a.mobileHome(cx)
}

func (a *app) dialogs(cx *core.Scope) core.Node {
	// Each modal owns its subscriptions. Keyboard/layout changes in one dialog
	// must not dispose a focused field in another one.
	dialogs := core.Group{
		core.Dynamic(a.newProjectDialog), core.Dynamic(a.settingsDialog),
		core.Dynamic(a.diffDialog), core.Dynamic(a.runsDialog), core.Dynamic(a.renameDialog), core.Dynamic(a.deleteDialog),
		core.Dynamic(a.paletteDialog), core.Dynamic(a.approvalDialog), core.Dynamic(a.discardDialog),
		core.Dynamic(a.editMessageDialog), core.Dynamic(a.newBranchDialog), core.Dynamic(a.branchesDialog),
		core.Dynamic(a.promptArgsDialog), core.Dynamic(a.devicesDialog),
		core.Dynamic(a.restoreDialog), core.Dynamic(a.dirsDialog), core.Dynamic(a.newDirDialog),
		core.Dynamic(a.mobileCommitDialog),
	}
	if a.manager == nil {
		dialogs = append(dialogs, core.Dynamic(a.customModelDialog))
	}
	return dialogs
}

// approvalDialog asks about the first tool call a supervised run is
// waiting on.
func (a *app) approvalDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	approvals := a.approvals.Get()
	if len(approvals) == 0 {
		return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: func() bool { return false }}
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
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx),
		Open:         func() bool { return len(a.approvals.Get()) > 0 },
		OnOpenChange: func(bool, base.Reason) {},
		Title:        "Allow tool call?",
		Description:  description,
		Children:     children,
		TestID:       "approval-dialog",
		Footer: []core.Node{
			ui.Button{Text: "Deny", Icon: lucide.X, Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "approval-deny", OnPress: func() {
				a.resolveApproval(request.ID, false, "Denied by the person running T3Go.")
			}},
			ui.Button{Text: "Allow", Icon: lucide.Check, Variant: ui.ButtonPrimary, Size: controlSize(cx), TestID: "approval-allow", OnPress: func() {
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
	commandHeight := core.Pt(580)
	if compactSettings(cx) {
		commandHeight = core.Pt(440)
	}
	command := ui.Command{
		AutoFocus: true,
		Style:     core.Style{FlexGrow: 1, FlexShrink: 1, Height: commandHeight},
		Items:     a.commandItems, OnSelect: a.runCommand,
		OnQueryChange: a.searchMessages,
		Loading:       func() bool { return a.searchBusy.Get() },
		Error:         func() string { return a.searchErr.Get() },
		Placeholder:   "Search threads, messages and actions…", SearchLabel: "Search commands",
		Empty: "No threads, messages or commands match.", Label: "Commands", TestID: "palette-command",
		OnCancel: func() { a.showPalette.Set(false) },
	}
	style := core.Style{}
	var footer []core.Node
	if compactSettings(cx) {
		// Keep the palette above the software keyboard, which covers the
		// bottom of the window on phones. A sheet swipe would take the
		// list's drags, so phones close the palette with this button.
		style.MarginEnv = core.EnvEdges{Bottom: core.EnvKeyboard}
		footer = []core.Node{ui.Button{Text: "Close", Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "palette-close",
			OnPress: func() { a.showPalette.Set(false) }}}
	} else {
		style.Width = core.Pt(720)
	}
	return ui.Dialog{Open: a.showPalette.Get, OnOpenChange: closeDialog(a.showPalette),
		TestID: "palette", Label: "Commands", Bare: true, Style: style, Footer: footer,
		// Let a tap outside dismiss the palette on phones too. The command
		// field may have the IME focused, so relying on the system back action
		// alone can leave the sheet visible after the keyboard is dismissed.
		NoCloseOnBackdropPress: false,
		Children:               []core.Node{command},
	}
}

func (a *app) newProjectDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	browseLabel := "Browse…"
	if compactSettings(cx) || ui.Touch(cx) {
		browseLabel = "Browse host…"
	}
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showNewProject.Get, OnOpenChange: closeDialog(a.showNewProject), Title: "New project",
		Description: "Register a directory the agent can work in. The folder must already exist.",
		TestID:      "new-project-dialog", Style: dialogWidth(cx, 520), Children: []core.Node{
			core.Dynamic(a.projectEnvironmentSelector),
			mobileInput{Scope: cx, Input: base.Input{Value: a.newProjectName.Get, OnChange: a.newProjectName.Set,
				Placeholder: "Defaults to the folder's name", Label: "Project name"}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
				core.View{Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
					mobileInput{Scope: cx, Input: base.Input{Value: a.newProjectPath.Get, OnChange: a.newProjectPath.Set,
						Placeholder: "/path/to/project", Label: "Project path", SubmitBehavior: core.SubmitBehaviorSubmit,
						OnSubmit: func(string) { a.createProject() }}},
				}},
				ui.Button{Text: browseLabel, Icon: lucide.FolderOpen, Variant: ui.ButtonSecondary,
					TestID: "browse-project", Disabled: a.projectBusy.Get, OnPress: a.browseProject},
			}},
			core.Text{Content: "The project's path is used for diffs, files, terminals and worktrees.",
				Style:     core.Style{FontSize: 11},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			core.Show{When: func() bool { return a.connErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return ui.Text(a.connErr.Get(), 12, ui.Regular, colors.Danger)
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "cancel-project",
				OnPress: func() { a.showNewProject.Set(false) }},
			ui.Button{Text: "Add project", Icon: lucide.FolderPlus, Variant: ui.ButtonPrimary, Size: controlSize(cx),
				TestID: "create-project", Disabled: func() bool { return a.projectBusy.Get() }, OnPress: a.createProject},
		}}
}

func (a *app) desktopNewThreadPage(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "new-thread-page", Style: core.Style{FlexGrow: 1, FlexShrink: 1,
		MinWidth: core.Pt(0), MinHeight: core.Pt(0), Padding: core.Symmetric(24, 28),
		Background: colors.Background()}, Children: []core.Node{a.desktopNewThreadBody(cx)}}
}

// desktopNewThreadBody keeps the composer outside the stateful option
// sections. Selecting a provider, permission or worktree option therefore
// does not replace the input or its send action.
func (a *app) desktopNewThreadBody(cx *core.Scope) core.Node {
	return core.View{
		Style: core.Style{
			FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0),
			AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.Scroll{
				Style: core.Style{Width: core.Pct(100), FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
				Children: []core.Node{core.View{
					Style: core.Style{Width: core.Pct(100), MinHeight: core.Pct(100),
						AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Padding: core.Edges{Bottom: 32}},
					Children: []core.Node{core.View{
						Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(900), Gap: 18},
						Children: []core.Node{
							core.Dynamic(a.desktopNewThreadHero),
							mobileComposer{
								Draft: a.newThreadPrompt.Get, OnDraftChange: a.newThreadPrompt.Set,
								Label:       "New thread message",
								Placeholder: "Ask anything. @ files, $ skills, / commands",
								TestID:      "new-thread-composer", SendTestID: "create-thread",
								EditorMinHeight: 104, EditorFocusedHeight: 104, EditorMaxHeight: 190,
								AllowEmptySend: true,
								Disabled:       func() bool { return a.threadBusy.Get() },
								SendIcon:       lucide.ArrowUp,
								OnSend: func() {
									a.createPreparedThread(strings.TrimSpace(a.newThreadPrompt.GetUntracked()) != "")
								},
								Accessory: func(cx *core.Scope) core.Node {
									return ui.IconButton{Icon: lucide.Paperclip, Label: "Attach files",
										TestID: "new-thread-attach", Size: 34, IconSize: 18,
										OnPress: a.pickNewThreadAttachments}
								},
								Controls:       a.desktopNewThreadComposerControls,
								Attachments:    func(cx *core.Scope) core.Node { return a.newThreadAttachmentChips(cx) },
								HasAttachments: func() bool { return len(a.newThreadFiles.Get()) > 0 },
								OnDropFiles:    a.attachNewThreadPaths,
							},
							core.Dynamic(a.desktopNewThreadContext),
							core.Dynamic(a.desktopNewThreadError),
						},
					}},
				}},
			},
		},
	}
}

func (a *app) desktopNewThreadHero(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	project := ""
	for _, candidate := range a.projects.Get() {
		if candidate.ID == a.newThreadProj.Get() {
			project = candidate.Name
			break
		}
	}
	subtitle := "Choose a project below to give the agent a workspace."
	if project != "" {
		subtitle = "Start a new thread in " + project + "."
	}
	return core.View{Style: core.Style{AlignItems: core.AlignCenter, Gap: 5, Padding: core.Edges{Bottom: 4}},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, JustifyContent: core.JustifyCenter,
				Gap: 0, Wrap: core.Wrap}, Children: []core.Node{
				core.Text{Content: "What should we build in",
					Style:     core.Style{FontSize: 28, FontWeight: ui.Semibold, TextAlign: core.TextAlignCenter},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.View{Style: core.Style{Padding: core.Edges{Left: 8}}, Children: []core.Node{brandWordmark(cx, 30)}},
				core.Text{Content: "?", Style: core.Style{FontSize: 28, FontWeight: ui.Semibold},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			}},
			ui.Text(subtitle, 13.5, ui.Regular, colors.Muted),
		}}
}

func (a *app) desktopNewThreadComposerControls(cx *core.Scope) core.Node {
	compact := ui.Touch(cx) && cx.Env().Window().Width < 620
	return core.View{Style: core.Style{Direction: core.Row, Width: core.Pct(100), MinWidth: core.Pt(0),
		AlignItems: core.AlignCenter, Gap: 2}, Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node { return a.newThreadAgentMenu(cx, compact) }),
		core.Dynamic(func(cx *core.Scope) core.Node { return a.newThreadModelMenu(cx, compact) }),
		core.Dynamic(func(cx *core.Scope) core.Node { return a.newThreadModeMenu(cx, compact) }),
	}}
}

func (a *app) newThreadAgentMenu(cx *core.Scope, compact bool) core.Node {
	provider := a.newThreadProv.Get()
	label := "Agent"
	var items []core.Node
	for _, candidate := range a.providers.Get() {
		candidate := candidate
		if candidate.ID == provider {
			label = candidate.Name
		}
		itemLabel := candidate.Name
		if !candidate.Installed {
			itemLabel += " (missing)"
		}
		items = append(items, ui.MenuItem{Text: itemLabel, TestID: "new-thread-provider-" + candidate.ID,
			Checked:  func() bool { return a.newThreadProv.Get() == candidate.ID },
			Disabled: func() bool { return !candidate.Installed },
			OnSelect: func() {
				a.newThreadProv.Set(candidate.ID)
				modelID := ""
				for _, model := range a.allModels() {
					if model.Provider == candidate.ID {
						modelID = model.ID
						break
					}
				}
				a.newThreadModel.Set(modelID)
			}})
	}
	return ui.Menu{TestID: "new-thread-agent-menu", MaxHeight: 320,
		Trigger: newThreadControlTrigger(cx, compact, lucide.Bot, label, "Agent: "+label, "new-thread-agent"),
		Face: newThreadControlFace(compact, lucide.Bot, func() string {
			provider := a.newThreadProv.Get()
			for _, candidate := range a.providers.Get() {
				if candidate.ID == provider {
					return candidate.Name
				}
			}
			return "Agent"
		}, "Agent", "new-thread-agent"), Children: items}
}

func (a *app) newThreadModelMenu(cx *core.Scope, compact bool) core.Node {
	colors := ui.ColorsOf(cx)
	provider := a.newThreadProv.Get()
	modelLabel := "Model"
	var items []core.Node
	for _, model := range a.allModels() {
		if model.Provider != provider {
			continue
		}
		model := model
		if model.ID == a.newThreadModel.Get() {
			modelLabel = model.Name
		}
		items = append(items, ui.MenuItem{Text: model.Name, TestID: "new-thread-model-" + model.ID,
			Checked:  func() bool { return a.newThreadModel.Get() == model.ID },
			OnSelect: func() { a.newThreadModel.Set(model.ID) }})
	}
	if len(items) == 0 {
		return ui.Text("No models for this provider", 12, ui.Regular, colors.Faint)
	}
	return ui.Menu{TestID: "new-thread-model", MaxHeight: 320,
		Trigger: newThreadControlTrigger(cx, compact, lucide.Sparkles, modelLabel, "Model: "+modelLabel, "new-thread-model-trigger"),
		Face: newThreadControlFace(compact, lucide.Sparkles, func() string {
			provider := a.newThreadProv.Get()
			modelID := a.newThreadModel.Get()
			for _, model := range a.allModels() {
				if model.Provider == provider && model.ID == modelID {
					return model.Name
				}
			}
			return "Model"
		}, "Model", "new-thread-model-trigger"), Children: items}
}

func (a *app) newThreadModeMenu(cx *core.Scope, compact bool) core.Node {
	permission := a.newThreadPerm.Get()
	var items []core.Node
	for _, mode := range []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
		proto.PermissionAuto, proto.PermissionFullAccess} {
		mode := mode
		items = append(items, ui.MenuItem{Text: string(mode), TestID: "new-thread-perm-" + string(mode),
			Checked:  func() bool { return a.newThreadPerm.Get() == mode },
			OnSelect: func() { a.newThreadPerm.Set(mode) }})
	}
	label := string(permission)
	return ui.Menu{TestID: "new-thread-mode-menu", MaxHeight: 280,
		Trigger: newThreadControlTrigger(cx, compact, lucide.ShieldCheck, label, "Mode: "+label, "new-thread-mode"),
		Face: newThreadControlFace(compact, lucide.ShieldCheck, func() string {
			return string(a.newThreadPerm.Get())
		}, "Mode", "new-thread-mode"), Children: items}
}

func newThreadControlFace(compact bool, icon func() *core.SVG, value func() string,
	labelPrefix, testID string) core.Dynamic {
	return func(cx *core.Scope) core.Node {
		text := value()
		return newThreadControlTrigger(cx, compact, icon, text, labelPrefix+": "+text, testID)
	}
}

func newThreadControlTrigger(cx *core.Scope, compact bool, icon func() *core.SVG,
	text, label, testID string) ui.Button {
	if compact {
		return ui.Button{Icon: icon, Label: label, TestID: testID,
			Variant: ui.ButtonGhost, Size: controlSize(cx)}
	}
	return ui.Button{Text: text, Icon: icon, Label: label, TestID: testID,
		Variant: ui.ButtonGhost, Size: controlSize(cx)}
}

func (a *app) desktopNewThreadContext(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	projectID := a.newThreadProj.Get()
	projectLabel := "Choose project"
	gitProject := false
	var projectItems []core.Node
	for _, project := range a.projects.Get() {
		project := project
		if project.ID == projectID {
			projectLabel = project.Name
			gitProject = project.IsGit
		}
		projectItems = append(projectItems, ui.MenuItem{Text: project.Name,
			TestID:  "new-thread-project-choice-" + project.ID,
			Checked: func() bool { return a.newThreadProj.Get() == project.ID },
			OnSelect: func() {
				a.selectNewThreadProject(project.ID)
			}})
	}
	projectItems = append(projectItems, ui.MenuSeparator{},
		ui.MenuItem{Text: "New project", Icon: lucide.FolderPlus,
			TestID: "new-thread-new-project", OnSelect: a.openNewProjectForThread})
	children := []core.Node{
		ui.Menu{TestID: "new-thread-project-menu", MaxHeight: 320,
			Trigger: ui.Button{Text: projectLabel, Icon: lucide.Folder, Variant: ui.ButtonGhost,
				Size: controlSize(cx), Label: "Project: " + projectLabel},
			Children: projectItems},
	}
	if !gitProject {
		return core.View{TestID: "new-thread-context", Style: core.Style{Direction: core.Row,
			Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 8}, Children: children}
	}

	worktreeLabel := "Current checkout"
	worktreeVariant := ui.ButtonGhost
	if a.newThreadWT.Get() {
		worktreeLabel, worktreeVariant = "New worktree", ui.ButtonSecondary
	}
	branches := a.newThreadBranches.Get()
	options := make([]ui.Option[string], 0, len(branches))
	for _, branch := range branches {
		options = append(options, ui.Option[string]{Value: branch.Name, Label: branch.Name})
	}
	children = append(children,
		ui.Menu{TestID: "toggle-worktree", MaxHeight: 240,
			Trigger: ui.Button{Text: worktreeLabel, Icon: lucide.GitFork, Variant: worktreeVariant,
				Size: controlSize(cx), Label: "Worktree: " + worktreeLabel},
			Children: []core.Node{
				ui.MenuItem{Text: "Current checkout", TestID: "new-thread-worktree-current",
					Checked:  func() bool { return !a.newThreadWT.Get() },
					OnSelect: func() { a.newThreadWT.Set(false) }},
				ui.MenuItem{Text: "New worktree", TestID: "new-thread-worktree-new",
					Checked:  func() bool { return a.newThreadWT.Get() },
					OnSelect: func() { a.newThreadWT.Set(true) }},
			}},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(220)}, Children: []core.Node{
			ui.Select[string]{TestID: "new-thread-branch", Options: options,
				Value: a.newThreadBase.Get, OnChange: a.newThreadBase.Set,
				Placeholder: "Choose a branch", Searchable: true,
				SearchPlaceholder: "Search branches", Empty: "No branches found",
				Label: "Branch", Disabled: func() bool {
					return !a.newThreadWT.Get() || a.newThreadBranchesBusy.Get()
				}, Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}},
		}},
	)
	status := ""
	statusColor := colors.Faint
	switch {
	case a.newThreadBranchesBusy.Get():
		status = "Loading branches…"
	case a.newThreadBranchesErr.Get() != "":
		status, statusColor = a.newThreadBranchesErr.Get(), colors.Danger
	case len(options) == 0:
		status = "This project has no local branches."
	case !a.newThreadWT.Get():
		status = "Choose New worktree to use a branch as its starting point."
	default:
		status = "The new worktree branch is named from the thread title."
	}
	children = append(children, core.View{Style: core.Style{Direction: core.Row, FlexBasis: core.Pct(100),
		AlignItems: core.AlignCenter, Gap: 5}, Children: []core.Node{
		ui.Icon(lucide.GitBranch, 14, statusColor),
		ui.Text(status, 10.5, ui.Regular, statusColor),
	}})
	return core.View{TestID: "new-thread-context", Style: core.Style{Direction: core.Row, Wrap: core.Wrap,
		AlignItems: core.AlignCenter, Gap: 8}, Children: children}
}

func (a *app) desktopNewThreadError(cx *core.Scope) core.Node {
	message := a.connErr.Get()
	if message == "" {
		return core.Group{}
	}
	return ui.Text(message, 12, ui.Regular, ui.ColorsOf(cx).Danger)
}

func (a *app) settingsDialog(cx *core.Scope) core.Node {
	return ui.Dialog{Open: a.showSettings.Get, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			a.closeSettings()
		}
	}, NoCloseOnBackdropPress: compactSettings(cx),
		Title: "Settings", Description: "Manage environments and workspace preferences.",
		TestID: "settings-dialog", Style: settingsDialogStyle(cx), Children: []core.Node{core.Dynamic(a.settingsBody)},
		Footer: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if !settingsSectionSaves(a.settingsSection.Get()) {
				return ui.Button{Text: "Close", Variant: ui.ButtonGhost, TestID: "settings-close", OnPress: a.closeSettings}
			}
			target := a.settingsTarget()
			if target == nil || !target.settingsConnectionReady() {
				return ui.Button{Text: "Close", Variant: ui.ButtonGhost, TestID: "settings-close", OnPress: a.closeSettings}
			}
			return core.View{Style: core.Style{Direction: core.Row, Gap: 8, Width: core.Pct(100)}, Children: settingsFooter(cx, a, target)}
		})}}
}

func settingsSectionSaves(section string) bool {
	switch section {
	case "environment-defaults", "providers", "access":
		return true
	default:
		return false
	}
}

// settingsDialogStyle is wide on desktops and the sheet's width on phones.
func settingsDialogStyle(cx *core.Scope) core.Style {
	if compactSettings(cx) {
		return dialogWidth(cx, 780)
	}
	return core.Style{Width: core.Pt(780)}
}

// dialogWidth keeps a desktop dialog's comfortable width off phones,
// where the sheet already fills the screen.
func dialogWidth(cx *core.Scope, width core.Points) core.Style {
	if compactSettings(cx) {
		// ui.Dialog is a bottom sheet on phones. Keep its panel above the
		// software keyboard while it is visible.
		return core.Style{MarginEnv: core.EnvEdges{Bottom: core.EnvKeyboard}}
	}
	return core.Style{Width: core.Pt(width)}
}

// settingsFooter saves only the current environment's preference draft.
func settingsFooter(cx *core.Scope, a, target *app) []core.Node {
	save := ui.Button{Text: "Save", Icon: lucide.Check, Variant: ui.ButtonPrimary, TestID: "settings-save",
		Size:     controlSize(cx),
		Disabled: a.settingsSaveBusy.Get, OnPress: func() { a.saveTargetSettings(target) }}
	if compactSettings(cx) {
		if ui.Touch(cx) {
			return []core.Node{ui.Spacer{}, save}
		}
		return []core.Node{core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
			Gap: 8, Width: core.Pct(100)}, Children: []core.Node{
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "settings-close", OnPress: a.closeSettings},
			ui.Spacer{},
			save,
		}}}
	}
	return []core.Node{
		ui.Spacer{},
		ui.Button{Text: "Close", Variant: ui.ButtonGhost, TestID: "settings-close", OnPress: a.closeSettings},
		save,
	}
}

// settingsEnvironmentDefaults configures execution defaults on one machine.
func (a *app) settingsEnvironmentDefaults(cx *core.Scope) core.Node {
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
		providerButtons = append(providerButtons, ui.Button{Text: label, Variant: variant, Size: controlSize(cx),
			Disabled: func() bool { return !p.Installed }, TestID: "settings-provider-" + p.ID, OnPress: func() {
				update(func(s *proto.Settings) {
					s.DefaultProvider = p.ID
					s.DefaultModel = ""
					for _, m := range a.allModels() {
						if m.Provider == p.ID {
							s.DefaultModel = m.ID
							break
						}
					}
				})
			}})
	}
	var modelItems []core.Node
	modelLabel := "Choose model"
	for _, m := range a.allModels() {
		if m.Provider != provider {
			continue
		}
		m := m
		if m.ID == draft.DefaultModel {
			modelLabel = m.Name
		}
		modelItems = append(modelItems, ui.MenuItem{Text: m.Name, TestID: "settings-model-" + m.ID,
			Checked:  func() bool { return a.settingsDraft.Get().DefaultModel == m.ID },
			OnSelect: func() { update(func(s *proto.Settings) { s.DefaultModel = m.ID }) }})
	}
	var modelField core.Node = ui.Text("No models for this provider", 12, ui.Regular, colors.Faint)
	if len(modelItems) > 0 {
		modelField = ui.Menu{TestID: "settings-model",
			Trigger:   ui.Button{Text: modelLabel, Variant: ui.ButtonSecondary, Size: controlSize(cx)},
			MaxHeight: 320, Children: modelItems}
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
		permButtons = append(permButtons, ui.Button{Text: string(p), Variant: variant, Size: controlSize(cx),
			TestID: "settings-perm-" + string(p), OnPress: func() {
				update(func(s *proto.Settings) { s.DefaultPermission = string(p) })
			}})
	}
	var editorButtons []core.Node
	for _, choice := range []string{"auto", "zed", "code", "cursor", "finder"} {
		choice := choice
		variant := ui.ButtonSecondary
		if choice == a.editorChoice.Get() {
			variant = ui.ButtonPrimary
		}
		editorButtons = append(editorButtons, ui.Button{Text: choice, Variant: variant, Size: controlSize(cx),
			TestID: "settings-editor-" + choice, OnPress: func() { a.setEditor(choice) }})
	}
	fields := []core.Node{
		mobileInput{Scope: cx, Input: base.Input{Value: a.settingsTitle.Get, OnChange: a.settingsTitle.Set,
			Placeholder: "Environment name", Label: "Environment name"}},
		ui.Text("Default provider", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: providerButtons},
		ui.Text("Default model", 11, ui.Semibold, colors.Muted),
		modelField,
		ui.Text("Default permission", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: permButtons},
		ui.Text("Editor", 11, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: editorButtons},
		core.View{Style: core.Style{Direction: core.Row, Gap: 8, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Text{Content: "Server", Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.Text{Content: a.serverURL.Get(), MaxLines: 1,
				Style:     core.Style{FontSize: 12, FlexShrink: 1, MinWidth: core.Pt(0)},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}},
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 12, Padding: core.Edges{Right: 16}}, Children: fields}
}

// promptArgsDialog asks for the arguments of an MCP prompt.
func (a *app) promptArgsDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	defs := a.promptArgsDefs
	var rows []core.Node
	for index, arg := range defs {
		index, arg := index, arg
		label := arg.Name
		if arg.Required {
			label += " *"
		}
		placeholder := arg.Description
		if placeholder == "" {
			placeholder = "Value"
		}
		rows = append(rows, core.Dynamic(func(cx *core.Scope) core.Node {
			return mobileInput{Scope: cx, Input: base.Input{
				Value: func() string {
					state := a.promptArgValues.Get()
					if index < len(state.Values) {
						return state.Values[index]
					}
					return ""
				},
				OnChange: func(value string) {
					state := a.promptArgValues.GetUntracked()
					if index < len(state.Values) {
						state.Values[index] = value
						a.promptArgValues.Set(state)
					}
				},
				Placeholder: placeholder, Label: label, TestID: "prompt-arg-" + arg.Name,
			}}
		}))
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("This prompt takes no arguments.", 11.5, ui.Regular, colors.Faint))
	}
	name := a.promptName.Get()
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showPromptArgs.Get, OnOpenChange: closeDialog(a.showPromptArgs),
		Title: "Run " + name, Description: "Fill in the prompt's arguments; empty ones are skipped.",
		TestID: "prompt-args-dialog", Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: rows},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showPromptArgs.Set(false) }},
			ui.Button{Text: "Insert", Variant: ui.ButtonPrimary, TestID: "confirm-prompt-args",
				OnPress: a.submitPromptArgs},
		}}
}

// branchesDialog switches, creates or pulls the thread's branch.
func (a *app) branchesDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	var rows []core.Node
	for _, ref := range a.branches.Get() {
		if ref.Remote {
			continue
		}
		ref := ref
		label := ref.Name
		if ref.Current {
			label += "  ✓"
		}
		rows = append(rows, ui.Button{Text: label, Variant: ui.ButtonGhost, Size: controlSize(cx),
			TestID: "header-branch-" + ref.Name, Flex: 1, OnPress: func() {
				a.showBranches.Set(false)
				a.switchBranch(ref.Name)
			}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No local branches.", 11.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showBranches.Get, OnOpenChange: closeDialog(a.showBranches), Title: "Branches",
		Description: "Switch this thread's checkout, or make a new branch.", TestID: "branches-dialog",
		Children: []core.Node{core.Scroll{Style: core.Style{MaxHeight: core.Pt(300)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 3, Padding: core.Edges{Right: 12}}, Children: rows},
		}}}, Footer: []core.Node{
			ui.Button{Text: "Pull", Icon: lucide.RefreshCw, Variant: ui.ButtonGhost, TestID: "branch-pull",
				OnPress: func() { a.showBranches.Set(false); a.pullBranch() }},
			ui.Button{Text: "New branch…", Icon: lucide.GitBranch, Variant: ui.ButtonSecondary, TestID: "branch-new",
				OnPress: func() { a.showBranches.Set(false); a.openNewBranch() }},
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, OnPress: func() { a.showBranches.Set(false) }},
		}}
}

// restoreDialog confirms reverting tracked files to a checkpoint.
func (a *app) restoreDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showRestore.Get, OnOpenChange: closeDialog(a.showRestore), Title: "Restore files?",
		Description: "Tracked files go back to this run's checkpoint. Untracked files stay, and the current files are saved as a backup checkpoint first.",
		TestID:      "restore-dialog", Children: []core.Node{
			core.Text{Content: "This changes the working tree; commit anything you want to keep first.",
				Style: core.Style{FontSize: 11.5}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showRestore.Set(false) }},
			ui.Button{Text: "Restore files", Icon: lucide.RotateCcw, Variant: ui.ButtonPrimary,
				TestID: "confirm-restore-run", OnPress: a.restoreRun},
		}}
}

// customModelDialog adds a model id a provider's CLI accepts.
func (a *app) customModelDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	provider := a.customModelProvider.Get()
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showCustomModel.Get, OnOpenChange: closeDialog(a.showCustomModel),
		Title: "Add model to " + provider, Description: "Use the model id the CLI expects.",
		TestID: "custom-model-dialog", Style: dialogWidth(cx, 520), Children: []core.Node{
			a.environmentContext(cx, "custom-model-environment"),
			mobileInput{Scope: cx, Input: base.Input{Value: a.customModelID.Get, OnChange: a.customModelID.Set,
				Placeholder: "model-id", Label: "Model id", TestID: "custom-model-id"}},
			mobileInput{Scope: cx, Input: base.Input{Value: a.customModelName.Get, OnChange: a.customModelName.Set,
				Placeholder: "Display name (optional)", Label: "Model name", TestID: "custom-model-name"}},
			core.Show{When: func() bool { return a.customModelErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return core.Text{Content: a.customModelErr.Get(), MaxLines: 2, Style: core.Style{FontSize: 11.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }}
			}},
		}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showCustomModel.Set(false) }},
			ui.Button{Text: "Add model", Icon: lucide.Plus, Variant: ui.ButtonPrimary, TestID: "confirm-custom-model",
				OnPress: a.addCustomModel},
		}}
}

// devicesDialog picks a simulator to show.
func (a *app) devicesDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	devices := a.devices.Get()
	selected := a.deviceSelected.Get()
	var rows []core.Node
	for i, device := range devices {
		device, i := device, i
		label := device.Name
		if device.ID == selected {
			label += "  ✓"
		}
		rows = append(rows, base.Button{OnPress: func(core.PressEvent) {
			a.showDevices.Set(false)
			a.selectDevice(device)
		}, TestID: fmt.Sprintf("device-%s-%d", device.ID, i), Label: device.Name,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pct(100),
				Padding: core.Symmetric(5, 7), CornerRadius: 6, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
			}, Children: []core.Node{
				ui.Icon(lucide.MonitorSmartphone, 12, colors.Muted),
				core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: strings.ToLower(device.State), Style: core.Style{FontSize: 10},
					StyleFunc: func() core.Style {
						if device.State == "booted" {
							return core.Style{Color: colors.Up()}
						}
						return core.Style{Color: colors.Faint()}
					}},
			}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No simulators found.", 11.5, ui.Regular, colors.Faint))
	}
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showDevices.Get, OnOpenChange: closeDialog(a.showDevices), Title: "Devices",
		Description: "Pick a simulator to see its screen.", TestID: "devices-dialog",
		Children: []core.Node{core.Scroll{Style: core.Style{MaxHeight: core.Pt(300)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 2, Padding: core.Edges{Right: 12}}, Children: rows},
		}}}, Footer: []core.Node{
			ui.Button{Text: "Refresh", Icon: lucide.RefreshCw, Variant: ui.ButtonGhost, TestID: "devices-dialog-refresh",
				OnPress: a.loadDevices},
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, OnPress: func() { a.showDevices.Set(false) }},
		}}
}

// newBranchDialog asks for a branch to create and check out.
func (a *app) newBranchDialog(cx *core.Scope) core.Node {
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showBranch.Get, OnOpenChange: closeDialog(a.showBranch), Title: "New branch",
		Description: "Create a branch in this thread's checkout and switch to it.", TestID: "new-branch-dialog",
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			return mobileInput{Scope: cx, Input: base.Input{Value: a.branchName.Get, OnChange: a.branchName.Set,
				Placeholder: "t3go/<slug>", Label: "Branch name"}}
		})}, Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: func() { a.showBranch.Set(false) }},
			ui.Button{Text: "Create branch", Icon: lucide.GitBranch, Variant: ui.ButtonPrimary, TestID: "confirm-new-branch",
				OnPress: a.createBranch},
		}}
}

func (a *app) diffDialog(cx *core.Scope) core.Node {
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showDiff.Get, OnOpenChange: closeDialog(a.showDiff), Title: "Changes",
		Description: "Commit, push, and open a pull request for this thread's checkout.", TestID: "diff-dialog",
		Children: []core.Node{core.Dynamic(a.diffBody)},
		Footer: []core.Node{
			ui.Button{Text: "Close", Variant: ui.ButtonSecondary, OnPress: func() { a.showDiff.Set(false) }},
		}}
}

// diffBody is the Changes dialog's status, source control actions and
// diff, rebuilt on its own so typing a commit message keeps the dialog
// still.
func (a *app) diffBody(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	git := a.detail.Get().Git
	var status core.Node
	switch {
	case git == nil:
		status = ui.Text("This thread is not in a git repository.", 12, ui.Regular, colors.Faint)
	case git.Error != "":
		status = ui.Text(git.Error, 12, ui.Regular, colors.Danger)
	default:
		branch := git.Branch
		if branch == "" {
			branch = "detached HEAD"
		}
		var detail []string
		if git.Dirty {
			detail = append(detail, "uncommitted changes")
		}
		if git.Ahead > 0 {
			detail = append(detail, fmt.Sprintf("%d ahead", git.Ahead))
		}
		if git.Behind > 0 {
			detail = append(detail, fmt.Sprintf("%d behind", git.Behind))
		}
		var branchField core.Node = ui.Text(branch, 12, ui.Semibold, colors.Text)
		if refs := a.branches.Get(); len(refs) > 1 {
			var items []core.Node
			for _, ref := range refs {
				ref := ref
				items = append(items, ui.MenuItem{Text: ref.Name, TestID: "git-branch-" + ref.Name,
					Checked:  func() bool { return ref.Current },
					OnSelect: func() { a.switchBranch(ref.Name) }})
			}
			branchField = ui.Menu{TestID: "git-branch",
				Trigger:   ui.Button{Text: branch, Icon: lucide.GitBranch, Variant: ui.ButtonGhost, Size: controlSize(cx)},
				MaxHeight: 320, Children: items}
		}
		line := []core.Node{branchField}
		if len(detail) > 0 {
			line = append(line, core.Text{Content: strings.Join(detail, " · "), MaxLines: 1,
				Style:     core.Style{FontSize: 11.5, FlexShrink: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		}
		status = core.View{Style: core.Style{Direction: core.Column, MinWidth: core.Pt(0), Gap: 1}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, MinWidth: core.Pt(0)}, Children: line},
			core.Text{Content: git.Root, MaxLines: 1, Style: core.Style{FontSize: 11},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}}
	}
	buttons := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
		ui.Button{Text: "Commit", Icon: lucide.Check, Variant: ui.ButtonSecondary, Size: controlSize(cx),
			TestID: "git-commit", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitCommit) }},
		ui.Button{Text: "Push", Icon: lucide.ArrowUp, Variant: ui.ButtonSecondary, Size: controlSize(cx),
			TestID: "git-push", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitPush) }},
		ui.Button{Text: "Commit & push", Icon: lucide.ArrowUp, Variant: ui.ButtonSecondary, Size: controlSize(cx),
			TestID: "git-commit-push", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitCommitPush) }},
		ui.Button{Text: "Create PR", Icon: lucide.GitPullRequest, Variant: ui.ButtonPrimary, Size: controlSize(cx),
			TestID: "git-pr", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitCommitPR) }},
		core.Show{When: a.gitBusy.Get, Then: func(*core.Scope) core.Node { return ui.Spinner{Size: 14, Label: "Running git action"} }},
	}}
	fields := []core.Node{
		status,
		mobileInput{Scope: cx, Input: base.Input{Value: a.gitMessage.Get, OnChange: a.gitMessage.Set,
			Placeholder: "Commit message (defaults to an update)", Label: "Commit message"}},
		buttons,
	}
	if note := a.gitNote.Get(); note != "" {
		fields = append(fields, core.Text{Content: note, Style: core.Style{FontSize: 11.5, LineHeight: 16},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }})
	}
	if pr := a.gitPR.Get(); pr != "" {
		fields = append(fields, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.Text{Content: pr, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Accent()} }},
			ui.Button{Text: "Open", Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "git-open-pr",
				OnPress: a.openPR},
		}})
	}
	fields = append(fields, core.View{Style: core.Style{Height: core.Pt(340), MinHeight: core.Pt(180),
		Background: colors.Inset(), CornerRadius: 8, Overflow: core.Hidden},
		Children: []core.Node{a.diffNode(cx, a.diffText.Get())}})
	return core.View{Style: core.Style{Direction: core.Column, Gap: 10, MinWidth: core.Pt(0)}, Children: fields}
}

// openPR opens the pull request a git action made.
func (a *app) openPR() {
	if pr := a.gitPR.GetUntracked(); pr != "" {
		a.cx.OpenURL(pr, nil)
	}
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
			line.Children = append(line.Children,
				ui.Button{Text: "Diff", Variant: ui.ButtonGhost, Size: controlSize(cx),
					TestID: "run-diff-" + r.ID, OnPress: func() { a.openRunDiff(r.ID) }},
				ui.Button{Text: "Restore", Variant: ui.ButtonGhost, Size: controlSize(cx),
					TestID: "run-restore-" + r.ID, OnPress: func() { a.askRestoreRun(r.ID) }})
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
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: a.showRuns.Get, OnOpenChange: closeDialog(a.showRuns), Title: "History",
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
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx),
		Open: func() bool { return target.Kind != "" },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				a.cancelRename()
			}
		},
		Title:       title,
		Description: "The new name is saved for everyone connected to this environment.",
		Children: []core.Node{
			mobileInput{Scope: cx, Input: base.Input{Value: a.renameName.Get, OnChange: a.renameName.Set, Placeholder: label, Label: label,
				SubmitBehavior: core.SubmitBehaviorSubmit, OnSubmit: func(string) { a.confirmRename() }}},
		},
		Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, OnPress: a.cancelRename},
			ui.Button{Text: "Save", Icon: lucide.Check, Variant: ui.ButtonPrimary, TestID: "confirm-rename", OnPress: a.confirmRename},
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
