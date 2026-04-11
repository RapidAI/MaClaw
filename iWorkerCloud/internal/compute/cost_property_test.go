// Feature: compute-power-management, Property 15-17
package compute

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"
	"testing/quick"
	"time"

	_ "modernc.org/sqlite"
)

// TestPropertyCostFormula verifies the cost calculation formula (Property 15).
func TestPropertyCostFormula(t *testing.T) {
	f := func(inputTokens, outputTokens uint32, inputPriceSeed, outputPriceSeed uint16) bool {
		in := int64(inputTokens)
		out := int64(outputTokens)
		inputPrice := float64(inputPriceSeed) / 100.0  // 0 to ~655
		outputPrice := float64(outputPriceSeed) / 100.0

		inputCost, outputCost, totalCost := CalculateCost(in, out, inputPrice, outputPrice)

		expectedInput := float64(in) * inputPrice / 1_000_000
		expectedOutput := float64(out) * outputPrice / 1_000_000
		expectedTotal := expectedInput + expectedOutput

		const epsilon = 1e-10
		if math.Abs(inputCost-expectedInput) > epsilon {
			t.Logf("input_cost mismatch: %f vs %f", inputCost, expectedInput)
			return false
		}
		if math.Abs(outputCost-expectedOutput) > epsilon {
			t.Logf("output_cost mismatch: %f vs %f", outputCost, expectedOutput)
			return false
		}
		if math.Abs(totalCost-expectedTotal) > epsilon {
			t.Logf("total_cost mismatch: %f vs %f", totalCost, expectedTotal)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 15 failed: %v", err)
	}
}

// TestPropertyCostAggregation verifies aggregation consistency (Property 16).
func TestPropertyCostAggregation(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_txlock=immediate&t=cost16")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	usageStore := NewUsageStore(db)
	ctx := context.Background()
	if err := usageStore.CreateUsageTable(ctx); err != nil {
		t.Fatalf("create usage table: %v", err)
	}

	providerStore := NewProviderStore(db, make([]byte, AES256KeyLen))
	if err := providerStore.CreateTable(ctx); err != nil {
		t.Fatalf("create provider table: %v", err)
	}

	costEngine := NewCostEngine(db, usageStore, providerStore)
	if err := costEngine.CreateCostTable(ctx); err != nil {
		t.Fatalf("create cost table: %v", err)
	}

	counter := 0
	f := func(numRecords uint8) bool {
		n := int(numRecords%5) + 1 // 1-5 records
		counter++
		centerID := fmt.Sprintf("center_agg_%d", counter)
		providerName := fmt.Sprintf("provider_agg_%d", counter)

		baseTime := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
		var expectedInput, expectedOutput int64

		for i := 0; i < n; i++ {
			in := int64((counter*10 + i) * 100)
			out := int64((counter*10 + i) * 50)
			expectedInput += in
			expectedOutput += out

			rec := TokenUsageRecord{
				CenterID:     centerID,
				DiWorkerID:   "dw-1",
				ProviderName: providerName,
				Model:        "gpt-4",
				InputTokens:  in,
				OutputTokens: out,
				TotalTokens:  in + out,
				Timestamp:    baseTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			}
			if err := usageStore.RecordUsage(ctx, rec); err != nil {
				t.Logf("record error: %v", err)
				return false
			}
		}

		// Generate daily summary
		if err := costEngine.GenerateDailySummary(ctx, baseTime); err != nil {
			t.Logf("generate summary error: %v", err)
			return false
		}

		// Query summaries
		summaries, err := costEngine.QueryCostSummaries(ctx, CostFilter{
			CenterID:   centerID,
			PeriodType: "daily",
			Start:      "2025-01-15",
			End:        "2025-01-15",
		})
		if err != nil {
			t.Logf("query error: %v", err)
			return false
		}

		// Find our summary
		var totalIn, totalOut, totalReqs int64
		for _, s := range summaries {
			if s.ProviderName == providerName {
				totalIn += s.TotalInputTokens
				totalOut += s.TotalOutputTokens
				totalReqs += s.RequestCount
			}
		}

		if totalIn != expectedInput {
			t.Logf("input tokens mismatch: %d vs %d", totalIn, expectedInput)
			return false
		}
		if totalOut != expectedOutput {
			t.Logf("output tokens mismatch: %d vs %d", totalOut, expectedOutput)
			return false
		}
		if totalReqs != int64(n) {
			t.Logf("request count mismatch: %d vs %d", totalReqs, n)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 16 failed: %v", err)
	}
}


// TestPropertyHistoricalPriceImmutability verifies that once a CostSummary is
// generated, changing the provider price does not affect the stored summary (Property 17).
func TestPropertyHistoricalPriceImmutability(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_txlock=immediate&t=cost17")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	key := make([]byte, AES256KeyLen)
	usageStore := NewUsageStore(db)
	providerStore := NewProviderStore(db, key)
	ctx := context.Background()

	if err := usageStore.CreateUsageTable(ctx); err != nil {
		t.Fatalf("create usage table: %v", err)
	}
	if err := providerStore.CreateTable(ctx); err != nil {
		t.Fatalf("create provider table: %v", err)
	}

	costEngine := NewCostEngine(db, usageStore, providerStore)
	if err := costEngine.CreateCostTable(ctx); err != nil {
		t.Fatalf("create cost table: %v", err)
	}

	counter := 0
	f := func(priceSeed uint16) bool {
		counter++
		originalPrice := float64(priceSeed%1000+1) / 10.0 // 0.1 to 100.0
		providerName := fmt.Sprintf("immut_%d", counter)
		centerID := fmt.Sprintf("center_immut_%d", counter)

		// Create provider with original price
		p := &ComputeProvider{
			Name:                 providerName,
			BaseURL:              "https://api.example.com",
			Protocol:             ProtocolOpenAI,
			Enabled:              true,
			InputPricePerMToken:  originalPrice,
			OutputPricePerMToken: originalPrice * 2,
		}
		if err := providerStore.CreateProvider(ctx, p); err != nil {
			t.Logf("create provider error: %v", err)
			return false
		}

		// Insert usage record
		baseTime := time.Date(2025, 2, 10, 12, 0, 0, 0, time.UTC)
		rec := TokenUsageRecord{
			CenterID:     centerID,
			DiWorkerID:   "dw-1",
			ProviderName: providerName,
			Model:        "gpt-4",
			InputTokens:  1000,
			OutputTokens: 500,
			TotalTokens:  1500,
			Timestamp:    baseTime.Format(time.RFC3339),
		}
		if err := usageStore.RecordUsage(ctx, rec); err != nil {
			t.Logf("record usage error: %v", err)
			return false
		}

		// Generate summary (captures current price)
		if err := costEngine.GenerateDailySummary(ctx, baseTime); err != nil {
			t.Logf("generate summary error: %v", err)
			return false
		}

		// Read the summary before price change
		summaries, err := costEngine.QueryCostSummaries(ctx, CostFilter{
			CenterID:   centerID,
			PeriodType: "daily",
			Start:      "2025-02-10",
			End:        "2025-02-10",
		})
		if err != nil {
			t.Logf("query error: %v", err)
			return false
		}

		var originalSummary *CostSummary
		for i, s := range summaries {
			if s.ProviderName == providerName {
				originalSummary = &summaries[i]
				break
			}
		}
		if originalSummary == nil {
			t.Log("summary not found")
			return false
		}

		savedInputPrice := originalSummary.InputPriceUsed
		savedOutputPrice := originalSummary.OutputPriceUsed
		savedInputCost := originalSummary.InputCost
		savedOutputCost := originalSummary.OutputCost
		savedTotalCost := originalSummary.TotalCost

		// Update provider price
		p.InputPricePerMToken = originalPrice * 10
		p.OutputPricePerMToken = originalPrice * 20
		if err := providerStore.UpdateProvider(ctx, p); err != nil {
			t.Logf("update provider error: %v", err)
			return false
		}

		// Re-read the same summary — it should be unchanged
		summaries2, err := costEngine.QueryCostSummaries(ctx, CostFilter{
			CenterID:   centerID,
			PeriodType: "daily",
			Start:      "2025-02-10",
			End:        "2025-02-10",
		})
		if err != nil {
			t.Logf("query error: %v", err)
			return false
		}

		for _, s := range summaries2 {
			if s.ProviderName == providerName {
				if s.InputPriceUsed != savedInputPrice {
					t.Logf("input_price_used changed: %f vs %f", s.InputPriceUsed, savedInputPrice)
					return false
				}
				if s.OutputPriceUsed != savedOutputPrice {
					t.Logf("output_price_used changed: %f vs %f", s.OutputPriceUsed, savedOutputPrice)
					return false
				}
				if s.InputCost != savedInputCost {
					return false
				}
				if s.OutputCost != savedOutputCost {
					return false
				}
				if s.TotalCost != savedTotalCost {
					return false
				}
				return true
			}
		}

		t.Log("summary not found after price change")
		return false
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 17 failed: %v", err)
	}
}
