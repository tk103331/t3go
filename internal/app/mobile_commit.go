package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// mobileCommitSheet is the phone's commit sheet: the changed files it
// sends, the message, and the branch it commits on. It is the phone's
// answer to the desktop's Changes dialog, which carries the same
// actions.
func (s *Session) mobileCommitSheet(c *ui.Context) {
	modal(c, &s.mobileCommitOpen, func() {
		t := c.Theme()
		dialogPanel(c, 460).Children(func() {
			text(c, i18n.T("Commit & push"), 16, t.Text, 600)
			git := s.detail.Git
			if git == nil || !git.Repo {
				faint(c, i18n.T("This thread is not in a Git repository."), 12)
				dialogFooter(c, func() {
					if ui.Button(c, i18n.T("Close")).Clicked() {
						s.mobileCommitOpen = false
					}
				})
				return
			}
			branch := git.Branch
			if branch == "" {
				branch = i18n.T("detached HEAD")
			}
			if s.mobileCommitNewBranch {
				ui.Field(c, i18n.T("Branch name"), func() {
					ui.TextInput(c, &s.branchName).Placeholder(i18n.T("Branch name"))
				})
			} else {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Icon(c, lucide.GitBranch()).FontSize(14).TextColor(t.TextMuted)
					ui.Text(c, branch).FontSize(12).FontWeight(600).TextColor(t.Text).SingleLine()
				})
			}
			s.mobileCommitFileList(c)
			ui.Field(c, i18n.T("Commit message"), func() {
				ui.TextInput(c, &s.gitMessage).Placeholder(i18n.T("Commit message (defaults to an update)")).MinHeight(controlMinHeight(c))
			})
			if s.gitNote != "" {
				faint(c, s.gitNote, 11.5)
			}
			dialogFooter(c, func() {
				if ui.Button(c, i18n.T("Cancel")).MinHeight(controlMinHeight(c)).Clicked() {
					s.mobileCommitOpen = false
				}
				commit := ui.Button(c, "").Label(i18n.T("Commit")).MinHeight(controlMinHeight(c)).Children(func() {
					ui.Icon(c, lucide.Check()).FontSize(15)
					ui.Text(c, i18n.T("Commit"))
				})
				if s.gitBusy {
					commit.Disabled(true)
				}
				commit.OnClick(func() { s.commitMobileFiles(proto.GitCommit) })
				push := ui.PrimaryButton(c, "").Label(i18n.T("Commit & push")).MinHeight(controlMinHeight(c)).Children(func() {
					ui.Icon(c, lucide.ArrowUp()).FontSize(15)
					ui.Text(c, i18n.T("Commit & push"))
				})
				if s.gitBusy {
					push.Disabled(true)
				}
				push.OnClick(func() { s.commitMobileFiles(proto.GitCommitPush) })
			})
		})
	})
}

// mobileCommitFileList lists the checkout's changed files, with the ones
// the commit carries picked.
func (s *Session) mobileCommitFileList(c *ui.Context) {
	t := c.Theme()
	selected := map[string]bool{}
	for _, path := range s.mobileCommitFiles {
		selected[path] = true
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			text(c, i18n.T("Changed files"), 12, t.Text, 600).Grow(1)
			all := len(s.workingFiles) > 0 && len(s.mobileCommitFiles) == len(s.workingFiles)
			label := i18n.T("Select all")
			if all {
				label = i18n.T("Deselect all")
			}
			ui.Button(c, label).MinHeight(controlMinHeight(c)).OnClick(func() { s.setAllMobileCommitFiles(!all) })
		})
		if len(s.workingFiles) == 0 {
			if s.gitBusy {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label(i18n.T("Loading changes"))
				})
			} else {
				faint(c, i18n.T("No changed files."), 12)
			}
			return
		}
		ui.Scroll(c).MaxHeight(200).Gap(2).Children(func() {
			for _, file := range s.workingFiles {
				file := file
				row := clickableRow(c, file.Path, func() {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						if selected[file.Path] {
							ui.Icon(c, lucide.Check()).FontSize(12).TextColor(t.Accent)
						} else {
							ui.Box(c).Size(14, 14).Radius(4).Border(1, t.Border)
						}
						ui.Text(c, file.Path).FontSize(12).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
						if file.Additions > 0 {
							text(c, "+"+strconv.Itoa(file.Additions), 11, t.Success)
						}
						if file.Deletions > 0 {
							text(c, "-"+strconv.Itoa(file.Deletions), 11, t.Danger)
						}
					})
				}).Padding(6, 8).MinHeight(controlMinHeight(c)).Radius(6)
				if row.Clicked() {
					s.toggleMobileCommitFile(file.Path)
				}
			}
		})
	})
}
