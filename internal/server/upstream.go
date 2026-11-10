package server

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/coder/websocket"
	"github.com/tk/t3go/internal/proto"
	"github.com/tk/t3go/internal/upstream"
)

// upstreamWriter serializes frames from the request loop and the shell
// subscriber.
type upstreamWriter struct {
	ctx  context.Context
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *upstreamWriter) write(frame any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.Write(w.ctx, websocket.MessageText, data)
}

// forwardShell sends the projects and threads T3Go changes to an upstream
// shell subscriber.
func (s *Server) forwardShell(ctx context.Context, writer *upstreamWriter, requestID json.RawMessage) {
	live, unsubscribe := s.Bus.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-live:
			if !ok {
				return
			}
			item, ok := s.shellItem(event)
			if !ok {
				continue
			}
			if err := writer.write(upstream.Chunk(requestID, []any{item})); err != nil {
				return
			}
		}
	}
}

func (s *Server) shellItem(event proto.Event) (map[string]any, bool) {
	switch event.Type {
	case proto.EventProject:
		if project, ok := decodeEvent[proto.Project](event.Data); ok && project.ID != "" {
			return map[string]any{
				"kind": "project.updated", "sequence": event.Seq,
				"project": upstream.ProjectRow(upstream.Project{
					ID: project.ID, Title: project.Name, WorkspaceRoot: project.Path,
					CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
				}),
			}, true
		}
		if deleted, ok := decodeEvent[map[string]string](event.Data); ok {
			if id := deleted["deleted"]; id != "" {
				return map[string]any{"kind": "project.removed", "sequence": event.Seq, "projectId": id}, true
			}
		}
	case proto.EventThreadCreated, proto.EventThreadUpdated:
		if thread, ok := decodeEvent[proto.Thread](event.Data); ok && thread.ID != "" {
			return map[string]any{
				"kind": "thread.updated", "sequence": event.Seq, "location": location(thread),
				"thread": upstream.ThreadRow(shellThread(thread)),
			}, true
		}
	case proto.EventThreadDeleted:
		if event.ThreadID != "" {
			return map[string]any{"kind": "thread.removed", "sequence": event.Seq, "location": "active", "threadId": event.ThreadID}, true
		}
	}
	return nil, false
}

func location(thread proto.Thread) string {
	if thread.Archived {
		return "archive"
	}
	return "active"
}

func shellThread(thread proto.Thread) upstream.Thread {
	return upstream.Thread{
		ID: thread.ID, ProjectID: thread.ProjectID, Title: thread.Title, Provider: thread.Provider, Model: thread.Model,
		Permission: string(thread.Permission), Branch: thread.Branch, WorktreePath: thread.WorktreePath,
		Status: string(thread.Status), LastError: thread.LastError, Archived: thread.Archived,
		CreatedAt: thread.CreatedAt, UpdatedAt: thread.UpdatedAt,
	}
}

func decodeEvent[T any](data any) (T, bool) {
	var out T
	raw, err := json.Marshal(data)
	if err != nil {
		return out, false
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, false
	}
	return out, true
}
