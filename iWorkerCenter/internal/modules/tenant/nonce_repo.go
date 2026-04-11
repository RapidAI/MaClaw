package tenant

import (
	"context"
	"database/sql"
	"time"
)

// NonceRepo provides replay-protection storage for provision nonces.
type NonceRepo struct {
	write *sql.DB
}

func NewNonceRepo(write *sql.DB) *NonceRepo {
	return &NonceRepo{write: write}
}

// Consume attempts to insert a nonce. Returns true if the nonce was new
// (successfully consumed), false if it already existed (replay).
func (r *NonceRepo) Consume(ctx context.Context, nonce string, expiresAt time.Time) (bool, error) {
	res, err := r.write.ExecContext(ctx,
		`INSERT OR IGNORE INTO provision_nonces (nonce, expires_at) VALUES (?, ?)`,
		nonce, expiresAt.Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Cleanup removes expired nonces.
func (r *NonceRepo) Cleanup(ctx context.Context) error {
	_, err := r.write.ExecContext(ctx,
		`DELETE FROM provision_nonces WHERE expires_at < datetime('now')`)
	return err
}
