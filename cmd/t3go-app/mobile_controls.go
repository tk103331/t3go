package main

import (
	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui"
)

// controlSize keeps desktop toolbars dense while making touch controls easier
// to hit on phones and tablets.
func controlSize(cx *core.Scope) ui.Size {
	if ui.Touch(cx) {
		return ui.SizeLarge
	}
	return ui.SizeSmall
}

// iconButtonSize gives touch toolbars a little more room without making
// desktop toolbars unnecessarily loose.
func iconButtonSize(cx *core.Scope) core.Points {
	if ui.Touch(cx) {
		return 34
	}
	return 28
}

// mobileInput is the app's larger touch-friendly text field. NSGo's standard
// ui.Input is intentionally dense for desktop toolbars, while form fields on
// Android need a larger hit target and a little more breathing room.
type mobileInput struct {
	Scope *core.Scope
	Input base.Input
}

func (f mobileInput) Render(cx *core.Scope) core.Node {
	if !ui.Touch(f.Scope) {
		return ui.Input{Input: f.Input}
	}

	colors := ui.ColorsOf(cx)
	in := f.Input
	focused := core.NewSignal(cx, false)
	onFocus, onBlur := in.OnFocus, in.OnBlur
	in.OnFocus = func() {
		focused.Set(true)
		if onFocus != nil {
			onFocus()
		}
	}
	in.OnBlur = func() {
		focused.Set(false)
		if onBlur != nil {
			onBlur()
		}
	}

	shape := core.Style{
		MinHeight: core.Pt(44),
		Padding:   core.Symmetric(0, 12),
	}
	if in.Multiline {
		shape = core.Style{
			MinHeight:         core.Pt(56),
			Padding:           core.Symmetric(8, 12),
			TextAlignVertical: core.TextAlignVerticalTop,
		}
	}
	in.StyleFunc = func() core.Style {
		s := core.Style{
			PlaceholderColor: colors.Faint(),
			SelectionColor:   cx.Palette().Selection,
			CaretColor:       colors.Accent(),
			FontSize:         15,
			LineHeight:       20,
			Color:            colors.Text(),
			Background:       colors.Field(),
			BorderWidth:      1,
			BorderColor:      colors.FieldBorder(),
			CornerRadius:     ui.RadiusControl,
			Hover:            &core.Style{BorderColor: ui.Mix(colors.FieldBorder(), colors.Text(), 0.25)},
		}
		if focused.Get() {
			s.BorderColor, s.Hover = colors.Accent(), nil
		}
		return in.Style.Merge(shape).Merge(s)
	}
	return in
}
