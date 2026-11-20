package app

import (
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// inboxThread is one thread row of the sidebar, carrying its environment
// session, since one inbox shows every connected environment.
type inboxThread struct {
	owner   *Session
	thread  proto.Thread
	project string
	pinned  bool
}

type inboxProject struct {
	owner   *Session
	project proto.Project
}

// sidebar is the client's left pane: the search field, one inbox of
// every environment's threads grouped by state, its projects, and the
// footer's actions.
func sidebar(c *ui.Context, s *Session) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Row(c).Padding(6, 10, 2, 10).Children(func() {
			ui.SearchField(c, &s.sidebarQuery).Label(i18n.T("Search threads")).
				Placeholder(i18n.T("Search threads")).Grow(1)
		})
		ui.Box(c).Grow(1).FillWidth().ClipY().Children(func() {
			s.sidebarInbox(c)
		})
		s.sidebarFooter(c)
	})
}

// sidebarInbox is the thread list itself, grouped the way the old client
// grouped it: recent first, then Working, then Settled, then Projects.
func (s *Session) sidebarInbox(c *ui.Context) {
	t := c.Theme()
	query := strings.ToLower(strings.TrimSpace(s.sidebarQuery))
	showArchived := s.showArchived
	sessions := []*Session{s}
	if s.manager != nil {
		sessions = nil
		for _, p := range s.manager.profiles {
			if session := s.manager.sessions[p.ID]; session != nil {
				sessions = append(sessions, session)
			}
		}
	}

	var recent, working, settled []inboxThread
	var projects []inboxProject
	for _, session := range sessions {
		projectNames := make(map[string]string)
		for _, project := range session.projects {
			projectNames[project.ID] = project.Name
			projects = append(projects, inboxProject{owner: session, project: project})
		}
		for _, thread := range session.threads {
			if !showArchived && thread.Archived {
				continue
			}
			if query != "" && !threadMatches(thread, query) {
				continue
			}
			entry := inboxThread{
				owner: session, thread: thread, project: projectNames[thread.ProjectID],
				pinned: session.pinned[thread.ID],
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

	type section = sidebarSection
	var sections []section
	toRows := func(threads []inboxThread) []inboxRow {
		rows := make([]inboxRow, 0, len(threads))
		for i := range threads {
			rows = append(rows, inboxRow{thread: &threads[i], owner: threads[i].owner})
		}
		return rows
	}
	if rows := toRows(recent); len(rows) > 0 {
		sections = append(sections, section{rows: rows})
	}
	for _, group := range []struct {
		name    string
		threads []inboxThread
	}{
		{name: i18n.T("Working"), threads: working},
		{name: "Settled", threads: settled},
	} {
		rows := toRows(group.threads)
		if len(rows) == 0 {
			continue
		}
		sections = append(sections, section{
			title: group.name, rows: rows, status: strings.ToLower(group.name),
		})
	}
	if len(projects) > 0 && query == "" {
		rows := make([]inboxRow, 0, len(projects))
		for _, entry := range projects {
			rows = append(rows, inboxRow{project: &entry, owner: entry.owner})
		}
		sections = append(sections, section{
			title: i18n.T("Projects"), rows: rows, status: "projects",
		})
	}
	if len(sections) == 0 && query != "" {
		ui.Column(c).FillWidth().Padding(12, 10).Gap(4).Children(func() {
			text(c, i18n.T("Threads"), 12, t.TextMuted, 500)
			faint(c, i18n.T("No threads match your search."), 12)
		})
		return
	}

	// One virtualized list of rows; headers are rows of their own, pinned
	// by the list while their rows scroll under them.
	s.sidebarList.Key = func(i int) any { return s.sidebarKey(i) }
	s.sidebarList.Header = func(i int) bool { return i < len(s.sidebarRows) && s.sidebarRows[i].header }
	s.sidebarList.Label = func(i int) string { return s.sidebarLabel(i) }
	s.sidebarRows = s.buildSidebarRows(sections)
	ui.List(c, &s.sidebarList, len(s.sidebarRows), func(i int) {
		s.sidebarRow(c, i)
	}).Grow(1).FillWidth().Gap(1).Padding(4, 6).Justify(ui.Start)
}

// inboxRow is one line of the sidebar: a header, a thread, or a project.
// Threads and projects carry the session they belong to, since one inbox
// shows every connected environment.
type inboxRow struct {
	header  bool
	title   string
	status  string // for a collapsible header
	thread  *inboxThread
	project *inboxProject
	owner   *Session
}

// sidebarSection is one titled group of rows.
type sidebarSection struct {
	title  string
	rows   []inboxRow
	status string // collapsible status groups carry one
}

// buildSidebarRows flattens the sections, honouring their collapse state.
func (s *Session) buildSidebarRows(sections []sidebarSection) []inboxRow {
	rows := make([]inboxRow, 0, 32)
	for _, sec := range sections {
		if sec.title != "" {
			collapsed := false
			if sec.status != "" {
				collapsed = s.statusSectionCollapsed(sec.status)
			}
			rows = append(rows, inboxRow{header: true, title: sec.title, status: sec.status})
			if collapsed {
				continue
			}
		}
		rows = append(rows, sec.rows...)
	}
	return rows
}

// sidebarKey identifies a row, so its state follows its item.
func (s *Session) sidebarKey(i int) any {
	row := s.sidebarRows[i]
	switch {
	case row.header:
		return "header:" + row.title
	case row.thread != nil:
		return row.owner.threadKey(row.thread.thread.ID)
	case row.project != nil:
		return row.owner.sidebarID("project:" + row.project.project.ID)
	}
	return i
}

// sidebarLabel names a row for typing its first letters and for screen
// readers.
func (s *Session) sidebarLabel(i int) string {
	row := s.sidebarRows[i]
	switch {
	case row.header:
		return row.title
	case row.thread != nil:
		return row.thread.thread.Title
	case row.project != nil:
		return row.project.project.Name
	}
	return ""
}

// sidebarRow draws one row of the inbox.
func (s *Session) sidebarRow(c *ui.Context, i int) {
	t := c.Theme()
	row := s.sidebarRows[i]
	switch {
	case row.header:
		head := ui.Row(c).AlignItems(ui.Center).Gap(6).FillWidth().
			Padding(6, 4).Background(t.Background)
		head.Children(func() {
			arrow := ui.Icon(c, lucide.ChevronDown()).FontSize(11).TextColor(t.TextMuted)
			if row.status != "" && s.statusSectionCollapsed(row.status) {
				arrow.Rotate(-90)
			}
			text(c, row.title, 11, t.TextMuted, 600)
			if n := s.statusRowCount(row.status); n > 0 {
				faint(c, strconv.Itoa(n), 10.5)
			}
			ui.Spacer(c)
		})
		if row.status != "" && head.RowClicked() {
			s.setStatusSectionCollapsed(row.status, !s.statusSectionCollapsed(row.status))
		}
	case row.thread != nil:
		s.threadRow(c, *row.thread)
	case row.project != nil:
		s.projectRow(c, *row.project)
	}
}

// statusRowCount counts the rows a collapsible header folds.
func (s *Session) statusRowCount(status string) int {
	n := 0
	for _, row := range s.sidebarRows {
		if row.header {
			continue
		}
		switch {
		case status == "working" && row.thread != nil && row.thread.thread.Status == proto.ThreadRunning:
			n++
		case status == "settled" && row.thread != nil && row.thread.thread.Settled:
			n++
		case status == "projects" && row.project != nil:
			n++
		}
	}
	return n
}

// threadRow is one thread's line: its state, title, the last prompt and
// when it changed.
func (s *Session) threadRow(c *ui.Context, entry inboxThread) {
	t := c.Theme()
	thread := entry.thread
	owner := entry.owner
	title := thread.Title
	if strings.TrimSpace(title) == "" {
		title = i18n.T("Untitled thread")
	}
	titleColor := t.Text
	if thread.Archived || thread.Settled {
		titleColor = t.TextMuted
	}
	row := ui.Row(c).AlignItems(ui.Center).Gap(6).FillWidth().MinWidth(0).
		Padding(4, 8).Radius(7)
	row.Children(func() {
		ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).MinWidth(0).Children(func() {
				switch {
				case thread.Status == proto.ThreadRunning:
					ui.Spinner(c).Size(11, 11).Label(i18n.T("Running"))
				case thread.Status == proto.ThreadFailed:
					ui.Icon(c, lucide.CircleAlert()).FontSize(12).TextColor(t.Danger)
				case owner.threadUnread(thread):
					ui.Box(c).Size(6, 6).Radius(3).Background(t.Accent)
				case thread.Archived:
					ui.Icon(c, lucide.Archive()).FontSize(11).TextColor(t.TextMuted)
				}
				if owner.threadPinned(thread.ID) {
					ui.Icon(c, lucide.Pin()).FontSize(10).TextColor(t.TextMuted)
				}
				ui.Text(c, title).FontSize(12.5).FontWeight(600).TextColor(titleColor).
					SingleLine().Grow(1).MinWidth(0)
				faint(c, relativeTime(thread.UpdatedAt), 10.5)
			})
			preview, previewColor := "", t.TextMuted
			draft := owner.draftFor(thread.ID)
			if draft != "" {
				preview = i18n.T("Draft: ") + oneLine(draft, 72)
				previewColor = t.Accent
			} else {
				preview = oneLine(thread.Summary, 80)
				if preview == "" {
					preview = thread.Provider
				}
			}
			context := strings.TrimSpace(entry.project)
			if name := strings.TrimSpace(owner.envName()); name != "" {
				if context != "" {
					context += " · "
				}
				context += name
			}
			if !owner.settingsConnectionReady() {
				context += " · " + owner.connectionLabel()
			}
			if context != "" && draft == "" {
				if preview == "" {
					preview = context
				} else {
					preview = context + " · " + preview
				}
			}
			if strings.TrimSpace(preview) != "" {
				ui.Row(c).AlignItems(ui.Center).Gap(4).MinWidth(0).Children(func() {
					switch {
					case draft != "":
						ui.Icon(c, lucide.SquarePen()).FontSize(10).TextColor(t.Accent)
					case thread.WorktreePath != "":
						ui.Icon(c, lucide.GitBranch()).FontSize(10).TextColor(t.TextMuted)
					}
					ui.Text(c, preview).FontSize(11).TextColor(previewColor).
						SingleLine().Grow(1).MinWidth(0)
				})
			}
		})
	})
	if row.RowClicked() {
		s.selectSidebarThread(owner.threadKey(thread.ID))
	}
	row.ContextMenu(func(m *ui.Menu) {
		owner.threadMenu(m, thread)
	})
}

