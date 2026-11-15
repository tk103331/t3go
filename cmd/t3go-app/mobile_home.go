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
	"github.com/tk/t3go/internal/proto"
)

type mobileThread struct {
	session *app
	thread  proto.Thread
	project proto.Project
	pinned  bool
}

// Phone threads form one inbox across machines. Project grouping remains
// available under Projects & Git, along with all existing project actions.
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
			if query != "" && !threadMatches(thread, query) &&
				!strings.Contains(strings.ToLower(project.Name+" "+session.envName()), query) {
				continue
			}
			rows = append(rows, mobileThread{session: session, thread: thread, project: project, pinned: pinned[thread.ID]})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].thread.UpdatedAt.After(rows[j].thread.UpdatedAt)
	})
	return rows
}

func (a *app) mobileHome(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "mobile-home", Style: core.Style{FlexGrow: 1, FlexShrink: 1,
		MinHeight: core.Pt(0), Overflow: core.Hidden, Gap: 8}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
			FlexShrink: 0, Padding: core.Symmetric(10, 14), Gap: 4}, Children: []core.Node{
			brandWordmark(cx, 26), ui.Spacer{},
			ui.IconButton{Icon: lucide.FolderPlus, Label: "New project", Size: 44, IconSize: 23,
				TestID: "new-project", OnPress: a.openNewProject},
			ui.IconButton{Icon: lucide.Search, Label: "Search", Size: 44, IconSize: 24,
				TestID: "open-palette", OnPress: a.openPalette},
			core.Dynamic(func(cx *core.Scope) core.Node {
				return ui.Menu{TestID: "mobile-filter",
					Trigger: ui.Button{Icon: lucide.ListFilter, Label: "Filter threads", Side: 44, IconSize: 24, Variant: ui.ButtonGhost},
					Children: []core.Node{
						ui.MenuItem{Text: a.archivedLabel(), Icon: lucide.Archive, TestID: "toggle-archived", OnSelect: a.toggleShowArchived},
						ui.MenuItem{Text: "Manage environments", Icon: lucide.Network, OnSelect: func() {
							a.openSettings()
							a.setSettingsSection("environments")
						}},
					}}
			}),
		}},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Overflow: core.Hidden,
			CornerRadius: 24, Background: colors.Surface()}, Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				if a.mobilePage.Get() == "projects" {
					return core.View{Style: ui.FillRest, Children: []core.Node{
						core.View{Style: core.Style{Padding: core.Symmetric(10, 18)}, Children: []core.Node{
							ui.Text("Projects & Git", 18, ui.Semibold, colors.Text),
						}},
						core.Dynamic(a.sidebarProjects),
					}}
				}
				return core.Scroll{TestID: "mobile-thread-scroll", Style: ui.FillRest,
					Children: []core.Node{core.Dynamic(a.mobileThreadList)}}
			}),
		}},
		a.mobileNavigation(cx),
	}}
}

func (a *app) sidebarProjects(cx *core.Scope) core.Node {
	return scopedSidebar{Sections: func() []ui.SidebarSection { return a.sidebarSections(cx) },
		Selected: func() string { return a.threadKey(a.selected.Get()) }, OnSelect: a.selectSidebarThread}
}

func (a *app) mobileThreadList(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	rows := a.mobileThreads()
	var pinned, working, settled []core.Node
	for _, row := range rows {
		node := a.mobileThreadRow(cx, row)
		switch {
		case row.pinned:
			pinned = append(pinned, node)
		case row.thread.Status == proto.ThreadRunning:
			working = append(working, node)
		default:
			settled = append(settled, node)
		}
	}
	var children []core.Node
	for _, group := range []struct {
		title string
		rows  []core.Node
	}{{"Pinned", pinned}, {"Working", working}, {"Settled", settled}} {
		if len(group.rows) == 0 {
			continue
		}
		children = append(children, core.View{TestID: "mobile-group-" + strings.ToLower(group.title),
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
				Padding: core.Edges{Top: 16, Right: 18, Bottom: 6, Left: 18}},
			Children: []core.Node{ui.Text(group.title, 12, ui.Medium, colors.Muted),
				core.View{Style: core.Style{Height: core.Pt(1), FlexGrow: 1, Background: colors.Border()}}}})
		children = append(children, group.rows...)
	}
	if len(rows) == 0 {
		message := "Start a thread in one of your projects, or add a project."
		if a.sidebarQuery.Get() != "" {
			message = "No threads match your search."
		}
		children = append(children, core.View{Style: core.Style{Padding: core.All(24), Gap: 10}, Children: []core.Node{
			ui.Text("Your conversations", 18, ui.Semibold, colors.Text),
			ui.Text(message, 14, ui.Regular, colors.Muted),
			ui.Button{Text: "Projects & Git", Icon: lucide.FolderOpen, Variant: ui.ButtonSecondary,
				OnPress: func() { a.mobilePage.Set("projects") }},
		}})
	}
	return core.View{Style: core.Style{Padding: core.Edges{Top: 6, Bottom: 18}}, Children: children}
}

