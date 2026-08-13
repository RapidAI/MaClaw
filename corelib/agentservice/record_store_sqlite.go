package agentservice

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type RecordStore interface {
	SaveStructuredRecord(StructuredRecord) error
	GetStructuredRecord(tenantID, userID, collection, recordID string) (StructuredRecord, error)
	ListStructuredRecords(tenantID, userID string, in ListStructuredRecordsInput) ([]StructuredRecord, error)
	DeleteStructuredRecord(tenantID, userID, collection, recordID string) error
	DeleteStructuredRecordsForUser(tenantID, userID string) error
}

type SQLiteRecordStore struct {
	db *sql.DB
}

func (s *SQLiteRecordStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func NewSQLiteRecordStore(path string) (*SQLiteRecordStore, error) {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteRecordStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteRecordStore) init() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS structured_records (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			collection TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			data_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS structured_record_tags (
			record_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			tag_norm TEXT NOT NULL,
			PRIMARY KEY(record_id, tag_norm),
			FOREIGN KEY(record_id) REFERENCES structured_records(id) ON DELETE CASCADE
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS structured_record_fts USING fts5(record_id UNINDEXED, tenant_id UNINDEXED, user_id UNINDEXED, collection UNINDEXED, text)`,
		`CREATE INDEX IF NOT EXISTS idx_structured_records_scope ON structured_records(tenant_id, user_id, collection, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_structured_records_updated ON structured_records(tenant_id, user_id, collection, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_structured_record_tags_lookup ON structured_record_tags(tag_norm, record_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteRecordStore) SaveStructuredRecord(record StructuredRecord) error {
	dataJSON, err := json.Marshal(record.Data)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx, &err)
	_, err = tx.Exec(`INSERT INTO structured_records(id, tenant_id, user_id, collection, title, data_json, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, data_json=excluded.data_json, updated_at=excluded.updated_at`,
		record.ID, record.TenantID, record.UserID, record.Collection, record.Title, string(dataJSON), formatRecordTime(record.CreatedAt), formatRecordTime(record.UpdatedAt))
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM structured_record_tags WHERE record_id = ?`, record.ID); err != nil {
		return err
	}
	for _, tag := range record.Tags {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO structured_record_tags(record_id, tag, tag_norm) VALUES(?, ?, ?)`, record.ID, tag, strings.ToLower(strings.TrimSpace(tag))); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`DELETE FROM structured_record_fts WHERE record_id = ?`, record.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO structured_record_fts(record_id, tenant_id, user_id, collection, text) VALUES(?, ?, ?, ?, ?)`, record.ID, record.TenantID, record.UserID, record.Collection, recordSearchText(record, string(dataJSON))); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func (s *SQLiteRecordStore) GetStructuredRecord(tenantID, userID, collection, recordID string) (StructuredRecord, error) {
	row := s.db.QueryRow(`SELECT id, tenant_id, user_id, collection, title, data_json, created_at, updated_at FROM structured_records WHERE tenant_id = ? AND user_id = ? AND collection = ? AND id = ?`, tenantID, userID, collection, recordID)
	record, err := scanStructuredRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StructuredRecord{}, ErrRecordNotFound
	}
	if err != nil {
		return StructuredRecord{}, err
	}
	record.Tags, err = s.recordTags(record.ID)
	if err != nil {
		return StructuredRecord{}, err
	}
	return record, nil
}

func (s *SQLiteRecordStore) ListStructuredRecords(tenantID, userID string, in ListStructuredRecordsInput) ([]StructuredRecord, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before := strings.TrimSpace(in.Before)
	tag := strings.ToLower(strings.TrimSpace(in.Tag))
	q := strings.TrimSpace(in.Q)
	clauses := []string{"r.tenant_id = ?", "r.user_id = ?"}
	whereArgs := []any{tenantID, userID}
	joinArgs := []any{}
	if strings.TrimSpace(in.Collection) != "" {
		clauses = append(clauses, "r.collection = ?")
		whereArgs = append(whereArgs, strings.TrimSpace(in.Collection))
	}
	if before != "" {
		clauses = append(clauses, "r.created_at < ?")
		whereArgs = append(whereArgs, before)
	}
	join := ""
	if tag != "" {
		join += " JOIN structured_record_tags t ON t.record_id = r.id AND t.tag_norm = ?"
		joinArgs = append(joinArgs, tag)
	}
	if q != "" {
		join += " JOIN structured_record_fts f ON f.record_id = r.id AND f.structured_record_fts MATCH ?"
		joinArgs = append(joinArgs, ftsQuery(q))
	}
	args := append(joinArgs, whereArgs...)
	args = append(args, limit)
	query := fmt.Sprintf(`SELECT r.id, r.tenant_id, r.user_id, r.collection, r.title, r.data_json, r.created_at, r.updated_at FROM structured_records r%s WHERE %s ORDER BY r.created_at DESC, r.id DESC LIMIT ?`, join, strings.Join(clauses, " AND "))
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []StructuredRecord{}
	for rows.Next() {
		record, err := scanStructuredRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range records {
		records[i].Tags, err = s.recordTags(records[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *SQLiteRecordStore) DeleteStructuredRecord(tenantID, userID, collection, recordID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx, &err)
	res, err := tx.Exec(`DELETE FROM structured_records WHERE tenant_id = ? AND user_id = ? AND collection = ? AND id = ?`, tenantID, userID, collection, recordID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrRecordNotFound
	}
	if _, err = tx.Exec(`DELETE FROM structured_record_fts WHERE record_id = ?`, recordID); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

// DeleteStructuredRecordsForUser removes every structured record and derived
// FTS/tag row for one user. It is used by host-level account unbinding, where
// listing collections first would be incomplete and race-prone.
func (s *SQLiteRecordStore) DeleteStructuredRecordsForUser(tenantID, userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx, &err)
	if _, err = tx.Exec(`DELETE FROM structured_record_fts WHERE record_id IN (SELECT id FROM structured_records WHERE tenant_id = ? AND user_id = ?)`, tenantID, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM structured_records WHERE tenant_id = ? AND user_id = ?`, tenantID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteRecordStore) recordTags(recordID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT tag FROM structured_record_tags WHERE record_id = ? ORDER BY tag`, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

type recordScanner interface {
	Scan(dest ...any) error
}

func scanStructuredRecord(scanner recordScanner) (StructuredRecord, error) {
	var record StructuredRecord
	var dataJSON string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&record.ID, &record.TenantID, &record.UserID, &record.Collection, &record.Title, &dataJSON, &createdAt, &updatedAt); err != nil {
		return StructuredRecord{}, err
	}
	if err := json.Unmarshal([]byte(dataJSON), &record.Data); err != nil {
		return StructuredRecord{}, err
	}
	record.CreatedAt = parseRecordCursor(createdAt)
	record.UpdatedAt = parseRecordCursor(updatedAt)
	return record, nil
}

func parseRecordCursor(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	return parsed
}

func rollbackUnlessCommitted(tx *sql.Tx, err *error) {
	if *err != nil {
		_ = tx.Rollback()
	}
}

func formatRecordTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func recordSearchText(record StructuredRecord, dataJSON string) string {
	parts := []string{record.Collection, record.Title, dataJSON}
	parts = append(parts, record.Tags...)
	return strings.Join(parts, " ")
}

func ftsQuery(q string) string {
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return ""
	}
	for i, field := range fields {
		fields[i] = strings.ReplaceAll(field, `"`, `""`) + "*"
	}
	return strings.Join(fields, " AND ")
}

func removeSQLiteRecordStore(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
