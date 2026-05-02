package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Tenant represents a company in the multi-tenant system.
type Tenant struct {
	ID            string
	CompanyName   string
	LegalPerson   string
	Email         string
	Address       string
	Status        string // active, disabled
	CloudCenterID string
	CloudSecret   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TenantRepo provides CRUD operations for the tenants table.
type TenantRepo struct {
	write *sql.DB
	read  *sql.DB
}

func NewTenantRepo(write, read *sql.DB) *TenantRepo {
	return &TenantRepo{write: write, read: read}
}

func (r *TenantRepo) Create(ctx context.Context, t *Tenant) error {
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO tenants (id, company_name, legal_person, email, address, status, cloud_center_id, cloud_secret, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.CompanyName, t.LegalPerson, t.Email, t.Address, t.Status,
		t.CloudCenterID, t.CloudSecret,
		t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *TenantRepo) GetByID(ctx context.Context, id string) (*Tenant, error) {
	return r.scanOne(r.read.QueryRowContext(ctx,
		`SELECT id, company_name, legal_person, email, address, status, cloud_center_id, cloud_secret, created_at, updated_at
		 FROM tenants WHERE id = ?`, id))
}

func (r *TenantRepo) GetByCloudCenterID(ctx context.Context, centerID string) (*Tenant, error) {
	return r.scanOne(r.read.QueryRowContext(ctx,
		`SELECT id, company_name, legal_person, email, address, status, cloud_center_id, cloud_secret, created_at, updated_at
		 FROM tenants WHERE cloud_center_id = ?`, centerID))
}
func (r *TenantRepo) GetByCompanyName(ctx context.Context, name string) (*Tenant, error) {
	return r.scanOne(r.read.QueryRowContext(ctx,
		`SELECT id, company_name, legal_person, email, address, status, cloud_center_id, cloud_secret, created_at, updated_at
		 FROM tenants WHERE company_name = ?`, name))
}

func (r *TenantRepo) List(ctx context.Context) ([]*Tenant, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT id, company_name, legal_person, email, address, status, cloud_center_id, cloud_secret, created_at, updated_at
		 FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		t, err := r.scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TenantRepo) ListActive(ctx context.Context) ([]*Tenant, error) {
	rows, err := r.read.QueryContext(ctx,
		`SELECT id, company_name, legal_person, email, address, status, cloud_center_id, cloud_secret, created_at, updated_at
		 FROM tenants WHERE status = 'active' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		t, err := r.scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TenantRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants`).Scan(&n)
	return n, err
}

func (r *TenantRepo) Update(ctx context.Context, t *Tenant) error {
	_, err := r.write.ExecContext(ctx,
		`UPDATE tenants SET company_name = ?, legal_person = ?, email = ?, address = ?, status = ?, updated_at = datetime('now') WHERE id = ?`,
		t.CompanyName, t.LegalPerson, t.Email, t.Address, t.Status, t.ID)
	return err
}

func (r *TenantRepo) Delete(ctx context.Context, id string) error {
	_, err := r.write.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, id)
	return err
}
func (r *TenantRepo) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := r.write.ExecContext(ctx,
		`UPDATE tenants SET status = ?, updated_at = datetime('now') WHERE id = ?`, status, id)
	return err
}

func (r *TenantRepo) UpdateCloudInfo(ctx context.Context, id, centerID, secret string) error {
	_, err := r.write.ExecContext(ctx,
		`UPDATE tenants SET cloud_center_id = ?, cloud_secret = ?, updated_at = datetime('now') WHERE id = ?`,
		centerID, secret, id)
	return err
}

func (r *TenantRepo) scanOne(row *sql.Row) (*Tenant, error) {
	t := &Tenant{}
	var createdAt, updatedAt string
	err := row.Scan(&t.ID, &t.CompanyName, &t.LegalPerson, &t.Email, &t.Address,
		&t.Status, &t.CloudCenterID, &t.CloudSecret, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan tenant: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func (r *TenantRepo) scanRow(s scannable) (*Tenant, error) {
	t := &Tenant{}
	var createdAt, updatedAt string
	err := s.Scan(&t.ID, &t.CompanyName, &t.LegalPerson, &t.Email, &t.Address,
		&t.Status, &t.CloudCenterID, &t.CloudSecret, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan tenant row: %w", err)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}
