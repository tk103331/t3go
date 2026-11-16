package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/device"
	"github.com/egoist/asuka/sdk/markdown"
	"github.com/egoist/asuka/sdk/speech"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/proto"
)

func conversation(a *app, narrow bool) ui.Node {
	return conversationBody(a, narrow)
}

func conversationBody(a *app, narrow bool) ui.Node {
	header := ui.Column(ui.Style{Gap: 2, Padding: ui.Symmetric(9, 14), Background: colors.SecondaryBackground},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
			ui.Show(signal.Func(func() bool { return narrow }), func() ui.Node {
				return ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: ui.Val("Threads"), OnPress: a.closeThread})
			}),
			ui.Column(ui.Style{Grow: 1, Gap: 1},
				ui.Text(signal.Func(func() string {
					t := a.detail.Get().Thread
					if strings.TrimSpace(t.Title) != "" {
						return t.Title
					}
					return "Select a thread"
				}), ui.Style{Font: fonts.Headline, Lines: 1}),
				ui.Text(signal.Func(func() string {
					t := a.detail.Get().Thread
					if t.ID == "" {
						return ""
					}
					return fmt.Sprintf("%s · %s · %s", a.providerName(t.Provider), t.Model, t.Permission)
				}), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 1}),
			),
			ui.Show(signal.Func(func() bool { return a.running.Get() }), func() ui.Node {
				return ui.Activity()
			}),
			ui.Button(ui.ButtonProps{Icon: ui.IconList, Label: ui.Val("Surfaces"),
				OnPress: func() { a.openSurface(a.rightTab.Peek()) }}),
			ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("Thread actions"),
				Menu: a.threadHeaderActions()}),
		),
	)

	empty := func() ui.Node {
		return ui.Column(ui.Style{Grow: 1, Align: ui.AlignCenter, Justify: ui.JustifyCenter, Gap: 6, Padding: ui.All(30)},
			ui.Text(ui.Val("Select a thread"), ui.Style{Font: fonts.Headline}),
			ui.Text(ui.Val("Or start a new one from the sidebar."), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
		)
	}
	body := ui.Show(signal.Func(func() bool { return a.selected.Get() == "" }), func() ui.Node {
		return empty()
	}, func() ui.Node {
		return ui.Column(ui.Style{Grow: 1},
			approvalsView(a),
			ui.Show(signal.Func(func() bool { return len(a.messages.Get()) == 0 }), func() ui.Node {
				return ui.Column(ui.Style{Grow: 1, Align: ui.AlignCenter, Justify: ui.JustifyCenter, Gap: 6},
					ui.Text(ui.Val("No messages yet"), ui.Style{Font: fonts.Headline}),
					ui.Text(ui.Val("Ask the agent to change this project."), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
				)
			}, func() ui.Node {
				return transcript(a)
			}),
			queueView(a),
			composer(a),
		)
	})
	return ui.Column(ui.Style{Grow: 1}, header, body)
}

func transcript(a *app) ui.Node {
	entries := signal.NewMemoFunc(func() []transcriptEntry {
		return transcriptEntries(a.messages.Get())
	}, nil)
	return ui.List(ui.ListProps{
		Appearance: ui.ListBare,
		Anchor:     ui.AnchorBottom,
		Section:    func(i int) string { return dayLabel(entries.Get()[i].CreatedAt) },
		Header: func(day signal.Accessor[string]) ui.Node {
			return ui.Row(ui.Style{Justify: ui.JustifyCenter, Padding: ui.Symmetric(6, 0)},
				ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(2, 9), CornerRadius: 999, Background: colors.SecondaryBackground}},
					ui.Text(day, ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel})))
		},
	}, entries, func(entry transcriptEntry) string { return entry.Key }, func(item signal.Accessor[transcriptEntry], _ signal.Accessor[int]) ui.Node {
		if signal.Untrack(func() string { return item.Get().RunID }) == "" {
			return messageRow(a, signal.Func(func() proto.Message { return item.Get().Message }))
		}
		return runBlockRow(a, item)
	})
}

