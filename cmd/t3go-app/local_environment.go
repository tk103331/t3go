package main

import (
	"errors"
	"os"
	"strings"

	"github.com/tk/t3go/internal/client"
)

// localEnvironmentRemoved remembers the environment the user removed from
// the list, so this machine does not come back on the next start.
const localEnvironmentRemoved = "localEnvironmentRemoved"

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
	go func() {
		srv, err := startLocalServer(ctx)
		m.cx.Post(func() {
			m.localStarting.Set(false)
			if err != nil {
				if !errors.Is(err, errNoBundledServer) {
					m.localErr.Set("The local environment server did not start: " + err.Error())
				}
				return
			}
			m.useLocalEnvironment(srv)
		})
	}()
}

// useLocalEnvironment puts this machine's own environment in the list and
// connects to it.
func (m *environmentManager) useLocalEnvironment(srv *localServer) {
	desc, err := fetchLocalDescriptor(srv.baseURL)
	if err != nil {
		m.localErr.Set("The local environment did not answer: " + err.Error())
		return
	}
	m.localErr.Set("")
	localID := desc.ID
	if removed, _ := m.store.GetItem(localEnvironmentRemoved); removed == localID {
		return // the user removed this machine from the list
	}
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
	m.localID = localID
	m.store.RemoveItem(localEnvironmentRemoved)
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
