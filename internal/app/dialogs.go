package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// modal leaves the content's spacing to dialogPanel. MyGo's default
// Modal also pads its panel, which would inset every form twice.
func modal(c *ui.Context, open *bool, build func()) {
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		width, height := c.Size()
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
		panel.MaxWidth(width-32).MaxHeight(height-32).Radius(14).
			Background(c.Theme().Background).Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.25))
		build()
	})
}

// dialogPanel scrolls a form when a small viewport cannot fit its fields.
func dialogPanel(c *ui.Context, width float32) ui.Element {
	t := c.Theme()
	w, h := c.Size()
	if w > 0 && width > w-32 {
		width = w - 32
	}
	return ui.Scroll(c).Width(width).MaxWidthPercent(100).MaxHeight(h - 32).Gap(10).Padding(20).
		Radius(14).Background(t.Background)
}

// dialogFooter is the row of buttons under a dialog's content.
func dialogFooter(c *ui.Context, build func()) {
	ui.Row(c).Gap(8).Wrap().Justify(ui.End).Children(build)
}

// newProjectDialog registers a directory the agent can work in.
func newProjectDialog(c *ui.Context, s *Session) {
	modal(c, &s.showNewProject, func() {
		t := c.Theme()
		dialogPanel(c, 520).Children(func() {
			text(c, i18n.T("New project"), 16, t.Text, 600)
			ui.Text(c, i18n.T("Register a directory the agent can work in. The folder must already exist.")).
				FontSize(12.5).TextColor(t.TextMuted)
			s.projectEnvironmentSelector(c)
			ui.Field(c, i18n.T("Project name"), func() {
				ui.TextInput(c, &s.newProjectName).Placeholder(i18n.T("Defaults to the folder's name")).MinHeight(controlMinHeight(c))
			})
			ui.Row(c).Gap(8).Wrap().AlignItems(ui.End).Children(func() {
				ui.Field(c, i18n.T("Project path"), func() {
					// Enter in the path adds the project, as the old
					// client did.
					path := ui.TextInput(c, &s.newProjectPath).Placeholder("/path/to/project").MinHeight(controlMinHeight(c))
					path.OnSubmit(func() { s.createProject() })
				}).Grow(1).MinWidth(160)
				ui.Button(c, "").Label(i18n.T("Browse…")).MinHeight(controlMinHeight(c)).Children(func() {
					ui.Icon(c, lucide.FolderOpen()).FontSize(15)
					ui.Text(c, i18n.T("Browse…"))
				}).Tooltip(i18n.T("Browse…")).OnClick(func() { s.browseProject() })
			})
			faint(c, i18n.T("The project's path is used for diffs, files, terminals and worktrees."), 11)
			if s.connErr != "" {
				errorNote(c, s.connErr)
			}
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).MinHeight(controlMinHeight(c)).Clicked() {
					s.showNewProject = false
				}
				b := ui.PrimaryButton(c, "").Label(i18n.T("Add project")).MinHeight(controlMinHeight(c)).Children(func() {
					ui.Icon(c, lucide.FolderPlus()).FontSize(15)
					ui.Text(c, i18n.T("Add project"))
				})
				if s.projectBusy {
					b.Disabled(true)
				}
				b.OnClick(s.createProject)
			})
		})
	})
}

// approvalDialog asks about the first tool call a supervised run is
// waiting on.
func approvalDialog(c *ui.Context, s *Session) {
	if len(s.approvals) == 0 {
		return
	}
	request := s.approvals[0]
	description := i18n.T("A supervised agent wants to use ") + request.Tool + "."
	for _, thread := range s.threads {
		if thread.ID == request.ThreadID {
			description = i18n.T("In ") + thread.Title + i18n.T(", the agent wants to use ") + request.Tool + "."
			break
		}
	}
	open := len(s.approvals) > 0
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		t := c.Theme()
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Width(440).MaxWidthPercent(92).MaxHeightPercent(80).Gap(10).Padding(20).
			Radius(14).Background(t.Background)
		ui.Column(c).Gap(10).Children(func() {
			text(c, i18n.T("Allow tool call?"), 16, t.Text, 600)
			ui.Text(c, description).FontSize(12.5).TextColor(t.TextMuted)
			if raw, err := json.MarshalIndent(request.Input, "", "  "); err == nil && len(request.Input) > 0 {
				ui.Scroll(c).MaxHeight(280).Children(func() {
					ui.Column(c).Padding(10).Radius(8).Background(t.Surface).Children(func() {
						ui.Text(c, string(raw)).FontSize(11.5).FixedLineHeight(16).
							Font("monospace").TextColor(t.Text).Selectable()
					})
				})
			}
			if len(s.approvals) > 1 {
				faint(c, i18n.T("%d more waiting", len(s.approvals)-1), 11.5)
			}
			dialogFooter(c, func() {
				if ui.Button(c, "").Label(i18n.T("Deny")).Children(func() {
					ui.Icon(c, lucide.X()).FontSize(15)
					ui.Text(c, i18n.T("Deny"))
				}).Clicked() {
					s.resolveApproval(request.ID, false, i18n.T("Denied by the person running T3Go."))
				}
				if ui.PrimaryButton(c, "").Label(i18n.T("Allow")).Children(func() {
					ui.Icon(c, lucide.Check()).FontSize(15)
					ui.Text(c, i18n.T("Allow"))
				}).Clicked() {
					s.resolveApproval(request.ID, true, "")
				}
			})
		})
	})
}

