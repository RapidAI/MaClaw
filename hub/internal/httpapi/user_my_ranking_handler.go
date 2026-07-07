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
// Leverages the pre-computed ranking cache for instant response.
func GetMyRankingHandler(identity *auth.IdentityService, sessions userUsageSummarizer, rankingCacheOpt ...*RankingCache) http.HandlerFunc {
	var rankingCache *RankingCache
	if len(rankingCacheOpt) > 0 {
		rankingCache = rankingCacheOpt[0]
	}
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
		periodLabel := start.Format("2006-01")

		// Try pre-computed cache first.
		var merged []userRankingRow
		if rankingCache != nil {
			entry := rankingCache.GetOrCompute(r.Context(), tenantID, period, periodLabel, start, end)
			if entry != nil {
				merged = entry.Rows
			}
		}

		// Fallback to on-demand computation.
		if merged == nil {
			rankingCtx := store.WithTenant(r.Context(), tenantID)
			tokenRows, err2 := sessions.SummarizeUserTokenUsage(rankingCtx, tenantID, start, end)
			if err2 != nil {
				writeError(w, http.StatusInternalServerError, "RANKING_TOKEN_LOAD_FAILED", err2.Error())
				return
			}
			durationRows, err2 := sessions.SummarizeUserDurations(rankingCtx, tenantID, start, end, now)
			if err2 != nil {
				writeError(w, http.StatusInternalServerError, "RANKING_DURATION_LOAD_FAILED", err2.Error())
				return
			}
			var users store.UserRepository
			if identity != nil {
				users = identity.UsersRepo()
			}
			merged = mergeUserRankingRows(rankingCtx, tenantID, users, tokenRows, durationRows)
			assignUserRankingRanks(merged)
		}

		// Find current user's row.
		myAccount := strings.ToLower(strings.TrimSpace(principal.Email))
		myUserID := strings.TrimSpace(principal.UserID)
		resp := myRankingResponse{
			Period:     period,
			TotalUsers: len(merged),
		}
		for _, row := range merged {
			if (myUserID != "" && row.UserID == myUserID) || (myUserID == "" && row.UserEmail == myAccount) {
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
