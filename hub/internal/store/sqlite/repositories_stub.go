package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"golang.org/x/text/unicode/norm"
)

type tenantRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
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
type userRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type enrollmentRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type emailBlockRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type machineRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
	coalesce   *WriteCoalescer
}
type viewerTokenRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type loginTokenRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type invitationCodeRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type sessionRepo struct {
	db, readDB              *sql.DB
	batch                   *writeBatcher
	coalesce                *WriteCoalescer
	heartbeatCleanupCounter atomic.Int32
}
type emailInviteRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type userReferralRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type llmPromptCacheRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}
type knowledgeShareRepo struct {
	db, readDB *sql.DB
	batch      *writeBatcher
}

func NewStore(p *Provider) *store.Store {
	return &store.Store{
		Tenants:         &tenantRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Admins:          &adminRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		System:          &systemRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		AdminAudit:      &adminAuditRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		FailureLogs:     &failureEventLogRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Users:           &userRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Enrollments:     &enrollmentRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		EmailBlocks:     &emailBlockRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		InvitationCodes: &invitationCodeRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Machines:        &machineRepo{db: p.Write, readDB: p.Read, batch: p.batch, coalesce: p.coalesce},
		ViewerTokens:    &viewerTokenRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		LoginTokens:     &loginTokenRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Sessions:        &sessionRepo{db: p.Write, readDB: p.Read, batch: p.batch, coalesce: p.coalesce},
		EmailInvites:    &emailInviteRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		UserReferrals:   &userReferralRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		WorkflowRepo:    &workflowRepo{db: p.Write, readDB: p.Read},
		LLMPromptCache:  &llmPromptCacheRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		KnowledgeShares: &knowledgeShareRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		DigitalAssets:   &digitalAssetRepo{db: p.Write, readDB: p.Read, batch: p.batch},
	}
}

// NewUserReferralRepository exposes the referral store to the HTTP layer when
// the router is constructed from an existing Hub SQL connection.
func NewUserReferralRepository(db, readDB *sql.DB) store.UserReferralRepository {
	if readDB == nil {
		readDB = db
	}
	return &userReferralRepo{db: db, readDB: readDB}
}

func normalizeAdminScope(scope string) string {
	if strings.TrimSpace(scope) == "tenant" {
		return "tenant"
	}
	return "global"
}

func normalizeAdminRole(scope string, role string) string {
	role = strings.TrimSpace(role)
	if role != "" {
		return role
	}
	if normalizeAdminScope(scope) == "tenant" {
		return "tenant_owner"
	}
	return "global_owner"
}

func normalizeTenantID(tenantID string) string {
	return store.NormalizeTenantID(tenantID)
}

func nullableTimeString(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func execWrite(ctx context.Context, batch *writeBatcher, db *sql.DB, query string, args ...any) error {
	if batch != nil {
		return batch.ExecContext(ctx, query, args...)
	}
	_, err := db.ExecContext(ctx, query, args...)
	return err
}

func (r *adminRepo) Create(ctx context.Context, admin *store.AdminUser) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO admin_users (id, username, password_hash, email, scope, role, tenant_id, display_name, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		admin.ID,
		admin.Username,
		admin.PasswordHash,
		admin.Email,
		normalizeAdminScope(admin.Scope),
		normalizeAdminRole(admin.Scope, admin.Role),
		admin.TenantID,
		admin.DisplayName,
		admin.Status,
		admin.CreatedAt.Format(time.RFC3339),
		admin.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *adminRepo) GetByUsername(ctx context.Context, username string) (*store.AdminUser, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, username, password_hash, email, scope, role, tenant_id, display_name, status, created_at, updated_at
		 FROM admin_users WHERE username = ? AND scope = 'global' AND tenant_id = ''`,
		strings.TrimSpace(username),
	)

	var (
		admin                store.AdminUser
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&admin.ID,
		&admin.Username,
		&admin.PasswordHash,
		&admin.Email,
		&admin.Scope,
		&admin.Role,
		&admin.TenantID,
		&admin.DisplayName,
		&admin.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	admin.CreatedAt = mustParseTime(createdAt)
	admin.UpdatedAt = mustParseTime(updatedAt)
	return &admin, nil
}

func (r *adminRepo) GetByUsernameScoped(ctx context.Context, username, scope, tenantID string) (*store.AdminUser, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, username, password_hash, email, scope, role, tenant_id, display_name, status, created_at, updated_at
		 FROM admin_users WHERE username = ? AND scope = ? AND tenant_id = ?`,
		strings.TrimSpace(username),
		normalizeAdminScope(scope),
		strings.TrimSpace(tenantID),
	)

	var (
		admin                store.AdminUser
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&admin.ID,
		&admin.Username,
		&admin.PasswordHash,
		&admin.Email,
		&admin.Scope,
		&admin.Role,
		&admin.TenantID,
		&admin.DisplayName,
		&admin.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	admin.CreatedAt = mustParseTime(createdAt)
	admin.UpdatedAt = mustParseTime(updatedAt)
	return &admin, nil
}

func (r *adminRepo) ListByScopeTenant(ctx context.Context, scope, tenantID string) ([]*store.AdminUser, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, username, password_hash, email, scope, role, tenant_id, display_name, status, created_at, updated_at
		 FROM admin_users WHERE scope = ? AND tenant_id = ? ORDER BY created_at DESC`, normalizeAdminScope(scope), strings.TrimSpace(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.AdminUser
	for rows.Next() {
		var admin store.AdminUser
		var createdAt, updatedAt string
		if err := rows.Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &admin.Email, &admin.Scope, &admin.Role, &admin.TenantID, &admin.DisplayName, &admin.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		admin.CreatedAt = mustParseTime(createdAt)
		admin.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, &admin)
	}
	return out, rows.Err()
}

func (r *adminRepo) ListAllTenantAdmins(ctx context.Context) ([]*store.AdminUser, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT a.id, a.username, a.password_hash, a.email, a.scope, a.role, a.tenant_id, a.display_name, a.status, a.created_at, a.updated_at
		 FROM admin_users a JOIN tenants t ON t.id = a.tenant_id
		 WHERE a.scope = 'tenant' AND a.status = 'active' AND t.status = 'active' AND t.deleted_at IS NULL
		 ORDER BY a.tenant_id, a.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.AdminUser
	for rows.Next() {
		var admin store.AdminUser
		var createdAt, updatedAt string
		if err := rows.Scan(&admin.ID, &admin.Username, &admin.PasswordHash, &admin.Email, &admin.Scope, &admin.Role, &admin.TenantID, &admin.DisplayName, &admin.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		admin.CreatedAt = mustParseTime(createdAt)
		admin.UpdatedAt = mustParseTime(updatedAt)
		out = append(out, &admin)
	}
	return out, rows.Err()
}

func (r *adminRepo) Count(ctx context.Context) (int, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users`)
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
	return r.UpdatePasswordScoped(ctx, username, "global", "", passwordHash, updatedAt)
}

func (r *adminRepo) UpdatePasswordScoped(ctx context.Context, username, scope, tenantID, passwordHash string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE admin_users SET password_hash = ?, updated_at = ? WHERE username = ? AND scope = ? AND tenant_id = ?`,
		passwordHash,
		updatedAt.Format(time.RFC3339),
		strings.TrimSpace(username),
		normalizeAdminScope(scope),
		strings.TrimSpace(tenantID),
	)
	return err
}

func (r *adminRepo) UpdateEmail(ctx context.Context, username, email string, updatedAt time.Time) error {
	return r.UpdateEmailScoped(ctx, username, "global", "", email, updatedAt)
}

func (r *adminRepo) UpdateEmailScoped(ctx context.Context, username, scope, tenantID, email string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE admin_users SET email = ?, updated_at = ? WHERE username = ? AND scope = ? AND tenant_id = ?`,
		email,
		updatedAt.Format(time.RFC3339),
		strings.TrimSpace(username),
		normalizeAdminScope(scope),
		strings.TrimSpace(tenantID),
	)
	return err
}

func (r *systemRepo) Set(ctx context.Context, key, valueJSON string) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO system_settings (key, value_json, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key,
		valueJSON,
		time.Now().Format(time.RFC3339),
	)
	return err
}

func (r *systemRepo) Get(ctx context.Context, key string) (string, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = ?`, key)
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
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO admin_audit_logs (id, tenant_id, admin_user_id, action, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		log.ID,
		strings.TrimSpace(log.TenantID),
		log.AdminUserID,
		log.Action,
		log.PayloadJSON,
		log.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *adminAuditRepo) List(ctx context.Context, filter store.AdminAuditLogFilter) ([]*store.AdminAuditLog, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where := []string{"1=1"}
	args := []any{}
	if filter.TenantScoped {
		where = append(where, "tenant_id = ?")
		args = append(args, normalizeTenantID(filter.TenantID))
	}
	if action := strings.TrimSpace(filter.Action); action != "" {
		where = append(where, "action = ?")
		args = append(args, action)
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		like := "%" + query + "%"
		where = append(where, "(admin_user_id LIKE ? OR action LIKE ? OR payload_json LIKE ?)")
		args = append(args, like, like, like)
	}
	if !filter.CreatedFrom.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, filter.CreatedFrom.UTC().Format(time.RFC3339))
	}
	if !filter.CreatedTo.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, filter.CreatedTo.UTC().Format(time.RFC3339))
	}
	args = append(args, limit)
	rows, err := r.readDB.QueryContext(ctx, fmt.Sprintf(`SELECT id, tenant_id, admin_user_id, action, payload_json, created_at FROM admin_audit_logs WHERE %s ORDER BY created_at DESC LIMIT ?`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*store.AdminAuditLog{}
	for rows.Next() {
		var item store.AdminAuditLog
		var createdAt string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.AdminUserID, &item.Action, &item.PayloadJSON, &createdAt); err != nil {
			return nil, err
		}
		if ts, err := time.Parse(time.RFC3339, createdAt); err == nil {
			item.CreatedAt = ts
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *userRepo) Create(ctx context.Context, user *store.User) error {
	tenantID := normalizeTenantID(user.TenantID)
	account := normalizeEmailLikeAccount(user.Email)
	identityType, identityValue := normalizeUserIdentityFromAccount(account)
	if primaryUserID, err := r.primaryAccountOwnerUserID(ctx, tenantID, account); err != nil {
		return err
	} else if primaryUserID != "" && primaryUserID != user.ID {
		return fmt.Errorf("user account %s already belongs to another user", account)
	}
	if identityType != "" && identityValue != "" {
		existingUserID, err := r.identityOwnerUserID(ctx, tenantID, identityType, identityValue)
		if err != nil {
			return err
		}
		if existingUserID != "" && existingUserID != user.ID {
			return fmt.Errorf("user identity %s:%s already belongs to another user", identityType, identityValue)
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		tenantID,
		account,
		user.SN,
		user.Status,
		user.EnrollmentStatus,
		user.CreatedAt.Format(time.RFC3339),
		user.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("user account %s already belongs to another user", account)
		}
		return err
	}
	if identityType == "" || identityValue == "" {
		if err := tx.Commit(); err != nil {
			return err
		}
		committed = true
		user.TenantID = tenantID
		user.Email = account
		return nil
	}
	now := user.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := r.upsertIdentityWithExec(ctx, tx, &store.UserIdentity{
		ID:        user.ID + "_" + identityType,
		TenantID:  tenantID,
		UserID:    user.ID,
		Type:      identityType,
		Value:     identityValue,
		Verified:  user.EmailVerified || identityType == "phone",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	user.TenantID = tenantID
	user.Email = account
	return nil
}

// CreateReferralUserWithAttribution persists the new account, its verified
// identities, and the immutable referral attribution in one SQLite
// transaction. Referral reward grants deliberately remain outside this
// transaction: they live in the service registry and are recovered by the
// attributed -> rewarded/reward_failed state machine.
func (r *userRepo) CreateReferralUserWithAttribution(ctx context.Context, user *store.User, extraIdentity *store.UserIdentity, referral *store.UserReferral) error {
	if user == nil || referral == nil || strings.TrimSpace(user.ID) == "" || strings.TrimSpace(referral.ID) == "" {
		return errors.New("referral user and attribution are required")
	}
	tenantID := normalizeTenantID(user.TenantID)
	account := normalizeEmailLikeAccount(user.Email)
	identityType, identityValue := normalizeUserIdentityFromAccount(account)
	if account == "" || identityType == "" || identityValue == "" {
		return errors.New("referral user account is required")
	}
	if normalizeTenantID(referral.TenantID) != tenantID {
		return errors.New("referral tenant does not match user tenant")
	}
	referral.InviteeUserID = strings.TrimSpace(user.ID)
	referral.TenantID = tenantID
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	emailVerifiedAt := ""
	if user.EmailVerified {
		emailVerifiedAt = user.CreatedAt.UTC().Format(time.RFC3339)
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, email_verified, email_verified_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, tenantID, account, user.SN, user.Status, user.EnrollmentStatus, boolToInt(user.EmailVerified), emailVerifiedAt,
		user.CreatedAt.Format(time.RFC3339), user.UpdatedAt.Format(time.RFC3339)); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("user account %s already belongs to another user", account)
		}
		return err
	}
	now := user.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err = r.upsertIdentityWithExec(ctx, tx, &store.UserIdentity{
		ID:        user.ID + "_" + identityType,
		TenantID:  tenantID,
		UserID:    user.ID,
		Type:      identityType,
		Value:     identityValue,
		Verified:  user.EmailVerified || identityType == "phone",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return err
	}
	if extraIdentity != nil {
		extra := *extraIdentity
		extra.TenantID = tenantID
		extra.UserID = user.ID
		if err = r.upsertIdentityWithExec(ctx, tx, &extra); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO user_referrals (id, tenant_id, referral_code_id, inviter_user_id, invitee_user_id, status, registered_at, service_group_id, inviter_credits, invitee_credits, duration_days, inviter_grant_id, invitee_grant_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		referral.ID, tenantID, referral.ReferralCodeID, referral.InviterUserID, referral.InviteeUserID, referral.Status,
		referral.RegisteredAt.UTC().Format(time.RFC3339), referral.ServiceGroupID, referral.InviterCredits, referral.InviteeCredits,
		referral.DurationDays, referral.InviterGrantID, referral.InviteeGrantID, referral.CreatedAt.UTC().Format(time.RFC3339), referral.UpdatedAt.UTC().Format(time.RFC3339)); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return errors.New("referral attribution already exists")
		}
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	user.TenantID = tenantID
	user.Email = account
	return nil
}

func (r *userRepo) GetByID(ctx context.Context, id string) (*store.User, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at
		 FROM users WHERE id = ?`,
		id,
	)

	var (
		user                 store.User
		smartRoute           int
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&user.ID,
		&user.TenantID,
		&user.Email,
		&user.SN,
		&user.Status,
		&user.EnrollmentStatus,
		&smartRoute,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	user.SmartRoute = smartRoute != 0
	user.CreatedAt = mustParseTime(createdAt)
	user.UpdatedAt = mustParseTime(updatedAt)
	r.fillEmailVerified(ctx, &user)
	return &user, nil
}

func (r *userRepo) fillEmailVerified(ctx context.Context, user *store.User) {
	if user == nil || user.ID == "" {
		return
	}

	var (
		emailVerified   int
		emailVerifiedAt string
	)
	err := r.readDB.QueryRowContext(
		ctx,
		`SELECT email_verified, email_verified_at FROM users WHERE id = ?`,
		user.ID,
	).Scan(&emailVerified, &emailVerifiedAt)
	if err != nil {
		return
	}

	user.EmailVerified = emailVerified != 0
	if emailVerifiedAt != "" {
		t := mustParseTime(emailVerifiedAt)
		user.EmailVerifiedAt = &t
	}
}

