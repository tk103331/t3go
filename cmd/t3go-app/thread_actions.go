package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/asuka/sdk/store"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/proto"
)

func threadTitle(t proto.Thread) string {
	if title := strings.TrimSpace(t.Title); title != "" {
		return title
	}
	return "Untitled thread"
}

func (a *app) threadActions(t proto.Thread) []ui.Action {
	pinTitle := "Pin thread"
	if a.pinned.Peek()[t.ID] {
		pinTitle = "Unpin thread"
	}
	return []ui.Action{
		{Title: ui.Val("Rename…"), Icon: ui.IconEdit, OnPress: func() { a.renameThread(t) }},
		{Title: ui.Val(pinTitle), Icon: ui.IconPin, OnPress: func() { a.toggleThreadPin(t.ID) }},
		{Title: ui.Val("Fork thread"), Icon: ui.IconCopy, OnPress: func() { a.forkThread(t.ID, "", "") }},
		{Title: ui.Val("Open side chat"), Icon: ui.IconMessage, OnPress: func() { a.openSideChat(t.ID) }},
		{Title: ui.Val("Mark read"), Icon: ui.IconCheck, OnPress: func() { a.markThreadRead(t.ID) }},
		{Title: ui.Val("Archive"), Icon: ui.IconTrash, OnPress: func() { a.setThreadArchived(t, true) }},
		{Title: ui.Val("Delete…"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { a.confirmDeleteThread(t) }},
	}
}

func (a *app) threadHeaderActions() []ui.Action {
	t := a.detail.Peek().Thread
	if t.ID == "" {
		if id := a.selected.Peek(); id != "" {
			t = a.threadByID(id)
		}
	}
	surfaceMenu := ui.Submenu(ui.Val("Surfaces"), ui.IconList,
		ui.Action{Title: ui.Val("Files"), Icon: ui.IconFolder, OnPress: func() { a.openSurface(surfaceFiles) }},
		ui.Action{Title: ui.Val("Changes"), Icon: ui.IconDocument, OnPress: func() { a.openSurface(surfaceChanges) }},
		ui.Action{Title: ui.Val("Pull request"), Icon: ui.IconGlobe, OnPress: func() { a.openSurface(surfacePullRequest) }},
		ui.Action{Title: ui.Val("Git graph"), Icon: ui.IconList, OnPress: func() { a.openSurface(surfaceGraph) }},
		ui.Action{Title: ui.Val("Terminal"), Icon: ui.IconDocument, OnPress: func() { a.openSurface(surfaceTerminal) }},
		ui.Action{Title: ui.Val("Preview"), Icon: ui.IconGlobe, OnPress: func() { a.openSurface(surfacePreview) }},
		ui.Action{Title: ui.Val("Devices"), Icon: ui.IconPhone, OnPress: func() { a.openSurface(surfaceDevices) }},
		ui.Action{Title: ui.Val("Side chat"), Icon: ui.IconMessage, OnPress: func() { a.openSurface(surfaceSideChat) }},
		ui.Action{Title: ui.Val("Browser"), Icon: ui.IconGlobe, OnPress: func() { a.openSurface(surfaceBrowser) }},
	)
	actions := []ui.Action{
		surfaceMenu,
		{Title: ui.Val("Command palette…"), Icon: ui.IconMenu, OnPress: a.openPalette},
		{Title: ui.Val("Search messages…"), Icon: ui.IconSearch, OnPress: a.openSearch},
		{Title: ui.Val("Settings…"), Icon: ui.IconSettings, OnPress: func() { a.openSettings(settingsGeneral) }},
	}
	if t.ID == "" {
		return actions
	}
	archiveTitle := "Archive"
	if t.Archived {
		archiveTitle = "Unarchive"
	}
	settleTitle := "Mark settled"
	if t.Settled {
		settleTitle = "Reopen"
	}
	planTitle := "Plan first"
	if t.Plan {
		planTitle = "Turn off plan mode"
	}
	return append(actions,
		ui.Action{Title: ui.Val("Rename…"), Icon: ui.IconEdit, OnPress: func() { a.renameThread(t) }},
		ui.Submenu(ui.Val("Agent"), ui.IconPerson, a.threadModelActions(t)...),
		ui.Submenu(ui.Val("Permission"), ui.IconLock, a.threadPermissionActions(t)...),
		ui.Action{Title: ui.Val(planTitle), Icon: ui.IconCheck, Checked: ui.Val(t.Plan),
			OnPress: func() { a.updateThread(t.ID, map[string]any{"plan": !t.Plan}) }},
		ui.Action{Title: ui.Val("Fork thread"), Icon: ui.IconCopy, OnPress: func() { a.forkThread(t.ID, "", "") }},
		ui.Action{Title: ui.Val("Open side chat"), Icon: ui.IconMessage, OnPress: func() { a.openSideChat(t.ID) }},
		ui.Action{Title: ui.Val(settleTitle), Icon: ui.IconCheck, OnPress: func() { a.toggleThreadSettled(t) }},
		ui.Action{Title: ui.Val(archiveTitle), Icon: ui.IconTrash, OnPress: func() { a.setThreadArchived(t, !t.Archived) }},
		ui.Action{Title: ui.Val("Open in editor"), Icon: ui.IconEdit, OnPress: func() { a.openEditor("") }},
		ui.Action{Title: ui.Val("Delete…"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { a.confirmDeleteThread(t) }},
	)
}

func (a *app) threadModelActions(t proto.Thread) []ui.Action {
	var actions []ui.Action
	for _, option := range a.modelOptions() {
		option := option
		actions = append(actions, ui.Action{
			Title:   ui.Val(option.Label),
			Checked: ui.Val(option.Provider == t.Provider && option.Model == t.Model),
			OnPress: func() {
				a.updateThread(t.ID, map[string]any{"provider": option.Provider, "model": option.Model})
			},
		})
	}
	if len(actions) == 0 {
		actions = append(actions, ui.Action{Title: ui.Val("No models reported"), Disabled: ui.Val(true)})
	}
	return actions
}

func (a *app) threadPermissionActions(t proto.Thread) []ui.Action {
	modes := []struct {
		Mode  proto.PermissionMode
		Label string
	}{
		{proto.PermissionSupervised, "Supervised"},
		{proto.PermissionAcceptEdits, "Accept edits"},
		{proto.PermissionAuto, "Auto"},
		{proto.PermissionFullAccess, "Full access"},
	}
	actions := make([]ui.Action, 0, len(modes))
	for _, mode := range modes {
		mode := mode
		actions = append(actions, ui.Action{
			Title:   ui.Val(mode.Label),
			Checked: ui.Val(t.Permission == mode.Mode),
			OnPress: func() { a.updateThread(t.ID, map[string]any{"permission": string(mode.Mode)}) },
		})
	}
	return actions
}

func (a *app) renameThread(t proto.Thread) {
	name := signal.New(t.Title)
	ui.Alert(ui.AlertProps{
		Title: "Rename thread",
		Fields: []ui.AlertField{{
			Text:        name,
			Placeholder: "Thread title",
		}},
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Rename"), Role: ui.ActionDefault, OnPress: func() {
				title := strings.TrimSpace(name.Peek())
				if title == "" {
					a.notice.Set("A thread title cannot be empty.")
					return
				}
				a.updateThread(t.ID, map[string]any{"title": title})
			}},
		},
	})
}

