package im

import (
	"math"
	"testing"
	"time"
)

const (
	testTenantA = "tenant_a"
	testTenantB = "tenant_b"
)

func testEpoch() time.Time {
	return time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
}

func TestLinkHealthEmptySnapshot(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	got := tracker.Snapshot(started.Add(10 * time.Second))
	if got.ObservedSeconds != 10 {
		t.Fatalf("observed_seconds = %d, want 10", got.ObservedSeconds)
	}
	if got.GuiOnline {
		t.Fatal("empty tracker must report gui_online=false")
	}
	if got.GuiOnlineRatio != 0 || got.DeviceEventSuccessRatio != 0 {
		t.Fatalf("empty tracker must not divide by zero: gui=%v events=%v",
			got.GuiOnlineRatio, got.DeviceEventSuccessRatio)
	}
	if got.GuiClaimTotal != 0 || got.DeviceEventAccepted != 0 || got.GuiOfflineRejections != 0 {
		t.Fatal("empty tracker must report zero counters")
	}
	if len(got.Tenants) != 0 {
		t.Fatalf("empty tracker must report no tenants, got %d", len(got.Tenants))
	}
}

func TestLinkHealthOnlineRatioFollowsClaimAndRelease(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	// Online for the first 30s of a 120s window.
	tracker.ObserveGuiClaim(testTenantA, started)
	tracker.ObserveGuiRelease(testTenantA, started.Add(30*time.Second))

	got := tracker.Snapshot(started.Add(120 * time.Second))
	if got.GuiOnline {
		t.Fatal("released tenant must report gui_online=false")
	}
	if got.GuiOnlineTenants != 0 {
		t.Fatalf("gui_online_tenants = %d, want 0", got.GuiOnlineTenants)
	}
	if got.GuiOnlineSeconds != 30 {
		t.Fatalf("gui_online_seconds = %d, want 30", got.GuiOnlineSeconds)
	}
	if math.Abs(got.GuiOnlineRatio-0.25) > 1e-9 {
		t.Fatalf("gui_online_ratio = %v, want 0.25", got.GuiOnlineRatio)
	}
	if got.GuiClaimTotal != 1 || got.GuiReleaseTotal != 1 {
		t.Fatalf("claims=%d releases=%d, want 1/1", got.GuiClaimTotal, got.GuiReleaseTotal)
	}
}

func TestLinkHealthOpenPeriodCountsWithoutMutating(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)
	tracker.ObserveGuiClaim(testTenantA, started)

	// The GUI is still connected: the open period must be folded in, and
	// repeated snapshots at the same instant must agree.
	first := tracker.Snapshot(started.Add(40 * time.Second))
	if !first.GuiOnline {
		t.Fatal("connected tenant must report gui_online=true")
	}
	if first.GuiOnlineSeconds != 40 {
		t.Fatalf("gui_online_seconds = %d, want 40", first.GuiOnlineSeconds)
	}
	second := tracker.Snapshot(started.Add(40 * time.Second))
	if first.GuiOnlineSeconds != second.GuiOnlineSeconds {
		t.Fatalf("snapshot mutated state: %d then %d",
			first.GuiOnlineSeconds, second.GuiOnlineSeconds)
	}

	// A later release must only accumulate the period once.
	tracker.ObserveGuiRelease(testTenantA, started.Add(40*time.Second))
	after := tracker.Snapshot(started.Add(100 * time.Second))
	if after.GuiOnlineSeconds != 40 {
		t.Fatalf("released after snapshot: gui_online_seconds = %d, want 40",
			after.GuiOnlineSeconds)
	}
	if after.GuiReleaseTotal != 1 {
		t.Fatalf("gui_release_total = %d, want 1", after.GuiReleaseTotal)
	}
}

func TestLinkHealthTakeoverDoesNotFabricateOfflineGap(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	// A same-user re-enroll claims again while the tenant is already online.
	tracker.ObserveGuiClaim(testTenantA, started)
	tracker.ObserveGuiClaim(testTenantA, started.Add(10*time.Second))

	got := tracker.Snapshot(started.Add(60 * time.Second))
	if got.GuiClaimTotal != 2 {
		t.Fatalf("gui_claim_total = %d, want 2", got.GuiClaimTotal)
	}
	if got.GuiReleaseTotal != 0 {
		t.Fatalf("takeover must not release: gui_release_total = %d", got.GuiReleaseTotal)
	}
	if got.GuiOnlineSeconds != 60 {
		t.Fatalf("takeover reset the online period: gui_online_seconds = %d, want 60",
			got.GuiOnlineSeconds)
	}
}

