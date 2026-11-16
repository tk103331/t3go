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
		}},
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Background: colors.Surface(),
			CornerRadius: 24, Overflow: core.Hidden}, Children: []core.Node{
			core.Scroll{Style: ui.FillRest, Children: []core.Node{
				core.View{Style: core.Style{Padding: core.Symmetric(24, 16), Gap: 10, AlignItems: core.AlignCenter},
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
						if target == nil {
							return ui.Text("Select an available environment.", 14, ui.Regular, colors.Danger)
						}
						if !target.settingsConnectionReady() {
							return ui.Text(target.envName()+" · "+target.connectionLabel()+". Reconnect in Settings.", 14, ui.Regular, colors.Danger)
						}
						// The target is the only dependency here. Draft/keyboard updates
						// belong to the composer, so they never replace its input node.
						return mobileComposer{Draft: target.newThreadPrompt.Get, OnDraftChange: target.newThreadPrompt.Set,
							Label: "New thread message", Placeholder: "Ask anything. @ files, $ skills, / commands",
							TestID: "new-thread-composer", SendTestID: "composer-send",
							Disabled: target.threadBusy.Get, AllowEmptySend: true, SendIcon: lucide.ArrowUp,
							OnSend: func() { a.createMobileThread(strings.TrimSpace(target.newThreadPrompt.GetUntracked()) != "") },
							Accessory: func(cx *core.Scope) core.Node {
								return ui.IconButton{Icon: lucide.Paperclip, Label: "Attach files",
									TestID: "new-thread-attach", Size: 40, IconSize: 20,
									OnPress: target.pickNewThreadAttachments}
							},
							Controls:       target.desktopNewThreadComposerControls,
							Attachments:    target.newThreadAttachmentChips,
							HasAttachments: func() bool { return len(target.newThreadFiles.Get()) > 0 },
							OnDropFiles:    target.attachNewThreadPaths,
						}
					}),
					core.Dynamic(func(cx *core.Scope) core.Node {
						if target := a.newThreadTarget(); target != nil {
							return target.desktopNewThreadContext(cx)
						}
						return core.Group{}
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
	var children []core.Node
	if target != nil {
		children = append(children, core.Text{Content: target.envName(), Style: core.Style{FontSize: 12},
			StyleFunc: func() core.Style { return core.Style{Color: ui.ColorsOf(cx).Muted()} }})
	}
	var items []core.Node
	for _, session := range a.mobileSessions("") {
		session := session
		items = append(items, ui.MenuItem{Text: session.envName(), TestID: "new-thread-environment-" + session.profile.ID,
			OnSelect: func() { a.selectThreadEnvironment(session.profile.ID) }})
	}
	children = append(children, ui.Menu{TestID: "new-thread-environment",
		Trigger: ui.Button{Text: "on " + name, Icon: lucide.Laptop, Flex: 1,
			Size: controlSize(cx), Variant: ui.ButtonGhost, Label: "Environment: " + name},
		Face: mobileMenuFace(cx, "on "+name, lucide.Laptop), Children: items, MaxHeight: 360})
	return core.View{Style: core.Style{Width: core.Pct(100), MinWidth: core.Pt(0), Direction: core.Row,
		AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 6},
		Children: children}
}

func (a *app) createMobileThread(send bool) {
	a.createPreparedThread(send)
}

func (a *app) createPreparedThread(send bool) {
	target := a
	if a.cx != nil && compactSettings(a.cx) {
		target = a.newThreadTarget()
	}
	if target == nil || target.threadBusy.GetUntracked() {
		return
	}
	if !target.settingsConnectionReady() {
		target.connErr.Set("Connect the selected environment in Settings first.")
		return
	}
	draft := target.newThreadPrompt.GetUntracked()
	prompt := strings.TrimSpace(draft)
	attachments := append([]proto.Attachment(nil), target.newThreadFiles.GetUntracked()...)
	if send && prompt == "" {
		return
	}
	if target.newThreadProj.GetUntracked() == "" {
		target.connErr.Set("Choose a project first.")
		return
	}
	if target.newThreadTitle.GetUntracked() == "" && prompt != "" {
		target.newThreadTitle.Set(oneLine(prompt, 72))
	}
	target.createThreadThen(func(created proto.Thread) {
		a.showNewThread.Set(false)
		target.newThreadFiles.Set(nil)
		if a.manager != nil {
			a.manager.activate(target.profile.ID)
		}
		if !send {
			target.saveAttachmentDraftFor(created.ID, attachments)
		}
		target.selectThread(created.ID)
		if send {
			// Keep any edits made while the create request was in flight.
			if target.newThreadPrompt.GetUntracked() == draft {
				target.newThreadPrompt.Set("")
			}
			target.sendPrompt(created.ID, prompt, attachments)
		} else if target.newThreadPrompt.GetUntracked() == draft {
			target.newThreadPrompt.Set("")
		}
	})
}
