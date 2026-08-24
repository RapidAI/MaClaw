package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestLLMUsageRecordsPersistGroupAndClass(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := st.LLMUsage.Insert(ctx, &store.LLMUsageRecord{
		TenantID:       store.DefaultTenantID,
		Email:          "user@example.com",
		ProviderID:     "maclaw_official",
		Model:          "official-high",
		ServiceGroupID: "coding-auto",
		WorkloadClass:  "plan",
		ClassSource:    "hint",
		Preview:        "draft a plan",
		InputTokens:    12,
		OutputTokens:   4,
		TotalTokens:    16,
		Credits:        1.5,
		CreatedAt:      now,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := st.LLMUsage.Insert(ctx, &store.LLMUsageRecord{
		TenantID:       store.DefaultTenantID,
		ServiceGroupID: "writing-auto",
		WorkloadClass:  "plan",
		InputTokens:    3,
		CreatedAt:      now,
	}); err != nil {
		t.Fatalf("insert other group: %v", err)
	}

	got, err := st.LLMUsage.ListByGroupClass(ctx, store.DefaultTenantID, "coding-auto", "plan")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("coding-auto plan rows=%d want 1", len(got))
	}
	rec := got[0]
	if rec.ServiceGroupID != "coding-auto" || rec.WorkloadClass != "plan" || rec.ClassSource != "hint" || rec.Model != "official-high" || rec.TotalTokens != 16 {
		t.Fatalf("unexpected row: %#v", rec)
	}

	var indexName string
	if err := st.System.(*systemRepo).db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_llm_usage_group_class_time'`).Scan(&indexName); err != nil {
		t.Fatalf("group/class index missing: %v", err)
	}
}
