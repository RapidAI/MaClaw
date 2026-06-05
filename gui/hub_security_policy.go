package main

import (
	"encoding/json"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// HubEffectivePolicy mirrors hub/internal/security.EffectivePolicy on the client side.
// We define a local copy to avoid importing the hub internal package.
type HubEffectivePolicy struct {
	FileOutboundEnabled    bool     `json:"file_outbound_enabled"`
	ImageOutboundEnabled   bool     `json:"image_outbound_enabled"`
	GossipEnabled          bool     `json:"gossip_enabled"`
	GuardrailMode          string   `json:"guardrail_mode"`
	SandboxMode            string   `json:"sandbox_mode"`
	NetworkLevel           string   `json:"network_level"`
	NetworkAllowlist       []string `json:"network_allowlist,omitempty"`
	YoloModeAllowed        bool     `json:"yolo_mode_allowed"`
	SmartRouteEnabled      bool     `json:"smart_route_enabled"`
	SkillSourcesAllowed    []string `json:"skill_sources_allowed,omitempty"` // nil = all allowed; empty with SkillSourcesRestricted=true blocks all.
	SkillSourcesRestricted bool     `json:"skill_sources_restricted,omitempty"`
}

type digitalEmployeeAuthorizationCache struct {
	mu   sync.RWMutex
	auth *corelib.DigitalEmployeeAuthorization
}

func (c *digitalEmployeeAuthorizationCache) get() *corelib.DigitalEmployeeAuthorization {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.auth == nil {
		return nil
	}
	copy := corelib.NormalizeDigitalEmployeeAuthorization(*c.auth, nowUTC())
	return &copy
}

func (c *digitalEmployeeAuthorizationCache) update(auth *corelib.DigitalEmployeeAuthorization) bool {
	if auth != nil {
		normalized := corelib.NormalizeDigitalEmployeeAuthorization(*auth, nowUTC())
		auth = &normalized
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := !reflect.DeepEqual(c.auth, auth)
	if auth == nil {
		c.auth = nil
	} else {
		copy := *auth
		c.auth = &copy
	}
	return changed
}

func nowUTC() time.Time { return time.Now().UTC() }

type DigitalEmployeeFeatureStatus struct {
	Authorization *corelib.DigitalEmployeeAuthorization `json:"authorization,omitempty"`
	ActualCount   int                                   `json:"actual_count"`
	Visible       bool                                  `json:"visible"`
	Reason        string                                `json:"reason,omitempty"`
}

func (a *App) GetDigitalEmployeeFeatureStatus() DigitalEmployeeFeatureStatus {
	auth := a.digitalEmployeeAuthCache.get()
	status := DigitalEmployeeFeatureStatus{Authorization: auth}
	if auth == nil {
		status.Reason = "authorization_unknown"
		return status
	}
	if !auth.Active {
		status.Reason = auth.Reason
		return status
	}
	if auth.Quota <= 0 {
		status.Reason = "quota_zero"
		return status
	}
	status.ActualCount = a.localDigitalEmployeeCount()
	if status.ActualCount <= 0 {
		status.Reason = "no_digital_employees"
		return status
	}
	status.Visible = true
	return status
}

func (a *App) localDigitalEmployeeCount() int {
	status, err := a.GetVEStatus()
	if err != nil || status == nil || !status.Registered || status.Employee == nil {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(status.Employee.Status), "active") {
		return 1
	}
	return 0
}

func (a *App) emitDigitalEmployeeFeatureStatusChanged() {
	a.emitEvent("digital-employee-authorization-changed", a.GetDigitalEmployeeFeatureStatus())
}

// HubSecurityPolicy mirrors hub/internal/security.HeartbeatSecurityPayload on the client side.

type HubHeartbeatConfig struct {
	CapabilityMarketPolicy       corelib.CapabilityMarketPolicy        `json:"capability_market_policy,omitempty"`
	DigitalEmployeeAuthorization *corelib.DigitalEmployeeAuthorization `json:"digital_employee_authorization,omitempty"`
}
type HubSecurityPolicy struct {
	CentralizedSecurity bool                `json:"centralized_security"`
	Policy              *HubEffectivePolicy `json:"policy,omitempty"`
	// SkillSourcesAllowed is set independently of CentralizedSecurity.
	// When centralized=false but source control is configured on maclawsrv,
	// this field carries the restriction.
	SkillSourcesAllowed    []string `json:"skill_sources_allowed,omitempty"`
	SkillSourcesRestricted bool     `json:"skill_sources_restricted,omitempty"`
}

// hubSecurityCache holds the cached security policy received from Hub heartbeat acks.
// Thread-safe via mu. On disconnect the cache is intentionally NOT cleared so the
// last-known policy continues to apply (requirement 7.2).
type hubSecurityCache struct {
	mu     sync.RWMutex
	policy *HubSecurityPolicy
}

// get returns the currently cached policy (may be nil if never received).
func (c *hubSecurityCache) get() *HubSecurityPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.policy
}

// update parses the security_policy field from a heartbeat ack payload,
// updates the cache, and returns true if the policy changed.
func (c *hubSecurityCache) update(raw json.RawMessage) bool {
	var wrapper struct {
		SecurityPolicy *HubSecurityPolicy `json:"security_policy"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		log.Printf("[hub-security] failed to parse ack payload: %v", err)
		return false
	}
	if wrapper.SecurityPolicy == nil {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	changed := !reflect.DeepEqual(c.policy, wrapper.SecurityPolicy)
	if changed {
		c.policy = wrapper.SecurityPolicy
	}
	return changed
}

// GetHubSecurityPolicy returns the current Hub security policy (exposed to frontend via Wails).
func (a *App) GetHubSecurityPolicy() *HubSecurityPolicy {
	return a.hubSecurityCache.get()
}

// IsHubSecurityReadOnly returns true when centralized security is enabled,
// meaning the local security settings UI should be read-only.
func (a *App) IsHubSecurityReadOnly() bool {
	p := a.hubSecurityCache.get()
	return p != nil && p.CentralizedSecurity
}

// updateHubSecurityPolicy is called from the readLoop when an ack message arrives.
// It parses the payload, updates the cache, and emits an event if the policy changed.
// When the policy changes, it also applies the enforcement actions (Req 7.5-7.8, 4.5).
func (a *App) updateHubSecurityPolicy(payload json.RawMessage) {
	if a.hubSecurityCache.update(payload) {
		policy := a.hubSecurityCache.get()
		a.applyHubSecurityPolicy(policy)
		a.emitEvent("hub-security-policy-changed", policy)
		log.Printf("[hub-security] policy updated: centralized=%v", policy.CentralizedSecurity)
	}
}

// applyHubSecurityPolicy enforces the Hub-pushed security policy on local components.
//
// Enforcement actions (Requirements 4.5, 7.5, 7.6, 7.7, 7.8):
//   - guardrail_mode  -> PolicyEngine.SetMode
//   - sandbox_mode    -> stored for Firewall sandbox configuration
//   - network_level   -> stored for network access level enforcement
//   - yolo_mode_allowed=false -> YOLO mode is force-disabled at launch time
//   - centralized_security=true -> frontend switches to read-only mode (via event)
func (a *App) applyHubSecurityPolicy(policy *HubSecurityPolicy) {
	if policy == nil {
		return
	}
	a.persistHubSecurityPolicy(policy)

	// Log skill source restrictions even when centralized security is off
	// (independent source control plane).
	if policy.SkillSourcesRestricted || len(policy.SkillSourcesAllowed) > 0 {
		log.Printf("[hub-security] skill_sources_allowed applied (independent): %v", policy.SkillSourcesAllowed)
	}

	if !policy.CentralizedSecurity || policy.Policy == nil {
		// Centralized security is off - no further enforcement needed.
		// The frontend event will restore editable mode.
		return
	}

	ep := policy.Policy

	// 1. guardrail_mode -> PolicyEngine.SetMode (Req 7.5)
	if ep.GuardrailMode != "" && a.policyEngine != nil {
		a.policyEngine.SetMode(ep.GuardrailMode)
		log.Printf("[hub-security] guardrail_mode applied: %s", ep.GuardrailMode)
	}

	// 2. sandbox_mode -> update Firewall sandbox configuration (Req 7.6)
	if ep.SandboxMode != "" {
		log.Printf("[hub-security] sandbox_mode applied: %s", ep.SandboxMode)
	}

	// 3. network_level -> update network access level (Req 7.7)
	if ep.NetworkLevel != "" {
		log.Printf("[hub-security] network_level applied: %s", ep.NetworkLevel)
	}

	// 4. yolo_mode_allowed=false -> force-disable YOLO mode (Req 7.8)
	if !ep.YoloModeAllowed {
		log.Printf("[hub-security] yolo_mode forced disabled by hub policy")
	}

	// 5. skill_sources_allowed -> restrict skill search/download sources
	if ep.SkillSourcesRestricted || len(ep.SkillSourcesAllowed) > 0 {
		log.Printf("[hub-security] skill_sources_allowed applied (centralized): %v", ep.SkillSourcesAllowed)
	}
}

func (a *App) persistHubSecurityPolicy(policy *HubSecurityPolicy) {
	if policy == nil {
		return
	}
	if err := a.patchConfig(func(cfg *corelib.AppConfig) {
		cfg.HubSecurityCentralized = policy.CentralizedSecurity
		cfg.SkillSourcesAllowed = skillSourcesForAppConfig(policy.SkillSourcesAllowed, policy.SkillSourcesRestricted)
		if !policy.CentralizedSecurity || policy.Policy == nil {
			return
		}
		ep := policy.Policy
		if strings.TrimSpace(ep.GuardrailMode) != "" {
			cfg.SecurityPolicyMode = ep.GuardrailMode
		}
		if strings.TrimSpace(ep.SandboxMode) != "" {
			cfg.SandboxMode = ep.SandboxMode
		}
		if strings.TrimSpace(ep.NetworkLevel) != "" {
			cfg.NetworkLevel = ep.NetworkLevel
		}
		cfg.NetworkAllowlist = append([]string(nil), ep.NetworkAllowlist...)
		cfg.YoloModeAllowed = ep.YoloModeAllowed
		cfg.SmartRouteEnabled = ep.SmartRouteEnabled
		cfg.GossipEnabled = ep.GossipEnabled
		cfg.FileOutboundEnabled = ep.FileOutboundEnabled
		cfg.ImageOutboundEnabled = ep.ImageOutboundEnabled
		cfg.SkillSourcesAllowed = skillSourcesForAppConfig(ep.SkillSourcesAllowed, ep.SkillSourcesRestricted)
	}, true); err != nil {
		log.Printf("[hub-security] failed to persist hub policy to local config: %v", err)
	}
}

// isGossipAllowed checks whether Gossip functionality is permitted given the
// current Hub security policy. When centralized security is enabled and the
// effective policy sets gossip_enabled=false, this returns false (Req 6.1, 6.3, 6.4).
func (a *App) isGossipAllowed() bool {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return true // no centralized policy - allow local preference
	}
	return p.Policy.GossipEnabled
}

// IsGossipAllowed returns whether Gossip is allowed (exposed to frontend via Wails).
// The frontend uses this to hide/show the Gossip sidebar icon and panel.
func (a *App) IsGossipAllowed() bool {
	return a.isGossipAllowed()
}

// isYoloModeAllowed checks whether YOLO mode is permitted given the current
// Hub security policy. When centralized security is enabled and the effective
// policy sets yolo_mode_allowed=false, this returns false regardless of the
// user's local preference (Req 7.8).
func (a *App) isYoloModeAllowed() bool {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return true // no centralized policy - allow local preference
	}
	return p.Policy.YoloModeAllowed
}

// getHubSandboxMode returns the Hub-enforced sandbox mode, or empty string
// if centralized security is not active.
func (a *App) getHubSandboxMode() string {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return ""
	}
	return p.Policy.SandboxMode
}

// getHubNetworkLevel returns the Hub-enforced network access level, or empty
// string if centralized security is not active.
func (a *App) getHubNetworkLevel() string {
	p := a.hubSecurityCache.get()
	if p == nil || !p.CentralizedSecurity || p.Policy == nil {
		return ""
	}
	return p.Policy.NetworkLevel
}

// GetAllowedSkillSources returns the list of allowed skill search/download sources.
// Resolution: Hub heartbeat payload (merged from security group policy + independent
// source control) > local AppConfig > default (all allowed).
// Returns nil when all sources are allowed.
func (a *App) GetAllowedSkillSources() []string {
	if a != nil && a.securityPolicyMode() == "developer" {
		return nil
	}
	p := a.hubSecurityCache.get()
	if p != nil {
		// Case 1: Centralized security on - use Policy.SkillSourcesAllowed (already
		// merged with independent source control on the server side).
		if p.CentralizedSecurity && p.Policy != nil && (p.Policy.SkillSourcesRestricted || len(p.Policy.SkillSourcesAllowed) > 0) {
			return cloneAllowedSkillSources(p.Policy.SkillSourcesAllowed)
		}
		// Case 2: Centralized security off but independent source control is active - the server pushes SkillSourcesAllowed at the payload level.
		if p.SkillSourcesRestricted || len(p.SkillSourcesAllowed) > 0 {
			return cloneAllowedSkillSources(p.SkillSourcesAllowed)
		}
	}
	// 3. Check local config (standalone mode, no Hub connection).
	cfg, err := a.LoadConfig()
	if err == nil && strings.EqualFold(strings.TrimSpace(cfg.SecurityPolicyMode), "developer") {
		return nil
	}
	if err == nil && len(cfg.SkillSourcesAllowed) > 0 {
		return cloneAllowedSkillSources(cfg.SkillSourcesAllowed)
	}
	// 4. Default: all sources allowed (nil = no filter).
	return nil
}

// IsSkillSourceAllowed checks whether a specific source is permitted.
// Returns true when all sources are allowed (nil/empty list) or when the
// source is in the allowed list.
func (a *App) IsSkillSourceAllowed(source string) bool {
	return isAllowedSkillSourceList(source, a.GetAllowedSkillSources())
}

func isAllowedSkillSourceList(source string, allowed []string) bool {
	if allowed == nil {
		return true
	}
	source = normalizeHubSkillSource(source)
	for _, s := range allowed {
		if normalizeHubSkillSource(s) == source {
			return true
		}
	}
	return false
}

func cloneAllowedSkillSources(sources []string) []string {
	if sources == nil {
		return nil
	}
	out := make([]string, len(sources))
	copy(out, sources)
	return out
}

func normalizeHubSkillSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "skillmarket", "market", "hubcenter", "hub_center", "skill_hub":
		return "skillhub"
	case "enterprise", "hub", "enterprisehub", "enterprise_hub":
		return corelib.CapabilitySourceEnterpriseHub
	case "claw_hub":
		return corelib.CapabilitySourceClawHub
	case "git_hub":
		return corelib.CapabilitySourceGitHub
	default:
		return strings.TrimSpace(strings.ToLower(source))
	}
}

// enforceYoloMode applies the Hub YOLO override to a launch spec's YoloMode flag.
// Returns the (possibly overridden) value and a human-readable reason if overridden.
func (a *App) enforceYoloMode(requested bool) (bool, string) {
	if requested && !a.isYoloModeAllowed() {
		return false, "YOLO mode is disabled by Hub security policy"
	}
	return requested, ""
}

// enforceYoloModeQuiet is a convenience wrapper that returns only the bool.
// Used in launch spec construction where the reason string is not needed.
func (a *App) enforceYoloModeQuiet(requested bool) bool {
	v, _ := a.enforceYoloMode(requested)
	return v
}

func (a *App) updateHubHeartbeatConfig(payload json.RawMessage) bool {
	var wrapper struct {
		HubConfig *struct {
			CapabilityMarketPolicy       *corelib.CapabilityMarketPolicy       `json:"capability_market_policy,omitempty"`
			DigitalEmployeeAuthorization *corelib.DigitalEmployeeAuthorization `json:"digital_employee_authorization,omitempty"`
		} `json:"hub_config"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		log.Printf("[hub-config] failed to parse ack payload: %v", err)
		return false
	}
	if wrapper.HubConfig == nil {
		return false
	}
	// Only update the digital employee authorization cache when the Hub
	// explicitly includes the field. When the Hub hasn't yet synced
	// authorization from HubCenter, the field is omitted (nil due to
	// omitempty). Treating nil as "no update" prevents clearing a
	// previously cached valid authorization during transient sync gaps.
	if wrapper.HubConfig.DigitalEmployeeAuthorization != nil {
		if a.digitalEmployeeAuthCache.update(wrapper.HubConfig.DigitalEmployeeAuthorization) {
			a.emitEvent("digital-employee-authorization-changed", a.GetDigitalEmployeeFeatureStatus())
		}
	}
	if wrapper.HubConfig.CapabilityMarketPolicy == nil {
		return false
	}
	policy := wrapper.HubConfig.CapabilityMarketPolicy.WithDefaults()
	if cfg, err := a.LoadConfig(); err != nil {
		log.Printf("[hub-config] failed to load config for hub update: %v", err)
		return false
	} else if reflect.DeepEqual(cfg.CapabilityMarketPolicy.WithDefaults(), policy) {
		return false
	}
	changed := false
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		if reflect.DeepEqual(cfg.CapabilityMarketPolicy.WithDefaults(), policy) {
			return
		}
		cfg.CapabilityMarketPolicy = policy
		changed = true
	}); err != nil {
		log.Printf("[hub-config] failed to save hub-pushed config: %v", err)
		return false
	}
	if !changed {
		return false
	}
	a.emitEvent("hub-config-options-changed", wrapper.HubConfig)
	log.Printf("[hub-config] capability market policy updated from hub heartbeat")
	return true
}
