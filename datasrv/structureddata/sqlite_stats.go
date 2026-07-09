package structureddata

import (
	"context"
	"os"
)

func (s *SQLiteStore) SystemStats(ctx context.Context, tenantID string) (*SystemStats, error) {
	version, err := s.SchemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	out := &SystemStats{
		Engine:        "sqlite",
		TenantID:      tenantID,
		SchemaVersion: version,
		ImportJobs:    map[string]int{},
		ExportJobs:    map[string]int{},
		Datasets:      []DatasetStats{},
		Extra:         map[string]interface{}{},
	}
	rdb := s.queryDB()
	if err := rdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM datasets WHERE tenant_id = ?`, tenantID).Scan(&out.DatasetCount); err != nil {
		return nil, err
	}
	if err := rdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM records WHERE tenant_id = ?`, tenantID).Scan(&out.RecordCount); err != nil {
		return nil, err
	}
	if err := rdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM field_definitions WHERE tenant_id = ?`, tenantID).Scan(&out.FieldCount); err != nil {
		return nil, err
	}
	if err := rdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_runs WHERE tenant_id = ?`, tenantID).Scan(&out.QualityRunCount); err != nil {
		return nil, err
	}
	if err := rdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE tenant_id = ?`, tenantID).Scan(&out.AuditLogCount); err != nil {
		return nil, err
	}
	backups, err := s.ListBackups(ctx, QueryBackupsInput{Limit: 500})
	if err == nil {
		out.BackupCount = len(backups)
	}
	if info, err := os.Stat(s.path); err == nil {
		out.DatabaseBytes = info.Size()
	}
	rows, err := rdb.QueryContext(ctx, `SELECT status, COUNT(*) FROM import_jobs WHERE tenant_id = ? GROUP BY status`, tenantID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out.ImportJobs[status] = count
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = rdb.QueryContext(ctx, `SELECT status, COUNT(*) FROM export_jobs WHERE tenant_id = ? GROUP BY status`, tenantID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out.ExportJobs[status] = count
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = rdb.QueryContext(ctx, `SELECT d.id, d.domain, d.name, d.title, d.schema_version, d.updated_at, COUNT(DISTINCT f.field_key), COUNT(DISTINCT r.id)
		FROM datasets d
		LEFT JOIN field_definitions f ON f.tenant_id = d.tenant_id AND f.dataset_id = d.id
		LEFT JOIN records r ON r.tenant_id = d.tenant_id AND r.dataset_id = d.id
		WHERE d.tenant_id = ?
		GROUP BY d.id, d.domain, d.name, d.title, d.schema_version, d.updated_at
		ORDER BY d.domain, d.name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item DatasetStats
		var updatedAt string
		if err := rows.Scan(&item.DatasetID, &item.Domain, &item.Name, &item.Title, &item.SchemaVersion, &updatedAt, &item.FieldCount, &item.RecordCount); err != nil {
			return nil, err
		}
		item.UpdatedAt = parseTime(updatedAt)
		out.Datasets = append(out.Datasets, item)
	}
	return out, rows.Err()
}
