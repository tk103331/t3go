package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/tk/t3go/internal/environment"
	"github.com/tk/t3go/internal/proto"
)

type Route struct {
	URL  string `json:"url"`
	Kind string `json:"kind"` // direct or relay
}

// EnvironmentProfile is local connection configuration, not execution state.
// Token retains the existing shared-token pairing contract for compatibility.
type EnvironmentProfile struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Hostname   string            `json:"hostname,omitempty"` // cached execution host for offline labels
	System     *proto.SystemInfo `json:"system,omitempty"`
	Alias      string            `json:"alias,omitempty"`
	PublicKey  string            `json:"publicKey,omitempty"`
	Token      string            `json:"token"`
	Routes     []Route           `json:"routes"`
	Preference string            `json:"preference,omitempty"` // auto, direct-only, relay-only
	Legacy     bool              `json:"legacy,omitempty"`
	// AccountServer marks a profile whose Token is a Relay user token
	// discovered through account sign-in rather than an environment token.
	AccountServer string `json:"accountServer,omitempty"`
}

// PairingProfile accepts existing links and identity-pinned multi-route links.
func PairingProfile(address, token string) (EnvironmentProfile, error) {
	p := EnvironmentProfile{Token: strings.TrimSpace(token)}
	address = strings.TrimSpace(address)
	raw := address
	if strings.HasPrefix(strings.TrimSpace(token), "t3go://") {
		raw = strings.TrimSpace(token)
		p.Token = ""
	}
	if strings.HasPrefix(raw, "t3go://") {
		u, err := url.Parse(raw)
		if err != nil {
			return p, err
		}
		q := u.Query()
		address = q.Get("url")
		if p.Token == "" {
			p.Token = q.Get("token")
		}
		p.ID, p.PublicKey = q.Get("environment"), q.Get("key")
		if direct := q.Get("direct"); direct != "" {
			if p.ID == "" || p.PublicKey == "" {
				return p, errors.New("multiple routes require a paired environment identity")
			}
			p.Routes = append(p.Routes, Route{URL: direct, Kind: "direct"})
		}
	}
	if address == "" {
		return p, errors.New("enter the server address")
	}
	c := New(address, p.Token)
	kind := "direct"
	if u, err := url.Parse(c.BaseURL); err == nil && strings.Contains(u.Path, "/e/") {
		kind = "relay"
	}
	p.Routes = append(p.Routes, Route{URL: c.BaseURL, Kind: kind})
	for i, route := range p.Routes {
		u, err := url.Parse(strings.TrimRight(route.URL, "/"))
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
			(u.Scheme != "http" && u.Scheme != "https") {
			return p, errors.New("invalid environment route")
		}
		p.Routes[i].URL = u.String()
	}
	return p, nil
}

// PairingLink encodes a saved environment profile as the portable link a
// phone can scan. A relay route is preferred for cross-network access, while
// one direct route is carried alongside it for LAN fallback.
func PairingLink(p EnvironmentProfile) (string, error) {
	return PairingLinkOptions(p, true, true)
}

// PairingLinkOptions shares only the selected route kinds. When both are
// enabled, relay remains primary and direct is carried as a LAN fallback.
func PairingLinkOptions(p EnvironmentProfile, includeDirect, includeRelay bool) (string, error) {
	p.Token = strings.TrimSpace(p.Token)
	if p.Token == "" {
		return "", errors.New("environment token is missing")
	}
	if len(p.Routes) == 0 {
		return "", errors.New("environment has no route")
	}
	// Prefer a direct route another device can reach: the loopback address
	// of the machine holding the environment only works there.
	var relay, direct, loopback string
	for _, route := range p.Routes {
		raw := strings.TrimRight(strings.TrimSpace(route.URL), "/")
		if raw == "" {
			continue
		}
		if route.Kind == "relay" && includeRelay && relay == "" {
			relay = raw
		}
		if route.Kind != "relay" && includeDirect {
			if isLoopbackRoute(raw) {
				if loopback == "" {
					loopback = raw
				}
			} else if direct == "" {
				direct = raw
			}
		}
	}
	if direct == "" {
		direct = loopback
	}
	primary := relay
	if primary == "" {
		primary = direct
	}
	if primary == "" {
		return "", errors.New("no selected environment route is available")
	}
	q := url.Values{"url": {primary}, "token": {p.Token}}
	if direct != "" && direct != primary {
		q.Set("direct", direct)
	}
	if p.ID != "" && p.PublicKey != "" {
		q.Set("environment", p.ID)
		q.Set("key", p.PublicKey)
	}
	return "t3go://pair?" + q.Encode(), nil
}

