package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/device"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/proto"
)

func sectionTitle(title, detail string) ui.Node {
	return ui.Column(ui.Style{Gap: 2, Padding: ui.Edges{Bottom: 4}},
		ui.Text(ui.Val(title), ui.Style{Font: fonts.Headline}),
		ui.Show(signal.Func(func() bool { return detail != "" }), func() ui.Node {
			return ui.Text(ui.Val(detail), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 3})
		}),
	)
}

func card(children ...ui.Node) ui.Node {
	return ui.Column(ui.Style{Padding: ui.All(12), Gap: 8, CornerRadius: 12,
		Background: colors.SecondaryBackground, BorderWidth: 1, BorderColor: colors.Separator}, children...)
}

func surfacePanel(a *app, narrow bool) ui.Node {
	header := ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(8, 12)},
		ui.Text(ui.Val("Surfaces"), ui.Style{Font: fonts.Headline, Grow: 1}),
		ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Refresh surface"),
			OnPress: func() { a.loadSurface(a.rightTab.Peek()) }}),
		ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close surfaces"), OnPress: a.closeSurface}),
	)
	var tabs []ui.Node
	for index, name := range surfaceNames {
		index, name := index, name
		tabs = append(tabs, ui.Button(ui.ButtonProps{
			Title: ui.Val(name),
			OnPress: func() {
				a.rightTab.Set(index)
				a.loadSurface(index)
			},
			Style: ui.StyleFunc(func() ui.Style {
				style := ui.Style{Padding: ui.Symmetric(5, 10), CornerRadius: 8}
				if a.rightTab.Get() == index {
					style.Background = colors.Accent.Opacity(0.14)
					style.Color = colors.Accent
				} else {
					style.Color = colors.SecondaryLabel
				}
				return style
			}),
		}))
	}
	tabStrip := ui.Scroll(ui.ScrollProps{Style: ui.Style{Shrink: ui.NoShrink, Padding: ui.Symmetric(0, 8)}, Horizontal: true},
		ui.Row(ui.Style{Gap: 3, Padding: ui.Edges{Bottom: 6}}, tabs...))
	body := ui.Column(ui.Style{Grow: 1},
		ui.Show(signal.Func(func() bool { return a.rightBusy.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(5, 12)},
				ui.Activity(), ui.Text(ui.Val("Loading…"), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
		}),
		ui.Show(signal.Func(func() bool { return a.rightErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.rightErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 5,
				Padding: ui.Symmetric(4, 12)})
		}),
		ui.Dynamic(func() ui.Node { return surfaceBody(a) }),
	)
	content := ui.Column(ui.Style{Grow: 1}, header, tabStrip, body)
	if narrow {
		return ui.SafeArea(ui.Style{Grow: 1}, content)
	}
	return ui.Column(ui.Style{Grow: 1}, content)
}

func surfaceBody(a *app) ui.Node {
	switch a.rightTab.Get() {
	case surfaceFiles:
		return filesSurface(a)
	case surfaceChanges:
		return changesSurface(a)
	case surfacePullRequest:
		return pullRequestSurface(a)
	case surfaceGraph:
		return graphSurface(a)
	case surfaceTerminal:
		return terminalSurface(a)
	case surfacePreview:
		return previewSurface(a)
	case surfaceDevices:
		return devicesSurface(a)
	case surfaceSideChat:
		return sideChatSurface(a)
	case surfaceBrowser:
		return browserSurface(a)
	}
	return ui.Text(ui.Val("Choose a surface."), ui.Style{Color: colors.SecondaryLabel})
}

func filesSurface(a *app) ui.Node {
	if a.selected.Get() == "" {
		return emptySurface("Open a thread to browse its checkout.")
	}
	if path := a.fileSelected.Get(); path != "" {
		file := a.fileView.Get()
		image := strings.HasPrefix(strings.ToLower(file.Mime), "image/") ||
			imageExtension(strings.ToLower(path))
		var preview ui.Node
		if image {
			preview = ui.Image(ui.ImageProps{
				Source: signal.Func(func() string { return a.currentRawFileURL(a.fileSelected.Get()) }),
				Mode:   ui.ImageFit,
				Style:  ui.Style{Width: ui.Pct(100), Height: ui.Pt(260), CornerRadius: 8, Background: colors.Background},
				Label:  signal.Func(func() string { return a.fileSelected.Get() }),
			})
		} else if file.Binary {
			preview = ui.Column(ui.Style{Padding: ui.All(20), Gap: 8, Align: ui.AlignCenter},
				ui.Symbol(ui.IconDocument, ui.Style{Color: colors.SecondaryLabel}),
				ui.Text(ui.Val("No inline preview for this file."), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Open in browser"), Icon: ui.IconGlobe,
					OnPress: func() { ui.OpenURL(a.currentRawFileURL(path)) }}),
			)
		} else {
			preview = ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
				ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(10), Background: colors.Background}},
					ui.Text(signal.Func(func() string { return a.fileView.Get().Content }),
						ui.Style{Font: fonts.Custom("monospace", fonts.Callout), Color: colors.Label}),
				),
			)
		}
		return ui.Column(ui.Style{Grow: 1},
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(6, 10)},
				ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: ui.Val("Back to files"),
					OnPress: func() { a.fileSelected.Set(""); a.fileView.Set(proto.WorkspaceFile{}) }}),
				ui.Text(ui.Val(path), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Lines: 1, Grow: 1}),
				ui.Button(ui.ButtonProps{Icon: ui.IconShare, Label: ui.Val("Open file"),
					OnPress: func() { ui.OpenURL(a.currentRawFileURL(path)) }}),
			),
			preview,
		)
	}
	var rows []ui.Node
	for _, entry := range a.filesEntries.Get() {
		entry := entry
		icon := ui.IconDocument
		if entry.Directory {
			icon = ui.IconFolder
		}
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() {
				if entry.Directory {
					a.loadFiles(entry.Path)
				} else {
					a.openFile(entry.Path)
				}
			},
			Style: ui.Style{Padding: ui.Symmetric(8, 10), Gap: 8},
		}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
			ui.Symbol(icon, ui.Style{Color: colors.Accent}),
			ui.Text(ui.Val(entry.Name), ui.Style{Font: fonts.Footnote, Grow: 1, Lines: 1}),
			ui.Show(signal.Func(func() bool { return !entry.Directory }), func() ui.Node {
				return ui.Text(ui.Val(humanSize(entry.Size)), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel})
			}),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, emptySurface("This folder is empty."))
	}
	return ui.Column(ui.Style{Grow: 1},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(6, 10)},
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: ui.Val("Parent folder"),
				Disabled: signal.Func(func() bool { return a.filesPath.Get() == "" }),
				OnPress:  func() { a.loadFiles(a.fileParent(a.filesPath.Peek())) }}),
			ui.Text(signal.Func(func() string {
				path := a.filesPath.Get()
				if path == "" {
					return "/"
				}
				return "/" + path
			}), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Grow: 1, Lines: 1}),
			ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Refresh files"),
				OnPress: func() { a.loadFiles(a.filesPath.Peek()) }}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.Edges{Bottom: 12}}, rows...)),
	)
}

