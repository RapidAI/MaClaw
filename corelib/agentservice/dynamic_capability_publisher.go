package agentservice

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
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

// MCPToolAnnotations carries the vendor-declared safety hints from an MCP
// tools/list response. They are the only signal the host has, before invoking
// an arbitrary unreviewed tool, about whether it reads or writes. A missing
// annotation is treated as "unknown", which the open lookup family resolves to
// an external effect so an untrusted vendor tool is never silently assumed
// side-effect-free.
type MCPToolAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
}

// ObservedMCPTool is one tools/list entry the host lifecycle already holds.
// The publisher accepts the name and schema only so it can compute the binding
// digest. Description and any caller-supplied digest are not inputs. Annotations
// are the read/write classification signal for the open lookup family.
// DeclaredCapabilities carries vendor self-declared external (x:) capability
// IDs the tool serves; the publisher registers each against the ontology (under
// the host default effect ceiling) and publishes a specific contract so the
// planner can name the tool by capability rather than only generic lookup.
type ObservedMCPTool struct {
	Name        string
	InputSchema map[string]interface{}
	Annotations  *MCPToolAnnotations
	DeclaredCapabilities []coretool.CapabilityID
}

// MCPToolObserver is a Service-free view of one server's last successful
// tools/list. observed is false on a cache miss; that must not be treated as
// an empty server. The observer does not discover or start a provider.
type MCPToolObserver interface {
	ObservedMCPTools(ctx context.Context, principal Principal, serverID string) ([]ObservedMCPTool, bool, error)
}

// SkillObserver is a Service-free view of the installed Skill set. observed
// is false when the host has not loaded that set. Package descriptions are
// not a capability source.
type SkillObserver interface {
	ObservedSkills(ctx context.Context, principal Principal) ([]corelib.NLSkillEntry, bool, error)
}

// ReviewedBindingSource returns the code-reviewed declaration templates.
// A test can replace it; production uses ReviewedMCPCapabilityContracts or
// ReviewedSkillCapabilityContracts. Templates must not carry a digest.
type ReviewedBindingSource func() map[string]DynamicCapabilityContract

