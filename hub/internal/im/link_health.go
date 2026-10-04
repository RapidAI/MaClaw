package im

import (
	"sort"
	"sync"
	"time"
)

// deviceErrorCodeGuiOffline is the 503 code a device receives when no MaClaw GUI
// holds the gateway for its tenant. The device classifies it as F3 ("brain
// offline"); it is the headline link-health signal because it is the failure
// that turns the companion into a brick.
const deviceErrorCodeGuiOffline = "gui_offline"

// LinkHealthTracker answers three questions the companion-terminal plan needs
// before any of its work can be judged (plan N0-3):
//
//  1. How often is the paired brain (a MaClaw GUI holding the gateway) actually
//     reachable? -> per-tenant online ratio.
//  2. How often did the Hub have to refuse a device request because no GUI was
//     connected? -> gui_offline rejections.
//  3. Of the device events that reached the Hub, how many were handed to a live
//     brain? -> device event success ratio, broken down by rejection reason.
//
// Why this exists: F3 ("brain offline") is the single biggest threat to the
// companion experience -- a 503 gui_offline turns the device into a brick --
// and until now that failure was only visible as a log line. Quantifying it is
// what makes the offline-resilience work (N2-2/N2-3/N2-5) provable rather than
// anecdotal.
//
// Online accounting is per tenant because the gateway owner map is per tenant:
// one global ratio would hide a single tenant being offline 100% of the time.
// Counters are process-lifetime and never reset, matching the Hub's existing
// metrics convention (see httpapi/ve_metrics.go).
type LinkHealthTracker struct {
	startedAt time.Time

	// mu guards every field below. Device events are human-paced, so a single
	// mutex is simpler and less error-prone than mixing atomics with the
	// per-tenant maps that the online accounting requires anyway.
	mu          sync.Mutex
	tenants     map[string]*tenantLinkHealth
	rejectByWhy map[string]uint64

	guiClaims   uint64
	guiReleases uint64
	guiOffline  uint64
	evAccepted  uint64
	evDuplicate uint64
	evRejected  uint64
}

// tenantLinkHealth is the per-tenant slice of the tracker.
type tenantLinkHealth struct {
	// onlineSince is when this tenant's GUI became the gateway owner. The zero
	// value means the tenant is currently offline.
	onlineSince time.Time
	// onlineNanos is the accumulated duration of completed online periods.
	onlineNanos uint64

	offlineRejections uint64
	eventsAccepted    uint64
	eventsDuplicate   uint64
	eventsRejected    uint64
}

// DeviceEventOutcome classifies what happened to one device-originated event.
type DeviceEventOutcome string

const (
	// DeviceEventAccepted means the Hub handed the event to a live GUI.
	DeviceEventAccepted DeviceEventOutcome = "accepted"
	// DeviceEventDuplicate means replay suppression matched an event the Hub had
	// already accepted. It is a delivery success, not a failure: the device
	// retried because it never saw our response.
	DeviceEventDuplicate DeviceEventOutcome = "duplicate"
	// DeviceEventRejected means the Hub refused to hand the event over.
	DeviceEventRejected DeviceEventOutcome = "rejected"
)

// LinkHealthSnapshot is the JSON view served to the admin panel.
type LinkHealthSnapshot struct {
	ObservedSeconds uint64 `json:"observed_seconds"`

	GuiOnline        bool    `json:"gui_online"`
	GuiOnlineTenants int     `json:"gui_online_tenants"`
	GuiClaimTotal    uint64  `json:"gui_claim_total"`
	GuiReleaseTotal  uint64  `json:"gui_release_total"`
	GuiOnlineSeconds uint64  `json:"gui_online_seconds"`
	GuiOnlineRatio   float64 `json:"gui_online_ratio"`
	// GuiOfflineRejections counts requests refused with the 503 gui_offline
	// code -- the direct measure of "the device had nobody to talk to".
	GuiOfflineRejections uint64 `json:"gui_offline_rejections"`

	DeviceEventAccepted     uint64            `json:"device_event_accepted"`
	DeviceEventDuplicate    uint64            `json:"device_event_duplicate"`
	DeviceEventRejected     uint64            `json:"device_event_rejected"`
	DeviceEventDelivered    uint64            `json:"device_event_delivered"`
	DeviceEventSuccessRatio float64           `json:"device_event_success_ratio"`
	DeviceEventRejectReason map[string]uint64 `json:"device_event_rejected_by_reason,omitempty"`

	Tenants []LinkHealthTenantSnapshot `json:"tenants"`
}

