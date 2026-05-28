package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestHubSecurityCacheUpdate_ParsesPolicy(t *testing.T) {
	cache := &hubSecurityCache{}

	payload := json.RawMessage(`{
		"request_id": "abc",
		"security_policy": {
			"centralized_security": true,
			"policy": {
				"file_outbound_enabled": false,
				"image_outbound_enabled": true,
				"gossip_enabled": false,
				"guardrail_mode": "strict",
				"sandbox_mode": "docker",
				"network_level": "intranet",
				"yolo_mode_allowed": false,
				"smart_route_enabled": true
			}
		}
	}`)

	changed := cache.update(payload)
	if !changed {
		t.Fatal("expected policy change on first update")
	}

	got := cache.get()
	if got == nil {
		t.Fatal("expected non-nil policy")
	}
	if !got.CentralizedSecurity {
		t.Error("expected centralized_security=true")
	}
	if got.Policy == nil {
		t.Fatal("expected non-nil effective policy")
	}
	if got.Policy.FileOutboundEnabled {
		t.Error("expected file_outbound_enabled=false")
	}
	if got.Policy.GuardrailMode != "strict" {
		t.Errorf("expected guardrail_mode=strict, got %s", got.Policy.GuardrailMode)
	}
	if got.Policy.YoloModeAllowed {
		t.Error("expected yolo_mode_allowed=false")
	}
}

func TestHubSecurityCacheUpdate_NoChangeOnSamePolicy(t *testing.T) {
	cache := &hubSecurityCache{}

	payload := json.RawMessage(`{
		"security_policy": {
			"centralized_security": false
		}
	}`)

	changed := cache.update(payload)
	if !changed {
		t.Fatal("expected change on first update")
	}

	changed = cache.update(payload)
	if changed {
		t.Error("expected no change on identical update")
	}
}

func TestHubSecurityCacheUpdate_NoSecurityPolicyField(t *testing.T) {
	cache := &hubSecurityCache{}

	payload := json.RawMessage(`{"request_id": "xyz"}`)
	changed := cache.update(payload)
	if changed {
		t.Error("expected no change when security_policy is absent")
	}
	if cache.get() != nil {
		t.Error("expected nil policy when never set")
	}
}

func TestHubSecurityCacheUpdate_InvalidJSON(t *testing.T) {
	cache := &hubSecurityCache{}

	payload := json.RawMessage(`not valid json`)
	changed := cache.update(payload)
	if changed {
		t.Error("expected no change on invalid JSON")
	}
}

func TestHubSecurityCacheUpdate_DetectsChange(t *testing.T) {
	cache := &hubSecurityCache{}

	p1 := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {"guardrail_mode": "standard"}
		}
	}`)
	p2 := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {"guardrail_mode": "strict"}
		}
	}`)

	cache.update(p1)
	changed := cache.update(p2)
	if !changed {
		t.Error("expected change when policy differs")
	}
	if cache.get().Policy.GuardrailMode != "strict" {
		t.Error("expected updated guardrail_mode")
	}
}

