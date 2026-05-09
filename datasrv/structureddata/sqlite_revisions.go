package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

func (s *SQLiteStore) AppendRecordRevision(ctx context.Context, revision RecordRevision) (*RecordRevision, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO record_revisions(id, tenant_id, dataset_id, record_id, action, title, tags_json, data_json, source_id, created_by, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revision.ID, revision.TenantID, revision.DatasetID, revision.RecordID, revision.Action, revision.Title, jsonString(revision.Tags), jsonString(revision.Data), revision.SourceID, revision.CreatedBy, formatTime(revision.CreatedAt))
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func (s *SQLiteStore) QueryRecordRevisions(ctx context.Context, tenantID, datasetID, recordID string, in QueryRecordRevisionsInput) ([]RecordRevision, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT rowid, id, tenant_id, dataset_id, record_id, action, title, tags_json, data_json, source_id, created_by, created_at FROM record_revisions WHERE tenant_id = ? AND dataset_id = ? AND record_id = ?`
	args := []any{tenantID, datasetID, recordID}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if rowID, err := strconv.ParseInt(beforeID, 10, 64); err == nil && rowID > 0 {
			query += ` AND (created_at < ? OR (created_at = ? AND rowid < ?))`
			args = append(args, before, before, rowID)
		} else if beforeID != "" {
			query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
			args = append(args, before, before, beforeID)
		} else {
			query += ` AND created_at < ?`
			args = append(args, before)
		}
	}
	query += ` ORDER BY created_at DESC, rowid DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecordRevision{}
	for rows.Next() {
		revision, err := scanRecordRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, revision)
	}
	return out, rows.Err()
}

func scanRecordRevision(scanner interface{ Scan(dest ...any) error }) (RecordRevision, error) {
	var revision RecordRevision
	var tagsJSON, dataJSON, createdAt string
	if err := scanner.Scan(&revision.RowID, &revision.ID, &revision.TenantID, &revision.DatasetID, &revision.RecordID, &revision.Action, &revision.Title, &tagsJSON, &dataJSON, &revision.SourceID, &revision.CreatedBy, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RecordRevision{}, err
		}
		return RecordRevision{}, err
	}
	_ = json.Unmarshal([]byte(tagsJSON), &revision.Tags)
	revision.Data = map[string]any{}
	_ = json.Unmarshal([]byte(dataJSON), &revision.Data)
	revision.CreatedAt = parseTime(createdAt)
	return revision, nil
}
