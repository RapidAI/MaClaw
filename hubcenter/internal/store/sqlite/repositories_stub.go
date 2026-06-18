package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
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

const hubInstanceSelectColumns = `id, installation_id, hub_origin, default_signup_scope, owner_email, name, description, base_url, host, port, visibility, enrollment_mode, corporate_email_domain,
	       accept_public_signup, status, is_disabled, disabled_reason, capabilities_json, registration_policy_json, hub_secret_hash,
	       invitation_code_required, digital_employee_quota, digital_employee_authorization_enabled, digital_employee_authorization_expires_at, allow_external_providers, last_seen_at, created_at, updated_at`

const hubInstanceSelectColumnsH = `h.id, h.installation_id, h.hub_origin, h.default_signup_scope, h.owner_email, h.name, h.description, h.base_url, h.host, h.port, h.visibility,
	       h.enrollment_mode, h.corporate_email_domain, h.accept_public_signup, h.status, h.is_disabled, h.disabled_reason,
	       h.capabilities_json, h.registration_policy_json, h.hub_secret_hash, h.invitation_code_required, h.digital_employee_quota, h.digital_employee_authorization_enabled, h.digital_employee_authorization_expires_at, h.allow_external_providers, h.last_seen_at, h.created_at, h.updated_at`

func NewStore(p *Provider) *store.Store {
	return &store.Store{
		Admins:               &adminRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		System:               &systemRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		AdminAudit:           &adminAuditRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		FailureLogs:          &failureEventLogRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Hubs:                 &hubRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HubUserLinks:         &hubUserLinkRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HubDomainRoutes:      &hubDomainRouteRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		BlockedEmails:        &blockedEmailRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		BlockedIPs:           &blockedIPRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		InvitationCodeRoutes: &invitationCodeRouteRepo{db: p.Write, readDB: p.Read},
		HASyncOps:            &haSyncOpRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HAPeerCursors:        &haPeerCursorRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HAEntityVersions:     &haEntityVersionRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		HAHeartbeatSync:      &haHeartbeatSyncStateRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Gossip:               &gossipRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		News:                 &newsRepo{db: p.Write, readDB: p.Read, batch: p.batch},
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
	var digitalEmployeeAuthEnabled int
	var digitalEmployeeAuthExpiresAt sql.NullString
	var allowExternalProviders int
	var lastSeen sql.NullString
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&item.ID,
		&item.InstallationID,
		&item.HubOrigin,
		&item.DefaultSignupScope,
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
		&item.RegistrationPolicyJSON,
		&item.HubSecretHash,
		&invitationCodeRequired,
		&item.DigitalEmployeeQuota,
		&digitalEmployeeAuthEnabled,
		&digitalEmployeeAuthExpiresAt,
		&allowExternalProviders,
		&lastSeen,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	item.IsDisabled = isDisabled == 1
	item.AcceptPublicSignup = acceptPublicSignup == 1
	item.InvitationCodeRequired = invitationCodeRequired == 1
	item.DigitalEmployeeAuthorizationEnabled = digitalEmployeeAuthEnabled == 1
	item.AllowExternalProviders = allowExternalProviders == 1
	if digitalEmployeeAuthExpiresAt.Valid && strings.TrimSpace(digitalEmployeeAuthExpiresAt.String) != "" {
		if ts, err := time.Parse(time.RFC3339, digitalEmployeeAuthExpiresAt.String); err == nil {
			item.DigitalEmployeeAuthorizationExpiresAt = &ts
		}
	}
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
	var tenantID sql.NullString
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.ID, &item.HubID, &tenantID, &item.Email, &isDefault, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	item.TenantID = tenantID.String
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
	if err := scanner.Scan(&item.ID, &item.HubID, &item.TenantID, &item.Domain, &enabled, &item.Priority, &createdAt, &updatedAt); err != nil {
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

func (r *systemRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM system_settings`).Scan(&count)
	return count, err
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
	normalizeHubInstanceEndpoint(hub)
	normalizeHubRegistrationPolicyFields(hub)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO hub_instances (
			id, installation_id, hub_origin, default_signup_scope, owner_email, name, description, base_url, host, port, visibility, enrollment_mode, corporate_email_domain,
			accept_public_signup, status, is_disabled, disabled_reason, capabilities_json, registration_policy_json, hub_secret_hash,
			invitation_code_required, digital_employee_quota, digital_employee_authorization_enabled, digital_employee_authorization_expires_at, allow_external_providers, last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		hub.ID,
		hub.InstallationID,
		hub.HubOrigin,
		hub.DefaultSignupScope,
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
		hub.RegistrationPolicyJSON,
		hub.HubSecretHash,
		boolToInt(hub.InvitationCodeRequired),
		hub.DigitalEmployeeQuota,
		boolToInt(hub.DigitalEmployeeAuthorizationEnabled),
		timePtrString(hub.DigitalEmployeeAuthorizationExpiresAt),
		boolToInt(hub.AllowExternalProviders),
		timePtrString(hub.LastSeenAt),
		hub.CreatedAt.Format(time.RFC3339),
		hub.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *hubRepo) ReplaceConflictingHubInstance(ctx context.Context, hub *store.HubInstance) error {
	if hub == nil {
		return nil
	}
	normalizeHubInstanceEndpoint(hub)
	normalizeHubRegistrationPolicyFields(hub)
	if strings.TrimSpace(hub.ID) == "" {
		return errors.New("missing hub id")
	}
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

	rows, err := conn.QueryContext(ctx, `
		SELECT id
		FROM hub_instances
		WHERE id <> ?
		  AND ((? <> '' AND installation_id = ?)
		    OR (? <> '' AND base_url = ?)
		    OR (? <> '' AND ? > 0 AND host = ? AND port = ?))
	`, hub.ID, hub.InstallationID, hub.InstallationID, hub.BaseURL, hub.BaseURL, hub.Host, hub.Port, hub.Host, hub.Port)
	if err != nil {
		return err
	}
	conflictIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		conflictIDs = append(conflictIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(conflictIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(conflictIDs)), ",")
		args := make([]any, 0, len(conflictIDs)+1)
		args = append(args, hub.ID)
		for _, id := range conflictIDs {
			args = append(args, id)
		}
		if _, err := conn.ExecContext(ctx, `UPDATE hub_user_links SET hub_id = ? WHERE hub_id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `UPDATE hub_domain_routes SET hub_id = ? WHERE hub_id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
		deleteArgs := make([]any, 0, len(conflictIDs))
		for _, id := range conflictIDs {
			deleteArgs = append(deleteArgs, id)
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM hub_instances WHERE id IN (`+placeholders+`)`, deleteArgs...); err != nil {
			return err
		}
	}

	_, err = conn.ExecContext(ctx, `
		INSERT INTO hub_instances (
			id, installation_id, hub_origin, default_signup_scope, owner_email, name, description, base_url, host, port, visibility, enrollment_mode, corporate_email_domain,
			accept_public_signup, status, is_disabled, disabled_reason, capabilities_json, registration_policy_json, hub_secret_hash,
			invitation_code_required, digital_employee_quota, digital_employee_authorization_enabled, digital_employee_authorization_expires_at, allow_external_providers, last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			installation_id = excluded.installation_id,
			hub_origin = excluded.hub_origin,
			default_signup_scope = excluded.default_signup_scope,
			owner_email = excluded.owner_email,
			name = excluded.name,
			description = excluded.description,
			base_url = excluded.base_url,
			host = excluded.host,
			port = excluded.port,
			visibility = excluded.visibility,
			enrollment_mode = excluded.enrollment_mode,
			corporate_email_domain = excluded.corporate_email_domain,
			accept_public_signup = excluded.accept_public_signup,
			status = excluded.status,
			is_disabled = excluded.is_disabled,
			disabled_reason = excluded.disabled_reason,
			capabilities_json = excluded.capabilities_json,
			registration_policy_json = excluded.registration_policy_json,
			hub_secret_hash = excluded.hub_secret_hash,
			invitation_code_required = excluded.invitation_code_required,
			digital_employee_quota = excluded.digital_employee_quota,
			digital_employee_authorization_enabled = excluded.digital_employee_authorization_enabled,
			digital_employee_authorization_expires_at = excluded.digital_employee_authorization_expires_at,
			allow_external_providers = excluded.allow_external_providers,
			last_seen_at = excluded.last_seen_at,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`,
		hub.ID,
		hub.InstallationID,
		hub.HubOrigin,
		hub.DefaultSignupScope,
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
		hub.RegistrationPolicyJSON,
		hub.HubSecretHash,
		boolToInt(hub.InvitationCodeRequired),
		hub.DigitalEmployeeQuota,
		boolToInt(hub.DigitalEmployeeAuthorizationEnabled),
		timePtrString(hub.DigitalEmployeeAuthorizationExpiresAt),
		boolToInt(hub.AllowExternalProviders),
		timePtrString(hub.LastSeenAt),
		hub.CreatedAt.Format(time.RFC3339),
		hub.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func normalizeHubInstanceEndpoint(hub *store.HubInstance) {
	if hub == nil {
		return
	}
	hub.InstallationID = strings.TrimSpace(hub.InstallationID)
	hub.Host = strings.ToLower(strings.TrimSpace(hub.Host))
	hub.BaseURL = normalizeHubInstanceBaseURL(hub.BaseURL)
}

func normalizeHubRegistrationPolicyFields(hub *store.HubInstance) {
	if hub == nil {
		return
	}
	hub.HubOrigin = strings.TrimSpace(hub.HubOrigin)
	if hub.HubOrigin == "" {
		hub.HubOrigin = "self_hosted"
	}
	hub.DefaultSignupScope = strings.TrimSpace(hub.DefaultSignupScope)
	if hub.DefaultSignupScope == "" {
		hub.DefaultSignupScope = "domain_restricted"
	}
	hub.RegistrationPolicyJSON = strings.TrimSpace(hub.RegistrationPolicyJSON)
	if hub.RegistrationPolicyJSON == "" {
		hub.RegistrationPolicyJSON = "{}"
	}
}

func normalizeHubInstanceBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func (r *hubRepo) GetByID(ctx context.Context, id string) (*store.HubInstance, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT `+hubInstanceSelectColumns+`
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
	installationID = strings.TrimSpace(installationID)
	row := r.readDB.QueryRowContext(ctx, `
		SELECT `+hubInstanceSelectColumns+`
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

func (r *hubRepo) GetByEndpoint(ctx context.Context, host string, port int, baseURL string) (*store.HubInstance, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	baseURL = normalizeHubInstanceBaseURL(baseURL)
	if host != "" && port > 0 {
		item, err := r.getOneHubByEndpointClause(ctx, "host = ? AND port = ?", host, port)
		if err != nil || item != nil {
			return item, err
		}
	}
	if baseURL != "" {
		return r.getOneHubByEndpointClause(ctx, "base_url = ?", baseURL)
	}
	return nil, nil
}

func (r *hubRepo) getOneHubByEndpointClause(ctx context.Context, clause string, args ...any) (*store.HubInstance, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT `+hubInstanceSelectColumns+`
		FROM hub_instances
		WHERE `+clause+`
		ORDER BY updated_at DESC
		LIMIT 1
	`, args...)

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
		SELECT DISTINCT `+hubInstanceSelectColumnsH+`
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
		SELECT `+hubInstanceSelectColumns+`
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

func (r *hubRepo) ListUserInventoryRefreshCandidates(ctx context.Context) ([]*store.HubInstance, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT `+hubInstanceSelectColumns+`
		FROM hub_instances
		WHERE trim(base_url) <> ''
		  AND trim(hub_secret_hash) <> ''
		  AND (lower(trim(base_url)) LIKE 'http://%' OR lower(trim(base_url)) LIKE 'https://%')
		ORDER BY updated_at DESC, id ASC
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

func (r *hubRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.HubInstance, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT `+hubInstanceSelectColumns+`
		FROM hub_instances
		ORDER BY updated_at DESC, id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
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

func (r *hubRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM hub_instances`).Scan(&count)
	return count, err
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
	normalizeHubInstanceEndpoint(hub)
	normalizeHubRegistrationPolicyFields(hub)
	_, err := r.db.ExecContext(ctx, `
		UPDATE hub_instances
		SET installation_id = ?, hub_origin = ?, default_signup_scope = ?, owner_email = ?, name = ?, description = ?, base_url = ?,
		    host = ?, port = ?, visibility = ?, enrollment_mode = ?, corporate_email_domain = ?, accept_public_signup = ?, status = ?,
		    is_disabled = ?, disabled_reason = ?, capabilities_json = ?, registration_policy_json = ?, hub_secret_hash = ?,
		    invitation_code_required = ?, digital_employee_quota = ?, digital_employee_authorization_enabled = ?,
		    digital_employee_authorization_expires_at = ?, allow_external_providers = ?, last_seen_at = ?, updated_at = ?
		WHERE id = ?
	`,
		hub.InstallationID,
		hub.HubOrigin,
		hub.DefaultSignupScope,
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
		hub.RegistrationPolicyJSON,
		hub.HubSecretHash,
		boolToInt(hub.InvitationCodeRequired),
		hub.DigitalEmployeeQuota,
		boolToInt(hub.DigitalEmployeeAuthorizationEnabled),
		timePtrString(hub.DigitalEmployeeAuthorizationExpiresAt),
		boolToInt(hub.AllowExternalProviders),
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

func (r *hubRepo) UpdateDigitalEmployeeAuthorization(ctx context.Context, hubID string, quota int, enabled bool, expiresAt *time.Time, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE hub_instances
		SET digital_employee_quota = ?, digital_employee_authorization_enabled = ?, digital_employee_authorization_expires_at = ?, updated_at = ?
		WHERE id = ?
	`, quota, boolToInt(enabled), timePtrString(expiresAt), updatedAt.Format(time.RFC3339), hubID)
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
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE lower(email) = lower(?)
		ORDER BY is_default DESC, updated_at DESC
	`, strings.TrimSpace(email))
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

func (r *hubUserLinkRepo) ListByHubID(ctx context.Context, hubID string) ([]*store.HubUserLink, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE hub_id = ?
		ORDER BY updated_at DESC, id ASC
	`, strings.TrimSpace(hubID))
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
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
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

func (r *hubUserLinkRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.HubUserLink, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		ORDER BY updated_at DESC, id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
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

func (r *hubUserLinkRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM hub_user_links`).Scan(&count)
	return count, err
}

func (r *hubUserLinkRepo) ListUserCountsByHubTenant(ctx context.Context) ([]store.HubTenantUserCount, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT hub_id,
		       CASE WHEN trim(tenant_id) = 'tenant_default' THEN '' ELSE trim(tenant_id) END AS tenant_id,
		       COUNT(DISTINCT lower(trim(email))),
		       0
		FROM hub_user_links
		WHERE trim(hub_id) <> '' AND trim(email) <> ''
		GROUP BY hub_id, CASE WHEN trim(tenant_id) = 'tenant_default' THEN '' ELSE trim(tenant_id) END
		UNION ALL
		SELECT hub_id,
		       '',
		       COUNT(DISTINCT lower(trim(email))),
		       1
		FROM hub_user_links
		WHERE trim(hub_id) <> '' AND trim(email) <> ''
		GROUP BY hub_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]store.HubTenantUserCount, 0)
	for rows.Next() {
		var item store.HubTenantUserCount
		if err := rows.Scan(&item.HubID, &item.TenantID, &item.Count, &item.AllTenants); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *hubUserLinkRepo) ListUserDomainsByHubTenant(ctx context.Context) ([]store.HubTenantUserDomain, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT DISTINCT l.hub_id,
		       CASE WHEN trim(l.tenant_id) = 'tenant_default' THEN '' ELSE trim(l.tenant_id) END AS tenant_id,
		       lower(trim(substr(l.email, instr(l.email, '@') + 1))) AS domain
		FROM hub_user_links l
		LEFT JOIN hub_instances h ON h.id = l.hub_id
		WHERE trim(l.hub_id) <> ''
		  AND trim(l.email) <> ''
		  AND instr(l.email, '@') > 0
		  AND instr(l.email, '*') = 0
		  AND lower(trim(l.email)) <> lower(trim(coalesce(h.owner_email, '')))
		ORDER BY hub_id, tenant_id, domain
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]store.HubTenantUserDomain, 0)
	for rows.Next() {
		var item store.HubTenantUserDomain
		if err := rows.Scan(&item.HubID, &item.TenantID, &item.Domain); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *hubUserLinkRepo) ListUserFirstSeen(ctx context.Context) ([]store.HubUserFirstSeen, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT hub_id,
		       CASE WHEN trim(tenant_id) = 'tenant_default' THEN '' ELSE trim(tenant_id) END AS tenant_id,
		       lower(trim(email)) AS email,
		       MIN(CASE WHEN trim(created_at) = '' THEN updated_at ELSE created_at END) AS first_seen
		FROM hub_user_links
		WHERE trim(hub_id) <> ''
		  AND trim(email) <> ''
		  AND instr(email, '*') = 0
		GROUP BY hub_id, CASE WHEN trim(tenant_id) = 'tenant_default' THEN '' ELSE trim(tenant_id) END, lower(trim(email))
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]store.HubUserFirstSeen, 0)
	for rows.Next() {
		var item store.HubUserFirstSeen
		var firstSeen string
		if err := rows.Scan(&item.HubID, &item.TenantID, &item.Email, &firstSeen); err != nil {
			return nil, err
		}
		item.FirstSeen = mustParseTime(firstSeen)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *hubUserLinkRepo) ListMigrationSourceLinks(ctx context.Context, pattern, fromHubID, sourceTenantID, excludeHubID string) ([]*store.HubUserLink, error) {
	where, args := migrationSourceLinkWhere(pattern, fromHubID, sourceTenantID, excludeHubID)
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY email ASC, is_default DESC, updated_at DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*store.HubUserLink, 0)
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
		INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		link.ID,
		link.HubID,
		normalizeStoreTenantID(link.TenantID),
		link.Email,
		boolToInt(link.IsDefault),
		link.CreatedAt.Format(time.RFC3339),
		link.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *hubUserLinkRepo) Upsert(ctx context.Context, link *store.HubUserLink) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			tenant_id = excluded.tenant_id,
			email = excluded.email,
			is_default = excluded.is_default,
			updated_at = excluded.updated_at
	`,
		link.ID,
		link.HubID,
		normalizeStoreTenantID(link.TenantID),
		link.Email,
		boolToInt(link.IsDefault),
		link.CreatedAt.Format(time.RFC3339),
		link.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *hubUserLinkRepo) GetDefaultByEmail(ctx context.Context, email string) (*store.HubUserLink, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE lower(email) = lower(?) AND is_default = 1
		LIMIT 1
	`, strings.TrimSpace(email))

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

func (r *hubUserLinkRepo) DeleteByHubTenantEmail(ctx context.Context, hubID, tenantID, email string) ([]*store.HubUserLink, error) {
	tenantID = normalizeStoreTenantID(tenantID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE hub_id = ? AND (tenant_id = ? OR (? = '' AND tenant_id = 'tenant_default')) AND lower(email) = lower(?)
	`, strings.TrimSpace(hubID), tenantID, tenantID, strings.TrimSpace(email))
	if err != nil {
		return nil, err
	}
	var removed []*store.HubUserLink
	for rows.Next() {
		item, scanErr := scanHubUserLink(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		if item == nil || strings.HasPrefix(strings.TrimSpace(item.ID), "hul_owner_") || strings.HasPrefix(strings.TrimSpace(item.ID), "hul_admin_") {
			continue
		}
		removed = append(removed, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, item := range removed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM hub_user_links WHERE id = ?`, item.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return removed, nil
}

func (r *hubUserLinkRepo) DeleteByHubID(ctx context.Context, hubID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM hub_user_links
		WHERE hub_id = ?
	`, hubID)
	return err
}

func (r *hubUserLinkRepo) MigrateEmailToHub(ctx context.Context, email, fromHubID, sourceTenantID string, link *store.HubUserLink) ([]*store.HubUserLink, *store.HubUserLink, error) {
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
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE lower(email) = lower(?)
		ORDER BY is_default DESC, updated_at DESC
	`, strings.TrimSpace(email))
	if err != nil {
		return nil, nil, err
	}
	var remove []*store.HubUserLink
	targetExists := false
	targetTenantID := normalizeStoreTenantID(link.TenantID)
	sourceTenantID = normalizeStoreTenantID(sourceTenantID)
	for rows.Next() {
		item, scanErr := scanHubUserLink(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, nil, scanErr
		}
		if item == nil || strings.HasPrefix(strings.TrimSpace(item.ID), "hul_owner_") {
			continue
		}
		itemTenantID := normalizeStoreTenantID(item.TenantID)
		if sourceTenantID != "" && itemTenantID != sourceTenantID {
			continue
		}
		if itemTenantID == targetTenantID && strings.TrimSpace(item.HubID) == strings.TrimSpace(link.HubID) {
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
			INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				hub_id = excluded.hub_id,
				tenant_id = excluded.tenant_id,
				email = excluded.email,
				is_default = excluded.is_default,
				updated_at = excluded.updated_at
		`, link.ID, link.HubID, targetTenantID, link.Email, boolToInt(link.IsDefault), link.CreatedAt.Format(time.RFC3339), link.UpdatedAt.Format(time.RFC3339)); err != nil {
			return nil, nil, err
		}
		copy := *link
		copy.TenantID = targetTenantID
		upserted = &copy
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

func (r *hubUserLinkRepo) MigrateEmailPatternToHub(ctx context.Context, pattern, fromHubID, sourceTenantID, toHubID, targetTenantID string, now time.Time) ([]*store.HubUserLink, []*store.HubUserLink, error) {
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

	where, args := migrationSourceLinkWhere(pattern, fromHubID, sourceTenantID, "")
	rows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY email ASC, is_default DESC, updated_at DESC
	`, args...)
	if err != nil {
		return nil, nil, err
	}
	var remove []*store.HubUserLink
	matchedEmailsByTenant := map[string]map[string]struct{}{}
	sourceTenantID = normalizeStoreTenantID(sourceTenantID)
	targetTenantID = normalizeStoreTenantID(targetTenantID)
	for rows.Next() {
		item, scanErr := scanHubUserLink(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, nil, scanErr
		}
		if item == nil || strings.HasPrefix(strings.TrimSpace(item.ID), "hul_owner_") || !emailWildcardMatch(pattern, item.Email) {
			continue
		}
		itemTenantID := normalizeStoreTenantID(item.TenantID)
		if sourceTenantID != "" && itemTenantID != sourceTenantID {
			continue
		}
		writeTenantID := targetTenantID
		if writeTenantID == "" {
			writeTenantID = itemTenantID
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(toHubID) && itemTenantID == writeTenantID {
			continue
		}
		if strings.TrimSpace(fromHubID) != "" && strings.TrimSpace(item.HubID) != strings.TrimSpace(fromHubID) {
			continue
		}
		matchedEmailsByTenant[writeTenantID] = ensureEmailSet(matchedEmailsByTenant[writeTenantID])
		matchedEmailsByTenant[writeTenantID][normalizeStoreEmail(item.Email)] = struct{}{}
		remove = append(remove, item)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	upserted := make([]*store.HubUserLink, 0)
	for tenantID, emails := range matchedEmailsByTenant {
		for email := range emails {
			link := &store.HubUserLink{ID: adminStoreUserLinkIDForTenant(tenantID, email), HubID: toHubID, TenantID: tenantID, Email: email, IsDefault: tenantID == "", CreatedAt: now, UpdatedAt: now}
			if _, err := tx.ExecContext(ctx, `
		INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			tenant_id = excluded.tenant_id,
			email = excluded.email,
				is_default = excluded.is_default,
				updated_at = excluded.updated_at
		`, link.ID, link.HubID, link.TenantID, link.Email, boolToInt(link.IsDefault), link.CreatedAt.Format(time.RFC3339), link.UpdatedAt.Format(time.RFC3339)); err != nil {
				return nil, nil, err
			}
			upserted = append(upserted, link)
		}
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
		INSERT INTO hub_domain_routes (id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			tenant_id = excluded.tenant_id,
			domain = excluded.domain,
			enabled = excluded.enabled,
			priority = excluded.priority,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`,
		route.ID,
		route.HubID,
		normalizeStoreTenantID(route.TenantID),
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
		SELECT id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at
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

func (r *hubDomainRouteRepo) ListByHubID(ctx context.Context, hubID string) ([]*store.HubDomainRoute, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		WHERE hub_id = ?
		ORDER BY priority ASC, updated_at DESC, id ASC
	`, strings.TrimSpace(hubID))
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

func (r *hubDomainRouteRepo) ListEnabledByDomain(ctx context.Context, domain string) ([]*store.HubDomainRoute, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		WHERE domain = ? AND enabled = 1
		ORDER BY priority ASC, updated_at DESC, id ASC
	`, normalizeStoreDomain(domain))
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

func (r *hubDomainRouteRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.HubDomainRoute, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		ORDER BY priority ASC, updated_at DESC, id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
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

func (r *hubDomainRouteRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM hub_domain_routes`).Scan(&count)
	return count, err
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

func (r *hubDomainRouteRepo) MigrateDomainToHub(ctx context.Context, domain, fromHubID, sourceTenantID string, route *store.HubDomainRoute) ([]*store.HubDomainRoute, error) {
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
		SELECT id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		WHERE domain = ?
		ORDER BY priority ASC, updated_at DESC, id ASC
	`, domain)
	if err != nil {
		return nil, err
	}
	var remove []*store.HubDomainRoute
	sourceTenantID = normalizeStoreTenantID(sourceTenantID)
	targetTenantID := normalizeStoreTenantID(route.TenantID)
	if sourceTenantID == "" {
		sourceTenantID = targetTenantID
	}
	for rows.Next() {
		item, scanErr := scanHubDomainRoute(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		if item == nil || strings.TrimSpace(item.ID) == strings.TrimSpace(route.ID) {
			continue
		}
		if normalizeStoreTenantID(item.TenantID) != sourceTenantID {
			continue
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(route.HubID) && sourceTenantID == targetTenantID {
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
		INSERT INTO hub_domain_routes (id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			tenant_id = excluded.tenant_id,
			domain = excluded.domain,
			enabled = excluded.enabled,
			priority = excluded.priority,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, route.ID, route.HubID, normalizeStoreTenantID(route.TenantID), route.Domain, boolToInt(route.Enabled), route.Priority, route.CreatedAt.Format(time.RFC3339), route.UpdatedAt.Format(time.RFC3339)); err != nil {
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

func (r *hubDomainRouteRepo) MigrateDomainAndEmailPatternToHub(ctx context.Context, domain, pattern, fromHubID, sourceTenantID, toHubID, targetTenantID string, route *store.HubDomainRoute, now time.Time) ([]*store.HubDomainRoute, []*store.HubUserLink, []*store.HubUserLink, error) {
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
		SELECT id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at
		FROM hub_domain_routes
		WHERE domain = ?
		ORDER BY priority ASC, updated_at DESC, id ASC
	`, domain)
	if err != nil {
		return nil, nil, nil, err
	}
	var removeRoutes []*store.HubDomainRoute
	sourceTenantID = normalizeStoreTenantID(sourceTenantID)
	targetTenantID = normalizeStoreTenantID(targetTenantID)
	for routeRows.Next() {
		item, scanErr := scanHubDomainRoute(routeRows)
		if scanErr != nil {
			_ = routeRows.Close()
			return nil, nil, nil, scanErr
		}
		if item == nil || strings.TrimSpace(item.ID) == strings.TrimSpace(route.ID) {
			continue
		}
		itemTenantID := normalizeStoreTenantID(item.TenantID)
		if sourceTenantID != "" && itemTenantID != sourceTenantID {
			continue
		}
		if sourceTenantID == "" && itemTenantID != normalizeStoreTenantID(route.TenantID) {
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

	linkWhere, linkArgs := migrationSourceLinkWhere(pattern, fromHubID, sourceTenantID, "")
	linkRows, err := tx.QueryContext(ctx, `
		SELECT id, hub_id, tenant_id, email, is_default, created_at, updated_at
		FROM hub_user_links
		WHERE `+strings.Join(linkWhere, " AND ")+`
		ORDER BY email ASC, is_default DESC, updated_at DESC
	`, linkArgs...)
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
		itemTenantID := normalizeStoreTenantID(item.TenantID)
		if sourceTenantID != "" && itemTenantID != sourceTenantID {
			continue
		}
		writeTenantID := targetTenantID
		if writeTenantID == "" {
			writeTenantID = normalizeStoreTenantID(route.TenantID)
		}
		if strings.TrimSpace(item.HubID) == strings.TrimSpace(toHubID) && itemTenantID == writeTenantID {
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
		INSERT INTO hub_domain_routes (id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			tenant_id = excluded.tenant_id,
			domain = excluded.domain,
			enabled = excluded.enabled,
			priority = excluded.priority,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, route.ID, route.HubID, normalizeStoreTenantID(route.TenantID), route.Domain, boolToInt(route.Enabled), route.Priority, route.CreatedAt.Format(time.RFC3339), route.UpdatedAt.Format(time.RFC3339)); err != nil {
		return nil, nil, nil, err
	}

	upsertedLinks := make([]*store.HubUserLink, 0, len(matchedEmails))
	for email := range matchedEmails {
		writeTenantID := targetTenantID
		if writeTenantID == "" {
			writeTenantID = normalizeStoreTenantID(route.TenantID)
		}
		link := &store.HubUserLink{ID: adminStoreUserLinkIDForTenant(writeTenantID, email), HubID: toHubID, TenantID: writeTenantID, Email: email, IsDefault: writeTenantID == "", CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO hub_user_links (id, hub_id, tenant_id, email, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hub_id = excluded.hub_id,
			tenant_id = excluded.tenant_id,
			email = excluded.email,
				is_default = excluded.is_default,
				updated_at = excluded.updated_at
		`, link.ID, link.HubID, link.TenantID, link.Email, boolToInt(link.IsDefault), link.CreatedAt.Format(time.RFC3339), link.UpdatedAt.Format(time.RFC3339)); err != nil {
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

func (r *blockedEmailRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.BlockedEmail, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, email, reason, created_at, updated_at
		FROM blocked_emails
		ORDER BY updated_at DESC, id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
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

func (r *blockedEmailRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM blocked_emails`).Scan(&count)
	return count, err
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

func (r *blockedIPRepo) ListPage(ctx context.Context, offset, limit int) ([]*store.BlockedIP, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, ip, reason, created_at, updated_at
		FROM blocked_ips
		ORDER BY updated_at DESC, id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
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

func (r *blockedIPRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM blocked_ips`).Scan(&count)
	return count, err
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

func normalizeStoreDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, "@")
	return domain
}

func normalizeStoreTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "tenant_default" {
		return ""
	}
	return tenantID
}

func primaryStoreUserLinkID(hubID, email string) string {
	sum := sha256.Sum256([]byte(normalizeStoreEmail(email)))
	return "hul_user_" + strings.TrimSpace(hubID) + "_" + hex.EncodeToString(sum[:])[:16]
}

func adminStoreUserLinkID(email string) string {
	return adminStoreUserLinkIDForTenant("", email)
}

func adminStoreUserLinkIDForTenant(tenantID, email string) string {
	sum := sha256.Sum256([]byte(normalizeStoreEmail(email)))
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" {
		tenantSum := sha256.Sum256([]byte(tenantID))
		return "hul_admin_" + hex.EncodeToString(tenantSum[:])[:8] + "_" + hex.EncodeToString(sum[:])[:16]
	}
	return "hul_admin_" + hex.EncodeToString(sum[:])[:20]
}

func ensureEmailSet(in map[string]struct{}) map[string]struct{} {
	if in != nil {
		return in
	}
	return map[string]struct{}{}
}

func migrationSourceLinkWhere(pattern, fromHubID, sourceTenantID, excludeHubID string) ([]string, []any) {
	where := []string{"trim(hub_id) <> ''", "trim(email) <> ''", "id NOT LIKE 'hul_owner_%'", "id NOT LIKE 'hul_admin_%'"}
	args := make([]any, 0, 4)
	if like := emailPatternSQLLike(pattern); like != "" {
		where = append(where, "lower(trim(email)) LIKE ? ESCAPE '\\'")
		args = append(args, like)
	}
	if fromHubID = strings.TrimSpace(fromHubID); fromHubID != "" {
		where = append(where, "hub_id = ?")
		args = append(args, fromHubID)
	}
	if excludeHubID = strings.TrimSpace(excludeHubID); excludeHubID != "" {
		where = append(where, "hub_id <> ?")
		args = append(args, excludeHubID)
	}
	if sourceTenantID = normalizeStoreTenantID(sourceTenantID); sourceTenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, sourceTenantID)
	}
	return where, args
}

func emailPatternSQLLike(pattern string) string {
	pattern = normalizeStoreEmail(pattern)
	if pattern == "" || pattern == "*" {
		return ""
	}
	var b strings.Builder
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteByte('%')
		case '%', '_', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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

// 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾?Gossip Repository 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾鐎涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻?
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

func (r *gossipRepo) CountSnapshotRecords(ctx context.Context) (int64, error) {
	var posts int64
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM gossip_posts`).Scan(&posts); err != nil {
		return 0, err
	}
	var comments int64
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM gossip_comments`).Scan(&comments); err != nil {
		return 0, err
	}
	return posts + comments, nil
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

func (r *gossipRepo) DeleteFlaggedPosts(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM gossip_comments WHERE post_id IN (SELECT id FROM gossip_posts WHERE flagged = 1)`); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM gossip_posts WHERE flagged = 1`)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(deleted), nil
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
		INSERT INTO failure_event_logs (id, tenant_id, category, event_code, message, entity_id, email, client_ip, details_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, normalizeStoreTenantID(log.TenantID), log.Category, log.EventCode, log.Message, log.EntityID, log.Email, log.ClientIP, log.DetailsJSON, log.CreatedAt.Format(time.RFC3339))
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
	tenantID := normalizeStoreTenantID(filter.TenantID)
	where := make([]string, 0, 2)
	args := make([]any, 0, 8)
	if filter.TenantIDSet {
		if tenantID == "" {
			where = append(where, "(tenant_id = '' OR tenant_id = 'tenant_default')")
		} else {
			where = append(where, "tenant_id = ?")
			args = append(args, tenantID)
		}
	}
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
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, tenant_id, category, event_code, message, entity_id, email, client_ip, details_json, created_at FROM failure_event_logs`+whereSQL+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*store.FailureEventLog, 0, limit)
	for rows.Next() {
		var item store.FailureEventLog
		var createdAt string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Category, &item.EventCode, &item.Message, &item.EntityID, &item.Email, &item.ClientIP, &item.DetailsJSON, &createdAt); err != nil {
			return nil, 0, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

// ── InvitationCodeRoute repository ──

type invitationCodeRouteRepo struct {
	db     *sql.DB
	readDB *sql.DB
}

func (r *invitationCodeRouteRepo) Upsert(ctx context.Context, code string, hubID string, tenantID string) error {
	code = strings.TrimSpace(strings.ToUpper(code))
	if code == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO invitation_code_routes (code, hub_id, tenant_id, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(code) DO UPDATE SET hub_id = excluded.hub_id, tenant_id = excluded.tenant_id
	`, code, hubID, tenantID, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (r *invitationCodeRouteRepo) GetByCode(ctx context.Context, code string) (*store.InvitationCodeRoute, error) {
	code = strings.TrimSpace(strings.ToUpper(code))
	var item store.InvitationCodeRoute
	var createdAt string
	err := r.readDB.QueryRowContext(ctx, `SELECT code, hub_id, tenant_id, created_at FROM invitation_code_routes WHERE code = ?`, code).
		Scan(&item.Code, &item.HubID, &item.TenantID, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &item, nil
}

func (r *invitationCodeRouteRepo) DeleteByCode(ctx context.Context, code string) error {
	code = strings.TrimSpace(strings.ToUpper(code))
	_, err := r.db.ExecContext(ctx, `DELETE FROM invitation_code_routes WHERE code = ?`, code)
	return err
}

func (r *invitationCodeRouteRepo) DeleteByHubID(ctx context.Context, hubID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM invitation_code_routes WHERE hub_id = ?`, hubID)
	return err
}

func (r *invitationCodeRouteRepo) ListAll(ctx context.Context) ([]*store.InvitationCodeRoute, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT code, hub_id, tenant_id, created_at FROM invitation_code_routes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*store.InvitationCodeRoute
	for rows.Next() {
		var item store.InvitationCodeRoute
		var createdAt string
		if err := rows.Scan(&item.Code, &item.HubID, &item.TenantID, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}
