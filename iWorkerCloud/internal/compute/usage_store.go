package compute

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UsageFilter specifies optional filters for querying token usage records.
type UsageFilter struct {
	CenterID   string
	DiWorkerID string
	Start      string // RFC3339
	End        string // RFC3339
}

// UsageStore manages token_usage_records in SQLite.
type UsageStore struct {
	db *sql.DB
}

// NewUsageStore creates a new UsageStore backed by the given database.
func NewUsageStore(db *sql.DB) *UsageStore {
	return &UsageStore{db: db}
}

// CreateUsageTable creates the token_usage_records table and indexes if they
// do not already exist.
func (s *UsageStore) CreateUsageTable(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS token_usage_records (
    id              TEXT PRIMARY KEY,
    center_id       TEXT NOT NULL,
    diworker_id     TEXT NOT NULL,
    provider_name   TEXT NOT NULL,
    model           TEXT NOT NULL,
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    estimated       INTEGER NOT NULL DEFAULT 0,
    timestamp       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_usage_center ON token_usage_records(center_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_usage_diworker ON token_usage_records(diworker_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_usage_timestamp ON token_usage_records(timestamp);`

	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// RecordUsage inserts a single token usage record. If the record has no ID, one
// is generated automatically. If Timestamp is empty, the current UTC time is used.
func (s *UsageStore) RecordUsage(ctx context.Context, rec TokenUsageRecord) error {
	if rec.ID == "" {
		id, err := generateID()
		if err != nil {
			return fmt.Errorf("record usage: %w", err)
		}
		rec.ID = id
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	const q = `INSERT INTO token_usage_records
		(id, center_id, diworker_id, provider_name, model,
		 input_tokens, output_tokens, total_tokens, estimated, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	estimated := 0
	if rec.Estimated {
		estimated = 1
	}

	_, err := s.db.ExecContext(ctx, q,
		rec.ID, rec.CenterID, rec.DiWorkerID, rec.ProviderName, rec.Model,
		rec.InputTokens, rec.OutputTokens, rec.TotalTokens, estimated, rec.Timestamp,
	)
	return err
}

// QueryUsage returns token usage records matching the given filter. An empty
// filter returns all records ordered by timestamp ascending.
func (s *UsageStore) QueryUsage(ctx context.Context, f UsageFilter) ([]TokenUsageRecord, error) {
	var (
		where []string
		args  []interface{}
	)

	if f.CenterID != "" {
		where = append(where, "center_id = ?")
		args = append(args, f.CenterID)
	}
	if f.DiWorkerID != "" {
		where = append(where, "diworker_id = ?")
		args = append(args, f.DiWorkerID)
	}
	if f.Start != "" {
		where = append(where, "timestamp >= ?")
		args = append(args, f.Start)
	}
	if f.End != "" {
		where = append(where, "timestamp <= ?")
		args = append(args, f.End)
	}

	q := "SELECT id, center_id, diworker_id, provider_name, model, input_tokens, output_tokens, total_tokens, estimated, timestamp FROM token_usage_records"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY timestamp ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []TokenUsageRecord
	for rows.Next() {
		var r TokenUsageRecord
		var est int
		if err := rows.Scan(&r.ID, &r.CenterID, &r.DiWorkerID, &r.ProviderName, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.TotalTokens, &est, &r.Timestamp); err != nil {
			return nil, err
		}
		r.Estimated = est != 0
		records = append(records, r)
	}
	return records, rows.Err()
}
