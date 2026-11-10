package upstream

import (
	"encoding/json"
	"strings"
	"time"
)

// Message is one transcript row the compatibility layer projects.
type Message struct {
	ID          string
	ThreadID    string
	RunID       string
	Role        string
	Kind        string
	Title       string
	Detail      string
	Content     string
	Status      string
	Attachments []Attachment
	CreatedAt   string
}

// Attachment is a file the message carries.
type Attachment struct {
	ID       string
	Name     string
	MimeType string
	Size     int64
}

// AttachmentRow is one upstream ChatAttachment.
func AttachmentRow(a Attachment) map[string]any {
	kind := "file"
	if strings.HasPrefix(strings.ToLower(a.MimeType), "image/") {
		kind = "image"
	}
	return map[string]any{
		"type": kind, "id": a.ID, "name": nonEmpty(a.Name, a.ID),
		"mimeType": nonEmpty(a.MimeType, "application/octet-stream"), "sizeBytes": a.Size,
	}
}

// AttachmentRows maps T3Go attachments to upstream ChatAttachments.
func AttachmentRows(attachments []Attachment) []any {
	return attachmentRows(attachments)
}

func attachmentRows(attachments []Attachment) []any {
	rows := make([]any, 0, len(attachments))
	for _, a := range attachments {
		rows = append(rows, AttachmentRow(a))
	}
	return rows
}

// Run is one agent turn as the upstream run projection carries it.
type Run struct {
	ID            string
	ThreadID      string
	UserMessageID string
	Provider      string
	Model         string
	Status        string
	StartedAt     string
	EndedAt       string
}

// RunRow is one upstream run.
func RunRow(r Run, ordinal int) map[string]any {
	status := "completed"
	switch r.Status {
	case "running":
		status = "running"
	case "failed":
		status = "failed"
	case "cancelled":
		status = "cancelled"
	}
	row := map[string]any{
		"id":                 r.ID,
		"threadId":           r.ThreadID,
		"ordinal":            ordinal,
		"providerInstanceId": r.Provider,
		"modelSelection":     map[string]any{"instanceId": r.Provider, "model": r.Model},
		"providerThreadId":   nil,
		"userMessageId":      nonEmpty(r.UserMessageID, "msg_"+r.ID),
		"rootNodeId":         nil,
		"activeAttemptId":    nil,
		"status":             status,
		"requestedAt":        nonEmpty(r.StartedAt, isoTimestamp(TimeNow())),
		"startedAt":          nullable(r.StartedAt),
		"completedAt":        nullable(r.EndedAt),
		"checkpointId":       nil,
		"contextHandoffId":   nil,
	}
	return row
}

// AppThread is the upstream thread object a detail stream carries.
func AppThread(t Thread) map[string]any {
	return map[string]any{
		"createdBy":              "user",
		"creationSource":         "web",
		"id":                     t.ID,
		"projectId":              t.ProjectID,
		"title":                  t.Title,
		"providerInstanceId":     t.Provider,
		"modelSelection":         map[string]any{"instanceId": t.Provider, "model": t.Model},
		"runtimeMode":            runtimeMode(t.Permission),
		"interactionMode":        "default",
		"branch":                 nullable(t.Branch),
		"worktreePath":           nullable(t.WorktreePath),
		"activeProviderThreadId": nil,
		"lineage":                map[string]any{"parentThreadId": nil, "relationshipToParent": nil, "rootThreadId": t.ID},
		"forkedFrom":             nil,
		"createdAt":              isoTimestamp(t.CreatedAt),
		"updatedAt":              isoTimestamp(t.UpdatedAt),
		"archivedAt":             archivedAt(t),
		"settledOverride":        nil,
		"settledAt":              nil,
		"lastVisitedAt":          nil,
		"deletedAt":              nil,
	}
}

// MessageRow is one upstream conversation message.
func MessageRow(m Message) map[string]any {
	createdBy, creationSource := "agent", "provider"
	role := "assistant"
	if m.Role == "user" {
		createdBy, creationSource, role = "user", "web", "user"
	} else if m.Role == "system" {
		createdBy, role = "system", "system"
	}
	created := m.CreatedAt
	if created == "" {
		created = isoTimestamp(TimeNow())
	}
	return map[string]any{
		"createdBy":      createdBy,
		"creationSource": creationSource,
		"id":             m.ID,
		"threadId":       m.ThreadID,
		"runId":          nullable(m.RunID),
		"nodeId":         nil,
		"role":           role,
		"text":           m.Content,
		"attachments":    attachmentRows(m.Attachments),
		"streaming":      m.Status == "running",
		"createdAt":      created,
		"updatedAt":      created,
	}
}

