package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oneo-me/nsgo/base"
	"github.com/oneo-me/nsgo/core"
	"github.com/oneo-me/nsgo/icons/lucide"
	"github.com/oneo-me/nsgo/ui"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

type appStorage interface {
	GetItem(string) (string, bool)
	SetItem(string, string) error
	RemoveItem(string)
}

// environmentStorage isolates execution-specific UI data. A migrated first
// environment mirrors legacy keys so older app versions retain their profile.
type environmentStorage struct {
	base   core.Storage
	id     string
	legacy bool
}

func scopedKey(key string) bool {
	switch key {
	case "serverUrl", "token", "customModels", "attachmentDrafts", "drafts", "pinnedThreads",
		"sidebarCollapsed", "selectedThread", "pendingMessages":
		return true
	}
	return false
}

func (s environmentStorage) key(key string) string {
	if s.id != "" && scopedKey(key) {
		return "env:" + s.id + ":" + key
	}
	return key
}

func (s environmentStorage) GetItem(key string) (string, bool) {
	if value, ok := s.base.GetItem(s.key(key)); ok {
		return value, true
	}
	if s.legacy && scopedKey(key) {
		return s.base.GetItem(key)
	}
	return "", false
}

func (s environmentStorage) SetItem(key, value string) error {
	if err := s.base.SetItem(s.key(key), value); err != nil {
		return err
	}
	if s.legacy && scopedKey(key) && s.id != "" {
		return s.base.SetItem(key, value)
	}
	return nil
}

func (s environmentStorage) RemoveItem(key string) {
	s.base.RemoveItem(s.key(key))
	if s.legacy && scopedKey(key) {
		s.base.RemoveItem(key)
	}
}

type savedEnvironments struct {
	Version  int                         `json:"version"`
	Active   string                      `json:"active"`
	Default  string                      `json:"default,omitempty"`
	Profiles []client.EnvironmentProfile `json:"profiles"`
}

func storedValue[T any](store appStorage, key string, fallback T) T {
	if raw, ok := store.GetItem(key); ok {
		var value T
		if json.Unmarshal([]byte(raw), &value) == nil && strings.TrimSpace(raw) != "null" {
			return value
		}
	}
	return fallback
}

// Load data again when a connection screen adopts an already saved identity,
// so adding another route cannot replace its existing drafts with empty maps.
func (a *app) restoreEnvironmentData() {
	a.customModels.Set(storedValue(a.store, "customModels", map[string][]proto.Model{}))
	a.attachmentDrafts.Set(storedValue(a.store, "attachmentDrafts", map[string][]proto.Attachment{}))
	a.drafts.Set(storedValue(a.store, "drafts", map[string]string{}))
	a.pinned.Set(storedValue(a.store, "pinnedThreads", map[string]bool{}))
	a.sidebarCollapsed.Set(storedValue(a.store, "sidebarCollapsed", map[string]bool{}))
	a.queued = storedValue(a.store, "pendingMessages", map[string][]queuedPrompt{})
	a.queuePaused.Set(len(a.queued) > 0)
}

type environmentManager struct {
	cx            *core.Scope
	store         core.Storage
	profiles      *core.Signal[[]client.EnvironmentProfile]
	revision      *core.Signal[int]
	current       string
	defaultID     string
	adding        bool
	migrating     bool
	app           *app
	sessions      map[string]*app
	active        *core.Signal[*app]
	pending       *core.Signal[*app]
	sidebarKeys   *core.Signal[bool]
	sidebarOffset core.Points
}

