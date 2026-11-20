package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/netimage"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
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
			ui.Button(c, "").Label(i18n.T("Add surface")).Children(func() {
				ui.Icon(c, lucide.Plus()).FontSize(15)
			}).Tooltip(i18n.T("Add surface")).PopoverMenuButton(func(m *ui.Menu) {
				for _, name := range []string{"files", "terminal", "pr", "diff", "graph", "preview", "device", "sidechat", "browser"} {
					name := name
					if name == s.surface {
						continue
					}
					if m.Item(surfaceTitle(name)).Chosen() {
						s.openSurface(name)
					}
				}
			})
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

// filesSurface lists the checkout's files, and shows the file chosen.
func (s *Session) filesSurface(c *ui.Context) {
	t := c.Theme()
	if s.selected == "" {
		ui.Column(c).Fill().Center().Padding(10).Children(func() {
			faint(c, i18n.T("Pick a thread to browse its files."), 12)
		})
		return
	}
	if s.fileSelected != "" {
		file := s.file
		ui.Column(c).Fill().Padding(10).Gap(6).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				iconBtn(c, lucide.ChevronLeft(), i18n.T("Back to files"), func() {
					s.fileSelected, s.file = "", proto.WorkspaceFile{}
				})
				ui.Text(c, s.fileSelected).FontSize(12).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
				faint(c, humanSize(file.Size), 10.5)
				iconBtn(c, lucide.ExternalLink(), i18n.T("Open file"), func() {
					c.OpenURL(s.rawFileURL(s.fileSelected))
				})
			})
			kind := fileKind(s.fileSelected, file.Mime)
			switch {
			case kind == "image":
				netimage.Image(c, s.rawFileURL(s.fileSelected)).FillWidth().AspectRatio(4.0 / 3.0).Radius(6)
			case kind == "markdown":
				ui.Scroll(c).Grow(1).Children(func() {
					markdown.View(c, s.markdownPreview(file.Content, s.fileSelected), &s.md).FillWidth()
				})
			case file.Binary:
				ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(8).Children(func() {
					ui.Icon(c, lucide.FileText()).FontSize(24).TextColor(t.TextMuted)
					faint(c, i18n.T("No preview for this file."), 12)
					ui.Button(c, i18n.T("Open outside T3Go")).Children(func() {
						ui.Icon(c, lucide.ExternalLink()).FontSize(15)
						ui.Text(c, i18n.T("Open outside T3Go"))
					}).OnClick(func() { c.OpenURL(s.rawFileURL(s.fileSelected)) })
				})
			case file.Content == "":
				faint(c, i18n.T("This file is empty."), 12)
			default:
				ui.Scroll(c).Grow(1).Children(func() {
					s.fileContent(c, s.fileSelected, file.Content)
				})
			}
		})
		return
	}
	path := s.filesPath
	ui.Column(c).Fill().Padding(10).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			up := iconBtn(c, lucide.ChevronUp(), i18n.T("Parent folder"), func() {
				if path == "" {
					return
				}
				if i := strings.LastIndex(path, "/"); i >= 0 {
					s.loadFiles(path[:i])
				} else {
					s.loadFiles("")
				}
			})
			if path == "" {
				up.Disabled(true)
			}
			ui.Text(c, "/"+path).FontSize(11.5).Font("monospace").TextColor(t.TextMuted).
				SingleLine().Grow(1).MinWidth(0)
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh files"), func() { s.loadFiles(path) })
		})
		if s.rightBusy {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14).Label(i18n.T("Loading files"))
				faint(c, i18n.T("Loading files…"), 11.5)
			})
		}
		if s.rightErr != "" {
			errorNote(c, s.rightErr)
		}
		ui.Scroll(c).Grow(1).Gap(1).Children(func() {
			if len(s.filesEntries) == 0 && !s.rightBusy {
				faint(c, i18n.T("This folder is empty."), 12)
			}
			for _, entry := range s.filesEntries {
				entry := entry
				icon := lucide.FileText()
				if entry.Directory {
					icon = lucide.Folder()
				}
				row := ui.Row(c).AlignItems(ui.Center).Gap(8).FillWidth().Padding(4, 6).Radius(6).Children(func() {
					ui.Icon(c, icon).FontSize(14).TextColor(t.TextMuted)
					ui.Text(c, entry.Name).FontSize(12).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
					if !entry.Directory {
						faint(c, humanSize(entry.Size), 10.5)
					}
				})
				if row.RowClicked() {
					if entry.Directory {
						s.loadFiles(entry.Path)
					} else {
						s.openFile(entry.Path)
					}
				}
			}
		})
	})
}

