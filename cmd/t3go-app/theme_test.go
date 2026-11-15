package main

import (
	"testing"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/nsgotest"
)

// TestAccentThemeKeepsUIRoles keeps NSGo's role colors in the palette when
// an accent is chosen. A theme without them leaves accent fills and insets
// transparent, so a selected control paints its own text on nothing.
func TestAccentThemeKeepsUIRoles(t *testing.T) {
	theme := accentTheme("#7c3aed")
	if theme == nil {
		t.Fatal("accent theme is nil")
	}
	for _, name := range []string{"accentFill", "inset", "raised", "field", "line", "faint", "light", "onLight"} {
		if _, ok := theme.Light.Colors[name]; !ok {
			t.Errorf("light theme is missing the %q role", name)
		}
		if _, ok := theme.Dark.Colors[name]; !ok {
			t.Errorf("dark theme is missing the %q role", name)
		}
	}
	if got := theme.Light.Colors["accentFill"]; got != core.RGB(0x7c3aed) {
		t.Errorf("light accent fill is %v, want the chosen accent", got)
	}
}

// TestUIRoleColorsAreInThePalette checks the app's own palette: the first
// frame must already carry the role colors the ui reads.
func TestUIRoleColorsAreInThePalette(t *testing.T) {
	var scope *core.Scope
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		scope = cx
		return Root(cx)
	}, nsgotest.Size(390, 844))
	palette := scope.Palette()
	for _, name := range []string{"accentFill", "inset", "raised", "field", "line", "faint"} {
		color, ok := palette.Colors[name]
		if !ok {
			t.Errorf("palette is missing the %q role", name)
			continue
		}
		if _, _, _, alpha := color.Components(); alpha == 0 {
			t.Errorf("palette role %q is transparent", name)
		}
	}
}
