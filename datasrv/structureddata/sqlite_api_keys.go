package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func (s *SQLiteStore) CreateAPIKeyPolicy(ctx context.Context, record APIKeyPolicyRecord, keyHash string) (*APIKeyPolicyRecord, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_key_policies(
		id, tenant_id, user_id, role, key_hash, key_prefix, enabled,
		allowed_domains_json, allowed_datasets_json, allowed_actions_json, allowed_views_json, allowed_reports_json, allowed_dashboards_json,
		allow_raw_data, allow_sensitive, allow_admin, note, expires_at, created_by, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.TenantID, record.UserID, record.Role, strings.TrimSpace(keyHash), record.KeyPrefix, boolInt(record.Enabled),
		jsonString(record.AllowedDomains), jsonString(record.AllowedDatasets), jsonString(record.AllowedActions), jsonString(record.AllowedViews), jsonString(record.AllowedReports), jsonString(record.AllowedDashboards),
		boolInt(record.AllowRawData), boolInt(record.AllowSensitive), boolInt(record.AllowAdmin), record.Note, formatOptionalAPIKeyTime(record.ExpiresAt), record.CreatedBy, formatTime(record.CreatedAt), formatTime(record.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return &record, nil
}

func (s *SQLiteStore) ListAPIKeyPolicies(ctx context.Context, tenantID string, in QueryAPIKeyPoliciesInput) ([]APIKeyPolicyRecord, error) {
	clauses := []string{"tenant_id = ?"}
	args := []any{tenantID}
	if in.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, boolInt(*in.Enabled))
	}
	if q := strings.ToLower(strings.TrimSpace(in.Q)); q != "" {
		clauses = append(clauses, "(LOWER(id) LIKE ? OR LOWER(user_id) LIKE ? OR LOWER(note) LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	before := strings.TrimSpace(in.Before)
	beforeID := strings.TrimSpace(in.BeforeID)
	if before != "" {
		if beforeID != "" {
			clauses = append(clauses, "(updated_at < ? OR (updated_at = ? AND id < ?))")
			args = append(args, before, before, beforeID)
		} else {
			clauses = append(clauses, "updated_at < ?")
			args = append(args, before)
		}
	}
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT `+apiKeyPolicyColumns()+` FROM api_key_policies WHERE `+strings.Join(clauses, " AND ")+` ORDER BY updated_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKeyPolicyRecord{}
	for rows.Next() {
		item, err := scanAPIKeyPolicyRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetAPIKeyPolicy(ctx context.Context, tenantID, keyID string) (*APIKeyPolicyRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+apiKeyPolicyColumns()+` FROM api_key_policies WHERE tenant_id = ? AND id = ?`, tenantID, strings.TrimSpace(keyID))
	item, err := scanAPIKeyPolicyRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDatasetNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) FindAPIKeyPolicyByHash(ctx context.Context, keyHash string) (*APIKeyPolicyRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+apiKeyPolicyColumns()+` FROM api_key_policies WHERE key_hash = ? AND enabled = 1`, strings.TrimSpace(keyHash))
	item, err := scanAPIKeyPolicyRecord(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) UpdateAPIKeyPolicy(ctx context.Context, record APIKeyPolicyRecord) (*APIKeyPolicyRecord, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE api_key_policies SET
		user_id = ?, role = ?, enabled = ?,
		allowed_domains_json = ?, allowed_datasets_json = ?, allowed_actions_json = ?, allowed_views_json = ?, allowed_reports_json = ?, allowed_dashboards_json = ?,
		allow_raw_data = ?, allow_sensitive = ?, allow_admin = ?, note = ?, expires_at = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ?`,
		record.UserID, record.Role, boolInt(record.Enabled),
		jsonString(record.AllowedDomains), jsonString(record.AllowedDatasets), jsonString(record.AllowedActions), jsonString(record.AllowedViews), jsonString(record.AllowedReports), jsonString(record.AllowedDashboards),
		boolInt(record.AllowRawData), boolInt(record.AllowSensitive), boolInt(record.AllowAdmin), record.Note, formatOptionalAPIKeyTime(record.ExpiresAt), formatTime(record.UpdatedAt),
		record.TenantID, record.ID)
	if err != nil {
		return nil, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return nil, ErrDatasetNotFound
	}
	return s.GetAPIKeyPolicy(ctx, record.TenantID, record.ID)
}

func (s *SQLiteStore) RotateAPIKeyPolicySecret(ctx context.Context, tenantID, keyID, keyHash, keyPrefix string, now time.Time) (*APIKeyPolicyRecord, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE api_key_policies SET key_hash = ?, key_prefix = ?, enabled = 1, updated_at = ? WHERE tenant_id = ? AND id = ?`, strings.TrimSpace(keyHash), strings.TrimSpace(keyPrefix), formatTime(now), tenantID, strings.TrimSpace(keyID))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return nil, ErrDatasetNotFound
	}
	return s.GetAPIKeyPolicy(ctx, tenantID, keyID)
}

func (s *SQLiteStore) TouchAPIKeyPolicyUse(ctx context.Context, tenantID, keyID, ip, userAgent string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_key_policies SET last_used_at = ?, last_used_ip = ?, last_used_user_agent = ? WHERE tenant_id = ? AND id = ?`,
		formatTime(now), strings.TrimSpace(ip), trimForStorage(userAgent, 500), tenantID, strings.TrimSpace(keyID))
	return err
}

func (s *SQLiteStore) DisableAPIKeyPolicy(ctx context.Context, tenantID, keyID, actor string, now time.Time) (*APIKeyPolicyRecord, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE api_key_policies SET enabled = 0, note = CASE WHEN note = '' THEN ? ELSE note END, updated_at = ? WHERE tenant_id = ? AND id = ?`, "disabled by "+strings.TrimSpace(actor), formatTime(now), tenantID, strings.TrimSpace(keyID))
	if err != nil {
		return nil, err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return nil, ErrDatasetNotFound
	}
	return s.GetAPIKeyPolicy(ctx, tenantID, keyID)
}