// DynamicCapabilityContractPublisher publishes only declarations the host
// lifecycle has already reviewed. See the constructor comments above.
type DynamicCapabilityContractPublisher struct {
	svc           *Service
	registry      *coretool.CapabilityRegistry
	contracts     DynamicCapabilityContractRegistry
	mcp           MCPToolObserver
	skills        SkillObserver
	mcpBindings   ReviewedBindingSource
	skillBindings ReviewedBindingSource

	// declaredExternal tracks, per server, the external (x:) capabilities its
	// tools self-declared during the last successful observation. It is the
	// in-process source for ListInstalledNotReviewedProviders; a process
	// restart repopulates it on the next observation, and Revoke clears it.
	declaredExternalMu sync.Mutex
	declaredExternal   map[string][]coretool.CapabilityID
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

// NewLifecycleDynamicCapabilityContractPublisher binds publication to a host
// contract registry and a sealed vocabulary without requiring Service. The
// GUI lifecycle uses this. MCP names, Skill names, and caller-supplied digests
// still cannot create a capability: PublishReviewedMCPObservation and
// PublishReviewedSkillObservation copy only the reviewed templates and write
// the digest from the observation.
func NewLifecycleDynamicCapabilityContractPublisher(contracts DynamicCapabilityContractRegistry, registry *coretool.CapabilityRegistry, mcp MCPToolObserver, skills SkillObserver) (*DynamicCapabilityContractPublisher, error) {
	if contracts == nil {
		return nil, fmt.Errorf("dynamic capability publisher requires a contract registry")
	}
	if registry == nil || strings.TrimSpace(registry.Version()) == "" || !registry.Sealed() {
		return nil, fmt.Errorf("dynamic capability publisher requires a sealed, versioned capability registry")
	}
	return &DynamicCapabilityContractPublisher{
		registry:      registry,
		contracts:     contracts,
		mcp:           mcp,
		skills:        skills,
		mcpBindings:   ReviewedMCPCapabilityContracts,
		skillBindings: ReviewedSkillCapabilityContracts,
	}, nil
}

// UseReviewedBindings replaces the production templates. A nil map leaves
// that family on its current source. Tests pass a fixture map; production
// hosts do not call this.
func (p *DynamicCapabilityContractPublisher) UseReviewedBindings(mcp, skills map[string]DynamicCapabilityContract) {
	if p == nil {
		return
	}
	if mcp != nil {
		copied := cloneReviewedBindingTemplates(mcp)
		p.mcpBindings = func() map[string]DynamicCapabilityContract { return copied }
	}
	if skills != nil {
		copied := cloneReviewedBindingTemplates(skills)
		p.skillBindings = func() map[string]DynamicCapabilityContract { return copied }
	}
}

// PublishObservedMCP resolves the exact ready MCP tool from the Service-owned
// runtime inventory, or from the lifecycle observer when the host has no
// Service. It binds the contract to the current closed schema digest. The
// caller may name a binding to review, but a schema digest on the contract
// is discarded and computed here.
// mcpStableBindingKey is the grant-stable identity of one MCP binding. It
// excludes the observed schema digest so a vendor description or parameter
// label tweak (schema drift) does not rotate the contract Digest and retire
// in-flight grants. A capability/effect/tool-name change still rotates it.
func mcpStableBindingKey(globalKey string, capability coretool.CapabilityID, toolName string) string {
	return strings.Join([]string{strings.TrimSpace(globalKey), strings.TrimSpace(string(capability)), strings.TrimSpace(toolName)}, ":")
}

// skillStableBindingKey is the grant-stable identity of one Skill binding.
// Version and content digest are included so a content/version change still
// retires grants (Skill updates are versioned, not free-form schema tweaks).
func skillStableBindingKey(stableID, version, contentDigest string) string {
	return strings.Join([]string{strings.TrimSpace(stableID), strings.TrimSpace(version), strings.TrimSpace(contentDigest)}, ":")
}

func (p *DynamicCapabilityContractPublisher) PublishObservedMCP(ctx context.Context, principal Principal, serverID, toolName string, contract DynamicCapabilityContract) error {
	tool, err := p.observeMCPTool(ctx, principal, serverID, toolName)
	if err != nil {
		return err
	}
	contract.ObservedBindingDigest = dynamicMCPObservedBindingDigest(serverID, tool.Name, tool.InputSchema)
	if len(contract.Provisions) > 0 {
		contract.StableBindingKey = mcpStableBindingKey("", contract.Provisions[0].Capability, tool.Name)
	}
	return p.publishObservedMCP(principal, serverID, tool.Name, contract)
}

// PublishObservedSkill binds a reviewed declaration to the installed Skill's
// immutable stable ID plus content/version digest. It never accepts package
// descriptions, triggers, or market metadata as a source of capability.
func (p *DynamicCapabilityContractPublisher) PublishObservedSkill(ctx context.Context, principal Principal, stableID string, contract DynamicCapabilityContract) error {
	entry, err := p.observeSkill(ctx, principal, stableID)
	if err != nil {
		return err
	}
	contract.ObservedBindingDigest = dynamicSkillObservedBindingDigest(skillStableID(entry), entry.Version, skillContentDigest(entry))
	contract.StableBindingKey = skillStableBindingKey(skillStableID(entry), entry.Version, skillContentDigest(entry))
	return p.publishObservedSkill(principal, skillStableID(entry), contract)
}

// PublishReviewedMCPObservation publishes the reviewed subset of one server's
// tools/list and revokes reviewed names that this observation no longer
// contains. Call it only after a real observation. A cache miss must not call
// it: an empty slice is a real empty server and does revoke. Tools whose
// names are not in the server-agnostic reviewed map stay unpublished unless
// they are admitted by the open read-only family (see
// PublishReviewedMCPObservationForCapabilityWithOpenLookup). Digests are
// computed here; templates that already carry one are overwritten. Servers
// with no capability identity cannot match a product-scoped binding.
func (p *DynamicCapabilityContractPublisher) PublishReviewedMCPObservation(principal Principal, serverID string, tools []ObservedMCPTool) error {
	return p.PublishReviewedMCPObservationForCapability(principal, serverID, MCPCapabilityIdentity{}, tools)
}

// PublishReviewedMCPObservationForCapability is PublishReviewedMCPObservation
// with the installed server's capability identity. The identity only selects
// a code-reviewed binding. It does not supply provisions, effects, or quality.
func (p *DynamicCapabilityContractPublisher) PublishReviewedMCPObservationForCapability(principal Principal, serverID string, identity MCPCapabilityIdentity, tools []ObservedMCPTool) error {
	return p.publishReviewedMCPObservationForCapability(principal, serverID, identity, tools, nil)
}

// PublishReviewedMCPObservationForCapabilityWithOpenLookup is the host entry
// point for a finished, successful tools/list observation. Besides the
// reviewed names, every remaining tool on a server whose health observation
// succeeded is published under the open read-only family, so a server the
// reviewers have never seen is still callable rather than invisible.
//
// openLookupReady reports health for one server as the host observed it. It is
// called only for tools with no reviewed name, and a false result leaves that
// tool unpublished: an unfinished, degraded, or failed observation is not
// evidence that the tool is ready to be invoked.
func (p *DynamicCapabilityContractPublisher) PublishReviewedMCPObservationForCapabilityWithOpenLookup(
	principal Principal, serverID string, identity MCPCapabilityIdentity, tools []ObservedMCPTool, openLookupReady func(serverID string) bool,
) error {
	return p.publishReviewedMCPObservationForCapability(principal, serverID, identity, tools, openLookupReady)
}

func (p *DynamicCapabilityContractPublisher) publishReviewedMCPObservationForCapability(
	principal Principal, serverID string, identity MCPCapabilityIdentity, tools []ObservedMCPTool, openLookupReady func(serverID string) bool,
) error {
	if p == nil || p.contracts == nil || p.registry == nil {
		return fmt.Errorf("dynamic capability publisher is unavailable")
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return fmt.Errorf("MCP contract requires server identity")
	}
	reviewed := p.mcpContractsForObservation(identity)
	seen := make(map[string]ObservedMCPTool, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = tool
	}
	openReady := openLookupReady != nil && openLookupReady(serverID)
	var errs []error
	// A tool that stops appearing must lose its capability, including one
	// published under the open family, where no reviewed name exists to
	// iterate over and therefore nothing would ever revoke it. Clearing this
	// server before republishing from this observation is what makes that
	// true for arbitrary names; a server change also invalidates the
	// transport/schema identity beneath every tool, so nothing here may
	// inherit a stale declaration. Clearing after publishing would discard
	// the contracts this pass just wrote.
	if err := p.contracts.RevokeMCPServerContracts(principal, serverID); err != nil {
		errs = append(errs, err)
	}
	// A fresh observation resets the server's installed-not-reviewed tracking;
	// the loop below repopulates it from this observation's self-declarations.
	p.declaredExternalMu.Lock()
	if p.declaredExternal == nil {
		p.declaredExternal = make(map[string][]coretool.CapabilityID)
	}
	p.declaredExternal[serverID] = nil
	p.declaredExternalMu.Unlock()
	for name, tool := range seen {
		template, ok := reviewed[name]
		if !ok {
			// No reviewed name. The tool is callable only through the open
			// read-only family, and only when this server's observation
			// actually succeeded. A tool that later gets reviewed keeps its
			// specific contract: the open provision is quality 1, so every
			// reviewed and host-owned provision outranks it.
			if !openReady {
				continue
			}
			template = ReviewedMCPOpenLookupContract()
		}
		contract := cloneDynamicCapabilityContract(template)
		contract.ObservedBindingDigest = dynamicMCPObservedBindingDigest(serverID, name, tool.InputSchema)
		if len(contract.Provisions) > 0 {
			contract.StableBindingKey = mcpStableBindingKey(identity.GlobalKey, contract.Provisions[0].Capability, name)
		}
		// Narrow the open family's effect envelope to this tool. A
		// read-only-annotated tool stays on the silent read path; a write or
		// unannotated tool is selected with EffectExternalEffect and settles
		// through the receipt boundary rather than running silently.
		if _, isOpen := reviewed[name]; !isOpen {
			contract.Effects = openLookupEffectsForTool(tool)
		}
		// Fold vendor self-declared external (x:) capabilities into the SAME
		// contract. PublishMCPContract is keyed by server+tool, so a second
		// publish would overwrite the open/reviewed contract. Each declared
		// capability is registered on the host registry (bounded by its
		// namespace effect ceiling) before becoming a provision; a
		// registration that exceeds the ceiling is rejected and recorded
		// (fail-closed: the vendor cap is not silently published).
		contract, declared, mergeErrs := p.mergeVendorDeclaredCapabilities(principal, serverID, tool, contract)
		errs = append(errs, mergeErrs...)
		if len(declared) > 0 {
			p.recordDeclaredExternal(serverID, declared)
		}
		if err := p.publishObservedMCP(principal, serverID, name, contract); err != nil {
			errs = append(errs, err)
		}
	}
	// A product-scoped binding is addressable only while the installed server
	// still presents that identity. An observation that contains the tool
	// without the identity must not leave it contracted.
	for _, binding := range ReviewedMCPCapabilityBindings() {
		name := strings.TrimSpace(binding.ToolName)
		if name == "" {
			continue
		}
		if _, ok := reviewed[name]; ok {
			continue
		}
		if _, ok := seen[name]; !ok {
			continue
		}
		if err := p.contracts.RevokeMCPContract(principal, serverID, name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// PublishReviewedSkillObservation is the Skill counterpart. Only exact stable
// IDs in the reviewed map are published. An observed list that omits a
// reviewed ID revokes it. Unlisted skills stay unpublished.
func (p *DynamicCapabilityContractPublisher) PublishReviewedSkillObservation(principal Principal, entries []corelib.NLSkillEntry) error {
	if p == nil || p.contracts == nil || p.registry == nil {
		return fmt.Errorf("dynamic capability publisher is unavailable")
	}
	reviewed := p.skillContractTemplates()
	seen := make(map[string]corelib.NLSkillEntry, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(skillStableID(entry))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = entry
	}
	var errs []error
	for id, entry := range seen {
		template, ok := reviewed[id]
		if !ok {
			continue
		}
		contract := cloneDynamicCapabilityContract(template)
		contract.ObservedBindingDigest = dynamicSkillObservedBindingDigest(skillStableID(entry), entry.Version, skillContentDigest(entry))
		contract.StableBindingKey = skillStableBindingKey(skillStableID(entry), entry.Version, skillContentDigest(entry))
		if err := p.publishObservedSkill(principal, id, contract); err != nil {
			errs = append(errs, err)
		}
	}
	for id := range reviewed {
		if _, ok := seen[id]; ok {
			continue
		}
		if err := p.contracts.RevokeSkillContract(principal, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RevokeObservedMCPServer drops every contract bound to one server after the
// server itself is removed or its transport identity changes. A cache miss
// is not a removal.
func (p *DynamicCapabilityContractPublisher) RevokeObservedMCPServer(principal Principal, serverID string) error {
	if p == nil || p.contracts == nil {
		return fmt.Errorf("dynamic capability publisher is unavailable")
	}
	p.declaredExternalMu.Lock()
	delete(p.declaredExternal, strings.TrimSpace(serverID))
	p.declaredExternalMu.Unlock()
	return p.contracts.RevokeMCPServerContracts(principal, serverID)
}

func (p *DynamicCapabilityContractPublisher) observeMCPTool(ctx context.Context, principal Principal, serverID, toolName string) (ObservedMCPTool, error) {
	if p == nil || (p.svc == nil && p.mcp == nil) {
		return ObservedMCPTool{}, fmt.Errorf("dynamic capability publisher is unavailable")
	}
	serverID = strings.TrimSpace(serverID)
	toolName = strings.TrimSpace(toolName)
	if p.svc != nil {
		tools, err := p.svc.GetMCPServerTools(ctx, principal, serverID)
		if err != nil {
			return ObservedMCPTool{}, fmt.Errorf("observe MCP binding: %w", err)
		}
		for _, tool := range tools {
			if strings.TrimSpace(tool.Name) != toolName {
				continue
			}
			return ObservedMCPTool{Name: tool.Name, InputSchema: tool.InputSchema}, nil
		}
		return ObservedMCPTool{}, fmt.Errorf("MCP tool %q/%q is not ready", serverID, toolName)
	}
	tools, observed, err := p.mcp.ObservedMCPTools(ctx, principal, serverID)
	if err != nil {
		return ObservedMCPTool{}, fmt.Errorf("observe MCP binding: %w", err)
	}
	if !observed {
		return ObservedMCPTool{}, fmt.Errorf("MCP server %q is not observed", serverID)
	}
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) != toolName {
			continue
		}
		return ObservedMCPTool{Name: strings.TrimSpace(tool.Name), InputSchema: tool.InputSchema}, nil
	}
	return ObservedMCPTool{}, fmt.Errorf("MCP tool %q/%q is not ready", serverID, toolName)
}

func (p *DynamicCapabilityContractPublisher) observeSkill(ctx context.Context, principal Principal, stableID string) (corelib.NLSkillEntry, error) {
	if p == nil || (p.svc == nil && p.skills == nil) {
		return corelib.NLSkillEntry{}, fmt.Errorf("dynamic capability publisher is unavailable")
	}
	stableID = strings.TrimSpace(stableID)
	if p.svc != nil {
		entries, err := p.svc.ListSkills(ctx, principal)
		if err != nil {
			return corelib.NLSkillEntry{}, fmt.Errorf("observe Skill binding: %w", err)
		}
		for _, entry := range entries {
			if strings.TrimSpace(skillStableID(entry)) != stableID {
				continue
			}
			return entry, nil
		}
		return corelib.NLSkillEntry{}, fmt.Errorf("Skill %q is not installed", stableID)
	}
	entries, observed, err := p.skills.ObservedSkills(ctx, principal)
	if err != nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("observe Skill binding: %w", err)
	}
	if !observed {
		return corelib.NLSkillEntry{}, fmt.Errorf("Skill inventory is not observed")
	}
	for _, entry := range entries {
		if strings.TrimSpace(skillStableID(entry)) != stableID {
			continue
		}
		return entry, nil
	}
	return corelib.NLSkillEntry{}, fmt.Errorf("Skill %q is not installed", stableID)
}

func (p *DynamicCapabilityContractPublisher) mcpContractTemplates() map[string]DynamicCapabilityContract {
	if p != nil && p.mcpBindings != nil {
		if templates := p.mcpBindings(); templates != nil {
			return templates
		}
	}
	return ReviewedMCPCapabilityContracts()
}

// mcpContractsForObservation joins the server-agnostic tool-name map with
// identity-scoped bindings that match this server. A scoped tool name that
// is already server-agnostic stays server-agnostic. An identity that does
// not match contributes no row.
func (p *DynamicCapabilityContractPublisher) mcpContractsForObservation(identity MCPCapabilityIdentity) map[string]DynamicCapabilityContract {
	out := cloneReviewedBindingTemplates(p.mcpContractTemplates())
	for _, binding := range ReviewedMCPCapabilityBindings() {
		name := strings.TrimSpace(binding.ToolName)
		if name == "" {
			continue
		}
		if _, exists := out[name]; exists {
			continue
		}
		if !identity.matchesReviewedMCP(binding) {
			continue
		}
		out[name] = cloneDynamicCapabilityContract(binding.Contract)
	}
	return out
}

func (p *DynamicCapabilityContractPublisher) skillContractTemplates() map[string]DynamicCapabilityContract {
	if p != nil && p.skillBindings != nil {
		if templates := p.skillBindings(); templates != nil {
			return templates
		}
	}
	return ReviewedSkillCapabilityContracts()
}

func cloneReviewedBindingTemplates(in map[string]DynamicCapabilityContract) map[string]DynamicCapabilityContract {
	out := make(map[string]DynamicCapabilityContract, len(in))
	for name, contract := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = cloneDynamicCapabilityContract(contract)
	}
	return out
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

// mergeVendorDeclaredCapabilities folds a tool's self-declared external (x:)
// capabilities into its single dynamic contract. Because PublishMCPContract
// replaces by server+tool, the open/reviewed provision and every vendor
// provision must share one contract. Each declared capability is registered on
// the host registry (bounded by its namespace effect ceiling) before being
// added as a provision; a registration that exceeds the ceiling is rejected
// and returned as an error so the caller can fail closed rather than publish a
// silently-escalated vendor cap.
func (p *DynamicCapabilityContractPublisher) mergeVendorDeclaredCapabilities(principal Principal, serverID string, tool ObservedMCPTool, contract DynamicCapabilityContract) (DynamicCapabilityContract, []coretool.CapabilityID, []error) {
	var errs []error
	declared := make([]coretool.CapabilityID, 0)
	seenCap := make(map[coretool.CapabilityID]bool, len(contract.Provisions))
	for _, prov := range contract.Provisions {
		seenCap[prov.Capability] = true
	}
	for _, raw := range tool.DeclaredCapabilities {
		capID := coretool.CapabilityID(strings.TrimSpace(string(raw)))
		if capID == "" {
			continue
		}
		if !strings.HasPrefix(string(capID), string(coretool.ExternalCapabilityPrefix)) {
			// Vendors must not claim builtin/reviewed capability IDs; those are
			// sourced only from the reviewed map / ontology. Ignore quietly.
			continue
		}
		if seenCap[capID] {
			continue
		}
		ns := externalNamespaceOf(capID)
		desc := coretool.CapabilityDescriptor{
			ID:      capID,
			Version: "v1",
			Owner:   "vendor",
			Summary: fmt.Sprintf("Vendor-declared capability %s from installed MCP %s/%s", capID, serverID, tool.Name),
			Effects: append([]coretool.EffectClass(nil), contract.Effects...),
		}
		if err := p.registry.RegisterExternalCapability(ns, desc); err != nil {
			errs = append(errs, fmt.Errorf("register vendor capability %q: %w", capID, err))
			continue
		}
		contract.Provisions = append(contract.Provisions, coretool.CapabilityProvision{Capability: capID, Quality: 1})
		declared = append(declared, capID)
		seenCap[capID] = true
	}
	return contract, declared, errs
}

func (p *DynamicCapabilityContractPublisher) recordDeclaredExternal(serverID string, caps []coretool.CapabilityID) {
	if len(caps) == 0 {
		return
	}
	p.declaredExternalMu.Lock()
	if p.declaredExternal == nil {
		p.declaredExternal = make(map[string][]coretool.CapabilityID)
	}
	existing := p.declaredExternal[serverID]
	for _, c := range caps {
		found := false
		for _, e := range existing {
			if e == c {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, c)
		}
	}
	p.declaredExternal[serverID] = existing
	p.declaredExternalMu.Unlock()
}

// InstalledNotReviewedProvider describes an installed MCP server that declared
// external (x:) capabilities but whose namespace effect ceiling is still the
// host default — i.e. the host has not yet reviewed/widened it. Surfacing this
// closes plan §2.1's "installed but not reviewed" gap: the provider is listed
// (not silently hidden) and can trigger a petition to widen its ceiling.
type InstalledNotReviewedProvider struct {
	ServerID            string
	DeclaredCapabilities []coretool.CapabilityID
	Namespace           string
	CeilingReviewed     bool
}

// ListInstalledNotReviewedProviders returns every installed MCP server that
// self-declared at least one external capability and has not yet been
// host-reviewed (its namespace ceiling is still the default). It is the
// consumer-facing query tools_search and the petition surface call.
func (p *DynamicCapabilityContractPublisher) ListInstalledNotReviewedProviders() []InstalledNotReviewedProvider {
	if p == nil {
		return nil
	}
	p.declaredExternalMu.Lock()
	defer p.declaredExternalMu.Unlock()
	out := make([]InstalledNotReviewedProvider, 0, len(p.declaredExternal))
	for serverID, caps := range p.declaredExternal {
		if len(caps) == 0 {
			continue
		}
		ns := externalNamespaceOf(caps[0])
		ceilingReviewed := false
		if p.registry != nil {
			ceilingReviewed = !p.registry.ExternalCeilingIsDefault(ns)
		}
		out = append(out, InstalledNotReviewedProvider{
			ServerID:            serverID,
			DeclaredCapabilities: append([]coretool.CapabilityID(nil), caps...),
			Namespace:           ns,
			CeilingReviewed:     ceilingReviewed,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServerID < out[j].ServerID })
	return out
}

// DiscoveryBudget is the remediation/recall allowance the discovery surface
// (tools_search / petition UI) should grant for the currently installed but
// not-yet-host-reviewed providers. Plan §2.4 requires this to scale with the
// number of unreviewed providers N so that "installing many MCPs" never
// silently starves recall: a fixed budget would let the first few providers
// consume the whole petition/search allowance and leave later installs
// undiscoverable.
type DiscoveryBudget struct {
	// PetitionSlots is how many host-review petitions the surface may open in
	// one pass (one per unreviewed provider, bounded by the cap).
	PetitionSlots int
	// SearchPasses is how many discovery/search iterations the surface may run
	// to surface unreviewed providers before yielding to the user.
	SearchPasses int
}

const (
	// discoveryBudgetBase is the floor allowance when no unreviewed provider
	// is installed: the surface can still petition/search for the builtin set.
	discoveryBudgetBase = 8
	// discoveryBudgetPerUnreviewed is the linear increment per unreviewed
	// provider (N in plan §2.4).
	discoveryBudgetPerUnreviewed = 2
	// discoveryBudgetCap prevents an unbounded N from exhausting the surface;
	// once the installed count crosses it the budget saturates.
	discoveryBudgetCap = 128
)

// RecommendedDiscoveryBudget computes the discovery/remediation allowance for
// unreviewedCount installed-but-not-reviewed providers. It is a pure function
// so both the publisher query and unit tests can call it without standing up
// an observation loop. The allowance grows linearly with N and saturates at
// discoveryBudgetCap.
func RecommendedDiscoveryBudget(unreviewedCount int) DiscoveryBudget {
	if unreviewedCount < 0 {
		unreviewedCount = 0
	}
	raw := discoveryBudgetBase + discoveryBudgetPerUnreviewed*unreviewedCount
	if raw > discoveryBudgetCap {
		raw = discoveryBudgetCap
	}
	return DiscoveryBudget{PetitionSlots: raw, SearchPasses: raw}
}

// RecommendedDiscoveryBudgetForInstalled derives the discovery budget from the
// live installed-not-reviewed list, closing plan §2.4's "budget scales with N"
// requirement end-to-end: the surface calls this once and gets an allowance that
// tracks how many providers were actually installed.
func (p *DynamicCapabilityContractPublisher) RecommendedDiscoveryBudgetForInstalled() DiscoveryBudget {
	if p == nil {
		return DiscoveryBudget{}
	}
	return RecommendedDiscoveryBudget(len(p.ListInstalledNotReviewedProviders()))
}

// externalNamespaceOf returns the namespace segment of an external capability
// ID: the token between ExternalCapabilityPrefix and the next colon.
func externalNamespaceOf(id coretool.CapabilityID) string {
	rest := strings.TrimPrefix(string(id), string(coretool.ExternalCapabilityPrefix))
	if i := strings.Index(rest, ":"); i >= 0 {
		return rest[:i]
	}
	return rest
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
