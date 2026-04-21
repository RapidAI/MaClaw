package tool

import (
	"testing"
)

// TestBugCondition_ScreenshotKeywords_ConditionalKeepRules tests that screenshot messages
// do NOT activate craft_tool via the document delivery conditional keep rule.
//
// **Validates: Requirements 1.2, 1.3**
//
// Bug Condition: matchConditionalKeepRules activates craft_tool for screenshot messages because:
// - "发给我" matches documentDeliveryKeywords
// - The rule has no exclusion for screenshot keywords ("截屏"/"截图")
// - craft_tool is in the keepTools list for document delivery
//
// These tests are EXPECTED TO FAIL on unfixed code — failure confirms the bug exists.

func TestBugCondition_Jieping_FaGeiWo_NoCraftTool(t *testing.T) {
	// Property 1b: "帮我截屏桌面发给我图片" should NOT have craft_tool in keep set.
	// Bug: "发给我" matches documentDeliveryKeywords → craft_tool activated
	// After fix: craft_tool is excluded but send_file/open remain available.
	keep, _, _ := matchConditionalKeepRules("帮我截屏桌面发给我图片")

	if keep["craft_tool"] {
		t.Errorf("BUG CONFIRMED: matchConditionalKeepRules(%q) has craft_tool in keep set. "+
			"Root cause: '发给我' matches documentDeliveryKeywords, no screenshot exclusion. "+
			"Keep set: %v",
			"帮我截屏桌面发给我图片", keep)
	}
	// send_file should still be activated — user wants to send the screenshot.
	if !keep["send_file"] {
		t.Errorf("matchConditionalKeepRules(%q) missing send_file in keep set. "+
			"Screenshot + '发给我' should still activate send_file for sending the image. "+
			"Keep set: %v",
			"帮我截屏桌面发给我图片", keep)
	}
}

func TestBugCondition_Jitu_FaGeiWo_NoCraftTool(t *testing.T) {
	// Property 1b: "截图发给我" should NOT have craft_tool in keep set.
	// Bug: "发给我" matches documentDeliveryKeywords → craft_tool activated
	keep, _, _ := matchConditionalKeepRules("截图发给我")

	if keep["craft_tool"] {
		t.Errorf("BUG CONFIRMED: matchConditionalKeepRules(%q) has craft_tool in keep set. "+
			"Root cause: '发给我' matches documentDeliveryKeywords, no screenshot exclusion. "+
			"Keep set: %v",
			"截图发给我", keep)
	}
}