// TurnItem is the upstream turn item for a text message, at ordinal.
func TurnItem(m Message, ordinal int) map[string]any {
	status := "completed"
	switch m.Status {
	case "running":
		status = "running"
	case "failed":
		status = "failed"
	case "cancelled":
		status = "cancelled"
	}
	created := m.CreatedAt
	if created == "" {
		created = isoTimestamp(TimeNow())
	}
	item := map[string]any{
		"id":               "ti_" + m.ID,
		"threadId":         m.ThreadID,
		"runId":            nullable(m.RunID),
		"nodeId":           nil,
		"providerThreadId": nil,
		"providerTurnId":   nil,
		"nativeItemRef":    nil,
		"parentItemId":     nil,
		"ordinal":          ordinal,
		"status":           status,
		"title":            nil,
		"startedAt":        nil,
		"completedAt":      created,
		"updatedAt":        created,
	}
	switch {
	case m.Role == "user":
		item["type"] = "user_message"
		item["createdBy"] = "user"
		item["creationSource"] = "web"
		item["messageId"] = m.ID
		item["inputIntent"] = "turn_start"
		item["text"] = m.Content
		item["attachments"] = attachmentRows(m.Attachments)
	case m.Kind == "reasoning":
		item["type"] = "reasoning"
		item["text"] = m.Content
		item["streaming"] = m.Status == "running"
	case m.Kind == "tool":
		item["type"] = "command_execution"
		input := m.Title
		if input == "" {
			input = m.Detail
		}
		if input == "" {
			input = "Tool"
		}
		item["input"] = input
		if m.Content != "" {
			item["output"] = m.Content
		}
		if m.Status == "failed" {
			item["outputIndicatesFailure"] = true
		}
	case m.Kind == "text":
		item["type"] = "assistant_message"
		item["messageId"] = m.ID
		item["text"] = m.Content
		item["streaming"] = m.Status == "running"
	default:
		return nil
	}
	return item
}

// ThreadProjection is the upstream thread detail snapshot.
func ThreadProjection(t Thread, messages []Message, runs []Run) map[string]any {
	rows := make([]any, 0, len(messages))
	items := make([]any, 0, len(messages))
	visible := make([]any, 0, len(messages))
	ordinal := 0
	for _, m := range messages {
		if !IsProjected(m) {
			continue
		}
		if m.Role == "user" || (m.Role == "assistant" && m.Kind == "text") {
			rows = append(rows, MessageRow(m))
		}
		item := TurnItem(m, ordinal)
		if item == nil {
			continue
		}
		items = append(items, item)
		visible = append(visible, map[string]any{
			"position": ordinal, "visibility": "local", "sourceThreadId": m.ThreadID,
			"sourceItemId": item["id"], "item": item,
		})
		ordinal++
	}
	return map[string]any{
		"thread":           AppThread(t),
		"runs":             runRows(runs),
		"attempts":         []any{},
		"nodes":            []any{},
		"subagents":        []any{},
		"providerSessions": []any{},
		"providerThreads":  []any{},
		"providerTurns":    []any{},
		"runtimeRequests":  []any{},
		"messages":         rows,
		"plans":            []any{},
		"turnItems":        items,
		"checkpointScopes": []any{},
		"checkpoints":      []any{},
		"contextHandoffs":  []any{},
		"contextTransfers": []any{},
		"visibleTurnItems": visible,
		"updatedAt":        isoTimestamp(t.UpdatedAt),
	}
}

// ThreadSnapshot is the first item of subscribeThread.
func ThreadSnapshot(t Thread, messages []Message, runs []Run) map[string]any {
	return map[string]any{
		"kind": "snapshot", "snapshotSequence": 0,
		"projection": ThreadProjection(t, messages, runs),
	}
}

// MessageUpdated is a thread stream event for a changed message.
func MessageUpdated(sequence int64, m Message, ordinal int) map[string]any {
	created := m.CreatedAt
	if created == "" {
		created = isoTimestamp(TimeNow())
	}
	event := map[string]any{
		"id":         eventID(sequence),
		"threadId":   m.ThreadID,
		"occurredAt": created,
		"type":       "message.updated",
		"payload":    MessageRow(m),
	}
	return map[string]any{"kind": "event", "sequence": sequence, "event": event}
}

