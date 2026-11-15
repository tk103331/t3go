package main

import (
	"fmt"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
)

func (a *app) mobileChangesView(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	git := a.detail.Get().Git
	branch := "detached HEAD"
	if git != nil && git.Branch != "" {
		branch = git.Branch
	}
	files := a.workingFiles.Get()
	additions, deletions := 0, 0
	for _, file := range files {
		additions += file.Additions
		deletions += file.Deletions
	}
	summary := fmt.Sprintf("%d files · +%d · −%d", len(files), additions, deletions)
	if len(files) == 0 {
		summary = "No changes"
	}

	var rows []core.Node
	selected := map[string]bool{}
	for _, path := range a.mobileCommitFiles.Get() {
		selected[path] = true
	}
	for _, file := range files {
		file := file
		rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
			MinHeight: core.Pt(48), Padding: core.Symmetric(4, 8), CornerRadius: 8}, Children: []core.Node{
			base.Button{Label: "Toggle " + file.Path, TestID: "mobile-commit-file-" + file.Path,
				OnPress: func(core.PressEvent) { a.toggleMobileCommitFile(file.Path) },
				Style: core.Style{Width: core.Pt(24), Height: core.Pt(24), AlignItems: core.AlignCenter,
					JustifyContent: core.JustifyCenter, CornerRadius: 7, BorderWidth: 1},
				StyleFunc: func() core.Style {
					if selected[file.Path] {
						return core.Style{Background: colors.AccentFill(), BorderColor: colors.AccentFill(), Color: colors.OnAccent()}
					}
					return core.Style{BorderColor: colors.Border(), Color: core.Transparent}
				}, Children: []core.Node{core.Text{Content: "✓", Style: core.Style{FontSize: 12},
					StyleFunc: func() core.Style { return core.Style{Color: colors.OnAccent()} }}}},
			core.Text{Content: file.Path, MaxLines: 1, Style: core.Style{FontSize: 12, FontFamily: "Menlo, monospace",
				FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: fmt.Sprintf("+%d −%d", file.Additions, file.Deletions), MaxLines: 1,
				Style: core.Style{FontSize: 10.5}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("Working tree clean.", 12, ui.Regular, colors.Faint))
	}

	actions := core.View{Style: core.Style{Direction: core.Row, Gap: 8, Padding: core.Edges{Bottom: 10}}, Children: []core.Node{
		ui.Button{Text: "Commit & push", Icon: lucide.ArrowUp, Variant: ui.ButtonPrimary, Size: controlSize(cx),
			TestID: "mobile-commit-push", Disabled: a.gitBusy.Get, OnPress: func() { a.openMobileCommit(false) }},
		ui.Button{Text: "Commit", Icon: lucide.Check, Variant: ui.ButtonSecondary, Size: controlSize(cx),
			TestID: "mobile-commit", Disabled: a.gitBusy.Get, OnPress: func() { a.openMobileCommit(false) }},
	}}

	return core.View{TestID: "mobile-changes", Style: core.Style{Direction: core.Column, FlexGrow: 1,
		FlexShrink: 1, MinHeight: core.Pt(0), Padding: core.All(10)}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Column, Gap: 1, Padding: core.Edges{Bottom: 4}}, Children: []core.Node{
			core.Text{Content: "Changes vs HEAD", MaxLines: 1, Style: core.Style{FontSize: 16, FontWeight: ui.Semibold},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
			core.Text{Content: branch + " · " + summary, MaxLines: 1, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
		}},
		actions,
		a.rightError(cx),
		core.Dynamic(func(*core.Scope) core.Node {
			note := a.gitNote.Get()
			if note == "" {
				return nil
			}
			return core.Text{Content: note, MaxLines: 3, Style: core.Style{FontSize: 11.5, LineHeight: 16},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }}
		}),
		core.Scroll{TestID: "mobile-changes-scroll", Style: ui.FillRest, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 2, Padding: core.Edges{Bottom: 12}}, Children: rows},
			a.diffNodeWith(a.workingDiff.Get(), a.workingRef, true),
		}},
	}}
}

