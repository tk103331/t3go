package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/proto"
)

const relayAccountStorageKey = "relayAccount:v1"

type savedRelayAccount struct {
	Version   int                    `json:"version"`
	Server    string                 `json:"server"`
	AllowHTTP bool                   `json:"allowHttp,omitempty"`
	Session   proto.RelayUserSession `json:"session"`
}

func loadRelayAccount(store appStorage) savedRelayAccount {
	var account savedRelayAccount
	if raw, ok := store.GetItem(relayAccountStorageKey); ok {
		_ = json.Unmarshal([]byte(raw), &account)
	}
	if account.Version != 1 ||
		(account.Session.Token != "" && !account.Session.ExpiresAt.After(time.Now())) {
		account.Session = proto.RelayUserSession{}
	}
	return account
}

func (m *environmentManager) saveRelayAccount() {
	session := m.accountSession.GetUntracked()
	account := savedRelayAccount{
		Version: 1, Server: strings.TrimRight(strings.TrimSpace(m.accountServer.GetUntracked()), "/"),
		AllowHTTP: m.accountAllowHTTP.GetUntracked(), Session: session,
	}
	if session.Token == "" {
		m.store.RemoveItem(relayAccountStorageKey)
		return
	}
	raw, err := json.Marshal(account)
	if err == nil {
		_ = m.store.SetItem(relayAccountStorageKey, string(raw))
	}
}

func relayAccountServer(raw string) string {
	server := strings.TrimRight(strings.TrimSpace(raw), "/")
	if server != "" && !strings.Contains(server, "://") {
		server = "https://" + server
	}
	return server
}

// signInRelayAccount authenticates directly with the Relay, then discovers
// every environment owned by the account.
func (m *environmentManager) signInRelayAccount() {
	if m.accountBusy.GetUntracked() {
		return
	}
	server := relayAccountServer(m.accountServer.GetUntracked())
	email := strings.TrimSpace(m.accountEmail.GetUntracked())
	password := m.accountPassword.GetUntracked()
	mode := strings.ToLower(strings.TrimSpace(m.accountMode.GetUntracked()))
	if mode != "register" {
		mode = "login"
	}
	if server == "" {
		m.accountErr.Set("Enter the Relay server URL.")
		return
	}
	if email == "" || password == "" {
		m.accountErr.Set("Enter your email and password.")
		return
	}
	allowHTTP := m.accountAllowHTTP.GetUntracked()
	in := proto.RelayAccountRequest{
		Server: server, Email: email, Password: password, Name: strings.TrimSpace(m.accountName.GetUntracked()),
		AllowHTTP: allowHTTP,
	}
	m.accountBusy.Set(true)
	m.accountErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(m.cx.Context(), 40*time.Second)
		defer cancel()
		var session proto.RelayUserSession
		var err error
		if mode == "register" {
			session, err = client.RegisterRelayAccount(ctx, server, allowHTTP, in)
		} else {
			session, err = client.LoginRelayAccount(ctx, server, allowHTTP, in)
		}
		var environments []client.RelayEnvironment
		if err == nil {
			environments, err = client.RelayAccountEnvironments(ctx, server, session.Token, allowHTTP)
		}
		m.cx.Post(func() {
			m.accountBusy.Set(false)
			if err != nil {
				m.accountErr.Set(friendlyError(err))
				return
			}
			old := m.accountSession.GetUntracked()
			if old.Token != "" && (old.User.ID != session.User.ID ||
				m.accountServer.GetUntracked() != server) {
				m.removeAccountProfiles("")
			}
			m.accountSession.Set(session)
			m.accountEnvs.Set(environments)
			m.accountServer.Set(server)
			m.accountEmail.Set(session.User.Email)
			m.accountPassword.Set("")
			m.saveRelayAccount()
			m.mergeAccountEnvironments(server, session.Token, environments, true)
		})
	}()
}

func (m *environmentManager) accountSessionValid() bool {
	session := m.accountSession.GetUntracked()
	return session.Token != "" && session.ExpiresAt.After(time.Now())
}

func (m *environmentManager) refreshRelayAccount(silent bool) {
	if !m.accountSessionValid() || m.accountBusy.GetUntracked() {
		return
	}
	server := relayAccountServer(m.accountServer.GetUntracked())
	token := m.accountSession.GetUntracked().Token
	allowHTTP := m.accountAllowHTTP.GetUntracked()
	m.accountBusy.Set(true)
	if !silent {
		m.accountErr.Set("")
	}
	go func() {
		ctx, cancel := context.WithTimeout(m.cx.Context(), 30*time.Second)
		defer cancel()
		environments, err := client.RelayAccountEnvironments(ctx, server, token, allowHTTP)
		m.cx.Post(func() {
			m.accountBusy.Set(false)
			if err != nil {
				if client.IsAuthorizationError(err) {
					m.accountSession.Set(proto.RelayUserSession{})
					m.saveRelayAccount()
					m.accountErr.Set("Your account session expired. Sign in again.")
					return
				}
				if !silent {
					m.accountErr.Set(friendlyError(err))
				}
				return
			}
			m.accountEnvs.Set(environments)
			m.mergeAccountEnvironments(server, token, environments, false)
		})
	}()
}