func TestLinkHealthReleaseWithoutClaimIsIgnored(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	// A stale connection cleanup must not close a period it does not own.
	tracker.ObserveGuiRelease(testTenantA, started.Add(5*time.Second))

	got := tracker.Snapshot(started.Add(10 * time.Second))
	if got.GuiReleaseTotal != 0 {
		t.Fatalf("gui_release_total = %d, want 0", got.GuiReleaseTotal)
	}
	if got.GuiOnlineSeconds != 0 {
		t.Fatalf("gui_online_seconds = %d, want 0", got.GuiOnlineSeconds)
	}
}

func TestLinkHealthGuiOfflineRejectionIsHeadline(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	tracker.ObserveDeviceEvent(testTenantA, DeviceEventRejected, deviceErrorCodeGuiOffline)
	tracker.ObserveDeviceEvent(testTenantA, DeviceEventRejected, deviceErrorCodeGuiOffline)
	tracker.ObserveDeviceEvent(testTenantA, DeviceEventRejected, "unavailable")

	got := tracker.Snapshot(started.Add(time.Second))
	if got.GuiOfflineRejections != 2 {
		t.Fatalf("gui_offline_rejections = %d, want 2", got.GuiOfflineRejections)
	}
	if got.DeviceEventRejected != 3 {
		t.Fatalf("device_event_rejected = %d, want 3", got.DeviceEventRejected)
	}
	if got.DeviceEventRejectReason[deviceErrorCodeGuiOffline] != 2 {
		t.Fatalf("reject reason breakdown missing gui_offline: %v",
			got.DeviceEventRejectReason)
	}
	if got.DeviceEventRejectReason["unavailable"] != 1 {
		t.Fatalf("reject reason breakdown missing unavailable: %v",
			got.DeviceEventRejectReason)
	}
	// Nothing was delivered, so the success ratio must be zero -- not NaN.
	if got.DeviceEventSuccessRatio != 0 {
		t.Fatalf("device_event_success_ratio = %v, want 0", got.DeviceEventSuccessRatio)
	}
}

func TestLinkHealthDeviceEventSuccessRatio(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	tracker.ObserveDeviceEvent(testTenantA, DeviceEventAccepted, "")
	tracker.ObserveDeviceEvent(testTenantA, DeviceEventAccepted, "")
	tracker.ObserveDeviceEvent(testTenantA, DeviceEventDuplicate, "")
	tracker.ObserveDeviceEvent(testTenantA, DeviceEventRejected, deviceErrorCodeGuiOffline)

	got := tracker.Snapshot(started.Add(time.Second))
	if got.DeviceEventDelivered != 3 {
		t.Fatalf("device_event_delivered = %d, want 3", got.DeviceEventDelivered)
	}
	if math.Abs(got.DeviceEventSuccessRatio-0.75) > 1e-9 {
		t.Fatalf("device_event_success_ratio = %v, want 0.75", got.DeviceEventSuccessRatio)
	}
}

func TestLinkHealthPerTenantIsolation(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	// Tenant A's brain is up the whole time; tenant B's device is bricked.
	tracker.ObserveGuiClaim(testTenantA, started)
	tracker.ObserveDeviceEvent(testTenantA, DeviceEventAccepted, "")
	tracker.ObserveDeviceEvent(testTenantB, DeviceEventRejected, deviceErrorCodeGuiOffline)

	got := tracker.Snapshot(started.Add(100 * time.Second))
	if len(got.Tenants) != 2 {
		t.Fatalf("tenants = %d, want 2", len(got.Tenants))
	}
	if got.Tenants[0].TenantID != testTenantA || got.Tenants[1].TenantID != testTenantB {
		t.Fatalf("tenants must be sorted by id, got %s,%s",
			got.Tenants[0].TenantID, got.Tenants[1].TenantID)
	}

	online := got.Tenants[0]
	if !online.GuiOnline || online.OnlineSeconds != 100 {
		t.Fatalf("tenant A online state wrong: %+v", online)
	}
	if math.Abs(online.OnlineRatio-1) > 1e-9 {
		t.Fatalf("tenant A online ratio = %v, want 1", online.OnlineRatio)
	}
	if online.EventsAccepted != 1 || online.EventsRejected != 0 {
		t.Fatalf("tenant A event counters wrong: %+v", online)
	}

	offline := got.Tenants[1]
	if offline.GuiOnline || offline.OnlineSeconds != 0 {
		t.Fatalf("tenant B must be offline: %+v", offline)
	}
	if offline.OfflineRejections != 1 {
		t.Fatalf("tenant B offline rejections = %d, want 1", offline.OfflineRejections)
	}
	if offline.EventsRejected != 1 {
		t.Fatalf("tenant B rejected events = %d, want 1", offline.EventsRejected)
	}

	// The global ratio must reflect that only one of two tenants was reachable,
	// which is exactly what a single blended number would have hidden.
	if math.Abs(got.GuiOnlineRatio-0.5) > 1e-9 {
		t.Fatalf("global gui_online_ratio = %v, want 0.5", got.GuiOnlineRatio)
	}
	if got.GuiOnlineTenants != 1 {
		t.Fatalf("gui_online_tenants = %d, want 1", got.GuiOnlineTenants)
	}
}

