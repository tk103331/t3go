package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/oneo-me/nsgo/base"
	basediff "github.com/oneo-me/nsgo/base/diff"
	"github.com/oneo-me/nsgo/base/textview"
	tvhighlight "github.com/oneo-me/nsgo/base/textview/highlight"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/oneo-me/nsgo/ui/commitgraph"
	uidiff "github.com/oneo-me/nsgo/ui/diff"
	"github.com/tk/t3go/internal/proto"
)

// rightRail is the resize handle and the surfaces panel beside it.
func (a *app) rightRail(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, FlexShrink: 0}, Children: []core.Node{
		base.ResizeHandle{Axis: base.Horizontal, Edge: base.HandleLeading, Label: "Resize right panel", TestID: "right-resize",
			Value: func() core.AccessibilityValue {
				return core.AccessibilityValue{Now: float32(a.rightWidth.GetUntracked()), Min: 300, Max: 680}
			},
			OnDragStart:   func() { a.rightDrag = a.rightWidth.GetUntracked() },
			OnDragMove:    func(d core.Points) { a.setRightWidth(a.rightDrag - d) },
			OnDragEnd:     func(d core.Points) { a.setRightWidth(a.rightDrag - d) },
			OnDoublePress: func() { a.setRightWidth(360) },
			StyleFunc:     ui.HandleLine(cx), ThumbStyle: ui.HandleThumb(cx), ThumbStyleFunc: ui.HandleThumbColor(cx)},
		core.Dynamic(a.rightPanel),
	}}
}

// rightPanel is the app's surfaces rail: a picker, then the file browser,
// diff or pull request it opens.
func (a *app) rightPanel(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "right-panel", Style: core.Style{Width: core.Pt(a.rightWidth.Get()), FlexShrink: 0, Direction: core.Column,
		MinHeight: core.Pt(0), Background: colors.Surface(), Borders: core.Edges{Left: 1}, BorderColor: colors.Border()},
		Children: []core.Node{
			core.Dynamic(a.rightHeader),
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Overflow: core.Hidden},
				Children: []core.Node{core.Dynamic(a.rightBody)}},
		}}
}

// rightHeader shows the open surface tabs, the picker and the close
// button.
func (a *app) rightHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	tabs := a.tabs.Get()
	surface := a.surface.Get()
	var tabNodes []core.Node
	for _, name := range tabs {
		tabNodes = append(tabNodes, a.surfaceTab(cx, name, name == surface))
	}
	var content core.Node
	if len(tabNodes) == 0 {
		content = core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, FlexGrow: 1, FlexShrink: 1, Padding: core.Edges{Left: 4}},
			Children: []core.Node{core.Text{Content: "Surfaces", MaxLines: 1, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}}}
	} else {
		content = core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 3,
			FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: tabNodes}
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2,
		Padding: core.Edges{Top: 6, Right: 6, Bottom: 6, Left: 6}}, Children: []core.Node{
		content,
		ui.IconButton{Icon: lucide.Plus, Label: "Add surface", TestID: "surface-add", OnPress: func() { a.openSurface("") }},
		ui.IconButton{Icon: lucide.X, Label: "Close right panel", TestID: "close-right-panel",
			OnPress: func() { a.setRightOpen(false) }},
	}}
}

// surfaceTab is one open surface's tab.
func (a *app) surfaceTab(cx *core.Scope, name string, selected bool) core.Node {
	colors := ui.ColorsOf(cx)
	label := surfaceTitle(name)
	tab := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 1, FlexShrink: 0,
		Padding: core.Edges{Left: 7, Right: 1, Top: 2, Bottom: 2}, CornerRadius: 6, BorderWidth: 1,
		BorderColor: colors.Border()}, StyleFunc: func() core.Style {
		style := core.Style{}
		if selected {
			style.Background = colors.Inset()
			style.BorderColor = colors.FieldBorder()
		}
		return style
	}, Children: []core.Node{
		base.Button{OnPress: func(core.PressEvent) { a.openSurface(name) }, Label: label, TestID: "surface-tab-" + name,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Hover()}, Focus: ui.FocusRing(cx, -2)}
			}, Children: []core.Node{
				ui.Icon(surfaceIcon(name), 12, func() core.Color {
					if selected {
						return colors.Text()
					}
					return colors.Muted()
				}),
				core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style {
						if selected {
							return core.Style{Color: colors.Text()}
						}
						return core.Style{Color: colors.Muted()}
					}},
			}},
		ui.IconButton{Icon: lucide.X, Label: "Close " + label, Size: 16, IconSize: 9, Tint: colors.Faint,
			TestID: "surface-tab-close-" + name, OnPress: func() { a.closeSurface(name) }},
	}}
	return tab
}

func surfaceTitle(name string) string {
	switch name {
	case "files":
		return "Files"
	case "diff":
		return "Diff"
	case "graph":
		return "Git graph"
	case "pr":
		return "Pull request"
	case "browser":
		return "Browser"
	case "preview":
		return "Preview"
	case "terminal":
		return "Terminal"
	}
	return name
}

func surfaceIcon(name string) func() *core.SVG {
	switch name {
	case "files":
		return lucide.Folder
	case "diff":
		return lucide.GitCompare
	case "graph":
		return lucide.GitBranch
	case "pr":
		return lucide.GitPullRequest
	case "browser":
		return lucide.Globe
	case "preview":
		return lucide.MonitorPlay
	case "linked":
		return lucide.Link2
	case "device":
		return lucide.MonitorSmartphone
	case "chat":
		return lucide.MessageCircleMore
	case "terminal":
		return lucide.Terminal
	}
	return lucide.PanelRight
}

// rightBody renders the open surface.
func (a *app) rightBody(cx *core.Scope) core.Node {
	switch a.surface.Get() {
	case "files":
		return a.rightFiles(cx)
	case "diff":
		return a.rightDiff(cx)
	case "pr":
		return a.rightPullRequest(cx)
	case "graph":
		return a.rightGraph(cx)
	case "browser":
		return a.rightBrowser(cx)
	case "preview":
		return a.rightPreview(cx)
	case "terminal":
		return a.rightTerminal(cx)
	case "linked":
		return a.rightLinkedPRs(cx)
	case "device":
		return a.rightDevices(cx)
	case "chat":
		return a.rightSideChat(cx)
	}
	return a.surfacePicker(cx)
}

// surfacePicker lists what the right panel can show.
func (a *app) surfacePicker(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	thread := a.selected.Get()
	hasThread := thread != ""
	rows := []core.Node{
		a.surfaceRow(cx, "browser", "Browser", lucide.Globe, "B", true),
		a.surfaceRow(cx, "preview", "Preview", lucide.MonitorPlay, "V", true),
		a.surfaceRow(cx, "terminal", "Terminal", lucide.Terminal, "T", hasThread),
		a.surfaceRow(cx, "files", "Files", lucide.Folder, "F", hasThread),
		a.surfaceRow(cx, "diff", "Diff", lucide.GitCompare, "D", hasThread),
		a.surfaceRow(cx, "graph", "Git graph", lucide.GitBranch, "G", hasThread),
		a.surfaceRow(cx, "pr", "Pull request", lucide.GitPullRequest, "P", hasThread),
		a.surfaceRow(cx, "linked", "Linked pull requests", lucide.Link2, "L", hasThread),
		a.surfaceRow(cx, "device", "Device", lucide.MonitorSmartphone, "M", true),
		a.surfaceRow(cx, "chat", "Side chat", lucide.MessageCircleMore, "S", hasThread),
	}
	note := "Open a surface"
	if !hasThread {
		note = "Pick a thread to open its files, diff and pull request."
	}
	return core.View{Style: core.Style{FlexGrow: 1, JustifyContent: core.JustifyCenter, AlignItems: core.AlignCenter,
		Padding: core.All(20)}, Children: []core.Node{
		core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(300), Direction: core.Column, Gap: 6}, Children: []core.Node{
			core.Text{Content: note, MaxLines: 2, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold, Padding: core.Edges{Bottom: 8}},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.View{Style: core.Style{Direction: core.Column, Gap: 2}, Children: rows},
		}},
	}}
}