// renameDeleteDialogs are the rename and delete confirmations the
// sidebar's menus ask for.
func renameDeleteDialogs(c *ui.Context, s *Session) {
	t := c.Theme()
	if s.pendingRename.Name != "" {
		s.renameOpen = true
		modal(c, &s.renameOpen, func() {
			dialogPanel(c, 380).Children(func() {
				text(c, i18n.T("Rename"), 16, t.Text, 600)
				ui.Field(c, i18n.T("Name"), func() {
					ui.TextInput(c, &s.renameName).AutoFocus()
				})
				if s.connErr != "" {
					errorNote(c, s.connErr)
				}
				dialogFooter(c, func() {
					if ui.Button(c, i18n.T("Cancel")).Clicked() {
						s.cancelRename()
					}
					ui.PrimaryButton(c, i18n.T("Rename")).OnClick(s.confirmRename)
				})
			})
		})
	}
	if s.pendingDelete.Name != "" {
		s.deleteOpen = true
		modal(c, &s.deleteOpen, func() {
			dialogPanel(c, 400).Children(func() {
				text(c, i18n.T("Delete"), 16, t.Text, 600)
				ui.Text(c, i18n.T("Delete %s? This cannot be undone.", s.pendingDelete.Name)).
					FontSize(12.5).TextColor(t.TextMuted)
				dialogFooter(c, func() {
					if ui.Button(c, i18n.T("Cancel")).Clicked() {
						s.cancelDelete()
					}
					if ui.Button(c, "").Label(i18n.T("Delete")).Children(func() {
						ui.Icon(c, lucide.Trash()).FontSize(15).TextColor(t.Danger)
						ui.Text(c, i18n.T("Delete")).TextColor(t.Danger)
					}).Clicked() {
						s.confirmDelete()
					}
				})
			})
		})
	}
}

// paletteItem is one row of the command palette.
type paletteItem struct {
	id          string
	group       string
	label       string
	description string
	icon        *ui.SVG
}

// paletteItems lists what the palette shows: the client's actions, then
// every environment's threads, then the messages the search found.
func (s *Session) paletteItems() []paletteItem {
	items := []paletteItem{
		{id: "new-thread", group: i18n.T("Actions"), label: i18n.T("New thread"), icon: lucide.MessageSquarePlus()},
		{id: "new-project", group: i18n.T("Actions"), label: i18n.T("New project"), icon: lucide.FolderPlus()},
		{id: "settings", group: i18n.T("Actions"), label: i18n.T("Settings"), icon: lucide.Settings()},
		{id: "toggle-archived", group: i18n.T("View"), label: s.archivedLabel(), icon: lucide.Archive()},
		{id: "toggle-sidebar", group: i18n.T("View"), label: i18n.T("Toggle sidebar"), icon: lucide.PanelLeft()},
	}
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
	for _, session := range sessions {
		projects := map[string]string{}
		for _, p := range session.projects {
			projects[p.ID] = p.Name
		}
		for _, t := range session.threads {
			if t.Archived && !showArchived {
				continue
			}
			items = append(items, paletteItem{
				id: "thread:" + session.threadKey(t.ID), group: i18n.T("Threads"),
				label: t.Title, description: projects[t.ProjectID] + " · " + session.envName(),
				icon: lucide.MessageSquare(),
			})
		}
	}
	for _, hit := range s.searchHits {
		items = append(items, paletteItem{
			id: "message:" + hit.ThreadID + ":" + hit.MessageID, group: i18n.T("Messages"),
			label: hit.Snippet, description: hit.ProjectName + " · " + hit.ThreadTitle,
			icon: lucide.MessageCircle(),
		})
	}
	return items
}

