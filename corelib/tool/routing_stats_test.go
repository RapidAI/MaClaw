package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// isolateRoutingStats redirects the durable stats file into t.TempDir() and
// resets the process-global holder, so the test neither reads nor deletes the
// real maclawpath.DataDir()/stats/routing.json.
func isolateRoutingStats(t *testing.T) {
	t.Helper()
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() {
		_ = ResetRoutingStats()
		maclawpath.SetBaseDir(oldBase)
	})
	if err := ResetRoutingStats(); err != nil {
		t.Fatalf("reset: %v", err)
	}
}

// blockStatsDir makes RoutingStatsPath() unwritable by placing a regular
// file where persistRoutingStats must create the "stats" directory.
func blockStatsDir(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(maclawpath.DataDir(), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(maclawpath.DataDir(), "stats"), []byte("block"), 0o644); err != nil {
		t.Fatalf("block stats dir: %v", err)
	}
}

func TestRoutingStatsRecordAndReset(t *testing.T) {
	isolateRoutingStats(t)

	RecordLegacyRoute(3)
	RecordLegacyRoute(10)
	RecordConditionalRouteActivation()
	RecordUnrenderedToolCallDenial()
	RecordConsumedGrantDenial()
	RecordPetitionGrant()
	RecordDiscoverToolCall(true)
	RecordDiscoverToolCall(false)

	st := GetRoutingStats()
	if st.LegacyRouteCalls != 2 {
		t.Fatalf("calls=%d", st.LegacyRouteCalls)
	}
	if st.LegacyRouteSelectedTotal != 13 || st.LegacyRouteMaxSelected != 10 || st.LegacyRouteLastSelected != 10 {
		t.Fatalf("selected total=%d max=%d last=%d", st.LegacyRouteSelectedTotal, st.LegacyRouteMaxSelected, st.LegacyRouteLastSelected)
	}
	if st.ConditionalActivated != 1 || st.UnrenderedDenials != 1 || st.ConsumedGrantDenials != 1 || st.PetitionGrants != 1 {
		t.Fatalf("stats=%+v", st)
	}
	if st.DiscoverToolCalls != 2 || st.DiscoverToolMisses != 1 {
		t.Fatalf("discover=%d miss=%d", st.DiscoverToolCalls, st.DiscoverToolMisses)
	}

	line := FormatRoutingLine()
	if !strings.Contains(line, "legacy=2") || !strings.Contains(line, "unrendered=1") {
		t.Fatalf("line=%q", line)
	}

	if err := ResetRoutingStats(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	st = GetRoutingStats()
	if st.LegacyRouteCalls != 0 || st.LegacyRouteMaxSelected != 0 || st.UnrenderedDenials != 0 {
		t.Fatalf("after reset: %+v", st)
	}
	if FormatRoutingLine() != "" {
		t.Fatalf("line after reset: %q", FormatRoutingLine())
	}
}

// TestRecordFunctionsHonorDisabledGate verifies that EVERY Record* function
// no-ops when RecordRoutingStats is false, not just the route-recording
// pair: the flag's contract (routing_stats.go doc) promises offline
// harnesses that replayed traffic never mutates live counters or schedules
// durable writes.
func TestRecordFunctionsHonorDisabledGate(t *testing.T) {
	isolateRoutingStats(t)
	RecordRoutingStats = false
	t.Cleanup(func() { RecordRoutingStats = true })

	recordFns := []struct {
		name string
		call func()
	}{
		{"RecordLegacyRoute", func() { RecordLegacyRoute(5) }},
		{"RecordConditionalRouteActivation", func() { RecordConditionalRouteActivation() }},
		{"RecordUnrenderedToolCallDenial", func() { RecordUnrenderedToolCallDenial() }},
		{"RecordConsumedGrantDenial", func() { RecordConsumedGrantDenial() }},
		{"RecordPetitionGrant", func() { RecordPetitionGrant() }},
		{"RecordDiscoverToolCall", func() { RecordDiscoverToolCall(false) }},
	}
	for _, fn := range recordFns {
		fn.call()
	}

	if st := GetRoutingStats(); st != (RoutingStats{}) {
		t.Fatalf("stats mutated with RecordRoutingStats=false: %+v", st)
	}
	processRoutingStats.mu.Lock()
	dirty := processRoutingStats.persistDirty
	timer := processRoutingStats.persistTimer
	processRoutingStats.mu.Unlock()
	if dirty || timer != nil {
		t.Fatal("disabled gate must not mark dirty or schedule a durable write")
	}

	// Flipping back on restores recording.
	RecordRoutingStats = true
	RecordUnrenderedToolCallDenial()
	if st := GetRoutingStats(); st.UnrenderedDenials != 1 {
		t.Fatalf("after re-enable: %+v", st)
	}
}

// TestRoutingStatsPersistFailureCap blocks the durable stats path (a regular
// file where the "stats" directory must be created) and drives repeated
// persist attempts: after routingStatsMaxPersistFailures consecutive
// failures the retry loop must stop re-arming, the dirty flag is dropped,
// and the in-memory counters remain intact.
func TestRoutingStatsPersistFailureCap(t *testing.T) {
	isolateRoutingStats(t)
	blockStatsDir(t)

	RecordLegacyRoute(2)

	// Each flush attempt fails at MkdirAll and counts one consecutive
	// failure; drive the loop deterministically instead of waiting for the
	// 400ms debounce between re-arms.
	for i := 0; i < routingStatsMaxPersistFailures; i++ {
		if err := FlushRoutingStats(); err == nil {
			t.Fatalf("flush %d: want persist error", i)
		}
	}

	processRoutingStats.mu.Lock()
	failures := processRoutingStats.persistFailures
	dirty := processRoutingStats.persistDirty
	timer := processRoutingStats.persistTimer
	processRoutingStats.mu.Unlock()
	if failures != routingStatsMaxPersistFailures {
		t.Fatalf("persistFailures=%d, want %d", failures, routingStatsMaxPersistFailures)
	}
	if dirty {
		t.Fatal("dirty flag must be dropped after the retry cap")
	}
	if timer != nil {
		t.Fatal("persist timer must not be re-armed after the retry cap")
	}

	st := GetRoutingStats()
	if st.LegacyRouteCalls != 1 || st.LegacyRouteSelectedTotal != 2 {
		t.Fatalf("in-memory counters must stay live after capped retries: %+v", st)
	}
	// A subsequent flush with a clean dirty flag must be a no-op success.
	if err := FlushRoutingStats(); err != nil {
		t.Fatalf("flush after cap: %v", err)
	}
}

// TestRoutingStatsPersistSuccessResetsFailureStreak verifies that a
// successful persist clears the consecutive-failure counter so later
// transient failures get a full retry budget again.
func TestRoutingStatsPersistSuccessResetsFailureStreak(t *testing.T) {
	isolateRoutingStats(t)
	blockStatsDir(t)
	RecordLegacyRoute(1)
	for i := 0; i < 3; i++ {
		if err := FlushRoutingStats(); err == nil {
			t.Fatalf("flush %d: want persist error", i)
		}
	}
	processRoutingStats.mu.Lock()
	failed := processRoutingStats.persistFailures
	processRoutingStats.mu.Unlock()
	if failed != 3 {
		t.Fatalf("persistFailures=%d, want 3", failed)
	}

	// Unblock the path and flush successfully.
	if err := os.Remove(filepath.Join(maclawpath.DataDir(), "stats")); err != nil {
		t.Fatalf("unblock stats dir: %v", err)
	}
	if err := FlushRoutingStats(); err != nil {
		t.Fatalf("flush after unblock: %v", err)
	}
	processRoutingStats.mu.Lock()
	after := processRoutingStats.persistFailures
	processRoutingStats.mu.Unlock()
	if after != 0 {
		t.Fatalf("persistFailures=%d after success, want 0", after)
	}
	if _, err := os.Stat(RoutingStatsPath()); err != nil {
		t.Fatalf("durable file missing after successful flush: %v", err)
	}
}