// surfaceRow is one choice in the surface picker.
func (a *app) surfaceRow(cx *core.Scope, name, label string, icon func() *core.SVG, key string, enabled bool) core.Node {
	colors := ui.ColorsOf(cx)
	tint := colors.Text
	if !enabled {
		tint = colors.Faint
	}
	press := func(core.PressEvent) {
		if enabled {
			a.openSurface(name)
		}
	}
	return base.Button{OnPress: press, Disabled: func() bool { return !enabled }, TestID: "surface-" + name,
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Width: core.Pct(100),
			Padding: core.Symmetric(6, 8), CornerRadius: 8, Cursor: core.CursorDefault},
		StyleFunc: func() core.Style {
			if !enabled {
				return core.Style{}
			}
			return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
		},
		Children: []core.Node{
			ui.Icon(icon, 16, tint),
			core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 13, FlexGrow: 1, FlexShrink: 1},
				StyleFunc: func() core.Style { return core.Style{Color: tint()} }},
			core.View{Style: core.Style{Width: core.Pt(20), Height: core.Pt(20), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
				Background: colors.Inset(), CornerRadius: 5}, Children: []core.Node{
				core.Text{Content: key, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			}},
		}}
}

// scrollWithBar puts a permanent bar over a scroll view.
func (a *app) scrollWithBar(cx *core.Scope, ref *core.Ref, testID string, scroll core.Scroll) core.Node {
	scroll.Ref = ref
	scroll.HideIndicator = true
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		scroll,
		ui.Scrollbar{Scroll: func() *core.Ref { return ref },
			Mode:  func() base.ScrollbarMode { return base.ScrollbarAlways },
			Label: "Scroll", TestID: testID},
	}}
}

// rightFiles browses the thread's checkout and previews one file.
func (a *app) rightFiles(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to browse its files.")
	}
	if selected := a.fileSelected.Get(); selected != "" {
		file := a.file.Get()
		head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
			ui.IconButton{Icon: lucide.ChevronLeft, Label: "Back to files", TestID: "file-back", OnPress: func() {
				a.fileSelected.Set("")
				a.file.Set(proto.WorkspaceFile{})
			}},
			core.Text{Content: selected, MaxLines: 1, Style: core.Style{FontSize: 12, FlexGrow: 1, FlexShrink: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: humanSize(file.Size), Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			ui.IconButton{Icon: lucide.ExternalLink, Label: "Open file", TestID: "file-open",
				OnPress: func() { a.cx.OpenURL(a.rawFileURL(selected), nil) }},
		}}
		kind := fileKind(selected, file.Mime)
		var body core.Node
		switch {
		case kind == "image":
			body = ui.Image{Source: core.ImageSource{URI: a.rawFileURL(selected)}, ResizeMode: core.Contain,
				Label: selected, TestID: "file-image",
				Style: core.Style{Width: core.Pct(100), AspectRatio: 4.0 / 3.0, CornerRadius: 6}}
		case kind == "markdown":
			body = ui.TextView{MarkdownFunc: func() string { return a.markdownPreview(file.Content, selected) },
				Selectable: true, OpenLinks: true}
		case file.Binary:
			body = core.View{Style: core.Style{Direction: core.Column, Gap: 8, AlignItems: core.AlignStart}, Children: []core.Node{
				ui.Icon(lucide.FileText, 24, colors.Faint),
				ui.Text("No preview for this file.", 12, ui.Regular, colors.Faint),
				ui.Button{Text: "Open outside T3Go", Icon: lucide.ExternalLink, Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
					TestID: "file-open-binary", OnPress: func() { a.cx.OpenURL(a.rawFileURL(selected), nil) }},
			}}
		case file.Content == "":
			body = ui.Text("This file is empty.", 12, ui.Regular, colors.Faint)
		default:
			content := file.Content
			if strings.EqualFold(filepath.Ext(selected), ".json") {
				content = prettyFileJSON(content)
			}
			if language := codeLanguage(selected); language != "" && len(content) < 400_000 {
				body = core.View{TestID: "file-code", Style: core.Style{FlexGrow: 1}, Children: []core.Node{
					ui.TextView{Document: textview.Document{Blocks: []textview.Block{
						textview.CodeBlock{Language: language, Code: strings.TrimSuffix(content, "\n")}}},
						Selectable: true, Highlighter: tvhighlight.New(cx)},
				}}
			} else {
				body = core.Text{Content: content, Selectable: core.SelectableOn,
					Style:     core.Style{FontSize: 11.5, LineHeight: 16, FontFamily: "Menlo, monospace"},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}
			}
		}
		return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Padding: core.All(10)}, Children: []core.Node{
			head,
			a.scrollWithBar(cx, a.filesScroll, "files-scrollbar", core.Scroll{Style: ui.FillRest, Children: []core.Node{body}}),
		}}
	}
	path := a.filesPath.Get()
	head := []core.Node{
		ui.IconButton{Icon: lucide.ChevronUp, Label: "Parent folder", TestID: "files-up", Disabled: func() bool { return path == "" },
			OnPress: func() {
				if path == "" {
					return
				}
				if i := strings.LastIndex(path, "/"); i >= 0 {
					a.loadFiles(path[:i])
				} else {
					a.loadFiles("")
				}
			}},
		core.Text{Content: "/" + path, MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1, FlexShrink: 1, FontFamily: "Menlo, monospace"},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh files", TestID: "files-refresh", OnPress: func() { a.loadFiles(path) }},
	}
	var rows []core.Node
	for _, entry := range a.filesEntries.Get() {
		entry := entry
		icon := lucide.FileText
		if entry.Directory {
			icon = lucide.Folder
		}
		press := func() {
			if entry.Directory {
				a.loadFiles(entry.Path)
			} else {
				a.openFile(entry.Path)
			}
		}
		trailing := core.Text{Content: "", Style: core.Style{FontSize: 10.5}}
		if !entry.Directory {
			trailing = core.Text{Content: humanSize(entry.Size), Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }}
		}
		rows = append(rows, base.Button{OnPress: func(core.PressEvent) { press() }, TestID: "file-" + entry.Path,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Width: core.Pct(100),
				Padding: core.Symmetric(4, 6), CornerRadius: 6, Cursor: core.CursorDefault},
			StyleFunc: func() core.Style {
				return core.Style{Hover: &core.Style{Background: colors.Inset()}, Focus: ui.FocusRing(cx, -2)}
			},
			Children: []core.Node{
				ui.Icon(icon, 14, colors.Muted),
				core.Text{Content: entry.Name, MaxLines: 1, Style: core.Style{FontSize: 12, FlexGrow: 1, FlexShrink: 1},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				trailing,
			}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("This folder is empty.", 12, ui.Regular, colors.Faint))
	}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Padding: core.All(10)}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, Padding: core.Edges{Bottom: 6}}, Children: head},
		core.Show{When: func() bool { return a.rightBusy.Get() }, Then: func(*core.Scope) core.Node {
			return ui.Spinner{Size: 14, Label: "Loading files"}
		}},
		a.rightError(cx),
		a.scrollWithBar(cx, a.filesScroll, "files-list-scrollbar",
			core.Scroll{Style: ui.FillRest, Children: []core.Node{core.View{Style: core.Style{Gap: 1}, Children: rows}}}),
	}}
}

