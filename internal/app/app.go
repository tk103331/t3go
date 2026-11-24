// Package app is the T3Go client, a MyGo native UI application. The same
// views run on the desktop, in a browser and on phones; the data layer
// they draw is one environment session per connection, every field of
// which lives on the main thread.
package app

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
)

// appID names the client's data directory, the one the NSGo client used,
// so its saved environments, drafts and preferences carry over.
const appID = "dev.nsgo.t3go"

// clientApp is the running client: its storage and its environment
// manager, the root of the window's view. Tests build one of their own
// with a memory store.
type clientApp struct {
	store              appStorage
	manager            *Manager
	win                *mygo.Window
	windowButtonsTheme mygo.ThemeSource
}

// Main runs the client: it opens the storage, the language and the
// window, and starts every saved environment's session.
func Main() {
	a := &clientApp{store: openStorage(appID)}
	initClientLanguage(a.store)
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "T3Go",
			Width:                1280,
			Height:               820,
			MinWidth:             480,
			MinHeight:            480,
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       44,
			TrafficLightPosition: &mygo.Point{X: 14, Y: 15},
			Content:              ui.View(a.root),
		})
		a.win.OnClosed(func() {
			if a.manager != nil {
				for _, s := range a.manager.sessions {
					if s != nil {
						s.disposeEnvironmentSession()
					}
				}
			}
			// A local environment server started for this client watches
			// the process and stops itself, unless it was started to
			// outlive the client.
			a.store.Close()
		})
		a.manager = newManager(a.win, a.store)
		a.manager.start()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// root draws the window: the theme, the shortcuts, and either the
// connection screen or the workspace.
func (a *clientApp) root(c *ui.Context) {
	if a.manager == nil {
		a.manager = newManager(a.win, a.store)
		a.manager.start()
	}
	compactView = compact(c)
	if active := a.manager.active; active != nil {
		active.narrow = compact(c)
	}
	t := themeFor(a.manager.appearance, c.Theme())
	c.SetTheme(t)
	if a.win != nil {
		source := mygo.ThemeLight
		if t.Dark {
			source = mygo.ThemeDark
		}
		if a.windowButtonsTheme != source {
			a.win.SetWindowButtonsTheme(source)
			a.windowButtonsTheme = source
		}
	}
	if active := a.manager.active; active != nil {
		active.md.Theme = markdownTheme(*t)
	}
	a.shortcuts(c)
	a.manager.view(c)
}

// shortcuts keeps navigation available while writing a draft. Editing
// shortcuts such as Cmd+B stay with a focused text field.
func (a *clientApp) shortcuts(c *ui.Context) {
	active := a.manager.active
	if active == nil {
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		active.openPalette()
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		active.openSettings()
	}
	if c.FocusTakesText() {
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) {
		active.openNewThread()
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN) {
		active.openNewProject()
	}
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		active.setSidebarOpen(!active.sidebarOpen)
	}
}

// view draws the manager's window: the connection screen until the
// active environment answers, then its workspace.
func (m *Manager) view(c *ui.Context) {
	a := m.active
	if a == nil {
		windowScreen(c, func() { connectScreen(c, nil, m) })
		return
	}
	if !a.connected {
		if a.profile.ID == "" && m.pending != nil {
			windowScreen(c, func() { connectScreen(c, m.pending, m) })
			return
		}
		if a.profile.ID == "" {
			windowScreen(c, func() { connectScreen(c, a, m) })
			return
		}
		if a.connecting && !anySessionReady(m) {
			windowScreen(c, func() { connectingScreen(c, m) })
			return
		}
	}
	m.workspace(c)
}

func anySessionReady(m *Manager) bool {
	for _, s := range m.sessions {
		if s != nil && (s.connected || s.offline) {
			return true
		}
	}
	return false
}

// connectingScreen is what shows while every saved environment tries to
// answer.
func connectingScreen(c *ui.Context, m *Manager) {
	ui.Column(c).Fill().Center().Background(c.Theme().Background).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Label(i18n.T("Connecting environments"))
			text(c, i18n.T("Connecting environments"), 13, c.Theme().TextMuted)
		})
		if m.localErr != "" {
			errorNote(c, m.localErr)
		}
	})
}

// workspace is the connected layout: the sidebar and the conversation,
// with the surfaces panel on the right when it is open.
func (m *Manager) workspace(c *ui.Context) {
	a := m.active
	if compact(c) {
		narrowLayout(c, a)
		return
	}
	if a.sidebarOpen {
		width, _ := c.Size()
		shown, _, _ := a.desktopPaneWidths(width)
		split := ui.Split(c, &shown, func() {
			sidebar(c, a)
		}, func() {
			a.mainPane(c)
		}).Fill().SplitReset(260)
		if split.Changed() {
			a.sidebarWidth = min(max(shown, 220), min(360, width-480))
			a.persistSplitWidths()
		}
	} else {
		a.mainPane(c)
	}

	// The dialogs and overlays of every session sit above the workspace.
	overlays(c, a, m)
}

