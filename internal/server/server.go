// Package server exposes a T3Go environment over HTTP, Server-Sent
// Events and WebSocket.
package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/events"
	"github.com/tk/t3go/internal/git"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/store"
	"github.com/tk/t3go/internal/upstream"
)

// Server is one running T3Go environment.
type Server struct {
	Config  config.Config
	Store   *store.Store
	Service *orchestrator.Service
	Bus     *events.Bus
	Log     *log.Logger

	ID        string
	StartedAt time.Time

	mu       sync.Mutex
	http     *http.Server
	listener net.Listener
	addr     string
	closing  chan struct{}
	stopOnce sync.Once
	ticketMu sync.Mutex
	tickets  map[string]time.Time
}

// New opens the environment's database and builds its services.
func New(cfg config.Config, logger *log.Logger) (*Server, error) {
	if logger == nil {
		logger = log.New(os.Stderr, "t3go: ", log.LstdFlags)
	}
	st, err := store.Open(config.DBPath(cfg.Home))
	if err != nil {
		return nil, err
	}
	bus := events.New(st)
	providers := provider.NewRegistry()
	svc := orchestrator.New(st, providers, bus, cfg.Home, logger)
	svc.ApprovalToken = cfg.Token
	s := &Server{Config: cfg, Store: st, Service: svc, Bus: bus, Log: logger, ID: id.New("env"), StartedAt: time.Now(),
		closing: make(chan struct{}), tickets: map[string]time.Time{}}
	if runs, threads, err := svc.RecoverInterrupted(context.Background()); err != nil {
		_ = st.Close()
		return nil, err
	} else if runs > 0 || threads > 0 {
		logger.Printf("recovered %d interrupted runs and %d threads", runs, threads)
	}
	return s, nil
}

// Addr returns the listener's address after Start.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Token returns the environment's bearer token.
func (s *Server) Token() string { return s.Config.Token }

// Start listens and serves in a goroutine. listen is host:port; an empty
// host or ":0" chooses an available port.
func (s *Server) Start(listen string) error {
	if listen == "" {
		listen = s.Config.Listen
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("server: listen: %w", err)
	}
	s.mu.Lock()
	s.listener = ln
	s.addr = ln.Addr().String()
	s.Service.ApprovalURL = localURL(s.addr)
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 15 * time.Second}
	httpServer := s.http
	addr := s.addr
	s.mu.Unlock()
	s.Log.Printf("listening on http://%s", addr)
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.Log.Printf("server: %v", err)
		}
	}()
	s.Bus.Prune(context.Background(), 5000)
	return nil
}

// Serve serves on an existing listener until ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.mu.Lock()
	s.listener = ln
	s.addr = ln.Addr().String()
	s.Service.ApprovalURL = localURL(s.addr)
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 15 * time.Second}
	httpServer := s.http
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.stopStreaming()
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// stopStreaming ends the live SSE and WebSocket handlers, which would
// otherwise hold Shutdown open until its deadline.
func (s *Server) stopStreaming() {
	s.stopOnce.Do(func() { close(s.closing) })
}

// Close stops the HTTP server, the live runs and the database.
func (s *Server) Close() error {
	s.stopStreaming()
	s.mu.Lock()
	h := s.http
	s.mu.Unlock()
	httpCtx, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if h != nil {
		_ = h.Shutdown(httpCtx)
	}
	httpCancel()
	runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Second)
	s.Service.Shutdown(runCtx)
	runCancel()
	return s.Store.Close()
}

// PairingURL is a one-time link a client can scan or paste.
func (s *Server) PairingURL() string {
	host := s.Addr()
	if strings.HasPrefix(host, "0.0.0.0:") || strings.HasPrefix(host, "[::]:") {
		ifaces, _ := net.Interfaces()
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
					host = ipnet.IP.String() + host[strings.LastIndex(host, ":"):]
					break
				}
			}
		}
	}
	return fmt.Sprintf("t3go://pair?url=%s&token=%s", "http://"+host, s.Config.Token)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("POST /oauth/token", s.handleOAuthToken)
	mux.HandleFunc("GET /api/auth/session", s.auth(s.handleAuthSession))
	mux.HandleFunc("POST /api/auth/websocket-ticket", s.auth(s.handleWebsocketTicket))
	mux.HandleFunc("POST /api/attachments/{id}", s.handleAttachmentUpload)
	mux.HandleFunc("GET /api/attachments/{id}", s.handleAttachmentRead)
	mux.HandleFunc("POST /api/auth/websocket-ticket/{use}", s.auth(s.handleWebsocketTicket))
	mux.HandleFunc("GET /.well-known/t3/environment", s.handleUpstreamDescriptor)
	mux.HandleFunc("GET /api/v1/state", s.auth(s.handleState))
	mux.HandleFunc("GET /api/v1/projects", s.auth(s.handleProjects))
	mux.HandleFunc("POST /api/v1/projects", s.auth(s.handleCreateProject))
	mux.HandleFunc("PATCH /api/v1/projects/{id}", s.auth(s.handleUpdateProject))
	mux.HandleFunc("DELETE /api/v1/projects/{id}", s.auth(s.handleDeleteProject))
	mux.HandleFunc("GET /api/v1/threads", s.auth(s.handleThreads))
	mux.HandleFunc("POST /api/v1/threads", s.auth(s.handleCreateThread))
	mux.HandleFunc("GET /api/v1/threads/{id}", s.auth(s.handleThread))
	mux.HandleFunc("PATCH /api/v1/threads/{id}", s.auth(s.handleUpdateThread))
	mux.HandleFunc("DELETE /api/v1/threads/{id}", s.auth(s.handleDeleteThread))
	mux.HandleFunc("POST /api/v1/threads/{id}/messages", s.auth(s.handleSend))
	mux.HandleFunc("POST /api/v1/threads/{id}/stop", s.auth(s.handleStop))
	mux.HandleFunc("GET /api/v1/threads/{id}/diff", s.auth(s.handleDiff))
	mux.HandleFunc("GET /api/v1/providers", s.auth(s.handleProviders))
	mux.HandleFunc("GET /api/v1/settings", s.auth(s.handleSettings))
	mux.HandleFunc("PATCH /api/v1/settings", s.auth(s.handleUpdateSettings))
	mux.HandleFunc("GET /api/v1/fs", s.auth(s.handleListDir))
	mux.HandleFunc("POST /api/v1/threads/{id}/approvals", s.auth(s.handleRequestApproval))
	mux.HandleFunc("GET /api/v1/approvals", s.auth(s.handleApprovals))
	mux.HandleFunc("POST /api/v1/approvals/{id}", s.auth(s.handleResolveApproval))
	mux.HandleFunc("POST /api/v1/uploads", s.auth(s.handleCreateUpload))
	mux.HandleFunc("GET /api/v1/uploads/{id}", s.auth(s.handleUpload))
	mux.HandleFunc("DELETE /api/v1/uploads/{id}", s.auth(s.handleDeleteUpload))
	mux.HandleFunc("GET /api/v1/events", s.auth(s.handleSSE))
	mux.HandleFunc("GET /ws", s.auth(s.handleWS))
	if s.Config.WebDir != "" {
		mux.HandleFunc("/", s.handleWeb)
	}
	return withLogging(withCORS(mux), s.Log)
}

