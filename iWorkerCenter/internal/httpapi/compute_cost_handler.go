package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/compute"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ComputeCostHandler handles cost statistics HTTP endpoints.
type ComputeCostHandler struct {
	costEngine *compute.CostEngine
}

// NewComputeCostHandler creates a new ComputeCostHandler.
func NewComputeCostHandler(costEngine *compute.CostEngine) *ComputeCostHandler {
	return &ComputeCostHandler{costEngine: costEngine}
}

// DiWorkerCostList handles GET /api/compute/cost/diworkers.
// Query params: period (daily|monthly), start, end.
func (h *ComputeCostHandler) DiWorkerCostList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := compute.CostFilter{
		PeriodType: q.Get("period"),
		Start:      q.Get("start"),
		End:        q.Get("end"),
	}

	summaries, err := h.costEngine.QueryCostSummaries(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if summaries == nil {
		summaries = []compute.CostSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"summaries": summaries})
}

// DiWorkerCostDetail handles GET /api/compute/cost/diworkers/{id}.
// Query params: period (daily|monthly), start, end.
func (h *ComputeCostHandler) DiWorkerCostDetail(w http.ResponseWriter, r *http.Request) {
	diworkerID := r.PathValue("id")
	if diworkerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing diworker id"})
		return
	}

	q := r.URL.Query()
	filter := compute.CostFilter{
		DiWorkerID: diworkerID,
		PeriodType: q.Get("period"),
		Start:      q.Get("start"),
		End:        q.Get("end"),
	}

	summaries, err := h.costEngine.QueryCostSummaries(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if summaries == nil {
		summaries = []compute.CostSummary{}
	}

	// Aggregate totals.
	var totalInputTokens, totalOutputTokens, totalTokens, requestCount int64
	var totalCost float64
	for _, s := range summaries {
		totalInputTokens += s.TotalInputTokens
		totalOutputTokens += s.TotalOutputTokens
		totalTokens += s.TotalTokens
		totalCost += s.TotalCost
		requestCount += s.RequestCount
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"diworker_id":         diworkerID,
		"total_input_tokens":  totalInputTokens,
		"total_output_tokens": totalOutputTokens,
		"total_tokens":        totalTokens,
		"total_cost":          totalCost,
		"request_count":       requestCount,
		"details":             summaries,
	})
}
