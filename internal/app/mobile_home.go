package app

import (
	"hash/fnv"
	"sort"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// mobileThread is one row of the phone's inbox, carrying the session it
// belongs to, since one inbox shows every connected environment.
type mobileThread struct {
	session *Session
	thread  proto.Thread
	project proto.Project
	pinned  bool
}

// mobileThreads gathers the phone's threads across machines, pinned
// first, then by recency.
func (s *Session) mobileThreads() []mobileThread {
	sessions := []*Session{s}
	if s.manager != nil {
		sessions = nil
		for _, p := range s.manager.profiles {
			if session := s.manager.sessions[p.ID]; session != nil {
				sessions = append(sessions, session)
			}
		}
	}
	query := strings.ToLower(strings.TrimSpace(s.sidebarQuery))
	archived := s.showArchived
	var rows []mobileThread
	for _, session := range sessions {
		projects := map[string]proto.Project{}
		for _, p := range session.projects {
			projects[p.ID] = p
		}
		for _, thread := range session.threads {
			if thread.Archived && !archived {
				continue
			}
			if query != "" && !threadMatches(thread, query) {
				continue
			}
			rows = append(rows, mobileThread{
				session: session, thread: thread, project: projects[thread.ProjectID],
				pinned: session.pinned[thread.ID],
			})
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

// mobileHomeScreen is the phone's shell: the header, one page, the
// compose button and the bottom navigation.
func (s *Session) mobileHomeScreen(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Gap(8).Children(func() {
		s.mobileHomeHeader(c)
		ui.Box(c).Grow(1).FillWidth().ClipY().Radius(24).Background(t.Background).Children(func() {
			switch s.mobilePage {
			case "prs":
				s.mobilePullRequestsPage(c)
			case "usage":
				s.mobileUsagePage(c)
			case "settings":
				s.mobileSettingsPage(c)
			case "projects":
				s.mobileThreadsPage(c)
			default:
				s.mobileThreadsPage(c)
			}
		})
		s.mobileComposeButton(c)
		s.mobileBottomNavigation(c)
	})
}

// mobileHomeHeader is the phone's top bar: the page's title, the search,
// and the actions the page owns.
func (s *Session) mobileHomeHeader(c *ui.Context) {
	t := c.Theme()
	if s.mobilePage == "threads" && s.mobileThreadSearch {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(10, 14).Children(func() {
			iconBtn(c, lucide.ChevronLeft(), i18n.T("Close search"), s.closeMobileThreadSearch)
			field := ui.SearchField(c, &s.sidebarQuery).Label(i18n.T("Search threads")).
				Placeholder(i18n.T("Search threads")).AutoFocus().Grow(1)
			_ = field
		})
		return
	}
	var title string
	switch s.mobilePage {
	case "usage":
		title = i18n.T("Usage")
	case "prs":
		title = i18n.T("Pull requests")
	case "settings":
		title = i18n.T("Settings")
		if s.mobileSettingsDetail != "" {
			title = s.settingsSectionLabel()
		}
	case "projects":
		title = i18n.T("Projects")
	default:
		title = ""
	}
	ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(10, 14).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(24).FontWeight(500).TextColor(t.Text)
		} else {
			brandWordmark(c, 26)
		}
		ui.Spacer(c)
		if s.mobilePage == "threads" {
			iconBtn(c, lucide.Search(), i18n.T("Search threads"), func() {
				s.mobileThreadSearch = true
			})
		}
		if s.mobilePage == "settings" && s.mobileSettingsDetail != "" {
			iconBtn(c, lucide.ChevronLeft(), i18n.T("Back to settings"), func() {
				if s.mobileSettingsDetail == "environments" {
					s.setSettingsSection("environments")
				}
				s.mobileSettingsDetail = ""
			})
		}
		if s.mobilePage == "projects" {
			iconBtn(c, lucide.FolderPlus(), i18n.T("New project"), s.openNewProject)
		}
		if s.mobilePage == "usage" {
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh usage"), s.loadMobileUsage)
		}
		if s.mobilePage == "prs" {
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh pull requests"), s.loadMobilePullRequests)
		}
		if s.mobilePage != "settings" && s.mobilePage != "usage" && s.mobilePage != "prs" {
			ui.Button(c, "").Label(i18n.T("Filter threads")).Children(func() {
				ui.Icon(c, lucide.ListFilter()).FontSize(22)
			}).PopoverMenuButton(func(m *ui.Menu) {
				if m.Item(s.archivedLabel()).Chosen() {
					s.toggleShowArchived()
				}
				if m.Item(i18n.T("Projects")).Chosen() {
					s.mobilePage = "projects"
				}
				if m.Item(i18n.T("Manage environments")).Chosen() {
					s.openSettings()
					s.setSettingsSection("environments")
				}
			})
		}
	})
}

// mobileThreadsPage is the phone's inbox: Completed at the top without a
// heading, then Working, then the collapsible Settled group.
func (s *Session) mobileThreadsPage(c *ui.Context) {
	t := c.Theme()
	rows := s.mobileThreads()
	var completed, working, settled []mobileThread
	for _, row := range rows {
		switch {
		case row.thread.Status == proto.ThreadRunning:
			working = append(working, row)
		case row.thread.Settled:
			settled = append(settled, row)
		default:
			completed = append(completed, row)
		}
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(6, 0, 100, 0).Children(func() {
			if len(completed) > 0 {
				ui.Column(c).Children(func() {
					for _, row := range completed {
						s.mobileThreadRow(c, row)
					}
				})
			}
			if len(working) > 0 {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(16, 18, 6, 18).Children(func() {
					ui.Text(c, i18n.T("Working")).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
					ui.Box(c).Grow(1).Height(1).Background(t.Border)
				})
				for _, row := range working {
					s.mobileThreadRow(c, row)
				}
			}
			if len(settled) > 0 {
				head := ui.Row(c).AlignItems(ui.Center).Gap(10).MinHeight(44).Padding(8, 18).Children(func() {
					ui.Text(c, i18n.T("Settled (%d)", len(settled))).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
					ui.Box(c).Grow(1).Height(1).Background(t.Border)
					chevron := lucide.ChevronDown()
					if !s.mobileSettledClosed {
						chevron = lucide.ChevronUp()
					}
					ui.Icon(c, chevron).FontSize(14).TextColor(t.TextMuted)
				})
				if head.RowClicked() {
					s.mobileSettledClosed = !s.mobileSettledClosed
				}
				if !s.mobileSettledClosed {
					for _, row := range settled {
						s.mobileThreadRow(c, row)
					}
				}
			}
			if len(rows) == 0 {
				message := i18n.T("Start a thread in one of your projects, or add a project.")
				if s.sidebarQuery != "" {
					message = i18n.T("No threads match your search.")
				}
				ui.Column(c).Padding(24).Gap(10).Children(func() {
					ui.Text(c, i18n.T("Your conversations")).FontSize(18).FontWeight(600).TextColor(t.Text)
					ui.Text(c, message).FontSize(14).TextColor(t.TextMuted)
					ui.Button(c, i18n.T("Projects")).Children(func() {
						ui.Icon(c, lucide.FolderOpen()).FontSize(15)
						ui.Text(c, i18n.T("Projects"))
					}).OnClick(func() { s.mobilePage = "projects" })
				})
			}
		})
	})
}

