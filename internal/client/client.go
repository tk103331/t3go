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
	return c.send(req, out)
}

// send runs req and decodes out from its answer.
func (c *Client) send(req *http.Request, out any) error {
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

// Dirs lists the folders in one directory of the environment's machine.
// An empty path starts at that machine's home folder.
func (c *Client) Dirs(ctx context.Context, path string) (proto.HostDirs, error) {
	q := url.Values{}
	if path != "" {
		q.Set("path", path)
	}
	var out proto.HostDirs
	target := "/api/v1/dirs"
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}
	err := c.do(ctx, http.MethodGet, target, nil, &out)
	return out, err
}

// CreateDir creates one folder in a directory on the environment's machine
// and returns the refreshed listing for that directory.
func (c *Client) CreateDir(ctx context.Context, path, name string) (proto.HostDirs, error) {
	var out proto.HostDirs
	err := c.do(ctx, http.MethodPost, "/api/v1/dirs", map[string]string{"path": path, "name": name}, &out)
	return out, err
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
	BaseRef    string               `json:"baseRef,omitempty"`
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
	return c.SendWith(ctx, threadID, prompt, nil)
}

// SendWith sends a prompt with the attachments it names.
func (c *Client) SendWith(ctx context.Context, threadID, prompt string, attachmentIDs []string) (proto.Run, error) {
	var out proto.Run
	err := c.do(ctx, http.MethodPost, "/api/v1/threads/"+url.PathEscape(threadID)+"/messages", map[string]any{
		"content": prompt, "attachmentIds": attachmentIDs,
	}, &out)
	return out, err
}

