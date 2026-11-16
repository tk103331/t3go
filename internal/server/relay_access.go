package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/relay"
)

type relayCredentials struct {
	Server        string `json:"server"`
	EnvironmentID string `json:"environmentId"`
	ConnectToken  string `json:"connectToken"`
	DirectURL     string `json:"directUrl,omitempty"`
}

func loadRelayCredentials(home string) (relayCredentials, error) {
	var credentials relayCredentials
	raw, err := os.ReadFile(filepath.Join(home, "relay.json"))
	if err != nil {
		return credentials, err
	}
	err = json.Unmarshal(raw, &credentials)
	return credentials, err
}

func saveRelayCredentials(home string, credentials relayCredentials) error {
	raw, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(home, ".relay-*.json")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if err := f.Chmod(0o600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(home, "relay.json"))
}

func relayRoute(server, environmentID string) string {
	return strings.TrimRight(server, "/") + "/e/" + environmentID
}

func (s *Server) relayAccessStatus(credentials relayCredentials) proto.RelayAccessStatus {
	s.relayConnMu.Lock()
	state, lastErr := s.relayState, s.relayError
	s.relayConnMu.Unlock()
	if credentials.Server == "" {
		return proto.RelayAccessStatus{Error: lastErr}
	}
	if state == "" {
		state = "configured"
	}
	return proto.RelayAccessStatus{
		Configured: true,
		Server:     credentials.Server,
		Route:      relayRoute(credentials.Server, credentials.EnvironmentID),
		DirectURL:  credentials.DirectURL,
		State:      state,
		Error:      lastErr,
	}
}

func (s *Server) handleRelayAccess(w http.ResponseWriter, r *http.Request) {
	credentials, err := loadRelayCredentials(s.Config.Home)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, s.relayAccessStatus(relayCredentials{}))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "relay", "could not read relay configuration")
		return
	}
	if credentials.EnvironmentID != s.Identity.ID || credentials.ConnectToken == "" {
		writeError(w, http.StatusConflict, "relay", "saved relay configuration belongs to another environment")
		return
	}
	writeJSON(w, http.StatusOK, s.relayAccessStatus(credentials))
}

func (s *Server) handleConfigureRelayAccess(w http.ResponseWriter, r *http.Request) {
	var in proto.RelayAccessConfig
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	in.Server = strings.TrimRight(strings.TrimSpace(in.Server), "/")
	in.UserToken = strings.TrimSpace(in.UserToken)
	in.AdminToken = strings.TrimSpace(in.AdminToken)
	in.DirectURL = strings.TrimRight(strings.TrimSpace(in.DirectURL), "/")
	base, err := relay.ValidateURL(in.Server)
	if err != nil {
		writeError(w, http.StatusBadRequest, "relay", err.Error())
		return
	}
	if base.Scheme != "https" && !in.AllowHTTP {
		writeError(w, http.StatusBadRequest, "relay", "HTTPS is required; explicitly allow HTTP only on a trusted network")
		return
	}
	authorization := in.UserToken
	if authorization == "" {
		authorization = in.AdminToken
	}
	if len(authorization) < 20 {
		writeError(w, http.StatusBadRequest, "relay", "a valid Relay user token is required")
		return
	}
	if in.DirectURL != "" {
		if _, err := relay.ValidateURL(in.DirectURL); err != nil {
			writeError(w, http.StatusBadRequest, "relay", "invalid direct URL: "+err.Error())
			return
		}
	}
	if _, err := loadRelayCredentials(s.Config.Home); err == nil {
		writeError(w, http.StatusConflict, "relay", "Relay is already configured for this environment")
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, "relay", "could not read relay configuration")
		return
	}

	registration := relay.Registration{
		ID:        s.Identity.ID,
		PublicKey: s.Identity.PublicKey(),
		Name:      strings.TrimSpace(s.Service.Settings(r.Context()).Title),
		Token:     s.Config.Token,
	}
	if registration.Name == "" {
		registration.Name = s.Hostname
	}
	if in.DirectURL != "" {
		registration.DirectURLs = []string{in.DirectURL}
	}
	if err := registration.Sign(s.Identity.PrivateKey); err != nil {
		writeError(w, http.StatusInternalServerError, "relay", "could not sign environment registration")
		return
	}
	raw, err := json.Marshal(registration)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "relay", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base.String(), "/")+"/api/v1/relay/environments", bytes.NewReader(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "relay", err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+authorization)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "relay", "relay registration connection failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		message := strings.TrimSpace(readErrorBody(resp.Body))
		if message == "" {
			message = fmt.Sprintf("relay rejected registration (%d)", resp.StatusCode)
		}
		writeError(w, statusForRelayRegistration(resp.StatusCode), "relay", message)
		return
	}
	var registered relay.Registered
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&registered); err != nil ||
		len(registered.ConnectToken) < 20 {
		writeError(w, http.StatusBadGateway, "relay", "relay returned an invalid registration response")
		return
	}
	credentials := relayCredentials{
		Server:        base.String(),
		EnvironmentID: s.Identity.ID,
		ConnectToken:  registered.ConnectToken,
		DirectURL:     in.DirectURL,
	}
	if err := saveRelayCredentials(s.Config.Home, credentials); err != nil {
		writeError(w, http.StatusInternalServerError, "relay", "could not save relay configuration")
		return
	}
	_ = s.StartRelayConnector(s.lifecycle, s.Addr())
	writeJSON(w, http.StatusOK, s.relayAccessStatus(credentials))
}