// isLoopbackRoute tells whether an address names only the machine that
// holds the environment.
func isLoopbackRoute(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func LegacyEnvironmentID(address string) string {
	sum := sha256.Sum256([]byte(address))
	return "legacy-" + base64.RawURLEncoding.EncodeToString(sum[:12])
}

// ErrIdentityMismatch reports an address that answered, but not as the
// environment the client paired with: another machine holds it, or the
// environment has been reset since.
var ErrIdentityMismatch = errors.New("environment identity mismatch")

// ProbeError is an address that rejected the identity probe with a status,
// such as a relay answering for an environment that is offline.
type ProbeError struct{ Status int }

func (e *ProbeError) Error() string {
	return fmt.Sprintf("environment identity probe rejected (%d)", e.Status)
}

// Probe verifies a fresh signed descriptor without exposing the environment token.
func (c *Client) Probe(ctx context.Context, id, key string) (environment.Descriptor, error) {
	var d environment.Descriptor
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return d, err
	}
	challenge := base64.RawURLEncoding.EncodeToString(nonce)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/environment?challenge="+challenge, nil)
	if err != nil {
		return d, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return d, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return d, &ProbeError{Status: resp.StatusCode}
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 64<<10)).Decode(&d); err != nil {
		return d, err
	}
	if err := environment.Verify(d, challenge, id, key); err != nil {
		return d, fmt.Errorf("%w: %v", ErrIdentityMismatch, err)
	}
	return d, nil
}

// ConnectEnvironment probes routes before sending credentials. Writes are never
// raced or retried here. An identity rejection is not a transient network failure.
func ConnectEnvironment(ctx context.Context, p EnvironmentProfile) (*Client, proto.State, error) {
	routes := append([]Route(nil), p.Routes...)
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].Kind == "direct" && routes[j].Kind != "direct" })
	var last error = errors.New("no permitted environment route")
	for _, route := range routes {
		if (p.Preference == "direct-only" && route.Kind != "direct") ||
			(p.Preference == "relay-only" && route.Kind != "relay") {
			continue
		}
		c := New(route.URL, p.Token)
		timeout := 8 * time.Second
		if route.Kind == "direct" && len(routes) > 1 {
			timeout = 1500 * time.Millisecond
		}
		routeCtx, cancel := context.WithTimeout(ctx, timeout)
		if p.PublicKey != "" {
			_, err := c.Probe(routeCtx, p.ID, p.PublicKey)
			if err != nil {
				cancel()
				last = err
				if ctx.Err() != nil {
					break
				}
				continue // quarantine this address: it receives no credentials
			}
		} else if len(routes) > 1 {
			cancel()
			return nil, proto.State{}, errors.New("unpaired routes cannot be selected automatically")
		}
		st, err := c.State(routeCtx)
		cancel()
		if err != nil {
			if IsAuthorizationError(err) {
				return nil, st, err
			}
			last = err
			continue
		}
		if p.PublicKey != "" && (st.EnvironmentID != p.ID || st.IdentityKey != p.PublicKey) {
			last = fmt.Errorf("%w: the state answered for another environment", ErrIdentityMismatch)
			continue
		}
		return c, st, nil
	}
	return nil, proto.State{}, last
}
