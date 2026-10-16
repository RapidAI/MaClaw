package corelib

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// SemanticToolScopeRoutingConfig is the persisted, task-scoped semantic
// routing policy. It deliberately does not reuse SmartRouteEnabled: that
// switch controls Hub message routing and has no authority over local tool
// planning.
type SemanticToolScopeRoutingConfig struct {
	Enabled                bool   `json:"enabled"`
	Mode                   string `json:"mode"`              // scope_only | shadow | off
	LegacyTextRoute        string `json:"legacy_text_route"` // shadow | off
	RequireCatalogCoverage bool   `json:"require_catalog_coverage"`
	AllowDegradedReadOnly  bool   `json:"allow_degraded_read_only"`
	MaxSelections          int    `json:"max_selections"`
	MaxSchemaTokens        int    `json:"max_schema_tokens"`
	MaxIterations          int    `json:"max_iterations"`
}

const (
	SemanticToolScopeRoutingPolicyVersion          = "semantic-tool-scope-router/v1"
	SemanticToolScopeRoutingDefaultMode            = "off"
	SemanticToolScopeRoutingDefaultLegacyTextRoute = "shadow"
	SemanticToolScopeRoutingDefaultMaxSelections   = 32
	SemanticToolScopeRoutingDefaultMaxSchemaTokens = 24000
	SemanticToolScopeRoutingDefaultMaxIterations   = 18
)

// EffectiveRoutingPolicy is the immutable policy snapshot a request should
// consume. Digest and Version are derived from the normalized values so
// downstream routing code never needs to reinterpret raw AppConfig booleans.
type EffectiveRoutingPolicy struct {
	Enabled                bool   `json:"enabled"`
	Mode                   string `json:"mode"`
	LegacyTextRoute        string `json:"legacy_text_route"`
	RequireCatalogCoverage bool   `json:"require_catalog_coverage"`
	AllowDegradedReadOnly  bool   `json:"allow_degraded_read_only"`
	MaxSelections          int    `json:"max_selections"`
	MaxSchemaTokens        int    `json:"max_schema_tokens"`
	MaxIterations          int    `json:"max_iterations"`
	PolicyVersion          string `json:"policy_version"`
	PolicyDigest           string `json:"policy_digest"`
	Version                string `json:"-"`
	Digest                 string `json:"-"`
	Source                 string `json:"source"`
	Valid                  bool   `json:"valid"`
	InvalidReason          string `json:"invalid_reason,omitempty"`
}

// EffectiveRoutingPolicyResolver is a stateless resolver that can be embedded
// by request ingress components. Keeping it stateless makes policy snapshots
// deterministic and easy to test.
type EffectiveRoutingPolicyResolver struct{}

func (EffectiveRoutingPolicyResolver) Resolve(c AppConfig) EffectiveRoutingPolicy {
	return ResolveEffectiveRoutingPolicy(c)
}

// DefaultSemanticToolScopeRoutingConfig returns the safe opt-in defaults.
// Existing installations therefore keep their current routing behavior until
// task-scoped routing is explicitly enabled.
func DefaultSemanticToolScopeRoutingConfig() SemanticToolScopeRoutingConfig {
	return SemanticToolScopeRoutingConfig{
		Enabled:                false,
		Mode:                   SemanticToolScopeRoutingDefaultMode,
		LegacyTextRoute:        SemanticToolScopeRoutingDefaultLegacyTextRoute,
		RequireCatalogCoverage: true,
		AllowDegradedReadOnly:  false,
		MaxSelections:          SemanticToolScopeRoutingDefaultMaxSelections,
		MaxSchemaTokens:        SemanticToolScopeRoutingDefaultMaxSchemaTokens,
		MaxIterations:          SemanticToolScopeRoutingDefaultMaxIterations,
	}
}

// Normalize applies defaults and validates enum/budget values. Invalid
// budgets are retained as invalid rather than silently converted to an
// executable policy, satisfying the zero-budget fail-closed rule.
func (c SemanticToolScopeRoutingConfig) Normalize() (SemanticToolScopeRoutingConfig, error) {
	if strings.TrimSpace(c.Mode) == "" {
		c.Mode = SemanticToolScopeRoutingDefaultMode
	}
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode != "scope_only" && c.Mode != "shadow" && c.Mode != "off" {
		return c, fmt.Errorf("invalid mode %q", c.Mode)
	}
	if strings.TrimSpace(c.LegacyTextRoute) == "" {
		c.LegacyTextRoute = SemanticToolScopeRoutingDefaultLegacyTextRoute
	}
	c.LegacyTextRoute = strings.ToLower(strings.TrimSpace(c.LegacyTextRoute))
	if c.LegacyTextRoute != "shadow" && c.LegacyTextRoute != "off" {
		return c, fmt.Errorf("invalid legacy_text_route %q", c.LegacyTextRoute)
	}
	if c.MaxSelections == 0 {
		return c, fmt.Errorf("max_selections must be positive")
	}
	if c.MaxSchemaTokens == 0 {
		return c, fmt.Errorf("max_schema_tokens must be positive")
	}
	if c.MaxIterations == 0 {
		return c, fmt.Errorf("max_iterations must be positive")
	}
	if c.MaxSelections < 0 || c.MaxSchemaTokens < 0 || c.MaxIterations < 0 {
		return c, fmt.Errorf("routing budgets must be non-negative")
	}
	if !c.Enabled {
		c.Mode = "off"
	}
	return c, nil
}

