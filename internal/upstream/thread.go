package upstream

import "time"

// Message is one transcript row the compatibility layer projects.
type Message struct {
	ID        string
	ThreadID  string
	RunID     string
	Role      string
	Kind      string
	Content   string
	Status    string
	CreatedAt string
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
		"attachments":    []any{},
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
	role := "assistant"
	if m.Role == "user" {
		role = "user"
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
	if role == "user" {
		item["type"] = "user_message"
		item["createdBy"] = "user"
		item["creationSource"] = "web"
		item["messageId"] = m.ID
		item["inputIntent"] = "turn_start"
		item["text"] = m.Content
		item["attachments"] = []any{}
		return item
	}
	item["type"] = "assistant_message"
	item["messageId"] = m.ID
	item["text"] = m.Content
	item["streaming"] = m.Status == "running"
	return item
}

// ThreadProjection is the upstream thread detail snapshot.
func ThreadProjection(t Thread, messages []Message) map[string]any {
	rows := make([]any, 0, len(messages))
	items := make([]any, 0, len(messages))
	visible := make([]any, 0, len(messages))
	ordinal := 0
	for _, m := range messages {
		if !isText(m) {
			continue
		}
		row := MessageRow(m)
		item := TurnItem(m, ordinal)
		rows = append(rows, row)
		items = append(items, item)
		visible = append(visible, map[string]any{
			"position": ordinal, "visibility": "local", "sourceThreadId": m.ThreadID,
			"sourceItemId": item["id"], "item": item,
		})
		ordinal++
	}
	return map[string]any{
		"thread":           AppThread(t),
		"runs":             []any{},
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
func ThreadSnapshot(t Thread, messages []Message) map[string]any {
	return map[string]any{
		"kind": "snapshot", "snapshotSequence": 0,
		"projection": ThreadProjection(t, messages),
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

func isText(m Message) bool {
	return m.Content != "" && (m.Role == "user" || (m.Role == "assistant" && m.Kind == "text"))
}
