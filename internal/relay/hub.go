// Package relay forwards existing HTTP and WebSocket protocols through outbound
// environment tunnels. A hub owns no project files or execution state.
package relay

import (
	"context"
	"crypto/ed25519"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// Binding contains public discovery data and hashes of environment-scoped secrets.
type Binding struct {
	ID          string   `json:"environmentId"`
	OwnerID     string   `json:"ownerId,omitempty"`
	Name        string   `json:"name"`
	PublicKey   string   `json:"publicKey"`
	DirectURLs  []string `json:"directUrls,omitempty"`
	ConnectHash string   `json:"connectHash"`
	AccessHash  string   `json:"accessHash"`
	// RelayKey signs requests from the hub to the environment after a
	// signed-in owner is authorized. It never leaves the hub and host.
	RelayKey string `json:"relayKey,omitempty"`
}

type Registration struct {
	ID         string    `json:"environmentId"`
	Name       string    `json:"name"`
	PublicKey  string    `json:"publicKey"`
	Token      string    `json:"token"`
	DirectURLs []string  `json:"directUrls,omitempty"`
	Nonce      string    `json:"nonce,omitempty"`
	IssuedAt   time.Time `json:"issuedAt,omitempty"`
	Signature  string    `json:"signature,omitempty"`
}

type Registered struct {
	ConnectToken string `json:"connectToken"`
	RelayKey     string `json:"relayKey,omitempty"`
}

func (r *Registration) Sign(privateKey ed25519.PrivateKey) error {
	if len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("invalid environment signing key")
	}
	nonce := make([]byte, 18)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	r.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	r.IssuedAt = time.Now().UTC()
	r.Signature = ""
	raw, err := registrationSigningBytes(*r)
	if err != nil {
		return err
	}
	r.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, raw))
	return nil
}

func registrationSigningBytes(registration Registration) ([]byte, error) {
	registration.Token = ""
	registration.Signature = ""
	return json.Marshal(registration)
}

func verifyRegistrationSignature(registration Registration, now time.Time) error {
	if registration.Nonce == "" || len(registration.Nonce) > 128 || registration.IssuedAt.IsZero() ||
		registration.IssuedAt.Before(now.Add(-10*time.Minute)) ||
		registration.IssuedAt.After(now.Add(10*time.Minute)) {
		return errors.New("registration proof is missing or expired")
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(registration.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid environment public key")
	}
	signature, err := base64.RawURLEncoding.DecodeString(registration.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid environment registration signature")
	}
	raw, err := registrationSigningBytes(registration)
	if err != nil || !ed25519.Verify(publicKey, raw, signature) {
		return errors.New("invalid environment registration signature")
	}
	return nil
}

type peer struct {
	session   *yamux.Session
	transport *http.Transport
}

type Hub struct {
	mu           sync.Mutex
	path         string
	usersPath    string
	admin        string
	bindings     map[string]Binding
	users        map[string]userRecord
	sessions     map[string]sessionRecord
	authFailures map[string]authFailure
	peers        map[string]*peer
	closed       bool

	dummyPasswordHash []byte
}

func New(home, admin string) (*Hub, error) {
	if len(admin) < 20 {
		return nil, errors.New("relay requires an authenticated administrator")
	}
	dummyHash, err := newDummyPasswordHash()
	if err != nil {
		return nil, err
	}
	h := &Hub{path: filepath.Join(home, "relay-bindings.json"),
		usersPath: filepath.Join(home, "relay-users.json"), admin: digest(admin),
		bindings: map[string]Binding{}, authFailures: map[string]authFailure{}, peers: map[string]*peer{}}
	raw, err := os.ReadFile(h.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &h.bindings); err != nil || h.bindings == nil {
			return nil, errors.New("invalid relay bindings file")
		}
	}
	users, err := loadUserFile(h.usersPath)
	if err != nil {
		return nil, err
	}
	h.users, h.sessions, h.dummyPasswordHash = users.Users, users.Sessions, dummyHash
	return h, nil
}

