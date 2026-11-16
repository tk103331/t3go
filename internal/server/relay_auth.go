package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/relay"
)

func relayAccountProto(in relay.UserSession) proto.RelayUserSession {
	return proto.RelayUserSession{
		User: proto.RelayUser{
			ID: in.User.ID, Email: in.User.Email, Name: in.User.Name,
			Provider: in.User.Provider, CreatedAt: in.User.CreatedAt,
		},
		Token: in.Token, ExpiresAt: in.ExpiresAt,
	}
}

func (s *Server) handleRelayAccount(w http.ResponseWriter, r *http.Request, endpoint string) {
	var in proto.RelayAccountRequest
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	in.Server = strings.TrimRight(strings.TrimSpace(in.Server), "/")
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	base, err := relay.ValidateURL(in.Server)
	if err != nil {
		writeError(w, http.StatusBadRequest, "relay", err.Error())
		return
	}
	if base.Scheme != "https" && !in.AllowHTTP {
		writeError(w, http.StatusBadRequest, "relay", "HTTPS is required; explicitly allow HTTP only on a trusted network")
		return
	}
	raw, err := json.Marshal(relay.UserCredentials{Email: in.Email, Password: in.Password, Name: in.Name})
	if err != nil {
		writeError(w, http.StatusBadRequest, "relay", "invalid account request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base.String(), "/")+"/api/v1/auth/"+endpoint, bytes.NewReader(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "relay", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "relay", "Relay account connection failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(readErrorBody(resp.Body))
		if message == "" {
			message = fmt.Sprintf("Relay rejected account request (%d)", resp.StatusCode)
		}
		writeError(w, statusForRelayAccount(resp.StatusCode), "relay", message)
		return
	}
	var session relay.UserSession
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&session); err != nil ||
		len(session.Token) < 20 || session.User.ID == "" {
		writeError(w, http.StatusBadGateway, "relay", "Relay returned an invalid account session")
		return
	}
	writeJSON(w, http.StatusOK, relayAccountProto(session))
}

func (s *Server) handleRegisterRelayUser(w http.ResponseWriter, r *http.Request) {
	s.handleRelayAccount(w, r, "register")
}

func (s *Server) handleLoginRelayUser(w http.ResponseWriter, r *http.Request) {
	s.handleRelayAccount(w, r, "login")
}

func statusForRelayAccount(status int) int {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusConflict, http.StatusTooManyRequests:
		return status
	default:
		return http.StatusBadGateway
	}
}
