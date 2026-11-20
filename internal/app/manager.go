package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// Manager owns every environment session: the saved profiles, which one
// is active, and the pending one a connection dialog fills in. Every
// field is read and written on the main thread; work on other goroutines
// reaches the manager through the window's Update.
type Manager struct {
	// post runs a function on the main thread, from any goroutine.
	post     func(func())
	win      *mygo.Window
	lifetime context.Context
	store    appStorage
	// appearance is the client's shared theme preference, copied into
	// every session it creates.
	appearance clientAppearance

	profiles []client.EnvironmentProfile
	// revision counts profile changes, so a view can cheaply notice that
	// the environment list changed.
	revision  int
	current   string
	defaultID string
	adding    bool
	migrating bool
	app       *Session
	sessions  map[string]*Session
	active    *Session
	pending   *Session

	// localStarting is set while the packaged environment server starts,
	// and localID names this machine's own environment once it answers.
	localStarting bool
	localErr      string
	localID       string
	// background keeps this machine's environment server running after the
	// client exits, so agent runs it owns finish. local is the handle to
	// the server this client started.
	background bool
	// lan controls whether the local server listens on this network. LAN
	// pairing uses the environment token and needs no Relay account.
	lan   bool
	local *localServer

	inboxCollapsed map[string]bool

	// showEnvironments opens the list of saved environments above the
	// workspace or the connection screen.
	showEnvironments bool

	accountOpen      bool
	accountBusy      bool
	accountErr       string
	accountSession   proto.RelayUserSession
	accountEnvs      []client.RelayEnvironment
	accountServer    string
	accountEmail     string
	accountPassword  string
	accountName      string
	accountMode      string
	accountAllowHTTP bool
}

// newManager loads the saved environments and prepares the manager. The
// session for the active profile is created in start.
func newManager(win *mygo.Window, store appStorage) *Manager {
	account := loadRelayAccount(store)
	post := func(fn func()) {
		if win != nil {
			win.Update(fn)
		}
	}
	m := &Manager{
		post:             post,
		win:              win,
		lifetime:         managerLifetime,
		store:            store,
		appearance:       loadClientAppearance(store),
		inboxCollapsed:   storedValue(store, "inboxCollapsed", map[string]bool{}),
		sessions:         map[string]*Session{},
		localStarting:    false,
		background:       loadBackgroundServer(store),
		lan:              loadLANAccess(store),
		accountOpen:      false,
		accountBusy:      false,
		accountErr:       "",
		accountSession:   account.Session,
		accountEnvs:      []client.RelayEnvironment{},
		accountServer:    account.Server,
		accountEmail:     account.Session.User.Email,
		accountPassword:  "",
		accountName:      account.Session.User.Name,
		accountMode:      "login",
		accountAllowHTTP: account.AllowHTTP,
	}
	var saved savedEnvironments
	if raw, ok := m.store.GetItem("environments:v1"); ok && json.Unmarshal([]byte(raw), &saved) == nil && saved.Version == 1 {
		m.profiles = saved.Profiles
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
			m.profiles = []client.EnvironmentProfile{p}
			m.current = p.ID
		}
	}
	localID, _ := m.store.GetItem(localEnvironmentID)
	localID = strings.TrimSpace(localID)
	if localID == "" {
		profiles := m.profiles
		for _, profile := range profiles {
			if profile.ID == m.current && looksLikeBundledLocalEnvironment(profile) {
				localID = profile.ID
				break
			}
		}
		if localID == "" {
			for _, profile := range profiles {
				if looksLikeBundledLocalEnvironment(profile) {
					localID = profile.ID
					break
				}
			}
		}
	}
	if localID != "" {
		m.localID = localID
		_ = m.store.SetItem(localEnvironmentID, localID)
		if removed, _ := m.store.GetItem(localEnvironmentRemoved); removed == localID {
			m.store.RemoveItem(localEnvironmentRemoved)
		}
		profiles := m.profiles
		next := append([]client.EnvironmentProfile(nil), profiles...)
		changed := false
		for i := range next {
			if next[i].ID == m.localID && next[i].Preference != "direct-only" {
				next[i].Preference = "direct-only"
				changed = true
			}
		}
		if changed {
			m.profiles = next
			_ = m.save()
		}
	}
	return m
}

func (m *Manager) find(id string) client.EnvironmentProfile {
	for _, p := range m.profiles {
		if p.ID == id {
			return p
		}
	}
	return client.EnvironmentProfile{}
}

func (m *Manager) save() error {
	raw, err := json.Marshal(savedEnvironments{Version: 1, Active: m.current, Default: m.defaultID, Profiles: m.profiles})
	if err != nil {
		return err
	}
	return m.store.SetItem("environments:v1", string(raw))
}

