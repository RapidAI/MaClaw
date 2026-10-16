package guiapp

import "testing"

func TestRecordFailureUsesDegradedInsteadOfSlow(t *testing.T) {
	registry := NewMCPRegistry(&App{testHomeDir: t.TempDir()})
	registry.recordFailure("srv")
	registry.recordFailure("srv")
	got := registry.health["srv"]
	if got == nil || got.Status != mcpHealthStatusDegraded || got.FailCount != 2 {
		t.Fatalf("two failures = %#v", got)
	}
	if mcpHealthObservationSucceeded(got.Status) {
		t.Fatal("a failed probe was treated as a successful observation")
	}
	registry.recordSuccess("srv")
	if registry.health["srv"].Status != mcpHealthStatusDegraded || registry.health["srv"].FailCount != 0 {
		t.Fatalf("tools/call must not restore a failed list probe: %#v", registry.health["srv"])
	}
	registry.recordFailure("srv")
	registry.recordFailure("srv")
	registry.recordFailure("srv")
	if registry.health["srv"].Status != mcpHealthStatusUnavailable || registry.health["srv"].FailCount != 3 {
		t.Fatalf("third failure = %#v", registry.health["srv"])
	}
	registry.recordSuccess("srv")
	if registry.health["srv"].Status != mcpHealthStatusUnavailable || registry.health["srv"].FailCount != 3 {
		t.Fatalf("unavailable repaired by tools/call: %#v", registry.health["srv"])
	}
	registry.health["srv"].Status = mcpHealthStatusSlow
	registry.health["srv"].FailCount = 0
	registry.recordSuccess("srv")
	if registry.health["srv"].Status != mcpHealthStatusHealthy {
		t.Fatalf("successful observation = %#v", registry.health["srv"])
	}
}

func TestMCPHealthObservationSucceededIsOnlyACompletedProbe(t *testing.T) {
	for _, status := range []mcpHealthStatus{mcpHealthStatusHealthy, mcpHealthStatusSlow, " SLOW "} {
		if !mcpHealthObservationSucceeded(status) {
			t.Fatalf("%q must be a completed probe", status)
		}
	}
	for _, status := range []mcpHealthStatus{mcpHealthStatusDegraded, mcpHealthStatusUnknown, mcpHealthStatusUnavailable, ""} {
		if mcpHealthObservationSucceeded(status) {
			t.Fatalf("%q must not be a completed probe", status)
		}
	}
}
