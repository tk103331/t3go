package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/netimage"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// turnGroup is one run's messages, so a turn is spaced from the next
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

// turnRunning reports whether the run a group carries is still going.
func (s *Session) turnRunning(group turnGroup) bool {
	if group.ID != "" {
		for _, run := range s.detail.Runs {
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

// threadPane is the open thread: the transcript above the composer, with
// what waits to send between them.
func (s *Session) threadPane(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Box(c).Grow(1).FillWidth().ClipY().Children(func() {
			s.transcript(c)
		})
		ui.Column(c).Padding(8, 16, 16, 16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).WidthPercent(100).MaxWidth(760).Gap(6).Children(func() {
				s.queuedBlock(c)
				s.planBanner(c)
				s.composerMentions(c)
				s.composerSuggestions(c)
				spec := composerSpec{
					Draft:          &s.draft,
					Label:          i18n.T("Message"),
					Placeholder:    i18n.T("Ask the agent…"),
					EditorMinLines: 1,
					Running:        s.running,
					OnSend:         s.send,
					OnStop:         s.stop,
					Attachments:    s.attachmentChips,
					Accessory:      s.pickAttachments,
					OnDropFiles:    s.attachPaths,
					OnPaste:        s.pasteInto,
					OnChange: func() {
						s.setDraft(s.draft)
					},
				}
				if compact(c) {
					spec.Controls = s.threadControlsMobile
				} else {
					spec.Controls = s.threadControls
				}
				s.composerCard(c, spec)
			})
		})
	})
}

// transcript is the thread's list of turns, which follows its end while
// a reply streams in.
func (s *Session) transcript(c *ui.Context) {
	t := c.Theme()
	groups := groupMessages(s.messages)
	s.transcriptList.Key = func(i int) any { return groups[i].ID }
	s.transcriptList.FollowEnd = true
	s.transcriptList.FollowAnimation = &ui.Smooth
	if len(groups) == 0 {
		ui.Column(c).Fill().Center().Gap(6).Children(func() {
			text(c, i18n.T("No messages yet"), 14, t.TextMuted, 600)
			ui.Text(c, i18n.T("Ask the agent to change this project, and its work streams here.")).
				FontSize(12.5).TextColor(t.TextMuted)
		})
		return
	}
	ui.List(c, &s.transcriptList, len(groups), func(i int) {
		s.turn(c, groups[i])
	}).Grow(1).FillWidth().Gap(14).Padding(16, 20).Justify(ui.End).
		Children(func() {
			if !s.transcriptList.AtEnd() && len(groups) > 0 {
				if ui.Button(c, i18n.T("Jump to latest")).Clicked() {
					s.transcriptList.ScrollToEnd()
				}
			}
		})
}

// turn draws one run's messages.
func (s *Session) turn(c *ui.Context, group turnGroup) {
	running := s.turnRunning(group)
	content := splitTurn(group.Messages, running)
	ui.Column(c).Gap(10).MinWidth(0).Children(func() {
		if group.Day != "" {
			s.daySeparator(c, group.Day)
		}
		for _, m := range content.Users {
			s.renderMessage(c, m)
		}
		process := func() {
			for _, m := range content.Process {
				s.renderMessage(c, m)
			}
		}
		if running {
			process()
		} else if len(content.Process) > 0 {
			key := "turn:" + group.ID
			summary := turnSummary(content.Thoughts, content.Tools, len(content.Process))
			ui.Column(c).Gap(8).MinWidth(0).Children(func() {
				turnFoldRow(c, summary, key, s.isExpanded(key, false), func() {
					s.setExpanded(key, !s.isExpanded(key, false))
				})
				if s.isExpanded(key, false) {
					process()
				}
			})
		}
		if content.Final != nil {
			s.renderMessage(c, *content.Final)
		}
		for _, m := range content.Retained {
			s.renderMessage(c, m)
		}
		if footer := s.runFooter(group.ID); footer != "" {
			faint(c, footer, 10.5)
		}
	})
}

// turnSummary writes what a finished turn folded away.
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
func turnFoldRow(c *ui.Context, summary, key string, open bool, toggle func()) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Height(26).Gap(10).Children(func() {
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
		ui.Button(c, "").Label(summary).Children(func() {
			ui.Text(c, summary).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
			icon := lucide.ChevronDown()
			if !open {
				icon = lucide.ChevronRight()
			}
			ui.Icon(c, icon).FontSize(11).TextColor(t.TextMuted)
		}).OnClick(toggle)
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
	})
}

