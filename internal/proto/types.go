// Package proto holds the types the T3Go server and its clients share.
package proto

import "time"

// ProtocolVersion is the version of the HTTP and WebSocket contract.
const ProtocolVersion = 1

// Project is a directory the server may start threads in.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ThreadStatus is a thread's current activity.
type ThreadStatus string

const (
	ThreadIdle    ThreadStatus = "idle"
	ThreadRunning ThreadStatus = "running"
	ThreadFailed  ThreadStatus = "failed"
)

// PermissionMode is how much an agent may do without asking.
type PermissionMode string

const (
	PermissionSupervised  PermissionMode = "supervised"
	PermissionAcceptEdits PermissionMode = "accept-edits"
	PermissionAuto        PermissionMode = "auto"
	PermissionFullAccess  PermissionMode = "full-access"
)

// Valid reports whether m is a permission mode the server knows.
func (m PermissionMode) Valid() bool {
	switch m {
	case PermissionSupervised, PermissionAcceptEdits, PermissionAuto, PermissionFullAccess:
		return true
	}
	return false
}

// Thread is the durable conversation a user opens in a project.
type Thread struct {
	ID              string         `json:"id"`
	ProjectID       string         `json:"projectId"`
	Title           string         `json:"title"`
	Provider        string         `json:"provider"`
	Model           string         `json:"model"`
	Permission      PermissionMode `json:"permission"`
	Branch          string         `json:"branch,omitempty"`
	WorktreePath    string         `json:"worktreePath,omitempty"`
	Status          ThreadStatus   `json:"status"`
	ProviderSession string         `json:"providerSession,omitempty"`
	Summary         string         `json:"summary,omitempty"`
	Archived        bool           `json:"archived"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	LastError       string         `json:"lastError,omitempty"`
}

// MessageKind says how a message shows in the transcript.
type MessageKind string

const (
	KindText      MessageKind = "text"
	KindReasoning MessageKind = "reasoning"
	KindTool      MessageKind = "tool"
	KindNotice    MessageKind = "notice"
	KindError     MessageKind = "error"
)

// Role is who wrote a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Attachment is a file uploaded to the environment and sent with a
// prompt, which providers read from its path on disk.
type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// Message is one transcript entry. A running assistant message grows as
// its provider streams tokens.
type Message struct {
	ID          string       `json:"id"`
	ThreadID    string       `json:"threadId"`
	RunID       string       `json:"runId,omitempty"`
	Role        Role         `json:"role"`
	Kind        MessageKind  `json:"kind"`
	Content     string       `json:"content,omitempty"`
	Title       string       `json:"title,omitempty"`
	Detail      string       `json:"detail,omitempty"`
	Status      string       `json:"status,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
}

// RunStatus is one user-to-agent cycle's state.
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Usage is the provider's report of a run's token use.
type Usage struct {
	InputTokens  int64   `json:"inputTokens,omitempty"`
	OutputTokens int64   `json:"outputTokens,omitempty"`
	TotalTokens  int64   `json:"totalTokens,omitempty"`
	CostUSD      float64 `json:"costUsd,omitempty"`
}

// Run is one turn: a user prompt and the work it caused.
type Run struct {
	ID         string         `json:"id"`
	ThreadID   string         `json:"threadId"`
	Status     RunStatus      `json:"status"`
	Provider   string         `json:"provider"`
	Model      string         `json:"model"`
	Permission PermissionMode `json:"permission"`
	Prompt     string         `json:"prompt,omitempty"`
	StartedAt  time.Time      `json:"startedAt"`
	EndedAt    *time.Time     `json:"endedAt,omitempty"`
	Error      string         `json:"error,omitempty"`
	Checkpoint string         `json:"checkpoint,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
}

// Model is one agent model a provider offers.
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Default  bool   `json:"default,omitempty"`
	Custom   bool   `json:"custom,omitempty"`
}

// Provider is an agent CLI the server can run.
type Provider struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Installed bool    `json:"installed"`
	Version   string  `json:"version,omitempty"`
	Path      string  `json:"path,omitempty"`
	Error     string  `json:"error,omitempty"`
	Models    []Model `json:"models,omitempty"`
}

// GitStatus is the checkout a thread works in.
type GitStatus struct {
	Repo       bool   `json:"repo"`
	Root       string `json:"root,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Dirty      bool   `json:"dirty"`
	Detached   bool   `json:"detached,omitempty"`
	Ahead      int    `json:"ahead,omitempty"`
	Behind     int    `json:"behind,omitempty"`
	Error      string `json:"error,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
	DiffStat   string `json:"diffStat,omitempty"`
}

// Settings are environment defaults and the UI's preferences.
type Settings struct {
	DefaultProvider   string `json:"defaultProvider"`
	DefaultModel      string `json:"defaultModel"`
	DefaultPermission string `json:"defaultPermission"`
	Theme             string `json:"theme"`
	Title             string `json:"title"`
}

// DefaultSettings are a new environment's defaults.
func DefaultSettings() Settings {
	return Settings{DefaultProvider: "claude", DefaultPermission: string(PermissionFullAccess), Theme: "system", Title: "T3Go"}
}

// State is everything a client needs to render the home screen.
type State struct {
	Version int `json:"version"`
	// Seq is the newest event the environment recorded when it built the
	// snapshot; a client streams after it to catch what changed since.
	Seq        int64      `json:"seq"`
	ServerID   string     `json:"serverId"`
	Name       string     `json:"name"`
	Projects   []Project  `json:"projects"`
	Threads    []Thread   `json:"threads"`
	Providers  []Provider `json:"providers"`
	Models     []Model    `json:"models"`
	Settings   Settings   `json:"settings"`
	ServerTime time.Time  `json:"serverTime"`
}

// ThreadDetail is one thread and its history.
type ThreadDetail struct {
	Thread   Thread     `json:"thread"`
	Messages []Message  `json:"messages"`
	Runs     []Run      `json:"runs"`
	Git      *GitStatus `json:"git,omitempty"`
}

// Event is a server change pushed to connected clients.
type Event struct {
	Seq       int64     `json:"seq"`
	Type      string    `json:"type"`
	ThreadID  string    `json:"threadId,omitempty"`
	ProjectID string    `json:"projectId,omitempty"`
	Data      any       `json:"data,omitempty"`
	At        time.Time `json:"at"`
}

// Event types.
const (
	EventThreadCreated = "thread.created"
	EventThreadUpdated = "thread.updated"
	EventThreadDeleted = "thread.deleted"
	EventMessage       = "message.updated"
	EventMessageDelta  = "message.delta"
	EventRunStarted    = "run.started"
	EventRunFinished   = "run.finished"
	EventProject       = "project.updated"
	EventSettings      = "settings.updated"
	EventNotice        = "notice"
)

// MessageDelta is an event's payload for a streaming assistant message.
type MessageDelta struct {
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
}

// WSFrame is a WebSocket message in either direction.
type WSFrame struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Method   string `json:"method,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	Result   any    `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
	Event    *Event `json:"event,omitempty"`
	Protocol int    `json:"protocol,omitempty"`
	ServerID string `json:"serverId,omitempty"`
}

// WebSocket frame types.
const (
	WSHello       = "hello"
	WSRequest     = "request"
	WSResponse    = "response"
	WSEvent       = "event"
	WSSubscribe   = "subscribe"
	WSUnsubscribe = "unsubscribe"
	WSPing        = "ping"
	WSPong        = "pong"
)
