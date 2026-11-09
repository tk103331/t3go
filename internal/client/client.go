// Package client is a small Go client for a T3Go environment: JSON over
// HTTP for actions and Server-Sent Events for live updates. It is what
// the native apps use, and it works from scripts too.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tk/t3go/internal/proto"
)

// ErrUnauthorized is returned when the environment rejects the token.
var ErrUnauthorized = fmt.Errorf("unauthorized")

// APIError is a structured error from the environment.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("t3go: %s (%d)", e.Message, e.Status)
	}
	return fmt.Sprintf("t3go: %s: %s (%d)", e.Code, e.Message, e.Status)
}

// Client talks to one environment.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a client. baseURL may be a bare host, an http(s) URL or a
// t3go://pair link; when a pair link carries a token it is used unless
// token is set.
func New(baseURL, token string) *Client {
	if u, t, ok := ParsePairing(baseURL); ok {
		baseURL = u
		if token == "" {
			token = t
		}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL != "" && !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{Timeout: 0}}
}

// ParsePairing reads a t3go://pair?url=...&token=... link.
func ParsePairing(raw string) (baseURL, token string, ok bool) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "t3go://") {
		return "", "", false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", false
	}
	return u.Query().Get("url"), u.Query().Get("token"), true
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("t3go: no server URL")
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{}
	}
	resp, err := h.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &payload)
		msg := payload.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return &APIError{Status: resp.StatusCode, Code: payload.Error.Code, Message: msg}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Health checks the environment without authentication.
func (c *Client) Health(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	return out, c.do(ctx, http.MethodGet, "/api/v1/health", nil, &out)
}

// State returns projects, threads, providers and settings.
func (c *Client) State(ctx context.Context) (proto.State, error) {
	var out proto.State
	return out, c.do(ctx, http.MethodGet, "/api/v1/state", nil, &out)
}

// Projects lists registered directories.
func (c *Client) Projects(ctx context.Context) ([]proto.Project, error) {
	var out struct {
		Projects []proto.Project `json:"projects"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/projects", nil, &out)
	return out.Projects, err
}

// CreateProject registers a directory.
func (c *Client) CreateProject(ctx context.Context, name, path string) (proto.Project, error) {
	var out proto.Project
	err := c.do(ctx, http.MethodPost, "/api/v1/projects", map[string]any{"name": name, "path": path}, &out)
	return out, err
}

// DeleteProject removes a project and its threads.
func (c *Client) UpdateProject(ctx context.Context, id, name, path string) (proto.Project, error) {
	in := map[string]string{}
	if name != "" {
		in["name"] = name
	}
	if path != "" {
		in["path"] = path
	}
	var out proto.Project
	err := c.do(ctx, http.MethodPatch, "/api/v1/projects/"+url.PathEscape(id), in, &out)
	return out, err
}

func (c *Client) DeleteProject(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/projects/"+url.PathEscape(id), nil, nil)
}

// Threads lists threads, optionally for one project.
func (c *Client) Threads(ctx context.Context, projectID string) ([]proto.Thread, error) {
	path := "/api/v1/threads"
	if projectID != "" {
		path += "?projectId=" + url.QueryEscape(projectID)
	}
	var out struct {
		Threads []proto.Thread `json:"threads"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Threads, err
}

// CreateThread starts a thread.
func (c *Client) CreateThread(ctx context.Context, in CreateThreadInput) (proto.Thread, error) {
	var out proto.Thread
	err := c.do(ctx, http.MethodPost, "/api/v1/threads", in, &out)
	return out, err
}

// CreateThreadInput is the JSON body for a new thread.
type CreateThreadInput struct {
	ProjectID  string               `json:"projectId"`
	Title      string               `json:"title,omitempty"`
	Provider   string               `json:"provider,omitempty"`
	Model      string               `json:"model,omitempty"`
	Permission proto.PermissionMode `json:"permission,omitempty"`
	Branch     string               `json:"branch,omitempty"`
	Worktree   bool                 `json:"worktree,omitempty"`
}

// ThreadDetail returns a thread with its transcript and checkout state.
func (c *Client) ThreadDetail(ctx context.Context, id string) (proto.ThreadDetail, error) {
	var out proto.ThreadDetail
	return out, c.do(ctx, http.MethodGet, "/api/v1/threads/"+url.PathEscape(id), nil, &out)
}

// UpdateThread patches a thread.
func (c *Client) UpdateThread(ctx context.Context, id string, patch map[string]any) (proto.Thread, error) {
	var out proto.Thread
	err := c.do(ctx, http.MethodPatch, "/api/v1/threads/"+url.PathEscape(id), patch, &out)
	return out, err
}

// DeleteThread removes a thread.
func (c *Client) DeleteThread(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/threads/"+url.PathEscape(id), nil, nil)
}

// Send starts a run with prompt.
func (c *Client) Send(ctx context.Context, threadID, prompt string) (proto.Run, error) {
	var out proto.Run
	err := c.do(ctx, http.MethodPost, "/api/v1/threads/"+url.PathEscape(threadID)+"/messages", map[string]string{"content": prompt}, &out)
	return out, err
}

// Stop cancels a thread's run.
func (c *Client) Stop(ctx context.Context, threadID string) (bool, error) {
	var out struct {
		Stopped bool `json:"stopped"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/threads/"+url.PathEscape(threadID)+"/stop", nil, &out)
	return out.Stopped, err
}

// Diff returns a run's checkpoint diff.
func (c *Client) Diff(ctx context.Context, threadID, runID string) (string, error) {
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/diff"
	if runID != "" {
		path += "?runId=" + url.QueryEscape(runID)
	}
	var out struct {
		Diff string `json:"diff"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Diff, err
}

// Providers lists agent CLIs and their models.
func (c *Client) Providers(ctx context.Context) ([]proto.Provider, []proto.Model, error) {
	var out struct {
		Providers []proto.Provider `json:"providers"`
		Models    []proto.Model    `json:"models"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/providers", nil, &out)
	return out.Providers, out.Models, err
}

// Settings reads the environment's defaults.
func (c *Client) Settings(ctx context.Context) (proto.Settings, error) {
	var out proto.Settings
	return out, c.do(ctx, http.MethodGet, "/api/v1/settings", nil, &out)
}

// SaveSettings writes environment defaults.
func (c *Client) SaveSettings(ctx context.Context, settings proto.Settings) (proto.Settings, error) {
	var out proto.Settings
	err := c.do(ctx, http.MethodPatch, "/api/v1/settings", settings, &out)
	return out, err
}

// Dir is one entry in a directory listing.
type Dir struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ListDir lists subdirectories of path.
func (c *Client) ListDir(ctx context.Context, path string) (string, string, []Dir, error) {
	endpoint := "/api/v1/fs"
	if path != "" {
		endpoint += "?path=" + url.QueryEscape(path)
	}
	var out struct {
		Path   string `json:"path"`
		Parent string `json:"parent"`
		Dirs   []Dir  `json:"dirs"`
	}
	err := c.do(ctx, http.MethodGet, endpoint, nil, &out)
	return out.Path, out.Parent, out.Dirs, err
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<min(attempt, 4)) * time.Second
	return min(d, 15*time.Second)
}