func readErrorBody(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 16<<10))
	var response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &response) == nil && response.Error.Message != "" {
		return response.Error.Message
	}
	return strings.TrimSpace(string(raw))
}

func statusForRelayRegistration(status int) int {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return http.StatusUnauthorized
	case http.StatusConflict:
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}

// StartRelayConnector starts or replaces this environment's active relay
// tunnel from its local credentials. The connector only targets the fixed
// local HTTP listener and never accepts a target from the hub.
func (s *Server) StartRelayConnector(ctx context.Context, listen string) error {
	credentials, err := loadRelayCredentials(s.Config.Home)
	if err != nil {
		return err
	}
	if credentials.EnvironmentID != s.Identity.ID ||
		credentials.EnvironmentID == "" || len(credentials.ConnectToken) < 20 {
		return errors.New("saved relay credentials do not match this environment")
	}
	if _, err := relay.ValidateURL(credentials.Server); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	target := net.JoinHostPort(host, port)
	if ctx == nil {
		ctx = context.Background()
	}
	connectorCtx, cancel := context.WithCancel(ctx)
	s.relayConnMu.Lock()
	if s.relayConnCancel != nil {
		s.relayConnCancel()
	}
	s.relayConnGen++
	generation := s.relayConnGen
	s.relayConnCancel = cancel
	s.relayState = "connecting"
	s.relayError = ""
	s.relayConnMu.Unlock()
	connector := relay.Connector{
		Server:        credentials.Server,
		EnvironmentID: credentials.EnvironmentID,
		Token:         credentials.ConnectToken,
		Target:        target,
		OnConnected: func() {
			s.relayConnMu.Lock()
			defer s.relayConnMu.Unlock()
			if s.relayConnGen != generation {
				return
			}
			s.relayState = "active"
			s.relayError = ""
		},
		OnError: func(err error) {
			s.relayConnMu.Lock()
			defer s.relayConnMu.Unlock()
			if s.relayConnGen != generation {
				return
			}
			s.relayState = "retrying"
			if err != nil {
				s.relayError = err.Error()
			}
		},
	}
	go func() {
		err := connector.Run(connectorCtx)
		s.relayConnMu.Lock()
		defer s.relayConnMu.Unlock()
		if s.relayConnGen == generation {
			s.relayConnCancel = nil
			if connectorCtx.Err() == nil && err != nil {
				s.relayState = "error"
				s.relayError = err.Error()
			}
		}
	}()
	return nil
}

// StartSavedRelayConnector restores a Relay tunnel after the environment
// restarts. Missing configuration is normal and does not prevent startup.
func (s *Server) StartSavedRelayConnector(ctx context.Context, listen string) (bool, error) {
	if _, err := loadRelayCredentials(s.Config.Home); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := s.StartRelayConnector(ctx, listen); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) stopRelayConnector() {
	s.relayConnMu.Lock()
	cancel := s.relayConnCancel
	s.relayConnCancel = nil
	s.relayConnMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