// LinkHealthTenantSnapshot is the per-tenant slice of the snapshot.
type LinkHealthTenantSnapshot struct {
	TenantID          string  `json:"tenant_id"`
	GuiOnline         bool    `json:"gui_online"`
	OnlineSeconds     uint64  `json:"online_seconds"`
	OnlineRatio       float64 `json:"online_ratio"`
	OfflineRejections uint64  `json:"offline_rejections"`
	EventsAccepted    uint64  `json:"events_accepted"`
	EventsDuplicate   uint64  `json:"events_duplicate"`
	EventsRejected    uint64  `json:"events_rejected"`
}

// NewLinkHealthTracker creates an empty tracker. Production code uses the
// package-level instance via LinkHealth(); tests construct their own.
func NewLinkHealthTracker() *LinkHealthTracker {
	return newLinkHealthTracker(time.Now())
}

func newLinkHealthTracker(startedAt time.Time) *LinkHealthTracker {
	return &LinkHealthTracker{
		startedAt:   startedAt,
		tenants:     make(map[string]*tenantLinkHealth),
		rejectByWhy: make(map[string]uint64),
	}
}

// ObserveGuiClaim records that a GUI became the gateway owner for a tenant.
//
// A takeover (same user, new machine) calls this while the tenant is already
// online. The claim is still counted, but the open online period is left alone
// so a machine swap does not fabricate an offline gap.
func (t *LinkHealthTracker) ObserveGuiClaim(tenantID string, now time.Time) {
	if t == nil {
		return
	}
	tenantID = normalizeRemoteTenantID(tenantID)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.guiClaims++
	state := t.tenantLocked(tenantID)
	if state.onlineSince.IsZero() {
		state.onlineSince = now
	}
}

// ObserveGuiRelease records that a tenant's GUI released the gateway. Releasing
// when no owner is held is a no-op, so a stale connection cleanup cannot close
// an online period that belongs to a newer claim.
func (t *LinkHealthTracker) ObserveGuiRelease(tenantID string, now time.Time) {
	if t == nil {
		return
	}
	tenantID = normalizeRemoteTenantID(tenantID)
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.tenantLocked(tenantID)
	if state.onlineSince.IsZero() {
		return
	}
	t.guiReleases++
	t.closeOnlineLocked(state, now)
}

// ObserveDeviceEvent records the outcome of one device-originated event. reason
// is only meaningful for DeviceEventRejected and should be the machine-readable
// error code the device received (e.g. "gui_offline").
func (t *LinkHealthTracker) ObserveDeviceEvent(tenantID string, outcome DeviceEventOutcome, reason string) {
	if t == nil {
		return
	}
	tenantID = normalizeRemoteTenantID(tenantID)
	t.mu.Lock()
	defer t.mu.Unlock()
	// The tenant entry is only created for a recognised outcome, so a caller
	// passing a typo cannot materialise a phantom tenant in the panel.
	switch outcome {
	case DeviceEventAccepted:
		t.evAccepted++
		t.tenantLocked(tenantID).eventsAccepted++
	case DeviceEventDuplicate:
		t.evDuplicate++
		t.tenantLocked(tenantID).eventsDuplicate++
	case DeviceEventRejected:
		t.evRejected++
		t.tenantLocked(tenantID).eventsRejected++
		if reason != "" {
			t.rejectByWhy[reason]++
			if reason == deviceErrorCodeGuiOffline {
				t.guiOffline++
				t.tenantLocked(tenantID).offlineRejections++
			}
		}
	}
}

