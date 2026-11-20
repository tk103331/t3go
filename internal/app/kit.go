package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/tk/t3go/internal/app/icons/lucide"
	"github.com/tk/t3go/internal/i18n"
)

// touch reports whether the pointing device is a finger, so the layout
// gives targets the room a fingertip needs.
func touch(c *ui.Context) bool {
	return c.Preferences().Pointer == ui.PointerCoarse
}

// controlMinHeight is the smallest height of a tappable control.
func controlMinHeight(c *ui.Context) float32 {
	if touch(c) {
		return 40
	}
	return 30
}

// iconSize is the size of the glyphs in icon buttons.
func iconSize(c *ui.Context) float32 {
	if touch(c) {
		return 20
	}
	return 16
}

// compact reports the phone-ish layout the settings dialogs and headers
// switch to.
func compact(c *ui.Context) bool {
	w, _ := c.Size()
	return w < 760
}

// narrow reports the phone layout.
func narrow(c *ui.Context) bool {
	w, _ := c.Size()
	return w < 520
}

// text writes a line of text in one style: the size, the color and an
// optional weight.
func text(c *ui.Context, s string, size float32, color ui.Color, weight ...int) ui.Element {
	t := ui.Text(c, s).FontSize(size).TextColor(color)
	for _, w := range weight {
		if w > 0 {
			t.FontWeight(w)
		}
	}
	return t
}

// muted is the theme's secondary text color.
func muted(c *ui.Context) ui.Color { return c.Theme().TextMuted }

// faintText writes small, quiet text.
func faint(c *ui.Context, s string, size float32) ui.Element {
	return text(c, s, size, c.Theme().TextMuted)
}

// iconBtn is a small button showing one icon, named for assistive
// technology and showing its action on hover.
func iconBtn(c *ui.Context, svg *ui.SVG, label string, clicked func()) ui.Element {
	size := touch(c) && compact(c)
	b := ui.Button(c, "").Label(label).Children(func() {
		ui.Icon(c, svg).FontSize(iconSize(c))
	})
	if size {
		b.Size(38, 38)
	}
	b.Tooltip(label)
	if clicked != nil {
		b.OnClick(clicked)
	}
	return b
}

// iconBtnSel is an icon button that shows itself active.
func iconBtnSel(c *ui.Context, svg *ui.SVG, label string, selected bool, clicked func()) ui.Element {
	b := iconBtn(c, svg, label, clicked)
	if selected {
		b.Background(c.Theme().SurfacePressed)
	}
	return b
}

// settingsSectionTitle heads a settings section.
func settingsSectionTitle(c *ui.Context, title, note string) {
	ui.Column(c).Gap(3).Children(func() {
		text(c, title, 15, c.Theme().Text, 600)
		if note != "" {
			text(c, note, 12, c.Theme().TextMuted)
		}
	})
}

// errorNote is one line of danger-colored text.
func errorNote(c *ui.Context, s string) {
	ui.Text(c, s).FontSize(12).TextColor(c.Theme().Danger).MaxLines(3)
}

// brandWordmark is a minimal two-color T3Go wordmark with emphasis on T3.
func brandWordmark(c *ui.Context, size float32) ui.Element {
	goSize := size * 0.62
	if goSize < 8 {
		goSize = 8
	}
	return ui.Row(c).AlignItems(ui.Baseline).Gap(1).Children(func() {
		ui.Text(c, "T3").FontSize(size).Bold().TextColor(c.Theme().Accent)
		ui.Text(c, "go").FontSize(goSize).FontWeight(600).TextColor(c.Theme().TextMuted)
	})
}

// connectField is a labelled field the connect and project dialogs use.
func connectField(c *ui.Context, label string, value *string, placeholder string, password bool) {
	ui.Field(c, label, func() {
		in := ui.TextInput(c, value).Placeholder(placeholder)
		if password {
			in.Password()
		}
	})
}

// segmentChoice is a row of buttons choosing one value, as the language
// and theme pickers show.
func segmentChoice(c *ui.Context, choices []string, current string, choose func(string)) {
	ui.Row(c).Gap(6).Wrap().Children(func() {
		for _, choice := range choices {
			name := choice
			var b ui.Element
			if name == current {
				b = ui.PrimaryButton(c, name)
			} else {
				b = ui.Button(c, name)
			}
			b.OnClick(func() { choose(name) })
		}
	})
}

var _ = lucide.X() // keep the import until the views fill in
var _ = i18n.T

// clickableRow is a row that acts on a click anywhere — a plain element
// in a dialog or a panel, not only a List's row. It draws like the other
// rows, keeps a label for assistive technology and the tests, and
// reports Clicked where a List row's RowClicked would.
func clickableRow(c *ui.Context, label string, build func()) ui.Element {
	return ui.ButtonBase(c).Label(label).FillWidth().Children(build)
}
