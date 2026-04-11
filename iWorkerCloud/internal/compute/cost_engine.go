package compute

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CostEngine computes and stores cost summaries from token usage records.
type CostEngine struct {
	db            *sql.DB
	usageStore    *UsageStore
	providerStore *ProviderStore
}

// NewCostEngine creates a new CostEngine.
func NewCostEngine(db *sql.DB, usageStore *UsageStore, providerStore *ProviderStore) *CostEngine {
	return &CostEngine{db: db, usageStore: usageStore, providerStore: providerStore}
}

// CreateCostTable creates the cost_summaries table and indexes.
func (e *CostEngine) CreateCostTable(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS cost_summaries (
    id                  TEXT PRIMARY KEY,
    center_id           TEXT NOT NULL,
    period_type         TEXT NOT NULL,
    period_start        DATE NOT NULL,
    provider_name       TEXT NOT NULL,
    model               TEXT NOT NULL,
    total_input_tokens  INTEGER NOT NULL DEFAULT 0,
    total_output_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens        INTEGER NOT NULL DEFAULT 0,
    input_cost          REAL NOT NULL DEFAULT 0.0,
    output_cost         REAL NOT NULL DEFAULT 0.0,
    total_cost          REAL NOT NULL DEFAULT 0.0,
    request_count       INTEGER NOT NULL DEFAULT 0,
    input_price_used    REAL NOT NULL DEFAULT 0.0,
    output_price_used   REAL NOT NULL DEFAULT 0.0,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`,
		`CREATE INDEX IF NOT EXISTS idx_cost_center_period ON cost_summaries(center_id, period_type, period_start)`,
	}
	for _, s := range stmts {
		if _, err := e.db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// CalculateCost computes input_cost and output_cost from token counts and prices.
// Formula: input_cost = input_tokens × input_price_per_mtoken / 1,000,000
func CalculateCost(inputTokens, outputTokens int64, inputPrice, outputPrice float64) (inputCost, outputCost, totalCost float64) {
	inputCost = float64(inputTokens) * inputPrice / 1_000_000
	outputCost = float64(outputTokens) * outputPrice / 1_000_000
	totalCost = inputCost + outputCost
	return
}

// GenerateDailySummary generates daily cost summaries for the given date.
// It aggregates token_usage_records by center_id, provider_name, model for
// the specified day, looks up current provider prices, and inserts cost_summaries.
func (e *CostEngine) GenerateDailySummary(ctx context.Context, date time.Time) error {
	return e.generateSummary(ctx, "daily", date)
}

// GenerateMonthlySummary generates monthly cost summaries for the given month.
func (e *CostEngine) GenerateMonthlySummary(ctx context.Context, month time.Time) error {
	return e.generateSummary(ctx, "monthly", month)
}

func (e *CostEngine) generateSummary(ctx context.Context, periodType string, ref time.Time) error {
	var start, end time.Time
	var periodStart string

	switch periodType {
	case "daily":
		start = time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, time.UTC)
		end = start.Add(24 * time.Hour)
		periodStart = start.Format("2006-01-02")
	case "monthly":
		start = time.Date(ref.Year(), ref.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
		periodStart = start.Format("2006-01")
	default:
		return fmt.Errorf("unsupported period type: %s", periodType)
	}

	// Aggregate usage records for the period.
	const aggQuery = `
SELECT center_id, provider_name, model,
       SUM(input_tokens), SUM(output_tokens), SUM(total_tokens), COUNT(*)
FROM token_usage_records
WHERE timestamp >= ? AND timestamp < ?
GROUP BY center_id, provider_name, model`

	rows, err := e.db.QueryContext(ctx, aggQuery,
		start.Format(time.RFC3339), end.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("aggregate usage: %w", err)
	}
	defer rows.Close()

	// Build a price lookup from current providers.
	prices := e.buildPriceLookup(ctx)

	for rows.Next() {
		var centerID, providerName, model string
		var inputTokens, outputTokens, totalTokens, requestCount int64

		if err := rows.Scan(&centerID, &providerName, &model,
			&inputTokens, &outputTokens, &totalTokens, &requestCount); err != nil {
			return fmt.Errorf("scan aggregate: %w", err)
		}

		// Look up prices for this provider.
		inputPrice, outputPrice := prices.lookup(providerName)
		inputCost, outputCost, totalCost := CalculateCost(inputTokens, outputTokens, inputPrice, outputPrice)

		id, err := generateID()
		if err != nil {
			return err
		}

		const insertQ = `INSERT OR REPLACE INTO cost_summaries
			(id, center_id, period_type, period_start, provider_name, model,
			 total_input_tokens, total_output_tokens, total_tokens,
			 input_cost, output_cost, total_cost, request_count,
			 input_price_used, output_price_used)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		if _, err := e.db.ExecContext(ctx, insertQ,
			id, centerID, periodType, periodStart, providerName, model,
			inputTokens, outputTokens, totalTokens,
			inputCost, outputCost, totalCost, requestCount,
			inputPrice, outputPrice); err != nil {
			return fmt.Errorf("insert summary: %w", err)
		}
	}
	return rows.Err()
}

// QueryCostSummaries returns cost summaries matching the given filter.
func (e *CostEngine) QueryCostSummaries(ctx context.Context, f CostFilter) ([]CostSummary, error) {
	var where []string
	var args []interface{}

	if f.CenterID != "" {
		where = append(where, "center_id = ?")
		args = append(args, f.CenterID)
	}
	if f.PeriodType != "" {
		where = append(where, "period_type = ?")
		args = append(args, f.PeriodType)
	}
	if f.Start != "" {
		where = append(where, "period_start >= ?")
		args = append(args, f.Start)
	}
	if f.End != "" {
		where = append(where, "period_start <= ?")
		args = append(args, f.End)
	}

	q := `SELECT id, center_id, period_type, period_start, provider_name, model,
		total_input_tokens, total_output_tokens, total_tokens,
		input_cost, output_cost, total_cost, request_count,
		input_price_used, output_price_used
		FROM cost_summaries`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY period_start ASC, center_id ASC"

	rows, err := e.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CostSummary
	for rows.Next() {
		var s CostSummary
		if err := rows.Scan(&s.ID, &s.CenterID, &s.PeriodType, &s.PeriodStart,
			&s.ProviderName, &s.Model,
			&s.TotalInputTokens, &s.TotalOutputTokens, &s.TotalTokens,
			&s.InputCost, &s.OutputCost, &s.TotalCost, &s.RequestCount,
			&s.InputPriceUsed, &s.OutputPriceUsed); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

// CostFilter specifies optional filters for querying cost summaries.
type CostFilter struct {
	CenterID   string
	PeriodType string // daily | monthly
	Start      string // date string
	End        string // date string
}

// priceLookup caches provider prices for batch cost calculation.
type priceLookup struct {
	prices map[string][2]float64 // provider_name → [input_price, output_price]
}

func (e *CostEngine) buildPriceLookup(ctx context.Context) *priceLookup {
	pl := &priceLookup{prices: make(map[string][2]float64)}
	if e.providerStore == nil {
		return pl
	}
	providers, err := e.providerStore.ListProviders(ctx)
	if err != nil {
		return pl
	}
	for _, p := range providers {
		pl.prices[p.Name] = [2]float64{p.InputPricePerMToken, p.OutputPricePerMToken}
	}
	return pl
}

func (pl *priceLookup) lookup(providerName string) (inputPrice, outputPrice float64) {
	if p, ok := pl.prices[providerName]; ok {
		return p[0], p[1]
	}
	return 0, 0
}
