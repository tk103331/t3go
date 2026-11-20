package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// newThreadPage is the workspace's new-thread screen: a hero, the first
// message's composer, and the choices that shape the thread.
func (s *Session) newThreadPage(c *ui.Context) {
	ui.Column(c).Fill().Padding(24, 28).Children(func() {
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).WidthPercent(100).MinHeightPercent(100).
				Center().Padding(0, 0, 32, 0).Children(func() {
				ui.Column(c).WidthPercent(100).MaxWidth(900).Gap(18).Children(func() {
					s.newThreadHero(c)
					s.composerCard(c, composerSpec{
						Draft:          &s.newThreadPrompt,
						Label:          i18n.T("New thread message"),
						Placeholder:    i18n.T("Ask anything. @ files, $ skills, / commands"),
						EditorMinLines: 4,
						AllowEmpty:     true,
						SendDisabled:   s.threadBusy,
						OnSend: func() {
							s.createPreparedThread(strings.TrimSpace(s.newThreadPrompt) != "")
						},
						Attachments: s.newThreadAttachmentChips,
						Accessory:   func() { s.pickNewThreadAttachments() },
						Controls:    s.newThreadControls,
						OnDropFiles: s.attachNewThreadPaths,
						OnPaste:     s.pasteInto,
					})
					s.newThreadContext(c)
					if s.connErr != "" {
						errorNote(c, s.connErr)
					}
				})
			})
		})
	})
}

// newThreadHero greets the reader and names the project a thread starts
// in.
func (s *Session) newThreadHero(c *ui.Context) {
	t := c.Theme()
	project := ""
	for _, candidate := range s.projects {
		if candidate.ID == s.newThreadProj {
			project = candidate.Name
			break
		}
	}
	subtitle := i18n.T("Choose a project below to give the agent a workspace.")
	if project != "" {
		subtitle = i18n.T("Start a new thread in ") + project + "."
	}
	ui.Column(c).AlignItems(ui.Center).Gap(5).Padding(0, 0, 4, 0).Children(func() {
		ui.Row(c).AlignItems(ui.Baseline).Center().Gap(0).Wrap().Children(func() {
			ui.Text(c, i18n.T("What should we build in")).FontSize(28).FontWeight(600).
				TextAlign(ui.Center).TextColor(t.Text)
			brandWordmark(c, 30).MarginStart(8)
			ui.Text(c, "?").FontSize(28).FontWeight(600).TextColor(t.Text)
		})
		text(c, subtitle, 13.5, t.TextMuted)
	})
}

// newThreadContext is the row of choices under the composer: the project
// a thread starts in, and, for a Git project, where it checks out.
func (s *Session) newThreadContext(c *ui.Context) {
	t := c.Theme()
	projectID := s.newThreadProj
	projectLabel := i18n.T("Choose project")
	gitProject := false
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Button(c, "").Label(i18n.T("Project: ") + projectLabel).Children(func() {
			ui.Icon(c, lucide.Folder()).FontSize(14)
			ui.Text(c, projectLabel).SingleLine().MaxWidth(220)
		}).PopoverMenuButton(func(m *ui.Menu) {
			for _, project := range s.projects {
				project := project
				if project.ID == projectID {
					projectLabel = project.Name
					gitProject = project.IsGit
				}
				if m.Item(project.Name).Checked(s.newThreadProj == project.ID).Chosen() {
					s.selectNewThreadProject(project.ID)
				}
			}
			m.Separator()
			if m.Item(i18n.T("New project")).Chosen() {
				s.openNewProjectForThread()
			}
		})
		if gitProject {
			s.newThreadWorktreeChoice(c)
		}
	})
	_ = t
}

// newThreadWorktreeChoice picks where a new Git thread checks out: the
// project's checkout, a fresh worktree, or one that exists already.
func (s *Session) newThreadWorktreeChoice(c *ui.Context) {
	t := c.Theme()
	worktreeLabel := i18n.T("Current checkout")
	if s.newThreadWT {
		worktreeLabel = i18n.T("New worktree")
	} else if s.newThreadExistingWT != "" {
		worktreeLabel = i18n.T("Previous worktree")
	}
	ui.Button(c, "").Label(i18n.T("Worktree: ") + worktreeLabel).Children(func() {
		ui.Icon(c, lucide.GitFork()).FontSize(14)
		ui.Text(c, worktreeLabel).SingleLine().MaxWidth(200)
	}).PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(i18n.T("Current checkout")).Checked(!s.newThreadWT && s.newThreadExistingWT == "").Chosen() {
			s.newThreadWT, s.newThreadExistingWT = false, ""
		}
		if m.Item(i18n.T("New worktree")).Checked(s.newThreadWT).Chosen() {
			s.newThreadWT, s.newThreadExistingWT = true, ""
		}
		shown := false
		for _, branch := range s.newThreadBranches {
			branch := branch
			if branch.Current || strings.TrimSpace(branch.WorktreePath) == "" {
				continue
			}
			if !shown {
				m.Separator()
				shown = true
			}
			if m.Item(i18n.T("Previous worktree · ") + branch.Name).Checked(s.newThreadExistingWT == branch.WorktreePath).Chosen() {
				s.newThreadWT, s.newThreadExistingWT, s.newThreadBase = false, branch.WorktreePath, branch.Name
			}
		}
	})
	ui.Column(c).Grow(1).MinWidth(220).Children(func() {
		ui.Combobox(c, &s.newThreadBase, s.branchNames()).Label(i18n.T("Branch")).
			Placeholder(i18n.T("Choose a branch")).
			Disabled(s.newThreadExistingWT != "" || s.newThreadBranchesBusy)
	})
	status, statusColor := "", t.TextMuted
	switch {
	case s.newThreadBranchesBusy:
		status = i18n.T("Loading branches…")
	case s.newThreadBranchesErr != "":
		status, statusColor = s.newThreadBranchesErr, t.Danger
	case len(s.branchNames()) == 0:
		status = i18n.T("This project has no local branches.")
	case s.newThreadExistingWT != "":
		status = i18n.T("This thread will reuse the selected worktree.")
	case !s.newThreadWT:
		status = i18n.T("The selected branch will be checked out in the project.")
	default:
		status = i18n.T("The new worktree branch is named from the thread title.")
	}
	ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Icon(c, lucide.GitBranch()).FontSize(14).TextColor(statusColor)
		text(c, status, 10.5, statusColor)
	})
}

