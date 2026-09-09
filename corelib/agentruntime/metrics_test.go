package agentruntime

import (
	"testing"
	"time"
)

func TestRuntimeMetricsAggregatesAndHashesTenantSeries(t *testing.T) {
	m := NewRuntimeMetrics(2)
	m.RecordTurnAdmitted("tenant-a", 1500*time.Millisecond)
	m.RecordToken("tenant-a", 12*time.Millisecond, true)
	m.RecordToken("tenant-a", 0, false)
	m.RecordToolCall("tenant-a", true)
	m.RecordEvent("tenant-a", true, false)
	m.RecordQuotaRejected("tenant-a")
	m.RecordRateLimited("tenant-a")
	m.RecordRateLimitBucketEvicted()
	m.RecordTurnFinished("tenant-a", "succeeded")

	// A third tenant is still counted globally but does not create a label
	// series once the configured cardinality budget is exhausted.
	m.RecordTurnAdmitted("tenant-b", 0)
	m.RecordTurnFinished("tenant-b", "failed")
	m.RecordTurnAdmitted("tenant-c", 0)
	m.RecordTurnFinished("tenant-c", "cancelled")

	s := m.Snapshot()
	if s.TurnsAdmitted != 3 || s.TurnsSucceeded != 1 || s.TurnsFailed != 1 || s.TurnsCancelled != 1 {
		t.Fatalf("unexpected global lifecycle snapshot: %#v", s)
	}
	if s.TokenDeltas != 2 || s.ToolCalls != 1 || s.ToolErrors != 1 || s.EventsEmitted != 1 || s.QuotaRejected != 1 || s.RateLimited != 1 || s.RateLimitBucketsEvicted != 1 {
		t.Fatalf("unexpected global counters: %#v", s)
	}
	if s.QueueWaitSamples != 3 || s.FirstTokenSamples != 1 || s.FirstTokenMilliseconds != 12 {
		t.Fatalf("unexpected timing aggregates: %#v", s)
	}
	if len(s.Tenants) != 2 || s.TenantSeriesDropped == 0 {
		t.Fatalf("tenant cardinality bound not enforced: %#v", s)
	}
	if s.Tenants[0].TenantHash == "tenant-a" || len(s.Tenants[0].TenantHash) != 16 {
		t.Fatalf("tenant id leaked or hash has wrong shape: %#v", s.Tenants)
	}
	if TenantMetricsHash("tenant-a") != s.Tenants[0].TenantHash && TenantMetricsHash("tenant-a") != s.Tenants[1].TenantHash {
		t.Fatalf("tenant hash missing from snapshot: %#v", s.Tenants)
	}
}

func TestRuntimeMetricsNilAndNegativeInputsAreSafe(t *testing.T) {
	var m *RuntimeMetrics
	m.RecordTurnAdmitted("tenant", -time.Second)
	m.RecordTurnFinished("tenant", "succeeded")
	if got := m.Snapshot(); got.TurnsAdmitted != 0 || len(got.Tenants) != 0 {
		t.Fatalf("nil metrics should be a no-op: %#v", got)
	}
	collector := NewRuntimeMetrics(1)
	collector.RecordTurnAdmitted("tenant", -time.Second)
	collector.RecordTurnFinished("tenant", "cancelled")
	s := collector.Snapshot()
	if s.QueueWaitSamples != 0 || s.TurnsCancelled != 1 || s.ActiveRuns != 0 {
		t.Fatalf("negative wait or lifecycle accounting incorrect: %#v", s)
	}
}
