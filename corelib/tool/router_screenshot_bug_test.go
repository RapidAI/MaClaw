package tool

import "testing"

func TestConditionalKeepRules_ScreenshotNoLocalCraftOrSend(t *testing.T) {
	keep, _, _ := matchConditionalKeepRules("take a screenshot and send it to me")

	for _, tool := range []string{"craft_tool", "send_file", "open"} {
		if keep[tool] {
			t.Errorf("matchConditionalKeepRules must not activate %q from local screenshot wording; keep=%v", tool, keep)
		}
	}
}
