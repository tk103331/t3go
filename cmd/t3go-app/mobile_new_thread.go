package main

import (
	"strings"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/proto"
)

func (a *app) newThreadTarget() *app {
	if a.manager == nil {
		return a
	}
	_ = a.manager.revision.Get()
	return a.manager.sessions[a.newThreadEnv.Get()]
}

func (a *app) selectThreadEnvironment(id string) {
	if target := a.newThreadTarget(); target != nil && target.threadBusy.GetUntracked() {
		return
	}
	for _, session := range a.mobileSessions("") {
		if session.profile.ID == id {
			if a.newThreadVisited == nil {
				a.newThreadVisited = map[*app]bool{}
			}
			if !a.newThreadVisited[session] {
				session.prepareNewThread()
				a.newThreadVisited[session] = true
			}
			a.newThreadEnv.Set(id)
			return
		}
	}
}

func (a *app) mobileNewThread(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{TestID: "new-thread-page", Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0),
		PaddingEnv: core.EnvEdges{Bottom: core.EnvKeyboard}}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Symmetric(8, 12),
			FlexShrink: 0}, Children: []core.Node{
			ui.IconButton{Icon: lucide.ArrowLeft, Label: "Cancel new thread", TestID: "cancel-thread", Size: 44,
				Disabled: func() bool { target := a.newThreadTarget(); return target != nil && target.threadBusy.Get() },
				OnPress:  func() { a.showNewThread.Set(false) }},
			ui.Text("New thread", 22, ui.Medium, colors.Text), ui.Spacer{},
			ui.IconButton{Icon: lucide.Folder, Label: "Choose project", TestID: "new-thread-choose-project", Size: 44,
				OnPress: func() { a.newThreadOptions.Set(true) }},
			ui.IconButton{Icon: lucide.GitBranch, Label: "Thread options", TestID: "new-thread-options", Size: 44,
				OnPress: func() { a.newThreadOptions.Set(true) }},
		}},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Background: colors.Surface(),
			CornerRadius: 24, Overflow: core.Hidden}, Children: []core.Node{
			core.Scroll{Style: ui.FillRest, Children: []core.Node{
				core.View{Style: core.Style{Padding: core.Symmetric(56, 22), Gap: 18, AlignItems: core.AlignCenter},
					Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
						target := a.newThreadTarget()
						project := "your project"
						if target != nil {
							for _, p := range target.projects.Get() {
								if p.ID == target.newThreadProj.Get() {
									project = p.Name
								}
							}
						}
						return core.Text{Content: "What should we build\nin " + project + "?", Style: core.Style{FontSize: 26,
							FontWeight: ui.Semibold, TextAlign: core.TextAlignCenter},
							StyleFunc: func() core.Style { return core.Style{Color: colors.Text()} }}
					}), core.Dynamic(a.mobileNewThreadEnvironment)}},
			}},
			core.View{Style: core.Style{Padding: core.Edges{Left: 12, Right: 12, Bottom: 10}, Gap: 8, FlexShrink: 0},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						if target := a.newThreadTarget(); target != nil {
							return ui.Text(target.connErr.Get(), 12, ui.Regular, colors.Danger)
						}
						return core.Group{}
					}),
					core.Dynamic(func(cx *core.Scope) core.Node {
						target := a.newThreadTarget()
						label := "Choose a project"
						if target != nil {
							label = "Current checkout"
							if target.newThreadWT.Get() {
								label = "New worktree"
							}
							if branch := target.newThreadBranch.Get(); branch != "" {
								label += " · " + branch
							} else {
								label += " · Current HEAD"
							}
						}
						return ui.Button{Text: label, Icon: lucide.GitBranch, Size: controlSize(cx), Variant: ui.ButtonGhost,
							TestID: "new-thread-checkout", OnPress: func() { a.newThreadOptions.Set(true) }}
					}),
					core.Dynamic(func(cx *core.Scope) core.Node {
						target := a.newThreadTarget()
						if target == nil {
							return ui.Text("Select an available environment.", 14, ui.Regular, colors.Danger)
						}
						if !target.settingsConnectionReady() {
							return ui.Text(target.envName()+" · "+target.connectionLabel()+". Reconnect in Settings.", 14, ui.Regular, colors.Danger)
						}
						// The target is the only dependency here. Draft/keyboard updates
						// belong to the composer, so they never replace its input node.
						return mobileComposer{Draft: target.newThreadPrompt.Get, OnDraftChange: target.newThreadPrompt.Set,
							Label: "New thread message", Placeholder: "Ask anything", TestID: "new-thread-composer",
							Disabled: target.threadBusy.Get,
							OnSend:   func() { a.createMobileThread(true) },
							Accessory: func(cx *core.Scope) core.Node {
								return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
									ui.IconButton{Icon: lucide.Plus, Label: "Thread options", TestID: "new-thread-more", Size: 40,
										OnPress: func() { a.newThreadOptions.Set(true) }},
									core.Dynamic(func(cx *core.Scope) core.Node {
										label := target.newThreadModel.Get()
										if label == "" {
											label = "Choose model"
										}
										return ui.Button{Text: oneLine(label, 22), Icon: lucide.ChevronDown, Variant: ui.ButtonGhost,
											TestID: "new-thread-model-options", OnPress: func() { a.newThreadOptions.Set(true) }}
									}),
								}}
							}}
					}),
				}},
		}},
	}}
}

