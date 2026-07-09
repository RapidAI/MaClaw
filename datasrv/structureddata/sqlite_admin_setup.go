package structureddata

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *SQLiteStore) AdminInitialized(ctx context.Context) (bool, error) {
	var count int
	if err := s.queryDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users WHERE enabled = 1`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *SQLiteStore) CreateAdminUser(ctx context.Context, record adminUserRecord) (*adminUserRecord, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_users(
		id, tenant_id, username, display_name, role, admin_scope, password_hash, enabled, created_at, updated_at, login_failure_count, login_locked_until
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.TenantID, record.Username, record.DisplayName, record.Role, normalizedAdminScope(record.AdminScope), record.PasswordHash, boolInt(record.Enabled), formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.LoginFailureCount, formatOptionalAdminTime(record.LoginLockedUntil))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return &record, nil
}

func (s *SQLiteStore) ListAdminUsers(ctx context.Context, tenantID string) ([]adminUserRecord, error) {
	tenantID = strings.TrimSpace(tenantID)
	query := `SELECT id, tenant_id, username, display_name, role, admin_scope, password_hash, enabled, last_login_at, created_at, updated_at, login_failure_count, login_locked_until
		FROM admin_users WHERE tenant_id = ? ORDER BY tenant_id, username`
	args := []any{tenantID}
	if strings.EqualFold(tenantID, "all") || tenantID == "*" {
		query = `SELECT id, tenant_id, username, display_name, role, admin_scope, password_hash, enabled, last_login_at, created_at, updated_at, login_failure_count, login_locked_until
			FROM admin_users ORDER BY tenant_id, username`
		args = nil
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adminUserRecord{}
	for rows.Next() {
		item, err := scanAdminUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) FindAdminUser(ctx context.Context, tenantID, username string) (*adminUserRecord, error) {
	row := s.queryDB().QueryRowContext(ctx, `SELECT id, tenant_id, username, display_name, role, admin_scope, password_hash, enabled, last_login_at, created_at, updated_at, login_failure_count, login_locked_until
		FROM admin_users WHERE tenant_id = ? AND username = ?`, strings.TrimSpace(tenantID), strings.ToLower(strings.TrimSpace(username)))
	item, err := scanAdminUser(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAdminNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) UpdateAdminUser(ctx context.Context, tenantID, username string, in UpdateAdminAccountInput, now time.Time) (*adminUserRecord, error) {
	existing, err := s.FindAdminUser(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	displayName := existing.DisplayName
	if in.DisplayName != nil {
		displayName = trimForStorage(*in.DisplayName, 120)
	}
	role, err := normalizeAdminRole(in.Role)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Role) == "" {
		role = existing.Role
	}
	enabled := existing.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	adminScope := normalizedAdminScope(existing.AdminScope)
	if strings.TrimSpace(in.AdminScope) != "" {
		adminScope = normalizedAdminScope(in.AdminScope)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE admin_users SET display_name = ?, role = ?, admin_scope = ?, enabled = ?, updated_at = ? WHERE tenant_id = ? AND username = ?`,
		displayName, role, adminScope, boolInt(enabled), formatTime(now), strings.TrimSpace(tenantID), strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return nil, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return nil, ErrAdminNotFound
	}
	return s.FindAdminUser(ctx, tenantID, username)
}

func (s *SQLiteStore) UpdateAdminPassword(ctx context.Context, tenantID, username, passwordHash string, now time.Time) (*adminUserRecord, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE admin_users SET password_hash = ?, login_failure_count = 0, login_locked_until = '', updated_at = ? WHERE tenant_id = ? AND username = ?`,
		strings.TrimSpace(passwordHash), formatTime(now), strings.TrimSpace(tenantID), strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return nil, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return nil, ErrAdminNotFound
	}
	return s.FindAdminUser(ctx, tenantID, username)
}

func (s *SQLiteStore) TouchAdminLogin(ctx context.Context, tenantID, userID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_users SET last_login_at = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`,
		formatTime(now), formatTime(now), strings.TrimSpace(tenantID), strings.TrimSpace(userID))
	return err
}

func (s *SQLiteStore) RecordAdminLoginFailure(ctx context.Context, tenantID, username string, now time.Time, maxFailures int, lockout time.Duration) (*adminUserRecord, error) {
	if maxFailures <= 0 {
		return s.FindAdminUser(ctx, tenantID, username)
	}
	nowText := formatTime(now)
	lockoutUntilText := formatTime(now.Add(lockout))
	tenantID = strings.TrimSpace(tenantID)
	username = strings.ToLower(strings.TrimSpace(username))
	nextFailureCount := `CASE
		WHEN login_locked_until <> '' AND login_locked_until > ? THEN login_failure_count
		WHEN login_locked_until <> '' AND login_locked_until <= ? THEN 1
		ELSE login_failure_count + 1
	END`
	res, err := s.db.ExecContext(ctx, `UPDATE admin_users SET
			login_failure_count = `+nextFailureCount+`,
			login_locked_until = CASE
				WHEN login_locked_until <> '' AND login_locked_until > ? THEN login_locked_until
				WHEN (`+nextFailureCount+`) >= ? THEN ?
				ELSE ''
			END,
			updated_at = CASE
				WHEN login_locked_until <> '' AND login_locked_until > ? THEN updated_at
				ELSE ?
			END
		WHERE tenant_id = ? AND username = ?`,
		nowText, nowText,
		nowText,
		nowText, nowText, maxFailures, lockoutUntilText,
		nowText, nowText,
		tenantID, username)
	if err != nil {
		return nil, err
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return nil, ErrAdminNotFound
	}
	return s.FindAdminUser(ctx, tenantID, username)
}

