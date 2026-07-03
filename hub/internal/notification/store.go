package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Store provides SQLite CRUD operations for admin notifications.
type Store struct {
	db *sql.DB
}

// NewStore creates a new notification Store backed by the given database.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// InitSchema creates the admin_notifications and admin_notification_reads
// tables along with their indexes if they do not already exist.
func (s *Store) InitSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS admin_notifications (
			id              TEXT PRIMARY KEY,
			title           TEXT NOT NULL,
			content         TEXT NOT NULL,
			category        TEXT NOT NULL DEFAULT 'system_announcement',
			priority        TEXT NOT NULL DEFAULT 'normal',
			audience_type   TEXT NOT NULL,
			audience_ids    TEXT NOT NULL DEFAULT '[]',
			status          TEXT NOT NULL DEFAULT 'draft',
			im_push         INTEGER NOT NULL DEFAULT 0,
			source          TEXT NOT NULL DEFAULT 'hub',
			source_id       TEXT NOT NULL DEFAULT '',
			created_by      TEXT NOT NULL DEFAULT '',
			publish_at      TEXT,
			expire_at       TEXT,
			created_at      TEXT NOT NULL,
			updated_at      TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_notif_status ON admin_notifications(status, publish_at)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_notif_source ON admin_notifications(source, source_id)`,
		`CREATE TABLE IF NOT EXISTS admin_notification_reads (
			notification_id TEXT NOT NULL,
			machine_id      TEXT NOT NULL,
			read_at         TEXT NOT NULL,
			PRIMARY KEY (notification_id, machine_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_notif_reads_machine ON admin_notification_reads(machine_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init notification schema: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Notification CRUD
// ---------------------------------------------------------------------------

// Create inserts a new notification into the database.
func (s *Store) Create(ctx context.Context, n *Notification) error {
	audienceJSON, err := json.Marshal(n.AudienceIDs)
	if err != nil {
		return fmt.Errorf("marshal audience_ids: %w", err)
	}

	imPush := 0
	if n.IMPush {
		imPush = 1
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO admin_notifications
			(id, title, content, category, priority, audience_type, audience_ids, status, im_push, source, source_id, created_by, publish_at, expire_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID,
		n.Title,
		n.Content,
		string(n.Category),
		string(n.Priority),
		string(n.AudienceType),
		string(audienceJSON),
		string(n.Status),
		imPush,
		n.Source,
		n.SourceID,
		n.CreatedBy,
		formatNullableTime(n.PublishAt),
		formatNullableTime(n.ExpireAt),
		n.CreatedAt.UTC().Format(time.RFC3339),
		n.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// GetByID retrieves a single notification by its ID.
func (s *Store) GetByID(ctx context.Context, id string) (*Notification, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, category, priority, audience_type, audience_ids, status, im_push, source, source_id, created_by, publish_at, expire_at, created_at, updated_at
		 FROM admin_notifications WHERE id = ?`, id)
	return scanNotification(row)
}

// List retrieves notifications with optional status/category filtering and pagination.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]*Notification, error) {
	var (
		clauses []string
		args    []interface{}
	)

	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.Category != "" {
		clauses = append(clauses, "category = ?")
		args = append(args, string(filter.Category))
	}

	query := "SELECT id, title, content, category, priority, audience_type, audience_ids, status, im_push, source, source_id, created_by, publish_at, expire_at, created_at, updated_at FROM admin_notifications"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*Notification
	for rows.Next() {
		n, err := scanNotificationFromRows(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, n)
	}
	return results, rows.Err()
}

// UpdateStatus updates the status and updated_at of a notification.
func (s *Store) UpdateStatus(ctx context.Context, id string, status Status) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE admin_notifications SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), now, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes a notification and its read receipts from the database.
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_notification_reads WHERE notification_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM admin_notifications WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// FindBySource queries for a notification with a given source and source_id.
// Returns nil, nil if not found.
func (s *Store) FindBySource(ctx context.Context, source, sourceID string) (*Notification, error) {
	if sourceID == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, category, priority, audience_type, audience_ids, status, im_push, source, source_id, created_by, publish_at, expire_at, created_at, updated_at
		 FROM admin_notifications WHERE source = ? AND source_id = ?`,
		source, sourceID)
	return scanNotification(row)
}

// UpdateFromCascade updates an existing notification with incoming cascade data.
func (s *Store) UpdateFromCascade(ctx context.Context, id string, incoming *Notification, now time.Time) error {
	audienceJSON, err := json.Marshal(incoming.AudienceIDs)
	if err != nil {
		audienceJSON = []byte("[]")
	}
	imPush := 0
	if incoming.IMPush {
		imPush = 1
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE admin_notifications SET title=?, content=?, category=?, priority=?, audience_type=?, audience_ids=?, im_push=?, expire_at=?, updated_at=? WHERE id=?`,
		incoming.Title,
		incoming.Content,
		string(incoming.Category),
		string(incoming.Priority),
		string(incoming.AudienceType),
		string(audienceJSON),
		imPush,
		formatNullableTime(incoming.ExpireAt),
		now.Format(time.RFC3339),
		id,
	)
	return err
}

// ExpirePublishedBefore marks all published notifications with expire_at <= t as expired.
func (s *Store) ExpirePublishedBefore(ctx context.Context, t time.Time) error {
	now := t.Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE admin_notifications SET status = ?, updated_at = ?
		 WHERE status = ? AND expire_at IS NOT NULL AND expire_at <= ?`,
		string(StatusExpired), now, string(StatusPublished), now)
	return err
}

// ---------------------------------------------------------------------------
// Read tracking
// ---------------------------------------------------------------------------

// MarkRead records that a machine has read a notification.
func (s *Store) MarkRead(ctx context.Context, machineID, notificationID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO admin_notification_reads (notification_id, machine_id, read_at) VALUES (?, ?, ?)`,
		notificationID, machineID, now)
	return err
}

// MarkAllRead marks all published, non-expired, unread notifications as read for a machine.
// Only marks notifications that the machine hasn't already read.
func (s *Store) MarkAllRead(ctx context.Context, machineID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	notifications, err := s.queryUnreadForMachine(ctx, machineID, now, 0)
	if err != nil {
		return err
	}
	if len(notifications) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO admin_notification_reads (notification_id, machine_id, read_at) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, n := range notifications {
		if _, err := stmt.ExecContext(ctx, n.ID, machineID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetUnreadForMachine returns up to 10 unread notifications targeted to the
// authenticated machine's user/tenant, excluding expired and revoked items.
func (s *Store) GetUnreadForMachine(ctx context.Context, machineID string) ([]*Notification, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	return s.queryUnreadForMachine(ctx, machineID, now, 10)
}

func (s *Store) queryUnreadForMachine(ctx context.Context, machineID, now string, limit int) ([]*Notification, error) {
	notifications, err := s.queryUnreadForMachineWithDepartments(ctx, machineID, now, limit)
	if err == nil {
		return notifications, nil
	}
	lowerErr := strings.ToLower(err.Error())
	if isMissingTableError(err) && strings.Contains(lowerErr, "machines") {
		return s.getUnreadForMachineLegacy(ctx, machineID, now, limit)
	}
	if isMissingTableError(err) && strings.Contains(lowerErr, "org_members") {
		return s.getUnreadForMachineWithoutDepartments(ctx, machineID, now, limit)
	}
	return nil, err
}

func (s *Store) queryUnreadForMachineWithDepartments(ctx context.Context, machineID, now string, limit int) ([]*Notification, error) {
	limitSQL := unreadLimitSQL(limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.title, n.content, n.category, n.priority, n.audience_type, n.audience_ids, n.status, n.im_push, n.source, n.source_id, n.created_by, n.publish_at, n.expire_at, n.created_at, n.updated_at
		 FROM admin_notifications n
		 JOIN machines m ON m.id = ?
		 LEFT JOIN users u ON u.id = m.user_id
		 LEFT JOIN admin_notification_reads r ON r.notification_id = n.id AND r.machine_id = ?
		 WHERE n.status = 'published'
		   AND (n.expire_at IS NULL OR n.expire_at > ?)
		   AND r.notification_id IS NULL
		   AND (
		     n.audience_type = 'all'
		     OR (n.audience_type = 'tenant' AND EXISTS (
		       SELECT 1 FROM json_each(n.audience_ids) a
		       WHERE a.value = COALESCE(NULLIF(u.tenant_id, ''), m.tenant_id)
		     ))
		     OR (n.audience_type = 'user' AND EXISTS (
		       SELECT 1 FROM json_each(n.audience_ids) a
		       WHERE a.value = m.user_id
		     ))
		     OR (n.audience_type = 'department' AND EXISTS (
		       SELECT 1 FROM org_members om
		       JOIN json_each(n.audience_ids) a ON a.value = om.department_id
		       WHERE om.user_id = m.user_id
		     ))
		   )
		 ORDER BY n.created_at DESC
		 `+limitSQL,
		machineID, machineID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotificationRows(rows)
}

func (s *Store) getUnreadForMachineLegacy(ctx context.Context, machineID, now string, limit int) ([]*Notification, error) {
	limitSQL := unreadLimitSQL(limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.title, n.content, n.category, n.priority, n.audience_type, n.audience_ids, n.status, n.im_push, n.source, n.source_id, n.created_by, n.publish_at, n.expire_at, n.created_at, n.updated_at
		 FROM admin_notifications n
		 LEFT JOIN admin_notification_reads r ON r.notification_id = n.id AND r.machine_id = ?
		 WHERE n.status = 'published'
		   AND (n.expire_at IS NULL OR n.expire_at > ?)
		   AND r.notification_id IS NULL
		 ORDER BY n.created_at DESC
		 `+limitSQL,
		machineID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotificationRows(rows)
}

func (s *Store) getUnreadForMachineWithoutDepartments(ctx context.Context, machineID, now string, limit int) ([]*Notification, error) {
	limitSQL := unreadLimitSQL(limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT n.id, n.title, n.content, n.category, n.priority, n.audience_type, n.audience_ids, n.status, n.im_push, n.source, n.source_id, n.created_by, n.publish_at, n.expire_at, n.created_at, n.updated_at
		 FROM admin_notifications n
		 JOIN machines m ON m.id = ?
		 LEFT JOIN users u ON u.id = m.user_id
		 LEFT JOIN admin_notification_reads r ON r.notification_id = n.id AND r.machine_id = ?
		 WHERE n.status = 'published'
		   AND (n.expire_at IS NULL OR n.expire_at > ?)
		   AND r.notification_id IS NULL
		   AND (
		     n.audience_type = 'all'
		     OR (n.audience_type = 'tenant' AND EXISTS (
		       SELECT 1 FROM json_each(n.audience_ids) a
		       WHERE a.value = COALESCE(NULLIF(u.tenant_id, ''), m.tenant_id)
		     ))
		     OR (n.audience_type = 'user' AND EXISTS (
		       SELECT 1 FROM json_each(n.audience_ids) a
		       WHERE a.value = m.user_id
		     ))
		   )
		 ORDER BY n.created_at DESC
		 `+limitSQL,
		machineID, machineID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNotificationRows(rows)
}

func unreadLimitSQL(limit int) string {
	if limit <= 0 {
		return ""
	}
	return fmt.Sprintf(" LIMIT %d", limit)
}

func scanNotificationRows(rows *sql.Rows) ([]*Notification, error) {
	var results []*Notification
	for rows.Next() {
		n, err := scanNotificationFromRows(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, n)
	}
	return results, rows.Err()
}

// GetReadStats returns delivery/read statistics for a notification.
// TotalPush is the count of users in the audience, ReadCount is how many
// machines have marked it read, and ReadRate is ReadCount/TotalPush.
func (s *Store) GetReadStats(ctx context.Context, notificationID string) (*ReadStats, error) {
	// Get the notification to resolve audience
	n, err := s.GetByID(ctx, notificationID)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, sql.ErrNoRows
	}

	totalPush, err := s.CountAudienceUsers(ctx, n)
	if err != nil {
		return nil, err
	}

	// Count reads
	var readCount int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM admin_notification_reads WHERE notification_id = ?`,
		notificationID).Scan(&readCount)
	if err != nil {
		return nil, err
	}

	var readRate float64
	if totalPush > 0 {
		readRate = float64(readCount) / float64(totalPush)
	}

	return &ReadStats{
		TotalPush: totalPush,
		ReadCount: readCount,
		ReadRate:  readRate,
	}, nil
}

// ---------------------------------------------------------------------------
// Audience resolution helpers
// ---------------------------------------------------------------------------

// AllActiveMachineIDs returns all machine IDs with status 'online' or all
// registered machines (the notification system sends to all, online ones
// receive immediately, offline ones pull on reconnect).
func (s *Store) AllActiveMachineIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM machines WHERE status != 'deleted'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

// CountAudienceUsers returns the number of non-deleted users in a notification's
// audience. Older tests and partial schemas may not have a users table yet; in
// that case it falls back to machine counts so notification behavior remains
// backward-compatible.
func (s *Store) CountAudienceUsers(ctx context.Context, n *Notification) (int, error) {
	if n == nil {
		return 0, nil
	}
	switch n.AudienceType {
	case AudienceAll:
		count, err := s.countUsersWhere(ctx, "", nil)
		if isMissingTableError(err) {
			ids, fallbackErr := s.AllActiveMachineIDs(ctx)
			if fallbackErr != nil {
				return 0, fallbackErr
			}
			return len(ids), nil
		}
		return count, err
	case AudienceTenant:
		if len(n.AudienceIDs) == 0 {
			return 0, nil
		}
		query, args := buildInQuery(`tenant_id IN (%s)`, n.AudienceIDs)
		count, err := s.countUsersWhere(ctx, query, args)
		if isMissingTableError(err) {
			ids, fallbackErr := s.MachineIDsByTenantIDs(ctx, n.AudienceIDs)
			if fallbackErr != nil {
				return 0, fallbackErr
			}
			return len(ids), nil
		}
		return count, err
	case AudienceDepartment:
		if len(n.AudienceIDs) == 0 {
			return 0, nil
		}
		query, args := buildInQuery(`id IN (SELECT user_id FROM org_members WHERE department_id IN (%s))`, n.AudienceIDs)
		count, err := s.countUsersWhere(ctx, query, args)
		if isMissingTableError(err) {
			if strings.Contains(strings.ToLower(err.Error()), "org_members") {
				return 0, nil
			}
			ids, fallbackErr := s.MachineIDsByDepartmentIDs(ctx, n.AudienceIDs)
			if fallbackErr != nil {
				return 0, fallbackErr
			}
			return len(ids), nil
		}
		return count, err
	case AudienceUser:
		if len(n.AudienceIDs) == 0 {
			return 0, nil
		}
		query, args := buildInQuery(`id IN (%s)`, n.AudienceIDs)
		count, err := s.countUsersWhere(ctx, query, args)
		if isMissingTableError(err) {
			ids, fallbackErr := s.MachineIDsByUserIDs(ctx, n.AudienceIDs)
			if fallbackErr != nil {
				return 0, fallbackErr
			}
			return len(ids), nil
		}
		return count, err
	default:
		return 0, fmt.Errorf("unknown audience type: %s", n.AudienceType)
	}
}

func (s *Store) countUsersWhere(ctx context.Context, where string, args []interface{}) (int, error) {
	query := `SELECT COUNT(*) FROM users WHERE status != 'deleted'`
	if strings.TrimSpace(where) != "" {
		query += " AND " + where
	}
	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// MachineIDsByTenantIDs returns machine IDs belonging to the given tenant IDs.
func (s *Store) MachineIDsByTenantIDs(ctx context.Context, tenantIDs []string) ([]string, error) {
	if len(tenantIDs) == 0 {
		return nil, nil
	}
	query, args := buildInQuery(
		`SELECT m.id FROM machines m
		 JOIN users u ON u.id = m.user_id
		 WHERE u.tenant_id IN (%s) AND m.status != 'deleted'`, tenantIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

// MachineIDsByDepartmentIDs returns machine IDs belonging to users in the
// given department IDs. Departments are resolved through the organization
// structure (org_members table if present, otherwise falls back to empty).
func (s *Store) MachineIDsByDepartmentIDs(ctx context.Context, departmentIDs []string) ([]string, error) {
	if len(departmentIDs) == 0 {
		return nil, nil
	}
	// org_members maps users to departments/groups in the organization tree.
	// If the table doesn't exist (schema not yet migrated), return empty.
	query, args := buildInQuery(
		`SELECT m.id FROM machines m
		 JOIN org_members om ON om.user_id = m.user_id
		 WHERE om.department_id IN (%s) AND m.status != 'deleted'`, departmentIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		// If org_members table doesn't exist, return empty gracefully
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

// MachineIDsByUserIDs returns machine IDs belonging to the given user IDs.
func (s *Store) MachineIDsByUserIDs(ctx context.Context, userIDs []string) ([]string, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	query, args := buildInQuery(
		`SELECT id FROM machines WHERE user_id IN (%s) AND status != 'deleted'`, userIDs)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStringColumn(rows)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func scanNotification(row *sql.Row) (*Notification, error) {
	var (
		n                                Notification
		category, priority, audienceType string
		audienceJSON, status, source     string
		sourceID, createdBy              string
		imPush                           int
		publishAt, expireAt              sql.NullString
		createdAt, updatedAt             string
	)
	err := row.Scan(
		&n.ID, &n.Title, &n.Content,
		&category, &priority, &audienceType, &audienceJSON,
		&status, &imPush, &source, &sourceID, &createdBy,
		&publishAt, &expireAt, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	n.Category = Category(category)
	n.Priority = Priority(priority)
	n.AudienceType = AudienceType(audienceType)
	n.Status = Status(status)
	n.IMPush = imPush != 0
	n.Source = source
	n.SourceID = sourceID
	n.CreatedBy = createdBy
	n.CreatedAt = mustParseTime(createdAt)
	n.UpdatedAt = mustParseTime(updatedAt)

	if publishAt.Valid {
		t := mustParseTime(publishAt.String)
		n.PublishAt = &t
	}
	if expireAt.Valid {
		t := mustParseTime(expireAt.String)
		n.ExpireAt = &t
	}

	if err := json.Unmarshal([]byte(audienceJSON), &n.AudienceIDs); err != nil {
		n.AudienceIDs = nil
	}

	return &n, nil
}

func scanNotificationFromRows(rows *sql.Rows) (*Notification, error) {
	var (
		n                                Notification
		category, priority, audienceType string
		audienceJSON, status, source     string
		sourceID, createdBy              string
		imPush                           int
		publishAt, expireAt              sql.NullString
		createdAt, updatedAt             string
	)
	err := rows.Scan(
		&n.ID, &n.Title, &n.Content,
		&category, &priority, &audienceType, &audienceJSON,
		&status, &imPush, &source, &sourceID, &createdBy,
		&publishAt, &expireAt, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	n.Category = Category(category)
	n.Priority = Priority(priority)
	n.AudienceType = AudienceType(audienceType)
	n.Status = Status(status)
	n.IMPush = imPush != 0
	n.Source = source
	n.SourceID = sourceID
	n.CreatedBy = createdBy
	n.CreatedAt = mustParseTime(createdAt)
	n.UpdatedAt = mustParseTime(updatedAt)

	if publishAt.Valid {
		t := mustParseTime(publishAt.String)
		n.PublishAt = &t
	}
	if expireAt.Valid {
		t := mustParseTime(expireAt.String)
		n.ExpireAt = &t
	}

	if err := json.Unmarshal([]byte(audienceJSON), &n.AudienceIDs); err != nil {
		n.AudienceIDs = nil
	}

	return &n, nil
}

func mustParseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		// Fallback: try ISO8601 without timezone
		t, err = time.Parse("2006-01-02T15:04:05", v)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

func formatNullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func scanStringColumn(rows *sql.Rows) ([]string, error) {
	var result []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func isMissingTableError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

// buildInQuery builds a query with a dynamically sized IN clause.
// The queryTemplate must contain exactly one %s placeholder for the IN list.
func buildInQuery(queryTemplate string, values []string) (string, []interface{}) {
	placeholders := make([]string, len(values))
	args := make([]interface{}, len(values))
	for i, v := range values {
		placeholders[i] = "?"
		args[i] = v
	}
	query := fmt.Sprintf(queryTemplate, strings.Join(placeholders, ","))
	return query, args
}
