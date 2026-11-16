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
	// lan records whether the server listens for other devices on this
	// network, and lanIP is the address a phone can use to reach it.
	lan   bool
	lanIP string
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

type localServerHealth struct {
	OK         bool   `json:"ok"`
	Name       string `json:"name"`
	Listen     string `json:"listen"`
	Background *bool  `json:"background"`
}

func fetchLocalServerHealth(baseURL string) (localServerHealth, error) {
	var h localServerHealth
	resp, err := (&http.Client{Timeout: 800 * time.Millisecond}).
		Get(strings.TrimRight(baseURL, "/") + "/api/v1/health")
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return h, fmt.Errorf("health: %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&h); err != nil {
		return h, err
	}
	if !h.OK || h.Name != "t3go" {
		return h, errors.New("not a T3Go environment server")
	}
	return h, nil
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
	_, err := fetchLocalServerHealth(baseURL)
	return err == nil
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

func serverExposesLAN(baseURL string) (lan, known bool) {
	h, err := fetchLocalServerHealth(baseURL)
	if err != nil || strings.TrimSpace(h.Listen) == "" {
		return false, false
	}
	return !isLoopbackURL(h.Listen), true
}

func stopLocalServer(baseURL, token string) error {
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/api/v1/local/shutdown", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("local server stop: %s", resp.Status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !serverAnswers(baseURL) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the local environment server did not stop")
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