// paletteDialog searches the environment's threads, messages and
// actions, with the arrows and Enter choosing from the list.
func paletteDialog(c *ui.Context, s *Session) {
	modal(c, &s.showPalette, func() {
		t := c.Theme()
		query := strings.ToLower(strings.TrimSpace(s.paletteQuery))
		var items []paletteItem
		for _, item := range s.paletteItems() {
			if query == "" || strings.Contains(strings.ToLower(item.label+" "+item.description), query) {
				items = append(items, item)
			}
		}
		dialogPanel(c, 720).Children(func() {
			field := ui.SearchField(c, &s.paletteQuery).Label(i18n.T("Search commands")).
				Placeholder(i18n.T("Search threads, messages and actions…")).AutoFocus()
			if field.Changed() {
				s.searchMessages(s.paletteQuery)
			}
			if s.searchErr != "" {
				errorNote(c, s.searchErr)
			}
			if len(items) == 0 {
				ui.Column(c).Padding(16).Children(func() {
					faint(c, i18n.T("No threads, messages or commands match."), 12)
				})
			} else {
				s.paletteRow = min(s.paletteRow, len(items)-1)
				s.paletteList.Key = func(i int) any { return items[i].id }
				s.paletteList.Selected = &s.paletteRow
				s.paletteList.Label = func(i int) string { return items[i].label }
				list := ui.List(c, &s.paletteList, len(items), func(i int) {
					item := items[i]
					row := ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 8).Radius(6).FillWidth().Children(func() {
						ui.Icon(c, item.icon).FontSize(14).TextColor(t.TextMuted)
						ui.Text(c, item.label).FontSize(12.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
						if item.description != "" {
							ui.Text(c, item.description).FontSize(11).TextColor(t.TextMuted).
								SingleLine().MinWidth(0).MaxWidthPercent(45)
						}
						faint(c, item.group, 9.5)
					})
					if row.RowClicked() {
						s.runCommand(item.id)
					}
				}).Height(420).FillWidth().Gap(1)
				if list.Submitted() {
					s.runCommand(items[s.paletteRow].id)
				}
			}
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Close")).Clicked() {
					s.showPalette = false
				}
			})
		})
	})
}

// branchesDialog lists the thread's branches and switches among them.
func branchesDialog(c *ui.Context, s *Session) {
	modal(c, &s.showBranches, func() {
		t := c.Theme()
		dialogPanel(c, 420).Children(func() {
			text(c, i18n.T("Branches"), 16, t.Text, 600)
			ui.Scroll(c).MaxHeight(320).Gap(2).Children(func() {
				for _, branch := range s.branches {
					branch := branch
					current := branch.Current
					row := clickableRow(c, branch.Name, func() {
						ui.Icon(c, lucide.GitBranch()).FontSize(13).TextColor(t.TextMuted)
						ui.Text(c, branch.Name).FontSize(12.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
						if current {
							text(c, i18n.T("Current"), 10.5, t.Accent)
						}
					}).Padding(6, 8).MinHeight(controlMinHeight(c)).Radius(6)
					if !current && row.Clicked() {
						s.switchBranch(branch.Name)
						s.showBranches = false
					}
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Field(c, i18n.T("New branch"), func() {
					ui.TextInput(c, &s.branchName).Placeholder(i18n.T("name-from-the-title"))
				}).Grow(1)
				ui.Button(c, i18n.T("Create")).OnClick(func() { s.createBranch() })
			})
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Close")).Clicked() {
					s.showBranches = false
				}
			})
		})
	})
}

// runsDialog lists the thread's runs, with each one's diff.
func runsDialog(c *ui.Context, s *Session) {
	modal(c, &s.showRuns, func() {
		t := c.Theme()
		dialogPanel(c, 560).Children(func() {
			text(c, i18n.T("Run history"), 16, t.Text, 600)
			ui.Scroll(c).MaxHeight(360).Gap(6).Children(func() {
				for _, run := range s.detail.Runs {
					run := run
					ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 8).Radius(6).Children(func() {
						ui.Icon(c, lucide.Clock()).FontSize(13).TextColor(t.TextMuted)
						ui.Text(c, run.StartedAt.Local().Format("Jan 2 15:04")).FontSize(12).TextColor(t.Text).SingleLine()
						ui.Text(c, runModelLabel(run)).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
						ui.Button(c, i18n.T("Diff")).OnClick(func() {
							s.showRuns = false
							s.openRunDiff(run.ID)
						})
						ui.Button(c, i18n.T("Restore")).OnClick(func() {
							s.pendingRestore = run.ID
							s.showRestore = true
						})
					})
				}
				if len(s.detail.Runs) == 0 {
					faint(c, i18n.T("No runs yet."), 12)
				}
			})
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Close")).Clicked() {
					s.showRuns = false
				}
			})
		})
	})
}

