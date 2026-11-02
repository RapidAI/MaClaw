//go:build windows

package guiapp

import (
	"testing"
	"time"
)

// The latency probe subtracts the newest native click from the newest
// page-reported input event. Both clocks are the system clock (time.Now on
// the Go side, Date.now() in the page), so a healthy pipeline yields ~0ms and
// a wedged one yields the delivery delay.
func TestNoteFrontendPageEventsMatchesLatency(t *testing.T) {
	clickEchoMu.Lock()
	clickEchoStamps = [clickEchoBufSize]time.Time{}
	clickEchoCount = 0
	clickEchoNextSlot = 0
	clickEchoMu.Unlock()
	t.Cleanup(func() {
		clickEchoMu.Lock()
		clickEchoStamps = [clickEchoBufSize]time.Time{}
		clickEchoCount = 0
		clickEchoNextSlot = 0
		clickEchoMu.Unlock()
		clickLatencyMu.Lock()
		clickLatencyAt = [clickLatencyRingSize]time.Time{}
		clickLatencyDur = [clickLatencyRingSize]time.Duration{}
		clickLatencyNext = 0
		clickLatencyCount = 0
		clickLatencyMu.Unlock()
	})

	// Two native clicks, then the page reports both delivered 90s late. The
	// lag is measured against the newest click. Base sits 120s in the past so
	// the "delivered 90s late" events stay past-dated, as real delayed
	// delivery always is (a delivered event can never postdate its click).
	base := time.Now().Add(-120 * time.Second)
	clickEchoRecord(base)
	clickEchoRecord(base.Add(2 * time.Second))
	noteFrontendPageEvents(nil)
	if got := recentMaxClickLatency(time.Minute); got != 0 {
		t.Fatalf("latency without page events = %s, want 0", got)
	}
	noteFrontendPageEvents([]int64{
		base.Add(90 * time.Second).UnixMilli(),
		base.Add(90*time.Second + 2*time.Second).UnixMilli(),
	})
	if got := recentMaxClickLatency(time.Minute); got < 85*time.Second {
		t.Fatalf("max latency = %s, want >= 85s (90s delay measured at the newest click)", got)
	}

	// A stale report that never caught up to the newest click is no evidence:
	// it must not record an additional sample at all.
	clickLatencyMu.Lock()
	countBefore := clickLatencyCount
	clickLatencyMu.Unlock()
	clickEchoRecord(time.Now())
	noteFrontendPageEvents([]int64{time.Now().Add(-30 * time.Second).UnixMilli()})
	clickLatencyMu.Lock()
	countAfter := clickLatencyCount
	clickLatencyMu.Unlock()
	if countAfter != countBefore {
		t.Fatalf("stale report recorded a sample: count %d -> %d", countBefore, countAfter)
	}

	// A clock-skewed report (events dated in the future) is untrustworthy and
	// must not record either.
	noteFrontendPageEvents([]int64{time.Now().Add(30 * time.Second).UnixMilli()})
	clickLatencyMu.Lock()
	countSkew := clickLatencyCount
	clickLatencyMu.Unlock()
	if countSkew != countBefore {
		t.Fatalf("skewed report recorded a sample: count %d -> %d", countBefore, countSkew)
	}

	// Fresh click + fresh report: the newest ring slot holds near-zero latency.
	noteFrontendPageEvents([]int64{time.Now().UnixMilli()})
	clickLatencyMu.Lock()
	idx := (clickLatencyNext + clickLatencyRingSize - 1) % clickLatencyRingSize
	fresh := clickLatencyDur[idx]
	clickLatencyMu.Unlock()
	if fresh > 10*time.Second {
		t.Fatalf("fresh latency = %s, want near zero", fresh)
	}
	if got := recentMaxClickLatency(time.Minute); got < 85*time.Second {
		t.Fatalf("windowed max lost the delayed sample: %s", got)
	}
}
