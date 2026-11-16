package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// localServer is the environment server the desktop package ships and
// starts for itself, so a fresh install works without a separately
// installed server.
type localServer struct {
	baseURL string
	token   string
	home    string
	// stop ends the server this client started. It is nil when an already
	// running server was reused.
	stop func()
	// background records whether the server was started to outlive the
	// client, which decides whether a setting change restarts it.
	background bool
}

// errNoBundledServer means the installed package carries no server binary:
// a web or phone client, or a desktop build made without one.
var errNoBundledServer = errors.New("no bundled environment server")

// localDescriptor is the identity an environment server describes.
type localDescriptor struct {
	ID        string `json:"environmentId"`
	PublicKey string `json:"publicKey"`
	Name      string `json:"name"`
}

// fetchLocalDescriptor reads the identity the environment server publishes.
func fetchLocalDescriptor(baseURL string) (localDescriptor, error) {
	var d localDescriptor
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/environment", nil)
	if err != nil {
		return d, err
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return d, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return d, fmt.Errorf("environment descriptor: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return d, err
	}
	if strings.TrimSpace(d.ID) == "" {
		return d, errors.New("environment descriptor has no identity")
	}
	return d, nil
}

// serverAnswers tells whether a T3Go environment server listens at
// baseURL.
func serverAnswers(baseURL string) bool {
	resp, err := (&http.Client{Timeout: 800 * time.Millisecond}).
		Get(strings.TrimRight(baseURL, "/") + "/api/v1/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil {
		return false
	}
	return body.OK && body.Name == "t3go"
}

// serverAcceptsToken tells whether the token opens the server's API, so a
// server keeping another data directory is never mistaken for this one's.
func serverAcceptsToken(baseURL, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/auth/session", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// isLoopbackURL tells whether an address names this machine only.
func isLoopbackURL(raw string) bool {
	address := strings.TrimSpace(raw)
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "localhost", "::1":
		return true
	}
	return strings.HasPrefix(host, "127.")
}