func (r *userRepo) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	return r.GetByTenantEmail(ctx, store.DefaultTenantID, email)
}

func (r *userRepo) GetByTenantEmail(ctx context.Context, tenantID, email string) (*store.User, error) {
	return r.getByEmail(ctx, normalizeTenantID(tenantID), normalizeEmailLikeAccount(email))
}

func (r *userRepo) GetByTenantIdentity(ctx context.Context, tenantID, identityType, value string) (*store.User, error) {
	identityType, value = normalizeUserIdentity(identityType, value)
	if identityType == "" || value == "" {
		return nil, nil
	}
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT u.id, u.tenant_id, u.email, u.sn, u.status, u.enrollment_status, u.smart_route, u.email_verified, u.email_verified_at, u.created_at, u.updated_at
		 FROM user_identities ui
		 JOIN users u ON u.id = ui.user_id AND u.tenant_id = ui.tenant_id
		 WHERE ui.tenant_id = ? AND ui.type = ? AND lower(ui.value) = lower(?)`,
		normalizeTenantID(tenantID), identityType, value,
	)
	user, err := scanUser(row)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return user, err
	}
	if identityType == "email" {
		return r.GetByTenantEmail(ctx, tenantID, value)
	}
	if identityType == "phone" {
		return r.GetByTenantEmail(ctx, tenantID, "phone:"+value)
	}
	return nil, nil
}

func (r *userRepo) ListIdentitiesByUser(ctx context.Context, tenantID, userID string) ([]*store.UserIdentity, error) {
	rows, err := r.readDB.QueryContext(
		ctx,
		`SELECT id, tenant_id, user_id, type, value, verified, verified_at, created_at, updated_at
		 FROM user_identities WHERE tenant_id = ? AND user_id = ? ORDER BY type, value`,
		normalizeTenantID(tenantID), strings.TrimSpace(userID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*store.UserIdentity
	for rows.Next() {
		item, err := scanUserIdentity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *userRepo) ListIdentitiesByUsers(ctx context.Context, tenantID string, userIDs []string) (map[string][]*store.UserIdentity, error) {
	out := map[string][]*store.UserIdentity{}
	cleanIDs := make([]string, 0, len(userIDs))
	seen := map[string]struct{}{}
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		cleanIDs = append(cleanIDs, userID)
		out[userID] = nil
	}
	if len(cleanIDs) == 0 {
		return out, nil
	}
	tenantID = normalizeTenantID(tenantID)
	const batchSize = 500
	for start := 0; start < len(cleanIDs); start += batchSize {
		end := start + batchSize
		if end > len(cleanIDs) {
			end = len(cleanIDs)
		}
		batch := cleanIDs[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, 1+len(batch))
		args = append(args, tenantID)
		for _, userID := range batch {
			args = append(args, userID)
		}
		rows, err := r.readDB.QueryContext(
			ctx,
			`SELECT id, tenant_id, user_id, type, value, verified, verified_at, created_at, updated_at
			 FROM user_identities WHERE tenant_id = ? AND user_id IN (`+placeholders+`) ORDER BY user_id, type, value`,
			args...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			item, err := scanUserIdentity(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[item.UserID] = append(out[item.UserID], item)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *userRepo) UpsertIdentity(ctx context.Context, identity *store.UserIdentity) error {
	return r.upsertIdentityWithExec(ctx, r.db, identity)
}

func (r *userRepo) primaryAccountOwnerUserID(ctx context.Context, tenantID, account string) (string, error) {
	account = normalizeEmailLikeAccount(account)
	if tenantID == "" || account == "" {
		return "", nil
	}
	var userID string
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE tenant_id = ? AND lower(email) = lower(?)`,
		normalizeTenantID(tenantID), account,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(userID), nil
}

func (r *userRepo) identityOwnerUserID(ctx context.Context, tenantID, identityType, value string) (string, error) {
	identityType, value = normalizeUserIdentity(identityType, value)
	if tenantID == "" || identityType == "" || value == "" {
		return "", nil
	}
	var userID string
	err := r.db.QueryRowContext(ctx,
		`SELECT ui.user_id
		 FROM user_identities ui
		 JOIN users u ON u.tenant_id = ui.tenant_id AND u.id = ui.user_id
		 WHERE ui.tenant_id = ? AND ui.type = ? AND lower(ui.value) = lower(?)`,
		normalizeTenantID(tenantID), identityType, value,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(userID), nil
}

func (r *userRepo) upsertIdentityWithExec(ctx context.Context, execer sqlExecer, identity *store.UserIdentity) error {
	if identity == nil {
		return nil
	}
	identityType, value := normalizeUserIdentity(identity.Type, identity.Value)
	if identityType == "" || value == "" || strings.TrimSpace(identity.UserID) == "" {
		return nil
	}
	now := identity.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := identity.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	verifiedAt := ""
	if identity.VerifiedAt != nil {
		verifiedAt = identity.VerifiedAt.UTC().Format(time.RFC3339)
	} else if identity.Verified {
		t := now.UTC()
		verifiedAt = t.Format(time.RFC3339)
	}
	verified := 0
	if identity.Verified {
		verified = 1
	}
	id := strings.TrimSpace(identity.ID)
	if id == "" {
		id = strings.TrimSpace(identity.UserID) + "_" + identityType
	}
	result, err := execer.ExecContext(ctx,
		`INSERT INTO user_identities (id, tenant_id, user_id, type, value, verified, verified_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id, type, value) DO UPDATE SET
		   id = excluded.id,
		   user_id = excluded.user_id,
		   verified = excluded.verified,
		   verified_at = excluded.verified_at,
		   updated_at = excluded.updated_at
		 WHERE user_identities.user_id = excluded.user_id
		    OR NOT EXISTS (
		      SELECT 1 FROM users u
		      WHERE u.tenant_id = user_identities.tenant_id
		        AND u.id = user_identities.user_id
		    )`,
		id,
		normalizeTenantID(identity.TenantID),
		strings.TrimSpace(identity.UserID),
		identityType,
		value,
		verified,
		verifiedAt,
		createdAt.UTC().Format(time.RFC3339),
		now.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return fmt.Errorf("user identity %s:%s already belongs to another user", identityType, value)
	}
	return nil
}

func (r *userRepo) ReassignIdentity(ctx context.Context, tenantID, identityType, value, userID string, verified bool, verifiedAt time.Time) error {
	identityType, value = normalizeUserIdentity(identityType, value)
	tenantID = normalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || identityType == "" || value == "" || userID == "" {
		return nil
	}
	now := time.Now().UTC()
	verifiedValue := 0
	if verified {
		verifiedValue = 1
	}
	verifiedAtValue := ""
	if !verifiedAt.IsZero() {
		verifiedAtValue = verifiedAt.UTC().Format(time.RFC3339)
	} else if verified {
		verifiedAtValue = now.Format(time.RFC3339)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_identities
		 WHERE tenant_id = ? AND user_id = ? AND type = ? AND lower(value) <> lower(?)`,
		tenantID,
		userID,
		identityType,
		value,
	); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE user_identities
		 SET id = ?, user_id = ?, verified = ?, verified_at = ?, updated_at = ?
		 WHERE tenant_id = ? AND type = ? AND lower(value) = lower(?)`,
		userID+"_"+identityType,
		userID,
		verifiedValue,
		verifiedAtValue,
		now.Format(time.RFC3339),
		tenantID,
		identityType,
		value,
	)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return fmt.Errorf("user identity %s:%s not found", identityType, value)
	}
	return tx.Commit()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*store.User, error) {
	var (
		user                      store.User
		smartRoute, emailVerified int
		emailVerifiedAt           string
		createdAt, updatedAt      string
	)
	if err := row.Scan(
		&user.ID,
		&user.TenantID,
		&user.Email,
		&user.SN,
		&user.Status,
		&user.EnrollmentStatus,
		&smartRoute,
		&emailVerified,
		&emailVerifiedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	user.SmartRoute = smartRoute != 0
	user.EmailVerified = emailVerified != 0
	if emailVerifiedAt != "" {
		t := mustParseTime(emailVerifiedAt)
		user.EmailVerifiedAt = &t
	}
	user.CreatedAt = mustParseTime(createdAt)
	user.UpdatedAt = mustParseTime(updatedAt)
	return &user, nil
}

func scanUserIdentity(row rowScanner) (*store.UserIdentity, error) {
	var (
		item                 store.UserIdentity
		verified             int
		verifiedAt           string
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.UserID,
		&item.Type,
		&item.Value,
		&verified,
		&verifiedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	item.Verified = verified != 0
	if verifiedAt != "" {
		t := mustParseTime(verifiedAt)
		item.VerifiedAt = &t
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func normalizeUserIdentityFromAccount(account string) (string, string) {
	account = normalizeEmailLikeAccount(account)
	if strings.HasPrefix(account, "phone:") {
		return normalizeUserIdentity("phone", strings.TrimPrefix(account, "phone:"))
	}
	return normalizeUserIdentity("email", account)
}

func normalizeEmailLikeAccount(account string) string {
	return strings.ToLower(norm.NFC.String(strings.TrimSpace(account)))
}

func normalizeUserIdentity(identityType, value string) (string, string) {
	identityType = strings.ToLower(strings.TrimSpace(identityType))
	value = strings.ToLower(norm.NFC.String(strings.TrimSpace(value)))
	if strings.HasPrefix(value, "phone:") {
		identityType = "phone"
		value = strings.TrimPrefix(value, "phone:")
	}
	switch identityType {
	case "email":
		return "email", value
	case "phone":
		var b strings.Builder
		for _, r := range value {
			if r >= '0' && r <= '9' {
				b.WriteRune(r)
			}
		}
		if b.Len() == 0 {
			return "", ""
		}
		return "phone", b.String()
	default:
		return "", ""
	}
}

func (r *userRepo) getByEmail(ctx context.Context, tenantID, email string) (*store.User, error) {
	email = normalizeEmailLikeAccount(email)
	where := `lower(email) = lower(?)`
	args := []any{email}
	if tenantID != "" {
		where = `tenant_id = ? AND lower(email) = lower(?)`
		args = []any{tenantID, email}
	}
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, email, sn, status, enrollment_status, smart_route, email_verified, email_verified_at, created_at, updated_at
		 FROM users WHERE `+where,
		args...,
	)

	var (
		user                      store.User
		smartRoute, emailVerified int
		emailVerifiedAt           string
		createdAt, updatedAt      string
	)
	if err := row.Scan(
		&user.ID,
		&user.TenantID,
		&user.Email,
		&user.SN,
		&user.Status,
		&user.EnrollmentStatus,
		&smartRoute,
		&emailVerified,
		&emailVerifiedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	user.SmartRoute = smartRoute != 0
	user.EmailVerified = emailVerified != 0
	if emailVerifiedAt != "" {
		t := mustParseTime(emailVerifiedAt)
		user.EmailVerifiedAt = &t
	}
	user.CreatedAt = mustParseTime(createdAt)
	user.UpdatedAt = mustParseTime(updatedAt)
	return &user, nil
}

func (r *userRepo) List(ctx context.Context) ([]*store.User, error) {
	return r.list(ctx, "")
}

func (r *userRepo) ListByTenant(ctx context.Context, tenantID string) ([]*store.User, error) {
	return r.list(ctx, normalizeTenantID(tenantID))
}

func (r *userRepo) list(ctx context.Context, tenantID string) ([]*store.User, error) {
	where := ""
	args := []any{}
	if tenantID != "" {
		where = "WHERE tenant_id = ?"
		args = append(args, tenantID)
	}
	// Use write DB (r.db) to guarantee read-after-write consistency.
	// The admin panel lists users immediately after delete; the read pool
	// may return stale WAL snapshot data. Write connection always sees its
	// own committed deletes. This is safe since admin list queries are infrequent.
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, tenant_id, email, sn, status, enrollment_status, smart_route, email_verified, email_verified_at, created_at, updated_at
		 FROM users `+where+`
		 ORDER BY updated_at DESC, email ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.User
	for rows.Next() {
		var (
			user                      store.User
			smartRoute, emailVerified int
			emailVerifiedAt           string
			createdAt, updatedAt      string
		)
		if err := rows.Scan(
			&user.ID,
			&user.TenantID,
			&user.Email,
			&user.SN,
			&user.Status,
			&user.EnrollmentStatus,
			&smartRoute,
			&emailVerified,
			&emailVerifiedAt,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		user.SmartRoute = smartRoute != 0
		user.EmailVerified = emailVerified != 0
		if emailVerifiedAt != "" {
			t := mustParseTime(emailVerifiedAt)
			user.EmailVerifiedAt = &t
		}
		user.CreatedAt = mustParseTime(createdAt)
		user.UpdatedAt = mustParseTime(updatedAt)
		items = append(items, &user)
	}
	return items, rows.Err()
}

func (r *userRepo) DeleteByEmail(ctx context.Context, email string) error {
	return r.DeleteByTenantEmail(ctx, store.DefaultTenantID, email)
}

func (r *userRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE tenant_id = ? AND lower(email) = lower(?)`, normalizeTenantID(tenantID), normalizeEmailLikeAccount(email))
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *userRepo) UpdateSmartRoute(ctx context.Context, userID string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := r.db.ExecContext(ctx, `UPDATE users SET smart_route = ? WHERE id = ?`, v, userID)
	return err
}

func (r *userRepo) MarkEmailVerified(ctx context.Context, tenantID, email string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET email_verified = 1, email_verified_at = ? WHERE tenant_id = ? AND lower(email) = lower(?)`,
		now, normalizeTenantID(tenantID), normalizeEmailLikeAccount(email))
	return err
}

func (r *enrollmentRepo) Create(ctx context.Context, item *store.UserEnrollment) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO user_enrollments (id, tenant_id, email, mobile, status, note, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		normalizeTenantID(item.TenantID),
		item.Email,
		item.Mobile,
		item.Status,
		item.Note,
		item.CreatedAt.Format(time.RFC3339),
		item.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *enrollmentRepo) GetPendingByEmail(ctx context.Context, email string) (*store.UserEnrollment, error) {
	return r.getPendingByEmailQuery(ctx, `email = ?`, email)
}

func (r *enrollmentRepo) GetPendingByTenantEmail(ctx context.Context, tenantID string, email string) (*store.UserEnrollment, error) {
	return r.getPendingByEmailQuery(ctx, `tenant_id = ? AND email = ?`, normalizeTenantID(tenantID), email)
}

func (r *enrollmentRepo) getPendingByEmailQuery(ctx context.Context, where string, args ...any) (*store.UserEnrollment, error) {
	queryArgs := append(args, "pending")
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, email, mobile, status, note, created_at, updated_at FROM user_enrollments WHERE `+where+` AND status = ? ORDER BY created_at DESC LIMIT 1`,
		queryArgs...,
	)
	var item store.UserEnrollment
	var createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.TenantID, &item.Email, &item.Mobile, &item.Status, &item.Note, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *enrollmentRepo) ListPending(ctx context.Context) ([]*store.UserEnrollment, error) {
	return r.listEnrollments(ctx, `status = ?`, "pending")

}

func (r *enrollmentRepo) ListPendingByTenant(ctx context.Context, tenantID string) ([]*store.UserEnrollment, error) {
	return r.listEnrollments(ctx, `tenant_id = ? AND status = ?`, normalizeTenantID(tenantID), "pending")
}

func (r *enrollmentRepo) ListAll(ctx context.Context) ([]*store.UserEnrollment, error) {
	return r.listEnrollments(ctx, `1 = 1`)
}

func (r *enrollmentRepo) ListAllByTenant(ctx context.Context, tenantID string) ([]*store.UserEnrollment, error) {
	return r.listEnrollments(ctx, `tenant_id = ?`, normalizeTenantID(tenantID))
}

func (r *enrollmentRepo) listEnrollments(ctx context.Context, where string, args ...any) ([]*store.UserEnrollment, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, tenant_id, email, mobile, status, note, created_at, updated_at FROM user_enrollments WHERE `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.UserEnrollment
	for rows.Next() {
		var item store.UserEnrollment
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Email, &item.Mobile, &item.Status, &item.Note, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *enrollmentRepo) Approve(ctx context.Context, id string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_enrollments SET status = 'approved', updated_at = ? WHERE id = ?`, updatedAt.Format(time.RFC3339), id)
	return err
}

func (r *enrollmentRepo) Reject(ctx context.Context, id string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_enrollments SET status = 'rejected', updated_at = ? WHERE id = ?`, updatedAt.Format(time.RFC3339), id)
	return err
}

func (r *enrollmentRepo) UpdateMobile(ctx context.Context, id string, mobile string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_enrollments SET mobile = ? WHERE id = ?`, mobile, id)
	return err
}

