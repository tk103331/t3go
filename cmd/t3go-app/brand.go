package main

import (
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui"
)

// brandWordmark is a minimal two-color T3Go wordmark with emphasis on T3.
func brandWordmark(cx *core.Scope, size core.Points) core.Node {
	colors := ui.ColorsOf(cx)
	goSize := size * 0.62
	if goSize < 8 {
		goSize = 8
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 1}, Children: []core.Node{
		core.Text{Content: "T3", Style: core.Style{FontSize: size, FontWeight: ui.Bold},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Accent()} }},
		core.Text{Content: "go", Style: core.Style{FontSize: goSize, FontWeight: ui.Semibold},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
	}}
}
