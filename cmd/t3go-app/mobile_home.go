package main

import (
	"hash/fnv"
	"sort"
	"strings"
	"unicode"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

type mobileThread struct {
	session *app
	thread  proto.Thread
	project proto.Project
	pinned  bool
}

// Phone threads form one inbox across machines. Project grouping remains
// available under Projects, along with all existing project actions.
func (a *app) mobileThreads() []mobileThread {
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
	query := strings.ToLower(strings.TrimSpace(a.sidebarQuery.Get()))
	archived := a.showArchived.Get()
	var rows []mobileThread
	for _, session := range sessions {
		projects := map[string]proto.Project{}
		for _, p := range session.projects.Get() {
			projects[p.ID] = p
		}
		pinned := session.pinned.Get()
		for _, thread := range session.threads.Get() {
			project := projects[thread.ProjectID]
			if thread.Archived && !archived {
				continue
			}
			if query != "" && !threadMatches(thread, query) {
				continue
			}
			rows = append(rows, mobileThread{session: session, thread: thread, project: project, pinned: pinned[thread.ID]})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].pinned != rows[j].pinned {
			return rows[i].pinned
		}
		return rows[i].thread.UpdatedAt.After(rows[j].thread.UpdatedAt)
	})
	return rows
}

func (a *app) mobileHome(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "mobile-home", Style: core.Style{FlexGrow: 1, FlexShrink: 1,
		MinHeight: core.Pt(0), Overflow: core.Hidden, Gap: 8}, Children: []core.Node{
		core.Dynamic(a.mobileHeader),
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Overflow: core.Hidden,
			CornerRadius: 24, Background: colors.Surface()}, Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				switch a.mobilePage.Get() {
				case "settings":
					return a.mobileSettings(cx)
				case "usage":
					return core.Func(a.mobileUsage)
				case "prs":
					return core.Func(a.mobilePullRequests)
				case "projects":
					return core.View{Style: ui.FillRest, Children: []core.Node{
						core.Dynamic(a.sidebarProjects),
					}}
				}
				return core.Func(a.mobileThreadsPage)
			}),
		}},
		core.Dynamic(a.mobileNewThreadAction),
		core.Dynamic(a.mobileNavigation),
	}}
}

func (a *app) mobileHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	page := a.mobilePage.Get()
	if page == "threads" && a.mobileThreadSearch.Get() {
		return a.mobileThreadSearchHeader(cx)
	}
	var title core.Node = brandWordmark(cx, 26)
	switch page {
	case "settings":
		name := i18n.T("Settings")
		if detail := a.mobileSettingsDetail.Get(); detail != "" {
			if detail == "environment-defaults" {
				name = i18n.T("Environment configuration")
			}
			for _, item := range settingsNavItems() {
				if item.ID == detail {
					name = item.Label
				}
			}
		}
		title = ui.Text(name, 24, ui.Medium, colors.Text)
	case "usage":
		title = ui.Text(i18n.T("Usage"), 24, ui.Medium, colors.Text)
	case "projects":
		title = ui.Text(i18n.T("Projects"), 24, ui.Medium, colors.Text)
	}
	var children []core.Node
	if page == "settings" && a.mobileSettingsDetail.Get() != "" {
		children = append(children, ui.IconButton{Icon: lucide.ChevronLeft, Label: i18n.T("Back to settings"), Size: 44,
			TestID: "mobile-settings-back", OnPress: func() {
				if a.mobileSettingsDetail.GetUntracked() == "environment-defaults" {
					a.setSettingsSection(i18n.T("environments"))
					a.mobileSettingsDetail.Set(i18n.T("environments"))
				} else {
					a.mobileSettingsDetail.Set("")
				}
			}})
	}
	children = append(children, title, ui.Spacer{})
	if page == "threads" {
		children = append(children, ui.IconButton{Icon: lucide.Search, Label: i18n.T("Search threads"), Size: 44, IconSize: 24,
			TestID: "mobile-thread-search-open", OnPress: func() { a.mobileThreadSearch.Set(true) }})
	}
	if page == "projects" {
		children = append(children,
			ui.IconButton{Icon: lucide.FolderPlus, Label: i18n.T("New project"), Size: 44, IconSize: 23,
				TestID: "new-project", OnPress: a.openNewProject})
	} else if page == "prs" {
		children = append(children, ui.IconButton{Icon: lucide.Folder, Label: i18n.T("Projects"), Size: 44, IconSize: 24,
			TestID: "mobile-tab-projects", OnPress: func() { a.mobilePage.Set(i18n.T("projects")) }})
	}
	var filter core.Node
	if page == "usage" {
		filter = ui.IconButton{Icon: lucide.RefreshCw, Label: i18n.T("Refresh usage"), Size: 44, IconSize: 24,
			TestID: "usage-refresh", OnPress: a.loadMobileUsage}
	} else if page == "prs" {
		filter = ui.IconButton{Icon: lucide.RefreshCw, Label: i18n.T("Refresh pull requests"), Size: 44, IconSize: 24,
			TestID: "mobile-pr-refresh", Disabled: a.mobilePRBusy.Get, OnPress: a.loadMobilePullRequests}
	} else if page != "settings" {
		filter = core.Dynamic(func(cx *core.Scope) core.Node {
			return ui.Menu{TestID: "mobile-filter",
				Trigger: ui.Button{Icon: lucide.ListFilter, Label: i18n.T("Filter threads"), Side: 44, IconSize: 24, Variant: ui.ButtonGhost},
				Children: []core.Node{
					ui.MenuItem{Text: a.archivedLabel(), Icon: lucide.Archive, TestID: "toggle-archived", OnSelect: a.toggleShowArchived},
					ui.MenuItem{Text: i18n.T("Projects"), Icon: lucide.Folder, TestID: "mobile-tab-projects",
						OnSelect: func() { a.mobilePage.Set(i18n.T("projects")) }},
					ui.MenuItem{Text: i18n.T("Manage environments"), Icon: lucide.Network, OnSelect: func() {
						a.openSettings()
						a.openMobileSettingsSection(i18n.T("environments"))
					}},
				}}
		})
	}
	children = append(children, filter)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		FlexShrink: 0, Padding: core.Symmetric(10, 14), Gap: 4}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, MinWidth: core.Pt(0), Gap: 4},
			Children: children},
	}}
}

