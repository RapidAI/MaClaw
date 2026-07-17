package clientsecurity

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestEnforceConfigBlocksIMMessageSendWhenOutboundDisabled(t *testing.T) {
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		FileOutboundEnabled:    false,
		ImageOutboundEnabled:   true,
		NetworkLevel:           "full",
	}
	if ok, reason := EnforceConfig(cfg, "im_message", map[string]interface{}{"action": "send", "text": "hi"}); ok || !strings.Contains(reason, "outbound") {
		t.Fatalf("send allowed=%v reason=%q", ok, reason)
	}
	if ok, reason := EnforceConfig(cfg, "im_message", map[string]interface{}{"action": "list_targets"}); !ok {
		t.Fatalf("list_targets blocked reason=%q", reason)
	}
}

func TestEnforceConfigAllowsIMMessageSendWhenOutboundEnabled(t *testing.T) {
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
		NetworkLevel:           "full",
	}
	if ok, reason := EnforceConfig(cfg, "im_message", map[string]interface{}{"action": "send", "text": "hi"}); !ok {
		t.Fatalf("send blocked reason=%q", reason)
	}
}

func TestIsIMMessageSendIntentInferredWithoutAction(t *testing.T) {
	cfg := corelib.AppConfig{
		HubSecurityCentralized: true,
		FileOutboundEnabled:    false,
		ImageOutboundEnabled:   true,
		NetworkLevel:           "full",
	}
	// Omitting action but providing text must still hit outbound gate.
	if ok, reason := EnforceConfig(cfg, "im_message", map[string]interface{}{"text": "hi", "group_name": "g"}); ok || !strings.Contains(reason, "outbound") {
		t.Fatalf("inferred send allowed=%v reason=%q", ok, reason)
	}
}