type transcriptEntry struct {
	Key       string
	RunID     string
	Message   proto.Message
	CreatedAt time.Time
}

func transcriptEntries(messages []proto.Message) []transcriptEntry {
	entries := make([]transcriptEntry, 0, len(messages))
	runs := map[string]bool{}
	for _, message := range messages {
		if message.Role == proto.RoleUser || message.RunID == "" {
			entries = append(entries, transcriptEntry{
				Key:       "message:" + message.ID,
				Message:   message,
				CreatedAt: message.CreatedAt,
			})
			continue
		}
		if runs[message.RunID] {
			continue
		}
		runs[message.RunID] = true
		entries = append(entries, transcriptEntry{
			Key:       "run:" + message.RunID,
			RunID:     message.RunID,
			CreatedAt: message.CreatedAt,
		})
	}
	return entries
}

func runBlockRow(a *app, item signal.Accessor[transcriptEntry]) ui.Node {
	return ui.Dynamic(func() ui.Node {
		entry := item.Get()
		messages := runMessagesFor(a.messages.Get(), entry.RunID)
		status := a.runStatusFor(entry.RunID)
		running := runMessagesRunning(status, messages)
		failed := status == proto.RunFailed || runMessagesFailed(messages)
		cancelled := status == proto.RunCancelled || runMessagesCancelled(messages)
		reasoning, tools := runActivityCounts(messages)

		if running {
			rows := runTimelineRows(a, messages, "")
			rows = append(rows, runningRunFooter(reasoning, tools))
			return ui.Column(ui.Style{Gap: 2}, rows...)
		}

		final, hasFinal := finalRunMessage(messages)
		hidden := len(runTimelineMessages(messages, final.ID))
		summary := completedRunSummary(reasoning, tools, failed, cancelled, hidden)
		summaryKey := "run:" + entry.RunID
		summaryOpen := a.runDisclosureOpen(summaryKey)
		var rows []ui.Node
		if summary != "" {
			summaryColor := colors.SecondaryLabel
			if failed {
				summaryColor = colors.Red
			} else if cancelled {
				summaryColor = colors.Orange
			}
			rows = append(rows, runSummaryRow(summary, summaryColor, summaryOpen, func() {
				a.toggleRunDisclosure(summaryKey)
			}))
			if summaryOpen {
				timeline := runTimelineRows(a, messages, final.ID)
				if len(timeline) > 0 {
					rows = append(rows, ui.Column(ui.Style{Padding: ui.Edges{Left: 22, Top: 2, Bottom: 2}, Gap: 2}, timeline...))
				}
			}
		}
		if hasFinal {
			rows = append(rows, messageRow(a, messageByID(a, final.ID)))
		} else if cancelled {
			rows = append(rows, ui.Text(ui.Val("Stopped."), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel,
				Padding: ui.Symmetric(5, 16)}))
		}
		return ui.Column(ui.Style{Padding: ui.Symmetric(2, 0), Gap: 2}, rows...)
	})
}

func (a *app) runDisclosureOpen(key string) bool {
	return a.runOpen.Get()[key]
}

func (a *app) toggleRunDisclosure(key string) {
	open := a.runOpen.Peek()
	next := cloneRunOpen(open)
	next[key] = !next[key]
	a.runOpen.Set(next)
}

func cloneRunOpen(open map[string]bool) map[string]bool {
	next := make(map[string]bool, len(open)+1)
	for id, value := range open {
		next[id] = value
	}
	return next
}

func runMessagesFor(messages []proto.Message, runID string) []proto.Message {
	var out []proto.Message
	for _, message := range messages {
		if message.RunID == runID && message.Role != proto.RoleUser {
			out = append(out, message)
		}
	}
	return out
}

func (a *app) runStatusFor(runID string) proto.RunStatus {
	for _, run := range a.detail.Get().Runs {
		if run.ID == runID {
			return run.Status
		}
	}
	return ""
}

