package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
)

// narrowView is the phone layout: the list of threads, or the open
// thread with a way back to the list.
//
// TODO(view): the phone layout fills in after the desktop one; these
// views keep the behaviour and take the polish of the old NSGo mobile
// screens one screen at a time.
func (s *Session) narrowView(c *ui.Context) {
	if s.showNewThread {
		s.mobileNewThread(c)
		return
	}
	if s.selected != "" {
		if s.mobileSurface != "" {
			s.mobileSurfaceView(c)
			return
		}
		ui.Column(c).Fill().Children(func() {
			s.mobileHeader(c)
			ui.Box(c).Grow(1).FillWidth().Clip().Children(func() {
				s.threadPane(c)
			})
		})
		return
	}
	s.mobileHome(c)
}

// mobileHeader is the open thread's top bar on a phone: back, the title
// and the entries to its surfaces.
func (s *Session) mobileHeader(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 10).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		iconBtn(c, lucide.ArrowLeft(), i18n.T("Back to the thread list"), func() {
			s.persistView()
			s.selected = ""
		})
		title := s.detail.Thread.Title
		if title == "" {
			title = i18n.T("Thread")
		}
		ui.Text(c, title).FontSize(13.5).FontWeight(600).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
		if s.detail.Thread.ID != "" {
			iconBtn(c, lucide.GitBranch(), i18n.T("Branches"), func() {
				s.showBranches = true
				s.loadBranches(s.selected)
			})
			ui.Button(c, "⋯").Label(i18n.T("Thread actions")).PopoverMenuButton(func(m *ui.Menu) {
				if m.Item(i18n.T("Rename thread…")).Chosen() {
					s.askRenameThread(s.detail.Thread)
				}
				if m.Item(i18n.T("Run history")).Chosen() {
					s.openRuns()
				}
				if m.Item(i18n.T("Open in editor")).Chosen() {
					s.openInEditor()
				}
				if m.Item(i18n.T("Changes in this thread")).Chosen() {
					s.openDiff()
				}
				if m.Item(i18n.T("Delete thread")).Danger().Chosen() {
					s.askDeleteThread(s.detail.Thread)
				}
			})
		}
	})
}

// mobileHome is the phone's first screen: the thread list.
//
// TODO(view): the phone home aggregates every environment's sessions,
// with Working and Settled groups, projects and the bottom bar.
func (s *Session) mobileHome(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Row(c).Padding(8, 12).Children(func() {
			ui.SearchField(c, &s.sidebarQuery).Label(i18n.T("Search threads")).
				Placeholder(i18n.T("Search threads")).Grow(1)
		})
		ui.Box(c).Grow(1).FillWidth().ClipY().Children(func() {
			s.sidebarInbox(c)
		})
		s.sidebarFooter(c)
	})
}

// mobileNewThread is the phone's new-thread screen.
func (s *Session) mobileNewThread(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 10).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			iconBtn(c, lucide.X(), i18n.T("Cancel new thread"), func() {
				s.showNewThread = false
			})
			text(c, i18n.T("New thread"), 13.5, t.Text, 600).Grow(1)
			if ui.Button(c, i18n.T("Create")).Clicked() {
				s.createPreparedThread(true)
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().Padding(16).Children(func() {
			s.composerCard(c, composerSpec{
				Draft:        &s.newThreadPrompt,
				Label:        i18n.T("New thread message"),
				Placeholder:  i18n.T("Ask anything. @ files, $ skills, / commands"),
				AllowEmpty:   true,
				SendDisabled: s.threadBusy,
				OnSend: func() {
					s.createPreparedThread(true)
				},
				Attachments: s.newThreadAttachmentChips,
				Accessory:   func() { s.pickNewThreadAttachments() },
				Controls:    s.newThreadControls,
				OnDropFiles: s.attachNewThreadPaths,
				OnPaste:     s.pasteInto,
			})
			s.newThreadContext(c)
		})
	})
}

// mobileSurfaceView is a phone surface shown over the thread.
//
// TODO(view): the phone surfaces fill in after the desktop ones.
func (s *Session) mobileSurfaceView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 10).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			iconBtn(c, lucide.ArrowLeft(), i18n.T("Back to the thread"), func() {
				s.closeMobileSurface()
			})
			text(c, surfaceTitle(s.mobileSurface), 13.5, t.Text, 600).Grow(1)
		})
		ui.Box(c).Grow(1).FillWidth().Children(func() {
			s.surfaceView(c)
		})
	})
}

// dialogs draws this session's dialogs above its views.
//
// TODO(view): the dialogs fill in one by one; each keeps the behaviour
// its old NSGo dialog had.
func (s *Session) dialogs(c *ui.Context) {
	newProjectDialog(c, s)
	approvalDialog(c, s)
	renameDeleteDialogs(c, s)
	paletteDialog(c, s)
	branchesDialog(c, s)
	runsDialog(c, s)
	diffDialog(c, s)
	editMessageDialog(c, s)
	promptArgsDialog(c, s)
	customModelDialog(c, s)
	dirsDialog(c, s)
	devicesDialog(c, s)
	restoreDialog(c, s)
	settingsDialog(c, s)
}
