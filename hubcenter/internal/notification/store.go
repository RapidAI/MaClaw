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
			pinned          INTEGER NOT NULL DEFAULT 0,
			pinned_forever  INTEGER NOT NULL DEFAULT 0,
			link_url        TEXT NOT NULL DEFAULT '',
			button_text     TEXT NOT NULL DEFAULT '',
			source_lang     TEXT NOT NULL DEFAULT '',
			locales_json    TEXT NOT NULL DEFAULT '',
			created_by      TEXT NOT NULL DEFAULT '',
			publish_at      TEXT,
			expire_at       TEXT,
			created_at      TEXT NOT NULL,
			updated_at      TEXT NOT NULL,
			dispatch_node   TEXT NOT NULL DEFAULT '',
			dispatch_until  TEXT
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
	// Additive upgrades keep databases created before pinned messages readable.
	// Duplicate-column errors on already-migrated schemas are ignored.
	for _, stmt := range []string{
		`ALTER TABLE hc_notifications ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE hc_notifications ADD COLUMN pinned_forever INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE hc_notifications ADD COLUMN link_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hc_notifications ADD COLUMN button_text TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hc_notifications ADD COLUMN source_lang TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hc_notifications ADD COLUMN locales_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hc_notifications ADD COLUMN dispatch_node TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE hc_notifications ADD COLUMN dispatch_until TEXT`,
	} {
		_, _ = s.db.ExecContext(ctx, stmt)
	}
	return nil
}

// Shared column lists so every read of hc_notifications carries the same
// fields (scanNotificationRow expects exactly this order).
const (
	hcNotificationColumns = `id, title, content, category, priority, audience_type, audience_ids, status, im_push, ` +
		`pinned, pinned_forever, link_url, button_text, source_lang, locales_json, ` +
		`created_by, publish_at, expire_at, created_at, updated_at, dispatch_node, dispatch_until`
)