func (h *Hub) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/register", h.registerUser)
	mux.HandleFunc("POST /api/v1/auth/login", h.loginUser)
	mux.HandleFunc("GET /api/v1/auth/me", h.currentUser)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logoutUser)
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
	ownerID, admin, authorized := h.registrationAuth(r)
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
	if !admin {
		if err := verifyRegistrationSignature(in, time.Now()); err != nil {
			fail(w, 400, "registration", err.Error())
			return
		}
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
	relayRaw := make([]byte, 32)
	if _, err := rand.Read(relayRaw); err != nil {
		fail(w, 500, "random", "could not generate relay access secret")
		return
	}
	relayKey := base64.RawURLEncoding.EncodeToString(relayRaw)
	binding := Binding{ID: in.ID, OwnerID: ownerID, Name: in.Name, PublicKey: in.PublicKey,
		DirectURLs: in.DirectURLs, ConnectHash: digest(token), AccessHash: digest(in.Token),
		RelayKey: relayKey}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		fail(w, 503, "closed", "relay is shutting down")
		return
	}
	existing, exists := h.bindings[in.ID]
	selfRotation := false
	if exists && existing.PublicKey == in.PublicKey && matches(existing.AccessHash, in.Token) {
		selfRotation = !authorized
	}
	if !authorized && !selfRotation {
		h.mu.Unlock()
		fail(w, 401, "unauthorized", "relay user token required")
		return
	}
	if exists {
		if !admin && !selfRotation && (ownerID == "" || existing.OwnerID != ownerID) {
			h.mu.Unlock()
			fail(w, 409, "exists", "environment is owned by another account")
			return
		}
		if existing.PublicKey != in.PublicKey {
			h.mu.Unlock()
			fail(w, 409, "exists", "environment identity does not match the registered binding")
			return
		}
		// A host can rotate its Relay key with its existing environment
		// token. The signed registration proves that the same environment
		// identity requested the upgrade, while the old binding retains its
		// account owner and therefore its discovery visibility.
		binding.OwnerID = existing.OwnerID
		h.bindings[in.ID] = binding
		if err := h.save(); err != nil {
			h.bindings[in.ID] = existing
			h.mu.Unlock()
			fail(w, 500, "store", "could not persist binding")
			return
		}
		h.mu.Unlock()
		reply(w, http.StatusOK, Registered{ConnectToken: token, RelayKey: relayKey})
		return
	}
	h.bindings[in.ID] = binding
	if err := h.save(); err != nil {
		delete(h.bindings, in.ID)
		h.mu.Unlock()
		fail(w, 500, "store", "could not persist binding")
		return
	}
	h.mu.Unlock()
	reply(w, 201, Registered{ConnectToken: token, RelayKey: relayKey})
}

func (h *Hub) list(w http.ResponseWriter, r *http.Request) {
	ownerID, admin, authorized := h.registrationAuth(r)
	if !authorized {
		fail(w, 401, "unauthorized", "relay user token required")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]map[string]any, 0, len(h.bindings))
	for id, binding := range h.bindings {
		if !admin && binding.OwnerID != ownerID {
			continue
		}
		p := h.peers[id]
		out = append(out, map[string]any{"environmentId": id, "name": binding.Name,
			"publicKey": binding.PublicKey, "directUrls": binding.DirectURLs,
			"online": p != nil && !p.session.IsClosed()})
	}
	reply(w, 200, map[string]any{"environments": out})
}

func (h *Hub) remove(w http.ResponseWriter, r *http.Request) {
	ownerID, admin, authorized := h.registrationAuth(r)
	if !authorized {
		fail(w, 401, "unauthorized", "relay user token required")
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
	if !admin && binding.OwnerID != ownerID {
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
	if err = f.Chmod(0o600); err == nil {
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
	secret := bearer(r)
	h.mu.Lock()
	binding, exists := h.bindings[id]
	p := h.peers[id]
	authorized := exists && publicProbe
	viaUser := false
	if exists && !authorized && matches(binding.AccessHash, secret) {
		authorized = true
	}
	if exists && !authorized && h.validUserSessionLocked(secret) {
		if binding.OwnerID != "" && binding.OwnerID == h.sessions[digest(secret)].UserID {
			authorized, viaUser = true, true
		}
	}
	relayKey := binding.RelayKey
	h.mu.Unlock()
	if !authorized {
		fail(w, 401, "unauthorized", "environment token required")
		return
	}
	if viaUser && relayKey == "" {
		fail(w, http.StatusConflict, "account_access_not_configured",
			"environment must be re-registered by its desktop before account access can use it")
		return
	}
	if p == nil || p.session.IsClosed() {
		fail(w, 503, "environment_offline", "environment tunnel is offline")
		return
	}
	signedAt := time.Now().UTC().Unix()
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
			if viaUser {
				query := pr.Out.URL.Query()
				query.Del("token")
				pr.Out.URL.RawQuery = query.Encode()
				uri := pr.Out.URL.RequestURI()
				pr.Out.Header.Del("Authorization")
				pr.Out.Header.Set(HeaderRelayEnvironment, id)
				pr.Out.Header.Set(HeaderRelayTimestamp, strconv.FormatInt(signedAt, 10))
				pr.Out.Header.Set(HeaderRelaySignature,
					SignRelayRequest(relayKey, pr.Out.Method, id, signedAt, uri))
			}
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
