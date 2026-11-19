package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/base/textview"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/oneo-me/nsgo/ui/chat"
	"github.com/tk/t3go/internal/i18n"
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

// turnContent is one run arranged the way its transcript presents it. While
// a run is active every process entry stays in order. Once it finishes, the
// final answer stands alone and earlier reasoning, tools and narration fold
// behind a summary.
type turnContent struct {
	Users    []proto.Message
	Process  []proto.Message
	Final    *proto.Message
	Retained []proto.Message
	Thoughts int
	Tools    int
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

func splitTurn(messages []proto.Message, running bool) turnContent {
	var content turnContent
	finalIndex := -1
	for i, m := range messages {
		switch {
		case m.Role == proto.RoleUser:
			content.Users = append(content.Users, m)
		case m.Kind == proto.KindNotice || m.Kind == proto.KindError:
			content.Retained = append(content.Retained, m)
		case m.Role == proto.RoleAssistant:
			if !running && m.Kind == proto.KindText && strings.TrimSpace(m.Content) != "" {
				finalIndex = i
			}
		}
	}
	for i, m := range messages {
		if m.Role != proto.RoleAssistant || m.Kind == proto.KindNotice || m.Kind == proto.KindError {
			continue
		}
		if !running && i == finalIndex {
			final := m
			content.Final = &final
			continue
		}
		if !running && m.Kind == proto.KindText && strings.TrimSpace(m.Content) == "" {
			continue
		}
		content.Process = append(content.Process, m)
		switch m.Kind {
		case proto.KindReasoning:
			content.Thoughts++
		case proto.KindTool:
			content.Tools++
		}
	}
	return content
}

func (a *app) turnRunning(group turnGroup) bool {
	if group.ID != "" {
		for _, run := range a.detail.Get().Runs {
			if run.ID == group.ID {
				return run.Status == proto.RunRunning
			}
		}
	}
	for _, m := range group.Messages {
		if m.Status == "running" {
			return true
		}
	}
	return false
}

func (a *app) threadPane(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	touch := ui.Touch(cx) && cx.Env().Window().Width < 520
	groups := core.Each(
		func() []turnGroup { return groupMessages(a.messages.Get()) },
		func(g turnGroup) string { return g.ID },
		func(cx *core.Scope, item func() turnGroup, _ func() int) core.Node {
			return core.Dynamic(func(cx *core.Scope) core.Node {
				g := item()
				rows := make([]core.Node, 0, len(g.Messages)+2)
				if g.Day != "" {
					rows = append(rows, a.daySeparator(cx, g.Day))
				}
				rows = append(rows, a.renderTurnContent(cx, g)...)
				if footer := a.runFooter(cx, g.ID); footer != nil {
					rows = append(rows, footer)
				}
				return core.View{Style: core.Style{Gap: 10, MinWidth: core.Pt(0)}, Children: rows}
			})
		},
	)
	accessory := func(cx *core.Scope) core.Node {
		attach := ui.IconButton{Icon: lucide.Paperclip, Label: i18n.T("Attach files"), TestID: "attach", OnPress: a.pickAttachments}
		if touch {
			attach.Size, attach.IconSize = 38, 20
		}
		children := []core.Node{
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
							return i18n.T("Stop dictation")
						}
						return "Dictate"
					},
					Selected: a.dictating.Get,
					TestID:   "dictate",
					OnPress:  a.toggleDictation,
				}
			}},
		}
		return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4}, Children: children}
	}
	var composer core.Node
	if touch {
		composer = mobileComposer{
			Draft: a.draft.Get, OnDraftChange: a.setDraft, OnPaste: a.pasteAttachments,
			Label: i18n.T("Message"), Placeholder: i18n.T("Ask the agent…"), Accessory: accessory,
			Controls:         func(cx *core.Scope) core.Node { return core.Dynamic(a.composerModelControl) },
			TrailingControls: func(cx *core.Scope) core.Node { return core.Dynamic(a.composerTrailingControls) },
			Attachments:      func(cx *core.Scope) core.Node { return a.attachmentChips(cx) },
			HasAttachments:   func() bool { return len(a.attachments.Get()) > 0 },
			OnDropFiles:      a.attachPaths, Running: a.running.Get, OnSend: a.send, OnStop: a.stop,
			TestID: "composer-card",
		}
	} else {
		composer = chat.Composer{
			Draft: a.draft.Get, OnDraftChange: a.setDraft, OnPaste: a.pasteAttachments,
			Label: i18n.T("Message"), Placeholder: i18n.T("Ask the agent…"), SendLabel: i18n.T("Send"), StopLabel: i18n.T("Stop"),
			Running: a.running.Get, OnSend: a.send, OnStop: a.stop,
			Attachments:    func(cx *core.Scope) core.Node { return a.attachmentChips(cx) },
			HasAttachments: func() bool { return len(a.attachments.Get()) > 0 },
			Accessory:      accessory, OnDropFiles: a.attachPaths,
			Trailing: func(cx *core.Scope) core.Node { return core.Dynamic(a.composerControls) }, TestID: "composer-card",
		}
	}
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		core.View{Style: ui.FillRest, Children: []core.Node{
			chat.Conversation{State: a.conv, JumpLabel: i18n.T("Jump to latest"), TestID: "transcript", Children: []core.Node{
				core.Show{When: func() bool { return len(a.messages.Get()) == 0 }, Then: func(*core.Scope) core.Node {
					return core.View{Style: core.Style{Padding: core.Symmetric(40, 0), Gap: 6, AlignItems: core.AlignCenter}, Children: []core.Node{
						ui.Text(i18n.T("No messages yet"), 14, ui.Semibold, colors.Muted),
						ui.Text(i18n.T("Ask the agent to change this project, and its work streams here."), 12.5, ui.Regular, colors.Faint),
					}}
				}},
				groups,
			}},
		}},
		// The Android IME overlays the bottom of the surface. Keep the
		// composer above it while the input is focused.
		core.View{Style: core.Style{
			Padding:    core.Edges{Top: 8, Right: 16, Bottom: 16, Left: 16},
			PaddingEnv: core.EnvEdges{Bottom: core.EnvKeyboard},
			AlignItems: core.AlignCenter,
		}, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(chat.ContentWidth)}, Children: []core.Node{
				a.queuedBlock(cx),
				core.Dynamic(a.planBanner),
				core.Dynamic(a.composerMentions),
				core.Dynamic(a.composerSuggestions),
				core.View{TestID: "composer-drop", OnFileDrop: a.attachPaths, Children: []core.Node{composer}},
			}},
		}},
	}}
}

