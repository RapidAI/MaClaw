package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestLLMBillingLedgerRecordsCacheLegsAndDirectionalAmounts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	settlement := &store.LLMBillingSettlement{
		TenantID:               store.DefaultTenantID,
		RequestID:              "request-cache-1",
		UserID:                 "u1",
		Email:                  "user@example.com",
		ProviderID:             "provider-a",
		ServiceGroupIDsJSON:    `["paid"]`,
		InputTokens:            10_000,
		OutputTokens:           5_000,
		RequestedMicrocredits:  2_280_000,
		DeductedMicrocredits:   2_280_000,
		ProviderMultiplier:     1,
		BillingGroupMultiplier: 1,
		PricingJSON:            `{"input_credits_per_10k":1,"output_credits_per_10k":4,"version":"cache-v1"}`,
		CachedInputTokens:      int64PtrForTest(8_000),
		CacheWriteTokens:       int64PtrForTest(1_000),
		NormalInputCredits:     float64PtrForTest(0.1),
		CacheReadCredits:       float64PtrForTest(0.08),
		CacheWriteCredits:      float64PtrForTest(0.1),
		OutputCredits:          float64PtrForTest(2),
		NormalInputCostRMB:     float64PtrForTest(0.002),
		CacheReadCostRMB:       float64PtrForTest(0.00016),
		CacheWriteCostRMB:      float64PtrForTest(0.002),
		OutputCostRMB:          float64PtrForTest(0.04),
		CreatedAt:              now,
	}
	inserted, err := st.LLMBillingLedger.RecordSettlement(ctx, settlement)
	if err != nil || !inserted {
		t.Fatalf("record settlement: inserted=%v err=%v", inserted, err)
	}

	var cached, written int64
	var normal, read, write, output, normalRMB, readRMB, writeRMB, outputRMB float64
	row := st.System.(*systemRepo).db.QueryRow(`SELECT cached_input_tokens, cache_write_tokens,
		normal_input_credits, cache_read_credits, cache_write_credits, output_credits,
		normal_input_cost_rmb, cache_read_cost_rmb, cache_write_cost_rmb, output_cost_rmb
		FROM llm_billing_ledger WHERE tenant_id = ? AND request_id = ?`, store.DefaultTenantID, "request-cache-1")
	if err := row.Scan(&cached, &written, &normal, &read, &write, &output, &normalRMB, &readRMB, &writeRMB, &outputRMB); err != nil {
		t.Fatalf("read back settlement: %v", err)
	}
	if cached != 8_000 || written != 1_000 {
		t.Fatalf("cache legs = %d/%d, want 8000/1000", cached, written)
	}
	if normal != 0.1 || read != 0.08 || write != 0.1 || output != 2 {
		t.Fatalf("directional credits = %v/%v/%v/%v, want 0.1/0.08/0.1/2", normal, read, write, output)
	}
	if normalRMB != 0.002 || readRMB != 0.00016 || writeRMB != 0.002 || outputRMB != 0.04 {
		t.Fatalf("directional RMB = %v/%v/%v/%v", normalRMB, readRMB, writeRMB, outputRMB)
	}
}

func TestLLMBillingLedgerLegacySettlementKeepsDirectionalColumnsNull(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// A legacy token-count debit has no directional pricing. Its cache-v1
	// columns must stay NULL so a later report never mistakes the row for a
	// settled zero-priced cache direction, and so historic rows are never
	// backfilled with recalculated values.
	inserted, err := st.LLMBillingLedger.RecordSettlement(ctx, &store.LLMBillingSettlement{
		TenantID:               store.DefaultTenantID,
		RequestID:              "request-legacy-1",
		InputTokens:            100,
		OutputTokens:           50,
		DeductedMicrocredits:   1_000_000,
		CachedInputTokens:      int64PtrForTest(0),
		CacheWriteTokens:       int64PtrForTest(0),
		BillingGroupMultiplier: 1,
		CreatedAt:              time.Now().UTC(),
	})
	if err != nil || !inserted {
		t.Fatalf("record legacy settlement: inserted=%v err=%v", inserted, err)
	}
	var normal, read, write, output, normalRMB, readRMB, writeRMB, outputRMB any
	row := st.System.(*systemRepo).db.QueryRow(`SELECT normal_input_credits, cache_read_credits, cache_write_credits, output_credits,
		normal_input_cost_rmb, cache_read_cost_rmb, cache_write_cost_rmb, output_cost_rmb
		FROM llm_billing_ledger WHERE tenant_id = ? AND request_id = ?`, store.DefaultTenantID, "request-legacy-1")
	if err := row.Scan(&normal, &read, &write, &output, &normalRMB, &readRMB, &writeRMB, &outputRMB); err != nil {
		t.Fatalf("read back legacy settlement: %v", err)
	}
	for name, value := range map[string]any{"normal_input_credits": normal, "cache_read_credits": read, "cache_write_credits": write, "output_credits": output, "normal_input_cost_rmb": normalRMB, "cache_read_cost_rmb": readRMB, "cache_write_cost_rmb": writeRMB, "output_cost_rmb": outputRMB} {
		if value != nil {
			t.Fatalf("legacy column %s = %v, want NULL", name, value)
		}
	}
}

func int64PtrForTest(value int64) *int64       { return &value }
func float64PtrForTest(value float64) *float64 { return &value }
