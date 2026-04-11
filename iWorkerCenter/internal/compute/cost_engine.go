package compute

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CostSummary is an aggregated cost record for a time period.
type CostSummary struct {
	ID                string  `json:"id"`
	DiWorkerID        string  `json:"diworker_id"`
	DiWorkerName      string  `json:"diworker_name"`
	PeriodType        string  `json:"period_type"`
	PeriodStart       string  `json:"period_start"`
	ProviderName      string  `json:"provider_name"`
	Model             string  `json:"model"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	InputCost         float64 `json:"input_cost"`
	OutputCost        float64 `json:"output_cost"`
	TotalCost         float64 `json:"total_cost"`
	RequestCount      int64   `json:"request_count"`
	InputPriceUsed    float64 `json:"input_price_used"`
	OutputPriceUsed   float64 `json:"output_price_used"`
}

// CostFilter specifies optional filters for querying cost summaries.
type CostFilter struct {
	DiWorkerID string
	PeriodType string
	Start      string
	End        string
}

// PriceProvider supplies per-provider pricing info.
type PriceProvider interface {
	GetPrice(providerName string) (inputPrice, outputPrice float64)
}

// CostEngine computes and stores cost summaries from local token usage records.
type CostEngine struct {
	db         *sql.DB
	usageStore *UsageStore
	prices     PriceProvider
}

// NewCostEngine creates a new Center-side CostEngine.
func NewCostEngine(db *sql.DB, usageStore *UsageStore, prices PriceProvider) *CostEngine {
	return &CostEngine{db: db, usageStore: usageStore, prices: prices}
}

// CreateCostTable creates the center_cost_summaries table.
func (e *CostEngine) CreateCostTable(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS center_cost_summaries (
    id                  TEXT PRIMARY KEY,
    diworker_id         TEXT NOT NULL,
    diworker_name       TEXT NOT NULL DEFAULT '',
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
		`CREATE INDEX IF NOT EXISTS idx_center_cost_dw ON center_cost_summaries(diworker_id, period_type, period_start)`,
	}
	for _, s := range stmts {
		if _, err := e.db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// CalculateCost computes costs from token counts and MToken prices.
func CalculateCost(inputTokens, outputTokens int64, inputPrice, outputPrice float64) (inputCost, outputCost, totalCost float64) {
	inputCost = float64(inputTokens) * inputPrice / 1_000_000
	outputCost = float64(outputTokens) * outputPrice / 1_000_000
	totalCost = inputCost + outputCost
	return
}

// GenerateDailySummary generates daily cost summaries for the given date.
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

	const aggQuery = `
SELECT diworker_id, provider_name, model,
       SUM(input_tokens), SUM(output_tokens), SUM(total_tokens), COUNT(*)
FROM center_token_usage
WHERE timestamp >= ? AND timestamp < ?
GROUP BY diworker_id, provider_name, model`

	rows, err := e.db.QueryContext(ctx, aggQuery,
		start.Format(time.RFC3339), end.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("aggregate usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var diworkerID, providerName, model string
		var inputTokens, outputTokens, totalTokens, requestCount int64

		if err := rows.Scan(&diworkerID, &providerName, &model,
			&inputTokens, &outputTokens, &totalTokens, &requestCount); err != nil {
			return fmt.Errorf("scan aggregate: %w", err)
		}

		var inputPrice, outputPrice float64
		if e.prices != nil {
			inputPrice, outputPrice = e.prices.GetPrice(providerName)
		}
		inputCost, outputCost, totalCost := CalculateCost(inputTokens, outputTokens, inputPrice, outputPrice)

		id, err := generateID()
		if err != nil {
			return err
		}

		const insertQ = `INSERT OR REPLACE INTO center_cost_summaries
			(id, diworker_id, period_type, period_start, provider_name, model,
			 total_input_tokens, total_output_tokens, total_tokens,
			 input_cost, output_cost, total_cost, request_count,
			 input_price_used, output_price_used)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		if _, err := e.db.ExecContext(ctx, insertQ,
			id, diworkerID, periodType, periodStart, providerName, model,
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

	if f.DiWorkerID != "" {
		where = append(where, "diworker_id = ?")
		args = append(args, f.DiWorkerID)
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

	q := `SELECT id, diworker_id, diworker_name, period_type, period_start, provider_name, model,
		total_input_tokens, total_output_tokens, total_tokens,
		input_cost, output_cost, total_cost, request_count,
		input_price_used, output_price_used
		FROM center_cost_summaries`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY period_start ASC, diworker_id ASC"

	rows, err := e.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CostSummary
	for rows.Next() {
		var s CostSummary
		if err := rows.Scan(&s.ID, &s.DiWorkerID, &s.DiWorkerName, &s.PeriodType, &s.PeriodStart,
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
