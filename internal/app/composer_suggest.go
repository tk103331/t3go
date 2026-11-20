package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// slashCommand is one app command the composer offers after a slash.
type slashCommand struct {
	Name string
	Desc string
	Run  func()
}

// slashCommands are T3Go's app-level composer commands.
func (s *Session) slashCommands() []slashCommand {
	return []slashCommand{
		{Name: "new", Desc: i18n.T("Start a thread"), Run: s.openNewThread},
		{Name: "settings", Desc: i18n.T("Environment defaults"), Run: s.openSettings},
		{Name: "files", Desc: i18n.T("Browse the checkout"), Run: func() { s.showSurface("files") }},
		{Name: "diff", Desc: i18n.T("Working tree changes"), Run: func() { s.showSurface("diff") }},
		{Name: "graph", Desc: i18n.T("Commit history"), Run: func() { s.showSurface("graph") }},
		{Name: "pr", Desc: i18n.T("Pull request"), Run: func() { s.showSurface("pr") }},
		{Name: "terminal", Desc: i18n.T("Open a shell"), Run: func() { s.showSurface("terminal") }},
		{Name: "browser", Desc: i18n.T("Open a web page"), Run: func() { s.showSurface("browser") }},
		{Name: "stop", Desc: i18n.T("Stop the running turn"), Run: s.stop},
		{Name: "archive", Desc: i18n.T("Archive this thread"), Run: s.archiveCurrentThread},
	}
}

// suggestionRow is one row of a suggestion card.
func suggestionRow(c *ui.Context, icon *ui.SVG, label, detail string, run func()) bool {
	t := c.Theme()
	row := ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(6, 8).Radius(6).FillWidth().Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(t.TextMuted)
		ui.Text(c, label).FontSize(12).Font("monospace").TextColor(t.Text).SingleLine()
		if detail != "" {
			ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
		}
	})
	if row.RowClicked() {
		run()
		return true
	}
	return false
}

// suggestionCard wraps suggestion rows.
func suggestionCard(c *ui.Context, title string, build func()) {
	t := c.Theme()
	ui.Column(c).Padding(6).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(1).Children(func() {
		faint(c, title, 10)
		build()
	})
}

// composerSuggestions shows @ file mentions or slash commands above the
// composer as they are typed.
func (s *Session) composerSuggestions(c *ui.Context) {
	draft := s.draft
	if query, ok := slashQuery(draft); ok {
		var matched bool
		suggestionCard(c, i18n.T("Skills & commands"), func() {
			var used = map[string]bool{}
			for _, command := range s.slashCommands() {
				if !strings.Contains(strings.ToLower(command.Name), strings.ToLower(query)) {
					continue
				}
				used[command.Name] = true
				command := command
				matched = suggestionRow(c, lucide.Sparkles(), "/"+command.Name, command.Desc, func() {
					s.setDraft("")
					command.Run()
				})
			}
			for _, command := range s.agentCommands {
				command := command
				if used[command.Name] || !strings.Contains(strings.ToLower(command.Name), strings.ToLower(query)) {
					continue
				}
				detail := command.Source
				if command.Kind != "" {
					detail = command.Kind + " · " + command.Source
				}
				matched = suggestionRow(c, lucide.Sparkles(), "/"+command.Name, detail, func() {
					s.pickAgentCommand(command)
				})
			}
		})
		if matched {
			return
		}
		return
	}
	if _, query, ok := activeMention(draft); ok {
		if s.fileIndexBusy && len(s.fileIndex) == 0 {
			suggestionCard(c, i18n.T("Files"), func() {
				faint(c, i18n.T("Searching files…"), 11.5)
			})
			return
		}
		var matched bool
		suggestionCard(c, i18n.T("Files"), func() {
			shown := 0
			for _, entry := range s.fileIndex {
				if shown >= 8 {
					break
				}
				if entry.Directory {
					continue
				}
				if query != "" && !strings.Contains(strings.ToLower(entry.Path), strings.ToLower(query)) {
					continue
				}
				entry := entry
				shown++
				matched = suggestionRow(c, lucide.FileText(), entry.Path, humanSize(entry.Size), func() {
					s.insertMention(entry.Path)
				})
			}
		})
		if matched {
			return
		}
	}
}

// composerMentions shows the files named with @ in the draft.
func (s *Session) composerMentions(c *ui.Context) {
	t := c.Theme()
	paths := mentionPaths(s.draft)
	if len(paths) == 0 {
		return
	}
	ui.Row(c).Wrap().Gap(4).Padding(0, 0, 4, 0).Children(func() {
		for _, path := range paths {
			path := path
			ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(3, 7).Radius(7).
				MaxWidth(280).Background(t.Background).Children(func() {
				ui.Icon(c, lucide.FileText()).FontSize(11).TextColor(t.TextMuted)
				ui.Text(c, path).FontSize(11).Font("monospace").TextColor(t.Text).SingleLine().MinWidth(0)
				ui.Button(c, "×").Label(i18n.T("Remove " + path)).Children(func() {
					ui.Icon(c, lucide.X()).FontSize(9)
				}).Tooltip(i18n.T("Remove " + path)).OnClick(func() { s.removeMention(path) })
			})
		}
	})
}

var _ = proto.AgentCommand{}