func (m *Manager) activate(id string) {
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
		next.sidebarQuery = m.app.sidebarQuery
		next.showArchived = m.app.showArchived
		next.sidebarOpen = m.app.sidebarOpen
		next.sidebarWidth = m.app.sidebarWidth
	}
	m.current, m.app = id, next
	m.active = next
	_ = m.save()
}

// newSession creates the session for one profile on the main thread.
func (m *Manager) newSession(p client.EnvironmentProfile) *Session {
	return newSession(m.win, m.store, m, p)
}

func (m *Manager) start() {
	for _, p := range m.profiles {
		m.sessions[p.ID] = m.newSession(p)
	}
	m.app = m.sessions[m.current]
	if m.app == nil {
		for _, p := range m.profiles {
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
	m.active = m.app
	// TODO(view): the view applies the client appearance every frame
	// (themeFor); the old client's applyAppearance ran here.
	// The desktop package ships an environment server: start it so the
	// client works on a fresh install without one installed separately.
	m.startLocalEnvironment()
	m.startSavedRelayAccount()
}

func (m *Manager) addEnvironment() {
	if m.pending != nil {
		return
	}
	m.adding = true
	pending := m.newSession(client.EnvironmentProfile{})
	m.adding = false
	m.pending = pending
}

func (m *Manager) closePending() {
	pending := m.pending
	if pending != nil {
		pending.connectionAttempt++
	}
	m.pending = nil
	if pending != nil {
		if m.sessions[pending.profile.ID] != pending && m.app != pending {
			// TODO(view): the view layer owns closing a session's window;
			// the old client disposed the session's scope here.
			pending.disposeEnvironmentSession()
		}
	}
}

func (m *Manager) register(s *Session, previous string) {
	if previous != s.profile.ID && m.sessions[previous] == s {
		delete(m.sessions, previous)
	}
	m.sessions[s.profile.ID] = s
	if m.app == s {
		m.current = s.profile.ID
	}
	if m.pending == s {
		// Keep the current conversation visible after adding an environment.
		m.pending = nil
		if m.app == nil || (m.app.profile.ID == "" && !m.app.connected) {
			previousApp := m.app
			m.app, m.current = s, s.profile.ID
			m.active = s
			if previousApp != nil {
				// TODO(view): the view layer owns closing a session's window;
				// the old client disposed the replaced session's scope here.
				previousApp.disposeEnvironmentSession()
			}
		}
	}
	_ = m.save()
	m.revision++
}

func (m *Manager) remember(p client.EnvironmentProfile, previous string) error {
	all := append([]client.EnvironmentProfile(nil), m.profiles...)
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
	m.profiles = next
	m.adding = false
	m.migrating = false
	return nil
}

func (m *Manager) forget(id string) {
	if m.isLocalEnvironment(id) {
		return
	}
	session := m.sessions[id]
	if session != nil {
		session.persistView()
	}
	var next []client.EnvironmentProfile
	for _, p := range m.profiles {
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
			m.active = m.app
		}
	}
	m.profiles = next
	_ = m.save()
	m.revision++
	if session != nil {
		// TODO(view): the view layer owns closing a session's window;
		// the old client disposed the forgotten session's scope here.
		session.disposeEnvironmentSession()
	}
}

func (m *Manager) update(profile client.EnvironmentProfile) {
	if m.isLocalEnvironment(profile.ID) {
		profile.Preference = "direct-only"
	}
	next := append([]client.EnvironmentProfile(nil), m.profiles...)
	for i, p := range next {
		if p.ID == profile.ID {
			next[i] = profile
			break
		}
	}
	m.profiles = next
	if session := m.sessions[profile.ID]; session != nil {
		session.profile = profile
	}
	_ = m.save()
}

// persistView records what a view keeps between frames: drafts, staged
// files, the selected thread and the pending messages.
func (s *Session) persistView() {
	if s.draftSave != nil {
		s.draftSave()
		s.draftSave = nil
	}
	s.persistDrafts()
	s.saveAttachmentDraft()
	_ = s.store.SetItem("selectedThread", s.selected)
	s.persistPendingMessages()
	// TODO(view): dictation (speech recognition) is not available in MyGo yet.
}

// disposeEnvironmentSession persists the session's data and cancels its
// IO. MyGo has no per-session scope to dispose: the view layer calls this
// when it closes the session's window.
func (s *Session) disposeEnvironmentSession() {
	if s.draftSave != nil {
		s.draftSave()
		s.draftSave = nil
	}
	drafts := make(map[string]string)
	for id, text := range s.drafts {
		drafts[id] = text
	}
	id := s.selected
	if id != "" {
		if text := s.draft; strings.TrimSpace(text) != "" {
			drafts[id] = text
		} else {
			delete(drafts, id)
		}
	}
	if raw, err := json.Marshal(drafts); err == nil {
		_ = s.store.SetItem("drafts", string(raw))
	}
	_ = s.store.SetItem("selectedThread", id)
	// TODO(view): dictation (speech recognition) is not available in MyGo yet.
	s.persistPendingMessages()
	s.mu.Lock()
	if s.streamCancel != nil {
		s.streamCancel()
		s.streamCancel = nil
	}
	s.mu.Unlock()
}

func (s *Session) saveEnvironmentSession() {
	s.persistView()
	s.mu.Lock()
	if s.streamCancel != nil {
		s.streamCancel()
		s.streamCancel = nil
	}
	s.mu.Unlock()
}

func (s *Session) adoptEnvironment(p client.EnvironmentProfile, c *client.Client, st proto.State) error {
	old := s.profile.ID
	if st.EnvironmentID != "" {
		p.ID, p.PublicKey = st.EnvironmentID, st.IdentityKey
	} else if p.ID == "" {
		p.ID = client.LegacyEnvironmentID(c.BaseURL)
	}
	p.Name = st.Name
	p.Hostname = strings.TrimSpace(st.Hostname)
	p.System = st.System
	if p.Name == "" {
		p.Name = c.BaseURL
	}
	p.Legacy = p.Legacy || s.profile.Legacy
	if s.manager != nil {
		if err := s.manager.remember(p, old); err != nil {
			return err
		}
		p = s.manager.find(p.ID)
		newStore := environmentStorage{base: s.manager.store, id: p.ID, legacy: p.Legacy}
		// Copy only this session's execution-specific values when adopting
		// a stable identity; do not import another environment's global data.
		if old != p.ID {
			for _, key := range []string{"customModels", "attachmentDrafts", "drafts", "pinnedThreads", "sidebarCollapsed", "selectedThread", "pendingMessages", "editorChoice"} {
				if value, ok := s.store.GetItem(key); ok {
					if _, exists := newStore.base.GetItem(newStore.key(key)); !exists {
						if err := newStore.SetItem(key, value); err != nil {
							return err
						}
					}
				}
			}
		}
		previousStore := s.store
		s.store = newStore
		if old != p.ID {
			s.restoreEnvironmentData()
			if scratch, ok := previousStore.(environmentStorage); ok && scratch.id == "pending" {
				for _, key := range []string{"customModels", "attachmentDrafts", "drafts", "pinnedThreads", "sidebarCollapsed", "selectedThread", "pendingMessages", "editorChoice"} {
					scratch.base.RemoveItem(scratch.key(key))
				}
			}
		}
	}
	s.profile = p
	if s.newProjectEnv == old {
		s.newProjectEnv = p.ID
	}
	if s.manager != nil {
		s.manager.register(s, old)
	}
	return nil
}

// tryAlternateRoute changes transport only after verifying the same environment.
// It is called after a stream failure, never to replay a business mutation.
func (s *Session) tryAlternateRoute(failed *client.Client, preferDirect bool) {
	if s.routing || s.transportBlocked || len(s.profile.Routes) < 2 || s.profile.PublicKey == "" {
		return
	}
	now := time.Now()
	if !preferDirect {
		s.routeRecovery.failed(now, failed.BaseURL)
	}
	p := s.profile
	p.Routes = s.routeRecovery.candidates(now, p, failed.BaseURL, preferDirect)
	if len(p.Routes) == 0 {
		return
	}
	s.routing = true
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 10*time.Second)
		defer cancel()
		next, st, err := client.ConnectEnvironment(ctx, p)
		s.post(func() {
			s.routing = false
			if s.client() != failed || s.transportBlocked {
				return
			}
			if err != nil {
				for _, route := range p.Routes {
					s.routeRecovery.failed(time.Now(), route.URL)
				}
				if client.IsAuthorizationError(err) {
					s.blockTransport(err)
				}
				return
			}
			if next.BaseURL == failed.BaseURL || (preferDirect && !s.routeRecovery.directReady(next.BaseURL)) {
				return
			}
			s.routeRecovery.enter(time.Now(), routeKind(s.profile.Routes, next.BaseURL))
			s.mu.Lock()
			s.cli = next
			s.mu.Unlock()
			s.serverURL = next.BaseURL
			_ = s.store.SetItem("serverUrl", next.BaseURL)
			s.applyState(st)
			s.eventSeq = st.Seq
			selected := s.selected
			if selected != "" {
				s.loadDetail(selected)
			}
			s.startStream(next, st.Seq)
			s.loadApprovals()
			// terminalHTML observes serverURL and reattaches the same session.
			s.markOnline()
		})
	}()
}