func (a *app) mobileThreadSearchHeader(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		FlexShrink: 0, Padding: core.Symmetric(10, 14), Gap: 6}, Children: []core.Node{
		ui.IconButton{Icon: lucide.ChevronLeft, Label: i18n.T("Close search"), Size: 44, IconSize: 24,
			TestID: "mobile-thread-search-back", OnPress: a.closeMobileThreadSearch},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			mobileInput{Scope: cx, TestID: "mobile-thread-search", Input: base.Input{
				Value: a.sidebarQuery.Get, OnChange: a.sidebarQuery.Set, AutoFocus: true,
				Placeholder: i18n.T("Search threads"), Label: i18n.T("Search threads")}},
		}},
		core.Show{When: func() bool { return strings.TrimSpace(a.sidebarQuery.Get()) != "" },
			Then: func(*core.Scope) core.Node {
				return ui.IconButton{Icon: lucide.X, Label: i18n.T("Clear thread search"), Size: 44, IconSize: 22,
					TestID: "clear-thread-search", OnPress: func() { a.sidebarQuery.Set("") }}
			}},
	}}
}

func (a *app) closeMobileThreadSearch() {
	a.sidebarQuery.Set("")
	a.mobileThreadSearch.Set(false)
}

func (a *app) mobileThreadsPage(cx *core.Scope) core.Node {
	return core.View{TestID: "mobile-threads-page", Style: core.Style{FlexGrow: 1, FlexShrink: 1,
		MinHeight: core.Pt(0)}, Children: []core.Node{
		core.Scroll{TestID: "mobile-thread-scroll", Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
			Children: []core.Node{core.Dynamic(a.mobileThreadList)}},
	}}
}

func (a *app) sidebarProjects(cx *core.Scope) core.Node {
	return scopedSidebar{Sections: func() []ui.SidebarSection { return a.sidebarSections(cx) },
		Selected: func() string { return a.threadKey(a.selected.Get()) }, OnSelect: a.selectSidebarThread}
}

