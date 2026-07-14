package tool

import "testing"

func TestConditionalKeepRules_DocumentDeliveryNoLocalActivation(t *testing.T) {
	keep, _, _ := matchConditionalKeepRules("send this report to me")

	for _, tool := range []string{"craft_tool", "send_file", "send_to_im", "open"} {
		if keep[tool] {
			t.Errorf("matchConditionalKeepRules must not activate %q from local wording; keep=%v", tool, keep)
		}
	}
}

func TestConditionalKeepRules_SSHNoLocalActivation(t *testing.T) {
	keep, _, _ := matchConditionalKeepRules("login to the server")

	if keep["ssh"] {
		t.Errorf("matchConditionalKeepRules must not activate ssh from local wording; keep=%v", keep)
	}
}
