package audit

import (
	"net/http"
	"strconv"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes HTTP endpoints for audit data.
type Handler struct {
	repo *Repo
}

// NewHandler creates a Handler.
func NewHandler(repo *Repo) *Handler {
	return &Handler{repo: repo}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/audit/stats", h.handleStats)
	mux.HandleFunc("/admin/audit/logs", h.handleLogs)
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	hours := 24
	if v := r.URL.Query().Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	tenantID := tenant.TenantIDFromContext(r.Context())
	stats, err := h.repo.GetStats(tenantID, hours)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, stats)
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	tenantID := tenant.TenantIDFromContext(r.Context())
	logs, err := h.repo.ListRecent(tenantID, limit)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	dtos := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		dtos = append(dtos, map[string]any{
			"id":           l.ID,
			"request_id":   l.RequestID,
			"provider_id":  l.ProviderID,
			"model":        l.Model,
			"work_type":    l.WorkType,
			"cost_tier":    l.CostTier,
			"status":       l.Status,
			"latency_ms":   l.LatencyMs,
			"input_tokens": l.InputTokens,
			"summary":      l.Summary,
			"error_msg":    l.ErrorMsg,
			"created_at":   l.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	response.OK(w, map[string]any{"logs": dtos})
}
