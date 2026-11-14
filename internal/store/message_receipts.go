package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/tk/t3go/internal/proto"
)

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// StartTurn commits the run, transcript, thread state and optional receipt as
// one unit. A disconnected HTTP request cannot leave a partially recorded turn.
func (s *Store) StartTurn(ctx context.Context, thread proto.Thread, run proto.Run, messages []proto.Message, requestID, payloadHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := createRun(ctx, tx, run); err != nil {
		return err
	}
	for _, message := range messages {
		if _, err := appendMessage(ctx, tx, message); err != nil {
			return err
		}
	}
	if err := updateThread(ctx, tx, thread); err != nil {
		return err
	}
	if requestID != "" {
		// Deliberately no foreign key to runs: restoring/deleting history must
		// not make an already accepted request executable again.
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_receipts(thread_id,request_id,payload_hash,run_id,accepted_at)
			VALUES(?,?,?,?,?)`, thread.ID, requestID, payloadHash, run.ID, run.StartedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MessageReceipt(ctx context.Context, threadID, requestID string) (proto.MessageReceipt, string, error) {
	receipt := proto.MessageReceipt{RequestID: requestID}
	var hash, accepted string
	err := s.db.QueryRowContext(ctx, `SELECT payload_hash,run_id,accepted_at FROM message_receipts
		WHERE thread_id=? AND request_id=?`, threadID, requestID).Scan(&hash, &receipt.RunID, &accepted)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, "", ErrNotFound
	}
	receipt.AcceptedAt = parseTime(accepted)
	return receipt, hash, err
}

func (s *Store) Run(ctx context.Context, runID string) (proto.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,thread_id,status,provider,model,permission,prompt,started_at,ended_at,error,checkpoint,usage
		FROM runs WHERE id=?`, runID)
	if err != nil {
		return proto.Run{}, err
	}
	defer rows.Close()
	runs, err := scanRuns(rows)
	if err != nil {
		return proto.Run{}, err
	}
	if len(runs) == 0 {
		return proto.Run{}, ErrNotFound
	}
	return runs[0], nil
}
