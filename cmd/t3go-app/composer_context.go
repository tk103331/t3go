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
		for _, command := range a.slashCommands() {
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
		if len(rows) == 0 {
			return core.View{}
		}
		return a.suggestionCard(cx, "Commands", rows)
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
