package guiapp

import (
	"sync"
	"time"
)

// Click-latency measurement for the delayed-input WebView2 failure variant:
// clicks are not dropped but delivered minutes late. The WH_MOUSE_LL hook
// timestamps each native click on the main window (system clock), the page's
// input agent timestamps each delivered DOM input event (Date.now(), the same
// system clock), and this matcher pairs them. Latency = page delivery time -
// native click time, so a wedged pipeline shows up as multi-second samples
// instead of the absent samples the dropped-input variant produces.
//
// Cross-platform by design: on non-Windows builds windowClickStamps returns
// nil and every function here no-ops, but the logic stays testable.

const clickLatencyMatchWindow = 5 * time.Minute

const clickLatencyRingSize = 32

var (
	clickLatencyMu     sync.Mutex
	clickLatencyAt     [clickLatencyRingSize]time.Time // when the sample was taken
	clickLatencyDur    [clickLatencyRingSize]time.Duration
	clickLatencyNext   int
	clickLatencyCount  int
)

// noteFrontendPageEvents records one latency sample per page report: the page
// has demonstrably delivered input up to its newest reported pointer event, so
// the queue lag is newestReportedPointerEvent - newestNativeClick. Only
// pointer events count on both sides: mouse movement or keyboard input after
// a click must not inflate "click latency" on a healthy page. Input is
// delivered in FIFO order, which makes the newest-to-newest comparison sound
// without per-event pairing: any pointer event at or after a click proves that
// click (and all before it) went through; a report that predates the newest
// click carries no information yet. Client (Date.now) and native hook
// (time.Now) share the system clock, so no offset correction is needed.
func noteFrontendPageEvents(evts []int64) {
	if len(evts) == 0 {
		return
	}
	clicks := windowClickStamps(clickLatencyMatchWindow)
	if len(clicks) == 0 {
		return
	}
	var newestPEvt int64
	for _, evt := range evts {
		if evt > newestPEvt {
			newestPEvt = evt
		}
	}
	if newestPEvt == 0 {
		return // report carried no pointer events
	}
	// Clock-skew guard, same policy as noteFrontendInputEvent: a report dated
	// in the future carries no trustworthy delivery information.
	if newestPEvt > time.Now().UnixMilli()+5000 {
		return
	}
	const jitterMs = int64(500)
	newestClickMs := clicks[len(clicks)-1].UnixMilli()
	if newestPEvt < newestClickMs-jitterMs {
		return // the page has not caught up to the newest click yet
	}
	d := time.Duration(newestPEvt-newestClickMs) * time.Millisecond
	if d < 0 {
		d = 0
	}
	clickLatencyMu.Lock()
	idx := clickLatencyNext
	clickLatencyAt[idx] = time.Now()
	clickLatencyDur[idx] = d
	clickLatencyNext = (idx + 1) % clickLatencyRingSize
	if clickLatencyCount < clickLatencyRingSize {
		clickLatencyCount++
	}
	clickLatencyMu.Unlock()
}

// recentMaxClickLatency returns the largest matched click latency inside the
// window, or 0 when no sample exists.
func recentMaxClickLatency(window time.Duration) time.Duration {
	cutoff := time.Now().Add(-window)
	clickLatencyMu.Lock()
	defer clickLatencyMu.Unlock()
	var max time.Duration
	for i := 0; i < clickLatencyCount; i++ {
		if clickLatencyAt[i].Before(cutoff) {
			continue
		}
		if clickLatencyDur[i] > max {
			max = clickLatencyDur[i]
		}
	}
	return max
}
