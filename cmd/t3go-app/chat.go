package main

import (
	"fmt"
	"strings"

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
}

func groupMessages(messages []proto.Message) []turnGroup {
	var out []turnGroup
	index := map[string]int{}
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
		out = append(out, turnGroup{ID: key, Messages: []proto.Message{m}})
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
				rows := make([]core.Node, 0, len(g.Messages))
				for _, m := range g.Messages {
					rows = append(rows, a.renderMessage(cx, m))
				}
				return core.View{Style: core.Style{Gap: 10, MinWidth: core.Pt(0)}, Children: rows}
			})
		},
	)
	composer := chat.Composer{
		Draft: a.draft.Get, OnDraftChange: a.draft.Set,
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
		TestID:      "composer-card",
	}
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		core.View{Style: ui.FillRest, Children: []core.Node{
			chat.Conversation{State: a.conv, JumpLabel: "Jump to latest", Children: []core.Node{
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
				core.View{TestID: "composer-drop", OnFileDrop: a.attachPaths, Children: []core.Node{composer}},
			}},
		}},
	}}
}

// attachmentChips are the files the next message carries.
func (a *app) attachmentChips(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	files := a.attachments.Get()
	rows := make([]core.Node, 0, len(files))
	for _, f := range files {
		f := f
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6,
			Padding: core.Symmetric(4, 8), Background: colors.Field(), CornerRadius: 8, MaxWidth: core.Pt(280)}, Children: []core.Node{
			ui.Icon(lucide.Paperclip, 12, colors.Muted),
			core.Text{Content: f.Name + " · " + humanSize(f.Size), MaxLines: 1, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			ui.Button{Text: "×", Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Remove attachment",
				TestID: "remove-attachment-" + f.ID, OnPress: func() { a.removeAttachment(f.ID) }},
		}})
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
				files = append(files, ui.Text(f.Name+" · "+humanSize(f.Size), 11, ui.Regular, colors.Faint))
			}
			body = append(body, core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 8}, Children: files})
		}
		return core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifyEnd, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.View{Style: core.Style{MaxWidth: core.Pt(560), Background: colors.Inset(), Padding: core.Symmetric(10, 14), CornerRadius: 14,
				Direction: core.Column, Gap: 6}, Children: body},
		}}
	}
	switch m.Kind {
	case proto.KindReasoning:
		return core.View{Style: core.Style{Gap: 3, Padding: core.Edges{Left: 2}}, Children: []core.Node{
			ui.Text("Thinking", 11, ui.Semibold, colors.Faint),
			core.Text{Content: m.Content, Selectable: core.SelectableOn, Style: core.Style{FontSize: 12, LineHeight: 18},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}}
	case proto.KindTool:
		var rows []core.Node
		status := m.Status
		head := m.Title
		if head == "" {
			head = "Tool"
		}
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
			ui.Text("•", 12, ui.Bold, colors.Accent),
			ui.Text(head, 12, ui.Semibold, colors.Text),
			core.Show{When: func() bool { return status != "" && status != "completed" }, Then: func(*core.Scope) core.Node {
				return ui.Text(status, 11, ui.Regular, colors.Faint)
			}},
		}})
		if m.Detail != "" {
			rows = append(rows, core.Text{Content: m.Detail, MaxLines: 3, Style: core.Style{FontSize: 11.5, LineHeight: 16},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		}
		if body := truncate(m.Content, 4000); body != "" {
			rows = append(rows, core.View{Style: core.Style{Background: colors.Inset(), CornerRadius: 6, Padding: core.All(8)}, Children: []core.Node{
				core.Text{Content: body, Selectable: core.SelectableOn, Style: core.Style{FontSize: 11, LineHeight: 15, FontFamily: "Menlo, monospace"},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}})
		}
		return core.View{Style: core.Style{Gap: 4, MinWidth: core.Pt(0)}, Children: rows}
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
	body := ui.TextView{MarkdownFunc: func() string { return m.Content }, Selectable: true, OpenLinks: true}
	if m.Status == "cancelled" {
		return core.Group{body, ui.Text("Stopped.", 11.5, ui.Regular, colors.Faint)}
	}
	return body
}

func truncate(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "\n…"
}