// renderMessage draws one message: the reader's in a bubble, the
// agent's as Markdown, the steps as cards.
func (s *Session) renderMessage(c *ui.Context, m proto.Message) {
	t := c.Theme()
	if m.Role == proto.RoleUser {
		ui.Column(c).AlignItems(ui.End).Gap(3).MinWidth(0).Children(func() {
			ui.Column(c).MaxWidth(560).Padding(10, 14).Radius(14).
				Border(1, t.TextMuted.Alpha(0.12)).Background(t.Surface).
				Gap(5).Children(func() {
				ui.Text(c, i18n.T("You")).FontSize(10.5).FontWeight(600).TextColor(t.TextMuted)
				if m.Content != "" {
					ui.Text(c, m.Content).FontSize(13).LineHeight(19).TextColor(t.Text).Selectable()
				}
				if len(m.Attachments) > 0 {
					ui.Row(c).Wrap().Gap(8).Children(func() {
						for _, f := range m.Attachments {
							f := f
							if strings.HasPrefix(f.MimeType, "image/") {
								netimage.Image(c, s.uploadURL(f.ID)).Size(72, 72).Radius(6)
								continue
							}
							faint(c, f.Name+" · "+humanSize(f.Size), 11)
						}
					})
				}
			})
			s.messageFooter(c, m)
		})
		return
	}
	switch m.Kind {
	case proto.KindReasoning:
		s.reasoningCard(c, m)
		return
	case proto.KindTool:
		s.toolCard(c, m)
		return
	case proto.KindNotice:
		ui.Text(c, m.Content).FontSize(12).LineHeight(17).TextColor(t.TextMuted).Selectable()
		return
	case proto.KindError:
		ui.Column(c).Padding(8, 10).Radius(8).Background(t.Danger.Alpha(0.09)).Children(func() {
			ui.Text(c, m.Content).FontSize(12).LineHeight(17).TextColor(t.Danger).Selectable()
		})
		return
	}
	// Assistant text.
	if strings.TrimSpace(m.Content) == "" {
		if m.Status == "running" {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Spinner(c).Size(13, 13).Label(i18n.T("Working"))
				faint(c, i18n.T("Working…"), 12)
			})
		}
		return
	}
	ui.Column(c).Gap(3).MinWidth(0).Children(func() {
		brandWordmark(c, 11).Margin(0, 0, 2, 0)
		text := s.markdownPreview(m.Content, "")
		if m.Status == "running" {
			markdown.Stream(c, text, &s.md)
		} else {
			markdown.View(c, text, &s.md)
		}
		if m.Status == "cancelled" {
			faint(c, i18n.T("Stopped."), 11.5)
		}
		s.messageFooter(c, m)
	})
}

// foldCard is one disclosure a row toggles: the state lives in the
// session's map of expanded cards, so a rebuild keeps it.
func foldCard(c *ui.Context, key string, s *Session, build func(c *ui.Context, open bool)) {
	build(c, s.isExpanded(key, false))
}

// reasoningCard folds a thinking stream into a disclosure.
func (s *Session) reasoningCard(c *ui.Context, m proto.Message) {
	t := c.Theme()
	key := "reasoning:" + m.ID
	open := s.isExpanded(key, false)
	running := m.Status == "running"
	title := "Thought"
	if running {
		title = i18n.T("Thinking")
	}
	card := ui.Column(c).MinWidth(0).Padding(7, 9).Radius(9).
		Border(1, t.TextMuted.Alpha(0.13)).Background(t.Surface)
	card.Children(func() {
		head := clickableRow(c, title, func() {
			ui.Icon(c, lucide.ChevronRight()).FontSize(12).TextColor(t.TextMuted)
			ui.Icon(c, lucide.Brain()).FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(t.TextMuted)
			if running {
				ui.Spinner(c).Size(11, 11).Label(i18n.T("Thinking"))
			} else if !open {
				if preview := oneLine(m.Content, 90); preview != "" {
					ui.Text(c, preview).FontSize(11.5).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
				}
			}
			ui.Spacer(c)
		}).AlignItems(ui.Center).Gap(5).FillWidth()
		if head.Clicked() {
			s.setExpanded(key, !open)
		}
		if open {
			ui.Text(c, m.Content).FontSize(12).LineHeight(18).TextColor(t.TextMuted).Selectable()
		}
	})
}

