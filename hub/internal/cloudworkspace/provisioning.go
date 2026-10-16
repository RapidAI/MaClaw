package cloudworkspace

// Durable workspace/task provisioning.
//
// A local task is created by the GUI and therefore cannot participate in the
// Hub's SQLite transaction.  The Hub records the remote half as a small
// state-machine instead: provisioning creates the workspace and its unique
// task binding atomically, the GUI acknowledges completion after the local
// task/sidecar is durable, and a failed attempt is compensated by moving the
// workspace to the deleted state.  Retrying any phase is safe and observable
// through the operation status endpoint.

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
	ProvisionStateProvisioning = "provisioning"
	ProvisionStateActive       = "active"
	ProvisionStateDeleting     = "deleting"
	ProvisionStateFailed       = "failed"
	ProvisioningGrace          = 30 * time.Minute
)

// WorkspaceTaskProvision is the durable remote half of a workspace/task
// creation.  OperationID is stable across retries and is safe to expose to a
// client for status polling.
type WorkspaceTaskProvision struct {
	OperationID      string `json:"operation_id"`
	TenantID         string `json:"-"`
	UserID           string `json:"-"`
	ClientInstanceID string `json:"-"`
	WorkspaceID      string `json:"workspace_id"`
	CloudTaskID      string `json:"cloud_task_id"`
	DeviceTaskID     string `json:"device_task_id,omitempty"`
	Name             string `json:"name,omitempty"`
	Mode             string `json:"mode,omitempty"`
	Tag              string `json:"tag,omitempty"`
	State            string `json:"state"`
	LastError        string `json:"last_error,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// WorkspaceTaskProvisionParams is intentionally independent of an HTTP
// request.  It is also used by recovery jobs and tests.
type WorkspaceTaskProvisionParams struct {
	TenantID         string
	UserID           string
	Name             string
	CloudTaskID      string
	DeviceTaskID     string
	Mode             string
	Tag              string
	ClientInstanceID string
	// IdempotencyKey/PayloadHash are persisted on the durable operation in
	// addition to the generic ledger. This lets a retry recover an operation
	// when the original process crashed after the workspace transaction but
	// before the ledger response was finalized.
	IdempotencyKey         string
	IdempotencyPayloadHash string
	Quota                  int
	TenantMaxTotalBytes    int64
}

func newWorkspaceTaskProvisionID() string {
	return "cwop_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func scanWorkspaceTaskProvision(scanner interface{ Scan(dest ...any) error }) (*WorkspaceTaskProvision, error) {
	var out WorkspaceTaskProvision
	if err := scanner.Scan(
		&out.OperationID, &out.TenantID, &out.UserID, &out.ClientInstanceID, &out.WorkspaceID,
		&out.CloudTaskID, &out.DeviceTaskID, &out.Name, &out.Mode, &out.Tag,
		&out.State, &out.LastError, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

// BeginWorkspaceTaskProvision atomically creates a provisioning workspace,
// its 1:1 binding, and the operation row.  No writer lease is acquired here;
// the GUI acquires one while materialising the local task.
func (s *Store) BeginWorkspaceTaskProvision(ctx context.Context, p WorkspaceTaskProvisionParams, now time.Time) (*WorkspaceTaskProvision, error) {
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
	var out *WorkspaceTaskProvision
	err := s.withImmediate(ctx, func(q queryer) error {
		idempotencyKey := strings.TrimSpace(p.IdempotencyKey)
		idempotencyHash := strings.TrimSpace(p.IdempotencyPayloadHash)
		if idempotencyKey != "" {
			var existingID, existingHash string
			existingErr := q.QueryRowContext(ctx, `SELECT operation_id, idempotency_payload_hash FROM cloud_workspace_task_provisions WHERE tenant_id = ? AND user_id = ? AND idempotency_key = ?`, tenantID, userID, idempotencyKey).Scan(&existingID, &existingHash)
			if existingErr == nil {
				if idempotencyHash != "" && strings.TrimSpace(existingHash) != "" && strings.TrimSpace(existingHash) != idempotencyHash {
					return ErrIdempotencyKeyReused
				}
				row, scanErr := scanWorkspaceTaskProvision(q.QueryRowContext(ctx, `SELECT operation_id, tenant_id, user_id, client_instance_id, workspace_id,
					cloud_task_id, device_task_id, name, mode, tag, state, last_error, created_at, updated_at
					FROM cloud_workspace_task_provisions WHERE operation_id = ?`, existingID))
				if scanErr != nil {
					return scanErr
				}
				out = row
				return stageAtomicIdempotency(ctx, q, out)
			}
			if !errors.Is(existingErr, sql.ErrNoRows) {
				return existingErr
			}
		}
		n, err := countActive(ctx, q, tenantID, userID)
		if err != nil {
			return err
		}
		if n >= quota {
			return ErrQuota
		}
		usage, err := tenantRetainedUsage(ctx, q, tenantID)
		if err != nil {
			return err
		}
		if p.TenantMaxTotalBytes > 0 && usage.RetainedBytes >= p.TenantMaxTotalBytes {
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
		workspaceID := newWorkspaceID()
		cloudTaskID := strings.TrimSpace(p.CloudTaskID)
		stableTaskID := stableCloudTaskID(workspaceID)
		if cloudTaskID == "" {
			cloudTaskID = stableTaskID
		} else if cloudTaskID != stableTaskID {
			// The logical cloud task identity is server-owned. Accepting an
			// arbitrary client value would let two workspaces contend for one
			// binding or make a task appear to belong to another workspace.
			return ErrInvalidInput
		}
		// Keep the workspace active while the local side of the operation is
		// being materialised. Lease acquisition and sidecar writes must be
		// possible during this phase; the durable operation state is what tells
		// recovery/entitlement consumers that the task is not fully committed.
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspaces (
			id, tenant_id, user_id, name, name_norm, status, used_bytes, file_count,
			manifest_revision, created_at, updated_at, deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, 0, 0, '', ?, ?, NULL)`,
			workspaceID, tenantID, userID, name, nameNorm, StatusActive, ts, ts); err != nil {
			if isUniqueConstraintError(err) {
				return ErrNameTaken
			}
			return err
		}
		if _, err := upsertTaskBindingTx(ctx, q, tenantID, userID, workspaceID, cloudTaskID, p.DeviceTaskID, name, p.Mode, p.Tag, 0, ts); err != nil {
			return err
		}
		opID := newWorkspaceTaskProvisionID()
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_task_provisions (
			operation_id, tenant_id, user_id, client_instance_id, workspace_id, cloud_task_id, device_task_id,
			name, mode, tag, state, last_error, idempotency_key, idempotency_payload_hash, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
			opID, tenantID, userID, strings.TrimSpace(p.ClientInstanceID), workspaceID, cloudTaskID,
			strings.TrimSpace(p.DeviceTaskID), name, strings.TrimSpace(p.Mode), strings.TrimSpace(p.Tag),
			ProvisionStateProvisioning, idempotencyKey, idempotencyHash, ts, ts); err != nil {
			return err
		}
		out = &WorkspaceTaskProvision{
			OperationID: opID, WorkspaceID: workspaceID, CloudTaskID: cloudTaskID,
			DeviceTaskID: strings.TrimSpace(p.DeviceTaskID), Name: name,
			Mode: strings.TrimSpace(p.Mode), Tag: strings.TrimSpace(p.Tag),
			State: ProvisionStateProvisioning, CreatedAt: ts, UpdatedAt: ts, ClientInstanceID: strings.TrimSpace(p.ClientInstanceID),
		}
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetWorkspaceTaskProvision(ctx context.Context, tenantID, userID, operationID string) (*WorkspaceTaskProvision, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	row := s.db.QueryRowContext(ctx, `SELECT operation_id, tenant_id, user_id, client_instance_id, workspace_id,
		cloud_task_id, device_task_id, name, mode, tag, state, last_error, created_at, updated_at
		FROM cloud_workspace_task_provisions WHERE operation_id = ? AND tenant_id = ? AND user_id = ?`,
		strings.TrimSpace(operationID), store.NormalizeTenantID(tenantID), strings.TrimSpace(userID))
	return scanWorkspaceTaskProvision(row)
}

