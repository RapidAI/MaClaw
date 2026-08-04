package corelib

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"pgregory.net/rapid"
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

func TestLansengerGroupFileLimitDefaultsUnlimitedAndRoundTrips(t *testing.T) {
	var cfg AppConfig
	if got := cfg.LansengerGroupFileLimit("group-1"); got != 0 {
		t.Fatalf("default limit = %d, want unlimited", got)
	}
	if !cfg.SetLansengerGroupFileLimit("group-1", 8<<20) {
		t.Fatal("setting limit reported no change")
	}
	if got := cfg.LansengerGroupFileLimit("group-1"); got != 8<<20 {
		t.Fatalf("limit = %d", got)
	}
	if !cfg.SetLansengerGroupFileLimit("group-1", 0) || cfg.LansengerGroupFileMaxBytes != nil {
		t.Fatalf("unlimited did not remove map entry: %#v", cfg.LansengerGroupFileMaxBytes)
	}
}

func TestAppConfigMarshalKeepsUserMemoryZeroValues(t *testing.T) {
	data, err := json.Marshal(AppConfig{})
	if err != nil {
		t.Fatalf("Marshal AppConfig: %v", err)
	}
	text := string(data)
	for _, want := range []string{`"memory_auto_compress":false`, `"memory_max_backups":0`, `"knowledge_skill_token_budget":0`} {
		if !strings.Contains(text, want) {
			t.Fatalf("default AppConfig JSON missing %s: %s", want, text)
		}
	}
}

func TestAppConfigConfiguredHubCenterBaseURLFiltersLoopback(t *testing.T) {
	// Loopback preferred is local/dev — do not promote leftover public list entries.
	cfg := AppConfig{
		RemoteHubCenterURL: "http://127.0.0.1:65140",
		RemoteHubCenterURLs: []string{
			"http://localhost:9388",
			"https://hubs.example.com/",
		},
	}
	if got := cfg.ConfiguredHubCenterBaseURL(); got != "" {
		t.Fatalf("ConfiguredHubCenterBaseURL() = %q, want empty for loopback preferred", got)
	}
	if got := cfg.SkillMarketBaseURL("https://default.example.com"); got != "" {
		t.Fatalf("SkillMarketBaseURL() = %q, want empty for loopback preferred", got)
	}

	// Public preferred is the enrollment identity.
	cfg2 := AppConfig{
		RemoteHubCenterURL:  "https://hubs.example.com/",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:65140", "https://hubs.example.com/"},
	}
	if got, want := cfg2.ConfiguredHubCenterBaseURL(), "https://hubs.example.com"; got != want {
		t.Fatalf("ConfiguredHubCenterBaseURL(public) = %q, want %q", got, want)
	}
}

func TestAppConfigSkillMarketBaseURLDoesNotReturnDefaultAsConfirmed(t *testing.T) {
	cfg := AppConfig{}

	if got := cfg.SkillMarketBaseURL("https://default.example.com"); got != "" {
		t.Fatalf("SkillMarketBaseURL() = %q, want empty without confirmed config", got)
	}
}

func TestAppConfigHubCenterBaseURLsKeepsDefaultsAsCandidates(t *testing.T) {
	cfg := AppConfig{RemoteHubCenterURL: "http://127.0.0.1:65140"}

	got := cfg.HubCenterBaseURLs("http://127.0.0.1:9999", []string{"https://default.example.com"})
	// Unregistered (loopback only): loopback config + official defaults.
	want := []string{"http://127.0.0.1:65140", "http://127.0.0.1:9999", "https://default.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HubCenterBaseURLs() = %#v, want %#v", got, want)
	}
}

func TestAppConfigHubCenterBaseURLsDoesNotMergeDefaultsWhenRegistered(t *testing.T) {
	cfg := AppConfig{
		RemoteHubCenterURL:  "https://hubs.maclaw.top",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:9", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"},
	}
	got := cfg.HubCenterBaseURLs("https://hubs.mypapers.top", []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	})
	// Non-preferred official default hubs2 stripped from polluted config.
	want := []string{"https://hubs.maclaw.top"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HubCenterBaseURLs(registered) = %#v, want %#v", got, want)
	}
	if got := cfg.ConfiguredHubCenterBaseURL(); got != "https://hubs.maclaw.top" {
		t.Fatalf("ConfiguredHubCenterBaseURL = %q", got)
	}
}

