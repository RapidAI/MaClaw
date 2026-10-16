package cloudworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// AuditEvent is the server-retained, content-free record for a cloud
// workspace operation.  It deliberately contains counters and stable reason
// codes only; paths, request URLs, file contents and credentials are never
// accepted by the service.
type AuditEvent struct {
	ID               int64  `json:"id"`
	EventID          string `json:"event_id"`
	WorkspaceID      string `json:"workspace_id"`
	ClientInstanceID string `json:"client_instance_id,omitempty"`
	MachineID        string `json:"machine_id,omitempty"`
	Operation        string `json:"operation"`
	Outcome          string `json:"outcome"`
	Revision         string `json:"revision,omitempty"`
	Files            int    `json:"files,omitempty"`
	Bytes            int64  `json:"bytes,omitempty"`
	Detail           string `json:"detail,omitempty"`
	CreatedAt        string `json:"created_at"`
}

// AuditExportEvent is the administrator-facing projection of an audit row.
// It intentionally contains only content-free metadata. TenantID and UserID
// are included so a tenant administrator can reconcile events across users;
// paths, payloads, URLs, credentials and key material are never exposed.
type AuditExportEvent struct {
	ID               int64  `json:"id"`
	EventID          string `json:"event_id"`
	TenantID         string `json:"tenant_id"`
	UserID           string `json:"user_id"`
	WorkspaceID      string `json:"workspace_id"`
	ClientInstanceID string `json:"client_instance_id,omitempty"`
	MachineID        string `json:"machine_id,omitempty"`
	Operation        string `json:"operation"`
	Outcome          string `json:"outcome"`
	Revision         string `json:"revision,omitempty"`
	Files            int    `json:"files,omitempty"`
	Bytes            int64  `json:"bytes,omitempty"`
	Detail           string `json:"detail,omitempty"`
	CreatedAt        string `json:"created_at"`
}

// AuditExportPage is a bounded cursor page for administrative exports. The
// caller supplies NextAfterID as the exclusive after_id on the next request.
type AuditExportPage struct {
	Events      []AuditExportEvent `json:"events"`
	NextAfterID int64              `json:"next_after_id,omitempty"`
	HasMore     bool               `json:"has_more"`
}

var (
	ErrInvalidAuditEvent  = errors.New("invalid cloud workspace audit event")
	ErrAuditEventIDReused = errors.New("cloud workspace audit event id reused with different payload")
)

const (
	maxAuditOperation = 64
	maxAuditOutcome   = 32
	maxAuditRevision  = 128
	maxAuditDetail    = 64
	maxAuditEventID   = 128
	maxAuditFiles     = 20000
)

var cloudWorkspaceAuditOperations = map[string]struct{}{
	"download": {}, "push": {}, "sync": {}, "restore": {},
	"local_release": {}, "remote_delete": {}, "remote_purge": {},
	"cache_purge": {}, "handoff": {}, "lease": {},
}

var cloudWorkspaceAuditOutcomes = map[string]struct{}{
	"ok": {}, "failed": {}, "already_gone": {}, "canceled": {},
}

var cloudWorkspaceAuditDetails = map[string]struct{}{
	"": {}, "revoked": {}, "canceled": {}, "timeout": {},
	"permission_denied": {}, "fenced": {}, "integrity_mismatch": {},
	"conflict": {}, "quota_exceeded": {}, "operation_failed": {},
}

func normalizeAuditToken(value string, max int, allow map[string]struct{}) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrInvalidAuditEvent
	}
	if len(value) > max {
		return "", ErrInvalidAuditEvent
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return "", ErrInvalidAuditEvent
	}
	if allow != nil {
		if _, ok := allow[value]; !ok {
			return "", ErrInvalidAuditEvent
		}
	}
	return value, nil
}

func normalizeAuditRevision(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > maxAuditRevision {
		return "", ErrInvalidAuditEvent
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return "", ErrInvalidAuditEvent
	}
	return value, nil
}

func normalizeAuditEvent(event AuditEvent) (AuditEvent, error) {
	var err error
	event.Operation, err = normalizeAuditToken(event.Operation, maxAuditOperation, cloudWorkspaceAuditOperations)
	if err != nil {
		return AuditEvent{}, err
	}
	event.Outcome = strings.TrimSpace(event.Outcome)
	if event.Outcome == "" {
		event.Outcome = "ok"
	}
	event.Outcome, err = normalizeAuditToken(event.Outcome, maxAuditOutcome, cloudWorkspaceAuditOutcomes)
	if err != nil {
		return AuditEvent{}, err
	}
	event.Revision, err = normalizeAuditRevision(event.Revision)
	if err != nil {
		return AuditEvent{}, err
	}
	event.Detail = strings.TrimSpace(event.Detail)
	if len(event.Detail) > maxAuditDetail {
		return AuditEvent{}, ErrInvalidAuditEvent
	}
	if _, ok := cloudWorkspaceAuditDetails[event.Detail]; !ok {
		return AuditEvent{}, ErrInvalidAuditEvent
	}
	if event.Files < 0 || event.Files > maxAuditFiles || event.Bytes < 0 {
		return AuditEvent{}, ErrInvalidAuditEvent
	}
	event.EventID = strings.TrimSpace(event.EventID)
	if event.EventID != "" {
		if len(event.EventID) > maxAuditEventID {
			return AuditEvent{}, ErrInvalidAuditEvent
		}
		for _, r := range event.EventID {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
				continue
			}
			return AuditEvent{}, ErrInvalidAuditEvent
		}
	}
	return event, nil
}

