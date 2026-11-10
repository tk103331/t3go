// Package upstream speaks the T3 Code environment protocol: the discovery
// descriptor and its JSON RPC frames over a WebSocket. It is a compatibility
// layer over T3Go's own API, not a second implementation of the upstream
// server.
package upstream

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// ProtocolVersion is the orchestration protocol this layer answers.
const ProtocolVersion = 2

// Project and Thread are the shell rows the compatibility layer lists.
// They are read-only projections of T3Go's own records.
type Project struct {
	ID            string
	Title         string
	WorkspaceRoot string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Thread is one thread in the upstream shell.
type Thread struct {
	ID           string
	ProjectID    string
	Title        string
	Provider     string
	Model        string
	Permission   string
	Branch       string
	WorktreePath string
	Status       string
	LastError    string
	Archived     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Environment is what the compatibility layer needs from the server.
type Environment struct {
	ID      string
	Label   string
	Version string
	Home    string
	CWD     string
	LogsDir string
	// Token lets a client authenticate as this environment. Empty accepts
	// no bearer credential.
	Token    string
	Projects []Project
	Threads  []Thread
}

// Descriptor is the .well-known/t3/environment answer.
func Descriptor(env Environment) map[string]any {
	arch := "other"
	switch runtime.GOARCH {
	case "arm64":
		arch = "arm64"
	case "amd64":
		arch = "x64"
	}
	osName := runtime.GOOS
	switch osName {
	case "darwin", "linux", "windows":
	default:
		osName = "unknown"
	}
	return map[string]any{
		"environmentId": env.ID,
		"label":         env.Label,
		"platform": map[string]any{
			"os":   osName,
			"arch": arch,
		},
		"serverVersion":                env.Version,
		"orchestrationProtocolVersion": ProtocolVersion,
		"capabilities":                 map[string]any{},
	}
}

// Config is a minimal ServerConfig: the environment and its defaults, with
// no providers yet. Upstream clients decode it with the fields they know and
// ignore the rest.
func Config(env Environment) map[string]any {
	return map[string]any{
		"environment": Descriptor(env),
		"auth": map[string]any{
			"policy":            "loopback-browser",
			"bootstrapMethods":  []string{"one-time-token"},
			"sessionMethods":    []string{"bearer-access-token"},
			"sessionCookieName": "t3_session",
		},
		"cwd":                   env.CWD,
		"keybindingsConfigPath": env.Home + "/keybindings.json",
		"keybindings":           []any{},
		"issues":                []any{},
		"providers":             []any{},
		"availableEditors":      []any{},
		"observability": map[string]any{
			"logsDirectoryPath":   env.LogsDir,
			"localTracingEnabled": false,
			"otlpTracesEnabled":   false,
			"otlpMetricsEnabled":  false,
			"otlpLogsEnabled":     false,
		},
		"settings": map[string]any{},
	}
}

// ShellSnapshot lists the environment's projects and threads in the v2
// shell shapes. Threads are shell rows only: this layer has no transcript
// or run projection yet.
func ShellSnapshot(env Environment) map[string]any {
	projects := make([]any, 0, len(env.Projects))
	for _, p := range env.Projects {
		projects = append(projects, map[string]any{
			"id":                    p.ID,
			"title":                 nonEmpty(p.Title, p.ID),
			"workspaceRoot":         nonEmpty(p.WorkspaceRoot, "/"),
			"defaultModelSelection": nil,
			"scripts":               []any{},
			"createdAt":             isoTimestamp(p.CreatedAt),
			"updatedAt":             isoTimestamp(p.UpdatedAt),
		})
	}
	threads := make([]any, 0, len(env.Threads))
	archived := make([]any, 0)
	for _, t := range env.Threads {
		row := threadShell(t)
		if t.Archived {
			archived = append(archived, row)
			continue
		}
		threads = append(threads, row)
	}
	return map[string]any{
		"schemaVersion":    1,
		"snapshotSequence": 0,
		"threads":          threads,
		"archivedThreads":  archived,
		"projects":         projects,
	}
}

func threadShell(t Thread) map[string]any {
	status := "idle"
	switch t.Status {
	case "running":
		status = "running"
	case "failed":
		status = "failed"
	}
	row := map[string]any{
		"createdBy":                 "user",
		"creationSource":            "web",
		"id":                        t.ID,
		"projectId":                 t.ProjectID,
		"title":                     t.Title,
		"providerInstanceId":        t.Provider,
		"modelSelection":            map[string]any{"instanceId": t.Provider, "model": t.Model},
		"runtimeMode":               runtimeMode(t.Permission),
		"interactionMode":           "default",
		"branch":                    nullable(t.Branch),
		"worktreePath":              nullable(t.WorktreePath),
		"lineage":                   map[string]any{"parentThreadId": nil, "relationshipToParent": nil, "rootThreadId": t.ID},
		"forkedFrom":                nil,
		"activeProviderThreadId":    nil,
		"latestRunId":               nil,
		"activeRunId":               nil,
		"status":                    status,
		"pendingRuntimeRequest":     nil,
		"latestVisibleMessage":      nil,
		"latestUserMessageAt":       nil,
		"hasActionableProposedPlan": false,
		"itemCount":                 0,
		"visibleItemCount":          0,
		"createdAt":                 isoTimestamp(t.CreatedAt),
		"updatedAt":                 isoTimestamp(t.UpdatedAt),
		"archivedAt":                archivedAt(t),
		"settledOverride":           nil,
		"settledAt":                 nil,
		"deletedAt":                 nil,
	}
	if t.LastError != "" {
		row["lastError"] = t.LastError
	}
	return row
}

func runtimeMode(permission string) string {
	switch permission {
	case "supervised":
		return "approval-required"
	case "accept-edits":
		return "auto-accept-edits"
	case "auto":
		return "auto"
	default:
		return "full-access"
	}
}

func archivedAt(t Thread) any {
	if !t.Archived {
		return nil
	}
	return isoTimestamp(t.UpdatedAt)
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isoTimestamp(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// Welcome is the first lifecycle event.
func Welcome(env Environment, projectName string) map[string]any {
	return map[string]any{
		"version":  1,
		"sequence": 0,
		"type":     "welcome",
		"payload": map[string]any{
			"environment": Descriptor(env),
			"cwd":         env.CWD,
			"projectName": projectName,
		},
	}
}

// Request is one JSON RPC request frame.
type Request struct {
	Tag     string          `json:"tag"`
	ID      json.RawMessage `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

// Exit answers a request with a value.
func Exit(id json.RawMessage, value any) map[string]any {
	return map[string]any{"_tag": "Exit", "requestId": id, "exit": map[string]any{"_tag": "Success", "value": value}}
}

// Chunk carries values of a streaming answer.
func Chunk(id json.RawMessage, values []any) map[string]any {
	return map[string]any{"_tag": "Chunk", "requestId": id, "values": values}
}

// UnknownFailure answers a method this layer does not implement, as a
// defect the client logs instead of mistaking for a method's typed error.
func UnknownFailure(id json.RawMessage, message string) map[string]any {
	return map[string]any{"_tag": "Exit", "requestId": id, "exit": map[string]any{
		"_tag":  "Failure",
		"cause": []any{map[string]any{"_tag": "Die", "defect": message}},
	}}
}

// Failure answers a request with an error.
func Failure(id json.RawMessage, kind, message string) map[string]any {
	return map[string]any{"_tag": "Exit", "requestId": id, "exit": map[string]any{
		"_tag": "Failure",
		"cause": []any{map[string]any{"_tag": "Fail", "error": map[string]any{
			"_tag": kind, "message": message,
		}}},
	}}
}

// Handle answers one request, reporting whether it opened a stream.
func Handle(env Environment, request Request) (frames []map[string]any, stream bool, known bool) {
	id := request.ID
	switch request.Tag {
	case "server.probe":
		return []map[string]any{Exit(id, map[string]any{})}, false, true
	case "server.getConfig":
		return []map[string]any{Exit(id, Config(env))}, false, true
	case "subscribeServerConfig":
		return []map[string]any{Chunk(id, []any{map[string]any{
			"version": 1, "type": "snapshot", "config": Config(env),
		}})}, true, true
	case "orchestration.subscribeShell":
		return []map[string]any{Chunk(id, []any{
			map[string]any{"kind": "snapshot", "snapshot": ShellSnapshot(env)},
			map[string]any{"kind": "synchronized"},
		})}, true, true
	case "subscribeServerLifecycle":
		return []map[string]any{Chunk(id, []any{Welcome(env, "T3Go")})}, true, true
	case "subscribeAuthAccess":
		return []map[string]any{Chunk(id, []any{})}, true, true
	default:
		return nil, false, false
	}
}

// Marshal helpers keep the handler free of JSON details.
func Marshal(value any) ([]byte, error) { return json.Marshal(value) }

// Unknown is the message a client sees for a method this layer lacks.
func Unknown(tag string) string {
	return fmt.Sprintf("t3go compatibility: %s is not implemented yet", tag)
}
