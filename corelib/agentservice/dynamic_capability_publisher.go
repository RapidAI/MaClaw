package agentservice

import (
	"context"
	"fmt"
	"math"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// DynamicCapabilityContractPublisher is the narrow control-plane authority
// that can publish a reviewed capability declaration for an already-known
// dynamic binding. It intentionally has no provider-discovery input: MCP
// names, Skill names, descriptions, triggers, schemas, and provider output
// cannot create a capability or alter its maximum effect.
//
// Hosts construct it from their Service and a sealed, versioned capability
// registry during bootstrap, then retain it in authenticated lifecycle code.
// It is not an Agent tool and must never be handed to request execution.
type DynamicCapabilityContractPublisher struct {
	svc       *Service
	registry  *coretool.CapabilityRegistry
	contracts DynamicCapabilityContractRegistry
}

// NewDynamicCapabilityContractPublisher binds publication to the Service's
// durable, principal-scoped registry. Requiring a sealed registry makes the
// capability vocabulary immutable for the publisher lifetime; upgrades must
// explicitly bootstrap a new registry version and publisher.
func NewDynamicCapabilityContractPublisher(svc *Service, registry *coretool.CapabilityRegistry) (*DynamicCapabilityContractPublisher, error) {
	if svc == nil || svc.dynamicCapabilities == nil {
		return nil, fmt.Errorf("dynamic capability publisher requires a service-owned registry")
	}
	if registry == nil || strings.TrimSpace(registry.Version()) == "" || !registry.Sealed() {
		return nil, fmt.Errorf("dynamic capability publisher requires a sealed, versioned capability registry")
	}
	return &DynamicCapabilityContractPublisher{svc: svc, registry: registry, contracts: svc.dynamicCapabilities}, nil
}

// PublishObservedMCP resolves the exact ready MCP tool from the Service-owned
// runtime inventory, binds the contract to its current closed schema digest,
// and only then publishes it. The caller may name a binding to review, but it
// cannot supply a schema digest or provider-produced capability declaration.
func (p *DynamicCapabilityContractPublisher) PublishObservedMCP(ctx context.Context, principal Principal, serverID, toolName string, contract DynamicCapabilityContract) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("dynamic capability publisher is unavailable")
	}
	tools, err := p.svc.GetMCPServerTools(ctx, principal, serverID)
	if err != nil {
		return fmt.Errorf("observe MCP binding: %w", err)
	}
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) != strings.TrimSpace(toolName) {
			continue
		}
		contract.ObservedBindingDigest = dynamicMCPObservedBindingDigest(serverID, tool.Name, tool.InputSchema)
		return p.publishObservedMCP(principal, serverID, tool.Name, contract)
	}
	return fmt.Errorf("MCP tool %q/%q is not ready", strings.TrimSpace(serverID), strings.TrimSpace(toolName))
}

// PublishObservedSkill binds a reviewed declaration to the installed Skill's
// immutable stable ID plus content/version digest. It never accepts package
// descriptions, triggers, or market metadata as a source of capability.
func (p *DynamicCapabilityContractPublisher) PublishObservedSkill(ctx context.Context, principal Principal, stableID string, contract DynamicCapabilityContract) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("dynamic capability publisher is unavailable")
	}
	entries, err := p.svc.ListSkills(ctx, principal)
	if err != nil {
		return fmt.Errorf("observe Skill binding: %w", err)
	}
	for _, entry := range entries {
		if strings.TrimSpace(skillStableID(entry)) != strings.TrimSpace(stableID) {
			continue
		}
		contract.ObservedBindingDigest = dynamicSkillObservedBindingDigest(skillStableID(entry), entry.Version, skillContentDigest(entry))
		return p.publishObservedSkill(principal, skillStableID(entry), contract)
	}
	return fmt.Errorf("Skill %q is not installed", strings.TrimSpace(stableID))
}

// publishObservedMCP is intentionally private: callers holding a publisher
// must use PublishObservedMCP so the binding digest is derived from the
// Service-owned ready inventory, never supplied in a request payload.
func (p *DynamicCapabilityContractPublisher) publishObservedMCP(principal Principal, serverID, toolName string, contract DynamicCapabilityContract) error {
	if err := p.validateContract(contract); err != nil {
		return fmt.Errorf("validate MCP capability contract: %w", err)
	}
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(toolName) == "" {
		return fmt.Errorf("MCP contract requires server and tool identity")
	}
	return p.contracts.PublishMCPContract(principal, serverID, toolName, contract)
}

// publishObservedSkill is the Skill counterpart to publishObservedMCP. Stable
// identity, version, and content digest must have been observed immediately
// before this write.
func (p *DynamicCapabilityContractPublisher) publishObservedSkill(principal Principal, stableID string, contract DynamicCapabilityContract) error {
	if err := p.validateContract(contract); err != nil {
		return fmt.Errorf("validate Skill capability contract: %w", err)
	}
	if strings.TrimSpace(stableID) == "" {
		return fmt.Errorf("Skill contract requires stable identity")
	}
	return p.contracts.PublishSkillContract(principal, stableID, contract)
}