// fileContent shows one file: code highlighted in the editor, or text.
func (s *Session) fileContent(c *ui.Context, path, content string) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		content = prettyFileJSON(content)
	}
	if language := editor.LanguageFor(path); language != nil && len(content) < 400_000 {
		buf := editor.NewBuffer(strings.TrimSuffix(content, "\n"))
		buf.SetLanguage(language)
		state := &editor.State{Buffer: buf, ReadOnly: true, SoftWrap: true}
		editor.View(c, state).FillWidth().MinHeight(0)
		return
	}
	t := c.Theme()
	ui.Text(c, content).FontSize(11.5).LineHeight(16).Font("monospace").
		TextColor(t.Text).Selectable()
}

// workingDiffSurface shows the working tree against the thread's
// checkpoint, with staging and discarding per file.
func (s *Session) workingDiffSurface(c *ui.Context) {
	t := c.Theme()
	if s.selected == "" {
		ui.Column(c).Fill().Center().Padding(10).Children(func() {
			faint(c, i18n.T("Pick a thread to read its diff."), 12)
		})
		return
	}
	ui.Column(c).Fill().Padding(10).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
			text(c, i18n.T("Working tree"), 12, t.Text, 600).Grow(1)
			iconBtn(c, lucide.RefreshCw(), i18n.T("Refresh diff"), func() {
				s.loadWorkingDiff(s.selected)
			})
			ui.Button(c, i18n.T("Changes…")).OnClick(func() {
				s.showDiff = true
				s.loadDiff(s.selected, "")
			})
		})
		if s.rightErr != "" {
			errorNote(c, s.rightErr)
		}
		if len(s.workingFiles) > 0 {
			ui.Column(c).Gap(1).Padding(0, 0, 6, 0).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Padding(2, 4).Children(func() {
					text(c, i18n.T("%d changed files", len(s.workingFiles)), 10.5, t.TextMuted, 600)
					ui.Spacer(c)
					faint(c, i18n.T("stage or discard a file"), 9.5)
				})
				ui.Scroll(c).MaxHeight(150).Gap(1).Children(func() {
					for _, file := range s.workingFiles {
						file := file
						detail := fmt.Sprintf("+%d −%d", file.Additions, file.Deletions)
						if file.Staged {
							detail += i18n.T(" · staged")
						}
						ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(2, 4).Children(func() {
							pathRow := ui.Row(c).AlignItems(ui.Center).Gap(5).Grow(1).MinWidth(0).Children(func() {
								ui.Icon(c, lucide.FileText()).FontSize(11).TextColor(t.TextMuted)
								ui.Text(c, file.Path).FontSize(10.5).Font("monospace").
									TextColor(t.Text).SingleLine().MinWidth(0)
							})
							if pathRow.RowClicked() {
								s.showFilePatch(file.Path)
							}
							faint(c, detail, 9.5)
							stage := iconBtn(c, lucide.Check(), i18n.T("Stage "+file.Path), func() {
								s.stagePath(file.Path, true)
							})
							if file.Staged {
								stage.Disabled(true)
							}
							iconBtn(c, lucide.Undo(), i18n.T("Discard "+file.Path), func() {
								s.diffDiscard = file.Path
							})
						})
					}
				})
			})
		}
		if len(s.workingFiles) == 0 {
			ui.Column(c).Grow(1).Center().Children(func() {
				faint(c, i18n.T("Working tree clean."), 12)
			})
			return
		}
		ui.Box(c).Grow(1).FillWidth().Radius(8).Background(t.Surface).Children(func() {
			s.diffPatchViewer(c, s.workingDiff)
		})
	})
}