func (a *app) mobileNewThreadEnvironment(cx *core.Scope) core.Node {
	target := a.newThreadTarget()
	name := "Choose environment"
	if target != nil {
		name = target.envName()
	}
	var items []core.Node
	for _, session := range a.mobileSessions("") {
		session := session
		items = append(items, ui.MenuItem{Text: session.envName(), TestID: "new-thread-environment-" + session.profile.ID,
			OnSelect: func() { a.selectThreadEnvironment(session.profile.ID) }})
	}
	return ui.Menu{TestID: "new-thread-environment", Trigger: ui.Button{Text: "on " + name, Icon: lucide.Laptop,
		Size: controlSize(cx), Variant: ui.ButtonGhost}, Children: items, MaxHeight: 360}
}

func (a *app) mobileNewThreadOptions(cx *core.Scope) core.Node {
	if !compactSettings(cx) {
		return core.Group{}
	}
	return ui.Dialog{Open: a.newThreadOptions.Get, OnOpenChange: closeDialog(a.newThreadOptions),
		Title: "Thread options", TestID: "new-thread-dialog", Style: dialogWidth(cx, 540),
		Children: []core.Node{
			core.Dynamic(a.mobileNewThreadEnvironment),
			core.Dynamic(func(cx *core.Scope) core.Node {
				if target := a.newThreadTarget(); target != nil {
					return core.Dynamic(target.newThreadBody)
				}
				return ui.Text("Choose an environment.", 14, ui.Regular, ui.ColorsOf(cx).Muted)
			}),
		},
		Footer: []core.Node{
			ui.Button{Text: "Done", TestID: "new-thread-options-done", Variant: ui.ButtonGhost, Size: controlSize(cx),
				OnPress: func() { a.newThreadOptions.Set(false) }},
			ui.Button{Text: "Create thread", Icon: lucide.MessageSquarePlus, TestID: "create-thread", Size: controlSize(cx),
				Disabled: func() bool { target := a.newThreadTarget(); return target == nil || target.threadBusy.Get() },
				OnPress:  func() { a.createMobileThread(false) }},
		}}
}

func (a *app) createMobileThread(send bool) {
	target := a.newThreadTarget()
	if target == nil || target.threadBusy.GetUntracked() {
		return
	}
	if !target.settingsConnectionReady() {
		target.connErr.Set("Connect the selected environment in Settings first.")
		return
	}
	draft := target.newThreadPrompt.GetUntracked()
	prompt := strings.TrimSpace(draft)
	if send && prompt == "" {
		return
	}
	if target.newThreadProj.GetUntracked() == "" {
		target.connErr.Set("Choose a project first.")
		a.newThreadOptions.Set(true)
		return
	}
	if target.newThreadTitle.GetUntracked() == "" && prompt != "" {
		target.newThreadTitle.Set(oneLine(prompt, 72))
	}
	target.createThreadThen(func(created proto.Thread) {
		a.showNewThread.Set(false)
		a.newThreadOptions.Set(false)
		if a.manager != nil {
			a.manager.activate(target.profile.ID)
		}
		target.selectThread(created.ID)
		if send {
			// Keep any edits made while the create request was in flight.
			if target.newThreadPrompt.GetUntracked() == draft {
				target.newThreadPrompt.Set("")
			}
			target.setDraft(prompt)
			target.send()
		}
	})
}
