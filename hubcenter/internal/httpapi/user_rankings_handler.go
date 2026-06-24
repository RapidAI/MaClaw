package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type centerUserUsageRepo interface {
	UpsertDaily(ctx context.Context, items []*store.HubUserUsageDaily) error
	Summarize(ctx context.Context, hubID, tenantID string, start, end time.Time) ([]*store.HubUserUsageDaily, error)
}

type hubUserUsageSyncRequest struct {
	HubSecret string                    `json:"hub_secret"`
	Items     []hubUserUsageSyncPayload `json:"items"`
}

type hubUserUsageSyncPayload struct {
	TenantID          string `json:"tenant_id,omitempty"`
	UserEmail         string `json:"user_email"`
	Day               string `json:"day"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CachedInputTokens int64  `json:"cached_input_tokens"`
	CacheWriteTokens  int64  `json:"cache_write_tokens"`
	DurationSeconds   int64  `json:"duration_seconds"`
}

type centerUserRankingRow struct {
	HubID           string `json:"hub_id"`
	HubName         string `json:"hub_name,omitempty"`
	TenantID        string `json:"tenant_id,omitempty"`
	UserEmail       string `json:"user_email"`
	TotalTokens     int64  `json:"total_tokens"`
	DurationSeconds int64  `json:"duration_seconds"`
	TokenRank       int    `json:"token_rank"`
	DurationRank    int    `json:"duration_rank"`
}

type centerUserRankingResponse struct {
	Period      string                 `json:"period"`
	Date        string                 `json:"date,omitempty"`
	Month       string                 `json:"month,omitempty"`
	Year        string                 `json:"year,omitempty"`
	Dimension   string                 `json:"dimension"`
	GlobalTop   []centerUserRankingRow `json:"global_top"`
	FilteredTop []centerUserRankingRow `json:"filtered_top"`
	GeneratedAt time.Time              `json:"generated_at"`
}

func HubUserUsageSyncHandler(hubService *hubs.Service, repo centerUserUsageRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_USAGE_UNAVAILABLE", "user usage repository is unavailable")
			return
		}
		hubID := strings.TrimSpace(r.PathValue("id"))
		if hubID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_HUB_ID", "Hub id is required")
			return
		}
		var req hubUserUsageSyncRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if hubService == nil || hubService.VerifyHubSecret(r.Context(), hubID, req.HubSecret) != nil {
			writeError(w, http.StatusUnauthorized, "HUB_UNAUTHORIZED", "Hub is not authorized")
			return
		}
		items := make([]*store.HubUserUsageDaily, 0, len(req.Items))
		now := time.Now().UTC()
		for _, raw := range req.Items {
			email := strings.ToLower(strings.TrimSpace(raw.UserEmail))
			day := strings.TrimSpace(raw.Day)
			if email == "" || day == "" {
				continue
			}
			items = append(items, &store.HubUserUsageDaily{
				HubID:             hubID,
				TenantID:          normalizeHubSyncTenantID(raw.TenantID),
				UserEmail:         email,
				Day:               day,
				InputTokens:       raw.InputTokens,
				OutputTokens:      raw.OutputTokens,
				CachedInputTokens: raw.CachedInputTokens,
				CacheWriteTokens:  raw.CacheWriteTokens,
				DurationSeconds:   raw.DurationSeconds,
				UpdatedAt:         now,
			})
		}
		if err := repo.UpsertDaily(r.Context(), items); err != nil {
			writeError(w, http.StatusInternalServerError, "USER_USAGE_SYNC_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "synced": len(items)})
	}
}

func CenterUserRankingsHandler(repo centerUserUsageRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			writeError(w, http.StatusServiceUnavailable, "USER_RANKINGS_UNAVAILABLE", "user usage repository is unavailable")
			return
		}
		now := time.Now().UTC()
		period := normalizeCenterRankingPeriod(r.URL.Query().Get("period"))
		start, end, label := centerRankingRange(r.URL.Query(), period, now)
		dimension := normalizeCenterRankingDimension(r.URL.Query().Get("dimension"))
		limit := parseCenterRankingPositiveInt(r.URL.Query().Get("limit"), 10, 1, 10)

		globalRows, err := repo.Summarize(r.Context(), "", "", start, end)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_RANKINGS_LOAD_FAILED", err.Error())
			return
		}
		hubID := strings.TrimSpace(r.URL.Query().Get("hub_id"))
		tenantID := normalizeHubSyncTenantID(r.URL.Query().Get("tenant_id"))
		filteredRows, err := repo.Summarize(r.Context(), hubID, tenantID, start, end)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "USER_RANKINGS_LOAD_FAILED", err.Error())
			return
		}
		global := buildCenterRankingRows(globalRows, dimension, limit)
		filtered := buildCenterRankingRows(filteredRows, dimension, limit)
		resp := centerUserRankingResponse{Period: period, Dimension: dimension, GlobalTop: global, FilteredTop: filtered, GeneratedAt: now}
		switch period {
		case "monthly":
			resp.Month = label
		case "yearly":
			resp.Year = label
		default:
			resp.Date = label
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func buildCenterRankingRows(items []*store.HubUserUsageDaily, dimension string, limit int) []centerUserRankingRow {
	rows := make([]centerUserRankingRow, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.UserEmail) == "" {
			continue
		}
		rows = append(rows, centerUserRankingRow{
			HubID:           item.HubID,
			HubName:         item.HubName,
			TenantID:        item.TenantID,
			UserEmail:       strings.ToLower(strings.TrimSpace(item.UserEmail)),
			TotalTokens:     item.TotalTokens(),
			DurationSeconds: item.DurationSeconds,
		})
	}
	assignCenterRankingRanks(rows)
	sortCenterRankingRows(rows, dimension)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func assignCenterRankingRanks(rows []centerUserRankingRow) {
	tokenOrder := append([]centerUserRankingRow(nil), rows...)
	sort.Slice(tokenOrder, func(i, j int) bool {
		if tokenOrder[i].TotalTokens == tokenOrder[j].TotalTokens {
			return tokenOrder[i].UserEmail < tokenOrder[j].UserEmail
		}
		return tokenOrder[i].TotalTokens > tokenOrder[j].TotalTokens
	})
	durationOrder := append([]centerUserRankingRow(nil), rows...)
	sort.Slice(durationOrder, func(i, j int) bool {
		if durationOrder[i].DurationSeconds == durationOrder[j].DurationSeconds {
			return durationOrder[i].UserEmail < durationOrder[j].UserEmail
		}
		return durationOrder[i].DurationSeconds > durationOrder[j].DurationSeconds
	})
	tokenRanks := map[string]int{}
	durationRanks := map[string]int{}
	for i, row := range tokenOrder {
		tokenRanks[centerRankingKey(row)] = i + 1
	}
	for i, row := range durationOrder {
		durationRanks[centerRankingKey(row)] = i + 1
	}
	for i := range rows {
		key := centerRankingKey(rows[i])
		rows[i].TokenRank = tokenRanks[key]
		rows[i].DurationRank = durationRanks[key]
	}
}

func centerRankingKey(row centerUserRankingRow) string {
	return row.HubID + "\x00" + row.TenantID + "\x00" + row.UserEmail
}

func sortCenterRankingRows(rows []centerUserRankingRow, dimension string) {
	sort.Slice(rows, func(i, j int) bool {
		if dimension == "duration" {
			if rows[i].DurationSeconds == rows[j].DurationSeconds {
				return rows[i].UserEmail < rows[j].UserEmail
			}
			return rows[i].DurationSeconds > rows[j].DurationSeconds
		}
		if rows[i].TotalTokens == rows[j].TotalTokens {
			if dimension == "tokens" || rows[i].DurationSeconds == rows[j].DurationSeconds {
				return rows[i].UserEmail < rows[j].UserEmail
			}
			return rows[i].DurationSeconds > rows[j].DurationSeconds
		}
		return rows[i].TotalTokens > rows[j].TotalTokens
	})
}

func normalizeCenterRankingPeriod(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "monthly", "month":
		return "monthly"
	case "yearly", "year":
		return "yearly"
	default:
		return "daily"
	}
}

func normalizeCenterRankingDimension(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "token", "tokens":
		return "tokens"
	case "time", "duration":
		return "duration"
	default:
		return "all"
	}
}

func centerRankingRange(q map[string][]string, period string, now time.Time) (time.Time, time.Time, string) {
	switch period {
	case "monthly":
		label := firstCenterQueryValue(q, "month")
		if t, err := time.ParseInLocation("2006-01", label, time.UTC); err == nil {
			return t, t.AddDate(0, 1, 0), label
		}
		t := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return t, t.AddDate(0, 1, 0), t.Format("2006-01")
	case "yearly":
		label := firstCenterQueryValue(q, "year")
		if y, err := strconv.Atoi(label); err == nil && y >= 1970 && y <= 9999 {
			t := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
			return t, t.AddDate(1, 0, 0), label
		}
		t := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return t, t.AddDate(1, 0, 0), t.Format("2006")
	default:
		label := firstCenterQueryValue(q, "date")
		if t, err := time.ParseInLocation("2006-01-02", label, time.UTC); err == nil {
			return t, t.AddDate(0, 0, 1), label
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return t, t.AddDate(0, 0, 1), t.Format("2006-01-02")
	}
}

func firstCenterQueryValue(q map[string][]string, key string) string {
	if vals := q[key]; len(vals) > 0 {
		return strings.TrimSpace(vals[0])
	}
	return ""
}

func parseCenterRankingPositiveInt(raw string, fallback, min, max int) int {
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
