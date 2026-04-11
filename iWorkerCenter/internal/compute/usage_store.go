package compute

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// UsageStore manages center_token_usage records in local SQLite.
type UsageStore struct {
	db *sql.DB
}

// NewUsageStore creates a new UsageStore backed by the given database.
func NewUsageStore(db *sql.DB) *UsageStore {
	return &UsageStore{db: db}
}

// CreateUsageTable creates the center_token_usage table and indexes if they
// do not already exist.
func (s *UsageStore) CreateUsageTable(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS center_token_usage (
    id              TEXT PRIMARY KEY,
    diworker_id     TEXT NOT NULL,
    provider_name   TEXT NOT NULL,
    model           TEXT NOT NULL,
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    estimated       INTEGER NOT NULL DEFAULT 0,
    timestamp       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_center_usage_dw ON center_token_usage(diworker_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_center_usage_ts ON center_token_usage(timestamp);`

	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// RecordUsage inserts a single token usage record. If the record has no ID,
// one is generated automatically. If Timestamp is empty, the current UTC time
// is used.
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

	const q = `INSERT INTO center_token_usage
		(id, diworker_id, provider_name, model,
		 input_tokens, output_tokens, total_tokens, estimated, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	estimated := 0
	if rec.Estimated {
		estimated = 1
	}

	_, err := s.db.ExecContext(ctx, q,
		rec.ID, rec.DiWorkerID, rec.ProviderName, rec.Model,
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

	q := "SELECT id, diworker_id, provider_name, model, input_tokens, output_tokens, total_tokens, estimated, timestamp FROM center_token_usage"
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
		if err := rows.Scan(&r.ID, &r.DiWorkerID, &r.ProviderName, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.TotalTokens, &est, &r.Timestamp); err != nil {
			return nil, err
		}
		r.Estimated = est != 0
		records = append(records, r)
	}
	return records, rows.Err()
}

func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