func (s *SQLiteStore) ClearAdminLoginFailure(ctx context.Context, tenantID, username string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE admin_users SET login_failure_count = 0, login_locked_until = '', updated_at = ? WHERE tenant_id = ? AND username = ?`,
		formatTime(now), strings.TrimSpace(tenantID), strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return err
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return ErrAdminNotFound
	}
	return nil
}

func (s *SQLiteStore) CreateAdminSession(ctx context.Context, record adminSessionRecord) (*adminSessionRecord, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_sessions(
		id, tenant_id, user_id, username, role, admin_scope, token_hash, expires_at, created_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.TenantID, record.UserID, record.Username, record.Role, normalizedAdminScope(record.AdminScope), record.TokenHash, formatTime(record.ExpiresAt), formatTime(record.CreatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return &record, nil
}

func (s *SQLiteStore) FindAdminSessionByHash(ctx context.Context, tokenHash string, now time.Time) (*adminSessionRecord, error) {
	row := s.queryDB().QueryRowContext(ctx, `SELECT id, tenant_id, user_id, username, role, admin_scope, token_hash, expires_at, created_at
		FROM admin_sessions WHERE token_hash = ? AND expires_at > ?`, strings.TrimSpace(tokenHash), formatTime(now))
	item, err := scanAdminSession(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) ListAdminSessions(ctx context.Context, tenantID string, now time.Time) ([]adminSessionRecord, error) {
	tenantID = strings.TrimSpace(tenantID)
	query := `SELECT id, tenant_id, user_id, username, role, admin_scope, token_hash, expires_at, created_at
		FROM admin_sessions WHERE tenant_id = ? AND expires_at > ? ORDER BY created_at DESC`
	args := []any{tenantID, formatTime(now)}
	if strings.EqualFold(tenantID, "all") || tenantID == "*" {
		query = `SELECT id, tenant_id, user_id, username, role, admin_scope, token_hash, expires_at, created_at
			FROM admin_sessions WHERE expires_at > ? ORDER BY tenant_id, created_at DESC`
		args = []any{formatTime(now)}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []adminSessionRecord{}
	for rows.Next() {
		item, err := scanAdminSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateAdminSessionExpiresAt(ctx context.Context, tenantID, sessionID string, expiresAt time.Time, now time.Time) (*adminSessionRecord, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE admin_sessions SET expires_at = ? WHERE tenant_id = ? AND id = ? AND expires_at > ?`,
		formatTime(expiresAt), strings.TrimSpace(tenantID), strings.TrimSpace(sessionID), formatTime(now))
	if err != nil {
		return nil, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return nil, ErrSessionNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, user_id, username, role, admin_scope, token_hash, expires_at, created_at
		FROM admin_sessions WHERE tenant_id = ? AND id = ?`, strings.TrimSpace(tenantID), strings.TrimSpace(sessionID))
	item, err := scanAdminSession(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) DeleteAdminSession(ctx context.Context, tenantID, sessionID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE tenant_id = ? AND id = ?`, strings.TrimSpace(tenantID), strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteAdminSessionsForUser(ctx context.Context, tenantID, userID string) error {
	if strings.EqualFold(strings.TrimSpace(tenantID), "all") || strings.TrimSpace(tenantID) == "*" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE user_id = ?`, strings.TrimSpace(userID))
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE tenant_id = ? AND user_id = ?`, strings.TrimSpace(tenantID), strings.TrimSpace(userID))
	return err
}

func (s *SQLiteStore) DeleteExpiredAdminSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at <= ?`, formatTime(now))
	return err
}

type adminUserScanner interface {
	Scan(dest ...any) error
}

func scanAdminUser(row adminUserScanner) (adminUserRecord, error) {
	var item adminUserRecord
	var enabled int
	var lastLoginAt, createdAt, updatedAt, loginLockedUntil string
	err := row.Scan(&item.ID, &item.TenantID, &item.Username, &item.DisplayName, &item.Role, &item.AdminScope, &item.PasswordHash, &enabled, &lastLoginAt, &createdAt, &updatedAt, &item.LoginFailureCount, &loginLockedUntil)
	if err != nil {
		return item, err
	}
	item.Enabled = enabled != 0
	if strings.TrimSpace(lastLoginAt) != "" {
		parsed := parseTime(lastLoginAt)
		item.LastLoginAt = &parsed
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	if strings.TrimSpace(loginLockedUntil) != "" {
		item.LoginLockedUntil = parseTime(loginLockedUntil)
	}
	return item, nil
}

func formatOptionalAdminTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}

type adminSessionScanner interface {
	Scan(dest ...any) error
}

func scanAdminSession(row adminSessionScanner) (adminSessionRecord, error) {
	var item adminSessionRecord
	var expiresAt, createdAt string
	err := row.Scan(&item.ID, &item.TenantID, &item.UserID, &item.Username, &item.Role, &item.AdminScope, &item.TokenHash, &expiresAt, &createdAt)
	if err != nil {
		return item, err
	}
	item.ExpiresAt = parseTime(expiresAt)
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}