// rightDiff shows the working tree against the thread's checkpoint.
func (a *app) rightDiff(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to read its diff.")
	}
	files := a.workingFiles.Get()
	split := a.diffSplit.Get()
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 3, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.Text{Content: "Working tree", MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		ui.IconButton{Icon: lucide.Columns2, Label: "Toggle split view", TestID: "diff-split",
			Selected: func() bool { return split }, OnPress: func() { a.diffSplit.Set(!a.diffSplit.GetUntracked()) }},
		ui.Button{Text: "Changes…", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: a.openDiff},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh diff", TestID: "right-diff-refresh", OnPress: func() {
			if id := a.selected.GetUntracked(); id != "" {
				a.loadWorkingDiff(id)
			}
		}},
	}}
	var rows []core.Node
	for _, file := range files {
		file := file
		detail := fmt.Sprintf("+%d −%d", file.Additions, file.Deletions)
		if file.Staged {
			detail += " · staged"
		}
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2,
			Padding: core.Symmetric(2, 4), CornerRadius: 5}, Children: []core.Node{
			base.Button{OnPress: func(core.PressEvent) { a.workingRef.ScrollToFile(file.Path) },
				Label: file.Path, TestID: "diff-file-" + file.Path, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter,
					Gap: 5, FlexGrow: 1, MinWidth: core.Pt(0), Cursor: core.CursorDefault}, StyleFunc: func() core.Style {
					return core.Style{Hover: &core.Style{Background: colors.Hover()}, Focus: ui.FocusRing(cx, -2)}
				}, Children: []core.Node{
					ui.Icon(lucide.FileText, 11, colors.Muted),
					core.Text{Content: file.Path, MaxLines: 1, Style: core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace", FlexShrink: 1},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				}},
			core.Text{Content: detail, MaxLines: 1, Style: core.Style{FontSize: 9.5, FlexShrink: 0},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			ui.IconButton{Icon: lucide.Check, Label: "Stage " + file.Path, Size: 20, IconSize: 11,
				TestID: "diff-stage-" + file.Path, Disabled: func() bool { return file.Staged },
				OnPress: func() { a.stagePath(file.Path, true) }},
			ui.IconButton{Icon: lucide.Undo2, Label: "Discard " + file.Path, Size: 20, IconSize: 11,
				Tint: colors.Danger, TestID: "diff-discard-" + file.Path,
				OnPress: func() { a.diffDiscard.Set(file.Path) }},
		}})
	}
	list := core.View{Style: core.Style{Direction: core.Column, Gap: 1, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(2, 4)}, Children: []core.Node{
			ui.Text(fmt.Sprintf("%d changed files", len(files)), 10.5, ui.Semibold, colors.Muted),
			ui.Spacer{},
			ui.Text("stage or discard a file", 9.5, ui.Regular, colors.Faint),
		}},
		core.Scroll{Style: core.Style{MaxHeight: core.Pt(150)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 1}, Children: rows},
		}},
	}}
	var body core.Node
	if len(files) == 0 {
		body = core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}, Children: []core.Node{
			ui.Text("Working tree clean.", 12, ui.Regular, colors.Faint),
		}}
	} else {
		body = core.View{Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0), Overflow: core.Hidden, Background: colors.Inset(), CornerRadius: 8},
			Children: []core.Node{a.diffNodeWith(a.workingDiff.Get(), a.workingRef, !split)}}
	}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Padding: core.All(10)}, Children: []core.Node{
		head,
		a.rightError(cx),
		list,
		body,
	}}
}

// rightLinkedPRs lists the branch's pull requests.
func (a *app) rightLinkedPRs(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to see its pull requests.")
	}
	if a.prViewNumber.Get() > 0 {
		return a.rightPullRequestDetail(cx)
	}
	pulls := a.pullRequests.Get()
	detail := a.prListDetail.Get()
	if strings.Contains(strings.ToLower(detail), "no pull requests") {
		detail = ""
	}
	var rows []core.Node
	for _, pr := range pulls {
		pr := pr
		meta := fmt.Sprintf("#%d · %s", pr.Number, strings.ToLower(pr.State))
		if pr.Draft {
			meta += " · draft"
		}
		rows = append(rows, core.View{Style: core.Style{Direction: core.Column, Gap: 3, Padding: core.Symmetric(7, 9),
			Background: colors.Inset(), CornerRadius: 7}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4}, Children: []core.Node{
				core.Text{Content: pr.Title, MaxLines: 2, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold, LineHeight: 16, FlexGrow: 1},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				ui.IconButton{Icon: lucide.ChevronRight, Label: "Details", TestID: "open-linked-pr-" + fmt.Sprint(pr.Number),
					OnPress: func() { a.openPullRequest(pr.Number) }},
			}},
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
				core.Text{Content: meta, MaxLines: 1, Style: core.Style{FontSize: 10.5, FlexGrow: 1},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				ui.Button{Text: "Open", Icon: lucide.ExternalLink, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
					TestID: "open-linked-pr-url-" + fmt.Sprint(pr.Number), OnPress: func() { a.cx.OpenURL(pr.URL, nil) }},
			}},
			core.Text{Content: pr.HeadBranch + " → " + pr.BaseBranch, MaxLines: 1, Style: core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No pull requests for this branch.", 11.5, ui.Regular, colors.Faint))
	}
	if detail != "" {
		rows = append(rows, core.Text{Content: detail, MaxLines: 3, Style: core.Style{FontSize: 10.5},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
	}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.Text{Content: fmt.Sprintf("%d linked", len(pulls)), MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh pull requests", TestID: "linked-pr-refresh",
			OnPress: func() { a.loadPullRequests(a.selected.GetUntracked()) }},
	}}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 6, Padding: core.All(10), FlexGrow: 1, MinHeight: core.Pt(0)},
		Children: []core.Node{head, a.rightError(cx), core.Scroll{Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0)},
			Children: []core.Node{core.View{Style: core.Style{Direction: core.Column, Gap: 6}, Children: rows}}}}}
}

// sideAttachmentChips are the files the side chat's next question carries.
func (a *app) sideAttachmentChips(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	var rows []core.Node
	for _, file := range a.sideAttachments.Get() {
		file := file
		rows = append(rows, core.View{TestID: "side-attachment-" + file.Name,
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, Padding: core.Symmetric(3, 6),
				Background: colors.Inset(), CornerRadius: 6}, Children: []core.Node{
				ui.Icon(lucide.Paperclip, 12, colors.Muted),
				core.Text{Content: file.Name, MaxLines: 1, Style: core.Style{FontSize: 10.5, FlexGrow: 1, FlexShrink: 1},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: humanSize(file.Size), Style: core.Style{FontSize: 9.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
				ui.IconButton{Icon: lucide.X, Label: "Remove " + file.Name, Size: 18, IconSize: 10,
					TestID: "side-attachment-remove-" + file.ID, Tint: colors.Faint,
					OnPress: func() { a.removeSideAttachment(file.ID) }},
			}})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 3}, Children: rows}
}