func (a *app) renderTurnContent(cx *core.Scope, group turnGroup) []core.Node {
	running := a.turnRunning(group)
	content := splitTurn(group.Messages, running)
	var rows []core.Node
	for _, m := range content.Users {
		rows = append(rows, a.renderMessage(cx, m))
	}

	process := func() core.Node {
		if len(content.Process) == 0 {
			return core.View{}
		}
		children := make([]core.Node, 0, len(content.Process))
		for _, m := range content.Process {
			children = append(children, a.renderMessage(cx, m))
		}
		return core.View{TestID: "turn-process-" + group.ID,
			Style: core.Style{Direction: core.Column, Gap: 8, MinWidth: core.Pt(0)}, Children: children}
	}

	if running {
		rows = append(rows, process())
	} else if len(content.Process) > 0 {
		key := "turn:" + group.ID
		open := func() bool { return a.isExpanded(key, false) }
		summary := turnSummary(content.Thoughts, content.Tools, len(content.Process))
		rows = append(rows, core.View{Style: core.Style{Direction: core.Column, Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
			turnFoldRow(cx, summary, open, func() { a.setExpanded(key, !open()) }, "turn-summary-"+group.ID),
			core.Show{When: open, Then: func(*core.Scope) core.Node { return process() }},
		}})
	}

	if content.Final != nil {
		rows = append(rows, a.renderMessage(cx, *content.Final))
	}
	for _, m := range content.Retained {
		rows = append(rows, a.renderMessage(cx, m))
	}
	return rows
}

