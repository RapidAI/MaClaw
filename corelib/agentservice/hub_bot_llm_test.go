package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestApplyHubBotLLMServiceGroupPinsSystemFree(t *testing.T) {
	cfg := applyHubBotLLMServiceGroup(corelib.MaclawLLMConfig{Model: "auto"}, map[string]string{"hub_bot": "1"})
	if cfg.ServiceGroupID != hubBotLLMServiceGroupID {
		t.Fatalf("service group = %q", cfg.ServiceGroupID)
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