// Diff is the upstream turn-diff result.
func Diff(threadID string, fromTurnCount, toTurnCount int, diff string) map[string]any {
	return map[string]any{
		"fromTurnCount": fromTurnCount,
		"toTurnCount":   toTurnCount,
		"threadId":      threadID,
		"diff":          diff,
	}
}

// TypedFailure answers a request with one of the method's tagged errors.
func TypedFailure(id json.RawMessage, tag, message string) map[string]any {
	return map[string]any{"_tag": "Exit", "requestId": id, "exit": map[string]any{
		"_tag": "Failure",
		"cause": []any{map[string]any{"_tag": "Fail", "error": map[string]any{
			"_tag": tag, "message": message,
		}}},
	}}
}

// RunUpdated is a thread stream event for a changed run.
func RunUpdated(sequence int64, r Run, ordinal int, created bool) map[string]any {
	kind := "run.updated"
	if created {
		kind = "run.created"
	}
	event := map[string]any{
		"id":         eventID(sequence),
		"threadId":   r.ThreadID,
		"occurredAt": nonEmpty(r.StartedAt, isoTimestamp(TimeNow())),
		"type":       kind,
		"payload":    RunRow(r, ordinal),
	}
	return map[string]any{"kind": "event", "sequence": sequence, "event": event}
}

// TurnItemUpdated is a thread stream event for a changed turn item.
func TurnItemUpdated(sequence int64, m Message, ordinal int) map[string]any {
	created := m.CreatedAt
	if created == "" {
		created = isoTimestamp(TimeNow())
	}
	event := map[string]any{
		"id":         eventID(sequence + 1),
		"threadId":   m.ThreadID,
		"occurredAt": created,
		"type":       "turn-item.updated",
		"payload":    TurnItem(m, ordinal),
	}
	return map[string]any{"kind": "event", "sequence": sequence + 1, "event": event}
}

func runRows(runs []Run) []any {
	rows := make([]any, 0, len(runs))
	for i, r := range runs {
		rows = append(rows, RunRow(r, i+1))
	}
	return rows
}

func eventID(sequence int64) string {
	return "evt_" + isoTimestamp(TimeNow()) + "_" + itoa(sequence)
}

func TimeNow() time.Time { return time.Now() }

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[i:])
}

// IsProjected reports whether a message becomes an upstream turn item.
func IsProjected(m Message) bool {
	if m.Content == "" {
		return false
	}
	if m.Role == "user" {
		return m.Kind == "text"
	}
	if m.Role != "assistant" {
		return false
	}
	switch m.Kind {
	case "text", "reasoning", "tool":
		return true
	}
	return false
}

// GitFile is one changed path in a status.
type GitFile struct {
	Path                  string
	Insertions, Deletions int
}

// StatusLocal is the VcsStatusLocalResult shape.
func StatusLocal(isRepo bool, root, ref string, hasPrimaryRemote, isDefaultRef bool, files []GitFile, insertions, deletions int) map[string]any {
	working := make([]any, 0, len(files))
	for _, f := range files {
		working = append(working, map[string]any{"path": f.Path, "insertions": f.Insertions, "deletions": f.Deletions})
	}
	local := map[string]any{
		"isRepo": isRepo, "hasPrimaryRemote": hasPrimaryRemote, "isDefaultRef": isDefaultRef,
		"refName": nullable(ref), "hasWorkingTreeChanges": len(files) > 0,
		"workingTree": map[string]any{"rootPath": nonEmpty(root, "/"), "files": working,
			"insertions": insertions, "deletions": deletions},
	}
	return local
}

// StatusResult is the VcsStatusResult shape.
func StatusResult(local map[string]any, hasUpstream bool, ahead, behind int) map[string]any {
	local["hasUpstream"] = hasUpstream
	local["aheadCount"] = ahead
	local["behindCount"] = behind
	local["pr"] = nil
	return local
}

// StatusSnapshot is the first VcsStatusStreamEvent.
func StatusSnapshot(local map[string]any, hasUpstream bool, ahead, behind int) map[string]any {
	return map[string]any{
		"_tag":   "snapshot",
		"local":  local,
		"remote": map[string]any{"hasUpstream": hasUpstream, "aheadCount": ahead, "behindCount": behind, "pr": nil},
	}
}
