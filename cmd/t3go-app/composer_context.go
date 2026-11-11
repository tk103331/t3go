package main

import (
	"strings"
	"unicode"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

// slashCommand is one app command the composer offers after a slash.
type slashCommand struct {
	Name string
	Desc string
	Icon func() *core.SVG
	Run  func()
}

// composerSuggestions shows @ file mentions or slash commands above the
// composer as they are typed.
func (a *app) composerSuggestions(cx *core.Scope) core.Node {
	draft := a.draft.Get()
	if query, ok := slashQuery(draft); ok {
		var rows []core.Node
		appCommands := map[string]bool{}
		for _, command := range a.slashCommands() {
			appCommands[command.Name] = true
			if !strings.Contains(strings.ToLower(command.Name), strings.ToLower(query)) {
				continue
			}
			command := command
			rows = append(rows, a.suggestionRow(cx, command.Icon, "/"+command.Name, command.Desc,
				"slash-command-"+command.Name, func() {
					a.setDraft("")
					command.Run()
				}))
		}
		for _, command := range a.agentCommands.Get() {
			command := command
			if appCommands[command.Name] || !strings.Contains(strings.ToLower(command.Name), strings.ToLower(query)) {
				continue
			}
			detail := command.Source
			if command.Kind != "" {
				detail = command.Kind + " · " + command.Source
			}
			rows = append(rows, a.suggestionRow(cx, lucide.Sparkles, "/"+command.Name, detail,
				"agent-command-"+command.Source+"-"+command.Name, func() {
					a.pickAgentCommand(command)
				}))
		}
		if len(rows) == 0 {
			return core.View{}
		}
		return a.suggestionCard(cx, "Skills & commands", rows)
	}
	start, query, ok := activeMention(draft)
	if !ok {
		return core.View{}
	}
	_ = start
	if a.fileIndexBusy.Get() && len(a.fileIndex.Get()) == 0 {
		return a.suggestionCard(cx, "Files", []core.Node{ui.Text("Searching files…", 11.5, ui.Regular, ui.ColorsOf(cx).Faint)})
	}
	var rows []core.Node
	for _, entry := range a.fileIndex.Get() {
		if entry.Directory {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Path), strings.ToLower(query)) {
			continue
		}
		entry := entry
		rows = append(rows, a.suggestionRow(cx, lucide.FileText, entry.Path, humanSize(entry.Size),
			"mention-file-"+entry.Path, func() { a.insertMention(entry.Path) }))
		if len(rows) >= 8 {
			break
		}
	}
	if len(rows) == 0 {
		return core.View{}
	}
	return a.suggestionCard(cx, "Files", rows)
}

func (a *app) suggestionCard(cx *core.Scope, title string, rows []core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border(),
		CornerRadius: 8, Padding: core.All(4), Gap: 2}, Children: []core.Node{
		core.Text{Content: title, Style: core.Style{FontSize: 10, FontWeight: ui.Semibold, Padding: core.Symmetric(2, 8)},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		core.Scroll{Style: core.Style{MaxHeight: core.Pt(240)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 1}, Children: rows},
		}},
	}}
}

func (a *app) suggestionRow(cx *core.Scope, icon func() *core.SVG, label, detail, testID string, press func()) core.Node {
	colors := ui.ColorsOf(cx)
	return base.Button{OnPress: func(core.PressEvent) { press() }, TestID: testID,
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pct(100),
			Padding: core.Symmetric(5, 8), CornerRadius: 6, Cursor: core.CursorDefault},
		StyleFunc: func() core.Style {
			return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
		},
		Children: []core.Node{
			ui.Icon(icon, 13, colors.Muted),
			core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1, FlexShrink: 1, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: detail, MaxLines: 1, Style: core.Style{FontSize: 10.5, FlexShrink: 0},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}}
}

// slashQuery returns what follows a leading slash, while it is one word.
func slashQuery(draft string) (string, bool) {
	trimmed := strings.TrimSpace(draft)
	if !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\n") {
		return "", false
	}
	query := strings.TrimPrefix(trimmed, "/")
	if strings.ContainsAny(query, " \t") {
		return "", false
	}
	return query, true
}