// Snapshot renders the tracker for a read-only endpoint. now is the moment the
// snapshot was taken; it is passed in rather than read from the clock so the
// online-ratio arithmetic is deterministic under test.
func (t *LinkHealthTracker) Snapshot(now time.Time) LinkHealthSnapshot {
	if t == nil {
		return LinkHealthSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	snapshot := LinkHealthSnapshot{
		ObservedSeconds:         uint64(positiveSeconds(now.Sub(t.startedAt))),
		GuiClaimTotal:           t.guiClaims,
		GuiReleaseTotal:         t.guiReleases,
		GuiOfflineRejections:    t.guiOffline,
		DeviceEventAccepted:     t.evAccepted,
		DeviceEventDuplicate:    t.evDuplicate,
		DeviceEventRejected:     t.evRejected,
		DeviceEventDelivered:    t.evAccepted + t.evDuplicate,
		DeviceEventSuccessRatio: ratio(t.evAccepted+t.evDuplicate, t.evAccepted+t.evDuplicate+t.evRejected),
	}
	if len(t.rejectByWhy) > 0 {
		snapshot.DeviceEventRejectReason = make(map[string]uint64, len(t.rejectByWhy))
		for reason, count := range t.rejectByWhy {
			snapshot.DeviceEventRejectReason[reason] = count
		}
	}

	observedSeconds := snapshot.ObservedSeconds
	snapshot.Tenants = make([]LinkHealthTenantSnapshot, 0, len(t.tenants))
	var ratioSum float64
	for tenantID, state := range t.tenants {
		onlineNanos := state.onlineNanos
		// Fold the period that is still open into the reported total, without
		// mutating the tracker.
		if !state.onlineSince.IsZero() {
			onlineNanos += uint64(positiveDuration(now.Sub(state.onlineSince)))
			snapshot.GuiOnlineTenants++
		}
		tenant := LinkHealthTenantSnapshot{
			TenantID:          tenantID,
			GuiOnline:         !state.onlineSince.IsZero(),
			OnlineSeconds:     onlineNanos / uint64(time.Second),
			OfflineRejections: state.offlineRejections,
			EventsAccepted:    state.eventsAccepted,
			EventsDuplicate:   state.eventsDuplicate,
			EventsRejected:    state.eventsRejected,
		}
		tenant.OnlineRatio = ratio(tenant.OnlineSeconds, observedSeconds)
		ratioSum += tenant.OnlineRatio
		snapshot.GuiOnlineSeconds += tenant.OnlineSeconds
		snapshot.Tenants = append(snapshot.Tenants, tenant)
	}
	sort.Slice(snapshot.Tenants, func(i, j int) bool {
		return snapshot.Tenants[i].TenantID < snapshot.Tenants[j].TenantID
	})
	snapshot.GuiOnline = snapshot.GuiOnlineTenants > 0
	// GuiOnlineSeconds is a fleet total: it exceeds the observed window whenever
	// several tenants are online at once. The fleet ratio is therefore the mean
	// of the per-tenant ratios. Total/observed would over-report and then be
	// silently clamped to 1, hiding a tenant whose brain is never reachable.
	if len(snapshot.Tenants) > 0 {
		snapshot.GuiOnlineRatio = clampRatio(ratioSum / float64(len(snapshot.Tenants)))
	}
	return snapshot
}

func (t *LinkHealthTracker) tenantLocked(tenantID string) *tenantLinkHealth {
	state := t.tenants[tenantID]
	if state == nil {
		state = &tenantLinkHealth{}
		t.tenants[tenantID] = state
	}
	return state
}

func (t *LinkHealthTracker) closeOnlineLocked(state *tenantLinkHealth, now time.Time) {
	state.onlineNanos += uint64(positiveDuration(now.Sub(state.onlineSince)))
	state.onlineSince = time.Time{}
}

// positiveDuration clamps a possibly-negative duration (clock skew, or a caller
// passing an observation time older than the period start) to zero so the
// accumulated totals can never wrap around.
func positiveDuration(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

func positiveSeconds(d time.Duration) int64 {
	return int64(positiveDuration(d) / time.Second)
}

// ratio returns numerator/denominator as a 0..1 fraction, or 0 when there is
// nothing to divide by.
func ratio(numerator, denominator uint64) float64 {
	if denominator == 0 {
		return 0
	}
	return clampRatio(float64(numerator) / float64(denominator))
}

// clampRatio keeps a computed fraction inside 0..1 so a caller can never render
// a percentage above 100.
func clampRatio(value float64) float64 {
	if value > 1 {
		return 1
	}
	if value < 0 {
		return 0
	}
	return value
}

// ---------------------------------------------------------------------------
// Package-level instance used by the gateway code paths.
// ---------------------------------------------------------------------------

var globalLinkHealth = NewLinkHealthTracker()

// LinkHealth returns the process-wide tracker. Production call sites use this;
// tests should construct their own tracker with NewLinkHealthTracker.
func LinkHealth() *LinkHealthTracker {
	return globalLinkHealth
}

// ResetLinkHealthForTest swaps in a fresh tracker and returns a restore func.
// Test-only: it exists so the global instance cannot leak observations between
// tests, mirroring resetMetricsForTest in internal/cloudworkspace.
func ResetLinkHealthForTest() func() {
	previous := globalLinkHealth
	globalLinkHealth = NewLinkHealthTracker()
	return func() { globalLinkHealth = previous }
}