func (r *enrollmentRepo) GetByMobile(ctx context.Context, mobile string) (*store.UserEnrollment, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, email, mobile, status, note, created_at, updated_at
		 FROM user_enrollments WHERE mobile = ?
		 ORDER BY created_at DESC LIMIT 1`,
		mobile,
	)
	var item store.UserEnrollment
	var createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.TenantID, &item.Email, &item.Mobile, &item.Status, &item.Note, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *enrollmentRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM user_enrollments WHERE tenant_id = ? AND lower(email) = lower(?)`, normalizeTenantID(tenantID), strings.TrimSpace(email))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *emailBlockRepo) Create(ctx context.Context, item *store.EmailBlockItem) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO email_blocklist (id, tenant_id, email, reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id, email) DO UPDATE SET reason = excluded.reason, updated_at = excluded.updated_at`,
		item.ID,
		normalizeTenantID(item.TenantID),
		item.Email,
		item.Reason,
		item.CreatedAt.Format(time.RFC3339),
		item.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *emailBlockRepo) DeleteByEmail(ctx context.Context, email string) error {
	return r.DeleteByTenantEmail(ctx, store.DefaultTenantID, email)
}

func (r *emailBlockRepo) DeleteByTenantEmail(ctx context.Context, tenantID string, email string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM email_blocklist WHERE tenant_id = ? AND email = ?`, normalizeTenantID(tenantID), email)
	return err
}

func (r *emailBlockRepo) GetByEmail(ctx context.Context, email string) (*store.EmailBlockItem, error) {
	return r.GetByTenantEmail(ctx, store.DefaultTenantID, email)
}

func (r *emailBlockRepo) GetByTenantEmail(ctx context.Context, tenantID string, email string) (*store.EmailBlockItem, error) {
	return r.getEmailBlock(ctx, `tenant_id = ? AND email = ?`, normalizeTenantID(tenantID), email)
}

func (r *userReferralRepo) GetActiveCodeForInviter(ctx context.Context, tenantID, inviterUserID string) (*store.UserReferralCode, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, tenant_id, inviter_user_id, code_hash, encrypted_code, status, created_at, rotated_at
		FROM user_referral_codes WHERE tenant_id = ? AND inviter_user_id = ? AND status = 'active'
		ORDER BY created_at DESC, id DESC LIMIT 1`, normalizeTenantID(tenantID), strings.TrimSpace(inviterUserID))
	return scanUserReferralCode(row)
}

func (r *userReferralRepo) GetCodeByHash(ctx context.Context, tenantID, codeHash string) (*store.UserReferralCode, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT id, tenant_id, inviter_user_id, code_hash, encrypted_code, status, created_at, rotated_at
		FROM user_referral_codes WHERE tenant_id = ? AND code_hash = ? AND status = 'active' LIMIT 1`, normalizeTenantID(tenantID), strings.TrimSpace(codeHash))
	return scanUserReferralCode(row)
}

func scanUserReferralCode(row rowScanner) (*store.UserReferralCode, error) {
	var item store.UserReferralCode
	var createdAt string
	var rotatedAt sql.NullString
	if err := row.Scan(&item.ID, &item.TenantID, &item.InviterUserID, &item.CodeHash, &item.EncryptedCode, &item.Status, &createdAt, &rotatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	if rotatedAt.Valid && rotatedAt.String != "" {
		t := mustParseTime(rotatedAt.String)
		item.RotatedAt = &t
	}
	return &item, nil
}

func (r *userReferralRepo) CreateCode(ctx context.Context, item *store.UserReferralCode) error {
	if item == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_referral_codes (id, tenant_id, inviter_user_id, code_hash, encrypted_code, status, created_at, rotated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, normalizeTenantID(item.TenantID), strings.TrimSpace(item.InviterUserID), item.CodeHash, item.EncryptedCode, item.Status, item.CreatedAt.UTC().Format(time.RFC3339), nullableTimeString(item.RotatedAt))
	return err
}

func (r *userReferralRepo) RotateCode(ctx context.Context, tenantID, codeID string, rotatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_referral_codes SET status = 'rotated', rotated_at = ? WHERE tenant_id = ? AND id = ? AND status = 'active'`, rotatedAt.UTC().Format(time.RFC3339), normalizeTenantID(tenantID), strings.TrimSpace(codeID))
	return err
}

func (r *userReferralRepo) ReplaceActiveCode(ctx context.Context, tenantID, inviterUserID string, item *store.UserReferralCode, rotatedAt time.Time) error {
	if item == nil || strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.CodeHash) == "" || strings.TrimSpace(item.EncryptedCode) == "" {
		return errors.New("replacement referral code is required")
	}
	tenantID = normalizeTenantID(tenantID)
	inviterUserID = strings.TrimSpace(inviterUserID)
	if inviterUserID == "" || normalizeTenantID(item.TenantID) != tenantID || strings.TrimSpace(item.InviterUserID) != inviterUserID {
		return errors.New("replacement referral code scope is invalid")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE user_referral_codes SET status = 'rotated', rotated_at = ?
		WHERE tenant_id = ? AND inviter_user_id = ? AND status = 'active'`, rotatedAt.UTC().Format(time.RFC3339), tenantID, inviterUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_referral_codes (id, tenant_id, inviter_user_id, code_hash, encrypted_code, status, created_at, rotated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, tenantID, inviterUserID, item.CodeHash, item.EncryptedCode, "active", item.CreatedAt.UTC().Format(time.RFC3339), nullableTimeString(item.RotatedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *userReferralRepo) CreateReferral(ctx context.Context, item *store.UserReferral) error {
	if item == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_referrals (id, tenant_id, referral_code_id, inviter_user_id, invitee_user_id, status, registered_at, service_group_id, inviter_credits, invitee_credits, duration_days, inviter_grant_id, invitee_grant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, normalizeTenantID(item.TenantID), item.ReferralCodeID, item.InviterUserID, item.InviteeUserID, item.Status, item.RegisteredAt.UTC().Format(time.RFC3339), item.ServiceGroupID, item.InviterCredits, item.InviteeCredits, item.DurationDays, item.InviterGrantID, item.InviteeGrantID, item.CreatedAt.UTC().Format(time.RFC3339), item.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}

func (r *userReferralRepo) GetReferralForInvitee(ctx context.Context, tenantID, inviteeUserID string) (*store.UserReferral, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT id, tenant_id, referral_code_id, inviter_user_id, invitee_user_id, status, registered_at, service_group_id, inviter_credits, invitee_credits, duration_days, inviter_grant_id, invitee_grant_id, created_at, updated_at
		FROM user_referrals WHERE tenant_id = ? AND invitee_user_id = ? LIMIT 1`, normalizeTenantID(tenantID), strings.TrimSpace(inviteeUserID))
	return scanUserReferral(row)
}

func (r *userReferralRepo) GetReferralByID(ctx context.Context, tenantID, referralID string) (*store.UserReferral, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT id, tenant_id, referral_code_id, inviter_user_id, invitee_user_id, status, registered_at, service_group_id, inviter_credits, invitee_credits, duration_days, inviter_grant_id, invitee_grant_id, created_at, updated_at
		FROM user_referrals WHERE tenant_id = ? AND id = ? LIMIT 1`, normalizeTenantID(tenantID), strings.TrimSpace(referralID))
	return scanUserReferral(row)
}

func (r *userReferralRepo) ListReferralsForInvitees(ctx context.Context, tenantID string, inviteeUserIDs []string) (map[string]*store.UserReferral, error) {
	result := make(map[string]*store.UserReferral)
	ids := make([]string, 0, len(inviteeUserIDs))
	seen := make(map[string]struct{}, len(inviteeUserIDs))
	for _, userID := range inviteeUserIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		ids = append(ids, userID)
	}
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, normalizeTenantID(tenantID))
	for _, userID := range ids {
		args = append(args, userID)
	}
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, tenant_id, referral_code_id, inviter_user_id, invitee_user_id, status, registered_at, service_group_id, inviter_credits, invitee_credits, duration_days, inviter_grant_id, invitee_grant_id, created_at, updated_at
		FROM user_referrals WHERE tenant_id = ? AND invitee_user_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanUserReferral(rows)
		if err != nil {
			return nil, err
		}
		result[item.InviteeUserID] = item
	}
	return result, rows.Err()
}

func scanUserReferral(row rowScanner) (*store.UserReferral, error) {
	var item store.UserReferral
	var registeredAt, createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.TenantID, &item.ReferralCodeID, &item.InviterUserID, &item.InviteeUserID, &item.Status, &registeredAt, &item.ServiceGroupID, &item.InviterCredits, &item.InviteeCredits, &item.DurationDays, &item.InviterGrantID, &item.InviteeGrantID, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.RegisteredAt, item.CreatedAt, item.UpdatedAt = mustParseTime(registeredAt), mustParseTime(createdAt), mustParseTime(updatedAt)
	return &item, nil
}

