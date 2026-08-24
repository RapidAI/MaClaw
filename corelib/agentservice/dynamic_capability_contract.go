package agentservice

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// DynamicCapabilityContract is the declared routing contract for one concrete
// Skill or MCP implementation. Discovery metadata (names, descriptions and
// provider schemas) cannot create this contract: it must be supplied by a
// trusted control-plane publisher before the implementation may be rendered
// into an Agent tool surface.
//
// This is deliberately provider-neutral. MCP and Skill use the same contract
// shape so neither can acquire a privileged default path through a legacy
// gateway. Trust, isolation and full semantic-planner selection are added by
// the next migration layer; absence of this minimum contract is already a
// hard quarantine condition.
type DynamicCapabilityContract struct {
	Provisions []coretool.CapabilityProvision
	Effects    []coretool.EffectClass
	Consumes   []coretool.ArtifactContract
	Produces   []coretool.ArtifactContract
	// ObservedBindingDigest is written only by the trusted lifecycle publisher
	// after it has resolved one concrete live binding. It binds the declaration
	// to the observed MCP schema or immutable Skill content/version identity.
	// It is mandatory for every routable declaration. A missing, malformed, or
	// non-matching binding is quarantined by the runtime bridge. Tests that
	// construct catalog entries directly may omit it only because they do not
	// model a Service-owned provider lifecycle.
	ObservedBindingDigest string
}

func (c DynamicCapabilityContract) declared() bool {
	return len(c.Provisions) > 0 && len(c.Effects) > 0
}

func (c DynamicCapabilityContract) validate() error {
	if !c.declared() {
		return fmt.Errorf("dynamic capability contract is required")
	}
	for _, provision := range c.Provisions {
		if strings.TrimSpace(string(provision.Capability)) == "" {
			return fmt.Errorf("dynamic capability provision is required")
		}
		for qualifier, value := range provision.Qualifiers {
			if strings.TrimSpace(qualifier) == "" || strings.TrimSpace(value) == "" {
				return fmt.Errorf("dynamic capability qualifier is invalid")
			}
		}
	}
	seenEffects := make(map[coretool.EffectClass]bool, len(c.Effects))
	for _, effect := range c.Effects {
		switch effect {
		case coretool.EffectReadOnly, coretool.EffectLocalMutation, coretool.EffectExternalEffect, coretool.EffectSensitive:
		default:
			return fmt.Errorf("dynamic capability effect %q is invalid", effect)
		}
		if seenEffects[effect] {
			return fmt.Errorf("duplicate dynamic capability effect %q", effect)
		}
		seenEffects[effect] = true
	}
	if err := validateDynamicArtifactContracts(c.Consumes); err != nil {
		return fmt.Errorf("dynamic capability consumes: %w", err)
	}
	if err := validateDynamicArtifactContracts(c.Produces); err != nil {
		return fmt.Errorf("dynamic capability produces: %w", err)
	}
	return nil
}

func validateDynamicArtifactContracts(contracts []coretool.ArtifactContract) error {
	seen := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		kind := strings.ToLower(strings.TrimSpace(contract.Kind))
		mimeType := strings.ToLower(strings.TrimSpace(contract.MIMEType))
		if kind == "" {
			return fmt.Errorf("artifact kind is required")
		}
		key := kind + "\x00" + mimeType
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate artifact contract %q", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func cloneDynamicCapabilityContract(in DynamicCapabilityContract) DynamicCapabilityContract {
	out := in
	out.Effects = append([]coretool.EffectClass(nil), in.Effects...)
	out.Consumes = append([]coretool.ArtifactContract(nil), in.Consumes...)
	out.Produces = append([]coretool.ArtifactContract(nil), in.Produces...)
	out.Provisions = make([]coretool.CapabilityProvision, len(in.Provisions))
	for i, provision := range in.Provisions {
		out.Provisions[i] = provision
		if len(provision.Qualifiers) > 0 {
			out.Provisions[i].Qualifiers = make(map[string]string, len(provision.Qualifiers))
			for key, value := range provision.Qualifiers {
				out.Provisions[i].Qualifiers[key] = value
			}
		}
	}
	return out
}

// Digest is a stable identity component. It includes every declaration that
// affects semantic feasibility or the maximum observable effect, so a control
// plane contract change cannot silently inherit an old binding/grant.
func (c DynamicCapabilityContract) Digest() string {
	canonical := struct {
		Provisions []dynamicContractProvision `json:"provisions"`
		Effects    []string                   `json:"effects"`
		Consumes   []dynamicContractArtifact  `json:"consumes"`
		Produces   []dynamicContractArtifact  `json:"produces"`
		Binding    string                     `json:"observed_binding_digest,omitempty"`
	}{}
	canonical.Binding = strings.TrimSpace(c.ObservedBindingDigest)
	for _, provision := range c.Provisions {
		qualifiers := make(map[string]string, len(provision.Qualifiers))
		for key, value := range provision.Qualifiers {
			qualifiers[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
		canonical.Provisions = append(canonical.Provisions, dynamicContractProvision{Capability: strings.TrimSpace(string(provision.Capability)), Qualifiers: qualifiers, Quality: provision.Quality})
	}
	for _, effect := range c.Effects {
		canonical.Effects = append(canonical.Effects, string(effect))
	}
	for _, contract := range c.Consumes {
		canonical.Consumes = append(canonical.Consumes, dynamicContractArtifact{Kind: strings.ToLower(strings.TrimSpace(contract.Kind)), MIMEType: strings.ToLower(strings.TrimSpace(contract.MIMEType)), Required: contract.Required})
	}
	for _, contract := range c.Produces {
		canonical.Produces = append(canonical.Produces, dynamicContractArtifact{Kind: strings.ToLower(strings.TrimSpace(contract.Kind)), MIMEType: strings.ToLower(strings.TrimSpace(contract.MIMEType)), Required: contract.Required})
	}
	sort.Slice(canonical.Provisions, func(i, j int) bool { return canonical.Provisions[i].key() < canonical.Provisions[j].key() })
	sort.Strings(canonical.Effects)
	sort.Slice(canonical.Consumes, func(i, j int) bool { return canonical.Consumes[i].key() < canonical.Consumes[j].key() })
	sort.Slice(canonical.Produces, func(i, j int) bool { return canonical.Produces[i].key() < canonical.Produces[j].key() })
	data, err := json.Marshal(canonical)
	if err != nil {
		return coretool.SchemaDigest([]byte("invalid-dynamic-capability-contract"))
	}
	return coretool.SchemaDigest(data)
}

type dynamicContractProvision struct {
	Capability string            `json:"capability"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
	Quality    float64           `json:"quality"`
}

func (p dynamicContractProvision) key() string {
	keys := make([]string, 0, len(p.Qualifiers))
	for key := range p.Qualifiers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{p.Capability, fmt.Sprintf("%.9g", p.Quality)}
	for _, key := range keys {
		parts = append(parts, key+"="+p.Qualifiers[key])
	}
	return strings.Join(parts, "\x00")
}

type dynamicContractArtifact struct {
	Kind     string `json:"kind"`
	MIMEType string `json:"mime_type"`
	Required bool   `json:"required"`
}

func (a dynamicContractArtifact) key() string {
	return strings.Join([]string{a.Kind, a.MIMEType, fmt.Sprintf("%t", a.Required)}, "\x00")
}