// auth checks the bearer token, or the token query parameter for clients
// that cannot set a header.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Config.NoAuth {
			next(w, r)
			return
		}
		token := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if r.URL.Path == "/ws" {
			if ticket := r.URL.Query().Get("wsTicket"); ticket != "" && s.consumeTicket(ticket) {
				next(w, r)
				return
			}
		}
		if !constantTimeEqual(token, s.Config.Token) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a valid token is required")
			return
		}
		next(w, r)
	}
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush lets streaming handlers see a real http.Flusher through the
// logging wrapper.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack lets the WebSocket handler take the connection through the
// logging wrapper.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("t3go: hijacking is not supported")
	}
	return h.Hijack()
}

// Unwrap lets http.ResponseController reach the real response writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func withLogging(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if !strings.HasPrefix(r.URL.Path, "/api/v1/events") && !strings.HasPrefix(r.URL.Path, "/ws") {
			logger.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "name": "t3go", "version": proto.ProtocolVersion, "serverId": s.ID,
		"time": time.Now(), "uptime": time.Since(s.StartedAt).Round(time.Second).String(),
	})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The sequence goes before the reads: a client that streams after it
	// sees what changed while the snapshot was built, which is a repeat at
	// worst.
	seq := s.Bus.LastSeq(ctx)
	projects, err := s.Store.Projects(ctx)
	if err != nil {
		writeError(w, 500, "store", err.Error())
		return
	}
	threads, err := s.Store.Threads(ctx, "", false)
	if err != nil {
		writeError(w, 500, "store", err.Error())
		return
	}
	providers := s.Service.Providers.Snapshot(ctx)
	settings := s.Service.Settings(ctx)
	writeJSON(w, 200, proto.State{
		Version: proto.ProtocolVersion, Seq: seq, ServerID: s.ID, Name: settings.Title,
		Projects: nonNil(projects), Threads: nonNil(threads),
		Providers: providers, Models: s.Service.Providers.Models(ctx), Settings: settings, ServerTime: time.Now(),
	})
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Store.Projects(r.Context())
	if err != nil {
		writeError(w, 500, "store", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"projects": nonNil(projects)})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	p, err := s.Service.CreateProject(r.Context(), in.Name, in.Path)
	if err != nil {
		writeError(w, 400, "project", err.Error())
		return
	}
	writeJSON(w, 201, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	p, err := s.Service.UpdateProject(r.Context(), r.PathValue("id"), in.Name, in.Path)
	if err != nil {
		writeError(w, statusFor(err), "project", err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.Service.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), "project", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	threads, err := s.Store.Threads(r.Context(), r.URL.Query().Get("projectId"), r.URL.Query().Get("archived") == "true")
	if err != nil {
		writeError(w, 500, "store", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"threads": nonNil(threads)})
}

func (s *Server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID  string               `json:"projectId"`
		Title      string               `json:"title"`
		Provider   string               `json:"provider"`
		Model      string               `json:"model"`
		Permission proto.PermissionMode `json:"permission"`
		Branch     string               `json:"branch"`
		Worktree   bool                 `json:"worktree"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	t, err := s.Service.CreateThread(r.Context(), orchestrator.CreateThreadInput{
		ProjectID: in.ProjectID, Title: in.Title, Provider: in.Provider, Model: in.Model,
		Permission: in.Permission, Branch: in.Branch, Worktree: in.Worktree,
	})
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	writeJSON(w, 201, t)
}

func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) {
	detail, err := s.Service.Detail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	writeJSON(w, 200, detail)
}

func (s *Server) handleUpdateThread(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title      *string               `json:"title"`
		Provider   *string               `json:"provider"`
		Model      *string               `json:"model"`
		Permission *proto.PermissionMode `json:"permission"`
		Archived   *bool                 `json:"archived"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	t, err := s.Service.UpdateThread(r.Context(), r.PathValue("id"), orchestrator.ThreadPatch{
		Title: in.Title, Provider: in.Provider, Model: in.Model, Permission: in.Permission, Archived: in.Archived,
	})
	if err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) handleDeleteThread(w http.ResponseWriter, r *http.Request) {
	if err := s.Service.DeleteThread(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), "thread", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content       string   `json:"content"`
		AttachmentIDs []string `json:"attachmentIds"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	run, err := s.Service.SendWith(r.Context(), r.PathValue("id"), in.Content, in.AttachmentIDs)
	if err != nil {
		writeError(w, statusFor(err), "run", err.Error())
		return
	}
	writeJSON(w, 202, run)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	stopped := s.Service.Stop(r.PathValue("id"))
	writeJSON(w, 200, map[string]bool{"stopped": stopped})
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	diff, err := s.Service.Diff(r.Context(), r.PathValue("id"), r.URL.Query().Get("runId"))
	if err != nil {
		writeError(w, statusFor(err), "diff", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"diff": diff})
}

// handleOAuthToken exchanges the environment's token for a bearer access
// token, the flow a T3 Code client uses after a pairing link.
func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	subject := r.PostFormValue("subject_token")
	if !constantTimeEqual(subject, s.Config.Token) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "the pairing token is not valid"})
		return
	}
	scope := strings.TrimSpace(r.PostFormValue("scope"))
	if scope == "" {
		scope = strings.Join(upstream.Scopes(), " ")
	}
	writeJSON(w, 200, map[string]any{
		"access_token":      s.Config.Token,
		"issued_token_type": "urn:ietf:params:oauth:token-type:access_token",
		"token_type":        "Bearer",
		"expires_in":        3600,
		"scope":             scope,
	})
}

// handleAuthSession reports who the bearer token belongs to.
func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"authenticated": true,
		"auth":          upstream.AuthDescriptor(s.upstreamEnvironment()),
		"scopes":        upstream.Scopes(),
		"permissions":   upstream.Scopes(),
		"sessionMethod": "bearer-access-token",
		"expiresAt":     time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000Z"),
	})
}

// handleWebsocketTicket mints the one-time ticket a socket URL carries.
func (s *Server) handleWebsocketTicket(w http.ResponseWriter, r *http.Request) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		writeError(w, 500, "ticket", err.Error())
		return
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(time.Hour)
	s.ticketMu.Lock()
	s.tickets[ticket] = expires
	s.ticketMu.Unlock()
	writeJSON(w, 200, map[string]any{"ticket": ticket, "expiresAt": expires.UTC().Format("2006-01-02T15:04:05.000Z")})
}

func (s *Server) consumeTicket(ticket string) bool {
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	expires, ok := s.tickets[ticket]
	if !ok {
		return false
	}
	delete(s.tickets, ticket)
	if time.Now().After(expires) {
		return false
	}
	return true
}

// upstreamEnvironment is what the compatibility layer answers about this
// environment.
func (s *Server) upstreamEnvironment() upstream.Environment {
	ctx := context.Background()
	cwd, _ := os.Getwd()
	env := upstream.Environment{
		ID:      s.ID,
		Label:   s.Service.Settings(ctx).Title,
		Version: maxVersion(s.Config.Version),
		Home:    s.Config.Home,
		CWD:     cwd,
		LogsDir: filepath.Join(s.Config.Home, "logs"),
		Token:   s.Config.Token,
	}
	if projects, err := s.Store.Projects(ctx); err == nil {
		for _, p := range projects {
			env.Projects = append(env.Projects, upstream.Project{
				ID: p.ID, Title: p.Name, WorkspaceRoot: p.Path, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
			})
		}
	}
	for _, p := range s.Service.Providers.Snapshot(ctx) {
		provider := upstream.Provider{ID: p.ID, Name: p.Name, Installed: p.Installed, Version: p.Version}
		for _, m := range p.Models {
			provider.Models = append(provider.Models, upstream.Model{ID: m.ID, Name: m.Name, Default: m.Default, Custom: m.Custom})
		}
		env.Providers = append(env.Providers, provider)
	}
	if threads, err := s.Store.Threads(ctx, "", true); err == nil {
		for _, t := range threads {
			env.Threads = append(env.Threads, upstream.Thread{
				ID: t.ID, ProjectID: t.ProjectID, Title: t.Title, Provider: t.Provider, Model: t.Model,
				Permission: string(t.Permission), Branch: t.Branch, WorktreePath: t.WorktreePath,
				Status: string(t.Status), LastError: t.LastError, Archived: t.Archived,
				CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
			})
		}
	}
	return env
}

func maxVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "0.0.0-t3go"
	}
	return v
}

// handleUpstreamDescriptor answers T3 Code's discovery request.
func (s *Server) handleUpstreamDescriptor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, 200, upstream.Descriptor(s.upstreamEnvironment()))
}

// handleUpstreamWS speaks the subset of T3 Code's JSON RPC that this
// compatibility layer implements. It never mutates T3Go's own API.
func (s *Server) handleUpstreamWS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("orchestrationProtocol") != strconv.Itoa(upstream.ProtocolVersion) {
		writeJSON(w, http.StatusUpgradeRequired, map[string]any{
			"code":                         "orchestration_protocol_incompatible",
			"message":                      "This environment speaks orchestration protocol " + strconv.Itoa(upstream.ProtocolVersion) + ".",
			"orchestrationProtocolVersion": upstream.ProtocolVersion,
		})
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-s.closing:
			conn.CloseNow()
		case <-done:
		}
	}()
	writer := &upstreamWriter{ctx: ctx, conn: conn}
	env := s.upstreamEnvironment()
	shellForwarding := false
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			continue
		}
		var envelope struct {
			Tag     string          `json:"tag"`
			ID      json.RawMessage `json:"id"`
			Payload json.RawMessage `json:"payload"`
			Control string          `json:"_tag"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}
		if envelope.Control == "Ping" {
			_ = writer.write(map[string]any{"_tag": "Pong"})
			continue
		}
		if envelope.Control != "" && envelope.Control != "Request" {
			continue
		}
		switch envelope.Tag {
		case "orchestration.subscribeThread":
			var input struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(envelope.Payload, &input)
			thread, messages, runs, ok := s.upstreamThread(input.ThreadID)
			if !ok {
				_ = writer.write(upstream.UnknownFailure(envelope.ID, "unknown thread"))
				continue
			}
			if err := writer.write(upstream.Chunk(envelope.ID, []any{upstream.ThreadSnapshot(thread, messages, runs)})); err != nil {
				return
			}
			go s.forwardThread(ctx, writer, envelope.ID, input.ThreadID, messages, runs)
			continue
		case "orchestration.getThreadProjection":
			var input struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(envelope.Payload, &input)
			thread, messages, runs, ok := s.upstreamThread(input.ThreadID)
			if !ok {
				_ = writer.write(upstream.UnknownFailure(envelope.ID, "unknown thread"))
				continue
			}
			if err := writer.write(upstream.Exit(envelope.ID, upstream.ThreadProjection(thread, messages, runs))); err != nil {
				return
			}
			continue
		case "orchestration.dispatchCommand":
			if !s.upstreamDispatch(ctx, writer, envelope.ID, envelope.Payload) {
				return
			}
			continue
		case "assets.persistChatAttachments":
			if !s.upstreamPersistAttachments(ctx, writer, envelope.ID, envelope.Payload) {
				return
			}
			continue
		case "assets.createUrl":
			if !s.upstreamAssetURL(writer, envelope.ID, envelope.Payload) {
				return
			}
			continue
		case "server.getUsageSummary":
			if !s.upstreamUsage(ctx, writer, envelope.ID, envelope.Payload) {
				return
			}
			continue
		case "orchestration.searchThreads":
			if !s.upstreamSearchThreads(ctx, writer, envelope.ID, envelope.Payload) {
				return
			}
			continue
		case "orchestration.searchThread":
			if !s.upstreamSearchThread(ctx, writer, envelope.ID, envelope.Payload) {
				return
			}
			continue
		case "vcs.refreshStatus", "subscribeVcsStatus":
			if !s.upstreamVcsStatus(ctx, writer, envelope.ID, envelope.Tag, envelope.Payload) {
				return
			}
			continue
		case "vcs.refreshBranch", "subscribeVcsBranch":
			if !s.upstreamVcsBranch(ctx, writer, envelope.ID, envelope.Tag, envelope.Payload) {
				return
			}
			continue
		case "attachments.createUploadUrl":
			var input struct {
				Name      string `json:"name"`
				MimeType  string `json:"mimeType"`
				SizeBytes int64  `json:"sizeBytes"`
			}
			_ = json.Unmarshal(envelope.Payload, &input)
			attachment, err := s.Service.CreateUpload(ctx, input.Name, input.MimeType, input.SizeBytes)
			if err != nil {
				_ = writer.write(upstream.UnknownFailure(envelope.ID, err.Error()))
				continue
			}
			relative := "/api/attachments/" + attachment.ID + "?token=" + url.QueryEscape(s.Config.Token)
			if err := writer.write(upstream.Exit(envelope.ID, map[string]any{
				"attachmentId": attachment.ID, "relativeUrl": relative,
				"expiresAt": float64(time.Now().Add(time.Hour).Unix()),
			})); err != nil {
				return
			}
			continue
		case "attachments.delete":
			var input struct {
				AttachmentID string `json:"attachmentId"`
			}
			_ = json.Unmarshal(envelope.Payload, &input)
			_ = s.Service.DeleteUpload(ctx, input.AttachmentID)
			if err := writer.write(upstream.ExitVoid(envelope.ID)); err != nil {
				return
			}
			continue
		case "orchestration.getTurnDiff", "orchestration.getFullThreadDiff":
			if !s.upstreamDiff(ctx, writer, envelope.ID, envelope.Tag, envelope.Payload) {
				return
			}
			continue
		case "orchestration.launchThread":
			if !s.upstreamLaunch(ctx, writer, envelope.ID, envelope.Payload) {
				return
			}
			continue
		}
		frames, _, known := upstream.Handle(env, upstream.Request{Tag: envelope.Tag, ID: envelope.ID, Payload: envelope.Payload})
		if !known {
			frames = []map[string]any{upstream.UnknownFailure(envelope.ID, upstream.Unknown(envelope.Tag))}
		}
		for _, frame := range frames {
			if err := writer.write(frame); err != nil {
				return
			}
		}
		if envelope.Tag == "orchestration.subscribeShell" && !shellForwarding {
			shellForwarding = true
			go s.forwardShell(ctx, writer, envelope.ID)
		}
	}
}

