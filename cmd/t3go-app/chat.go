package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/oneo-me/nsgo/ui/chat"
	"github.com/tk/t3go/internal/proto"
)

// turn group is one run's messages, so a turn is spaced from the next
// while its own rows stay close.
type turnGroup struct {
	ID       string
	Messages []proto.Message
	// Day starts a new date's messages, empty when they continue one.
	Day string
}

func groupMessages(messages []proto.Message) []turnGroup {
	var out []turnGroup
	index := map[string]int{}
	lastDay := ""
	for _, m := range messages {
		key := m.RunID
		if key == "" {
			key = m.ID
		}
		if i, ok := index[key]; ok {
			out[i].Messages = append(out[i].Messages, m)
			continue
		}
		index[key] = len(out)
		group := turnGroup{ID: key, Messages: []proto.Message{m}}
		day := m.CreatedAt.Local().Format("2006-01-02")
		if day != lastDay {
			group.Day = dayLabel(m.CreatedAt)
			lastDay = day
		}
		out = append(out, group)
	}
	return out
}

func (a *app) threadPane(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	groups := core.Each(
		func() []turnGroup { return groupMessages(a.messages.Get()) },
		func(g turnGroup) string { return g.ID },
		func(cx *core.Scope, item func() turnGroup, _ func() int) core.Node {
			return core.Func(func(cx *core.Scope) core.Node {
				g := item()
				rows := make([]core.Node, 0, len(g.Messages)+2)
				if g.Day != "" {
					rows = append(rows, a.daySeparator(cx, g.Day))
				}
				for _, m := range g.Messages {
					rows = append(rows, a.renderMessage(cx, m))
				}
				if footer := a.runFooter(cx, g.ID); footer != nil {
					rows = append(rows, footer)
				}
				return core.View{Style: core.Style{Gap: 10, MinWidth: core.Pt(0)}, Children: rows}
			})
		},
	)
	composer := chat.Composer{
		Draft: a.draft.Get, OnDraftChange: a.setDraft, OnPaste: a.pasteAttachments,
		Label: "Message", Placeholder: "Ask the agent…", SendLabel: "Send", StopLabel: "Stop",
		Running: a.running.Get, OnSend: a.send, OnStop: a.stop,
		Attachments:    func(cx *core.Scope) core.Node { return a.attachmentChips(cx) },
		HasAttachments: func() bool { return len(a.attachments.Get()) > 0 },
		Accessory: func(cx *core.Scope) core.Node {
			attach := ui.IconButton{Icon: lucide.Paperclip, Label: "Attach files", TestID: "attach", OnPress: a.pickAttachments}
			return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4}, Children: []core.Node{
				attach,
				core.Show{When: func() bool { return a.cx.Env().CanRecognizeSpeech() }, Then: func(*core.Scope) core.Node {
					return ui.IconButton{
						IconFunc: func() *core.SVG {
							if a.dictating.Get() {
								return lucide.MicOff()
							}
							return lucide.Mic()
						},
						LabelFunc: func() string {
							if a.dictating.Get() {
								return "Stop dictation"
							}
							return "Dictate"
						},
						Selected: a.dictating.Get,
						TestID:   "dictate",
						OnPress:  a.toggleDictation,
					}
				}},
			}}
		},
		OnDropFiles: a.attachPaths,
		Trailing:    a.composerControls,
		TestID:      "composer-card",
	}
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		core.View{Style: ui.FillRest, Children: []core.Node{
			chat.Conversation{State: a.conv, JumpLabel: "Jump to latest", TestID: "transcript", Children: []core.Node{
				core.Show{When: func() bool { return len(a.messages.Get()) == 0 }, Then: func(*core.Scope) core.Node {
					return core.View{Style: core.Style{Padding: core.Symmetric(40, 0), Gap: 6, AlignItems: core.AlignCenter}, Children: []core.Node{
						ui.Text("No messages yet", 14, ui.Semibold, colors.Muted),
						ui.Text("Ask the agent to change this project, and its work streams here.", 12.5, ui.Regular, colors.Faint),
					}}
				}},
				groups,
			}},
		}},
		core.View{Style: core.Style{Padding: core.Edges{Top: 8, Right: 16, Bottom: 16, Left: 16}, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(chat.ContentWidth)}, Children: []core.Node{
				a.queuedBlock(cx),
				core.Dynamic(a.composerMentions),
				core.Dynamic(a.composerSuggestions),
				core.View{TestID: "composer-drop", OnFileDrop: a.attachPaths, Children: []core.Node{composer}},
			}},
		}},
	}}
}