func runMessagesRunning(status proto.RunStatus, messages []proto.Message) bool {
	if status == proto.RunRunning {
		return true
	}
	if status != "" {
		return false
	}
	for _, message := range messages {
		if message.Status == "running" {
			return true
		}
	}
	return false
}

func runMessagesFailed(messages []proto.Message) bool {
	for _, message := range messages {
		if message.Status == "failed" || message.Kind == proto.KindError {
			return true
		}
	}
	return false
}

func runMessagesCancelled(messages []proto.Message) bool {
	for _, message := range messages {
		if message.Status == "cancelled" {
			return true
		}
	}
	return false
}

func runActivityCounts(messages []proto.Message) (reasoning, tools int) {
	for _, message := range messages {
		switch message.Kind {
		case proto.KindReasoning:
			reasoning++
		case proto.KindTool:
			tools++
		}
	}
	return reasoning, tools
}

func finalRunMessage(messages []proto.Message) (proto.Message, bool) {
	var final proto.Message
	found := false
	for _, message := range messages {
		if message.Role == proto.RoleAssistant && message.Kind == proto.KindText && strings.TrimSpace(message.Content) != "" {
			final = message
			found = true
		}
	}
	if found {
		return final, true
	}
	for _, message := range messages {
		if message.Kind == proto.KindError && strings.TrimSpace(message.Content) != "" {
			final = message
			found = true
		}
	}
	return final, found
}

func runTimelineMessages(messages []proto.Message, skipID string) []proto.Message {
	out := make([]proto.Message, 0, len(messages))
	for _, message := range messages {
		if message.ID == skipID || (message.Kind == proto.KindText && strings.TrimSpace(message.Content) == "") {
			continue
		}
		out = append(out, message)
	}
	return out
}

func completedRunSummary(reasoning, tools int, failed, cancelled bool, hidden int) string {
	var parts []string
	if reasoning > 0 || tools > 0 {
		parts = append(parts,
			countPhrase(reasoning, "thought", "thoughts"),
			countPhrase(tools, "tool call", "tool calls"),
		)
	} else if hidden > 0 {
		parts = append(parts, "Run details")
	}
	if failed {
		parts = append(parts, "failed")
	}
	if cancelled {
		parts = append(parts, "stopped")
	}
	return strings.Join(parts, " · ")
}

func countPhrase(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", count, many)
}

func runSummaryRow(summary string, color ui.Color, open bool, toggle func()) ui.Node {
	icon := ui.IconForward
	if open {
		icon = ui.IconDown
	}
	return ui.View(ui.ViewProps{
		OnPress: toggle,
		Label:   ui.Val(summary),
		Style:   ui.Style{Padding: ui.Symmetric(6, 8), CornerRadius: 8},
	}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
		ui.Symbol(icon, ui.Style{Color: color}),
		ui.Text(ui.Val(summary), ui.Style{Font: fonts.Footnote.Weighted(fonts.Medium), Color: color, Grow: 1, Lines: 1}),
	))
}

func runningRunFooter(reasoning, tools int) ui.Node {
	label := "Working"
	var counts []string
	if reasoning > 0 {
		counts = append(counts, countPhrase(reasoning, "thought", "thoughts"))
	}
	if tools > 0 {
		counts = append(counts, countPhrase(tools, "tool call", "tool calls"))
	}
	if len(counts) > 0 {
		label += " · " + strings.Join(counts, " · ")
	}
	return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(5, 8)},
		ui.Activity(),
		ui.Text(ui.Val(label), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Grow: 1}),
	)
}

func runTimelineRows(a *app, messages []proto.Message, skipID string) []ui.Node {
	var rows []ui.Node
	for _, message := range runTimelineMessages(messages, skipID) {
		message := message
		if message.Kind == proto.KindReasoning || message.Kind == proto.KindTool {
			rows = append(rows, runStepRow(a, message))
			continue
		}
		rows = append(rows, messageRow(a, messageByID(a, message.ID)))
	}
	return rows
}