// rightSideChat asks questions in a hidden thread with the current one's
// context, without touching its transcript.
func (a *app) rightSideChat(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to ask a side question.")
	}
	thread := a.sideChat.Get()
	if thread.ID == "" {
		note := "A side chat keeps this thread's conversation out of the way."
		if message := a.sideErr.Get(); message != "" {
			note = message
		}
		var action core.Node = ui.Button{Text: "Start side chat", Icon: lucide.MessageCircleMore, Variant: ui.ButtonPrimary,
			Size: ui.SizeSmall, TestID: "side-start", OnPress: a.openSideChat}
		if a.sideBusy.Get() {
			action = ui.Spinner{Size: 14, Label: "Starting side chat"}
		}
		return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
			Gap: 8, Padding: core.All(18)}, Children: []core.Node{
			ui.Icon(lucide.MessageCircleMore, 22, colors.Faint),
			core.Text{Content: note, MaxLines: 4, Style: core.Style{FontSize: 11.5, TextAlign: core.TextAlignCenter},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			action,
		}}
	}
	var rows []core.Node
	for _, message := range a.sideMessages.Get() {
		message := message
		switch message.Kind {
		case proto.KindText:
			if message.Role == proto.RoleUser {
				rows = append(rows, core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifyEnd},
					Children: []core.Node{core.View{Style: core.Style{MaxWidth: core.Pct(88), Padding: core.Symmetric(6, 9),
						Background: colors.Inset(), CornerRadius: 8}, Children: []core.Node{
						core.Text{Content: message.Content, Selectable: core.SelectableOn,
							Style:     core.Style{FontSize: 11.5, LineHeight: 16},
							StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
					}}}})
			} else {
				rows = append(rows, core.View{TestID: "side-message-" + message.ID, Style: core.Style{MinWidth: core.Pt(0)},
					Children: []core.Node{ui.TextView{MarkdownFunc: func() string { return a.markdownPreview(message.Content, "") },
						Selectable: true, OpenLinks: true}}})
			}
		case proto.KindReasoning:
			rows = append(rows, core.Text{Content: message.Content, MaxLines: 4,
				Style:     core.Style{FontSize: 10.5, FontStyle: core.FontStyleItalic},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		case proto.KindTool:
			label := message.Title
			if label == "" {
				label = "Tool"
			}
			rows = append(rows, core.Text{Content: label, MaxLines: 2, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }})
		case proto.KindError:
			rows = append(rows, core.Text{Content: message.Content, MaxLines: 5, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }})
		case proto.KindNotice:
			rows = append(rows, core.Text{Content: message.Content, MaxLines: 2, Style: core.Style{FontSize: 10},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		}
	}
	if len(rows) == 0 {
		rows = append(rows, core.Text{Content: "Ask a question about this thread.", MaxLines: 2,
			Style: core.Style{FontSize: 11}, StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
	}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4,
		Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.Text{Content: "Side chat", MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		core.Show{When: func() bool { return a.sideBusy.Get() }, Then: func(*core.Scope) core.Node {
			return ui.Spinner{Size: 12, Label: "Working on the side chat"}
		}},
		ui.Button{Text: "Reset", Variant: ui.ButtonGhost, Size: ui.SizeSmall, TestID: "side-reset",
			OnPress: func() { a.resetSideChat(); a.sideFor.Set(""); a.openSideChat() }},
	}}
	var modelItems []core.Node
	modelLabel := shortModel(thread.Model)
	for _, model := range a.allModels() {
		model := model
		label := model.Name
		if model.Provider != thread.Provider {
			label += " · " + model.Provider
		}
		if model.ID == thread.Model && model.Provider == thread.Provider {
			modelLabel = shortModel(model.Name)
		}
		modelItems = append(modelItems, ui.MenuItem{Text: label, TestID: "side-model-" + model.ID,
			Checked:  func() bool { return a.sideChat.Get().Model == model.ID && a.sideChat.Get().Provider == model.Provider },
			OnSelect: func() { a.setSideModel(model) }})
	}
	var modelControl core.Node = ui.Text(modelLabel, 10.5, ui.Regular, colors.Faint)
	if len(modelItems) > 0 {
		modelControl = ui.Menu{TestID: "side-model", MaxHeight: 320,
			Trigger:  ui.Button{Text: modelLabel, Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Side chat model"},
			Children: modelItems}
	}
	var permissionItems []core.Node
	for _, permission := range []proto.PermissionMode{proto.PermissionSupervised, proto.PermissionAcceptEdits,
		proto.PermissionAuto, proto.PermissionFullAccess} {
		permission := permission
		permissionItems = append(permissionItems, ui.MenuItem{Text: string(permission), TestID: "side-perm-" + string(permission),
			Checked:  func() bool { return a.sideChat.Get().Permission == permission },
			OnSelect: func() { a.setSidePermission(permission) }})
	}
	permissionControl := ui.Menu{TestID: "side-permission", MaxHeight: 240,
		Trigger:  ui.Button{Text: string(thread.Permission), Variant: ui.ButtonGhost, Size: ui.SizeSmall, Label: "Side chat permission"},
		Children: permissionItems}
	var submit core.Node
	if a.sideRunning.Get() {
		submit = ui.Button{Text: "Stop", Variant: ui.ButtonSecondary, Size: ui.SizeSmall, TestID: "side-stop",
			OnPress: a.stopSideChat}
	} else {
		submit = ui.Button{Text: "Send", Icon: lucide.Send, Variant: ui.ButtonPrimary, Size: ui.SizeSmall, TestID: "side-send",
			Disabled: func() bool { return strings.TrimSpace(a.sideDraft.Get()) == "" }, OnPress: a.sendSideChat}
	}
	if message := a.sideErr.Get(); message != "" {
		head = core.View{Style: core.Style{Direction: core.Column, Gap: 4}, Children: []core.Node{head,
			core.Text{Content: message, MaxLines: 3, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }}}}
	}
	composer := core.View{TestID: "side-composer", OnFileDrop: a.attachSidePaths,
		Style: core.Style{Direction: core.Column, Gap: 5, Padding: core.Edges{Top: 6}}, Children: []core.Node{
			core.Show{When: func() bool { return len(a.sideAttachments.Get()) > 0 }, Then: func(cx *core.Scope) core.Node {
				return a.sideAttachmentChips(cx)
			}},
			ui.Input{Input: base.Input{Multiline: true, Value: a.sideDraft.Get, OnChange: a.sideDraft.Set,
				Placeholder: "Ask a side question…", Label: "Side question", TestID: "side-input"}},
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4}, Children: []core.Node{
				modelControl,
				permissionControl,
				ui.Spacer{},
				ui.IconButton{Icon: lucide.Paperclip, Label: "Attach files", TestID: "side-attach", OnPress: a.pickSideAttachments},
				submit,
			}},
		}}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Padding: core.All(10)},
		Children: []core.Node{
			head,
			core.Scroll{Style: ui.FillRest, Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: rows}}},
			composer,
		}}
}

