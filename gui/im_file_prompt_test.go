package main

import (
	"strings"
	"testing"
)

func TestFileDeliveryPromptRequiresTargetResolution(t *testing.T) {
	for _, platform := range []string{"desktop", "weixin"} {
		var b strings.Builder
		appendFileDeliveryChannelRules(&b, platform)
		text := b.String()
		for _, want := range []string{"im_message", "list_targets", "send_to_im", "group_id", "user_id"} {
			if !strings.Contains(text, want) {
				t.Fatalf("platform=%s prompt missing %q:\n%s", platform, want, text)
			}
		}
	}
}
