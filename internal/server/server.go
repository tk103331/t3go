// Package server exposes a T3Go environment over HTTP, Server-Sent
// Events and WebSocket.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tk/t3go/internal/config"
	"github.com/tk/t3go/internal/events"
	"github.com/tk/t3go/internal/id"
	"github.com/tk/t3go/internal/orchestrator"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/provider"
	"github.com/tk/t3go/internal/store"
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
	s := &Server{Config: cfg, Store: st, Service: svc, Bus: bus, Log: logger, ID: id.New("env"), StartedAt: time.Now()}
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
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 15 * time.Second}
	httpServer := s.http
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close stops the HTTP server and the database.
func (s *Server) Close() error {
	s.mu.Lock()
	h := s.http
	s.mu.Unlock()
	if h != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = h.Shutdown(ctx)
		cancel()
	}
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
	mux.HandleFunc("GET /api/v1/events", s.auth(s.handleSSE))
	mux.HandleFunc("GET /ws", s.auth(s.handleWS))
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
		Version: proto.ProtocolVersion, ServerID: s.ID, Name: settings.Title, Projects: nonNil(projects), Threads: nonNil(threads),
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
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	run, err := s.Service.Send(r.Context(), r.PathValue("id"), in.Content)
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
	case errors.Is(err, orchestrator.ErrEmpty), errors.Is(err, orchestrator.ErrBadPath):
		return 400
	case errors.Is(err, orchestrator.ErrArchived), errors.Is(err, orchestrator.ErrNoProvider):
		return 422
	default:
		return 500
	}
}
