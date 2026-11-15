package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui"
)

type clientAppearance struct {
	Theme  string `json:"theme"`
	Accent string `json:"accent,omitempty"`
}

func loadClientAppearance(store core.Storage) clientAppearance {
	prefs := clientAppearance{Theme: "system"}
	if raw, ok := store.GetItem("appearance:v1"); ok {
		_ = json.Unmarshal([]byte(raw), &prefs)
	}
	switch prefs.Theme {
	case "system", "light", "dark":
	default:
		prefs.Theme = "system"
	}
	if prefs.Accent != "" && accentTheme(prefs.Accent) == nil {
		prefs.Accent = ""
	}
	return prefs
}

func (a *app) setClientAppearance(update func(*clientAppearance)) {
	prefs := a.appearance.GetUntracked()
	update(&prefs)
	store := a.cx.Storage()
	if a.manager != nil {
		store = a.manager.store
	}
	raw, err := json.Marshal(prefs)
	if err != nil {
		return
	}
	if err := store.SetItem("appearance:v1", string(raw)); err != nil {
		a.settingsSaveErr.Set(friendlyError(err))
		return
	}
	a.appearance.Set(prefs)
	if a.manager != nil {
		a.manager.app.applyAppearance()
	} else {
		a.applyAppearance()
	}
}

func (a *app) settingsAppearance(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	prefs := a.appearance.Get()
	var themes, accents []core.Node
	for _, theme := range []string{"system", "light", "dark"} {
		variant := ui.ButtonSecondary
		if prefs.Theme == theme {
			variant = ui.ButtonPrimary
		}
		themes = append(themes, ui.Button{Text: theme, TestID: "settings-theme-" + theme,
			Variant: variant, Size: controlSize(cx), OnPress: func() {
				a.setClientAppearance(func(p *clientAppearance) { p.Theme = theme })
			}})
	}
	for _, choice := range []struct{ name, hex string }{
		{"default", ""}, {"blue", "#007aff"}, {"violet", "#7c3aed"},
		{"green", "#16a34a"}, {"orange", "#ea580c"}, {"pink", "#db2777"}, {"teal", "#0d9488"},
	} {
		swatch := ui.Alpha(colors.Text(), 0.25)
		if value, err := strconv.ParseUint(strings.TrimPrefix(choice.hex, "#"), 16, 32); err == nil {
			swatch = core.RGB(uint32(value))
		}
		minHeight := core.Points(28)
		if ui.Touch(cx) {
			minHeight = 40
		}
		accents = append(accents, base.Button{Label: choice.name, TestID: "settings-accent-" + choice.name,
			OnPress: func(core.PressEvent) { a.setClientAppearance(func(p *clientAppearance) { p.Accent = choice.hex }) },
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, MinHeight: core.Pt(minHeight),
				Padding: core.Symmetric(4, 8), CornerRadius: 6, BorderWidth: 1},
			StyleFunc: func() core.Style {
				border := colors.Border()
				if prefs.Accent == choice.hex {
					border = colors.Accent()
				}
				return core.Style{BorderColor: border, Hover: &core.Style{Background: colors.Inset()}}
			},
			Children: []core.Node{
				core.View{Style: core.Style{Width: core.Pt(13), Height: core.Pt(13), CornerRadius: 7, Background: swatch}},
				ui.Text(choice.name, 12, ui.Regular, colors.Muted),
			}})
	}
	return core.View{TestID: "settings-appearance", Style: core.Style{Gap: 12}, Children: []core.Node{
		settingsSectionTitle(cx, "Appearance", "Saved automatically on this client, across all environments."),
		core.Text{Content: a.settingsSaveErr.Get(), MaxLines: 3, Style: core.Style{FontSize: 12},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }},
		ui.Text("Theme", 12, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Gap: 6, Wrap: core.Wrap}, Children: themes},
		ui.Text("Accent", 12, ui.Semibold, colors.Muted),
		core.View{Style: core.Style{Direction: core.Row, Gap: 6, Wrap: core.Wrap}, Children: accents},
	}}
}