func TestAppConfigHubCenterBaseURLsNormalizesBareHost(t *testing.T) {
	cfg := AppConfig{RemoteHubCenterURL: "hubs.maclaw.top"}
	got := cfg.HubCenterBaseURLs("", nil)
	want := []string{"https://hubs.maclaw.top"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HubCenterBaseURLs(bare host) = %#v, want %#v", got, want)
	}
	if got := cfg.ConfiguredHubCenterBaseURL(); got != "https://hubs.maclaw.top" {
		t.Fatalf("ConfiguredHubCenterBaseURL(bare host) = %q", got)
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

func TestAppConfigHardwareDefaultsSurviveLegacyUnmarshal(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{"thirdparty_gateway_enabled":true}`), &cfg); err != nil {
		t.Fatalf("unmarshal legacy config: %v", err)
	}
	if cfg.HardwareWelcomeText != "Hello, Maclaw" || cfg.HardwareVolume != 70 {
		t.Fatalf("hardware defaults=%#v, want welcome text and 70%% volume", cfg)
	}

	if err := json.Unmarshal([]byte(`{"hardware_volume":0}`), &cfg); err != nil {
		t.Fatalf("unmarshal muted config: %v", err)
	}
	if cfg.HardwareVolume != 0 {
		t.Fatalf("explicit mute was overwritten: %d", cfg.HardwareVolume)
	}
}

func TestAppConfigSubAgentConcurrencyDefaultsAndClamps(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "missing", raw: `{}`, want: DefaultSubAgentConcurrency},
		{name: "zero", raw: `{"subagent_concurrency":0}`, want: DefaultSubAgentConcurrency},
		{name: "negative", raw: `{"subagent_concurrency":-3}`, want: DefaultSubAgentConcurrency},
		{name: "one", raw: `{"subagent_concurrency":1}`, want: 1},
		{name: "max", raw: `{"subagent_concurrency":10}`, want: 10},
		{name: "over max", raw: `{"subagent_concurrency":15}`, want: MaxSubAgentConcurrency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg AppConfig
			if err := json.Unmarshal([]byte(tt.raw), &cfg); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if cfg.SubAgentConcurrency != tt.want {
				t.Fatalf("SubAgentConcurrency = %d, want %d", cfg.SubAgentConcurrency, tt.want)
			}
		})
	}
}

func TestAppConfigProjectsDefaultToValidArray(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing", raw: `{}`},
		{name: "null", raw: `{"projects":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg AppConfig
			if err := json.Unmarshal([]byte(tt.raw), &cfg); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if cfg.Projects == nil || len(cfg.Projects) != 1 {
				t.Fatalf("Projects = %#v, want one default project", cfg.Projects)
			}
			if cfg.Projects[0].Id != "default" || cfg.CurrentProject != "default" {
				t.Fatalf("default project mismatch: current=%q projects=%#v", cfg.CurrentProject, cfg.Projects)
			}
		})
	}
}

func TestAppConfigSecurityBoolDefaults(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !cfg.YoloModeAllowed || !cfg.SmartRouteEnabled || !cfg.GossipEnabled || !cfg.FileOutboundEnabled || !cfg.ImageOutboundEnabled {
		t.Fatalf("security booleans should default true: %+v", cfg)
	}

	var disabled AppConfig
	if err := json.Unmarshal([]byte(`{"yolo_mode_allowed":false,"smart_route_enabled":false,"gossip_enabled":false,"file_outbound_enabled":false,"image_outbound_enabled":false}`), &disabled); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if disabled.YoloModeAllowed || disabled.SmartRouteEnabled || disabled.GossipEnabled || disabled.FileOutboundEnabled || disabled.ImageOutboundEnabled {
		t.Fatalf("explicit false security booleans should be preserved: %+v", disabled)
	}
}

