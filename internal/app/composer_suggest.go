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

// suggestion is one row of the composer's floating list.
type suggestion struct {
	icon   *ui.SVG
	label  string
	detail string
	run    func()
}

// suggestionRow draws one row and reports whether it was chosen. The row
// keeps the composer's focus so a press and release land on it as it
// appears.
func suggestionRow(c *ui.Context, item suggestion) bool {
	row := clickableRow(c, item.label, func() {
		t := c.Theme()
		ui.Icon(c, item.icon).FontSize(14).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, item.label).FontSize(12).Font("monospace").TextColor(t.Text).SingleLine()
		if item.detail != "" {
			ui.Text(c, item.detail).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
		}
	}).Padding(6, 8).Radius(6).KeepFocus()
	if row.Hovered() {
		row.Background(c.Theme().SurfaceHover)
	}
	if row.Clicked() {
		item.run()
		return true
	}
	return false
}

// composerSuggestionKind is the query the floating list is built from:
// a slash command, or an @ file mention.
type composerSuggestionKind struct {
	kind  string
	query string
	title string
}

// composerSuggestionSet returns what the draft asks the floating list to
// show, or false when no @ or / query is active.
func (s *Session) composerSuggestionSet() (composerSuggestionKind, []suggestion, bool) {
	draft := s.draft
	if query, ok := slashQuery(draft); ok {
		kind := composerSuggestionKind{kind: "slash", query: query, title: i18n.T("Skills & commands")}
		var items []suggestion
		used := map[string]bool{}
		for _, command := range s.slashCommands() {
			if !strings.Contains(strings.ToLower(command.Name), strings.ToLower(query)) {
				continue
			}
			used[command.Name] = true
			command := command
			items = append(items, suggestion{icon: lucide.Sparkles(), label: "/" + command.Name, detail: command.Desc,
				run: func() {
					s.setDraft("")
					command.Run()
				}})
		}
		for _, command := range s.agentCommands {
			if used[command.Name] || !strings.Contains(strings.ToLower(command.Name), strings.ToLower(query)) {
				continue
			}
			command := command
			detail := command.Source
			if command.Kind != "" {
				detail = command.Kind + " · " + command.Source
			}
			items = append(items, suggestion{icon: lucide.Sparkles(), label: "/" + command.Name, detail: detail,
				run: func() { s.pickAgentCommand(command) }})
		}
		return kind, items, true
	}
	if _, query, ok := activeMention(draft); ok {
		kind := composerSuggestionKind{kind: "file", query: query, title: i18n.T("Files")}
		if s.fileIndexBusy && len(s.fileIndex) == 0 {
			return kind, nil, true
		}
		var items []suggestion
		for _, entry := range s.fileIndex {
			if entry.Directory {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(entry.Path), strings.ToLower(query)) {
				continue
			}
			entry := entry
			items = append(items, suggestion{icon: lucide.FileText(), label: entry.Path, detail: humanSize(entry.Size),
				run: func() { s.insertMention(entry.Path) }})
		}
		return kind, items, true
	}
	return composerSuggestionKind{}, nil, false
}

// composerSuggestPopover floats the @ file or / command list above the
// composer: a fixed height keeps the transcript and composer steady, and
// a long list scrolls inside the panel.
func (s *Session) composerSuggestPopover(c *ui.Context, anchor ui.Element) {
	kind, items, ok := s.composerSuggestionSet()
	if !ok {
		return
	}
	state := ui.Local(anchor, "composer-suggest", func() composerSuggestState {
		return composerSuggestState{open: true, kind: kind.kind, query: kind.query}
	})
	// A changed query reopens a list the pointer dismissed; a press
	// outside closes it until the next character is typed.
	if state.kind != kind.kind || state.query != kind.query {
		state.kind, state.query, state.open = kind.kind, kind.query, true
	}
	if !state.open {
		return
	}
	ui.PopoverBase(c, anchor, &state.open, func(panel ui.Element) {
		t := c.Theme()
		width, height := c.Size()
		top, right, bottom, left := c.SafeArea()
		bounds := anchor.Bounds()
		above := max(float32(0), bounds.Y-top-8)
		below := max(float32(0), height-bottom-bounds.Y-bounds.H-8)
		room := above
		// The list prefers the room above the composer; a keyboard on a
		// phone leaves more there than below.
		if above >= 140 || above >= below {
			panel.AttachTo(anchor, ui.AnchorTopStart, ui.AnchorBottomStart)
		} else {
			room = below
			panel.AttachTo(anchor, ui.AnchorBottomStart, ui.AnchorTopStart)
		}
		panel.Width(min(560, width-left-right-24)).MaxWidth(width-left-right-24).
			Padding(6).Gap(4).Radius(10).Border(1, t.Border).Background(t.Surface).
			Shadow(0, 4, 16, 0, t.Text.Alpha(0.12))
		faint(c, kind.title, 10).Padding(2, 6)
		if len(items) == 0 {
			if s.fileIndexBusy && kind.kind == "file" {
				faint(c, i18n.T("Searching files…"), 11.5).Padding(6, 8)
			} else {
				faint(c, i18n.T("No matches"), 11.5).Padding(6, 8)
			}
			return
		}
		listHeight := max(float32(120), min(float32(280), room-40))
		ui.Scroll(c).FillWidth().MaxHeight(listHeight).Gap(1).Children(func() {
			for _, item := range items {
				item := item
				if suggestionRow(c, item) {
					state.open = false
				}
			}
		})
	})
}

// composerSuggestState remembers the floating list's dismissal so a
// press outside it does not re-open on the next frame.
type composerSuggestState struct {
	open  bool
	kind  string
	query string
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