func (r *userReferralRepo) UpdateRewardGrants(ctx context.Context, tenantID, referralID, status, inviterGrantID, inviteeGrantID string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_referrals SET status = ?, inviter_grant_id = ?, invitee_grant_id = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`, status, inviterGrantID, inviteeGrantID, updatedAt.UTC().Format(time.RFC3339), normalizeTenantID(tenantID), strings.TrimSpace(referralID))
	return err
}

func (r *userReferralRepo) TransitionReferralStatus(ctx context.Context, tenantID, referralID string, fromStatuses []string, toStatus string, updatedAt time.Time) (bool, error) {
	if len(fromStatuses) == 0 || strings.TrimSpace(toStatus) == "" {
		return false, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(fromStatuses)), ",")
	args := make([]any, 0, len(fromStatuses)+4)
	args = append(args, strings.TrimSpace(toStatus), updatedAt.UTC().Format(time.RFC3339), normalizeTenantID(tenantID), strings.TrimSpace(referralID))
	for _, status := range fromStatuses {
		args = append(args, strings.TrimSpace(status))
	}
	result, err := r.db.ExecContext(ctx, `UPDATE user_referrals SET status = ?, updated_at = ? WHERE tenant_id = ? AND id = ? AND status IN (`+placeholders+`)`, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (r *userReferralRepo) GetRegistrationIdempotency(ctx context.Context, tenantID, keyHash string, now time.Time) (*store.UserReferralRegistrationIdempotency, error) {
	tenantID = normalizeTenantID(tenantID)
	keyHash = strings.TrimSpace(keyHash)
	if keyHash == "" {
		return nil, nil
	}
	row := r.readDB.QueryRowContext(ctx, `SELECT tenant_id, key_hash, fingerprint, status, payload, expires_at, created_at
		FROM user_referral_registration_idempotency WHERE tenant_id = ? AND key_hash = ? AND expires_at > ? LIMIT 1`, tenantID, keyHash, now.UTC().Format(time.RFC3339))
	var item store.UserReferralRegistrationIdempotency
	var expiresAt, createdAt string
	if err := row.Scan(&item.TenantID, &item.KeyHash, &item.Fingerprint, &item.Status, &item.Payload, &expiresAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.ExpiresAt, item.CreatedAt = mustParseTime(expiresAt), mustParseTime(createdAt)
	return &item, nil
}

func (r *userReferralRepo) SaveRegistrationIdempotency(ctx context.Context, item *store.UserReferralRegistrationIdempotency) error {
	if item == nil || strings.TrimSpace(item.KeyHash) == "" || strings.TrimSpace(item.Fingerprint) == "" || len(item.Payload) == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_referral_registration_idempotency (tenant_id, key_hash, fingerprint, status, payload, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, key_hash) DO UPDATE SET fingerprint = excluded.fingerprint, status = excluded.status, payload = excluded.payload, expires_at = excluded.expires_at, created_at = excluded.created_at`,
		normalizeTenantID(item.TenantID), strings.TrimSpace(item.KeyHash), strings.TrimSpace(item.Fingerprint), item.Status, item.Payload, item.ExpiresAt.UTC().Format(time.RFC3339), item.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (r *userReferralRepo) GetRegistrationSession(ctx context.Context, tenantID, tokenHash string, now time.Time) (*store.UserReferralRegistrationSession, error) {
	tenantID = normalizeTenantID(tenantID)
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return nil, nil
	}
	row := r.readDB.QueryRowContext(ctx, `SELECT tenant_id, token_hash, code_hash, config_epoch, user_agent_hash, invitee_user_id, referral_id, completed_at, expires_at, created_at
		FROM user_referral_registration_sessions WHERE tenant_id = ? AND token_hash = ? AND expires_at > ? LIMIT 1`, tenantID, tokenHash, now.UTC().Format(time.RFC3339))
	var item store.UserReferralRegistrationSession
	var completedAt sql.NullString
	var expiresAt, createdAt string
	if err := row.Scan(&item.TenantID, &item.TokenHash, &item.CodeHash, &item.ConfigEpoch, &item.UserAgentHash, &item.InviteeUserID, &item.ReferralID, &completedAt, &expiresAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.ExpiresAt, item.CreatedAt = mustParseTime(expiresAt), mustParseTime(createdAt)
	if completedAt.Valid && strings.TrimSpace(completedAt.String) != "" {
		value := mustParseTime(completedAt.String)
		item.CompletedAt = &value
	}
	return &item, nil
}

func (r *userReferralRepo) SaveRegistrationSession(ctx context.Context, item *store.UserReferralRegistrationSession) error {
	if item == nil || strings.TrimSpace(item.TokenHash) == "" || strings.TrimSpace(item.CodeHash) == "" || strings.TrimSpace(item.UserAgentHash) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_referral_registration_sessions (tenant_id, token_hash, code_hash, config_epoch, user_agent_hash, invitee_user_id, referral_id, completed_at, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, token_hash) DO UPDATE SET code_hash = excluded.code_hash, config_epoch = excluded.config_epoch, user_agent_hash = excluded.user_agent_hash, expires_at = excluded.expires_at, created_at = excluded.created_at`,
		normalizeTenantID(item.TenantID), strings.TrimSpace(item.TokenHash), strings.TrimSpace(item.CodeHash), strings.TrimSpace(item.ConfigEpoch), strings.TrimSpace(item.UserAgentHash), strings.TrimSpace(item.InviteeUserID), strings.TrimSpace(item.ReferralID), nullableTimeString(item.CompletedAt), item.ExpiresAt.UTC().Format(time.RFC3339), item.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (r *userReferralRepo) MarkRegistrationSessionCompleted(ctx context.Context, tenantID, tokenHash, inviteeUserID, referralID string, completedAt time.Time) error {
	if strings.TrimSpace(tokenHash) == "" || strings.TrimSpace(inviteeUserID) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `UPDATE user_referral_registration_sessions
		SET invitee_user_id = ?, referral_id = ?, completed_at = ?
		WHERE tenant_id = ? AND token_hash = ?`, strings.TrimSpace(inviteeUserID), strings.TrimSpace(referralID), completedAt.UTC().Format(time.RFC3339), normalizeTenantID(tenantID), strings.TrimSpace(tokenHash))
	return err
}

func (r *userReferralRepo) ExpireReservedReferrals(ctx context.Context, tenantID string, before, updatedAt time.Time) ([]string, error) {
	tenantID = normalizeTenantID(tenantID)
	before = before.UTC()
	updatedAt = updatedAt.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM user_referrals
		WHERE tenant_id = ? AND status = 'reserved' AND registered_at < ?
		ORDER BY registered_at ASC, id ASC`, tenantID, before.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	expiredIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		result, err := tx.ExecContext(ctx, `UPDATE user_referrals SET status = 'expired', updated_at = ?
			WHERE tenant_id = ? AND id = ? AND status = 'reserved'`, updatedAt.Format(time.RFC3339), tenantID, id)
		if err != nil {
			return nil, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if changed == 0 {
			continue
		}
		expiredIDs = append(expiredIDs, id)
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_referral_status_history (id, tenant_id, referral_id, from_status, to_status, reason, actor_user_id, created_at)
			VALUES (?, ?, ?, 'reserved', 'expired', 'review window expired', 'system', ?)`, newUserReferralHistoryID(), tenantID, id, updatedAt.Format(time.RFC3339)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return expiredIDs, nil
}

func (r *userReferralRepo) CleanupExpiredRegistrationArtifacts(ctx context.Context, before time.Time) (store.UserReferralRegistrationCleanupResult, error) {
	var cleaned store.UserReferralRegistrationCleanupResult
	beforeText := before.UTC().Format(time.RFC3339)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return cleaned, err
	}
	defer tx.Rollback()
	for _, target := range []struct {
		query string
		count *int
	}{
		{`DELETE FROM user_referral_registration_idempotency WHERE expires_at <= ?`, &cleaned.IdempotencyRecords},
		{`DELETE FROM user_referral_registration_sessions WHERE expires_at <= ?`, &cleaned.Sessions},
		{`DELETE FROM user_referral_identity_reservations WHERE expires_at <= ?`, &cleaned.IdentityReservations},
		{`DELETE FROM user_referral_handoffs WHERE expires_at <= ?`, &cleaned.Handoffs},
	} {
		result, err := tx.ExecContext(ctx, target.query, beforeText)
		if err != nil {
			return cleaned, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return cleaned, err
		}
		*target.count = int(count)
	}
	if err := tx.Commit(); err != nil {
		return cleaned, err
	}
	return cleaned, nil
}

func newUserReferralHistoryID() string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("refhist_expire_%d", time.Now().UTC().UnixNano())
	}
	return "refhist_expire_" + hex.EncodeToString(buf)
}

func (r *userReferralRepo) CreateHandoff(ctx context.Context, item *store.UserReferralHandoff) error {
	if item == nil || strings.TrimSpace(item.TokenHash) == "" || strings.TrimSpace(item.TenantID) == "" || strings.TrimSpace(item.CodeHash) == "" || strings.TrimSpace(item.ReferralCodeID) == "" || strings.TrimSpace(item.InviterUserID) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_referral_handoffs (token_hash, tenant_id, code_hash, referral_code_id, inviter_user_id, config_epoch, service_group_id, inviter_credits, invitee_credits, duration_days, expires_at, used_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
		strings.TrimSpace(item.TokenHash), normalizeTenantID(item.TenantID), strings.TrimSpace(item.CodeHash), strings.TrimSpace(item.ReferralCodeID), strings.TrimSpace(item.InviterUserID), strings.TrimSpace(item.ConfigEpoch), strings.TrimSpace(item.ServiceGroupID), item.InviterCredits, item.InviteeCredits, item.DurationDays, item.ExpiresAt.UTC().Format(time.RFC3339), item.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (r *userReferralRepo) GetHandoff(ctx context.Context, tokenHash string, now time.Time) (*store.UserReferralHandoff, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return nil, nil
	}
	row := r.readDB.QueryRowContext(ctx, `SELECT token_hash, tenant_id, code_hash, referral_code_id, inviter_user_id, config_epoch, service_group_id, inviter_credits, invitee_credits, duration_days, expires_at, used_at, created_at
		FROM user_referral_handoffs WHERE token_hash = ? AND expires_at > ? AND used_at IS NULL LIMIT 1`, tokenHash, now.UTC().Format(time.RFC3339))
	var item store.UserReferralHandoff
	var expiresAt, createdAt string
	var usedAt sql.NullString
	if err := row.Scan(&item.TokenHash, &item.TenantID, &item.CodeHash, &item.ReferralCodeID, &item.InviterUserID, &item.ConfigEpoch, &item.ServiceGroupID, &item.InviterCredits, &item.InviteeCredits, &item.DurationDays, &expiresAt, &usedAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.ExpiresAt, item.CreatedAt = mustParseTime(expiresAt), mustParseTime(createdAt)
	if usedAt.Valid && strings.TrimSpace(usedAt.String) != "" {
		parsed := mustParseTime(usedAt.String)
		item.UsedAt = &parsed
	}
	return &item, nil
}

func (r *userReferralRepo) ConsumeHandoff(ctx context.Context, tokenHash string, usedAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE user_referral_handoffs SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`, usedAt.UTC().Format(time.RFC3339), strings.TrimSpace(tokenHash), usedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (r *userReferralRepo) ReserveIdentity(ctx context.Context, item *store.UserReferralIdentityReservation, now time.Time) (bool, error) {
	if item == nil || strings.TrimSpace(item.IdentityHash) == "" || strings.TrimSpace(item.CodeHash) == "" || strings.TrimSpace(item.SessionHash) == "" {
		return false, nil
	}
	tenantID := normalizeTenantID(item.TenantID)
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_referral_identity_reservations WHERE tenant_id = ? AND identity_hash = ? AND expires_at <= ?`, tenantID, strings.TrimSpace(item.IdentityHash), now.Format(time.RFC3339)); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO user_referral_identity_reservations (tenant_id, identity_hash, code_hash, session_hash, reserved_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, identity_hash) DO UPDATE SET expires_at = excluded.expires_at
		WHERE user_referral_identity_reservations.code_hash = excluded.code_hash AND user_referral_identity_reservations.session_hash = excluded.session_hash`,
		tenantID, strings.TrimSpace(item.IdentityHash), strings.TrimSpace(item.CodeHash), strings.TrimSpace(item.SessionHash), item.ReservedAt.UTC().Format(time.RFC3339), item.ExpiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *userReferralRepo) GetIdentityReservation(ctx context.Context, tenantID, identityHash string, now time.Time) (*store.UserReferralIdentityReservation, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT tenant_id, identity_hash, code_hash, session_hash, reserved_at, expires_at
		FROM user_referral_identity_reservations WHERE tenant_id = ? AND identity_hash = ? AND expires_at > ? LIMIT 1`, normalizeTenantID(tenantID), strings.TrimSpace(identityHash), now.UTC().Format(time.RFC3339))
	var item store.UserReferralIdentityReservation
	var reservedAt, expiresAt string
	if err := row.Scan(&item.TenantID, &item.IdentityHash, &item.CodeHash, &item.SessionHash, &reservedAt, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.ReservedAt, item.ExpiresAt = mustParseTime(reservedAt), mustParseTime(expiresAt)
	return &item, nil
}

func (r *userReferralRepo) ReleaseIdentityReservation(ctx context.Context, tenantID, identityHash, sessionHash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM user_referral_identity_reservations WHERE tenant_id = ? AND identity_hash = ? AND session_hash = ?`, normalizeTenantID(tenantID), strings.TrimSpace(identityHash), strings.TrimSpace(sessionHash))
	return err
}

func (r *userReferralRepo) CreateStatusHistory(ctx context.Context, item *store.UserReferralStatusHistory) error {
	if item == nil || strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.ReferralID) == "" || strings.TrimSpace(item.ToStatus) == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_referral_status_history (id, tenant_id, referral_id, from_status, to_status, reason, actor_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, normalizeTenantID(item.TenantID), strings.TrimSpace(item.ReferralID), strings.TrimSpace(item.FromStatus), strings.TrimSpace(item.ToStatus), strings.TrimSpace(item.Reason), strings.TrimSpace(item.ActorUserID), item.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (r *userReferralRepo) ListStatusHistory(ctx context.Context, tenantID, referralID string) ([]*store.UserReferralStatusHistory, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, tenant_id, referral_id, from_status, to_status, reason, actor_user_id, created_at
		FROM user_referral_status_history WHERE tenant_id = ? AND referral_id = ? ORDER BY created_at DESC, id DESC`, normalizeTenantID(tenantID), strings.TrimSpace(referralID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*store.UserReferralStatusHistory, 0)
	for rows.Next() {
		var item store.UserReferralStatusHistory
		var createdAt string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ReferralID, &item.FromStatus, &item.ToStatus, &item.Reason, &item.ActorUserID, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *userReferralRepo) CountInviterRewardedOnOrAfter(ctx context.Context, tenantID, inviterUserID string, start time.Time) (int, error) {
	var count int
	err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_referrals WHERE tenant_id = ? AND inviter_user_id = ? AND status IN ('attributed', 'rewarded', 'reward_failed') AND registered_at >= ?`, normalizeTenantID(tenantID), strings.TrimSpace(inviterUserID), start.UTC().Format(time.RFC3339)).Scan(&count)
	return count, err
}

func (r *userReferralRepo) ListRewardRecoveryCandidates(ctx context.Context, tenantID string, limit int) ([]*store.UserReferral, error) {
	query := `SELECT id, tenant_id, referral_code_id, inviter_user_id, invitee_user_id, status, registered_at, service_group_id, inviter_credits, invitee_credits, duration_days, inviter_grant_id, invitee_grant_id, created_at, updated_at
		FROM user_referrals WHERE tenant_id = ? AND status IN ('attributed', 'reward_failed') ORDER BY updated_at ASC, id ASC`
	args := []any{normalizeTenantID(tenantID)}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*store.UserReferral, 0)
	for rows.Next() {
		item, err := scanUserReferral(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *userReferralRepo) IncrementDailyMetric(ctx context.Context, tenantID, event string, occurredAt time.Time) error {
	tenantID = normalizeTenantID(tenantID)
	event = strings.TrimSpace(strings.ToLower(event))
	if event == "" {
		return nil
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_referral_daily_metrics (tenant_id, metric_date, event, count, updated_at)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(tenant_id, metric_date, event) DO UPDATE SET count = count + 1, updated_at = excluded.updated_at`,
		tenantID, occurredAt.UTC().Format("2006-01-02"), event, time.Now().UTC().Format(time.RFC3339))
	return err
}

// RecordRewardMetricEvent makes reward-lifecycle aggregates safe to reconcile
// repeatedly. The unique event row and daily counter live in one transaction:
// an already-observed grant is a no-op rather than a second metric increment.
func (r *userReferralRepo) RecordRewardMetricEvent(ctx context.Context, tenantID, eventKey, event string, occurredAt time.Time) (bool, error) {
	tenantID = normalizeTenantID(tenantID)
	eventKey = strings.TrimSpace(eventKey)
	event = strings.TrimSpace(strings.ToLower(event))
	if eventKey == "" || event == "" {
		return false, nil
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	occurredAt = occurredAt.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO user_referral_reward_metric_events
		(tenant_id, event_key, event, occurred_at, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, event_key, event) DO NOTHING`,
		tenantID, eventKey, event, occurredAt.Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	created, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if created == 0 {
		return false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_referral_daily_metrics (tenant_id, metric_date, event, count, updated_at)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(tenant_id, metric_date, event) DO UPDATE SET count = count + 1, updated_at = excluded.updated_at`,
		tenantID, occurredAt.Format("2006-01-02"), event, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *userReferralRepo) ListDailyMetrics(ctx context.Context, tenantID string, from, to time.Time) ([]*store.UserReferralDailyMetric, error) {
	tenantID = normalizeTenantID(tenantID)
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -29)
	}
	fromDate, toDate := from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02")
	rows, err := r.readDB.QueryContext(ctx, `SELECT tenant_id, metric_date, event, count
		FROM user_referral_daily_metrics WHERE tenant_id = ? AND metric_date >= ? AND metric_date <= ?
		ORDER BY metric_date DESC, event ASC`, tenantID, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*store.UserReferralDailyMetric, 0)
	for rows.Next() {
		item := &store.UserReferralDailyMetric{}
		if err := rows.Scan(&item.TenantID, &item.Date, &item.Event, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *userReferralRepo) ListReservedReferrals(ctx context.Context, tenantID string, offset, limit int) ([]*store.UserReferralInvitee, int, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	tenantID = normalizeTenantID(tenantID)
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_referrals WHERE tenant_id = ? AND status = 'reserved'`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx, `SELECT r.id, r.invitee_user_id, u.email, r.registered_at, r.status, r.inviter_credits, r.invitee_credits, r.inviter_grant_id, r.invitee_grant_id
		FROM user_referrals r JOIN users u ON u.tenant_id = r.tenant_id AND u.id = r.invitee_user_id
		WHERE r.tenant_id = ? AND r.status = 'reserved' ORDER BY r.registered_at ASC, r.id ASC LIMIT ? OFFSET ?`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*store.UserReferralInvitee, 0, limit)
	for rows.Next() {
		var item store.UserReferralInvitee
		var registered string
		if err := rows.Scan(&item.ReferralID, &item.InviteeUserID, &item.InviteeEmail, &registered, &item.Status, &item.InviterCredits, &item.InviteeCredits, &item.InviterGrantID, &item.InviteeGrantID); err != nil {
			return nil, 0, err
		}
		item.RegisteredAt = mustParseTime(registered)
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (r *userReferralRepo) ListInviterSummaries(ctx context.Context, filter store.UserReferralFilter) ([]*store.UserReferralInviterSummary, int, error) {
	tenantID := normalizeTenantID(filter.TenantID)
	offset, limit := filter.Offset, filter.Limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	where, args := `r.tenant_id = ?`, []any{tenantID}
	if q := strings.TrimSpace(filter.Search); q != "" {
		where += ` AND (lower(u.email) LIKE lower(?) OR lower(u.id) LIKE lower(?))`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	var total int
	// Only registrations that reached the recoverable reward pipeline belong in
	// invitation activity. Reserved/rejected/expired/revoked records remain
	// available through their direct admin review paths but must not be
	// represented as successful invitations on the inviter dashboard.
	where += ` AND r.status IN ('rewarded', 'reward_failed')`
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT r.inviter_user_id) FROM user_referrals r JOIN users u ON u.tenant_id = r.tenant_id AND u.id = r.inviter_user_id WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := r.readDB.QueryContext(ctx, `SELECT r.inviter_user_id, u.email, COUNT(*), COALESCE(SUM(r.inviter_credits), 0), MAX(r.registered_at), GROUP_CONCAT(r.inviter_grant_id)
		FROM user_referrals r JOIN users u ON u.tenant_id = r.tenant_id AND u.id = r.inviter_user_id WHERE `+where+` GROUP BY r.inviter_user_id, u.email ORDER BY MAX(r.registered_at) DESC, r.inviter_user_id ASC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []*store.UserReferralInviterSummary{}
	for rows.Next() {
		var item store.UserReferralInviterSummary
		var last string
		var grantIDs string
		if err := rows.Scan(&item.InviterUserID, &item.InviterEmail, &item.InviteeCount, &item.CreditsGranted, &last, &grantIDs); err != nil {
			return nil, 0, err
		}
		if last != "" {
			t := mustParseTime(last)
			item.LastRegisteredAt = &t
		}
		for _, grantID := range strings.Split(grantIDs, ",") {
			if grantID = strings.TrimSpace(grantID); grantID != "" {
				item.InviterGrantIDs = append(item.InviterGrantIDs, grantID)
			}
		}
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (r *userReferralRepo) ListInvitees(ctx context.Context, filter store.UserReferralFilter) ([]*store.UserReferralInvitee, int, error) {
	return r.listInvitees(ctx, filter, false)
}

func (r *userReferralRepo) ListReferralInviteesForReview(ctx context.Context, filter store.UserReferralFilter) ([]*store.UserReferralInvitee, int, error) {
	return r.listInvitees(ctx, filter, true)
}

func (r *userReferralRepo) listInvitees(ctx context.Context, filter store.UserReferralFilter, includeReviewAndAudit bool) ([]*store.UserReferralInvitee, int, error) {
	tenantID := normalizeTenantID(filter.TenantID)
	offset, limit := filter.Offset, filter.Limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	where, args := `r.tenant_id = ? AND r.inviter_user_id = ?`, []any{tenantID, strings.TrimSpace(filter.InviterUserID)}
	if !includeReviewAndAudit {
		// The user-facing invitation history is an expansion of the successful
		// invitation metric, so retain failed-but-retryable rewards and omit
		// records that never became a successful registration attribution.
		where += ` AND r.status IN ('rewarded', 'reward_failed')`
	}
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_referrals r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := r.readDB.QueryContext(ctx, `SELECT r.id, r.invitee_user_id, u.email, r.registered_at, r.status, r.inviter_credits, r.invitee_credits, r.inviter_grant_id, r.invitee_grant_id FROM user_referrals r JOIN users u ON u.tenant_id = r.tenant_id AND u.id = r.invitee_user_id WHERE `+where+` ORDER BY r.registered_at DESC, r.id ASC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []*store.UserReferralInvitee{}
	for rows.Next() {
		var item store.UserReferralInvitee
		var registered string
		if err := rows.Scan(&item.ReferralID, &item.InviteeUserID, &item.InviteeEmail, &registered, &item.Status, &item.InviterCredits, &item.InviteeCredits, &item.InviterGrantID, &item.InviteeGrantID); err != nil {
			return nil, 0, err
		}
		item.RegisteredAt = mustParseTime(registered)
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (r *emailBlockRepo) getEmailBlock(ctx context.Context, where string, args ...any) (*store.EmailBlockItem, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT id, tenant_id, email, reason, created_at, updated_at FROM email_blocklist WHERE `+where, args...)
	var item store.EmailBlockItem
	var createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.TenantID, &item.Email, &item.Reason, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *emailBlockRepo) List(ctx context.Context) ([]*store.EmailBlockItem, error) {
	return r.listEmailBlocks(ctx, `1 = 1`)
}

func (r *emailBlockRepo) ListByTenant(ctx context.Context, tenantID string) ([]*store.EmailBlockItem, error) {
	return r.listEmailBlocks(ctx, `tenant_id = ?`, normalizeTenantID(tenantID))
}

func (r *emailBlockRepo) listEmailBlocks(ctx context.Context, where string, args ...any) ([]*store.EmailBlockItem, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, tenant_id, email, reason, created_at, updated_at FROM email_blocklist WHERE `+where+` ORDER BY email ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.EmailBlockItem
	for rows.Next() {
		var item store.EmailBlockItem
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Email, &item.Reason, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *machineRepo) Create(ctx context.Context, machine *store.Machine) error {
	var lastSeen any
	if machine.LastSeenAt != nil {
		lastSeen = machine.LastSeenAt.Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO machines (id, tenant_id, user_id, client_id, name, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		machine.ID,
		normalizeTenantID(machine.TenantID),
		machine.UserID,
		machine.ClientID,
		machine.Name,
		machine.Platform,
		machine.Hostname,
		machine.Arch,
		machine.AppVersion,
		machine.HeartbeatSec,
		machine.MachineTokenHash,
		machine.Status,
		lastSeen,
		machine.CreatedAt.Format(time.RFC3339),
		machine.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *machineRepo) GetByID(ctx context.Context, id string) (*store.Machine, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, user_id, client_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
		 FROM machines WHERE id = ?`,
		id,
	)

	var (
		machine                        store.Machine
		lastSeen, createdAt, updatedAt sql.NullString
	)
	if err := row.Scan(
		&machine.ID,
		&machine.TenantID,
		&machine.UserID,
		&machine.ClientID,
		&machine.Name,
		&machine.Alias,
		&machine.Platform,
		&machine.Hostname,
		&machine.Arch,
		&machine.AppVersion,
		&machine.HeartbeatSec,
		&machine.MachineTokenHash,
		&machine.Status,
		&lastSeen,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if lastSeen.Valid {
		t := mustParseTime(lastSeen.String)
		machine.LastSeenAt = &t
	}
	if createdAt.Valid {
		machine.CreatedAt = mustParseTime(createdAt.String)
	}
	if updatedAt.Valid {
		machine.UpdatedAt = mustParseTime(updatedAt.String)
	}
	return &machine, nil
}

func (r *machineRepo) ListByUserID(ctx context.Context, userID string) ([]*store.Machine, error) {
	rows, err := r.readDB.QueryContext(
		ctx,
		`SELECT id, tenant_id, user_id, client_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
		 FROM machines WHERE user_id = ? ORDER BY updated_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.Machine
	for rows.Next() {
		var (
			machine                        store.Machine
			lastSeen, createdAt, updatedAt sql.NullString
		)
		if err := rows.Scan(
			&machine.ID,
			&machine.TenantID,
			&machine.UserID,
			&machine.ClientID,
			&machine.Name,
			&machine.Alias,
			&machine.Platform,
			&machine.Hostname,
			&machine.Arch,
			&machine.AppVersion,
			&machine.HeartbeatSec,
			&machine.MachineTokenHash,
			&machine.Status,
			&lastSeen,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			t := mustParseTime(lastSeen.String)
			machine.LastSeenAt = &t
		}
		if createdAt.Valid {
			machine.CreatedAt = mustParseTime(createdAt.String)
		}
		if updatedAt.Valid {
			machine.UpdatedAt = mustParseTime(updatedAt.String)
		}
		items = append(items, &machine)
	}

	return items, rows.Err()
}

func (r *machineRepo) UpdateMetadata(ctx context.Context, machineID string, metadata store.MachineMetadata) error {
	// Match enroll/runtime policy: unset → 30s default, tiny values clamp to 5s min.
	heartbeatSec := metadata.HeartbeatIntervalSec
	if heartbeatSec <= 0 || heartbeatSec > 3600 {
		heartbeatSec = 30
	} else if heartbeatSec < 5 {
		heartbeatSec = 5
	}
	// Metadata writes are infrequent relative to last-seen heartbeats and must be
	// durable promptly (enroll path + hello). Do not route through the coalescer.
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE machines
		 SET name = ?, platform = ?, hostname = ?, arch = ?, app_version = ?, heartbeat_sec = ?, updated_at = ?
		 WHERE id = ?`,
		metadata.Name,
		metadata.Platform,
		metadata.Hostname,
		metadata.Arch,
		metadata.AppVersion,
		heartbeatSec,
		time.Now().Format(time.RFC3339),
		machineID,
	)
}

func (r *machineRepo) UpdateStatus(ctx context.Context, machineID string, status string) error {
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE machines SET status = ?, updated_at = ? WHERE id = ?`,
		status,
		time.Now().Format(time.RFC3339),
		machineID,
	)
}

func (r *machineRepo) UpdateHeartbeat(ctx context.Context, machineID string, at time.Time) error {
	// Use write-coalescer: heartbeats are the highest frequency write
	// (~1 per 5-60s per machine). At 10K machines this is 167-2000 writes/sec.
	// Coalescing by machineID reduces to ~2000 writes per 5s flush = 1 tx.
	if r.coalesce != nil {
		r.coalesce.Set(
			"machine_hb:"+machineID,
			`UPDATE machines SET last_seen_at = ?, updated_at = ? WHERE id = ?`,
			at.Format(time.RFC3339),
			at.Format(time.RFC3339),
			machineID,
		)
		return nil
	}
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE machines SET last_seen_at = ?, updated_at = ? WHERE id = ?`,
		at.Format(time.RFC3339),
		at.Format(time.RFC3339),
		machineID,
	)
}

func (r *machineRepo) GetByUserAndClientID(ctx context.Context, userID, clientID string) (*store.Machine, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, user_id, client_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
		 FROM machines WHERE user_id = ? AND client_id = ?`,
		userID, clientID,
	)

	var (
		machine                        store.Machine
		lastSeen, createdAt, updatedAt sql.NullString
	)
	if err := row.Scan(
		&machine.ID,
		&machine.TenantID,
		&machine.UserID,
		&machine.ClientID,
		&machine.Name,
		&machine.Alias,
		&machine.Platform,
		&machine.Hostname,
		&machine.Arch,
		&machine.AppVersion,
		&machine.HeartbeatSec,
		&machine.MachineTokenHash,
		&machine.Status,
		&lastSeen,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if lastSeen.Valid {
		t := mustParseTime(lastSeen.String)
		machine.LastSeenAt = &t
	}
	if createdAt.Valid {
		machine.CreatedAt = mustParseTime(createdAt.String)
	}
	if updatedAt.Valid {
		machine.UpdatedAt = mustParseTime(updatedAt.String)
	}
	return &machine, nil
}

func (r *machineRepo) UpdateTokenHash(ctx context.Context, machineID string, tokenHash string) error {
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE machines SET machine_token_hash = ?, updated_at = ? WHERE id = ?`,
		tokenHash,
		time.Now().Format(time.RFC3339),
		machineID,
	)
}

func (r *machineRepo) UpdateAlias(ctx context.Context, machineID string, alias string) error {
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE machines SET alias = ?, updated_at = ? WHERE id = ?`,
		alias,
		time.Now().Format(time.RFC3339),
		machineID,
	)
}

func (r *machineRepo) ListAll(ctx context.Context) ([]*store.Machine, error) {
	return r.listMachines(ctx, "")
}

func (r *machineRepo) ListByTenant(ctx context.Context, tenantID string) ([]*store.Machine, error) {
	return r.listMachines(ctx, normalizeTenantID(tenantID))
}

func (r *machineRepo) listMachines(ctx context.Context, tenantID string) ([]*store.Machine, error) {
	where := ""
	args := []any{}
	if tenantID != "" {
		where = "WHERE tenant_id = ?"
		args = append(args, tenantID)
	}
	rows, err := r.readDB.QueryContext(
		ctx,
		`SELECT id, tenant_id, user_id, client_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
		 FROM machines `+where+` ORDER BY updated_at DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.Machine
	for rows.Next() {
		var (
			machine                        store.Machine
			lastSeen, createdAt, updatedAt sql.NullString
		)
		if err := rows.Scan(
			&machine.ID,
			&machine.TenantID,
			&machine.UserID,
			&machine.ClientID,
			&machine.Name,
			&machine.Alias,
			&machine.Platform,
			&machine.Hostname,
			&machine.Arch,
			&machine.AppVersion,
			&machine.HeartbeatSec,
			&machine.MachineTokenHash,
			&machine.Status,
			&lastSeen,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			t := mustParseTime(lastSeen.String)
			machine.LastSeenAt = &t
		}
		if createdAt.Valid {
			machine.CreatedAt = mustParseTime(createdAt.String)
		}
		if updatedAt.Valid {
			machine.UpdatedAt = mustParseTime(updatedAt.String)
		}
		items = append(items, &machine)
	}
	return items, rows.Err()
}

func (r *machineRepo) Delete(ctx context.Context, machineID string) error {
	return execWrite(ctx, r.batch, r.db, `DELETE FROM machines WHERE id = ?`, machineID)
}

func (r *machineRepo) DeleteByUserID(ctx context.Context, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE user_id = ? AND status != 'online'`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) DeleteByTenantUserID(ctx context.Context, tenantID, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE tenant_id = ? AND user_id = ? AND status != 'online'`, normalizeTenantID(tenantID), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) ForceDeleteByUserID(ctx context.Context, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) ForceDeleteByTenantUserID(ctx context.Context, tenantID, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE tenant_id = ? AND user_id = ?`, normalizeTenantID(tenantID), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) DeleteOffline(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE status != 'online'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) DeleteOfflineByTenant(ctx context.Context, tenantID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE tenant_id = ? AND status != 'online'`, normalizeTenantID(tenantID))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) DeleteOfflineByUserID(ctx context.Context, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE user_id = ? AND status != 'online'`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) DeleteOfflineByTenantUserID(ctx context.Context, tenantID, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE tenant_id = ? AND user_id = ? AND status != 'online'`, normalizeTenantID(tenantID), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *machineRepo) ResetAllOnline(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE machines SET status = 'offline' WHERE status = 'online'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *viewerTokenRepo) Create(ctx context.Context, token *store.ViewerToken) error {
	var revokedAt any
	if token.RevokedAt != nil {
		revokedAt = token.RevokedAt.Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO viewer_tokens (id, tenant_id, user_id, token_hash, expires_at, created_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token.ID,
		normalizeTenantID(token.TenantID),
		token.UserID,
		token.TokenHash,
		token.ExpiresAt.Format(time.RFC3339),
		token.CreatedAt.Format(time.RFC3339),
		revokedAt,
	)
	return err
}

func (r *viewerTokenRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*store.ViewerToken, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, user_id, token_hash, expires_at, created_at, revoked_at
		 FROM viewer_tokens WHERE token_hash = ?`,
		tokenHash,
	)

	var (
		token                           store.ViewerToken
		expiresAt, createdAt, revokedAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
		&token.TenantID,
		&token.UserID,
		&token.TokenHash,
		&expiresAt,
		&createdAt,
		&revokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if expiresAt.Valid {
		token.ExpiresAt = mustParseTime(expiresAt.String)
	}
	if createdAt.Valid {
		token.CreatedAt = mustParseTime(createdAt.String)
	}
	if revokedAt.Valid {
		t := mustParseTime(revokedAt.String)
		token.RevokedAt = &t
	}
	return &token, nil
}

func (r *viewerTokenRepo) ExtendExpiry(ctx context.Context, tokenID string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE viewer_tokens SET expires_at = ? WHERE id = ?`,
		expiresAt.Format(time.RFC3339),
		tokenID,
	)
	return err
}

func (r *viewerTokenRepo) DeleteByUserID(ctx context.Context, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM viewer_tokens WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *loginTokenRepo) Create(ctx context.Context, token *store.LoginToken) error {
	var consumedAt any
	if token.ConsumedAt != nil {
		consumedAt = token.ConsumedAt.Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO login_tokens (id, tenant_id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID,
		normalizeTenantID(token.TenantID),
		token.Email,
		token.TokenHash,
		token.PollTokenHash,
		token.Purpose,
		token.ExpiresAt.Format(time.RFC3339),
		consumedAt,
		token.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *loginTokenRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*store.LoginToken, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens WHERE token_hash = ?`,
		tokenHash,
	)

	var (
		token                            store.LoginToken
		expiresAt, consumedAt, createdAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
		&token.TenantID,
		&token.Email,
		&token.TokenHash,
		&token.PollTokenHash,
		&token.Purpose,
		&expiresAt,
		&consumedAt,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if expiresAt.Valid {
		token.ExpiresAt = mustParseTime(expiresAt.String)
	}
	if consumedAt.Valid {
		t := mustParseTime(consumedAt.String)
		token.ConsumedAt = &t
	}
	if createdAt.Valid {
		token.CreatedAt = mustParseTime(createdAt.String)
	}
	return &token, nil
}

func (r *loginTokenRepo) GetByPollTokenHash(ctx context.Context, pollTokenHash string) (*store.LoginToken, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens WHERE poll_token_hash = ?`,
		pollTokenHash,
	)

	var (
		token                            store.LoginToken
		expiresAt, consumedAt, createdAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
		&token.TenantID,
		&token.Email,
		&token.TokenHash,
		&token.PollTokenHash,
		&token.Purpose,
		&expiresAt,
		&consumedAt,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if expiresAt.Valid {
		token.ExpiresAt = mustParseTime(expiresAt.String)
	}
	if consumedAt.Valid {
		t := mustParseTime(consumedAt.String)
		token.ConsumedAt = &t
	}
	if createdAt.Valid {
		token.CreatedAt = mustParseTime(createdAt.String)
	}
	return &token, nil
}

func (r *loginTokenRepo) Consume(ctx context.Context, tokenID string, consumedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE login_tokens SET consumed_at = ? WHERE id = ?`,
		consumedAt.Format(time.RFC3339),
		tokenID,
	)
	return err
}

func (r *loginTokenRepo) GetPendingByEmail(ctx context.Context, email string) (*store.LoginToken, error) {
	return r.getPendingByEmail(ctx, "", email)
}

func (r *loginTokenRepo) GetPendingByTenantEmail(ctx context.Context, tenantID string, email string) (*store.LoginToken, error) {
	return r.getPendingByEmail(ctx, normalizeTenantID(tenantID), email)
}

func (r *loginTokenRepo) getPendingByEmail(ctx context.Context, tenantID string, email string) (*store.LoginToken, error) {
	where := `email = ? AND consumed_at IS NULL AND expires_at > ?`
	args := []any{email, time.Now().Format(time.RFC3339)}
	if tenantID != "" {
		where = `tenant_id = ? AND email = ? AND consumed_at IS NULL AND expires_at > ?`
		args = []any{tenantID, email, time.Now().Format(time.RFC3339)}
	}
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, tenant_id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens
		 WHERE `+where+`
		 ORDER BY created_at DESC LIMIT 1`,
		args...,
	)

	var (
		token                            store.LoginToken
		expiresAt, consumedAt, createdAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
		&token.TenantID,
		&token.Email,
		&token.TokenHash,
		&token.PollTokenHash,
		&token.Purpose,
		&expiresAt,
		&consumedAt,
		&createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if expiresAt.Valid {
		token.ExpiresAt = mustParseTime(expiresAt.String)
	}
	if consumedAt.Valid {
		t := mustParseTime(consumedAt.String)
		token.ConsumedAt = &t
	}
	if createdAt.Valid {
		token.CreatedAt = mustParseTime(createdAt.String)
	}
	return &token, nil
}

func (r *loginTokenRepo) ListPending(ctx context.Context) ([]*store.LoginToken, error) {
	return r.listPending(ctx, `consumed_at IS NULL AND expires_at > ?`, time.Now().Format(time.RFC3339))
}

func (r *loginTokenRepo) ListPendingByTenant(ctx context.Context, tenantID string) ([]*store.LoginToken, error) {
	return r.listPending(ctx, `tenant_id = ? AND consumed_at IS NULL AND expires_at > ?`, normalizeTenantID(tenantID), time.Now().Format(time.RFC3339))
}

func (r *loginTokenRepo) listPending(ctx context.Context, where string, args ...any) ([]*store.LoginToken, error) {
	rows, err := r.readDB.QueryContext(
		ctx,
		`SELECT id, tenant_id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens
		 WHERE `+where+`
		 ORDER BY created_at DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*store.LoginToken
	for rows.Next() {
		var (
			token                            store.LoginToken
			expiresAt, consumedAt, createdAt sql.NullString
		)
		if err := rows.Scan(
			&token.ID,
			&token.TenantID,
			&token.Email,
			&token.TokenHash,
			&token.PollTokenHash,
			&token.Purpose,
			&expiresAt,
			&consumedAt,
			&createdAt,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			token.ExpiresAt = mustParseTime(expiresAt.String)
		}
		if consumedAt.Valid {
			t := mustParseTime(consumedAt.String)
			token.ConsumedAt = &t
		}
		if createdAt.Valid {
			token.CreatedAt = mustParseTime(createdAt.String)
		}
		tokens = append(tokens, &token)
	}
	return tokens, rows.Err()
}

func (r *loginTokenRepo) RefreshToken(ctx context.Context, tokenID string, tokenHash string, pollTokenHash string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE login_tokens SET token_hash = ?, poll_token_hash = ? WHERE id = ?`,
		tokenHash,
		pollTokenHash,
		tokenID,
	)
	return err
}

func (r *sessionRepo) Create(ctx context.Context, session *store.Session) error {
	var endedAt any
	if session.EndedAt != nil {
		endedAt = session.EndedAt.Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO sessions (id, tenant_id, machine_id, user_id, tool, title, project_path, status, summary_json, preview_text, output_seq, host_online, started_at, updated_at, ended_at, exit_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
		normalizeTenantID(session.TenantID),
		session.MachineID,
		session.UserID,
		session.Tool,
		session.Title,
		session.ProjectPath,
		session.Status,
		session.SummaryJSON,
		session.PreviewText,
		session.OutputSeq,
		boolToInt(session.HostOnline),
		session.StartedAt.Format(time.RFC3339),
		session.UpdatedAt.Format(time.RFC3339),
		endedAt,
		session.ExitCode,
	)
	return err
}

func (r *sessionRepo) UpdateSummary(ctx context.Context, sessionID string, summaryJSON string, status string, updatedAt time.Time) error {
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE sessions SET summary_json = ?, status = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		summaryJSON,
		status,
		updatedAt.Format(time.RFC3339),
		sessionID,
		store.TenantIDFromContext(ctx),
	)
}

func (r *sessionRepo) UpdatePreview(ctx context.Context, sessionID string, previewText string, outputSeq int64, updatedAt time.Time) error {
	// Use write-coalescer: preview deltas are already throttled to 1 write per
	// 2s per session by the session.Service. With 1000 active sessions that's
	// still 500 writes/sec. Coalescing by sessionID reduces to ~1000 writes per
	// 5s flush = 1 transaction.
	if r.coalesce != nil {
		r.coalesce.Set(
			"session_preview:"+sessionID,
			`UPDATE sessions SET preview_text = ?, output_seq = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
			previewText,
			outputSeq,
			updatedAt.Format(time.RFC3339),
			sessionID,
			store.TenantIDFromContext(ctx),
		)
		return nil
	}
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE sessions SET preview_text = ?, output_seq = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		previewText,
		outputSeq,
		updatedAt.Format(time.RFC3339),
		sessionID,
		store.TenantIDFromContext(ctx),
	)
}

func (r *sessionRepo) UpdateHostOnline(ctx context.Context, sessionID string, hostOnline bool, updatedAt time.Time) error {
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE sessions SET host_online = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		boolToInt(hostOnline),
		updatedAt.Format(time.RFC3339),
		sessionID,
		store.TenantIDFromContext(ctx),
	)
}

func (r *sessionRepo) Close(ctx context.Context, sessionID string, exitCode *int, endedAt time.Time, status string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE sessions SET status = ?, ended_at = ?, exit_code = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		status,
		endedAt.Format(time.RFC3339),
		exitCode,
		endedAt.Format(time.RFC3339),
		sessionID,
		store.TenantIDFromContext(ctx),
	)
	return err
}

func (r *sessionRepo) RecordHeartbeat(ctx context.Context, tenantID, machineID, userID string, at time.Time) error {
	tenantID = normalizeTenantID(tenantID)
	machineID = strings.TrimSpace(machineID)
	userID = strings.TrimSpace(userID)
	if machineID == "" || userID == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	// INSERT OR IGNORE: the UNIQUE constraint on (tenant_id, machine_id, heartbeat_at)
	// prevents exact-second duplicates from network retransmissions.
	err := execWrite(
		ctx,
		r.batch,
		r.db,
		`INSERT OR IGNORE INTO machine_heartbeat_log (tenant_id, machine_id, user_id, heartbeat_at) VALUES (?, ?, ?, ?)`,
		tenantID,
		machineID,
		userID,
		at.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return err
	}
	// Probabilistic cleanup: ~1/500 calls delete records older than 90 days.
	// With 60s heartbeat interval this triggers roughly once every 8 hours per machine.
	if r.heartbeatCleanupCounter.Add(1) >= 500 {
		r.heartbeatCleanupCounter.Store(0)
		cutoff := at.AddDate(0, 0, -90).Format(time.RFC3339)
		_ = execWrite(ctx, r.batch, r.db, `DELETE FROM machine_heartbeat_log WHERE heartbeat_at < ?`, cutoff)
	}
	return nil
}

func (r *sessionRepo) RecordUserTokenUsageSnapshot(ctx context.Context, tenantID, sourceID, userID string, usage store.UserTokenUsage, observedAt time.Time) error {
	tenantID = normalizeTenantID(tenantID)
	sourceID = strings.TrimSpace(sourceID)
	userID = strings.TrimSpace(userID)
	if sourceID == "" || userID == "" || usage.TotalTokens() <= 0 {
		return nil
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var prev store.UserTokenUsage
	err = tx.QueryRowContext(ctx, `
		SELECT input_tokens, output_tokens, cached_input_tokens, cache_write_tokens
		  FROM session_token_usage_snapshots
		 WHERE tenant_id = ? AND session_id = ?`, tenantID, sourceID).
		Scan(&prev.InputTokens, &prev.OutputTokens, &prev.CachedInputTokens, &prev.CacheWriteTokens)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	delta := usageSnapshotDelta(usage, prev)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_token_usage_snapshots (tenant_id, session_id, user_id, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, session_id) DO UPDATE SET
			user_id = excluded.user_id,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cached_input_tokens = excluded.cached_input_tokens,
			cache_write_tokens = excluded.cache_write_tokens,
			updated_at = excluded.updated_at`,
		tenantID, sourceID, userID, usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens, observedAt.Format(time.RFC3339)); err != nil {
		return err
	}

	if delta.TotalTokens() > 0 {
		email := strings.ToLower(strings.TrimSpace(userID))
		var dbEmail string
		if err := tx.QueryRowContext(ctx, `SELECT LOWER(email) FROM users WHERE tenant_id = ? AND id = ?`, tenantID, userID).Scan(&dbEmail); err == nil {
			if trimmed := strings.TrimSpace(dbEmail); trimmed != "" {
				email = strings.ToLower(trimmed)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if email != "" {
			day := observedAt.Format("2006-01-02")
			if _, err := tx.ExecContext(ctx, `
					INSERT INTO user_usage_daily (tenant_id, user_id, user_email, day, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT(tenant_id, user_email, day) DO UPDATE SET
						user_id = CASE WHEN excluded.user_id <> '' THEN excluded.user_id ELSE user_usage_daily.user_id END,
						input_tokens = user_usage_daily.input_tokens + excluded.input_tokens,
						output_tokens = user_usage_daily.output_tokens + excluded.output_tokens,
						cached_input_tokens = user_usage_daily.cached_input_tokens + excluded.cached_input_tokens,
						cache_write_tokens = user_usage_daily.cache_write_tokens + excluded.cache_write_tokens,
						updated_at = excluded.updated_at`,
				tenantID, userID, email, day, delta.InputTokens, delta.OutputTokens, delta.CachedInputTokens, delta.CacheWriteTokens, observedAt.Format(time.RFC3339)); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (r *sessionRepo) SummarizeUserTokenUsage(ctx context.Context, tenantID string, start, end time.Time) ([]store.UserTokenSummary, error) {
	tenantID = normalizeTenantID(tenantID)
	if end.Before(start) || end.Equal(start) {
		return []store.UserTokenSummary{}, nil
	}
	startDay := start.UTC().Format("2006-01-02")
	endDay := end.UTC().Add(-time.Nanosecond).Format("2006-01-02")
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(uud.user_id, ''), ui.user_id, u.id, ''),
		       uud.user_email,
		       SUM(input_tokens),
		       SUM(output_tokens),
		       SUM(cached_input_tokens),
		       SUM(cache_write_tokens)
		  FROM user_usage_daily uud
		  LEFT JOIN user_identities ui
		    ON ui.tenant_id = uud.tenant_id
		   AND (
		     (lower(uud.user_email) NOT LIKE 'phone:%' AND ui.type = 'email' AND lower(ui.value) = lower(uud.user_email))
		     OR
		     (lower(uud.user_email) LIKE 'phone:%' AND ui.type = 'phone' AND lower(ui.value) = lower(substr(uud.user_email, 7)))
		   )
		  LEFT JOIN users u
		    ON u.tenant_id = uud.tenant_id
		   AND lower(u.email) = lower(uud.user_email)
		 WHERE uud.tenant_id = ?
		   AND uud.day >= ?
		   AND uud.day <= ?
		 GROUP BY COALESCE(NULLIF(uud.user_id, ''), ui.user_id, u.id, ''), uud.user_email`, tenantID, startDay, endDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byKey := map[string]store.UserTokenSummary{}
	for rows.Next() {
		var item store.UserTokenSummary
		if err := rows.Scan(&item.UserID, &item.UserEmail, &item.Usage.InputTokens, &item.Usage.OutputTokens, &item.Usage.CachedInputTokens, &item.Usage.CacheWriteTokens); err != nil {
			return nil, err
		}
		item.UserID = strings.TrimSpace(item.UserID)
		item.UserEmail = strings.ToLower(strings.TrimSpace(item.UserEmail))
		if item.UserID == "" && item.UserEmail == "" {
			continue
		}
		key := "email:" + item.UserEmail
		if item.UserID != "" {
			key = "user:" + item.UserID
		}
		existing := byKey[key]
		existing.UserID = item.UserID
		existing.UserEmail = preferredUsageDisplayAccount(existing.UserEmail, item.UserEmail)
		existing.Usage.InputTokens += item.Usage.InputTokens
		existing.Usage.OutputTokens += item.Usage.OutputTokens
		existing.Usage.CachedInputTokens += item.Usage.CachedInputTokens
		existing.Usage.CacheWriteTokens += item.Usage.CacheWriteTokens
		byKey[key] = existing
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]store.UserTokenSummary, 0, len(byKey))
	userIDs := make([]string, 0, len(byKey))
	for _, item := range byKey {
		if item.UserID != "" {
			userIDs = append(userIDs, item.UserID)
		}
	}
	emailByUserID := r.resolveUserEmails(ctx, tenantID, userIDs)
	for _, item := range byKey {
		if email := emailByUserID[item.UserID]; email != "" {
			item.UserEmail = preferredUsageDisplayAccount(item.UserEmail, email)
		}
		if item.UserEmail == "" && item.UserID != "" {
			item.UserEmail = item.UserID
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UserEmail == out[j].UserEmail {
			return out[i].UserID < out[j].UserID
		}
		return out[i].UserEmail < out[j].UserEmail
	})
	return out, nil
}

func usageSnapshotDelta(current, previous store.UserTokenUsage) store.UserTokenUsage {
	if current.TotalTokens() <= 0 {
		return store.UserTokenUsage{}
	}
	if current.TotalTokens() < previous.TotalTokens() {
		return current
	}
	return store.UserTokenUsage{
		InputTokens:       positiveFieldDelta(current.InputTokens, previous.InputTokens),
		OutputTokens:      positiveFieldDelta(current.OutputTokens, previous.OutputTokens),
		CachedInputTokens: positiveFieldDelta(current.CachedInputTokens, previous.CachedInputTokens),
		CacheWriteTokens:  positiveFieldDelta(current.CacheWriteTokens, previous.CacheWriteTokens),
	}
}

func positiveFieldDelta(current, previous int64) int64 {
	if current > previous {
		return current - previous
	}
	return 0
}

func (r *sessionRepo) SummarizeUserDurations(ctx context.Context, tenantID string, start, end, now time.Time) ([]store.UserDurationSummary, error) {
	tenantID = normalizeTenantID(tenantID)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	if end.Before(start) || end.Equal(start) {
		return []store.UserDurationSummary{}, nil
	}

	// Duration is calculated exclusively from machine_heartbeat_log.
	// See comment below at the heartbeat query for rationale.

	type durationInterval struct {
		start time.Time
		end   time.Time
	}
	byUserID := map[string][]durationInterval{}
	addInterval := func(userID string, intervalStart, intervalEnd time.Time) {
		userID = strings.TrimSpace(userID)
		if userID == "" || !intervalEnd.After(intervalStart) {
			return
		}
		if intervalStart.Before(start) {
			intervalStart = start
		}
		if intervalEnd.After(end) {
			intervalEnd = end
		}
		if intervalEnd.After(intervalStart) {
			byUserID[userID] = append(byUserID[userID], durationInterval{start: intervalStart, end: intervalEnd})
		}
	}

	parseTime := func(raw string) (time.Time, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return time.Time{}, false
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, false
		}
		return parsed, true
	}

	// Duration is calculated exclusively from machine_heartbeat_log, which only
	// records entries when LLM token usage increases (see handlers_machine.go).
	// This ensures the leaderboard shows "AI usage time", not "idle client uptime".
	// The sessions table is NOT used for duration — it represents connection
	// status (client connected to Hub), not actual AI interaction.
	heartbeatRows, err := r.readDB.QueryContext(ctx, `
		SELECT user_id, heartbeat_at
		  FROM machine_heartbeat_log
		 WHERE tenant_id = ?
		   AND heartbeat_at >= ?
		   AND heartbeat_at < ?
		 ORDER BY user_id, heartbeat_at`,
		tenantID, start.Format(time.RFC3339), end.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer heartbeatRows.Close()

	const maxHeartbeatGap = 5 * time.Minute
	const baseHeartbeatDuration = time.Minute

	var currentUser string
	var heartbeatStart, heartbeatEnd time.Time
	flushHeartbeat := func() {
		if currentUser == "" || heartbeatStart.IsZero() {
			return
		}
		intervalEnd := heartbeatEnd
		if intervalEnd.Sub(heartbeatStart) < baseHeartbeatDuration {
			intervalEnd = heartbeatStart.Add(baseHeartbeatDuration)
		}
		addInterval(currentUser, heartbeatStart, intervalEnd)
	}
	for heartbeatRows.Next() {
		var userID string
		var atRaw string
		if err := heartbeatRows.Scan(&userID, &atRaw); err != nil {
			return nil, err
		}
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		at, ok := parseTime(atRaw)
		if !ok {
			continue
		}
		if currentUser == "" {
			currentUser = userID
			heartbeatStart = at
			heartbeatEnd = at
			continue
		}
		if userID == currentUser && at.Sub(heartbeatEnd) <= maxHeartbeatGap {
			heartbeatEnd = at
		} else {
			flushHeartbeat()
			currentUser = userID
			heartbeatStart = at
			heartbeatEnd = at
		}
	}
	if err := heartbeatRows.Err(); err != nil {
		return nil, err
	}
	flushHeartbeat()

	durationByUserID := map[string]int64{}
	for userID, intervals := range byUserID {
		sort.Slice(intervals, func(i, j int) bool {
			if intervals[i].start.Equal(intervals[j].start) {
				return intervals[i].end.Before(intervals[j].end)
			}
			return intervals[i].start.Before(intervals[j].start)
		})
		var mergedStart, mergedEnd time.Time
		var total int64
		for _, interval := range intervals {
			if mergedStart.IsZero() {
				mergedStart = interval.start
				mergedEnd = interval.end
				continue
			}
			if !interval.start.After(mergedEnd) {
				if interval.end.After(mergedEnd) {
					mergedEnd = interval.end
				}
				continue
			}
			total += int64(mergedEnd.Sub(mergedStart).Seconds())
			mergedStart = interval.start
			mergedEnd = interval.end
		}
		if !mergedStart.IsZero() {
			total += int64(mergedEnd.Sub(mergedStart).Seconds())
		}
		if total > 0 {
			durationByUserID[userID] = total
		}
	}

	userIDs := make([]string, 0, len(durationByUserID))
	for uid := range durationByUserID {
		userIDs = append(userIDs, uid)
	}
	emailByUserID := r.resolveUserEmails(ctx, tenantID, userIDs)

	type durationTotals struct {
		userEmail       string
		durationSeconds int64
	}
	durationTotalsByUserID := map[string]durationTotals{}
	for userID, seconds := range durationByUserID {
		email := emailByUserID[userID]
		if email == "" {
			if strings.Contains(userID, "@") {
				email = strings.ToLower(strings.TrimSpace(userID))
			}
		}
		if email == "" {
			continue
		}
		total := durationTotalsByUserID[userID]
		total.userEmail = preferredUsageDisplayAccount(total.userEmail, email)
		total.durationSeconds += seconds
		durationTotalsByUserID[userID] = total
	}

	out := make([]store.UserDurationSummary, 0, len(durationTotalsByUserID))
	for userID, total := range durationTotalsByUserID {
		if total.durationSeconds <= 0 {
			continue
		}
		out = append(out, store.UserDurationSummary{UserID: userID, UserEmail: total.userEmail, DurationSeconds: total.durationSeconds})
	}

	// Calculate online time from sessions table as tie-breaker for ranking.
	// Online time = total connection uptime (client connected to Hub), distinct
	// from usage time (actual LLM interaction measured by heartbeats above).
	onlineByUserID := r.summarizeOnlineSeconds(ctx, tenantID, start, end, now)
	for i := range out {
		out[i].OnlineSeconds = onlineByUserID[out[i].UserID]
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].UserEmail < out[j].UserEmail
	})
	return out, nil
}

// summarizeOnlineSeconds calculates connection uptime from sessions table.
// Used only as a tie-breaker in rankings (not as the primary "usage duration").
func (r *sessionRepo) summarizeOnlineSeconds(ctx context.Context, tenantID string, start, end, now time.Time) map[string]int64 {
	result := make(map[string]int64)
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT user_id, status, host_online, started_at, ended_at
		  FROM sessions
		 WHERE tenant_id = ?
		   AND started_at < ?`,
		tenantID, end.Format(time.RFC3339))
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var userID, status string
		var hostOnline bool
		var startedRaw string
		var endedRaw sql.NullString
		if err := rows.Scan(&userID, &status, &hostOnline, &startedRaw, &endedRaw); err != nil {
			continue
		}
		startedRaw = strings.TrimSpace(startedRaw)
		startedAt, err := time.Parse(time.RFC3339, startedRaw)
		if err != nil {
			continue
		}
		var finishedAt time.Time
		if endedRaw.Valid {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(endedRaw.String)); err == nil {
				finishedAt = t
			}
		}
		if finishedAt.IsZero() {
			if strings.EqualFold(strings.TrimSpace(status), "running") || hostOnline {
				finishedAt = now
			}
		}
		if finishedAt.IsZero() || !finishedAt.After(startedAt) {
			continue
		}
		// Clamp to query window
		if startedAt.Before(start) {
			startedAt = start
		}
		if finishedAt.After(end) {
			finishedAt = end
		}
		if !finishedAt.After(startedAt) {
			continue
		}
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		result[userID] += int64(finishedAt.Sub(startedAt).Seconds())
	}

	// Cap online seconds per user to the query window duration (prevent stale
	// overlapping sessions from producing values exceeding calendar time).
	maxOnline := int64(end.Sub(start).Seconds())
	if now.Before(end) {
		elapsed := int64(now.Sub(start).Seconds())
		if elapsed > 0 {
			maxOnline = elapsed
		}
	}
	for userID, seconds := range result {
		if maxOnline > 0 && seconds > maxOnline {
			result[userID] = maxOnline
		}
	}

	return result
}

func preferredUsageDisplayAccount(current, candidate string) string {
	current = strings.ToLower(strings.TrimSpace(current))
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if candidate == "" {
		return current
	}
	if current == "" {
		return candidate
	}
	currentIsEmail := strings.Contains(current, "@") && !strings.HasPrefix(current, "phone:")
	candidateIsEmail := strings.Contains(candidate, "@") && !strings.HasPrefix(candidate, "phone:")
	if candidateIsEmail && !currentIsEmail {
		return candidate
	}
	return current
}

// resolveUserEmails batch-resolves user IDs to lowercase email addresses.
// Handles SQLite's SQLITE_MAX_VARIABLE_NUMBER limit by batching.
func (r *sessionRepo) resolveUserEmails(ctx context.Context, tenantID string, userIDs []string) map[string]string {
	result := make(map[string]string, len(userIDs))
	if len(userIDs) == 0 {
		return result
	}
	const batchSize = 400 // well under SQLite's default 999 limit, minus 1 for tenantID
	for i := 0; i < len(userIDs); i += batchSize {
		end := i + batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		batch := userIDs[i:end]
		placeholders := make([]string, len(batch))
		args := make([]any, 0, len(batch)+1)
		args = append(args, tenantID)
		for j, uid := range batch {
			placeholders[j] = "?"
			args = append(args, uid)
		}
		query := `SELECT id, LOWER(TRIM(email)) FROM users WHERE tenant_id = ? AND id IN (` + strings.Join(placeholders, ",") + `)`
		rows, err := r.readDB.QueryContext(ctx, query, args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var uid, email string
			if err := rows.Scan(&uid, &email); err != nil {
				continue
			}
			if strings.TrimSpace(email) != "" {
				result[uid] = preferredUsageDisplayAccount(result[uid], email)
			}
		}
		rows.Close()
	}
	for i := 0; i < len(userIDs); i += batchSize {
		end := i + batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		batch := userIDs[i:end]
		placeholders := make([]string, len(batch))
		args := make([]any, 0, len(batch)+1)
		args = append(args, tenantID)
		for j, uid := range batch {
			placeholders[j] = "?"
			args = append(args, uid)
		}
		query := `SELECT user_id, LOWER(TRIM(value)) FROM user_identities WHERE tenant_id = ? AND type = 'email' AND user_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY user_id, verified DESC, updated_at DESC`
		rows, err := r.readDB.QueryContext(ctx, query, args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var uid, email string
			if err := rows.Scan(&uid, &email); err != nil {
				continue
			}
			if strings.TrimSpace(email) != "" {
				result[uid] = strings.ToLower(strings.TrimSpace(email))
			}
		}
		rows.Close()
	}
	return result
}

// Legacy helpers — kept for backward compatibility with older data.
type usageDurationInterval struct {
	start time.Time
	end   time.Time
}

func isActiveUsageDurationSession(status string, hostOnline bool) bool {
	if !hostOnline {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "stopped", "finished", "failed", "killed", "exited", "closed", "done", "error", "completed", "terminated":
		return false
	default:
		return true
	}
}

func mergedUsageDurationSeconds(intervals []usageDurationInterval) int64 {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start.Equal(intervals[j].start) {
			return intervals[i].end.Before(intervals[j].end)
		}
		return intervals[i].start.Before(intervals[j].start)
	})
	var total int64
	currentStart := intervals[0].start
	currentEnd := intervals[0].end
	for _, interval := range intervals[1:] {
		if interval.end.Before(interval.start) || interval.end.Equal(interval.start) {
			continue
		}
		if interval.start.After(currentEnd) {
			total += int64(currentEnd.Sub(currentStart).Seconds())
			currentStart = interval.start
			currentEnd = interval.end
			continue
		}
		if interval.end.After(currentEnd) {
			currentEnd = interval.end
		}
	}
	if currentEnd.After(currentStart) {
		total += int64(currentEnd.Sub(currentStart).Seconds())
	}
	return total
}

func (r *invitationCodeRepo) Create(ctx context.Context, item *store.InvitationCode) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO invitation_codes (id, tenant_id, code, status, used_by_email, used_at, validity_days, vip, llm_service_group_id, llm_grant_duration_days, llm_grant_credits, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		normalizeTenantID(item.TenantID),
		item.Code,
		item.Status,
		item.UsedByEmail,
		nil,
		item.ValidityDays,
		boolToInt(item.VIP),
		item.LLMServiceGroupID,
		item.LLMGrantDurationDays,
		item.LLMGrantCredits,
		item.CreatedAt.Format(time.RFC3339),
	)
	return err
}

const invitationCodeSelectColumns = `id, tenant_id, code, status, used_by_email, used_at, validity_days, exported, vip, llm_service_group_id, llm_grant_duration_days, llm_grant_credits, created_at`

func (r *invitationCodeRepo) GetByID(ctx context.Context, id string) (*store.InvitationCode, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT `+invitationCodeSelectColumns+`
		 FROM invitation_codes WHERE id = ?`,
		id,
	)
	var item store.InvitationCode
	var usedAt sql.NullString
	var createdAt string
	var exported, vip int
	if err := row.Scan(&item.ID, &item.TenantID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &exported, &vip, &item.LLMServiceGroupID, &item.LLMGrantDurationDays, &item.LLMGrantCredits, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if usedAt.Valid {
		t := mustParseTime(usedAt.String)
		item.UsedAt = &t
	}
	item.Exported = exported != 0
	item.VIP = vip != 0
	item.CreatedAt = mustParseTime(createdAt)
	return &item, nil
}

func (r *invitationCodeRepo) GetByCode(ctx context.Context, code string) (*store.InvitationCode, error) {
	return r.getByCode(ctx, "", code)
}

func (r *invitationCodeRepo) GetByTenantCode(ctx context.Context, tenantID, code string) (*store.InvitationCode, error) {
	return r.getByCode(ctx, normalizeTenantID(tenantID), code)
}

func (r *invitationCodeRepo) getByCode(ctx context.Context, tenantID, code string) (*store.InvitationCode, error) {
	where := `code = ?`
	args := []any{code}
	if tenantID != "" {
		where = `tenant_id = ? AND code = ?`
		args = []any{tenantID, code}
	}
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT `+invitationCodeSelectColumns+`
		 FROM invitation_codes WHERE `+where,
		args...,
	)
	var item store.InvitationCode
	var usedAt sql.NullString
	var createdAt string
	var exported, vip int
	if err := row.Scan(&item.ID, &item.TenantID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &exported, &vip, &item.LLMServiceGroupID, &item.LLMGrantDurationDays, &item.LLMGrantCredits, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if usedAt.Valid {
		t := mustParseTime(usedAt.String)
		item.UsedAt = &t
	}
	item.Exported = exported != 0
	item.VIP = vip != 0
	item.CreatedAt = mustParseTime(createdAt)
	return &item, nil
}

func (r *invitationCodeRepo) List(ctx context.Context, status string, search string) ([]*store.InvitationCode, error) {
	query := `SELECT ` + invitationCodeSelectColumns + ` FROM invitation_codes`
	var conditions []string
	var args []any

	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if search != "" {
		conditions = append(conditions, "(code LIKE ? OR used_by_email LIKE ?)")
		args = append(args, "%"+search+"%", "%"+search+"%")
	}

	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for _, c := range conditions[1:] {
			query += " AND " + c
		}
	}
	query += " ORDER BY created_at DESC"

	rows, err := r.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.InvitationCode
	for rows.Next() {
		var item store.InvitationCode
		var usedAt sql.NullString
		var createdAt string
		var exported, vip int
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &exported, &vip, &item.LLMServiceGroupID, &item.LLMGrantDurationDays, &item.LLMGrantCredits, &createdAt); err != nil {
			return nil, err
		}
		if usedAt.Valid {
			t := mustParseTime(usedAt.String)
			item.UsedAt = &t
		}
		item.Exported = exported != 0
		item.VIP = vip != 0
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *invitationCodeRepo) ListPaged(ctx context.Context, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	return r.listPaged(ctx, "", status, search, offset, limit)
}

func (r *invitationCodeRepo) ListPagedByTenant(ctx context.Context, tenantID string, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	return r.listPaged(ctx, normalizeTenantID(tenantID), status, search, offset, limit)
}

func (r *invitationCodeRepo) listPaged(ctx context.Context, tenantID string, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	baseWhere := ""
	var conditions []string
	var args []any
	if tenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, tenantID)
	}

	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if search != "" {
		conditions = append(conditions, "(code LIKE ? OR used_by_email LIKE ?)")
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	if len(conditions) > 0 {
		baseWhere = " WHERE " + conditions[0]
		for _, c := range conditions[1:] {
			baseWhere += " AND " + c
		}
	}

	// Count total
	var total int
	countQuery := `SELECT COUNT(*) FROM invitation_codes` + baseWhere
	if err := r.readDB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Fetch page
	query := `SELECT ` + invitationCodeSelectColumns + ` FROM invitation_codes` + baseWhere + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.readDB.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []*store.InvitationCode
	for rows.Next() {
		var item store.InvitationCode
		var usedAt sql.NullString
		var createdAt string
		var exported, vip int
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &exported, &vip, &item.LLMServiceGroupID, &item.LLMGrantDurationDays, &item.LLMGrantCredits, &createdAt); err != nil {
			return nil, 0, err
		}
		if usedAt.Valid {
			t := mustParseTime(usedAt.String)
			item.UsedAt = &t
		}
		item.Exported = exported != 0
		item.VIP = vip != 0
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (r *invitationCodeRepo) MarkUsed(ctx context.Context, id string, email string, usedAt time.Time) error {
	res, err := r.db.ExecContext(
		ctx,
		`UPDATE invitation_codes SET status = 'used', used_by_email = ?, used_at = ? WHERE id = ? AND status = 'unused'`,
		email,
		usedAt.Format(time.RFC3339),
		id,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *invitationCodeRepo) Unbind(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE invitation_codes SET status = 'unused', used_by_email = '', used_at = NULL, exported = 0 WHERE id = ?`,
		id,
	)
	return err
}

func (r *invitationCodeRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM invitation_codes WHERE id = ?`, id)
	return err
}

func (r *invitationCodeRepo) DeleteByEmail(ctx context.Context, email string) (int64, error) {
	res, err := r.db.ExecContext(
		ctx,
		`DELETE FROM invitation_codes WHERE used_by_email = ? AND status = 'used'`,
		email,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *invitationCodeRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) (int64, error) {
	res, err := r.db.ExecContext(
		ctx,
		`DELETE FROM invitation_codes WHERE tenant_id = ? AND used_by_email = ? AND status = 'used'`,
		normalizeTenantID(tenantID),
		email,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *invitationCodeRepo) ListUnused(ctx context.Context, exportedFilter string, vipOnly ...bool) ([]*store.InvitationCode, error) {
	return r.listUnused(ctx, "", exportedFilter, vipOnly...)
}

func (r *invitationCodeRepo) ListUnusedByTenant(ctx context.Context, tenantID, exportedFilter string, vipOnly ...bool) ([]*store.InvitationCode, error) {
	return r.listUnused(ctx, normalizeTenantID(tenantID), exportedFilter, vipOnly...)
}

func (r *invitationCodeRepo) listUnused(ctx context.Context, tenantID, exportedFilter string, vipOnly ...bool) ([]*store.InvitationCode, error) {
	query := `SELECT ` + invitationCodeSelectColumns + ` FROM invitation_codes WHERE status = 'unused'`
	args := []any{}
	if tenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	switch exportedFilter {
	case "exported":
		query += ` AND exported = 1`
	case "all":
		// no additional filter
	default:
		// "unexported" or any unknown value defaults to unexported
		query += ` AND exported = 0`
	}
	if len(vipOnly) > 0 && vipOnly[0] {
		query += ` AND vip = 1`
	}
	query += ` ORDER BY created_at DESC`

	rows, err := r.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.InvitationCode
	for rows.Next() {
		var item store.InvitationCode
		var usedAt sql.NullString
		var createdAt string
		var exported, vip int
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &exported, &vip, &item.LLMServiceGroupID, &item.LLMGrantDurationDays, &item.LLMGrantCredits, &createdAt); err != nil {
			return nil, err
		}
		if usedAt.Valid {
			t := mustParseTime(usedAt.String)
			item.UsedAt = &t
		}
		item.Exported = exported != 0
		item.VIP = vip != 0
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *invitationCodeRepo) MarkExported(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `UPDATE invitation_codes SET exported = 1 WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *invitationCodeRepo) GetByEmail(ctx context.Context, email string) (*store.InvitationCode, error) {
	return r.getByEmail(ctx, "", email)
}

func (r *invitationCodeRepo) GetByTenantEmail(ctx context.Context, tenantID, email string) (*store.InvitationCode, error) {
	return r.getByEmail(ctx, normalizeTenantID(tenantID), email)
}

func (r *invitationCodeRepo) getByEmail(ctx context.Context, tenantID, email string) (*store.InvitationCode, error) {
	where := `used_by_email = ? AND status = 'used'`
	args := []any{email}
	if tenantID != "" {
		where = `tenant_id = ? AND used_by_email = ? AND status = 'used'`
		args = []any{tenantID, email}
	}
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT `+invitationCodeSelectColumns+`
		 FROM invitation_codes WHERE `+where+`
		 ORDER BY used_at DESC LIMIT 1`,
		args...,
	)
	var item store.InvitationCode
	var usedAt sql.NullString
	var createdAt string
	var exported, vip int
	if err := row.Scan(&item.ID, &item.TenantID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &exported, &vip, &item.LLMServiceGroupID, &item.LLMGrantDurationDays, &item.LLMGrantCredits, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if usedAt.Valid {
		t := mustParseTime(usedAt.String)
		item.UsedAt = &t
	}
	item.Exported = exported != 0
	item.VIP = vip != 0
	item.CreatedAt = mustParseTime(createdAt)
	return &item, nil
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

func (r *failureEventLogRepo) Create(ctx context.Context, log *store.FailureEventLog) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO failure_event_logs (id, tenant_id, category, event_code, message, entity_id, email, client_ip, details_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID,
		normalizeTenantID(log.TenantID),
		log.Category,
		log.EventCode,
		log.Message,
		log.EntityID,
		log.Email,
		log.ClientIP,
		log.DetailsJSON,
		log.CreatedAt.Format(time.RFC3339),
	)
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
	if filter.TenantScoped {
		where = append(where, "tenant_id = ?")
		args = append(args, normalizeTenantID(filter.TenantID))
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

func (r *knowledgeShareRepo) List(ctx context.Context, filter store.KnowledgeShareFilter) ([]*store.KnowledgeShare, int, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	where := make([]string, 0, 6)
	args := make([]any, 0, 10)
	if filter.TenantScoped {
		where = append(where, "tenant_id = ?")
		args = append(args, normalizeTenantID(filter.TenantID))
	} else if tenantID := strings.TrimSpace(filter.TenantID); tenantID != "" {
		where = append(where, "tenant_id = ?")
		args = append(args, normalizeTenantID(tenantID))
	}
	if !filter.IncludeDeleted {
		where = append(where, "status <> 'deleted'")
	}
	ownerUserID := strings.TrimSpace(filter.OwnerUserID)
	ownerUserEmail := strings.TrimSpace(filter.OwnerUserEmail)
	if ownerUserID != "" && ownerUserEmail != "" {
		where = append(where, "(owner_user_id = ? OR LOWER(owner_user_email) = LOWER(?))")
		args = append(args, ownerUserID, ownerUserEmail)
	} else if ownerUserID != "" {
		where = append(where, "owner_user_id = ?")
		args = append(args, ownerUserID)
	} else if ownerUserEmail != "" {
		where = append(where, "LOWER(owner_user_email) = LOWER(?)")
		args = append(args, ownerUserEmail)
	}
	if user := strings.TrimSpace(filter.User); user != "" {
		like := "%" + user + "%"
		where = append(where, "(owner_user_id = ? OR owner_user_email LIKE ?)")
		args = append(args, user, like)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		where = append(where, "(title LIKE ? OR description LIKE ?)")
		args = append(args, like, like)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	orderBy := "published_at DESC, created_at DESC"
	switch strings.TrimSpace(filter.Sort) {
	case "updated_at_desc":
		orderBy = "updated_at DESC, published_at DESC"
	case "view_count_desc":
		orderBy = "view_count DESC, published_at DESC"
	case "import_count_desc":
		orderBy = "import_count DESC, published_at DESC"
	case "published_at_asc":
		orderBy = "published_at ASC, created_at ASC"
	}
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_shares`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx, `SELECT knowledge_id, tenant_id, owner_user_id, owner_user_email, title, description, visibility_scope, visibility_users_json, source_summary_json, share_url, hub_id, storage_ref, status, view_count, import_count, created_at, updated_at, published_at, expires_at, forced_deleted_by, forced_deleted_reason, forced_deleted_at FROM knowledge_shares`+whereSQL+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*store.KnowledgeShare, 0, limit)
	for rows.Next() {
		item, err := scanKnowledgeShare(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *knowledgeShareRepo) Create(ctx context.Context, share *store.KnowledgeShare) error {
	if share == nil {
		return errors.New("knowledge share is nil")
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO knowledge_shares (knowledge_id, tenant_id, owner_user_id, owner_user_email, title, description, visibility_scope, visibility_users_json, source_summary_json, share_url, hub_id, storage_ref, status, view_count, import_count, created_at, updated_at, published_at, expires_at, forced_deleted_by, forced_deleted_reason, forced_deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', NULL)`,
		strings.TrimSpace(share.KnowledgeID),
		normalizeTenantID(share.TenantID),
		strings.TrimSpace(share.OwnerUserID),
		strings.TrimSpace(share.OwnerUserEmail),
		strings.TrimSpace(share.Title),
		strings.TrimSpace(share.Description),
		strings.TrimSpace(share.VisibilityScope),
		strings.TrimSpace(share.VisibilityUsersJSON),
		strings.TrimSpace(share.SourceSummaryJSON),
		strings.TrimSpace(share.ShareURL),
		strings.TrimSpace(share.HubID),
		strings.TrimSpace(share.StorageRef),
		strings.TrimSpace(share.Status),
		share.ViewCount,
		share.ImportCount,
		share.CreatedAt.Format(time.RFC3339),
		share.UpdatedAt.Format(time.RFC3339),
		share.PublishedAt.Format(time.RFC3339),
		nullableTimeString(share.ExpiresAt),
	)
}

func (r *knowledgeShareRepo) Get(ctx context.Context, knowledgeID string) (*store.KnowledgeShare, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT knowledge_id, tenant_id, owner_user_id, owner_user_email, title, description, visibility_scope, visibility_users_json, source_summary_json, share_url, hub_id, storage_ref, status, view_count, import_count, created_at, updated_at, published_at, expires_at, forced_deleted_by, forced_deleted_reason, forced_deleted_at FROM knowledge_shares WHERE knowledge_id = ?`, strings.TrimSpace(knowledgeID))
	item, err := scanKnowledgeShare(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

func (r *knowledgeShareRepo) UpdateOwner(ctx context.Context, share *store.KnowledgeShare) error {
	if share == nil {
		return errors.New("knowledge share is nil")
	}
	return execWrite(ctx, r.batch, r.db, `UPDATE knowledge_shares SET title = ?, description = ?, visibility_scope = ?, visibility_users_json = ?, source_summary_json = ?, share_url = ?, hub_id = ?, storage_ref = ?, expires_at = ?, updated_at = ? WHERE knowledge_id = ? AND tenant_id = ? AND owner_user_id = ? AND status <> 'deleted'`,
		strings.TrimSpace(share.Title),
		strings.TrimSpace(share.Description),
		strings.TrimSpace(share.VisibilityScope),
		strings.TrimSpace(share.VisibilityUsersJSON),
		strings.TrimSpace(share.SourceSummaryJSON),
		strings.TrimSpace(share.ShareURL),
		strings.TrimSpace(share.HubID),
		strings.TrimSpace(share.StorageRef),
		nullableTimeString(share.ExpiresAt),
		share.UpdatedAt.Format(time.RFC3339),
		strings.TrimSpace(share.KnowledgeID),
		normalizeTenantID(share.TenantID),
		strings.TrimSpace(share.OwnerUserID),
	)
}

func (r *knowledgeShareRepo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	query := `UPDATE knowledge_shares SET status = 'deleted', updated_at = ? WHERE status <> 'deleted' AND expires_at IS NOT NULL AND expires_at <> '' AND expires_at <= ?`
	args := []any{
		now.Format(time.RFC3339),
		now.Format(time.RFC3339),
	}
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *knowledgeShareRepo) DeleteOwner(ctx context.Context, knowledgeID, tenantID, ownerUserID string, deletedAt time.Time) error {
	return execWrite(ctx, r.batch, r.db, `UPDATE knowledge_shares SET status = 'deleted', updated_at = ? WHERE knowledge_id = ? AND tenant_id = ? AND owner_user_id = ? AND status <> 'deleted'`,
		deletedAt.Format(time.RFC3339),
		strings.TrimSpace(knowledgeID),
		normalizeTenantID(tenantID),
		strings.TrimSpace(ownerUserID),
	)
}

func (r *knowledgeShareRepo) ForceDelete(ctx context.Context, req store.KnowledgeShareForceDeleteRequest) error {
	return execWrite(ctx, r.batch, r.db, `UPDATE knowledge_shares SET status = 'deleted', forced_deleted_by = ?, forced_deleted_reason = ?, forced_deleted_at = ?, updated_at = ? WHERE knowledge_id = ?`,
		strings.TrimSpace(req.AdminUserID),
		strings.TrimSpace(req.Reason),
		req.DeletedAt.Format(time.RFC3339),
		req.DeletedAt.Format(time.RFC3339),
		strings.TrimSpace(req.KnowledgeID),
	)
}

func (r *knowledgeShareRepo) IncrementCounters(ctx context.Context, knowledgeID string, viewDelta, importDelta int64, at time.Time) error {
	if viewDelta == 0 && importDelta == 0 {
		return nil
	}
	return execWrite(ctx, r.batch, r.db, `UPDATE knowledge_shares SET view_count = view_count + ?, import_count = import_count + ?, updated_at = ? WHERE knowledge_id = ? AND status <> 'deleted'`,
		viewDelta,
		importDelta,
		at.Format(time.RFC3339),
		strings.TrimSpace(knowledgeID),
	)
}

type knowledgeShareScanner interface {
	Scan(dest ...any) error
}

func scanKnowledgeShare(row knowledgeShareScanner) (*store.KnowledgeShare, error) {
	var item store.KnowledgeShare
	var createdAt, updatedAt, publishedAt string
	var expiresAt, forcedDeletedAt sql.NullString
	if err := row.Scan(
		&item.KnowledgeID,
		&item.TenantID,
		&item.OwnerUserID,
		&item.OwnerUserEmail,
		&item.Title,
		&item.Description,
		&item.VisibilityScope,
		&item.VisibilityUsersJSON,
		&item.SourceSummaryJSON,
		&item.ShareURL,
		&item.HubID,
		&item.StorageRef,
		&item.Status,
		&item.ViewCount,
		&item.ImportCount,
		&createdAt,
		&updatedAt,
		&publishedAt,
		&expiresAt,
		&item.ForcedDeletedBy,
		&item.ForcedDeletedReason,
		&forcedDeletedAt,
	); err != nil {
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	item.PublishedAt = mustParseTime(publishedAt)
	if expiresAt.Valid && strings.TrimSpace(expiresAt.String) != "" {
		t := mustParseTime(expiresAt.String)
		item.ExpiresAt = &t
	}
	if forcedDeletedAt.Valid && strings.TrimSpace(forcedDeletedAt.String) != "" {
		t := mustParseTime(forcedDeletedAt.String)
		item.ForcedDeletedAt = &t
	}
	return &item, nil
}
