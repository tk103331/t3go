package main

import (
	"slices"
	"strings"

	"github.com/tk/t3go/internal/client"
)

func (a *app) updateProfile(id string, change func(*client.EnvironmentProfile)) {
	for i := range a.profiles {
		if a.profiles[i].ID != id {
			continue
		}
		change(&a.profiles[i])
		a.profilesV.Set(slices.Clone(a.profiles))
		a.saveProfiles()
		return
	}
}

func (a *app) setProfilePreference(id, preference string) {
	preference = strings.TrimSpace(preference)
	switch preference {
	case "", "auto", "direct-only", "relay-only":
	default:
		return
	}
	a.updateProfile(id, func(p *client.EnvironmentProfile) { p.Preference = preference })
	if a.activeID == id && a.connected.Peek() {
		a.reconnectProfile(id)
	}
}

func (a *app) renameProfile(id, alias string) {
	alias = strings.TrimSpace(alias)
	a.updateProfile(id, func(p *client.EnvironmentProfile) { p.Alias = alias })
	if a.activeID == id {
		if p := a.findProfile(id); p.ID != "" {
			a.envName.Set(environmentBaseName(p))
			a.envHost.Set(environmentHost(p))
		}
	}
	a.emitEnvironmentSnapshots()
}

func (a *app) setDefaultProfile(id string) {
	if a.findProfile(id).ID == "" {
		return
	}
	a.defaultID = id
	a.saveProfiles()
	a.profilesV.Set(slices.Clone(a.profiles))
}

func (a *app) updateProfileToken(id, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	a.updateProfile(id, func(p *client.EnvironmentProfile) { p.Token = token })
	if a.activeID == id {
		a.reconnectProfile(id)
	}
}

func (a *app) reconnectProfile(id string) {
	p := a.findProfile(id)
	if p.ID == "" {
		return
	}
	a.activeID = id
	a.activeV.Set(id)
	a.saveProfiles()
	a.connectProfile(p, true)
}

func profilePreferenceLabel(preference string) string {
	switch preference {
	case "direct-only":
		return "Direct only"
	case "relay-only":
		return "Relay only"
	}
	return "Auto"
}
