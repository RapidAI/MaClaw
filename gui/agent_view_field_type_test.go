package main

import "testing"

func TestNormalizeAgentViewFieldTypeRecognizesDateTime(t *testing.T) {
	if got := normalizeAgentViewFieldType("datetime"); got != agentViewFieldTypeDateTime {
		t.Fatalf("normalizeAgentViewFieldType(datetime) = %q, want %q", got, agentViewFieldTypeDateTime)
	}
	if got := normalizeMISDatasetAgentViewFieldType("timestamp"); got != agentViewFieldTypeDateTime {
		t.Fatalf("normalizeMISDatasetAgentViewFieldType(timestamp) = %q, want %q", got, agentViewFieldTypeDateTime)
	}
}
