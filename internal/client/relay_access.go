package client

import (
	"context"
	"net/http"

	"github.com/tk/t3go/internal/proto"
)

// RelayAccess returns this environment's registered relay route without
// exposing its tunnel credential.
func (c *Client) RelayAccess(ctx context.Context) (proto.RelayAccessStatus, error) {
	var out proto.RelayAccessStatus
	err := c.do(ctx, http.MethodGet, "/api/v1/access/relay", nil, &out)
	return out, err
}

// ConfigureRelay registers this environment with a trusted relay hub. The
// administrator token is sent only to the local environment handler.
func (c *Client) ConfigureRelay(ctx context.Context, in proto.RelayAccessConfig) (proto.RelayAccessStatus, error) {
	var out proto.RelayAccessStatus
	err := c.do(ctx, http.MethodPost, "/api/v1/access/relay", in, &out)
	return out, err
}

// RegisterRelayUser creates a Relay account and returns its bearer token.
func (c *Client) RegisterRelayUser(ctx context.Context, in proto.RelayAccountRequest) (proto.RelayUserSession, error) {
	var out proto.RelayUserSession
	err := c.do(ctx, http.MethodPost, "/api/v1/access/relay/register", in, &out)
	return out, err
}

// LoginRelayUser signs in to an existing Relay account and returns a fresh token.
func (c *Client) LoginRelayUser(ctx context.Context, in proto.RelayAccountRequest) (proto.RelayUserSession, error) {
	var out proto.RelayUserSession
	err := c.do(ctx, http.MethodPost, "/api/v1/access/relay/login", in, &out)
	return out, err
}