// composerControls are the thread's model and permission pickers.
func (a *app) composerControls(cx *core.Scope) core.Node {
	thread := a.detail.Get().Thread
	if thread.ID == "" {
		return core.View{}
	}
	colors := ui.ColorsOf(cx)
	var modelItems []core.Node
	modelLabel := shortModel(thread.Model)
	for _, model := range a.models.Get() {
		model := model
		label := model.Name
		if model.Provider != thread.Provider {
			label += " · " + model.Provider
		}
		if model.ID == thread.Model && model.Provider == thread.Provider {
			modelLabel = shortModel(model.Name)
		}
		modelItems = append(modelItems, ui.MenuItem{Text: label, TestID: "composer-model-" + model.ID,
			Checked: func() bool {
				return a.detail.Get().Thread.Model == model.ID && a.detail.Get().Thread.Provider == model.Provider
			},
			OnSelect: func() { a.setThreadModel(model) }})
	}
	var model core.Node = ui.Text(modelLabel, 10.5, ui.Regular, colors.Faint)
	if len(modelItems) > 0 {
		model = ui.Menu{TestID: "composer-model", MaxHeight: 320,
			Trigger:  ui.Button{Text: modelLabel, Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Model"},
			Children: modelItems}
	}
	var permItems []core.Node
	for _, permission := range []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
		proto.PermissionAuto, proto.PermissionFullAccess} {
		permission := permission
		permItems = append(permItems, ui.MenuItem{Text: string(permission), TestID: "composer-perm-" + string(permission),
			Checked:  func() bool { return a.detail.Get().Thread.Permission == permission },
			OnSelect: func() { a.setThreadPermission(permission) }})
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2}, Children: []core.Node{
		model,
		ui.Menu{TestID: "composer-permission", MaxHeight: 240,
			Trigger:  ui.Button{Text: string(thread.Permission), Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Permission"},
			Children: permItems},
	}}
}

func shortModel(name string) string {
	if name == "" {
		return "Model"
	}
	r := []rune(name)
	if len(r) > 18 {
		return string(r[:17]) + "…"
	}
	return name
}

// attachmentChips are the files the next message carries.
func (a *app) attachmentChips(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	files := a.attachments.Get()
	rows := make([]core.Node, 0, len(files))
	for _, f := range files {
		f := f
		lead := []core.Node{ui.Icon(lucide.Paperclip, 12, colors.Muted)}
		if strings.HasPrefix(f.MimeType, "image/") {
			lead = []core.Node{ui.Image{Source: core.ImageSource{URI: a.uploadURL(f.ID)}, ResizeMode: core.Cover, Label: f.Name,
				Style: core.Style{Width: core.Pt(34), Height: core.Pt(34), AspectRatio: 1, CornerRadius: 6}}}
		}
		children := append(lead,
			core.Text{Content: f.Name + " · " + humanSize(f.Size), MaxLines: 1, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			ui.Button{Text: "×", Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Remove attachment",
				TestID: "remove-attachment-" + f.ID, OnPress: func() { a.removeAttachment(f.ID) }},
		)
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6,
			Padding: core.Symmetric(4, 8), Background: colors.Field(), CornerRadius: 8, MaxWidth: core.Pt(280)}, Children: children})
	}
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: rows}
}

// attachmentSummary names files in one line.
func attachmentSummary(attachments []proto.Attachment) string {
	names := make([]string, 0, len(attachments))
	for _, a := range attachments {
		names = append(names, a.Name)
	}
	line := strings.Join(names, ", ")
	r := []rune(line)
	if len(r) > 80 {
		line = string(r[:79]) + "…"
	}
	return line
}

// humanSize writes a byte count in the largest fitting unit.
func humanSize(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	case size > 0:
		return fmt.Sprintf("%d B", size)
	}
	return "0 B"
}