func imageExtension(path string) bool {
	switch filepath.Ext(path) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".avif", ".svg":
		return true
	}
	return false
}

func changesSurface(a *app) ui.Node {
	if a.selected.Get() == "" {
		return emptySurface("Open a thread to inspect its changes.")
	}
	working := a.workingDiff.Get()
	additions, deletions := 0, 0
	var rows []ui.Node
	for _, file := range working.Files {
		additions += file.Additions
		deletions += file.Deletions
		rows = append(rows, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 7, Padding: ui.Symmetric(4, 2)},
			ui.Text(ui.Val(file.Path), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Grow: 1, Lines: 1}),
			ui.Text(ui.Val(fmt.Sprintf("+%d −%d", file.Additions, file.Deletions)),
				ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
		))
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text(ui.Val("Working tree clean."), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}))
	}
	controls := card(
		ui.Text(ui.Val("Commit or push"), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold)}),
		ui.TextField(ui.TextFieldProps{Value: a.gitMessage, Placeholder: ui.Val("Commit message (optional)")}),
		ui.TextField(ui.TextFieldProps{Value: a.gitBase, Placeholder: ui.Val("Pull request base branch (optional)"),
			Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo}),
		ui.Row(ui.Style{Wrap: true, Gap: 6},
			ui.Button(ui.ButtonProps{Title: ui.Val("Commit"), Variant: ui.ButtonFilled,
				Disabled: a.gitBusy, OnPress: func() { a.runGitAction(proto.GitCommit) }}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Commit & push"),
				OnPress: func() { a.runGitAction(proto.GitCommitPush) }}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Commit, push & PR"),
				OnPress: func() { a.runGitAction(proto.GitCommitPR) }}),
		),
		ui.Row(ui.Style{Wrap: true, Gap: 6},
			ui.Button(ui.ButtonProps{Title: ui.Val("Stage all"), OnPress: a.stageAllChanges}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Discard all…"),
				Style: ui.Style{Color: colors.Red}, OnPress: a.confirmDiscardChanges}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Pull"), OnPress: a.pullBranch}),
			ui.Button(ui.ButtonProps{Title: ui.Val("New branch…"), OnPress: func() {
				name := signal.New("")
				ui.Alert(ui.AlertProps{Title: "New branch", Fields: []ui.AlertField{{Text: name, Placeholder: "branch-name"}},
					Actions: []ui.Action{{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
						{Title: ui.Val("Create"), Role: ui.ActionDefault, OnPress: func() { a.createBranch(name.Peek()) }}}})
			}}),
		),
		ui.Show(signal.Func(func() bool { return a.gitNote.Get() != "" }), func() ui.Node {
			return ui.Text(a.gitNote, ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 4})
		}),
	)
	branchRow := ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
		ui.Text(signal.Func(func() string {
			if git := a.detail.Get().Git; git != nil && git.Branch != "" {
				return git.Branch
			}
			return "detached HEAD"
		}), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Grow: 1}),
		ui.Dynamic(func() ui.Node {
			branches := a.branches.Get()
			if len(branches) == 0 {
				return ui.View(ui.ViewProps{})
			}
			var actions []ui.Action
			for _, branch := range branches {
				branch := branch
				actions = append(actions, ui.Action{
					Title:   ui.Val(branch.Name),
					Checked: signal.Func(func() bool { return branch.Current }),
					OnPress: func() { a.switchBranch(branch.Name) },
				})
			}
			return ui.Button(ui.ButtonProps{Title: ui.Val("Switch branch"), Icon: ui.IconList,
				Menu: actions})
		}),
		ui.Text(ui.Val(fmt.Sprintf("%d files · +%d −%d", len(working.Files), additions, deletions)),
			ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
	)
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.Column(ui.Style{Padding: ui.All(10), Gap: 10},
			branchRow,
			controls,
			card(ui.Column(ui.Style{Gap: 3}, rows...)),
			ui.Show(signal.Func(func() bool { return strings.TrimSpace(a.diffText.Get()) != "" }), func() ui.Node {
				return card(ui.Text(a.diffText, ui.Style{Font: fonts.Custom("monospace", fonts.Caption2),
					Color: colors.SecondaryLabel, Lines: 0}))
			}),
		),
	)
}

