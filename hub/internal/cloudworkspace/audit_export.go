package cloudworkspace

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	defaultAuditExportLimit = 1000
	maxAuditExportLimit     = 10000
)

// ListAuditForTenant returns a bounded, cursor-paginated projection for a
// tenant administrator. The query is deliberately scoped by tenant_id before
// applying the optional workspace filter; callers cannot use a workspace ID
// to cross tenant boundaries. One extra row is read to determine HasMore.
func (s *Store) ListAuditForTenant(ctx context.Context, tenantID, workspaceID string, afterID int64, limit int) (AuditExportPage, error) {
	var page AuditExportPage
	if s == nil || s.db == nil {
		return page, ErrUnavailable
	}
	if strings.TrimSpace(tenantID) == "" {
		return page, ErrInvalidAuditEvent
	}
	tenantID = store.NormalizeTenantID(tenantID)
	workspaceID = strings.TrimSpace(workspaceID)
	if afterID < 0 {
		return page, ErrInvalidAuditEvent
	}
	if limit <= 0 {
		limit = defaultAuditExportLimit
	}
	if limit > maxAuditExportLimit {
		limit = maxAuditExportLimit
	}

	args := []any{tenantID, afterID}
	where := "tenant_id = ? AND id > ?"
	if workspaceID != "" {
		if len(workspaceID) > 128 {
			return page, ErrInvalidAuditEvent
		}
		where += " AND workspace_id = ?"
		args = append(args, workspaceID)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_id, tenant_id, user_id, workspace_id,
		       client_instance_id, machine_id, operation, outcome, revision,
		       files, bytes, detail, created_at
		  FROM cloud_workspace_audit_events
		 WHERE `+where+`
		 ORDER BY id ASC LIMIT ?`, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Events = make([]AuditExportEvent, 0, limit)
	for rows.Next() {
		var item AuditExportEvent
		if err := rows.Scan(&item.ID, &item.EventID, &item.TenantID, &item.UserID,
			&item.WorkspaceID, &item.ClientInstanceID, &item.MachineID,
			&item.Operation, &item.Outcome, &item.Revision, &item.Files,
			&item.Bytes, &item.Detail, &item.CreatedAt); err != nil {
			return AuditExportPage{}, err
		}
		// Re-validate rows at the export boundary as well as on write. This
		// protects deployments that imported legacy/corrupt rows directly into
		// SQLite: a malformed detail or operation must fail closed rather than
		// accidentally becoming an unrestricted free-text export channel.
		safe, err := normalizeAuditEvent(AuditEvent{
			EventID: item.EventID, Operation: item.Operation, Outcome: item.Outcome,
			Revision: item.Revision, Files: item.Files, Bytes: item.Bytes, Detail: item.Detail,
		})
		if err != nil {
			return AuditExportPage{}, ErrInvalidAuditEvent
		}
		item.EventID, item.Operation, item.Outcome = safe.EventID, safe.Operation, safe.Outcome
		item.Revision, item.Files, item.Bytes, item.Detail = safe.Revision, safe.Files, safe.Bytes, safe.Detail
		if len(page.Events) == limit {
			page.HasMore = true
			break
		}
		page.Events = append(page.Events, item)
		page.NextAfterID = item.ID
	}
	if err := rows.Err(); err != nil {
		return AuditExportPage{}, err
	}
	if page.Events == nil {
		page.Events = []AuditExportEvent{}
	}
	return page, nil
}
