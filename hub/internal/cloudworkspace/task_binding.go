package cloudworkspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// TaskBinding is the server-owned identity that connects one workspace to one
// logical task. DeviceTaskID is only a projection and may change when a task
// is resumed on another device.
type TaskBinding struct {
	WorkspaceID  string `json:"workspace_id"`
	CloudTaskID  string `json:"cloud_task_id"`
	DeviceTaskID string `json:"device_task_id,omitempty"`
	Name         string `json:"name,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Tag          string `json:"tag,omitempty"`
	Version      int64  `json:"version"`
	UpdatedAt    string `json:"updated_at"`
}

func stableCloudTaskID(workspaceID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(workspaceID)))
	return "ct_" + hex.EncodeToString(sum[:16])
}

func scanTaskBinding(scanner interface{ Scan(dest ...any) error }) (*TaskBinding, error) {
	var b TaskBinding
	if err := scanner.Scan(&b.WorkspaceID, &b.CloudTaskID, &b.DeviceTaskID, &b.Name, &b.Mode, &b.Tag, &b.Version, &b.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &b, nil
}

// GetTaskBinding returns the unique binding for an owned active workspace.
func (s *Store) GetTaskBinding(ctx context.Context, tenantID, userID, workspaceID string) (*TaskBinding, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	var b TaskBinding
	// A binding is part of an active workspace's task projection. Once a
	// workspace is soft-deleted, callers must not be able to resurrect its
	// task identity through this read API; the binding row is retained only for
	// the restore window and is removed by purge.
	err := s.db.QueryRowContext(ctx, `
		SELECT b.workspace_id, b.cloud_task_id, b.device_task_id, b.name, b.mode, b.tag, b.version, b.updated_at
		FROM cloud_workspace_task_bindings b
		JOIN cloud_workspaces w ON w.id = b.workspace_id AND w.tenant_id = b.tenant_id AND w.user_id = b.user_id
		WHERE b.workspace_id = ? AND b.tenant_id = ? AND b.user_id = ? AND w.status = ?`,
		strings.TrimSpace(workspaceID), store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), StatusActive).Scan(&b.WorkspaceID, &b.CloudTaskID, &b.DeviceTaskID, &b.Name, &b.Mode, &b.Tag, &b.Version, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// UpsertTaskBinding creates the binding once and then updates the device
// projection. A workspace can never point at a second cloud task id.
func (s *Store) UpsertTaskBinding(ctx context.Context, tenantID, userID, workspaceID, cloudTaskID, deviceTaskID, name, mode, tag string, expectedVersion int64, now time.Time) (*TaskBinding, error) {
	return s.UpsertTaskBindingWithSession(ctx, tenantID, userID, workspaceID, "", "", 0, cloudTaskID, deviceTaskID, name, mode, tag, expectedVersion, now)
}

func (s *Store) UpsertTaskBindingWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, cloudTaskID, deviceTaskID, name, mode, tag string, expectedVersion int64, now time.Time) (*TaskBinding, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID, workspaceID = strings.TrimSpace(userID), strings.TrimSpace(workspaceID)
	cloudTaskID = strings.TrimSpace(cloudTaskID)
	if workspaceID == "" || userID == "" {
		return nil, ErrNotFound
	}
	if cloudTaskID == "" {
		cloudTaskID = stableCloudTaskID(workspaceID)
	}
	ts := now.UTC().Format(time.RFC3339)
	var out *TaskBinding
	err := s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID); err != nil {
			return err
		}
		if strings.TrimSpace(machineID) != "" {
			if err := assertLeaseHeldForSession(ctx, q, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
				return err
			}
		}
		var err error
		out, err = upsertTaskBindingTx(ctx, q, tenantID, userID, workspaceID, cloudTaskID, deviceTaskID, name, mode, tag, expectedVersion, ts)
		if err != nil {
			return err
		}
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// upsertTaskBindingTx is the transaction-scoped implementation used by both
// the standalone binding API and task.json sidecar commits. Callers must have
// already authenticated the writer lease when the operation is part of a
// sidecar commit; keeping the mutation in the caller's transaction makes the
// binding and sidecar metadata commit/rollback together.
func upsertTaskBindingTx(ctx context.Context, q queryer, tenantID, userID, workspaceID, cloudTaskID, deviceTaskID, name, mode, tag string, expectedVersion int64, ts string) (*TaskBinding, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	userID, workspaceID = strings.TrimSpace(userID), strings.TrimSpace(workspaceID)
	cloudTaskID = strings.TrimSpace(cloudTaskID)
	if workspaceID == "" || userID == "" {
		return nil, ErrNotFound
	}
	if cloudTaskID == "" {
		cloudTaskID = stableCloudTaskID(workspaceID)
	}
	var current TaskBinding
	err := q.QueryRowContext(ctx, `SELECT workspace_id, cloud_task_id, device_task_id, name, mode, tag, version, updated_at FROM cloud_workspace_task_bindings WHERE workspace_id = ?`, workspaceID).Scan(&current.WorkspaceID, &current.CloudTaskID, &current.DeviceTaskID, &current.Name, &current.Mode, &current.Tag, &current.Version, &current.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_task_bindings (workspace_id, tenant_id, user_id, cloud_task_id, device_task_id, name, mode, tag, version, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`, workspaceID, tenantID, userID, cloudTaskID, strings.TrimSpace(deviceTaskID), strings.TrimSpace(name), strings.TrimSpace(mode), strings.TrimSpace(tag), ts); err != nil {
			if isUniqueConstraintError(err) {
				return nil, ErrRevisionConflict
			}
			return nil, err
		}
		return &TaskBinding{WorkspaceID: workspaceID, CloudTaskID: cloudTaskID, DeviceTaskID: strings.TrimSpace(deviceTaskID), Name: strings.TrimSpace(name), Mode: strings.TrimSpace(mode), Tag: strings.TrimSpace(tag), Version: 1, UpdatedAt: ts}, nil
	}
	if err != nil {
		return nil, err
	}
	if current.CloudTaskID != cloudTaskID {
		return nil, ErrRevisionConflict
	}
	if expectedVersion > 0 && current.Version != expectedVersion {
		return nil, ErrRevisionConflict
	}
	next := current.Version + 1
	res, err := q.ExecContext(ctx, `UPDATE cloud_workspace_task_bindings SET device_task_id = ?, name = ?, mode = ?, tag = ?, version = ?, updated_at = ? WHERE workspace_id = ? AND version = ?`, strings.TrimSpace(deviceTaskID), strings.TrimSpace(name), strings.TrimSpace(mode), strings.TrimSpace(tag), next, ts, workspaceID, current.Version)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrRevisionConflict
	}
	return &TaskBinding{WorkspaceID: workspaceID, CloudTaskID: current.CloudTaskID, DeviceTaskID: strings.TrimSpace(deviceTaskID), Name: strings.TrimSpace(name), Mode: strings.TrimSpace(mode), Tag: strings.TrimSpace(tag), Version: next, UpdatedAt: ts}, nil
}