func TestHubSecurityCache_PreservedOnDisconnect(t *testing.T) {
	// Simulate: receive policy, then "disconnect" (just stop updating).
	// The cache should still hold the last policy.
	cache := &hubSecurityCache{}

	payload := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {"gossip_enabled": false}
		}
	}`)
	cache.update(payload)

	// After disconnect, get() should still return the cached policy.
	got := cache.get()
	if got == nil {
		t.Fatal("expected cached policy to persist after disconnect")
	}
	if got.Policy.GossipEnabled {
		t.Error("expected gossip_enabled=false from cached policy")
	}
}

// Policy enforcement tests (Task 13.2)

func TestApplyHubSecurityPolicy_GuardrailMode(t *testing.T) {
	// Validates: Requirements 7.5 - guardrail_mode change calls PolicyEngine.SetMode
	app := &App{}
	app.policyEngine = NewPolicyEngine() // starts with "standard" rules

	policy := &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			GuardrailMode: "strict",
		},
	}
	app.applyHubSecurityPolicy(policy)

	// Verify the policy engine now uses strict rules.
	rules := app.policyEngine.Rules()
	foundDenyCritical := false
	for _, r := range rules {
		if r.Name == "deny-critical" {
			foundDenyCritical = true
			break
		}
	}
	if !foundDenyCritical {
		t.Error("expected strict mode to include deny-critical rule")
	}
}

func TestApplyHubSecurityPolicy_NilPolicy(t *testing.T) {
	app := &App{}
	app.policyEngine = NewPolicyEngine()
	// Should not panic on nil policy.
	app.applyHubSecurityPolicy(nil)
}

func TestApplyHubSecurityPolicy_CentralizedOff(t *testing.T) {
	// When centralized_security is false, no enforcement should happen.
	app := &App{}
	app.policyEngine = NewPolicyEngine()

	policy := &HubSecurityPolicy{
		CentralizedSecurity: false,
		Policy: &HubEffectivePolicy{
			GuardrailMode: "strict",
		},
	}
	app.applyHubSecurityPolicy(policy)

	// Policy engine should still be in standard mode (not changed to strict).
	rules := app.policyEngine.Rules()
	for _, r := range rules {
		if r.Name == "deny-critical" {
			t.Error("expected standard mode, but found strict-mode rule deny-critical")
		}
	}
}

func TestIsYoloModeAllowed_NoCachedPolicy(t *testing.T) {
	// Validates: Requirements 7.8 - no cached policy means YOLO is allowed
	app := &App{}
	if !app.isYoloModeAllowed() {
		t.Error("expected YOLO allowed when no cached policy")
	}
}

func TestIsYoloModeAllowed_CentralizedOff(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: false,
		Policy:              &HubEffectivePolicy{YoloModeAllowed: false},
	}
	app.hubSecurityCache.mu.Unlock()

	if !app.isYoloModeAllowed() {
		t.Error("expected YOLO allowed when centralized security is off")
	}
}

func TestIsYoloModeAllowed_Forbidden(t *testing.T) {
	// Validates: Requirements 7.8 - yolo_mode_allowed=false forces YOLO off
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{YoloModeAllowed: false},
	}
	app.hubSecurityCache.mu.Unlock()

	if app.isYoloModeAllowed() {
		t.Error("expected YOLO forbidden when policy disallows it")
	}
}

func TestIsYoloModeAllowed_Allowed(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{YoloModeAllowed: true},
	}
	app.hubSecurityCache.mu.Unlock()

	if !app.isYoloModeAllowed() {
		t.Error("expected YOLO allowed when policy permits it")
	}
}

func TestEnforceYoloMode_Override(t *testing.T) {
	// Validates: Requirements 7.8 - enforceYoloMode overrides requested=true
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{YoloModeAllowed: false},
	}
	app.hubSecurityCache.mu.Unlock()

	got, reason := app.enforceYoloMode(true)
	if got {
		t.Error("expected YOLO to be overridden to false")
	}
	if reason == "" {
		t.Error("expected non-empty reason when YOLO is overridden")
	}
}

func TestEnforceYoloMode_NoOverrideWhenNotRequested(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{YoloModeAllowed: false},
	}
	app.hubSecurityCache.mu.Unlock()

	// If user didn't request YOLO, no override needed.
	got, reason := app.enforceYoloMode(false)
	if got {
		t.Error("expected false when not requested")
	}
	if reason != "" {
		t.Error("expected empty reason when YOLO was not requested")
	}
}

func TestEnforceYoloModeQuiet(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{YoloModeAllowed: false},
	}
	app.hubSecurityCache.mu.Unlock()

	if app.enforceYoloModeQuiet(true) {
		t.Error("expected enforceYoloModeQuiet to return false")
	}
	if app.enforceYoloModeQuiet(false) {
		t.Error("expected false when not requested")
	}
}

func TestIsHubSecurityReadOnly(t *testing.T) {
	// Validates: Requirements 4.5 - centralized_security=true -> read-only mode
	app := &App{}

	// No cached policy -> not read-only.
	if app.IsHubSecurityReadOnly() {
		t.Error("expected not read-only when no cached policy")
	}

	// Centralized off -> not read-only.
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{CentralizedSecurity: false}
	app.hubSecurityCache.mu.Unlock()
	if app.IsHubSecurityReadOnly() {
		t.Error("expected not read-only when centralized is off")
	}

	// Centralized on -> read-only.
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{CentralizedSecurity: true}
	app.hubSecurityCache.mu.Unlock()
	if !app.IsHubSecurityReadOnly() {
		t.Error("expected read-only when centralized is on")
	}
}

func TestGetHubSandboxMode(t *testing.T) {
	// Validates: Requirements 7.6 - sandbox_mode accessible when centralized
	app := &App{}

	// No policy -> empty.
	if got := app.getHubSandboxMode(); got != "" {
		t.Errorf("expected empty sandbox mode, got %q", got)
	}

	// Centralized off -> empty.
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: false,
		Policy:              &HubEffectivePolicy{SandboxMode: "docker"},
	}
	app.hubSecurityCache.mu.Unlock()
	if got := app.getHubSandboxMode(); got != "" {
		t.Errorf("expected empty when centralized off, got %q", got)
	}

	// Centralized on -> returns value.
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{SandboxMode: "docker"},
	}
	app.hubSecurityCache.mu.Unlock()
	if got := app.getHubSandboxMode(); got != "docker" {
		t.Errorf("expected docker, got %q", got)
	}
}

func TestGetHubNetworkLevel(t *testing.T) {
	// Validates: Requirements 7.7 - network_level accessible when centralized
	app := &App{}

	// No policy -> empty.
	if got := app.getHubNetworkLevel(); got != "" {
		t.Errorf("expected empty network level, got %q", got)
	}

	// Centralized on -> returns value.
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{NetworkLevel: "intranet"},
	}
	app.hubSecurityCache.mu.Unlock()
	if got := app.getHubNetworkLevel(); got != "intranet" {
		t.Errorf("expected intranet, got %q", got)
	}
}

func TestUpdateHubSecurityPolicy_AppliesEnforcement(t *testing.T) {
	// Integration test: updateHubSecurityPolicy should apply enforcement.
	app := &App{}
	app.policyEngine = NewPolicyEngine()

	payload := json.RawMessage(`{
		"security_policy": {
			"centralized_security": true,
			"policy": {
				"guardrail_mode": "relaxed",
				"sandbox_mode": "os",
				"network_level": "allowlist",
				"yolo_mode_allowed": false
			}
		}
	}`)

	app.updateHubSecurityPolicy(payload)

	// Verify guardrail mode was applied.
	rules := app.policyEngine.Rules()
	foundAllowHigh := false
	for _, r := range rules {
		if r.Name == "allow-high" {
			foundAllowHigh = true
			break
		}
	}
	if !foundAllowHigh {
		t.Error("expected relaxed mode to include allow-high rule")
	}

	// Verify YOLO is blocked.
	if app.isYoloModeAllowed() {
		t.Error("expected YOLO to be blocked after policy update")
	}

	// Verify sandbox and network level are accessible.
	if got := app.getHubSandboxMode(); got != "os" {
		t.Errorf("expected sandbox_mode=os, got %q", got)
	}
	if got := app.getHubNetworkLevel(); got != "allowlist" {
		t.Errorf("expected network_level=allowlist, got %q", got)
	}

	// Verify read-only mode.
	if !app.IsHubSecurityReadOnly() {
		t.Error("expected read-only mode when centralized is on")
	}
}

func TestPersistHubSecurityPolicyClearsRemovedSliceSettings(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}

	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.HubSecurityCentralized = true
		cfg.NetworkAllowlist = []string{"api.example.com"}
		cfg.SkillSourcesAllowed = []string{"github"}
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	app.persistHubSecurityPolicy(&HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy: &HubEffectivePolicy{
			NetworkLevel:         "allowlist",
			FileOutboundEnabled:  true,
			ImageOutboundEnabled: true,
			GossipEnabled:        true,
			SmartRouteEnabled:    false,
			YoloModeAllowed:      true,
		},
	})

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.NetworkAllowlist) != 0 {
		t.Fatalf("NetworkAllowlist = %#v, want cleared", cfg.NetworkAllowlist)
	}
	if len(cfg.SkillSourcesAllowed) != 0 {
		t.Fatalf("SkillSourcesAllowed = %#v, want cleared", cfg.SkillSourcesAllowed)
	}
	if cfg.SmartRouteEnabled {
		t.Fatal("SmartRouteEnabled = true, want persisted false")
	}
}

func TestIsSkillSourceAllowedNormalizesAliases(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{SkillSourcesAllowed: []string{"hubcenter", "git_hub", "enterprise"}}
	app.hubSecurityCache.mu.Unlock()

	for _, source := range []string{"skillhub", "github", corelib.CapabilitySourceEnterpriseHub} {
		if !app.IsSkillSourceAllowed(source) {
			t.Fatalf("source %q should be allowed by aliases", source)
		}
	}
	if app.IsSkillSourceAllowed("clawhub") {
		t.Fatal("clawhub should not be allowed")
	}
}

// Gossip permission tests (Task 13.3)

func TestIsGossipAllowed_NoCachedPolicy(t *testing.T) {
	// Validates: Requirements 6.5 - no cached policy means gossip follows local config
	app := &App{}
	if !app.isGossipAllowed() {
		t.Error("expected gossip allowed when no cached policy")
	}
}

func TestIsGossipAllowed_CentralizedOff(t *testing.T) {
	// Validates: Requirements 6.5 - centralized off means gossip follows local config
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: false,
		Policy:              &HubEffectivePolicy{GossipEnabled: false},
	}
	app.hubSecurityCache.mu.Unlock()

	if !app.isGossipAllowed() {
		t.Error("expected gossip allowed when centralized security is off")
	}
}

func TestIsGossipAllowed_Forbidden(t *testing.T) {
	// Validates: Requirements 6.1, 6.3, 6.4 - gossip_enabled=false disables gossip
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{GossipEnabled: false},
	}
	app.hubSecurityCache.mu.Unlock()

	if app.isGossipAllowed() {
		t.Error("expected gossip forbidden when policy disallows it")
	}
}

func TestIsGossipAllowed_Allowed(t *testing.T) {
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{GossipEnabled: true},
	}
	app.hubSecurityCache.mu.Unlock()

	if !app.isGossipAllowed() {
		t.Error("expected gossip allowed when policy permits it")
	}
}

func TestIsGossipAllowed_NilEffectivePolicy(t *testing.T) {
	// Centralized on but no effective policy -> allow (fail-open)
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              nil,
	}
	app.hubSecurityCache.mu.Unlock()

	if !app.isGossipAllowed() {
		t.Error("expected gossip allowed when effective policy is nil")
	}
}

func TestIsGossipAllowed_WailsBinding(t *testing.T) {
	// Validates: the public IsGossipAllowed() binding matches isGossipAllowed()
	app := &App{}
	app.hubSecurityCache.mu.Lock()
	app.hubSecurityCache.policy = &HubSecurityPolicy{
		CentralizedSecurity: true,
		Policy:              &HubEffectivePolicy{GossipEnabled: false},
	}
	app.hubSecurityCache.mu.Unlock()

	if app.IsGossipAllowed() != app.isGossipAllowed() {
		t.Error("expected IsGossipAllowed() to match isGossipAllowed()")
	}
}

func TestDigitalEmployeeFeatureStatusHidesInactiveAuthorizationReasons(t *testing.T) {
	cases := []struct {
		name string
		auth corelib.DigitalEmployeeAuthorization
		want string
	}{
		{name: "expired", auth: corelib.DigitalEmployeeAuthorization{Quota: 1, Enabled: true, ExpiresAt: "2000-01-01T00:00:00Z"}, want: "expired"},
		{name: "quota zero", auth: corelib.DigitalEmployeeAuthorization{Quota: 0, Enabled: true, ExpiresAt: "2999-01-01T00:00:00Z"}, want: "quota_zero"},
		{name: "not subscribed", auth: corelib.DigitalEmployeeAuthorization{Quota: 1, Enabled: true}, want: "not_subscribed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &App{testHomeDir: t.TempDir()}
			normalized := corelib.NormalizeDigitalEmployeeAuthorization(tc.auth, nowUTC())
			app.digitalEmployeeAuthCache.update(&normalized)
			status := app.GetDigitalEmployeeFeatureStatus()
			if status.Visible || status.Reason != tc.want {
				t.Fatalf("status = %+v, want hidden reason %q", status, tc.want)
			}
		})
	}
}
func TestDigitalEmployeeFeatureStatusRechecksCachedExpiryAtReadTime(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.digitalEmployeeAuthCache.mu.Lock()
	app.digitalEmployeeAuthCache.auth = &corelib.DigitalEmployeeAuthorization{Enabled: true, Active: true, Quota: 1, ExpiresAt: "2000-01-01T00:00:00Z"}
	app.digitalEmployeeAuthCache.mu.Unlock()

	status := app.GetDigitalEmployeeFeatureStatus()
	if status.Visible || status.Reason != "expired" || status.Authorization == nil || status.Authorization.Active {
		t.Fatalf("stale cached authorization status = %+v, want expired hidden", status)
	}
}

func TestDigitalEmployeeFeatureStatusVisibilityRequiresActiveLocalEmployee(t *testing.T) {
	var statusJSON string
	var statusRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer machine-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-local" {
			t.Fatalf("X-Machine-ID header = %q", got)
		}
		statusRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(statusJSON))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineToken: "machine-token", RemoteMachineID: "machine-local"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	status := app.GetDigitalEmployeeFeatureStatus()
	if status.Visible || status.Reason != "authorization_unknown" {
		t.Fatalf("without authorization status = %+v, want hidden authorization_unknown", status)
	}
	if statusRequests != 0 {
		t.Fatalf("inactive authorization should not query local employee status, got %d requests", statusRequests)
	}

	auth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 2, Enabled: true, ExpiresAt: "2999-01-01T00:00:00Z"}, nowUTC())
	app.digitalEmployeeAuthCache.update(&auth)
	statusJSON = `{"registered":false}`
	status = app.GetDigitalEmployeeFeatureStatus()
	if status.Visible || status.ActualCount != 0 || status.Reason != "no_digital_employees" {
		t.Fatalf("no employees status = %+v, want hidden no_digital_employees", status)
	}

	statusJSON = `{"registered":true,"employee":{"id":"ve-1","name":"Research","status":"pending"}}`
	status = app.GetDigitalEmployeeFeatureStatus()
	if status.Visible || status.ActualCount != 0 || status.Reason != "no_digital_employees" {
		t.Fatalf("pending employee status = %+v, want hidden no_digital_employees", status)
	}

	statusJSON = `{"registered":true,"employee":{"id":"ve-1","name":"Research","status":"active"}}`
	status = app.GetDigitalEmployeeFeatureStatus()
	if !status.Visible || status.ActualCount != 1 || status.Reason != "" {
		t.Fatalf("authorized employee status = %+v, want visible", status)
	}

	disabled := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 2, Enabled: false, ExpiresAt: "2999-01-01T00:00:00Z"}, nowUTC())
	app.digitalEmployeeAuthCache.update(&disabled)
	beforeDisabled := statusRequests
	status = app.GetDigitalEmployeeFeatureStatus()
	if status.Visible || status.Reason != "disabled" {
		t.Fatalf("disabled authorization status = %+v, want hidden disabled", status)
	}
	if statusRequests != beforeDisabled {
		t.Fatalf("disabled authorization should not query local employee status, before=%d after=%d", beforeDisabled, statusRequests)
	}
}