// upstreamThread reads one thread and its text transcript.
func (s *Server) upstreamThread(threadID string) (upstream.Thread, []upstream.Message, []upstream.Run, bool) {
	ctx := context.Background()
	thread, err := s.Store.Thread(ctx, threadID)
	if err != nil {
		return upstream.Thread{}, nil, nil, false
	}
	stored, err := s.Store.Messages(ctx, threadID, 4000)
	if err != nil {
		return upstream.Thread{}, nil, nil, false
	}
	userMessages := map[string]string{}
	for _, m := range stored {
		if m.Role == proto.RoleUser && m.RunID != "" {
			if _, ok := userMessages[m.RunID]; !ok {
				userMessages[m.RunID] = m.ID
			}
		}
	}
	messages := make([]upstream.Message, 0, len(stored))
	for _, m := range stored {
		if !upstream.IsProjected(upstream.Message{Role: string(m.Role), Kind: string(m.Kind), Content: m.Content, Status: m.Status}) {
			continue
		}
		messages = append(messages, upstream.Message{
			ID: m.ID, ThreadID: m.ThreadID, RunID: m.RunID, Role: string(m.Role), Kind: string(m.Kind),
			Title: m.Title, Detail: m.Detail, Content: m.Content, Status: m.Status,
			Attachments: upstreamAttachments(m.Attachments),
			CreatedAt:   m.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		})
	}
	protoRuns, _ := s.Store.Runs(ctx, threadID)
	runs := make([]upstream.Run, 0, len(protoRuns))
	for _, r := range protoRuns {
		ended := ""
		if r.EndedAt != nil {
			ended = r.EndedAt.UTC().Format("2006-01-02T15:04:05.000Z")
		}
		runs = append(runs, upstream.Run{
			ID: r.ID, ThreadID: r.ThreadID, UserMessageID: userMessages[r.ID], Provider: r.Provider, Model: r.Model,
			Status: string(r.Status), StartedAt: r.StartedAt.UTC().Format("2006-01-02T15:04:05.000Z"), EndedAt: ended,
		})
	}
	return shellThread(thread), messages, runs, true
}

