package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
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
	db, readDB *sql.DB
	batch      *writeBatcher
	coalesce   *WriteCoalescer
	heartbeatCleanupCounter atomic.Int32
}
type emailInviteRepo struct {
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
		WorkflowRepo:    &workflowRepo{db: p.Write, readDB: p.Read},
		LLMPromptCache:  &llmPromptCacheRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		KnowledgeShares: &knowledgeShareRepo{db: p.Write, readDB: p.Read, batch: p.batch},
	}
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
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		normalizeTenantID(user.TenantID),
		user.Email,
		user.SN,
		user.Status,
		user.EnrollmentStatus,
		user.CreatedAt.Format(time.RFC3339),
		user.UpdatedAt.Format(time.RFC3339),
	)
	return err
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
	return r.getByEmail(ctx, normalizeTenantID(tenantID), email)
}

func (r *userRepo) getByEmail(ctx context.Context, tenantID, email string) (*store.User, error) {
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
	res, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE tenant_id = ? AND lower(email) = lower(?)`, normalizeTenantID(tenantID), strings.TrimSpace(email))
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
		now, normalizeTenantID(tenantID), email)
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
		`SELECT id, tenant_id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
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
		`SELECT id, tenant_id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
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
	heartbeatSec := metadata.HeartbeatIntervalSec
	if heartbeatSec < 5 {
		heartbeatSec = 10
	}
	// Use write-coalescer: metadata updates arrive on every heartbeat (~every 10-60s
	// per machine). At 10K machines, that's 167-1000 writes/sec. The coalescer
	// deduplicates per machineID, only flushing the latest values every 5s.
	if r.coalesce != nil {
		r.coalesce.Set(
			"machine_meta:"+machineID,
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
		return nil
	}
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
		`SELECT id, tenant_id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
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
	machine.ClientID = clientID
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
		`SELECT id, tenant_id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
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
				INSERT INTO user_usage_daily (tenant_id, user_email, day, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(tenant_id, user_email, day) DO UPDATE SET
					input_tokens = user_usage_daily.input_tokens + excluded.input_tokens,
					output_tokens = user_usage_daily.output_tokens + excluded.output_tokens,
					cached_input_tokens = user_usage_daily.cached_input_tokens + excluded.cached_input_tokens,
					cache_write_tokens = user_usage_daily.cache_write_tokens + excluded.cache_write_tokens,
					updated_at = excluded.updated_at`,
				tenantID, email, day, delta.InputTokens, delta.OutputTokens, delta.CachedInputTokens, delta.CacheWriteTokens, observedAt.Format(time.RFC3339)); err != nil {
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
		SELECT user_email,
		       SUM(input_tokens),
		       SUM(output_tokens),
		       SUM(cached_input_tokens),
		       SUM(cache_write_tokens)
		  FROM user_usage_daily
		 WHERE tenant_id = ?
		   AND day >= ?
		   AND day <= ?
		 GROUP BY user_email`, tenantID, startDay, endDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []store.UserTokenSummary{}
	for rows.Next() {
		var item store.UserTokenSummary
		if err := rows.Scan(&item.UserEmail, &item.Usage.InputTokens, &item.Usage.OutputTokens, &item.Usage.CachedInputTokens, &item.Usage.CacheWriteTokens); err != nil {
			return nil, err
		}
		item.UserEmail = strings.ToLower(strings.TrimSpace(item.UserEmail))
		if item.UserEmail != "" {
			out = append(out, item)
		}
	}
	return out, rows.Err()
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

	// Step 1: Query heartbeat timestamps grouped by user_id.
	// Uses the (tenant_id, user_id, heartbeat_at) index for efficient scan.
	rows, err := r.readDB.QueryContext(ctx, `
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
	defer rows.Close()

	// Merge consecutive heartbeats where gap <= maxHeartbeatGap into intervals.
	// Each heartbeat represents at least baseHeartbeatDuration of online time
	// (the interval since the previous heartbeat tick, or the tick itself for
	// a single isolated point).
	const maxHeartbeatGap = 5 * time.Minute
	const baseHeartbeatDuration = 60 // seconds — assumed heartbeat interval

	type userState struct {
		intervalStart time.Time
		intervalEnd   time.Time
		totalSeconds  int64
	}
	byUserID := map[string]*userState{}

	for rows.Next() {
		var userID string
		var atRaw string
		if err := rows.Scan(&userID, &atRaw); err != nil {
			return nil, err
		}
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(atRaw))
		if err != nil {
			continue
		}

		state := byUserID[userID]
		if state == nil {
			state = &userState{intervalStart: at, intervalEnd: at}
			byUserID[userID] = state
			continue
		}

		gap := at.Sub(state.intervalEnd)
		if gap <= maxHeartbeatGap {
			// Extend current interval.
			state.intervalEnd = at
		} else {
			// Close current interval. For single-point intervals (start==end),
			// count baseHeartbeatDuration.
			dur := int64(state.intervalEnd.Sub(state.intervalStart).Seconds())
			if dur < int64(baseHeartbeatDuration) {
				dur = int64(baseHeartbeatDuration)
			}
			state.totalSeconds += dur
			state.intervalStart = at
			state.intervalEnd = at
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Close the last open interval for each user.
	for _, state := range byUserID {
		dur := int64(state.intervalEnd.Sub(state.intervalStart).Seconds())
		if dur < int64(baseHeartbeatDuration) {
			dur = int64(baseHeartbeatDuration)
		}
		state.totalSeconds += dur
	}

	// Step 2: Resolve user_id → email. Batch-load all relevant users once.
	userIDs := make([]string, 0, len(byUserID))
	for uid := range byUserID {
		userIDs = append(userIDs, uid)
	}
	emailByUserID := r.resolveUserEmails(ctx, tenantID, userIDs)

	// Step 3: Aggregate by email (multiple machines of same user → merge).
	byEmail := map[string]int64{}
	for userID, state := range byUserID {
		email := emailByUserID[userID]
		if email == "" {
			// Fallback: treat user_id as email if it looks like one.
			if strings.Contains(userID, "@") {
				email = strings.ToLower(strings.TrimSpace(userID))
			}
		}
		if email == "" {
			continue
		}
		byEmail[email] += state.totalSeconds
	}

	out := make([]store.UserDurationSummary, 0, len(byEmail))
	for email, seconds := range byEmail {
		if seconds <= 0 {
			continue
		}
		out = append(out, store.UserDurationSummary{UserEmail: email, DurationSeconds: seconds})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UserEmail < out[j].UserEmail
	})
	return out, nil
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
	where := make([]string, 0, 4)
	args := make([]any, 0, 8)
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