// runModelLabel names a run's model, or its provider.
func runModelLabel(run proto.Run) string {
	if run.Model != "" {
		return run.Model
	}
	return run.Provider
}

// diffDialog shows one run's checkpoint diff, or the thread's working
// changes, in the diff viewer, with the checkout's git controls.
func diffDialog(c *ui.Context, s *Session) {
	modal(c, &s.showDiff, func() {
		t := c.Theme()
		dialogPanel(c, 860).Children(func() {
			title := s.diffTitle
			if title == "" {
				title = i18n.T("Changes")
			}
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				text(c, title, 16, t.Text, 600).SingleLine().MaxWidth(520)
				ui.Spacer(c)
				s.diffLayoutChoice(c)
				if s.diffDiscard != "" {
					path := s.diffDiscard
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						faint(c, i18n.T("Discard changes to ")+path+"?", 11.5)
						ui.Button(c, "").Label(i18n.T("Discard")).Children(func() {
							ui.Icon(c, lucide.Undo()).FontSize(15)
							ui.Text(c, i18n.T("Discard"))
						}).OnClick(func() {
							s.diffDiscard = ""
							s.stagePath(path, false)
						})
					})
				}
				if ui.Button(c, "").Label("").Label(i18n.T("Close diff")).Children(func() {
					ui.Icon(c, lucide.X()).FontSize(15)
				}).Tooltip(i18n.T("Close diff")).Clicked() {
					s.showDiff = false
				}
			})
			if strings.Contains(s.diffText, i18n.T("Loading diff…")) {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label(i18n.T("Loading diff"))
					faint(c, i18n.T("Loading diff…"), 12)
				})
			}
			ui.Box(c).Height(420).FillWidth().Children(func() {
				s.diffViewer(c)
			})
			s.gitControls(c)
		})
	})
}

// gitControls are the Changes dialog's git controls: the checkout's
// status line, the commit message, and the actions.
func (s *Session) gitControls(c *ui.Context) {
	t := c.Theme()
	git := s.detail.Git
	branch := ""
	if git != nil {
		branch = git.Branch
	}
	if git == nil || !git.Repo {
		return
	}
	ui.Column(c).Gap(8).Children(func() {
		var detail []string
		if git.Dirty {
			detail = append(detail, i18n.T("uncommitted changes"))
		}
		if git.Ahead > 0 {
			detail = append(detail, i18n.T("%d ahead", git.Ahead))
		}
		if git.Behind > 0 {
			detail = append(detail, i18n.T("%d behind", git.Behind))
		}
		branchField := text(c, branch, 12, t.Text, 600)
		if len(s.branches) > 1 {
			branchField = ui.Button(c, "").Label("").Label(i18n.T("Branch") + " " + branch).Children(func() {
				ui.Icon(c, lucide.GitBranch()).FontSize(14)
				ui.Text(c, branch).SingleLine().MaxWidth(160)
			}).PopoverMenuButton(func(m *ui.Menu) {
				for _, ref := range s.branches {
					ref := ref
					if m.Item(ref.Name).Checked(ref.Current).Chosen() {
						s.switchBranch(ref.Name)
					}
				}
			})
		}
		ui.Row(c).AlignItems(ui.Center).Gap(4).MinWidth(0).Children(func() {
			branchField.Padding(0)
			if len(detail) > 0 {
				faint(c, strings.Join(detail, " · "), 11.5)
			}
			ui.Spacer(c)
		})
		ui.Field(c, i18n.T("Commit message"), func() {
			ui.TextInput(c, &s.gitMessage).Placeholder(i18n.T("Commit message (defaults to an update)"))
		})
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(6).Children(func() {
			commit := ui.Button(c, "").Label(i18n.T("Commit")).Children(func() {
				ui.Icon(c, lucide.Check()).FontSize(15)
				ui.Text(c, i18n.T("Commit"))
			})
			if s.gitBusy {
				commit.Disabled(true)
			}
			commit.OnClick(func() { s.runGitAction(proto.GitCommit) })
			push := ui.Button(c, "").Label(i18n.T("Push")).Children(func() {
				ui.Icon(c, lucide.ArrowUp()).FontSize(15)
				ui.Text(c, i18n.T("Push"))
			})
			if s.gitBusy {
				push.Disabled(true)
			}
			push.OnClick(func() { s.runGitAction(proto.GitPush) })
			cp := ui.Button(c, i18n.T("Commit & push")).Children(func() {
				ui.Icon(c, lucide.ArrowUp()).FontSize(15)
				ui.Text(c, i18n.T("Commit & push"))
			})
			if s.gitBusy {
				cp.Disabled(true)
			}
			cp.OnClick(func() { s.runGitAction(proto.GitCommitPush) })
			pr := ui.PrimaryButton(c, "").Label(i18n.T("Create PR")).Children(func() {
				ui.Icon(c, lucide.GitPullRequest()).FontSize(15)
				ui.Text(c, i18n.T("Create PR"))
			})
			if s.gitBusy {
				pr.Disabled(true)
			}
			pr.OnClick(func() { s.runGitAction(proto.GitCommitPR) })
			if s.gitBusy {
				ui.Spinner(c).Size(14, 14).Label(i18n.T("Running git action"))
			}
		})
		if s.gitNote != "" {
			ui.Text(c, s.gitNote).FontSize(11.5).FixedLineHeight(16).TextColor(t.TextMuted)
		}
		if s.gitPR != "" {
			ui.Text(c, s.gitPR).FontSize(11.5).TextColor(t.Accent).SingleLine()
		}
	})
}

