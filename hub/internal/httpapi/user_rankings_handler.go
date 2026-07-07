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
	UserID          string `json:"user_id,omitempty"`
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

func GetUserRankingsHandler(sessions userUsageSummarizer, users store.UserRepository, rankingCacheOpt ...*RankingCache) http.HandlerFunc {
	var rankingCache *RankingCache
	if len(rankingCacheOpt) > 0 {
		rankingCache = rankingCacheOpt[0]
	}
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

		// Try pre-computed cache first.
		var merged []userRankingRow
		var generatedAt time.Time

		if rankingCache != nil {
			entry := rankingCache.GetOrCompute(r.Context(), tenantID, period, label, start, end)
			if entry != nil {
				merged = entry.Rows
				generatedAt = entry.GeneratedAt
			}
		}

		// Fallback to on-demand computation if cache unavailable.
		if merged == nil {
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
			merged = mergeUserRankingRows(store.WithTenant(r.Context(), tenantID), tenantID, users, tokenRows, durationRows)
			assignUserRankingRanks(merged)
			generatedAt = now
		}

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
			GeneratedAt: generatedAt,
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

func mergeUserRankingRows(ctx context.Context, tenantID string, users store.UserRepository, tokenRows []store.UserTokenSummary, durationRows []store.UserDurationSummary) []userRankingRow {
	type rankingIdentity struct {
		key     string
		userID  string
		account string
	}
	displayAccountByUserID := map[string]string{}
	resolve := func(userID, account string) rankingIdentity {
		userID = strings.TrimSpace(userID)
		account = strings.ToLower(strings.TrimSpace(account))
		if userID != "" {
			display := displayAccountByUserID[userID]
			if display == "" {
				display = rankingDisplayAccountForUser(ctx, tenantID, users, userID, account)
				displayAccountByUserID[userID] = display
			}
			return rankingIdentity{key: "user:" + userID, userID: userID, account: display}
		}
		if !isUserRankingAccount(account) {
			return rankingIdentity{}
		}
		if users != nil {
			identityType := "email"
			identityValue := account
			if isUserRankingPhone(account) {
				identityType = "phone"
				identityValue = strings.TrimPrefix(account, "phone:")
			}
			if user, err := users.GetByTenantIdentity(ctx, tenantID, identityType, identityValue); err == nil && user != nil && strings.TrimSpace(user.ID) != "" {
				userID = strings.TrimSpace(user.ID)
				display := displayAccountByUserID[userID]
				if display == "" {
					display = rankingDisplayAccountForUser(ctx, tenantID, users, userID, account)
					displayAccountByUserID[userID] = display
				}
				return rankingIdentity{key: "user:" + userID, userID: userID, account: display}
			}
		}
		return rankingIdentity{key: "account:" + account, account: account}
	}

	byKey := map[string]*userRankingRow{}
	upsert := func(identity rankingIdentity) *userRankingRow {
		if identity.key == "" || identity.account == "" || !isUserRankingAccount(identity.account) {
			return nil
		}
		row := byKey[identity.key]
		if row == nil {
			row = &userRankingRow{UserID: identity.userID, UserEmail: identity.account, UserName: identity.account}
			byKey[identity.key] = row
			return row
		}
		row.UserID = preferNonEmpty(row.UserID, identity.userID)
		row.UserEmail = preferredUserRankingAccount(row.UserEmail, identity.account)
		row.UserName = row.UserEmail
		return row
	}

	for _, t := range tokenRows {
		row := upsert(resolve(t.UserID, t.UserEmail))
		if row != nil {
			row.TotalTokens += t.Usage.TotalTokens()
		}
	}
	for _, d := range durationRows {
		row := upsert(resolve(d.UserID, d.UserEmail))
		if row != nil {
			row.DurationSeconds += d.DurationSeconds
			row.OnlineSeconds += d.OnlineSeconds
		}
	}
	merged := make([]userRankingRow, 0, len(byKey))
	for _, row := range byKey {
		merged = append(merged, *row)
	}
	return merged
}

func rankingDisplayAccountForUser(ctx context.Context, tenantID string, users store.UserRepository, userID, fallback string) string {
	fallback = strings.ToLower(strings.TrimSpace(fallback))
	if users == nil || strings.TrimSpace(userID) == "" {
		return fallback
	}
	identities, err := users.ListIdentitiesByUser(ctx, tenantID, userID)
	if err == nil {
		for _, identity := range identities {
			if identity == nil || !strings.EqualFold(strings.TrimSpace(identity.Type), "email") {
				continue
			}
			value := strings.ToLower(strings.TrimSpace(identity.Value))
			if isUserRankingEmail(value) {
				return value
			}
		}
		for _, identity := range identities {
			if identity == nil || !strings.EqualFold(strings.TrimSpace(identity.Type), "phone") {
				continue
			}
			value := strings.TrimSpace(identity.Value)
			if value != "" && isUserRankingPhone("phone:"+value) {
				return "phone:" + value
			}
		}
	}
	if user, err := users.GetByID(ctx, userID); err == nil && user != nil {
		account := strings.ToLower(strings.TrimSpace(user.Email))
		if isUserRankingAccount(account) {
			return preferredUserRankingAccount(account, fallback)
		}
	}
	return fallback
}

func preferredUserRankingAccount(current, candidate string) string {
	current = strings.ToLower(strings.TrimSpace(current))
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if candidate == "" {
		return current
	}
	if current == "" {
		return candidate
	}
	if isUserRankingEmail(candidate) && !isUserRankingEmail(current) {
		return candidate
	}
	return current
}

func preferNonEmpty(current, candidate string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	return strings.TrimSpace(candidate)
}

func assignUserRankingRanks(rows []userRankingRow) {
	tokenOrder := append([]userRankingRow(nil), rows...)
	sort.Slice(tokenOrder, func(i, j int) bool {
		if tokenOrder[i].TotalTokens == tokenOrder[j].TotalTokens {
			if tokenOrder[i].OnlineSeconds == tokenOrder[j].OnlineSeconds {
				return userRankingLessByIdentity(tokenOrder[i], tokenOrder[j])
			}
			return tokenOrder[i].OnlineSeconds > tokenOrder[j].OnlineSeconds
		}
		return tokenOrder[i].TotalTokens > tokenOrder[j].TotalTokens
	})
	tokenRanks := map[string]int{}
	for i, row := range tokenOrder {
		tokenRanks[userRankingRowKey(row)] = i + 1
	}
	durationOrder := append([]userRankingRow(nil), rows...)
	sort.Slice(durationOrder, func(i, j int) bool {
		if durationOrder[i].DurationSeconds == durationOrder[j].DurationSeconds {
			if durationOrder[i].OnlineSeconds == durationOrder[j].OnlineSeconds {
				return userRankingLessByIdentity(durationOrder[i], durationOrder[j])
			}
			return durationOrder[i].OnlineSeconds > durationOrder[j].OnlineSeconds
		}
		return durationOrder[i].DurationSeconds > durationOrder[j].DurationSeconds
	})
	durationRanks := map[string]int{}
	for i, row := range durationOrder {
		durationRanks[userRankingRowKey(row)] = i + 1
	}
	for i := range rows {
		key := userRankingRowKey(rows[i])
		rows[i].TokenRank = tokenRanks[key]
		rows[i].DurationRank = durationRanks[key]
	}
}

func userRankingRowKey(row userRankingRow) string {
	userID := strings.TrimSpace(row.UserID)
	if userID != "" {
		return "user:" + userID
	}
	return "account:" + strings.ToLower(strings.TrimSpace(row.UserEmail))
}

func userRankingLessByIdentity(a, b userRankingRow) bool {
	aEmail := strings.ToLower(strings.TrimSpace(a.UserEmail))
	bEmail := strings.ToLower(strings.TrimSpace(b.UserEmail))
	if aEmail != bEmail {
		return aEmail < bEmail
	}
	return strings.TrimSpace(a.UserID) < strings.TrimSpace(b.UserID)
}

func sortUserRankingRows(rows []userRankingRow, dimension string) {
	sort.Slice(rows, func(i, j int) bool {
		if dimension == "duration" {
			if rows[i].DurationSeconds == rows[j].DurationSeconds {
				if rows[i].OnlineSeconds == rows[j].OnlineSeconds {
					return userRankingLessByIdentity(rows[i], rows[j])
				}
				return rows[i].OnlineSeconds > rows[j].OnlineSeconds
			}
			return rows[i].DurationSeconds > rows[j].DurationSeconds
		}
		if rows[i].TotalTokens == rows[j].TotalTokens {
			if rows[i].DurationSeconds == rows[j].DurationSeconds {
				if rows[i].OnlineSeconds == rows[j].OnlineSeconds {
					return userRankingLessByIdentity(rows[i], rows[j])
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