// toolCard folds one tool call into a card: its name, a one-line summary
// of the input, its outcome, and the input and output on demand.
func (s *Session) toolCard(c *ui.Context, m proto.Message) {
	t := c.Theme()
	key := "tool:" + m.ID
	open := s.isExpanded(key, false)
	head := m.Title
	if head == "" {
		head = "Tool"
	}
	summary := toolSummary(m.Detail)
	card := ui.Column(c).MinWidth(0).Padding(6, 9).Radius(8).
		Border(1, t.TextMuted.Alpha(0.1)).Background(t.Surface)
	card.Children(func() {
		row := clickableRow(c, head, nil).AlignItems(ui.Center).Gap(4).MinWidth(0).FillWidth()
		row.Children(func() {
			arrow := ui.Icon(c, lucide.ChevronRight()).FontSize(13).TextColor(t.TextMuted)
			if open {
				arrow.Rotate(90)
			}
			ui.Icon(c, toolIcon(head)).FontSize(13).TextColor(t.TextMuted)
			ui.Text(c, head).FontSize(12).FontWeight(600).TextColor(t.Text).SingleLine().MinWidth(0)
			if summary != "" {
				ui.Text(c, summary).FontSize(11.5).Font("monospace").TextColor(t.TextMuted).
					SingleLine().Grow(1).MinWidth(0)
			}
			switch m.Status {
			case "running":
				ui.Spinner(c).Size(12, 12).Label(i18n.T("Running ") + head)
			case "failed":
				ui.Icon(c, lucide.CircleX()).FontSize(13).TextColor(t.Danger)
			case "completed":
				ui.Icon(c, lucide.CircleCheck()).FontSize(13).TextColor(t.TextMuted)
			}
			if files := toolChangedFiles(head, m.Detail); len(files) > 0 {
				label := i18n.T("1 file")
				if len(files) > 1 {
					label = i18n.T("%d files", len(files))
				}
				faint(c, label, 10)
			}
			ui.Spacer(c)
		})
		if row.Clicked() {
			s.setExpanded(key, !open)
		}
		if open {
			ui.Column(c).Gap(6).MinWidth(0).Padding(4, 0, 4, 22).Children(func() {
				if files := toolChangedFiles(head, m.Detail); len(files) > 0 {
					s.changedFilesBlock(c, files)
				}
				if detail := prettyDetail(m.Detail); detail != "" {
					detailBlock(c, "Input", detail)
				}
				if body := truncate(m.Content, 6000); strings.TrimSpace(body) != "" {
					detailBlock(c, "Output", body)
				}
				if m.Detail == "" && strings.TrimSpace(m.Content) == "" {
					faint(c, i18n.T("No output"), 11.5)
				}
			})
		}
	})
}

// changedFilesBlock lists the files a tool call changed.
func (s *Session) changedFilesBlock(c *ui.Context, files []string) {
	t := c.Theme()
	root := ""
	if git := s.detail.Git; git != nil {
		root = git.Root
	}
	ui.Column(c).Gap(2).Children(func() {
		faint(c, i18n.T("Changed files"), 10)
		ui.Column(c).Gap(1).Children(func() {
			for _, path := range files {
				path := path
				relative := toolFileRelative(root, path)
				if relative == "" {
					ui.Row(c).AlignItems(ui.Center).Gap(5).Padding(2, 4).Children(func() {
						ui.Icon(c, lucide.FileText()).FontSize(11).TextColor(t.TextMuted)
						ui.Text(c, path).FontSize(11).Font("monospace").TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
					})
					continue
				}
				target := relative
				fileRow := clickableRow(c, path, func() {
					ui.Icon(c, lucide.FileText()).FontSize(11).TextColor(t.TextMuted)
					ui.Text(c, path).FontSize(11).Font("monospace").TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
				}).AlignItems(ui.Center).Gap(5).Padding(2, 4).Radius(5)
				if fileRow.Clicked() {
					s.showSurface("files")
					s.openFile(target)
				}
			}
		})
	})
}

// detailBlock is a labelled monospaced block of a tool card.
func detailBlock(c *ui.Context, label, body string) {
	t := c.Theme()
	ui.Column(c).Gap(3).MinWidth(0).Children(func() {
		faint(c, label, 10.5)
		ui.Column(c).MinWidth(0).Padding(8).Radius(6).Background(t.Surface).Children(func() {
			ui.Text(c, body).FontSize(11).LineHeight(15).Font("monospace").
				TextColor(t.TextMuted).Selectable()
		})
	})
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
		var indented strings.Builder
		encoder := json.NewEncoder(&indented)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if encoder.Encode(value) == nil {
			return truncate(strings.TrimRight(indented.String(), "\n"), 6000)
		}
	}
	return truncate(detail, 6000)
}