// queuedBlock shows the prompts waiting for the running turn to end.
func (a *app) queuedBlock(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.Show{When: func() bool { return len(a.queue.Get()) > 0 }, Then: func(*core.Scope) core.Node {
		queue := a.queue.Get()
		rows := make([]core.Node, 0, len(queue))
		for i, prompt := range queue {
			i, prompt := i, prompt
			lines := []core.Node{
				core.Text{Content: prompt.Text, MaxLines: 2, Style: core.Style{FontSize: 12, LineHeight: 17},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}
			if len(prompt.Attachments) > 0 {
				lines = append(lines, ui.Text(attachmentSummary(prompt.Attachments), 11, ui.Regular, colors.Faint))
			}
			rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
				Padding: core.Symmetric(6, 10), Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{
				ui.Text("Queued", 11, ui.Semibold, colors.Faint),
				core.View{Style: core.Style{Direction: core.Column, Gap: 2, FlexGrow: 1, MinWidth: core.Pt(0)}, Children: lines},
				ui.Button{Text: "×", Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Remove queued message",
					TestID: fmt.Sprintf("queued-remove-%d", i), OnPress: func() { a.dropQueued(i) }},
			}})
		}
		return core.View{TestID: "queued", Style: core.Style{Gap: 4, Padding: core.Edges{Bottom: 6}}, Children: rows}
	}}
}

func (a *app) renderMessage(cx *core.Scope, m proto.Message) core.Node {
	colors := ui.ColorsOf(cx)
	if m.Role == proto.RoleUser {
		body := []core.Node{}
		if m.Content != "" {
			body = append(body, core.Text{Content: m.Content, Selectable: core.SelectableOn, Style: core.Style{FontSize: 13, LineHeight: 19},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }})
		}
		if len(m.Attachments) > 0 {
			files := make([]core.Node, 0, len(m.Attachments))
			for _, f := range m.Attachments {
				f := f
				if strings.HasPrefix(f.MimeType, "image/") {
					files = append(files, ui.Image{Source: core.ImageSource{URI: a.uploadURL(f.ID)}, ResizeMode: core.Cover, Label: f.Name,
						Style: core.Style{Width: core.Pt(72), Height: core.Pt(72), AspectRatio: 1, CornerRadius: 6}})
					continue
				}
				files = append(files, ui.Text(f.Name+" · "+humanSize(f.Size), 11, ui.Regular, colors.Faint))
			}
			body = append(body, core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 8}, Children: files})
		}
		return core.View{Style: core.Style{Direction: core.Column, AlignItems: core.AlignEnd, Gap: 2, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.View{Style: core.Style{MaxWidth: core.Pt(560), Background: colors.Inset(), Padding: core.Symmetric(10, 14), CornerRadius: 14,
				Direction: core.Column, Gap: 6}, Children: body},
			a.messageFooter(cx, m),
		}}
	}
	switch m.Kind {
	case proto.KindReasoning:
		return a.reasoningCard(cx, m)
	case proto.KindTool:
		return a.toolCard(cx, m)
	case proto.KindNotice:
		return core.Text{Content: m.Content, Selectable: core.SelectableOn, Style: core.Style{FontSize: 12, LineHeight: 17},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }}
	case proto.KindError:
		return core.View{Style: core.Style{Background: ui.Alpha(colors.Danger(), 0.09), Padding: core.Symmetric(8, 10), CornerRadius: 8}, Children: []core.Node{
			core.Text{Content: m.Content, Selectable: core.SelectableOn, Style: core.Style{FontSize: 12, LineHeight: 17},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }},
		}}
	}
	// Assistant text.
	if strings.TrimSpace(m.Content) == "" {
		if m.Status == "running" {
			return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				ui.Spinner{Size: 13, Label: "Working"}, ui.Text("Working…", 12, ui.Regular, colors.Faint),
			}}
		}
		return core.View{}
	}
	body := ui.TextView{MarkdownFunc: func() string { return a.markdownPreview(m.Content, "") }, Selectable: true, OpenLinks: true}
	footer := a.messageFooter(cx, m)
	if m.Status == "cancelled" {
		return core.View{Style: core.Style{Direction: core.Column, Gap: 3, MinWidth: core.Pt(0)}, Children: []core.Node{
			body, ui.Text("Stopped.", 11.5, ui.Regular, colors.Faint), footer}}
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 3, MinWidth: core.Pt(0)}, Children: []core.Node{body, footer}}
}

