package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type adminRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type systemRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type adminAuditRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type hubRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type hubUserLinkRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type blockedEmailRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type blockedIPRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

func NewStore(p *Provider) *store.Store {
	return &store.Store{
		Admins:        &adminRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		System:        &systemRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		AdminAudit:    &adminAuditRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Hubs:          &hubRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HubUserLinks:  &hubUserLinkRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		BlockedEmails: &blockedEmailRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		BlockedIPs:    &blockedIPRepo{db: p.Write, readDB: p.Read, batch: p.batch},
	}
}

func execWrite(ctx context.Context, batch *writeBatcher, db *sql.DB, query string, args ...any) error {
	if batch != nil {
		return batch.ExecContext(ctx, query, args...)
	}
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *adminRepo) Create(ctx context.Context, admin *store.AdminUser) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO admin_users (id, username, password_hash, email, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		admin.ID,
		admin.Username,
		admin.PasswordHash,
		admin.Email,
		admin.Status,
		admin.CreatedAt.Format(time.RFC3339),
		admin.UpdatedAt.Format(time.RFC3339),
	)
	return err
}
func (r *adminRepo) GetByUsername(ctx context.Context, username string) (*store.AdminUser, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, username, password_hash, email, status, created_at, updated_at
		FROM admin_users
		WHERE username = ?
	`, username)

	var item store.AdminUser
	var createdAt, updatedAt string
	if err := row.Scan(
		&item.ID,
		&item.Username,
		&item.PasswordHash,
		&item.Email,
		&item.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}
func (r *adminRepo) Count(ctx context.Context) (int, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM admin_users`)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *adminRepo) DeleteAll(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM admin_users`)
	return err
}

func (r *adminRepo) UpdatePassword(ctx context.Context, username, passwordHash string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE admin_users
		SET password_hash = ?, updated_at = ?
		WHERE username = ?
	`, passwordHash, updatedAt.Format(time.RFC3339), username)
	return err
}