func turnSummary(thoughts, tools, steps int) string {
	var parts []string
	if thoughts > 0 {
		if thoughts == 1 {
			parts = append(parts, i18n.T("Thought once"))
		} else {
			parts = append(parts, i18n.T("Thought %d times", thoughts))
		}
	}
	if tools > 0 {
		if tools == 1 {
			parts = append(parts, i18n.T("1 tool call"))
		} else {
			parts = append(parts, i18n.T("%d tool calls", tools))
		}
	}
	if len(parts) == 0 {
		if steps == 1 {
			return i18n.T("1 message")
		}
		return i18n.T("%d messages", steps)
	}
	return strings.Join(parts, " · ")
}

// turnFoldRow is the divider-centered summary of a finished turn.
func turnFoldRow(cx *core.Scope, summary string, open func() bool, toggle func(), testID string) core.Node {
	colors := ui.ColorsOf(cx)
	divider := func() core.Node {
		return core.View{Style: core.Style{FlexGrow: 1, Height: core.Pt(1)}, StyleFunc: ui.Filled(colors.Border)}
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Height: core.Pt(26), Gap: 10}, Children: []core.Node{
		divider(),
		base.Button{Expanded: open, OnPress: func(core.PressEvent) { toggle() }, Label: summary,
			TestID: testID,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Height: core.Pt(24), Padding: core.Symmetric(0, 3), Gap: 5,
				CornerRadius: ui.RadiusControl},
			StyleFunc: func() core.Style { return core.Style{Focus: ui.FocusRing(cx, 2)} },
			Children: []core.Node{
				ui.Text(summary, 12, ui.Medium, colors.Muted),
				ui.IconOf(func() *core.SVG {
					if open() {
						return lucide.ChevronDown()
					}
					return lucide.ChevronRight()
				}, 11, colors.Faint),
			}},
		divider(),
	}}
}

// planBanner offers to leave plan mode once a planning turn finishes.
func (a *app) planBanner(cx *core.Scope) core.Node {
	if !a.planReady() {
		return core.View{}
	}
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
		Padding: core.Symmetric(7, 10), Background: ui.Alpha(colors.Accent(), 0.08), CornerRadius: 8,
		BorderWidth: 1, BorderColor: ui.Alpha(colors.Accent(), 0.24)}, Children: []core.Node{
		ui.Icon(lucide.Lightbulb, 15, colors.Accent),
		core.Text{Content: i18n.T("Plan ready — review it, then start implementing."), MaxLines: 2,
			Style:     core.Style{FontSize: 11.5, FlexGrow: 1, FlexShrink: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		ui.Button{Text: i18n.T("Implement"), Icon: lucide.ClipboardCheck, Variant: ui.ButtonPrimary, Size: controlSize(cx),
			TestID: "plan-implement", OnPress: a.approvePlan},
	}}
}