func (a *app) mobileThreadList(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	rows := a.mobileThreads()
	var completed, working, settled []core.Node
	for _, row := range rows {
		node := a.mobileThreadRow(cx, row)
		switch {
		case row.thread.Status == proto.ThreadRunning:
			working = append(working, node)
		case row.thread.Settled:
			settled = append(settled, node)
		default:
			completed = append(completed, node)
		}
	}
	var children []core.Node
	for _, group := range []struct {
		title string
		rows  []core.Node
	}{{"Completed", completed}, {i18n.T("Working"), working}, {"Settled", settled}} {
		if len(group.rows) == 0 {
			continue
		}
		// Completed conversations appear at the top without a heading.
		// A completed turn never implies an explicit Settle.
		if group.title == "Completed" {
			children = append(children, core.View{TestID: "mobile-group-completed", Children: group.rows})
			continue
		}
		var heading core.Node = core.View{TestID: "mobile-group-" + strings.ToLower(group.title),
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
				Padding: core.Edges{Top: 16, Right: 18, Bottom: 6, Left: 18}},
			Children: []core.Node{ui.Text(group.title, 12, ui.Medium, colors.Muted),
				core.View{Style: core.Style{Height: core.Pt(1), FlexGrow: 1, Background: colors.Border()}}}}
		if group.title == "Settled" {
			icon := lucide.ChevronDown
			if !a.mobileSettledClosed.Get() {
				icon = lucide.ChevronUp
			}
			heading = base.Button{Label: i18n.T("Toggle settled threads"), TestID: "mobile-group-settled",
				Expanded: func() bool { return !a.mobileSettledClosed.Get() },
				OnPress: func(core.PressEvent) {
					a.mobileSettledClosed.Set(!a.mobileSettledClosed.GetUntracked())
				},
				Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinHeight: core.Pt(44),
					Padding: core.Symmetric(8, 18)}, Children: []core.Node{
					ui.Text(i18n.T("Settled (%d)", len(group.rows)), 12, ui.Medium, colors.Muted),
					core.View{Style: core.Style{Height: core.Pt(1), FlexGrow: 1, Background: colors.Border()}},
					ui.Icon(icon, 14, colors.Muted),
				}}
		}
		children = append(children, heading)
		if group.title == "Settled" && a.mobileSettledClosed.Get() {
			continue
		}
		children = append(children, group.rows...)
	}
	if len(rows) == 0 {
		message := i18n.T("Start a thread in one of your projects, or add a project.")
		if a.sidebarQuery.Get() != "" {
			message = i18n.T("No threads match your search.")
		}
		children = append(children, core.View{Style: core.Style{Padding: core.All(24), Gap: 10}, Children: []core.Node{
			ui.Text(i18n.T("Your conversations"), 18, ui.Semibold, colors.Text),
			ui.Text(message, 14, ui.Regular, colors.Muted),
			ui.Button{Text: i18n.T("Projects"), Icon: lucide.FolderOpen, Variant: ui.ButtonSecondary,
				OnPress: func() { a.mobilePage.Set(i18n.T("projects")) }},
		}})
	}
	return core.View{Style: core.Style{Padding: core.Edges{Top: 6, Bottom: 100}}, Children: children}
}

func (a *app) mobileThreadRow(cx *core.Scope, row mobileThread) core.Node {
	colors := ui.ColorsOf(cx)
	session, t := row.session, row.thread
	id := session.sidebarID(t.ID)
	title := t.Title
	if strings.TrimSpace(title) == "" {
		title = i18n.T("Untitled thread")
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(row.project.Name))
	tints := []core.Color{core.RGB(0x7563d6), core.RGB(0x178eb0), core.RGB(0xba59b5), core.RGB(0x409478)}
	tint := tints[int(h.Sum32())%len(tints)]
	var status core.Node = ui.Icon(lucide.Check, 14, colors.Faint)
	switch {
	case t.Status == proto.ThreadRunning:
		status = ui.Spinner{Size: 15, Label: i18n.T("Running")}
	case t.Status == proto.ThreadFailed:
		status = ui.Icon(lucide.CircleAlert, 16, colors.Danger)
	case t.Archived:
		status = ui.Icon(lucide.Archive, 15, colors.Faint)
	case row.pinned:
		status = ui.Icon(lucide.Pin, 14, colors.Muted)
	}
	subtitle := session.envName()
	if !session.settingsConnectionReady() {
		subtitle += " · " + session.connectionLabel()
	}
	if session.draftFor(t.ID) != "" {
		subtitle += i18n.T(" · Draft")
	}
	titleColor := colors.Text
	if t.Archived || t.Settled {
		titleColor = colors.Muted
	}
	content := base.Button{Label: title, TestID: "thread-" + id,
		OnPress: func(core.PressEvent) { session.inEnvironment(func() { session.selectThread(t.ID) })() },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignStart, Gap: 10, MinHeight: core.Pt(78),
			Padding: core.Symmetric(12, 18), MinWidth: core.Pt(0)},
		StyleFunc: func() core.Style {
			return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
		}, Children: []core.Node{
			core.View{Label: row.project.Name, Style: core.Style{Width: core.Pt(28), Height: core.Pt(28),
				FlexShrink: 0, CornerRadius: 8, Background: ui.Alpha(tint, 0.12), AlignItems: core.AlignCenter,
				JustifyContent: core.JustifyCenter}, Children: []core.Node{
				core.Text{Content: projectInitials(row.project.Name), Style: core.Style{FontSize: 11, FontWeight: ui.Bold, Color: tint}},
			}},
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 6}, Children: []core.Node{
				core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 16, FontWeight: ui.Medium},
					StyleFunc: func() core.Style { return core.Style{Color: titleColor()} }},
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5}, Children: []core.Node{
					ui.Icon(lucide.Laptop, 13, colors.Muted),
					core.Text{Content: subtitle, TestID: "mobile-thread-environment-" + id, MaxLines: 1,
						Style:     core.Style{FontSize: 12, FlexShrink: 1},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				}},
			}},
			core.View{Style: core.Style{AlignItems: core.AlignEnd, FlexShrink: 0, Gap: 8}, Children: []core.Node{
				ui.Text(relativeTime(t.UpdatedAt), 12, ui.Regular, colors.Muted), status,
			}},
		}}
	pin, archive := i18n.T("Pin thread"), i18n.T("Archive thread")
	if row.pinned {
		pin = i18n.T("Unpin thread")
	}
	if t.Archived {
		archive = i18n.T("Unarchive thread")
	}
	return ui.ContextMenu{Trigger: content, Children: []core.Node{
		ui.MenuItem{Text: pin, TestID: "pin-thread-" + id, OnSelect: func() { session.togglePin(t) }},
		session.settleMenuItem(t, id),
		ui.MenuItem{Text: i18n.T("Rename thread…"), TestID: "rename-thread-" + id, OnSelect: session.inEnvironment(func() { session.askRenameThread(t) })},
		ui.MenuItem{Text: archive, TestID: "archive-thread-" + id, OnSelect: func() { session.setArchived(t, !t.Archived) }},
		ui.MenuItem{Text: i18n.T("Delete thread"), Variant: ui.MenuItemDanger, TestID: "delete-thread-" + id,
			OnSelect: session.inEnvironment(func() { session.askDeleteThread(t) })},
	}}
}