// mainPane is the sidebar's other side: the conversation, and the
// surfaces panel when it is open.
func (s *Session) mainPane(c *ui.Context) {
	if s.rightOpen {
		width, _ := c.Size()
		sidebarWidth, conversationWidth, _ := s.desktopPaneWidths(width)
		if sidebarWidth > 0 {
			width -= sidebarWidth + 1
		}
		// Split sizes its first pane. Store the right pane's width so
		// resizing the window gives the extra room to the conversation.
		split := ui.Split(c, &conversationWidth, func() {
			conversation(c, s)
		}, func() {
			rightPanel(c, s)
		}).Fill().SplitReset(max(200, width-361))
		if split.Changed() {
			s.rightWidth = min(max(width-conversationWidth-1, 280), max(280, width-420))
			s.persistSplitWidths()
		}
		return
	}
	conversation(c, s)
}

// middleRoom is the conversation pane's width, the space the composer's
// controls have inside the window.
func (s *Session) middleRoom(c *ui.Context) float32 {
	width, _ := c.Size()
	_, room, _ := s.desktopPaneWidths(width)
	return room
}

// desktopPaneWidths keeps responsive controls in step with the split panes.
// Preferred widths stay saved when a smaller window temporarily limits them.
func (s *Session) desktopPaneWidths(width float32) (sidebar, conversation, panel float32) {
	if s.sidebarOpen {
		sidebar = min(max(s.sidebarWidth, 220), min(360, width-480))
		width -= sidebar + 1
	}
	conversation = width
	if s.rightOpen {
		panel = min(max(s.rightWidth, 280), max(280, width-420))
		conversation = max(200, width-panel-1)
	}
	return
}

// conversation is the right of the sidebar: the thread, or the welcome
// screen, under its header.
func conversation(c *ui.Context, a *Session) {
	ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
		a.header(c)
		a.banner(c)
		ui.Box(c).Fill().Clip().Children(func() {
			a.content(c)
		})
	})
}

// narrowLayout is the phone layout: one column.
func narrowLayout(c *ui.Context, a *Session) {
	ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
		windowTitleBar(c)
		ui.Box(c).Grow(1).FillWidth().Clip().Children(func() { a.narrowView(c) })
	})
	overlays(c, a, nil)
}

// overlays draws the dialogs and toasts above the workspace: the active
// session's, and the manager's own.
func overlays(c *ui.Context, a *Session, m *Manager) {
	if a != nil {
		a.dialogs(c)
	}
	if m != nil {
		m.dialogs(c)
	}
}

// banner draws the error line above the content, with a way to dismiss
// it, and the reconnected notice.
func (s *Session) banner(c *ui.Context) {
	if s.connectionRecovered {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 12).
			Background(c.Theme().Surface).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).
			Children(func() {
				ui.Icon(c, lucide.Check()).FontSize(13).TextColor(c.Theme().Success)
				text(c, i18n.T("Reconnected to the environment."), 12, c.Theme().TextMuted)
				ui.Spacer(c)
				if ui.Button(c, i18n.T("Dismiss")).Clicked() {
					s.connectionRecovered = false
				}
			})
	}
	if s.connErr != "" && !s.transportBlocked {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 12).
			Background(c.Theme().Surface).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).
			Children(func() {
				ui.Text(c, s.connErr).FontSize(12).TextColor(c.Theme().Danger).MaxLines(3).Grow(1)
				if ui.Button(c, i18n.T("Dismiss")).Clicked() {
					s.connErr = ""
				}
			})
	}
}

// content draws what the header belongs to: the open thread, the
// new-thread page, or the welcome screen.
func (s *Session) content(c *ui.Context) {
	if s.showNewThread && !compact(c) {
		s.newThreadPage(c)
		return
	}
	if s.selected == "" {
		welcome(c, s)
		return
	}
	s.threadPane(c)
}

// welcome is what an environment with nothing open shows.
func welcome(c *ui.Context, s *Session) {
	t := c.Theme()
	message := i18n.T("Create a project to give your agent a workspace, or start a thread in one you already have.")
	if n := len(s.projects); n > 0 {
		message = i18n.T("You have %d %s ready. Pick a thread from the sidebar or start a fresh one.",
			n, plural(n, i18n.T("project"), i18n.T("projects")))
	}
	ui.Column(c).Fill().Center().Padding(24).Children(func() {
		ui.Column(c).MaxWidth(420).WidthPercent(100).AlignItems(ui.Center).Gap(12).
			Padding(24).Radius(14).Border(1, t.Border).Background(t.Surface).Children(func() {
			brandWordmark(c, 24)
			ui.Text(c, i18n.T("Welcome to ")+s.envName()).FontSize(19).Bold().TextColor(t.Text)
			ui.Text(c, message).FontSize(12.5).FixedLineHeight(19).TextAlign(ui.Center).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).WidthPercent(100).Children(func() {
				ui.PrimaryButton(c, i18n.T("New thread")).Grow(1).OnClick(s.openNewThread)
				ui.Button(c, i18n.T("New project")).Grow(1).OnClick(s.openNewProject)
			})
		})
	})
}

