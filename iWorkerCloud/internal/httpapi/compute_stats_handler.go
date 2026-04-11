package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/compute"
)

// CenterCostStats handles GET /api/stats/center-costs.
// Query params: center_id, period (daily|monthly), start, end.
func (h *ComputeHandler) CenterCostStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.costEngine == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cost engine not available"})
			return
		}

		q := r.URL.Query()
		filter := compute.CostFilter{
			CenterID:   q.Get("center_id"),
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

		// If no center_id specified, also compute aggregate totals.
		if filter.CenterID == "" {
			var totalCost, totalInput, totalOutput float64
			var totalTokens int64
			for _, s := range summaries {
				totalCost += s.TotalCost
				totalInput += s.InputCost
				totalOutput += s.OutputCost
				totalTokens += s.TotalTokens
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"summaries":    summaries,
				"total_cost":   totalCost,
				"input_cost":   totalInput,
				"output_cost":  totalOutput,
				"total_tokens": totalTokens,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"summaries": summaries})
	}
}

// CenterMonthlyUsage handles GET /api/centers/{id}/monthly-usage.
// Query param: month (YYYY-MM). Used by iWorkerCenter for reconciliation.
func (h *ComputeHandler) CenterMonthlyUsage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing center id"})
			return
		}

		month := r.URL.Query().Get("month")
		if month == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing month parameter"})
			return
		}

		if h.costEngine == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cost engine not available"})
			return
		}

		filter := compute.CostFilter{
			CenterID:   centerID,
			PeriodType: "monthly",
			Start:      month,
			End:        month,
		}

		summaries, err := h.costEngine.QueryCostSummaries(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		// Aggregate totals for the month.
		var totalInputTokens, totalOutputTokens, totalTokens int64
		var totalCost float64
		for _, s := range summaries {
			totalInputTokens += s.TotalInputTokens
			totalOutputTokens += s.TotalOutputTokens
			totalTokens += s.TotalTokens
			totalCost += s.TotalCost
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"center_id":           centerID,
			"month":               month,
			"total_input_tokens":  totalInputTokens,
			"total_output_tokens": totalOutputTokens,
			"total_tokens":        totalTokens,
			"total_cost":          totalCost,
			"details":             summaries,
		})
	}
}