// Upload stores a file and returns its attachment metadata.
func (c *Client) Upload(ctx context.Context, name, mimeType string, r io.Reader) (proto.Attachment, error) {
	var out proto.Attachment
	if c.BaseURL == "" {
		return out, fmt.Errorf("t3go: no server URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/uploads?name="+url.QueryEscape(name), r)
	if err != nil {
		return out, err
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", mimeType)
	err = c.send(req, &out)
	return out, err
}

// DeleteUpload removes an uploaded file that was never sent.
func (c *Client) DeleteUpload(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/uploads/"+url.PathEscape(id), nil, nil)
}

// Approvals lists the tool calls waiting for a decision.
func (c *Client) Approvals(ctx context.Context, threadID string) ([]proto.ApprovalRequest, error) {
	path := "/api/v1/approvals"
	if threadID != "" {
		path += "?threadId=" + url.QueryEscape(threadID)
	}
	var out struct {
		Approvals []proto.ApprovalRequest `json:"approvals"`
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Approvals, err
}

// ResolveApproval answers a waiting tool call.
func (c *Client) ResolveApproval(ctx context.Context, id string, allow bool, message string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/approvals/"+url.PathEscape(id),
		proto.ApprovalDecision{Allow: allow, Message: message}, nil)
}

// Stop cancels a thread's run.
func (c *Client) Stop(ctx context.Context, threadID string) (bool, error) {
	var out struct {
		Stopped bool `json:"stopped"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/threads/"+url.PathEscape(threadID)+"/stop", nil, &out)
	return out.Stopped, err
}

// OpenEditor opens a thread's checkout in an editor.
func (c *Client) OpenEditor(ctx context.Context, threadID, editor string) error {
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/open-editor"
	return c.do(ctx, http.MethodPost, path, map[string]string{"editor": editor}, nil)
}

// RestoreRun puts the checkout's tracked files back to a run's checkpoint.
func (c *Client) RestoreRun(ctx context.Context, threadID, runID string) (proto.GitStatus, string, error) {
	var out struct {
		Status proto.GitStatus `json:"status"`
		Backup string          `json:"backup"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/runs/" + url.PathEscape(runID) + "/restore"
	err := c.do(ctx, http.MethodPost, path, map[string]any{}, &out)
	return out.Status, out.Backup, err
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

// EnsureTerminal starts or reuses the thread's shell.
func (c *Client) EnsureTerminal(ctx context.Context, threadID string) (proto.TerminalSession, error) {
	var out proto.TerminalSession
	endpoint := "/api/v1/threads/" + url.PathEscape(threadID) + "/terminal"
	err := c.do(ctx, http.MethodPost, endpoint, map[string]any{}, &out)
	return out, err
}

// Terminals lists a thread's shells.
func (c *Client) Terminals(ctx context.Context, threadID string) ([]proto.TerminalSession, error) {
	var out struct {
		Sessions []proto.TerminalSession `json:"sessions"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/terminals"
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Sessions, err
}

// NewTerminal starts another shell for a thread.
func (c *Client) NewTerminal(ctx context.Context, threadID string) (proto.TerminalSession, error) {
	var out proto.TerminalSession
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/terminals"
	err := c.do(ctx, http.MethodPost, path, map[string]any{}, &out)
	return out, err
}

// CloseTerminal ends one shell.
func (c *Client) CloseTerminal(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/terminals/"+url.PathEscape(id), nil, nil)
}

// TerminalURL is where a terminal page attaches to its shell.
// TerminalInput writes to a shell.
func (c *Client) TerminalInput(ctx context.Context, id, data string) error {
	path := "/api/v1/terminals/" + url.PathEscape(id) + "/input"
	return c.do(ctx, http.MethodPost, path, map[string]string{"data": data}, nil)
}

func (c *Client) TerminalURL(id string) string {
	base := strings.Replace(c.BaseURL, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	return base + "/api/v1/terminals/" + url.PathEscape(id) + "/ws?token=" + url.QueryEscape(c.Token)
}

// AgentCommands lists workspace skills and custom commands.
func (c *Client) AgentCommands(ctx context.Context, threadID string) ([]proto.AgentCommand, error) {
	var out struct {
		Commands []proto.AgentCommand `json:"commands"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/commands"
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Commands, err
}

// Graph lists the repository's commits for the graph view.
func (c *Client) Graph(ctx context.Context, threadID string) ([]proto.GraphCommit, error) {
	var out struct {
		Commits []proto.GraphCommit `json:"commits"`
	}
	endpoint := "/api/v1/threads/" + url.PathEscape(threadID) + "/graph?limit=200"
	err := c.do(ctx, http.MethodGet, endpoint, nil, &out)
	return out.Commits, err
}

// Files lists a directory in a thread's checkout.
func (c *Client) Files(ctx context.Context, threadID, path string) ([]proto.WorkspaceEntry, error) {
	var out struct {
		Entries []proto.WorkspaceEntry `json:"entries"`
	}
	endpoint := "/api/v1/threads/" + url.PathEscape(threadID) + "/files?path=" + url.QueryEscape(path)
	err := c.do(ctx, http.MethodGet, endpoint, nil, &out)
	return out.Entries, err
}

// FilesRecursive lists every source file in a thread's checkout.
func (c *Client) FilesRecursive(ctx context.Context, threadID string) ([]proto.WorkspaceEntry, error) {
	var out struct {
		Entries []proto.WorkspaceEntry `json:"entries"`
	}
	endpoint := "/api/v1/threads/" + url.PathEscape(threadID) + "/files?recursive=1"
	err := c.do(ctx, http.MethodGet, endpoint, nil, &out)
	return out.Entries, err
}

// File reads one file from a thread's checkout.
func (c *Client) File(ctx context.Context, threadID, path string) (proto.WorkspaceFile, error) {
	var out proto.WorkspaceFile
	endpoint := "/api/v1/threads/" + url.PathEscape(threadID) + "/file?path=" + url.QueryEscape(path)
	err := c.do(ctx, http.MethodGet, endpoint, nil, &out)
	return out, err
}

// UploadURL is where an uploaded attachment can be read.
func (c *Client) UploadURL(id string) string {
	return c.BaseURL + "/api/v1/uploads/" + url.PathEscape(id) + "?token=" + url.QueryEscape(c.Token)
}

// RawFileURL is where a browser or image view can read one file.
func (c *Client) RawFileURL(threadID, path string) string {
	return c.BaseURL + "/api/v1/threads/" + url.PathEscape(threadID) + "/raw?path=" + url.QueryEscape(path) + "&token=" + url.QueryEscape(c.Token)
}

// PullRequest reads the open pull request for a thread's branch.
func (c *Client) PullRequest(ctx context.Context, threadID string) (proto.PullRequest, string, error) {
	var out struct {
		PullRequest *proto.PullRequest `json:"pullRequest"`
		Detail      string             `json:"detail"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/threads/"+url.PathEscape(threadID)+"/pull-request", nil, &out)
	if err != nil {
		return proto.PullRequest{}, "", err
	}
	if out.PullRequest == nil {
		return proto.PullRequest{}, out.Detail, nil
	}
	return *out.PullRequest, out.Detail, nil
}

// CreateBranch makes a branch and checks it out.
func (c *Client) CreateBranch(ctx context.Context, threadID, name string) ([]proto.Branch, error) {
	var out struct {
		Branches []proto.Branch `json:"branches"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/branches"
	err := c.do(ctx, http.MethodPost, path, map[string]any{"name": name, "create": true, "switch": true}, &out)
	return out.Branches, err
}

// PullBranch fast-forwards the checkout from its upstream.
func (c *Client) PullBranch(ctx context.Context, threadID string) (string, string, error) {
	var out struct {
		Status  string `json:"status"`
		RefName string `json:"refName"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/git/pull"
	err := c.do(ctx, http.MethodPost, path, map[string]any{}, &out)
	return out.Status, out.RefName, err
}

// SearchMessages finds message text across visible threads.
func (c *Client) SearchMessages(ctx context.Context, query string, limit int) ([]proto.MessageHit, error) {
	var out struct {
		Messages []proto.MessageHit `json:"messages"`
	}
	path := "/api/v1/search/messages?q=" + url.QueryEscape(query) + "&limit=" + fmt.Sprint(limit)
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Messages, err
}

// Usage summarizes runs over the last days.
func (c *Client) Usage(ctx context.Context, days int) (proto.UsageReport, error) {
	var out proto.UsageReport
	path := "/api/v1/usage?days=" + fmt.Sprint(days)
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// PreviewServers lists the local development servers on the machine.
func (c *Client) PreviewServers(ctx context.Context) ([]proto.PreviewServer, string, error) {
	var out struct {
		Servers []proto.PreviewServer `json:"servers"`
		Detail  string                `json:"detail"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/preview/servers", nil, &out)
	return out.Servers, out.Detail, err
}

// Devices lists the environment's simulators.
func (c *Client) Devices(ctx context.Context) ([]proto.Device, string, error) {
	var out struct {
		Devices []proto.Device `json:"devices"`
		Detail  string         `json:"detail"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/devices", nil, &out)
	return out.Devices, out.Detail, err
}

// BootDevice starts a simulator.
func (c *Client) BootDevice(ctx context.Context, id string) error {
	path := "/api/v1/devices/" + url.PathEscape(id) + "/boot"
	return c.do(ctx, http.MethodPost, path, map[string]any{}, nil)
}

// ShutdownDevice stops a simulator.
func (c *Client) ShutdownDevice(ctx context.Context, id string) error {
	path := "/api/v1/devices/" + url.PathEscape(id) + "/shutdown"
	return c.do(ctx, http.MethodPost, path, map[string]any{}, nil)
}

// DeviceScreenshot reads a simulator's current screen as PNG bytes.
func (c *Client) DeviceScreenshot(ctx context.Context, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/api/v1/devices/"+url.PathEscape(id)+"/screenshot", nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		return nil, &APIError{Status: resp.StatusCode, Code: "device", Message: "could not read the device screen"}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// PullRequestDetail reads one pull request's checks and reviews.
func (c *Client) PullRequestDetail(ctx context.Context, threadID string, number int) (proto.PullRequestDetail, error) {
	var out proto.PullRequestDetail
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/pull-requests/" + fmt.Sprint(number)
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// MergePullRequest merges a pull request.
func (c *Client) MergePullRequest(ctx context.Context, threadID string, number int, method string, deleteBranch bool) error {
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/pull-requests/" + fmt.Sprint(number) + "/merge"
	return c.do(ctx, http.MethodPost, path, map[string]any{"method": method, "deleteBranch": deleteBranch}, nil)
}

// ReviewPullRequest submits a review on a pull request.
func (c *Client) ReviewPullRequest(ctx context.Context, threadID string, number int, verdict, body string) error {
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/pull-requests/" + fmt.Sprint(number) + "/review"
	return c.do(ctx, http.MethodPost, path, map[string]string{"verdict": verdict, "body": body}, nil)
}

// CommentPullRequest adds a conversation comment to a pull request.
func (c *Client) CommentPullRequest(ctx context.Context, threadID string, number int, body string) error {
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/pull-requests/" + fmt.Sprint(number) + "/comments"
	return c.do(ctx, http.MethodPost, path, map[string]string{"body": body}, nil)
}

// PullRequestAction changes a pull request's state.
func (c *Client) PullRequestAction(ctx context.Context, threadID string, number int, action string) error {
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/pull-requests/" + fmt.Sprint(number) + "/action"
	return c.do(ctx, http.MethodPost, path, map[string]string{"action": action}, nil)
}

// PullRequests lists the branch's pull requests.
func (c *Client) PullRequests(ctx context.Context, threadID string) ([]proto.PullRequest, string, error) {
	var out struct {
		PullRequests []proto.PullRequest `json:"pullRequests"`
		Detail       string              `json:"detail"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/pull-requests"
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.PullRequests, out.Detail, err
}

// Branches lists the local branches of a thread's checkout.
func (c *Client) Branches(ctx context.Context, threadID string) ([]proto.Branch, error) {
	var out struct {
		Branches []proto.Branch `json:"branches"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/branches"
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out.Branches, err
}

// SwitchBranch checks out a branch in a thread's checkout.
func (c *Client) SwitchBranch(ctx context.Context, threadID, name string) ([]proto.Branch, error) {
	var out struct {
		Branches []proto.Branch `json:"branches"`
	}
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/branches"
	err := c.do(ctx, http.MethodPost, path, map[string]string{"name": name}, &out)
	return out.Branches, err
}

// WorkingDiff reads a checkout's changes against HEAD.
func (c *Client) WorkingDiff(ctx context.Context, threadID string) (proto.WorkingDiff, error) {
	var out proto.WorkingDiff
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/working-diff"
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// StageFiles adds paths to the index.
func (c *Client) StageFiles(ctx context.Context, threadID string, paths []string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/threads/"+url.PathEscape(threadID)+"/git/stage",
		map[string]any{"paths": paths}, nil)
}

// DiscardFiles restores paths to HEAD, removing untracked files.
func (c *Client) DiscardFiles(ctx context.Context, threadID string, paths []string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/threads/"+url.PathEscape(threadID)+"/git/discard",
		map[string]any{"paths": paths}, nil)
}

// Retry reruns a turn in the same thread.
func (c *Client) Retry(ctx context.Context, threadID, runID string) (proto.Run, error) {
	var out proto.Run
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/retry"
	err := c.do(ctx, http.MethodPost, path, map[string]string{"runId": runID}, &out)
	return out, err
}

// CreateSideChat returns a thread's hidden side chat, creating it first.
func (c *Client) CreateSideChat(ctx context.Context, threadID string) (proto.Thread, error) {
	var thread proto.Thread
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/side-chat"
	err := c.do(ctx, http.MethodPost, path, map[string]any{}, &thread)
	return thread, err
}

// Fork copies a thread into a new one, optionally starting with content.
func (c *Client) Fork(ctx context.Context, threadID, messageID, content string) (proto.Thread, error) {
	var out proto.Thread
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/fork"
	err := c.do(ctx, http.MethodPost, path, map[string]string{"messageId": messageID, "content": content}, &out)
	return out, err
}

// GitAction commits, pushes, or opens a pull request in a thread's
// checkout.
func (c *Client) GitAction(ctx context.Context, threadID string, in proto.GitActionInput) (proto.GitActionResult, error) {
	var out proto.GitActionResult
	path := "/api/v1/threads/" + url.PathEscape(threadID) + "/git"
	err := c.do(ctx, http.MethodPost, path, in, &out)
	return out, err
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