// FindWorkspaceTaskProvisionByIdempotencyKey returns the durable operation
// associated with a provisioning request. It is used only for recovery when a
// process crashed after the workspace transaction committed but before the
// generic idempotency ledger stored its response.
func (s *Store) FindWorkspaceTaskProvisionByIdempotencyKey(ctx context.Context, tenantID, userID, key, payloadHash string) (*WorkspaceTaskProvision, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrNotFound
	}
	var operationID, storedHash string
	err := s.db.QueryRowContext(ctx, `SELECT operation_id, idempotency_payload_hash FROM cloud_workspace_task_provisions WHERE tenant_id = ? AND user_id = ? AND idempotency_key = ?`, store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), key).Scan(&operationID, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(payloadHash) != "" && strings.TrimSpace(storedHash) != "" && strings.TrimSpace(storedHash) != strings.TrimSpace(payloadHash) {
		return nil, ErrIdempotencyKeyReused
	}
	return s.GetWorkspaceTaskProvision(ctx, tenantID, userID, operationID)
}

// GetLatestWorkspaceTaskProvision returns the newest non-terminal operation
// for a workspace. It is used by entitlement/recovery UI to surface a create
// that was interrupted before the local task acknowledgement.
func (s *Store) GetLatestWorkspaceTaskProvision(ctx context.Context, tenantID, userID, workspaceID string) (*WorkspaceTaskProvision, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	row := s.db.QueryRowContext(ctx, `SELECT operation_id, tenant_id, user_id, client_instance_id, workspace_id,
		cloud_task_id, device_task_id, name, mode, tag, state, last_error, created_at, updated_at
		FROM cloud_workspace_task_provisions WHERE tenant_id = ? AND user_id = ? AND workspace_id = ?
		ORDER BY updated_at DESC, operation_id DESC LIMIT 1`, store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), strings.TrimSpace(workspaceID))
	return scanWorkspaceTaskProvision(row)
}