// EffectiveRoutingPolicy resolves and digests the task-scoped policy.
func ResolveEffectiveRoutingPolicy(c AppConfig) EffectiveRoutingPolicy {
	cfg := c.SemanticToolScopeRouting
	// A zero struct can occur when callers construct AppConfig directly rather
	// than loading JSON. Treat it as absent/default, preserving fail-closed mode.
	if cfg == (SemanticToolScopeRoutingConfig{}) {
		cfg = DefaultSemanticToolScopeRoutingConfig()
	}
	normalized, err := cfg.Normalize()
	policy := EffectiveRoutingPolicy{
		Enabled:                normalized.Enabled,
		Mode:                   normalized.Mode,
		LegacyTextRoute:        normalized.LegacyTextRoute,
		RequireCatalogCoverage: normalized.RequireCatalogCoverage,
		AllowDegradedReadOnly:  normalized.AllowDegradedReadOnly,
		MaxSelections:          normalized.MaxSelections,
		MaxSchemaTokens:        normalized.MaxSchemaTokens,
		MaxIterations:          normalized.MaxIterations,
		PolicyVersion:          SemanticToolScopeRoutingPolicyVersion,
		Version:                SemanticToolScopeRoutingPolicyVersion,
		Source:                 "app_config",
		Valid:                  err == nil,
	}
	if err != nil {
		policy.Enabled = false
		policy.Mode = "off"
		policy.InvalidReason = err.Error()
	}
	policy.PolicyDigest = semanticRoutingPolicyDigest(policy)
	policy.Digest = policy.PolicyDigest
	return policy
}

// EffectiveRoutingPolicy is also available as an AppConfig method for callers
// that already hold a configuration snapshot.
func (c AppConfig) EffectiveRoutingPolicy() EffectiveRoutingPolicy {
	return ResolveEffectiveRoutingPolicy(c)
}

// EffectiveSemanticToolScopeRouting is a descriptive alias for callers that
// want to make the policy family explicit at the call site.
func (c AppConfig) EffectiveSemanticToolScopeRouting() EffectiveRoutingPolicy {
	return ResolveEffectiveRoutingPolicy(c)
}

// AllowsExecutableScope reports whether this snapshot can publish executable
// task-scoped tools. Shadow and invalid/off policies are observational only.
func (p EffectiveRoutingPolicy) AllowsExecutableScope() bool {
	return p.Valid && p.Enabled && p.Mode == "scope_only"
}

// AllowsCatalogCoverage applies the policy's coverage gate to a normalized
// catalog state. Unknown/empty states are rejected when coverage is required.
func (p EffectiveRoutingPolicy) AllowsCatalogCoverage(state string) bool {
	if !p.Valid || !p.Enabled {
		return false
	}
	state = strings.ToLower(strings.TrimSpace(state))
	legacyPending := state == "pending"
	// CatalogCoverage uses complete/incomplete/stale. Keep the older
	// pending/degraded spellings as input aliases for callers during migration,
	// but never treat an unknown or failed state as executable coverage.
	switch state {
	case "ready":
		state = "complete"
	case "pending", "degraded":
		state = "incomplete"
	case "failed":
		state = "stale"
	}
	if state == "complete" {
		return true
	}
	// An incomplete snapshot may be used only by an explicitly read-only
	// degraded policy. The caller still has to enforce the read-only effect
	// ceiling; this method only answers the catalog half of that decision.
	if !legacyPending && state == "incomplete" && p.AllowDegradedReadOnly {
		return true
	}
	// RequireCatalogCoverage is intentionally fail-closed for all lifecycle
	// states other than complete. When it is disabled, callers may opt into the
	// same explicit degraded read-only path, but stale/unknown snapshots remain
	// rejected so a failed refresh cannot masquerade as an empty catalog.
	return !p.RequireCatalogCoverage && !legacyPending && state == "incomplete" && p.AllowDegradedReadOnly
}

// WithinBudget validates planner output against the immutable request
// budgets. Iterations are checked here even though the catalog planner only
// consumes selections/schema tokens, so callers cannot accidentally treat a
// zero or over-limit loop budget as executable.
func (p EffectiveRoutingPolicy) WithinBudget(selections, schemaTokens, iterations int) bool {
	return p.Valid && selections >= 0 && schemaTokens >= 0 && iterations >= 0 &&
		selections <= p.MaxSelections && schemaTokens <= p.MaxSchemaTokens && iterations <= p.MaxIterations
}

func semanticRoutingPolicyDigest(p EffectiveRoutingPolicy) string {
	// Exclude digest itself to avoid recursive identity; validity and source are
	// included so invalid/alternate-source snapshots cannot collide silently.
	p.PolicyDigest = ""
	p.Digest = ""
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return "policy:sha256:" + hex.EncodeToString(h[:])
}
