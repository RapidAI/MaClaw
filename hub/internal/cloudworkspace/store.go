package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const workspaceCols = `id, tenant_id, user_id, name, name_norm, status, used_bytes, file_count, manifest_revision, created_at, updated_at, deleted_at`

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Store persists cloud_workspaces rows.
type Store struct {
	db *sql.DB
}

// NewStore wraps a Hub SQLite handle.
func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

// CreateParams is the input for a quota-checked insert.
type CreateParams struct {
	TenantID            string
	UserID              string
	Name                string
	Quota               int
	TenantMaxTotalBytes int64
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func (s *Store) withImmediate(ctx context.Context, fn func(queryer) error) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// SQLite-only: never fall back to DEFERRED. A second BeginTx while this
	// Conn is held would block on MaxWriteOpenConns=1; DEFERRED would also
	// re-open the COUNT+INSERT race IMMEDIATE is meant to close.
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func scanWorkspace(scanner interface{ Scan(dest ...any) error }) (*Workspace, error) {
	var (
		ws      Workspace
		deleted sql.NullString
	)
	if err := scanner.Scan(
		&ws.ID, &ws.TenantID, &ws.UserID, &ws.Name, &ws.NameNorm, &ws.Status,
		&ws.UsedBytes, &ws.FileCount, &ws.ManifestRevision, &ws.CreatedAt, &ws.UpdatedAt, &deleted,
	); err != nil {
		return nil, err
	}
	if deleted.Valid {
		ws.DeletedAt = deleted.String
	}
	return &ws, nil
}

func countActive(ctx context.Context, q queryer, tenantID, userID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cloud_workspaces WHERE tenant_id = ? AND user_id = ? AND status = ?`,
		tenantID, userID, StatusActive,
	).Scan(&n)
	return n, err
}

func tenantUsedBytes(ctx context.Context, q queryer, tenantID string) (int64, error) {
	var n sql.NullInt64
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(used_bytes), 0) FROM cloud_workspaces WHERE tenant_id = ?`,
		tenantID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	if n.Valid {
		return n.Int64, nil
	}
	return 0, nil
}

func nameTaken(ctx context.Context, q queryer, tenantID, userID, nameNorm, excludeID string) (bool, error) {
	query := `SELECT 1 FROM cloud_workspaces WHERE tenant_id = ? AND user_id = ? AND name_norm = ? AND status != ?`
	args := []any{tenantID, userID, nameNorm, StatusDeleted}
	if excludeID != "" {
		query += ` AND id != ?`
		args = append(args, excludeID)
	}
	query += ` LIMIT 1`
	var one int
	err := q.QueryRowContext(ctx, query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func listNonDeletedNames(ctx context.Context, q queryer, tenantID, userID string) ([]string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT name FROM cloud_workspaces WHERE tenant_id = ? AND user_id = ? AND status != ?`,
		tenantID, userID, StatusDeleted,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func getOwned(ctx context.Context, q queryer, tenantID, userID, id string) (*Workspace, error) {
	ws, err := scanWorkspace(q.QueryRowContext(ctx,
		`SELECT `+workspaceCols+` FROM cloud_workspaces WHERE id = ? AND tenant_id = ? AND user_id = ?`,
		id, tenantID, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return ws, nil
}

// Create inserts status=active in one BEGIN IMMEDIATE COUNT+INSERT.
func (s *Store) Create(ctx context.Context, p CreateParams, now time.Time) (*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID := store.NormalizeTenantID(p.TenantID)
	userID := strings.TrimSpace(p.UserID)
	if userID == "" {
		return nil, ErrNotFound
	}
	quota := p.Quota
	if quota < 1 {
		quota = 1
	}
	ts := now.UTC().Format(time.RFC3339)
	var created *Workspace
	err := s.withImmediate(ctx, func(q queryer) error {
		n, err := countActive(ctx, q, tenantID, userID)
		if err != nil {
			return err
		}
		if n >= quota {
			return ErrQuota
		}
		used, err := tenantUsedBytes(ctx, q, tenantID)
		if err != nil {
			return err
		}
		if p.TenantMaxTotalBytes > 0 && used >= p.TenantMaxTotalBytes {
			return ErrTenantDisk
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			existing, err := listNonDeletedNames(ctx, q, tenantID, userID)
			if err != nil {
				return err
			}
			name = nextDefaultName(existing)
		}
		name, err = validateDisplayName(name)
		if err != nil {
			return err
		}
		nameNorm := normalizeName(name)
		taken, err := nameTaken(ctx, q, tenantID, userID, nameNorm, "")
		if err != nil {
			return err
		}
		if taken {
			return ErrNameTaken
		}
		ws := &Workspace{
			ID:        newWorkspaceID(),
			TenantID:  tenantID,
			UserID:    userID,
			Name:      name,
			NameNorm:  nameNorm,
			Status:    StatusActive,
			CreatedAt: ts,
			UpdatedAt: ts,
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspaces (
			id, tenant_id, user_id, name, name_norm, status, used_bytes, file_count, manifest_revision, created_at, updated_at, deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, 0, 0, '', ?, ?, NULL)`,
			ws.ID, ws.TenantID, ws.UserID, ws.Name, ws.NameNorm, ws.Status, ws.CreatedAt, ws.UpdatedAt,
		); err != nil {
			if isUniqueConstraintError(err) {
				return ErrNameTaken
			}
			return err
		}
		created = ws
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// GetOwned returns a workspace owned by tenant+user, or ErrNotFound.
func (s *Store) GetOwned(ctx context.Context, tenantID, userID, id string) (*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	return getOwned(ctx, s.db, store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), strings.TrimSpace(id))
}

// ListOwned returns the user's workspaces (active and deleted), oldest first.
func (s *Store) ListOwned(ctx context.Context, tenantID, userID string) ([]*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+workspaceCols+` FROM cloud_workspaces WHERE tenant_id = ? AND user_id = ? ORDER BY created_at ASC, id ASC`,
		tenantID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Workspace{}
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

// Rename updates an active owned workspace's display name.
func (s *Store) Rename(ctx context.Context, tenantID, userID, id, name, nameNorm string, now time.Time) (*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	id = strings.TrimSpace(id)
	ts := now.UTC().Format(time.RFC3339)
	var out *Workspace
	err := s.withImmediate(ctx, func(q queryer) error {
		ws, err := getOwned(ctx, q, tenantID, userID, id)
		if err != nil {
			return err
		}
		if ws.Status != StatusActive {
			return ErrNotFound
		}
		taken, err := nameTaken(ctx, q, tenantID, userID, nameNorm, id)
		if err != nil {
			return err
		}
		if taken {
			return ErrNameTaken
		}
		if _, err := q.ExecContext(ctx,
			`UPDATE cloud_workspaces SET name = ?, name_norm = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`,
			name, nameNorm, ts, id, tenantID, userID, StatusActive,
		); err != nil {
			if isUniqueConstraintError(err) {
				return ErrNameTaken
			}
			return err
		}
		ws.Name = name
		ws.NameNorm = nameNorm
		ws.UpdatedAt = ts
		out = ws
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SoftDelete marks an active owned workspace deleted.
// This machine's lease is released first; another machine's unexpired lease is 409.
func (s *Store) SoftDelete(ctx context.Context, tenantID, userID, machineID, id string, now time.Time) (*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	machineID = strings.TrimSpace(machineID)
	id = strings.TrimSpace(id)
	ts := now.UTC().Format(time.RFC3339)
	var out *Workspace
	err := s.withImmediate(ctx, func(q queryer) error {
		ws, err := getOwned(ctx, q, tenantID, userID, id)
		if err != nil {
			return err
		}
		if ws.Status != StatusActive {
			return ErrNotFound
		}
		lease, err := getActiveLease(ctx, q, id)
		if err != nil {
			return err
		}
		if lease != nil {
			if lease.MachineID != machineID && !leaseExpired(lease.ExpiresAt, now) {
				return newInUseError(lease)
			}
			if err := releaseLease(ctx, q, lease.ID, ts, ""); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx,
			`UPDATE cloud_workspaces SET status = ?, deleted_at = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`,
			StatusDeleted, ts, ts, id, tenantID, userID, StatusActive,
		); err != nil {
			return err
		}
		ws.Status = StatusDeleted
		ws.DeletedAt = ts
		ws.UpdatedAt = ts
		out = ws
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Restore undeletes a workspace within the 7-day window if quota allows.
func (s *Store) Restore(ctx context.Context, tenantID, userID, id string, quota int, now time.Time) (*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	id = strings.TrimSpace(id)
	if quota < 1 {
		quota = 1
	}
	ts := now.UTC().Format(time.RFC3339)
	var out *Workspace
	err := s.withImmediate(ctx, func(q queryer) error {
		ws, err := getOwned(ctx, q, tenantID, userID, id)
		if err != nil {
			return err
		}
		if ws.Status != StatusDeleted {
			return ErrNotFound
		}
		deadline, ok := restoreDeadline(ws.DeletedAt)
		if !ok || !now.UTC().Before(deadline) {
			return ErrRestoreWindow
		}
		n, err := countActive(ctx, q, tenantID, userID)
		if err != nil {
			return err
		}
		if n >= quota {
			return ErrQuota
		}
		taken, err := nameTaken(ctx, q, tenantID, userID, ws.NameNorm, id)
		if err != nil {
			return err
		}
		if taken {
			return ErrNameTaken
		}
		if _, err := q.ExecContext(ctx,
			`UPDATE cloud_workspaces SET status = ?, deleted_at = NULL, updated_at = ? WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`,
			StatusActive, ts, id, tenantID, userID, StatusDeleted,
		); err != nil {
			if isUniqueConstraintError(err) {
				return ErrNameTaken
			}
			return err
		}
		ws.Status = StatusActive
		ws.DeletedAt = ""
		ws.UpdatedAt = ts
		out = ws
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CountActive returns the user's live workspace count.
func (s *Store) CountActive(ctx context.Context, tenantID, userID string) (int, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	return countActive(ctx, s.db, store.NormalizeTenantID(tenantID), strings.TrimSpace(userID))
}

// HardDeleteDeleted permanently removes an owned soft-deleted workspace.
func (s *Store) HardDeleteDeleted(ctx context.Context, tenantID, userID, id string) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	tenantID, userID, id = store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), strings.TrimSpace(id)
	return s.withImmediate(ctx, func(q queryer) error {
		ws, err := getOwned(ctx, q, tenantID, userID, id)
		if err != nil {
			return err
		}
		if ws.Status != StatusDeleted {
			return ErrNotFound
		}
		for _, stmt := range []string{
			`DELETE FROM cloud_workspace_manifest_entries WHERE workspace_id = ?`,
			`DELETE FROM cloud_workspace_objects WHERE workspace_id = ?`,
			`DELETE FROM cloud_workspace_leases WHERE workspace_id = ?`,
			`DELETE FROM cloud_workspaces WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`,
		} {
			args := []any{id}
			if strings.Contains(stmt, "cloud_workspaces WHERE") {
				args = []any{id, tenantID, userID, StatusDeleted}
			}
			if _, err := q.ExecContext(ctx, stmt, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// TenantUsedBytes sums used_bytes across all statuses, including deleted.
func (s *Store) TenantUsedBytes(ctx context.Context, tenantID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	return tenantUsedBytes(ctx, s.db, store.NormalizeTenantID(tenantID))
}

// ListOverQuotaUsers returns users whose active count exceeds quota.
func (s *Store) ListOverQuotaUsers(ctx context.Context, tenantID string, quota int) ([]OverQuotaUser, error) {
	if s == nil || s.db == nil {
		return []OverQuotaUser{}, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	if quota < 1 {
		quota = 1
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(u.sn, ''), COUNT(*)
		FROM cloud_workspaces w
		LEFT JOIN users u ON u.id = w.user_id AND u.tenant_id = w.tenant_id
		WHERE w.tenant_id = ? AND w.status = ?
		GROUP BY w.user_id
		HAVING COUNT(*) > ?
		ORDER BY COUNT(*) DESC, COALESCE(u.sn, '') ASC`,
		tenantID, StatusActive, quota,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OverQuotaUser{}
	for rows.Next() {
		var item OverQuotaUser
		if err := rows.Scan(&item.SN, &item.Used); err != nil {
			return nil, err
		}
		item.Quota = quota
		out = append(out, item)
	}
	return out, rows.Err()
}