func (a *app) setThreadArchived(t proto.Thread, archived bool) {
	a.updateThread(t.ID, map[string]any{"archived": archived})
}

func (a *app) toggleThreadSettled(t proto.Thread) {
	a.updateThread(t.ID, map[string]any{"settled": !t.Settled})
}

func (a *app) updateThread(id string, patch map[string]any) {
	c := a.client()
	if c == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		thread, err := c.UpdateThread(ctx, id, patch)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.upsertThread(thread)
			if a.selected.Peek() == thread.ID {
				detail := a.detail.Peek()
				detail.Thread = thread
				a.detail.Set(detail)
			}
		})
	}()
}

func (a *app) confirmDeleteThread(t proto.Thread) {
	ui.Alert(ui.AlertProps{
		Title:   "Delete thread?",
		Message: threadTitle(t) + " and its transcript will be removed.",
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() { a.deleteThread(t.ID) }},
		},
	})
}

func (a *app) deleteThread(id string) {
	c := a.client()
	if c == nil || id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		err := c.DeleteThread(ctx, id)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.removeThread(id)
			a.dropQueue(id)
			if a.selected.Peek() == id {
				a.clearThread()
			}
		})
	}()
}

func (a *app) forkThread(threadID, messageID, content string) {
	c := a.client()
	if c == nil || threadID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		thread, err := c.Fork(ctx, threadID, messageID, content)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.upsertThread(thread)
			a.selectThread(thread.ID)
		})
	}()
}