// rightDevices lists the environment's simulators and shows one screen.
func (a *app) rightDevices(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	devices := a.devices.Get()
	selectedID := a.deviceSelected.Get()
	var selected proto.Device
	for _, device := range devices {
		if device.ID == selectedID {
			selected = device
		}
	}
	selector := "Choose a device"
	if selected.Name != "" {
		selector = selected.Name
	}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4,
		Padding: core.Edges{Top: 4, Right: 10, Bottom: 4, Left: 10}}, Children: []core.Node{
		ui.Button{Text: selector, Icon: lucide.MonitorSmartphone, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
			Label: "Devices", TestID: "device-select", OnPress: func() { a.showDevices.Set(true) }},
		core.Text{Content: strings.ToLower(selected.State), MaxLines: 1, Style: core.Style{FontSize: 10},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		ui.Spacer{},
		core.Show{When: func() bool { return a.deviceBusy.Get() }, Then: func(*core.Scope) core.Node {
			return ui.Spinner{Size: 12, Label: "Working with the simulator"}
		}},
		ui.IconButton{IconFunc: func() *core.SVG {
			if a.deviceLive.Get() {
				return lucide.Pause()
			}
			return lucide.Play()
		}, LabelFunc: func() string {
			if a.deviceLive.Get() {
				return "Pause live screen"
			}
			return "Resume live screen"
		}, TestID: "device-live", OnPress: func() { a.setDeviceLive(!a.deviceLive.GetUntracked()) }},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh devices", TestID: "devices-refresh",
			OnPress: a.loadDevices},
	}}
	body := []core.Node{head, a.rightError(cx),
		core.Show{When: func() bool { return a.deviceErr.Get() != "" }, Then: func(*core.Scope) core.Node {
			return core.View{Style: core.Style{Padding: core.Edges{Right: 10, Bottom: 4, Left: 10}}, Children: []core.Node{
				core.Text{Content: a.deviceErr.Get(), MaxLines: 3, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }}}}
		}},
	}
	switch {
	case len(devices) == 0:
		detail := a.deviceDetail.Get()
		if detail == "" {
			detail = "No simulators found."
		}
		body = append(body, core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
			Padding: core.All(16)}, Children: []core.Node{core.Text{Content: detail, MaxLines: 6,
			Style:     core.Style{FontSize: 11.5, TextAlign: core.TextAlignCenter},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }}}})
	case selected.ID == "":
		body = append(body, core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
			Children: []core.Node{ui.Text("Choose a simulator above.", 11.5, ui.Regular, colors.Faint)}})
	case strings.ToLower(selected.State) == "booted":
		body = append(body, core.View{Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0),
			Padding: core.Edges{Top: 2, Right: 10, Bottom: 10, Left: 10}}, Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0), Background: colors.Inset(),
				CornerRadius: 8, Overflow: core.Hidden}, Children: []core.Node{
				core.Image{Source: core.ImageSource{Data: a.deviceImage.Get()},
					ResizeMode: core.Contain, Label: selected.Name + " screen", TestID: "device-screen",
					Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0),
						Width: core.Pct(100)}},
			}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 4, Padding: core.Edges{Top: 6}}, Children: []core.Node{
				ui.Button{Text: "Refresh screen", Icon: lucide.RefreshCw, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
					TestID: "device-refresh", OnPress: a.refreshDeviceShot},
				ui.Spacer{},
				ui.Button{Text: "Shut down", Icon: lucide.Power, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
					TestID: "device-shutdown", OnPress: func() { a.shutdownDevice(selected.ID) }},
			}},
		}})
	default:
		body = append(body, core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
			Gap: 6, Padding: core.All(16)}, Children: []core.Node{
			core.Text{Content: selected.Name, MaxLines: 1, Style: core.Style{FontSize: 12.5, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: selected.OS + " · " + strings.ToLower(selected.State), Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			ui.Button{Text: "Boot", Icon: lucide.Play, Variant: ui.ButtonPrimary, Size: ui.SizeSmall,
				TestID: "device-boot", OnPress: func() { a.bootDevice(selected.ID) }},
		}})
	}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0)}, Children: body}
}

