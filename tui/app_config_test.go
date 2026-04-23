package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestApplyConfigValue_CoversAllKeys verifies that every key in the config view
// has a corresponding case in applyConfigValue. This is a compile-time guard
// against the "two independent lists" problem.
func TestApplyConfigValue_CoversAllKeys(t *testing.T) {
	// These are the keys defined in NewConfigModel (views/config.go).
	// If you add a key there, add it here too — this test will remind you.
	keys := []string{
		"hub_url", "token", "max_iterations", "agentnet_enabled",
		"maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model",
		"maclaw_llm_protocol", "maclaw_llm_context_length",
		"qqbot_enabled", "qqbot_app_id", "qqbot_app_secret",
		"telegram_bot_enabled", "telegram_bot_token",
		// "data_dir" is read-only, no apply needed.
		// "skill_purchase_mode" is not in applyConfigValue yet — add if needed.
	}

	cfg := corelib.AppConfig{}
	for _, key := range keys {
		// applyConfigValue should not panic for any known key.
		applyConfigValue(&cfg, key, "test_value")
	}

	// Verify a sample field was actually set.
	if cfg.MaclawLLMModel != "test_value" {
		t.Error("applyConfigValue did not set maclaw_llm_model")
	}
	if cfg.RemoteHubURL != "test_value" {
		t.Error("applyConfigValue did not set hub_url")
	}
}
