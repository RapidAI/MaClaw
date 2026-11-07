package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestHandoffRecoveryMarkerMatchesCodingMarker(t *testing.T) {
	if codingSubAgentRecoveryMarker != agent.HandoffRecoveryMarker {
		t.Fatalf("coding marker %q != agent marker %q", codingSubAgentRecoveryMarker, agent.HandoffRecoveryMarker)
	}
}