// composerModelControl chooses the thread's model.
func (a *app) composerModelControl(cx *core.Scope) core.Node {
	thread := a.detail.Get().Thread
	if thread.ID == "" {
		return core.View{}
	}
	colors := ui.ColorsOf(cx)
	var modelItems []core.Node
	modelLabel := shortModel(thread.Model)
	for _, model := range a.allModels() {
		model := model
		label := model.Name
		if model.Provider != thread.Provider {
			label += " · " + model.Provider
		}
		if model.ID == thread.Model && model.Provider == thread.Provider {
			modelLabel = shortModel(model.Name)
		}
		modelItems = append(modelItems, ui.MenuItem{Text: label, Icon: a.providerMark(model.Provider),
			TestID: "composer-model-" + model.ID,
			Checked: func() bool {
				return a.detail.Get().Thread.Model == model.ID && a.detail.Get().Thread.Provider == model.Provider
			},
			OnSelect: func() { a.setThreadModel(model) }})
	}
	var model core.Node = ui.Text(modelLabel, 10.5, ui.Regular, colors.Faint)
	if len(modelItems) > 0 {
		model = ui.Menu{TestID: "composer-model", MaxHeight: 360,
			Trigger: ui.Button{Text: modelLabel, Label: i18n.T("Choose model: ") + modelLabel, Flex: 1},
			Face: core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
				MinHeight: core.Pt(44), MinWidth: core.Pt(0), Gap: 8, Padding: core.Symmetric(0, 8)},
				Children: []core.Node{
					core.View{TestID: "composer-model-provider", Children: []core.Node{
						ui.Icon(a.providerMark(thread.Provider), 20, colors.Muted)}},
					core.Text{Content: modelLabel, TestID: "composer-model-name", MaxLines: 1,
						Style:     core.Style{FontSize: 14, FlexShrink: 1, MinWidth: core.Pt(0)},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					ui.Icon(lucide.ChevronDown, 16, colors.Muted),
				}},
			Children: modelItems,
			Open:     a.modelMenuOpen.Get,
			OnOpenChange: func(open bool, _ base.Reason) {
				a.modelMenuGuard.change(open, a.modelMenuOpen.Set)
			}}
	}
	return model
}

// composerTrailingControls shows the phone composer's mode picker beside
// its send button.
func (a *app) composerTrailingControls(cx *core.Scope) core.Node {
	thread := a.detail.Get().Thread
	if thread.ID == "" {
		return core.View{}
	}
	var permItems []core.Node
	for _, permission := range []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
		proto.PermissionAuto, proto.PermissionFullAccess} {
		permission := permission
		permItems = append(permItems, ui.MenuItem{Text: string(permission), TestID: "composer-perm-" + string(permission),
			Checked:  func() bool { return a.detail.Get().Thread.Permission == permission },
			OnSelect: func() { a.setThreadPermission(permission) }})
	}
	if ui.Touch(cx) && cx.Env().Window().Width < 520 {
		var items []core.Node
		items = append(items, ui.MenuItem{Text: i18n.T("Permission"), Disabled: func() bool { return true }})
		items = append(items, permItems...)
		items = append(items, ui.MenuItem{Text: i18n.T("Plan mode"), TestID: "composer-plan",
			Checked:  func() bool { return a.detail.Get().Thread.Plan },
			OnSelect: func() { a.setThreadPlan(!a.detail.Get().Thread.Plan) }})
		return ui.Menu{TestID: "composer-more", MaxHeight: 320,
			Trigger: ui.Button{Icon: lucide.SlidersHorizontal, Variant: ui.ButtonGhost, Side: 44, IconSize: 22,
				Label: i18n.T("Composer options")},
			Children: items,
			Open:     a.composerMenuOpen.Get,
			OnOpenChange: func(open bool, _ base.Reason) {
				a.composerMenuGuard.change(open, a.composerMenuOpen.Set)
			}}
	}
	return core.View{}
}

