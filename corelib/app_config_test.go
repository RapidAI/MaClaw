package corelib

import (
	"encoding/json"
	"testing"
)

// TestAppConfig_UnmarshalIgnoresUnknownExtraToolKeys verifies that when a
// config JSON contains an "extra_tool_configs" map with OEM-specific keys
// (e.g. "tigerclaw") but the current brand is the default brand, loading the
// config does NOT produce an error. Go's encoding/json silently ignores
// unknown top-level fields and correctly deserialises map entries regardless
// of the current brand.
//
// Validates: Requirements 9.4, 9.2, 9.3
func TestAppConfig_UnmarshalIgnoresUnknownExtraToolKeys(t *testing.T) {
	raw := `{
		"claude": {"current_model": "sonnet"},
		"extra_tool_configs": {
			"tigerclaw": {
				"current_model": "tc-v1",
				"models": [{"name": "tc-v1", "command": "tigerclaw"}]
			},
			"some_future_oem_tool": {
				"current_model": "future-1"
			}
		}
	}`

	var cfg AppConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("expected no error unmarshalling config with OEM extra_tool_configs, got: %v", err)
	}

	// The extra_tool_configs map should be populated with the keys from JSON.
	if cfg.ExtraToolConfigs == nil {
		t.Fatal("ExtraToolConfigs should not be nil after unmarshal")
	}
	if _, ok := cfg.ExtraToolConfigs["tigerclaw"]; !ok {
		t.Error("ExtraToolConfigs should contain 'tigerclaw' key")
	}
	if tc := cfg.ExtraToolConfigs["tigerclaw"]; tc.CurrentModel != "tc-v1" {
		t.Errorf("tigerclaw CurrentModel = %q, want %q", tc.CurrentModel, "tc-v1")
	}
	if _, ok := cfg.ExtraToolConfigs["some_future_oem_tool"]; !ok {
		t.Error("ExtraToolConfigs should contain 'some_future_oem_tool' key")
	}

	// Verify that standard fields still deserialise correctly alongside extra configs.
	if cfg.Claude.CurrentModel != "sonnet" {
		t.Errorf("Claude.CurrentModel = %q, want %q", cfg.Claude.CurrentModel, "sonnet")
	}
}

// TestAppConfig_UnmarshalIgnoresCompletelyUnknownTopLevelKeys verifies that
// truly unknown top-level JSON keys (not mapped to any struct field) are
// silently ignored by Go's json.Unmarshal.
//
// Validates: Requirements 9.4
func TestAppConfig_UnmarshalIgnoresCompletelyUnknownTopLevelKeys(t *testing.T) {
	raw := `{
		"claude": {"current_model": "sonnet"},
		"totally_unknown_field": "should be ignored",
		"another_unknown": 42
	}`

	var cfg AppConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("expected no error with unknown top-level keys, got: %v", err)
	}

	if cfg.Claude.CurrentModel != "sonnet" {
		t.Errorf("Claude.CurrentModel = %q, want %q", cfg.Claude.CurrentModel, "sonnet")
	}
}

// TestIsWorkflowEnabled verifies the three-state behavior of the workflow toggle:
// nil (default) → true, explicit true → true, explicit false → false.
// Also verifies JSON round-trip: *bool with omitempty serializes false correctly.
func TestIsWorkflowEnabled(t *testing.T) {
	// nil → default true
	var cfg AppConfig
	if !cfg.IsWorkflowEnabled() {
		t.Error("nil WorkflowEnabled should default to true")
	}

	// explicit true
	cfg.SetWorkflowEnabled(true)
	if !cfg.IsWorkflowEnabled() {
		t.Error("explicit true should return true")
	}

	// explicit false
	cfg.SetWorkflowEnabled(false)
	if cfg.IsWorkflowEnabled() {
		t.Error("explicit false should return false")
	}

	// JSON round-trip: false must survive marshal → unmarshal
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var cfg2 AppConfig
	if err := json.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg2.IsWorkflowEnabled() {
		t.Error("workflow_enabled=false should survive JSON round-trip")
	}

	// JSON round-trip: absent field → nil → default true
	var cfg3 AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg3); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if !cfg3.IsWorkflowEnabled() {
		t.Error("absent workflow_enabled should default to true")
	}
}

func TestAppConfig_GroupDiscussionDefaultsWhenAbsent(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal empty config: %v", err)
	}

	gd := cfg.GroupDiscussion
	if !gd.Enabled {
		t.Error("absent group_discussion should default enabled")
	}
	if !gd.Discoverable {
		t.Error("absent group_discussion should default discoverable")
	}
	if !gd.SuggestConsultation {
		t.Error("absent group_discussion should default suggest_consultation")
	}
	if !gd.ConfirmBeforeStart {
		t.Error("absent group_discussion should require confirmation before starting")
	}
	if !gd.RejectWhenDND {
		t.Error("absent group_discussion should reject invites while DND by default")
	}
	if gd.AllowSecurityGroupFreeDiscussion {
		t.Error("absent group_discussion should not allow same-security-group free discussion by default")
	}
	if !gd.CrossAgentExperienceEnabled() {
		t.Error("absent group_discussion should allow cross-agent experience by default for compatibility")
	}
	if gd.InvitePolicy != "ask_always" {
		t.Errorf("InvitePolicy = %q, want ask_always", gd.InvitePolicy)
	}
	if gd.ContextPolicy != "summary_only" {
		t.Errorf("ContextPolicy = %q, want summary_only", gd.ContextPolicy)
	}
	if gd.MaxRounds != 3 {
		t.Errorf("MaxRounds = %d, want 3", gd.MaxRounds)
	}
	if gd.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds = %d, want 300", gd.TimeoutSeconds)
	}
	if gd.ConcurrentLimit != 1 {
		t.Errorf("ConcurrentLimit = %d, want 1", gd.ConcurrentLimit)
	}
}

func TestAppConfig_GroupDiscussionExplicitFalseSurvivesUnmarshal(t *testing.T) {
	raw := `{
		"group_discussion": {
			"enabled": false,
			"discoverable": false,
			"suggest_consultation": false,
			"confirm_before_start": false,
			"reject_when_dnd": false,
			"allow_security_group_free_discussion": true,
			"use_cross_agent_experience": false
		}
	}`

	var cfg AppConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal explicit group_discussion config: %v", err)
	}

	gd := cfg.GroupDiscussion
	if gd.Enabled || gd.Discoverable || gd.SuggestConsultation || gd.ConfirmBeforeStart || gd.RejectWhenDND {
		t.Fatalf("explicit false group_discussion booleans should be preserved: %+v", gd)
	}
	if !gd.AllowSecurityGroupFreeDiscussion {
		t.Fatal("allow_security_group_free_discussion=true should be preserved")
	}
	if gd.CrossAgentExperienceEnabled() {
		t.Fatal("use_cross_agent_experience=false should be preserved")
	}
	if gd.InvitePolicy != "ask_always" {
		t.Errorf("InvitePolicy = %q, want ask_always", gd.InvitePolicy)
	}
	if gd.ContextPolicy != "summary_only" {
		t.Errorf("ContextPolicy = %q, want summary_only", gd.ContextPolicy)
	}
	if gd.Availability != "available" {
		t.Errorf("Availability = %q, want available", gd.Availability)
	}
}