// header is the conversation's top bar: the panel toggles, the thread's
// title and branch, and its actions.
func (s *Session) header(c *ui.Context) {
	t := c.Theme()
	if compact(c) {
		s.mobileHeader(c)
		return
	}
	bar := c.TitleBar()
	left, right := float32(16), float32(16)
	if !s.sidebarOpen {
		left += bar.Left
	}
	if !s.rightOpen {
		right += bar.Right
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Height(windowTitleBarHeight(c)).Shrink(0).
		Padding(6, right, 6, left).DragWindow().
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		toggle := toolbarButton(c, "").Label(i18n.T("Toggle sidebar")).
			Size(30, 30).Padding(0).Shrink(0).Tooltip(i18n.T("Toggle sidebar")).Children(func() {
			ui.Icon(c, lucide.PanelLeft()).FontSize(16)
		})
		if toggle.Clicked() {
			s.setSidebarOpen(!s.sidebarOpen)
		}
		if !s.sidebarOpen {
			toggle.Background(t.SurfaceHover)
		}
		s.headerTitle(c)
		if s.detail.Thread.ID != "" && !s.showNewThread {
			s.branchMenuButton(c)
			toolbarButton(c, "").Label(i18n.T("Thread actions")).
				Size(30, 30).Padding(0).Shrink(0).Tooltip(i18n.T("Thread actions")).Children(func() {
				ui.Icon(c, lucide.Ellipsis()).FontSize(16)
			}).PopoverMenuButton(func(m *ui.Menu) {
				if m.Item(i18n.T("Run history")).Chosen() {
					s.openRuns()
				}
				if m.Item(i18n.T("Open in editor")).Chosen() {
					s.openInEditor()
				}
				if m.Item(i18n.T("Changes in this thread")).Chosen() {
					s.openDiff()
				}
				m.Separator()
				s.threadMenu(m, s.detail.Thread)
			})
		}
		right := toolbarButton(c, "").Label(i18n.T("Toggle right panel")).
			Size(30, 30).Padding(0).Shrink(0).Tooltip(i18n.T("Toggle right panel")).Children(func() {
			ui.Icon(c, lucide.PanelRight()).FontSize(16)
		})
		if right.Clicked() {
			s.setRightOpen(!s.rightOpen)
		}
		if s.rightOpen {
			right.Background(t.SurfaceHover)
		}
	})
}

// headerTitle names the open thread, or the page the workspace shows.
func (s *Session) headerTitle(c *ui.Context) {
	t := c.Theme()
	if s.showNewThread && !compact(c) {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Grow(1).MinWidth(0).Children(func() {
			brandWordmark(c, 17)
			faint(c, "/", 12)
			text(c, i18n.T("New thread"), 12.5, t.Text, 500)
			ui.Spacer(c)
			if toolbarButton(c, i18n.T("Cancel")).Clicked() {
				s.showNewThread = false
			}
		})
		return
	}
	d := s.detail
	if d.Thread.ID == "" {
		ui.Row(c).Gap(6).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			s.environmentStatus(c)
		})
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(7).Grow(1).MinWidth(0).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, d.Thread.Title).FontSize(13.5).FontWeight(600).TextColor(t.Text).
				SingleLine().FillWidth().MinWidth(0).Tooltip(d.Thread.Title)
			s.environmentStatus(c)
		})
	})
}

// environmentStatus is one quiet line under the title: the environment
// and how it is reached.
func (s *Session) environmentStatus(c *ui.Context) {
	t := c.Theme()
	kind, label := s.connectionStatus()
	if label == "" {
		return
	}
	ui.Row(c).Gap(4).AlignItems(ui.Center).Tooltip(label).Children(func() {
		// How the environment is reached is a route type: its glyph says
		// whether the client talks to it directly or through Relay, and
		// the tooltip keeps the words.
		switch kind {
		case "direct", "connected":
			ui.Icon(c, lucide.Link()).FontSize(12).TextColor(t.TextMuted)
		case "relay":
			ui.Icon(c, lucide.Network()).FontSize(12).TextColor(t.TextMuted)
		default:
			if s.offline {
				ui.Icon(c, lucide.CloudOff()).FontSize(11).TextColor(t.TextMuted)
			}
			text(c, label, 10, t.TextMuted).SingleLine().MinWidth(0)
		}
	})
}

// branchMenuButton shows and switches the thread's branch.
func (s *Session) branchMenuButton(c *ui.Context) {
	branch := s.detail.Thread.Branch
	if s.detail.Git != nil && s.detail.Git.Branch != "" {
		branch = s.detail.Git.Branch
	}
	if branch == "" {
		branch = "detached"
	}
	width, _ := c.Size()
	_, room, _ := s.desktopPaneWidths(width)
	toolbarButton(c, "").Label(i18n.T("Branch") + " " + branch).Shrink(0).Tooltip(branch).Children(func() {
		ui.Icon(c, lucide.GitBranch()).FontSize(14)
		if room >= 600 {
			ui.Text(c, branch).SingleLine().MaxWidth(140)
		}
	}).OnClick(func() {
		s.showBranches = true
		s.loadBranches(s.selected)
	})
}
