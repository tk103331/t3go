package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// composerSpec is what one composer card shows: the draft it edits, the
// actions around it, and the choices beside its send button. The
// attachments and controls are build functions, since an element is part
// of the frame that builds it.
type composerSpec struct {
	Draft *string
	Label string
	// Placeholder names what the reader can ask for.
	Placeholder string
	// EditorMinLines is how tall the field is at rest.
	EditorMinLines int
	// FocusLines is how many lines the field shows while it has the
	// keyboard focus, one line at rest: a phone's message field makes
	// room for what is typed and gives it back after. Zero keeps
	// EditorMinLines at rest and as focused.
	FocusLines int
	// Running replaces the send button with a stop button.
	Running bool
	// AllowEmpty lets an empty draft be sent, as a new thread's first
	// message, which only names the choices, may be.
	AllowEmpty   bool
	SendDisabled bool
	OnSend       func()
	OnStop       func()
	// Attachments draws the chips of the files the next message carries.
	Attachments func(c *ui.Context)
	// Accessory runs when the reader taps the paperclip.
	Accessory func()
	// Controls draws the choices left of the send button.
	Controls func(c *ui.Context)
	// OnDropFiles takes the files dropped on the card.
	OnDropFiles func([]string)
	// OnPaste takes a paste before the field does; returning true keeps
	// the field out of it.
	OnPaste func(text string) bool
	// SendLabel, StopLabel name the buttons; defaults below.
	SendLabel string
	StopLabel string
	// ContentInset and ContentMaxWidth describe the page's room around
	// the card, and ControlsMinWidth how much the choices and the send
	// action need on one line. A page narrower than that puts its choices
	// on their own row above the actions.
	ContentInset     float32
	ContentMaxWidth  float32
	ControlsMinWidth float32
	// OnChange runs when the draft's text changed in this pass.
	OnChange func()
}

// composerCard is the message field and everything around it, shared by
// the conversation and the new-thread page.
func (s *Session) composerCard(c *ui.Context, spec composerSpec) ui.Element {
	t := c.Theme()
	if spec.SendLabel == "" {
		spec.SendLabel = i18n.T("Send")
	}
	if spec.StopLabel == "" {
		spec.StopLabel = i18n.T("Stop")
	}
	if spec.EditorMinLines == 0 {
		spec.EditorMinLines = 1
	}
	compactLayout := compact(c)
	separateControls := compactLayout && spec.AllowEmpty
	if !compactLayout {
		// Keep the options on the send row for as long as the page's
		// content room allows; a page that names how much its choices need
		// moves them above the actions instead of wrapping them beside the
		// send button. The token reading hides before that.
		contentWidth := s.middleRoom(c) - spec.ContentInset
		if spec.ContentMaxWidth > 0 {
			contentWidth = min(contentWidth, spec.ContentMaxWidth)
		}
		if spec.ControlsMinWidth > 0 {
			separateControls = contentWidth < spec.ControlsMinWidth
		} else {
			separateControls = s.middleRoom(c) < 440
		}
	}
	padV, padH, gap := float32(10), float32(12), float32(8)
	actionSize := float32(28)
	if touch(c) {
		actionSize = 44
	}
	if compactLayout {
		// A phone shows the field with a tighter frame, so more of a
		// short message's line is left for what it says.
		padV, padH, gap = 8, 10, 8
	}
	card := ui.Column(c).WidthPercent(100).Gap(gap).Padding(padV, padH).
		Radius(14).Border(1, t.Border).Background(t.Surface)
	card.Children(func() {
		if spec.Attachments != nil && spec.hasAttachments() {
			spec.Attachments(c)
		}
		field := ui.TextArea(c, spec.Draft).Label(spec.Label).
			Placeholder(spec.Placeholder).MinHeight(0).FontSize(13).FixedLineHeight(20).Autocorrect(false).
			BorderWidth(0).Background(ui.Color{}).FocusRing(false).Padding(5, 2)
		if field.Focused() {
			card.BorderColor(t.Accent.Alpha(0.65))
		}
		switch {
		case spec.FocusLines > 0:
			// One line at rest, room for FocusLines while typing, which
			// the field gives back when it loses the focus.
			if field.Focused() {
				field.Lines(spec.FocusLines, 8)
			} else {
				field.Lines(1, 1)
			}
		default:
			field.Lines(spec.EditorMinLines, 8)
		}
		if compactLayout {
			field.Padding(t.Space(0.75), t.Space(1.5))
		}
		if spec.OnPaste != nil {
			field.OnPaste(spec.OnPaste)
		}
		field.OnTextKey(func(k ui.TextKey) bool {
			if k.Key == ui.KeyEnter && k.Mods == 0 {
				if spec.canSend() {
					spec.OnSend()
					k.SetText("")
					return true
				}
			}
			return false
		})
		if spec.OnChange != nil && field.Changed() {
			spec.OnChange()
		}
		if separateControls && spec.Controls != nil {
			spec.Controls(c)
		}
		// Keep send outside the wrapping choices so it stays visible at
		// every pane width. Only the choices consume extra lines.
		ui.Row(c).FillWidth().AlignItems(ui.End).Gap(6).KeepFocus().Children(func() {
			if spec.Accessory != nil {
				iconBtn(c, lucide.Paperclip(), i18n.T("Attach files"), spec.Accessory).Shrink(0)
			}
			choices := ui.Box(c).Grow(1).MinWidth(0)
			if separateControls {
				choices.AlignSelf(ui.Center)
			}
			choices.Children(func() {
				if separateControls {
					hint := i18n.T("Enter to send")
					if spec.Running {
						hint = i18n.T("Enter to queue")
					}
					faint(c, hint, 10).SingleLine().Tooltip(i18n.T("Shift+Enter for a new line"))
				} else if spec.Controls != nil {
					spec.Controls(c)
				}
			})
			if spec.Running {
				ui.Button(c, "").Label(spec.StopLabel).Size(actionSize, actionSize).Padding(0).Radius(10).
					Shrink(0).Tooltip(spec.StopLabel).Children(func() {
					ui.Icon(c, lucide.CircleStop()).FontSize(15)
				}).OnClick(spec.OnStop)
				return
			}
			// The action is the arrow alone, as in the conversation, with its name
			// kept for assistive technology and its tooltip.
			send := ui.PrimaryButton(c, "").Label(spec.SendLabel).Size(actionSize, actionSize).
				Padding(0).Radius(10).Children(func() {
				ui.Icon(c, lucide.ArrowUp()).FontSize(15)
			}).Shrink(0)
			send.Tooltip(spec.SendLabel + " · " + i18n.T("Enter to send") + " · " + i18n.T("Shift+Enter for a new line"))
			// A candidate being composed is not part of Draft yet. Keep
			// it in the editor until the input method confirms it, rather
			// than sending a message with its last word missing.
			composing := field.Composing()
			if !spec.canSend() || composing {
				send.Disabled(true)
			}
			send.OnClick(func() {
				if spec.canSend() && !composing {
					spec.OnSend()
				}
			})
		})
	})
	if spec.OnDropFiles != nil {
		if files := card.DroppedFiles(); len(files) > 0 {
			spec.OnDropFiles(files)
		}
	}
	return card
}

