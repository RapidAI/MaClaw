package workflow

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// PgAuditStore implements AuditStore backed by PostgreSQL.
// It provides append-only access to the audit_trail table.
// There are no Update or Delete methods — immutability is enforced by DB triggers.
type PgAuditStore struct {
	db *sql.DB
}

// NewPgAuditStore creates a new PgAuditStore using the given database connection.
func NewPgAuditStore(db *sql.DB) *PgAuditStore {
	return &PgAuditStore{db: db}
}

// Append writes a new audit entry. Entries cannot be modified or deleted.
// If the entry's ID is empty, a random ID is generated.
// The entry's Timestamp is normalized to UTC with millisecond precision.
func (s *PgAuditStore) Append(ctx context.Context, entry *AuditEntry) error {
	if entry.ID == "" {
		entry.ID = generatePgAuditID()
	}
	entry.Timestamp = NormalizeAuditTimestamp(entry.Timestamp)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_trail (id, tenant_id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		entry.ID,
		store.TenantIDFromContext(ctx),
		entry.InstanceID,
		entry.NodeID,
		entry.EventType,
		entry.ActorID,
		entry.Decision,
		entry.MatchedRule,
		entry.Rationale,
		entry.Details,
		entry.Timestamp,
	)
	return err
}

// QueryByInstance returns all entries for a workflow instance, chronologically.
// Returns the matching entries, total count, and any error.
// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
func (s *PgAuditStore) QueryByInstance(ctx context.Context, instanceID string, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	offset := pgAuditOffset(page, pageSize)

	total, err := s.countWhere(ctx, "tenant_id = $1 AND instance_id = $2", store.TenantIDFromContext(ctx), instanceID)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM audit_trail
		 WHERE tenant_id = $1 AND instance_id = $2
		 ORDER BY timestamp ASC
		 LIMIT $3 OFFSET $4`,
		store.TenantIDFromContext(ctx), instanceID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanPgAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// QueryByApprover returns entries where the given VE acted as approver.
// Returns the matching entries, total count, and any error.
// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
func (s *PgAuditStore) QueryByApprover(ctx context.Context, approverID string, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	offset := pgAuditOffset(page, pageSize)

	total, err := s.countWhere(ctx, "tenant_id = $1 AND actor_id = $2", store.TenantIDFromContext(ctx), approverID)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM audit_trail
		 WHERE tenant_id = $1 AND actor_id = $2
		 ORDER BY timestamp ASC
		 LIMIT $3 OFFSET $4`,
		store.TenantIDFromContext(ctx), approverID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanPgAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// QueryByTimeRange returns entries within a time window.
// Returns the matching entries, total count, and any error.
// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
func (s *PgAuditStore) QueryByTimeRange(ctx context.Context, start, end time.Time, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	offset := pgAuditOffset(page, pageSize)

	// Normalize to UTC millisecond precision for consistent comparison.
	startNorm := NormalizeAuditTimestamp(start)
	endNorm := NormalizeAuditTimestamp(end)

	total, err := s.countWhere(ctx, "tenant_id = $1 AND timestamp >= $2 AND timestamp <= $3", store.TenantIDFromContext(ctx), startNorm, endNorm)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM audit_trail
		 WHERE tenant_id = $1 AND timestamp >= $2 AND timestamp <= $3
		 ORDER BY timestamp ASC
		 LIMIT $4 OFFSET $5`,
		store.TenantIDFromContext(ctx), startNorm, endNorm, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanPgAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// QueryByDecision returns entries filtered by decision outcome.
// Returns the matching entries, total count, and any error.
// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
func (s *PgAuditStore) QueryByDecision(ctx context.Context, decision string, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	offset := pgAuditOffset(page, pageSize)

	total, err := s.countWhere(ctx, "tenant_id = $1 AND decision = $2", store.TenantIDFromContext(ctx), decision)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM audit_trail
		 WHERE tenant_id = $1 AND decision = $2
		 ORDER BY timestamp ASC
		 LIMIT $3 OFFSET $4`,
		store.TenantIDFromContext(ctx), decision, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanPgAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// countWhere returns the count of rows matching the given WHERE clause.
// The where clause must use PostgreSQL-style $N placeholders.
func (s *PgAuditStore) countWhere(ctx context.Context, where string, args ...any) (int, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM audit_trail WHERE %s`, where),
		args...,
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// scanPgAuditEntries scans all rows into a slice of AuditEntry.
// PostgreSQL TIMESTAMP(3) columns are scanned directly into time.Time
// with millisecond precision preserved. For compatibility with drivers
// that return timestamps as strings, a fallback parser is used.
func scanPgAuditEntries(rows *sql.Rows) ([]AuditEntry, error) {
	var entries []AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var ts pgTimestamp
		if err := rows.Scan(
			&entry.ID,
			&entry.TenantID,
			&entry.InstanceID,
			&entry.NodeID,
			&entry.EventType,
			&entry.ActorID,
			&entry.Decision,
			&entry.MatchedRule,
			&entry.Rationale,
			&entry.Details,
			&ts,
		); err != nil {
			return nil, err
		}
		// Ensure UTC and millisecond precision on read.
		entry.Timestamp = ts.Time.UTC().Truncate(time.Millisecond)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// pgTimestamp is a custom scanner that handles both native time.Time values
// (from PostgreSQL drivers) and string timestamps (fallback).
type pgTimestamp struct {
	Time time.Time
}

// Scan implements the sql.Scanner interface.
func (pt *pgTimestamp) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		pt.Time = v
		return nil
	case string:
		return pt.parseString(v)
	case []byte:
		return pt.parseString(string(v))
	case nil:
		pt.Time = time.Time{}
		return nil
	default:
		return fmt.Errorf("pgTimestamp: unsupported type %T", src)
	}
}

// parseString attempts to parse a timestamp string in common formats.
func (pt *pgTimestamp) parseString(s string) error {
	// Try ISO 8601 with milliseconds (PostgreSQL default output)
	formats := []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05.999Z",
		"2006-01-02T15:04:05.000-07:00",
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05.000 -0700 MST",
		"2006-01-02 15:04:05.999 -0700 MST",
		"2006-01-02T15:04:05Z",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			pt.Time = t
			return nil
		}
	}
	return fmt.Errorf("pgTimestamp: cannot parse %q", s)
}

// pgAuditOffset computes the SQL OFFSET from page and pageSize.
// Page is 0-indexed. Negative pages are treated as 0.
func pgAuditOffset(page, pageSize int) int {
	if page < 0 {
		page = 0
	}
	return page * pageSize
}

// generatePgAuditID creates a unique ID for an audit entry.
func generatePgAuditID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "audit_fallback_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "audit_" + hex.EncodeToString(b[:])
}
