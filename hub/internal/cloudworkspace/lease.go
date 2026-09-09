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
	leaseCols     = `id, workspace_id, tenant_id, user_id, machine_id, machine_name, heartbeat_at, expires_at, released_at, stolen_by, created_at, client_instance_id, fencing_token, lease_state, last_committed_revision, handoff_requested_at, handoff_requested_by`
)

// Lease is one cloud_workspace_leases row.
type Lease struct {
	ID                    string
	WorkspaceID           string
	TenantID              string
	UserID                string
	MachineID             string
	MachineName           string
	HeartbeatAt           string
	ExpiresAt             string
	ReleasedAt            string
	StolenBy              string
	CreatedAt             string
	ClientInstanceID      string
	FencingToken          int64
	LeaseState            string
	LastCommittedRevision string
	HandoffRequestedAt    string
	HandoffRequestedBy    string
}

// AcquireParams is the input for exclusive lease grant/renew/steal.
type AcquireParams struct {
	TenantID         string
	UserID           string
	WorkspaceID      string
	MachineID        string
	Force            bool
	ClientInstanceID string
}

// AcquireOutcome is POST /leases 200 body.
type AcquireOutcome struct {
	LeaseID          string `json:"lease_id"`
	ExpiresAt        string `json:"expires_at"`
	Acquired         string `json:"acquired"`
	ClientInstanceID string `json:"client_instance_id,omitempty"`
	FencingToken     int64  `json:"fencing_token,omitempty"`
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
		lease     Lease
		released  sql.NullString
		handoff   sql.NullString
		handoffBy sql.NullString
	)
	if err := scanner.Scan(
		&lease.ID, &lease.WorkspaceID, &lease.TenantID, &lease.UserID, &lease.MachineID, &lease.MachineName,
		&lease.HeartbeatAt, &lease.ExpiresAt, &released, &lease.StolenBy, &lease.CreatedAt,
		&lease.ClientInstanceID, &lease.FencingToken, &lease.LeaseState, &lease.LastCommittedRevision, &handoff, &handoffBy,
	); err != nil {
		return nil, err
	}
	if released.Valid {
		lease.ReleasedAt = released.String
	}
	if handoff.Valid {
		lease.HandoffRequestedAt = handoff.String
	}
	if handoffBy.Valid {
		lease.HandoffRequestedBy = handoffBy.String
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
		heartbeat_at, expires_at, released_at, stolen_by, created_at,
		client_instance_id, fencing_token, lease_state, last_committed_revision, handoff_requested_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, '', ?, ?, ?, 'active', '', NULL)`,
		lease.ID, lease.WorkspaceID, lease.TenantID, lease.UserID,
		lease.MachineID, lease.MachineName, lease.HeartbeatAt, lease.ExpiresAt, lease.CreatedAt,
		lease.ClientInstanceID, lease.FencingToken,
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

func grantLease(p AcquireParams, machineName string, fencingToken int64, now time.Time) *Lease {
	heartbeatAt, expiresAt := leaseExpiry(now)
	ts := now.UTC().Format(time.RFC3339)
	return &Lease{
		ID:               newLeaseID(),
		WorkspaceID:      p.WorkspaceID,
		TenantID:         p.TenantID,
		UserID:           p.UserID,
		MachineID:        p.MachineID,
		MachineName:      machineName,
		HeartbeatAt:      heartbeatAt,
		ExpiresAt:        expiresAt,
		CreatedAt:        ts,
		ClientInstanceID: strings.TrimSpace(p.ClientInstanceID),
		FencingToken:     fencingToken,
		LeaseState:       "active",
	}
}

func acquireOutcome(lease *Lease, acquired string) *AcquireOutcome {
	return &AcquireOutcome{
		LeaseID:          lease.ID,
		ExpiresAt:        lease.ExpiresAt,
		Acquired:         acquired,
		ClientInstanceID: lease.ClientInstanceID,
		FencingToken:     lease.FencingToken,
	}
}

func conflictFromActive(ctx context.Context, q queryer, workspaceID string) error {
	lease, err := getActiveLease(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	return newInUseError(lease)
}

func nextFencingToken(ctx context.Context, q queryer, workspaceID string) (int64, error) {
	var token sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(fencing_token), 0) + 1 FROM cloud_workspace_leases WHERE workspace_id = ?`, workspaceID).Scan(&token); err != nil {
		return 0, err
	}
	if !token.Valid || token.Int64 <= 0 {
		return 1, nil
	}
	return token.Int64, nil
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
	p.ClientInstanceID = strings.TrimSpace(p.ClientInstanceID)
	if p.ClientInstanceID == "" {
		// Older clients have no process session identifier. Machine ID remains a
		// compatibility fallback, while new clients can fence individual sessions.
		p.ClientInstanceID = p.MachineID
	}
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
			token, tokenErr := nextFencingToken(ctx, q, p.WorkspaceID)
			if tokenErr != nil {
				return tokenErr
			}
			lease := grantLease(p, machineName, token, now)
			if err := insertLease(ctx, q, lease); err != nil {
				if isUniqueConstraintError(err) {
					return conflictFromActive(ctx, q, p.WorkspaceID)
				}
				return err
			}
			out = acquireOutcome(lease, AcquiredGranted)
			return stageAtomicIdempotency(ctx, q, out)
		}
		if !leaseExpired(current.ExpiresAt, now) && current.MachineID == p.MachineID && (strings.TrimSpace(current.ClientInstanceID) == "" || current.ClientInstanceID == p.ClientInstanceID) {
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
			return stageAtomicIdempotency(ctx, q, out)
		}
		if leaseExpired(current.ExpiresAt, now) || p.Force {
			ts := now.UTC().Format(time.RFC3339)
			if err := releaseLease(ctx, q, current.ID, ts, p.MachineID); err != nil {
				return err
			}
			token, tokenErr := nextFencingToken(ctx, q, p.WorkspaceID)
			if tokenErr != nil {
				return tokenErr
			}
			lease := grantLease(p, machineName, token, now)
			if err := insertLease(ctx, q, lease); err != nil {
				if isUniqueConstraintError(err) {
					return conflictFromActive(ctx, q, p.WorkspaceID)
				}
				return err
			}
			out = acquireOutcome(lease, AcquiredGranted)
			return stageAtomicIdempotency(ctx, q, out)
		}
		return newInUseError(current)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RequestHandoff records that another device is waiting for the current