func pullRequestSurface(a *app) ui.Node {
	if a.selected.Get() == "" {
		return emptySurface("Open a thread to inspect its pull request.")
	}
	requests := a.pullRequests.Get()
	view := a.prView.Get()
	if view.PullRequest.Number > 0 {
		return pullRequestDetailView(a, view)
	}
	var rows []ui.Node
	for _, request := range requests {
		request := request
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() { a.loadPullRequestDetail(request.Number) },
			Style:   ui.Style{Padding: ui.Symmetric(8, 10), Gap: 3},
		}, ui.Column(ui.Style{Gap: 2},
			ui.Text(ui.Val(fmt.Sprintf("#%d  %s", request.Number, request.Title)),
				ui.Style{Font: fonts.Footnote.Weighted(fonts.Medium), Lines: 2}),
			ui.Text(ui.Val(fmt.Sprintf("%s → %s · %s", request.HeadBranch, request.BaseBranch, request.State)),
				ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1}),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, emptySurface("No pull request is linked to this branch."))
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.Column(ui.Style{Padding: ui.All(10), Gap: 8},
			sectionTitle("Pull requests", "Open a request to see checks, reviews and actions."),
			ui.Show(signal.Func(func() bool { return a.prDetail.Get() != "" }), func() ui.Node {
				return ui.Text(a.prDetail, ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 4})
			}),
			ui.Column(ui.Style{Gap: 2}, rows...),
			ui.Button(ui.ButtonProps{Title: ui.Val("Refresh"), Icon: ui.IconRefresh, OnPress: a.loadPullRequests}),
		))
}

func pullRequestDetailView(a *app, view proto.PullRequestDetail) ui.Node {
	pr := view.PullRequest
	comment := a.prComment
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.Column(ui.Style{Padding: ui.All(10), Gap: 10},
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
				ui.Button(ui.ButtonProps{Icon: ui.IconBack, Label: ui.Val("Back to pull requests"),
					OnPress: func() { a.prView.Set(proto.PullRequestDetail{}) }}),
				ui.Text(ui.Val(fmt.Sprintf("#%d", pr.Number)), ui.Style{Font: fonts.Headline, Grow: 1}),
				ui.Button(ui.ButtonProps{Icon: ui.IconGlobe, Label: ui.Val("Open pull request"),
					OnPress: func() { ui.OpenURL(pr.URL) }}),
			),
			sectionTitle(pr.Title, fmt.Sprintf("%s · +%d −%d · %d files", pr.State, view.Additions, view.Deletions, view.ChangedFiles)),
			ui.Show(signal.Func(func() bool { return view.Body != "" }), func() ui.Node {
				return card(ui.Text(ui.Val(view.Body), ui.Style{Font: fonts.Footnote, Lines: 0}))
			}),
			ui.Row(ui.Style{Wrap: true, Gap: 6},
				ui.Button(ui.ButtonProps{Title: ui.Val("Merge…"), Variant: ui.ButtonFilled, OnPress: func() {
					ui.Alert(ui.AlertProps{Title: "Merge pull request?", Message: pr.Title, Actions: []ui.Action{
						{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
						{Title: ui.Val("Squash"), Role: ui.ActionDefault, OnPress: func() { a.mergePullRequest(pr.Number, "squash", true) }},
						{Title: ui.Val("Merge"), OnPress: func() { a.mergePullRequest(pr.Number, "merge", true) }},
						{Title: ui.Val("Rebase"), OnPress: func() { a.mergePullRequest(pr.Number, "rebase", true) }},
					}})
				}}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Approve"), OnPress: func() {
					a.reviewPullRequest(pr.Number, "approve", "")
				}}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Request changes"), OnPress: func() {
					a.reviewPullRequest(pr.Number, "request-changes", strings.TrimSpace(comment.Peek()))
				}}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Close"), Style: ui.Style{Color: colors.Red}, OnPress: func() {
					a.pullRequestAction(pr.Number, "close")
				}}),
			),
			ui.TextField(ui.TextFieldProps{Value: comment, Multiline: true, Placeholder: ui.Val("Leave a comment…"),
				Style: ui.Style{Lines: 4, MinHeight: ui.Pt(40)}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Comment"), OnPress: func() {
				body := strings.TrimSpace(comment.Peek())
				comment.Set("")
				a.commentPullRequest(pr.Number, body)
			}}),
			ui.Text(ui.Val("Checks"), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold)}),
			ui.Column(ui.Style{Gap: 3}, checkRows(view.Checks)...),
			ui.Text(ui.Val("Reviews"), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold)}),
			ui.Column(ui.Style{Gap: 3}, reviewRows(view.Reviews)...),
			ui.Show(signal.Func(func() bool { return len(view.Comments) > 0 }), func() ui.Node {
				return ui.Column(ui.Style{Gap: 3},
					ui.Text(ui.Val("Comments"), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold)}),
					ui.Column(ui.Style{Gap: 3}, commentRows(view.Comments)...))
			}),
			ui.Show(signal.Func(func() bool { return a.gitNote.Get() != "" }), func() ui.Node {
				return ui.Text(a.gitNote, ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 4})
			}),
		))
}

func checkRows(checks []proto.PullRequestCheck) []ui.Node {
	var rows []ui.Node
	for _, check := range checks {
		rows = append(rows, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(3, 2)},
			ui.Symbol(ui.IconCheck, ui.Style{Color: colors.Green}),
			ui.Text(ui.Val(check.Name), ui.Style{Font: fonts.Caption, Grow: 1, Lines: 1}),
			ui.Text(ui.Val(check.Status), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
		))
	}
	return rows
}

func reviewRows(reviews []proto.PullRequestReview) []ui.Node {
	var rows []ui.Node
	for _, review := range reviews {
		rows = append(rows, ui.Column(ui.Style{Gap: 1, Padding: ui.Symmetric(3, 2)},
			ui.Text(ui.Val(review.Author+" · "+review.State), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold)}),
			ui.Show(signal.Func(func() bool { return review.Body != "" }), func() ui.Node {
				return ui.Text(ui.Val(review.Body), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 4})
			}),
		))
	}
	return rows
}

func commentRows(comments []proto.PullRequestComment) []ui.Node {
	var rows []ui.Node
	for _, comment := range comments {
		rows = append(rows, card(ui.Text(ui.Val(comment.Author), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold)}),
			ui.Text(ui.Val(comment.Body), ui.Style{Font: fonts.Caption, Lines: 0})))
	}
	return rows
}

