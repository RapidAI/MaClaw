package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func normalizeTenantRecord(t *store.Tenant) store.Tenant {
	now := time.Now().UTC().Truncate(time.Second)
	out := store.Tenant{ID: store.DefaultTenantID, Slug: "default", Name: "Default Tenant", Status: "active", SettingsJSON: "{}", CreatedByAdminID: "migration", CreatedAt: now, UpdatedAt: now}
	if t != nil {
		out = *t
	}
	if out.ID == "" {
		out.ID = store.DefaultTenantID
	}
	if out.Slug == "" {
		out.Slug = "default"
	}
	if out.Name == "" {
		out.Name = "Default Tenant"
	}
	if out.Status == "" {
		out.Status = "active"
	}
	if out.SettingsJSON == "" {
		out.SettingsJSON = "{}"
	}
	if out.CreatedByAdminID == "" {
		out.CreatedByAdminID = "migration"
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = now
	}
	return out
}

func (r *tenantRepo) Create(ctx context.Context, tenant *store.Tenant) error {
	t := normalizeTenantRecord(tenant)
	var deletedAt any
	if t.DeletedAt != nil {
		deletedAt = t.DeletedAt.Format(time.RFC3339)
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO tenants (id, slug, name, status, primary_domain, settings_json, created_by_admin_id, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, t.ID, t.Slug, t.Name, t.Status, t.PrimaryDomain, t.SettingsJSON, t.CreatedByAdminID, t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339), deletedAt)
}

func (r *tenantRepo) EnsureDefault(ctx context.Context) (*store.Tenant, error) {
	if existing, err := r.GetByID(ctx, store.DefaultTenantID); err != nil || existing != nil {
		return existing, err
	}
	t := normalizeTenantRecord(nil)
	if err := r.Create(ctx, &t); err != nil {
		if existing, getErr := r.GetByID(ctx, store.DefaultTenantID); getErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	return r.GetByID(ctx, store.DefaultTenantID)
}

func (r *tenantRepo) GetByID(ctx context.Context, id string) (*store.Tenant, error) {
	return r.getOne(ctx, `SELECT id, slug, name, status, primary_domain, settings_json, created_by_admin_id, created_at, updated_at, deleted_at FROM tenants WHERE id = ?`, id)
}

func (r *tenantRepo) GetBySlug(ctx context.Context, slug string) (*store.Tenant, error) {
	return r.getOne(ctx, `SELECT id, slug, name, status, primary_domain, settings_json, created_by_admin_id, created_at, updated_at, deleted_at FROM tenants WHERE slug = ?`, slug)
}

func (r *tenantRepo) getOne(ctx context.Context, query string, arg any) (*store.Tenant, error) {
	row := r.readDB.QueryRowContext(ctx, query, arg)
	var t store.Tenant
	var createdAt, updatedAt string
	var deletedAt sql.NullString
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.PrimaryDomain, &t.SettingsJSON, &t.CreatedByAdminID, &createdAt, &updatedAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	t.CreatedAt = mustParseTime(createdAt)
	t.UpdatedAt = mustParseTime(updatedAt)
	if deletedAt.Valid && deletedAt.String != "" {
		parsed := mustParseTime(deletedAt.String)
		t.DeletedAt = &parsed
	}
	return &t, nil
}

func (r *tenantRepo) List(ctx context.Context) ([]*store.Tenant, error) {
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, slug, name, status, primary_domain, settings_json, created_by_admin_id, created_at, updated_at, deleted_at FROM tenants ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*store.Tenant
	for rows.Next() {
		var t store.Tenant
		var createdAt, updatedAt string
		var deletedAt sql.NullString
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.PrimaryDomain, &t.SettingsJSON, &t.CreatedByAdminID, &createdAt, &updatedAt, &deletedAt); err != nil {
			return nil, err
		}
		t.CreatedAt = mustParseTime(createdAt)
		t.UpdatedAt = mustParseTime(updatedAt)
		if deletedAt.Valid && deletedAt.String != "" {
			parsed := mustParseTime(deletedAt.String)
			t.DeletedAt = &parsed
		}
		items = append(items, &t)
	}
	return items, rows.Err()
}
