package main

import (
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

// mobileComposer is the phone composer. Its send icon is a fixed node whose
// press handler checks the current draft, instead of swapping a text pill
// when the draft changes. This avoids an Android renderer issue where the
// pill's visual face can disappear while its hit target remains.
type mobileComposer struct {
	Draft                  func() string
	OnDraftChange          func(string)
	Label, Placeholder     string
	Accessory, Attachments func(*core.Scope) core.Node
	HasAttachments         func() bool
	OnPaste                func(core.PasteEvent)
	OnDropFiles            func([]string)
	Running                func() bool
	OnSend, OnStop         func()
	TestID                 string
}

func (m mobileComposer) Render(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	draft := func() string {
		if m.Draft == nil {
			return ""
		}
		return m.Draft()
	}
	focused, dragging := core.NewSignal(cx, false), core.NewSignal(cx, false)
	height := core.NewSignal(cx, core.Points(20))

	editor := base.Input{
		Value: draft, OnChange: m.OnDraftChange, Multiline: true,
		SubmitBehavior: core.SubmitBehaviorNewline, Label: m.Label,
		Placeholder: m.Placeholder, TestID: "composer", OnPaste: m.OnPaste,
		OnFocus: func() { focused.Set(true) },
		OnBlur:  func() { focused.Set(false) },
		OnContentSizeChange: func(s core.Size) {
			if s.Height < 20 {
				s.Height = 20
			}
			if s.Height > 200 {
				s.Height = 200
			}
			height.Set(s.Height)
		},
		Style: core.Style{
			Padding:   core.Edges{Top: 2, Right: 14, Left: 14},
			MinHeight: core.Pt(40), FontSize: 13.5, LineHeight: 20,
			TextAlignVertical: core.TextAlignVerticalTop,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Height: core.Pt(height.Get() + 2), Color: colors.Text(),
				PlaceholderColor: colors.Faint(), SelectionColor: cx.Palette().Selection,
				CaretColor: colors.Accent(),
			}
		},
	}

	send := ui.IconButton{
		IconFunc: func() *core.SVG {
			if m.Running != nil && m.Running() && strings.TrimSpace(draft()) == "" {
				return lucide.Square()
			}
			return lucide.Send()
		},
		LabelFunc: func() string {
			if m.Running != nil && m.Running() && strings.TrimSpace(draft()) == "" {
				return "Stop"
			}
			return "Send"
		},
		Variant: ui.IconButtonPrimary, Size: 36, IconSize: 18, TestID: "composer-send",
		OnPress: func() {
			if m.Running != nil && m.Running() && strings.TrimSpace(draft()) == "" {
				if m.OnStop != nil {
					m.OnStop()
				}
				return
			}
			if strings.TrimSpace(draft()) != "" && m.OnSend != nil {
				m.OnSend()
			}
		},
	}
	row := []core.Node{
		core.View{
			Style: core.Style{
				Direction: core.Row, FlexGrow: 1, FlexShrink: 1,
				MinWidth: core.Pt(0), AlignItems: core.AlignCenter, Gap: 4,
			},
			Children: []core.Node{core.Func(m.Accessory)},
		},
		send,
	}

	var body []core.Node
	if m.Attachments != nil {
		body = append(body, core.Show{
			When: func() bool {
				return m.HasAttachments != nil && m.HasAttachments()
			},
			Then: func(*core.Scope) core.Node {
				return core.View{
					Style: core.Style{
						Direction: core.Row, Wrap: core.Wrap, Gap: 6,
						Padding: core.Edges{Right: 10, Bottom: 6, Left: 10},
					},
					Children: []core.Node{core.Func(m.Attachments)},
				}
			},
		})
	}
	body = append(body, editor, core.View{
		Style: core.Style{
			Direction: core.Row, MinWidth: core.Pt(0),
			Margin: core.Edges{Top: 8}, Padding: core.Symmetric(0, 10),
			AlignItems: core.AlignCenter, Gap: 8,
		},
		Children: row,
	})

	card := core.View{
		Style: core.Style{
			MinWidth: core.Pt(0), Padding: core.Symmetric(10, 0),
			CornerRadius: 13, BorderWidth: 1,
		},
		StyleFunc: func() core.Style {
			border := colors.Border()
			if focused.Get() {
				border = ui.Mix(border, colors.Accent(), 0.55)
			}
			if dragging.Get() {
				border = ui.Alpha(colors.Accent(), 0.7)
			}
			return core.Style{
				Background: colors.Surface(), BorderColor: border,
				Shadows: []core.BoxShadow{ui.Shadow(0, 2, 8, 0, ui.Black(0.04))},
			}
		},
		Children: body,
	}
	if m.OnDropFiles != nil {
		card.OnFileDragEnter = func() { dragging.Set(true) }
		card.OnFileDragLeave = func() { dragging.Set(false) }
		card.OnFileDrop = func(uris []string) {
			dragging.Set(false)
			m.OnDropFiles(uris)
		}
	}
	return core.View{
		TestID:   m.TestID,
		Style:    core.Style{MinWidth: core.Pt(0), FlexShrink: 0},
		Children: []core.Node{card},
	}
}
