package store

import (
	"context"

	"github.com/tk/t3go/internal/proto"
)

// SetProviderSession and FinishThread update only run-owned fields. A running
// provider holds an earlier snapshot and must not overwrite a later model
// selection, rename, archive or explicit settlement.
func (s *Store) SetProviderSession(ctx context.Context, id, provider, session string) (proto.Thread, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE threads SET provider_session=?,updated_at=? WHERE id=? AND provider=?`,
		session, now(), id, provider)
	if err != nil {
		return proto.Thread{}, err
	}
	return s.Thread(ctx, id)
}

func (s *Store) FinishThread(ctx context.Context, id string, status proto.ThreadStatus, lastError string) (proto.Thread, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE threads SET status=?,last_error=?,updated_at=? WHERE id=?`,
		status, lastError, now(), id)
	if err != nil {
		return proto.Thread{}, err
	}
	return s.Thread(ctx, id)
}