func messageByID(a *app, id string) signal.Accessor[proto.Message] {
	return signal.Func(func() proto.Message {
		for _, message := range a.messages.Get() {
			if message.ID == id {
				return message
			}
		}
		return proto.Message{}
	})
}

func runStepRow(a *app, message proto.Message) ui.Node {
	key := "step:" + message.ID
	return ui.Dynamic(func() ui.Node {
		return activityStepView(a, message, a.runDisclosureOpen(key), func() { a.toggleRunDisclosure(key) })
	})
}

func messageRow(a *app, item signal.Accessor[proto.Message]) ui.Node {
	m := signal.Untrack(func() proto.Message { return item.Get() })
	if m.Role == proto.RoleUser {
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(4, 14)}},
			ui.Column(ui.Style{Align: ui.AlignEnd, Gap: 3},
				ui.View(ui.ViewProps{Style: ui.Style{MaxWidth: ui.Pct(82), Padding: ui.Symmetric(9, 13), Gap: 5,
					CornerRadius: 14, Background: colors.SecondaryBackground, BorderWidth: 1, BorderColor: colors.Separator}},
					ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
						ui.Text(ui.Val("You"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel, Grow: 1}),
						ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("Message actions"),
							Menu: messageActions(a, m)})),
					ui.Show(signal.Func(func() bool { return item.Get().Content != "" }), func() ui.Node {
						return markdown.View(markdown.Props{Text: signal.Func(func() string { return item.Get().Content })})
					}),
					attachmentChips(a, item),
				),
			))
	}
	switch m.Kind {
	case proto.KindReasoning:
		return reasoningRow(a, item)
	case proto.KindTool:
		return toolRow(a, item)
	case proto.KindNotice:
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(3, 16)}},
			ui.Text(signal.Func(func() string { return item.Get().Content }), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}))
	case proto.KindError:
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(4, 16)}},
			ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(8, 11), CornerRadius: 9, Background: colors.Red.Opacity(0.09)}},
				ui.Text(signal.Func(func() string { return item.Get().Content }), ui.Style{Font: fonts.Footnote, Color: colors.Red})))
	}

	return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(5, 16), Gap: 4}},
		ui.Show(signal.Func(func() bool { return strings.TrimSpace(item.Get().Content) == "" && item.Get().Status == "running" }),
			func() ui.Node {
				return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8}, ui.Activity(),
					ui.Text(ui.Val("Working…"), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}))
			}),
		ui.Show(signal.Func(func() bool { return strings.TrimSpace(item.Get().Content) != "" }), func() ui.Node {
			return markdown.View(markdown.Props{
				Text:      signal.Func(func() string { return item.Get().Content }),
				Streaming: signal.Func(func() bool { return item.Get().Status == "running" }),
				Theme:     messageTheme(),
			})
		}),
		ui.Show(signal.Func(func() bool { return item.Get().Status == "cancelled" }), func() ui.Node {
			return ui.Text(ui.Val("Stopped."), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel})
		}),
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 4},
			ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}),
			ui.Button(ui.ButtonProps{Icon: ui.IconCopy, Label: ui.Val("Copy message"),
				OnPress: func() { ui.Copy(item.Get().Content) }}),
			ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("Message actions"),
				Menu: messageActions(a, m)})),
	)
}