func graphSurface(a *app) ui.Node {
	if a.selected.Get() == "" {
		return emptySurface("Open a thread to see its commit graph.")
	}
	var rows []ui.Node
	for _, commit := range a.graph.Get() {
		commit := commit
		refs := make([]string, 0, len(commit.Refs))
		for _, ref := range commit.Refs {
			refs = append(refs, ref.Name)
		}
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() { ui.Copy(commit.Hash) },
			Style:   ui.Style{Padding: ui.Symmetric(6, 2), Gap: 2},
		}, ui.Column(ui.Style{Gap: 1},
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
				ui.Text(ui.Val(commit.Short), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Color: colors.Accent}),
				ui.Text(ui.Val(commit.Subject), ui.Style{Font: fonts.Caption, Grow: 1, Lines: 2}),
			),
			ui.Text(ui.Val(strings.Join(refs, " · ")+"  "+commit.Author+"  "+commit.Date.Local().Format("Jan 2")),
				ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1}),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, emptySurface("No commits found."))
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.Column(ui.Style{Padding: ui.All(10), Gap: 2}, rows...))
}

func terminalSurface(a *app) ui.Node {
	if a.selected.Get() == "" {
		return emptySurface("Open a thread to start its shell.")
	}
	var tabs []ui.Node
	for _, session := range a.terminals.Get() {
		session := session
		tabs = append(tabs, ui.Button(ui.ButtonProps{
			Title: ui.Val(session.Title), OnPress: func() { a.terminalID.Set(session.ID) },
			Style: ui.StyleFunc(func() ui.Style {
				style := ui.Style{Padding: ui.Symmetric(4, 8), CornerRadius: 7}
				if a.terminalID.Get() == session.ID {
					style.Background = colors.Accent.Opacity(0.14)
				}
				return style
			}),
		}))
	}
	return ui.Column(ui.Style{Grow: 1},
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Shrink: ui.NoShrink, Padding: ui.Symmetric(0, 8)}, Horizontal: true},
			ui.Row(ui.Style{Gap: 4, Padding: ui.Edges{Bottom: 5}},
				append(tabs,
					ui.Button(ui.ButtonProps{Icon: ui.IconPlus, Label: ui.Val("New terminal"), OnPress: a.newTerminal}),
					ui.Button(ui.ButtonProps{Icon: ui.IconTrash, Label: ui.Val("Close terminal"),
						OnPress: func() { a.closeTerminal(a.terminalID.Peek()) }}),
				)...,
			),
		),
		ui.Show(signal.Func(func() bool { return a.terminalID.Get() != "" }), func() ui.Node {
			return ui.WebView(ui.WebViewProps{
				HTML:  signal.Func(func() string { return a.terminalPageHTML() }),
				Style: ui.Style{Grow: 1, MinHeight: ui.Pt(320)},
			})
		}, func() ui.Node {
			return emptySurface("No terminal is available.")
		}),
	)
}

func previewSurface(a *app) ui.Node {
	var rows []ui.Node
	for _, server := range a.previewList.Get() {
		server := server
		label := fmt.Sprintf(":%d", server.Port)
		if server.Process != "" {
			label += "  " + server.Process
		}
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() { a.openPreview(server.URL) },
			Style:   ui.Style{Padding: ui.Symmetric(8, 10), Gap: 2},
		}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
			ui.Symbol(ui.IconGlobe, ui.Style{Color: colors.Accent}),
			ui.Column(ui.Style{Grow: 1, Gap: 1},
				ui.Text(ui.Val(label), ui.Style{Font: fonts.Footnote.Weighted(fonts.Medium)}),
				ui.Text(ui.Val(server.URL), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1}),
			),
			ui.Symbol(ui.IconForward, ui.Style{Color: colors.SecondaryLabel}),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, emptySurface("No local development servers are listening."))
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.Column(ui.Style{Padding: ui.All(10), Gap: 8},
			sectionTitle("Local preview", "Servers discovered on the environment's machine."),
			ui.Show(signal.Func(func() bool { return a.previewDetail.Get() != "" }), func() ui.Node {
				return ui.Text(a.previewDetail, ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 4})
			}),
			ui.Column(ui.Style{Gap: 2}, rows...),
			ui.Button(ui.ButtonProps{Title: ui.Val("Refresh"), Icon: ui.IconRefresh, OnPress: a.loadPreviewServers}),
		))
}

func devicesSurface(a *app) ui.Node {
	var rows []ui.Node
	for _, dev := range a.devices.Get() {
		dev := dev
		selected := a.deviceID.Get() == dev.ID
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() { a.selectDevice(dev.ID) },
			Style: ui.StyleFunc(func() ui.Style {
				style := ui.Style{Padding: ui.Symmetric(7, 8), Gap: 4, CornerRadius: 8}
				if selected {
					style.Background = colors.Accent.Opacity(0.12)
				}
				return style
			}),
		}, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 7},
			ui.Symbol(ui.IconPhone, ui.Style{Color: colors.Accent}),
			ui.Column(ui.Style{Grow: 1, Gap: 1},
				ui.Text(ui.Val(dev.Name), ui.Style{Font: fonts.Footnote.Weighted(fonts.Medium), Lines: 1}),
				ui.Text(ui.Val(dev.Platform+" "+dev.OS+" · "+dev.State),
					ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1}),
			),
			ui.Button(ui.ButtonProps{Title: ui.Val(map[bool]string{true: "Stop", false: "Boot"}[dev.State == "booted"]),
				OnPress: func() { a.setDeviceRunning(dev.ID, dev.State != "booted") }}),
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, emptySurface("No simulators or emulators were found."))
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.Column(ui.Style{Padding: ui.All(10), Gap: 8},
			sectionTitle("Devices", "Simulators and emulators on the environment's machine."),
			ui.Column(ui.Style{Gap: 2}, rows...),
			ui.Show(signal.Func(func() bool { return a.deviceShot.Get() != "" }), func() ui.Node {
				return ui.Column(ui.Style{Gap: 6},
					ui.Image(ui.ImageProps{Source: a.deviceShot, Mode: ui.ImageFit,
						Style: ui.Style{Width: ui.Pct(100), Height: ui.Pt(420), CornerRadius: 10, Background: colors.Background},
						Label: signal.Func(func() string { return "Device screenshot " + a.deviceID.Get() })}),
					ui.Button(ui.ButtonProps{Title: ui.Val("Refresh screenshot"), Icon: ui.IconRefresh,
						OnPress: a.refreshDeviceScreenshot}))
			}),
			ui.Show(signal.Func(func() bool { return a.deviceDetail.Get() != "" }), func() ui.Node {
				return ui.Text(a.deviceDetail, ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 4})
			}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Refresh"), Icon: ui.IconRefresh, OnPress: a.loadDevices}),
		))
}