// branchNames lists the branches a new worktree can start from.
func (s *Session) branchNames() []string {
	names := make([]string, 0, len(s.newThreadBranches))
	for _, branch := range s.newThreadBranches {
		names = append(names, branch.Name)
	}
	return names
}

// newThreadControls are the agent, model and permission pickers under the
// new thread's composer.
func (s *Session) newThreadControls(c *ui.Context) {
	ui.Row(c).WidthPercent(100).MinWidth(0).AlignItems(ui.Center).Gap(2).Children(func() {
		ui.Spacer(c)
		ui.Button(c, "").Label("").Label(i18n.T("Agent") + " " + s.newThreadAgentLabel()).Children(func() {
			ui.Icon(c, lucide.Bot()).FontSize(14)
			ui.Text(c, s.newThreadAgentLabel()).SingleLine().MaxWidth(180)
		}).PopoverMenuButton(func(m *ui.Menu) {
			for _, candidate := range s.providers {
				candidate := candidate
				label := candidate.Name
				if !candidate.Installed {
					label += i18n.T(" (missing)")
				}
				if m.Item(label).Checked(s.newThreadProv == candidate.ID).Disabled(!candidate.Installed).Chosen() {
					s.newThreadProv = candidate.ID
					modelID := ""
					for _, model := range s.allModels() {
						if model.Provider == candidate.ID {
							modelID = model.ID
							break
						}
					}
					s.newThreadModel = modelID
				}
			}
		})
		ui.Button(c, "").Label("").Label(i18n.T("Model")).Children(func() {
			ui.Icon(c, lucide.Cpu()).FontSize(14)
			ui.Text(c, shortModel(s.newThreadModelLabel())).SingleLine().MaxWidth(220)
		}).PopoverMenuButton(func(m *ui.Menu) {
			for _, model := range s.allModels() {
				model := model
				label := model.Name
				if model.Provider != s.newThreadProv {
					label += " · " + model.Provider
				}
				if m.Item(label).Checked(s.newThreadModel == model.ID && s.newThreadProv == model.Provider).Chosen() {
					s.newThreadModel, s.newThreadProv = model.ID, model.Provider
				}
			}
		})
		ui.Button(c, "").Label("").Label(i18n.T("Permission")).Children(func() {
			ui.Icon(c, lucide.KeyRound()).FontSize(14)
			ui.Text(c, string(s.newThreadPerm)).SingleLine()
		}).PopoverMenuButton(func(m *ui.Menu) {
			for _, permission := range []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
				proto.PermissionAuto, proto.PermissionFullAccess} {
				permission := permission
				if m.Item(string(permission)).Checked(s.newThreadPerm == permission).Chosen() {
					s.newThreadPerm = permission
				}
			}
		})
		ui.Button(c, "").Label("").Label(i18n.T("Worktree")).Children(func() {
			ui.Icon(c, lucide.GitBranch()).FontSize(14)
			ui.Text(c, s.newThreadWorktreeLabel()).SingleLine().MaxWidth(200)
		}).PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(i18n.T("The project's checkout")).Checked(!s.newThreadWT && s.newThreadExistingWT == "").Chosen() {
				s.newThreadWT, s.newThreadExistingWT = false, ""
			}
			if s.newThreadWT {
				if m.Item(i18n.T("Worktree: ") + s.newThreadWorktreeLabel()).Checked(true).Chosen() {
					s.newThreadWT = true
				}
				if m.Item(i18n.T("New worktree…")).Chosen() {
					s.openNewBranch()
				}
			}
		})
	})
}

// newThreadAgentLabel names the agent a new thread starts.
func (s *Session) newThreadAgentLabel() string {
	for _, candidate := range s.providers {
		if candidate.ID == s.newThreadProv {
			return candidate.Name
		}
	}
	return "Agent"
}

// newThreadModelLabel names the model a new thread starts.
func (s *Session) newThreadModelLabel() string {
	for _, model := range s.allModels() {
		if model.ID == s.newThreadModel && model.Provider == s.newThreadProv {
			return model.Name
		}
	}
	return "Model"
}

// newThreadWorktreeLabel says where a new thread checks out.
func (s *Session) newThreadWorktreeLabel() string {
	if s.newThreadExistingWT != "" {
		return s.newThreadExistingWT
	}
	if s.newThreadWT {
		return i18n.T("New worktree")
	}
	return i18n.T("Checkout")
}