func newEnvironmentManager(cx *core.Scope) *environmentManager {
	m := &environmentManager{cx: cx, store: cx.Storage(), sessions: map[string]*app{},
		sidebarKeys: core.NewSignal(cx, false),
		active:      core.NewSignal[*app](cx, nil), pending: core.NewSignal[*app](cx, nil), profiles: core.NewSignalFunc(cx,
			[]client.EnvironmentProfile{}, func([]client.EnvironmentProfile, []client.EnvironmentProfile) bool { return false }),
		revision: core.NewSignal(cx, 0)}
	var saved savedEnvironments
	if raw, ok := m.store.GetItem("environments:v1"); ok && json.Unmarshal([]byte(raw), &saved) == nil && saved.Version == 1 {
		m.profiles.Set(saved.Profiles)
		m.current, m.defaultID = saved.Active, saved.Default
		if m.find(m.current).ID == "" {
			m.current = m.defaultID
		}
		if m.find(m.current).ID == "" {
			m.current = ""
		}
	} else {
		m.migrating = true
		address, _ := m.store.GetItem("serverUrl")
		token, _ := m.store.GetItem("token")
		if p, err := client.PairingProfile(address, token); err == nil && address != "" {
			p.Legacy = true
			if p.ID == "" {
				p.ID = client.LegacyEnvironmentID(p.Routes[len(p.Routes)-1].URL)
			}
			p.Name = p.Routes[len(p.Routes)-1].URL
			m.profiles.Set([]client.EnvironmentProfile{p})
			m.current = p.ID
		}
	}
	return m
}

func (m *environmentManager) find(id string) client.EnvironmentProfile {
	for _, p := range m.profiles.GetUntracked() {
		if p.ID == id {
			return p
		}
	}
	return client.EnvironmentProfile{}
}

func (m *environmentManager) save() error {
	raw, err := json.Marshal(savedEnvironments{Version: 1, Active: m.current, Default: m.defaultID, Profiles: m.profiles.GetUntracked()})
	if err != nil {
		return err
	}
	return m.store.SetItem("environments:v1", string(raw))
}

func (m *environmentManager) activate(id string) {
	if id == "" {
		m.addEnvironment()
		return
	}
	next := m.sessions[id]
	if next == nil || next == m.app {
		return
	}
	if m.app != nil {
		m.app.persistView()
		next.sidebarQuery.Set(m.app.sidebarQuery.GetUntracked())
		next.showArchived.Set(m.app.showArchived.GetUntracked())
		next.sidebarOpen.Set(m.app.sidebarOpen.GetUntracked())
		next.sidebarWidth.Set(m.app.sidebarWidth.GetUntracked())
	}
	m.current, m.app = id, next
	next.applyAppearance(next.settings.GetUntracked())
	m.active.Set(next)
	_ = m.save()
}

// Sessions belong to the workspace scope, never to the selected conversation's
// render scope. Changing the visible conversation must not cancel another SSE.
func (m *environmentManager) newSession(p client.EnvironmentProfile) *app {
	var a *app
	m.cx.Run(func() {
		child := m.cx.Child()
		child.Run(func() {
			child.Untrack(func() { a = newAppForEnvironment(child, m, p) })
		})
	})
	return a
}

func (m *environmentManager) start() {
	for _, p := range m.profiles.GetUntracked() {
		m.sessions[p.ID] = m.newSession(p)
	}
	m.app = m.sessions[m.current]
	if m.app == nil {
		for _, p := range m.profiles.GetUntracked() {
			m.app, m.current = m.sessions[p.ID], p.ID
			break
		}
	}
	if m.app == nil {
		m.app = m.newSession(client.EnvironmentProfile{})
	}
	if m.defaultID == "" {
		m.defaultID = m.current
	}
	m.active.Set(m.app)
}

func (m *environmentManager) addEnvironment() {
	if m.pending.GetUntracked() != nil {
		return
	}
	m.adding = true
	pending := m.newSession(client.EnvironmentProfile{})
	m.adding = false
	m.pending.Set(pending)
}

func (m *environmentManager) closePending() {
	pending := m.pending.GetUntracked()
	if pending != nil {
		pending.connectionAttempt++
	}
	m.pending.Set(nil)
	if pending != nil {
		m.cx.Post(func() {
			if m.sessions[pending.profile.ID] != pending && m.app != pending {
				pending.cx.Dispose()
			}
		})
	}
}

