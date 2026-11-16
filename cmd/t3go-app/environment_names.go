package main

import (
	"net/url"
	"strings"

	"github.com/tk/t3go/internal/client"
)

// environmentHost keeps a display name independent of the active route:
// a relay fallback should not rename the machine the user works on.
func environmentHost(p client.EnvironmentProfile) string {
	for _, relay := range []bool{false, true} {
		for _, route := range p.Routes {
			if (route.Kind == "relay") != relay {
				continue
			}
			if u, err := url.Parse(route.URL); err == nil && u.Host != "" {
				return u.Host
			}
		}
	}
	return ""
}

func environmentBaseName(p client.EnvironmentProfile) string {
	if alias := strings.TrimSpace(p.Alias); alias != "" {
		return alias
	}
	hostname := strings.TrimSpace(p.Hostname)
	if name := strings.TrimSpace(p.Name); name != "" && !strings.EqualFold(name, "T3Go") {
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
	if host := environmentHost(p); host != "" {
		return host
	}
	return "Environment"
}