// rightPullRequestDetail shows one pull request's checks, reviews and
// merge actions.
func (a *app) rightPullRequestDetail(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	detail := a.prView.Get()
	pr := detail.PullRequest
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 3, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		ui.IconButton{Icon: lucide.ChevronLeft, Label: "All pull requests", TestID: "pr-back", OnPress: a.closePullRequest},
		core.Text{Content: fmt.Sprintf("#%d", pr.Number), MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		core.Show{When: a.prViewBusy.Get, Then: func(*core.Scope) core.Node { return ui.Spinner{Size: 13, Label: "Loading pull request"} }},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh pull request", TestID: "pr-detail-refresh",
			OnPress: func() { a.openPullRequest(pr.Number) }},
	}}
	var fields []core.Node
	fields = append(fields, head,
		core.Text{Content: pr.Title, MaxLines: 3, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold, LineHeight: 18},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
	)
	meta := strings.ToLower(pr.State)
	if pr.Draft {
		meta += " · draft"
	}
	if detail.Mergeable != "" {
		meta += " · " + strings.ToLower(detail.Mergeable)
	}
	if detail.ReviewDecision != "" {
		meta += " · " + strings.ToLower(detail.ReviewDecision)
	}
	fields = append(fields, core.Text{Content: meta, Style: core.Style{FontSize: 11},
		StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }})
	fields = append(fields, core.Text{Content: fmt.Sprintf("%s → %s · +%d −%d · %d files",
		pr.HeadBranch, pr.BaseBranch, detail.Additions, detail.Deletions, detail.ChangedFiles),
		MaxLines: 2, Style: core.Style{FontSize: 10.5, FontFamily: "Menlo, monospace"},
		StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
	if detail.Author != "" {
		fields = append(fields, core.Text{Content: "by " + detail.Author, Style: core.Style{FontSize: 11},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }})
	}
	if detail.Body != "" {
		fields = append(fields, core.View{Style: core.Style{Background: colors.Inset(), CornerRadius: 6, Padding: core.All(8)},
			Children: []core.Node{core.Text{Content: detail.Body, MaxLines: 8, Selectable: core.SelectableOn,
				Style: core.Style{FontSize: 11, LineHeight: 16}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }}}})
	}
	if len(detail.Checks) > 0 {
		fields = append(fields, ui.Text("Checks", 10.5, ui.Semibold, colors.Faint))
		var checks []core.Node
		for _, check := range detail.Checks {
			color := colors.Muted
			switch check.Status {
			case "success":
				color = colors.Up
			case "failure":
				color = colors.Danger
			case "pending":
				color = colors.Accent
			}
			label := check.Name
			if check.Description != "" {
				label += " · " + check.Description
			}
			checks = append(checks, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5},
				Children: []core.Node{ui.Icon(lucide.CircleCheck, 11, color),
					core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 10.5, FlexGrow: 1},
						StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}}})
		}
		fields = append(fields, core.View{Style: core.Style{Direction: core.Column, Gap: 2}, Children: checks})
	}
	if len(detail.Reviews) > 0 {
		fields = append(fields, ui.Text("Reviews", 10.5, ui.Semibold, colors.Faint))
		var reviews []core.Node
		for _, review := range detail.Reviews {
			label := review.Author + " · " + strings.ToLower(review.State)
			reviews = append(reviews, core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 10.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }})
		}
		fields = append(fields, core.View{Style: core.Style{Direction: core.Column, Gap: 1}, Children: reviews})
	}
	if len(detail.Comments) > 0 {
		fields = append(fields, ui.Text("Comments", 10.5, ui.Semibold, colors.Faint))
		for _, comment := range detail.Comments {
			label := comment.Author
			if comment.CreatedAt != "" {
				label += " · " + comment.CreatedAt
			}
			fields = append(fields, core.View{Style: core.Style{Direction: core.Column, Gap: 2,
				Background: colors.Inset(), CornerRadius: 6, Padding: core.All(7)}, Children: []core.Node{
				core.Text{Content: label, MaxLines: 1, Style: core.Style{FontSize: 10},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
				core.Text{Content: comment.Body, MaxLines: 6, Selectable: core.SelectableOn,
					Style:     core.Style{FontSize: 11, LineHeight: 16},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			}})
		}
	}
	if strings.EqualFold(pr.State, "open") {
		fields = append(fields, ui.Text("Review or comment", 10.5, ui.Semibold, colors.Faint),
			ui.Input{Input: base.Input{Multiline: true, Value: a.prNote.Get, OnChange: a.prNote.Set,
				Placeholder: "Leave a note…", Label: "Note", TestID: "pr-note"}},
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4}, Children: []core.Node{
				ui.Button{Text: "Approve", Icon: lucide.CircleCheck, Variant: ui.ButtonPrimary, Size: ui.SizeSmall,
					TestID: "pr-review-approve", OnPress: func() { a.submitPRReview("approve") }},
				ui.Button{Text: "Request changes", Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
					TestID: "pr-review-request-changes", OnPress: func() { a.submitPRReview("request-changes") }},
				ui.Button{Text: "Add comment", Icon: lucide.MessageCircle, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
					TestID: "pr-comment", OnPress: a.submitPRComment},
			}},
		)
	}
	var actions []core.Node
	if pr.URL != "" {
		actions = append(actions, ui.Button{Text: "Open on GitHub", Icon: lucide.ExternalLink, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
			TestID: "pr-open-url", OnPress: func() { a.cx.OpenURL(pr.URL, nil) }})
	}
	if strings.EqualFold(pr.State, "open") {
		if pr.Draft {
			actions = append(actions, ui.Button{Text: "Ready", Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
				TestID: "pr-action-ready", OnPress: func() { a.changePullRequestState("ready") }})
		} else if detail.Mergeable == "MERGEABLE" || detail.Mergeable == "" {
			for _, method := range []string{"merge", "squash", "rebase"} {
				method := method
				actions = append(actions, ui.Button{Text: strings.Title(method), Variant: ui.ButtonPrimary, Size: ui.SizeSmall,
					TestID: "pr-merge-" + method, OnPress: func() { a.mergePullRequest(method) }})
			}
		}
		actions = append(actions, ui.Button{Text: "Close", Variant: ui.ButtonGhost, Size: ui.SizeSmall,
			TestID: "pr-action-close", OnPress: func() { a.changePullRequestState("close") }})
	} else {
		actions = append(actions, ui.Button{Text: "Reopen", Variant: ui.ButtonSecondary, Size: ui.SizeSmall,
			TestID: "pr-action-reopen", OnPress: func() { a.changePullRequestState("reopen") }})
	}
	if len(actions) > 0 {
		fields = append(fields, core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4, Padding: core.Edges{Top: 4}}, Children: actions})
	}
	if strings.EqualFold(pr.State, "open") {
		deleteLabel := "Keep branch"
		if a.prDeleteBranch.Get() {
			deleteLabel = "Delete branch"
		}
		fields = append(fields, ui.Button{Text: deleteLabel, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
			TestID: "pr-delete-branch", OnPress: func() { a.prDeleteBranch.Set(!a.prDeleteBranch.GetUntracked()) }})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 8, Padding: core.All(10), FlexGrow: 1, MinHeight: core.Pt(0)},
		Children: []core.Node{core.Scroll{Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: fields}}}}}
}

// rightPullRequest shows the branch's open pull request, or offers one.
func (a *app) rightPullRequest(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to read its pull request.")
	}
	pr := a.pullRequest.Get()
	fields := []core.Node{
		a.rightError(cx),
		core.Show{When: func() bool { return a.gitBusy.Get() }, Then: func(*core.Scope) core.Node {
			return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				ui.Spinner{Size: 14, Label: "Running git action"}, ui.Text("Working…", 12, ui.Regular, colors.Faint),
			}}
		}},
	}
	if pr.URL != "" {
		meta := fmt.Sprintf("#%d · %s", pr.Number, strings.ToLower(pr.State))
		fields = append(fields,
			core.Text{Content: pr.Title, MaxLines: 3, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold, LineHeight: 18},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: meta, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			core.Text{Content: pr.HeadBranch + " → " + pr.BaseBranch, MaxLines: 1, Style: core.Style{FontSize: 11.5, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			core.Text{Content: pr.URL, MaxLines: 2, Selectable: core.SelectableOn, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Accent()} }},
			core.View{Style: core.Style{Direction: core.Row, Gap: 6, Padding: core.Edges{Top: 4}}, Children: []core.Node{
				ui.Button{Text: "Open pull request", Icon: lucide.GitPullRequest, Variant: ui.ButtonPrimary, Size: ui.SizeSmall,
					TestID: "right-pr-open", OnPress: func() { a.cx.OpenURL(pr.URL, nil) }},
				ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh pull request", TestID: "right-pr-refresh",
					OnPress: func() { a.loadPullRequest(a.selected.GetUntracked()) }},
			}},
		)
	} else {
		detail := a.prDetail.Get()
		if strings.Contains(strings.ToLower(detail), "no pull requests") {
			detail = ""
		}
		fields = append(fields,
			core.Text{Content: "No open pull request", Style: core.Style{FontSize: 13, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: "Commit and push this branch, then open a pull request with GitHub CLI.", MaxLines: 3,
				Style:     core.Style{FontSize: 12, LineHeight: 17},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		)
		if detail != "" {
			fields = append(fields, core.Text{Content: detail, MaxLines: 3, Style: core.Style{FontSize: 11},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
		}
		fields = append(fields, core.View{Style: core.Style{Direction: core.Row, Gap: 6, Padding: core.Edges{Top: 4}}, Children: []core.Node{
			ui.Button{Text: "Create PR", Icon: lucide.GitPullRequest, Variant: ui.ButtonPrimary, Size: ui.SizeSmall,
				TestID: "right-pr-create", Disabled: a.gitBusy.Get, OnPress: func() { a.runGitAction(proto.GitCommitPR) }},
			ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh pull request", TestID: "right-pr-refresh",
				OnPress: func() { a.loadPullRequest(a.selected.GetUntracked()) }},
		}})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 8, Padding: core.All(12), FlexGrow: 1, MinHeight: core.Pt(0)},
		Children: fields}
}

func (a *app) rightNotice(cx *core.Scope, text string) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Padding: core.All(20)},
		Children: []core.Node{ui.Text(text, 12.5, ui.Regular, colors.Faint)}}
}

func (a *app) rightError(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.Show{When: func() bool { return a.rightErr.Get() != "" }, Then: func(*core.Scope) core.Node {
		return core.Text{Content: a.rightErr.Get(), MaxLines: 3, Style: core.Style{FontSize: 11.5, LineHeight: 16},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }}
	}}
}

