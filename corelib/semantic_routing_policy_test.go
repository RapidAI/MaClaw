package corelib

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSemanticToolScopeRoutingDefaultsFailClosed(t *testing.T) {
	cfg := AppConfigDefaults()
	policy := cfg.EffectiveRoutingPolicy()
	if policy.Enabled || policy.Mode != "off" || policy.AllowsExecutableScope() {
		t.Fatalf("default policy must be disabled/fail-closed: %#v", policy)
	}
	if !policy.Valid || policy.MaxSelections != SemanticToolScopeRoutingDefaultMaxSelections {
		t.Fatalf("default policy should still be valid and budgeted: %#v", policy)
	}
	if cfg.SmartRouteEnabled != true {
		t.Fatal("Hub smart route default unexpectedly changed")
	}
}

func TestSemanticToolScopeRoutingEnabledScopeOnly(t *testing.T) {
	cfg := AppConfigDefaults()
	cfg.SemanticToolScopeRouting.Enabled = true
	cfg.SemanticToolScopeRouting.Mode = "scope_only"
	policy := cfg.EffectiveRoutingPolicy()
	if !policy.Valid || !policy.Enabled || !policy.AllowsExecutableScope() {
		t.Fatalf("scope_only policy not executable: %#v", policy)
	}
	if !strings.HasPrefix(policy.Digest, "policy:sha256:") {
		t.Fatalf("unexpected policy digest %q", policy.Digest)
	}
	if policy.Version != SemanticToolScopeRoutingPolicyVersion {
		t.Fatalf("unexpected policy version %q", policy.Version)
	}
}

func TestSemanticToolScopeRoutingInvalidBudgetFailsClosed(t *testing.T) {
	cfg := AppConfig{SemanticToolScopeRouting: SemanticToolScopeRoutingConfig{
		Enabled: true, Mode: "scope_only", LegacyTextRoute: "shadow",
		RequireCatalogCoverage: true, MaxSelections: 0, MaxSchemaTokens: 24000, MaxIterations: 18,
	}}
	policy := cfg.EffectiveRoutingPolicy()
	if policy.Valid || policy.Enabled || policy.AllowsExecutableScope() || policy.InvalidReason == "" {
		t.Fatalf("zero budget must fail closed: %#v", policy)
	}
}

func TestSemanticToolScopeRoutingJSONRoundTrip(t *testing.T) {
	cfg := AppConfigDefaults()
	cfg.SemanticToolScopeRouting.Enabled = true
	cfg.SemanticToolScopeRouting.Mode = "shadow"
	cfg.SemanticToolScopeRouting.AllowDegradedReadOnly = true
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AppConfig
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SemanticToolScopeRouting != cfg.SemanticToolScopeRouting {
		t.Fatalf("semantic policy changed across JSON roundtrip: %#v != %#v", decoded.SemanticToolScopeRouting, cfg.SemanticToolScopeRouting)
	}
	if decoded.SmartRouteEnabled != cfg.SmartRouteEnabled {
		t.Fatal("Hub smart route changed across JSON roundtrip")
	}
}

func TestEffectiveRoutingPolicyGatesCoverageAndBudget(t *testing.T) {
	cfg := AppConfigDefaults()
	cfg.SemanticToolScopeRouting.Enabled = true
	cfg.SemanticToolScopeRouting.Mode = "scope_only"
	p := cfg.EffectiveRoutingPolicy()
	if !p.AllowsCatalogCoverage("complete") || p.AllowsCatalogCoverage("pending") || p.AllowsCatalogCoverage("degraded") {
		t.Fatalf("coverage gate unexpectedly permissive: %#v", p)
	}
	if !p.WithinBudget(1, 100, 1) || p.WithinBudget(p.MaxSelections+1, 1, 1) || p.WithinBudget(1, p.MaxSchemaTokens+1, 1) || p.WithinBudget(1, 1, p.MaxIterations+1) {
		t.Fatalf("budget gate failed: %#v", p)
	}
	cfg.SemanticToolScopeRouting.AllowDegradedReadOnly = true
	if !cfg.EffectiveRoutingPolicy().AllowsCatalogCoverage("degraded") {
		t.Fatal("degraded read-only allowance was not honored")
	}
}

func TestEffectiveRoutingPolicyUsesCatalogLifecycleStates(t *testing.T) {
	cfg := AppConfigDefaults()
	cfg.SemanticToolScopeRouting.Enabled = true
	cfg.SemanticToolScopeRouting.Mode = "scope_only"
	p := cfg.EffectiveRoutingPolicy()
	for _, state := range []string{"incomplete", "stale", "pending", "failed", "unknown", ""} {
		if p.AllowsCatalogCoverage(state) {
			t.Fatalf("state %q must be rejected by the default complete-only policy", state)
		}
	}
	cfg.SemanticToolScopeRouting.AllowDegradedReadOnly = true
	p = cfg.EffectiveRoutingPolicy()
	if !p.AllowsCatalogCoverage("incomplete") || !p.AllowsCatalogCoverage("degraded") {
		t.Fatal("explicit degraded read-only policy should accept incomplete/degraded aliases")
	}
	if p.AllowsCatalogCoverage("pending") || p.AllowsCatalogCoverage("stale") || p.AllowsCatalogCoverage("failed") {
		t.Fatal("pending/stale/failed coverage must remain blocked")
	}
}