// diffViewer draws the diff text the dialog holds, or the message that
// there is nothing to show.
func (s *Session) diffViewer(c *ui.Context) {
	text := s.diffText
	if text == "" {
		text = s.workingDiff
	}
	s.diffPatchViewer(c, text)
}

// editMessageDialog changes a message the reader sent.
func editMessageDialog(c *ui.Context, s *Session) {
	if s.editingMessage.ID == "" {
		return
	}
	s.editOpen = true
	modal(c, &s.editOpen, func() {
		t := c.Theme()
		dialogPanel(c, 520).Children(func() {
			text(c, i18n.T("Edit message"), 16, t.Text, 600)
			ui.TextArea(c, &s.editDraft).Lines(3, 10)
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).Clicked() {
					s.cancelEditMessage()
				}
				ui.PrimaryButton(c, i18n.T("Save and rerun")).OnClick(s.confirmEditMessage)
			})
		})
	})
}

// promptArgsDialog fills the arguments a skill or command asks for.
func promptArgsDialog(c *ui.Context, s *Session) {
	modal(c, &s.showPromptArgs, func() {
		t := c.Theme()
		dialogPanel(c, 460).Children(func() {
			text(c, s.promptName, 16, t.Text, 600)
			values := s.promptArgValues
			for i, def := range s.promptArgsDefs {
				if i >= len(values.Values) {
					break
				}
				def := def
				index := i
				ui.Field(c, def.Name, func() {
					ui.TextInput(c, &values.Values[index]).Placeholder(def.Description)
				})
			}
			s.promptArgValues = values
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).Clicked() {
					s.showPromptArgs = false
				}
				ui.PrimaryButton(c, i18n.T("Run")).OnClick(s.submitPromptArgs)
			})
		})
	})
}

// customModelDialog adds a model id the environment's CLIs accept but do
// not list.
func customModelDialog(c *ui.Context, s *Session) {
	modal(c, &s.showCustomModel, func() {
		t := c.Theme()
		dialogPanel(c, 440).Children(func() {
			text(c, i18n.T("Custom model"), 16, t.Text, 600)
			ui.Field(c, i18n.T("Provider"), func() {
				ui.TextInput(c, &s.customModelProvider).Placeholder("claude")
			})
			ui.Field(c, i18n.T("Model id"), func() {
				ui.TextInput(c, &s.customModelID).Placeholder("model-id")
			})
			ui.Field(c, i18n.T("Display name"), func() {
				ui.TextInput(c, &s.customModelName)
			})
			if s.customModelErr != "" {
				errorNote(c, s.customModelErr)
			}
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).Clicked() {
					s.showCustomModel = false
				}
				ui.PrimaryButton(c, i18n.T("Add model")).OnClick(s.addCustomModel)
			})
		})
	})
}