func sideChatSurface(a *app) ui.Node {
	thread := a.sideChat.Get()
	if thread.ID == "" {
		return ui.Column(ui.Style{Grow: 1, Align: ui.AlignCenter, Justify: ui.JustifyCenter, Gap: 10, Padding: ui.All(22)},
			ui.Symbol(ui.IconMessage, ui.Style{Color: colors.Accent}),
			ui.Text(ui.Val("Side chat"), ui.Style{Font: fonts.Headline}),
			ui.Text(ui.Val("Ask a question without changing the thread's main conversation."),
				ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, TextAlign: ui.TextCenter}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Open side chat"), Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool { return a.selected.Get() == "" }),
				OnPress:  func() { a.openSideChat(a.selected.Peek()) }}),
		)
	}
	var messages []ui.Node
	for _, message := range a.sideMessages.Get() {
		message := message
		if message.Role == proto.RoleUser {
			messages = append(messages, ui.View(ui.ViewProps{Style: ui.Style{Align: ui.AlignEnd}},
				ui.Text(ui.Val(message.Content), ui.Style{Font: fonts.Footnote, Padding: ui.Symmetric(7, 10),
					CornerRadius: 10, Background: colors.Accent.Opacity(0.12), Lines: 0})))
			continue
		}
		messages = append(messages, ui.Text(ui.Val(message.Content),
			ui.Style{Font: fonts.Footnote, Color: colors.Label, Padding: ui.Symmetric(4, 2), Lines: 0}))
	}
	return ui.Column(ui.Style{Grow: 1},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(6, 10)},
			ui.Text(ui.Val(thread.Title), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold), Grow: 1, Lines: 1}),
			ui.Button(ui.ButtonProps{Icon: ui.IconRefresh, Label: ui.Val("Reload side chat"),
				OnPress: func() { a.loadSideChat(thread.ID) }}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.All(10), Gap: 8}, messages...)),
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.All(8)},
			ui.TextField(ui.TextFieldProps{Value: a.sideDraft, Placeholder: ui.Val("Ask in the side chat…"),
				Multiline: true, Style: ui.Style{Grow: 1, Lines: 4}}),
			ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send side chat"),
				Variant: ui.ButtonFilled, Disabled: a.sideBusy, OnPress: a.sendSideChat}),
		),
	)
}

func browserSurface(a *app) ui.Node {
	return ui.Column(ui.Style{Grow: 1},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6, Padding: ui.Symmetric(6, 8)},
			ui.TextField(ui.TextFieldProps{Value: a.browserInput, Placeholder: ui.Val("https://example.com"),
				Keyboard: ui.KeyboardURL, Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo,
				ReturnKey: ui.ReturnGo, OnSubmit: a.openBrowser, Style: ui.Style{Grow: 1}}),
			ui.Button(ui.ButtonProps{Icon: ui.IconForward, Label: ui.Val("Open address"), OnPress: a.openBrowser}),
		),
		ui.Show(signal.Func(func() bool { return a.browserURL.Get() != "" }), func() ui.Node {
			return ui.WebView(ui.WebViewProps{
				URL:   a.browserURL,
				Style: ui.Style{Grow: 1, MinHeight: ui.Pt(320)},
				OnError: func(message string) {
					ui.Post(func() { a.rightErr.Set(message) })
				},
				OnLoad: func(url string) {
					ui.Post(func() { a.browserInput.Set(url) })
				},
			})
		}, func() ui.Node {
			return emptySurface("Enter an address to open an in-app browser.")
		}),
	)
}

func emptySurface(message string) ui.Node {
	return ui.Column(ui.Style{Grow: 1, Align: ui.AlignCenter, Justify: ui.JustifyCenter, Gap: 6, Padding: ui.All(20)},
		ui.Symbol(ui.IconInfo, ui.Style{Color: colors.SecondaryLabel}),
		ui.Text(ui.Val(message), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, TextAlign: ui.TextCenter}),
	)
}

func settingsView(a *app) ui.Node {
	return ui.Dynamic(func() ui.Node { return settingsContentView(a) })
}