func TestLinkHealthFleetRatioIsMeanNotSum(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	// Two tenants online for the whole window. The fleet *total* is 2x the
	// observed window, but the fleet *ratio* must still be 1, not 2 clamped.
	tracker.ObserveGuiClaim(testTenantA, started)
	tracker.ObserveGuiClaim(testTenantB, started)

	got := tracker.Snapshot(started.Add(50 * time.Second))
	if got.GuiOnlineSeconds != 100 {
		t.Fatalf("fleet gui_online_seconds = %d, want 100 (2 tenants x 50s)",
			got.GuiOnlineSeconds)
	}
	if math.Abs(got.GuiOnlineRatio-1) > 1e-9 {
		t.Fatalf("fleet gui_online_ratio = %v, want 1", got.GuiOnlineRatio)
	}
	if got.GuiOnlineTenants != 2 {
		t.Fatalf("gui_online_tenants = %d, want 2", got.GuiOnlineTenants)
	}
}

func TestLinkHealthEmptyTenantFallsBackToDefault(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	tracker.ObserveGuiClaim("", started)
	tracker.ObserveGuiRelease("  ", started.Add(5*time.Second))

	got := tracker.Snapshot(started.Add(10 * time.Second))
	if len(got.Tenants) != 1 {
		t.Fatalf("blank tenant ids must collapse to one default tenant, got %d", len(got.Tenants))
	}
	if got.Tenants[0].TenantID != "tenant_default" {
		t.Fatalf("tenant id = %q, want tenant_default", got.Tenants[0].TenantID)
	}
	if got.GuiClaimTotal != 1 || got.GuiReleaseTotal != 1 {
		t.Fatalf("claims=%d releases=%d, want 1/1", got.GuiClaimTotal, got.GuiReleaseTotal)
	}
}

func TestLinkHealthClockSkewIsClamped(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	// A release timestamp earlier than the claim must not underflow the
	// accumulated duration into an astronomically large number.
	tracker.ObserveGuiClaim(testTenantA, started.Add(time.Minute))
	tracker.ObserveGuiRelease(testTenantA, started)

	got := tracker.Snapshot(started.Add(2 * time.Minute))
	if got.GuiOnlineSeconds != 0 {
		t.Fatalf("gui_online_seconds = %d, want 0 after clamping", got.GuiOnlineSeconds)
	}
}

func TestLinkHealthUnknownOutcomeIsIgnored(t *testing.T) {
	started := testEpoch()
	tracker := newLinkHealthTracker(started)

	tracker.ObserveDeviceEvent(testTenantA, DeviceEventOutcome("bogus"), "")

	got := tracker.Snapshot(started.Add(time.Second))
	if got.DeviceEventAccepted+got.DeviceEventDuplicate+got.DeviceEventRejected != 0 {
		t.Fatalf("unknown outcome must be ignored: %+v", got)
	}
	if len(got.Tenants) != 0 {
		t.Fatalf("unknown outcome must not create a tenant entry, got %d", len(got.Tenants))
	}
}

func TestLinkHealthNilTrackerIsSafe(t *testing.T) {
	var tracker *LinkHealthTracker
	tracker.ObserveGuiClaim(testTenantA, testEpoch())
	tracker.ObserveGuiRelease(testTenantA, testEpoch())
	tracker.ObserveDeviceEvent(testTenantA, DeviceEventAccepted, "")
	if got := tracker.Snapshot(testEpoch()); got.ObservedSeconds != 0 {
		t.Fatalf("nil tracker snapshot = %+v, want zero value", got)
	}
}

func TestResetLinkHealthForTestRestoresPrevious(t *testing.T) {
	started := testEpoch()
	previous := globalLinkHealth
	restore := ResetLinkHealthForTest()
	if LinkHealth() == previous {
		t.Fatal("reset must swap in a fresh tracker")
	}
	LinkHealth().ObserveGuiClaim(testTenantA, started)
	if LinkHealth().Snapshot(started).GuiClaimTotal != 1 {
		t.Fatal("fresh tracker did not record the claim")
	}
	restore()
	if LinkHealth() != previous {
		t.Fatal("restore must put the previous tracker back")
	}
}
