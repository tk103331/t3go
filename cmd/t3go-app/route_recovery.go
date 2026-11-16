package main

import (
	"context"
	"time"

	"github.com/egoist/asuka/ui"
	"github.com/tk/t3go/internal/client"
)

const (
	routeCooldown  = 30 * time.Second
	relayResidence = 45 * time.Second
	directProbe    = 30 * time.Second
)

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

func (a *app) startRouteMonitor() {
	a.routeMonitor.Do(func() {
		go func() {
			ticker := time.NewTicker(directProbe)
			defer ticker.Stop()
			for {
				select {
				case <-a.lifetime.Done():
					return
				case <-ticker.C:
					ui.Post(func() {
						c := a.client()
						if c == nil || a.offline.Peek() {
							return
						}
						profile := a.findProfile(a.activeID)
						if routeKind(profile.Routes, c.BaseURL) == "relay" {
							a.tryAlternateRoute(c, true)
						}
					})
				}
			}
		}()
	})
}

// tryAlternateRoute opens another verified route. Writes are not replayed:
// only the event stream and subsequent reads move to the new connection.
func (a *app) tryAlternateRoute(failed *client.Client, preferDirect bool) {
	if failed == nil || !a.routeMu.TryLock() {
		return
	}
	profile := a.findProfile(a.activeID)
	if profile.ID == "" || a.findProfile(profile.ID).PublicKey == "" || len(profile.Routes) < 2 {
		a.routeMu.Unlock()
		return
	}
	now := time.Now()
	if !preferDirect {
		a.routeRecovery.failed(now, failed.BaseURL)
	}
	candidate := profile
	candidate.Routes = a.routeRecovery.candidates(now, profile, failed.BaseURL, preferDirect)
	if len(candidate.Routes) == 0 {
		a.routeMu.Unlock()
		return
	}
	go func() {
		defer a.routeMu.Unlock()
		ctx, cancel := context.WithTimeout(a.lifetime, 12*time.Second)
		defer cancel()
		next, st, err := client.ConnectEnvironment(ctx, candidate)
		ui.Post(func() {
			if a.client() != failed {
				if next != nil {
					next.HTTP.CloseIdleConnections()
				}
				return
			}
			if err != nil {
				for _, route := range candidate.Routes {
					a.routeRecovery.failed(time.Now(), route.URL)
				}
				if client.IsAuthorizationError(err) {
					a.connected.Set(false)
					a.connErr.Set(friendlyError(err))
				}
				return
			}
			kind := routeKind(profile.Routes, next.BaseURL)
			if preferDirect && kind != "direct" {
				next.HTTP.CloseIdleConnections()
				return
			}
			if preferDirect && !a.routeRecovery.directReady(next.BaseURL) {
				next.HTTP.CloseIdleConnections()
				return
			}
			a.routeRecovery.enter(time.Now(), kind)
			a.notice.Set("")
			a.finishConnection(profile, next, st, false)
		})
	}()
}
