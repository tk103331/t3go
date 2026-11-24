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
	_, height := c.Size()
	// A scroll child's percentage height has no viewport to resolve
	// against. Give the welcome group room to center in a tall window,
	// while letting a short window scroll all of its controls.
	minHeight := max(float32(0), min(height-windowTitleBarHeight(c)-48, 720))
	ui.Column(c).Fill().Padding(24, 28).Children(func() {
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).WidthPercent(100).MinHeight(minHeight).
				Center().Children(func() {
				ui.Column(c).WidthPercent(100).MaxWidth(800).Gap(24).Children(func() {
					s.newThreadHero(c)
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						s.newThreadContext(c)
						s.composerCard(c, composerSpec{
							Draft:          &s.newThreadPrompt,
							Label:          i18n.T("New thread message"),
							Placeholder:    i18n.T("Ask anything. @ files, $ skills, / commands"),
							EditorMinLines: 3,
							AllowEmpty:     true,
							SendDisabled:   s.threadBusy || s.newThreadProj == "",
							SendLabel:      i18n.T("Create thread"),
							OnSend: func() {
								s.createPreparedThread(strings.TrimSpace(s.newThreadPrompt) != "")
							},
							Attachments: s.newThreadAttachmentChips,
							Accessory:   func() { s.pickNewThreadAttachments() },
							Controls:    s.newThreadControls,
							// The page's content room decides whether the
							// agent, model and permission pills share the
							// Create thread row or take one above it.
							ContentInset:     80,
							ContentMaxWidth:  800,
							ControlsMinWidth: s.newThreadControlsMinWidth(),
							OnDropFiles:      s.attachNewThreadPaths,
							OnPaste:          s.pasteInto,
						})
					})
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
	ui.Column(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Baseline).Center().Gap(0).Wrap().Children(func() {
			ui.Text(c, i18n.T("What should we build in")).FontSize(22).FontWeight(600).
				TextAlign(ui.Center).TextColor(t.Text)
			brandWordmark(c, 24).MarginStart(8)
			ui.Text(c, "?").FontSize(22).FontWeight(600).TextColor(t.Text)
		})
		text(c, subtitle, 12.5, t.TextMuted).TextAlign(ui.Center).MaxLines(2)
	})
}

