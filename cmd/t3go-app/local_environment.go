package main

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/i18n"
)

// localEnvironmentRemoved is a legacy marker from releases that allowed the
// bundled environment to be forgotten. Startup clears it so that environment
// is restored, but keeping the key lets that migration be tested directly.
const localEnvironmentRemoved = "localEnvironmentRemoved"

// localEnvironmentID names the environment server bundled with this client.
// Persist it so the client can recognize this machine before the server
// answers again.
const localEnvironmentID = "localEnvironmentID"

// backgroundServerKey remembers whether this machine's environment server
// keeps running after the client exits.
const backgroundServerKey = "backgroundServer"

// lanAccessKey remembers whether the local server listens for phones on
// this network, which needs no account.
const lanAccessKey = "lanAccess"

// loadBackgroundServer reads the saved choice; foreground is the default,
// so an environment never outlives the client unasked.
func loadBackgroundServer(store appStorage) bool {
	raw, _ := store.GetItem(backgroundServerKey)
	return raw == "true"
}

// loadLANAccess reads the saved choice; a fresh client listens on the
// loopback address until the user pairs a device on this network.
func loadLANAccess(store appStorage) bool {
	raw, _ := store.GetItem(lanAccessKey)
	return raw == "true"
}

// setBackgroundServer saves the choice and restarts the server this
// client started, since only a server's own command line carries the
// parent watch.
func (m *environmentManager) setBackgroundServer(on bool) {
	background := strconv.FormatBool(on)
	if err := m.store.SetItem(backgroundServerKey, background); err != nil {
		return
	}
	m.background.Set(on)
	m.restartLocalServer()
}

// setLANAccess saves the choice and restarts the server, since a listen
// address only takes effect when the process starts.
func (m *environmentManager) setLANAccess(on bool) {
	if err := m.store.SetItem(lanAccessKey, strconv.FormatBool(on)); err != nil {
		return
	}
	m.lan.Set(on)
	m.restartLocalServer()
}

// restartLocalServer applies a changed choice to the local server. Both a
// process owned by this app and a reusable background server expose an
// authenticated stop function.
func (m *environmentManager) restartLocalServer() {
	srv := m.local
	if !needsRestart(srv, m.background.GetUntracked(), m.lan.GetUntracked()) {
		return
	}
	srv.stop()
	m.local = nil
	m.startLocalEnvironment()
}

// needsRestart tells whether a changed choice restarts the running server.
func needsRestart(srv *localServer, background, lan bool) bool {
	return srv != nil && srv.stop != nil && (srv.background != background || srv.lan != lan)
}

// startLocalEnvironment starts the environment server the desktop package
// ships and connects this machine to it, so a fresh install works without
// a separately installed server.
func (m *environmentManager) startLocalEnvironment() {
	// A client pointed at an environment from outside (scripts/dev.sh, a
	// test harness) connects to that one instead of starting its own.
	if strings.TrimSpace(os.Getenv("T3GO_SERVER")) != "" {
		return
	}
	if bundledServerPath() == "" {
		return
	}
	m.localStarting.Set(true)
	// Read the scope's context here: the graph's current owner is only
	// meaningful while this runs on the graph's goroutine.
	ctx := m.cx.Context()
	background := m.background.GetUntracked()
	lan := m.lan.GetUntracked()
	go func() {
		srv, err := startLocalServer(ctx, background, lan)
		m.cx.Post(func() {
			m.localStarting.Set(false)
			if err != nil {
				if !errors.Is(err, errNoBundledServer) {
					m.localErr.Set(i18n.T("The local environment server did not start: ") + err.Error())
				}
				return
			}
			m.local = srv
			m.useLocalEnvironment(srv)
		})
	}()
}