func (a *app) mobileThreadRow(cx *core.Scope, row mobileThread) core.Node {
	colors := ui.ColorsOf(cx)
	session, t := row.session, row.thread
	id := session.sidebarID(t.ID)
	title := t.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled thread"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(row.project.Name))
	tints := []core.Color{core.RGB(0x7563d6), core.RGB(0x178eb0), core.RGB(0xba59b5), core.RGB(0x409478)}
	tint := tints[int(h.Sum32())%len(tints)]
	var status core.Node = ui.Icon(lucide.Check, 14, colors.Faint)
	switch {
	case t.Status == proto.ThreadRunning:
		status = ui.Spinner{Size: 15, Label: "Running"}
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
		subtitle += " · Draft"
	}
	titleColor := colors.Text
	if t.Archived {
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
	pin, archive := "Pin thread", "Archive thread"
	if row.pinned {
		pin = "Unpin thread"
	}
	if t.Archived {
		archive = "Unarchive thread"
	}
	return ui.ContextMenu{Trigger: content, Children: []core.Node{
		ui.MenuItem{Text: pin, TestID: "pin-thread-" + id, OnSelect: func() { session.togglePin(t) }},
		ui.MenuItem{Text: "Rename thread…", TestID: "rename-thread-" + id, OnSelect: session.inEnvironment(func() { session.askRenameThread(t) })},
		ui.MenuItem{Text: archive, TestID: "archive-thread-" + id, OnSelect: func() { session.setArchived(t, !t.Archived) }},
		ui.MenuItem{Text: "Delete thread", Variant: ui.MenuItemDanger, TestID: "delete-thread-" + id,
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
			Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(44), Height: core.Pt(48),
				CornerRadius: 24, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
			StyleFunc: func() core.Style {
				return core.Style{Selected: &core.Style{Background: ui.Alpha(colors.Text(), 0.1)}, Focus: ui.FocusRing(cx, -2)}
			}, Children: []core.Node{ui.Icon(icon, 24, colors.Text)}}
	}
	return core.View{TestID: "mobile-navigation", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
		FlexShrink: 0, Gap: 10, Padding: core.Edges{Top: 4, Right: 14, Bottom: 10, Left: 14}},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, FlexGrow: 1, MinWidth: core.Pt(0), Padding: core.All(4),
				CornerRadius: 30, BorderWidth: 1, BorderColor: colors.Border(), Background: colors.Surface()}, Children: []core.Node{
				tab(lucide.MessageSquare, "Threads", "mobile-tab-threads", func() bool { return a.mobilePage.Get() == "threads" },
					func() { a.mobilePage.Set("threads") }),
				tab(lucide.GitBranch, "Projects & Git", "mobile-tab-projects", func() bool { return a.mobilePage.Get() == "projects" },
					func() { a.mobilePage.Set("projects") }),
				tab(lucide.ChartColumn, "Usage", "mobile-tab-usage", func() bool {
					return a.showSettings.Get() && a.settingsSection.Get() == "usage"
				}, func() { a.openSettings(); a.setSettingsSection("usage") }),
				tab(lucide.Settings, "Settings", "settings", a.showSettings.Get, a.openSettings),
			}},
			base.Button{Label: "New thread", TestID: "new-thread", OnPress: func(core.PressEvent) { a.openNewThread() },
				Style: core.Style{Width: core.Pt(56), Height: core.Pt(56), FlexShrink: 0, CornerRadius: 18,
					AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
				StyleFunc: func() core.Style { return core.Style{Background: colors.Accent(), Focus: ui.FocusRing(cx, 2)} },
				Children:  []core.Node{ui.Icon(lucide.SquarePen, 26, colors.OnAccent)}},
		}}
}
