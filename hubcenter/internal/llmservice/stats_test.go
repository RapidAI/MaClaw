package llmservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

type recordingUsageRepo struct {
	record         *TenantUsageRecord
	summaries      []TenantUsageSummary
	groupSummaries []TenantUsageSummary
	filter         UsageFilter
	groupFilter    UsageFilter
}

func (r *recordingUsageRepo) Insert(_ context.Context, record *TenantUsageRecord) error {
	r.record = record
	return nil
}

func (r *recordingUsageRepo) QuerySummary(_ context.Context, filter UsageFilter) ([]TenantUsageSummary, error) {
	if filter.GroupByServiceGroup {
		r.groupFilter = filter
		return r.groupSummaries, nil
	}
	r.filter = filter
	return r.summaries, nil
}

func TestUsageReconciliationIsScopedToHubTenantAndCalendarDay(t *testing.T) {
	repo := &recordingUsageRepo{
		summaries: []TenantUsageSummary{{
			HubID: "hub1", TenantID: "tenant1", Period: "daily", PeriodStart: "2026-09-01",
			InputTokens: 10, OutputTokens: 20, CachedInputTokens: 5, CacheWriteTokens: 2, TotalCredits: 1.5, TotalRequests: 3,
		}},
		groupSummaries: []TenantUsageSummary{{
			HubID: "hub1", TenantID: "tenant1", ServiceGroupID: "redeem", Period: "daily", PeriodStart: "2026-09-01",
			InputTokens: 7, OutputTokens: 12, CachedInputTokens: 3, CacheWriteTokens: 1, TotalCredits: 1.1, TotalRequests: 2,
		}, {
			HubID: "hub1", TenantID: "tenant1", ServiceGroupID: "maclaw-official", Period: "daily", PeriodStart: "2026-09-01",
			InputTokens: 3, OutputTokens: 8, CachedInputTokens: 2, CacheWriteTokens: 1, TotalCredits: 0.4, TotalRequests: 1,
		}, {
			HubID: "other-hub", TenantID: "tenant1", ServiceGroupID: "ignored", InputTokens: 99, TotalRequests: 9,
		}},
	}
	report, err := NewStatsService(repo).QueryUsageReconciliation(context.Background(), "hub1", "tenant1", "2026-09-01", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("QueryUsageReconciliation() error = %v", err)
	}
	if report == nil || report.Upstream.InputTokens != 10 || report.Upstream.CachedInputTokens != 5 || report.Upstream.TotalRequests != 3 || report.Upstream.TotalCredits != 1.5 {
		t.Fatalf("reconciliation report = %#v", report)
	}
	if !repo.groupFilter.GroupByServiceGroup || repo.groupFilter.HubID != "hub1" || repo.groupFilter.TenantID != "tenant1" || repo.groupFilter.StartDate != "2026-09-01" || repo.groupFilter.EndDate != "2026-09-01" || repo.groupFilter.Timezone != "Asia/Shanghai" {
		t.Fatalf("service-group filter = %#v", repo.groupFilter)
	}
	if len(report.ServiceGroups) != 2 {
		t.Fatalf("service groups = %#v", report.ServiceGroups)
	}
	if report.ServiceGroups[0].ServiceGroupID != "maclaw-official" || report.ServiceGroups[0].TotalCredits != 0.4 {
		t.Fatalf("first service group = %#v, want maclaw-official sorted first", report.ServiceGroups[0])
	}
	if report.ServiceGroups[1].ServiceGroupID != "redeem" || report.ServiceGroups[1].InputTokens != 7 {
		t.Fatalf("second service group = %#v", report.ServiceGroups[1])
	}
}

func TestUsageReconciliationMergesServiceGroupCaseVariants(t *testing.T) {
	repo := &recordingUsageRepo{groupSummaries: []TenantUsageSummary{
		{HubID: "hub1", TenantID: "tenant1", ServiceGroupID: "Redeem", InputTokens: 4, OutputTokens: 1, TotalCredits: 0.2, TotalRequests: 1, CacheHits: 0},
		{HubID: "hub1", TenantID: "tenant1", ServiceGroupID: "redeem", InputTokens: 6, OutputTokens: 2, TotalCredits: 0.3, TotalRequests: 1, CacheHits: 1},
	}}
	report, err := NewStatsService(repo).QueryUsageReconciliation(context.Background(), "hub1", "tenant1", "2026-09-01", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("QueryUsageReconciliation() error = %v", err)
	}
	if report.Upstream.InputTokens != 10 || report.Upstream.OutputTokens != 3 || report.Upstream.TotalCredits != 0.5 || report.Upstream.TotalRequests != 2 {
		t.Fatalf("merged upstream = %#v", report.Upstream)
	}
	if len(report.ServiceGroups) != 1 || report.ServiceGroups[0].InputTokens != 10 || report.ServiceGroups[0].CacheHits != 1 {
		t.Fatalf("merged groups = %#v", report.ServiceGroups)
	}
}