// threadMenu is a thread row's actions.
func (s *Session) threadMenu(m *ui.Menu, thread proto.Thread) {
	owner := s
	archiveText := i18n.T("Archive thread")
	if thread.Archived {
		archiveText = i18n.T("Unarchive thread")
	}
	pinText := i18n.T("Pin thread")
	if owner.threadPinned(thread.ID) {
		pinText = i18n.T("Unpin thread")
	}
	if m.Item(pinText).Chosen() {
		owner.togglePin(thread)
	}
	if m.Item(i18n.T("Rename thread…")).Chosen() {
		owner.inEnvironment(func() { owner.askRenameThread(thread) })()
	}
	if m.Item(archiveText).Chosen() {
		owner.setArchived(thread, !thread.Archived)
	}
	if m.Item(i18n.T("Delete thread")).Danger().Chosen() {
		owner.inEnvironment(func() { owner.askDeleteThread(thread) })()
	}
}

// projectRow is one project's line: its name, its environment, and the
// actions that make a thread inside it.
func (s *Session) projectRow(c *ui.Context, entry inboxProject) {
	t := c.Theme()
	owner, project := entry.owner, entry.project
	row := ui.Row(c).AlignItems(ui.Center).Gap(7).FillWidth().MinWidth(0).
		Padding(4, 8).Radius(7)
	row.Children(func() {
		ui.Icon(c, lucide.Folder()).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, project.Name).FontSize(12.5).FontWeight(500).TextColor(t.Text).
			SingleLine().MinWidth(0)
		ui.Text(c, projectEnvironmentLabel(owner)).FontSize(10).TextColor(t.TextMuted).
			SingleLine().MinWidth(0)
		ui.Spacer(c)
		ui.Button(c, "⋯").Label(i18n.T("Project actions")).PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(i18n.T("New thread…")).Chosen() {
				owner.inEnvironment(func() { owner.openNewThreadIn(project.ID) })()
			}
			if m.Item(i18n.T("Rename project…")).Chosen() {
				owner.inEnvironment(func() { owner.askRenameProject(project) })()
			}
			if m.Item(i18n.T("Delete project")).Danger().Chosen() {
				owner.inEnvironment(func() { owner.askDeleteProject(project) })()
			}
		})
	})
	if row.RowClicked() {
		s.selectSidebarThread(owner.sidebarID("project:" + project.ID))
	}
}

// projectEnvironmentLabel names the project's environment and how it is
// reached.
func projectEnvironmentLabel(owner *Session) string {
	label := owner.envName()
	if owner.settingsConnectionReady() {
		return label
	}
	return label + " · " + owner.connectionLabel()
}

// sidebarFooter is the row of actions under the inbox.
func (s *Session) sidebarFooter(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(4).WidthPercent(100).Padding(6, 4).Children(func() {
		iconBtn(c, lucide.FolderPlus(), i18n.T("New project"), s.openNewProject)
		iconBtn(c, lucide.MessageSquarePlus(), i18n.T("New thread"), s.openNewThread)
		ui.Spacer(c)
		archived := lucide.Archive()
		label := "Archived"
		if s.showArchived {
			archived = lucide.ArchiveRestore()
			label = i18n.T("Hide archived")
		}
		iconBtn(c, archived, label, s.toggleShowArchived)
		iconBtn(c, lucide.Search(), i18n.T("Search"), s.openPalette)
		iconBtn(c, lucide.Settings(), i18n.T("Settings"), s.openSettings)
	})
}

// archivedLabel names the archived toggle's action.
func (s *Session) archivedLabel() string {
	if s.showArchived {
		return i18n.T("Hide archived")
	}
	return "Archived"
}