// composerControls are the desktop thread's model and permission pickers.
func (a *app) composerControls(cx *core.Scope) core.Node {
	thread := a.detail.Get().Thread
	if thread.ID == "" {
		return core.View{}
	}
	colors := ui.ColorsOf(cx)
	var permItems []core.Node
	for _, permission := range []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
		proto.PermissionAuto, proto.PermissionFullAccess} {
		permission := permission
		permItems = append(permItems, ui.MenuItem{Text: string(permission), TestID: "composer-perm-" + string(permission),
			Checked:  func() bool { return a.detail.Get().Thread.Permission == permission },
			OnSelect: func() { a.setThreadPermission(permission) }})
	}
	planVariant := ui.ButtonGhost
	if thread.Plan {
		planVariant = ui.ButtonPrimary
	}
	contextLabel := ""
	if used, limit := contextUsage(thread.Model, a.detail.Get().Runs, a.allModels()); used > 0 {
		remaining := limit - used
		if remaining < 0 {
			remaining = 0
		}
		contextLabel = humanTokens(remaining) + i18n.T(" left")
	}
	model := core.Func(a.composerModelControl)
	children := []core.Node{
		model,
		ui.Menu{TestID: "composer-permission", MaxHeight: 240,
			Trigger:  ui.Button{Text: string(thread.Permission), Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: i18n.T("Permission")},
			Children: permItems},
		ui.Button{Text: i18n.T("Plan"), Variant: planVariant, Size: ui.SizeSmall, TestID: "composer-plan",
			OnPress: func() { a.setThreadPlan(!thread.Plan) }},
	}
	if contextLabel != "" {
		children = append(children, core.Text{Content: contextLabel, MaxLines: 1, Style: core.Style{FontSize: 10},
			StyleFunc: func() core.Style {
				if used, limit := contextUsage(thread.Model, a.detail.Get().Runs, a.allModels()); used*10 >= limit*9 {
					return core.Style{Color: colors.Danger()}
				}
				return core.Style{Color: colors.Faint()}
			}})
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2}, Children: children}
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

// contextUsage estimates the last turn's context use and the model's
// context window.
func contextUsage(model string, runs []proto.Run, models []proto.Model) (used, limit int64) {
	limit = 200_000
	switch name := strings.ToLower(model); {
	case strings.Contains(name, "gemini"):
		limit = 1_000_000
	case strings.Contains(name, "gpt-5"), strings.Contains(name, "gpt-6"), strings.Contains(name, "codex"):
		limit = 400_000
	}
	for _, m := range models {
		if m.ID == model && m.ContextWindow > 0 {
			limit = int64(m.ContextWindow)
			break
		}
	}
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Usage == nil {
			continue
		}
		usage := runs[i].Usage
		used = usage.TotalTokens
		if used == 0 {
			used = usage.InputTokens + usage.OutputTokens
		}
		if usage.ContextWindow > 0 {
			limit = usage.ContextWindow
		}
		break
	}
	return used, limit
}

// attachmentChips are the files the next message carries.
func (a *app) attachmentChips(cx *core.Scope) core.Node {
	return a.attachmentChipsFor(cx, a.attachments.Get(), "remove-attachment-", a.removeAttachment)
}

func (a *app) newThreadAttachmentChips(cx *core.Scope) core.Node {
	return a.attachmentChipsFor(cx, a.newThreadFiles.Get(), "remove-new-thread-attachment-", a.removeNewThreadAttachment)
}