// TestAppConfigDefaultTrueBoolRoundTrip is a mechanical guarantee that every
// bool field with a default-true semantic correctly survives a JSON round-trip
// for both true and false values. It uses reflection to auto-discover all
// default-true fields from AppConfigDefaults() — no manual list to maintain.
func TestAppConfigDefaultTrueBoolRoundTrip(t *testing.T) {
	// Auto-discover all bool fields that are true in AppConfigDefaults().
	defaults := AppConfigDefaults()
	rv := reflect.ValueOf(defaults)
	rt := rv.Type()

	var defaultTrueBools []string
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Type.Kind() != reflect.Bool {
			continue
		}
		if !rv.Field(i).Bool() {
			continue
		}
		tag := field.Tag.Get("json")
		jsonKey := strings.Split(tag, ",")[0]
		if jsonKey == "" || jsonKey == "-" {
			continue
		}
		defaultTrueBools = append(defaultTrueBools, jsonKey)
	}
	if len(defaultTrueBools) == 0 {
		t.Fatal("no default-true bool fields found — AppConfigDefaults() is broken or struct changed")
	}

	for _, field := range defaultTrueBools {
		t.Run(field+"=true_roundtrip", func(t *testing.T) {
			jsonData := `{"` + field + `":true}`
			var cfg AppConfig
			if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
				t.Fatalf("Unmarshal(%s) error: %v", jsonData, err)
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			var cfg2 AppConfig
			if err := json.Unmarshal(data, &cfg2); err != nil {
				t.Fatalf("Unmarshal(round-trip) error: %v", err)
			}
			data2, _ := json.Marshal(cfg2)
			if !strings.Contains(string(data2), `"`+field+`":true`) {
				t.Errorf("field %q lost its true value after round-trip.\nFirst marshal: %s\nSecond marshal: %s", field, string(data), string(data2))
			}
		})

		t.Run(field+"=false_roundtrip", func(t *testing.T) {
			jsonData := `{"` + field + `":false}`
			var cfg AppConfig
			if err := json.Unmarshal([]byte(jsonData), &cfg); err != nil {
				t.Fatalf("Unmarshal(%s) error: %v", jsonData, err)
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			var cfg2 AppConfig
			if err := json.Unmarshal(data, &cfg2); err != nil {
				t.Fatalf("Unmarshal(round-trip) error: %v", err)
			}
			data2, _ := json.Marshal(cfg2)
			if strings.Contains(string(data2), `"`+field+`":true`) {
				t.Errorf("field %q flipped from false to true after round-trip.\nFirst marshal: %s\nSecond marshal: %s", field, string(data), string(data2))
			}
		})

		t.Run(field+"=absent_defaults_true", func(t *testing.T) {
			var cfg AppConfig
			if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
				t.Fatalf("Unmarshal({}) error: %v", err)
			}
			data, _ := json.Marshal(cfg)
			if !strings.Contains(string(data), `"`+field+`":true`) {
				t.Errorf("field %q should default to true when absent, got marshal: %s", field, string(data))
			}
		})
	}
}

func TestAppConfigGossipAutoPublishDefault(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !cfg.GossipAutoPublish {
		t.Fatal("missing gossip_auto_publish should default true")
	}

	var disabled AppConfig
	if err := json.Unmarshal([]byte(`{"gossip_auto_publish":false}`), &disabled); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if disabled.GossipAutoPublish {
		t.Fatal("explicit false gossip_auto_publish should be preserved")
	}
}