// reasoningCard folds a thinking stream into a disclosure. It opens while
// the model thinks and folds away when the answer starts; once the reader
// opens or closes it, their choice wins.
func (a *app) reasoningCard(cx *core.Scope, m proto.Message) core.Node {
	colors := ui.ColorsOf(cx)
	key := "reasoning:" + m.ID
	running := m.Status == "running"
	title := "Thought"
	if running {
		title = "Thinking"
	}
	trigger := []core.Node{ui.Icon(lucide.Brain, 12, colors.Faint), ui.Text(title, 11.5, ui.Semibold, colors.Muted)}
	if running {
		trigger = append(trigger, ui.Spinner{Size: 11, Label: "Thinking"})
	} else {
		trigger = append(trigger, core.Show{When: func() bool { return !a.isExpanded(key, running) }, Then: func(*core.Scope) core.Node {
			preview := oneLine(m.Content, 90)
			if preview == "" {
				return core.View{}
			}
			return core.Text{Content: preview, MaxLines: 1, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }}
		}})
	}
	return core.View{Style: core.Style{MinWidth: core.Pt(0), Padding: core.Edges{Left: 2}}, Children: []core.Node{
		ui.Collapsible{Variant: ui.CollapsibleArrowLeft, Text: title, Trigger: trigger,
			Open:         func() bool { return a.isExpanded(key, running) },
			OnOpenChange: func(open bool) { a.setExpanded(key, open) },
			TestID:       "reasoning-" + m.ID,
			Content: core.Text{Content: m.Content, Selectable: core.SelectableOn, Style: core.Style{FontSize: 12, LineHeight: 18},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }}},
	}}
}

// toolCard folds one tool call into a card: its name, a one-line summary
// of the input, its outcome, and the input and output on demand.
func (a *app) toolCard(cx *core.Scope, m proto.Message) core.Node {
	colors := ui.ColorsOf(cx)
	key := "tool:" + m.ID
	head := m.Title
	if head == "" {
		head = "Tool"
	}
	failed := m.Status == "failed"
	trigger := []core.Node{ui.Icon(toolIcon(head), 13, colors.Muted), ui.Text(head, 12, ui.Semibold, colors.Text)}
	if summary := toolSummary(m.Detail); summary != "" {
		trigger = append(trigger, core.Text{Content: summary, MaxLines: 1, Style: core.Style{FontSize: 11.5, FontFamily: "Menlo, monospace"},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
	}
	switch m.Status {
	case "running":
		trigger = append(trigger, ui.Spinner{Size: 12, Label: "Running " + head})
	case "failed":
		trigger = append(trigger, ui.Icon(lucide.CircleX, 13, colors.Danger))
	case "completed":
		trigger = append(trigger, ui.Icon(lucide.CircleCheck, 13, colors.Faint))
	}
	files := toolChangedFiles(head, m.Detail)
	if len(files) > 0 {
		label := "1 file"
		if len(files) > 1 {
			label = fmt.Sprintf("%d files", len(files))
		}
		trigger = append(trigger, core.Text{Content: label, MaxLines: 1,
			Style: core.Style{FontSize: 10, FlexShrink: 0}, StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
	}
	var blocks []core.Node
	if len(files) > 0 {
		blocks = append(blocks, a.changedFilesBlock(cx, files))
	}
	if detail := prettyDetail(m.Detail); detail != "" {
		blocks = append(blocks, detailBlock(cx, "Input", detail))
	}
	if body := truncate(m.Content, 6000); strings.TrimSpace(body) != "" {
		blocks = append(blocks, detailBlock(cx, "Output", body))
	}
	if len(blocks) == 0 {
		blocks = append(blocks, ui.Text("No output", 11.5, ui.Regular, colors.Faint))
	}
	return core.View{Style: core.Style{MinWidth: core.Pt(0)}, Children: []core.Node{
		ui.Collapsible{Variant: ui.CollapsibleArrowLeft, Text: head, Trigger: trigger,
			Open:         func() bool { return a.isExpanded(key, failed) },
			OnOpenChange: func(open bool) { a.setExpanded(key, open) },
			TestID:       "tool-" + m.ID,
			Content:      core.View{Style: core.Style{Direction: core.Column, Gap: 6, MinWidth: core.Pt(0)}, Children: blocks}},
	}}
}

// changedFilesBlock lists the files a tool call changed.
func (a *app) changedFilesBlock(cx *core.Scope, files []string) core.Node {
	colors := ui.ColorsOf(cx)
	root := ""
	if git := a.detail.Get().Git; git != nil {
		root = git.Root
	}
	var rows []core.Node
	for _, path := range files {
		path := path
		label := core.Text{Content: path, MaxLines: 1, Style: core.Style{FontSize: 11, FontFamily: "Menlo, monospace", FlexGrow: 1, FlexShrink: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}
		row := []core.Node{ui.Icon(lucide.FileText, 11, colors.Muted), label}
		relative := toolFileRelative(root, path)
		if relative == "" {
			rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5,
				Padding: core.Symmetric(2, 4)}, Children: row})
			continue
		}
		rows = append(rows, base.Button{OnPress: func(core.PressEvent) {
			a.showSurface("files")
			a.openFile(relative)
		}, Label: "Open " + path, TestID: "tool-file-" + relative,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5, Width: core.Pct(100),
				Padding: core.Symmetric(2, 4), CornerRadius: 5, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Hover()}, Focus: ui.FocusRing(cx, -2)}
			}, Children: row})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 2}, Children: []core.Node{
		ui.Text("Changed files", 10, ui.Semibold, colors.Faint),
		core.View{Style: core.Style{Direction: core.Column, Gap: 1}, Children: rows},
	}}
}