func messageActions(a *app, m proto.Message) []ui.Action {
	var actions []ui.Action
	if strings.TrimSpace(m.Content) != "" {
		actions = append(actions,
			ui.Action{Title: ui.Val("Copy"), Icon: ui.IconCopy, OnPress: func() { ui.Copy(m.Content) }},
			ui.Action{Title: ui.Val("Share"), Icon: ui.IconShare, OnPress: func() { ui.Share(m.Content) }},
			ui.Action{Title: ui.Val("Read aloud"), Icon: ui.IconSpeaker, OnPress: func() {
				speech.Speak(m.Content, speech.Options{}, nil)
			}},
		)
	}
	if m.Role == proto.RoleUser && m.ThreadID != "" {
		actions = append(actions, ui.Action{Title: ui.Val("Fork from here"), Icon: ui.IconCopy, OnPress: func() {
			a.forkThread(m.ThreadID, m.ID, m.Content)
		}})
	}
	if m.RunID != "" {
		actions = append(actions,
			ui.Action{Title: ui.Val("Retry run"), Icon: ui.IconRefresh, OnPress: func() { a.retryRun(m.RunID) }},
			ui.Action{Title: ui.Val("Restore checkpoint…"), Icon: ui.IconClock, Role: ui.ActionDestructive,
				OnPress: func() { a.confirmRestoreRun(m.RunID) }},
		)
	}
	return actions
}

func messageTheme() *markdown.Theme {
	th := markdown.DefaultTheme()
	th.BlockSpacing = 8
	return th
}

func reasoningRow(a *app, item signal.Accessor[proto.Message]) ui.Node {
	return ui.Dynamic(func() ui.Node {
		message := item.Get()
		key := "step:" + message.ID
		return activityStepView(a, message, a.runDisclosureOpen(key), func() { a.toggleRunDisclosure(key) })
	})
}

func toolRow(a *app, item signal.Accessor[proto.Message]) ui.Node {
	return ui.Dynamic(func() ui.Node {
		message := item.Get()
		key := "step:" + message.ID
		return activityStepView(a, message, a.runDisclosureOpen(key), func() { a.toggleRunDisclosure(key) })
	})
}

func activityStepView(a *app, message proto.Message, open bool, toggle func()) ui.Node {
	title := activityStepTitle(message)
	status, statusColor := activityStepStatus(message)
	chevron := ui.IconForward
	if open {
		chevron = ui.IconDown
	}
	rowChildren := []ui.Node{
		ui.Symbol(chevron, ui.Style{Color: colors.TertiaryLabel}),
		ui.Symbol(activityStepIcon(message), ui.Style{Color: colors.SecondaryLabel}),
		ui.Text(ui.Val(title), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, Grow: 1, Lines: 2}),
	}
	if status != "" {
		rowChildren = append(rowChildren, ui.Text(ui.Val(status), ui.Style{Font: fonts.Caption2, Color: statusColor}))
	}
	row := ui.View(ui.ViewProps{
		OnPress: toggle,
		Label:   ui.Val(title),
		Style:   ui.Style{Padding: ui.Symmetric(5, 7), CornerRadius: 8},
	}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 7}, rowChildren...))
	children := []ui.Node{row}
	if open {
		if body := activityStepBody(a, message); len(body) > 0 {
			children = append(children, ui.Column(ui.Style{
				Padding: ui.Edges{Left: 27, Top: 2, Bottom: 5}, Gap: 5,
			}, body...))
		}
	}
	return ui.Column(ui.Style{Gap: 1}, children...)
}

func activityStepTitle(message proto.Message) string {
	if message.Kind == proto.KindReasoning {
		return "Thinking"
	}
	title := strings.TrimSpace(message.Title)
	if title == "" {
		title = "Tool call"
	}
	if hint := toolDetailHint(message.Detail); hint != "" {
		title += " · " + hint
	}
	return title
}

func activityStepIcon(message proto.Message) ui.Icon {
	if message.Kind == proto.KindReasoning {
		return ui.IconMessage
	}
	name := strings.ToLower(strings.TrimSpace(message.Title))
	switch {
	case strings.Contains(name, "search"), strings.Contains(name, "grep"), strings.Contains(name, "find"):
		return ui.IconSearch
	case strings.Contains(name, "edit"), strings.Contains(name, "write"), strings.Contains(name, "patch"):
		return ui.IconEdit
	case strings.Contains(name, "read"), strings.Contains(name, "file"):
		return ui.IconDocument
	default:
		return ui.IconDocument
	}
}