// dirsDialog lists the folders of the environment's machine, for the
// project path a new project names.
func dirsDialog(c *ui.Context, s *Session) {
	modal(c, &s.showDirs, func() {
		t := c.Theme()
		dialogPanel(c, 520).Children(func() {
			text(c, i18n.T("Choose a folder"), 16, t.Text, 600)
			s.dirsBreadcrumb(c)
			if s.dirsBusy {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label(i18n.T("Loading folders"))
				})
			}
			if s.dirsErr != "" {
				errorNote(c, s.dirsErr)
			}
			ui.Scroll(c).MaxHeight(320).Gap(2).Children(func() {
				if s.dirs.Parent != "" {
					parentRow := clickableRow(c, i18n.T("Parent folder"), func() {
						ui.Icon(c, lucide.ArrowLeft()).FontSize(13).TextColor(t.TextMuted)
						faint(c, "..", 12.5)
					}).Padding(6, 8).MinHeight(controlMinHeight(c)).Radius(6)
					if parentRow.Clicked() {
						s.loadDirs(s.dirs.Parent)
					}
				}
				for _, entry := range s.dirs.Entries {
					entry := entry
					entryRow := clickableRow(c, entry.Name, func() {
						ui.Icon(c, lucide.Folder()).FontSize(13).TextColor(t.TextMuted)
						ui.Text(c, entry.Name).FontSize(12.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
					}).Padding(6, 8).MinHeight(controlMinHeight(c)).Radius(6)
					if entryRow.Clicked() {
						s.loadDirs(entry.Path)
					}
				}
			})
			s.newDirForm(c)
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).MinHeight(controlMinHeight(c)).Clicked() {
					s.closeDirs()
				}
				ui.PrimaryButton(c, i18n.T("Use this folder")).MinHeight(controlMinHeight(c)).OnClick(s.useDirs)
			})
		})
	})
}

// dirsBreadcrumb is the path of the folder on screen, with a way back
// to each of its parents.
func (s *Session) dirsBreadcrumb(c *ui.Context) {
	path := strings.TrimSpace(s.dirs.Path)
	if path == "" {
		return
	}
	t := c.Theme()
	root := "/"
	if !strings.HasPrefix(path, "/") {
		root = ""
	}
	ui.ScrollHorizontal(c).FillWidth().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(1).Children(func() {
			prefix := root
			for i, part := range strings.Split(strings.Trim(path, "/"), "/") {
				if part == "" {
					continue
				}
				if i > 0 {
					faint(c, "/", 11)
				}
				prefix = strings.TrimRight(prefix, "/") + "/" + part
				target := prefix
				ui.Button(c, part).FontSize(11.5).Padding(2, 4).
					TextColor(t.TextMuted).OnClick(func() { s.loadDirs(target) })
			}
		})
	})
}

// newDirForm names a folder to create in the directory on screen.
func (s *Session) newDirForm(c *ui.Context) {
	if !s.showNewDir {
		ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
			ui.Button(c, "").Label(i18n.T("New folder")).MinHeight(controlMinHeight(c)).Children(func() {
				ui.Icon(c, lucide.FolderPlus()).FontSize(14)
				ui.Text(c, i18n.T("New folder"))
			}).OnClick(func() {
				s.showNewDir = true
				s.newDirName = ""
				s.newDirErr = ""
			})
			faint(c, i18n.T("Create a folder in the selected directory."), 11)
		})
		return
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Field(c, i18n.T("Folder name"), func() {
			ui.TextInput(c, &s.newDirName).Placeholder(i18n.T("Folder name"))
		})
		if s.newDirErr != "" {
			errorNote(c, s.newDirErr)
		}
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, i18n.T("Cancel")).Clicked() {
				s.closeNewDir()
			}
			create := ui.PrimaryButton(c, i18n.T("Create folder"))
			if s.newDirBusy {
				create.Disabled(true)
			}
			create.OnClick(s.createDir)
		})
	})
}

// devicesDialog lists the simulators the environment machine has, with
// their screens.
func devicesDialog(c *ui.Context, s *Session) {
	modal(c, &s.showDevices, func() {
		t := c.Theme()
		dialogPanel(c, 560).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				text(c, i18n.T("Devices"), 16, t.Text, 600)
				ui.Spacer(c)
				live := s.deviceLive
				ui.Checkbox(c, &live, i18n.T("Live screenshot")).OnChange(func() {
					s.setDeviceLive(live)
				})
			})
			if s.deviceErr != "" {
				errorNote(c, s.deviceErr)
			}
			ui.Scroll(c).MaxHeight(320).Gap(4).Children(func() {
				for _, device := range s.devices {
					device := device
					ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 8).Radius(6).Children(func() {
						ui.Icon(c, lucide.Smartphone()).FontSize(13).TextColor(t.TextMuted)
						ui.Text(c, device.Name).FontSize(12.5).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
						if device.State == "booted" {
							faint(c, i18n.T("Running"), 11)
						} else {
							ui.Button(c, i18n.T("Boot")).OnClick(func() { s.bootDevice(device.ID) })
						}
					})
				}
				if len(s.devices) == 0 {
					faint(c, i18n.T("No simulators on this machine."), 12)
				}
			})
			if s.deviceSelected != "" && len(s.deviceImage) > 0 {
				ui.Box(c).Height(280).FillWidth().Children(func() {
					s.deviceScreen(c)
				})
			}
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Close")).Clicked() {
					s.showDevices = false
				}
			})
		})
	})
}