// upstreamUsage aggregates T3Go's runs into the usage summary.
func (s *Server) upstreamUsage(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		SinceDay string `json:"sinceDay"`
		UntilDay string `json:"untilDay"`
		TimeZone string `json:"timeZone"`
	}
	_ = json.Unmarshal(payload, &input)
	since, err := time.Parse("2006-01-02", input.SinceDay)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid sinceDay"))
		return true
	}
	until, err := time.Parse("2006-01-02", input.UntilDay)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid untilDay"))
		return true
	}
	until = until.Add(24 * time.Hour)
	runs, err := s.Store.RunsSince(ctx, since)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
		return true
	}
	usageRuns := make([]upstream.UsageRun, 0, len(runs))
	for _, run := range runs {
		if run.StartedAt.Before(since) || !run.StartedAt.Before(until) {
			continue
		}
		u := upstream.UsageRun{Provider: run.Provider, Model: run.Model, StartedAt: run.StartedAt}
		if run.Usage != nil {
			u.HasUsage = true
			u.Input = run.Usage.InputTokens
			u.Output = run.Usage.OutputTokens
			u.Total = run.Usage.TotalTokens
			u.CostUSD = run.Usage.CostUSD
		}
		usageRuns = append(usageRuns, u)
	}
	return writer.write(upstream.Exit(requestID,
		upstream.UsageSummary(s.upstreamEnvironment(), input.SinceDay, input.UntilDay, input.TimeZone, usageRuns))) == nil
}

