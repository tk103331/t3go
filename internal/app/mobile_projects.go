package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
)

// mobileProjectsPage lists projects even when they have no threads yet.
// Each row keeps its owning environment for opening and managing it.
func (s *Session) mobileProjectsPage(c *ui.Context) {
	t := c.Theme()
	sessions := []*Session{s}
	if s.manager != nil {
		sessions = nil
		for _, profile := range s.manager.profiles {
			if owner := s.manager.sessions[profile.ID]; owner != nil {
				sessions = append(sessions, owner)
			}
		}
	}
	ui.Scroll(c).Fill().Padding(12, 16, 24, 16).Gap(16).Children(func() {
		count := 0
		for _, owner := range sessions {
			if len(owner.projects) == 0 {
				continue
			}
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				text(c, projectEnvironmentLabel(owner), 12, t.TextMuted, 500)
				for _, project := range owner.projects {
					project := project
					count++
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Padding(4, 8, 4, 0).
						Radius(12).Background(t.Surface).Children(func() {
						row := clickableRow(c, project.Name, func() {
							ui.Icon(c, lucide.Folder()).FontSize(20).TextColor(t.TextMuted)
							ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
								text(c, project.Name, 14, t.Text, 500).SingleLine().MinWidth(0)
								text(c, project.Path, 11, t.TextMuted).SingleLine().MinWidth(0).Tooltip(project.Path)
							})
						}).Grow(1).MinWidth(0).MinHeight(60).Padding(12)
						row.OnClick(owner.inEnvironment(func() { owner.openNewThreadIn(project.ID) }))
						iconBtn(c, lucide.Ellipsis(), i18n.T("Project actions")+" "+project.Name, nil).
							Shrink(0).PopoverMenuButton(func(m *ui.Menu) { owner.projectMenu(m, project) })
					})
				}
			})
		}
		if count == 0 {
			ui.Column(c).Gap(12).Padding(16, 0).Children(func() {
				text(c, i18n.T("Projects"), 18, t.Text, 600)
				text(c, i18n.T("Start a thread in one of your projects, or add a project."), 13, t.TextMuted)
				ui.PrimaryButton(c, i18n.T("New project")).MinHeight(controlMinHeight(c)).OnClick(s.openNewProject)
			})
		}
	})
}
