package compute

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupCostTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCalculateCost(t *testing.T) {
	in, out, total := CalculateCost(1_000_000, 500_000, 3.0, 6.0)
	if math.Abs(in-3.0) > 0.001 {
		t.Errorf("input_cost: got %f, want 3.0", in)
	}
	if math.Abs(out-3.0) > 0.001 {
		t.Errorf("output_cost: got %f, want 3.0", out)
	}
	if math.Abs(total-6.0) > 0.001 {
		t.Errorf("total_cost: got %f, want 6.0", total)
	}
}

func TestCalculateCost_Zero(t *testing.T) {
	in, out, total := CalculateCost(0, 0, 10.0, 20.0)
	if in != 0 || out != 0 || total != 0 {
		t.Errorf("expected all zeros, got %f/%f/%f", in, out, total)
	}
}

func TestCostEngine_CreateTable(t *testing.T) {
	db := setupCostTestDB(t)
	engine := NewCostEngine(db, nil, nil)
	ctx := context.Background()

	if err := engine.CreateCostTable(ctx); err != nil {
		t.Fatal(err)
	}
	// Idempotent.
	if err := engine.CreateCostTable(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCostEngine_GenerateDailySummary(t *testing.T) {
	db := setupCostTestDB(t)
	ctx := context.Background()

	// Set up provider store with prices.
	provStore := NewProviderStore(db, make([]byte, 32))
	if err := provStore.CreateTable(ctx); err != nil {
		t.Fatal(err)
	}
	p := &ComputeProvider{
		Name:                 "gpt4-provider",
		BaseURL:              "https://api.openai.com",
		Protocol:             "openai",
		UserAgent:            "test",
		ComputeType:          "general",
		InputPricePerMToken:  3.0,
		OutputPricePerMToken: 6.0,
		Enabled:              true,
	}
	if err := provStore.CreateProvider(ctx, p); err != nil {
		t.Fatal(err)
	}

	// Set up usage store with records.
	usageStore := NewUsageStore(db)
	if err := usageStore.CreateUsageTable(ctx); err != nil {
		t.Fatal(err)
	}

	day := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	records := []TokenUsageRecord{
		{CenterID: "c1", DiWorkerID: "dw1", ProviderName: "gpt4-provider", Model: "gpt-4",
			InputTokens: 100_000, OutputTokens: 50_000, TotalTokens: 150_000,
			Timestamp: day.Add(2 * time.Hour).Format(time.RFC3339)},
		{CenterID: "c1", DiWorkerID: "dw2", ProviderName: "gpt4-provider", Model: "gpt-4",
			InputTokens: 200_000, OutputTokens: 100_000, TotalTokens: 300_000,
			Timestamp: day.Add(5 * time.Hour).Format(time.RFC3339)},
	}
	for _, r := range records {
		if err := usageStore.RecordUsage(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	// Create cost engine and generate summary.
	engine := NewCostEngine(db, usageStore, provStore)
	if err := engine.CreateCostTable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engine.GenerateDailySummary(ctx, day); err != nil {
		t.Fatal(err)
	}

	// Query summaries.
	summaries, err := engine.QueryCostSummaries(ctx, CostFilter{PeriodType: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	s := summaries[0]
	if s.CenterID != "c1" {
		t.Errorf("center_id: got %s, want c1", s.CenterID)
	}
	if s.TotalInputTokens != 300_000 {
		t.Errorf("input_tokens: got %d, want 300000", s.TotalInputTokens)
	}
	if s.TotalOutputTokens != 150_000 {
		t.Errorf("output_tokens: got %d, want 150000", s.TotalOutputTokens)
	}
	if s.RequestCount != 2 {
		t.Errorf("request_count: got %d, want 2", s.RequestCount)
	}
	// input_cost = 300000 * 3.0 / 1000000 = 0.9
	if math.Abs(s.InputCost-0.9) > 0.001 {
		t.Errorf("input_cost: got %f, want 0.9", s.InputCost)
	}
	// output_cost = 150000 * 6.0 / 1000000 = 0.9
	if math.Abs(s.OutputCost-0.9) > 0.001 {
		t.Errorf("output_cost: got %f, want 0.9", s.OutputCost)
	}
	if math.Abs(s.InputPriceUsed-3.0) > 0.001 {
		t.Errorf("input_price_used: got %f, want 3.0", s.InputPriceUsed)
	}
}

func TestCostEngine_GenerateMonthlySummary(t *testing.T) {
	db := setupCostTestDB(t)
	ctx := context.Background()

	provStore := NewProviderStore(db, make([]byte, 32))
	if err := provStore.CreateTable(ctx); err != nil {
		t.Fatal(err)
	}
	p := &ComputeProvider{
		Name: "claude-provider", BaseURL: "https://api.anthropic.com",
		Protocol: "anthropic", UserAgent: "test", ComputeType: "coding",
		InputPricePerMToken: 8.0, OutputPricePerMToken: 24.0, Enabled: true,
	}
	if err := provStore.CreateProvider(ctx, p); err != nil {
		t.Fatal(err)
	}

	usageStore := NewUsageStore(db)
	if err := usageStore.CreateUsageTable(ctx); err != nil {
		t.Fatal(err)
	}

	// Records across multiple days in June 2025.
	for d := 1; d <= 3; d++ {
		ts := time.Date(2025, 6, d, 12, 0, 0, 0, time.UTC)
		if err := usageStore.RecordUsage(ctx, TokenUsageRecord{
			CenterID: "c2", DiWorkerID: "dw1", ProviderName: "claude-provider", Model: "claude-3",
			InputTokens: 50_000, OutputTokens: 25_000, TotalTokens: 75_000,
			Timestamp: ts.Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
	}

	engine := NewCostEngine(db, usageStore, provStore)
	if err := engine.CreateCostTable(ctx); err != nil {
		t.Fatal(err)
	}

	month := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := engine.GenerateMonthlySummary(ctx, month); err != nil {
		t.Fatal(err)
	}

	summaries, err := engine.QueryCostSummaries(ctx, CostFilter{CenterID: "c2", PeriodType: "monthly"})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	s := summaries[0]
	if s.TotalInputTokens != 150_000 {
		t.Errorf("input_tokens: got %d, want 150000", s.TotalInputTokens)
	}
	if s.RequestCount != 3 {
		t.Errorf("request_count: got %d, want 3", s.RequestCount)
	}
	if s.PeriodStart != "2025-06" {
		t.Errorf("period_start: got %s, want 2025-06", s.PeriodStart)
	}
}

func TestCostEngine_QueryFilter(t *testing.T) {
	db := setupCostTestDB(t)
	ctx := context.Background()

	engine := NewCostEngine(db, nil, nil)
	if err := engine.CreateCostTable(ctx); err != nil {
		t.Fatal(err)
	}

	// Insert summaries directly.
	for _, cs := range []struct {
		center, period, start string
	}{
		{"c1", "daily", "2025-06-15"},
		{"c1", "daily", "2025-06-16"},
		{"c2", "daily", "2025-06-15"},
		{"c1", "monthly", "2025-06"},
	} {
		id, _ := generateID()
		_, err := db.ExecContext(ctx,
			`INSERT INTO cost_summaries (id, center_id, period_type, period_start, provider_name, model) VALUES (?, ?, ?, ?, 'p', 'm')`,
			id, cs.center, cs.period, cs.start)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Filter by center.
	got, _ := engine.QueryCostSummaries(ctx, CostFilter{CenterID: "c1"})
	if len(got) != 3 {
		t.Errorf("center filter: expected 3, got %d", len(got))
	}

	// Filter by period type.
	got, _ = engine.QueryCostSummaries(ctx, CostFilter{PeriodType: "daily"})
	if len(got) != 3 {
		t.Errorf("period filter: expected 3, got %d", len(got))
	}

	// Combined.
	got, _ = engine.QueryCostSummaries(ctx, CostFilter{CenterID: "c1", PeriodType: "daily"})
	if len(got) != 2 {
		t.Errorf("combined filter: expected 2, got %d", len(got))
	}
}