func (m *environmentManager) register(a *app, previous string) {
	if previous != a.profile.ID && m.sessions[previous] == a {
		delete(m.sessions, previous)
	}
	m.sessions[a.profile.ID] = a
	if m.app == a {
		m.current = a.profile.ID
	}
	if m.pending.GetUntracked() == a {
		// Keep the current conversation visible after adding an environment.
		m.pending.Set(nil)
		if m.app == nil || (m.app.profile.ID == "" && !m.app.connected.GetUntracked()) {
			previousApp := m.app
			m.app, m.current = a, a.profile.ID
			m.active.Set(a)
			if previousApp != nil {
				m.cx.Post(func() { previousApp.cx.Dispose() })
			}
		}
	}
	_ = m.save()
	m.revision.Set(m.revision.GetUntracked() + 1)
}

func (a *app) persistView() {
	if a.draftSave != nil {
		a.draftSave()
		a.draftSave = nil
	}
	a.persistDrafts()
	a.saveAttachmentDraft()
	_ = a.store.SetItem("selectedThread", a.selected.GetUntracked())
	a.persistPendingMessages()
	a.stopDictation()
}

// Disposing a reactive scope must not change its signals: doing so can re-enter
// disposal. Persist raw values and cancel IO only.
func (a *app) disposeEnvironmentSession() {
	if a.draftSave != nil {
		a.draftSave()
		a.draftSave = nil
	}
	drafts := make(map[string]string)
	for id, text := range a.drafts.GetUntracked() {
		drafts[id] = text
	}
	id := a.selected.GetUntracked()
	if id != "" {
		if text := a.draft.GetUntracked(); strings.TrimSpace(text) != "" {
			drafts[id] = text
		} else {
			delete(drafts, id)
		}
	}
	if raw, err := json.Marshal(drafts); err == nil {
		_ = a.store.SetItem("drafts", string(raw))
	}
	_ = a.store.SetItem("selectedThread", id)
	a.persistPendingMessages()
	if a.speech != nil && !a.speech.Ended() {
		a.speech.Stop()
	}
	a.mu.Lock()
	if a.streamCancel != nil {
		a.streamCancel()
		a.streamCancel = nil
	}
	a.mu.Unlock()
}

func (m *environmentManager) remember(p client.EnvironmentProfile, previous string) error {
	all := append([]client.EnvironmentProfile(nil), m.profiles.GetUntracked()...)
	for _, old := range all {
		if old.ID == p.ID || old.ID == previous {
			if p.Alias == "" {
				p.Alias = old.Alias
			}
			if p.Preference == "" {
				p.Preference = old.Preference
			}
			// Preserve only routes already paired to this same identity.
			if old.PublicKey == p.PublicKey && p.PublicKey != "" {
				for _, route := range old.Routes {
					found := false
					for _, r := range p.Routes {
						if r.URL == route.URL {
							found = true
						}
					}
					if !found {
						p.Routes = append(p.Routes, route)
					}
				}
			}
			p.Legacy = p.Legacy || old.Legacy
		}
	}
	var next []client.EnvironmentProfile
	inserted := false
	for _, old := range all {
		if old.ID == p.ID || old.ID == previous {
			if !inserted {
				next = append(next, p)
				inserted = true
			}
		} else {
			next = append(next, old)
		}
	}
	if !inserted {
		next = append(next, p)
	}
	current, defaultID := m.current, m.defaultID
	if current == previous {
		current = p.ID
	}
	if defaultID == "" || defaultID == previous {
		defaultID = p.ID
	}
	raw, err := json.Marshal(savedEnvironments{Version: 1, Active: current, Default: defaultID, Profiles: next})
	if err != nil {
		return err
	}
	if err := m.store.SetItem("environments:v1", string(raw)); err != nil {
		return err
	}
	m.current, m.defaultID = current, defaultID
	m.profiles.Set(next)
	m.adding = false
	m.migrating = false
	return nil
}

