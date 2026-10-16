package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestApplyHubBotLLMServiceGroupLeavesViewerSelection(t *testing.T) {
	cfg := applyHubBotLLMServiceGroup(corelib.MaclawLLMConfig{Model: "auto", ServiceGroupID: "system-free"}, map[string]string{
		"hub_bot": "1", "llm_service_group_id": "maclaw_official_group",
	})
	if cfg.ServiceGroupID != "" {
		t.Fatalf("service group = %q, want the GUI viewer path", cfg.ServiceGroupID)
	}
	legacy := applyHubBotLLMServiceGroup(corelib.MaclawLLMConfig{Model: "auto", ServiceGroupID: "maclaw_official_group"}, map[string]string{
		"hub_bot": "1", "llm_service_group_id": "system-free",
	})
	if legacy.ServiceGroupID != "" {
		t.Fatalf("legacy service group = %q", legacy.ServiceGroupID)
	}
	plain := applyHubBotLLMServiceGroup(corelib.MaclawLLMConfig{ServiceGroupID: "keep"}, map[string]string{"hub_bot": "0"})
	if plain.ServiceGroupID != "keep" {
		t.Fatalf("non-bot service group = %q", plain.ServiceGroupID)
	}
	ctx := contextWithHubBotLLMGroup(context.Background(), map[string]string{"hub_bot": "1"})
	if !HubBotLLMGroup(ctx) {
		t.Fatal("hub bot context was not marked")
	}
	if HubBotLLMGroup(contextWithHubBotLLMGroup(context.Background(), map[string]string{"hub_bot": "0"})) {
		t.Fatal("non-bot context was marked")
	}
}