// showFilePatch opens the Changes dialog on one file's changes.
func (s *Session) showFilePatch(path string) {
	// The dialog shows the whole patch; the file list filters by path.
	s.showDiff = true
	s.loadDiff(s.selected, "")
}

// terminalSurface shows the thread's shell, drawn by the terminal
// plugin.
func (s *Session) terminalSurface(c *ui.Context) {
	if s.selected == "" {
		ui.Column(c).Fill().Center().Padding(10).Children(func() {
			faint(c, i18n.T("Pick a thread to open its terminal."), 12)
		})
		return
	}
	session := s.terminalSession
	ui.Column(c).Fill().Children(func() {
		if s.terminalErr != "" {
			ui.Text(c, s.terminalErr).FontSize(11.5).TextColor(c.Theme().Danger).MaxLines(3).Padding(6, 10)
		}
		if session.ID != "" {
			term := s.terminalHub.get(session.ID)
			if term == nil {
				term = s.terminalHub.open(session.ID, s.client().TerminalURL(session.ID))
			}
			if term != nil {
				terminal.View(c, term).Grow(1).FillWidth().Padding(6, 8, 8, 8).AutoFocus()
				if s.terminalHub.exited(session.ID) {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 10).
						Background(c.Theme().Surface).Children(func() {
						faint(c, i18n.T("Shell exited"), 11.5)
						ui.Button(c, i18n.T("Restart")).OnClick(s.restartTerminal)
					})
				}
				return
			}
			faint(c, i18n.T("The terminal library is not available here."), 12)
			return
		}
		if len(s.terminals) == 0 && !s.terminalBusy {
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				faint(c, i18n.T("No terminal open"), 12.5)
				ui.PrimaryButton(c, i18n.T("New terminal")).Children(func() {
					ui.Icon(c, lucide.Plus()).FontSize(15)
					ui.Text(c, i18n.T("New terminal"))
				}).OnClick(s.newTerminal)
			})
			return
		}
		ui.Column(c).Grow(1).Center().Children(func() {
			faint(c, i18n.T("Starting shell…"), 12)
		})
	})
}

// fileKind names how a file previews: markdown, an image, or text.
func fileKind(path, mimeType string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return "markdown"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".svg", ".avif":
		return "image"
	}
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return "image"
	}
	return "text"
}

// prettyFileJSON indents JSON that parses, leaving anything else as it is.
func prettyFileJSON(content string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(content), "", "  "); err != nil {
		return content
	}
	return buf.String()
}

// diffPatchViewer shows a unified patch in the diff viewer, falling back
// to plain text when the text is not a patch. The viewer's state keeps
// across frames, so its scroll and expansions survive a rebuild.
func (s *Session) diffPatchViewer(c *ui.Context, patch string) {
	if strings.TrimSpace(patch) == "" {
		faint(c, i18n.T("No changes."), 12)
		return
	}
	if s.diffState == nil || s.diffStateKey != patch {
		files, err := editor.ParsePatch(patch)
		if err != nil || len(files) == 0 {
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).Padding(10).Children(func() {
					ui.Text(c, patch).FontSize(11.5).LineHeight(16).Font("monospace").
						TextColor(c.Theme().Text).Selectable()
				})
			})
			return
		}
		diff := &editor.DiffState{}
		diff.SetPatch(files[min(s.diffFileIndex, len(files)-1)])
		s.diffState, s.diffStateKey = diff, patch
	}
	editor.DiffView(c, s.diffState).Grow(1).FillWidth()
}
