package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Alias      string  `json:"alias,omitempty"`
	PublicKey  string  `json:"publicKey,omitempty"`
	Token      string  `json:"token"`
	Routes     []Route `json:"routes"`
	Preference string  `json:"preference,omitempty"` // auto, direct-only, relay-only
	Legacy     bool    `json:"legacy,omitempty"`
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

func LegacyEnvironmentID(address string) string {
	sum := sha256.Sum256([]byte(address))
	return "legacy-" + base64.RawURLEncoding.EncodeToString(sum[:12])
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
		return d, fmt.Errorf("environment identity probe rejected (%d)", resp.StatusCode)
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 64<<10)).Decode(&d); err != nil {
		return d, err
	}
	return d, environment.Verify(d, challenge, id, key)
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
			last = errors.New("environment state identity mismatch")
			continue
		}
		return c, st, nil
	}
	return nil, proto.State{}, last
}