func (a *app) attachmentChipsFor(cx *core.Scope, files []proto.Attachment,
	testPrefix string, remove func(string)) core.Node {
	colors := ui.ColorsOf(cx)
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
			ui.Button{Text: "×", Variant: ui.ButtonGhost, Size: controlSize(cx), Label: i18n.T("Remove attachment"),
				TestID: testPrefix + f.ID, OnPress: func() { remove(f.ID) }},
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
	return core.Dynamic(func(cx *core.Scope) core.Node {
		queue := a.queue.Get()
		if len(queue) == 0 {
			return core.View{}
		}
		rows := make([]core.Node, 0, len(queue))
		if a.queuePaused.Get() {
			rows = append(rows, ui.Button{Text: i18n.T("Resume queued messages"), TestID: "queue-resume",
				Variant: ui.ButtonSecondary, Size: controlSize(cx), OnPress: func() {
					a.queuePaused.Set(false)
					a.maybeFlush(a.selected.GetUntracked())
				}})
		}
		for i, prompt := range queue {
			i, prompt := i, prompt
			threadID := a.selected.GetUntracked()
			lines := []core.Node{
				core.Text{Content: prompt.Text, MaxLines: 2, Style: core.Style{FontSize: 12, LineHeight: 17},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}
			if len(prompt.Attachments) > 0 {
				lines = append(lines, ui.Text(attachmentSummary(prompt.Attachments), 11, ui.Regular, colors.Faint))
			}
			label := "Queued"
			if prompt.Uncertain {
				label = i18n.T("Check delivery")
			}
			var actions []core.Node
			if prompt.Uncertain && prompt.RequestID != "" && a.idempotentMessages {
				actions = append(actions,
					ui.Button{Text: i18n.T("Check receipt"), Variant: ui.ButtonGhost, Size: controlSize(cx),
						TestID: fmt.Sprintf("queued-check-%d", i), OnPress: func() { a.checkQueuedDelivery(threadID, prompt.RequestID) }},
					ui.Button{Text: i18n.T("Retry same request"), Variant: ui.ButtonSecondary, Size: controlSize(cx),
						TestID: fmt.Sprintf("queued-retry-%d", i), OnPress: func() { a.retryQueuedRequest(threadID, prompt.RequestID) }})
			} else {
				actions = append(actions,
					ui.IconButton{Icon: lucide.Pencil, Label: i18n.T("Edit"), Variant: ui.IconButtonGhost,
						Size: iconButtonSize(cx), TestID: fmt.Sprintf("queued-edit-%d", i),
						OnPress: func() {
							if strings.TrimSpace(a.draft.GetUntracked()) != "" {
								a.connErr.Set(i18n.T("Save or send the current draft before editing a queued message."))
								return
							}
							id := a.selected.GetUntracked()
							current := a.queued[id]
							a.queued[id] = append(current[:i:i], current[i+1:]...)
							a.draft.Set(prompt.Text)
							a.attachments.Set(prompt.Attachments)
							a.persistDrafts()
							a.saveAttachmentDraft()
							a.refreshQueue()
						}})
			}
			actions = append(actions,
				ui.IconButton{Icon: lucide.X, Label: i18n.T("Remove queued message"), Variant: ui.IconButtonGhost,
					Size: iconButtonSize(cx), TestID: fmt.Sprintf("queued-remove-%d", i),
					OnPress: func() { a.dropQueued(i) }})
			rows = append(rows, core.View{Style: core.Style{Gap: 4,
				Padding: core.Symmetric(6, 10), Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{
				ui.Text(label, 11, ui.Semibold, colors.Faint),
				core.View{Style: core.Style{Gap: 2, MinWidth: core.Pt(0)}, Children: lines},
				// The message's actions sit at the end of its row, next to
				// the text they act on.
				core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter,
					JustifyContent: core.JustifyEnd, Gap: 2}, Children: actions},
			}})
		}
		return core.View{TestID: "queued", Style: core.Style{Gap: 4, Padding: core.Edges{Bottom: 6}}, Children: rows}
	})
}

func (a *app) renderMessage(cx *core.Scope, m proto.Message) core.Node {
	colors := ui.ColorsOf(cx)
	if m.Role == proto.RoleUser {
		body := []core.Node{}
		body = append(body, core.Text{Content: i18n.T("You"), Style: core.Style{FontSize: 10.5, FontWeight: ui.Semibold},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
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
		return core.View{Style: core.Style{Direction: core.Column, AlignItems: core.AlignEnd, Gap: 3, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.View{Style: core.Style{MaxWidth: core.Pt(560), Background: colors.Inset(), Padding: core.Symmetric(10, 14), CornerRadius: 14,
				Direction: core.Column, Gap: 5, BorderWidth: 1, BorderColor: ui.Alpha(colors.Muted(), 0.12)}, Children: body},
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
				ui.Spinner{Size: 13, Label: i18n.T("Working")}, ui.Text(i18n.T("Working…"), 12, ui.Regular, colors.Faint),
			}}
		}
		return core.View{}
	}
	body := ui.TextView{MarkdownFunc: func() string { return a.markdownPreview(m.Content, "") }, Selectable: true, OpenLinks: true,
		CodeBlockActions: []textview.CodeAction{{ID: textview.CopyAction, Label: i18n.T("Copy code")}}}
	footer := a.messageFooter(cx, m)
	identity := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6,
		Padding: core.Edges{Bottom: 2}}, Children: []core.Node{
		brandWordmark(cx, 11),
	}}
	if m.Status == "cancelled" {
		return core.View{Style: core.Style{Direction: core.Column, Gap: 3, MinWidth: core.Pt(0)}, Children: []core.Node{
			identity, body, ui.Text(i18n.T("Stopped."), 11.5, ui.Regular, colors.Faint), footer}}
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 3, MinWidth: core.Pt(0)}, Children: []core.Node{identity, body, footer}}
}