func TestNormalizeAgentTimeoutSec(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "missing", in: 0, want: DefaultAgentTimeoutSec},
		{name: "below min", in: 120, want: MinAgentTimeoutSec},
		{name: "valid", in: 360, want: 360},
		{name: "above max", in: 1200, want: MaxAgentTimeoutSec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeAgentTimeoutSec(tt.in); got != tt.want {
				t.Fatalf("NormalizeAgentTimeoutSec(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeSkillRunnerTimeoutSec(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "missing", in: 0, want: DefaultSkillRunnerTimeoutSec},
		{name: "below min", in: 120, want: MinSkillRunnerTimeoutSec},
		{name: "long document", in: 3600, want: 3600},
		{name: "above max", in: 20000, want: MaxSkillRunnerTimeoutSec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSkillRunnerTimeoutSec(tt.in); got != tt.want {
				t.Fatalf("NormalizeSkillRunnerTimeoutSec(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestIsWorkflowEnabled verifies the three-state behavior of the workflow toggle:
// nil (default) → false, explicit true → true, explicit false → false.
// Also verifies JSON round-trip: *bool with omitempty serializes false correctly.
func TestIsWorkflowEnabled(t *testing.T) {
	// nil → default false
	var cfg AppConfig
	if cfg.IsWorkflowEnabled() {
		t.Error("nil WorkflowEnabled should default to false")
	}

	// AppConfigDefaults → explicit default false
	defaults := AppConfigDefaults()
	if defaults.WorkflowEnabled == nil {
		t.Fatal("AppConfigDefaults should set WorkflowEnabled explicitly")
	}
	if defaults.IsWorkflowEnabled() {
		t.Error("AppConfigDefaults WorkflowEnabled should default to false")
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

	// JSON round-trip: absent field → AppConfigDefaults → default false
	var cfg3 AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg3); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if cfg3.WorkflowEnabled == nil {
		t.Fatal("absent workflow_enabled should be filled from AppConfigDefaults")
	}
	if cfg3.IsWorkflowEnabled() {
		t.Error("absent workflow_enabled should default to false")
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
	if gd.AuthRequestSoundPreset != "classic" {
		t.Errorf("AuthRequestSoundPreset = %q, want classic", gd.AuthRequestSoundPreset)
	}
	if gd.AuthRequestSoundMuted {
		t.Error("absent group_discussion should leave auth request sound enabled")
	}
}

func TestAppConfig_GroupDiscussionSensitiveQueryPolicyNormalizesCaseAndSpaces(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{"group_discussion":{"sensitive_query_policy":" ALLOW "}}`), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.GroupDiscussion.SensitiveQueryPolicy != "allow" {
		t.Fatalf("SensitiveQueryPolicy = %q, want allow", cfg.GroupDiscussion.SensitiveQueryPolicy)
	}
}

func TestAppConfig_GroupDiscussionAuthRequestSoundPresetNormalizesCaseAndSpaces(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{"group_discussion":{"auth_request_sound_preset":" URGENT "}}`), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.GroupDiscussion.AuthRequestSoundPreset != "urgent" {
		t.Fatalf("AuthRequestSoundPreset = %q, want urgent", cfg.GroupDiscussion.AuthRequestSoundPreset)
	}
}

func TestAppConfig_GroupDiscussionAuthRequestSoundPresetDefaultsInvalidValue(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{"group_discussion":{"auth_request_sound_preset":"unknown"}}`), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg.GroupDiscussion.AuthRequestSoundPreset != "classic" {
		t.Fatalf("AuthRequestSoundPreset = %q, want classic", cfg.GroupDiscussion.AuthRequestSoundPreset)
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

func TestCapabilityMarketPolicyDefaults(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("unmarshal empty config: %v", err)
	}

	policy := cfg.CapabilityMarketPolicy
	if policy.EffectiveEnterpriseOnlyInstall() {
		t.Fatal("enterprise_only_install should default to false")
	}
	if policy.EffectiveEnterpriseOnlySearch() {
		t.Fatal("enterprise_only_search should default to false")
	}
	if policy.ViewMode != "merged" {
		t.Fatalf("ViewMode = %q, want merged", policy.ViewMode)
	}
	if policy.EffectivePreferredUploadTarget() != CapabilitySourceHubCenter {
		t.Fatalf("preferred upload target = %q, want hubcenter", policy.EffectivePreferredUploadTarget())
	}
	if got := policy.UploadTargets(true); strings.Join(got, ",") != "hubcenter,enterprise_hub" {
		t.Fatalf("default upload targets = %#v", got)
	}
	if policy.ManagedDeployment.RetryIntervalMinutes != 60 {
		t.Fatalf("RetryIntervalMinutes = %d, want 60", policy.ManagedDeployment.RetryIntervalMinutes)
	}
	if policy.UpdatePolicy.EnterpriseHub.Default != "auto_update_approved" {
		t.Fatalf("enterprise hub update default = %q", policy.UpdatePolicy.EnterpriseHub.Default)
	}
	if policy.UpdatePolicy.HubCenter.FreeCapability != "auto_update" {
		t.Fatalf("hubcenter free update = %q", policy.UpdatePolicy.HubCenter.FreeCapability)
	}
	if policy.UpdatePolicy.HubCenter.PaidCapability != "require_license_and_purchase_policy" {
		t.Fatalf("hubcenter paid update = %q", policy.UpdatePolicy.HubCenter.PaidCapability)
	}
	if got := policy.SourcePriority["enterprise_hub"]; got != 100 {
		t.Fatalf("enterprise_hub priority = %d, want 100", got)
	}
	if got := policy.ResourceTypes["mcp"].DefaultSources; len(got) != 1 || got[0] != "enterprise_hub" {
		t.Fatalf("mcp default sources = %#v", got)
	}
}

func TestCapabilityMarketPolicyPreferredUploadTarget(t *testing.T) {
	policy := CapabilityMarketPolicy{PreferredUploadTarget: "enterprise"}.WithDefaults()
	if policy.EffectivePreferredUploadTarget() != CapabilitySourceEnterpriseHub {
		t.Fatalf("preferred upload target = %q, want enterprise_hub", policy.EffectivePreferredUploadTarget())
	}
	if got := policy.UploadTargets(true); len(got) != 1 || got[0] != CapabilitySourceEnterpriseHub {
		t.Fatalf("enterprise upload targets = %#v", got)
	}
	if got := policy.UploadTargets(false); len(got) != 0 {
		t.Fatalf("enterprise upload without hub = %#v, want empty", got)
	}
}

func TestCapabilityMarketPolicyExplicitEnterpriseSearchSurvivesUnmarshal(t *testing.T) {
	raw := `{
		"capability_market_policy": {
			"enterprise_only_install": false,
			"enterprise_only_search": true,
			"update_policy": {
				"hubcenter": {
					"free_capability": "notify_admin",
					"paid_capability": "require_license_and_purchase_policy"
				}
			}
		}
	}`
	var cfg AppConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal explicit capability_market_policy: %v", err)
	}
	policy := cfg.CapabilityMarketPolicy
	if policy.EffectiveEnterpriseOnlyInstall() {
		t.Fatal("explicit enterprise_only_install=false should survive unmarshal")
	}
	if !policy.EffectiveEnterpriseOnlySearch() {
		t.Fatal("explicit enterprise_only_search=true should survive unmarshal")
	}
	if policy.UpdatePolicy.HubCenter.FreeCapability != "notify_admin" {
		t.Fatalf("hubcenter free update = %q, want notify_admin", policy.UpdatePolicy.HubCenter.FreeCapability)
	}
	if policy.UpdatePolicy.EnterpriseHub.Default != "auto_update_approved" {
		t.Fatalf("enterprise hub update default should be filled, got %q", policy.UpdatePolicy.EnterpriseHub.Default)
	}
}

// TestProperty7_ConfigSerializationRoundTrip verifies that for any list of
// valid absolute directory path strings, serializing to JSON via AppConfig
// marshal and deserializing back produces an identical list.
//
// Feature: ve-file-sharing-directories, Property 7: Configuration serialization round-trip
// **Validates: Requirements 2.3**
func TestProperty7_ConfigSerializationRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random list of valid absolute directory path strings.
		numDirs := rapid.IntRange(0, 20).Draw(t, "numDirs")
		dirs := make([]string, numDirs)
		for i := 0; i < numDirs; i++ {
			dirs[i] = genAbsolutePath(t, i)
		}

		// Create AppConfig with the generated directories.
		cfg := AppConfig{
			VEAllowedDirectories: dirs,
		}

		// Marshal to JSON.
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		// Unmarshal back.
		var cfg2 AppConfig
		if err := json.Unmarshal(data, &cfg2); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		// Verify identical list.
		if len(cfg2.VEAllowedDirectories) != len(dirs) {
			t.Fatalf("length mismatch: got %d, want %d", len(cfg2.VEAllowedDirectories), len(dirs))
		}
		for i, want := range dirs {
			got := cfg2.VEAllowedDirectories[i]
			if got != want {
				t.Fatalf("dirs[%d] mismatch: got %q, want %q", i, got, want)
			}
		}
	})
}

// genAbsolutePath generates a random valid absolute directory path string.
// It produces both Windows-style (D:\path\to\dir) and Unix-style (/path/to/dir) paths.
func genAbsolutePath(t *rapid.T, idx int) string {
	isWindows := rapid.Bool().Draw(t, "isWindows")
	numSegments := rapid.IntRange(1, 6).Draw(t, "numSegments")

	var path string
	if isWindows {
		// Windows drive letter + backslash-separated segments.
		driveLetter := rapid.SampledFrom([]string{"C", "D", "E", "F", "G", "H"}).Draw(t, "drive")
		path = driveLetter + ":\\"
		for i := 0; i < numSegments; i++ {
			segment := rapid.StringMatching(`[A-Za-z0-9_\-\.]{1,20}`).Draw(t, "segment")
			if i > 0 {
				path += "\\"
			}
			path += segment
		}
	} else {
		// Unix absolute path with forward slashes.
		path = "/"
		for i := 0; i < numSegments; i++ {
			segment := rapid.StringMatching(`[a-z0-9_\-\.]{1,20}`).Draw(t, "segment")
			if i > 0 {
				path += "/"
			}
			path += segment
		}
	}
	return path
}