func (s *Session) monitorDirect() {
	s.after(30*time.Second, func() {
		if c := s.client(); c != nil {
			for _, route := range s.profile.Routes {
				if route.URL == c.BaseURL && route.Kind == "relay" {
					s.tryAlternateRoute(c, true)
					break
				}
			}
		}
		s.monitorDirect()
	})
}

func (s *Session) routeLabel() string {
	c := s.client()
	if c == nil {
		return ""
	}
	for _, route := range s.profile.Routes {
		if route.URL == c.BaseURL {
			return fmt.Sprintf("%s · %s", route.Kind, c.BaseURL)
		}
	}
	return c.BaseURL
}

func (s *Session) persistPendingMessages() error {
	pending := make(map[string][]queuedPrompt, len(s.queued)+len(s.inflight))
	for thread, queue := range s.queued {
		pending[thread] = append([]queuedPrompt(nil), queue...)
	}
	for thread, prompt := range s.inflight {
		pending[thread] = append([]queuedPrompt{prompt}, pending[thread]...)
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return s.store.SetItem("pendingMessages", string(raw))
}

func (s *Session) resyncEnvironment(c *client.Client) {
	if s.client() != c || s.transportBlocked {
		return
	}
	if s.routing {
		s.after(time.Second, func() { s.resyncEnvironment(c) })
		return
	}
	s.routing = true
	go func() {
		ctx, cancel := context.WithTimeout(s.lifetime, 10*time.Second)
		defer cancel()
		st, err := c.State(ctx)
		s.post(func() {
			s.routing = false
			if s.client() != c || s.transportBlocked {
				return
			}
			if err != nil {
				if client.IsAuthorizationError(err) {
					s.blockTransport(err)
					return
				}
				s.offline = true
				s.connErr = friendlyError(err)
				s.tryAlternateRoute(c, false)
				// A stopped resync stream needs a new attempt after connectivity returns.
				s.after(3*time.Second, func() { s.resyncEnvironment(c) })
				return
			}
			s.applyState(st)
			s.eventSeq = st.Seq
			if id := s.selected; id != "" {
				s.loadDetail(id)
			}
			s.startStream(c, st.Seq)
			s.markOnline()
		})
	}()
}

func (s *Session) blockTransport(err error) {
	s.transportBlocked = true
	s.queuePaused = true
	s.offline = true
	s.connErr = i18n.T("Environment access was rejected. Update the connection token before reconnecting. ") + friendlyError(err)
	s.mu.Lock()
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.mu.Unlock()
}

func (s *Session) markOnline() {
	wasOffline := s.offline
	s.offline = false
	if wasOffline && s.connected {
		s.connectionRecovered = true
		if s.recoveryNoticeCancel != nil {
			s.recoveryNoticeCancel()
		}
		s.recoveryNoticeCancel = s.after(4*time.Second, func() {
			s.connectionRecovered = false
			s.recoveryNoticeCancel = nil
		})
	}
}

func (s *Session) connectionLabel() string {
	_, label := s.connectionStatus()
	return label
}

// connectionStatus reports the connection's state and the label that
// shows it. The key stays the same in every language, so the interface
// can tell the states apart.
func (s *Session) connectionStatus() (string, string) {
	if s.connecting {
		return "connecting", i18n.T("Connecting…")
	}
	if s.transportBlocked {
		return "blocked", i18n.T("Authorization required")
	}
	if s.offline {
		return "reconnecting", i18n.T("Reconnecting…")
	}
	if !s.connected {
		return "offline", i18n.T("Not connected")
	}
	switch routeKind(s.profile.Routes, s.serverURL) {
	case "relay":
		return "relay", "Relay"
	case "direct":
		return "direct", i18n.T("Direct")
	default:
		return "connected", i18n.T("Connected")
	}
}

func (s *Session) openCredentials() {
	s.credentialToken = ""
	s.credentialError = ""
	s.showCredentials = true
}

func (s *Session) closeCredentials() {
	if s.connecting {
		return
	}
	s.showCredentials = false
	s.credentialToken = ""
	s.credentialError = ""
}

func (s *Session) updateCredentials() {
	if s.connecting {
		return
	}
	token := strings.TrimSpace(s.credentialToken)
	if token == "" {
		s.credentialError = i18n.T("Enter the environment token.")
		return
	}
	s.credentialError = ""
	profile := s.profile
	profile.Token = token
	s.connectProfile(profile)
}

// savedEnvironments is the storage record the client keeps.
type savedEnvironments struct {
	Version  int                         `json:"version"`
	Active   string                      `json:"active"`
	Default  string                      `json:"default,omitempty"`
	Profiles []client.EnvironmentProfile `json:"profiles"`
}