// toolIcon picks a glyph for a tool by its name.
func toolIcon(name string) *ui.SVG {
	name = strings.ToLower(name)
	switch {
	case hasAny(name, "bash", "shell", "exec", "terminal", "command", "run"):
		return lucide.Terminal()
	case hasAny(name, "read", "write", "edit", "patch", "file", "notebook"):
		return lucide.FileText()
	case hasAny(name, "grep", "search", "find", "glob", "list"):
		return lucide.Search()
	case hasAny(name, "fetch", "http", "web", "browser", "url"):
		return lucide.Globe()
	case hasAny(name, "task", "agent", "delegate", "subagent"):
		return lucide.Bot()
	}
	return lucide.Wrench()
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
func (s *Session) daySeparator(c *ui.Context, label string) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(16, 0, 2, 0).Children(func() {
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
		faint(c, label, 10.5)
		ui.Box(c).Grow(1).Height(1).Background(t.Border)
	})
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

// messageFooter is a message's time and its actions.
func (s *Session) messageFooter(c *ui.Context, m proto.Message) {
	ui.Row(c).AlignItems(ui.Center).Gap(3).Padding(0, 0, 0, 2).Children(func() {
		faint(c, m.CreatedAt.Local().Format("3:04 PM"), 10.5)
		if strings.TrimSpace(m.Content) != "" {
			iconBtn(c, lucide.Copy(), i18n.T("Copy message"), func() { s.setClipboard(m.Content) })
		}
		if m.Role == proto.RoleUser {
			iconBtn(c, lucide.Pencil(), i18n.T("Edit message"), func() { s.openEditMessage(m) })
		}
		if m.Role == proto.RoleAssistant && m.RunID != "" {
			b := iconBtn(c, lucide.RotateCcw(), i18n.T("Retry turn"), func() { s.retryRun(m.RunID) })
			if s.running {
				b.Disabled(true)
			}
		}
		iconBtn(c, lucide.GitBranch(), i18n.T("Branch from here"), func() { s.branchFromMessage(m) })
	})
}

// runFooter reports a finished run's model, size and duration under its
// turn.
func (s *Session) runFooter(runID string) string {
	if runID == "" {
		return ""
	}
	var run *proto.Run
	runs := s.detail.Runs
	for i := range runs {
		if runs[i].ID == runID {
			r := runs[i]
			run = &r
			break
		}
	}
	if run == nil || run.Status == proto.RunRunning {
		return ""
	}
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
	return strings.Join(parts, " · ")
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

var markdownImage = regexp.MustCompile(`(!\[[^\]]*\]\()([^)\s]+)([^)]*\))`)

// markdownPreview points relative images at the checkout's raw file URL,
// so a README's screenshots show in the conversation and the files panel.
func (s *Session) markdownPreview(content, path string) string {
	dir := ""
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir = path[:i]
	}
	return markdownImage.ReplaceAllStringFunc(content, func(match string) string {
		parts := markdownImage.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		target := strings.Trim(strings.TrimSpace(parts[2]), "<>")
		lower := strings.ToLower(target)
		if strings.Contains(target, "://") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(target, "#") {
			return match
		}
		joined := target
		if dir != "" {
			joined = dir + "/" + target
		}
		joined = filepath.ToSlash(filepath.Clean(joined))
		return parts[1] + s.rawFileURL(joined) + parts[3]
	})
}

// threadControlsMobile is the phone composer's options menu: the
// permission and the plan toggle the narrow row cannot fit.
func (s *Session) threadControlsMobile(c *ui.Context) {
	thread := s.detail.Thread
	if thread.ID == "" {
		return
	}
	ui.Button(c, "").Label(i18n.T("Composer options")).Children(func() {
		ui.Icon(c, lucide.SlidersHorizontal()).FontSize(20)
	}).PopoverMenuButton(func(m *ui.Menu) {
		for _, permission := range []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
			proto.PermissionAuto, proto.PermissionFullAccess} {
			permission := permission
			if m.Item(string(permission)).Checked(thread.Permission == permission).Chosen() {
				s.setThreadPermission(permission)
			}
		}
		if m.Item(i18n.T("Plan mode")).Checked(thread.Plan).Chosen() {
			s.setThreadPlan(!thread.Plan)
		}
	})
}
