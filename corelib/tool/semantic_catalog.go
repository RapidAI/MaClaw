package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// CapabilityID names a user-visible outcome, independent of a concrete tool.
// It is intentionally not a tool name: tools, skills, and MCP implementations
// may provide the same capability.
type CapabilityID string

// EffectClass is the maximum externally observable effect an implementation can
// have. Values are ordered by the planner's policy, not lexicographically.
type EffectClass string

const (
	EffectReadOnly       EffectClass = "read_only"
	EffectLocalMutation  EffectClass = "local_mutation"
	EffectExternalEffect EffectClass = "external_effect"
	EffectSensitive      EffectClass = "sensitive"
)

// CapabilityDescriptor is the governed contract for a capability.
// Qualifiers are intentionally explicit: an unregistered qualifier cannot
// influence a selection or become an implicit authorisation channel.
type CapabilityDescriptor struct {
	ID         CapabilityID
	Version    string
	Summary    string
	Qualifiers map[string]QualifierConstraint
	Effects    []EffectClass
	// Consumes and Produces are the reviewed artifact vocabulary for this
	// capability. A dynamic provider contract may declare only entries from
	// these lists; discovery and a control-plane caller cannot introduce a new
	// artifact kind/MIME contract while publishing a binding.
	Consumes    []ArtifactContract
	Produces    []ArtifactContract
	Owner       string
	Deprecated  bool
	Replacement CapabilityID
}

// QualifierConstraint describes a supported qualifier. An empty Values set
// means any non-empty normalised value is permitted.
type QualifierConstraint struct {
	Values   []string
	Required bool
}

// CapabilityRegistry is the single governed source for capability contracts.
// It deliberately has no knowledge of request text or tool names.
type CapabilityRegistry struct {
	mu          sync.RWMutex
	version     string
	descriptors map[CapabilityID]CapabilityDescriptor
	sealed      bool
}

func NewCapabilityRegistry(version string) *CapabilityRegistry {
	return &CapabilityRegistry{version: strings.TrimSpace(version), descriptors: make(map[CapabilityID]CapabilityDescriptor)}
}

func (r *CapabilityRegistry) Version() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// Register validates and publishes one descriptor. Callers must publish a new
// registry version for a semantic contract change rather than mutate an old ID.
func (r *CapabilityRegistry) Register(d CapabilityDescriptor) error {
	if r == nil {
		return fmt.Errorf("nil capability registry")
	}
	d.ID = CapabilityID(strings.TrimSpace(string(d.ID)))
	if d.ID == "" {
		return fmt.Errorf("capability id is required")
	}
	if strings.TrimSpace(d.Version) == "" {
		return fmt.Errorf("capability %q version is required", d.ID)
	}
	if len(d.Effects) == 0 {
		return fmt.Errorf("capability %q effects are required", d.ID)
	}
	if err := validateEffectClasses(d.Effects); err != nil {
		return fmt.Errorf("capability %q effects: %w", d.ID, err)
	}
	if err := validateCapabilityDescriptorArtifacts(d.Consumes); err != nil {
		return fmt.Errorf("capability %q consumes: %w", d.ID, err)
	}
	if err := validateCapabilityDescriptorArtifacts(d.Produces); err != nil {
		return fmt.Errorf("capability %q produces: %w", d.ID, err)
	}
	if d.Qualifiers == nil {
		d.Qualifiers = map[string]QualifierConstraint{}
	}
	for name, rule := range d.Qualifiers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("capability %q has an empty qualifier", d.ID)
		}
		for _, value := range rule.Values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("capability %q qualifier %q has an empty value", d.ID, name)
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return fmt.Errorf("capability registry %q is sealed", r.version)
	}
	if _, exists := r.descriptors[d.ID]; exists {
		return fmt.Errorf("capability %q is already registered", d.ID)
	}
	r.descriptors[d.ID] = cloneCapabilityDescriptor(d)
	return nil
}

