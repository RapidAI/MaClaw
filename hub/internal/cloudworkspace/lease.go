package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	LeaseTTL = 90 * time.Second

	AcquiredGranted = "granted"
	AcquiredRenewed = "renewed"

	leaseIDPrefix = "cwl_"
	leaseCols     = `id, workspace_id, tenant_id, user_id, machine_id, machine_name, heartbeat_at, expires_at, released_at, stolen_by, created_at`
)

// Lease is one cloud_workspace_leases row.
type Lease struct {
	ID          string
	WorkspaceID string
	TenantID    string
	UserID      string
	MachineID   string
	MachineName string
	HeartbeatAt string
	ExpiresAt   string
	ReleasedAt  string
	StolenBy    string
	CreatedAt   string
}

// AcquireParams is the input for exclusive lease grant/renew/steal.
type AcquireParams struct {
	TenantID    string
	UserID      string
	WorkspaceID string
	MachineID   string
	Force       bool
}

// AcquireOutcome is POST /leases 200 body.
type AcquireOutcome struct {
	LeaseID   string `json:"lease_id"`
	ExpiresAt string `json:"expires_at"`
	Acquired  string `json:"acquired"`
}

// InUseError is 409 CLOUD_WORKSPACE_IN_USE.
type InUseError struct {
	HolderMachineID   string
	HolderMachineName string
	ExpiresAt         string
}

func (e *InUseError) Error() string {
	return ErrInUse.Error()
}

func (e *InUseError) Unwrap() error {
	return ErrInUse
}

func newInUseError(lease *Lease) *InUseError {
	if lease == nil {
		return &InUseError{}
	}
	return &InUseError{
		HolderMachineID:   lease.MachineID,
		HolderMachineName: lease.MachineName,
		ExpiresAt:         lease.ExpiresAt,
	}
}

func newLeaseID() string {
	return leaseIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func leaseExpired(expiresAt string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(expiresAt))
	if err != nil {
		return true
	}
	return !t.UTC().After(now.UTC())
}

func leaseExpiry(now time.Time) (heartbeatAt, expiresAt string) {
	now = now.UTC()
	return now.Format(time.RFC3339), now.Add(LeaseTTL).Format(time.RFC3339)
}

func scanLease(scanner interface{ Scan(dest ...any) error }) (*Lease, error) {
	var (
		lease    Lease
		released sql.NullString
	)
	if err := scanner.Scan(
		&lease.ID, &lease.WorkspaceID, &lease.TenantID, &lease.UserID, &lease.MachineID, &lease.MachineName,
		&lease.HeartbeatAt, &lease.ExpiresAt, &released, &lease.StolenBy, &lease.CreatedAt,
	); err != nil {
		return nil, err
	}
	if released.Valid {
		lease.ReleasedAt = released.String
	}
	return &lease, nil
}

func lookupMachineName(ctx context.Context, q queryer, machineID string) string {
	var hostname, name sql.NullString
	err := q.QueryRowContext(ctx, `SELECT hostname, name FROM machines WHERE id = ?`, machineID).Scan(&hostname, &name)
	if err != nil {
		return ""
	}
	if host := strings.TrimSpace(hostname.String); host != "" {
		return host
	}
	return strings.TrimSpace(name.String)
}