// hasAttachments says whether the spec draws a files row: the caller
// passes a builder only when it has something to show.
func (spec composerSpec) hasAttachments() bool {
	return spec.Attachments != nil
}

// pasteInto takes a paste into the composer: a file the system clipboard
// holds becomes an attachment, text goes on to the field.
func (s *Session) pasteInto(text string) bool {
	if text != "" {
		return false
	}
	data, err := mygo.Clipboard.Read(transfer.PNG)
	if err != nil {
		return false
	}
	png, err := data.Read(transfer.PNG)
	if err != nil || len(png) == 0 {
		return false
	}
	name := "pasted-" + time.Now().Format("20060102-150405") + ".png"
	s.attachFiles([]pickedFile{{Name: name, MIME: "image/png", Size: int64(len(png)), Data: png}})
	return true
}

// attachmentChipsFor draws the files a message carries, as chips with a
// way to take each out.
func (s *Session) attachmentChipsFor(c *ui.Context, files []proto.Attachment, remove func(string)) {
	t := c.Theme()
	if len(files) == 0 {
		return
	}
	ui.Row(c).Wrap().Gap(6).Children(func() {
		for _, f := range files {
			f := f
			ui.Row(c).AlignItems(ui.Center).Gap(6).Padding(4, 8).
				Radius(8).MaxWidth(280).Background(t.Background).Children(func() {
				ui.Icon(c, lucide.Paperclip()).FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, f.Name+" · "+humanSize(f.Size)).FontSize(11.5).TextColor(t.Text).SingleLine().MinWidth(0)
				ui.Button(c, "×").Label(i18n.T("Remove attachment")).
					Tooltip(i18n.T("Remove attachment")).OnClick(func() { remove(f.ID) })
			})
		}
	})
}

// attachmentChips are the files the next message of the open thread
// carries.
func (s *Session) attachmentChips(c *ui.Context) {
	s.attachmentChipsFor(c, s.attachments, s.removeAttachment)
}

// newThreadAttachmentChips are the files a new thread's first message
// carries.
func (s *Session) newThreadAttachmentChips(c *ui.Context) {
	s.attachmentChipsFor(c, s.newThreadFiles, s.removeNewThreadAttachment)
}

