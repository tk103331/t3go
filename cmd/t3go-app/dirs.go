package main

import (
	"fmt"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

// dirsDialog picks a folder on the connected environment's machine: a
// phone cannot browse the host's file system itself.
func (a *app) dirsDialog(cx *core.Scope) core.Node {
	return ui.Dialog{
		Open: a.showDirs.Get, OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				a.closeDirs()
			}
		},
		NoCloseOnBackdropPress: compactSettings(cx),
		Title:                  "Choose a folder",
		Description:            "Folders on the machine running T3Go.",
		TestID:                 "dirs-dialog",
		Style:                  dialogWidth(cx, 520),
		Children:               []core.Node{core.Dynamic(a.dirsBody)},
		Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, TestID: "dirs-cancel", OnPress: a.closeDirs},
			ui.Button{Text: "Use this folder", Icon: lucide.FolderOpen, Variant: ui.ButtonPrimary, TestID: "dirs-use",
				Disabled: func() bool { return a.dirsBusy.Get() || a.dirs.GetUntracked().Path == "" },
				OnPress:  a.useDirs},
		},
	}
}

// dirsBody is the current folder's path, its parents and its folders.
func (a *app) dirsBody(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	listing := a.dirs.Get()
	busy := a.dirsBusy.Get()
	rows := make([]core.Node, 0, len(listing.Entries))
	for _, dir := range listing.Entries {
		dir := dir
		rows = append(rows, base.Button{
			Label: dir.Name, TestID: "dirs-entry-" + dir.Name,
			OnPress: func(core.PressEvent) { a.loadDirs(dir.Path) },
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pct(100),
				Padding: core.Symmetric(6, 7), CornerRadius: 6, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
			},
			Children: []core.Node{
				ui.Icon(lucide.Folder, 13, colors.Muted),
				core.Text{Content: dir.Name, MaxLines: 1, Style: core.Style{FontSize: 12.5, FlexGrow: 1},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			},
		})
	}
	var contents core.Node
	switch {
	case busy && listing.Path == "":
		contents = core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
			ui.Spinner{Size: 12, Label: "Reading folders"}, ui.Text("Reading folders…", 12, ui.Regular, colors.Muted),
		}}
	case len(rows) == 0:
		contents = ui.Text("This folder has no subfolders.", 12, ui.Regular, colors.Faint)
	default:
		contents = core.Scroll{Style: core.Style{MaxHeight: core.Pt(dirsBodyHeight(cx)), MinHeight: core.Pt(0)},
			Children: []core.Node{core.View{Style: core.Style{Direction: core.Column, Gap: 1,
				Padding: core.Edges{Right: 8}}, Children: rows}}}
	}
	head := []core.Node{
		core.Text{Content: listing.Path, MaxLines: 2, TestID: "dirs-path",
			Style:     core.Style{FontSize: 11.5, FontFamily: "Menlo, monospace"},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
			ui.Button{Text: "Up", Icon: lucide.ArrowUp, Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
				TestID: "dirs-up", Disabled: func() bool { return a.dirs.GetUntracked().Parent == "" },
				OnPress: func() { a.loadDirs(a.dirs.GetUntracked().Parent) }},
			ui.Button{Text: "Home", Icon: lucide.House, Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
				TestID: "dirs-home", OnPress: func() { a.loadDirs("") }},
			ui.Button{Text: "Refresh", Icon: lucide.RefreshCw, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
				TestID: "dirs-refresh", OnPress: func() { a.loadDirs(listing.Path) }},
			ui.Spacer{},
			core.Show{When: func() bool { return busy }, Then: func(*core.Scope) core.Node {
				return ui.Spinner{Size: 11, Label: "Reading folders"}
			}},
			core.Text{Content: foldersLabel(len(listing.Entries)), Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}},
	}
	if err := a.dirsErr.Get(); err != "" {
		head = append(head, core.Text{Content: err, MaxLines: 3, Style: core.Style{FontSize: 11.5},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 8},
		Children: append(head, contents)}
}

// foldersLabel counts a folder's subfolders.
func foldersLabel(n int) string {
	if n == 1 {
		return "1 folder"
	}
	return fmt.Sprintf("%d folders", n)
}

// dirsBodyHeight caps the folder list where a sheet's footer leaves room.
func dirsBodyHeight(cx *core.Scope) core.Points {
	if compactSettings(cx) {
		return cx.Env().Window().Height * 0.5
	}
	return 320
}
