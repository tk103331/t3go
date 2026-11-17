package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Device is one paired client. It holds the digest of the token that
// client presents, never the token, so a revoked grant cannot be replayed
// from the database.
type Device struct {
	ID         string
	Label      string
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  time.Time
}

// Revoked reports whether the grant no longer authenticates.
func (d Device) Revoked() bool { return !d.RevokedAt.IsZero() }

// CreateDevice stores a grant and the digest of its token.
func (s *Store) CreateDevice(ctx context.Context, d Device, digest string) error {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices(id,label,token_digest,created_at,last_seen_at,revoked_at)
		VALUES(?,?,?,?,'','')`, d.ID, d.Label, digest, stamp(d.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create device: %w", err)
	}
	return nil
}

// Devices lists the environment's grants, oldest first, revoked ones
// included so their history stays readable.
func (s *Store) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,label,created_at,last_seen_at,revoked_at
		FROM devices ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: devices: %w", err)
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var created, seen, revoked string
		if err := rows.Scan(&d.ID, &d.Label, &created, &seen, &revoked); err != nil {
			return nil, err
		}
		d.CreatedAt, d.LastSeenAt, d.RevokedAt = parseTime(created), parseTime(seen), parseTime(revoked)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeviceByDigest returns the active grant a token digest belongs to.
func (s *Store) DeviceByDigest(ctx context.Context, digest string) (Device, error) {
	var d Device
	var created, seen, revoked string
	err := s.db.QueryRowContext(ctx, `SELECT id,label,created_at,last_seen_at,revoked_at
		FROM devices WHERE token_digest=? AND revoked_at=''`, digest).
		Scan(&d.ID, &d.Label, &created, &seen, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, fmt.Errorf("store: device: %w", err)
	}
	d.CreatedAt, d.LastSeenAt, d.RevokedAt = parseTime(created), parseTime(seen), parseTime(revoked)
	return d, nil
}

// ActiveDevice returns a grant that still authenticates.
func (s *Store) ActiveDevice(ctx context.Context, id string) (Device, error) {
	var d Device
	var created, seen, revoked string
	err := s.db.QueryRowContext(ctx, `SELECT id,label,created_at,last_seen_at,revoked_at
		FROM devices WHERE id=? AND revoked_at=''`, id).Scan(&d.ID, &d.Label, &created, &seen, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, fmt.Errorf("store: device: %w", err)
	}
	d.CreatedAt, d.LastSeenAt, d.RevokedAt = parseTime(created), parseTime(seen), parseTime(revoked)
	return d, nil
}

// TouchDevice records when a grant last authenticated.
func (s *Store) TouchDevice(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE devices SET last_seen_at=? WHERE id=? AND revoked_at=''`, stamp(at), id)
	if err != nil {
		return fmt.Errorf("store: touch device: %w", err)
	}
	return nil
}

// RevokeDevice stops a grant from authenticating. Revoking twice keeps the
// first time.
func (s *Store) RevokeDevice(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET revoked_at=? WHERE id=? AND revoked_at=''`, stamp(at), id)
	if err != nil {
		return fmt.Errorf("store: revoke device: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM devices WHERE id=?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("store: revoke device: %w", err)
	}
	return nil
}

// PruneDevices forgets grants revoked before the given time.
func (s *Store) PruneDevices(ctx context.Context, before time.Time) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE revoked_at<>'' AND revoked_at<?`, stamp(before)); err != nil {
		return fmt.Errorf("store: prune devices: %w", err)
	}
	return nil
}

// stamp formats a time for a device row, and nothing for a zero one.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