func (a *app) retryRun(runID string) {
	threadID := a.selected.Peek()
	c := a.client()
	if c == nil || threadID == "" || runID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		if _, err := c.Retry(ctx, threadID, runID); err != nil {
			ui.Post(func() { a.notice.Set(friendlyError(err)) })
			return
		}
		ui.Post(func() {
			if a.selected.Peek() == threadID {
				a.running.Set(true)
			}
		})
	}()
}

func (a *app) confirmRestoreRun(runID string) {
	if runID == "" {
		return
	}
	ui.Alert(ui.AlertProps{
		Title:   "Restore this checkpoint?",
		Message: "Tracked files in the checkout will return to this run's checkpoint. A backup is made first.",
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Restore"), Role: ui.ActionDestructive, OnPress: func() { a.restoreRun(runID) }},
		},
	})
}

func (a *app) restoreRun(runID string) {
	threadID := a.selected.Peek()
	c := a.client()
	if c == nil || threadID == "" || runID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		status, backup, err := c.RestoreRun(ctx, threadID, runID)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			detail := a.detail.Peek()
			detail.Git = &status
			a.detail.Set(detail)
			message := "Checkpoint restored."
			if backup != "" {
				message += " Backup: " + backup
			}
			a.notice.Set(message)
			a.loadSurface(a.rightTab.Peek())
		})
	}()
}

func (a *app) markThreadRead(id string) {
	seen := cloneSeen(a.seen.Peek())
	seen[id] = time.Now().UTC()
	_ = store.Set("threadSeen", seen)
	a.seen.Set(seen)
}

func cloneSeen(value map[string]time.Time) map[string]time.Time {
	next := make(map[string]time.Time, len(value)+1)
	for key, at := range value {
		next[key] = at
	}
	return next
}

func (a *app) toggleThreadPin(id string) {
	pinned := make(map[string]bool, len(a.pinned.Peek())+1)
	for key, value := range a.pinned.Peek() {
		pinned[key] = value
	}
	if pinned[id] {
		delete(pinned, id)
	} else {
		pinned[id] = true
	}
	_ = store.Set("pinnedThreads", pinned)
	a.pinned.Set(pinned)
}

func (a *app) isThreadPinned(id string) bool {
	return a.pinned.Get()[id]
}

func (a *app) isThreadUnread(t proto.Thread) bool {
	if t.ID == "" || t.ID == a.selected.Get() {
		return false
	}
	seen := a.seen.Get()[t.ID]
	return !seen.IsZero() && t.UpdatedAt.After(seen)
}

func (a *app) openEditor(editor string) {
	threadID := a.selected.Peek()
	c := a.client()
	if c == nil || threadID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		if err := c.OpenEditor(ctx, threadID, editor); err != nil {
			ui.Post(func() { a.notice.Set(friendlyError(err)) })
		}
	}()
}

func (a *app) openSideChat(parentID string) {
	c := a.client()
	if c == nil || parentID == "" {
		return
	}
	a.sideBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		thread, err := c.CreateSideChat(ctx, parentID)
		if err != nil {
			ui.Post(func() {
				if a.client() == c {
					a.sideBusy.Set(false)
					a.notice.Set(friendlyError(err))
				}
			})
			return
		}
		detail, err := c.ThreadDetail(ctx, thread.ID)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.sideBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.sideChat.Set(detail.Thread)
			a.sideMessages.Set(detail.Messages)
			a.sideDraft.Set("")
			a.rightTab.Set(surfaceSideChat)
			a.rightOpen.Set(true)
		})
	}()
}

