package tool

import (
	"testing"
)

// Preservation property tests for conditionalKeepRules (Property 2b).
// These tests capture baseline behavior that MUST be preserved after the fix.
//
// **Validates: Requirements 3.1, 3.5**
//
// Observation-first methodology: behavior observed on UNFIXED code, then asserted.

func TestPreservation_ConditionalKeep_DocDelivery_CraftTool(t *testing.T) {
	// "把报告发给我" → craft_tool, send_file, open in keep (preserved)
	// "发给我" matches documentDeliveryKeywords → activates craft_tool, send_file, open.
	keep, _, _ := matchConditionalKeepRules("把报告发给我")

	expectedTools := []string{"craft_tool", "send_file", "open"}
	for _, tool := range expectedTools {
		if !keep[tool] {
			t.Errorf("PRESERVATION FAILED: matchConditionalKeepRules(%q) missing %q in keep set. "+
				"Pure document delivery messages must continue to activate document delivery tools. "+
				"Keep set: %v",
				"把报告发给我", tool, keep)
		}
	}
}

func TestPreservation_ConditionalKeep_SSH(t *testing.T) {
	// "登录服务器" → ssh in keep (preserved)
	// "服务器" matches SSH keywords → activates ssh.
	keep, _, _ := matchConditionalKeepRules("登录服务器")

	if !keep["ssh"] {
		t.Errorf("PRESERVATION FAILED: matchConditionalKeepRules(%q) missing 'ssh' in keep set. "+
			"SSH messages must continue to activate ssh tool. "+
			"Keep set: %v",
			"登录服务器", keep)
	}
}
