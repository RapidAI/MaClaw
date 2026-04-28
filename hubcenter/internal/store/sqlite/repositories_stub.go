package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
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
type failureEventLogRepo struct {
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
type hubDomainRouteRepo struct {
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

type haSyncOpRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

type haPeerCursorRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

type haEntityVersionRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

type haHeartbeatSyncStateRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

type gossipRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

type newsRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

func NewStore(p *Provider) *store.Store {
	return &store.Store{
		Admins:           &adminRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		System:           &systemRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		AdminAudit:       &adminAuditRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		FailureLogs:      &failureEventLogRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Hubs:             &hubRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HubUserLinks:     &hubUserLinkRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HubDomainRoutes:  &hubDomainRouteRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		BlockedEmails:    &blockedEmailRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		BlockedIPs:       &blockedIPRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HASyncOps:        &haSyncOpRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HAPeerCursors:    &haPeerCursorRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HAEntityVersions: &haEntityVersionRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HAHeartbeatSync:  &haHeartbeatSyncStateRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Gossip:           &gossipRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		News:             &newsRepo{db: p.Write, readDB: p.Read, batch: p.batch},
	}
}

func execWrite(ctx context.Context, batch *writeBatcher, db *sql.DB, query string, args ...any) error {
	if batch != nil {
		return batch.ExecContext(ctx, query, args...)
	}
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func scanHubInstance(scanner interface{ Scan(dest ...any) error }) (*store.HubInstance, error) {
	var item store.HubInstance
	var isDisabled int
	var invitationCodeRequired int
	var acceptPublicSignup int
	var lastSeen sql.NullString
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
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
		&item.CorporateEmailDomain,
		&acceptPublicSignup,
		&item.Status,
		&isDisabled,
		&item.DisabledReason,
		&item.CapabilitiesJSON,
		&item.HubSecretHash,
		&invitationCodeRequired,
		&lastSeen,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	item.IsDisabled = isDisabled == 1
	item.AcceptPublicSignup = acceptPublicSignup == 1
	item.InvitationCodeRequired = invitationCodeRequired == 1
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

func scanHubUserLink(scanner interface{ Scan(dest ...any) error }) (*store.HubUserLink, error) {
	var item store.HubUserLink
	var isDefault int
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.ID, &item.HubID, &item.Email, &isDefault, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	item.IsDefault = isDefault == 1
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func scanHubDomainRoute(scanner interface{ Scan(dest ...any) error }) (*store.HubDomainRoute, error) {
	var item store.HubDomainRoute
	var enabled int
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.ID, &item.HubID, &item.Domain, &enabled, &item.Priority, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	item.Enabled = enabled == 1
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
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

func (r *systemRepo) List(ctx context.Context) ([]*store.SystemSettingEntry, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT key, value_json, updated_at
		FROM system_settings
		ORDER BY key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*store.SystemSettingEntry, 0)
	for rows.Next() {
		var (
			item         store.SystemSettingEntry
			rawUpdatedAt string
		)
		if err := rows.Scan(&item.Key, &item.ValueJSON, &rawUpdatedAt); err != nil {
			return nil, err
		}
		if ts, err := time.Parse(time.RFC3339, rawUpdatedAt); err == nil {
			item.UpdatedAt = ts
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
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
			id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode, corporate_email_domain,
			accept_public_signup, status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
			invitation_code_required, last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		hub.CorporateEmailDomain,
		boolToInt(hub.AcceptPublicSignup),
		hub.Status,
		boolToInt(hub.IsDisabled),
		hub.DisabledReason,
		hub.CapabilitiesJSON,
		hub.HubSecretHash,
		boolToInt(hub.InvitationCodeRequired),
		timePtrString(hub.LastSeenAt),
		hub.CreatedAt.Format(time.RFC3339),
		hub.UpdatedAt.Format(time.RFC3339),
	)
	return err
}
func (r *hubRepo) GetByID(ctx context.Context, id string) (*store.HubInstance, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode, corporate_email_domain,
		       accept_public_signup, status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
		       invitation_code_required, last_seen_at, created_at, updated_at
		FROM hub_instances
		WHERE id = ?
	`, id)

	item, err := scanHubInstance(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}
func (r *hubRepo) GetByInstallationID(ctx context.Context, installationID string) (*store.HubInstance, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode, corporate_email_domain,
		       accept_public_signup, status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
		       invitation_code_required, last_seen_at, created_at, updated_at
		FROM hub_instances
		WHERE installation_id = ?
	`, installationID)

	item, err := scanHubInstance(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
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
		       h.enrollment_mode, h.corporate_email_domain, h.accept_public_signup, h.status, h.is_disabled, h.disabled_reason,
		       h.capabilities_json, h.hub_secret_hash, h.invitation_code_required, h.last_seen_at, h.created_at, h.updated_at
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
		item, err := scanHubInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *hubRepo) ListAll(ctx context.Context) ([]*store.HubInstance, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, installation_id, owner_email, name, description, base_url, host, port, visibility, enrollment_mode, corporate_email_domain,
		       accept_public_signup, status, is_disabled, disabled_reason, capabilities_json, hub_secret_hash,
		       invitation_code_required, last_seen_at, created_at, updated_at
		FROM hub_instances
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HubInstance
	for rows.Next() {
		item, err := scanHubInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
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
		    host = ?, port = ?, visibility = ?, enrollment_mode = ?, corporate_email_domain = ?, accept_public_signup = ?, status = ?,
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
		hub.CorporateEmailDomain,
		boolToInt(hub.AcceptPublicSignup),
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

func (r *hubRepo) UpdateInvitationCodeRequired(ctx context.Context, hubID string, required bool, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE hub_instances
		SET invitation_code_required = ?, updated_at = ?
		WHERE id = ?
	`, boolToInt(required), updatedAt.Format(time.RFC3339), hubID)
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
		item, err := scanHubUserLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *hubUserLinkRepo) ListAll(ctx context.Context) ([]*store.HubUserLink, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		ORDER BY updated_at DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HubUserLink
	for rows.Next() {
		item, err := scanHubUserLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
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

func (r *hubUserLinkRepo) Upsert(ctx context.Context, link *store.HubUserLink) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			email = excluded.email,
			is_default = excluded.is_default,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
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

	item, err := scanHubUserLink(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

func (r *hubUserLinkRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM hub_user_links
		WHERE id = ?
	`, id)
	return err
}

func (r *hubUserLinkRepo) DeleteByHubID(ctx context.Context, hubID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM hub_user_links
		WHERE hub_id = ?
	`, hubID)
	return err
}

func (r *hubUserLinkRepo) MigrateEmailToHub(ctx context.Context, email, fromHubID string, link *store.HubUserLink) ([]*store.HubUserLink, *store.HubUserLink, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE email = ?
		ORDER BY is_default DESC, updated_at DESC
	`, email)
	if err != nil {
		return nil, nil, err
	}
	var remove []*store.HubUserLink
	targetExists := false
	for rows.Next() {
		item, scanErr := scanHubUserLink(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, nil, scanErr
		}
		if item == nil || strings.HasPrefix(strings.TrimSpace(item.ID), "hul_owner_") {
			continue
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(link.HubID) {
			targetExists = true
			continue
		}
		if strings.TrimSpace(item.ID) == strings.TrimSpace(link.ID) {
			continue
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(item.HubID) != strings.TrimSpace(fromHubID) {
			continue
		}
		remove = append(remove, item)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var upserted *store.HubUserLink
	if !targetExists || len(remove) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				hub_id = excluded.hub_id,
				email = excluded.email,
				is_default = excluded.is_default,
				created_at = excluded.created_at,
				updated_at = excluded.updated_at
		`, link.ID, link.HubID, link.Email, boolToInt(link.IsDefault), link.CreatedAt.Format(time.RFC3339), link.UpdatedAt.Format(time.RFC3339)); err != nil {
			return nil, nil, err
		}
		upserted = link
	}
	for _, item := range remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM hub_user_links WHERE id = ?`, item.ID); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	committed = true
	return remove, upserted, nil
}

func (r *hubUserLinkRepo) MigrateEmailPatternToHub(ctx context.Context, pattern, fromHubID, toHubID string, now time.Time) ([]*store.HubUserLink, []*store.HubUserLink, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		ORDER BY email ASC, is_default DESC, updated_at DESC
	`)
	if err != nil {
		return nil, nil, err
	}
	var remove []*store.HubUserLink
	matchedEmails := map[string]struct{}{}
	for rows.Next() {
		item, scanErr := scanHubUserLink(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, nil, scanErr
		}
		if item == nil || strings.HasPrefix(strings.TrimSpace(item.ID), "hul_owner_") || !emailWildcardMatch(pattern, item.Email) {
			continue
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(toHubID) {
			continue
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(item.HubID) != strings.TrimSpace(fromHubID) {
			continue
		}
		matchedEmails[normalizeStoreEmail(item.Email)] = struct{}{}
		remove = append(remove, item)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	upserted := make([]*store.HubUserLink, 0, len(matchedEmails))
	for email := range matchedEmails {
		link := &store.HubUserLink{ID: adminStoreUserLinkID(email), HubID: toHubID, Email: email, IsDefault: true, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				hub_id = excluded.hub_id,
				email = excluded.email,
				is_default = excluded.is_default,
				updated_at = excluded.updated_at
		`, link.ID, link.HubID, link.Email, boolToInt(link.IsDefault), link.CreatedAt.Format(time.RFC3339), link.UpdatedAt.Format(time.RFC3339)); err != nil {
			return nil, nil, err
		}
		upserted = append(upserted, link)
	}
	for _, item := range remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM hub_user_links WHERE id = ?`, item.ID); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	committed = true
	return remove, upserted, nil
}

func (r *hubDomainRouteRepo) Upsert(ctx context.Context, route *store.HubDomainRoute) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO hub_domain_routes (id, hub_id, domain, enabled, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			domain = excluded.domain,
			enabled = excluded.enabled,
			priority = excluded.priority,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`,
		route.ID,
		route.HubID,
		route.Domain,
		boolToInt(route.Enabled),
		route.Priority,
		route.CreatedAt.Format(time.RFC3339),
		route.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *hubDomainRouteRepo) ListAll(ctx context.Context) ([]*store.HubDomainRoute, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		ORDER BY priority ASC, updated_at DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*store.HubDomainRoute
	for rows.Next() {
		item, err := scanHubDomainRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *hubDomainRouteRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM hub_domain_routes
		WHERE id = ?
	`, id)
	return err
}

func (r *hubDomainRouteRepo) DeleteByHubID(ctx context.Context, hubID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM hub_domain_routes
		WHERE hub_id = ?
	`, hubID)
	return err
}

func (r *hubDomainRouteRepo) MigrateDomainToHub(ctx context.Context, domain, fromHubID string, route *store.HubDomainRoute) ([]*store.HubDomainRoute, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		WHERE domain = ?
		ORDER BY priority ASC, updated_at DESC, id ASC
	`, domain)
	if err != nil {
		return nil, err
	}
	var remove []*store.HubDomainRoute
	for rows.Next() {
		item, scanErr := scanHubDomainRoute(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		if item == nil || strings.TrimSpace(item.ID) == strings.TrimSpace(route.ID) {
			continue
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(route.HubID) {
			continue
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(item.HubID) != strings.TrimSpace(fromHubID) {
			continue
		}
		remove = append(remove, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hub_domain_routes (id, hub_id, domain, enabled, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			domain = excluded.domain,
			enabled = excluded.enabled,
			priority = excluded.priority,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, route.ID, route.HubID, route.Domain, boolToInt(route.Enabled), route.Priority, route.CreatedAt.Format(time.RFC3339), route.UpdatedAt.Format(time.RFC3339)); err != nil {
		return nil, err
	}
	for _, item := range remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM hub_domain_routes WHERE id = ?`, item.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return remove, nil
}

func (r *hubDomainRouteRepo) MigrateDomainAndEmailPatternToHub(ctx context.Context, domain, pattern, fromHubID, toHubID string, route *store.HubDomainRoute, now time.Time) ([]*store.HubDomainRoute, []*store.HubUserLink, []*store.HubUserLink, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	routeRows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		WHERE domain = ?
		ORDER BY priority ASC, updated_at DESC, id ASC
	`, domain)
	if err != nil {
		return nil, nil, nil, err
	}
	var removeRoutes []*store.HubDomainRoute
	for routeRows.Next() {
		item, scanErr := scanHubDomainRoute(routeRows)
		if scanErr != nil {
			_ = routeRows.Close()
			return nil, nil, nil, scanErr
		}
		if item == nil || strings.TrimSpace(item.ID) == strings.TrimSpace(route.ID) {
			continue
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(route.HubID) {
			continue
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(item.HubID) != strings.TrimSpace(fromHubID) {
			continue
		}
		removeRoutes = append(removeRoutes, item)
	}
	if err := routeRows.Close(); err != nil {
		return nil, nil, nil, err
	}
	if err := routeRows.Err(); err != nil {
		return nil, nil, nil, err
	}

	linkRows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		ORDER BY email ASC, is_default DESC, updated_at DESC
	`)
	if err != nil {
		return nil, nil, nil, err
	}
	var removeLinks []*store.HubUserLink
	matchedEmails := map[string]struct{}{}
	for linkRows.Next() {
		item, scanErr := scanHubUserLink(linkRows)
		if scanErr != nil {
			_ = linkRows.Close()
			return nil, nil, nil, scanErr
		}
		if item == nil || strings.HasPrefix(strings.TrimSpace(item.ID), "hul_owner_") || !emailWildcardMatch(pattern, item.Email) {
			continue
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(toHubID) {
			continue
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(item.HubID) != strings.TrimSpace(fromHubID) {
			continue
		}
		matchedEmails[normalizeStoreEmail(item.Email)] = struct{}{}
		removeLinks = append(removeLinks, item)
	}
	if err := linkRows.Close(); err != nil {
		return nil, nil, nil, err
	}
	if err := linkRows.Err(); err != nil {
		return nil, nil, nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO hub_domain_routes (id, hub_id, domain, enabled, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			domain = excluded.domain,
			enabled = excluded.enabled,
			priority = excluded.priority,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, route.ID, route.HubID, route.Domain, boolToInt(route.Enabled), route.Priority, route.CreatedAt.Format(time.RFC3339), route.UpdatedAt.Format(time.RFC3339)); err != nil {
		return nil, nil, nil, err
	}

	upsertedLinks := make([]*store.HubUserLink, 0, len(matchedEmails))
	for email := range matchedEmails {
		link := &store.HubUserLink{ID: adminStoreUserLinkID(email), HubID: toHubID, Email: email, IsDefault: true, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				hub_id = excluded.hub_id,
				email = excluded.email,
				is_default = excluded.is_default,
				updated_at = excluded.updated_at
		`, link.ID, link.HubID, link.Email, boolToInt(link.IsDefault), link.CreatedAt.Format(time.RFC3339), link.UpdatedAt.Format(time.RFC3339)); err != nil {
			return nil, nil, nil, err
		}
		upsertedLinks = append(upsertedLinks, link)
	}
	for _, item := range removeRoutes {
		if _, err := tx.ExecContext(ctx, `DELETE FROM hub_domain_routes WHERE id = ?`, item.ID); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, item := range removeLinks {
		if _, err := tx.ExecContext(ctx, `DELETE FROM hub_user_links WHERE id = ?`, item.ID); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}
	committed = true
	return removeRoutes, removeLinks, upsertedLinks, nil
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

func normalizeStoreEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func primaryStoreUserLinkID(hubID, email string) string {
	sum := sha256.Sum256([]byte(normalizeStoreEmail(email)))
	return "hul_user_" + strings.TrimSpace(hubID) + "_" + hex.EncodeToString(sum[:])[:16]
}

func adminStoreUserLinkID(email string) string {
	sum := sha256.Sum256([]byte(normalizeStoreEmail(email)))
	return "hul_admin_" + hex.EncodeToString(sum[:])[:20]
}

func emailWildcardMatch(pattern, email string) bool {
	pattern = normalizeStoreEmail(pattern)
	email = normalizeStoreEmail(email)
	if strings.HasPrefix(pattern, "@") {
		pattern = "*" + pattern
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == email
	}
	if parts[0] != "" && !strings.HasPrefix(email, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue
		}
		idx := strings.Index(email[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(email[pos:], last)
}

func timePtrString(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.Format(time.RFC3339)
}

// 闂傚倸鍊风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛?Gossip Repository 闂傚倸鍊风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛瀛樼矒缁犳牕顫忓ú顏勭闁圭粯甯掓潏鍛存⒑缁嬫鍎愰柟鐟版喘瀵顓兼径濠勵槯婵犮垼娉涢敃锝嗙珶閺囥垺鈷掑ù锝囶焾閺嗛亶鏌涘Ο鑽ょ煉鐎规洘鍨块獮妯肩磼濡厧甯楅梻浣侯焾缁绘劙藝椤栨稓顩插Δ锝呭暞閳锋垿鏌涢幇顓炵祷閻㈩垬鍔戦弻娑氣偓锝庡亝瀹曞矂鏌＄仦鐣屝х€规洘顨嗗鍕節娴ｅ壊妫滈梻鍌氬€风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛瀛樼矒缁犳牕顫忓ú顏勭闁圭粯甯掓潏鍛存⒑缁嬫鍎愰柟鐟版喘瀵顓兼径濠勵槯婵犮垼娉涢敃锝嗙珶閺囥垺鈷掑ù锝囶焾閺嗛亶鏌涘Ο鑽ょ煉鐎规洘鍨块獮妯肩磼濡厧甯楅梻浣侯焾缁绘劙藝椤栨稓顩插Δ锝呭暞閳锋垿鏌涢幇顓炵祷閻㈩垬鍔戦弻娑氣偓锝庡亝瀹曞矂鏌＄仦鐣屝х€规洘顨嗗鍕節娴ｅ壊妫滈梻鍌氬€风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛瀛樼矒缁犳牕顫忓ú顏勭闁圭粯甯掓潏鍛存⒑缁嬫鍎愰柟鐟版喘瀵顓兼径濠勵槯婵犮垼娉涢敃锝嗙珶閺囥垺鈷掑ù锝囶焾閺嗛亶鏌涘Ο鑽ょ煉鐎规洘鍨块獮妯肩磼濡厧甯楅梻浣侯焾缁绘劙藝椤栨稓顩插Δ锝呭暞閳锋垿鏌涢幇顓炵祷閻㈩垬鍔戦弻娑氣偓锝庡亝瀹曞矂鏌＄仦鐣屝х€规洘顨嗗鍕節娴ｅ壊妫滈梻鍌氬€风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛瀛樼矒缁犳牕顫忓ú顏勭闁圭粯甯掓潏鍛存⒑缁嬫鍎愰柟鐟版喘瀵顓兼径濠勵槯婵犮垼娉涢敃锝嗙珶閺囥垺鈷掑ù锝囶焾閺嗛亶鏌涘Ο鑽ょ煉鐎规洘鍨块獮妯肩磼濡厧甯楅梻浣侯焾缁绘劙藝椤栨稓顩插Δ锝呭暞閳锋垿鏌涢幇顓炵祷閻㈩垬鍔戦弻娑氣偓锝庡亝瀹曞矂鏌＄仦鐣屝х€规洘顨嗗鍕節娴ｅ壊妫滈梻鍌氬€风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛瀛樼矒缁犳牕顫忓ú顏勭闁圭粯甯掓潏鍛存⒑缁嬫鍎愰柟鐟版喘瀵顓兼径濠勵槯婵犮垼娉涢敃锝嗙珶閺囥垺鈷掑ù锝囶焾閺嗛亶鏌涘Ο鑽ょ煉鐎规洘鍨块獮妯肩磼濡厧甯楅梻浣侯焾缁绘劙藝椤栨稓顩插Δ锝呭暞閳锋垿鏌涢幇顓炵祷閻㈩垬鍔戦弻娑氣偓锝庡亝瀹曞矂鏌＄仦鐣屝х€规洘顨嗗鍕節娴ｅ壊妫滈梻鍌氬€风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛瀛樼矒缁犳牕顫忓ú顏勭闁圭粯甯掓潏鍛存⒑缁嬫鍎愰柟鐟版喘瀵顓兼径濠勵槯婵犮垼娉涢敃锝嗙珶閺囥垺鈷掑ù锝囶焾閺嗛亶鏌涘Ο鑽ょ煉鐎规洘鍨块獮妯肩磼濡厧甯楅梻浣侯焾缁绘劙藝椤栨稓顩插Δ锝呭暞閳锋垿鏌涢幇顓炵祷閻㈩垬鍔戦弻娑氣偓锝庡亝瀹曞矂鏌＄仦鐣屝х€规洘顨嗗鍕節娴ｅ壊妫滈梻鍌氬€风粈渚€宕崸妤€鍌ㄦ繝濠傜墕绾惧鏌熼崜褏甯涢柣鎾冲暣閺屾稖绠涢幙鍐┬︽繛?
func (r *gossipRepo) CreatePost(ctx context.Context, post *store.GossipPost) error {
	return execWrite(ctx, r.batch, r.db,
		`INSERT INTO gossip_posts (id, machine_id, user_email, nickname, content, category, score, votes, locked, flagged, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`,
		post.ID, post.MachineID, post.UserEmail, post.Nickname, post.Content, post.Category, boolToInt(post.Flagged), post.CreatedAt.Format(time.RFC3339))
}

func (r *gossipRepo) ListPosts(ctx context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gossip_posts WHERE flagged = 0`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, machine_id, user_email, nickname, content, category, score, votes, locked, flagged, created_at
		 FROM gossip_posts WHERE flagged = 0 ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []*store.GossipPost
	for rows.Next() {
		var p store.GossipPost
		var locked, flagged int
		var createdAt string
		if err := rows.Scan(&p.ID, &p.MachineID, &p.UserEmail, &p.Nickname, &p.Content, &p.Category, &p.Score, &p.Votes, &locked, &flagged, &createdAt); err != nil {
			return nil, 0, err
		}
		p.Locked = locked != 0
		p.Flagged = flagged != 0
		p.CreatedAt = mustParseTime(createdAt)
		items = append(items, &p)
	}
	return items, total, rows.Err()
}

func (r *gossipRepo) ListAllPosts(ctx context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gossip_posts`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, machine_id, user_email, nickname, content, category, score, votes, locked, flagged, created_at
		 FROM gossip_posts ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []*store.GossipPost
	for rows.Next() {
		var p store.GossipPost
		var locked, flagged int
		var createdAt string
		if err := rows.Scan(&p.ID, &p.MachineID, &p.UserEmail, &p.Nickname, &p.Content, &p.Category, &p.Score, &p.Votes, &locked, &flagged, &createdAt); err != nil {
			return nil, 0, err
		}
		p.Locked = locked != 0
		p.Flagged = flagged != 0
		p.CreatedAt = mustParseTime(createdAt)
		items = append(items, &p)
	}
	return items, total, rows.Err()
}

func (r *gossipRepo) GetPost(ctx context.Context, id string) (*store.GossipPost, error) {
	var p store.GossipPost
	var locked, flagged int
	var createdAt string
	err := r.readDB.QueryRowContext(ctx,
		`SELECT id, machine_id, user_email, nickname, content, category, score, votes, locked, flagged, created_at
		 FROM gossip_posts WHERE id = ?`, id).Scan(
		&p.ID, &p.MachineID, &p.UserEmail, &p.Nickname, &p.Content, &p.Category, &p.Score, &p.Votes, &locked, &flagged, &createdAt)
	if err != nil {
		return nil, err
	}
	p.Locked = locked != 0
	p.Flagged = flagged != 0
	p.CreatedAt = mustParseTime(createdAt)
	return &p, nil
}

func (r *gossipRepo) DeletePost(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM gossip_comments WHERE post_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gossip_posts WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *gossipRepo) LockPost(ctx context.Context, id string, locked bool) error {
	return execWrite(ctx, r.batch, r.db, `UPDATE gossip_posts SET locked = ? WHERE id = ?`, boolToInt(locked), id)
}

func (r *gossipRepo) FlagPost(ctx context.Context, id string, flagged bool) error {
	return execWrite(ctx, r.batch, r.db, `UPDATE gossip_posts SET flagged = ? WHERE id = ?`, boolToInt(flagged), id)
}

func (r *gossipRepo) ReplaceAll(ctx context.Context, posts []*store.GossipPost, comments []*store.GossipComment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM gossip_comments`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gossip_posts`); err != nil {
		return err
	}
	for _, post := range posts {
		if post == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO gossip_posts (id, machine_id, user_email, nickname, content, category, score, votes, locked, flagged, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, post.ID, post.MachineID, post.UserEmail, post.Nickname, post.Content, post.Category, post.Score, post.Votes, boolToInt(post.Locked), boolToInt(post.Flagged), post.CreatedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	for _, comment := range comments {
		if comment == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO gossip_comments (id, post_id, machine_id, user_email, nickname, content, rating, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, comment.ID, comment.PostID, comment.MachineID, comment.UserEmail, comment.Nickname, comment.Content, comment.Rating, comment.CreatedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *gossipRepo) ListFlaggedPosts(ctx context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gossip_posts WHERE flagged = 1`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, machine_id, user_email, nickname, content, category, score, votes, locked, flagged, created_at
		 FROM gossip_posts WHERE flagged = 1 ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []*store.GossipPost
	for rows.Next() {
		var p store.GossipPost
		var locked, flagged int
		var createdAt string
		if err := rows.Scan(&p.ID, &p.MachineID, &p.UserEmail, &p.Nickname, &p.Content, &p.Category, &p.Score, &p.Votes, &locked, &flagged, &createdAt); err != nil {
			return nil, 0, err
		}
		p.Locked = locked != 0
		p.Flagged = flagged != 0
		p.CreatedAt = mustParseTime(createdAt)
		items = append(items, &p)
	}
	return items, total, rows.Err()
}

func (r *gossipRepo) CreateComment(ctx context.Context, comment *store.GossipComment) error {
	return execWrite(ctx, r.batch, r.db,
		`INSERT INTO gossip_comments (id, post_id, machine_id, user_email, nickname, content, rating, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		comment.ID, comment.PostID, comment.MachineID, comment.UserEmail, comment.Nickname, comment.Content, comment.Rating, comment.CreatedAt.Format(time.RFC3339))
}

func (r *gossipRepo) ListComments(ctx context.Context, postID string, offset, limit int) ([]*store.GossipComment, int, error) {
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gossip_comments WHERE post_id = ?`, postID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, post_id, machine_id, user_email, nickname, content, rating, created_at
		 FROM gossip_comments WHERE post_id = ? ORDER BY created_at ASC LIMIT ? OFFSET ?`, postID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []*store.GossipComment
	for rows.Next() {
		var c store.GossipComment
		var createdAt string
		if err := rows.Scan(&c.ID, &c.PostID, &c.MachineID, &c.UserEmail, &c.Nickname, &c.Content, &c.Rating, &createdAt); err != nil {
			return nil, 0, err
		}
		c.CreatedAt = mustParseTime(createdAt)
		items = append(items, &c)
	}
	return items, total, rows.Err()
}

func (r *gossipRepo) DeleteComment(ctx context.Context, id string) error {
	return execWrite(ctx, r.batch, r.db, `DELETE FROM gossip_comments WHERE id = ?`, id)
}

func (r *gossipRepo) UpdatePostScore(ctx context.Context, postID string) error {
	return execWrite(ctx, r.batch, r.db,
		`UPDATE gossip_posts SET score = COALESCE((SELECT SUM(rating) FROM gossip_comments WHERE post_id = ? AND rating > 0), 0),
		 votes = COALESCE((SELECT COUNT(*) FROM gossip_comments WHERE post_id = ? AND rating > 0), 0) WHERE id = ?`,
		postID, postID, postID)
}

// RateComment performs an atomic check-insert-update in a single transaction
// on the write connection, bypassing the batcher to eliminate the race window.
func (r *gossipRepo) RateComment(ctx context.Context, comment *store.GossipComment) error {
	// Use a raw connection with BEGIN IMMEDIATE to acquire the write lock
	// upfront, serializing concurrent rating attempts and preventing
	// deadlocks with the batcher's flush transactions.
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	// Check if already rated within the write connection
	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM gossip_comments WHERE post_id = ? AND machine_id = ? AND rating > 0`,
		comment.PostID, comment.MachineID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return store.ErrAlreadyRated
	}

	// Insert the rating comment 闂?unique index acts as final safety net
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO gossip_comments (id, post_id, machine_id, user_email, nickname, content, rating, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		comment.ID, comment.PostID, comment.MachineID, comment.UserEmail,
		comment.Nickname, comment.Content, comment.Rating, comment.CreatedAt.Format(time.RFC3339)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return store.ErrAlreadyRated
		}
		return err
	}

	// Update post score and votes in the same transaction
	if _, err := conn.ExecContext(ctx,
		`UPDATE gossip_posts SET
		 score = COALESCE((SELECT SUM(rating) FROM gossip_comments WHERE post_id = ? AND rating > 0), 0),
		 votes = COALESCE((SELECT COUNT(*) FROM gossip_comments WHERE post_id = ? AND rating > 0), 0)
		 WHERE id = ?`,
		comment.PostID, comment.PostID, comment.PostID); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *gossipRepo) HasRated(ctx context.Context, postID, machineID string) (bool, error) {
	var count int
	err := r.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM gossip_comments WHERE post_id = ? AND machine_id = ? AND rating > 0`,
		postID, machineID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *failureEventLogRepo) Create(ctx context.Context, log *store.FailureEventLog) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO failure_event_logs (id, category, event_code, message, entity_id, email, client_ip, details_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.Category, log.EventCode, log.Message, log.EntityID, log.Email, log.ClientIP, log.DetailsJSON, log.CreatedAt.Format(time.RFC3339))
	return err
}

func (r *failureEventLogRepo) List(ctx context.Context, filter store.FailureEventLogFilter) ([]*store.FailureEventLog, int, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	keyword := strings.TrimSpace(filter.Keyword)
	category := strings.TrimSpace(filter.Category)
	where := make([]string, 0, 2)
	args := make([]any, 0, 8)
	if category != "" {
		where = append(where, "category = ?")
		args = append(args, category)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		where = append(where, "(event_code LIKE ? OR message LIKE ? OR entity_id LIKE ? OR email LIKE ? OR client_ip LIKE ? OR details_json LIKE ?)")
		args = append(args, like, like, like, like, like, like)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM failure_event_logs`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, category, event_code, message, entity_id, email, client_ip, details_json, created_at FROM failure_event_logs`+whereSQL+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*store.FailureEventLog, 0, limit)
	for rows.Next() {
		var item store.FailureEventLog
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Category, &item.EventCode, &item.Message, &item.EntityID, &item.Email, &item.ClientIP, &item.DetailsJSON, &createdAt); err != nil {
			return nil, 0, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, total, rows.Err()
}
