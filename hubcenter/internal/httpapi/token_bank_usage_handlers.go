package httpapi

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
)

// TokenBankUsageDaily is the 30-day earnings curve and the top models.
//
// GET /api/v1/token-bank/usage/daily?days=30
func (h *SkillMarketHandlers) TokenBankUsageDaily(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	days := usageDays(r)
	daily, models, err := repo.UsageDaily(r.Context(), user.ID, days)
	if err != nil {
		tbError(w, http.StatusInternalServerError, "usage_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"days":   days,
		"daily":  daily,
		"models": models,
	})
}

// TokenBankUsageCSV downloads the caller's settled rows.
//
// GET /api/v1/token-bank/usage.csv?days=30
func (h *SkillMarketHandlers) TokenBankUsageCSV(w http.ResponseWriter, r *http.Request) {
	user, ok := h.tokenBankSessionUser(w, r)
	if !ok {
		return
	}
	repo := h.tokenBankRepo()
	if repo == nil {
		tbError(w, http.StatusServiceUnavailable, "token_bank_unavailable", "token bank is not available on this node")
		return
	}
	rows, err := repo.UsageExport(r.Context(), user.ID, usageDays(r))
	if err != nil {
		tbError(w, http.StatusInternalServerError, "usage_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="token-bank-usage.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"created_at", "request_id", "model", "input_tokens", "output_tokens", "gross_micro", "fee_micro", "net_micro", "charged_micro", "self_use"})
	for _, row := range rows {
		selfUse := "0"
		if row.SelfUse {
			selfUse = "1"
		}
		_ = cw.Write([]string{
			row.CreatedAt, row.RequestID, row.ModelName,
			strconv.FormatInt(row.InputTokens, 10), strconv.FormatInt(row.OutputTokens, 10),
			strconv.FormatInt(row.GrossMicro, 10), strconv.FormatInt(row.FeeMicro, 10),
			strconv.FormatInt(row.NetMicro, 10), strconv.FormatInt(row.ChargedMicro, 10),
			selfUse,
		})
	}
	cw.Flush()
}

func usageDays(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("days"))
	if raw == "" {
		return 30
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 30
	}
	if n > 366 {
		return 366
	}
	return n
}
