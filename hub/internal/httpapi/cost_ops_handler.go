package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// CostOpsMetricsHandler returns fleet-level cost-route + daily $ aggregates
// from online machines' latest heartbeats.
//
//	GET /api/admin/cost-ops/metrics
func CostOpsMetricsHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if devices == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":                  true,
				"online_machines":     0,
				"machines_with_stats": 0,
				"totals":              corelib.CostOpsStat{},
			})
			return
		}
		tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
		if tenantID == "" && isTenantScopedAdminRequest(r) {
			tenantID = RequestTenantID(r)
		}
		if IsGlobalAdmin(r.Context()) && strings.TrimSpace(r.URL.Query().Get("tenant_id")) == "" {
			tenantID = ""
		}
		totals, withStats, online := devices.SumOnlineCostOps(store.NormalizeTenantID(tenantID))
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                  true,
			"online_machines":     online,
			"machines_with_stats": withStats,
			"tenant_id":           store.NormalizeTenantID(tenantID),
			"totals":              totals,
			"hint":                "Per-machine: GET /api/admin/debug/machines (cost_ops). Local: maclaw-cli cost",
		})
	}
}
