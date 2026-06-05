package structureddata

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func (s *SQLiteStore) UpsertDataTenants(ctx context.Context, tenants []DataTenantInfo, source string, now time.Time) ([]DataTenantInfo, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	out := make([]DataTenantInfo, 0, len(tenants))
	for _, raw := range tenants {
		item, err := normalizeDataTenantInfo(raw, source, now)
		if err != nil {
			return nil, err
		}
		domainsJSON, err := json.Marshal(item.Domains)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO data_tenants(id, hub_tenant_id, slug, name, status, primary_domain, domains_json, virtual_mail_domain, source, synced_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET hub_tenant_id = excluded.hub_tenant_id, slug = excluded.slug, name = excluded.name, status = excluded.status, primary_domain = excluded.primary_domain, domains_json = excluded.domains_json, virtual_mail_domain = excluded.virtual_mail_domain, source = excluded.source, synced_at = excluded.synced_at, updated_at = excluded.updated_at`,
			item.ID, item.HubTenantID, item.Slug, item.Name, item.Status, item.PrimaryDomain, string(domainsJSON), item.VirtualMailDomain, item.Source, formatTime(item.SyncedAt), formatTime(item.UpdatedAt))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return out, nil
}

func (s *SQLiteStore) ListDataTenants(ctx context.Context) ([]DataTenantInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, hub_tenant_id, slug, name, status, primary_domain, domains_json, virtual_mail_domain, source, synced_at, updated_at FROM data_tenants ORDER BY id`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return []DataTenantInfo{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	out := []DataTenantInfo{}
	for rows.Next() {
		item, err := scanDataTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type dataTenantScanner interface{ Scan(dest ...any) error }

func scanDataTenant(row dataTenantScanner) (DataTenantInfo, error) {
	var item DataTenantInfo
	var domainsJSON, syncedAt, updatedAt string
	err := row.Scan(&item.ID, &item.HubTenantID, &item.Slug, &item.Name, &item.Status, &item.PrimaryDomain, &domainsJSON, &item.VirtualMailDomain, &item.Source, &syncedAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return item, err
		}
		return item, err
	}
	_ = json.Unmarshal([]byte(domainsJSON), &item.Domains)
	if strings.TrimSpace(syncedAt) != "" {
		item.SyncedAt = parseTime(syncedAt)
	}
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}
