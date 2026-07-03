package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type myRankingResponse struct {
	TotalTokens     int64  `json:"total_tokens"`
	DurationSeconds int64  `json:"duration_seconds"`
	TokenRank       int    `json:"token_rank"`
	DurationRank    int    `json:"duration_rank"`
	TotalUsers      int    `json:"total_users"`
	Period          string `json:"period"`
}

// GetMyRankingHandler returns the current authenticated user's usage stats
// and ranking within their tenant. Uses viewer token auth (no admin required).
func GetMyRankingHandler(identity *auth.IdentityService, sessions userUsageSummarizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "RANKING_UNAVAILABLE", "session repository is unavailable")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil || principal == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		tenantID := strings.TrimSpace(principal.TenantID)
		if tenantID == "" {
			tenantID = RequestTenantID(r)
		}
		if tenantID == "" {
			tenantID = store.DefaultTenantID
		}
		now := time.Now().UTC()

		// Use monthly period for ranking (same as admin dashboard default).
		period := "monthly"
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		rankingCtx := store.WithTenant(r.Context(), tenantID)

		tokenRows, err := sessions.SummarizeUserTokenUsage(rankingCtx, tenantID, start, end)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RANKING_TOKEN_LOAD_FAILED", err.Error())
			return
		}
		durationRows, err := sessions.SummarizeUserDurations(rankingCtx, tenantID, start, end, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RANKING_DURATION_LOAD_FAILED", err.Error())
			return
		}

		// Merge token and duration data by account (same logic as admin rankings).
		byAccount := map[string]*userRankingRow{}
		for _, t := range tokenRows {
			account := strings.ToLower(strings.TrimSpace(t.UserEmail))
			if !isUserRankingAccount(account) {
				continue
			}
			byAccount[account] = &userRankingRow{UserEmail: account, TotalTokens: t.Usage.TotalTokens()}
		}
		for _, d := range durationRows {
			account := strings.ToLower(strings.TrimSpace(d.UserEmail))
			if !isUserRankingAccount(account) {
				continue
			}
			row := byAccount[account]
			if row == nil {
				row = &userRankingRow{UserEmail: account}
				byAccount[account] = row
			}
			row.DurationSeconds += d.DurationSeconds
			row.OnlineSeconds += d.OnlineSeconds
		}

		merged := make([]userRankingRow, 0, len(byAccount))
		for _, row := range byAccount {
			merged = append(merged, *row)
		}
		assignUserRankingRanks(merged)

		// Find current user's row.
		myAccount := strings.ToLower(strings.TrimSpace(principal.Email))
		resp := myRankingResponse{
			Period:     period,
			TotalUsers: len(merged),
		}
		for _, row := range merged {
			if row.UserEmail == myAccount {
				resp.TotalTokens = row.TotalTokens
				resp.DurationSeconds = row.DurationSeconds
				resp.TokenRank = row.TokenRank
				resp.DurationRank = row.DurationRank
				break
			}
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