// Create inserts a new HubCenter notification into the database.
func (s *SQLiteStore) Create(ctx context.Context, n *Notification) error {
	args, err := notificationSQLArgs(n)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO hc_notifications (`+hcNotificationColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		`INSERT INTO hc_notifications (`+hcNotificationColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			content = excluded.content,
			category = excluded.category,
			priority = excluded.priority,
			audience_type = excluded.audience_type,
			audience_ids = excluded.audience_ids,
			status = excluded.status,
			im_push = excluded.im_push,
			pinned = excluded.pinned,
			pinned_forever = excluded.pinned_forever,
			link_url = excluded.link_url,
			button_text = excluded.button_text,
			source_lang = excluded.source_lang,
			locales_json = excluded.locales_json,
			created_by = excluded.created_by,
			publish_at = excluded.publish_at,
			expire_at = excluded.expire_at,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at,
			dispatch_node = excluded.dispatch_node,
			dispatch_until = excluded.dispatch_until`,
		args...,
	)
	return err
}

// GetByID retrieves a single notification by its ID.
func (s *SQLiteStore) GetByID(ctx context.Context, id string) (*Notification, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+hcNotificationColumns+` FROM hc_notifications WHERE id = ?`, id)
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

	query := "SELECT " + hcNotificationColumns + " FROM hc_notifications" + whereClause
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

// ListDueDrafts returns drafts whose publish_at has arrived. publish_at is
// stored as UTC RFC3339, so the string comparison matches that format.
func (s *SQLiteStore) ListDueDrafts(ctx context.Context, now time.Time) ([]*Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+hcNotificationColumns+` FROM hc_notifications
		 WHERE status = ? AND publish_at IS NOT NULL AND publish_at != '' AND publish_at <= ?
		 ORDER BY publish_at ASC LIMIT 50`,
		string(StatusDraft), now.UTC().Format(time.RFC3339))
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

// ClaimDraft moves a draft to next. A second caller sees ok=false.
func (s *SQLiteStore) ClaimDraft(ctx context.Context, id string, next Status, updatedAt time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE hc_notifications SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		string(next), updatedAt.UTC().Format(time.RFC3339), id, string(StatusDraft))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ClaimDispatch fences a draft for node until the lease time. A second node
// loses while that lease is still current. The holder may renew it. until
// and updatedAt are stored as UTC RFC3339 so the comparison matches ListDueDrafts.
func (s *SQLiteStore) ClaimDispatch(ctx context.Context, id, node string, until, updatedAt time.Time) (bool, error) {
	now := updatedAt.UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE hc_notifications
		 SET dispatch_node = ?, dispatch_until = ?, updated_at = ?
		 WHERE id = ? AND status = ?
		   AND (dispatch_until IS NULL OR dispatch_until = '' OR dispatch_until <= ? OR dispatch_node = ?)`,
		node, until.UTC().Format(time.RFC3339), now,
		id, string(StatusDraft), now, node)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
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

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanNotificationRow scans one hc_notifications row in the
// hcNotificationColumns order and applies the parsed values to a Notification.
func scanNotificationRow(scan rowScanner) (*Notification, error) {
	var (
		n                                Notification
		category, priority, audienceType string
		audienceJSON, status             string
		createdBy                        string
		imPush                           int
		pinned, pinnedForever            int
		linkURL, buttonText              string
		sourceLang, localesJSON          string
		publishAt, expireAt              sql.NullString
		createdAt, updatedAt             string
		dispatchNode                     string
		dispatchUntil                    sql.NullString
	)
	err := scan.Scan(
		&n.ID, &n.Title, &n.Content,
		&category, &priority, &audienceType, &audienceJSON,
		&status, &imPush, &pinned, &pinnedForever, &linkURL, &buttonText,
		&sourceLang, &localesJSON,
		&createdBy,
		&publishAt, &expireAt, &createdAt, &updatedAt,
		&dispatchNode, &dispatchUntil,
	)
	if err != nil {
		return nil, err
	}
	n.Category = Category(category)
	n.Priority = Priority(priority)
	n.AudienceType = AudienceType(audienceType)
	n.Status = Status(status)
	n.IMPush = imPush != 0
	n.Pinned = pinned != 0
	n.PinnedForever = pinnedForever != 0
	n.LinkURL = linkURL
	n.ButtonText = buttonText
	n.SourceLang = sourceLang
	n.Locales = unmarshalLocales(localesJSON)
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
	n.DispatchNode = dispatchNode
	if dispatchUntil.Valid && dispatchUntil.String != "" {
		t := mustParseTime(dispatchUntil.String)
		if !t.IsZero() {
			n.DispatchUntil = &t
		}
	}
	if err := json.Unmarshal([]byte(audienceJSON), &n.AudienceIDs); err != nil {
		n.AudienceIDs = nil
	}
	return &n, nil
}

func scanNotification(row *sql.Row) (*Notification, error) {
	n, err := scanNotificationRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return n, nil
}

func scanNotificationFromRows(rows *sql.Rows) (*Notification, error) {
	return scanNotificationRow(rows)
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
	pinned := 0
	if n.Pinned {
		pinned = 1
	}
	pinnedForever := 0
	if n.PinnedForever {
		pinnedForever = 1
	}
	return []interface{}{
		n.ID, n.Title, n.Content,
		string(n.Category), string(n.Priority), string(n.AudienceType),
		string(audienceJSON), string(n.Status), imPush,
		pinned, pinnedForever, n.LinkURL, n.ButtonText,
		n.SourceLang, marshalLocales(n.Locales),
		n.CreatedBy,
		formatNullableTime(n.PublishAt), formatNullableTime(n.ExpireAt),
		n.CreatedAt.UTC().Format(time.RFC3339), n.UpdatedAt.UTC().Format(time.RFC3339),
		n.DispatchNode, formatNullableTime(n.DispatchUntil),
	}, nil
}