func (a *app) saveEnvironmentSession() {
	a.persistView()
	a.mu.Lock()
	if a.streamCancel != nil {
		a.streamCancel()
		a.streamCancel = nil
	}
	a.mu.Unlock()
}

func (a *app) adoptEnvironment(p client.EnvironmentProfile, c *client.Client, st proto.State) error {
	old := a.profile.ID
	if st.EnvironmentID != "" {
		p.ID, p.PublicKey = st.EnvironmentID, st.IdentityKey
	} else if p.ID == "" {
		p.ID = client.LegacyEnvironmentID(c.BaseURL)
	}
	p.Name = st.Name
	if p.Name == "" {
		p.Name = c.BaseURL
	}
	p.Legacy = p.Legacy || a.profile.Legacy
	if a.manager != nil {
		if err := a.manager.remember(p, old); err != nil {
			return err
		}
		p = a.manager.find(p.ID)
		newStore := environmentStorage{base: a.manager.store, id: p.ID, legacy: p.Legacy}
		// Copy only this session's execution-specific values when adopting
		// a stable identity; do not import another environment's global data.
		if old != p.ID {
			for _, key := range []string{"customModels", "attachmentDrafts", "drafts", "pinnedThreads", "sidebarCollapsed", "selectedThread", "pendingMessages"} {
				if value, ok := a.store.GetItem(key); ok {
					if _, exists := newStore.base.GetItem(newStore.key(key)); !exists {
						if err := newStore.SetItem(key, value); err != nil {
							return err
						}
					}
				}
			}
		}
		previousStore := a.store
		a.store = newStore
		if old != p.ID {
			a.restoreEnvironmentData()
			if scratch, ok := previousStore.(environmentStorage); ok && scratch.id == "pending" {
				for _, key := range []string{"customModels", "attachmentDrafts", "drafts", "pinnedThreads", "sidebarCollapsed", "selectedThread", "pendingMessages"} {
					scratch.base.RemoveItem(scratch.key(key))
				}
			}
		}
	}
	a.profile = p
	if a.newProjectEnv.GetUntracked() == old {
		a.newProjectEnv.Set(p.ID)
	}
	if a.manager != nil {
		a.manager.register(a, old)
	}
	return nil
}

