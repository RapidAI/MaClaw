package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
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
}

func NewStore(p *Provider) *store.Store {
	return &store.Store{
		Admins:       &adminRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		System:       &systemRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		AdminAudit:   &adminAuditRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Users:        &userRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Enrollments:  &enrollmentRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		EmailBlocks:  &emailBlockRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		InvitationCodes: &invitationCodeRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Machines:        &machineRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		ViewerTokens: &viewerTokenRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		LoginTokens:  &loginTokenRepo{db: p.Write, readDB: p.Read, batch: p.batch},
		Sessions:     &sessionRepo{db: p.Write, readDB: p.Read, batch: p.batch},
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
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO admin_users (id, username, password_hash, email, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
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
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, username, password_hash, email, status, created_at, updated_at
		 FROM admin_users WHERE username = ?`,
		username,
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
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE admin_users SET password_hash = ?, updated_at = ? WHERE username = ?`,
		passwordHash,
		updatedAt.Format(time.RFC3339),
		username,
	)
	return err
}

func (r *adminRepo) UpdateEmail(ctx context.Context, username, email string, updatedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE admin_users SET email = ?, updated_at = ? WHERE username = ?`,
		email,
		updatedAt.Format(time.RFC3339),
		username,
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
		`INSERT INTO admin_audit_logs (id, admin_user_id, action, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		log.ID,
		log.AdminUserID,
		log.Action,
		log.PayloadJSON,
		log.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *userRepo) Create(ctx context.Context, user *store.User) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO users (id, email, sn, status, enrollment_status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
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
		`SELECT id, email, sn, status, enrollment_status, created_at, updated_at
		 FROM users WHERE id = ?`,
		id,
	)

	var (
		user                 store.User
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.SN,
		&user.Status,
		&user.EnrollmentStatus,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	user.CreatedAt = mustParseTime(createdAt)
	user.UpdatedAt = mustParseTime(updatedAt)
	return &user, nil
}

func (r *userRepo) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, email, sn, status, enrollment_status, created_at, updated_at
		 FROM users WHERE email = ?`,
		email,
	)

	var (
		user                 store.User
		createdAt, updatedAt string
	)
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.SN,
		&user.Status,
		&user.EnrollmentStatus,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	user.CreatedAt = mustParseTime(createdAt)
	user.UpdatedAt = mustParseTime(updatedAt)
	return &user, nil
}

func (r *userRepo) List(ctx context.Context) ([]*store.User, error) {
	rows, err := r.readDB.QueryContext(
		ctx,
		`SELECT id, email, sn, status, enrollment_status, created_at, updated_at
		 FROM users
		 ORDER BY updated_at DESC, email ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.User
	for rows.Next() {
		var (
			user                 store.User
			createdAt, updatedAt string
		)
		if err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.SN,
			&user.Status,
			&user.EnrollmentStatus,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		user.CreatedAt = mustParseTime(createdAt)
		user.UpdatedAt = mustParseTime(updatedAt)
		items = append(items, &user)
	}
	return items, rows.Err()
}

func (r *enrollmentRepo) Create(ctx context.Context, item *store.UserEnrollment) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO user_enrollments (id, email, mobile, status, note, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
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
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, email, mobile, status, note, created_at, updated_at
		 FROM user_enrollments WHERE email = ? AND status = 'pending'
		 ORDER BY created_at DESC LIMIT 1`,
		email,
	)
	var item store.UserEnrollment
	var createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.Email, &item.Mobile, &item.Status, &item.Note, &createdAt, &updatedAt); err != nil {
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
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, email, mobile, status, note, created_at, updated_at FROM user_enrollments WHERE status = 'pending' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.UserEnrollment
	for rows.Next() {
		var item store.UserEnrollment
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Email, &item.Mobile, &item.Status, &item.Note, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *enrollmentRepo) ListAll(ctx context.Context) ([]*store.UserEnrollment, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, email, mobile, status, note, created_at, updated_at FROM user_enrollments ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.UserEnrollment
	for rows.Next() {
		var item store.UserEnrollment
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Email, &item.Mobile, &item.Status, &item.Note, &createdAt, &updatedAt); err != nil {
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

func (r *emailBlockRepo) Create(ctx context.Context, item *store.EmailBlockItem) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO email_blocklist (id, email, reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(email) DO UPDATE SET reason = excluded.reason, updated_at = excluded.updated_at`,
		item.ID,
		item.Email,
		item.Reason,
		item.CreatedAt.Format(time.RFC3339),
		item.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *emailBlockRepo) DeleteByEmail(ctx context.Context, email string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM email_blocklist WHERE email = ?`, email)
	return err
}

func (r *emailBlockRepo) GetByEmail(ctx context.Context, email string) (*store.EmailBlockItem, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT id, email, reason, created_at, updated_at FROM email_blocklist WHERE email = ?`, email)
	var item store.EmailBlockItem
	var createdAt, updatedAt string
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

func (r *emailBlockRepo) List(ctx context.Context) ([]*store.EmailBlockItem, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, email, reason, created_at, updated_at FROM email_blocklist ORDER BY email ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.EmailBlockItem
	for rows.Next() {
		var item store.EmailBlockItem
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Email, &item.Reason, &createdAt, &updatedAt); err != nil {
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
		`INSERT INTO machines (id, user_id, client_id, name, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		machine.ID,
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
		`SELECT id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
		 FROM machines WHERE id = ?`,
		id,
	)

	var (
		machine                        store.Machine
		lastSeen, createdAt, updatedAt sql.NullString
	)
	if err := row.Scan(
		&machine.ID,
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
		`SELECT id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
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
		`SELECT id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
		 FROM machines WHERE user_id = ? AND client_id = ?`,
		userID, clientID,
	)

	var (
		machine                        store.Machine
		lastSeen, createdAt, updatedAt sql.NullString
	)
	if err := row.Scan(
		&machine.ID,
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
	rows, err := r.readDB.QueryContext(
		ctx,
		`SELECT id, user_id, name, alias, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at
		 FROM machines ORDER BY updated_at DESC`,
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

func (r *machineRepo) ForceDeleteByUserID(ctx context.Context, userID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM machines WHERE user_id = ?`, userID)
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

func (r *viewerTokenRepo) Create(ctx context.Context, token *store.ViewerToken) error {
	var revokedAt any
	if token.RevokedAt != nil {
		revokedAt = token.RevokedAt.Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO viewer_tokens (id, user_id, token_hash, expires_at, created_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		token.ID,
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
		`SELECT id, user_id, token_hash, expires_at, created_at, revoked_at
		 FROM viewer_tokens WHERE token_hash = ?`,
		tokenHash,
	)

	var (
		token                           store.ViewerToken
		expiresAt, createdAt, revokedAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
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

func (r *loginTokenRepo) Create(ctx context.Context, token *store.LoginToken) error {
	var consumedAt any
	if token.ConsumedAt != nil {
		consumedAt = token.ConsumedAt.Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO login_tokens (id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID,
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
		`SELECT id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens WHERE token_hash = ?`,
		tokenHash,
	)

	var (
		token                            store.LoginToken
		expiresAt, consumedAt, createdAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
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
		`SELECT id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens WHERE poll_token_hash = ?`,
		pollTokenHash,
	)

	var (
		token                            store.LoginToken
		expiresAt, consumedAt, createdAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
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
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens
		 WHERE email = ? AND consumed_at IS NULL AND expires_at > ?
		 ORDER BY created_at DESC LIMIT 1`,
		email,
		time.Now().Format(time.RFC3339),
	)

	var (
		token                            store.LoginToken
		expiresAt, consumedAt, createdAt sql.NullString
	)
	if err := row.Scan(
		&token.ID,
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
	rows, err := r.readDB.QueryContext(
		ctx,
		`SELECT id, email, token_hash, poll_token_hash, purpose, expires_at, consumed_at, created_at
		 FROM login_tokens
		 WHERE consumed_at IS NULL AND expires_at > ?
		 ORDER BY created_at DESC`,
		time.Now().Format(time.RFC3339),
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
		`INSERT INTO sessions (id, machine_id, user_id, tool, title, project_path, status, summary_json, preview_text, output_seq, host_online, started_at, updated_at, ended_at, exit_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
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
		`UPDATE sessions SET summary_json = ?, status = ?, updated_at = ? WHERE id = ?`,
		summaryJSON,
		status,
		updatedAt.Format(time.RFC3339),
		sessionID,
	)
}

func (r *sessionRepo) UpdatePreview(ctx context.Context, sessionID string, previewText string, outputSeq int64, updatedAt time.Time) error {
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE sessions SET preview_text = ?, output_seq = ?, updated_at = ? WHERE id = ?`,
		previewText,
		outputSeq,
		updatedAt.Format(time.RFC3339),
		sessionID,
	)
}

func (r *sessionRepo) UpdateHostOnline(ctx context.Context, sessionID string, hostOnline bool, updatedAt time.Time) error {
	return execWrite(
		ctx,
		r.batch,
		r.db,
		`UPDATE sessions SET host_online = ?, updated_at = ? WHERE id = ?`,
		boolToInt(hostOnline),
		updatedAt.Format(time.RFC3339),
		sessionID,
	)
}

func (r *sessionRepo) Close(ctx context.Context, sessionID string, exitCode *int, endedAt time.Time, status string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE sessions SET status = ?, ended_at = ?, exit_code = ?, updated_at = ? WHERE id = ?`,
		status,
		endedAt.Format(time.RFC3339),
		exitCode,
		endedAt.Format(time.RFC3339),
		sessionID,
	)
	return err
}

func (r *invitationCodeRepo) Create(ctx context.Context, item *store.InvitationCode) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO invitation_codes (id, code, status, used_by_email, used_at, validity_days, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		item.Code,
		item.Status,
		item.UsedByEmail,
		nil,
		item.ValidityDays,
		item.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *invitationCodeRepo) GetByID(ctx context.Context, id string) (*store.InvitationCode, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, code, status, used_by_email, used_at, validity_days, created_at
		 FROM invitation_codes WHERE id = ?`,
		id,
	)
	var item store.InvitationCode
	var usedAt sql.NullString
	var createdAt string
	if err := row.Scan(&item.ID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if usedAt.Valid {
		t := mustParseTime(usedAt.String)
		item.UsedAt = &t
	}
	item.CreatedAt = mustParseTime(createdAt)
	return &item, nil
}

func (r *invitationCodeRepo) GetByCode(ctx context.Context, code string) (*store.InvitationCode, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, code, status, used_by_email, used_at, validity_days, created_at
		 FROM invitation_codes WHERE code = ?`,
		code,
	)
	var item store.InvitationCode
	var usedAt sql.NullString
	var createdAt string
	if err := row.Scan(&item.ID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if usedAt.Valid {
		t := mustParseTime(usedAt.String)
		item.UsedAt = &t
	}
	item.CreatedAt = mustParseTime(createdAt)
	return &item, nil
}

func (r *invitationCodeRepo) List(ctx context.Context, status string, search string) ([]*store.InvitationCode, error) {
	query := `SELECT id, code, status, used_by_email, used_at, validity_days, created_at FROM invitation_codes`
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
		if err := rows.Scan(&item.ID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &createdAt); err != nil {
			return nil, err
		}
		if usedAt.Valid {
			t := mustParseTime(usedAt.String)
			item.UsedAt = &t
		}
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, rows.Err()
}

func (r *invitationCodeRepo) ListPaged(ctx context.Context, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	baseWhere := ""
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
	query := `SELECT id, code, status, used_by_email, used_at, validity_days, created_at FROM invitation_codes` + baseWhere + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
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
		if err := rows.Scan(&item.ID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &createdAt); err != nil {
			return nil, 0, err
		}
		if usedAt.Valid {
			t := mustParseTime(usedAt.String)
			item.UsedAt = &t
		}
		item.CreatedAt = mustParseTime(createdAt)
		items = append(items, &item)
	}
	return items, total, rows.Err()
}

func (r *invitationCodeRepo) MarkUsed(ctx context.Context, id string, email string, usedAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE invitation_codes SET status = 'used', used_by_email = ?, used_at = ? WHERE id = ?`,
		email,
		usedAt.Format(time.RFC3339),
		id,
	)
	return err
}

func (r *invitationCodeRepo) Unbind(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE invitation_codes SET status = 'unused', used_by_email = '', used_at = NULL WHERE id = ?`,
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

func (r *invitationCodeRepo) GetByEmail(ctx context.Context, email string) (*store.InvitationCode, error) {
	row := r.readDB.QueryRowContext(
		ctx,
		`SELECT id, code, status, used_by_email, used_at, validity_days, created_at
		 FROM invitation_codes WHERE used_by_email = ? AND status = 'used'
		 ORDER BY used_at DESC LIMIT 1`,
		email,
	)
	var item store.InvitationCode
	var usedAt sql.NullString
	var createdAt string
	if err := row.Scan(&item.ID, &item.Code, &item.Status, &item.UsedByEmail, &usedAt, &item.ValidityDays, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if usedAt.Valid {
		t := mustParseTime(usedAt.String)
		item.UsedAt = &t
	}
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