// useLocalEnvironment puts this machine's own environment in the list and
// connects to it.
func (m *environmentManager) useLocalEnvironment(srv *localServer) {
	desc, err := fetchLocalDescriptor(srv.baseURL)
	if err != nil {
		m.localErr.Set(i18n.T("The local environment did not answer: ") + err.Error())
		return
	}
	m.localErr.Set("")
	localID := desc.ID
	m.rememberLocalEnvironment(localID)
	// This machine's bundled environment is permanent. Clear an old removal
	// marker so clients that previously allowed forgetting recover it now.
	m.store.RemoveItem(localEnvironmentRemoved)
	profile := client.EnvironmentProfile{
		ID:        localID,
		Name:      desc.Name,
		PublicKey: desc.PublicKey,
		Token:     srv.token,
		Routes:    []client.Route{{URL: srv.baseURL, Kind: "direct"}},
	}
	// Keep what the user set for this machine and every other route to it.
	for _, p := range m.profiles.GetUntracked() {
		if p.ID != localID {
			continue
		}
		profile.Alias = p.Alias
		profile.Preference = p.Preference
		profile.Legacy = p.Legacy
		if p.Name != "" {
			profile.Name = p.Name
		}
		profile.Routes = mergeLocalRoutes(p.Routes, srv.baseURL)
		break
	}
	profile.Routes = applyLANRoute(profile.Routes, srv)
	profile.Preference = "direct-only"
	session := m.sessions[localID]
	if session == nil {
		session = m.newSession(profile)
		m.sessions[localID] = session
	} else {
		session.profile = profile
		session.connectProfile(profile)
	}
	// Show this machine only where nothing else is open yet: a saved
	// environment the user was working in stays in front.
	if m.app == nil || m.app.profile.ID == "" {
		previous := m.app
		m.current, m.app = localID, session
		m.active.Set(session)
		if previous != nil {
			m.cx.Post(func() { previous.cx.Dispose() })
		}
	}
	if m.defaultID == "" {
		m.defaultID = localID
	}
	// The list keeps this machine's own routes only: an address the
	// server no longer answers on would fail on every connect.
	profiles := m.profiles.GetUntracked()
	next := make([]client.EnvironmentProfile, 0, len(profiles)+1)
	inserted := false
	for _, p := range profiles {
		if p.ID != localID {
			next = append(next, p)
			continue
		}
		next = append(next, profile)
		inserted = true
	}
	if !inserted {
		next = append(next, profile)
	}
	m.profiles.Set(next)
	_ = m.save()
	session.refreshAccess()
}

// rememberLocalEnvironment records this machine's stable identity so the
// bundle remains recognizable before the server finishes starting.
func (m *environmentManager) rememberLocalEnvironment(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	m.localID = id
	_ = m.store.SetItem(localEnvironmentID, id)
}

// isLocalEnvironment recognizes the bundled environment before its startup
// callback has populated localID. New clients use the persisted identity;
// older clients fall back to the running server's direct address.
func (m *environmentManager) isLocalEnvironment(id string) bool {
	if id == "" {
		return false
	}
	if id == m.localID {
		return true
	}
	if saved, _ := m.store.GetItem(localEnvironmentID); saved == id {
		return true
	}
	profile := m.find(id)
	if profile.ID == "" {
		return false
	}
	if m.local != nil {
		for _, route := range profile.Routes {
			if route.Kind == "direct" &&
				strings.TrimRight(route.URL, "/") == strings.TrimRight(m.local.baseURL, "/") {
				return true
			}
		}
	}
	return looksLikeBundledLocalEnvironment(profile)
}

// looksLikeBundledLocalEnvironment migrates clients written before the
// local identity was persisted. Legacy clients paired directly to the
// bundled server have a single loopback-only profile.
func looksLikeBundledLocalEnvironment(profile client.EnvironmentProfile) bool {
	if profile.ID == "" || !profile.Legacy || profile.AccountServer != "" || len(profile.Routes) == 0 {
		return false
	}
	for _, route := range profile.Routes {
		if route.Kind != "direct" || !isLoopbackURL(route.URL) {
			return false
		}
	}
	return true
}

// mergeLocalRoutes keeps the routes to this machine and replaces its
// loopback address with the one the local server answered on.
func mergeLocalRoutes(routes []client.Route, base string) []client.Route {
	var next []client.Route
	for _, r := range routes {
		if r.Kind == "direct" && isLoopbackURL(r.URL) {
			continue
		}
		next = append(next, r)
	}
	return append([]client.Route{{URL: base, Kind: "direct"}}, next...)
}

// applyLANRoute keeps the address a phone on this network uses in step
// with the server's listen mode. The loopback route stays first so this
// client keeps connecting the short way.
func applyLANRoute(routes []client.Route, srv *localServer) []client.Route {
	port := ""
	if u, err := url.Parse(srv.baseURL); err == nil {
		port = u.Port()
	}
	next := make([]client.Route, 0, len(routes)+1)
	for _, r := range routes {
		if r.Kind == "direct" && port != "" && isLANRoute(r.URL, port) {
			continue
		}
		next = append(next, r)
	}
	if !srv.lan || srv.lanIP == "" || port == "" {
		return next
	}
	return append(next, client.Route{URL: "http://" + net.JoinHostPort(srv.lanIP, port), Kind: "direct"})
}

// isLANRoute tells whether an address is a private network address on the
// port this machine's server uses.
func isLANRoute(raw, port string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Port() != port {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