// mobileThreadRow is one thread of the phone's inbox: the project's
// initials, the title, the machine it runs on, and its state.
func (s *Session) mobileThreadRow(c *ui.Context, row mobileThread) {
	t := c.Theme()
	session, thread := row.session, row.thread
	title := thread.Title
	if strings.TrimSpace(title) == "" {
		title = i18n.T("Untitled thread")
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(row.project.Name))
	tints := []ui.Color{ui.RGB(0x75, 0x63, 0xd6), ui.RGB(0x17, 0x8e, 0xb0), ui.RGB(0xba, 0x59, 0xb5), ui.RGB(0x40, 0x94, 0x78)}
	tint := tints[int(h.Sum32())%len(tints)]
	titleColor := t.Text
	if thread.Archived || thread.Settled {
		titleColor = t.TextMuted
	}
	row_ := ui.Row(c).AlignItems(ui.Start).Gap(10).MinHeight(78).Padding(12, 18).MinWidth(0).Children(func() {
		ui.Column(c).Size(28, 28).Radius(8).Background(tint.Alpha(0.12)).Center().Children(func() {
			ui.Text(c, projectInitials(row.project.Name)).FontSize(11).Bold().TextColor(tint)
		})
		ui.Column(c).Gap(6).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(16).FontWeight(500).TextColor(titleColor).SingleLine()
			subtitle := session.envName()
			if !session.settingsConnectionReady() {
				subtitle += " · " + session.connectionLabel()
			}
			if session.draftFor(thread.ID) != "" {
				subtitle += i18n.T(" · Draft")
			}
			ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
				ui.Icon(c, lucide.Laptop()).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, subtitle).FontSize(12).TextColor(t.TextMuted).SingleLine().MinWidth(0)
			})
		})
		ui.Column(c).AlignItems(ui.End).Gap(8).Children(func() {
			faint(c, relativeTime(thread.UpdatedAt), 12)
			switch {
			case thread.Status == proto.ThreadRunning:
				ui.Spinner(c).Size(15, 15).Label(i18n.T("Running"))
			case thread.Status == proto.ThreadFailed:
				ui.Icon(c, lucide.CircleAlert()).FontSize(16).TextColor(t.Danger)
			case thread.Archived:
				ui.Icon(c, lucide.Archive()).FontSize(15).TextColor(t.TextMuted)
			case row.pinned:
				ui.Icon(c, lucide.Pin()).FontSize(14).TextColor(t.TextMuted)
			default:
				ui.Icon(c, lucide.Check()).FontSize(14).TextColor(t.TextMuted)
			}
		})
	})
	if row_.RowClicked() {
		session.inEnvironment(func() { session.selectThread(thread.ID) })()
	}
	row_.ContextMenu(func(m *ui.Menu) {
		session.threadMenu(m, thread)
	})
}