// upstreamSearchThreads searches text messages across the environment.
func (s *Server) upstreamSearchThreads(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(payload, &input)
	if len([]rune(input.Query)) < 2 {
		return writer.write(upstream.Exit(requestID, map[string]any{"matches": []any{}})) == nil
	}
	if input.Limit <= 0 || input.Limit > 50 {
		input.Limit = 20
	}
	messages, err := s.Store.SearchMessages(ctx, input.Query, input.Limit)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
		return true
	}
	projects := map[string]string{}
	if threads, err := s.Store.Threads(ctx, "", true); err == nil {
		for _, t := range threads {
			projects[t.ID] = t.ProjectID
		}
	}
	matches := make([]any, 0, len(messages))
	for _, m := range messages {
		if m.Role != proto.RoleUser && m.Role != proto.RoleAssistant {
			continue
		}
		matches = append(matches, map[string]any{
			"threadId":         m.ThreadID,
			"projectId":        projects[m.ThreadID],
			"source":           string(m.Role),
			"snippet":          truncateRunes(m.Content, 240),
			"messageCreatedAt": m.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
			"quality":          1,
			"hits":             1,
			"messageId":        m.ID,
		})
	}
	return writer.write(upstream.Exit(requestID, map[string]any{"matches": matches})) == nil
}

// upstreamSearchThread finds a query inside one thread's transcript.
func (s *Server) upstreamSearchThread(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		ThreadID string `json:"threadId"`
		Query    string `json:"query"`
		Index    int    `json:"index"`
	}
	_ = json.Unmarshal(payload, &input)
	messages, err := s.Store.Messages(ctx, input.ThreadID, 4000)
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
		return true
	}
	var matched []proto.Message
	for _, m := range messages {
		if m.Kind != proto.KindText || !strings.Contains(strings.ToLower(m.Content), strings.ToLower(input.Query)) {
			continue
		}
		matched = append(matched, m)
	}
	active := input.Index
	if active < 0 || active >= len(matched) {
		active = 0
	}
	var match any
	if len(matched) > 0 {
		m := matched[active]
		match = map[string]any{"entryId": m.ID, "runId": nullableString(m.RunID), "occurrence": 0}
	}
	return writer.write(upstream.Exit(requestID, map[string]any{
		"snapshotSequence": 0, "totalMatches": len(matched), "activeIndex": active, "match": match,
	})) == nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

// upstreamVcsStatus answers git status calls for a project or worktree.
func (s *Server) upstreamVcsStatus(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, tag string, payload json.RawMessage) bool {
	var input struct {
		Cwd string `json:"cwd"`
	}
	_ = json.Unmarshal(payload, &input)
	if !s.allowedDir(input.Cwd) {
		_ = writer.write(upstream.UnknownFailure(requestID, "unknown working directory"))
		return true
	}
	local, upstreamBranch, ahead, behind := s.vcsStatus(ctx, input.Cwd)
	if tag == "vcs.refreshStatus" {
		return writer.write(upstream.Exit(requestID, upstream.StatusResult(local, upstreamBranch, ahead, behind))) == nil
	}
	event := upstream.StatusSnapshot(local, upstreamBranch, ahead, behind)
	return writer.write(upstream.Chunk(requestID, []any{event})) == nil
}

func (s *Server) upstreamVcsBranch(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, tag string, payload json.RawMessage) bool {
	var input struct {
		Cwd string `json:"cwd"`
	}
	_ = json.Unmarshal(payload, &input)
	if !s.allowedDir(input.Cwd) {
		_ = writer.write(upstream.UnknownFailure(requestID, "unknown working directory"))
		return true
	}
	status := git.Status(ctx, input.Cwd)
	result := map[string]any{"refName": nil}
	if status.Branch != "" {
		result["refName"] = status.Branch
	}
	if tag == "vcs.refreshBranch" {
		return writer.write(upstream.Exit(requestID, result)) == nil
	}
	return writer.write(upstream.Chunk(requestID, []any{result})) == nil
}

func (s *Server) vcsStatus(ctx context.Context, cwd string) (map[string]any, bool, int, int) {
	status := git.Status(ctx, cwd)
	if !status.Repo {
		return upstream.StatusLocal(false, "", "", false, false, nil, 0, 0), false, 0, 0
	}
	files, insertions, deletions, upstreamBranch := git.WorkingTree(ctx, cwd)
	rows := make([]upstream.GitFile, 0, len(files))
	for _, f := range files {
		rows = append(rows, upstream.GitFile{Path: f.Path, Insertions: f.Insertions, Deletions: f.Deletions})
	}
	isDefault := status.Branch == "main" || status.Branch == "master"
	local := upstream.StatusLocal(true, status.Root, status.Branch, git.HasRemote(ctx, cwd), isDefault, rows, insertions, deletions)
	return local, upstreamBranch, status.Ahead, status.Behind
}

// allowedDir limits status reads to known project checkouts and worktrees.
func (s *Server) allowedDir(cwd string) bool {
	clean := filepath.Clean(cwd)
	if clean == "" || clean == "." {
		return false
	}
	ctx := context.Background()
	if projects, err := s.Store.Projects(ctx); err == nil {
		for _, p := range projects {
			if filepath.Clean(p.Path) == clean {
				return true
			}
		}
	}
	if threads, err := s.Store.Threads(ctx, "", true); err == nil {
		for _, t := range threads {
			if t.WorktreePath != "" && filepath.Clean(t.WorktreePath) == clean {
				return true
			}
		}
	}
	return false
}

