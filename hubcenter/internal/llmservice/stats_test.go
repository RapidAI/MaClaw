package llmservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

type recordingUsageRepo struct {
	record *TenantUsageRecord
}

func (r *recordingUsageRepo) Insert(_ context.Context, record *TenantUsageRecord) error {
	r.record = record
	return nil
}

func (r *recordingUsageRepo) QuerySummary(_ context.Context, _ UsageFilter) ([]TenantUsageSummary, error) {
	return nil, nil
}

func (r *recordingUsageRepo) QueryRecent(_ context.Context, _, _ string, _ int) ([]*TenantUsageRecord, error) {
	return nil, nil
}

func TestUsageRecorderCopiesChargedAuthorizationID(t *testing.T) {
	repo := &recordingUsageRepo{}
	recorder := NewUsageRecorder(repo)
	ts := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)

	err := recorder.RecordUsage(WithUsageContext(context.Background(), "hub1", "tenant1"), &llmpool.UsageRecord{
		ProviderID:   "p1",
		Model:        "gpt-4",
		InputTokens:  10,
		OutputTokens: 20,
		Credits:      0.1,
		AuthID:       "auth-small,auth-large",
		Timestamp:    ts,
	})
	if err != nil {
		t.Fatalf("RecordUsage() error = %v", err)
	}
	if repo.record == nil {
		t.Fatal("RecordUsage() did not insert a record")
	}
	if repo.record.HubID != "hub1" || repo.record.TenantID != "tenant1" || repo.record.AuthID != "auth-small,auth-large" {
		t.Fatalf("inserted record = %#v, want hub/tenant/auth IDs copied", repo.record)
	}
}