func (a *app) mobileCommitDialog(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	git := a.detail.Get().Git
	branch := "detached HEAD"
	if git != nil && git.Branch != "" {
		branch = git.Branch
	}
	files := a.workingFiles.Get()
	selected := a.mobileCommitFiles.Get()
	additions, deletions := 0, 0
	for _, file := range files {
		if !containsString(selected, file.Path) {
			continue
		}
		additions += file.Additions
		deletions += file.Deletions
	}
	all := len(selected) == len(files)
	var filesNode core.Node = ui.Text("No changed files.", 12, ui.Regular, colors.Faint)
	if len(files) > 0 {
		var rows []core.Node
		for _, file := range files {
			file := file
			rows = append(rows, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
				MinHeight: core.Pt(44)}, Children: []core.Node{
				base.Button{Label: "Toggle " + file.Path, TestID: "mobile-commit-file-" + file.Path,
					OnPress: func(core.PressEvent) { a.toggleMobileCommitFile(file.Path) },
					Style: core.Style{Width: core.Pt(24), Height: core.Pt(24), AlignItems: core.AlignCenter,
						JustifyContent: core.JustifyCenter, CornerRadius: 7, BorderWidth: 1},
					StyleFunc: func() core.Style {
						if containsString(selected, file.Path) {
							return core.Style{Background: colors.AccentFill(), BorderColor: colors.AccentFill()}
						}
						return core.Style{BorderColor: colors.Border()}
					}, Children: []core.Node{core.Text{Content: "✓", Style: core.Style{FontSize: 12},
						StyleFunc: func() core.Style { return core.Style{Color: colors.OnAccent()} }}}},
				core.Text{Content: file.Path, MaxLines: 1, Style: core.Style{FontSize: 12, FontFamily: "Menlo, monospace",
					FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }},
				core.Text{Content: fmt.Sprintf("+%d −%d", file.Additions, file.Deletions), MaxLines: 1,
					Style: core.Style{FontSize: 10.5}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			}})
		}
		filesNode = core.Scroll{Style: core.Style{MaxHeight: core.Pt(240)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Column, Gap: 2}, Children: rows},
		}}
	}

	var branchField core.Node
	if a.mobileCommitNewBranch.Get() {
		branchField = mobileInput{Scope: cx, Input: base.Input{Value: a.branchName.Get, OnChange: a.branchName.Set,
			Placeholder: "t3go/<slug>", Label: "New branch"}}
	}
	return ui.Dialog{NoCloseOnBackdropPress: true, Open: a.mobileCommitOpen.Get, OnOpenChange: func(open bool, _ base.Reason) {
		if !open {
			a.mobileCommitOpen.Set(false)
		}
	}, Title: "Commit changes", Description: branch, TestID: "mobile-commit-dialog", Children: []core.Node{
		core.Text{Content: "You're on " + branch + ". You can create a new branch when committing.",
			Style: core.Style{FontSize: 11.5, LineHeight: 16}, StyleFunc: func() core.Style { return core.Style{Color: colors.Warn()} }},
		mobileInput{Scope: cx, TestID: "mobile-commit-message", Input: base.Input{Value: a.gitMessage.Get,
			OnChange: a.gitMessage.Set, Placeholder: "Describe your changes…", Label: "Commit message"}},
		core.Text{Content: "Leave empty to generate a message automatically.", Style: core.Style{FontSize: 10.5},
			StyleFunc: func() core.Style { return core.Style{Color: colors.Faint()} }},
		branchField,
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
			core.Text{Content: fmt.Sprintf("Changed files · %d of %d · +%d −%d", len(selected), len(files), additions, deletions),
				MaxLines: 1, Style: core.Style{FontSize: 11, FlexGrow: 1, FlexShrink: 1},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
			ui.Button{Text: map[bool]string{true: "Select all", false: "Deselect all"}[all], Variant: ui.ButtonGhost,
				Size: controlSize(cx), TestID: "mobile-commit-toggle-all", OnPress: func() { a.setAllMobileCommitFiles(!all) }},
		}},
		filesNode,
		core.Show{When: a.gitBusy.Get, Then: func(*core.Scope) core.Node { return ui.Spinner{Size: 14, Label: "Committing"} }},
		core.Dynamic(func(cx *core.Scope) core.Node {
			note := a.gitNote.Get()
			if note == "" || a.gitBusy.Get() {
				return nil
			}
			return core.Text{Content: note, MaxLines: 3, Style: core.Style{FontSize: 11.5},
				StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }}
		}),
	}, Footer: []core.Node{
		ui.Button{Text: "Cancel", Variant: ui.ButtonGhost, TestID: "mobile-commit-cancel",
			OnPress: func() { a.mobileCommitOpen.Set(false) }},
		ui.Button{Text: "Commit changes", Icon: lucide.Check, Variant: ui.ButtonPrimary,
			TestID: "mobile-commit-confirm", Disabled: a.gitBusy.Get, OnPress: a.confirmMobileCommit},
		ui.Button{Text: map[bool]string{true: "Commit on current branch", false: "Commit on a new branch"}[a.mobileCommitNewBranch.Get()],
			Icon: lucide.GitBranch, Variant: ui.ButtonGhost, TestID: "mobile-commit-branch-mode",
			OnPress: func() { a.mobileCommitNewBranch.Set(!a.mobileCommitNewBranch.GetUntracked()) }},
	}}
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
