package main

import (
	"time"

	"github.com/tk/t3go/internal/client"
)

const (
	routeCooldown  = 30 * time.Second
	relayResidence = 45 * time.Second
)

// Owned by the UI loop; time is passed in so recovery can be tested without waits.
type routeRecovery struct {
	failedUntil     map[string]time.Time
	relaySince      time.Time
	directURL       string
	directSuccesses int
}

func routeKind(routes []client.Route, address string) string {
	for _, route := range routes {
		if route.URL == address {
			return route.Kind
		}
	}
	return ""
}

func (r *routeRecovery) enter(now time.Time, kind string) {
	r.relaySince = time.Time{}
	if kind == "relay" {
		r.relaySince = now
	}
	r.directURL, r.directSuccesses = "", 0
}

func (r *routeRecovery) failed(now time.Time, address string) {
	if r.failedUntil == nil {
		r.failedUntil = map[string]time.Time{}
	}
	r.failedUntil[address] = now.Add(routeCooldown)
	r.directURL, r.directSuccesses = "", 0
}

func (r *routeRecovery) candidates(now time.Time, profile client.EnvironmentProfile, current string, direct bool) []client.Route {
	if direct && (profile.Preference == "relay-only" || r.relaySince.IsZero() || now.Sub(r.relaySince) < relayResidence) {
		return nil
	}
	var routes []client.Route
	for _, route := range profile.Routes {
		if route.URL == current || now.Before(r.failedUntil[route.URL]) ||
			(direct && route.Kind != "direct") ||
			(profile.Preference == "direct-only" && route.Kind != "direct") ||
			(profile.Preference == "relay-only" && route.Kind != "relay") {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func (r *routeRecovery) directReady(address string) bool {
	if address != r.directURL {
		r.directURL, r.directSuccesses = address, 0
	}
	r.directSuccesses++
	return r.directSuccesses >= 2
}