func activityStepStatus(message proto.Message) (string, ui.Color) {
	switch message.Status {
	case "running":
		return "Running", colors.Orange
	case "failed":
		return "Failed", colors.Red
	case "cancelled":
		return "Stopped", colors.SecondaryLabel
	}
	return "", colors.SecondaryLabel
}

func activityStepBody(a *app, message proto.Message) []ui.Node {
	var body []ui.Node
	if message.Kind == proto.KindReasoning {
		if text := strings.TrimSpace(message.Content); text != "" {
			body = append(body, ui.Text(ui.Val(text), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, Lines: 0}))
		}
		return body
	}
	if detail := formatToolDetail(message.Detail); detail != "" {
		body = append(body,
			ui.Text(ui.Val("Input"), ui.Style{Font: fonts.Caption2.Weighted(fonts.Semibold), Color: colors.TertiaryLabel}),
			ui.Text(ui.Val(detail), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Color: colors.SecondaryLabel, Lines: 12}),
		)
	}
	if output := strings.TrimSpace(message.Content); output != "" {
		body = append(body,
			ui.Text(ui.Val("Output"), ui.Style{Font: fonts.Caption2.Weighted(fonts.Semibold), Color: colors.TertiaryLabel}),
			ui.Text(ui.Val(output), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Color: colors.SecondaryLabel, Lines: 12}),
		)
	}
	body = append(body, a.toolFileChips(message))
	return body
}

func toolDetailHint(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var input map[string]any
	if json.Unmarshal([]byte(detail), &input) == nil {
		for _, key := range []string{"command", "query", "pattern", "path", "file_path", "url", "description", "prompt"} {
			if value, ok := input[key]; ok {
				if text := toolValueText(value); text != "" {
					return truncateInline(text, 80)
				}
			}
		}
		if raw, err := json.Marshal(input); err == nil {
			return truncateInline(string(raw), 80)
		}
	}
	return truncateInline(strings.Join(strings.Fields(detail), " "), 80)
}

func toolValueText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		raw, _ := json.Marshal(typed)
		return string(raw)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func formatToolDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var input any
	if json.Unmarshal([]byte(detail), &input) == nil {
		if raw, err := json.MarshalIndent(input, "", "  "); err == nil {
			detail = string(raw)
		}
	}
	return truncateRunes(detail, 4000)
}

func truncateInline(text string, limit int) string {
	return truncateRunes(strings.Join(strings.Fields(text), " "), limit)
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

func attachmentChips(a *app, item signal.Accessor[proto.Message]) ui.Node {
	return ui.Show(signal.Func(func() bool { return len(item.Get().Attachments) > 0 }), func() ui.Node {
		return ui.Column(ui.Style{Gap: 4}, ui.For(
			signal.Func(func() []proto.Attachment { return item.Get().Attachments }),
			func(f proto.Attachment) string { return f.ID },
			func(file signal.Accessor[proto.Attachment], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{
					OnPress: func() {
						if c := a.client(); c != nil {
							ui.OpenURL(c.UploadURL(file.Get().ID))
						}
					},
					Style: ui.Style{Padding: ui.Symmetric(2, 0)},
				}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
					ui.Symbol(ui.IconAttach, ui.Style{Color: colors.SecondaryLabel}),
					ui.Text(signal.Func(func() string { return file.Get().Name }), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 1}),
				))
			}))
	})
}