func TestUsageReconciliationEmptyDayHasZeroTotalsAndNoGroups(t *testing.T) {
	repo := &recordingUsageRepo{}
	report, err := NewStatsService(repo).QueryUsageReconciliation(context.Background(), "hub1", "tenant1", "2026-09-01", "Asia/Shanghai")
	if err != nil {
		t.Fatalf("QueryUsageReconciliation() error = %v", err)
	}
	if report == nil || len(report.ServiceGroups) != 0 || report.Upstream.TotalRequests != 0 || report.Upstream.InputTokens != 0 {
		t.Fatalf("empty day report = %#v", report)
	}
	if !repo.groupFilter.GroupByServiceGroup || repo.groupFilter.HubID != "hub1" || repo.groupFilter.TenantID != "tenant1" {
		t.Fatalf("empty day still queries the grouped ledger: %#v", repo.groupFilter)
	}
}

func (r *recordingUsageRepo) QueryRecent(_ context.Context, _, _ string, _ int) ([]*TenantUsageRecord, error) {
	return nil, nil
}

func (r *recordingUsageRepo) QueryProviderTraffic(_ context.Context, _, _, _ time.Time) (map[string]ProviderPeriodTraffic, error) {
	return nil, nil
}

func (r *recordingUsageRepo) QueryServiceGroupTraffic(_ context.Context, _, _, _ time.Time) (map[string]ProviderPeriodTraffic, error) {
	return nil, nil
}

func (r *recordingUsageRepo) QueryClassTraffic(_ context.Context, _ string, _ time.Time) ([]ClassTrafficRow, map[string]int64, []ClassTrafficSample, error) {
	return nil, nil, nil, nil
}

func TestUsageRecorderCopiesChargedAuthorizationID(t *testing.T) {
	repo := &recordingUsageRepo{}
	recorder := NewUsageRecorder(repo)
	ts := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)

	err := recorder.RecordUsage(WithUsageContext(context.Background(), "hub1", "tenant1"), &llmpool.UsageRecord{
		RequestID:    "req-1",
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
	if repo.record.HubID != "hub1" || repo.record.TenantID != "tenant1" || repo.record.RequestID != "req-1" || repo.record.AuthID != "auth-small,auth-large" {
		t.Fatalf("inserted record = %#v, want hub/tenant/auth IDs copied", repo.record)
	}
}

func TestProviderTrafficBoundsUsesMondayWeekAndLocalMidnight(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	now := time.Date(2026, 8, 12, 15, 4, 0, 0, loc) // Wednesday
	day, week, month := ProviderTrafficBounds(now, loc)
	if !day.Equal(time.Date(2026, 8, 12, 0, 0, 0, 0, loc)) {
		t.Fatalf("day start = %s", day)
	}
	if !week.Equal(time.Date(2026, 8, 10, 0, 0, 0, 0, loc)) {
		t.Fatalf("week start = %s", week)
	}
	if !month.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("month start = %s", month)
	}
}

func TestNormalizeClassTrafficRowsAlwaysEmitsFrozenClasses(t *testing.T) {
	rows := NormalizeClassTrafficRows([]ClassTrafficRow{
		{Class: "code", Requests: 2, InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
		{Class: "", Requests: 1, InputTokens: 3, OutputTokens: 1, TotalTokens: 4},
	})
	if len(rows) != len(llmpool.FrozenWorkloadClasses)+3 {
		t.Fatalf("rows=%d want %d", len(rows), len(llmpool.FrozenWorkloadClasses)+3)
	}
	byClass := map[string]ClassTrafficRow{}
	for _, row := range rows {
		byClass[row.Class] = row
	}
	if byClass["code"].Requests != 2 || byClass["unclassified"].Requests != 1 {
		t.Fatalf("class rows = %#v", rows)
	}
	if byClass["plan"].Requests != 0 || byClass["balanced"].Class != "balanced" {
		t.Fatalf("missing zero-filled classes: %#v", rows)
	}
	if byClass["total"].Requests != 3 || byClass["total"].TotalTokens != 18 {
		t.Fatalf("total = %#v", byClass["total"])
	}
}

func TestQueryProviderTrafficInvalidTimezoneFallsBackToShanghai(t *testing.T) {
	report, err := NewStatsService(&recordingUsageRepo{}).QueryProviderTraffic(context.Background(), "Not/A/Zone", time.Date(2026, 8, 12, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("QueryProviderTraffic() error = %v", err)
	}
	if report == nil || report.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %#v, want Asia/Shanghai", report)
	}
}

func TestClassTrafficSinceMatchesCalendarCards(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	now := time.Date(2026, 8, 12, 8, 0, 0, 0, loc)
	dayStart, weekStart, monthStart := ProviderTrafficBounds(now, loc)

	since, window := ClassTrafficSince("24h", loc, now)
	if window != "day" || !since.Equal(dayStart) {
		t.Fatalf("24h alias = %s %s, want day %s", window, since, dayStart)
	}
	since, window = ClassTrafficSince("7d", loc, now)
	if window != "week" || !since.Equal(weekStart) {
		t.Fatalf("7d alias = %s %s, want week %s", window, since, weekStart)
	}
	since, window = ClassTrafficSince("month", loc, now)
	if window != "month" || !since.Equal(monthStart) {
		t.Fatalf("month = %s %s, want %s", window, since, monthStart)
	}
}
