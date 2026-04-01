package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ── Auth tables migration ───────────────────────────────────────────────

func (s *Store) migrateAuth() error {
	stmts := []string{
		// password_hash column on sm_users (ALTER TABLE ADD COLUMN is idempotent-safe via IF NOT EXISTS workaround)
		`CREATE TABLE IF NOT EXISTS sm_auth_tokens (
			token       TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			token_type  TEXT NOT NULL,
			expires_at  TEXT NOT NULL,
			created_at  TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sm_auth_tokens_user ON sm_auth_tokens(user_id, token_type)`,
		`CREATE TABLE IF NOT EXISTS sm_sessions (
			token      TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			email      TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sm_sessions_user ON sm_sessions(user_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	// Add password_hash column if not exists (SQLite doesn't support IF NOT EXISTS for ALTER TABLE)
	_, _ = s.db.Exec(`ALTER TABLE sm_users ADD COLUMN password_hash TEXT NOT NULL DEFAULT ''`)
	return nil
}

// ── Password ────────────────────────────────────────────────────────────

func (s *Store) GetPasswordHash(ctx context.Context, userID string) (string, error) {
	var hash string
	err := s.readDB.QueryRowContext(ctx, `SELECT password_hash FROM sm_users WHERE id = ?`, userID).Scan(&hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return hash, nil
}

func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sm_users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hash, time.Now().Format(timeFmt), userID)
	return err
}

// ── Auth Tokens (activation / identity verification) ────────────────────

type AuthToken struct {
	Token     string
	UserID    string
	TokenType string // "activation", "identity"
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (s *Store) CreateAuthToken(ctx context.Context, t *AuthToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sm_auth_tokens (token, user_id, token_type, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		t.Token, t.UserID, t.TokenType, fmtTime(t.ExpiresAt), fmtTime(t.CreatedAt))
	return err
}

func (s *Store) GetAuthToken(ctx context.Context, token string) (*AuthToken, error) {
	var t AuthToken
	var expiresAt, createdAt string
	err := s.readDB.QueryRowContext(ctx,
		`SELECT token, user_id, token_type, expires_at, created_at FROM sm_auth_tokens WHERE token = ?`, token).
		Scan(&t.Token, &t.UserID, &t.TokenType, &expiresAt, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.ExpiresAt = parseTime(expiresAt)
	t.CreatedAt = parseTime(createdAt)
	return &t, nil
}

func (s *Store) DeleteAuthToken(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sm_auth_tokens WHERE token = ?`, token)
	return err
}

// DeleteExpiredAuthTokens cleans up expired tokens.
func (s *Store) DeleteExpiredAuthTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sm_auth_tokens WHERE expires_at < ?`, fmtTime(time.Now()))
	return err
}

// ── Sessions ────────────────────────────────────────────────────────────

type Session struct {
	Token     string
	UserID    string
	Email     string
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sm_sessions (token, user_id, email, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		sess.Token, sess.UserID, sess.Email, fmtTime(sess.ExpiresAt), fmtTime(sess.CreatedAt))
	return err
}

func (s *Store) GetSession(ctx context.Context, token string) (*Session, error) {
	var sess Session
	var expiresAt, createdAt string
	err := s.readDB.QueryRowContext(ctx,
		`SELECT token, user_id, email, expires_at, created_at FROM sm_sessions WHERE token = ?`, token).
		Scan(&sess.Token, &sess.UserID, &sess.Email, &expiresAt, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sess.ExpiresAt = parseTime(expiresAt)
	sess.CreatedAt = parseTime(createdAt)
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sm_sessions WHERE token = ?`, token)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sm_sessions WHERE expires_at < ?`, fmtTime(time.Now()))
	return err
}
