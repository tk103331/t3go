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
	Plan            bool           `json:"plan,omitempty"`
	Branch          string         `json:"branch,omitempty"`
	WorktreePath    string         `json:"worktreePath,omitempty"`
	Status          ThreadStatus   `json:"status"`
	ProviderSession string         `json:"providerSession,omitempty"`
	Summary         string         `json:"summary,omitempty"`
	Archived        bool           `json:"archived"`
	// ParentID is the thread a side chat belongs to.
	ParentID string `json:"parentId,omitempty"`
	// SideChat hides the thread from lists and gives it handoff context.
	SideChat  bool      `json:"sideChat,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	LastError string    `json:"lastError,omitempty"`
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
	// ContextWindow is the model's window when the provider reported it.
	ContextWindow int64 `json:"contextWindow,omitempty"`
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
	// ContextWindow is how many tokens the model can hold, 0 when unknown.
	ContextWindow int `json:"contextWindow,omitempty"`
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
	// AuthStatus is authenticated, unauthenticated or unknown.
	AuthStatus      string `json:"authStatus,omitempty"`
	AuthDetail      string `json:"authDetail,omitempty"`
	CanInstall      bool   `json:"canInstall,omitempty"`
	CanAuthenticate bool   `json:"canAuthenticate,omitempty"`
	InstallCommand  string `json:"installCommand,omitempty"`
	LoginCommand    string `json:"loginCommand,omitempty"`
}

// MessageHit is a message matching a search.
type MessageHit struct {
	MessageID   string    `json:"messageId"`
	ThreadID    string    `json:"threadId"`
	ThreadTitle string    `json:"threadTitle"`
	ProjectID   string    `json:"projectId,omitempty"`
	ProjectName string    `json:"projectName,omitempty"`
	Role        Role      `json:"role"`
	Snippet     string    `json:"snippet"`
	CreatedAt   time.Time `json:"createdAt"`
}

// UsageTotals is a token and cost total.
type UsageTotals struct {
	Runs   int     `json:"runs"`
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"costUsd"`
}

// UsageDay is one day's usage.
type UsageDay struct {
	Date   string  `json:"date"`
	Runs   int     `json:"runs"`
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"costUsd"`
}

// UsageProvider is one provider's usage in the range.
type UsageProvider struct {
	Provider string  `json:"provider"`
	Runs     int     `json:"runs"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"costUsd"`
}

// UsageReport is the usage of a range of days.
type UsageReport struct {
	Days      []UsageDay      `json:"days"`
	Providers []UsageProvider `json:"providers"`
	Totals    UsageTotals     `json:"totals"`
	DaysCount int             `json:"daysCount"`
	Since     string          `json:"since"`
}

// PreviewServer is a listening local server the app can open.
type PreviewServer struct {
	Port    int    `json:"port"`
	PID     int    `json:"pid,omitempty"`
	Process string `json:"process,omitempty"`
	Address string `json:"address,omitempty"`
	URL     string `json:"url"`
}

// Device is a simulator or emulator the environment can drive.
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	OS       string `json:"os,omitempty"`
	State    string `json:"state"`
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

// TerminalSession is one shell in a thread's checkout.
type TerminalSession struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
	Title    string `json:"title"`
	Shell    string `json:"shell"`
	Cwd      string `json:"cwd"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Exited   bool   `json:"exited,omitempty"`
}

// GraphRef is a branch, remote or tag label on a commit.
type GraphRef struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Current bool   `json:"current,omitempty"`
}

// GraphCommit is one row of the repository's commit graph.
type GraphCommit struct {
	Hash    string     `json:"hash"`
	Short   string     `json:"short"`
	Parents []string   `json:"parents,omitempty"`
	Subject string     `json:"subject"`
	Author  string     `json:"author,omitempty"`
	Date    time.Time  `json:"date"`
	Refs    []GraphRef `json:"refs,omitempty"`
	Head    bool       `json:"head,omitempty"`
}

// DiffFile is one changed path in the working tree.
type DiffFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Staged    bool   `json:"staged,omitempty"`
	Untracked bool   `json:"untracked,omitempty"`
}

// WorkingDiff is the checkout's changes against HEAD.
type WorkingDiff struct {
	Patch string     `json:"patch"`
	Files []DiffFile `json:"files"`
}

// AgentCommand is a workspace skill or custom command an agent CLI can run.
type AgentCommand struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Kind        string                 `json:"kind"`
	Source      string                 `json:"source"`
	Path        string                 `json:"path,omitempty"`
	Arguments   []AgentCommandArgument `json:"arguments,omitempty"`
}

// AgentCommandArgument is one argument an agent command accepts.
type AgentCommandArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// WorkspaceEntry is a directory or file in a thread's checkout.
type WorkspaceEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

// HostDir is a folder on the machine running the environment.
type HostDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// HostDirs is one folder on the environment's machine and its folders.
type HostDirs struct {
	Path    string    `json:"path"`
	Parent  string    `json:"parent,omitempty"`
	Home    string    `json:"home,omitempty"`
	Entries []HostDir `json:"entries"`
}