func (a *app) environmentList(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	if a.manager == nil {
		return core.Group{}
	}
	var rows []core.Node
	for _, profile := range a.manager.profiles.Get() {
		p := profile
		name := p.Name
		if p.Alias != "" {
			name = p.Alias
		}
		if name == "" {
			name = p.ID
		}
		if p.ID == a.manager.defaultID {
			name += " · Default"
		}
		var addresses []string
		for _, r := range p.Routes {
			addresses = append(addresses, r.Kind+": "+r.URL)
		}
		var modes []core.Node
		for _, mode := range []struct{ value, label string }{{"auto", "Auto"}, {"direct-only", "Direct"}, {"relay-only", "Relay"}} {
			mode := mode
			variant := ui.ButtonGhost
			if p.Preference == mode.value || (p.Preference == "" && mode.value == "auto") {
				variant = ui.ButtonSecondary
			}
			modes = append(modes, ui.Button{Text: mode.label, Variant: variant, Size: controlSize(cx),
				TestID: "environment-mode-" + p.ID + "-" + mode.value, OnPress: func() {
					p.Preference = mode.value
					a.manager.update(p)
					if session := a.manager.sessions[p.ID]; session != nil {
						session.connectProfile(p)
					}
				}})
		}
		status := "Not connected"
		var sessionError string
		session := a.manager.sessions[p.ID]
		if session != nil {
			status = session.connectionLabel()
			sessionError = session.connErr.Get()
		}
		rows = append(rows, core.View{TestID: "environment-" + p.ID,
			Style: core.Style{Gap: 6, Padding: core.All(10), CornerRadius: 8, BorderWidth: 1, BorderColor: colors.Border()},
			Children: []core.Node{
				ui.Text(name, 14, ui.Semibold, colors.Text),
				core.Text{Content: status, TestID: "settings-environment-status-" + p.ID,
					Style: core.Style{FontSize: 12}, StyleFunc: func() core.Style { return core.Style{Color: colors.Muted()} }},
				ui.Text(strings.Join(addresses, "\n"), 11, ui.Regular, colors.Muted),
				ui.Text(sessionError, 11, ui.Regular, colors.Danger),
				core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 6}, Children: []core.Node{
					ui.Button{Text: "Reconnect", Size: controlSize(cx), TestID: "environment-reconnect-" + p.ID,
						Disabled: func() bool { return session == nil || session.connecting.Get() },
						OnPress: func() {
							if session != nil {
								session.connectProfile(a.manager.find(p.ID))
							}
						}},
					ui.Button{Text: "Update token", Variant: ui.ButtonGhost, Size: controlSize(cx),
						TestID: "environment-token-" + p.ID, OnPress: func() {
							if session != nil {
								session.openCredentials()
							}
						}},
					ui.Button{Text: "Default", Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "environment-default-" + p.ID, OnPress: func() {
						a.manager.defaultID = p.ID
						_ = a.manager.save()
						a.manager.profiles.Set(append([]client.EnvironmentProfile(nil), a.manager.profiles.GetUntracked()...))
					}},
					ui.Button{Text: "Forget", Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "environment-forget-" + p.ID, OnPress: func() {
						a.forgetEnvironment.Set(p.ID)
					}},
					ui.Button{Text: "Rename", Variant: ui.ButtonGhost, Size: controlSize(cx), TestID: "environment-rename-" + p.ID, OnPress: func() {
						a.environmentAlias.Set(p.Alias)
						a.renameEnvironment.Set(p.ID)
					}},
				}},
				core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 4}, Children: modes},
			}})
	}
	if len(rows) == 0 {
		rows = append(rows, ui.Text("No saved environments.", 13, ui.Regular, colors.Muted))
	}
	return core.View{Style: core.Style{Gap: 8}, Children: rows}
}

func (a *app) environmentDialog(cx *core.Scope) core.Node {
	return ui.Dialog{Open: a.showEnvironments.Get, OnOpenChange: closeDialog(a.showEnvironments),
		Title: "Environments", Description: "Manage connections to your machines.",
		TestID: "environments-dialog", Style: dialogWidth(cx, 620),
		Children: []core.Node{core.Scroll{Style: core.Style{MaxHeight: core.Pt(420)}, Children: []core.Node{a.environmentManagement(cx)}}},
		Footer: []core.Node{
			ui.Button{Text: "Close", Variant: ui.ButtonGhost, Size: controlSize(cx), OnPress: func() { a.showEnvironments.Set(false) }},
		}}
}

func (a *app) environmentManagement(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Gap: 10}, Children: []core.Node{
		settingsSectionTitle(cx, "Environments", "All connected environments appear in the session list. Choose a target when creating a project."),
		ui.Button{Text: "Add environment", Icon: lucide.Plus, TestID: "environment-add", Size: controlSize(cx),
			OnPress: func() { a.manager.addEnvironment() }},
		core.Dynamic(a.environmentList),
		core.Show{When: func() bool { return a.renameEnvironment.Get() != "" }, Then: func(cx *core.Scope) core.Node {
			return core.View{Style: core.Style{Gap: 6}, Children: []core.Node{
				mobileInput{Scope: cx, Input: base.Input{Label: "Environment alias", Value: a.environmentAlias.Get, OnChange: a.environmentAlias.Set}},
				ui.Button{Text: "Save alias", OnPress: func() {
					p := a.manager.find(a.renameEnvironment.GetUntracked())
					p.Alias = strings.TrimSpace(a.environmentAlias.GetUntracked())
					a.manager.update(p)
					a.renameEnvironment.Set("")
				}},
			}}
		}},
		core.Show{When: func() bool { return a.forgetEnvironment.Get() != "" }, Then: func(*core.Scope) core.Node {
			return core.View{Style: core.Style{Gap: 6}, Children: []core.Node{
				ui.Text("Forget this connection? Local drafts and queued messages are kept. Remote projects and running tasks are not deleted.", 12, ui.Regular, ui.ColorsOf(cx).Muted),
				ui.Button{Text: "Confirm forget", TestID: "environment-confirm-forget", OnPress: func() {
					id := a.forgetEnvironment.GetUntracked()
					a.forgetEnvironment.Set("")
					a.manager.forget(id)
				}},
			}}
		}},
	}}
}

