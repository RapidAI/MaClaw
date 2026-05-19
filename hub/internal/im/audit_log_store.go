package im

import (
	"context"
	"database/sql"
	"fmt"
)

// SQLiteAuditLogStore implements AuditLogStore using SQLite.
type SQLiteAuditLogStore struct {
	db *sql.DB
}

// NewSQLiteAuditLogStore creates a new SQLiteAuditLogStore.
func NewSQLiteAuditLogStore(db *sql.DB) *SQLiteAuditLogStore {
	return &SQLiteAuditLogStore{db: db}
}

// WriteLog persists an AuditLogEntry to the content_audit_logs table.
func (s *SQLiteAuditLogStore) WriteLog(ctx context.Context, entry *AuditLogEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO content_audit_logs (tenant_id, timestamp, user_id, platform, content_type, summary, return_code, duration_ms, message, content_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.TenantID,
		entry.Timestamp,
		entry.UserID,
		entry.Platform,
		entry.ContentType,
		entry.Summary,
		entry.ReturnCode,
		entry.Duration.Milliseconds(),
		entry.Message,
		entry.ContentHash,
	)
	if err != nil {
		return fmt.Errorf("write content audit log: %w", err)
	}
	return nil
}