// WorkspaceFile is one file's contents for the file viewer.
type WorkspaceFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Mime    string `json:"mime,omitempty"`
	Binary  bool   `json:"binary,omitempty"`
	Content string `json:"content,omitempty"`
}

// PullRequest is the open change request for a checkout.
type PullRequest struct {
	URL        string `json:"url"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	BaseBranch string `json:"baseBranch,omitempty"`
	HeadBranch string `json:"headBranch,omitempty"`
	State      string `json:"state,omitempty"`
	Draft      bool   `json:"draft,omitempty"`
}

// Branch is one local or remote branch in a checkout.
type Branch struct {
	Name         string `json:"name"`
	Current      bool   `json:"current,omitempty"`
	Default      bool   `json:"default,omitempty"`
	Remote       bool   `json:"remote,omitempty"`
	RemoteName   string `json:"remoteName,omitempty"`
	UpstreamRef  string `json:"upstreamRef,omitempty"`
	WorktreePath string `json:"worktreePath,omitempty"`
}

// GitAction names a source control operation on a thread's checkout.
type GitAction string

const (
	GitCommit     GitAction = "commit"
	GitPush       GitAction = "push"
	GitCreatePR   GitAction = "create_pr"
	GitCommitPush GitAction = "commit_push"
	GitCommitPR   GitAction = "commit_push_pr"
)

// GitActionInput runs a source control action in a thread's checkout.
type GitActionInput struct {
	Action  GitAction `json:"action"`
	Message string    `json:"message,omitempty"`
	Files   []string  `json:"files,omitempty"`
	Base    string    `json:"base,omitempty"`
}

// GitActionResult reports what a source control action changed.
type GitActionResult struct {
	Action        GitAction `json:"action"`
	Branch        string    `json:"branch,omitempty"`
	CommitSHA     string    `json:"commitSha,omitempty"`
	CommitSubject string    `json:"commitSubject,omitempty"`
	Pushed        bool      `json:"pushed,omitempty"`
	PRURL         string    `json:"prUrl,omitempty"`
	PRNumber      int       `json:"prNumber,omitempty"`
	PRExisted     bool      `json:"prExisted,omitempty"`
	NoChanges     bool      `json:"noChanges,omitempty"`
	Status        GitStatus `json:"status"`
}

// Settings are environment defaults and the UI's preferences.
type Settings struct {
	DefaultProvider   string `json:"defaultProvider"`
	DefaultModel      string `json:"defaultModel"`
	DefaultPermission string `json:"defaultPermission"`
	Theme             string `json:"theme"`
	// Accent is a #rrggbb accent, empty for the theme's own.
	Accent string `json:"accent,omitempty"`
	Title  string `json:"title"`
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
	EventThreadCreated    = "thread.created"
	EventThreadUpdated    = "thread.updated"
	EventThreadDeleted    = "thread.deleted"
	EventMessage          = "message.updated"
	EventMessageDelta     = "message.delta"
	EventRunStarted       = "run.started"
	EventRunFinished      = "run.finished"
	EventProject          = "project.updated"
	EventSettings         = "settings.updated"
	EventApproval         = "approval.requested"
	EventApprovalResolved = "approval.resolved"
	EventNotice           = "notice"
	EventTerminalExit     = "terminal.exit"
)

// ApprovalRequest is a tool call waiting for a person's decision.
type ApprovalRequest struct {
	ID        string         `json:"id"`
	ThreadID  string         `json:"threadId"`
	RunID     string         `json:"runId,omitempty"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// ApprovalDecision is what the person answered, and why when denied.
type ApprovalDecision struct {
	Allow   bool   `json:"allow"`
	Message string `json:"message,omitempty"`
}

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

// PullRequestCheck is one CI check on a pull request.
type PullRequestCheck struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PullRequestReview is one review on a pull request.
type PullRequestReview struct {
	Author string `json:"author,omitempty"`
	State  string `json:"state,omitempty"`
	Body   string `json:"body,omitempty"`
	URL    string `json:"url,omitempty"`
}

// PullRequestComment is one conversation comment on a pull request.
type PullRequestComment struct {
	Author    string `json:"author,omitempty"`
	Body      string `json:"body,omitempty"`
	URL       string `json:"url,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// PullRequestDetail is a pull request with its checks and reviews.
type PullRequestDetail struct {
	PullRequest    PullRequest          `json:"pullRequest"`
	Body           string               `json:"body,omitempty"`
	Author         string               `json:"author,omitempty"`
	Mergeable      string               `json:"mergeable,omitempty"`
	ReviewDecision string               `json:"reviewDecision,omitempty"`
	Additions      int                  `json:"additions,omitempty"`
	Deletions      int                  `json:"deletions,omitempty"`
	ChangedFiles   int                  `json:"changedFiles,omitempty"`
	Checks         []PullRequestCheck   `json:"checks,omitempty"`
	Reviews        []PullRequestReview  `json:"reviews,omitempty"`
	Comments       []PullRequestComment `json:"comments,omitempty"`
}