func apiKeyPolicyColumns() string {
	return `id, tenant_id, user_id, role, key_prefix, enabled, allowed_domains_json, allowed_datasets_json, allowed_actions_json, allowed_views_json, allowed_reports_json, allowed_dashboards_json, allow_raw_data, allow_sensitive, allow_admin, note, expires_at, last_used_at, last_used_ip, last_used_user_agent, created_by, created_at, updated_at`
}

type apiKeyPolicyScanner interface {
	Scan(dest ...any) error
}

func scanAPIKeyPolicyRecord(row apiKeyPolicyScanner) (APIKeyPolicyRecord, error) {
	var item APIKeyPolicyRecord
	var enabled, allowRawData, allowSensitive, allowAdmin int
	var domainsJSON, datasetsJSON, actionsJSON, viewsJSON, reportsJSON, dashboardsJSON string
	var expiresAt, lastUsedAt, createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.TenantID, &item.UserID, &item.Role, &item.KeyPrefix, &enabled, &domainsJSON, &datasetsJSON, &actionsJSON, &viewsJSON, &reportsJSON, &dashboardsJSON, &allowRawData, &allowSensitive, &allowAdmin, &item.Note, &expiresAt, &lastUsedAt, &item.LastUsedIP, &item.LastUsedUserAgent, &item.CreatedBy, &createdAt, &updatedAt)
	if err != nil {
		return item, err
	}
	item.Enabled = enabled != 0
	item.AllowedDomains = parseStringListJSON(domainsJSON)
	item.AllowedDatasets = parseStringListJSON(datasetsJSON)
	item.AllowedActions = parseStringListJSON(actionsJSON)
	item.AllowedViews = parseStringListJSON(viewsJSON)
	item.AllowedReports = parseStringListJSON(reportsJSON)
	item.AllowedDashboards = parseStringListJSON(dashboardsJSON)
	item.AllowRawData = allowRawData != 0
	item.AllowSensitive = allowSensitive != 0
	item.AllowAdmin = allowAdmin != 0
	if strings.TrimSpace(expiresAt) != "" {
		parsed := parseTime(expiresAt)
		item.ExpiresAt = &parsed
	}
	if strings.TrimSpace(lastUsedAt) != "" {
		parsed := parseTime(lastUsedAt)
		item.LastUsedAt = &parsed
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func formatOptionalAPIKeyTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return formatTime(*value)
}

func parseStringListJSON(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		return nil
	}
	return normalizeStringList(out)
}

func trimForStorage(value string, max int) string {
	value = strings.TrimSpace(value)
	if max > 0 && len(value) > max {
		return value[:max]
	}
	return value
}
