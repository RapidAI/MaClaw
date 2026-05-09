package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *SQLiteStore) UpsertExternalConnector(ctx context.Context, connector ExternalConnector) (*ExternalConnector, error) {
	actionsJSON, err := json.Marshal(connector.SubscribedActions)
	if err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(connector.Config)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO external_connectors(id, tenant_id, domain, name, kind, base_url, auth_type, token_ref, enabled, subscribed_actions_json, config_json, created_by, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			domain = excluded.domain,
			name = excluded.name,
			kind = excluded.kind,
			base_url = excluded.base_url,
			auth_type = excluded.auth_type,
			token_ref = excluded.token_ref,
			enabled = excluded.enabled,
			subscribed_actions_json = excluded.subscribed_actions_json,
			config_json = excluded.config_json,
			updated_at = excluded.updated_at`,
		connector.ID, connector.TenantID, connector.Domain, connector.Name, connector.Kind, connector.BaseURL, connector.AuthType, connector.TokenRef, boolInt(connector.Enabled), string(actionsJSON), string(configJSON), connector.CreatedBy, formatTime(connector.CreatedAt), formatTime(connector.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return s.GetExternalConnector(ctx, connector.TenantID, connector.ID)
}

func (s *SQLiteStore) ListExternalConnectors(ctx context.Context, tenantID string, in QueryExternalConnectorsInput) ([]ExternalConnector, error) {
	query := `SELECT id, tenant_id, domain, name, kind, base_url, auth_type, token_ref, enabled, subscribed_actions_json, config_json, created_by, created_at, updated_at FROM external_connectors WHERE tenant_id = ?`
	args := []any{tenantID}
	if strings.TrimSpace(in.Domain) != "" {
		query += ` AND domain = ?`
		args = append(args, strings.ToLower(strings.TrimSpace(in.Domain)))
	}
	if strings.TrimSpace(in.Kind) != "" {
		query += ` AND kind = ?`
		args = append(args, strings.ToLower(strings.TrimSpace(in.Kind)))
	}
	if in.Enabled != nil {
		query += ` AND enabled = ?`
		args = append(args, boolInt(*in.Enabled))
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" {
			query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
			args = append(args, before, before, beforeID)
		} else {
			query += ` AND updated_at < ?`
			args = append(args, before)
		}
	}
	query += ` ORDER BY updated_at DESC, id DESC`
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += ` LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExternalConnector{}
	for rows.Next() {
		connector, err := scanExternalConnector(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, connector)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetExternalConnector(ctx context.Context, tenantID, connectorID string) (*ExternalConnector, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, domain, name, kind, base_url, auth_type, token_ref, enabled, subscribed_actions_json, config_json, created_by, created_at, updated_at FROM external_connectors WHERE tenant_id = ? AND id = ?`, tenantID, strings.TrimSpace(connectorID))
	connector, err := scanExternalConnector(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &connector, nil
}

func (s *SQLiteStore) DeleteExternalConnector(ctx context.Context, tenantID, connectorID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM external_connectors WHERE tenant_id = ? AND id = ?`, tenantID, strings.TrimSpace(connectorID))
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
	return nil
}

func scanExternalConnector(scanner interface{ Scan(dest ...any) error }) (ExternalConnector, error) {
	var connector ExternalConnector
	var enabled int
	var actionsJSON, configJSON, createdAt, updatedAt string
	if err := scanner.Scan(&connector.ID, &connector.TenantID, &connector.Domain, &connector.Name, &connector.Kind, &connector.BaseURL, &connector.AuthType, &connector.TokenRef, &enabled, &actionsJSON, &configJSON, &connector.CreatedBy, &createdAt, &updatedAt); err != nil {
		return ExternalConnector{}, err
	}
	connector.Enabled = intBool(enabled)
	_ = json.Unmarshal([]byte(actionsJSON), &connector.SubscribedActions)
	_ = json.Unmarshal([]byte(configJSON), &connector.Config)
	if connector.Config == nil {
		connector.Config = map[string]any{}
	}
	connector.CreatedAt = parseTime(createdAt)
	connector.UpdatedAt = parseTime(updatedAt)
	return connector, nil
}
