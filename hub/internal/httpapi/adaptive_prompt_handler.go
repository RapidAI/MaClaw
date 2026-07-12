package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// AdaptivePromptMetricsHandler returns fleet-level adaptive system-prompt cost
// stats aggregated from online machines' latest heartbeats.
//
//	GET /api/admin/adaptive-prompt/metrics
//
// Totals sum process-level counters (no user/session labels). Optional
// tenant_id query scopes the sum for tenant admins.
func AdaptivePromptMetricsHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if devices == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":                  true,
				"online_machines":     0,
				"machines_with_stats": 0,
				"totals":              corelib.AdaptivePromptStat{},
			})
			return
		}
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if tenantID == "" && isTenantScopedAdminRequest(r) {
			// Prefer explicit request tenant from admin session when present.
			tenantID = RequestTenantID(r)
		}
		// Global admins without tenant_id see all online machines.
		if IsGlobalAdmin(r.Context()) && strings.TrimSpace(r.URL.Query().Get("tenant_id")) == "" {
			tenantID = ""
		}
		totals, withStats, online := devices.SumOnlineAdaptivePrompt(store.NormalizeTenantID(tenantID))
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                  true,
			"online_machines":     online,
			"machines_with_stats": withStats,
			"tenant_id":           store.NormalizeTenantID(tenantID),
			"totals":              totals,
			"hint":                "Per-machine detail: GET /api/admin/debug/machines (adaptive_prompt field). Local CLI: maclaw-cli shared-loop export|merge-exports",
		})
	}
}
