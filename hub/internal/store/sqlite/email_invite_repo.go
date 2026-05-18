package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func (r *emailInviteRepo) Create(ctx context.Context, item *store.EmailInvite) error {
	if item.TenantID == "" {
		item.TenantID = store.DefaultTenantID
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO email_invites (id, tenant_id, email, role, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.TenantID, item.Email, item.Role, item.Status,
		item.CreatedAt.Format(time.RFC3339),
		item.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *emailInviteRepo) List(ctx context.Context) ([]*store.EmailInvite, error) {
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, tenant_id, email, role, status, created_at, updated_at FROM email_invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEmailInvites(rows)
}

func (r *emailInviteRepo) ListByTenant(ctx context.Context, tenantID string) ([]*store.EmailInvite, error) {
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, tenant_id, email, role, status, created_at, updated_at FROM email_invites WHERE tenant_id = ? ORDER BY created_at DESC`, normalizeTenantID(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEmailInvites(rows)
}

func (r *emailInviteRepo) GetByID(ctx context.Context, id string) (*store.EmailInvite, error) {
	row := r.readDB.QueryRowContext(ctx,
		`SELECT id, tenant_id, email, role, status, created_at, updated_at FROM email_invites WHERE id = ?`, id)
	var item store.EmailInvite
	var createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.TenantID, &item.Email, &item.Role, &item.Status, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	return &item, nil
}

func (r *emailInviteRepo) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM email_invites WHERE id = ?`, id)
	return err
}

func scanEmailInvites(rows *sql.Rows) ([]*store.EmailInvite, error) {
	var result []*store.EmailInvite
	for rows.Next() {
		var item store.EmailInvite
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Email, &item.Role, &item.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if item.TenantID == "" {
			item.TenantID = store.DefaultTenantID
		}
		item.CreatedAt = mustParseTime(createdAt)
		item.UpdatedAt = mustParseTime(updatedAt)
		result = append(result, &item)
	}
	return result, rows.Err()
}