// writer to release the workspace. It never changes lease ownership and is
// safe to call repeatedly; the active lease holder can observe the timestamp
// through entitlement polling and finish its flush/release flow.
func (s *Store) RequestHandoff(ctx context.Context, tenantID, userID, workspaceID string, now time.Time) (*Lease, error) {
	return s.RequestHandoffWithSession(ctx, tenantID, userID, workspaceID, "", "", now)
}

func (s *Store) RequestHandoffWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, now time.Time) (*Lease, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID, workspaceID = strings.TrimSpace(userID), strings.TrimSpace(workspaceID)
	if userID == "" || workspaceID == "" {
		return nil, ErrNotFound
	}
	var out *Lease
	err := s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID); err != nil {
			return err
		}
		lease, err := getActiveLease(ctx, q, workspaceID)
		if err != nil {
			return err
		}
		if lease == nil {
			return ErrNotFound
		}
		ts := now.UTC().Format(time.RFC3339)
		requestedBy := strings.TrimSpace(clientInstanceID)
		if requestedBy == "" {
			requestedBy = strings.TrimSpace(machineID)
		}
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_leases SET handoff_requested_at = ?, handoff_requested_by = ? WHERE id = ? AND released_at IS NULL`, ts, requestedBy, lease.ID); err != nil {
			return err
		}
		lease.HandoffRequestedAt = ts
		lease.HandoffRequestedBy = requestedBy
		out = lease
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Heartbeat extends an exclusive lease held by this machine.
func (s *Store) Heartbeat(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID string, now time.Time) (*AcquireOutcome, error) {
	return s.HeartbeatWithSession(ctx, tenantID, userID, workspaceID, leaseID, machineID, "", now)
}

func (s *Store) HeartbeatWithSession(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID string, now time.Time) (*AcquireOutcome, error) {
	return s.HeartbeatWithSessionAndToken(ctx, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID, 0, now)
}

func (s *Store) HeartbeatWithSessionAndToken(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID string, fencingToken int64, now time.Time) (*AcquireOutcome, error) {
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
		// An expired lease is no longer renewable, even when the old process is
		// still running and presents the same machine/session identity.  Without
		// this check a writer that was partitioned from the Hub could reconnect
		// just after TTL and resurrect its fencing epoch, racing a takeover.
		if leaseExpired(lease.ExpiresAt, now) {
			if strings.TrimSpace(clientInstanceID) != "" || fencingToken > 0 {
				return ErrFenced
			}
			return ErrLeaseRequired
		}
		if lease.ReleasedAt != "" || strings.TrimSpace(lease.StolenBy) != "" || lease.MachineID != machineID || (strings.TrimSpace(clientInstanceID) != "" && strings.TrimSpace(lease.ClientInstanceID) != "" && lease.ClientInstanceID != clientInstanceID) {
			return conflictFromActive(ctx, q, workspaceID)
		}
		if strings.TrimSpace(clientInstanceID) != "" && lease.FencingToken > 0 && fencingToken <= 0 {
			return ErrFenced
		}
		if fencingToken > 0 && lease.FencingToken > 0 && lease.FencingToken != fencingToken {
			return ErrFenced
		}
		heartbeatAt, expiresAt := leaseExpiry(now)
		if _, err := q.ExecContext(ctx,
			`UPDATE cloud_workspace_leases SET heartbeat_at = ?, expires_at = ? WHERE id = ? AND released_at IS NULL AND stolen_by = ''`,
			heartbeatAt, expiresAt, lease.ID,
		); err != nil {
			return err
		}
		out = &AcquireOutcome{LeaseID: lease.ID, ExpiresAt: expiresAt, ClientInstanceID: lease.ClientInstanceID, FencingToken: lease.FencingToken}
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Release marks the caller's exclusive lease released.
func (s *Store) Release(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID string, now time.Time) error {
	return s.ReleaseWithSession(ctx, tenantID, userID, workspaceID, leaseID, machineID, "", now)
}

func (s *Store) ReleaseWithSession(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID string, now time.Time) error {
	return s.ReleaseWithSessionAndRevision(ctx, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID, "", now)
}

func (s *Store) ReleaseWithSessionAndRevision(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID, lastCommittedRevision string, now time.Time) error {
	return s.ReleaseWithSessionRevisionAndToken(ctx, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID, 0, lastCommittedRevision, now)
}

func (s *Store) ReleaseWithSessionRevisionAndToken(ctx context.Context, tenantID, userID, workspaceID, leaseID, machineID, clientInstanceID string, fencingToken int64, lastCommittedRevision string, now time.Time) error {
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
		if lease.MachineID != machineID || (strings.TrimSpace(clientInstanceID) != "" && strings.TrimSpace(lease.ClientInstanceID) != "" && lease.ClientInstanceID != clientInstanceID) {
			return newInUseError(lease)
		}
		if fencingToken > 0 && lease.FencingToken > 0 && lease.FencingToken != fencingToken {
			return ErrFenced
		}
		if expected := strings.TrimSpace(lastCommittedRevision); expected != "" {
			var current string
			if err := q.QueryRowContext(ctx, `SELECT manifest_revision FROM cloud_workspaces WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`, workspaceID, tenantID, userID, StatusActive).Scan(&current); err != nil {
				return err
			}
			if current != expected {
				return ErrRevisionConflict
			}
		}
		if err := releaseLease(ctx, q, lease.ID, ts, ""); err != nil {
			return err
		}
		return stageAtomicIdempotency(ctx, q, nil)
	})
}

func assertLeaseHeld(ctx context.Context, q queryer, workspaceID, machineID string, now time.Time) error {
	return assertLeaseHeldForSession(ctx, q, workspaceID, machineID, "", 0, now)
}

func assertLeaseHeldForSession(ctx context.Context, q queryer, workspaceID, machineID, clientInstanceID string, fencingToken int64, now time.Time) error {
	lease, err := getActiveLease(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	if lease == nil {
		return ErrLeaseRequired
	}
	if strings.TrimSpace(lease.MachineID) != strings.TrimSpace(machineID) || leaseExpired(lease.ExpiresAt, now) {
		// A caller presenting a session/fencing token is an old writer when a
		// different lease is now active (or the lease has expired). Return the
		// stronger FENCED signal so it cannot retry writes as if it merely
		// needed to reacquire the lease.
		if strings.TrimSpace(clientInstanceID) != "" || fencingToken > 0 {
			return ErrFenced
		}
		return ErrLeaseRequired
	}
	if strings.TrimSpace(clientInstanceID) != "" && strings.TrimSpace(lease.ClientInstanceID) != "" && strings.TrimSpace(lease.ClientInstanceID) != strings.TrimSpace(clientInstanceID) {
		return ErrFenced
	}
	if strings.TrimSpace(clientInstanceID) != "" && lease.FencingToken > 0 && fencingToken <= 0 {
		return ErrFenced
	}
	if fencingToken > 0 && lease.FencingToken > 0 && lease.FencingToken != fencingToken {
		return ErrFenced
	}
	return nil
}

func requireActiveOwned(ctx context.Context, q queryer, tenantID, userID, id string) (*Workspace, error) {
	ws, err := getOwned(ctx, q, tenantID, userID, id)
	if err != nil {
		return nil, err
	}
	if ws.Status != StatusActive {
		return nil, ErrNotFound
	}
	return ws, nil
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
		TenantID:         principal.TenantID,
		UserID:           principal.UserID,
		WorkspaceID:      workspaceID,
		MachineID:        principal.MachineID,
		ClientInstanceID: principal.ClientInstanceID,
		Force:            force,
	}, s.now())
}

// RequestLeaseHandoff records a waiting device's handoff request without
// granting it write access.
func (s *Service) RequestLeaseHandoff(ctx context.Context, principal auth.MachinePrincipal, workspaceID string) (*Lease, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.RequestHandoffWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, s.now())
}

// HeartbeatLease extends a lease held by this machine.
func (s *Service) HeartbeatLease(ctx context.Context, principal auth.MachinePrincipal, workspaceID, leaseID string) (*AcquireOutcome, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.HeartbeatWithSessionAndToken(ctx, principal.TenantID, principal.UserID, workspaceID, leaseID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, s.now())
}

// ReleaseLease releases a lease held by this machine.
func (s *Service) ReleaseLease(ctx context.Context, principal auth.MachinePrincipal, workspaceID, leaseID string) error {
	return s.ReleaseLeaseWithRevision(ctx, principal, workspaceID, leaseID, "")
}

func (s *Service) ReleaseLeaseWithRevision(ctx context.Context, principal auth.MachinePrincipal, workspaceID, leaseID, lastCommittedRevision string) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	return s.Workspaces.ReleaseWithSessionRevisionAndToken(ctx, principal.TenantID, principal.UserID, workspaceID, leaseID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, lastCommittedRevision, s.now())
}