// detailBlock is a labelled monospaced block of a tool card.
func detailBlock(cx *core.Scope, label, text string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Column, Gap: 3, MinWidth: core.Pt(0)}, Children: []core.Node{
		ui.Text(label, 10.5, ui.Semibold, colors.Faint),
		core.View{Style: core.Style{Background: colors.Inset(), CornerRadius: 6, Padding: core.All(8), MinWidth: core.Pt(0)}, Children: []core.Node{
			core.Text{Content: text, Selectable: core.SelectableOn, Style: core.Style{FontSize: 11, LineHeight: 15, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}},
	}}
}

// toolSummary writes a tool's input as the one line worth showing closed.
func toolSummary(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var input map[string]any
	if json.Unmarshal([]byte(detail), &input) == nil {
		for _, key := range []string{"command", "cmd", "file_path", "path", "pattern", "query", "url", "description", "title", "prompt"} {
			if value, ok := input[key].(string); ok && strings.TrimSpace(value) != "" {
				return oneLine(value, 110)
			}
		}
	}
	return oneLine(detail, 110)
}

// prettyDetail indents JSON input, and passes anything else through.
func prettyDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var value any
	if json.Unmarshal([]byte(detail), &value) == nil {
		var indented bytes.Buffer
		encoder := json.NewEncoder(&indented)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if encoder.Encode(value) == nil {
			return truncate(strings.TrimRight(indented.String(), "\n"), 6000)
		}
	}
	return truncate(detail, 6000)
}

// oneLine collapses whitespace and clips text to limit runes.
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > limit {
		return strings.TrimSpace(string(r[:limit-1])) + "…"
	}
	return s
}

// toolIcon picks a glyph for a tool by its name.
func toolIcon(name string) func() *core.SVG {
	name = strings.ToLower(name)
	switch {
	case hasAny(name, "bash", "shell", "exec", "terminal", "command", "run"):
		return lucide.Terminal
	case hasAny(name, "read", "write", "edit", "patch", "file", "notebook"):
		return lucide.FileText
	case hasAny(name, "grep", "search", "find", "glob", "list"):
		return lucide.Search
	case hasAny(name, "fetch", "http", "web", "browser", "url"):
		return lucide.Globe
	case hasAny(name, "task", "agent", "delegate", "subagent"):
		return lucide.Bot
	}
	return lucide.Wrench
}

func hasAny(s string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "\n…"
}

// daySeparator starts a new calendar day's messages.
func (a *app) daySeparator(cx *core.Scope, label string) core.Node {
	colors := ui.ColorsOf(cx)
	line := core.View{Style: core.Style{Height: core.Pt(1), FlexGrow: 1, Background: colors.Line()}}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
		Padding: core.Edges{Top: 16, Bottom: 2}}, Children: []core.Node{
		line, ui.Text(label, 10.5, ui.Semibold, colors.Faint), line,
	}}
}