func (m *environmentManager) addEnvironmentDialog(cx *core.Scope) core.Node {
	pending := m.pending.Get()
	if pending == nil {
		return core.Group{}
	}
	return ui.Dialog{Open: func() bool { return m.pending.Get() != nil },
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				m.closePending()
			}
		},
		Title: "Add environment", TestID: "add-environment-dialog", Style: dialogWidth(cx, 560),
		Children: []core.Node{core.Scroll{StyleFunc: func() core.Style {
			height := (cx.Env().Window().Height - cx.Env().KeyboardHeight()) * 0.72
			return core.Style{MaxHeight: core.Pt(height)}
		},
			Children: []core.Node{core.Dynamic(pending.connectView)}}},
		Footer: []core.Node{ui.Button{Text: "Cancel", TestID: "environment-add-cancel", Variant: ui.ButtonGhost,
			Size: controlSize(cx), OnPress: m.closePending}}}
}

func (m *environmentManager) forget(id string) {
	session := m.sessions[id]
	if session != nil {
		session.persistView()
	}
	var next []client.EnvironmentProfile
	for _, p := range m.profiles.GetUntracked() {
		if p.ID != id {
			next = append(next, p)
		} else {
			s := environmentStorage{base: m.store, id: p.ID, legacy: p.Legacy}
			s.RemoveItem("token")
			s.RemoveItem("serverUrl")
		}
	}
	delete(m.sessions, id)
	if m.defaultID == id {
		m.defaultID = ""
	}
	if m.current == id {
		m.app = nil
		if len(next) > 0 {
			m.activate(next[0].ID)
		} else {
			m.current = ""
			m.adding = true
			m.app = m.newSession(client.EnvironmentProfile{})
			m.adding = false
			m.active.Set(m.app)
		}
	}
	m.profiles.Set(next)
	_ = m.save()
	m.revision.Set(m.revision.GetUntracked() + 1)
	if session != nil {
		m.cx.Post(func() { session.cx.Dispose() })
	}
}

func (m *environmentManager) update(profile client.EnvironmentProfile) {
	next := append([]client.EnvironmentProfile(nil), m.profiles.GetUntracked()...)
	for i, p := range next {
		if p.ID == profile.ID {
			next[i] = profile
			break
		}
	}
	m.profiles.Set(next)
	if session := m.sessions[profile.ID]; session != nil {
		session.profile = profile
	}
	_ = m.save()
}