func settingsContentView(a *app) ui.Node {
	sections := make([]ui.Node, 0, len(settingsSectionNames))
	for index, name := range settingsSectionNames {
		index, name := index, name
		sections = append(sections, ui.Button(ui.ButtonProps{
			Title: ui.Val(name),
			OnPress: func() {
				a.settingsSection.Set(index)
				switch index {
				case settingsEnvironments:
					a.refreshEnvironmentSnapshots()
				case settingsProviders:
					a.refreshProviders()
				case settingsUsage:
					a.loadUsage(a.usageDays.Peek())
				}
			},
			Style: ui.StyleFunc(func() ui.Style {
				style := ui.Style{Padding: ui.Symmetric(6, 10), CornerRadius: 8}
				if a.settingsSection.Get() == index {
					style.Background = colors.Accent.Opacity(0.14)
					style.Color = colors.Accent
				} else {
					style.Color = colors.SecondaryLabel
				}
				return style
			}),
		}))
	}
	var body ui.Node
	switch a.settingsSection.Get() {
	case settingsGeneral:
		body = settingsGeneralView(a)
	case settingsEnvironments:
		body = settingsEnvironmentsView(a)
	case settingsProviders:
		body = settingsProvidersView(a)
	case settingsUsage:
		body = settingsUsageView(a)
	case settingsWorktrees:
		body = settingsWorktreesView(a)
	case settingsShortcuts:
		body = settingsShortcutsView()
	case settingsAbout:
		body = settingsAboutView(a)
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(8, 12)},
			ui.Text(ui.Val("Settings"), ui.Style{Font: fonts.Title3.Weighted(fonts.Bold), Grow: 1}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close settings"),
				OnPress: func() { a.showSettings.Set(false) }}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Shrink: ui.NoShrink, Padding: ui.Symmetric(0, 10)}, Horizontal: true},
			ui.Row(ui.Style{Gap: 4, Padding: ui.Edges{Bottom: 8}}, sections...)),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.All(14), Gap: 12}, body)),
	)
}

func settingsGeneralView(a *app) ui.Node {
	options := a.modelOptions()
	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, option.Label)
	}
	return ui.Column(ui.Style{Gap: 12},
		sectionTitle("General", "Defaults for new threads and this environment's appearance."),
		ui.Text(ui.Val("Environment name"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
		ui.TextField(ui.TextFieldProps{Value: a.settingsTitle, Placeholder: ui.Val("T3Go")}),
		ui.Text(ui.Val("Default agent"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
		ui.Show(signal.Func(func() bool { return len(labels) > 0 }), func() ui.Node {
			return ui.Picker(ui.PickerProps{Items: ui.Val(labels), Selected: a.settingsModel, Label: ui.Val("Default agent")})
		}, func() ui.Node {
			return ui.Text(ui.Val("No installed provider reports models."), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel})
		}),
		ui.Text(ui.Val("Default permission"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
		ui.Picker(ui.PickerProps{Items: ui.Val([]string{"Supervised", "Accept edits", "Auto", "Full access"}),
			Selected: a.settingsPerm, Label: ui.Val("Default permission")}),
		ui.Text(ui.Val("Appearance"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
		ui.Picker(ui.PickerProps{Items: ui.Val([]string{"System", "Light", "Dark"}),
			Selected: a.settingsTheme, Label: ui.Val("Appearance")}),
		ui.Text(ui.Val("Accent (hex, optional)"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
		ui.TextField(ui.TextFieldProps{Value: a.settingsAccent, Placeholder: ui.Val("#0a84ff"),
			Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo}),
		ui.Show(signal.Func(func() bool { return a.settingsErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.settingsErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 4})
		}),
		ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
			if a.settingsSave.Get() {
				return "Saving…"
			}
			return "Save settings"
		}), Variant: ui.ButtonFilled, Disabled: a.settingsSave, OnPress: a.saveSettings}),
	)
}

func settingsProvidersView(a *app) ui.Node {
	var cards []ui.Node
	for _, provider := range a.providers.Get() {
		provider := provider
		status := "Installed"
		statusColor := colors.Green
		if !provider.Installed {
			status, statusColor = "Not found", colors.Red
		} else if provider.AuthStatus == "unauthenticated" {
			status, statusColor = "Signed out", colors.Orange
		} else if provider.AuthStatus == "authenticated" {
			status = "Installed · signed in"
		}
		var modelRows []ui.Node
		for _, model := range a.modelsForProvider(provider.ID) {
			label := modelLabel(model)
			if model.ContextWindow > 0 {
				label += " · " + humanTokens(int64(model.ContextWindow)) + " context"
			}
			row := []ui.Node{ui.Text(ui.Val(label),
				ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel, Lines: 1, Grow: 1})}
			if model.Custom {
				model := model
				row = append(row, ui.Button(ui.ButtonProps{Icon: ui.IconClose,
					Label:   ui.Val("Remove " + model.Name),
					OnPress: func() { a.removeCustomModel(provider.ID, model.ID) }}))
			}
			modelRows = append(modelRows, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 4}, row...))
		}
		commands := []ui.Node{}
		if !provider.Installed && provider.InstallCommand != "" {
			commands = append(commands, ui.Text(ui.Val(provider.InstallCommand),
				ui.Style{Font: fonts.Custom("monospace", fonts.Caption2), Color: colors.SecondaryLabel, Lines: 2}))
		}
		if provider.Installed && provider.AuthStatus == "unauthenticated" && provider.LoginCommand != "" {
			commands = append(commands, ui.Text(ui.Val(provider.LoginCommand),
				ui.Style{Font: fonts.Custom("monospace", fonts.Caption2), Color: colors.SecondaryLabel, Lines: 2}))
		}
		var providerActions []ui.Node
		providerActions = append(providerActions,
			ui.Button(ui.ButtonProps{Title: ui.Val("Add model"), Icon: ui.IconPlus,
				OnPress: func() { a.openCustomModelDialog(provider.ID) }}))
		if !provider.Installed && provider.CanInstall && provider.InstallCommand != "" {
			providerActions = append(providerActions,
				ui.Button(ui.ButtonProps{Title: ui.Val("Run install"), Icon: ui.IconDownload,
					OnPress: func() { a.runProviderCommand(provider.InstallCommand) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Copy command"), Icon: ui.IconCopy,
					OnPress: func() { ui.Copy(provider.InstallCommand) }}),
			)
		}
		if provider.Installed && provider.CanAuthenticate && provider.AuthStatus != "authenticated" && provider.LoginCommand != "" {
			providerActions = append(providerActions,
				ui.Button(ui.ButtonProps{Title: ui.Val("Sign in"), Icon: ui.IconPerson,
					OnPress: func() { a.runProviderCommand(provider.LoginCommand) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Copy command"), Icon: ui.IconCopy,
					OnPress: func() { ui.Copy(provider.LoginCommand) }}),
			)
		}
		cards = append(cards, card(
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
				ui.Text(ui.Val(provider.Name), ui.Style{Font: fonts.Subheadline.Weighted(fonts.Semibold), Grow: 1}),
				ui.Text(ui.Val(status), ui.Style{Font: fonts.Caption2, Color: statusColor}),
			),
			ui.Show(signal.Func(func() bool { return provider.Version != "" }), func() ui.Node {
				return ui.Text(ui.Val(provider.Version), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel})
			}),
			ui.Show(signal.Func(func() bool { return provider.Error != "" }), func() ui.Node {
				return ui.Text(ui.Val(provider.Error), ui.Style{Font: fonts.Caption2, Color: colors.Red, Lines: 3})
			}),
			ui.Column(ui.Style{Gap: 1}, modelRows...),
			ui.Column(ui.Style{Gap: 2}, commands...),
			ui.Row(ui.Style{Wrap: true, Gap: 5}, providerActions...),
		))
	}
	if len(cards) == 0 {
		cards = append(cards, emptySurface("No providers were reported."))
	}
	return ui.Column(ui.Style{Gap: 10},
		sectionTitle("Providers", "Agent CLIs available on the environment's machine."),
		ui.Button(ui.ButtonProps{Title: ui.Val("Refresh providers"), Icon: ui.IconRefresh,
			Disabled: a.providersBusy, OnPress: a.refreshProviders}),
		ui.Show(signal.Func(func() bool { return a.providersBusy.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8}, ui.Activity(), ui.Text(ui.Val("Detecting…"),
				ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
		}),
		ui.Show(signal.Func(func() bool { return a.providersErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.providersErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 4})
		}),
		ui.Column(ui.Style{Gap: 8}, cards...),
	)
}

