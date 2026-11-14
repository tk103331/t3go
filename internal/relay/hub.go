// Package relay forwards existing HTTP and WebSocket protocols through outbound
// environment tunnels. A hub owns no project files or execution state.
package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// Binding contains public discovery data and hashes of environment-scoped secrets.
type Binding struct {
	ID          string   `json:"environmentId"`
	Name        string   `json:"name"`
	PublicKey   string   `json:"publicKey"`
	DirectURLs  []string `json:"directUrls,omitempty"`
	ConnectHash string   `json:"connectHash"`
	AccessHash  string   `json:"accessHash"`
}

type Registration struct {
	ID         string   `json:"environmentId"`
	Name       string   `json:"name"`
	PublicKey  string   `json:"publicKey"`
	Token      string   `json:"token"`
	DirectURLs []string `json:"directUrls,omitempty"`
}

type Registered struct {
	ConnectToken string `json:"connectToken"`
}

type peer struct {
	session   *yamux.Session
	transport *http.Transport
}

type Hub struct {
	mu       sync.Mutex
	path     string
	admin    string
	bindings map[string]Binding
	peers    map[string]*peer
	closed   bool
}

func New(home, admin string) (*Hub, error) {
	if len(admin) < 20 {
		return nil, errors.New("relay requires an authenticated administrator")
	}
	h := &Hub{path: filepath.Join(home, "relay-bindings.json"), admin: digest(admin),
		bindings: map[string]Binding{}, peers: map[string]*peer{}}
	raw, err := os.ReadFile(h.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &h.bindings); err != nil || h.bindings == nil {
			return nil, errors.New("invalid relay bindings file")
		}
	}
	return h, nil
}

func (h *Hub) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/relay/environments", h.register)
	mux.HandleFunc("GET /api/v1/relay/environments", h.list)
	mux.HandleFunc("DELETE /api/v1/relay/environments/{id}", h.remove)
	mux.HandleFunc("GET /relay/connect/{id}", h.connect)
	mux.HandleFunc("/e/{id}/{rest...}", h.proxy)
}

func digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func matches(hash, secret string) bool {
	return secret != "" && subtle.ConstantTimeCompare([]byte(hash), []byte(digest(secret))) == 1
}

