package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type userRankingRow struct {
	UserEmail       string `json:"user_email"`
	UserName        string `json:"user_name"`
	TotalTokens     int64  `json:"total_tokens"`
	DurationSeconds int64  `json:"duration_seconds"`
	OnlineSeconds   int64  `json:"online_seconds"`
	TokenRank       int    `json:"token_rank"`
	DurationRank    int    `json:"duration_rank"`
}

type userRankingResponse struct {
	Period      string           `json:"period"`
	Date        string           `json:"date,omitempty"`
	Month       string           `json:"month,omitempty"`
	Year        string           `json:"year,omitempty"`
	Dimension   string           `json:"dimension"`
	Page        int              `json:"page"`
	PageSize    int              `json:"page_size"`
	Total       int              `json:"total"`
	Rows        []userRankingRow `json:"rows"`
	GeneratedAt time.Time        `json:"generated_at"`
}

type userDurationSummarizer interface {
	SummarizeUserDurations(ctx context.Context, tenantID string, start, end, now time.Time) ([]store.UserDurationSummary, error)
}

type userTokenSummarizer interface {
	SummarizeUserTokenUsage(ctx context.Context, tenantID string, start, end time.Time) ([]store.UserTokenSummary, error)
}

type userUsageSummarizer interface {
	userDurationSummarizer
	userTokenSummarizer
}