func settingsUsageView(a *app) ui.Node {
	report := a.usageReport.Get()
	usageEnv := "This environment"
	if a.usageEnv.Get() == "all" {
		usageEnv = "All environments"
	}
	var ranges []ui.Node
	for _, days := range []int{1, 7, 30, 90} {
		days := days
		label := fmt.Sprintf("%dd", days)
		if days == 1 {
			label = "Today"
		}
		ranges = append(ranges, ui.Button(ui.ButtonProps{Title: ui.Val(label), OnPress: func() { a.loadUsage(days) },
			Style: ui.StyleFunc(func() ui.Style {
				style := ui.Style{Padding: ui.Symmetric(4, 8), CornerRadius: 7}
				if a.usageDays.Get() == days {
					style.Background = colors.Accent.Opacity(0.14)
					style.Color = colors.Accent
				}
				return style
			})}))
	}
	var maxTokens int64
	for _, day := range report.Days {
		if day.Tokens > maxTokens {
			maxTokens = day.Tokens
		}
	}
	var bars []ui.Node
	for _, day := range report.Days {
		day := day
		height := float32(2)
		if maxTokens > 0 && day.Tokens > 0 {
			height = 4 + 90*float32(day.Tokens)/float32(maxTokens)
		}
		bars = append(bars, ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, MinWidth: ui.Pt(2),
			Height: ui.Pt(height), CornerRadius: 2, Background: colors.Accent.Opacity(0.6)}}))
	}
	var providerRows []ui.Node
	for _, provider := range report.Providers {
		providerRows = append(providerRows, ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(4, 2)},
			ui.Text(ui.Val(provider.Provider), ui.Style{Font: fonts.Caption.Weighted(fonts.Medium), Grow: 1}),
			ui.Text(ui.Val(fmt.Sprintf("%d runs", provider.Runs)), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
			ui.Text(ui.Val(humanTokens(provider.Tokens)), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
			ui.Text(ui.Val(fmt.Sprintf("$%.2f", provider.Cost)), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
		))
	}
	return ui.Column(ui.Style{Gap: 12},
		sectionTitle("Usage", "Tokens and cost recorded by the selected environments."),
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
			ui.Text(ui.Val(usageEnv), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Grow: 1}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Usage source"), Icon: ui.IconGlobe, Menu: usageEnvironmentActions(a)}),
		),
		ui.Row(ui.Style{Wrap: true, Gap: 4, Align: ui.AlignCenter}, ranges...),
		ui.Show(signal.Func(func() bool { return a.usageBusy.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8}, ui.Activity(), ui.Text(ui.Val("Loading usage…"),
				ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
		}),
		ui.Show(signal.Func(func() bool { return a.usageErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.usageErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 4})
		}),
		ui.Row(ui.Style{Gap: 6},
			usageCard("Runs", fmt.Sprint(report.Totals.Runs)),
			usageCard("Tokens", humanTokens(report.Totals.Tokens)),
			usageCard("Cost", fmt.Sprintf("$%.2f", report.Totals.Cost)),
		),
		ui.Show(signal.Func(func() bool { return len(report.Days) > 0 }), func() ui.Node {
			return ui.Column(ui.Style{Gap: 4},
				ui.Text(ui.Val(report.Since+" – today"), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
				ui.Row(ui.Style{Align: ui.AlignEnd, Gap: 2, Height: ui.Pt(98)}, bars...),
			)
		}),
		ui.Show(signal.Func(func() bool { return len(providerRows) > 0 }), func() ui.Node {
			return ui.Column(ui.Style{Gap: 2},
				ui.Text(ui.Val("Providers"), ui.Style{Font: fonts.Caption.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}),
				ui.Column(ui.Style{Gap: 2}, providerRows...))
		}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Refresh usage"), Icon: ui.IconRefresh,
			OnPress: func() { a.loadUsage(a.usageDays.Peek()) }}),
	)
}

