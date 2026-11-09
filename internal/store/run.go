package store

import "context"

// SetRunCheckpoint records a run's Git checkpoint ref.
func (s *Store) SetRunCheckpoint(ctx context.Context, runID, ref string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET checkpoint=? WHERE id=?`, ref, runID)
	return err
}