func (p *DynamicCapabilityContractPublisher) validateContract(contract DynamicCapabilityContract) error {
	if p == nil || p.registry == nil || p.contracts == nil || !p.registry.Sealed() {
		return fmt.Errorf("dynamic capability publisher is unavailable")
	}
	if err := contract.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(contract.ObservedBindingDigest) == "" {
		return fmt.Errorf("dynamic capability contract requires an observed binding digest")
	}
	for _, provision := range contract.Provisions {
		if math.IsNaN(provision.Quality) || math.IsInf(provision.Quality, 0) || provision.Quality < 0 {
			return fmt.Errorf("capability %q has invalid quality", provision.Capability)
		}
		descriptor, ok := p.registry.Lookup(provision.Capability)
		if !ok || descriptor.Deprecated {
			return fmt.Errorf("unknown or deprecated capability %q", provision.Capability)
		}
		if err := validatePublisherProvision(descriptor, provision); err != nil {
			return err
		}
		for _, effect := range contract.Effects {
			if !publisherEffectAllowed(descriptor.Effects, effect) {
				return fmt.Errorf("capability %q does not permit effect %q", provision.Capability, effect)
			}
		}
	}
	if err := validatePublisherArtifactContracts(p.registry, contract.Provisions, contract.Consumes, contract.Produces); err != nil {
		return fmt.Errorf("artifact contracts: %w", err)
	}
	return nil
}

// validatePublisherArtifactContracts confines a dynamic binding to the union
// of artifact vocabularies reviewed for its declared capabilities. It lives at
// the trusted publication boundary so older static catalog callers retain
// their existing descriptor migration path.
func validatePublisherArtifactContracts(registry *coretool.CapabilityRegistry, provisions []coretool.CapabilityProvision, consumes, produces []coretool.ArtifactContract) error {
	if registry == nil {
		return fmt.Errorf("capability registry is unavailable")
	}
	allowedConsumes := make([]coretool.ArtifactContract, 0)
	allowedProduces := make([]coretool.ArtifactContract, 0)
	for _, provision := range provisions {
		descriptor, ok := registry.Lookup(provision.Capability)
		if !ok || descriptor.Deprecated {
			return fmt.Errorf("unknown or deprecated capability %q", provision.Capability)
		}
		allowedConsumes = append(allowedConsumes, descriptor.Consumes...)
		allowedProduces = append(allowedProduces, descriptor.Produces...)
	}
	if err := validatePublisherArtifactContractSet("consume", allowedConsumes, consumes); err != nil {
		return err
	}
	return validatePublisherArtifactContractSet("produce", allowedProduces, produces)
}

func validatePublisherArtifactContractSet(direction string, allowed, declared []coretool.ArtifactContract) error {
	for _, contract := range declared {
		if !publisherArtifactContractAllowed(allowed, contract) {
			return fmt.Errorf("%s artifact %q/%q required=%t is not reviewed", direction, strings.TrimSpace(contract.Kind), strings.TrimSpace(contract.MIMEType), contract.Required)
		}
	}
	return nil
}

func publisherArtifactContractAllowed(allowed []coretool.ArtifactContract, want coretool.ArtifactContract) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate.Kind), strings.TrimSpace(want.Kind)) &&
			strings.EqualFold(strings.TrimSpace(candidate.MIMEType), strings.TrimSpace(want.MIMEType)) &&
			candidate.Required == want.Required {
			return true
		}
	}
	return false
}

// validatePublisherProvision mirrors the shared catalog validation at the
// control-plane boundary. Keeping this check here prevents an invalid contract
// from being durably recorded only to be quarantined later during catalog
// publication.
func validatePublisherProvision(descriptor coretool.CapabilityDescriptor, provision coretool.CapabilityProvision) error {
	for qualifier, value := range provision.Qualifiers {
		rule, ok := descriptor.Qualifiers[qualifier]
		if !ok {
			return fmt.Errorf("capability %q does not declare qualifier %q", descriptor.ID, qualifier)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("capability %q qualifier %q is empty", descriptor.ID, qualifier)
		}
		if len(rule.Values) > 0 && !publisherContainsFold(rule.Values, value) {
			return fmt.Errorf("capability %q qualifier %q value %q is not allowed", descriptor.ID, qualifier, value)
		}
	}
	for qualifier, rule := range descriptor.Qualifiers {
		if rule.Required && strings.TrimSpace(provision.Qualifiers[qualifier]) == "" {
			return fmt.Errorf("capability %q requires qualifier %q", descriptor.ID, qualifier)
		}
	}
	return nil
}

func publisherContainsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func publisherEffectAllowed(allowed []coretool.EffectClass, effect coretool.EffectClass) bool {
	for _, candidate := range allowed {
		if candidate == effect {
			return true
		}
	}
	return false
}