// tryAlternateRoute changes transport only after verifying the same environment.
// It is called after a stream failure, never to replay a business mutation.
func (a *app) tryAlternateRoute(failed *client.Client, preferDirect bool) {
	if a.routing || a.transportBlocked.GetUntracked() || len(a.profile.Routes) < 2 || a.profile.PublicKey == "" {
		return
	}
	now := time.Now()
	if !preferDirect {
		a.routeRecovery.failed(now, failed.BaseURL)
	}
	p := a.profile
	p.Routes = a.routeRecovery.candidates(now, p, failed.BaseURL, preferDirect)
	if len(p.Routes) == 0 {
		return
	}
	a.routing = true
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 10*time.Second)
		defer cancel()
		next, st, err := client.ConnectEnvironment(ctx, p)
		a.cx.Post(func() {
			a.routing = false
			if a.client() != failed || a.transportBlocked.GetUntracked() {
				return
			}
			if err != nil {
				for _, route := range p.Routes {
					a.routeRecovery.failed(time.Now(), route.URL)
				}
				if client.IsAuthorizationError(err) {
					a.blockTransport(err)
				}
				return
			}
			if next.BaseURL == failed.BaseURL || (preferDirect && !a.routeRecovery.directReady(next.BaseURL)) {
				return
			}
			a.routeRecovery.enter(time.Now(), routeKind(a.profile.Routes, next.BaseURL))
			a.mu.Lock()
			a.cli = next
			a.mu.Unlock()
			a.serverURL.Set(next.BaseURL)
			_ = a.store.SetItem("serverUrl", next.BaseURL)
			a.applyState(st)
			a.eventSeq = st.Seq
			selected := a.selected.GetUntracked()
			if selected != "" {
				a.loadDetail(selected)
			}
			a.startStream(next, st.Seq)
			a.loadApprovals()
			// terminalHTML observes serverURL and reattaches the same session.
			a.markOnline()
		})
	}()
}

func (a *app) monitorDirect() {
	a.cx.AfterFunc(30*time.Second, func() {
		if c := a.client(); c != nil {
			for _, route := range a.profile.Routes {
				if route.URL == c.BaseURL && route.Kind == "relay" {
					a.tryAlternateRoute(c, true)
					break
				}
			}
		}
		a.monitorDirect()
	})
}

func (a *app) routeLabel() string {
	c := a.client()
	if c == nil {
		return ""
	}
	for _, route := range a.profile.Routes {
		if route.URL == c.BaseURL {
			return fmt.Sprintf("%s · %s", route.Kind, c.BaseURL)
		}
	}
	return c.BaseURL
}

func (a *app) persistPendingMessages() error {
	pending := make(map[string][]queuedPrompt, len(a.queued)+len(a.inflight))
	for thread, queue := range a.queued {
		pending[thread] = append([]queuedPrompt(nil), queue...)
	}
	for thread, prompt := range a.inflight {
		pending[thread] = append([]queuedPrompt{prompt}, pending[thread]...)
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return a.store.SetItem("pendingMessages", string(raw))
}

func (a *app) resyncEnvironment(c *client.Client) {
	if a.client() != c || a.transportBlocked.GetUntracked() {
		return
	}
	if a.routing {
		a.cx.AfterFunc(time.Second, func() { a.resyncEnvironment(c) })
		return
	}
	a.routing = true
	go func() {
		ctx, cancel := context.WithTimeout(a.lifetime, 10*time.Second)
		defer cancel()
		st, err := c.State(ctx)
		a.cx.Post(func() {
			a.routing = false
			if a.client() != c || a.transportBlocked.GetUntracked() {
				return
			}
			if err != nil {
				if client.IsAuthorizationError(err) {
					a.blockTransport(err)
					return
				}
				a.offline.Set(true)
				a.connErr.Set(friendlyError(err))
				a.tryAlternateRoute(c, false)
				// A stopped resync stream needs a new attempt after connectivity returns.
				a.cx.AfterFunc(3*time.Second, func() { a.resyncEnvironment(c) })
				return
			}
			a.applyState(st)
			a.eventSeq = st.Seq
			if id := a.selected.GetUntracked(); id != "" {
				a.loadDetail(id)
			}
			a.startStream(c, st.Seq)
			a.markOnline()
		})
	}()
}

func (a *app) blockTransport(err error) {
	a.transportBlocked.Set(true)
	a.queuePaused.Set(true)
	a.offline.Set(true)
	a.connErr.Set("Environment access was rejected. Update the connection token before reconnecting. " + friendlyError(err))
	a.mu.Lock()
	if a.streamCancel != nil {
		a.streamCancel()
	}
	a.mu.Unlock()
}