// upstreamDiff answers the turn and whole-thread diff requests from T3Go's
// run checkpoints. Checkpoints are taken before a turn, so the range always
// reaches the current working tree.
func (s *Server) upstreamDiff(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, tag string, payload json.RawMessage) bool {
	var input struct {
		ThreadID      string `json:"threadId"`
		FromTurnCount int    `json:"fromTurnCount"`
		ToTurnCount   int    `json:"toTurnCount"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid diff request"))
		return true
	}
	from := input.FromTurnCount
	if tag == "orchestration.getFullThreadDiff" {
		from = 0
	}
	if from < 0 || input.ToTurnCount < from {
		_ = writer.write(upstream.TypedFailure(requestID, diffErrorTag(tag), "invalid turn range"))
		return true
	}
	if input.ToTurnCount == from {
		return writer.write(upstream.Exit(requestID, upstream.Diff(input.ThreadID, from, input.ToTurnCount, ""))) == nil
	}
	runID := ""
	if tag == "orchestration.getTurnDiff" {
		if runs, err := s.Store.Runs(ctx, input.ThreadID); err == nil {
			if from < len(runs) {
				runID = runs[from].ID
			}
		}
	}
	diff, err := s.Service.Diff(ctx, input.ThreadID, runID)
	if err != nil {
		_ = writer.write(upstream.TypedFailure(requestID, diffErrorTag(tag), err.Error()))
		return true
	}
	return writer.write(upstream.Exit(requestID, upstream.Diff(input.ThreadID, from, input.ToTurnCount, diff))) == nil
}

func diffErrorTag(tag string) string {
	if tag == "orchestration.getFullThreadDiff" {
		return "OrchestrationGetFullThreadDiffError"
	}
	return "OrchestrationGetTurnDiffError"
}

type upstreamAttachmentRef struct {
	ID string `json:"id"`
}

func attachmentIDs(items []upstreamAttachmentRef) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			out = append(out, item.ID)
		}
	}
	return out
}

func upstreamAttachments(attachments []proto.Attachment) []upstream.Attachment {
	out := make([]upstream.Attachment, 0, len(attachments))
	for _, a := range attachments {
		out = append(out, upstream.Attachment{ID: a.ID, Name: a.Name, MimeType: a.MimeType, Size: a.Size})
	}
	return out
}

// handleAttachmentUpload receives the bytes a T3 Code client PUTs to the
// URL attachments.createUploadUrl minted.
func (s *Server) handleAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	if !s.Config.NoAuth {
		token := r.URL.Query().Get("token")
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if !constantTimeEqual(token, s.Config.Token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	attachment, err := s.Store.Upload(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.OpenFile(s.Service.UploadPath(attachment.ID), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, 500, "upload", err.Error())
		return
	}
	body := http.MaxBytesReader(w, r.Body, 50<<20)
	_, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(s.Service.UploadPath(attachment.ID))
		writeError(w, 413, "too-large", "the attachment could not be stored")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// upstreamPersistAttachments stores the data URLs a client pastes.
func (s *Server) upstreamPersistAttachments(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		Attachments []struct {
			Name     string `json:"name"`
			MimeType string `json:"mimeType"`
			DataURL  string `json:"dataUrl"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		_ = writer.write(upstream.TypedFailure(requestID, "PersistChatAttachmentsError", "invalid attachments"))
		return true
	}
	rows := make([]any, 0, len(input.Attachments))
	for _, in := range input.Attachments {
		comma := strings.IndexByte(in.DataURL, ',')
		if comma < 0 {
			_ = writer.write(upstream.TypedFailure(requestID, "PersistChatAttachmentsError", "an attachment has no data"))
			return true
		}
		raw, err := base64.StdEncoding.DecodeString(in.DataURL[comma+1:])
		if err != nil {
			_ = writer.write(upstream.TypedFailure(requestID, "PersistChatAttachmentsError", "an attachment is not valid base64"))
			return true
		}
		attachment, err := s.Service.SaveUpload(ctx, in.Name, in.MimeType, bytes.NewReader(raw))
		if err != nil {
			_ = writer.write(upstream.TypedFailure(requestID, "PersistChatAttachmentsError", err.Error()))
			return true
		}
		rows = append(rows, upstream.AttachmentRow(upstream.Attachment{
			ID: attachment.ID, Name: attachment.Name, MimeType: attachment.MimeType, Size: attachment.Size,
		}))
	}
	return writer.write(upstream.Exit(requestID, map[string]any{"attachments": rows})) == nil
}

// upstreamAssetURL signs a URL for an attachment a client shows.
func (s *Server) upstreamAssetURL(writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		Resource struct {
			Type         string `json:"type"`
			AttachmentID string `json:"attachmentId"`
			Disposition  string `json:"disposition"`
		} `json:"resource"`
	}
	_ = json.Unmarshal(payload, &input)
	if input.Resource.Type != "attachment" || input.Resource.AttachmentID == "" {
		_ = writer.write(upstream.TypedFailure(requestID, "AssetAccessError", "only attachment resources are available"))
		return true
	}
	if _, err := s.Store.Upload(context.Background(), input.Resource.AttachmentID); err != nil {
		_ = writer.write(upstream.TypedFailure(requestID, "AssetAccessError", "the attachment is gone"))
		return true
	}
	relative := "/api/attachments/" + input.Resource.AttachmentID + "?token=" + url.QueryEscape(s.Config.Token)
	if input.Resource.Disposition == "inline" {
		relative += "&inline=1"
	}
	return writer.write(upstream.Exit(requestID, map[string]any{
		"relativeUrl": relative, "expiresAt": float64(time.Now().Add(time.Hour).Unix()),
	})) == nil
}