func (r *adminRepo) UpdateEmail(ctx context.Context, username, email string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE admin_users
		SET email = ?, updated_at = ?
		WHERE username = ?
	`, email, updatedAt.Format(time.RFC3339), username)
	return err
}

func (r *systemRepo) Set(ctx context.Context, key, valueJSON string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO system_settings (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, key, valueJSON, time.Now().Format(time.RFC3339))
	return err
}
func (r *systemRepo) Get(ctx context.Context, key string) (string, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT value_json
		FROM system_settings
		WHERE key = ?
	`, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func (r *adminAuditRepo) Create(ctx context.Context, log *store.AdminAuditLog) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO admin_audit_logs (id, admin_user_id, action, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`,
		log.ID,
		log.AdminUserID,
		log.Action,
		log.PayloadJSON,
		log.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *hubRepo) Create(ctx context.Context, hub *store.HubInstance) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO hub_instances (
			id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode,
			status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
			last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		hub.ID,
		hub.InstallationID,
		hub.OwnerEmail,
		hub.Name,
		hub.Description,
		hub.BaseURL,
		hub.Host,
		hub.Port,
		hub.Visibility,
		hub.EnrollmentMode,
		hub.Status,
		boolToInt(hub.IsDisabled),
		hub.DisabledReason,
		hub.CapabilitiesJSON,
		hub.HubSecretHash,
		timePtrString(hub.LastSeenAt),
		hub.CreatedAt.Format(time.RFC3339),
		hub.UpdatedAt.Format(time.RFC3339),
	)
	return err
}
func (r *hubRepo) GetByID(ctx context.Context, id string) (*store.HubInstance, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode,
		       status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
		       last_seen_at, created_at, updated_at
		FROM hub_instances
		WHERE id = ?
	`, id)

	var item store.HubInstance
	var isDisabled int
	var lastSeen sql.NullString
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&item.ID,
		&item.InstallationID,
		&item.OwnerEmail,
		&item.Name,
		&item.Description,
		&item.BaseURL,
		&item.Host,
		&item.Port,
		&item.Visibility,
		&item.EnrollmentMode,
		&item.Status,
		&isDisabled,
		&item.DisabledReason,
		&item.CapabilitiesJSON,
		&item.HubSecretHash,
		&lastSeen,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.IsDisabled = isDisabled == 1
	if lastSeen.Valid {
		ts, err := time.Parse(time.RFC3339, lastSeen.String)
		if err == nil {
			item.LastSeenAt = &ts
		}
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}
func (r *hubRepo) GetByInstallationID(ctx context.Context, installationID string) (*store.HubInstance, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode,
		       status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
		       last_seen_at, created_at, updated_at
		FROM hub_instances
		WHERE installation_id = ?
	`, installationID)

	var item store.HubInstance
	var isDisabled int
	var lastSeen sql.NullString
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&item.ID,
		&item.InstallationID,
		&item.OwnerEmail,
		&item.Name,
		&item.Description,
		&item.BaseURL,
		&item.Host,
		&item.Port,
		&item.Visibility,
		&item.EnrollmentMode,
		&item.Status,
		&isDisabled,
		&item.DisabledReason,
		&item.CapabilitiesJSON,
		&item.HubSecretHash,
		&lastSeen,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.IsDisabled = isDisabled == 1
	if lastSeen.Valid {
		ts, err := time.Parse(time.RFC3339, lastSeen.String)
		if err == nil {
			item.LastSeenAt = &ts
		}
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}
func (r *hubRepo) UpdateHeartbeat(ctx context.Context, hubID string, at time.Time) error {
	return execWrite(ctx, r.batch, r.db, `
		UPDATE hub_instances
		SET status = CASE WHEN is_disabled = 1 THEN 'disabled' ELSE 'online' END,
		    last_seen_at = ?, updated_at = ?
		WHERE id = ?
	`, at.Format(time.RFC3339), at.Format(time.RFC3339), hubID)
}
func (r *hubRepo) ListByEmail(ctx context.Context, email string) ([]*store.HubInstance, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT DISTINCT h.id, h.installation_id, h.owner_email, h.name, h.description, h.base_url, h.host, h.port, h.visibility,
		       h.enrollment_mode, h.status, h.is_disabled, h.disabled_reason,
		       h.capabilities_json, h.hub_secret_hash, h.last_seen_at, h.created_at, h.updated_at
		FROM hub_instances h
		LEFT JOIN hub_user_links l ON l.hub_id = h.id
		WHERE h.owner_email = ? OR l.email = ?
		ORDER BY h.updated_at DESC
	`, email, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HubInstance
	for rows.Next() {
		var item store.HubInstance
		var isDisabled int
		var lastSeen sql.NullString
		var createdAt string
		var updatedAt string
		if err := rows.Scan(
			&item.ID,
			&item.InstallationID,
			&item.OwnerEmail,
			&item.Name,
			&item.Description,
			&item.BaseURL,
			&item.Host,
			&item.Port,
			&item.Visibility,
			&item.EnrollmentMode,
			&item.Status,
			&isDisabled,
			&item.DisabledReason,
			&item.CapabilitiesJSON,
			&item.HubSecretHash,
			&lastSeen,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		item.IsDisabled = isDisabled == 1
		if lastSeen.Valid {
			ts, err := time.Parse(time.RFC3339, lastSeen.String)
			if err == nil {
				item.LastSeenAt = &ts
			}
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (r *hubRepo) ListAll(ctx context.Context) ([]*store.HubInstance, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode,
		       status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
		       last_seen_at, created_at, updated_at
		FROM hub_instances
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HubInstance
	for rows.Next() {
		var item store.HubInstance
		var isDisabled int
		var lastSeen sql.NullString
		var createdAt string
		var updatedAt string
		if err := rows.Scan(
			&item.ID,
			&item.InstallationID,
			&item.OwnerEmail,
			&item.Name,
			&item.Description,
			&item.BaseURL,
			&item.Host,
			&item.Port,
			&item.Visibility,
			&item.EnrollmentMode,
			&item.Status,
			&isDisabled,
			&item.DisabledReason,
			&item.CapabilitiesJSON,
			&item.HubSecretHash,
			&lastSeen,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		item.IsDisabled = isDisabled == 1
		if lastSeen.Valid {
			ts, err := time.Parse(time.RFC3339, lastSeen.String)
			if err == nil {
				item.LastSeenAt = &ts
			}
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (r *hubRepo) UpdateVisibility(ctx context.Context, hubID string, visibility string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE hub_instances
		SET visibility = ?, updated_at = ?
		WHERE id = ?
	`, visibility, updatedAt.Format(time.RFC3339), hubID)
	return err
}

func (r *hubRepo) SetDisabled(ctx context.Context, hubID string, disabled bool, reason string, updatedAt time.Time) error {
	status := "online"
	if disabled {
		status = "disabled"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE hub_instances
		SET is_disabled = ?, disabled_reason = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, boolToInt(disabled), reason, status, updatedAt.Format(time.RFC3339), hubID)
	return err
}

func (r *hubRepo) UpdateRegistration(ctx context.Context, hub *store.HubInstance) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE hub_instances
		SET installation_id = ?, owner_email = ?, name = ?, description = ?, base_url = ?,
		    host = ?, port = ?, visibility = ?, enrollment_mode = ?, status = ?,
		    is_disabled = ?, disabled_reason = ?, capabilities_json = ?, hub_secret_hash = ?,
		    last_seen_at = ?, updated_at = ?
		WHERE id = ?
	`,
		hub.InstallationID,
		hub.OwnerEmail,
		hub.Name,
		hub.Description,
		hub.BaseURL,
		hub.Host,
		hub.Port,
		hub.Visibility,
		hub.EnrollmentMode,
		hub.Status,
		boolToInt(hub.IsDisabled),
		hub.DisabledReason,
		hub.CapabilitiesJSON,
		hub.HubSecretHash,
		timePtrString(hub.LastSeenAt),
		hub.UpdatedAt.Format(time.RFC3339),
		hub.ID,
	)
	return err
}

func (r *hubRepo) DeleteByID(ctx context.Context, hubID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM hub_instances
		WHERE id = ?
	`, hubID)
	return err
}

func (r *hubUserLinkRepo) ListByEmail(ctx context.Context, email string) ([]*store.HubUserLink, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE email = ?
		ORDER BY is_default DESC, updated_at DESC
	`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HubUserLink
	for rows.Next() {
		var item store.HubUserLink
		var isDefault int
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&item.ID, &item.HubID, &item.Email, &isDefault, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.IsDefault = isDefault == 1
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (r *hubUserLinkRepo) Create(ctx context.Context, link *store.HubUserLink) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		link.ID,
		link.HubID,
		link.Email,
		boolToInt(link.IsDefault),
		link.CreatedAt.Format(time.RFC3339),
		link.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *hubUserLinkRepo) GetDefaultByEmail(ctx context.Context, email string) (*store.HubUserLink, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, hub_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE email = ? AND is_default = 1
		LIMIT 1
	`, email)

	var item store.HubUserLink
	var isDefault int
	var createdAt string
	var updatedAt string
	if err := row.Scan(&item.ID, &item.HubID, &item.Email, &isDefault, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.IsDefault = isDefault == 1
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *hubUserLinkRepo) DeleteByHubID(ctx context.Context, hubID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM hub_user_links
		WHERE hub_id = ?
	`, hubID)
	return err
}

func (r *blockedEmailRepo) GetByEmail(ctx context.Context, email string) (*store.BlockedEmail, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, email, reason, created_at, updated_at
		FROM blocked_emails
		WHERE email = ?
	`, email)

	var item store.BlockedEmail
	var createdAt string
	var updatedAt string
	if err := row.Scan(&item.ID, &item.Email, &item.Reason, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *blockedEmailRepo) Create(ctx context.Context, item *store.BlockedEmail) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO blocked_emails (id, email, reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, item.ID, item.Email, item.Reason, item.CreatedAt.Format(time.RFC3339), item.UpdatedAt.Format(time.RFC3339))
	return err
}

func (r *blockedEmailRepo) DeleteByEmail(ctx context.Context, email string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM blocked_emails
		WHERE email = ?
	`, email)
	return err
}

func (r *blockedEmailRepo) List(ctx context.Context) ([]*store.BlockedEmail, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, email, reason, created_at, updated_at
		FROM blocked_emails
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.BlockedEmail
	for rows.Next() {
		var item store.BlockedEmail
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Email, &item.Reason, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

func (r *blockedIPRepo) GetByIP(ctx context.Context, ip string) (*store.BlockedIP, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, ip, reason, created_at, updated_at
		FROM blocked_ips
		WHERE ip = ?
	`, ip)

	var item store.BlockedIP
	var createdAt string
	var updatedAt string
	if err := row.Scan(&item.ID, &item.IP, &item.Reason, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *blockedIPRepo) Create(ctx context.Context, item *store.BlockedIP) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO blocked_ips (id, ip, reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, item.ID, item.IP, item.Reason, item.CreatedAt.Format(time.RFC3339), item.UpdatedAt.Format(time.RFC3339))
	return err
}

func (r *blockedIPRepo) DeleteByIP(ctx context.Context, ip string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM blocked_ips
		WHERE ip = ?
	`, ip)
	return err
}

func (r *blockedIPRepo) List(ctx context.Context) ([]*store.BlockedIP, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, ip, reason, created_at, updated_at
		FROM blocked_ips
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.BlockedIP
	for rows.Next() {
		var item store.BlockedIP
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.IP, &item.Reason, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, &item)
	}
	return out, rows.Err()
}

func mustParseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func timePtrString(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.Format(time.RFC3339)
}
