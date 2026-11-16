package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/tk/t3go/internal/proto"
)

// RelayEnvironment is an environment owned by a signed-in Relay user.
type RelayEnvironment struct {
	EnvironmentID string   `json:"environmentId"`
	Name          string   `json:"name"`
	PublicKey     string   `json:"publicKey"`
	DirectURLs    []string `json:"directUrls,omitempty"`
	Online        bool     `json:"online"`
}

func relayAccountClient(server string, allowHTTP bool) (*Client, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return nil, errors.New("enter the Relay server URL")
	}
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	u, err := url.Parse(server)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("enter a valid Relay server URL")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return nil, errors.New("Relay requires HTTPS; allow HTTP only on a trusted network")
	}
	return New(u.String(), ""), nil
}

// RegisterRelayAccount creates an account directly with a Relay server.
func RegisterRelayAccount(ctx context.Context, server string, allowHTTP bool,
	in proto.RelayAccountRequest) (proto.RelayUserSession, error) {
	var out proto.RelayUserSession
	c, err := relayAccountClient(server, allowHTTP)
	if err != nil {
		return out, err
	}
	err = c.do(ctx, http.MethodPost, "/api/v1/auth/register", in, &out)
	return out, err
}

// LoginRelayAccount signs in directly to a Relay server.
func LoginRelayAccount(ctx context.Context, server string, allowHTTP bool,
	in proto.RelayAccountRequest) (proto.RelayUserSession, error) {
	var out proto.RelayUserSession
	c, err := relayAccountClient(server, allowHTTP)
	if err != nil {
		return out, err
	}
	err = c.do(ctx, http.MethodPost, "/api/v1/auth/login", in, &out)
	return out, err
}

// RelayAccountEnvironments lists the environments owned by a user session.
func RelayAccountEnvironments(ctx context.Context, server, token string,
	allowHTTP bool) ([]RelayEnvironment, error) {
	c, err := relayAccountClient(server, allowHTTP)
	if err != nil {
		return nil, err
	}
	c.Token = strings.TrimSpace(token)
	var out struct {
		Environments []RelayEnvironment `json:"environments"`
	}
	err = c.do(ctx, http.MethodGet, "/api/v1/relay/environments", nil, &out)
	return out.Environments, err
}

// LogoutRelayAccount revokes the current account session.
func LogoutRelayAccount(ctx context.Context, server, token string, allowHTTP bool) error {
	c, err := relayAccountClient(server, allowHTTP)
	if err != nil {
		return err
	}
	c.Token = strings.TrimSpace(token)
	return c.do(ctx, http.MethodPost, "/api/v1/auth/logout", nil, nil)
}

// RelayAccountProfile turns a discovered environment into a relay-only
// profile authenticated by the account session.
func RelayAccountProfile(server string, token string, environment RelayEnvironment) EnvironmentProfile {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	return EnvironmentProfile{
		ID: environment.EnvironmentID, Name: environment.Name, PublicKey: environment.PublicKey,
		Token: strings.TrimSpace(token), AccountServer: server, Preference: "relay-only",
		Routes: []Route{{URL: server + "/e/" + url.PathEscape(environment.EnvironmentID), Kind: "relay"}},
	}
}