func usageCard(label, value string) ui.Node {
	return ui.Column(ui.Style{Grow: 1, Padding: ui.Symmetric(7, 9), Gap: 1, CornerRadius: 8,
		Background: colors.SecondaryBackground},
		ui.Text(ui.Val(value), ui.Style{Font: fonts.Headline.Weighted(fonts.Semibold)}),
		ui.Text(ui.Val(label), ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
	)
}

func settingsWorktreesView(a *app) ui.Node {
	var rows []ui.Node
	for _, thread := range a.threads.Get() {
		if thread.WorktreePath == "" {
			continue
		}
		rows = append(rows, card(
			ui.Text(ui.Val(threadTitle(thread)), ui.Style{Font: fonts.Footnote.Weighted(fonts.Semibold), Lines: 1}),
			ui.Text(ui.Val(thread.Branch+" · "+thread.WorktreePath),
				ui.Style{Font: fonts.Custom("monospace", fonts.Caption2), Color: colors.SecondaryLabel, Lines: 2}),
		))
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text(ui.Val("No thread is using a worktree."),
			ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}))
	}
	return ui.Column(ui.Style{Gap: 8},
		sectionTitle("Worktrees", "Threads that run in their own checkout."),
		ui.Column(ui.Style{Gap: 6}, rows...),
	)
}

func settingsShortcutsView() ui.Node {
	return ui.Column(ui.Style{Gap: 8},
		sectionTitle("Shortcuts", "Keyboard shortcuts on desktop and the web."),
		shortcutRow("⌘/Ctrl K", "Command palette"),
		shortcutRow("⌘/Ctrl F", "Search messages"),
		shortcutRow("⌘/Ctrl N", "New thread"),
		shortcutRow("⌘/Ctrl ⇧ N", "New project"),
		shortcutRow("⌘/Ctrl ⇧ \\", "Open surfaces"),
		shortcutRow("⌘/Ctrl ,", "Settings"),
		shortcutRow("Esc", "Close the open sheet"),
		shortcutRow("↑ / ↓", "Move through the sidebar"),
	)
}

func shortcutRow(keys, action string) ui.Node {
	return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 10, Padding: ui.Symmetric(3, 2)},
		ui.Text(ui.Val(keys), ui.Style{Font: fonts.Custom("monospace", fonts.Caption), Color: colors.Accent}),
		ui.Text(ui.Val(action), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Grow: 1}),
	)
}

func settingsAboutView(a *app) ui.Node {
	info, _ := device.GetInfo()
	rows := []ui.Node{
		aboutRow("Environment", a.envName.Get()),
		aboutRow("Host", a.envHost.Get()),
		aboutRow("Server", a.serverURL.Get()),
		aboutRow("Protocol", fmt.Sprint(proto.ProtocolVersion)),
		aboutRow("Device", strings.TrimSpace(info.System+" "+info.SystemVersion+" · "+info.Model)),
		aboutRow("App", strings.TrimSpace("T3Go "+info.AppVersion+" "+info.Build)),
	}
	return ui.Column(ui.Style{Gap: 8},
		sectionTitle("About", "The connected environment and this app."),
		card(ui.Column(ui.Style{Gap: 5}, rows...)),
	)
}

func aboutRow(label, value string) ui.Node {
	return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
		ui.Text(ui.Val(label), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Width: ui.Pt(82)}),
		ui.Text(ui.Val(value), ui.Style{Font: fonts.Caption, Lines: 2, Grow: 1}),
	)
}

func searchView(a *app) ui.Node {
	return ui.Dynamic(func() ui.Node { return searchContentView(a) })
}

func searchContentView(a *app) ui.Node {
	var rows []ui.Node
	for _, hit := range a.searchHits.Get() {
		hit := hit
		rows = append(rows, ui.View(ui.ViewProps{
			OnPress: func() { a.pickSearchHit(hit) },
			Style:   ui.Style{Padding: ui.Symmetric(8, 10), Gap: 3},
		}, ui.Column(ui.Style{Gap: 1},
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 6},
				ui.Text(ui.Val(hit.ThreadTitle), ui.Style{Font: fonts.Footnote.Weighted(fonts.Medium), Grow: 1, Lines: 1}),
				ui.Text(ui.Val(hit.CreatedAt.Local().Format("Jan 2, 15:04")),
					ui.Style{Font: fonts.Caption2, Color: colors.SecondaryLabel}),
			),
			ui.Text(ui.Val(hit.Snippet), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel, Lines: 3}),
			ui.Text(ui.Val(hit.ProjectName+" · "+string(hit.Role)),
				ui.Style{Font: fonts.Caption2, Color: colors.TertiaryLabel}),
		)))
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.Symmetric(8, 12)},
			ui.Text(ui.Val("Search"), ui.Style{Font: fonts.Headline, Grow: 1}),
			ui.Button(ui.ButtonProps{Icon: ui.IconClose, Label: ui.Val("Close search"),
				OnPress: func() { a.showSearch.Set(false) }}),
		),
		ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(4, 10)}},
			ui.TextField(ui.TextFieldProps{Value: a.searchQuery, Placeholder: ui.Val("Search messages"),
				ReturnKey: ui.ReturnSearch, OnSubmit: a.loadSearch}),
		),
		ui.Show(signal.Func(func() bool { return a.searchBusy.Get() }), func() ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8, Padding: ui.All(10)},
				ui.Activity(), ui.Text(ui.Val("Searching…"), ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}))
		}),
		ui.Show(signal.Func(func() bool { return a.searchErr.Get() != "" }), func() ui.Node {
			return ui.Text(a.searchErr, ui.Style{Font: fonts.Caption, Color: colors.Red, Lines: 4,
				Padding: ui.Symmetric(4, 12)})
		}),
		ui.Show(signal.Func(func() bool {
			return !a.searchBusy.Get() && len(a.searchHits.Get()) == 0 && strings.TrimSpace(a.searchQuery.Get()) != ""
		}),
			func() ui.Node {
				return emptySurface("No messages matched that search.")
			}),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.Column(ui.Style{Padding: ui.Edges{Bottom: 16}}, rows...)),
	)
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(diff.Hours()/24))
	}
	return t.Local().Format("Jan 2")
}
