package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/proto"
)

// rightPanel is the app's surfaces rail: a picker, then the file browser,
// diff or pull request it opens.
func (a *app) rightPanel(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "right-panel", Style: core.Style{Width: core.Pt(360), FlexShrink: 0, Direction: core.Column,
		MinHeight: core.Pt(0), Background: colors.Surface(), Borders: core.Edges{Left: 1}, BorderColor: colors.Border()},
		Children: []core.Node{
			core.Dynamic(a.rightHeader),
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Overflow: core.Hidden},
				Children: []core.Node{core.Dynamic(a.rightBody)}},
		}}
}

// rightHeader names the open surface and goes back to the picker.
func (a *app) rightHeader(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	surface := a.surface.Get()
	title := "Surfaces"
	switch surface {
	case "files":
		title = "Files"
	case "diff":
		title = "Diff"
	case "pr":
		title = "Pull request"
	}
	header := []core.Node{}
	if surface != "" {
		header = append(header, ui.IconButton{Icon: lucide.ChevronLeft, Label: "All surfaces", TestID: "surface-back",
			OnPress: func() { a.openSurface("") }})
	}
	header = append(header,
		core.Text{Content: title, MaxLines: 1, Style: core.Style{FontSize: 13, FontWeight: ui.Semibold, FlexGrow: 1, FlexShrink: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		ui.IconButton{Icon: lucide.X, Label: "Close right panel", TestID: "close-right-panel",
			OnPress: func() { a.setRightOpen(false) }},
	)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6,
		Padding: core.Edges{Top: 8, Right: 8, Bottom: 8, Left: 8}}, Children: header}
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
	}
	return a.surfacePicker(cx)
}

// surfacePicker lists what the right panel can show.
func (a *app) surfacePicker(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	thread := a.selected.Get()
	hasThread := thread != ""
	rows := []core.Node{
		a.surfaceRow(cx, "browser", "Browser", lucide.Globe, "B", false),
		a.surfaceRow(cx, "terminal", "Terminal", lucide.Terminal, "T", false),
		a.surfaceRow(cx, "files", "Files", lucide.Folder, "F", hasThread),
		a.surfaceRow(cx, "diff", "Diff", lucide.GitCompare, "D", hasThread),
		a.surfaceRow(cx, "pr", "Pull request", lucide.GitPullRequest, "P", hasThread),
		a.surfaceRow(cx, "linked", "Linked pull requests", lucide.Link2, "L", false),
		a.surfaceRow(cx, "device", "Device", lucide.MonitorSmartphone, "M", false),
		a.surfaceRow(cx, "chat", "Side chat", lucide.MessageCircleMore, "S", false),
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
			body = core.Text{Content: content, Selectable: core.SelectableOn,
				Style:     core.Style{FontSize: 11.5, LineHeight: 16, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}
		}
		return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Padding: core.All(10)}, Children: []core.Node{
			head,
			core.Scroll{Style: ui.FillRest, Children: []core.Node{body}},
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
		core.Scroll{Style: ui.FillRest, Children: []core.Node{core.View{Style: core.Style{Gap: 1}, Children: rows}}},
	}}
}

// rightDiff shows the working tree against the thread's checkpoint.
func (a *app) rightDiff(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.selected.Get() == "" {
		return a.rightNotice(cx, "Pick a thread to read its diff.")
	}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, Padding: core.Edges{Bottom: 6}}, Children: []core.Node{
		core.Text{Content: "Working tree", MaxLines: 1, Style: core.Style{FontSize: 12, FontWeight: ui.Semibold, FlexGrow: 1},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		ui.Button{Text: "Changes…", Variant: ui.ButtonGhost, Size: ui.SizeSmall, OnPress: a.openDiff},
		ui.IconButton{Icon: lucide.RefreshCw, Label: "Refresh diff", TestID: "right-diff-refresh", OnPress: func() {
			if id := a.selected.GetUntracked(); id != "" {
				a.loadDiff(id, "")
			}
		}},
	}}
	return core.View{Style: core.Style{Direction: core.Column, FlexGrow: 1, MinHeight: core.Pt(0), Padding: core.All(10)}, Children: []core.Node{
		head,
		a.rightError(cx),
		core.Scroll{Style: ui.FillRest, Children: []core.Node{
			core.Text{Content: a.diffText.Get(), Selectable: core.SelectableOn,
				Style:     core.Style{FontSize: 11, LineHeight: 15, FontFamily: "Menlo, monospace"},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
		}},
	}}
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
