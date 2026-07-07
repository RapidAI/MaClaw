package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Skill ID Ownership — binds a skill_id (publisher.skill-name) to an uploader
// account. Once bound, only that account can upload new versions.
// ────────────────────────────────────────────────────────────────────────────

// SkillIDOwnership represents the ownership record for a skill_id.
type SkillIDOwnership struct {
	SkillID        string    `json:"skill_id"`
	UserID         string    `json:"user_id"`
	Email          string    `json:"email"`
	RegisteredAt   time.Time `json:"registered_at"`
	TransferredFrom string   `json:"transferred_from,omitempty"`
	TransferredAt  *time.Time `json:"transferred_at,omitempty"`
}

// MaskedEmail returns the email with middle chars masked for display.
// "zhangsan@gmail.com" → "zha***@gmail.com"
func (o *SkillIDOwnership) MaskedEmail() string {
	if o == nil || o.Email == "" {
		return ""
	}
	at := indexOf(o.Email, '@')
	if at <= 3 {
		return o.Email
	}
	return o.Email[:3] + "***" + o.Email[at:]
}

func indexOf(s string, ch byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ch {
			return i
		}
	}
	return -1
}

// migrateSkillIDOwnership creates the ownership table.
func (s *Store) migrateSkillIDOwnership() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sm_skill_id_ownership (
			skill_id         TEXT PRIMARY KEY,
			user_id          TEXT NOT NULL,
			email            TEXT NOT NULL,
			registered_at    TEXT NOT NULL,
			transferred_from TEXT NOT NULL DEFAULT '',
			transferred_at   TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sm_skill_id_ownership_user ON sm_skill_id_ownership(user_id)`,
		// Version history: tracks all published versions for a skill_id.
		`CREATE TABLE IF NOT EXISTS sm_skill_versions (
			skill_id       TEXT NOT NULL,
			version        TEXT NOT NULL,
			internal_id    TEXT NOT NULL,
			package_sha256 TEXT NOT NULL DEFAULT '',
			uploaded_at    TEXT NOT NULL,
			uploader_id    TEXT NOT NULL,
			PRIMARY KEY (skill_id, version)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sm_skill_versions_internal ON sm_skill_versions(internal_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// GetSkillIDOwner returns the ownership record for a skill_id, or nil if unregistered.
func (s *Store) GetSkillIDOwner(ctx context.Context, skillID string) (*SkillIDOwnership, error) {
	var o SkillIDOwnership
	var registeredAt, transferredAt string
	err := s.readDB.QueryRowContext(ctx,
		`SELECT skill_id, user_id, email, registered_at, transferred_from, transferred_at
		 FROM sm_skill_id_ownership WHERE skill_id = ?`, skillID,
	).Scan(&o.SkillID, &o.UserID, &o.Email, &registeredAt, &o.TransferredFrom, &transferredAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	o.RegisteredAt = parseTime(registeredAt)
	if transferredAt != "" {
		t := parseTime(transferredAt)
		o.TransferredAt = &t
	}
	return &o, nil
}

// RegisterSkillIDOwnership registers a new skill_id ownership binding.
// Returns an error if the skill_id is already registered to a different user.
func (s *Store) RegisterSkillIDOwnership(ctx context.Context, skillID, userID, email string) error {
	now := time.Now().Format(timeFmt)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sm_skill_id_ownership (skill_id, user_id, email, registered_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(skill_id) DO NOTHING`,
		skillID, userID, email, now)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

// RegisterSkillIDOwnershipIfAbsent registers ownership only if not already registered.
// Used during migration — does not overwrite existing bindings.
func (s *Store) RegisterSkillIDOwnershipIfAbsent(ctx context.Context, skillID, userID, email string) error {
	return s.RegisterSkillIDOwnership(ctx, skillID, userID, email)
}

// TransferSkillIDOwnership transfers ownership from one user to another (admin operation).
func (s *Store) TransferSkillIDOwnership(ctx context.Context, skillID, fromUserID, toUserID, toEmail string) error {
	now := time.Now().Format(timeFmt)
	res, err := s.db.ExecContext(ctx,
		`UPDATE sm_skill_id_ownership
		 SET user_id = ?, email = ?, transferred_from = ?, transferred_at = ?
		 WHERE skill_id = ? AND user_id = ?`,
		toUserID, toEmail, fromUserID, now, skillID, fromUserID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New("ownership transfer failed: skill_id not found or owner mismatch")
	}
	s.emitSync(ctx)
	return nil
}

// RecordSkillVersion records a new version upload for a skill_id.
func (s *Store) RecordSkillVersion(ctx context.Context, skillID, version, internalID, packageSHA256, uploaderID string) error {
	now := time.Now().Format(timeFmt)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sm_skill_versions (skill_id, version, internal_id, package_sha256, uploaded_at, uploader_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(skill_id, version) DO UPDATE SET
			internal_id = excluded.internal_id,
			package_sha256 = excluded.package_sha256,
			uploaded_at = excluded.uploaded_at,
			uploader_id = excluded.uploader_id`,
		skillID, version, internalID, packageSHA256, now, uploaderID)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

// GetLatestVersionForSkillID returns the latest semver version for a skill_id.
// Returns "" if no versions are recorded.
func (s *Store) GetLatestVersionForSkillID(ctx context.Context, skillID string) (string, error) {
	var version string
	err := s.readDB.QueryRowContext(ctx,
		`SELECT version FROM sm_skill_versions
		 WHERE skill_id = ?
		 ORDER BY uploaded_at DESC LIMIT 1`, skillID,
	).Scan(&version)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return version, nil
}

// ListSkillIDsByUser returns all skill_ids owned by a user.
func (s *Store) ListSkillIDsByUser(ctx context.Context, userID string) ([]SkillIDOwnership, error) {
	rows, err := s.readDB.QueryContext(ctx,
		`SELECT skill_id, user_id, email, registered_at FROM sm_skill_id_ownership WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SkillIDOwnership
	for rows.Next() {
		var o SkillIDOwnership
		var registeredAt string
		if err := rows.Scan(&o.SkillID, &o.UserID, &o.Email, &registeredAt); err != nil {
			continue
		}
		o.RegisteredAt = parseTime(registeredAt)
		result = append(result, o)
	}
	return result, rows.Err()
}
