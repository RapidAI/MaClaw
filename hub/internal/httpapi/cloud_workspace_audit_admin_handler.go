package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

const (
	auditExportDefaultLimit = 1000
	auditExportMaxLimit     = 10000
)

// CloudWorkspaceAuditExportAdminHandler exports content-free Cloud Workspace
// audit rows for the authenticated tenant administrator. Pagination is
// cursor-based (after_id) and the next cursor is returned in a response
// header, which keeps CSV/JSONL streams line-oriented and pipe-friendly.
//
// Supported query parameters:
//   - format=csv (default) or format=jsonl (ndjson is accepted as an alias)
//   - workspace_id=<id> (optional tenant-scoped filter)
//   - after_id=<non-negative durable audit id>
//   - limit=1..10000 (default 1000)
func CloudWorkspaceAuditExportAdminHandler(svc *cloudworkspace.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil || svc.Workspaces == nil {
			writeError(w, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "cloud workspace audit store is unavailable")
			return
		}
		// RequireTenantAdmin is applied at route registration. Keep this guard
		// here too so direct embedding/tests cannot accidentally invoke the
		// exporter without an authenticated tenant scope.
		admin := AdminFromContext(r.Context())
		tenantID := ""
		if admin != nil && adminHasTenantScope(admin) {
			tenantID = strings.TrimSpace(admin.TenantID)
		}
		if tenantID == "" {
			writeError(w, http.StatusForbidden, "TENANT_ADMIN_REQUIRED", "Tenant administrator authorization required")
			return
		}

		format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
		if format == "" {
			format = "csv"
		}
		if format == "ndjson" {
			format = "jsonl"
		}
		if format != "csv" && format != "jsonl" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "format must be csv or jsonl")
			return
		}

		afterID := int64(0)
		if raw := strings.TrimSpace(r.URL.Query().Get("after_id")); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "after_id must be a non-negative integer")
				return
			}
			afterID = parsed
		}
		limit := auditExportDefaultLimit
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > auditExportMaxLimit {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "limit must be between 1 and 10000")
				return
			}
			limit = parsed
		}
		workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
		if len(workspaceID) > 128 {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "workspace_id is too long")
			return
		}

		page, err := svc.Workspaces.ListAuditForTenant(r.Context(), tenantID, workspaceID, afterID, limit)
		if err != nil {
			if errors.Is(err, cloudworkspace.ErrInvalidAuditEvent) {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
				return
			}
			if errors.Is(err, cloudworkspace.ErrUnavailable) {
				writeError(w, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "cloud workspace audit store is unavailable")
				return
			}
			writeError(w, http.StatusInternalServerError, "AUDIT_EXPORT_FAILED", "cloud workspace audit export failed")
			return
		}
		w.Header().Set("X-Audit-Next-After-ID", strconv.FormatInt(page.NextAfterID, 10))
		w.Header().Set("X-Audit-Has-More", strconv.FormatBool(page.HasMore))
		w.Header().Set("Content-Disposition", `attachment; filename="cloud-workspace-audit.`+format+`"`)
		w.Header().Set("Cache-Control", "no-store")
		if format == "jsonl" {
			w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
			enc := json.NewEncoder(w)
			for _, item := range page.Events {
				if err := enc.Encode(item); err != nil {
					return
				}
			}
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "event_id", "tenant_id", "user_id", "workspace_id", "client_instance_id", "machine_id", "operation", "outcome", "revision", "files", "bytes", "detail", "created_at"})
		for _, item := range page.Events {
			_ = cw.Write([]string{
				csvAuditCell(strconv.FormatInt(item.ID, 10)), csvAuditCell(item.EventID), csvAuditCell(item.TenantID), csvAuditCell(item.UserID),
				csvAuditCell(item.WorkspaceID), csvAuditCell(item.ClientInstanceID), csvAuditCell(item.MachineID), csvAuditCell(item.Operation),
				csvAuditCell(item.Outcome), csvAuditCell(item.Revision), csvAuditCell(strconv.Itoa(item.Files)), csvAuditCell(strconv.FormatInt(item.Bytes, 10)),
				csvAuditCell(item.Detail), csvAuditCell(item.CreatedAt),
			})
		}
		cw.Flush()
		// A write failure is reported by net/http to the client; there is no
		// safe way to change the already-started attachment response here.
	}
}

// GetCloudWorkspaceAuditExportAdminHandler is the descriptive alias used by
// route wiring and keeps naming consistent with the other GET admin handlers.
func GetCloudWorkspaceAuditExportAdminHandler(svc *cloudworkspace.Service) http.HandlerFunc {
	return CloudWorkspaceAuditExportAdminHandler(svc)
}

// csvAuditCell prevents spreadsheet formula injection for the few identifiers
// (user/machine IDs) whose source is outside the audit allowlist. CSV quoting
// alone does not disable formulas in common spreadsheet applications.
func csvAuditCell(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}