// rightTerminal shows the thread's interactive shell.
func (a *app) rightTerminal(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to open its terminal.")
	}
	sessions := a.terminals.Get()
	session := a.terminalSession.Get()
	body := []core.Node{}
	if message := a.terminalErr.Get(); message != "" {
		body = append(body, core.View{Style: core.Style{Padding: core.Symmetric(6, 10)}, Children: []core.Node{
			core.Text{Content: message, MaxLines: 3, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }},
		}})
	}
	if session.ID != "" && session.Exited {
		body = append(body, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8,
			Padding: core.Symmetric(4, 10), Background: colors.Inset()}, Children: []core.Node{
			core.Text{Content: "Shell exited", MaxLines: 1, Style: core.Style{FontSize: 11.5, FlexGrow: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			ui.Button{Text: "Restart", Icon: lucide.RotateCcw, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
				TestID: "terminal-restart-exited", OnPress: a.restartTerminal},
		}})
	}
	switch {
	case session.ID != "":
		body = append(body, core.View{Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0),
			Padding: core.Edges{Top: 6, Right: 8, Bottom: 8, Left: 8}}, Children: []core.Node{
			core.WebView{TestID: "terminal-view", Style: ui.FillRest,
				SourceFunc: func() core.WebSource { return core.WebHTML(a.terminalHTML(session.ID), "") },
				OnMessage: func(data string) {
					if data == "exit" {
						a.loadTerminalSessions()
					}
				}},
		}})
	case len(sessions) == 0 && !a.terminalBusy.Get():
		body = append(body, core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 8},
			Children: []core.Node{
				ui.Text("No terminal open", 12.5, ui.Regular, colors.Faint),
				ui.Button{Text: "New terminal", Icon: lucide.Plus, Variant: ui.ButtonPrimary, Size: ui.SizeSmall,
					TestID: "terminal-start", OnPress: a.newTerminal},
			}})
	default:
		body = append(body, core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
			Children: []core.Node{ui.Text("Starting shell…", 12, ui.Regular, colors.Faint)}})
	}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Overflow: core.Hidden},
		Children: body}
}

// rightPreview lists the local development servers to open.
func (a *app) rightPreview(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	servers := a.previewServers.Get()
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4,
		Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.Text{Content: fmt.Sprintf("Local servers · %d", len(servers)), MaxLines: 1,
			Style:     core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		core.Show{When: func() bool { return a.previewBusy.Get() }, Then: func(*core.Scope) core.Node {
			return ui.Spinner{Size: 12, Label: "Looking for local servers"}
		}},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh local servers", TestID: "preview-refresh",
			OnPress: a.loadPreviewServers},
	}}
	var rows []core.Node
	for _, server := range servers {
		server := server
		name := server.Process
		if name == "" {
			name = "Server"
		}
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6,
			Padding: core.Symmetric(6, 8), Background: colors.Inset(), CornerRadius: 7}, Children: []core.Node{
			ui.Icon(lucide.MonitorPlay, 14, colors.Muted),
			core.View{Style: core.Style{Direction: core.Column, Gap: 1, FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
				core.Text{Content: fmt.Sprintf("%s · %d", name, server.Port), MaxLines: 1,
					Style:     core.Style{FontSize: 11.5, FontWeight: ui.Medium},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: server.URL, MaxLines: 1, Style: core.Style{FontSize: 10, FontFamily: "Menlo, monospace"},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			}},
			ui.Button{Text: "Open", Icon: lucide.ExternalLink, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
				TestID:  fmt.Sprintf("preview-open-%d", server.Port),
				OnPress: func() { a.openPreviewServer(server.URL) }},
		}})
	}
	if len(rows) == 0 {
		detail := a.previewDetail.Get()
		if detail == "" {
			detail = "No local servers found. Start a dev server, then refresh."
		}
		rows = append(rows, core.Text{Content: detail, MaxLines: 5, Style: core.Style{FontSize: 11},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }})
	}
	return core.View{Style: core.Style{Direction: core.Column, Gap: 6, Padding: core.All(10), FlexGrow: 1, MinHeight: core.Pt(0)},
		Children: []core.Node{head, a.rightError(cx),
			core.Show{When: func() bool { return a.previewErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return core.Text{Content: a.previewErr.Get(), MaxLines: 3, Style: core.Style{FontSize: 10.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }}
			}},
			core.Scroll{Style: ui.FillRest, Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Column, Gap: 5}, Children: rows},
			}},
		}}
}

// rightBrowser shows a web page with its address bar.
func (a *app) rightBrowser(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		a.browserToolbar(cx),
		core.Show{When: func() bool { return a.browserURL.Get() == "" }, Then: func(*core.Scope) core.Node {
			return a.browserStart(cx)
		}},
		core.Show{When: func() bool { return a.browserURL.Get() != "" }, Then: func(cx *core.Scope) core.Node {
			return core.WebView{Ref: a.browserRef, TestID: "browser-view", Style: ui.FillRest,
				SourceFunc: func() core.WebSource { return core.WebSrc(a.browserURL.Get()) },
				OnNavigationStateChange: func(nav core.WebNavigation) {
					a.browserInput.Set(nav.URL)
					a.browserBack.Set(nav.CanGoBack)
					a.browserNext.Set(nav.CanGoForward)
				},
				OnError: func(err error) { a.browserNote.Set(err.Error()) }}
		}},
		core.Show{When: func() bool { return a.browserNote.Get() != "" }, Then: func(*core.Scope) core.Node {
			return core.View{Style: core.Style{Padding: core.Symmetric(6, 10)}, Children: []core.Node{
				core.Text{Content: a.browserNote.Get(), MaxLines: 2, Style: core.Style{FontSize: 11.5},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Danger()} }},
			}}
		}},
	}}
}

// browserToolbar is the address bar under the panel's title.
func (a *app) browserToolbar(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2,
		Padding: core.Edges{Top: 6, Right: 10, Bottom: 6, Left: 10}}, Children: []core.Node{
		ui.IconButton{Icon: lucide.ChevronLeft, Label: "Back", TestID: "browser-back", Size: 24, IconSize: 13,
			Disabled: func() bool { return !a.browserBack.Get() }, OnPress: func() { a.browserRef.GoBack() }},
		ui.IconButton{Icon: lucide.ChevronRight, Label: "Forward", TestID: "browser-forward", Size: 24, IconSize: 13,
			Disabled: func() bool { return !a.browserNext.Get() }, OnPress: func() { a.browserRef.GoForward() }},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Reload", TestID: "browser-reload", Size: 24, IconSize: 13,
			OnPress: func() { a.browserRef.Reload() }},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			ui.Input{Input: base.Input{Value: a.browserInput.Get, OnChange: a.browserInput.Set,
				Placeholder: "https://example.com", Label: "Address"}},
		}},
		ui.IconButton{Icon: lucide.ArrowRight, Label: "Go", TestID: "browser-go", Size: 24, IconSize: 13,
			OnPress: func() { a.goBrowser(a.browserInput.GetUntracked()) }},
	}}
}