func (a *app) sendSideChat() {
	thread := a.sideChat.Peek()
	text := strings.TrimSpace(a.sideDraft.Peek())
	c := a.client()
	if c == nil || thread.ID == "" || text == "" {
		return
	}
	a.sideDraft.Set("")
	a.sideBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 60*time.Second)
		defer cancel()
		_, err := c.Send(ctx, thread.ID, text)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.sideBusy.Set(false)
			if err != nil {
				a.sideDraft.Set(text)
				a.notice.Set(friendlyError(err))
			}
		})
	}()
}

func (a *app) loadSideChat(id string) {
	c := a.client()
	if c == nil || id == "" {
		return
	}
	a.sideBusy.Set(true)
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		detail, err := c.ThreadDetail(ctx, id)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			a.sideBusy.Set(false)
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.sideChat.Set(detail.Thread)
			a.sideMessages.Set(detail.Messages)
		})
	}()
}

func (a *app) applySideMessage(m proto.Message) {
	thread := a.sideChat.Peek()
	if thread.ID == "" || m.ThreadID != thread.ID {
		return
	}
	list := a.sideMessages.Peek()
	next := append([]proto.Message(nil), list...)
	for i := range next {
		if next[i].ID == m.ID {
			next[i] = m
			a.sideMessages.Set(next)
			return
		}
	}
	a.sideMessages.Set(append(next, m))
}

func (a *app) applySideDelta(d proto.MessageDelta) {
	list := a.sideMessages.Peek()
	next := append([]proto.Message(nil), list...)
	for i := range next {
		if next[i].ID == d.MessageID {
			next[i].Content += d.Delta
			a.sideMessages.Set(next)
			return
		}
	}
}

func (a *app) projectActions(p proto.Project) []ui.Action {
	return []ui.Action{
		{Title: ui.Val("New thread"), Icon: ui.IconPlus, OnPress: func() { a.openNewThread(p.ID) }},
		{Title: ui.Val("Rename…"), Icon: ui.IconEdit, OnPress: func() { a.renameProject(p) }},
		{Title: ui.Val("Delete…"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { a.confirmDeleteProject(p) }},
	}
}

func (a *app) renameProject(p proto.Project) {
	name := signal.New(p.Name)
	ui.Alert(ui.AlertProps{
		Title: "Rename project",
		Fields: []ui.AlertField{{
			Text:        name,
			Placeholder: "Project name",
		}},
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Rename"), Role: ui.ActionDefault, OnPress: func() {
				value := strings.TrimSpace(name.Peek())
				if value == "" {
					a.notice.Set("A project name cannot be empty.")
					return
				}
				a.updateProject(p.ID, value)
			}},
		},
	})
}

func (a *app) updateProject(id, name string) {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 20*time.Second)
		defer cancel()
		project, err := c.UpdateProject(ctx, id, name, "")
		ui.Post(func() {
			if a.client() != c {
				return
			}
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.upsertProject(project)
		})
	}()
}

func (a *app) confirmDeleteProject(p proto.Project) {
	ui.Alert(ui.AlertProps{
		Title:   "Delete project?",
		Message: fmt.Sprintf("%s and its %d threads will be removed.", p.Name, a.projectThreadCount(p.ID)),
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() { a.deleteProject(p.ID) }},
		},
	})
}

func (a *app) projectThreadCount(projectID string) int {
	count := 0
	for _, thread := range a.threads.Peek() {
		if thread.ProjectID == projectID {
			count++
		}
	}
	return count
}

func (a *app) deleteProject(id string) {
	c := a.client()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 30*time.Second)
		defer cancel()
		err := c.DeleteProject(ctx, id)
		ui.Post(func() {
			if a.client() != c {
				return
			}
			if err != nil {
				a.notice.Set(friendlyError(err))
				return
			}
			a.removeProject(id)
			if project := a.detail.Peek().Thread.ProjectID; project == id {
				a.clearThread()
			}
		})
	}()
}