// Seal makes a reviewed capability registry immutable. Dynamic provider
// contracts must be validated against a sealed registry so a runtime
// installation, discovery result, or accidental late registration cannot
// expand the vocabulary that a control-plane publisher is allowed to grant.
// A new capability contract requires construction and review of a new
// versioned registry rather than mutation of an in-use one.
func (r *CapabilityRegistry) Seal() error {
	if r == nil {
		return fmt.Errorf("nil capability registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(r.version) == "" {
		return fmt.Errorf("capability registry version is required")
	}
	if len(r.descriptors) == 0 {
		return fmt.Errorf("capability registry %q has no descriptors", r.version)
	}
	r.sealed = true
	return nil
}

// Sealed reports whether this registry can safely be used as the immutable
// semantic vocabulary for a control-plane contract publisher.
func (r *CapabilityRegistry) Sealed() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sealed
}

func (r *CapabilityRegistry) Lookup(id CapabilityID) (CapabilityDescriptor, bool) {
	if r == nil {
		return CapabilityDescriptor{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.descriptors[id]
	return cloneCapabilityDescriptor(d), ok
}

func cloneCapabilityDescriptor(in CapabilityDescriptor) CapabilityDescriptor {
	out := in
	out.Effects = append([]EffectClass(nil), in.Effects...)
	out.Consumes = append([]ArtifactContract(nil), in.Consumes...)
	out.Produces = append([]ArtifactContract(nil), in.Produces...)
	out.Qualifiers = make(map[string]QualifierConstraint, len(in.Qualifiers))
	for name, rule := range in.Qualifiers {
		rule.Values = append([]string(nil), rule.Values...)
		out.Qualifiers[name] = rule
	}
	return out
}

func validateCapabilityDescriptorArtifacts(contracts []ArtifactContract) error {
	seen := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		kind := strings.ToLower(strings.TrimSpace(contract.Kind))
		mimeType := strings.ToLower(strings.TrimSpace(contract.MIMEType))
		if kind == "" {
			return fmt.Errorf("artifact kind is required")
		}
		key := kind + "\x00" + mimeType + "\x00" + fmt.Sprintf("%t", contract.Required)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate artifact contract %q", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// ProviderBinding is the immutable identity of one implementation in a catalog
// snapshot. ProviderID and ImplementationID must never be inferred from an LLM
// function name during execution.
type ProviderBinding struct {
	Kind              string // builtin, skill, mcp, channel
	ProviderID        string
	ImplementationID  string
	SchemaDigest      string
	CatalogGeneration uint64
}

func (b ProviderBinding) StableID() string {
	return strings.Join([]string{strings.TrimSpace(b.Kind), strings.TrimSpace(b.ProviderID), strings.TrimSpace(b.ImplementationID), strings.TrimSpace(b.SchemaDigest)}, ":")
}

// CapabilityProvision is one exact capability offered by a provider.
type CapabilityProvision struct {
	Capability CapabilityID
	Qualifiers map[string]string
	Quality    float64
}

// ArtifactContract describes typed data that may cross a plan boundary. It is
// deliberately provider-independent: an executor transports an ArtifactRef,
// never a provider-defined local path or an unverified result string.
type ArtifactContract struct {
	Kind     string
	MIMEType string
	Required bool
}

// ProviderClassification is the explicit coverage class of one catalog
// entry. Every published implementation is exactly one of: a provision
// provider that may enter planning, an approved fixed control-plane entry
// that is not part of the user-task tool surface, or a quarantined entry
// that must never be planned. A publish without one of these states is
// unclassified and fails closed.
type ProviderClassification string

const (
	// ProviderClassProvision is the default class: the provider carries at
	// least one capability provision and may become a planner candidate.
	ProviderClassProvision ProviderClassification = "provision"
	// ProviderClassFixedControlPlane marks an approved system control-plane
	// entry (for example a Skill/MCP compatibility gateway). It is catalog
	// coverage metadata only and must never become a planner candidate.
	ProviderClassFixedControlPlane ProviderClassification = "fixed_control_plane"
	// ProviderClassQuarantined marks an implementation whose identity, schema,
	// or trust contract is not yet verified. It must never silently fall back
	// to a plannable default.
	ProviderClassQuarantined ProviderClassification = "quarantined"
)

// normalized returns the effective classification. The zero value means
// provision so existing catalog publishers stay valid; a provision-class
// provider still must carry at least one provision that resolves against the
// registry, otherwise Publish rejects it as unclassified.
func (c ProviderClassification) normalized() ProviderClassification {
	trimmed := ProviderClassification(strings.ToLower(strings.TrimSpace(string(c))))
	if trimmed == "" {
		return ProviderClassProvision
	}
	return trimmed
}

// valid reports whether the classification is one of the three governed
// states after zero-value normalisation.
func (c ProviderClassification) valid() bool {
	switch c.normalized() {
	case ProviderClassProvision, ProviderClassFixedControlPlane, ProviderClassQuarantined:
		return true
	}
	return false
}

// plannable reports whether entries of this class may become planner
// candidates. Fixed control-plane and quarantined entries are coverage
// records only.
func (c ProviderClassification) plannable() bool {
	return c.normalized() == ProviderClassProvision
}

// ProviderSpec is catalog metadata only. AdapterName is the trusted runtime
// adapter identity, not evidence that a request needs this provider.
type ProviderSpec struct {
	AdapterName string
	Binding     ProviderBinding
	// Classification is the explicit coverage class of this entry. The zero
	// value means provision for backwards compatibility; Publish normalises
	// it to an explicit value in the stored snapshot.
	Classification ProviderClassification
	// ParameterAuthorization is the catalog-time server-generated projection
	// for the trusted invocation schema. It is copied into selections and
	// grants so execution cannot silently accept a schema that drifted after
	// planning.
	ParameterAuthorization ParameterAuthorization
	Provides               []CapabilityProvision
	Consumes               []ArtifactContract
	Produces               []ArtifactContract
	Effects                []EffectClass
	Ready                  bool
	ReadyUntil             time.Time
	ChannelScopes          []string
}

// ToolCatalogSnapshot is immutable after publication. It can safely be kept by
// a plan while a subsequent discovery or health refresh publishes a new one.
type ToolCatalogSnapshot struct {
	Generation      uint64
	RegistryVersion string
	CreatedAt       time.Time
	Providers       []ProviderSpec
	// Coverage tells the planner whether this snapshot is a complete view of
	// the governed inventory for the current request scope. An empty provider
	// list without coverage is not evidence that a capability is unavailable:
	// a dynamic Skill/MCP refresh may still be in progress. Legacy Publish
	// callers receive explicit Complete coverage; dynamic hosts must publish
	// their observed readiness through PublishWithCoverage.
	Coverage CatalogCoverage
}

// CatalogCoverageState distinguishes an actually infeasible need from a
// catalog that has not finished a trusted lifecycle refresh. It is catalog
// state, never an LLM-provided fact and never a reason to expose a discovery
// or provider-name gateway.
type CatalogCoverageState string

const (
	CatalogCoverageComplete   CatalogCoverageState = "complete"
	CatalogCoverageIncomplete CatalogCoverageState = "incomplete"
	CatalogCoverageStale      CatalogCoverageState = "stale"
)

// Catalog coverage reasons are a deliberately closed diagnostic vocabulary.
// Lifecycle implementations must not copy transport errors, provider metadata,
// or other untrusted text into a route snapshot.
const (
	CatalogCoverageReasonIncomplete = "catalog_incomplete"
	CatalogCoverageReasonNotReady   = "provider_not_ready"
	CatalogCoverageReasonStale      = "catalog_stale"
)

// CatalogCoverageFamily is the lifecycle watermark for one implementation
// kind (for example, a Skill, MCP, channel, or builtin family). Kind is an
// implementation class, never a concrete provider identity. Keeping these
// watermarks separate prevents an unavailable unrelated family from hiding a
// ready candidate in another family, while still making a missing candidate
// fail closed when any potentially relevant family is incomplete.
//
// StaleUntil is an explicit stale-while-revalidate deadline. It may only be
// used for read-only candidates; external and sensitive candidates require
// complete coverage irrespective of this deadline.
type CatalogCoverageFamily struct {
	Kind       string
	State      CatalogCoverageState
	ReasonCode string
	ObservedAt time.Time
	StaleUntil time.Time
}

// CatalogCoverage is deliberately small and request-neutral. Provider names,
// descriptions and schemas stay inside the immutable snapshot; a reason code
// is only a bounded diagnostic projection such as provider_not_ready.
type CatalogCoverage struct {
	State      CatalogCoverageState
	ReasonCode string
	ObservedAt time.Time
	StaleUntil time.Time
	// Families is optional for legacy, scope-wide catalog publishers. New
	// dynamic publishers populate one entry per participating provider kind.
	Families []CatalogCoverageFamily
}

// ForProviderKind returns the lifecycle that governs a concrete provider
// kind. Legacy snapshots have no family watermarks and therefore use their
// scope-wide state. The returned value is a value copy and safe to retain.
func (c CatalogCoverage) ForProviderKind(kind string) CatalogCoverage {
	kind = canonicalCatalogCoverageKind(kind)
	for _, family := range c.Families {
		if canonicalCatalogCoverageKind(family.Kind) == kind {
			return CatalogCoverage{
				State: family.State, ReasonCode: family.ReasonCode,
				ObservedAt: family.ObservedAt, StaleUntil: family.StaleUntil,
			}
		}
	}
	if len(c.Families) > 0 {
		// A family watermark is an explicit declaration of the full dynamic
		// inventory scope. A provider kind absent from that declaration is not
		// covered by the snapshot and must never inherit the aggregate state.
		return CatalogCoverage{State: CatalogCoverageIncomplete, ReasonCode: CatalogCoverageReasonIncomplete}
	}
	return CatalogCoverage{State: c.State, ReasonCode: c.ReasonCode, ObservedAt: c.ObservedAt, StaleUntil: c.StaleUntil}
}

// UnavailabilityReason reports the bounded lifecycle reason when a need has
// no eligible provider. A complete catalog is evidence of infeasibility; an
// incomplete/stale family remains an availability condition instead. This is
// intentionally conservative because a need does not name its provider kind.
func (c CatalogCoverage) UnavailabilityReason() string {
	if len(c.Families) > 0 {
		for _, family := range c.Families {
			if family.State == CatalogCoverageIncomplete && family.ReasonCode == CatalogCoverageReasonNotReady {
				return CatalogCoverageReasonNotReady
			}
		}
		for _, family := range c.Families {
			if family.State == CatalogCoverageIncomplete {
				return CatalogCoverageReasonIncomplete
			}
		}
		for _, family := range c.Families {
			if family.State == CatalogCoverageStale {
				return CatalogCoverageReasonStale
			}
		}
	}
	if c.State == CatalogCoverageIncomplete || c.State == CatalogCoverageStale {
		return c.ReasonCode
	}
	return ""
}

func canonicalCatalogCoverageKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func (s ToolCatalogSnapshot) ProviderByStableID(id string) (ProviderSpec, bool) {
	for _, provider := range s.Providers {
		if provider.Binding.StableID() == id {
			return cloneProviderSpec(provider), true
		}
	}
	return ProviderSpec{}, false
}

// ToolCatalog atomically publishes validated provider snapshots. Discovery and
// readiness refreshes update this catalog; request routing only reads snapshots.
type ToolCatalog struct {
	mu       sync.RWMutex
	registry *CapabilityRegistry
	snapshot ToolCatalogSnapshot
}

func NewToolCatalog(registry *CapabilityRegistry) *ToolCatalog {
	return &ToolCatalog{registry: registry}
}

func (c *ToolCatalog) Snapshot() ToolCatalogSnapshot {
	if c == nil {
		return ToolCatalogSnapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneCatalogSnapshot(c.snapshot)
}

// Publish validates every provider against the same CapabilityRegistry and
// creates one new immutable generation. Invalid refreshes leave the previous
// snapshot untouched.
func (c *ToolCatalog) Publish(providers []ProviderSpec, now time.Time) (ToolCatalogSnapshot, error) {
	return c.PublishWithCoverage(providers, CatalogCoverage{State: CatalogCoverageComplete}, now)
}

// PublishWithCoverage atomically publishes providers and their lifecycle
// completeness. A caller must not manufacture Complete merely because its
// current list is empty; dynamic lifecycle owners publish Incomplete/Stale
// until they have a verified inventory result for the request scope.
func (c *ToolCatalog) PublishWithCoverage(providers []ProviderSpec, coverage CatalogCoverage, now time.Time) (ToolCatalogSnapshot, error) {
	if c == nil || c.registry == nil {
		return ToolCatalogSnapshot{}, fmt.Errorf("tool catalog requires a capability registry")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	coverage, err := normalizeCatalogCoverage(coverage, now)
	if err != nil {
		return ToolCatalogSnapshot{}, err
	}
	seenAdapters := make(map[string]bool, len(providers))
	seenBindings := make(map[string]bool, len(providers))
	validated := make([]ProviderSpec, 0, len(providers))
	for _, provider := range providers {
		provider.AdapterName = strings.TrimSpace(provider.AdapterName)
		if provider.AdapterName == "" {
			return ToolCatalogSnapshot{}, fmt.Errorf("provider adapter name is required")
		}
		if seenAdapters[provider.AdapterName] {
			return ToolCatalogSnapshot{}, fmt.Errorf("duplicate provider adapter %q", provider.AdapterName)
		}
		seenAdapters[provider.AdapterName] = true
		if strings.TrimSpace(provider.Binding.Kind) == "" || strings.TrimSpace(provider.Binding.ProviderID) == "" || strings.TrimSpace(provider.Binding.ImplementationID) == "" || strings.TrimSpace(provider.Binding.SchemaDigest) == "" {
			return ToolCatalogSnapshot{}, fmt.Errorf("provider %q binding is required", provider.AdapterName)
		}
		if strings.TrimSpace(provider.ParameterAuthorization.Digest) == "" || strings.TrimSpace(provider.ParameterAuthorization.CanonicalizerVer) == "" || provider.ParameterAuthorization.AllowedFields == nil {
			return ToolCatalogSnapshot{}, fmt.Errorf("provider %q parameter authorization is required", provider.AdapterName)
		}
		bindingID := provider.Binding.StableID()
		if seenBindings[bindingID] {
			return ToolCatalogSnapshot{}, fmt.Errorf("duplicate provider binding %q", bindingID)
		}
		seenBindings[bindingID] = true
		if !provider.Classification.valid() {
			return ToolCatalogSnapshot{}, fmt.Errorf("provider %q has unknown classification %q", provider.AdapterName, provider.Classification)
		}
		provider.Classification = provider.Classification.normalized()
		if provider.Classification == ProviderClassProvision {
			if len(provider.Provides) == 0 {
				return ToolCatalogSnapshot{}, fmt.Errorf("provider %q is unclassified: a provision-class provider requires at least one capability provision", provider.AdapterName)
			}
			if len(provider.Effects) == 0 {
				return ToolCatalogSnapshot{}, fmt.Errorf("provider %q has no effect declaration", provider.AdapterName)
			}
		} else if len(provider.Provides) > 0 {
			// A fixed control-plane or quarantined entry is coverage metadata
			// only; carrying a provision would make it a planner candidate and
			// reopen the implicit fallback this classification exists to close.
			return ToolCatalogSnapshot{}, fmt.Errorf("provider %q is classified %q and must not declare capability provisions", provider.AdapterName, provider.Classification)
		}
		if err := validateEffectClasses(provider.Effects); err != nil {
			return ToolCatalogSnapshot{}, fmt.Errorf("provider %q effects: %w", provider.AdapterName, err)
		}
		if err := validateArtifactContracts(provider.Consumes); err != nil {
			return ToolCatalogSnapshot{}, fmt.Errorf("provider %q consumes: %w", provider.AdapterName, err)
		}
		if err := validateArtifactContracts(provider.Produces); err != nil {
			return ToolCatalogSnapshot{}, fmt.Errorf("provider %q produces: %w", provider.AdapterName, err)
		}
		for _, provision := range provider.Provides {
			if math.IsNaN(provision.Quality) || math.IsInf(provision.Quality, 0) || provision.Quality < 0 {
				return ToolCatalogSnapshot{}, fmt.Errorf("provider %q capability %q has invalid quality", provider.AdapterName, provision.Capability)
			}
			descriptor, ok := c.registry.Lookup(provision.Capability)
			if !ok || descriptor.Deprecated {
				return ToolCatalogSnapshot{}, fmt.Errorf("provider %q declares unknown or deprecated capability %q", provider.AdapterName, provision.Capability)
			}
			if err := validateProvision(descriptor, provision); err != nil {
				return ToolCatalogSnapshot{}, fmt.Errorf("provider %q: %w", provider.AdapterName, err)
			}
		}
		validated = append(validated, cloneProviderSpec(provider))
	}
	sort.Slice(validated, func(i, j int) bool { return validated[i].AdapterName < validated[j].AdapterName })
	c.mu.Lock()
	defer c.mu.Unlock()
	generation := c.snapshot.Generation + 1
	for i := range validated {
		validated[i].Binding.CatalogGeneration = generation
	}
	c.snapshot = ToolCatalogSnapshot{Generation: generation, RegistryVersion: c.registry.Version(), CreatedAt: now, Providers: validated, Coverage: coverage}
	return cloneCatalogSnapshot(c.snapshot), nil
}

func normalizeCatalogCoverage(coverage CatalogCoverage, now time.Time) (CatalogCoverage, error) {
	coverage.State = CatalogCoverageState(strings.TrimSpace(string(coverage.State)))
	coverage.ReasonCode = strings.TrimSpace(coverage.ReasonCode)
	coverage.StaleUntil = coverage.StaleUntil.UTC()
	if coverage.ObservedAt.IsZero() {
		coverage.ObservedAt = now.UTC()
	} else {
		coverage.ObservedAt = coverage.ObservedAt.UTC()
	}
	if err := validateCatalogCoverageState(coverage.State, coverage.ReasonCode, coverage.StaleUntil); err != nil {
		return CatalogCoverage{}, err
	}
	if len(coverage.Families) == 0 {
		return coverage, nil
	}
	families := make([]CatalogCoverageFamily, 0, len(coverage.Families))
	seenKinds := make(map[string]bool, len(coverage.Families))
	for _, family := range coverage.Families {
		family.Kind = canonicalCatalogCoverageKind(family.Kind)
		family.State = CatalogCoverageState(strings.TrimSpace(string(family.State)))
		family.ReasonCode = strings.TrimSpace(family.ReasonCode)
		family.StaleUntil = family.StaleUntil.UTC()
		if family.Kind == "" {
			return CatalogCoverage{}, fmt.Errorf("catalog coverage family kind is required")
		}
		if seenKinds[family.Kind] {
			return CatalogCoverage{}, fmt.Errorf("duplicate catalog coverage family %q", family.Kind)
		}
		seenKinds[family.Kind] = true
		if family.ObservedAt.IsZero() {
			family.ObservedAt = coverage.ObservedAt
		} else {
			family.ObservedAt = family.ObservedAt.UTC()
		}
		if err := validateCatalogCoverageState(family.State, family.ReasonCode, family.StaleUntil); err != nil {
			return CatalogCoverage{}, fmt.Errorf("catalog coverage family %q: %w", family.Kind, err)
		}
		families = append(families, family)
	}
	sort.Slice(families, func(i, j int) bool { return families[i].Kind < families[j].Kind })
	coverage.Families = families
	return coverage, nil
}

func validateCatalogCoverageState(state CatalogCoverageState, reasonCode string, staleUntil time.Time) error {
	switch state {
	case CatalogCoverageComplete:
		if reasonCode != "" || !staleUntil.IsZero() {
			return fmt.Errorf("complete catalog coverage cannot have a reason code or stale deadline")
		}
	case CatalogCoverageIncomplete:
		if reasonCode != CatalogCoverageReasonIncomplete && reasonCode != CatalogCoverageReasonNotReady {
			return fmt.Errorf("incomplete catalog coverage requires a bounded reason code")
		}
		if !staleUntil.IsZero() {
			return fmt.Errorf("incomplete catalog coverage cannot have a stale deadline")
		}
	case CatalogCoverageStale:
		if reasonCode != CatalogCoverageReasonStale {
			return fmt.Errorf("stale catalog coverage requires catalog_stale reason code")
		}
		if staleUntil.IsZero() {
			return fmt.Errorf("stale catalog coverage requires a stale deadline")
		}
	default:
		return fmt.Errorf("invalid catalog coverage state %q", state)
	}
	return nil
}

func validateProvision(descriptor CapabilityDescriptor, provision CapabilityProvision) error {
	for qualifier, value := range provision.Qualifiers {
		rule, ok := descriptor.Qualifiers[qualifier]
		if !ok {
			return fmt.Errorf("capability %q does not declare qualifier %q", descriptor.ID, qualifier)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("capability %q qualifier %q is empty", descriptor.ID, qualifier)
		}
		if len(rule.Values) > 0 && !containsFold(rule.Values, value) {
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

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func validateArtifactContracts(contracts []ArtifactContract) error {
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

func validateEffectClasses(effects []EffectClass) error {
	seen := make(map[EffectClass]bool, len(effects))
	for _, effect := range effects {
		switch effect {
		case EffectReadOnly, EffectLocalMutation, EffectExternalEffect, EffectSensitive:
		default:
			return fmt.Errorf("invalid effect class %q", effect)
		}
		if seen[effect] {
			return fmt.Errorf("duplicate effect class %q", effect)
		}
		seen[effect] = true
	}
	return nil
}

func cloneProviderSpec(in ProviderSpec) ProviderSpec {
	out := in
	out.ParameterAuthorization.AllowedFields = append([]string(nil), in.ParameterAuthorization.AllowedFields...)
	out.Effects = append([]EffectClass(nil), in.Effects...)
	out.ChannelScopes = append([]string(nil), in.ChannelScopes...)
	out.Consumes = append([]ArtifactContract(nil), in.Consumes...)
	out.Produces = append([]ArtifactContract(nil), in.Produces...)
	out.Provides = make([]CapabilityProvision, len(in.Provides))
	for i, provision := range in.Provides {
		out.Provides[i] = provision
		out.Provides[i].Qualifiers = cloneStringMap(provision.Qualifiers)
	}
	return out
}

func cloneCatalogSnapshot(in ToolCatalogSnapshot) ToolCatalogSnapshot {
	out := in
	out.Coverage.ObservedAt = in.Coverage.ObservedAt.UTC()
	out.Coverage.StaleUntil = in.Coverage.StaleUntil.UTC()
	out.Coverage.Families = make([]CatalogCoverageFamily, len(in.Coverage.Families))
	for i, family := range in.Coverage.Families {
		family.ObservedAt = family.ObservedAt.UTC()
		family.StaleUntil = family.StaleUntil.UTC()
		out.Coverage.Families[i] = family
	}
	out.Providers = make([]ProviderSpec, len(in.Providers))
	for i, provider := range in.Providers {
		out.Providers[i] = cloneProviderSpec(provider)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// SchemaDigest returns a deterministic digest suitable for a provider binding.
// The caller should pass a canonical schema representation.
func SchemaDigest(schema []byte) string {
	sum := sha256.Sum256(schema)
	return hex.EncodeToString(sum[:])
}
