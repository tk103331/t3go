package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
)

// rightPanel is the surfaces panel on the right of the conversation: the
// files, the terminal, the pull requests and the other surfaces the
// thread works with.
//
// TODO(view): the surfaces fill in one by one; this panel carries the
// tab strip and the panes it opens.
func rightPanel(c *ui.Context, s *Session) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(6).Wrap().
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			tabs := s.tabs
			if len(tabs) == 0 {
				text(c, i18n.T("Surfaces"), 13, t.Text, 600).MarginStart(4)
			}
			for _, name := range tabs {
				name := name
				selected := name == s.surface
				tab := ui.Row(c).AlignItems(ui.Center).Gap(5).Padding(2, 1, 2, 7).
					Radius(6).Border(1, t.Border).Shrink(0).Children(func() {
					b := ui.Button(c, "").Label(surfaceTitle(name)).Children(func() {
						color := t.TextMuted
						if selected {
							color = t.Text
						}
						ui.Icon(c, surfaceIcon(name)).FontSize(12).TextColor(color)
						ui.Text(c, surfaceTitle(name)).FontSize(10.5).TextColor(color).SingleLine()
					})
					b.OnClick(func() { s.openSurface(name) })
					ui.Button(c, "").Label(i18n.T("Close ") + surfaceTitle(name)).
						Tooltip(i18n.T("Close " + surfaceTitle(name))).Children(func() {
						ui.Icon(c, lucide.X()).FontSize(9).TextColor(t.TextMuted)
					}).OnClick(func() { s.closeSurface(name) })
					if selected {
						b.Background(t.Surface)
					}
				})
				_ = tab
			}
			ui.Spacer(c)
			iconBtn(c, lucide.Plus(), i18n.T("Add surface"), func() { s.openSurface("") })
			iconBtn(c, lucide.X(), i18n.T("Close right panel"), func() { s.setRightOpen(false) })
		})
		ui.Box(c).Grow(1).FillWidth().Clip().Children(func() {
			s.surfaceView(c)
		})
	})
}

// surfaceTitle names a surface the way its tab does.
func surfaceTitle(name string) string {
	switch name {
	case "files":
		return i18n.T("Files")
	case "diff":
		return i18n.T("Diff")
	case "graph":
		return i18n.T("Git graph")
	case "pr":
		return i18n.T("Pull request")
	case "browser":
		return "Browser"
	case "preview":
		return "Preview"
	case "terminal":
		return i18n.T("Terminal")
	case "device":
		return i18n.T("Devices")
	case "sidechat":
		return i18n.T("Side chat")
	}
	return name
}

// surfaceIcon picks a surface tab's glyph.
func surfaceIcon(name string) *ui.SVG {
	switch name {
	case "files":
		return lucide.Folder()
	case "diff":
		return lucide.GitCompare()
	case "graph":
		return lucide.GitBranch()
	case "pr":
		return lucide.GitPullRequest()
	case "browser":
		return lucide.Globe()
	case "preview":
		return lucide.MonitorPlay()
	case "device":
		return lucide.MonitorSmartphone()
	case "sidechat":
		return lucide.MessageCircleMore()
	case "terminal":
		return lucide.Terminal()
	}
	return lucide.PanelRight()
}

// surface draws the pane the tab strip chose.
func (s *Session) surfaceView(c *ui.Context) {
	switch s.surface {
	case "files":
		s.filesSurface(c)
	case "terminal":
		s.terminalSurface(c)
	case "pr":
		s.pullRequestSurface(c)
	case "preview":
		s.previewSurface(c)
	case "device":
		s.deviceSurface(c)
	case "sidechat":
		s.sideChatSurface(c)
	case "browser":
		s.browserSurface(c)
	case "diff":
		s.workingDiffSurface(c)
	case "graph":
		s.graphSurface(c)
	}
}

// The surfaces below fill in as the panel is built out; each keeps the
// behaviour its old NSGo view had.

func (s *Session) filesSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): files surface", 12)
	})
}

func (s *Session) terminalSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): terminal surface", 12)
	})
}

func (s *Session) pullRequestSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): pull request surface", 12)
	})
}

func (s *Session) previewSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): preview surface", 12)
	})
}

func (s *Session) deviceSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): device surface", 12)
	})
}

func (s *Session) sideChatSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): side chat surface", 12)
	})
}

func (s *Session) browserSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): browser surface", 12)
	})
}

func (s *Session) workingDiffSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): working diff surface", 12)
	})
}

func (s *Session) graphSurface(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		faint(c, "TODO(view): git graph surface", 12)
	})
}
