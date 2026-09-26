package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type flushUsageReportsTestRepo struct {
	testSystemSettingsRepo
	failReportsSet bool
	failReportsGet bool
}

func (r *flushUsageReportsTestRepo) Set(ctx context.Context, key, valueJSON string) error {
	if r.failReportsSet && key == llmUsageReportsKey {
		return errors.New("usage reports save failed")
	}
	return r.testSystemSettingsRepo.Set(ctx, key, valueJSON)
}

func (r *flushUsageReportsTestRepo) Get(ctx context.Context, key string) (string, error) {
	if r.failReportsGet && key == llmUsageReportsKey {
		return "", errors.New("usage reports load failed")
	}
	return r.testSystemSettingsRepo.Get(ctx, key)
}

func flushTestPendingReports(requests int64) *llmUsageReportsStore {
	reports := &llmUsageReportsStore{Version: llmUsageReportsVersion, Days: map[string]*llmUsageReportDay{}}
	reports.addUsage(time.Now().UTC().Truncate(time.Second), "user@example.com", nil,
		corelib.TokenUsageStat{InputTokens: requests * 100, TotalTokens: requests * 100, Requests: requests}, 0)
	return reports
}

func flushTestStoredDayRequests(t *testing.T, system *testSystemSettingsRepo, dayKey string) (int64, int64) {
	t.Helper()
	stored, err := loadLLMUsageReports(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	day := stored.Days[dayKey]
	if day == nil {
		t.Fatalf("no day %s in stored report: %+v", dayKey, stored.Days)
	}
	return day.Totals.Requests, day.Totals.InputTokens
}

// Two consecutive flushes must accumulate exactly once each: the second flush
// reuses the cached parsed base (raw blob unchanged) and merges on top of it.
func TestFlushLLMUsageReportsReusesBaseWithoutDoubleCounting(t *testing.T) {
	system := &testSystemSettingsRepo{}
	dayKey := time.Now().UTC().Truncate(time.Second).Format("2006-01-02")

	if err := flushLLMUsageReports(t.Context(), system, flushTestPendingReports(1)); err != nil {
		t.Fatal(err)
	}
	if err := flushLLMUsageReports(t.Context(), system, flushTestPendingReports(2)); err != nil {
		t.Fatal(err)
	}
	requests, inputTokens := flushTestStoredDayRequests(t, system, dayKey)
	if requests != 3 || inputTokens != 300 {
		t.Fatalf("after two flushes requests=%d inputTokens=%d; want 3/300", requests, inputTokens)
	}
}

// An external write (import, direct DB edit) between flushes must survive:
// the changed raw blob forces the next flush to re-parse instead of reusing
// the stale in-memory base.
func TestFlushLLMUsageReportsDetectsExternalWrite(t *testing.T) {
	system := &testSystemSettingsRepo{}
	now := time.Now().UTC().Truncate(time.Second)
	dayKey := now.Format("2006-01-02")

	if err := flushLLMUsageReports(t.Context(), system, flushTestPendingReports(1)); err != nil {
		t.Fatal(err)
	}
	// External write: read the persisted document, append another user's
	// counters, and save it back (simulating an import or direct DB edit).
	imported, err := loadLLMUsageReports(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	imported.addUsage(now, "admin@example.com", nil,
		corelib.TokenUsageStat{InputTokens: 7, TotalTokens: 7, Requests: 7}, 0)
	if err := saveLLMUsageReports(t.Context(), system, imported); err != nil {
		t.Fatal(err)
	}
	if err := flushLLMUsageReports(t.Context(), system, flushTestPendingReports(2)); err != nil {
		t.Fatal(err)
	}
	stored, err := loadLLMUsageReports(t.Context(), system)
	if err != nil {
		t.Fatal(err)
	}
	day := stored.Days[dayKey]
	if day == nil {
		t.Fatalf("no day %s stored", dayKey)
	}
	userEntry := day.Users["user@example.com"]
	adminEntry := day.Users["admin@example.com"]
	if userEntry == nil || adminEntry == nil {
		t.Fatalf("expected both users after external write, got %+v", day.Users)
	}
	if userEntry.Totals.Requests != 3 {
		t.Fatalf("user requests=%d; want 3 (1+2, no double count)", userEntry.Totals.Requests)
	}
	if adminEntry.Totals.Requests != 7 {
		t.Fatalf("admin requests=%d; want 7 (import must survive)", adminEntry.Totals.Requests)
	}
}

// A failed persist drops the cached base so the requeued pending batch is
// merged exactly once on retry.
func TestFlushLLMUsageReportsFailureDoesNotDoubleCount(t *testing.T) {
	system := &flushUsageReportsTestRepo{}
	dayKey := time.Now().UTC().Truncate(time.Second).Format("2006-01-02")
	pending := flushTestPendingReports(1)

	system.failReportsSet = true
	if err := flushLLMUsageReports(t.Context(), system, pending); err == nil {
		t.Fatal("expected flush to fail on reports Set error")
	}
	system.failReportsSet = false
	if err := flushLLMUsageReports(t.Context(), system, pending); err != nil {
		t.Fatal(err)
	}
	requests, inputTokens := flushTestStoredDayRequests(t, &system.testSystemSettingsRepo, dayKey)
	if requests != 1 || inputTokens != 100 {
		t.Fatalf("after retried flush requests=%d inputTokens=%d; want 1/100 (no double count)", requests, inputTokens)
	}
}

// A failed read must fail the flush (caller requeues) instead of merging the
// pending batch onto an empty base, which would overwrite retained history.
func TestFlushLLMUsageReportsFailsOnReadError(t *testing.T) {
	system := &flushUsageReportsTestRepo{failReportsGet: true}
	if err := flushLLMUsageReports(t.Context(), system, flushTestPendingReports(1)); err == nil {
		t.Fatal("expected flush to fail on reports Get error")
	}
	stored, err := loadLLMUsageReports(t.Context(), &system.testSystemSettingsRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Days) != 0 {
		t.Fatalf("failed flush must not persist anything: %+v", stored.Days)
	}
}