func projectInitials(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var initials []rune
	for _, word := range words {
		initials = append(initials, []rune(word)[0])
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 1 {
		letters := []rune(words[0])
		if len(letters) > 1 {
			initials = append(initials, letters[1])
		}
	}
	if len(initials) == 0 {
		return "PR"
	}
	return strings.ToUpper(string(initials))
}

func (a *app) mobileNavigation(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	tab := func(icon func() *core.SVG, label, id string, selected func() bool, press func()) core.Node {
		return base.Button{Label: label, TestID: id, Selected: selected, OnPress: func(core.PressEvent) { press() },
			Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(44), Height: core.Pt(72),
				AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 5},
			StyleFunc: func() core.Style {
				return core.Style{Focus: ui.FocusRing(cx, -2)}
			}, Children: []core.Node{
				core.View{Style: core.Style{Width: core.Pt(56), Height: core.Pt(34), CornerRadius: 20,
					AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
					StyleFunc: func() core.Style {
						if selected() {
							return core.Style{Background: ui.Alpha(colors.Text(), 0.13)}
						}
						return core.Style{}
					}, Children: []core.Node{ui.Icon(icon, 26, colors.Text)}},
				ui.Text(label, 12, ui.Medium, colors.Text),
			}}
	}
	children := []core.Node{
		core.View{Style: core.Style{Direction: core.Row, FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			tab(lucide.MessageSquare, "Chats", "mobile-tab-threads", func() bool { return a.mobilePage.Get() == "threads" },
				func() { a.closeMobileThreadSearch(); a.mobilePage.Set("threads") }),
			tab(lucide.GitPullRequest, "Pulls", "mobile-tab-prs", func() bool { return a.mobilePage.Get() == "prs" },
				func() { a.closeMobileThreadSearch(); a.mobilePage.Set("prs"); a.loadMobilePullRequests() }),
			tab(lucide.ChartColumn, i18n.T("Usage"), "mobile-tab-usage", func() bool { return a.mobilePage.Get() == "usage" },
				func() { a.closeMobileThreadSearch(); a.mobilePage.Set("usage"); a.loadMobileUsage() }),
			tab(lucide.Settings, i18n.T("Settings"), "settings", func() bool { return a.mobilePage.Get() == "settings" }, a.openSettings),
		}},
	}
	return core.View{TestID: "mobile-navigation", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		FlexShrink: 0, Background: colors.Inset(), Padding: core.Symmetric(4, 0)}, Children: children}
}

func (a *app) mobileNewThreadAction(cx *core.Scope) core.Node {
	if a.mobilePage.Get() != "threads" {
		return core.Group{}
	}
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Height: core.Pt(72), Direction: core.Row, AlignItems: core.AlignCenter,
		JustifyContent: core.JustifyEnd, Padding: core.Edges{Right: 16}, FlexShrink: 0}, Children: []core.Node{
		base.Button{Label: i18n.T("New thread"), TestID: "new-thread", OnPress: func(core.PressEvent) { a.openNewThread() },
			Style: core.Style{Width: core.Pt(56), Height: core.Pt(56), CornerRadius: 18, AlignItems: core.AlignCenter,
				JustifyContent: core.JustifyCenter},
			StyleFunc: func() core.Style {
				return core.Style{Background: colors.Accent(), Focus: ui.FocusRing(cx, 2),
					Shadows: []core.BoxShadow{ui.Shadow(0, 3, 8, 0, ui.Black(0.18))}}
			}, Children: []core.Node{ui.Icon(lucide.SquarePen, 26, colors.OnAccent)}},
	}}
}
