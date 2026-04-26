package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

// Store aggregates all repositories.
type Store struct {
	Centers  *CenterRepo
	Licenses *LicenseRepo
	Admins   *AdminRepo
	System   *SystemRepo
}

func NewStore(p *Provider) *Store {
	return &Store{
		Centers:  &CenterRepo{w: p.Write, r: p.Read},
		Licenses: &LicenseRepo{w: p.Write, r: p.Read},
		Admins:   &AdminRepo{w: p.Write, r: p.Read},
		System:   &SystemRepo{w: p.Write, r: p.Read},
	}
}

// ---------- CenterRepo ----------

type CenterRepo struct{ w, r *sql.DB }

func (repo *CenterRepo) Create(ctx context.Context, c *store.Center) error {
	_, err := repo.w.ExecContext(ctx,
		`INSERT INTO centers (id, company_name, admin_email, admin_phone, address, legal_person, base_url, supports_multi_tenant, tenant_count, cloud_control_mode, last_sync_status, status, secret_hash, last_heartbeat, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.CompanyName, c.AdminEmail, c.AdminPhone, c.Address, c.LegalPerson,
		c.BaseURL, boolToInt(c.SupportsMultiTenant), c.TenantCount, normalizeRepoControlMode(c.CloudControlMode), c.LastSyncStatus,
		c.Status, c.SecretHash, c.LastHeartbeat.Format(time.RFC3339), c.CreatedAt.Format(time.RFC3339), c.UpdatedAt.Format(time.RFC3339))
	return err
}

func (repo *CenterRepo) GetByID(ctx context.Context, id string) (*store.Center, error) {
	row := repo.r.QueryRowContext(ctx,
		`SELECT id, company_name, admin_email, admin_phone, address, legal_person, base_url, supports_multi_tenant, tenant_count, cloud_control_mode, last_sync_status, status, secret_hash, last_heartbeat, created_at, updated_at FROM centers WHERE id=?`, id)
	return scanCenter(row)
}

func (repo *CenterRepo) List(ctx context.Context) ([]*store.Center, error) {
	rows, err := repo.r.QueryContext(ctx,
		`SELECT id, company_name, admin_email, admin_phone, address, legal_person, base_url, supports_multi_tenant, tenant_count, cloud_control_mode, last_sync_status, status, secret_hash, last_heartbeat, created_at, updated_at FROM centers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.Center, 0)
	for rows.Next() {
		c, err := scanCenterRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (repo *CenterRepo) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := repo.w.ExecContext(ctx,
		`UPDATE centers SET status=?, updated_at=? WHERE id=?`, status, time.Now().Format(time.RFC3339), id)
	return err
}

func (repo *CenterRepo) UpdateHeartbeat(ctx context.Context, id string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := repo.w.ExecContext(ctx,
		`UPDATE centers SET last_heartbeat=?, updated_at=? WHERE id=?`, now, now, id)
	return err
}

func (repo *CenterRepo) UpdateIntegration(ctx context.Context, c *store.Center) error {
	_, err := repo.w.ExecContext(ctx,
		`UPDATE centers SET base_url=?, supports_multi_tenant=?, tenant_count=?, cloud_control_mode=?, last_sync_status=?, updated_at=? WHERE id=?`,
		c.BaseURL, boolToInt(c.SupportsMultiTenant), c.TenantCount, normalizeRepoControlMode(c.CloudControlMode), c.LastSyncStatus, time.Now().Format(time.RFC3339), c.ID)
	return err
}

func (repo *CenterRepo) Delete(ctx context.Context, id string) error {
	_, err := repo.w.ExecContext(ctx, `DELETE FROM centers WHERE id=?`, id)
	return err
}

func scanCenter(row *sql.Row) (*store.Center, error) {
	var c store.Center
	var hb, ca, ua string
	var supportsMultiTenant int
	if err := row.Scan(&c.ID, &c.CompanyName, &c.AdminEmail, &c.AdminPhone, &c.Address, &c.LegalPerson, &c.BaseURL, &supportsMultiTenant, &c.TenantCount, &c.CloudControlMode, &c.LastSyncStatus, &c.Status, &c.SecretHash, &hb, &ca, &ua); err != nil {
		return nil, err
	}
	c.SupportsMultiTenant = supportsMultiTenant == 1
	if c.CloudControlMode == "" {
		c.CloudControlMode = "cloud_managed"
	}
	c.LastHeartbeat, _ = time.Parse(time.RFC3339, hb)
	c.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return &c, nil
}

func scanCenterRows(rows *sql.Rows) (*store.Center, error) {
	var c store.Center
	var hb, ca, ua string
	var supportsMultiTenant int
	if err := rows.Scan(&c.ID, &c.CompanyName, &c.AdminEmail, &c.AdminPhone, &c.Address, &c.LegalPerson, &c.BaseURL, &supportsMultiTenant, &c.TenantCount, &c.CloudControlMode, &c.LastSyncStatus, &c.Status, &c.SecretHash, &hb, &ca, &ua); err != nil {
		return nil, err
	}
	c.SupportsMultiTenant = supportsMultiTenant == 1
	if c.CloudControlMode == "" {
		c.CloudControlMode = "cloud_managed"
	}
	c.LastHeartbeat, _ = time.Parse(time.RFC3339, hb)
	c.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return &c, nil
}

// ---------- LicenseRepo ----------

type LicenseRepo struct{ w, r *sql.DB }

func (repo *LicenseRepo) Create(ctx context.Context, l *store.License) error {
	isLong := 0
	if l.IsLongTerm {
		isLong = 1
	}
	_, err := repo.w.ExecContext(ctx,
		`INSERT INTO licenses (id, center_id, modules, type, expires_at, is_long_term, certificate, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.CenterID, l.Modules, l.Type, l.ExpiresAt.Format(time.RFC3339), isLong, l.Certificate, l.CreatedAt.Format(time.RFC3339))
	return err
}

func (repo *LicenseRepo) GetByID(ctx context.Context, id string) (*store.License, error) {
	row := repo.r.QueryRowContext(ctx,
		`SELECT id, center_id, modules, type, expires_at, is_long_term, certificate, created_at, revoked_at FROM licenses WHERE id=?`, id)
	return scanLicense(row)
}

func (repo *LicenseRepo) GetByCenterID(ctx context.Context, centerID string) ([]*store.License, error) {
	rows, err := repo.r.QueryContext(ctx,
		`SELECT id, center_id, modules, type, expires_at, is_long_term, certificate, created_at, revoked_at FROM licenses WHERE center_id=? ORDER BY created_at DESC`, centerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.License, 0)
	for rows.Next() {
		l, err := scanLicenseRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func (repo *LicenseRepo) GetActiveByCenterID(ctx context.Context, centerID string) (*store.License, error) {
	row := repo.r.QueryRowContext(ctx,
		`SELECT id, center_id, modules, type, expires_at, is_long_term, certificate, created_at, revoked_at
		 FROM licenses WHERE center_id=? AND revoked_at IS NULL
		 AND (is_long_term=1 OR expires_at > datetime('now'))
		 ORDER BY created_at DESC LIMIT 1`, centerID)
	return scanLicense(row)
}

func (repo *LicenseRepo) Revoke(ctx context.Context, id string) error {
	_, err := repo.w.ExecContext(ctx,
		`UPDATE licenses SET revoked_at=? WHERE id=?`, time.Now().Format(time.RFC3339), id)
	return err
}

func (repo *LicenseRepo) List(ctx context.Context) ([]*store.License, error) {
	rows, err := repo.r.QueryContext(ctx,
		`SELECT id, center_id, modules, type, expires_at, is_long_term, certificate, created_at, revoked_at FROM licenses ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.License, 0)
	for rows.Next() {
		l, err := scanLicenseRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func scanLicense(row *sql.Row) (*store.License, error) {
	var l store.License
	var ea, ca string
	var ra sql.NullString
	var isLong int
	if err := row.Scan(&l.ID, &l.CenterID, &l.Modules, &l.Type, &ea, &isLong, &l.Certificate, &ca, &ra); err != nil {
		return nil, err
	}
	l.ExpiresAt, _ = time.Parse(time.RFC3339, ea)
	l.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	l.IsLongTerm = isLong == 1
	if ra.Valid {
		t, _ := time.Parse(time.RFC3339, ra.String)
		l.RevokedAt = &t
	}
	return &l, nil
}

func scanLicenseRows(rows *sql.Rows) (*store.License, error) {
	var l store.License
	var ea, ca string
	var ra sql.NullString
	var isLong int
	if err := rows.Scan(&l.ID, &l.CenterID, &l.Modules, &l.Type, &ea, &isLong, &l.Certificate, &ca, &ra); err != nil {
		return nil, err
	}
	l.ExpiresAt, _ = time.Parse(time.RFC3339, ea)
	l.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	l.IsLongTerm = isLong == 1
	if ra.Valid {
		t, _ := time.Parse(time.RFC3339, ra.String)
		l.RevokedAt = &t
	}
	return &l, nil
}

// ---------- AdminRepo ----------

type AdminRepo struct{ w, r *sql.DB }

func (repo *AdminRepo) Create(ctx context.Context, a *store.Admin) error {
	_, err := repo.w.ExecContext(ctx,
		`INSERT INTO admins (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		a.ID, a.Username, a.PasswordHash, a.CreatedAt.Format(time.RFC3339))
	return err
}

func (repo *AdminRepo) GetByUsername(ctx context.Context, username string) (*store.Admin, error) {
	var a store.Admin
	var ca string
	err := repo.r.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM admins WHERE username=?`, username).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &ca)
	if err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	return &a, nil
}

func (repo *AdminRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := repo.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

func (repo *AdminRepo) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	_, err := repo.w.ExecContext(ctx, `UPDATE admins SET password_hash=? WHERE id=?`, passwordHash, id)
	return err
}

// ---------- SystemRepo ----------

type SystemRepo struct{ w, r *sql.DB }

func (repo *SystemRepo) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := repo.r.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key=?`, key).Scan(&v)
	if err != nil {
		return "", err
	}
	return v, nil
}

func (repo *SystemRepo) Set(ctx context.Context, key, value string) error {
	_, err := repo.w.ExecContext(ctx,
		`INSERT INTO system_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeRepoControlMode(mode string) string {
	switch mode {
	case "cloud_managed", "self_managed", "hybrid":
		return mode
	default:
		return "cloud_managed"
	}
}
