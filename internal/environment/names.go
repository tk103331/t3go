package environment

import (
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// UsableName rejects local-only placeholders as machine display names. It also
// handles old profiles that stored a URL or a host with a port as their name.
func UsableName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if u, err := url.Parse(name); err == nil && u.Host != "" {
		name = u.Hostname()
	} else if host, _, err := net.SplitHostPort(name); err == nil {
		name = host
	}
	name = strings.TrimRight(strings.ToLower(strings.Trim(name, "[]")), ".")
	switch name {
	case "", "localhost", "localhost.localdomain", "localhost6", "localhost6.localdomain6", "ip6-localhost", "(none)":
		return false
	}
	if strings.HasSuffix(name, ".localhost") {
		return false
	}
	if ip, err := netip.ParseAddr(name); err == nil {
		ip = ip.Unmap()
		return !ip.IsLoopback() && !ip.IsUnspecified()
	}
	return true
}
