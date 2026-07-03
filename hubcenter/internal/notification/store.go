package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SQLiteStore implements the Store interface using SQLite for HubCenter-level
// notifications and their cascade push results.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new notification SQLiteStore backed by the given database.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// Ensure SQLiteStore implements Store.
var _ Store = (*SQLiteStore)(nil)

// InitSchema creates the hc_notifications and hc_cascade_results tables
// along with their indexes if they do not already exist.
func (s *SQLiteStore) InitSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS hc_notifications (
			id              TEXT PRIMARY KEY,
			title           TEXT NOT NULL,
			content         TEXT NOT NULL,
			category        TEXT NOT NULL DEFAULT 'system_announcement',
			priority        TEXT NOT NULL DEFAULT 'normal',
			audience_type   TEXT NOT NULL,
			audience_ids    TEXT NOT NULL DEFAULT '[]',
			status          TEXT NOT NULL DEFAULT 'draft',
			im_push         INTEGER NOT NULL DEFAULT 0,
			created_by      TEXT NOT NULL DEFAULT '',
			publish_at      TEXT,
			expire_at       TEXT,
			created_at      TEXT NOT NULL,
			updated_at      TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hc_notif_status ON hc_notifications(status, publish_at)`,
		`CREATE INDEX IF NOT EXISTS idx_hc_notif_category ON hc_notifications(category)`,
		`CREATE TABLE IF NOT EXISTS hc_cascade_results (
			notification_id TEXT NOT NULL,
			hub_id          TEXT NOT NULL,
			status          TEXT NOT NULL DEFAULT 'pending',
			error_message   TEXT NOT NULL DEFAULT '',
			pushed_at       TEXT,
			PRIMARY KEY (notification_id, hub_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hc_cascade_notif ON hc_cascade_results(notification_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init hc notification schema: %w", err)
		}
	}
	return nil
}

// Create inserts a new HubCenter notification into the database.
func (s *SQLiteStore) Create(ctx context.Context, n *Notification) error {
	args, err := notificationSQLArgs(n)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO hc_notifications
				(id, title, content, category, priority, audience_type, audience_ids, status, im_push, created_by, publish_at, expire_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		args...,
	)
	return err
}

// Upsert inserts or replaces a HubCenter notification without triggering
// cascade side effects. It is used by HA apply paths.
func (s *SQLiteStore) Upsert(ctx context.Context, n *Notification) error {
	args, err := notificationSQLArgs(n)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO hc_notifications
			(id, title, content, category, priority, audience_type, audience_ids, status, im_push, created_by, publish_at, expire_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			content = excluded.content,
			category = excluded.category,
			priority = excluded.priority,
			audience_type = excluded.audience_type,
			audience_ids = excluded.audience_ids,
			status = excluded.status,
			im_push = excluded.im_push,
			created_by = excluded.created_by,
			publish_at = excluded.publish_at,
			expire_at = excluded.expire_at,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at`,
		args...,
	)
	return err
}

// GetByID retrieves a single notification by its ID.
func (s *SQLiteStore) GetByID(ctx context.Context, id string) (*Notification, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, category, priority, audience_type, audience_ids, status, im_push, created_by, publish_at, expire_at, created_at, updated_at
		 FROM hc_notifications WHERE id = ?`, id)
	return scanNotification(row)
}

// List retrieves notifications with optional status/category filtering and pagination.
func (s *SQLiteStore) List(ctx context.Context, filter ListFilter) ([]*Notification, int, error) {
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

	whereClause := ""
	if len(clauses) > 0 {
		whereClause = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM hc_notifications" + whereClause
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := "SELECT id, title, content, category, priority, audience_type, audience_ids, status, im_push, created_by, publish_at, expire_at, created_at, updated_at FROM hc_notifications" + whereClause
	query += " ORDER BY created_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []*Notification
	for rows.Next() {
		n, err := scanNotificationFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, n)
	}
	return results, total, rows.Err()
}

// UpdateStatus updates the status and updated_at of a notification.
func (s *SQLiteStore) UpdateStatus(ctx context.Context, id string, status Status, updatedAt time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE hc_notifications SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), updatedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes a HubCenter notification and its cascade results.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM hc_cascade_results WHERE notification_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM hc_notifications WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// RecordCascadeResult records or updates the push result for a specific Hub.
func (s *SQLiteStore) RecordCascadeResult(ctx context.Context, result *CascadeResult) error {
	var pushedAt interface{}
	if result.PushedAt != nil {
		pushedAt = result.PushedAt.UTC().Format(time.RFC3339)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO hc_cascade_results (notification_id, hub_id, status, error_message, pushed_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(notification_id, hub_id) DO UPDATE SET
			status = excluded.status,
			error_message = excluded.error_message,
			pushed_at = excluded.pushed_at`,
		result.NotificationID, result.HubID, string(result.Status), result.ErrorMessage, pushedAt)
	return err
}

// GetCascadeResults returns all cascade push results for a notification.
func (s *SQLiteStore) GetCascadeResults(ctx context.Context, notificationID string) ([]*CascadeResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT notification_id, hub_id, status, error_message, pushed_at
		 FROM hc_cascade_results WHERE notification_id = ?
		 ORDER BY hub_id`, notificationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*CascadeResult
	for rows.Next() {
		r, err := scanCascadeResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func scanNotification(row *sql.Row) (*Notification, error) {
	var (
		n                                Notification
		category, priority, audienceType string
		audienceJSON, status             string
		createdBy                        string
		imPush                           int
		publishAt, expireAt              sql.NullString
		createdAt, updatedAt             string
	)
	err := row.Scan(
		&n.ID, &n.Title, &n.Content,
		&category, &priority, &audienceType, &audienceJSON,
		&status, &imPush, &createdBy,
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
		audienceJSON, status             string
		createdBy                        string
		imPush                           int
		publishAt, expireAt              sql.NullString
		createdAt, updatedAt             string
	)
	err := rows.Scan(
		&n.ID, &n.Title, &n.Content,
		&category, &priority, &audienceType, &audienceJSON,
		&status, &imPush, &createdBy,
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

func scanCascadeResult(rows *sql.Rows) (*CascadeResult, error) {
	var (
		r        CascadeResult
		status   string
		pushedAt sql.NullString
	)
	err := rows.Scan(&r.NotificationID, &r.HubID, &status, &r.ErrorMessage, &pushedAt)
	if err != nil {
		return nil, err
	}
	r.Status = CascadeStatus(status)
	if pushedAt.Valid {
		t := mustParseTime(pushedAt.String)
		r.PushedAt = &t
	}
	return &r, nil
}

func mustParseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
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

func notificationSQLArgs(n *Notification) ([]interface{}, error) {
	audienceJSON, err := json.Marshal(n.AudienceIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal audience_ids: %w", err)
	}
	imPush := 0
	if n.IMPush {
		imPush = 1
	}
	return []interface{}{
		n.ID, n.Title, n.Content,
		string(n.Category), string(n.Priority), string(n.AudienceType),
		string(audienceJSON), string(n.Status), imPush, n.CreatedBy,
		formatNullableTime(n.PublishAt), formatNullableTime(n.ExpireAt),
		n.CreatedAt.UTC().Format(time.RFC3339), n.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}