func (s *Store) DeleteTaskBinding(ctx context.Context, tenantID, userID, workspaceID string, expectedVersion int64) error {
	return s.DeleteTaskBindingWithSession(ctx, tenantID, userID, workspaceID, "", "", 0, expectedVersion, time.Now().UTC())
}

func (s *Store) DeleteTaskBindingWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, expectedVersion int64, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	return s.withImmediate(ctx, func(q queryer) error {
		if strings.TrimSpace(machineID) != "" {
			if _, err := requireActiveOwned(ctx, q, store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), strings.TrimSpace(workspaceID)); err != nil {
				return err
			}
			if err := assertLeaseHeldForSession(ctx, q, strings.TrimSpace(workspaceID), machineID, clientInstanceID, fencingToken, now); err != nil {
				return err
			}
		}
		args := []any{strings.TrimSpace(workspaceID), store.NormalizeTenantID(tenantID), strings.TrimSpace(userID)}
		query := `DELETE FROM cloud_workspace_task_bindings WHERE workspace_id = ? AND tenant_id = ? AND user_id = ?`
		if expectedVersion > 0 {
			query += ` AND version = ?`
			args = append(args, expectedVersion)
		}
		res, err := q.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if expectedVersion > 0 {
			if n, _ := res.RowsAffected(); n == 0 {
				return ErrRevisionConflict
			}
		}
		return stageAtomicIdempotency(ctx, q, nil)
	})
}

func (s *Service) GetTaskBinding(ctx context.Context, principal auth.MachinePrincipal, workspaceID string) (*TaskBinding, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.GetTaskBinding(ctx, principal.TenantID, principal.UserID, workspaceID)
}

func (s *Service) UpsertTaskBinding(ctx context.Context, principal auth.MachinePrincipal, workspaceID, cloudTaskID, deviceTaskID, name, mode, tag string, expectedVersion int64) (*TaskBinding, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	now := s.now()
	return s.Workspaces.UpsertTaskBindingWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, cloudTaskID, deviceTaskID, name, mode, tag, expectedVersion, now)
}

func (s *Service) DeleteTaskBinding(ctx context.Context, principal auth.MachinePrincipal, workspaceID string, expectedVersion int64) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	return s.Workspaces.DeleteTaskBindingWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, expectedVersion, s.now())
}
