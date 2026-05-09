package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

// Store aggregates all repositories.
type Store struct {
	Centers  *CenterRepo
	Licenses *LicenseRepo
	Admins   *AdminRepo
	System   *SystemRepo
	Skills   *SkillRepo
}

func NewStore(p *Provider) *Store {
	return &Store{
		Centers:  &CenterRepo{w: p.Write, r: p.Read},
		Licenses: &LicenseRepo{w: p.Write, r: p.Read},
		Admins:   &AdminRepo{w: p.Write, r: p.Read},
		System:   &SystemRepo{w: p.Write, r: p.Read},
		Skills:   &SkillRepo{w: p.Write, r: p.Read},
	}
}

// ---------- CenterRepo ----------

type CenterRepo struct{ w, r *sql.DB }

func (repo *CenterRepo) Create(ctx context.Context, c *store.Center) error {
	_, err := repo.w.ExecContext(ctx,
		`INSERT INTO centers (id, machine_id, company_id, company_name, admin_email, admin_phone, address, legal_person, base_url, supports_multi_tenant, tenant_count, cloud_control_mode, last_sync_status, iworker_ready, iworker_readiness_status, iworker_tenant_count, iworker_role_count, iworker_colleague_count, iworker_local_account_count, iworker_agent_instance_count, iworker_readiness_json, runtime_status_json, status, secret_hash, management_secret, last_heartbeat, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.MachineID, c.CompanyID, c.CompanyName, c.AdminEmail, c.AdminPhone, c.Address, c.LegalPerson,
		c.BaseURL, boolToInt(c.SupportsMultiTenant), c.TenantCount, normalizeRepoControlMode(c.CloudControlMode), c.LastSyncStatus,
		boolToInt(c.IWorkerReady), c.IWorkerReadinessStatus, c.IWorkerTenantCount, c.IWorkerRoleCount, c.IWorkerColleagueCount, c.IWorkerLocalAccountCount, c.IWorkerAgentInstanceCount, c.IWorkerReadinessJSON, c.RuntimeStatusJSON,
		c.Status, c.SecretHash, c.ManagementSecret, c.LastHeartbeat.Format(time.RFC3339), c.CreatedAt.Format(time.RFC3339), c.UpdatedAt.Format(time.RFC3339))
	return err
}

func (repo *CenterRepo) GetByID(ctx context.Context, id string) (*store.Center, error) {
	row := repo.r.QueryRowContext(ctx,
		`SELECT id, machine_id, company_id, company_name, admin_email, admin_phone, address, legal_person, base_url, supports_multi_tenant, tenant_count, cloud_control_mode, last_sync_status, iworker_ready, iworker_readiness_status, iworker_tenant_count, iworker_role_count, iworker_colleague_count, iworker_local_account_count, iworker_agent_instance_count, iworker_readiness_json, runtime_status_json, status, secret_hash, management_secret, last_heartbeat, created_at, updated_at FROM centers WHERE id=?`, id)
	return scanCenter(row)
}

func (repo *CenterRepo) GetByRegistrationKey(ctx context.Context, machineID, companyID string) (*store.Center, error) {
	machineID = strings.TrimSpace(machineID)
	companyID = strings.TrimSpace(companyID)
	if machineID == "" || companyID == "" {
		return nil, sql.ErrNoRows
	}
	row := repo.r.QueryRowContext(ctx,
		`SELECT id, machine_id, company_id, company_name, admin_email, admin_phone, address, legal_person, base_url, supports_multi_tenant, tenant_count, cloud_control_mode, last_sync_status, iworker_ready, iworker_readiness_status, iworker_tenant_count, iworker_role_count, iworker_colleague_count, iworker_local_account_count, iworker_agent_instance_count, iworker_readiness_json, runtime_status_json, status, secret_hash, management_secret, last_heartbeat, created_at, updated_at FROM centers WHERE machine_id=? AND company_id=? ORDER BY created_at DESC LIMIT 1`, machineID, companyID)
	return scanCenter(row)
}

func (repo *CenterRepo) List(ctx context.Context) ([]*store.Center, error) {
	rows, err := repo.r.QueryContext(ctx,
		`SELECT id, machine_id, company_id, company_name, admin_email, admin_phone, address, legal_person, base_url, supports_multi_tenant, tenant_count, cloud_control_mode, last_sync_status, iworker_ready, iworker_readiness_status, iworker_tenant_count, iworker_role_count, iworker_colleague_count, iworker_local_account_count, iworker_agent_instance_count, iworker_readiness_json, runtime_status_json, status, secret_hash, management_secret, last_heartbeat, created_at, updated_at FROM centers ORDER BY created_at DESC`)
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

func (repo *CenterRepo) UpdateHeartbeat(ctx context.Context, c *store.Center) error {
	now := time.Now().Format(time.RFC3339)
	_, err := repo.w.ExecContext(ctx,
		`UPDATE centers SET last_heartbeat=?, last_sync_status=?, iworker_ready=?, iworker_readiness_status=?, iworker_tenant_count=?, iworker_role_count=?, iworker_colleague_count=?, iworker_local_account_count=?, iworker_agent_instance_count=?, iworker_readiness_json=?, runtime_status_json=?, updated_at=? WHERE id=?`,
		now, c.LastSyncStatus, boolToInt(c.IWorkerReady), c.IWorkerReadinessStatus, c.IWorkerTenantCount, c.IWorkerRoleCount, c.IWorkerColleagueCount, c.IWorkerLocalAccountCount, c.IWorkerAgentInstanceCount, c.IWorkerReadinessJSON, c.RuntimeStatusJSON, now, c.ID)
	return err
}

func (repo *CenterRepo) UpdateIntegration(ctx context.Context, c *store.Center) error {
	_, err := repo.w.ExecContext(ctx,
		`UPDATE centers SET base_url=?, supports_multi_tenant=?, tenant_count=?, cloud_control_mode=?, last_sync_status=?, iworker_ready=?, iworker_readiness_status=?, iworker_tenant_count=?, iworker_role_count=?, iworker_colleague_count=?, iworker_local_account_count=?, iworker_agent_instance_count=?, iworker_readiness_json=?, runtime_status_json=?, updated_at=? WHERE id=?`,
		c.BaseURL, boolToInt(c.SupportsMultiTenant), c.TenantCount, normalizeRepoControlMode(c.CloudControlMode), c.LastSyncStatus, boolToInt(c.IWorkerReady), c.IWorkerReadinessStatus, c.IWorkerTenantCount, c.IWorkerRoleCount, c.IWorkerColleagueCount, c.IWorkerLocalAccountCount, c.IWorkerAgentInstanceCount, c.IWorkerReadinessJSON, c.RuntimeStatusJSON, time.Now().Format(time.RFC3339), c.ID)
	return err
}

func (repo *CenterRepo) UpdateRegistration(ctx context.Context, c *store.Center) error {
	_, err := repo.w.ExecContext(ctx,
		`UPDATE centers SET machine_id=?, company_id=?, company_name=?, admin_email=?, admin_phone=?, address=?, legal_person=?, base_url=?, supports_multi_tenant=?, tenant_count=?, cloud_control_mode=?, last_sync_status=?, secret_hash=?, management_secret=?, updated_at=? WHERE id=?`,
		c.MachineID, c.CompanyID, c.CompanyName, c.AdminEmail, c.AdminPhone, c.Address, c.LegalPerson, c.BaseURL, boolToInt(c.SupportsMultiTenant), c.TenantCount, normalizeRepoControlMode(c.CloudControlMode), c.LastSyncStatus, c.SecretHash, c.ManagementSecret, time.Now().Format(time.RFC3339), c.ID)
	return err
}

func (repo *CenterRepo) Delete(ctx context.Context, id string) error {
	tx, err := repo.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	cleanup := []struct {
		query string
		args  []any
	}{
		{query: `DELETE FROM licenses WHERE center_id=?`, args: []any{id}},
		{query: `DELETE FROM center_provider_assignments WHERE center_id=?`, args: []any{id}},
		{query: `DELETE FROM compute_settings WHERE key IN (?, ?)`, args: []any{"compute_permission_" + id, "force_sync_" + id}},
		{query: `DELETE FROM token_usage_records WHERE center_id=?`, args: []any{id}},
		{query: `DELETE FROM cost_summaries WHERE center_id=?`, args: []any{id}},
		{query: `UPDATE skill_market_skills SET source_center_id='', updated_at=? WHERE source_center_id=?`, args: []any{time.Now().Format(time.RFC3339), id}},
	}
	for _, item := range cleanup {
		if _, err := tx.ExecContext(ctx, item.query, item.args...); err != nil && !isMissingOptionalTable(err) {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM centers WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func isMissingOptionalTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "no such column")
}

func scanCenter(row *sql.Row) (*store.Center, error) {
	var c store.Center
	var hb, ca, ua string
	var supportsMultiTenant, iworkerReady int
	if err := row.Scan(&c.ID, &c.MachineID, &c.CompanyID, &c.CompanyName, &c.AdminEmail, &c.AdminPhone, &c.Address, &c.LegalPerson, &c.BaseURL, &supportsMultiTenant, &c.TenantCount, &c.CloudControlMode, &c.LastSyncStatus, &iworkerReady, &c.IWorkerReadinessStatus, &c.IWorkerTenantCount, &c.IWorkerRoleCount, &c.IWorkerColleagueCount, &c.IWorkerLocalAccountCount, &c.IWorkerAgentInstanceCount, &c.IWorkerReadinessJSON, &c.RuntimeStatusJSON, &c.Status, &c.SecretHash, &c.ManagementSecret, &hb, &ca, &ua); err != nil {
		return nil, err
	}
	c.SupportsMultiTenant = supportsMultiTenant == 1
	c.IWorkerReady = iworkerReady == 1
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
	var supportsMultiTenant, iworkerReady int
	if err := rows.Scan(&c.ID, &c.MachineID, &c.CompanyID, &c.CompanyName, &c.AdminEmail, &c.AdminPhone, &c.Address, &c.LegalPerson, &c.BaseURL, &supportsMultiTenant, &c.TenantCount, &c.CloudControlMode, &c.LastSyncStatus, &iworkerReady, &c.IWorkerReadinessStatus, &c.IWorkerTenantCount, &c.IWorkerRoleCount, &c.IWorkerColleagueCount, &c.IWorkerLocalAccountCount, &c.IWorkerAgentInstanceCount, &c.IWorkerReadinessJSON, &c.RuntimeStatusJSON, &c.Status, &c.SecretHash, &c.ManagementSecret, &hb, &ca, &ua); err != nil {
		return nil, err
	}
	c.SupportsMultiTenant = supportsMultiTenant == 1
	c.IWorkerReady = iworkerReady == 1
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

// ---------- SkillRepo ----------

type SkillRepo struct{ w, r *sql.DB }

func (repo *SkillRepo) Create(ctx context.Context, s *store.Skill) error {
	_, err := repo.w.ExecContext(ctx,
		`INSERT INTO skill_market_skills (id, name, description, category, version, tags, risk_level, status, price, author, author_email, source_center_id, avg_rating, download_count, package_format, package_content, package_sha256, package_size, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Description, s.Category, s.Version, s.Tags, s.RiskLevel, normalizeSkillStatus(s.Status), s.Price, s.Author, s.AuthorEmail, s.SourceCenterID, s.AvgRating, s.DownloadCount, s.PackageFormat, s.PackageContent, s.PackageSHA256, s.PackageSize, s.CreatedAt.Format(time.RFC3339), s.UpdatedAt.Format(time.RFC3339))
	return err
}

func (repo *SkillRepo) Update(ctx context.Context, s *store.Skill) error {
	_, err := repo.w.ExecContext(ctx,
		`UPDATE skill_market_skills SET name=?, description=?, category=?, version=?, tags=?, risk_level=?, status=?, price=?, author=?, author_email=?, source_center_id=?, avg_rating=?, download_count=?, package_format=?, package_content=?, package_sha256=?, package_size=?, updated_at=? WHERE id=?`,
		s.Name, s.Description, s.Category, s.Version, s.Tags, s.RiskLevel, normalizeSkillStatus(s.Status), s.Price, s.Author, s.AuthorEmail, s.SourceCenterID, s.AvgRating, s.DownloadCount, s.PackageFormat, s.PackageContent, s.PackageSHA256, s.PackageSize, time.Now().Format(time.RFC3339), s.ID)
	return err
}

func (repo *SkillRepo) GetByID(ctx context.Context, id string) (*store.Skill, error) {
	row := repo.r.QueryRowContext(ctx,
		`SELECT id, name, description, category, version, tags, risk_level, status, price, author, author_email, source_center_id, avg_rating, download_count, package_format, package_content, package_sha256, package_size, created_at, updated_at FROM skill_market_skills WHERE id=?`, id)
	return scanSkill(row)
}

func (repo *SkillRepo) List(ctx context.Context) ([]*store.Skill, error) {
	rows, err := repo.r.QueryContext(ctx,
		`SELECT id, name, description, category, version, tags, risk_level, status, price, author, author_email, source_center_id, avg_rating, download_count, package_format, package_content, package_sha256, package_size, created_at, updated_at FROM skill_market_skills ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSkillRows(rows)
}

func (repo *SkillRepo) SearchActive(ctx context.Context, query string) ([]*store.Skill, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		rows, err := repo.r.QueryContext(ctx,
			`SELECT id, name, description, category, version, tags, risk_level, status, price, author, author_email, source_center_id, avg_rating, download_count, package_format, package_content, package_sha256, package_size, created_at, updated_at
			 FROM skill_market_skills
			 WHERE status='active' AND TRIM(COALESCE(package_content, ''))<>'' AND TRIM(COALESCE(package_sha256, ''))<>''
			 ORDER BY download_count DESC, category, name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanSkillRows(rows)
	}
	like := "%" + query + "%"
	rows, err := repo.r.QueryContext(ctx,
		`SELECT id, name, description, category, version, tags, risk_level, status, price, author, author_email, source_center_id, avg_rating, download_count, package_format, package_content, package_sha256, package_size, created_at, updated_at
		 FROM skill_market_skills
		 WHERE status='active' AND TRIM(COALESCE(package_content, ''))<>'' AND TRIM(COALESCE(package_sha256, ''))<>''
		   AND (id LIKE ? OR name LIKE ? OR description LIKE ? OR category LIKE ? OR tags LIKE ?)
		 ORDER BY download_count DESC, category, name`, like, like, like, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSkillRows(rows)
}

func (repo *SkillRepo) Delete(ctx context.Context, id string) error {
	_, err := repo.w.ExecContext(ctx, `DELETE FROM skill_market_skills WHERE id=?`, id)
	return err
}

func (repo *SkillRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := repo.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_market_skills`).Scan(&n)
	return n, err
}

type skillScannable interface {
	Scan(dest ...any) error
}

func scanSkill(row skillScannable) (*store.Skill, error) {
	var s store.Skill
	var ca, ua string
	if err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Category, &s.Version, &s.Tags, &s.RiskLevel, &s.Status, &s.Price, &s.Author, &s.AuthorEmail, &s.SourceCenterID, &s.AvgRating, &s.DownloadCount, &s.PackageFormat, &s.PackageContent, &s.PackageSHA256, &s.PackageSize, &ca, &ua); err != nil {
		return nil, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return &s, nil
}

func scanSkillRows(rows *sql.Rows) ([]*store.Skill, error) {
	out := make([]*store.Skill, 0)
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func normalizeSkillStatus(status string) string {
	switch status {
	case "active", "disabled", "draft":
		return status
	default:
		return "active"
	}
}
