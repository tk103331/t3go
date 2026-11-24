package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
)

// narrowView is the phone layout: the list of threads, or the open
// thread with a way back to the list.
//
// The phone layout draws the same views as the desktop one, arranged
// for a narrow screen: a list of threads, the open conversation, and the
// pages in between.
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
	s.mobileHomeScreen(c)
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
			iconBtn(c, lucide.Folder(), i18n.T("Files"), func() {
				s.openMobileSurface("files")
			})
			// The git controls of the phone: the branch it works on, the
			// changes it has, and the commit that sends them.
			iconBtn(c, lucide.GitBranch(), i18n.T("Git controls"), nil).
				PopoverMenuButton(func(m *ui.Menu) {
					if m.Item(i18n.T("Branches")).Chosen() {
						s.showBranches = true
						s.loadBranches(s.selected)
					}
					if m.Item(i18n.T("Commit & push")).Chosen() {
						s.openMobileCommit(false)
					}
					if m.Item(i18n.T("Review changes")).Chosen() {
						s.openMobileSurface("diff")
					}
				})
			iconBtn(c, lucide.Terminal(), i18n.T("Terminal"), func() {
				s.openMobileSurface("terminal")
			})
			iconBtn(c, lucide.Ellipsis(), i18n.T("Thread actions"), nil).PopoverMenuButton(func(m *ui.Menu) {
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

// mobileNewThread is the phone's new-thread screen.
func (s *Session) mobileNewThread(c *ui.Context) {
	t := c.Theme()
	create := func() { s.createPreparedThread(strings.TrimSpace(s.newThreadPrompt) != "") }
	disabled := s.threadBusy || s.newThreadProj == ""
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 10).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			iconBtn(c, lucide.X(), i18n.T("Cancel new thread"), func() {
				s.showNewThread = false
			})
			text(c, i18n.T("New thread"), 13.5, t.Text, 600).Grow(1)
			toolbarButton(c, i18n.T("Create")).TextColor(t.Accent).Disabled(disabled).OnClick(create)
		})
		ui.Scroll(c).Grow(1).FillWidth().Padding(16).Gap(16).Children(func() {
			s.newThreadContext(c)
			s.composerCard(c, composerSpec{
				Draft:        &s.newThreadPrompt,
				Label:        i18n.T("New thread message"),
				Placeholder:  i18n.T("Ask anything. @ files, $ skills, / commands"),
				FocusLines:   3,
				AllowEmpty:   true,
				SendDisabled: disabled,
				SendLabel:    i18n.T("Create thread"),
				OnSend:       create,
				Attachments:  s.newThreadAttachmentChips,
				Accessory:    func() { s.pickNewThreadAttachments() },
				Controls:     s.newThreadControls,
				OnDropFiles:  s.attachNewThreadPaths,
				OnPaste:      s.pasteInto,
			})
			if s.connErr != "" {
				errorNote(c, s.connErr)
			}
		})
	})
}

// mobileSurfaceView is a phone surface shown over the thread.
//
// The phone shows a surface over the conversation, as the desktop
// shows it beside it.
func (s *Session) mobileSurfaceView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(6, 10).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			iconBtn(c, lucide.ArrowLeft(), i18n.T("Back to the thread"), func() {
				s.closeMobileSurface()
			})
			text(c, s.surfaceTitle(s.mobileSurface), 13.5, t.Text, 600).Grow(1)
		})
		ui.Box(c).Grow(1).FillWidth().Children(func() {
			s.surfaceView(c)
		})
	})
}

// dialogs draws this session's dialogs above its views.
//
// The dialogs are the desktop ones, each keeping the behaviour its old
// NSGo dialog had.
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
	fileEntryDialog(c, s)
	settingsDialog(c, s)
	credentialsDialog(c, s)
	environmentAliasDialog(c, s)
	s.mobileCommitSheet(c)
}