func composerAttachments(a *app) ui.Node {
	return ui.Show(signal.Func(func() bool {
		return len(a.attachments.Get()) > 0 || a.attachmentBusy.Get() || a.attachmentErr.Get() != ""
	}), func() ui.Node {
		return ui.Column(ui.Style{Gap: 3, Padding: ui.Symmetric(2, 2)},
			ui.Show(signal.Func(func() bool { return a.attachmentBusy.Get() }), func() ui.Node {
				return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
					ui.Activity(), ui.Text(ui.Val("Uploading…"), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
			}),
			ui.Show(signal.Func(func() bool { return a.attachmentErr.Get() != "" }), func() ui.Node {
				return ui.Text(a.attachmentErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 2})
			}),
			ui.Row(ui.Style{Wrap: true, Gap: 4}, ui.For(a.attachments,
				func(file proto.Attachment) string { return file.ID },
				func(item signal.Accessor[proto.Attachment], _ signal.Accessor[int]) ui.Node {
					return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 4, Padding: ui.Symmetric(2, 6),
						CornerRadius: 7, Background: colors.Background},
						ui.Symbol(ui.IconAttach, ui.Style{Color: colors.SecondaryLabel}),
						ui.Text(signal.Func(func() string { return item.Get().Name }),
							ui.Style{Font: fonts.Caption, Lines: 1, MaxWidth: ui.Pt(150)}),
						ui.Button(ui.ButtonProps{Icon: ui.IconShare, Label: ui.Val("Open attachment"),
							OnPress: func() {
								if c := a.client(); c != nil {
									ui.OpenURL(c.UploadURL(item.Get().ID))
								}
							}}),
						ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Remove attachment"),
							OnPress: func() { a.removeAttachment(item.Get().ID) }}),
					)
				})),
		)
	})
}

func approvalsView(a *app) ui.Node {
	return ui.Show(signal.Func(func() bool { return len(a.approvals.Get()) > 0 }), func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.Symmetric(8, 14), Gap: 8},
			ui.For(a.approvals, func(r proto.ApprovalRequest) string { return r.ID },
				func(item signal.Accessor[proto.ApprovalRequest], _ signal.Accessor[int]) ui.Node {
					r := item.Get()
					return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(12), Gap: 8, CornerRadius: 12,
						Background: colors.Orange.Opacity(0.1), BorderWidth: 1, BorderColor: colors.Orange.Opacity(0.35)}},
						ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
							ui.Symbol(ui.IconWarning, ui.Style{Color: colors.Orange}),
							ui.Text(signal.Func(func() string {
								if r.Tool != "" {
									return "Approval required · " + r.Tool
								}
								return "Approval required"
							}), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold), Grow: 1}),
						),
						ui.Show(signal.Func(func() bool { return formatApproval(r.Input) != "" }), func() ui.Node {
							return ui.Text(ui.Val(formatApproval(r.Input)), ui.Style{Font: fonts.Custom("monospace", fonts.Callout), Color: colors.SecondaryLabel, Lines: 10})
						}),
						ui.Row(ui.Style{Justify: ui.JustifyEnd, Gap: 8},
							ui.Button(ui.ButtonProps{Title: ui.Val("Deny"), OnPress: func() {
								a.resolveApproval(r.ID, false, "Denied by the user")
							}}),
							ui.Button(ui.ButtonProps{Title: ui.Val("Allow"), Variant: ui.ButtonFilled, OnPress: func() {
								a.resolveApproval(r.ID, true, "")
							}}),
						),
					)
				}))
	})
}

func formatApproval(in map[string]any) string {
	if len(in) == 0 {
		return ""
	}
	raw, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return ""
	}
	text := string(raw)
	if len(text) > 1200 {
		text = text[:1200] + "…"
	}
	return text
}

func queueView(a *app) ui.Node {
	return ui.Show(signal.Func(func() bool { return len(a.queue.Get()) > 0 }), func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(4, 14)}},
			ui.Column(ui.Style{Gap: 4},
				ui.Text(ui.Val("Waiting for the current run"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.For(a.queue, func(p queuedPrompt) string { return p.Key },
					func(item signal.Accessor[queuedPrompt], _ signal.Accessor[int]) ui.Node {
						return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
							ui.Text(signal.Func(func() string { return item.Get().Text }), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, Lines: 2, Grow: 1}),
							ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Remove queued message"),
								OnPress: func() { a.removeQueued(item.Get().Key) }}),
						)
					}),
			))
	})
}

