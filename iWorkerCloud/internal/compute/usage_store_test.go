package compute

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupUsageTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUsageStore_CreateUsageTable(t *testing.T) {
	db := setupUsageTestDB(t)
	store := NewUsageStore(db)
	ctx := context.Background()

	if err := store.CreateUsageTable(ctx); err != nil {
		t.Fatalf("CreateUsageTable: %v", err)
	}

	// Calling again should be idempotent.
	if err := store.CreateUsageTable(ctx); err != nil {
		t.Fatalf("CreateUsageTable (idempotent): %v", err)
	}
}

func TestUsageStore_RecordAndQuery(t *testing.T) {
	db := setupUsageTestDB(t)
	store := NewUsageStore(db)
	ctx := context.Background()

	if err := store.CreateUsageTable(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	rec := TokenUsageRecord{
		CenterID:     "center-1",
		DiWorkerID:   "dw-1",
		ProviderName: "openai-gpt4",
		Model:        "gpt-4",
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		Estimated:    false,
		Timestamp:    now,
	}

	if err := store.RecordUsage(ctx, rec); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	// Query all records.
	records, err := store.QueryUsage(ctx, UsageFilter{})
	if err != nil {
		t.Fatalf("QueryUsage: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got := records[0]
	if got.CenterID != "center-1" || got.DiWorkerID != "dw-1" {
		t.Errorf("unexpected center/diworker: %s / %s", got.CenterID, got.DiWorkerID)
	}
	if got.InputTokens != 100 || got.OutputTokens != 50 || got.TotalTokens != 150 {
		t.Errorf("unexpected tokens: %d/%d/%d", got.InputTokens, got.OutputTokens, got.TotalTokens)
	}
	if got.Estimated {
		t.Error("expected estimated=false")
	}
	if got.ID == "" {
		t.Error("expected auto-generated ID")
	}
}

func TestUsageStore_QueryFilter(t *testing.T) {
	db := setupUsageTestDB(t)
	store := NewUsageStore(db)
	ctx := context.Background()

	if err := store.CreateUsageTable(ctx); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	records := []TokenUsageRecord{
		{CenterID: "c1", DiWorkerID: "dw1", ProviderName: "p1", Model: "m1",
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Timestamp: base.Format(time.RFC3339)},
		{CenterID: "c1", DiWorkerID: "dw2", ProviderName: "p1", Model: "m1",
			InputTokens: 20, OutputTokens: 10, TotalTokens: 30, Timestamp: base.Add(time.Hour).Format(time.RFC3339)},
		{CenterID: "c2", DiWorkerID: "dw1", ProviderName: "p2", Model: "m2",
			InputTokens: 30, OutputTokens: 15, TotalTokens: 45, Estimated: true,
			Timestamp: base.Add(2 * time.Hour).Format(time.RFC3339)},
	}
	for _, r := range records {
		if err := store.RecordUsage(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	// Filter by center_id.
	got, err := store.QueryUsage(ctx, UsageFilter{CenterID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("center filter: expected 2, got %d", len(got))
	}

	// Filter by diworker_id.
	got, err = store.QueryUsage(ctx, UsageFilter{DiWorkerID: "dw1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("diworker filter: expected 2, got %d", len(got))
	}

	// Filter by time range.
	got, err = store.QueryUsage(ctx, UsageFilter{
		Start: base.Add(30 * time.Minute).Format(time.RFC3339),
		End:   base.Add(3 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("time filter: expected 2, got %d", len(got))
	}

	// Combined filter.
	got, err = store.QueryUsage(ctx, UsageFilter{CenterID: "c2", DiWorkerID: "dw1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("combined filter: expected 1, got %d", len(got))
	}
	if !got[0].Estimated {
		t.Error("expected estimated=true for c2/dw1 record")
	}
}

func TestUsageStore_RecordAutoID(t *testing.T) {
	db := setupUsageTestDB(t)
	store := NewUsageStore(db)
	ctx := context.Background()

	if err := store.CreateUsageTable(ctx); err != nil {
		t.Fatal(err)
	}

	rec := TokenUsageRecord{
		CenterID:     "c1",
		DiWorkerID:   "dw1",
		ProviderName: "p1",
		Model:        "m1",
		InputTokens:  10,
		OutputTokens: 5,
		TotalTokens:  15,
	}

	if err := store.RecordUsage(ctx, rec); err != nil {
		t.Fatal(err)
	}

	records, err := store.QueryUsage(ctx, UsageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ID == "" {
		t.Error("expected auto-generated ID")
	}
	if records[0].Timestamp == "" {
		t.Error("expected auto-generated timestamp")
	}
}
