package store

import (
	"context"
	"crypto/ed25519"
	"fmt"

	"github.com/tk/t3go/internal/environment"
)

// EnvironmentIdentity creates the identity transactionally, including when two
// processes first open the same database. Restarting preserves it.
func (s *Store) EnvironmentIdentity(ctx context.Context) (*environment.Identity, error) {
	generated, err := environment.Generate()
	if err != nil {
		return nil, err
	}
	if _, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO environment_identity(singleton,id,private_key) VALUES(1,?,?)`,
		generated.ID, []byte(generated.PrivateKey)); err != nil {
		return nil, err
	}
	var identity environment.Identity
	var key []byte
	if err = s.db.QueryRowContext(ctx, `SELECT id,private_key FROM environment_identity WHERE singleton=1`).Scan(&identity.ID, &key); err != nil {
		return nil, err
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid persisted environment private key")
	}
	identity.PrivateKey = ed25519.PrivateKey(key)
	if identity.ID != identity.PublicKey() {
		return nil, fmt.Errorf("persisted environment identity does not match its key")
	}
	return &identity, nil
}