// queuedBlock shows the prompts waiting for the running turn to end.
func (s *Session) queuedBlock(c *ui.Context) {
	t := c.Theme()
	if len(s.queue) == 0 {
		return
	}
	ui.Column(c).Gap(4).Padding(0, 0, 6, 0).Children(func() {
		if s.queuePaused {
			ui.Button(c, i18n.T("Resume queued messages")).OnClick(func() {
				s.queuePaused = false
				s.maybeFlush(s.selected)
			})
		}
		for i, prompt := range s.queue {
			i, prompt := i, prompt
			threadID := s.selected
			ui.Column(c).Gap(4).Padding(6, 10).Background(t.Background).Radius(8).Children(func() {
				label := "Queued"
				if prompt.Uncertain {
					label = i18n.T("Check delivery")
				}
				faint(c, label, 11)
				ui.Text(c, prompt.Text).FontSize(12).FixedLineHeight(17).TextColor(t.TextMuted).MaxLines(2)
				if len(prompt.Attachments) > 0 {
					faint(c, attachmentSummary(prompt.Attachments), 11)
				}
				ui.Row(c).Wrap().Gap(4).Children(func() {
					if prompt.Uncertain && prompt.RequestID != "" && s.idempotentMessages {
						ui.Button(c, i18n.T("Check receipt")).OnClick(func() {
							s.checkQueuedDelivery(threadID, prompt.RequestID)
						})
						ui.Button(c, i18n.T("Retry same request")).OnClick(func() {
							s.retryQueuedRequest(threadID, prompt.RequestID)
						})
					} else {
						ui.Button(c, i18n.T("Edit")).OnClick(func() {
							if strings.TrimSpace(s.draft) != "" {
								s.connErr = i18n.T("Save or send the current draft before editing a queued message.")
								return
							}
							id := s.selected
							current := s.queued[id]
							s.queued[id] = append(current[:i:i], current[i+1:]...)
							s.draft = prompt.Text
							s.attachments = prompt.Attachments
							s.persistDrafts()
							s.saveAttachmentDraft()
							s.refreshQueue()
						})
					}
					ui.Button(c, "×").Label(i18n.T("Remove queued message")).
						Tooltip(i18n.T("Remove queued message")).OnClick(func() { s.dropQueued(i) })
				})
			})
		}
	})
}

// planBanner offers to leave plan mode once a planning turn finishes.
func (s *Session) planBanner(c *ui.Context) {
	if !s.planReady() {
		return
	}
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(7, 10).
		Radius(8).Border(1, t.Accent.Alpha(0.24)).
		Background(t.Accent.Alpha(0.08)).Children(func() {
		ui.Icon(c, lucide.Lightbulb()).FontSize(15).TextColor(t.Accent)
		ui.Text(c, i18n.T("Plan ready — review it, then start implementing.")).
			FontSize(11.5).TextColor(t.Text).MaxLines(2).Grow(1)
		ui.PrimaryButton(c, "").Label(i18n.T("Implement")).Children(func() {
			ui.Icon(c, lucide.ClipboardCheck()).FontSize(14)
			ui.Text(c, i18n.T("Implement"))
		}).OnClick(s.approvePlan)
	})
}

// threadControls are the open thread's model and permission pickers,
// under its composer.
func (s *Session) threadControls(c *ui.Context) {
	thread := s.detail.Thread
	if thread.ID == "" {
		return
	}
	t := c.Theme()
	modelLabel := s.modelDisplayName(thread.Provider, thread.Model)
	ui.Row(c).FillWidth().MinWidth(0).Wrap().AlignItems(ui.Center).Gap(2).Children(func() {
		modelButton := toolbarButton(c, "").Label(i18n.T("Choose model: ") + modelLabel).MaxWidthPercent(100).MinWidth(0).Children(func() {
			ui.Icon(c, lucide.Cpu()).FontSize(13)
			ui.Text(c, shortModel(modelLabel)).FontSize(11.5).SingleLine().MinWidth(0).MaxWidth(150)
			ui.Icon(c, lucide.ChevronDown()).FontSize(10)
		}).Tooltip(modelLabel)
		s.modelPicker(c, modelButton, thread.Provider, thread.Model, s.setThreadModel)
		s.configOptions(c, s.threadConfigOptions(thread))
		// The token reading is the first thing to give up when the row is
		// tight, so the model and config pills keep their place on one
		// line instead of wrapping.
		if s.middleRoom(c) >= 760 {
			if used, limit := contextUsage(thread.Model, s.detail.Runs, s.allModels()); used > 0 {
				remaining := limit - used
				if remaining < 0 {
					remaining = 0
				}
				color := t.TextMuted
				if used*10 >= limit*9 {
					color = t.Danger
				}
				ui.Text(c, humanTokens(remaining)+i18n.T(" left")).FontSize(10).TextColor(color).SingleLine()
			}
		}
	})
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

// canSend says whether the composer's send action is live: the draft
// holds something, or an empty send is allowed.
func (spec composerSpec) canSend() bool {
	if spec.SendDisabled {
		return false
	}
	if spec.AllowEmpty {
		return true
	}
	return strings.TrimSpace(*spec.Draft) != ""
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

// shortModel shortens a model name the way the composer shows it.
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
