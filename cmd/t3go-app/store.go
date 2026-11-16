package main

import (
	"encoding/json"
	"strings"

	"github.com/egoist/asuka/sdk/store"
	"github.com/tk/t3go/internal/client"
)

type savedEnvironments struct {
	Version  int                         `json:"version"`
	Active   string                      `json:"active"`
	Default  string                      `json:"default,omitempty"`
	Profiles []client.EnvironmentProfile `json:"profiles"`
}

func loadEnvironments() savedEnvironments {
	var saved savedEnvironments
	if store.Get("environments:v1", &saved) && saved.Version == 1 {
		return saved
	}
	// Older clients stored one connection under two keys.
	var address, token string
	_, _ = store.Get("serverUrl", &address), store.Get("token", &token)
	if strings.TrimSpace(address) == "" {
		return savedEnvironments{Version: 1}
	}
	p, err := client.PairingProfile(address, token)
	if err != nil {
		return savedEnvironments{Version: 1}
	}
	p.Legacy = true
	if p.ID == "" {
		p.ID = client.LegacyEnvironmentID(p.Routes[len(p.Routes)-1].URL)
	}
	p.Name = environmentHost(p)
	return savedEnvironments{Version: 1, Active: p.ID, Default: p.ID, Profiles: []client.EnvironmentProfile{p}}
}

func saveEnvironments(saved savedEnvironments) {
	saved.Version = 1
	_ = store.Set("environments:v1", saved)
}

func storedValue[T any](key string, fallback T) T {
	var value T
	if store.Get(key, &value) {
		return value
	}
	return fallback
}

func storeJSON(key string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	_ = store.Set(key, string(raw))
}
