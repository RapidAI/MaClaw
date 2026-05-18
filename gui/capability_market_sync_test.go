package main

import "testing"

func TestCapabilitySyncImmediateReason(t *testing.T) {
	for _, reason := range []string{"hub-connect", "hub-config-update", "manual", "startup", " install "} {
		if !isCapabilitySyncImmediateReason(reason) {
			t.Fatalf("reason %q should run immediately", reason)
		}
	}
	if isCapabilitySyncImmediateReason("hub-heartbeat") {
		t.Fatal("heartbeat sync should be throttleable")
	}
}

func TestCapabilityManagedSyncRetryDelayThrottlesHeartbeatNoise(t *testing.T) {
	if got := capabilityManagedSyncRetryDelay([]string{"download skill failed: unexpected EOF"}); got < capabilityManagedSyncMinRetry {
		t.Fatalf("retry delay = %s, want at least %s", got, capabilityManagedSyncMinRetry)
	}
}