// deviceScreen draws the screenshot a simulator took.
func (s *Session) deviceScreen(c *ui.Context) {
	bitmap := s.deviceBitmap()
	if bitmap == nil {
		faint(c, i18n.T("No screenshot yet."), 12)
		return
	}
	ui.Image(c, bitmap).Fit(ui.Contain).Fill()
}

// deviceBitmap decodes the latest screenshot once, keeping the bitmap for
// the frames that draw it.
func (s *Session) deviceBitmap() *ui.Bitmap {
	if len(s.deviceImage) == 0 {
		return nil
	}
	if s.deviceBitmapCache == nil || s.deviceBitmapKey != s.deviceShot {
		bitmap, err := ui.DecodeBitmap(s.deviceImage)
		if err != nil {
			return nil
		}
		s.deviceBitmapCache, s.deviceBitmapKey = bitmap, s.deviceShot
	}
	return s.deviceBitmapCache
}

// restoreDialog asks before a run's checkpoint takes the files back.
func restoreDialog(c *ui.Context, s *Session) {
	modal(c, &s.showRestore, func() {
		t := c.Theme()
		dialogPanel(c, 460).Children(func() {
			text(c, i18n.T("Restore this run?"), 16, t.Text, 600)
			ui.Text(c, i18n.T("The tracked files go back to how this run's checkpoint left them. The current files are backed up first.")).
				FontSize(12.5).TextColor(t.TextMuted)
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).Clicked() {
					s.showRestore = false
					s.pendingRestore = ""
				}
				ui.PrimaryButton(c, i18n.T("Restore")).OnClick(func() {
					s.restoreRun()
					s.showRestore = false
				})
			})
		})
	})
}

// settingsDialog is the client's settings.
func settingsDialog(c *ui.Context, s *Session) {
	modal(c, &s.showSettings, func() {
		t := c.Theme()
		width, height := c.Size()
		dialogHeight := min(620, height-64)
		ui.Row(c).Width(min(920, width-64)).Height(dialogHeight).Gap(0).
			Radius(14).Clip().Border(1, t.Border).Background(t.Background).Children(func() {
			// The section list, as the old settings dialog showed it.
			ui.Column(c).Width(200).FillHeight().Gap(12).Padding(16, 12).
				BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Background(t.Surface).Children(func() {
				text(c, i18n.T("Settings"), 16, t.Text, 600).Margin(0, 8, 4, 8)
				ui.Scroll(c).Grow(1).FillWidth().Children(func() {
					for _, section := range settingsSections() {
						selected := s.settingsSection == section.key
						color := t.TextMuted
						if selected {
							color = t.Accent
						}
						row := clickableRow(c, section.label, func() {
							ui.Icon(c, section.icon()).FontSize(15).TextColor(color)
							ui.Text(c, section.label).FontSize(12.5).
								TextColor(color).SingleLine()
						}).MinHeight(36).Padding(8, 10).Radius(7)
						if selected {
							row.Background(t.Accent.Alpha(0.1))
						}
						if row.Clicked() {
							s.setSettingsSection(section.key)
						}
					}
				})
			})
			// The section's page.
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Gap(0).
				Background(t.Background).Children(func() {
				ui.Row(c).MinHeight(64).Padding(16, 24).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
					text(c, s.settingsSectionLabel(), 18, t.Text, 600).Grow(1)
					iconBtn(c, lucide.X(), i18n.T("Close settings"), func() { s.closeSettings() })
				})
				ui.Scroll(c).Grow(1).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(16).Padding(24).Children(func() {
						s.settingsSectionView(c)
					})
				})
				if s.settingsSaveErr != "" {
					errorNote(c, s.settingsSaveErr)
				}
			})
		})
	})
}

// settingsSection is one entry of the settings dialog's list.
type settingsSection struct {
	key   string
	label string
	icon  func() *ui.SVG
}

