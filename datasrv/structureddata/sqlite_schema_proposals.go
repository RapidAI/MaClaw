package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateSchemaProposal(ctx context.Context, proposal SchemaProposal) (*SchemaProposal, error) {
	suggestedJSON, err := json.Marshal(proposal.Suggested)
	if err != nil {
		return nil, err
	}
	ignoredJSON, err := json.Marshal(proposal.Ignored)
	if err != nil {
		return nil, err
	}
	impactJSON, err := json.Marshal(proposal.Impact)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO schema_proposals(id, tenant_id, dataset_id, status, reason, suggested_json, ignored_json, impact_json, created_by, applied_by, applied_at, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, proposal.ID, proposal.TenantID, proposal.DatasetID, proposal.Status, proposal.Reason, string(suggestedJSON), string(ignoredJSON), string(impactJSON), proposal.CreatedBy, proposal.AppliedBy, formatOptionalTime(proposal.AppliedAt), formatTime(proposal.CreatedAt), formatTime(proposal.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return s.GetSchemaProposal(ctx, proposal.TenantID, proposal.DatasetID, proposal.ID)
}

func (s *SQLiteStore) ListSchemaProposals(ctx context.Context, tenantID, datasetID string, in ListSchemaProposalsInput) ([]SchemaProposal, error) {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, tenant_id, dataset_id, status, reason, suggested_json, ignored_json, impact_json, created_by, applied_by, applied_at, created_at, updated_at FROM schema_proposals WHERE tenant_id = ? AND dataset_id = ?`
	args := []any{tenantID, datasetID}
	if strings.TrimSpace(in.Status) != "" {
		query += ` AND status = ?`
		args = append(args, strings.TrimSpace(in.Status))
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" {
			query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
			args = append(args, before, before, beforeID)
		} else {
			query += ` AND created_at < ?`
			args = append(args, before)
		}
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SchemaProposal{}
	for rows.Next() {
		proposal, err := scanSchemaProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, proposal)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetSchemaProposal(ctx context.Context, tenantID, datasetID, proposalID string) (*SchemaProposal, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, dataset_id, status, reason, suggested_json, ignored_json, impact_json, created_by, applied_by, applied_at, created_at, updated_at FROM schema_proposals WHERE tenant_id = ? AND dataset_id = ? AND id = ?`, tenantID, datasetID, proposalID)
	proposal, err := scanSchemaProposal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &proposal, nil
}

func (s *SQLiteStore) UpdateSchemaProposalStatus(ctx context.Context, tenantID, datasetID, proposalID, status, actor string, now time.Time) (*SchemaProposal, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE schema_proposals SET status = ?, applied_by = ?, applied_at = ?, updated_at = ? WHERE tenant_id = ? AND dataset_id = ? AND id = ?`, strings.TrimSpace(status), strings.TrimSpace(actor), formatTime(now), formatTime(now), tenantID, datasetID, proposalID)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrRecordNotFound
	}
	return s.GetSchemaProposal(ctx, tenantID, datasetID, proposalID)
}

func scanSchemaProposal(scanner interface{ Scan(dest ...any) error }) (SchemaProposal, error) {
	var proposal SchemaProposal
	var suggestedJSON, ignoredJSON, impactJSON, appliedAt, createdAt, updatedAt string
	if err := scanner.Scan(&proposal.ID, &proposal.TenantID, &proposal.DatasetID, &proposal.Status, &proposal.Reason, &suggestedJSON, &ignoredJSON, &impactJSON, &proposal.CreatedBy, &proposal.AppliedBy, &appliedAt, &createdAt, &updatedAt); err != nil {
		return SchemaProposal{}, err
	}
	_ = json.Unmarshal([]byte(suggestedJSON), &proposal.Suggested)
	_ = json.Unmarshal([]byte(ignoredJSON), &proposal.Ignored)
	_ = json.Unmarshal([]byte(impactJSON), &proposal.Impact)
	if strings.TrimSpace(appliedAt) != "" {
		parsed := parseTime(appliedAt)
		proposal.AppliedAt = &parsed
	}
	proposal.CreatedAt = parseTime(createdAt)
	proposal.UpdatedAt = parseTime(updatedAt)
	return proposal, nil
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}
