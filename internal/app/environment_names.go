package app

import (
	"net/url"
	"strings"

	"github.com/tk/t3go/internal/client"
	"github.com/tk/t3go/internal/environment"
	"github.com/tk/t3go/internal/i18n"
	"github.com/tk/t3go/internal/proto"
)

// Keep display names independent of the active route: a relay fallback should
// not rename the machine the user was working on.
func environmentHost(p client.EnvironmentProfile) string {
	for _, relay := range []bool{false, true} {
		for _, route := range p.Routes {
			if (route.Kind == "relay") != relay {
				continue
			}
			if u, err := url.Parse(route.URL); err == nil && u.Host != "" && environment.UsableName(u.Hostname()) {
				return u.Host
			}
		}
	}
	return ""
}

func environmentBaseName(p client.EnvironmentProfile) string {
	if alias := strings.TrimSpace(p.Alias); environment.UsableName(alias) {
		return alias
	}
	hostname := strings.TrimSpace(p.Hostname)
	if !environment.UsableName(hostname) {
		hostname = ""
	}
	if name := strings.TrimSpace(p.Name); environment.UsableName(name) && !strings.EqualFold(name, "T3Go") {
		// Legacy profiles can use the full connection URL as their name.
		if u, err := url.Parse(name); err == nil && u.Host != "" {
			if hostname == "" {
				return u.Host
			}
		} else {
			return name
		}
	}
	if hostname != "" {
		return hostname
	}
	if p.System != nil && strings.TrimSpace(p.System.OS) != "" {
		return p.System.OS
	}
	if host := environmentHost(p); host != "" {
		return host
	}
	if p.ID != "" {
		return i18n.T("Environment")
	}
	return "T3Go"
}

func sameSystemInfo(a, b *proto.SystemInfo) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func environmentIDLabel(p client.EnvironmentProfile, profiles []client.EnvironmentProfile) string {
	for length := min(6, len(p.ID)); length < len(p.ID); length++ {
		unique := true
		for _, other := range profiles {
			if other.ID != p.ID && len(other.ID) >= length && strings.EqualFold(other.ID[:length], p.ID[:length]) {
				unique = false
				break
			}
		}
		if unique {
			return p.ID[:length]
		}
	}
	return p.ID
}

// Custom names win. Repeated names gain an address; machines sharing a relay
// host or the same address gain a short stable identity as a final distinction.
func environmentDisplayNames(profiles []client.EnvironmentProfile) map[string]string {
	names := make(map[string]string, len(profiles))
	counts := map[string]int{}
	for _, p := range profiles {
		name := environmentBaseName(p)
		names[p.ID] = name
		counts[strings.ToLower(name)]++
	}
	for _, p := range profiles {
		name := names[p.ID]
		if counts[strings.ToLower(name)] > 1 {
			if host := environmentHost(p); host != "" && !strings.EqualFold(host, name) {
				names[p.ID] = name + " · " + host
			}
		}
	}
	clear(counts)
	for _, name := range names {
		counts[strings.ToLower(name)]++
	}
	for _, p := range profiles {
		if name := names[p.ID]; counts[strings.ToLower(name)] > 1 {
			names[p.ID] = name + " · " + environmentIDLabel(p, profiles)
		}
	}
	return names
}

func (m *Manager) environmentNames() map[string]string {
	profiles := append([]client.EnvironmentProfile(nil), m.profiles...)
	for i, p := range profiles {
		if session := m.sessions[p.ID]; session != nil && session.connected {
			if title := strings.TrimSpace(session.settings.Title); title != "" {
				profiles[i].Name = title
			}
		}
	}
	return environmentDisplayNames(profiles)
}

func (s *Session) envName() string {
	if s.manager != nil {
		if name := s.manager.environmentNames()[s.profile.ID]; name != "" {
			return name
		}
	}
	profile := s.profile
	if s.connected {
		if title := strings.TrimSpace(s.settings.Title); title != "" {
			profile.Name = title
		}
	}
	return environmentBaseName(profile)
}