// settingsSections lists the settings dialog's sections in order.
func settingsSections() []settingsSection {
	return []settingsSection{
		{key: "general", label: i18n.T("General"), icon: lucide.Settings},
		{key: "account", label: i18n.T("Account"), icon: lucide.UserRound},
		{key: "providers", label: i18n.T("Providers"), icon: lucide.Cpu},
		{key: "usage", label: i18n.T("Usage"), icon: lucide.ChartColumn},
		{key: "worktrees", label: i18n.T("Worktrees"), icon: lucide.GitFork},
		{key: "access", label: i18n.T("Access"), icon: lucide.KeyRound},
		{key: "appearance", label: i18n.T("Appearance"), icon: lucide.Sparkles},
		{key: "environments", label: i18n.T("Environments"), icon: lucide.Network},
		{key: "background", label: i18n.T("Background server"), icon: lucide.Power},
		{key: "shortcuts", label: i18n.T("Shortcuts"), icon: lucide.Keyboard},
		{key: "about", label: i18n.T("About"), icon: lucide.Info},
	}
}

// settingsSectionLabel names the section the settings dialog shows.
func (s *Session) settingsSectionLabel() string {
	for _, section := range settingsSections() {
		if section.key == s.settingsSection {
			return section.label
		}
	}
	return s.settingsSection
}

// settingsSectionView draws one settings section.
func (s *Session) settingsSectionView(c *ui.Context) {
	switch s.settingsSection {
	case "general":
		// The environment's own name and defaults, as the old client's
		// environment-defaults page had them.
		s.settingsTargetPage(c, true, func(target *Session) { target.settingsGeneral(c) })
	case "account":
		s.settingsAccount(c)
	case "appearance":
		s.settingsAppearance(c)
	case "providers":
		s.settingsTargetPage(c, true, func(target *Session) { target.settingsProviders(c) })
	case "usage":
		s.settingsTargetPage(c, true, func(target *Session) { target.settingsUsage(c) })
	case "worktrees":
		s.settingsTargetPage(c, true, func(target *Session) { target.settingsWorktrees(c) })
	case "environments":
		s.settingsEnvironments(c)
	case "background":
		s.settingsBackground(c)
	case "access":
		s.settingsAccess(c)
	case "shortcuts":
		s.settingsShortcuts(c)
	case "about":
		s.settingsAbout(c)
	}
}

// settingsGeneral edits the environment's title and defaults.
func (s *Session) settingsGeneral(c *ui.Context) {
	draft := s.settingsDraft
	ui.Column(c).Gap(12).Children(func() {
		settingsSectionTitle(c, i18n.T("General"), i18n.T("The environment's name and defaults for new threads."))
		ui.Field(c, i18n.T("Environment title"), func() {
			ui.TextInput(c, &draft.Title).Placeholder("T3Go")
		})
		if ui.PrimaryButton(c, i18n.T("Save")).Clicked() {
			s.settingsDraft = draft
			s.saveSettings(draft)
		}
	})
}

// settingsAppearance edits the client's theme, accent and language.
func (s *Session) settingsAppearance(c *ui.Context) {
	ui.Column(c).Gap(12).Children(func() {
		settingsSectionTitle(c, i18n.T("Appearance"), i18n.T("Saved automatically on this client, across all environments."))
		faint(c, i18n.T("Language"), 12)
		segmentChoice(c, languageNames(), i18n.Current().Name(), func(choice string) {
			for _, lang := range i18n.Languages() {
				if lang.Name() == choice {
					setClientLanguage(lang)
				}
			}
		})
		faint(c, i18n.T("Theme"), 12)
		segmentChoice(c, []string{"system", "light", "dark"}, s.appearance.Theme, func(choice string) {
			prefs := s.appearance
			prefs.Theme = choice
			s.saveClientAppearance(prefs)
		})
		faint(c, i18n.T("Accent"), 12)
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, choice := range accentChoices {
				choice := choice
				swatch := c.Theme().TextMuted.Alpha(0.25)
				if choice.hex != "" {
					swatch = ui.Hex(choice.hex)
				}
				swatchName := choice.name
				b := toolbarButton(c, "").Label("Accent "+choice.name).Children(func() {
					ui.Box(c).Size(13, 13).Radius(7).Background(swatch)
					faint(c, swatchName, 12)
				}).Padding(6, 10).Radius(7).Border(1, c.Theme().Border)
				if s.appearance.Accent == choice.hex {
					b.Background(c.Theme().Surface)
				}
				if b.Clicked() {
					prefs := s.appearance
					prefs.Accent = choice.hex
					s.saveClientAppearance(prefs)
				}
			}
		})
	})
}

// languageNames lists the interface languages by their own names.
func languageNames() []string {
	var names []string
	for _, lang := range i18n.Languages() {
		names = append(names, lang.Name())
	}
	return names
}

var _ = fmt.Sprintf