func getActiveLease(ctx context.Context, q queryer, workspaceID string) (*Lease, error) {
	lease, err := scanLease(q.QueryRowContext(ctx,
		`SELECT `+leaseCols+` FROM cloud_workspace_leases WHERE workspace_id = ? AND released_at IS NULL`,
		workspaceID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func getLeaseByID(ctx context.Context, q queryer, tenantID, userID, workspaceID, leaseID string) (*Lease, error) {
	lease, err := scanLease(q.QueryRowContext(ctx,
		`SELECT `+leaseCols+` FROM cloud_workspace_leases WHERE id = ? AND workspace_id = ? AND tenant_id = ? AND user_id = ?`,
		leaseID, workspaceID, tenantID, userID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func insertLease(ctx context.Context, q queryer, lease *Lease) error {
	_, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_leases (
		id, workspace_id, tenant_id, user_id, machine_id, machine_name,
		heartbeat_at, expires_at, released_at, stolen_by, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, '', ?)`,
		lease.ID, lease.WorkspaceID, lease.TenantID, lease.UserID,
		lease.MachineID, lease.MachineName, lease.HeartbeatAt, lease.ExpiresAt, lease.CreatedAt,
	)
	return err
}

func releaseLease(ctx context.Context, q queryer, id, releasedAt, stolenBy string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE cloud_workspace_leases SET released_at = ?, stolen_by = ? WHERE id = ? AND released_at IS NULL`,
		releasedAt, stolenBy, id,
	)
	return err
}

func grantLease(p AcquireParams, machineName string, now time.Time) *Lease {
	heartbeatAt, expiresAt := leaseExpiry(now)
	ts := now.UTC().Format(time.RFC3339)
	return &Lease{
		ID:          newLeaseID(),
		WorkspaceID: p.WorkspaceID,
		TenantID:    p.TenantID,
		UserID:      p.UserID,
		MachineID:   p.MachineID,
		MachineName: machineName,
		HeartbeatAt: heartbeatAt,
		ExpiresAt:   expiresAt,
		CreatedAt:   ts,
	}
}

func acquireOutcome(lease *Lease, acquired string) *AcquireOutcome {
	return &AcquireOutcome{
		LeaseID:   lease.ID,
		ExpiresAt: lease.ExpiresAt,
		Acquired:  acquired,
	}
}

func conflictFromActive(ctx context.Context, q queryer, workspaceID string) error {
	lease, err := getActiveLease(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	return newInUseError(lease)
}

// Acquire grants, renews, or steals the exclusive workspace lease in one IMMEDIATE tx.
func (s *Store) Acquire(ctx context.Context, p AcquireParams, now time.Time) (*AcquireOutcome, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	p.TenantID = store.NormalizeTenantID(p.TenantID)
	p.UserID = strings.TrimSpace(p.UserID)
	p.WorkspaceID = strings.TrimSpace(p.WorkspaceID)
	p.MachineID = strings.TrimSpace(p.MachineID)
	if p.UserID == "" || p.WorkspaceID == "" || p.MachineID == "" {
		return nil, ErrNotFound
	}
	var out *AcquireOutcome
	err := s.withImmediate(ctx, func(q queryer) error {
		ws, err := getOwned(ctx, q, p.TenantID, p.UserID, p.WorkspaceID)
		if err != nil {
			return err
		}
		if ws.Status != StatusActive {
			return ErrNotFound
		}
		current, err := getActiveLease(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		machineName := lookupMachineName(ctx, q, p.MachineID)
		heartbeatAt, expiresAt := leaseExpiry(now)
		if current == nil {
			lease := grantLease(p, machineName, now)
			if err := insertLease(ctx, q, lease); err != nil {
				if isUniqueConstraintError(err) {
					return conflictFromActive(ctx, q, p.WorkspaceID)
				}
				return err
			}
			out = acquireOutcome(lease, AcquiredGranted)
			return nil
		}
		if current.MachineID == p.MachineID {
			if _, err := q.ExecContext(ctx,
				`UPDATE cloud_workspace_leases SET heartbeat_at = ?, expires_at = ?, machine_name = ? WHERE id = ? AND released_at IS NULL`,
				heartbeatAt, expiresAt, machineName, current.ID,
			); err != nil {
				return err
			}
			current.HeartbeatAt = heartbeatAt
			current.ExpiresAt = expiresAt
			current.MachineName = machineName
			out = acquireOutcome(current, AcquiredRenewed)
			return nil
		}
		if leaseExpired(current.ExpiresAt, now) || p.Force {
			ts := now.UTC().Format(time.RFC3339)
			if err := releaseLease(ctx, q, current.ID, ts, p.MachineID); err != nil {
				return err
			}
			lease := grantLease(p, machineName, now)
			if err := insertLease(ctx, q, lease); err != nil {
				if isUniqueConstraintError(err) {
					return conflictFromActive(ctx, q, p.WorkspaceID)
				}
				return err
			}
			out = acquireOutcome(lease, AcquiredGranted)
			return nil
		}
		return newInUseError(current)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Heartbeat extends an exclusive lease held by this machine.
func (s *Store) Heartbeat(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID string, now time.Time) (*AcquireOutcome, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	leaseID = strings.TrimSpace(leaseID)
	machineID = strings.TrimSpace(machineID)
	var out *AcquireOutcome
	err := s.withImmediate(ctx, func(q queryer) error {
		lease, err := getLeaseByID(ctx, q, tenantID, userID, workspaceID, leaseID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return conflictFromActive(ctx, q, workspaceID)
			}
			return err
		}
		if lease.ReleasedAt != "" || strings.TrimSpace(lease.StolenBy) != "" || lease.MachineID != machineID {
			return conflictFromActive(ctx, q, workspaceID)
		}
		heartbeatAt, expiresAt := leaseExpiry(now)
		if _, err := q.ExecContext(ctx,
			`UPDATE cloud_workspace_leases SET heartbeat_at = ?, expires_at = ? WHERE id = ? AND released_at IS NULL AND stolen_by = ''`,
			heartbeatAt, expiresAt, lease.ID,
		); err != nil {
			return err
		}
		out = &AcquireOutcome{LeaseID: lease.ID, ExpiresAt: expiresAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Release marks the caller's exclusive lease released.
func (s *Store) Release(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID string, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	leaseID = strings.TrimSpace(leaseID)
	machineID = strings.TrimSpace(machineID)
	ts := now.UTC().Format(time.RFC3339)
	return s.withImmediate(ctx, func(q queryer) error {
		lease, err := getLeaseByID(ctx, q, tenantID, userID, workspaceID, leaseID)
		if err != nil {
			return err
		}
		if lease.ReleasedAt != "" {
			return ErrNotFound
		}
		if lease.MachineID != machineID {
			return newInUseError(lease)
		}
		return releaseLease(ctx, q, lease.ID, ts, "")
	})
}

// ListActiveLeases returns unreleased leases for the user, keyed by workspace ID.
func (s *Store) ListActiveLeases(ctx context.Context, tenantID, userID string) (map[string]*Lease, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+leaseCols+` FROM cloud_workspace_leases WHERE tenant_id = ? AND user_id = ? AND released_at IS NULL`,
		tenantID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*Lease{}
	for rows.Next() {
		lease, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		out[lease.WorkspaceID] = lease
	}
	return out, rows.Err()
}

// AcquireLease grants, renews, or steals the exclusive lease for an owned workspace.
func (s *Service) AcquireLease(ctx context.Context, principal auth.MachinePrincipal, workspaceID string, force bool) (*AcquireOutcome, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.Acquire(ctx, AcquireParams{
		TenantID:    principal.TenantID,
		UserID:      principal.UserID,
		WorkspaceID: workspaceID,
		MachineID:   principal.MachineID,
		Force:       force,
	}, s.now())
}

// HeartbeatLease extends a lease held by this machine.
func (s *Service) HeartbeatLease(ctx context.Context, principal auth.MachinePrincipal, workspaceID, leaseID string) (*AcquireOutcome, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.Heartbeat(ctx, principal.TenantID, principal.UserID, workspaceID, leaseID, principal.MachineID, s.now())
}

// ReleaseLease releases a lease held by this machine.
func (s *Service) ReleaseLease(ctx context.Context, principal auth.MachinePrincipal, workspaceID, leaseID string) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	return s.Workspaces.Release(ctx, principal.TenantID, principal.UserID, workspaceID, leaseID, principal.MachineID, s.now())
}