// projectInitials writes a project's first letters for its badge.
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

// mobileComposeButton is the new-thread button of the phone's home.
func (s *Session) mobileComposeButton(c *ui.Context) {
	if s.mobilePage != "threads" {
		return
	}
	ui.Row(c).Height(72).AlignItems(ui.Center).Justify(ui.End).Padding(0, 16, 0, 0).Children(func() {
		ui.PrimaryButton(c, "").Label(i18n.T("New thread")).Size(56, 56).Radius(18).
			Children(func() {
				ui.Icon(c, lucide.SquarePen()).FontSize(26)
			}).OnClick(s.openNewThread)
	})
}

// mobileBottomNavigation is the phone's bar of tabs.
func (s *Session) mobileBottomNavigation(c *ui.Context) {
	t := c.Theme()
	tab := func(icon *ui.SVG, label, page string, pressed func()) {
		selected := s.mobilePage == page
		b := ui.Column(c).Grow(1).MinWidth(44).Height(72).Center().Gap(5).Children(func() {
			badge := ui.Column(c).Size(56, 34).Radius(20).Center().Children(func() {
				ui.Icon(c, icon).FontSize(26).TextColor(t.Text)
			})
			if selected {
				badge.Background(t.Text.Alpha(0.13))
			}
			ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(t.Text)
		})
		if b.RowClicked() {
			pressed()
		}
	}
	ui.Row(c).AlignItems(ui.Center).Padding(4, 0).Background(t.Surface).Children(func() {
		tab(lucide.MessageSquare(), "Chats", "threads", func() {
			s.closeMobileThreadSearch()
			s.mobilePage = "threads"
		})
		tab(lucide.GitPullRequest(), "Pulls", "prs", func() {
			s.closeMobileThreadSearch()
			s.mobilePage = "prs"
			s.loadMobilePullRequests()
		})
		tab(lucide.ChartColumn(), i18n.T("Usage"), "usage", func() {
			s.closeMobileThreadSearch()
			s.mobilePage = "usage"
			s.loadMobileUsage()
		})
		tab(lucide.Settings(), i18n.T("Settings"), "settings", s.openSettings)
	})
}
