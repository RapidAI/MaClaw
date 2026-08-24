package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// ExpertCapabilityRule is a reviewed control-plane restriction. It names a
// registered capability (and optional registered qualifiers) and may only
// tighten to deny or require_confirmation. Tool and skill names have no
// input path here.
type ExpertCapabilityRule struct {
	Capability string            `json:"capability"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
	Effect     string            `json:"effect"`
}

var (
	expertCapabilityRegistryOnce sync.Once
	expertCapabilityRegistry     *tool.CapabilityRegistry
)

func expertCapabilityPolicyRegistry() *tool.CapabilityRegistry {
	expertCapabilityRegistryOnce.Do(func() {
		expertCapabilityRegistry = newIMSemanticCapabilityRegistry()
	})
	return expertCapabilityRegistry
}

func validateExpertStoreCapabilityRules(experts []ExpertDefinition) error {
	for _, def := range experts {
		if err := validateExpertCapabilityRules(def.CapabilityRules); err != nil {
			return fmt.Errorf("expert %q: %w", strings.TrimSpace(def.ID), err)
		}
	}
	return nil
}

func validateExpertCapabilityRules(rules []ExpertCapabilityRule) error {
	if len(rules) == 0 {
		return nil
	}
	registry := expertCapabilityPolicyRegistry()
	seen := make(map[string]struct{}, len(rules))
	for i, rule := range rules {
		capability := strings.TrimSpace(rule.Capability)
		effect := strings.TrimSpace(rule.Effect)
		if capability == "" {
			return fmt.Errorf("capability rule %d is missing a capability id", i)
		}
		if effect != "deny" && effect != "require_confirmation" {
			return fmt.Errorf("capability %q effect %q is not deny or require_confirmation", capability, effect)
		}
		descriptor, ok := registry.Lookup(tool.CapabilityID(capability))
		if !ok || descriptor.Deprecated {
			return fmt.Errorf("unknown capability %q", capability)
		}
		for qualifier, value := range rule.Qualifiers {
			constraint, declared := descriptor.Qualifiers[qualifier]
			if !declared {
				return fmt.Errorf("capability %q does not declare qualifier %q", capability, qualifier)
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("capability %q qualifier %q is empty", capability, qualifier)
			}
			if len(constraint.Values) > 0 && !expertQualifierValueAllowed(constraint.Values, value) {
				return fmt.Errorf("capability %q qualifier %q value %q is not allowed", capability, qualifier, value)
			}
		}
		key := capability + "\x00" + effect + "\x00" + expertQualifierKey(rule.Qualifiers)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate capability rule for %q", capability)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func expertQualifierValueAllowed(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func expertQualifierKey(qualifiers map[string]string) string {
	if len(qualifiers) == 0 {
		return ""
	}
	parts := make([]string, 0, len(qualifiers))
	for key, value := range qualifiers {
		parts = append(parts, key+"="+value)
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func expertCapabilityPolicyConstraintsForUser(userID string) ([]tool.RoutingConstraint, error) {
	id := expertIDFromUserID(userID)
	if id != "" {
		def, ok, err := defaultExpertStore.Get(id)
		if err != nil {
			return nil, err
		}
		if ok {
			return expertCapabilityPolicyConstraints(&def)
		}
	}
	return expertCapabilityPolicyConstraints(expertDefForUserID(userID))
}

func expertCapabilityPolicyConstraints(def *ExpertDefinition) ([]tool.RoutingConstraint, error) {
	if def == nil || len(def.CapabilityRules) == 0 {
		return nil, nil
	}
	if err := validateExpertCapabilityRules(def.CapabilityRules); err != nil {
		return nil, err
	}
	constraints := make([]tool.RoutingConstraint, 0, len(def.CapabilityRules))
	for i, rule := range def.CapabilityRules {
		constraints = append(constraints, tool.RoutingConstraint{
			ID:         fmt.Sprintf("expert:%s:cap-%d", strings.TrimSpace(def.ID), i),
			Capability: tool.CapabilityID(strings.TrimSpace(rule.Capability)),
			Effect:     strings.TrimSpace(rule.Effect),
			Authority:  tool.AuthorityPolicy,
			Attributes: cloneExpertQualifiers(rule.Qualifiers),
		})
	}
	return constraints, nil
}

func cloneExpertCapabilityRules(in []ExpertCapabilityRule) []ExpertCapabilityRule {
	if len(in) == 0 {
		return nil
	}
	out := make([]ExpertCapabilityRule, len(in))
	for i, rule := range in {
		out[i] = ExpertCapabilityRule{
			Capability: strings.TrimSpace(rule.Capability),
			Effect:     strings.TrimSpace(rule.Effect),
			Qualifiers: cloneExpertQualifiers(rule.Qualifiers),
		}
	}
	return out
}

func cloneExpertQualifiers(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
