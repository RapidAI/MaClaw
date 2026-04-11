// Feature: compute-power-management, Property 14
package compute

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"testing/quick"
	"time"

	_ "modernc.org/sqlite"
)

func TestPropertyUsageRecordCompleteness(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_txlock=immediate&t=usage14")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewUsageStore(db)
	ctx := context.Background()
	if err := store.CreateUsageTable(ctx); err != nil {
		t.Fatalf("create table: %v", err)
	}

	counter := 0
	f := func(inputTokens, outputTokens uint32) bool {
		counter++
		in := int64(inputTokens)
		out := int64(outputTokens)

		rec := TokenUsageRecord{
			CenterID:     "center-1",
			DiWorkerID:   fmt.Sprintf("dw_%d", counter),
			ProviderName: "test-provider",
			Model:        "gpt-4",
			InputTokens:  in,
			OutputTokens: out,
			TotalTokens:  in + out,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
		}

		if err := store.RecordUsage(ctx, rec); err != nil {
			t.Logf("record error: %v", err)
			return false
		}

		// Query back
		records, err := store.QueryUsage(ctx, UsageFilter{DiWorkerID: rec.DiWorkerID})
		if err != nil {
			t.Logf("query error: %v", err)
			return false
		}
		if len(records) == 0 {
			t.Log("no records found")
			return false
		}

		last := records[len(records)-1]

		// Verify all required fields are present
		if last.ProviderName == "" {
			t.Log("provider_name should not be empty")
			return false
		}
		if last.Model == "" {
			t.Log("model should not be empty")
			return false
		}
		if last.Timestamp == "" {
			t.Log("timestamp should not be empty")
			return false
		}

		// Verify total_tokens = input_tokens + output_tokens
		if last.TotalTokens != last.InputTokens+last.OutputTokens {
			t.Logf("total_tokens mismatch: %d != %d + %d",
				last.TotalTokens, last.InputTokens, last.OutputTokens)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 14 failed: %v", err)
	}
}