// reasoningCard folds a thinking stream into a disclosure. Process details
// start closed; the turn remains legible from the labels and status marks,
// and a reader can inspect any step on demand.
func (a *app) reasoningCard(cx *core.Scope, m proto.Message) core.Node {
	colors := ui.ColorsOf(cx)
	key := "reasoning:" + m.ID
	running := m.Status == "running"
	title := "Thought"
	if running {
		title = i18n.T("Thinking")
	}
	trigger := []core.Node{ui.Icon(lucide.Brain, 12, colors.Faint), ui.Text(title, 11.5, ui.Semibold, colors.Muted)}
	if running {
		trigger = append(trigger, ui.Spinner{Size: 11, Label: i18n.T("Thinking")})
	} else {
		trigger = append(trigger, core.Show{When: func() bool { return !a.isExpanded(key, false) }, Then: func(*core.Scope) core.Node {
			preview := oneLine(m.Content, 90)
			if preview == "" {
				return core.View{}
			}
			return core.Text{Content: preview, MaxLines: 1, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }}
		}})
	}
	return core.View{Style: core.Style{MinWidth: core.Pt(0), Padding: core.Symmetric(7, 9),
		Background: colors.Inset(), BorderWidth: 1, BorderColor: ui.Alpha(colors.Muted(), 0.13), CornerRadius: 9}, Children: []core.Node{
		ui.Collapsible{Variant: ui.CollapsibleArrowLeft, Text: title, Trigger: trigger,
			Open:         func() bool { return a.isExpanded(key, false) },
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
	open := func() bool { return a.isExpanded(key, false) }
	head := m.Title
	if head == "" {
		head = "Tool"
	}
	summary := toolSummary(m.Detail)
	label := []core.Node{
		ui.Icon(toolIcon(head), 13, colors.Muted),
		core.Text{Content: head, MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold,
			FlexShrink: 1, MinWidth: core.Pt(0)},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
	}
	if summary != "" {
		label = append(label, core.Text{Content: summary, TestID: "tool-summary-" + m.ID, MaxLines: 1,
			Style: core.Style{FontSize: 11.5, FontFamily: "Menlo, monospace", FlexGrow: 1, FlexShrink: 1,
				MinWidth: core.Pt(0), Overflow: core.Hidden},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
	}
	trigger := []core.Node{toolDisclosureArrow(cx, open),
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4,
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Overflow: core.Hidden}, Children: label}}
	switch m.Status {
	case "running":
		trigger = append(trigger, ui.Spinner{Size: 12, Label: i18n.T("Running ") + head})
	case "failed":
		trigger = append(trigger, ui.Icon(lucide.CircleX, 13, colors.Danger))
	case "completed":
		trigger = append(trigger, ui.Icon(lucide.CircleCheck, 13, colors.Faint))
	}
	files := toolChangedFiles(head, m.Detail)
	if len(files) > 0 {
		label := i18n.T("1 file")
		if len(files) > 1 {
			label = i18n.T("%d files", len(files))
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
		blocks = append(blocks, ui.Text(i18n.T("No output"), 11.5, ui.Regular, colors.Faint))
	}
	return core.View{Style: core.Style{MinWidth: core.Pt(0), Padding: core.Symmetric(6, 9),
		Background: ui.Alpha(colors.Inset(), 0.72), BorderWidth: 1, BorderColor: ui.Alpha(colors.Muted(), 0.1), CornerRadius: 8}, Children: []core.Node{
		base.Collapsible{Open: open, Reveal: func() bool { return true }, TestID: "tool-" + m.ID,
			Content: core.View{Style: core.Style{Direction: core.Column, Gap: 6, MinWidth: core.Pt(0),
				Padding: core.Edges{Top: 4, Bottom: 4, Left: 22}}, Children: blocks},
			Children: []core.Node{base.Button{Expanded: open, Label: head,
				OnPress: func(core.PressEvent) {
					a.setExpanded(key, !core.Untracked(cx, open))
				},
				Style: core.Style{Direction: core.Row, AlignSelf: core.AlignStretch, AlignItems: core.AlignCenter,
					Gap: 4, Padding: core.Edges{Top: 4, Right: 8, Bottom: 4, Left: 2}, MinWidth: core.Pt(0),
					FlexShrink: 1, Overflow: core.Hidden, CornerRadius: ui.RadiusControl},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text(), Focus: ui.FocusRing(cx, 2)} },
				Children:  trigger}},
		}},
	}
}

func toolDisclosureArrow(cx *core.Scope, open func() bool) core.Node {
	icon := ui.IconOf(lucide.ChevronRight, 13, ui.ColorsOf(cx).Muted)
	return core.View{Style: core.Style{FlexShrink: 0, Width: core.Pt(13), Height: core.Pt(13)}, StyleFunc: func() core.Style {
		var degrees float32
		if open() {
			degrees = 90
		}
		style := core.Style{Transform: core.Transform{Rotate: degrees}}
		if !ui.ReduceMotion(cx) {
			style.Transition = core.Transition{Duration: 200 * time.Millisecond, Easing: core.EaseOut,
				Properties: core.TransitionTransform}
		}
		return style
	}, Children: []core.Node{icon}}
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
		}, Label: i18n.T("Open ") + path, TestID: "tool-file-" + relative,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5, Width: core.Pct(100),
				Padding: core.Symmetric(2, 4), CornerRadius: 5, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Hover()}, Focus: ui.FocusRing(cx, -2)}
			}, Children: row})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 2}, Children: []core.Node{
		ui.Text(i18n.T("Changed files"), 10, ui.Semibold, colors.Faint),
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
		return i18n.T("Today")
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
			return ui.IconButton{Icon: lucide.Copy, Label: i18n.T("Copy message"), Size: 20, IconSize: 11,
				Tint: colors.Faint, TestID: "copy-message-" + m.ID, OnPress: func() { a.cx.SetClipboard(m.Content) }}
		}},
	}
	if m.Role == proto.RoleUser {
		actions = append(actions,
			ui.IconButton{Icon: lucide.Pencil, Label: i18n.T("Edit message"), Size: 20, IconSize: 11, Tint: colors.Faint,
				TestID: "edit-message-" + m.ID, OnPress: func() { a.openEditMessage(m) }},
		)
	}
	if m.Role == proto.RoleAssistant && m.RunID != "" {
		actions = append(actions, ui.IconButton{Icon: lucide.RotateCcw, Label: i18n.T("Retry turn"), Size: 20, IconSize: 11,
			Tint: colors.Faint, TestID: "retry-message-" + m.ID, Disabled: a.running.Get,
			OnPress: func() { a.retryRun(m.RunID) }})
	}
	actions = append(actions, ui.IconButton{Icon: lucide.GitBranch, Label: i18n.T("Branch from here"), Size: 20, IconSize: 11,
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
			parts = append(parts, humanTokens(run.Usage.TotalTokens)+i18n.T(" tokens"))
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
