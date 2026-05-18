package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// auditTimeFormat is the ISO 8601 format with millisecond precision used for
// storing timestamps in the approval_audit_trail table.
const auditTimeFormat = "2006-01-02T15:04:05.000Z"

// AuditStore implements workflow.AuditStore backed by SQLite.
// It provides append-only access to the approval_audit_trail table.
// There are no Update or Delete methods — immutability is enforced by DB triggers.
type AuditStore struct {
	db *sql.DB // write DB
}

// NewAuditStore creates a new AuditStore using the given write database connection.
func NewAuditStore(db *sql.DB) *AuditStore {
	return &AuditStore{db: db}
}

// Append writes a new audit entry. Entries cannot be modified or deleted.
// If the entry's ID is empty, a random ID is generated.
// The entry's Timestamp is normalized to UTC with millisecond precision.
func (s *AuditStore) Append(ctx context.Context, entry *workflow.AuditEntry) error {
	if entry.ID == "" {
		entry.ID = generateAuditID()
	}
	entry.Timestamp = workflow.NormalizeAuditTimestamp(entry.Timestamp)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO approval_audit_trail (id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID,
		entry.InstanceID,
		entry.NodeID,
		entry.EventType,
		entry.ActorID,
		entry.Decision,
		entry.MatchedRule,
		entry.Rationale,
		entry.Details,
		entry.Timestamp.Format(auditTimeFormat),
	)
	return err
}

// QueryByInstance returns all entries for a workflow instance, chronologically.
// Returns the matching entries, total count, and any error.
func (s *AuditStore) QueryByInstance(ctx context.Context, instanceID string, page, pageSize int) ([]workflow.AuditEntry, int, error) {
	pageSize = workflow.NormalizePageSize(pageSize)
	offset := normalizeOffset(page, pageSize)

	total, err := s.countWhere(ctx, "instance_id = ?", instanceID)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM approval_audit_trail
		 WHERE instance_id = ?
		 ORDER BY timestamp ASC
		 LIMIT ? OFFSET ?`,
		instanceID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// QueryByApprover returns entries where the given VE acted as approver.
// Returns the matching entries, total count, and any error.
func (s *AuditStore) QueryByApprover(ctx context.Context, approverID string, page, pageSize int) ([]workflow.AuditEntry, int, error) {
	pageSize = workflow.NormalizePageSize(pageSize)
	offset := normalizeOffset(page, pageSize)

	total, err := s.countWhere(ctx, "actor_id = ?", approverID)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM approval_audit_trail
		 WHERE actor_id = ?
		 ORDER BY timestamp ASC
		 LIMIT ? OFFSET ?`,
		approverID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// QueryByTimeRange returns entries within a time window.
// Returns the matching entries, total count, and any error.
func (s *AuditStore) QueryByTimeRange(ctx context.Context, start, end time.Time, page, pageSize int) ([]workflow.AuditEntry, int, error) {
	pageSize = workflow.NormalizePageSize(pageSize)
	offset := normalizeOffset(page, pageSize)

	startStr := workflow.NormalizeAuditTimestamp(start).Format(auditTimeFormat)
	endStr := workflow.NormalizeAuditTimestamp(end).Format(auditTimeFormat)

	total, err := s.countWhere(ctx, "timestamp >= ? AND timestamp <= ?", startStr, endStr)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM approval_audit_trail
		 WHERE timestamp >= ? AND timestamp <= ?
		 ORDER BY timestamp ASC
		 LIMIT ? OFFSET ?`,
		startStr, endStr, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// QueryByDecision returns entries filtered by decision outcome.
// Returns the matching entries, total count, and any error.
func (s *AuditStore) QueryByDecision(ctx context.Context, decision string, page, pageSize int) ([]workflow.AuditEntry, int, error) {
	pageSize = workflow.NormalizePageSize(pageSize)
	offset := normalizeOffset(page, pageSize)

	total, err := s.countWhere(ctx, "decision = ?", decision)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instance_id, node_id, event_type, actor_id, decision, matched_rule, rationale, details_json, timestamp
		 FROM approval_audit_trail
		 WHERE decision = ?
		 ORDER BY timestamp ASC
		 LIMIT ? OFFSET ?`,
		decision, pageSize, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries, err := scanAuditEntries(rows)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// countWhere returns the count of rows matching the given WHERE clause.
func (s *AuditStore) countWhere(ctx context.Context, where string, args ...any) (int, error) {
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM approval_audit_trail WHERE %s`, where),
		args...,
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// scanAuditEntries scans all rows into a slice of AuditEntry.
func scanAuditEntries(rows *sql.Rows) ([]workflow.AuditEntry, error) {
	var entries []workflow.AuditEntry
	for rows.Next() {
		var (
			entry     workflow.AuditEntry
			timestamp string
		)
		if err := rows.Scan(
			&entry.ID,
			&entry.InstanceID,
			&entry.NodeID,
			&entry.EventType,
			&entry.ActorID,
			&entry.Decision,
			&entry.MatchedRule,
			&entry.Rationale,
			&entry.Details,
			&timestamp,
		); err != nil {
			return nil, err
		}
		entry.Timestamp = parseAuditTimestamp(timestamp)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// parseAuditTimestamp parses an ISO 8601 timestamp with millisecond precision.
// Falls back to RFC3339 if the millisecond format fails.
func parseAuditTimestamp(s string) time.Time {
	t, err := time.Parse(auditTimeFormat, s)
	if err != nil {
		// Fallback: try RFC3339 (without milliseconds)
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t.UTC()
}

// normalizeOffset computes the SQL OFFSET from page and pageSize.
// Page is 0-indexed. Negative pages are treated as 0.
func normalizeOffset(page, pageSize int) int {
	if page < 0 {
		page = 0
	}
	return page * pageSize
}

// generateAuditID creates a unique ID for an audit entry.
func generateAuditID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "audit_fallback_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "audit_" + hex.EncodeToString(b[:])
}
