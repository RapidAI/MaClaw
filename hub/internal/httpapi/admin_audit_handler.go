package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

func AdminAuditLogsHandler(audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if audit == nil {
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		createdFrom, err := parseAdminAuditTime(r.URL.Query().Get("from"), false)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_AUDIT_FROM", err.Error())
			return
		}
		createdTo, err := parseAdminAuditTime(r.URL.Query().Get("to"), true)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_AUDIT_TO", err.Error())
			return
		}
		if !createdFrom.IsZero() && !createdTo.IsZero() && createdFrom.After(createdTo) {
			writeError(w, http.StatusBadRequest, "INVALID_AUDIT_RANGE", "from must be before or equal to to")
			return
		}
		filter := store.AdminAuditLogFilter{Limit: limit, Action: r.URL.Query().Get("action"), Query: r.URL.Query().Get("q"), CreatedFrom: createdFrom, CreatedTo: createdTo}
		if admin := AdminFromContext(r.Context()); admin != nil && strings.TrimSpace(admin.Scope) == "tenant" {
			filter.TenantID = AdminTenantID(r.Context())
			filter.TenantScoped = true
		} else if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
			filter.TenantID = tenantID
			filter.TenantScoped = true
		}
		items, err := audit.List(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ADMIN_AUDIT_LIST_FAILED", err.Error())
			return
		}
		resp := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			payload := map[string]any{}
			if raw := strings.TrimSpace(item.PayloadJSON); raw != "" {
				_ = json.Unmarshal([]byte(raw), &payload)
			}
			resp = append(resp, map[string]any{"id": item.ID, "tenant_id": item.TenantID, "admin_user_id": item.AdminUserID, "action": item.Action, "payload": payload, "payload_json": item.PayloadJSON, "created_at": item.CreatedAt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": resp})
	}
}

func parseAdminAuditTime(raw string, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, nil
	}
	if day, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			return day.Add(24*time.Hour - time.Nanosecond), nil
		}
		return day, nil
	}
	return time.Time{}, fmt.Errorf("invalid audit timestamp %q; use RFC3339 or YYYY-MM-DD", raw)
}

func firstAdminAuditRepo(audits ...store.AdminAuditRepository) store.AdminAuditRepository {
	if len(audits) == 0 {
		return nil
	}
	return audits[0]
}

func adminAuditUserID(r *http.Request) string {
	if admin := AdminFromContext(r.Context()); admin != nil && strings.TrimSpace(admin.ID) != "" {
		return admin.ID
	}
	return "admin"
}

// writeAdminAuditLog records an admin action that has already taken effect.
//
// It cannot refuse the action, because by the time it runs the verdict is
// applied; failing here would only add a second problem to the first. What it
// must not do is disappear. Both ways of losing a record — no repository wired
// on this instance, and a repository that rejected the write — leave an admin
// action with no trace, which is indistinguishable from an admin action nobody
// took. Every real bootstrap supplies the repository (hub/internal/app), so a
// nil one is a misconfiguration, and the operator learns about it here or not
// at all.
func writeAdminAuditLog(ctx context.Context, audit store.AdminAuditRepository, adminUserID, action string, payload map[string]any) {
	if audit == nil {
		// The action name and actor are enough to reconstruct what was lost.
		// The payload is not logged: it carries admin-supplied values that the
		// audit store is the reviewed place for.
		log.Printf("[admin-audit] MISSING RECORD: no audit repository configured; action=%q admin=%q took effect unrecorded", action, adminUserID)
		return
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte("{}")
	}
	tenantID := ""
	if admin := AdminFromContext(ctx); admin != nil && strings.TrimSpace(admin.Scope) == "tenant" {
		tenantID = AdminTenantID(ctx)
	} else if value := ctx.Value(requestTenantContextKey{}); value != nil {
		if requestedTenantID, ok := value.(string); ok {
			tenantID = strings.TrimSpace(requestedTenantID)
		}
	}
	if err := audit.Create(ctx, &store.AdminAuditLog{ID: uuid.New().String(), TenantID: tenantID, AdminUserID: adminUserID, Action: action, PayloadJSON: string(payloadJSON), CreatedAt: time.Now().UTC()}); err != nil {
		log.Printf("[admin-audit] MISSING RECORD: write failed; action=%q admin=%q tenant=%q took effect unrecorded: %v", action, adminUserID, tenantID, err)
	}
}
