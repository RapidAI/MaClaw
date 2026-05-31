package httpapi

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// spyPresenceChecker is a deterministic stand-in for the Hub device service's
// presence source. It records the machine IDs it was asked about and reports
// online/offline from a fixed map, so a test can assert the availability
// checker mirrors real presence (the post-fix behavior the noop checker — which
// always reported AVAILABLE — could never provide).
type spyPresenceChecker struct {
	online  map[string]bool
	queried []string
}

func (s *spyPresenceChecker) IsMachineOnline(machineID string) bool {
	s.queried = append(s.queried, machineID)
	return s.online[machineID]
}

// Compile-time assertion the spy satisfies the dependency the checker needs.
var _ machinePresenceChecker = (*spyPresenceChecker)(nil)

func TestHubAvailabilityChecker_MirrorsRealPresence(t *testing.T) {
	presence := &spyPresenceChecker{online: map[string]bool{
		"ve-online":  true,
		"ve-offline": false,
	}}
	c := NewHubAvailabilityChecker(presence)

	// An online approver mirrors as available.
	if !c.IsAvailable(context.Background(), "ve-online") {
		t.Fatal("expected online approver to report AVAILABLE")
	}
	// An offline approver mirrors as unavailable (the bug-condition case 2.4:
	// noopAvailabilityChecker hardcoded true so escalation/queueing never fired).
	if c.IsAvailable(context.Background(), "ve-offline") {
		t.Fatal("expected offline approver to report UNAVAILABLE")
	}

	// The checker must consult the real presence source, not guess.
	if len(presence.queried) != 2 {
		t.Fatalf("expected 2 presence queries, got %d (%v)", len(presence.queried), presence.queried)
	}
}

func TestHubAvailabilityChecker_UnknownApproverUnavailable(t *testing.T) {
	presence := &spyPresenceChecker{online: map[string]bool{}}
	c := NewHubAvailabilityChecker(presence)

	// A machine the presence source has never seen is treated as offline.
	if c.IsAvailable(context.Background(), "ve-never-seen") {
		t.Fatal("expected unknown approver to report UNAVAILABLE")
	}
}

func TestHubAvailabilityChecker_EmptyApproverUnavailable(t *testing.T) {
	presence := &spyPresenceChecker{online: map[string]bool{"": true}}
	c := NewHubAvailabilityChecker(presence)

	// An empty/whitespace approver id is rejected before consulting presence,
	// so a malformed request cannot be reported as reachable.
	if c.IsAvailable(context.Background(), "   ") {
		t.Fatal("expected empty approver id to report UNAVAILABLE")
	}
	if len(presence.queried) != 0 {
		t.Fatalf("expected no presence query for empty approver, got %v", presence.queried)
	}
}

func TestHubAvailabilityChecker_NilPresenceUnavailable(t *testing.T) {
	c := NewHubAvailabilityChecker(nil)

	// With no presence source wired, the checker reports unavailable so
	// escalation/queueing fires rather than silently assuming reachability.
	if c.IsAvailable(context.Background(), "ve-1") {
		t.Fatal("expected unavailable when no presence source is configured")
	}
}

// TestHubAvailabilityChecker_SatisfiesInterface confirms the real checker is a
// drop-in for the unchanged HumanApproverChecker the EscalationManager depends
// on (Preservation 3.6 — only the availability source changes).
func TestHubAvailabilityChecker_SatisfiesInterface(t *testing.T) {
	var _ workflow.HumanApproverChecker = NewHubAvailabilityChecker(&spyPresenceChecker{online: map[string]bool{}})
}