// browserStart is the empty browser surface.
func (a *app) browserStart(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	links := []string{"https://go.dev", "http://localhost:3000", "http://localhost:5173"}
	var quick []core.Node
	for _, link := range links {
		link := link
		quick = append(quick, ui.Button{Text: link, Variant: ui.ButtonGhost, Size: ui.SizeSmall,
			OnPress: func() { a.goBrowser(link) }})
	}
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		Padding: core.All(20), Gap: 8}, Children: []core.Node{
		ui.Icon(lucide.Globe, 28, colors.Faint),
		ui.Text("Open a page", 13, ui.Semibold, colors.Text),
		ui.Text("Type an address above, or open a local dev server.", 11.5, ui.Regular, colors.Faint),
		core.View{Style: core.Style{Direction: core.Column, Gap: 2, AlignItems: core.AlignCenter, Padding: core.Edges{Top: 6}}, Children: quick},
	}}
}

// rightGraph draws the repository's commits beside their revision graph.
func (a *app) rightGraph(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to see its commit graph.")
	}
	commits := a.graph.Get()
	view := commitgraph.View{Label: "Commits", TestID: "git-graph", Empty: ui.Text("No commits yet.", 12, ui.Regular, colors.Faint),
		Style: ui.FillRest, Ref: a.graphScroll,
		Commits: func() []commitgraph.Commit {
			list := a.graph.Get()
			out := make([]commitgraph.Commit, 0, len(list))
			for _, commit := range list {
				refs := make([]commitgraph.Ref, 0, len(commit.Refs))
				for _, ref := range commit.Refs {
					kind := commitgraph.RefBranch
					switch ref.Kind {
					case "remote":
						kind = commitgraph.RefRemote
					case "tag":
						kind = commitgraph.RefTag
					}
					refs = append(refs, commitgraph.Ref{Name: ref.Name, Kind: kind, Current: ref.Current})
				}
				out = append(out, commitgraph.Commit{Hash: commit.Hash, Parents: commit.Parents, Short: commit.Short,
					Subject: commit.Subject, Author: commit.Author, Date: commit.Date, Refs: refs, Head: commit.Head})
			}
			return out
		}}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.Text{Content: fmt.Sprintf("%d commits", len(commits)), MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh graph", TestID: "git-graph-refresh",
			OnPress: func() { a.loadGraph(a.selected.GetUntracked()) }},
	}}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Padding: core.All(10)}, Children: []core.Node{
		head,
		a.rightError(cx),
		core.View{Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0), Overflow: core.Hidden}, Children: []core.Node{
			view,
			ui.Scrollbar{Scroll: func() *core.Ref { return a.graphScroll },
				Mode:  func() base.ScrollbarMode { return base.ScrollbarAlways },
				Label: "Scroll commits", TestID: "git-graph-scrollbar"},
		}},
	}}
}

// diffNode renders a unified diff with colors and line numbers, and any
// message that is not a patch as text.
func (a *app) diffNode(cx *core.Scope, patch string) core.Node {
	trimmed := strings.TrimSpace(patch)
	if !strings.HasPrefix(trimmed, "diff --git") && !strings.Contains(trimmed, "\n@@ ") {
		colors := ui.ColorsOf(cx)
		return core.Text{Content: patch, Selectable: core.SelectableOn,
			Style:     core.Style{FontSize: 11.5, LineHeight: 16, FontFamily: "Menlo, monospace"},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}
	}
	return a.diffNodeWith(patch, nil, true)
}

func (a *app) diffNodeWith(patch string, ref *basediff.Ref, unified bool) core.Node {
	trimmed := strings.TrimSpace(patch)
	if strings.HasPrefix(trimmed, "diff --git") || strings.Contains(trimmed, "\n@@ ") {
		return uidiff.View{Patch: func() string { return patch }, Unified: unified, Selectable: true, ContextLines: 3,
			SoftWrap: true, FontSize: 11.5, LineHeight: 16, Ref: ref, TestID: "code-diff",
			Style: core.Style{FlexGrow: 1, MinHeight: core.Pt(0)}}
	}
	return core.Group{}
}

// editMessageDialog edits a user message by branching before it.
func (a *app) editMessageDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	message := a.editingMessage.Get()
	return ui.Dialog{NoCloseOnBackdropPress: compactSettings(cx), Open: func() bool { return message.ID != "" }, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			a.cancelEditMessage()
		}
	}, Title: "Edit message", Description: "The thread branches before this message, then sends the edited prompt.", TestID: "edit-message-dialog",
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			return core.View{Style: core.Style{Direction: core.Column, Gap: 8}, Children: []core.Node{
				ui.Input{Input: base.Input{Multiline: true, Value: a.editDraft.Get, OnChange: a.editDraft.Set,
					Label: "Message", Placeholder: "Edit the prompt"}},
				core.Text{Content: "The new thread copies the transcript before this message.", Style: core.Style{FontSize: 11},
					StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
			}}
		})},
		Footer: []core.Node{
			ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, TestID: "cancel-edit-message", OnPress: a.cancelEditMessage},
			ui.Button{Text: "Save & send", Icon: lucide.Check, Variant: ui.ButtonPrimary, TestID: "confirm-edit-message",
				OnPress: a.confirmEditMessage},
		}}
}

// discardDialog confirms restoring one changed file.
func (a *app) discardDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	path := a.diffDiscard.Get()
	return ui.AlertDialog{Open: func() bool { return path != "" }, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			a.diffDiscard.Set("")
		}
	}, Title: "Discard changes?", TestID: "discard-dialog",
		Description: path + " will be restored to HEAD, or removed when it is untracked. This cannot be undone.",
		Actions: []ui.AlertAction{
			{Text: "Cancel", Variant: ui.ButtonGhost, TestID: "cancel-discard", OnPress: func() { a.diffDiscard.Set("") }},
			{Text: "Discard", Variant: ui.ButtonPrimary, Color: func() core.Color { return colors.Danger() }, TestID: "confirm-discard",
				OnPress: func() {
					a.diffDiscard.Set("")
					a.stagePath(path, false)
				}},
		}}
}

// fileKind names how a file previews: markdown, an image, or text.
func fileKind(path, mimeType string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return "markdown"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".ico", ".svg", ".avif":
		return "image"
	}
	if strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return "image"
	}
	return "text"
}

// codeLanguage names a file's lexer for highlighting, empty for plain text.
func codeLanguage(path string) string {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		return ""
	}
	if config := lexer.Config(); config != nil {
		return config.Name
	}
	return ""
}

// markdownPreview points relative images at the checkout's raw file URL,
// so a README's screenshots show in the panel.
func (a *app) markdownPreview(content, path string) string {
	dir := ""
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir = path[:i]
	}
	return markdownImage.ReplaceAllStringFunc(content, func(match string) string {
		parts := markdownImage.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		target := strings.Trim(strings.TrimSpace(parts[2]), "<>")
		lower := strings.ToLower(target)
		if strings.Contains(target, "://") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(target, "#") {
			return match
		}
		joined := target
		if dir != "" {
			joined = dir + "/" + target
		}
		joined = filepath.ToSlash(filepath.Clean(joined))
		return parts[1] + a.rawFileURL(joined) + parts[3]
	})
}

var markdownImage = regexp.MustCompile(`(!\[[^\]]*\]\()([^)\s]+)([^)]*\))`)

// prettyFileJSON indents JSON that parses, leaving anything else as it is.
func prettyFileJSON(content string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(content), "", "  "); err != nil {
		return content
	}
	return buf.String()
}