func auditEventID(event AuditEvent, principal auth.MachinePrincipal) string {
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s",
		principal.ClientInstanceID, event.WorkspaceID, event.Operation,
		event.Outcome, event.Revision, event.Files, event.Bytes, event.Detail)
	sum := sha256.Sum256([]byte(raw))
	return "cwa_" + hex.EncodeToString(sum[:16])
}

// RecordAudit appends a content-free audit record.  EventID makes retries
// idempotent; if it is omitted the server derives a stable operation identity
// for the current instance.  Audits do not require an active lease: a fenced
// or failed release must remain observable after the writer has lost the lease.
func (s *Service) RecordAudit(ctx context.Context, principal auth.MachinePrincipal, workspaceID string, event AuditEvent) (*AuditEvent, error) {
	if s == nil || s.Workspaces == nil || s.Workspaces.db == nil {
		return nil, ErrUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrNotFound
	}
	if allowed, err := s.Workspaces.CanAccessAudit(ctx, principal.TenantID, principal.UserID, workspaceID); err != nil {
		return nil, err
	} else if !allowed {
		return nil, ErrNotFound
	}
	event.WorkspaceID = workspaceID
	event.ClientInstanceID = strings.TrimSpace(principal.ClientInstanceID)
	event.MachineID = strings.TrimSpace(principal.MachineID)
	normalized, err := normalizeAuditEvent(event)
	if err != nil {
		return nil, err
	}
	if normalized.EventID == "" {
		normalized.EventID = auditEventID(normalized, principal)
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	_, err = s.Workspaces.db.ExecContext(ctx, `
		INSERT INTO cloud_workspace_audit_events
			(event_id, tenant_id, user_id, workspace_id, client_instance_id, machine_id,
			 operation, outcome, revision, files, bytes, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, event_id) DO NOTHING`,
		normalized.EventID, principal.TenantID, principal.UserID, workspaceID,
		normalized.ClientInstanceID, normalized.MachineID, normalized.Operation,
		normalized.Outcome, normalized.Revision, normalized.Files, normalized.Bytes,
		normalized.Detail, now)
	if err != nil {
		return nil, err
	}
	var out AuditEvent
	err = s.Workspaces.db.QueryRowContext(ctx, `
		SELECT id, event_id, workspace_id, client_instance_id, machine_id,
		       operation, outcome, revision, files, bytes, detail, created_at
		  FROM cloud_workspace_audit_events
		 WHERE workspace_id = ? AND event_id = ?`, workspaceID, normalized.EventID).
		Scan(&out.ID, &out.EventID, &out.WorkspaceID, &out.ClientInstanceID, &out.MachineID,
			&out.Operation, &out.Outcome, &out.Revision, &out.Files, &out.Bytes,
			&out.Detail, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	if out.ClientInstanceID != normalized.ClientInstanceID || out.MachineID != normalized.MachineID ||
		out.Operation != normalized.Operation || out.Outcome != normalized.Outcome ||
		out.Revision != normalized.Revision || out.Files != normalized.Files ||
		out.Bytes != normalized.Bytes || out.Detail != normalized.Detail {
		return nil, ErrAuditEventIDReused
	}
	return &out, nil
}

// ListAudit returns records in ascending durable-id order after an exclusive
// numeric cursor. This lets a client resume delivery without trusting client
// timestamps; callers can keep the last returned ID as the next cursor. The
// retention policy is deployment-controlled; this method never silently
// truncates history.
func (s *Service) ListAudit(ctx context.Context, principal auth.MachinePrincipal, workspaceID string, afterID int64, limit int) ([]AuditEvent, error) {
	if s == nil || s.Workspaces == nil || s.Workspaces.db == nil {
		return nil, ErrUnavailable
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrNotFound
	}
	if allowed, err := s.Workspaces.CanAccessAudit(ctx, principal.TenantID, principal.UserID, workspaceID); err != nil {
		return nil, err
	} else if !allowed {
		return nil, ErrNotFound
	}
	if afterID < 0 {
		return nil, ErrInvalidAuditEvent
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.Workspaces.db.QueryContext(ctx, `
		SELECT id, event_id, workspace_id, client_instance_id, machine_id,
		       operation, outcome, revision, files, bytes, detail, created_at
		  FROM cloud_workspace_audit_events
		 WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND id > ?
		 ORDER BY id ASC LIMIT ?`, principal.TenantID, principal.UserID, workspaceID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEvent, 0)
	for rows.Next() {
		var item AuditEvent
		if err := rows.Scan(&item.ID, &item.EventID, &item.WorkspaceID, &item.ClientInstanceID, &item.MachineID,
			&item.Operation, &item.Outcome, &item.Revision, &item.Files, &item.Bytes, &item.Detail, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
