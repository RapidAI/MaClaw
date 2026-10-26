package tool

import (
	"fmt"
	"testing"
)

// benchmarkUsageTracker builds a tracker at the record cap (2000) spread over
// 100 tools, which is the worst case for routing-time scoring.
func benchmarkUsageTracker(b *testing.B) *UsageTracker {
	b.Helper()
	tr, err := NewUsageTracker("")
	if err != nil {
		b.Fatal(err)
	}
	tokens := []string{"read", "config", "file", "database", "settings"}
	for i := 0; i < 2000; i++ {
		tr.RecordOutcome(fmt.Sprintf("tool_%d", i%100), tokens, i%3 != 0, "")
	}
	return tr
}

// BenchmarkUsageTrackerRoutingScores measures the three per-tool signals the
// router requests for every candidate: 100 tools × 3 queries over 2000
// records. This was 34.8 ms before the per-tool index and allocation-free
// Jaccard; a regression here dominates routing CPU.
func BenchmarkUsageTrackerRoutingScores(b *testing.B) {
	tr := benchmarkUsageTracker(b)
	query := []string{"read", "config", "file", "database", "settings"}
	tools := make([]string, 100)
	for i := range tools {
		tools[i] = fmt.Sprintf("tool_%d", i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for _, tool := range tools {
			_ = tr.ExperienceScore(tool, query)
			_ = tr.ContextOutcomeScore(tool, query)
			_ = tr.RoutingHintAdjustment(tool, query)
		}
	}
}

// BenchmarkUsageTrackerRecord measures the write path at the record cap, where
// every append trims one record. Historically this rewrote the whole history
// file synchronously (2.2 ms, 825 KB per call); it now only arms the
// coalescing writer.
func BenchmarkUsageTrackerRecord(b *testing.B) {
	tr := benchmarkUsageTracker(b)
	tokens := []string{"read", "config"}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		tr.RecordOutcome(fmt.Sprintf("tool_%d", n%100), tokens, true, "")
	}
}
