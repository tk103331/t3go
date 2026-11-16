package main

import (
	"slices"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

// scopedSidebar uses stable project/environment IDs for its groups. NSGo's
// Sidebar keys groups by their title, which can repeat across environments.
type scopedSidebar struct {
	Sections   func() []ui.SidebarSection
	Selected   func() string
	OnSelect   func(string)
	CanFocus   func(string) bool
	TakingKeys *core.Signal[bool]
	Offset     *core.Points
}

func (s scopedSidebar) Render(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	groups := core.NewMemoFunc(cx, s.Sections, func([]ui.SidebarSection, []ui.SidebarSection) bool { return false })
	takingKeys := s.TakingKeys
	if takingKeys == nil {
		takingKeys = core.NewSignal(cx, false)
	}
	scroll, contents := &core.Ref{}, &core.Ref{}
	rowRefs := map[string]*core.Ref{}
	var offset core.Points
	if s.Offset != nil {
		offset = *s.Offset
	}
	takeKeys := func() { takingKeys.Set(true) }
	choose := func(id string) {
		takeKeys()
		s.OnSelect(id)
	}
	visibleIDs := func() []string {
		var ids []string
		for _, section := range groups.GetUntracked() {
			if section.Collapsible && section.Open != nil && !section.Open() {
				continue
			}
			for _, item := range section.Items {
				if s.CanFocus != nil && !s.CanFocus(item.ID) {
					continue
				}
				ids = append(ids, item.ID)
			}
		}
		return ids
	}
	reveal := func(id string) {
		ref := rowRefs[id]
		if ref == nil {
			return
		}
		row, rowOK := ref.Bounds()
		body, bodyOK := contents.Bounds()
		viewport, viewportOK := scroll.Bounds()
		if !rowOK || !bodyOK || !viewportOK {
			return
		}
		top := row.Origin.Y - body.Origin.Y
		bottom := top + row.Size.Height
		if top < offset {
			scroll.ScrollTo(core.Point{Y: top}, true)
		} else if bottom > offset+viewport.Size.Height {
			scroll.ScrollTo(core.Point{Y: bottom - viewport.Size.Height}, true)
		}
	}
	move := func(delta int, edge bool) {
		ids := visibleIDs()
		if len(ids) == 0 {
			return
		}
		index := slices.Index(ids, s.Selected())
		if edge {
			index = 0
			if delta < 0 {
				index = len(ids) - 1
			}
		} else if index < 0 {
			index = 0
		} else {
			index = max(0, min(len(ids)-1, index+delta))
		}
		choose(ids[index])
		reveal(ids[index])
	}
	bindings := core.Show{When: takingKeys.Get, Then: func(cx *core.Scope) core.Node {
		options := core.KeyOptions{When: core.KeyNotEditing, Repeat: true}
		cx.OnKeyWith("up", options, func() { move(-1, false) })
		cx.OnKeyWith("down", options, func() { move(1, false) })
		cx.OnKeyWith("home", options, func() { move(1, true) })
		cx.OnKeyWith("end", options, func() { move(-1, true) })
		return core.Group{}
	}}
	sections := core.Each(groups.Get, func(section ui.SidebarSection) string {
		if section.TestID != "" {
			return section.TestID
		}
		return "empty:" + section.Title
	}, func(cx *core.Scope, item func() ui.SidebarSection, _ func() int) core.Node {
		section := item()
		open := func() bool {
			current := item()
			return !current.Collapsible || current.Open == nil || current.Open()
		}
		header := core.Dynamic(func(cx *core.Scope) core.Node {
			current := item()
			label := core.Text{Content: current.Title, MaxLines: 1,
				Style:     core.Style{FontSize: 11, FontWeight: ui.Bold, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }}
			var title core.Node = label
			if current.Collapsible {
				testID := ""
				if current.TestID != "" {
					testID = current.TestID + "-toggle"
				}
				title = base.Button{Label: current.Title, TestID: testID, Expanded: open, OnPress: func(core.PressEvent) {
					takeKeys()
					if current.OnOpenChange != nil {
						current.OnOpenChange(!open())
					}
				}, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5, FlexGrow: 1, MinWidth: core.Pt(0),
					MinHeight: core.Pt(32), Padding: core.Symmetric(3, 8), CornerRadius: 6},
					StyleFunc: func() core.Style { return core.Style{Focus: ui.FocusRing(cx, -2)} },
					Children: []core.Node{
						label, current.Trailing,
						core.View{StyleFunc: func() core.Style {
							if !open() {
								return core.Style{Transform: core.Transform{Rotate: -90}}
							}
							return core.Style{}
						}, Children: []core.Node{ui.Icon(lucide.ChevronDown, 14, colors.Muted)}},
					}}
			} else {
				label.Role = core.RoleHeader
				title = core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5,
					MinWidth: core.Pt(0), FlexGrow: 1, Padding: core.Symmetric(3, 8)}, Children: []core.Node{label, current.Trailing}}
			}
			return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, MinWidth: core.Pt(0)},
				Children: []core.Node{title, current.Actions}}
		})
		rows := core.Dynamic(func(cx *core.Scope) core.Node {
			current := item()
			var nodes []core.Node
			for _, entry := range current.Items {
				entry := entry
				ref := &core.Ref{}
				rowRefs[entry.ID] = ref
				cx.Cleanup(func() {
					if rowRefs[entry.ID] == ref {
						delete(rowRefs, entry.ID)
					}
				})
				height := core.Points(32)
				if ui.Touch(cx) {
					height = 44
				}
				content := entry.Content
				if content == nil {
					content = ui.Text(entry.Text, 13, ui.Regular, colors.Text)
				}
				row := core.View{Ref: ref, TestID: entry.TestID, Role: core.RoleButton, Label: entry.Text, NoFocus: true,
					Selected: func() bool { return s.Selected() == entry.ID },
					OnPress:  func(core.PressEvent) { choose(entry.ID) },
					Style: core.Style{MinHeight: core.Pt(height), Padding: core.Symmetric(3, 8), CornerRadius: 8,
						MinWidth: core.Pt(0), Cursor: core.CursorDefault},
					StyleFunc: func() core.Style {
						return core.Style{Selected: &core.Style{Background: ui.Alpha(colors.Text(), 0.1)}, Focus: ui.FocusRing(cx, -2)}
					}, Children: []core.Node{content}}
				if len(entry.Menu) > 0 {
					nodes = append(nodes, ui.ContextMenu{Trigger: row, Children: entry.Menu})
				} else {
					nodes = append(nodes, row)
				}
			}
			if current.Note != nil {
				nodes = append(nodes, core.View{Style: core.Style{Padding: core.Symmetric(4, 8)}, Children: []core.Node{current.Note}})
			}
			return core.View{Style: core.Style{Gap: 2}, Children: nodes}
		})
		if section.Collapsible {
			return base.Collapsible{TestID: section.TestID, Open: open, Reveal: func() bool { return true },
				Children: []core.Node{header}, Content: rows}
		}
		if strings.TrimSpace(section.Title) == "" && section.Actions == nil && section.Trailing == nil {
			return core.View{TestID: section.TestID, Style: core.Style{Gap: 2}, Children: []core.Node{rows}}
		}
		return core.View{TestID: section.TestID, Style: core.Style{Gap: 2}, Children: []core.Node{header, rows}}
	})
	return core.Scroll{Ref: scroll, Style: ui.FillRest, ContentOffset: core.Point{Y: offset}, OnScroll: func(e core.ScrollEvent) {
		offset = e.Offset.Y
		if s.Offset != nil {
			*s.Offset = offset
		}
	},
		Children: []core.Node{core.View{Ref: contents, OnPress: func(core.PressEvent) { takeKeys() },
			OnFocus: takeKeys, OnBlur: func() { takingKeys.Set(false) },
			Style:    core.Style{Padding: core.Edges{Left: 10, Right: 10, Bottom: 12}, Gap: 10},
			Children: []core.Node{sections, bindings}}}}
}
