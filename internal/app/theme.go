package app

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// clientAppearance is the appearance the client keeps of its own, across
// every environment: the theme it follows and its accent color.
type clientAppearance struct {
	Theme  string `json:"theme"`
	Accent string `json:"accent,omitempty"`
}

var accentChoices = []struct{ name, hex string }{
	{"default", ""}, {"blue", "#007aff"}, {"violet", "#7c3aed"},
	{"green", "#16a34a"}, {"orange", "#ea580c"}, {"pink", "#db2777"}, {"teal", "#0d9488"},
}

func loadClientAppearance(store appStorage) clientAppearance {
	prefs := clientAppearance{Theme: "system"}
	if raw, ok := store.GetItem("appearance:v1"); ok {
		_ = json.Unmarshal([]byte(raw), &prefs)
	}
	switch prefs.Theme {
	case "system", "light", "dark":
	default:
		prefs.Theme = "system"
	}
	if prefs.Accent != "" && !validAccent(prefs.Accent) {
		prefs.Accent = ""
	}
	return prefs
}

func validAccent(hex string) bool {
	for _, choice := range accentChoices {
		if choice.hex == hex {
			return true
		}
	}
	return false
}

// saveClientAppearance writes the appearance to the shared store, the one
// all environments share.
func (s *Session) saveClientAppearance(prefs clientAppearance) {
	store := s.sharedStore()
	raw, err := json.Marshal(prefs)
	if err != nil {
		return
	}
	if err := store.SetItem("appearance:v1", string(raw)); err != nil {
		s.settingsSaveErr = friendlyError(err)
		return
	}
	s.appearance = prefs
}

// themeFor builds the window theme from the client's appearance: the
// theme it follows, and the accent it chose. Themes are rebuilt every
// frame, so a change of the setting, or of the desktop's light or dark,
// shows at once.
func themeFor(prefs clientAppearance, system *ui.Theme) *ui.Theme {
	t := system
	switch prefs.Theme {
	case "light":
		t = ui.LightTheme()
	case "dark":
		t = ui.DarkTheme()
	}
	if prefs.Accent != "" {
		t = t.WithAccent(ui.Hex(prefs.Accent))
	}
	// A compact desk for the client's dense lists, and plain hairlines
	// like the NSGo client drew.
	t.Radius = 7
	return t
}

// markdownTheme colors a Markdown view with the window's theme, in the
// sizes the conversation used.
func markdownTheme(t ui.Theme) *markdown.Theme {
	return &markdown.Theme{
		FontSize: 13.5,
		Link:     t.Accent,
	}
}

// terminalThemeColors maps the window's theme onto the terminal's: the
// terminal follows the window when the client follows its setting.
func terminalThemeColors(t ui.Theme) (background, foreground ui.Color) {
	return t.Background, t.Text
}

// hexColor parses an #rrggbb color, or the theme's text color when the
// value is empty.
func hexColor(value string, fallback ui.Color) ui.Color {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return fallback
	}
	n, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return fallback
	}
	return ui.RGB(uint8(n>>16), uint8(n>>8), uint8(n))
}