func composer(a *app) ui.Node {
	field := ui.TextField(ui.TextFieldProps{
		Value:          a.draft,
		Placeholder:    ui.Val("Message the agent…"),
		Multiline:      true,
		ReturnKey:      ui.ReturnSend,
		Autocapitalize: ui.CapitalizeSentences,
		Style:          ui.Style{Grow: 1, Lines: 6, MinHeight: ui.Pt(40)},
	})
	card := ui.View(ui.ViewProps{
		Style: ui.Style{Padding: ui.All(8), Gap: 6, CornerRadius: 14, Background: colors.SecondaryBackground,
			BorderWidth: 1, BorderColor: colors.Separator},
		Keys: ui.KeyMap{{Key: "enter", Mods: ui.Primary}: a.send},
	},
		composerAttachments(a),
		field,
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
			ui.Button(ui.ButtonProps{Icon: ui.IconMenu, Label: ui.Val("Command palette"), OnPress: a.openPalette}),
			ui.Button(ui.ButtonProps{Icon: ui.IconAttach, Label: ui.Val("Add attachment"),
				Disabled: a.attachmentBusy, Menu: []ui.Action{
					{Title: ui.Val("Files…"), Icon: ui.IconAttach, OnPress: a.pickAttachments},
					{Title: ui.Val("Photos…"), Icon: ui.IconPhoto, OnPress: a.pickPhotos},
					{Title: ui.Val("Transcribe audio…"), Icon: ui.IconMic, OnPress: a.transcribeAudio},
					{Title: ui.Val("Paste image"), Icon: ui.IconCopy, OnPress: func() {
						path, ok, err := device.ClipboardImage()
						if err != nil {
							a.attachmentErr.Set(friendlyError(err))
							return
						}
						if !ok || path == "" {
							a.attachmentErr.Set("The clipboard has no image.")
							return
						}
						a.uploadAttachments([]device.File{{Path: path, Name: "clipboard.png", Type: mimeTypeOf(path)}})
					}},
				}}),
			ui.Show(signal.Func(func() bool { return a.running.Get() }), func() ui.Node {
				return ui.Button(ui.ButtonProps{Title: ui.Val("Stop"), Icon: ui.IconPause, OnPress: a.stop})
			}),
			ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}),
			ui.Button(ui.ButtonProps{
				Title: signal.Func(func() string {
					if a.running.Get() {
						return "Queue"
					}
					return "Send"
				}),
				Icon:    ui.IconSend,
				Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool {
					return (strings.TrimSpace(a.draft.Get()) == "" && len(a.attachments.Get()) == 0) ||
						!a.connected.Get() || a.attachmentBusy.Get()
				}),
				OnPress: a.send,
			}),
		),
	)
	return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Edges{Top: 4, Right: 14, Bottom: 14, Left: 14}}},
		ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pct(100), MaxWidth: ui.Pt(860), AlignSelf: ui.AlignCenter}},
			a.composerSuggestionView(),
			ui.Show(signal.Func(func() bool { return a.composerBusy.Get() }), func() ui.Node {
				return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(2, 3)},
					ui.Activity(), ui.Text(ui.Val("Indexing files and commands…"),
						ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}))
			}),
			ui.Show(signal.Func(func() bool { return a.composerErr.Get() != "" }), func() ui.Node {
				return ui.Text(a.composerErr, ui.Style{Font: fonts.Caption2, Color: colors.Red, Lines: 2,
					Padding: ui.Symmetric(2, 3)})
			}),
			card,
			ui.Text(ui.Val("While the agent is working, Send queues the next message."), ui.Style{Font: fonts.Caption2, Color: colors.TertiaryLabel, Padding: ui.Edges{Top: 5, Left: 2}}),
		))
}

func dayLabel(t time.Time) string {
	now := time.Now()
	y, m, d := t.Local().Date()
	ny, nm, nd := now.Date()
	switch {
	case y == ny && m == nm && d == nd:
		return "Today"
	case y == ny && m == nm && d == nd-1:
		return "Yesterday"
	case y == ny:
		return t.Local().Format("Monday, January 2")
	}
	return t.Local().Format("January 2, 2006")
}
