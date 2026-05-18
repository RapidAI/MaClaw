package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
)

func TestLansengerStatusNeedsWatchdogRestart(t *testing.T) {
	restartStatuses := []gatewayConnectionStatus{
		gatewayConnectionStatusConnecting,
		gatewayConnectionStatusReconnecting,
		gatewayConnectionStatusUnknown,
	}
	for _, status := range restartStatuses {
		if !lansengerStatusNeedsWatchdogRestart(status) {
			t.Fatalf("status %q should trigger watchdog restart", status)
		}
	}

	steadyStatuses := []gatewayConnectionStatus{
		gatewayConnectionStatusConnected,
		gatewayConnectionStatusDisconnected,
		gatewayConnectionStatusError,
	}
	for _, status := range steadyStatuses {
		if lansengerStatusNeedsWatchdogRestart(status) {
			t.Fatalf("status %q should not trigger stale-status watchdog restart", status)
		}
	}
}

func TestLansengerGatewayManagerIgnoresStaleStatus(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	gateway := &lansenger.Gateway{}
	m.gateway = gateway
	m.status = gatewayConnectionStatusConnected
	m.statusSince = time.Now()
	before := m.statusSince

	m.onGatewayStatusChange(nil, "error")

	if m.Status() != gatewayConnectionStatusConnected.String() {
		t.Fatalf("status = %q, want connected", m.Status())
	}
	if !m.statusSince.Equal(before) {
		t.Fatal("stale status update changed statusSince")
	}
}

func TestLansengerRestartCooldown(t *testing.T) {
	now := time.Now()
	if lansengerRestartInCooldown(time.Time{}, now) {
		t.Fatal("zero last restart should not be in cooldown")
	}
	if !lansengerRestartInCooldown(now.Add(-30*time.Second), now) {
		t.Fatal("restart should be in cooldown")
	}
	if lansengerRestartInCooldown(now.Add(-2*time.Minute), now) {
		t.Fatal("restart should not be in cooldown")
	}
}
