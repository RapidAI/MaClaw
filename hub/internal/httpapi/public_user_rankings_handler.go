package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type publicUserRankingRow struct {
	MaskedEmail     string `json:"masked_email"`
	TotalTokens     int64  `json:"total_tokens"`
	DurationSeconds int64  `json:"duration_seconds"`
	OnlineSeconds   int64  `json:"online_seconds"`
	TokenRank       int    `json:"token_rank"`
	DurationRank    int    `json:"duration_rank"`
}

type publicUserRankingResponse struct {
	Period      string                 `json:"period"`
	Month       string                 `json:"month,omitempty"`
	Dimension   string                 `json:"dimension"`
	Page        int                    `json:"page"`
	PageSize    int                    `json:"page_size"`
	Total       int                    `json:"total"`
	Rows        []publicUserRankingRow `json:"rows"`
	GeneratedAt time.Time              `json:"generated_at"`
}

// maskEmail masks an email address for public display.
// "user@example.com" -> "u***r@example.com"
// "ab@x.com" -> "a***b@x.com"
// "a@x.com" -> "a***@x.com"
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	local := email[:at]
	domain := email[at:]
	runes := []rune(local)
	if len(runes) <= 1 {
		return local + "***" + domain
	}
	if len(runes) == 2 {
		return string(runes[0]) + "***" + string(runes[1]) + domain
	}
	return string(runes[0]) + "***" + string(runes[len(runes)-1]) + domain
}

// GetPublicUserRankingsHandler returns a public (no auth) leaderboard with masked emails.
// Uses default tenant and supports daily/weekly/monthly period.
func GetPublicUserRankingsHandler(sessions userUsageSummarizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "RANKING_UNAVAILABLE", "ranking service unavailable")
			return
		}

		// Allow CORS for public page
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		tenantID := store.DefaultTenantID
		// Allow tenant_id override via query param (for multi-tenant hubs)
		if tid := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tid != "" {
			tenantID = tid
		}

		now := time.Now().UTC()
		dimension := normalizeUserRankingDimension(r.URL.Query().Get("dimension"))
		page := parseUserRankingPositiveInt(r.URL.Query().Get("page"), 1, 1, 10000)
		pageSize := 100 // fixed at 100 per page for public leaderboard

		// Support daily/weekly/monthly periods (default: monthly)
		period := normalizePublicRankingPeriod(r.URL.Query().Get("period"))
		var start, end time.Time
		var periodLabel string
		switch period {
		case "daily":
			start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			end = start.AddDate(0, 0, 1)
			periodLabel = start.Format("2006-01-02")
		case "weekly":
			// ISO week: Monday as first day
			weekday := int(now.Weekday())
			if weekday == 0 {
				weekday = 7
			}
			start = time.Date(now.Year(), now.Month(), now.Day()-(weekday-1), 0, 0, 0, 0, time.UTC)
			end = start.AddDate(0, 0, 7)
			periodLabel = start.Format("2006-01-02")
		default: // monthly
			start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			end = start.AddDate(0, 1, 0)
			periodLabel = start.Format("2006-01")
		}

		ctx := store.WithTenant(r.Context(), tenantID)
		tokenRows, err := sessions.SummarizeUserTokenUsage(ctx, tenantID, start, end)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RANKING_LOAD_FAILED", "failed to load ranking data")
			return
		}
		durationRows, err := sessions.SummarizeUserDurations(ctx, tenantID, start, end, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RANKING_LOAD_FAILED", "failed to load ranking data")
			return
		}

		// Merge (reuse existing logic pattern)
		byEmail := map[string]*userRankingRow{}
		for _, t := range tokenRows {
			email := strings.ToLower(strings.TrimSpace(t.UserEmail))
			if !isUserRankingEmail(email) {
				continue
			}
			byEmail[email] = &userRankingRow{UserEmail: email, TotalTokens: t.Usage.TotalTokens()}
		}
		for _, d := range durationRows {
			email := strings.ToLower(strings.TrimSpace(d.UserEmail))
			if !isUserRankingEmail(email) {
				continue
			}
			row := byEmail[email]
			if row == nil {
				row = &userRankingRow{UserEmail: email}
				byEmail[email] = row
			}
			row.DurationSeconds += d.DurationSeconds
			row.OnlineSeconds += d.OnlineSeconds
		}

		merged := make([]userRankingRow, 0, len(byEmail))
		for _, row := range byEmail {
			merged = append(merged, *row)
		}
		assignUserRankingRanks(merged)
		sortUserRankingRows(merged, dimension)

		total := len(merged)
		startIdx := (page - 1) * pageSize
		if startIdx > total {
			startIdx = total
		}
		endIdx := startIdx + pageSize
		if endIdx > total {
			endIdx = total
		}

		// Convert to public rows with masked emails
		publicRows := make([]publicUserRankingRow, 0, endIdx-startIdx)
		for _, row := range merged[startIdx:endIdx] {
			publicRows = append(publicRows, publicUserRankingRow{
				MaskedEmail:     maskEmail(row.UserEmail),
				TotalTokens:     row.TotalTokens,
				DurationSeconds: row.DurationSeconds,
				OnlineSeconds:   row.OnlineSeconds,
				TokenRank:       row.TokenRank,
				DurationRank:    row.DurationRank,
			})
		}

		resp := publicUserRankingResponse{
			Period:      period,
			Month:       periodLabel,
			Dimension:   dimension,
			Page:        page,
			PageSize:    pageSize,
			Total:       total,
			Rows:        publicRows,
			GeneratedAt: now,
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func normalizePublicRankingPeriod(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "daily", "day", "today":
		return "daily"
	case "weekly", "week":
		return "weekly"
	default:
		return "monthly"
	}
}