// newThreadContext keeps project, checkout and branch in one wrapping
// workspace bar immediately above the message.
func (s *Session) newThreadContext(c *ui.Context) {
	projectID := s.newThreadProj
	projectLabel := i18n.T("Choose project")
	gitProject := false
	for _, project := range s.projects {
		if project.ID == projectID {
			projectLabel = project.Name
			gitProject = project.IsGit
			break
		}
	}
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(6).Children(func() {
			toolbarButton(c, "").Label(i18n.T("Project: ")+projectLabel).
				TextColor(c.Theme().Text).Border(1, c.Theme().Border).Radius(8).
				MaxWidthPercent(100).MinWidth(0).Tooltip(i18n.T("Project: ") + projectLabel).Children(func() {
				ui.Icon(c, lucide.Folder()).FontSize(14)
				ui.Text(c, projectLabel).FontSize(12).SingleLine().MinWidth(0).MaxWidth(160)
				ui.Icon(c, lucide.ChevronDown()).FontSize(10)
			}).PopoverMenuButton(func(m *ui.Menu) {
				for _, project := range s.projects {
					project := project
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
		if gitProject {
			s.newThreadWorkspaceNote(c)
		}
	})
}

// newThreadWorktreeChoice picks where a new Git thread checks out: the
// project's checkout, a fresh worktree, or one that exists already.
func (s *Session) newThreadWorktreeChoice(c *ui.Context) {
	worktreeLabel := i18n.T("Current checkout")
	if s.newThreadWT {
		worktreeLabel = i18n.T("New worktree")
	} else if s.newThreadExistingWT != "" {
		worktreeLabel = i18n.T("Previous worktree")
	}
	toolbarButton(c, "").Label(i18n.T("Worktree: ")+worktreeLabel).
		Border(1, c.Theme().Border).Radius(8).MaxWidthPercent(100).MinWidth(0).
		Tooltip(i18n.T("The selected branch will be checked out in the project.")).Children(func() {
		ui.Icon(c, lucide.GitFork()).FontSize(14)
		ui.Text(c, worktreeLabel).FontSize(12).SingleLine().MinWidth(0).MaxWidth(160)
		ui.Icon(c, lucide.ChevronDown()).FontSize(10)
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
	ui.Combobox(c, &s.newThreadBase, s.branchNames()).Label(i18n.T("Branch")).
		Width(180).MaxWidthPercent(100).MinWidth(0).MinHeight(controlMinHeight(c)).
		Placeholder(i18n.T("Choose a branch")).
		Tooltip(i18n.T("The selected branch will be checked out in the project.")).
		Disabled(s.newThreadExistingWT != "" || s.newThreadBranchesBusy)
}

// Show guidance when the choice needs attention. The normal checkout's
// explanation stays available on the controls without occupying a row.
func (s *Session) newThreadWorkspaceNote(c *ui.Context) {
	t := c.Theme()
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
		return
	default:
		status = i18n.T("The new worktree branch is named from the thread title.")
	}
	ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Icon(c, lucide.GitBranch()).FontSize(14).TextColor(statusColor)
		text(c, status, 11.5, statusColor).Grow(1).MinWidth(0).MaxLines(3)
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

// newThreadControlsMinWidth estimates the room the agent, model and
// permission pills need beside the paperclip and the Create thread
// button, so the page can put them on their own row when it is narrower.
func (s *Session) newThreadControlsMinWidth() float32 {
	labelWidth := func(label string) float32 { return float32(len([]rune(label))) * 7.2 }
	const pill = float32(16 + 13 + 10 + 12)
	const attach, send, gaps = float32(30), float32(32), float32(24)
	need := labelWidth(s.newThreadAgentLabel()) + labelWidth(s.newThreadModelLabel()) +
		labelWidth(string(s.newThreadPerm)) + 3*pill + attach + send + gaps
	return max(460, need)
}

// newThreadControls are the agent, model and permission pickers under the
// new thread's composer.
func (s *Session) newThreadControls(c *ui.Context) {
	ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(2).Children(func() {
		toolbarButton(c, "").Label(i18n.T("Agent") + " " + s.newThreadAgentLabel()).MaxWidthPercent(100).MinWidth(0).Children(func() {
			ui.Icon(c, lucide.Bot()).FontSize(13)
			ui.Text(c, s.newThreadAgentLabel()).FontSize(11.5).SingleLine().MaxWidth(110)
			ui.Icon(c, lucide.ChevronDown()).FontSize(10)
		}).PopoverMenuButton(func(m *ui.Menu) {
			for _, candidate := range s.providers {
				candidate := candidate
				label := candidate.Name
				if !candidate.Installed {
					label += i18n.T(" (missing)")
				} else if !providerUsable(candidate) {
					label += i18n.T(" (disabled)")
				}
				if m.Item(label).Checked(s.newThreadProv == candidate.ID).Disabled(!providerUsable(candidate)).Chosen() {
					s.newThreadProv = candidate.ID
					s.newThreadModel = s.defaultProviderModel(candidate.ID)
				}
			}
		})
		modelButton := toolbarButton(c, "").Label(i18n.T("Model")).Tooltip(s.newThreadModelLabel()).MaxWidthPercent(100).MinWidth(0).Children(func() {
			ui.Icon(c, lucide.Cpu()).FontSize(13)
			ui.Text(c, shortModel(s.newThreadModelLabel())).FontSize(11.5).SingleLine().MinWidth(0).MaxWidth(140)
			ui.Icon(c, lucide.ChevronDown()).FontSize(10)
		})
		s.modelPicker(c, modelButton, s.newThreadProv, s.newThreadModel, func(model proto.Model) {
			s.newThreadModel, s.newThreadProv = model.ID, model.Provider
		})
		s.configOptions(c, []configOption{s.newThreadPermissionOption()})
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