func (m *environmentManager) mergeAccountEnvironments(server, token string,
	environments []client.RelayEnvironment, activateFirst bool) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	present := make(map[string]bool, len(environments))
	for _, environment := range environments {
		if environment.EnvironmentID == "" {
			continue
		}
		present[environment.EnvironmentID] = true
		discovered := client.RelayAccountProfile(server, token, environment)
		profiles := m.profiles.GetUntracked()
		index := -1
		for i, profile := range profiles {
			if profile.ID == environment.EnvironmentID {
				index = i
				break
			}
		}
		if index < 0 {
			m.addAccountProfile(discovered, activateFirst)
			continue
		}
		current := profiles[index]
		updated := current
		keepDirectAccess := current.AccountServer == "" &&
			strings.TrimSpace(current.Token) != "" && hasDirectEnvironmentRoute(current)
		if !keepDirectAccess {
			updated.Token = token
			updated.AccountServer = server
			updated.Preference = "relay-only"
			updated.Routes = mergeAccountRoute(current.Routes, discovered.Routes[0].URL)
		} else {
			// A paired LAN route keeps its environment token and route
			// preference. Account discovery only adds the Relay fallback.
			updated.Routes = mergeAccountRoute(current.Routes, discovered.Routes[0].URL)
		}
		if updated.Name == "" {
			updated.Name = environment.Name
		}
		if updated.PublicKey == "" {
			updated.PublicKey = environment.PublicKey
		}
		m.update(updated)
		if session := m.sessions[updated.ID]; session != nil {
			session.connectProfile(updated)
		}
	}

	// Account-discovered profiles disappear when ownership is revoked or
	// the environment is registered under another account.
	var stale []string
	for _, profile := range m.profiles.GetUntracked() {
		if profile.AccountServer == server && !present[profile.ID] {
			stale = append(stale, profile.ID)
		}
	}
	for _, id := range stale {
		m.forget(id)
	}
}

func hasDirectEnvironmentRoute(profile client.EnvironmentProfile) bool {
	for _, route := range profile.Routes {
		if route.Kind != "relay" {
			return true
		}
	}
	return false
}

func mergeAccountRoute(routes []client.Route, relayURL string) []client.Route {
	next := make([]client.Route, 0, len(routes)+1)
	for _, route := range routes {
		if route.Kind == "relay" && route.URL == relayURL {
			continue
		}
		next = append(next, route)
	}
	return append(next, client.Route{URL: relayURL, Kind: "relay"})
}

func (m *environmentManager) addAccountProfile(profile client.EnvironmentProfile, activate bool) {
	session := m.sessions[profile.ID]
	if session == nil {
		session = m.newSession(profile)
		m.sessions[profile.ID] = session
	} else {
		session.profile = profile
		session.connectProfile(profile)
	}
	profiles := append([]client.EnvironmentProfile(nil), m.profiles.GetUntracked()...)
	replaced := false
	for i := range profiles {
		if profiles[i].ID == profile.ID {
			profiles[i], replaced = profile, true
			break
		}
	}
	if !replaced {
		profiles = append(profiles, profile)
	}
	m.profiles.Set(profiles)
	if m.defaultID == "" {
		m.defaultID = profile.ID
	}
	if activate && (m.app == nil || m.app.profile.ID == "") {
		previous := m.app
		m.app, m.current = session, profile.ID
		m.active.Set(session)
		if previous != nil {
			m.cx.Post(func() { previous.cx.Dispose() })
		}
	}
	_ = m.save()
	m.revision.Set(m.revision.GetUntracked() + 1)
}

func (m *environmentManager) removeAccountProfiles(server string) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	var ids []string
	for _, profile := range m.profiles.GetUntracked() {
		if profile.AccountServer == "" || (server != "" && profile.AccountServer != server) {
			continue
		}
		ids = append(ids, profile.ID)
	}
	for _, id := range ids {
		m.forget(id)
	}
}

func (m *environmentManager) signOutRelayAccount() {
	session := m.accountSession.GetUntracked()
	server := relayAccountServer(m.accountServer.GetUntracked())
	allowHTTP := m.accountAllowHTTP.GetUntracked()
	m.accountSession.Set(proto.RelayUserSession{})
	m.accountEnvs.Set(nil)
	m.accountErr.Set("")
	m.removeAccountProfiles("")
	m.saveRelayAccount()
	if session.Token != "" && server != "" {
		go func() {
			ctx, cancel := context.WithTimeout(m.cx.Context(), 10*time.Second)
			defer cancel()
			_ = client.LogoutRelayAccount(ctx, server, session.Token, allowHTTP)
		}()
	}
}

func (m *environmentManager) startSavedRelayAccount() {
	if !m.accountSessionValid() {
		return
	}
	m.refreshRelayAccount(true)
}