// ReconcileStaleWorkspaceTaskProvisions compensates operations that were left
// in provisioning by a crashed GUI. A live lease is never revoked here; the
// operation is retried on the next sweep after that lease expires.
func (s *Store) ReconcileStaleWorkspaceTaskProvisions(ctx context.Context, now time.Time, grace time.Duration) (int, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	if grace <= 0 {
		grace = ProvisioningGrace
	}
	cutoff := now.UTC().Add(-grace)
	rows, err := s.db.QueryContext(ctx, `SELECT operation_id, tenant_id, user_id
		FROM cloud_workspace_task_provisions WHERE state = ? AND updated_at < ? ORDER BY updated_at ASC`, ProvisionStateProvisioning, cutoff.Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	type candidate struct{ opID, tenantID, userID string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.opID, &c.tenantID, &c.userID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	count := 0
	for _, c := range candidates {
		// Skip an operation whose workspace is still actively leased. This
		// avoids deleting a task while its creator is merely slow to ack.
		var workspaceID string
		if err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM cloud_workspace_task_provisions WHERE operation_id = ?`, c.opID).Scan(&workspaceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return count, err
		}
		lease, err := getActiveLease(ctx, s.db, workspaceID)
		if err != nil {
			return count, err
		}
		if lease != nil && !leaseExpired(lease.ExpiresAt, now) {
			continue
		}
		if _, err := s.AbortWorkspaceTaskProvision(ctx, c.tenantID, c.userID, c.opID, "provisioning timeout", now); err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrProvisionState) || errors.Is(err, ErrInUse) {
				continue
			}
			return count, err
		}
		count++
	}
	return count, nil
}

// CompleteWorkspaceTaskProvision is the commit acknowledgement sent after
// the local task and its sidecar have been durably written.
func (s *Store) CompleteWorkspaceTaskProvision(ctx context.Context, tenantID, userID, operationID string, now time.Time) (*WorkspaceTaskProvision, error) {
	return s.CompleteWorkspaceTaskProvisionWithSession(ctx, tenantID, userID, operationID, "", "", 0, now)
}

func (s *Store) CompleteWorkspaceTaskProvisionWithSession(ctx context.Context, tenantID, userID, operationID, machineID, clientInstanceID string, fencingToken int64, now time.Time) (*WorkspaceTaskProvision, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID, userID, operationID = store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), strings.TrimSpace(operationID)
	ts := now.UTC().Format(time.RFC3339)
	var out *WorkspaceTaskProvision
	err := s.withImmediate(ctx, func(q queryer) error {
		row, err := scanWorkspaceTaskProvision(q.QueryRowContext(ctx, `SELECT operation_id, tenant_id, user_id, client_instance_id, workspace_id,
			cloud_task_id, device_task_id, name, mode, tag, state, last_error, created_at, updated_at
			FROM cloud_workspace_task_provisions WHERE operation_id = ? AND tenant_id = ? AND user_id = ?`, operationID, tenantID, userID))
		if err != nil {
			return err
		}
		switch row.State {
		case ProvisionStateActive:
			out = row
			return stageAtomicIdempotency(ctx, q, out)
		case ProvisionStateProvisioning:
		default:
			return ErrProvisionState
		}
		if stored := strings.TrimSpace(row.ClientInstanceID); stored != "" && strings.TrimSpace(clientInstanceID) != "" && strings.TrimSpace(clientInstanceID) != stored {
			lease, leaseErr := getActiveLease(ctx, q, row.WorkspaceID)
			if leaseErr != nil {
				return leaseErr
			}
			if lease == nil || strings.TrimSpace(lease.ClientInstanceID) != strings.TrimSpace(clientInstanceID) {
				return ErrFenced
			}
		}
		lease, leaseErr := getActiveLease(ctx, q, row.WorkspaceID)
		if leaseErr != nil {
			return leaseErr
		}
		if strings.TrimSpace(row.ClientInstanceID) != "" && strings.TrimSpace(clientInstanceID) != "" && lease == nil {
			return ErrFenced
		}
		if lease != nil {
			if leaseExpired(lease.ExpiresAt, now) {
				return ErrFenced
			}
			if strings.TrimSpace(machineID) != "" && lease.MachineID != strings.TrimSpace(machineID) {
				return newInUseError(lease)
			}
			if strings.TrimSpace(clientInstanceID) != "" && strings.TrimSpace(lease.ClientInstanceID) != "" && strings.TrimSpace(clientInstanceID) != strings.TrimSpace(lease.ClientInstanceID) {
				return ErrFenced
			}
			if strings.TrimSpace(clientInstanceID) != "" && lease.FencingToken > 0 && fencingToken <= 0 {
				return ErrFenced
			}
			if fencingToken > 0 && lease.FencingToken > 0 && fencingToken != lease.FencingToken {
				return ErrFenced
			}
		}
		var workspaceStatus string
		if err := q.QueryRowContext(ctx, `SELECT status FROM cloud_workspaces WHERE id = ? AND tenant_id = ? AND user_id = ?`, row.WorkspaceID, tenantID, userID).Scan(&workspaceStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrProvisionState
			}
			return err
		}
		if workspaceStatus != StatusActive {
			return ErrProvisionState
		}
		var bindingTaskID string
		if err := q.QueryRowContext(ctx, `SELECT cloud_task_id FROM cloud_workspace_task_bindings WHERE workspace_id = ? AND tenant_id = ? AND user_id = ?`, row.WorkspaceID, tenantID, userID).Scan(&bindingTaskID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrProvisionState
			}
			return err
		}
		if strings.TrimSpace(bindingTaskID) != strings.TrimSpace(row.CloudTaskID) {
			return ErrProvisionState
		}
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_task_provisions SET state = ?, updated_at = ?, last_error = '' WHERE operation_id = ?`, ProvisionStateActive, ts, operationID); err != nil {
			return err
		}
		row.State, row.LastError, row.UpdatedAt = ProvisionStateActive, "", ts
		out = row
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AbortWorkspaceTaskProvision performs the compensating transition.  The
// binding is removed so a failed local task cannot be resurrected on a later
// restore; object/sidecar files, if any, remain subject to normal deleted
// workspace GC and can therefore be retried safely.
func (s *Store) AbortWorkspaceTaskProvision(ctx context.Context, tenantID, userID, operationID, reason string, now time.Time) (*WorkspaceTaskProvision, error) {
	return s.AbortWorkspaceTaskProvisionWithSession(ctx, tenantID, userID, operationID, reason, "", "", 0, now)
}

func (s *Store) AbortWorkspaceTaskProvisionWithSession(ctx context.Context, tenantID, userID, operationID, reason, machineID, clientInstanceID string, fencingToken int64, now time.Time) (*WorkspaceTaskProvision, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID, userID, operationID = store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), strings.TrimSpace(operationID)
	reason = strings.TrimSpace(reason)
	if len(reason) > 2048 {
		reason = reason[:2048]
	}
	ts := now.UTC().Format(time.RFC3339)
	var out *WorkspaceTaskProvision
	err := s.withImmediate(ctx, func(q queryer) error {
		row, err := scanWorkspaceTaskProvision(q.QueryRowContext(ctx, `SELECT operation_id, tenant_id, user_id, client_instance_id, workspace_id,
			cloud_task_id, device_task_id, name, mode, tag, state, last_error, created_at, updated_at
			FROM cloud_workspace_task_provisions WHERE operation_id = ? AND tenant_id = ? AND user_id = ?`, operationID, tenantID, userID))
		if err != nil {
			return err
		}
		if row.State == ProvisionStateFailed {
			out = row
			return stageAtomicIdempotency(ctx, q, out)
		}
		if row.State != ProvisionStateProvisioning {
			return ErrProvisionState
		}
		if stored := strings.TrimSpace(row.ClientInstanceID); stored != "" && strings.TrimSpace(clientInstanceID) != "" && strings.TrimSpace(clientInstanceID) != stored {
			lease, leaseErr := getActiveLease(ctx, q, row.WorkspaceID)
			if leaseErr != nil {
				return leaseErr
			}
			if lease == nil || strings.TrimSpace(lease.ClientInstanceID) != strings.TrimSpace(clientInstanceID) {
				return ErrFenced
			}
		}
		lease, leaseErr := getActiveLease(ctx, q, row.WorkspaceID)
		if leaseErr != nil {
			return leaseErr
		}
		if lease != nil {
			if strings.TrimSpace(machineID) != "" && lease.MachineID != strings.TrimSpace(machineID) {
				return newInUseError(lease)
			}
			if strings.TrimSpace(clientInstanceID) != "" && strings.TrimSpace(lease.ClientInstanceID) != "" && strings.TrimSpace(clientInstanceID) != strings.TrimSpace(lease.ClientInstanceID) {
				return ErrFenced
			}
			if strings.TrimSpace(clientInstanceID) != "" && lease.FencingToken > 0 && fencingToken <= 0 {
				return ErrFenced
			}
			if fencingToken > 0 && lease.FencingToken > 0 && fencingToken != lease.FencingToken {
				return ErrFenced
			}
			// Compensation owns the provisioning lease when the caller supplied
			// matching session information. Expired leases are safe to release as
			// part of stale-operation reconciliation.
			if strings.TrimSpace(machineID) != "" || leaseExpired(lease.ExpiresAt, now) {
				if err := releaseLease(ctx, q, lease.ID, ts, ""); err != nil {
					return err
				}
			}
		} else if stored := strings.TrimSpace(row.ClientInstanceID); stored != "" && stored != strings.TrimSpace(clientInstanceID) {
			// With no active writer, only the Hub-issued instance that began the
			// operation may compensate it. This keeps Prepare failures recoverable
			// without giving another device a lease-free delete path.
			return ErrFenced
		}
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_task_provisions SET state = ?, last_error = ?, updated_at = ? WHERE operation_id = ?`, ProvisionStateDeleting, reason, ts, operationID); err != nil {
			return err
		}
		// A compensated workspace must not remain visible or writable. Keep the
		// row for the restore/purge retention window so recovery is auditable.
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspaces SET status = ?, deleted_at = ?, updated_at = ? WHERE id = ? AND tenant_id = ? AND user_id = ? AND status = ?`, StatusDeleted, ts, ts, row.WorkspaceID, tenantID, userID, StatusActive); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_task_bindings WHERE workspace_id = ? AND tenant_id = ? AND user_id = ?`, row.WorkspaceID, tenantID, userID); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_task_provisions SET state = ?, updated_at = ? WHERE operation_id = ?`, ProvisionStateFailed, ts, operationID); err != nil {
			return err
		}
		row.State, row.LastError, row.UpdatedAt = ProvisionStateFailed, reason, ts
		out = row
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) BeginWorkspaceTaskProvision(ctx context.Context, principal auth.MachinePrincipal, p WorkspaceTaskProvisionParams) (*WorkspaceTaskProvision, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	settings := s.LoadTenantSettings(ctx, principal.TenantID)
	p.TenantID, p.UserID = principal.TenantID, principal.UserID
	p.ClientInstanceID = principal.ClientInstanceID
	p.Quota, p.TenantMaxTotalBytes = settings.Quota, settings.TenantMaxTotalBytes
	return s.Workspaces.BeginWorkspaceTaskProvision(ctx, p, s.now())
}

func (s *Service) GetWorkspaceTaskProvision(ctx context.Context, principal auth.MachinePrincipal, operationID string) (*WorkspaceTaskProvision, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.GetWorkspaceTaskProvision(ctx, principal.TenantID, principal.UserID, operationID)
}

func (s *Service) FindWorkspaceTaskProvisionByIdempotencyKey(ctx context.Context, principal auth.MachinePrincipal, key, payloadHash string) (*WorkspaceTaskProvision, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.FindWorkspaceTaskProvisionByIdempotencyKey(ctx, principal.TenantID, principal.UserID, key, payloadHash)
}

func (s *Service) CompleteWorkspaceTaskProvision(ctx context.Context, principal auth.MachinePrincipal, operationID string) (*WorkspaceTaskProvision, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.CompleteWorkspaceTaskProvisionWithSession(ctx, principal.TenantID, principal.UserID, operationID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, s.now())
}

func (s *Service) AbortWorkspaceTaskProvision(ctx context.Context, principal auth.MachinePrincipal, operationID, reason string) (*WorkspaceTaskProvision, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.AbortWorkspaceTaskProvisionWithSession(ctx, principal.TenantID, principal.UserID, operationID, reason, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, s.now())
}