// handleAttachmentRead serves an attachment to a URL a client was given.
func (s *Server) handleAttachmentRead(w http.ResponseWriter, r *http.Request) {
	if !s.Config.NoAuth {
		token := r.URL.Query().Get("token")
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if !constantTimeEqual(token, s.Config.Token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	attachment, err := s.Store.Upload(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(s.Service.UploadPath(attachment.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if attachment.MimeType != "" {
		w.Header().Set("Content-Type", attachment.MimeType)
	}
	disposition := "attachment"
	if r.URL.Query().Get("inline") != "" {
		disposition = "inline"
	}
	if value := mime.FormatMediaType(disposition, map[string]string{"filename": attachment.Name}); value != "" {
		w.Header().Set("Content-Disposition", value)
	}
	http.ServeContent(w, r, attachment.Name, time.Time{}, f)
}

// upstreamLaunch creates a thread for a T3 Code client and optionally sends
// its first message.
func (s *Server) upstreamLaunch(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var input struct {
		ThreadID       string `json:"threadId"`
		ReuseExisting  bool   `json:"reuseExistingThread"`
		ProjectID      string `json:"projectId"`
		Title          string `json:"title"`
		RuntimeMode    string `json:"runtimeMode"`
		ModelSelection struct {
			InstanceID string `json:"instanceId"`
			Model      string `json:"model"`
		} `json:"modelSelection"`
		InitialMessage struct {
			Text        string                  `json:"text"`
			Attachments []upstreamAttachmentRef `json:"attachments"`
		} `json:"initialMessage"`
		WorkspaceStrategy struct {
			Type         string `json:"type"`
			BaseRef      string `json:"baseRef"`
			Branch       string `json:"branch"`
			WorktreePath string `json:"worktreePath"`
		} `json:"workspaceStrategy"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid launch"))
		return true
	}
	thread, err := s.Store.Thread(ctx, input.ThreadID)
	resumed := false
	if input.ReuseExisting && err == nil {
		resumed = true
	} else {
		create := orchestrator.CreateThreadInput{
			ID: input.ThreadID, ProjectID: input.ProjectID, Title: input.Title,
			Provider: input.ModelSelection.InstanceID, Model: input.ModelSelection.Model,
			Permission: permissionFromRuntimeMode(input.RuntimeMode), Branch: input.WorkspaceStrategy.Branch,
		}
		switch input.WorkspaceStrategy.Type {
		case "worktree":
			create.Worktree = true
			create.BaseRef = input.WorkspaceStrategy.BaseRef
		case "existing_worktree":
			create.ExistingWorktree = input.WorkspaceStrategy.WorktreePath
		}
		thread, err = s.Service.CreateThread(ctx, create)
	}
	if err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
		return true
	}
	if strings.TrimSpace(input.InitialMessage.Text) != "" {
		_, _ = s.Service.SendWith(ctx, thread.ID, input.InitialMessage.Text, attachmentIDs(input.InitialMessage.Attachments))
	}
	shell, messages, runs, ok := s.upstreamThread(thread.ID)
	if !ok {
		_ = writer.write(upstream.UnknownFailure(requestID, "the thread vanished"))
		return true
	}
	return writer.write(upstream.Exit(requestID, map[string]any{
		"threadId":   thread.ID,
		"projection": upstream.ThreadProjection(shell, messages, runs),
		"resumed":    resumed,
		"sequence":   0,
	})) == nil
}

func permissionFromRuntimeMode(mode string) proto.PermissionMode {
	switch mode {
	case "approval-required":
		return proto.PermissionSupervised
	case "auto-accept-edits":
		return proto.PermissionAcceptEdits
	case "auto":
		return proto.PermissionAuto
	default:
		return proto.PermissionFullAccess
	}
}

// upstreamDispatch maps the T3 Code commands this layer understands.
func (s *Server) upstreamDispatch(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, payload json.RawMessage) bool {
	var command struct {
		Type        string                  `json:"type"`
		ThreadID    string                  `json:"threadId"`
		Text        string                  `json:"text"`
		Attachments []upstreamAttachmentRef `json:"attachments"`
	}
	if err := json.Unmarshal(payload, &command); err != nil {
		_ = writer.write(upstream.UnknownFailure(requestID, "invalid command"))
		return true
	}
	switch command.Type {
	case "message.dispatch":
		if _, err := s.Service.SendWith(ctx, command.ThreadID, command.Text, attachmentIDs(command.Attachments)); err != nil {
			_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
			return true
		}
		return writer.write(upstream.Exit(requestID, map[string]any{"sequence": 0})) == nil
	case "thread.stop":
		s.Service.Stop(command.ThreadID)
		return writer.write(upstream.Exit(requestID, map[string]any{"sequence": 0})) == nil
	case "thread.archive", "thread.unarchive":
		archived := command.Type == "thread.archive"
		if _, err := s.Service.UpdateThread(ctx, command.ThreadID, orchestrator.ThreadPatch{Archived: &archived}); err != nil {
			_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
			return true
		}
		return writer.write(upstream.Exit(requestID, map[string]any{"sequence": 0})) == nil
	case "thread.delete":
		if err := s.Service.DeleteThread(ctx, command.ThreadID); err != nil {
			_ = writer.write(upstream.UnknownFailure(requestID, err.Error()))
			return true
		}
		return writer.write(upstream.Exit(requestID, map[string]any{"sequence": 0})) == nil
	default:
		_ = writer.write(upstream.UnknownFailure(requestID, upstream.Unknown(command.Type)))
		return true
	}
}

// forwardThread sends text message changes to an upstream thread stream.
func (s *Server) forwardThread(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage, threadID string, existing []upstream.Message, runs []upstream.Run) {
	live, unsubscribe := s.Bus.Subscribe()
	defer unsubscribe()
	runOrdinals := map[string]int{}
	for i, run := range runs {
		runOrdinals[run.ID] = i + 1
	}
	nextRunOrdinal := len(runs) + 1
	ordinals := map[string]int{}
	nextOrdinal := 0
	for _, m := range existing {
		if !upstream.IsProjected(m) {
			continue
		}
		ordinals[m.ID] = nextOrdinal
		nextOrdinal++
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-live:
			if !ok {
				return
			}
			if event.ThreadID != threadID {
				continue
			}
			if event.Type == proto.EventRunStarted || event.Type == proto.EventRunFinished {
				run, ok := decodeEvent[proto.Run](event.Data)
				if !ok {
					continue
				}
				ordinal, seen := runOrdinals[run.ID]
				if !seen {
					ordinal = nextRunOrdinal
					runOrdinals[run.ID] = ordinal
					nextRunOrdinal++
				}
				ended := ""
				if run.EndedAt != nil {
					ended = run.EndedAt.UTC().Format("2006-01-02T15:04:05.000Z")
				}
				up := upstream.Run{
					ID: run.ID, ThreadID: run.ThreadID, Provider: run.Provider, Model: run.Model,
					Status: string(run.Status), StartedAt: run.StartedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
					EndedAt: ended, UserMessageID: s.userMessageForRun(threadID, run.ID),
				}
				frame := upstream.RunUpdated(event.Seq*2, up, ordinal, event.Type == proto.EventRunStarted)
				if err := writer.write(upstream.Chunk(requestID, []any{frame})); err != nil {
					return
				}
				continue
			}
			if event.Type != proto.EventMessage {
				continue
			}
			m, ok := decodeEvent[proto.Message](event.Data)
			if !ok {
				continue
			}
			projected := upstream.Message{ID: m.ID, ThreadID: m.ThreadID, RunID: m.RunID, Role: string(m.Role),
				Kind: string(m.Kind), Title: m.Title, Detail: m.Detail, Content: m.Content, Status: m.Status,
				Attachments: upstreamAttachments(m.Attachments),
				CreatedAt:   m.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z")}
			if !upstream.IsProjected(projected) {
				continue
			}
			ordinal, seen := ordinals[m.ID]
			if !seen {
				ordinal = nextOrdinal
				ordinals[m.ID] = ordinal
				nextOrdinal++
			}
			sequence := event.Seq * 2
			if projected.Role == "user" || projected.Kind == "text" {
				if err := writer.write(upstream.Chunk(requestID, []any{upstream.MessageUpdated(sequence, projected, ordinal)})); err != nil {
					return
				}
			}
			if err := writer.write(upstream.Chunk(requestID, []any{upstream.TurnItemUpdated(sequence+1, projected, ordinal)})); err != nil {
				return
			}
		}
	}
}

func (s *Server) userMessageForRun(threadID, runID string) string {
	messages, err := s.Store.Messages(context.Background(), threadID, 4000)
	if err != nil {
		return ""
	}
	for _, m := range messages {
		if m.Role == proto.RoleUser && m.RunID == runID {
			return m.ID
		}
	}
	return ""
}

// localURL is where a program on this machine reaches listen, with a
// wildcard host turned into loopback.
func localURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// handleRequestApproval publishes a provider's tool call and waits for a
// person, which the provider's helper calls.
func (s *Server) handleRequestApproval(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RunID string         `json:"runId"`
		Tool  string         `json:"tool"`
		Input map[string]any `json:"input"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	if strings.TrimSpace(in.Tool) == "" {
		writeError(w, 400, "invalid", "tool is required")
		return
	}
	decision := s.Service.RequestApproval(r.Context(), proto.ApprovalRequest{
		ThreadID: r.PathValue("id"), RunID: in.RunID, Tool: in.Tool, Input: in.Input,
	})
	writeJSON(w, 200, decision)
}

// handleApprovals lists the requests still waiting.
func (s *Server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"approvals": nonNil(s.Service.PendingApprovals(r.URL.Query().Get("threadId")))})
}

// handleResolveApproval answers a waiting request.
func (s *Server) handleResolveApproval(w http.ResponseWriter, r *http.Request) {
	var decision proto.ApprovalDecision
	if err := decodeJSON(r, &decision); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	if err := s.Service.ResolveApproval(r.Context(), r.PathValue("id"), decision); err != nil {
		writeError(w, statusFor(err), "approval", err.Error())
		return
	}
	writeJSON(w, 200, decision)
}

// handleWeb serves the built web client: only files, never a listing.
func (s *Server) handleWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
	if path == "" || path == "." {
		path = "index.html"
	}
	if path == ".." || strings.HasPrefix(path, "../") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	full := filepath.Join(s.Config.WebDir, filepath.FromSlash(path))
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

// maxUploadSize is a prompt attachment's limit.
const maxUploadSize = 25 << 20

// handleCreateUpload stores one request body as an attachment.
func (s *Server) handleCreateUpload(w http.ResponseWriter, r *http.Request) {
	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	body := http.MaxBytesReader(w, r.Body, maxUploadSize)
	attachment, err := s.Service.SaveUpload(r.Context(), r.URL.Query().Get("name"), mimeType, body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too-large", "attachments are limited to 25 MB")
			return
		}
		writeError(w, 500, "upload", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}

// handleUpload serves an attachment's bytes.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	attachment, err := s.Store.Upload(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "upload", err.Error())
		return
	}
	f, err := os.Open(s.Service.UploadPath(attachment.ID))
	if err != nil {
		writeError(w, 404, "upload", "the attachment is gone")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", attachment.MimeType)
	if disposition := mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Name}); disposition != "" {
		w.Header().Set("Content-Disposition", disposition)
	}
	http.ServeContent(w, r, attachment.Name, time.Time{}, f)
}

// handleDeleteUpload removes an attachment that was never sent.
func (s *Server) handleDeleteUpload(w http.ResponseWriter, r *http.Request) {
	if err := s.Service.DeleteUpload(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), "upload", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"providers": s.Service.Providers.Snapshot(r.Context()), "models": s.Service.Providers.Models(r.Context())})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Service.Settings(r.Context()))
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	settings := s.Service.Settings(r.Context())
	if err := decodeJSON(r, &settings); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	if err := s.Service.SaveSettings(r.Context(), settings); err != nil {
		writeError(w, 500, "settings", err.Error())
		return
	}
	writeJSON(w, 200, settings)
}

func (s *Server) handleListDir(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path, _ = os.UserHomeDir()
	}
	path = store.NormalizePath(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, 400, "fs", err.Error())
		return
	}
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var dirs []entry
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, entry{Name: e.Name(), Path: filepath.Join(path, e.Name())})
		}
	}
	writeJSON(w, 200, map[string]any{"path": path, "parent": filepath.Dir(path), "dirs": nonNil(dirs)})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return 404
	case errors.Is(err, orchestrator.ErrBusy):
		return 409
	case errors.Is(err, orchestrator.ErrEmpty), errors.Is(err, orchestrator.ErrBadPath), errors.Is(err, orchestrator.ErrBadAttachment):
		return 400
	case errors.Is(err, orchestrator.ErrArchived), errors.Is(err, orchestrator.ErrNoProvider):
		return 422
	default:
		return 500
	}
}