func GetUserRankingsHandler(sessions userUsageSummarizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_RANKINGS_UNAVAILABLE", "session repository is unavailable")
			return
		}
		tenantID := RequestTenantID(r)
		now := time.Now().UTC()
		period := normalizeUserRankingPeriod(r.URL.Query().Get("period"))
		start, end, label := userRankingRange(r.URL.Query(), period, now)
		dimension := normalizeUserRankingDimension(r.URL.Query().Get("dimension"))
		page := parseUserRankingPositiveInt(r.URL.Query().Get("page"), 1, 1, 1000000)
		pageSize := parseUserRankingPositiveInt(r.URL.Query().Get("page_size"), 100, 1, 100)

		tokenRows, err := sessions.SummarizeUserTokenUsage(store.WithTenant(r.Context(), tenantID), tenantID, start, end)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_RANKINGS_TOKEN_LOAD_FAILED", err.Error())
			return
		}
		durationRows, err := sessions.SummarizeUserDurations(store.WithTenant(r.Context(), tenantID), tenantID, start, end, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_RANKINGS_DURATION_LOAD_FAILED", err.Error())
			return
		}
		byAccount := map[string]*userRankingRow{}
		for _, t := range tokenRows {
			account := strings.ToLower(strings.TrimSpace(t.UserEmail))
			if !isUserRankingAccount(account) {
				continue
			}
			byAccount[account] = &userRankingRow{UserEmail: account, UserName: account, TotalTokens: t.Usage.TotalTokens()}
		}
		for _, d := range durationRows {
			account := strings.ToLower(strings.TrimSpace(d.UserEmail))
			if !isUserRankingAccount(account) {
				continue
			}
			row := byAccount[account]
			if row == nil {
				row = &userRankingRow{UserEmail: account, UserName: account}
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
		resp := userRankingResponse{
			Period:      period,
			Dimension:   dimension,
			Page:        page,
			PageSize:    pageSize,
			Total:       total,
			Rows:        merged[startIdx:endIdx],
			GeneratedAt: now,
		}
		if period == "daily" {
			resp.Date = label
		} else if period == "monthly" {
			resp.Month = label
		} else {
			resp.Year = label
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func assignUserRankingRanks(rows []userRankingRow) {
	tokenOrder := append([]userRankingRow(nil), rows...)
	sort.Slice(tokenOrder, func(i, j int) bool {
		if tokenOrder[i].TotalTokens == tokenOrder[j].TotalTokens {
			if tokenOrder[i].OnlineSeconds == tokenOrder[j].OnlineSeconds {
				return tokenOrder[i].UserEmail < tokenOrder[j].UserEmail
			}
			return tokenOrder[i].OnlineSeconds > tokenOrder[j].OnlineSeconds
		}
		return tokenOrder[i].TotalTokens > tokenOrder[j].TotalTokens
	})
	tokenRanks := map[string]int{}
	for i, row := range tokenOrder {
		tokenRanks[row.UserEmail] = i + 1
	}
	durationOrder := append([]userRankingRow(nil), rows...)
	sort.Slice(durationOrder, func(i, j int) bool {
		if durationOrder[i].DurationSeconds == durationOrder[j].DurationSeconds {
			if durationOrder[i].OnlineSeconds == durationOrder[j].OnlineSeconds {
				return durationOrder[i].UserEmail < durationOrder[j].UserEmail
			}
			return durationOrder[i].OnlineSeconds > durationOrder[j].OnlineSeconds
		}
		return durationOrder[i].DurationSeconds > durationOrder[j].DurationSeconds
	})
	durationRanks := map[string]int{}
	for i, row := range durationOrder {
		durationRanks[row.UserEmail] = i + 1
	}
	for i := range rows {
		rows[i].TokenRank = tokenRanks[rows[i].UserEmail]
		rows[i].DurationRank = durationRanks[rows[i].UserEmail]
	}
}

func sortUserRankingRows(rows []userRankingRow, dimension string) {
	sort.Slice(rows, func(i, j int) bool {
		if dimension == "duration" {
			if rows[i].DurationSeconds == rows[j].DurationSeconds {
				if rows[i].OnlineSeconds == rows[j].OnlineSeconds {
					return rows[i].UserEmail < rows[j].UserEmail
				}
				return rows[i].OnlineSeconds > rows[j].OnlineSeconds
			}
			return rows[i].DurationSeconds > rows[j].DurationSeconds
		}
		if rows[i].TotalTokens == rows[j].TotalTokens {
			if rows[i].DurationSeconds == rows[j].DurationSeconds {
				if rows[i].OnlineSeconds == rows[j].OnlineSeconds {
					return rows[i].UserEmail < rows[j].UserEmail
				}
				return rows[i].OnlineSeconds > rows[j].OnlineSeconds
			}
			return rows[i].DurationSeconds > rows[j].DurationSeconds
		}
		return rows[i].TotalTokens > rows[j].TotalTokens
	})
}

func isUserRankingAccount(account string) bool {
	account = strings.TrimSpace(account)
	return isUserRankingEmail(account) || isUserRankingPhone(account)
}

func isUserRankingEmail(email string) bool {
	email = strings.TrimSpace(email)
	return strings.Count(email, "@") == 1 && !strings.ContainsAny(email, " \t\r\n") && !strings.HasPrefix(email, "@") && !strings.HasSuffix(email, "@")
}

func isUserRankingPhone(account string) bool {
	account = strings.TrimSpace(strings.ToLower(account))
	if !strings.HasPrefix(account, "phone:") || strings.ContainsAny(account, " \t\r\n") {
		return false
	}
	digits := strings.TrimPrefix(account, "phone:")
	if len(digits) < 6 || len(digits) > 20 {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalizeUserRankingPeriod(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "monthly", "month":
		return "monthly"
	case "yearly", "year":
		return "yearly"
	default:
		return "daily"
	}
}

func normalizeUserRankingDimension(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "token", "tokens":
		return "tokens"
	case "time", "duration":
		return "duration"
	default:
		return "all"
	}
}

func userRankingRange(q map[string][]string, period string, now time.Time) (time.Time, time.Time, string) {
	loc := time.UTC
	switch period {
	case "monthly":
		label := firstQueryValue(q, "month")
		if t, err := time.ParseInLocation("2006-01", label, loc); err == nil {
			return t, t.AddDate(0, 1, 0), label
		}
		t := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return t, t.AddDate(0, 1, 0), t.Format("2006-01")
	case "yearly":
		label := firstQueryValue(q, "year")
		if y, err := strconv.Atoi(label); err == nil && y >= 1970 && y <= 9999 {
			t := time.Date(y, 1, 1, 0, 0, 0, 0, loc)
			return t, t.AddDate(1, 0, 0), label
		}
		t := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		return t, t.AddDate(1, 0, 0), t.Format("2006")
	default:
		label := firstQueryValue(q, "date")
		if t, err := time.ParseInLocation("2006-01-02", label, loc); err == nil {
			return t, t.AddDate(0, 0, 1), label
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return t, t.AddDate(0, 0, 1), t.Format("2006-01-02")
	}
}

func firstQueryValue(q map[string][]string, key string) string {
	if vals := q[key]; len(vals) > 0 {
		return strings.TrimSpace(vals[0])
	}
	return ""
}

func parseUserRankingPositiveInt(raw string, fallback, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		n = fallback
	}
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return n
}
