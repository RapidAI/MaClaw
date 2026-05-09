package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/views"
)

// TestApplyConfigValue_CoversAllKeys verifies that every key in the config view
// is accepted by the app-level save wrapper.
func TestApplyConfigValue_CoversAllKeys(t *testing.T) {
	cfg := corelib.AppConfig{}
	for _, key := range views.ConfigFieldKeys() {
		// applyConfigValue should not panic for any known key.
		applyConfigValue(&cfg, key, "test_value")
	}

	// Verify a sample field was actually set.
	if cfg.MaclawLLMModel != "test_value" {
		t.Error("applyConfigValue did not set maclaw_llm_model")
	}
	if cfg.RemoteHubCenterURL != "test_value" {
		t.Error("applyConfigValue did not set hubcenter_url")
	}
}