// dayLabel names a message day the way a transcript does.
func dayLabel(at time.Time) string {
	local := at.Local()
	now := time.Now()
	y, m, d := local.Date()
	ny, nm, nd := now.Date()
	yesterday := now.AddDate(0, 0, -1)
	yy, ym, yd := yesterday.Date()
	switch {
	case y == ny && m == nm && d == nd:
		return "Today"
	case y == yy && m == ym && d == yd:
		return "Yesterday"
	case y == now.Year():
		return local.Format("Jan 2")
	}
	return local.Format("Jan 2, 2006")
}

// messageFooter is a message's time and copy action.
func (a *app) messageFooter(cx *core.Scope, m proto.Message) core.Node {
	colors := ui.ColorsOf(cx)
	actions := []core.Node{
		ui.Text(m.CreatedAt.Local().Format("3:04 PM"), 10.5, ui.Regular, colors.Faint),
		core.Show{When: func() bool { return strings.TrimSpace(m.Content) != "" }, Then: func(*core.Scope) core.Node {
			return ui.IconButton{Icon: lucide.Copy, Label: "Copy message", Size: 20, IconSize: 11,
				Tint: colors.Faint, TestID: "copy-message-" + m.ID, OnPress: func() { a.cx.SetClipboard(m.Content) }}
		}},
	}
	if m.Role == proto.RoleUser {
		actions = append(actions,
			ui.IconButton{Icon: lucide.Pencil, Label: "Edit message", Size: 20, IconSize: 11, Tint: colors.Faint,
				TestID: "edit-message-" + m.ID, OnPress: func() { a.openEditMessage(m) }},
		)
	}
	if m.Role == proto.RoleAssistant && m.RunID != "" {
		actions = append(actions, ui.IconButton{Icon: lucide.RotateCcw, Label: "Retry turn", Size: 20, IconSize: 11,
			Tint: colors.Faint, TestID: "retry-message-" + m.ID, Disabled: a.running.Get,
			OnPress: func() { a.retryRun(m.RunID) }})
	}
	actions = append(actions, ui.IconButton{Icon: lucide.GitBranch, Label: "Branch from here", Size: 20, IconSize: 11,
		Tint: colors.Faint, TestID: "branch-message-" + m.ID, OnPress: func() { a.branchFromMessage(m) }})
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 3, Padding: core.Edges{Left: 2}},
		Children: actions}
}

// runFooter reports a finished run's model, size and duration under its
// turn.
func (a *app) runFooter(cx *core.Scope, runID string) core.Node {
	if runID == "" {
		return nil
	}
	var run *proto.Run
	runs := a.detail.Get().Runs
	for i := range runs {
		if runs[i].ID == runID {
			r := runs[i]
			run = &r
			break
		}
	}
	if run == nil || run.Status == proto.RunRunning {
		return nil
	}
	colors := ui.ColorsOf(cx)
	parts := []string{}
	if run.Model != "" {
		parts = append(parts, run.Model)
	}
	if run.Usage != nil {
		if run.Usage.TotalTokens > 0 {
			parts = append(parts, humanTokens(run.Usage.TotalTokens)+" tokens")
		}
		if run.Usage.CostUSD > 0 {
			parts = append(parts, fmt.Sprintf("$%.2f", run.Usage.CostUSD))
		}
	}
	if run.EndedAt != nil {
		if d := run.EndedAt.Sub(run.StartedAt); d >= time.Second {
			parts = append(parts, d.Round(time.Second).String())
		}
	}
	if run.Status == proto.RunFailed {
		parts = append(parts, "failed")
	}
	if len(parts) == 0 {
		return nil
	}
	color := colors.Faint
	if run.Status == proto.RunFailed {
		color = colors.Danger
	}
	return core.View{Style: core.Style{Padding: core.Edges{Top: 2, Left: 2}}, Children: []core.Node{
		ui.Text(strings.Join(parts, " · "), 10.5, ui.Regular, color),
	}}
}

// humanTokens shortens a token count.
func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