// activeMention returns where a mention starts and what follows its @,
// while that is one word.
func activeMention(draft string) (int, string, bool) {
	at := strings.LastIndex(draft, "@")
	if at < 0 {
		return 0, "", false
	}
	if at > 0 {
		before := rune(draft[at-1])
		if !unicode.IsSpace(before) && before != '(' && before != '[' {
			return 0, "", false
		}
	}
	query := draft[at+1:]
	if strings.ContainsAny(query, " \t\n,)]") {
		return 0, "", false
	}
	return at, query, true
}

// composerMentions shows the files named with @ in the draft.
func (a *app) composerMentions(cx *core.Scope) core.Node {
	paths := mentionPaths(a.draft.Get())
	if len(paths) == 0 {
		return core.View{}
	}
	colors := ui.ColorsOf(cx)
	var chips []core.Node
	for _, path := range paths {
		path := path
		chips = append(chips, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4,
			Padding: core.Symmetric(3, 7), Background: colors.Field(), CornerRadius: 7, MaxWidth: core.Pt(280)}, Children: []core.Node{
			ui.Icon(lucide.FileText, 11, colors.Muted),
			core.Text{Content: path, MaxLines: 1, Style: core.Style{FontSize: 11, FontFamily: "Menlo, monospace", FlexShrink: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			ui.IconButton{Icon: lucide.X, Label: "Remove " + path, Size: 17, IconSize: 9, Tint: colors.Faint,
				TestID: "mention-chip-" + path, OnPress: func() { a.removeMention(path) }},
		}})
	}
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4, Padding: core.Edges{Bottom: 4}}, Children: chips}
}

// mentionPaths reads the @ paths in the draft without duplicates.
func mentionPaths(draft string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, field := range strings.Fields(draft) {
		path := strings.TrimPrefix(field, "@")
		if path == field || path == "" {
			continue
		}
		path = strings.TrimRight(path, ".,;:!?)]")
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

// removeMention takes one @ path out of the draft.
func (a *app) removeMention(path string) {
	draft := a.draft.GetUntracked()
	draft = strings.Replace(draft, "@"+path, "", 1)
	a.setDraft(strings.Join(strings.Fields(draft), " "))
}

// insertMention replaces the mention being typed with one file.
func (a *app) insertMention(path string) {
	draft := a.draft.GetUntracked()
	at, _, ok := activeMention(draft)
	if !ok {
		return
	}
	a.setDraft(draft[:at] + "@" + path + " ")
}

// slashCommands are T3Go's app-level composer commands.
func (a *app) slashCommands() []slashCommand {
	return []slashCommand{
		{Name: "new", Desc: "Start a thread", Icon: lucide.MessageSquarePlus, Run: a.openNewThread},
		{Name: "settings", Desc: "Environment defaults", Icon: lucide.Settings, Run: a.openSettings},
		{Name: "files", Desc: "Browse the checkout", Icon: lucide.Folder, Run: func() { a.showSurface("files") }},
		{Name: "diff", Desc: "Working tree changes", Icon: lucide.GitCompare, Run: func() { a.showSurface("diff") }},
		{Name: "graph", Desc: "Commit history", Icon: lucide.GitBranch, Run: func() { a.showSurface("graph") }},
		{Name: "pr", Desc: "Pull request", Icon: lucide.GitPullRequest, Run: func() { a.showSurface("pr") }},
		{Name: "terminal", Desc: "Open a shell", Icon: lucide.Terminal, Run: func() { a.showSurface("terminal") }},
		{Name: "browser", Desc: "Open a web page", Icon: lucide.Globe, Run: func() { a.showSurface("browser") }},
		{Name: "stop", Desc: "Stop the running turn", Icon: lucide.CircleStop, Run: a.stop},
		{Name: "archive", Desc: "Archive this thread", Icon: lucide.Archive, Run: a.archiveCurrentThread},
	}
}

// showSurface opens the right panel on a surface.
func (a *app) showSurface(name string) {
	a.setRightOpen(true)
	a.openSurface(name)
}

// archiveCurrentThread archives the open thread.
func (a *app) archiveCurrentThread() {
	id := a.selected.GetUntracked()
	for _, thread := range a.threads.GetUntracked() {
		if thread.ID == id {
			a.setArchived(thread, true)
			return
		}
	}
}
