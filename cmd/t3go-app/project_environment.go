package main

import (
	"context"
	"errors"
	"time"

	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

func (a *app) projectTarget() *app {
	if a.manager == nil {
		return a
	}
	return a.manager.sessions[a.newProjectEnv.GetUntracked()]
}

func (a *app) selectProjectEnvironment(id string) {
	if a.projectBusy.GetUntracked() || id == a.newProjectEnv.GetUntracked() {
		return
	}
	a.newProjectEnv.Set(id)
	a.newProjectPath.Set("")
	a.newProjectRemote.Set(true)
	a.connErr.Set("")
	a.dirsSeq++
	a.newDirSeq++
	a.dirs.Set(proto.HostDirs{})
	a.dirsBusy.Set(false)
	a.dirsErr.Set("")
	a.newDirBusy.Set(false)
	a.showDirs.Set(false)
	a.closeNewDir()
}

func (a *app) projectEnvironmentSelector(cx *core.Scope) core.Node {
	if a.manager == nil {
		return a.environmentContext(cx, "project-environment")
	}
	selected := a.newProjectEnv.Get()
	_ = a.manager.revision.Get()
	var choices []core.Node
	for _, p := range a.manager.profiles.Get() {
		p := p
		session := a.manager.sessions[p.ID]
		if session == nil {
			continue
		}
		variant := ui.ButtonSecondary
		if selected == p.ID {
			variant = ui.ButtonPrimary
		}
		label := session.envName()
		if !session.connected.Get() || session.offline.Get() || session.transportBlocked.Get() {
			label += " · " + session.connectionLabel()
		}
		choices = append(choices, ui.Button{Text: label, Variant: variant, Size: controlSize(cx),
			TestID: "project-environment-" + p.ID, Disabled: a.projectBusy.Get,
			OnPress: func() { a.selectProjectEnvironment(p.ID) }})
	}
	return core.View{TestID: "project-environment", Style: core.Style{Gap: 6}, Children: []core.Node{
		ui.Text("Environment", 12, ui.Semibold, ui.ColorsOf(cx).Muted),
		core.Scroll{Style: core.Style{MaxHeight: core.Pt(120)}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: choices},
		}},
	}}
}

func (a *app) projectEnvironmentContext(cx *core.Scope, testID string) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = a.newProjectEnv.Get()
		if a.manager != nil {
			_ = a.manager.profiles.Get()
		}
		name := "Unavailable"
		if target := a.projectTarget(); target != nil {
			name = target.envName()
		}
		return core.Text{Content: "Environment: " + name, TestID: testID, MaxLines: 1,
			Style:     core.Style{FontSize: 12, FontWeight: ui.Semibold},
			StyleFunc: func() core.Style { return core.Style{Color: ui.ColorsOf(cx).Muted()} }}
	})
}

// Capture the selected client before starting IO. Completion belongs to the
// dialog's scope; a removed target cancels IO without mutating another session.
func (a *app) callProjectTarget(fn func(context.Context, *client.Client) error, done func(error)) {
	target := a.projectTarget()
	if target == nil || !target.connected.GetUntracked() || target.client() == nil {
		done(errors.New("Connect the selected environment in Settings first."))
		return
	}
	if target.transportBlocked.GetUntracked() {
		done(errors.New("Update the selected environment's token in Settings first."))
		return
	}
	c := target.client()
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		stop := context.AfterFunc(target.lifetime, cancel)
		defer stop()
		defer cancel()
		err := fn(ctx, c)
		a.cx.Post(func() { done(err) })
	}()
}
