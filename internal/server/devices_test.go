package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tk/t3go/internal/access"
	"github.com/tk/t3go/internal/proto"
)

// TestDeviceGrantsPairAuthenticateAndRevoke walks the per-device grant
// flow: the environment token mints a grant, the grant authenticates like
// the environment token, and revoking it stops only that device.
func TestDeviceGrantsPairAuthenticateAndRevoke(t *testing.T) {
	env := newTestEnv(t)
	var pairing proto.DevicePairing
	code := env.request(t, http.MethodPost, "/api/v1/access/devices", map[string]string{"label": "Pixel 8"}, env.token, &pairing)
	if code != http.StatusCreated {
		t.Fatalf("pair device = %d", code)
	}
	if !access.IsDevice(pairing.Token) || pairing.Device.ID == "" || pairing.Device.Label != "Pixel 8" {
		t.Fatalf("pairing = %+v", pairing)
	}

	// The grant authenticates the same API, on the bearer header.
	if code := env.request(t, http.MethodGet, "/api/v1/state", nil, pairing.Token, nil); code != http.StatusOK {
		t.Fatalf("device token state = %d", code)
	}
	// The pairing exchange hands back the token the device holds, so a
	// revoked grant stops working there too.
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token": {pairing.Token}}
	resp, err := http.PostForm(env.http.URL+"/oauth/token", form)
	if err != nil {
		t.Fatal(err)
	}
	var exchanged struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&exchanged)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || exchanged.AccessToken != pairing.Token {
		t.Fatalf("token exchange with a paired device = %d, %+v, %v", resp.StatusCode, exchanged, err)
	}

	// The asking device sees itself, and its visit.
	var listed struct {
		Devices []proto.DeviceGrant `json:"devices"`
	}
	if code := env.request(t, http.MethodGet, "/api/v1/access/devices", nil, pairing.Token, &listed); code != http.StatusOK {
		t.Fatalf("list devices = %d", code)
	}
	if len(listed.Devices) != 1 {
		t.Fatalf("devices = %+v", listed.Devices)
	}
	device := listed.Devices[0]
	if !device.Current || device.LastSeenAt == nil || device.RevokedAt != nil {
		t.Fatalf("asking device = %+v", device)
	}
	// The environment token's own requests belong to no device.
	listed.Devices = nil
	if code := env.request(t, http.MethodGet, "/api/v1/access/devices", nil, env.token, &listed); code != http.StatusOK {
		t.Fatalf("list devices = %d", code)
	}
	if len(listed.Devices) != 1 || listed.Devices[0].Current {
		t.Fatalf("environment token still marks a device current: %+v", listed.Devices)
	}

	if code := env.request(t, http.MethodDelete, "/api/v1/access/devices/"+device.ID, nil, env.token, nil); code != http.StatusOK {
		t.Fatalf("revoke device = %d", code)
	}
	if code := env.request(t, http.MethodGet, "/api/v1/state", nil, pairing.Token, nil); code != http.StatusUnauthorized {
		t.Fatalf("revoked device token state = %d, want 401", code)
	}
	if code := env.request(t, http.MethodGet, "/api/v1/state", nil, env.token, nil); code != http.StatusOK {
		t.Fatalf("revoking a device stopped the environment token: %d", code)
	}
	if code := env.request(t, http.MethodGet, "/api/v1/access/devices", nil, env.token, &listed); code != http.StatusOK || listed.Devices[0].RevokedAt == nil {
		t.Fatalf("revoked grant = %d, %+v", code, listed.Devices)
	}
}

// TestDeviceGrantsRejectBadRequests covers the shapes a device list should
// not accept.
func TestDeviceGrantsRejectBadRequests(t *testing.T) {
	env := newTestEnv(t)
	if code := env.request(t, http.MethodPost, "/api/v1/access/devices", map[string]string{"label": "One"}, "not-a-token", nil); code != http.StatusUnauthorized {
		t.Fatalf("pairing with a wrong token = %d", code)
	}
	if code := env.request(t, http.MethodDelete, "/api/v1/access/devices/dev_missing", nil, env.token, nil); code != http.StatusNotFound {
		t.Fatalf("revoking an unknown device = %d", code)
	}
	var pairing proto.DevicePairing
	if code := env.request(t, http.MethodPost, "/api/v1/access/devices", map[string]any{}, env.token, &pairing); code != http.StatusCreated {
		t.Fatalf("pairing without a label = %d", code)
	}
	if strings.TrimSpace(pairing.Device.Label) == "" {
		t.Fatal("a device paired without a label")
	}
	// A quarter-written token never authenticates.
	if code := env.request(t, http.MethodGet, "/api/v1/state", nil, pairing.Token[:len(access.Prefix)+8], nil); code != http.StatusUnauthorized {
		t.Fatalf("a shortened device token state = %d", code)
	}
}

// TestRevokingADeviceClosesItsStream pins immediate revocation: a stream a
// device already holds stops when its grant is revoked.
func TestRevokingADeviceClosesItsStream(t *testing.T) {
	env := newTestEnv(t)
	var pairing proto.DevicePairing
	if code := env.request(t, http.MethodPost, "/api/v1/access/devices", map[string]string{"label": "Phone"}, env.token, &pairing); code != http.StatusCreated {
		t.Fatalf("pair device = %d", code)
	}
	req, err := http.NewRequest(http.MethodGet, env.http.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+pairing.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device stream = %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("stream did not start: %v", err)
	}

	if code := env.request(t, http.MethodDelete, "/api/v1/access/devices/"+pairing.Device.ID, nil, env.token, nil); code != http.StatusOK {
		t.Fatalf("revoke device = %d", code)
	}
	closed := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, reader)
		closed <- err
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a revoked device kept its stream open")
	}
}

// TestRevokedDeviceTicketCannotOpenASocket pins that a socket ticket minted
// before revocation cannot open a stream after it.
func TestRevokedDeviceTicketCannotOpenASocket(t *testing.T) {
	env := newTestEnv(t)
	var pairing proto.DevicePairing
	if code := env.request(t, http.MethodPost, "/api/v1/access/devices", map[string]string{"label": "Phone"}, env.token, &pairing); code != http.StatusCreated {
		t.Fatalf("pair device = %d", code)
	}
	dial := func(ticket string) (*websocket.Conn, *http.Response, error) {
		url := "ws" + strings.TrimPrefix(env.http.URL, "http") + "/ws?wsTicket=" + url.QueryEscape(ticket)
		return websocket.Dial(context.Background(), url, nil)
	}
	var held struct {
		Ticket string `json:"ticket"`
	}
	if code := env.request(t, http.MethodPost, "/api/auth/websocket-ticket", map[string]any{}, pairing.Token, &held); code != http.StatusOK || held.Ticket == "" {
		t.Fatalf("ticket = %d, %+v", code, held)
	}
	conn, _, err := dial(held.Ticket)
	if err != nil {
		t.Fatalf("a paired device cannot open its socket: %v", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
	// The ticket is one-time already; mint another for after revocation.
	if code := env.request(t, http.MethodPost, "/api/auth/websocket-ticket", map[string]any{}, pairing.Token, &held); code != http.StatusOK {
		t.Fatalf("ticket = %d", code)
	}
	if code := env.request(t, http.MethodDelete, "/api/v1/access/devices/"+pairing.Device.ID, nil, env.token, nil); code != http.StatusOK {
		t.Fatalf("revoke device = %d", code)
	}
	conn, resp, err := dial(held.Ticket)
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("a revoked device opened a socket with a ticket minted before revocation")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked ticket = %v, %v", resp, err)
	}
}
