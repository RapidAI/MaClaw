package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// tokenBankCapHit reports which owner-configured limit this share crossed,
// including the usage row just inserted in the same transaction. Zero caps
// are unlimited. A spike versus earlier days is not a limit.
func tokenBankCapHit(ctx context.Context, tx *sql.Tx, shareID string, now time.Time) (string, error) {
	var dailyCap, monthlyCap int64
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(daily_token_cap, 0), COALESCE(monthly_token_cap, 0)
		   FROM token_bank_shares WHERE id = ?`, shareID).Scan(&dailyCap, &monthlyCap)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("load token bank caps: %w", err)
	}
	dayStart := tokenBankDayStart(now)
	monthStart := tokenBankMonthStart(now)
	if dailyCap > 0 {
		today, err := tokenBankUsageTokens(ctx, tx, shareID, dayStart, time.Time{})
		if err != nil {
			return "", err
		}
		if today > dailyCap {
			return "daily", nil
		}
	}
	if monthlyCap > 0 {
		month, err := tokenBankUsageTokens(ctx, tx, shareID, monthStart, time.Time{})
		if err != nil {
			return "", err
		}
		if month > monthlyCap {
			return "monthly", nil
		}
	}
	return "", nil
}

func tokenBankUsageTokens(ctx context.Context, tx *sql.Tx, shareID string, from, until time.Time) (int64, error) {
	query := `SELECT COALESCE(SUM(input_tokens + output_tokens + cached_input_tokens + cache_write_tokens), 0)
	            FROM token_bank_usage WHERE share_id = ? AND created_at >= ?`
	args := []any{shareID, from.UTC().Format(time.RFC3339)}
	if !until.IsZero() {
		query += ` AND created_at < ?`
		args = append(args, until.UTC().Format(time.RFC3339))
	}
	var n int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("sum token bank usage: %w", err)
	}
	return n, nil
}

// TokenBankUsageDay is one day of a share owner's earnings.
type TokenBankUsageDay struct {
	Day          string `json:"day"`
	NetMicro     int64  `json:"net_micro"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

// TokenBankUsageModel is one model's earnings inside the same window.
type TokenBankUsageModel struct {
	Model    string `json:"model"`
	NetMicro int64  `json:"net_micro"`
}

// TokenBankUsageExportRow is one settled call for the CSV export.
type TokenBankUsageExportRow struct {
	CreatedAt    string
	RequestID    string
	ModelName    string
	InputTokens  int64
	OutputTokens int64
	GrossMicro   int64
	FeeMicro     int64
	NetMicro     int64
	ChargedMicro int64
	SelfUse      bool
}

func clampUsageDays(days int) int {
	if days <= 0 {
		return 30
	}
	if days > 366 {
		return 366
	}
	return days
}

// UsageDaily returns the owner's per-day net and the top models for the window.
func (r *TokenBankRepo) UsageDaily(ctx context.Context, ownerID string, days int) ([]TokenBankUsageDay, []TokenBankUsageModel, error) {
	if r == nil {
		return nil, nil, nil
	}
	ownerID = strings.TrimSpace(ownerID)
	days = clampUsageDays(days)
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	dayRows, err := r.read.QueryContext(ctx,
		`SELECT substr(created_at, 1, 10), COALESCE(SUM(net_micro), 0),
		        COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0)
		   FROM token_bank_usage
		  WHERE owner_user_id = ? AND created_at >= ?
		  GROUP BY substr(created_at, 1, 10)
		  ORDER BY 1`, ownerID, since)
	if err != nil {
		return nil, nil, fmt.Errorf("token bank usage by day: %w", err)
	}
	defer dayRows.Close()
	var daysOut []TokenBankUsageDay
	for dayRows.Next() {
		var day TokenBankUsageDay
		if err := dayRows.Scan(&day.Day, &day.NetMicro, &day.InputTokens, &day.OutputTokens); err != nil {
			return nil, nil, err
		}
		daysOut = append(daysOut, day)
	}
	if err := dayRows.Err(); err != nil {
		return nil, nil, err
	}
	modelRows, err := r.read.QueryContext(ctx,
		`SELECT model_name, COALESCE(SUM(net_micro), 0)
		   FROM token_bank_usage
		  WHERE owner_user_id = ? AND created_at >= ?
		  GROUP BY model_name
		  ORDER BY 2 DESC
		  LIMIT 10`, ownerID, since)
	if err != nil {
		return nil, nil, fmt.Errorf("token bank usage by model: %w", err)
	}
	defer modelRows.Close()
	var models []TokenBankUsageModel
	for modelRows.Next() {
		var model TokenBankUsageModel
		if err := modelRows.Scan(&model.Model, &model.NetMicro); err != nil {
			return nil, nil, err
		}
		models = append(models, model)
	}
	return daysOut, models, modelRows.Err()
}

// UsageExport returns up to 5000 settled rows for the owner's CSV download.
func (r *TokenBankRepo) UsageExport(ctx context.Context, ownerID string, days int) ([]TokenBankUsageExportRow, error) {
	if r == nil {
		return nil, nil
	}
	ownerID = strings.TrimSpace(ownerID)
	days = clampUsageDays(days)
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	rows, err := r.read.QueryContext(ctx,
		`SELECT created_at, request_id, model_name, input_tokens, output_tokens,
		        gross_micro, fee_micro, net_micro, charged_micro, self_use
		   FROM token_bank_usage
		  WHERE owner_user_id = ? AND created_at >= ?
		  ORDER BY created_at
		  LIMIT 5000`, ownerID, since)
	if err != nil {
		return nil, fmt.Errorf("token bank usage export: %w", err)
	}
	defer rows.Close()
	var out []TokenBankUsageExportRow
	for rows.Next() {
		var row TokenBankUsageExportRow
		var selfUse int
		if err := rows.Scan(&row.CreatedAt, &row.RequestID, &row.ModelName, &row.InputTokens, &row.OutputTokens,
			&row.GrossMicro, &row.FeeMicro, &row.NetMicro, &row.ChargedMicro, &selfUse); err != nil {
			return nil, err
		}
		row.SelfUse = selfUse != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// --- gross-margin view (§9 #20) ---------------------------------------------

// TokenBankMarginRow is one slice of the platform's gross margin.
//
// The three numbers an admin prices against are charged, net, and their
// difference. Gross is carried too because it is the number the price book
// produces and the one that reveals an inversion: when gross exceeds charged the
// platform sold the call for less than its own list price would pay out.
type TokenBankMarginRow struct {
	// Key is the group value: a model name, a share id, a day, or "" for the
	// grand total. Empty rather than a sentinel word so a group with a blank
	// name cannot be confused with the total.
	Key          string `json:"key"`
	ChargedMicro int64  `json:"charged_micro"` // what the consumer paid
	GrossMicro   int64  `json:"gross_micro"`   // list price before the platform fee
	FeeMicro     int64  `json:"fee_micro"`     // the platform's cut of gross
	NetMicro     int64  `json:"net_micro"`     // what the sharer actually received
	MarginMicro  int64  `json:"margin_micro"`  // charged - net
	Calls        int64  `json:"calls"`
	// InvertedCount counts rows where gross exceeded charged — the §13 inversion
	// signal. It is deliberately NOT the same as ClampedCount: a 10% fee absorbs
	// a small inversion, so clamping only happens once the gap is wider than the
	// fee. Reporting only one of the two hides either the mild cases or the
	// severity of the bad ones.
	InvertedCount int64 `json:"inverted_count"`
	// ClampedCount counts rows where §5 ⑥ reduced the payout because even after
	// the fee it still exceeded what the consumer paid.
	ClampedCount int64 `json:"clamped_count"`
	// ShortfallMicro is the sharer payout the clamp held back: sum of
	// (gross - fee - net) where that difference is positive. The fee stays with
	// the platform, so a list-price gap the fee already absorbs is not an
	// overpay. A positive shortfall is what the platform would have paid the
	// sharer above the recorded net had §5 ⑥ not capped it.
	ShortfallMicro int64 `json:"shortfall_micro"`
}

// MarginRate is the platform's share of what the consumer paid.
//
// Zero when nothing was charged, because a free route has no rate to speak of —
// dividing by zero and reporting NaN to an admin is worse than saying nothing.
func (m TokenBankMarginRow) MarginRate() float64 {
	if m.ChargedMicro <= 0 {
		return 0
	}
	return float64(m.MarginMicro) / float64(m.ChargedMicro)
}

// TokenBankMarginGroup is an allowed grouping for the margin view. Whitelisted
// rather than interpolated from the query string: the value lands in a GROUP BY
// clause, and a column name is not something to accept from a request.
type TokenBankMarginGroup string

const (
	TokenBankMarginByModel TokenBankMarginGroup = "model"
	TokenBankMarginByShare TokenBankMarginGroup = "share"
	TokenBankMarginByDay   TokenBankMarginGroup = "day"
)

// marginGroupColumn maps a group to a real column expression, or "" when the
// group is not one of the three known ones.
//
// Returning "" rather than a constant like `”` matters: a caller cannot inject
// SQL through here, and the caller can then *skip* the grouped query entirely.
// Grouping on a constant would produce one row that is itself the grand total,
// which would then be reported twice — once as a group and once as the total.
func marginGroupColumn(group TokenBankMarginGroup) string {
	switch group {
	case TokenBankMarginByModel:
		return "model_name"
	case TokenBankMarginByShare:
		return "share_id"
	case TokenBankMarginByDay:
		return "substr(created_at, 1, 10)"
	default:
		return ""
	}
}

// UsageMargins returns the platform's gross margin for a window, grouped as
// asked and always with a grand total appended.
//
// Only settled usage counts: an unsettled call has no charged figure yet, and
// including it would understate the margin.
func (r *TokenBankRepo) UsageMargins(ctx context.Context, days int, group TokenBankMarginGroup) ([]TokenBankMarginRow, error) {
	if r == nil {
		return nil, nil
	}
	days = clampUsageDays(days)
	since := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	column := marginGroupColumn(group)

	// The margin is charged - net, which §5 ⑥ guarantees is never negative: net
	// is clamped to charged. It is computed here rather than in SQL subtraction
	// chains so the definition lives in exactly one place.
	sums := `COALESCE(SUM(charged_micro), 0), COALESCE(SUM(gross_micro), 0),
			COALESCE(SUM(fee_micro), 0), COALESCE(SUM(net_micro), 0),
			COUNT(*),
			COALESCE(SUM(CASE WHEN gross_micro > charged_micro THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(net_clamped), 0),
			COALESCE(SUM(CASE WHEN gross_micro - fee_micro > net_micro THEN gross_micro - fee_micro - net_micro ELSE 0 END), 0)
		   FROM token_bank_usage
		  WHERE created_at >= ?`

	// Days read chronologically; everything else is ranked by what the sharer
	// earned, because that is the column an admin scans for outliers.
	order := ` ORDER BY 5 DESC, 1`
	if group == TokenBankMarginByDay {
		order = ` ORDER BY 1`
	}
	var out []TokenBankMarginRow
	if column != "" {
		grouped := `SELECT ` + column + `, ` + sums + ` GROUP BY 1` + order
		rows, err := r.read.QueryContext(ctx, grouped, since)
		if err != nil {
			return nil, fmt.Errorf("token bank margin by %s: %w", group, err)
		}
		defer rows.Close()
		for rows.Next() {
			var row TokenBankMarginRow
			if err := rows.Scan(&row.Key, &row.ChargedMicro, &row.GrossMicro, &row.FeeMicro,
				&row.NetMicro, &row.Calls, &row.InvertedCount, &row.ClampedCount, &row.ShortfallMicro); err != nil {
				return nil, err
			}
			row.MarginMicro = row.ChargedMicro - row.NetMicro
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// The total is queried separately rather than summed in Go: an ungrouped
	// aggregate is what the database is for, and summing groups by hand would
	// miss rows whose group key is NULL.
	//
	// Its key column is a literal empty string, not the group expression. SQLite
	// permits a bare column alongside aggregates and would then return that
	// column from an arbitrary row — a "total" labelled with one model's name is
	// exactly the kind of quiet wrongness this view exists to prevent.
	var total TokenBankMarginRow
	if err := r.read.QueryRowContext(ctx, `SELECT '', `+sums, since).Scan(&total.Key, &total.ChargedMicro,
		&total.GrossMicro, &total.FeeMicro, &total.NetMicro, &total.Calls,
		&total.InvertedCount, &total.ClampedCount, &total.ShortfallMicro); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("token bank margin total: %w", err)
		}
	}
	total.MarginMicro = total.ChargedMicro - total.NetMicro
	out = append(out, total)
	return out, nil
}

// tokenBankCalendarLoc is China Standard Time, which has no daylight-saving
// shift. Share-card windows and the daily/monthly caps both use it, so "today"
// is the same civil day in both places.
var tokenBankCalendarLoc = time.FixedZone("CST", 8*3600)

const (
	TokenBankRangeToday = "today"
	TokenBankRangeMonth = "month"
	TokenBankRangeAll   = "all"
)

// CanonicalTokenBankRange accepts the share-list window names. An empty value
// is "all", which keeps older clients on the lifetime figures.
func CanonicalTokenBankRange(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", TokenBankRangeAll:
		return TokenBankRangeAll, true
	case TokenBankRangeToday:
		return TokenBankRangeToday, true
	case TokenBankRangeMonth:
		return TokenBankRangeMonth, true
	default:
		return "", false
	}
}

func tokenBankDayStart(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	local := now.In(tokenBankCalendarLoc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, tokenBankCalendarLoc).UTC()
}

func tokenBankMonthStart(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	local := now.In(tokenBankCalendarLoc)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, tokenBankCalendarLoc).UTC()
}

// TokenBankCivilBounds returns the UTC instants where the current China civil
// day and month begin. Callers that insert test rows use the same instants the
// SUM queries compare against.
func TokenBankCivilBounds(now time.Time) (dayStart, monthStart time.Time) {
	return tokenBankDayStart(now), tokenBankMonthStart(now)
}

// TokenBankUsageTotals is one settled window: a civil day, a civil month, or
// the whole history of a share or model. The numbers are sums of the usage
// rows, which already applied §5 per call. Summing the tokens and recomputing
// the fee would not match, because each call rounds on its own.
type TokenBankUsageTotals struct {
	Calls             int64
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	GrossMicro        int64
	FeeMicro          int64
	NetMicro          int64
	ChargedMicro      int64
	ClampedCalls      int64
}

// Tokens is every token the window consumed, including cache read and write.
func (t TokenBankUsageTotals) Tokens() int64 {
	return t.InputTokens + t.OutputTokens + t.CachedInputTokens + t.CacheWriteTokens
}

func (t *TokenBankUsageTotals) appendScan(dest []any) []any {
	return append(dest,
		&t.Calls, &t.InputTokens, &t.OutputTokens, &t.CachedInputTokens, &t.CacheWriteTokens,
		&t.GrossMicro, &t.FeeMicro, &t.NetMicro, &t.ChargedMicro, &t.ClampedCalls)
}

func addUsageTotals(a, b TokenBankUsageTotals) TokenBankUsageTotals {
	a.Calls += b.Calls
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.GrossMicro += b.GrossMicro
	a.FeeMicro += b.FeeMicro
	a.NetMicro += b.NetMicro
	a.ChargedMicro += b.ChargedMicro
	a.ClampedCalls += b.ClampedCalls
	return a
}

// TokenBankUsageBuckets holds the three windows a share card shows at once.
// Label is the model name as stored, and is empty for a share-level total.
type TokenBankUsageBuckets struct {
	Label string
	Today TokenBankUsageTotals
	Month TokenBankUsageTotals
	All   TokenBankUsageTotals
}

// CombineUsageBuckets adds every entry. The model map folds case first, so
// combining it is the share total and does not double-count.
func CombineUsageBuckets(in map[string]TokenBankUsageBuckets) TokenBankUsageBuckets {
	var out TokenBankUsageBuckets
	for _, buckets := range in {
		out.Today = addUsageTotals(out.Today, buckets.Today)
		out.Month = addUsageTotals(out.Month, buckets.Month)
		out.All = addUsageTotals(out.All, buckets.All)
	}
	return out
}

// Pick returns the window selected by a canonical range name.
func (b TokenBankUsageBuckets) Pick(rangeName string) TokenBankUsageTotals {
	switch rangeName {
	case TokenBankRangeToday:
		return b.Today
	case TokenBankRangeMonth:
		return b.Month
	default:
		return b.All
	}
}

// SumUsageBucketsByShare totals each of the owner's shares for today, this
// month, and all time. A share with no settled calls is absent; the caller
// treats that as zero. Rows are matched on owner_user_id, so another account's
// calls on a reused id cannot leak into this map.
func (r *TokenBankRepo) SumUsageBucketsByShare(ctx context.Context, ownerID string, now time.Time) (map[string]TokenBankUsageBuckets, error) {
	ownerID = strings.TrimSpace(ownerID)
	if r == nil || ownerID == "" {
		return map[string]TokenBankUsageBuckets{}, nil
	}
	return r.queryUsageBuckets(ctx, "share_id", "owner_user_id = ?", []any{ownerID}, now)
}

// SumUsageBucketsByModel totals one share's models. Keys are lower-cased model
// names, so a later lookup survives a casing difference between the share row
// and the usage row. Label keeps the first stored spelling.
func (r *TokenBankRepo) SumUsageBucketsByModel(ctx context.Context, ownerID, shareID string, now time.Time) (map[string]TokenBankUsageBuckets, error) {
	ownerID = strings.TrimSpace(ownerID)
	shareID = strings.TrimSpace(shareID)
	if r == nil || ownerID == "" || shareID == "" {
		return map[string]TokenBankUsageBuckets{}, nil
	}
	raw, err := r.queryUsageBuckets(ctx, "model_name", "owner_user_id = ? AND share_id = ?", []any{ownerID, shareID}, now)
	if err != nil {
		return nil, err
	}
	folded := make(map[string]TokenBankUsageBuckets, len(raw))
	for name, buckets := range raw {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		buckets.Label = strings.TrimSpace(name)
		prev, ok := folded[key]
		if !ok {
			folded[key] = buckets
			continue
		}
		prev.Today = addUsageTotals(prev.Today, buckets.Today)
		prev.Month = addUsageTotals(prev.Month, buckets.Month)
		prev.All = addUsageTotals(prev.All, buckets.All)
		folded[key] = prev
	}
	return folded, nil
}

func (r *TokenBankRepo) queryUsageBuckets(ctx context.Context, groupCol, where string, whereArgs []any, now time.Time) (map[string]TokenBankUsageBuckets, error) {
	today := tokenBankDayStart(now).Format(time.RFC3339)
	month := tokenBankMonthStart(now).Format(time.RFC3339)
	query := `SELECT ` + groupCol + `, ` +
		usageWindowSums("created_at >= ?") + `, ` +
		usageWindowSums("created_at >= ?") + `, ` +
		usageWindowSums("1 = 1") + `
		   FROM token_bank_usage
		  WHERE ` + where + `
		  GROUP BY ` + groupCol
	args := make([]any, 0, 20+len(whereArgs))
	for i := 0; i < 10; i++ {
		args = append(args, today)
	}
	for i := 0; i < 10; i++ {
		args = append(args, month)
	}
	args = append(args, whereArgs...)
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("token bank usage buckets: %w", err)
	}
	defer rows.Close()
	out := map[string]TokenBankUsageBuckets{}
	for rows.Next() {
		var key string
		var buckets TokenBankUsageBuckets
		dest := []any{&key}
		dest = buckets.Today.appendScan(dest)
		dest = buckets.Month.appendScan(dest)
		dest = buckets.All.appendScan(dest)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out[key] = buckets
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// usageWindowSums emits the ten aggregates of one window. predicate is either
// "created_at >= ?" (one placeholder per column) or "1 = 1".
func usageWindowSums(predicate string) string {
	cols := []string{
		"1", "input_tokens", "output_tokens", "cached_input_tokens", "cache_write_tokens",
		"gross_micro", "fee_micro", "net_micro", "charged_micro", "net_clamped",
	}
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = fmt.Sprintf("COALESCE(SUM(CASE WHEN %s THEN %s ELSE 0 END), 0)", predicate, col)
	}
	return strings.Join(parts, ", ")
}