func bearer(r *http.Request) string {
	if value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(value)
	}
	return r.URL.Query().Get("token") // compatibility with existing embedded clients
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	reply(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (h *Hub) register(w http.ResponseWriter, r *http.Request) {
	if !matches(h.admin, bearer(r)) {
		fail(w, 401, "unauthorized", "relay administrator token required")
		return
	}
	var in Registration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		fail(w, 400, "registration", "invalid registration")
		return
	}
	key, err := base64.RawURLEncoding.DecodeString(in.PublicKey)
	if err != nil || len(key) != 32 || in.ID != in.PublicKey || len(in.Token) < 20 || len(in.Name) > 200 || len(in.DirectURLs) > 16 {
		fail(w, 400, "registration", "valid environment identity and token required")
		return
	}
	for _, address := range in.DirectURLs {
		if _, err := ValidateURL(address); err != nil {
			fail(w, 400, "registration", "invalid direct URL")
			return
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		fail(w, 500, "random", "could not generate connection secret")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	binding := Binding{ID: in.ID, Name: in.Name, PublicKey: in.PublicKey,
		DirectURLs: in.DirectURLs, ConnectHash: digest(token), AccessHash: digest(in.Token)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		fail(w, 503, "closed", "relay is shutting down")
		return
	}
	if _, exists := h.bindings[in.ID]; exists {
		fail(w, 409, "exists", "environment already registered; explicitly revoke before registering again")
		return
	}
	h.bindings[in.ID] = binding
	if err := h.save(); err != nil {
		delete(h.bindings, in.ID)
		fail(w, 500, "store", "could not persist binding")
		return
	}
	reply(w, 201, Registered{ConnectToken: token})
}

func (h *Hub) list(w http.ResponseWriter, r *http.Request) {
	if !matches(h.admin, bearer(r)) {
		fail(w, 401, "unauthorized", "relay administrator token required")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]map[string]any, 0, len(h.bindings))
	for id, binding := range h.bindings {
		p := h.peers[id]
		out = append(out, map[string]any{"environmentId": id, "name": binding.Name,
			"publicKey": binding.PublicKey, "directUrls": binding.DirectURLs,
			"online": p != nil && !p.session.IsClosed()})
	}
	reply(w, 200, map[string]any{"environments": out})
}

func (h *Hub) remove(w http.ResponseWriter, r *http.Request) {
	if !matches(h.admin, bearer(r)) {
		fail(w, 401, "unauthorized", "relay administrator token required")
		return
	}
	h.mu.Lock()
	id := r.PathValue("id")
	binding, exists := h.bindings[id]
	if !exists {
		h.mu.Unlock()
		fail(w, 404, "environment", "unknown environment")
		return
	}
	delete(h.bindings, id)
	if err := h.save(); err != nil {
		h.bindings[id] = binding
		h.mu.Unlock()
		fail(w, 500, "store", "could not revoke binding")
		return
	}
	p := h.peers[id]
	delete(h.peers, id)
	h.mu.Unlock()
	if p != nil {
		p.transport.CloseIdleConnections()
		_ = p.session.Close()
	}
	reply(w, 200, map[string]bool{"revoked": true})
}

func (h *Hub) save() error {
	raw, err := json.MarshalIndent(h.bindings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(h.path), ".relay-bindings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), h.path)
}

func tunnelConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	cfg.StreamOpenTimeout = 10 * time.Second
	cfg.StreamCloseTimeout = 10 * time.Second
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.AcceptBacklog = 64
	return cfg
}

func (h *Hub) connect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.mu.Lock()
	binding, exists := h.bindings[id]
	if h.closed || !exists || !matches(binding.ConnectHash, bearer(r)) {
		h.mu.Unlock()
		fail(w, 401, "unauthorized", "valid environment connection token required")
		return
	}
	if current := h.peers[id]; current != nil && !current.session.IsClosed() {
		h.mu.Unlock()
		fail(w, 409, "connected", "environment already has an active tunnel")
		return
	}
	// Serialize upgrade with revocation and competing registrations.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.mu.Unlock()
		return
	}
	conn.SetReadLimit(1 << 20)
	session, err := yamux.Client(websocket.NetConn(r.Context(), conn, websocket.MessageBinary), tunnelConfig())
	if err != nil {
		h.mu.Unlock()
		conn.CloseNow()
		return
	}
	p := &peer{session: session}
	p.transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			type result struct {
				stream net.Conn
				err    error
			}
			ch := make(chan result)
			go func() {
				s, err := session.OpenStream()
				select {
				case ch <- result{s, err}:
				case <-ctx.Done():
					if s != nil {
						_ = s.Close()
					}
				}
			}()
			select {
			case out := <-ch:
				return out.stream, out.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		MaxConnsPerHost: 32, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	h.peers[id] = p
	h.mu.Unlock()
	defer conn.CloseNow()
	defer session.Close()
	defer p.transport.CloseIdleConnections()
	defer func() {
		h.mu.Lock()
		if h.peers[id] == p {
			delete(h.peers, id)
		}
		h.mu.Unlock()
	}()
	select {
	case <-session.CloseChan():
	case <-r.Context().Done():
	}
}

func (h *Hub) proxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	path := "/" + r.PathValue("rest")
	// Only the signed, nonce-bound descriptor is exposed without an API token.
	publicProbe := r.Method == http.MethodGet && path == "/api/v1/environment"
	h.mu.Lock()
	binding, exists := h.bindings[id]
	p := h.peers[id]
	authorized := exists && (publicProbe || matches(binding.AccessHash, bearer(r)))
	h.mu.Unlock()
	if !authorized {
		fail(w, 401, "unauthorized", "environment token required")
		return
	}
	if p == nil || p.session.IsClosed() {
		fail(w, 503, "environment_offline", "environment tunnel is offline")
		return
	}
	proxy := &httputil.ReverseProxy{
		Transport: p.transport, FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "environment"
			pr.Out.URL.Path = path
			pr.Out.URL.RawPath = ""
			pr.Out.Host = "localhost"
			pr.Out.Header.Del("Forwarded")
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Header.Del("X-Forwarded-Host")
			pr.Out.Header.Del("X-Forwarded-Proto")
		},
		ErrorLog: nil,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			fail(w, 502, "tunnel", "environment connection interrupted; operation outcome may be unknown")
		},
	}
	proxy.ServeHTTP(w, r)
}

func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	peers := h.peers
	h.peers = map[string]*peer{}
	h.mu.Unlock()
	for _, p := range peers {
		p.transport.CloseIdleConnections()
		_ = p.session.Close()
	}
}

// ValidateURL rejects credential-bearing, ambiguous and unsupported hub URLs.
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("expected an http(s) URL without credentials, query or fragment")
	}
	return u, nil
}
