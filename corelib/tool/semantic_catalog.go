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
	// externalEffectCeilings overrides the default effect ceiling for a
	// specific external namespace (the token between ExternalCapabilityPrefix
	// and the next colon). Keys are namespace segments, not full capability IDs.
	externalEffectCeilings map[string][]EffectClass
}

func NewCapabilityRegistry(version string) *CapabilityRegistry {
	return &CapabilityRegistry{
		version:                strings.TrimSpace(version),
		descriptors:            make(map[CapabilityID]CapabilityDescriptor),
		externalEffectCeilings: make(map[string][]EffectClass),
	}
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
	if strings.HasPrefix(string(d.ID), string(ExternalCapabilityPrefix)) {
		return fmt.Errorf("capability %q uses the external %q prefix; register vendor capabilities via RegisterExternalCapability", d.ID, ExternalCapabilityPrefix)
	}
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

// effectWithinCeiling reports whether e is a member of ceiling.
func effectWithinCeiling(e EffectClass, ceiling []EffectClass) bool {
	for _, c := range ceiling {
		if c == e {
			return true
		}
	}
	return false
}

// EffectsWithinCeiling reports whether every member of effects is allowed by
// ceiling.
//
// This is the enforcement counterpart to ExternalEffectCeiling. A publisher
// that stamps a capability's registered descriptor with the namespace ceiling
// (so registrations stay identical and idempotent across servers) thereby makes
// RegisterExternalCapability's own ceiling check vacuous: comparing the ceiling
// against itself can never fail. Such a publisher MUST still call this against
// the per-provider envelope before registering, or it silently removes the
// per-provider ceiling check entirely and lets an installed server register a
// capability whose execution effects exceed what the host reviewed.
//
// An empty effects set is within every ceiling: it grants nothing. Callers that
// treat empty as "unspecified and therefore permitted" must reject it before
// reaching here.
func EffectsWithinCeiling(effects []EffectClass, ceiling []EffectClass) bool {
	for _, e := range effects {
		if !effectWithinCeiling(e, ceiling) {
			return false
		}
	}
	return true
}

// externalEffectCeiling returns the effect ceiling for a namespace, falling
// back to the package default when the namespace has no explicit override.
func (r *CapabilityRegistry) externalEffectCeiling(ns string) []EffectClass {
	if r == nil {
		return DefaultExternalCapabilityEffectCeiling
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if ceil, ok := r.externalEffectCeilings[ns]; ok {
		return ceil
	}
	return DefaultExternalCapabilityEffectCeiling
}

// ExternalEffectCeiling returns a copy of the effect ceiling that governs ns,
// normalized into the canonical planner effect order (read_only,
// local_mutation, external_effect, sensitive) with duplicates removed.
//
// It exists so a host can stamp a vendor-declared external capability's
// vocabulary descriptor with a namespace-derived, identity-free effect set.
// External descriptors are compared field by field (externalDescriptorEqual),
// so a descriptor built from one server's per-tool annotations would conflict
// with a second server that annotated the same capability differently. Deriving
// the descriptor from the namespace ceiling instead makes every registration of
// the same capability identical, so registration is idempotent regardless of how
// many servers declare it or in which order.
//
// The returned slice is always in canonical order because externalDescriptorEqual
// compares Effects by index; SetExternalCapabilityCeiling stores the caller's
// order unchanged, so normalization cannot be assumed.
func (r *CapabilityRegistry) ExternalEffectCeiling(ns string) []EffectClass {
	// externalEffectCeiling already handles a nil receiver and falls back to the
	// package default when the namespace has no reviewed override.
	return canonicalizeExternalEffects(r.externalEffectCeiling(strings.TrimSpace(ns)))
}

// canonicalizeExternalEffects normalizes an effect ceiling into the canonical
// planner order (read_only, local_mutation, external_effect, sensitive) with
// duplicates removed. externalDescriptorEqual compares Effects by index, so
// every descriptor stamped with a ceiling must present it in this order.
func canonicalizeExternalEffects(ceiling []EffectClass) []EffectClass {
	seen := make(map[EffectClass]bool, len(ceiling))
	for _, e := range ceiling {
		seen[e] = true
	}
	out := make([]EffectClass, 0, len(seen))
	for _, e := range canonicalEffectOrder {
		if seen[e] {
			out = append(out, e)
			delete(seen, e)
		}
	}
	// Preserve any effect class that is valid today but not yet listed in
	// canonicalEffectOrder, so this helper can never silently narrow a ceiling.
	for _, e := range ceiling {
		if seen[e] {
			out = append(out, e)
			delete(seen, e)
		}
	}
	return out
}

// canonicalEffectOrder is the planner's effect ordering (see EffectClass).
// Descriptors that externalDescriptorEqual compares must present effects in
// this order.
var canonicalEffectOrder = []EffectClass{
	EffectReadOnly,
	EffectLocalMutation,
	EffectExternalEffect,
	EffectSensitive,
}

// ExternalCeilingIsDefault reports whether ns still uses the host default
// effect ceiling (no reviewed override). tools_search and petition surfaces
// use this to distinguish an installed-but-host-unreviewed vendor capability
// (default ceiling) from one the host has explicitly widened.
func (r *CapabilityRegistry) ExternalCeilingIsDefault(ns string) bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ceil, ok := r.externalEffectCeilings[strings.TrimSpace(ns)]
	if !ok {
		return true
	}
	def := DefaultExternalCapabilityEffectCeiling
	if len(ceil) != len(def) {
		return false
	}
	seen := make(map[EffectClass]bool, len(def))
	for _, e := range def {
		seen[e] = true
	}
	for _, e := range ceil {
		if !seen[e] {
			return false
		}
	}
	return true
}

// SetExternalCapabilityCeiling widens (or narrows) the effect ceiling for one
// external namespace. It is a host-reviewed operation: a namespace in the
// default ceiling may only declare read_only and external_effect, while a
// reviewed override can additionally grant sensitive or local_mutation. Every
// capability registered under that namespace is then bounded by this ceiling.
func (r *CapabilityRegistry) SetExternalCapabilityCeiling(ns string, effects []EffectClass) error {
	if r == nil {
		return fmt.Errorf("nil capability registry")
	}
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return fmt.Errorf("external capability namespace is required")
	}
	if err := validateEffectClasses(effects); err != nil {
		return fmt.Errorf("namespace %q effect ceiling: %w", ns, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.externalEffectCeilings[ns] = append([]EffectClass(nil), effects...)
	// Retarget external descriptors already registered under this namespace
	// to the new ceiling. Namespace-ceiling descriptors are derived, not
	// vendor-authored, so leaving the old ceiling stamped on them would make
	// later RegisterExternalCapability calls for the same capability conflict
	// against a descriptor that no longer reflects the governing ceiling.
	canonical := canonicalizeExternalEffects(effects)
	prefix := string(ExternalCapabilityPrefix) + ns + ":"
	for id, descriptor := range r.descriptors {
		if !strings.HasPrefix(string(id), prefix) {
			continue
		}
		descriptor.Effects = append([]EffectClass(nil), canonical...)
		r.descriptors[id] = descriptor
	}
	return nil
}

// RegisterExternalCapability registers a vendor-declared capability under the
// external namespace at runtime. Unlike Register it is permitted on a sealed
// registry: the sealed builtin vocabulary must not change, but a freshly
// installed MCP/Skill legitimately introduces a new x: capability that the
// planner must be able to name. Registration still enforces every external
// invariant (prefix, vendor owner, effect ceiling) so an installed provider
// cannot expand the governed vocabulary past what its namespace is allowed to
// declare.
//
// ns is the namespace segment: the token after ExternalCapabilityPrefix and
// before the next colon. def.ID must already carry the x:<ns>: prefix.
// Re-registration of an identical descriptor is idempotent; a conflicting
// descriptor for the same ID is rejected so a reconnecting vendor cannot
// silently change a capability's governed contract.
func (r *CapabilityRegistry) RegisterExternalCapability(ns string, def CapabilityDescriptor) error {
	if r == nil {
		return fmt.Errorf("nil capability registry")
	}
	ns = strings.TrimSpace(ns)
	if ns == "" {
		return fmt.Errorf("external capability namespace is required")
	}
	def.ID = CapabilityID(strings.TrimSpace(string(def.ID)))
	if def.ID == "" {
		return fmt.Errorf("external capability id is required")
	}
	if !strings.HasPrefix(string(def.ID), string(ExternalCapabilityPrefix)) {
		return fmt.Errorf("external capability %q must carry the %q prefix", def.ID, ExternalCapabilityPrefix)
	}
	if !strings.HasPrefix(string(def.ID), string(ExternalCapabilityPrefix)+ns+":") {
		return fmt.Errorf("external capability %q must belong to namespace %q", def.ID, ns)
	}
	if strings.TrimSpace(def.Owner) != "vendor" {
		return fmt.Errorf("external capability %q must declare owner %q, got %q", def.ID, "vendor", def.Owner)
	}
	if strings.TrimSpace(def.Version) == "" {
		return fmt.Errorf("external capability %q version is required", def.ID)
	}
	if len(def.Effects) == 0 {
		return fmt.Errorf("external capability %q effects are required", def.ID)
	}
	if err := validateEffectClasses(def.Effects); err != nil {
		return fmt.Errorf("external capability %q effects: %w", def.ID, err)
	}
	ceiling := r.externalEffectCeiling(ns)
	for _, e := range def.Effects {
		if !effectWithinCeiling(e, ceiling) {
			return fmt.Errorf("external capability %q effect %q exceeds namespace %q ceiling %v", def.ID, e, ns, ceiling)
		}
	}
	if err := validateCapabilityDescriptorArtifacts(def.Consumes); err != nil {
		return fmt.Errorf("external capability %q consumes: %w", def.ID, err)
	}
	if err := validateCapabilityDescriptorArtifacts(def.Produces); err != nil {
		return fmt.Errorf("external capability %q produces: %w", def.ID, err)
	}
	if def.Qualifiers == nil {
		def.Qualifiers = map[string]QualifierConstraint{}
	}
	for name, rule := range def.Qualifiers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("external capability %q has an empty qualifier", def.ID)
		}
		for _, value := range rule.Values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("external capability %q qualifier %q has an empty value", def.ID, name)
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.descriptors[def.ID]; ok {
		if !externalDescriptorEqual(existing, def) {
			return fmt.Errorf("external capability %q conflicts with an already registered descriptor", def.ID)
		}
		return nil
	}
	r.descriptors[def.ID] = cloneCapabilityDescriptor(def)
	return nil
}

// externalDescriptorEqual compares the governed contract fields of two external
// capability descriptors. ArtifactContract and QualifierConstraint are value
// comparable, so the slice/map elements compare field by field.
func externalDescriptorEqual(a, b CapabilityDescriptor) bool {
	if a.Version != b.Version || a.Owner != b.Owner || a.Summary != b.Summary {
		return false
	}
	if len(a.Effects) != len(b.Effects) || len(a.Consumes) != len(b.Consumes) || len(a.Produces) != len(b.Produces) || len(a.Qualifiers) != len(b.Qualifiers) {
		return false
	}
	for i := range a.Effects {
		if a.Effects[i] != b.Effects[i] {
			return false
		}
	}
	for i := range a.Consumes {
		if a.Consumes[i] != b.Consumes[i] {
			return false
		}
	}
	for i := range a.Produces {
		if a.Produces[i] != b.Produces[i] {
			return false
		}
	}
	for k, v := range a.Qualifiers {
		bv, ok := b.Qualifiers[k]
		if !ok || bv.Required != v.Required || len(bv.Values) != len(v.Values) {
			return false
		}
		for i := range bv.Values {
			if bv.Values[i] != v.Values[i] {
				return false
			}
		}
	}
	return true
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
// ProviderTrustTier is the explicit trust classification of a provider, set by
// the trusted control-plane publisher from the contract family it resolved
// (reviewed/open), not from discovery metadata. It lets bestProvider prefer a
// reviewed or host-owned implementation over an arbitrary unreviewed
// installation even when the unreviewed tool declares a higher Quality, so a
// future change that bumps an unreviewed tool's Quality can never silently
// outrank a trusted provider of the same capability.
//
// The zero value is treated as approved (see TrustTierRank): every
// builtin/host/code-reviewed provider is constructed without an explicit tier,
// and only the open unreviewed family opts into the lower tier. Defaulting the
// unknown case to the trusted tier keeps the 50+ legacy ProviderSpec literals
// safe and means a missing field can never demote a trusted provider.
type ProviderTrustTier string

const (
	// TrustTierApproved is a builtin, host-owned, or code-reviewed provider.
	TrustTierApproved ProviderTrustTier = "approved"
	// TrustTierUnreviewed is an arbitrary installed MCP reachable only through
	// the open information.lookup family; it may act on external systems but is
	// never pre-approved over a trusted provider of the same capability.
	TrustTierUnreviewed ProviderTrustTier = "unreviewed"
)

// TrustTierRank returns the selection precedence of a tier: approved (and any
// unspecified/legacy provider) outranks unreviewed.
func (t ProviderTrustTier) Rank() int {
	if t == TrustTierUnreviewed {
		return 0
	}
	return 1
}

type ProviderSpec struct {
	AdapterName string
	Binding     ProviderBinding
	// TrustTier is the explicit trust classification projected from the
	// control-plane contract family. It is advisory for bestProvider ordering
	// only and is intentionally excluded from any grant or catalog digest: a
	// tier change must not rotate in-flight grants.
	TrustTier ProviderTrustTier
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
	// Digest is the immutable identity of this catalog snapshot. It is kept
	// separate from a plan digest: a plan also commits needs, facts, policy
	// constraints and budgets, while this value answers only "which provider
	// inventory did the host admit?".
	Digest    string
	CreatedAt time.Time
	Providers []ProviderSpec
	// Coverage tells the planner whether this snapshot is a complete view of
	// the governed inventory for the current request scope. An empty provider
	// list without coverage is not evidence that a capability is unavailable:
	// a dynamic Skill/MCP refresh may still be in progress. Legacy Publish
	// callers receive explicit Complete coverage; dynamic hosts must publish
	// their observed readiness through PublishWithCoverage.
	Coverage CatalogCoverage
}

// EffectiveCatalogSnapshotDigest returns the canonical identity of a catalog
// snapshot.  Digest is a persisted claim supplied by a publisher, so callers
// must never use it as an authority without checking it against the immutable
// snapshot contents.  A blank claim is accepted for legacy in-memory callers
// and is filled from the canonical representation.
func EffectiveCatalogSnapshotDigest(snapshot ToolCatalogSnapshot) (string, error) {
	expected := CatalogSnapshotDigest(snapshot)
	if supplied := strings.TrimSpace(snapshot.Digest); supplied != "" && supplied != expected {
		return "", fmt.Errorf("catalog_snapshot_digest_mismatch")
	}
	return expected, nil
}

// ValidateCatalogSnapshotDigest checks the optional persisted digest claim
// without mutating the snapshot.  It is useful at boundaries that need a
// boolean-style validation before handing a snapshot to a planner.
func ValidateCatalogSnapshotDigest(snapshot ToolCatalogSnapshot) error {
	_, err := EffectiveCatalogSnapshotDigest(snapshot)
	return err
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
			if strings.HasPrefix(string(provision.Capability), string(ExternalCapabilityPrefix)) {
				// An external capability's governed effect set is whatever the
				// host admitted at registration time (already bounded by the
				// namespace ceiling). A publishing provider may not declare an
				// effect the registered descriptor does not permit, so an
				// installed vendor tool cannot silently widen its authority
				// past what was reviewed.
				for _, e := range provider.Effects {
					if !effectWithinCeiling(e, descriptor.Effects) {
						return ToolCatalogSnapshot{}, fmt.Errorf("provider %q capability %q effect %q is not permitted by its registered descriptor %v", provider.AdapterName, provision.Capability, e, descriptor.Effects)
					}
				}
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
	snapshot := ToolCatalogSnapshot{Generation: generation, RegistryVersion: c.registry.Version(), CreatedAt: now, Providers: validated, Coverage: coverage}
	snapshot.Digest = CatalogSnapshotDigest(snapshot)
	c.snapshot = snapshot
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
	out.ParameterAuthorization.AllowedTargets = append([]string(nil), in.ParameterAuthorization.AllowedTargets...)
	out.ParameterAuthorization.AllowedArtifactIDs = append([]string(nil), in.ParameterAuthorization.AllowedArtifactIDs...)
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

// CatalogSnapshotDigest returns a stable identity for the host-admitted
// provider inventory. Diagnostic timestamps and the stored Digest field are
// deliberately excluded; changing a provider binding, schema authorization,
// readiness watermark, coverage state or generation produces a new identity.
//
// The digest payload is a length-delimited canonical multiset. Provider and
// coverage-family order therefore cannot affect the result, and a malformed
// snapshot containing duplicate stable IDs still has a deterministic identity
// (the duplicate entries are retained and sorted by their complete payload).
// The v2 marker is intentional: v1 joined raw slices and was order-sensitive,
// so an old and a new publisher must never silently treat those identities as
// interchangeable during recovery.
//
// Callers may use this for recovery checks even when they received a snapshot
// from an older publisher that did not persist Digest.
func CatalogSnapshotDigest(snapshot ToolCatalogSnapshot) string {
	parts := []string{
		"catalog-snapshot-v2",
		snapshot.RegistryVersion,
		fmt.Sprintf("%d", snapshot.Generation),
		"coverage",
		string(snapshot.Coverage.State),
		snapshot.Coverage.ReasonCode,
		canonicalDigestTime(snapshot.Coverage.StaleUntil),
	}
	families := append([]CatalogCoverageFamily(nil), snapshot.Coverage.Families...)
	sort.Slice(families, func(i, j int) bool {
		return canonicalCatalogCoverageFamilyDigest(families[i]) < canonicalCatalogCoverageFamilyDigest(families[j])
	})
	for _, family := range families {
		parts = append(parts, "family", canonicalCatalogCoverageFamilyDigest(family))
	}
	providers := append([]ProviderSpec(nil), snapshot.Providers...)
	sort.Slice(providers, func(i, j int) bool {
		return canonicalProviderDigest(providers[i]) < canonicalProviderDigest(providers[j])
	})
	for _, provider := range providers {
		parts = append(parts, "provider", canonicalProviderDigest(provider))
	}
	return "catalog:sha256:" + SchemaDigest([]byte(canonicalDigestEncode(parts...)))
}

// canonicalDigestEncode gives every field an explicit byte length. It is used
// only for immutable identity payloads; unlike a delimiter join it cannot
// collapse two values that contain a delimiter or an embedded NUL.
func canonicalDigestEncode(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(fmt.Sprintf("%d:", len(part)))
		builder.WriteString(part)
	}
	return builder.String()
}

func canonicalDigestTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

// canonicalDigestStringList represents a multiset: entries are sorted but
// deliberately not deduplicated. Duplicate values are invalid for several
// catalog fields, yet retaining them makes a hand-built invalid snapshot
// deterministic and keeps the digest from masking the malformed state.
func canonicalDigestStringList(values []string) string {
	if len(values) == 0 {
		return canonicalDigestEncode("0")
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	parts := make([]string, 0, len(sorted)+1)
	parts = append(parts, fmt.Sprintf("%d", len(sorted)))
	parts = append(parts, sorted...)
	return canonicalDigestEncode(parts...)
}

func canonicalDigestNormalizedStringList(values []string, lower bool) string {
	if len(values) == 0 {
		return canonicalDigestEncode("0")
	}
	normalized := make([]string, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}
		normalized[index] = value
	}
	return canonicalDigestStringList(normalized)
}

func canonicalCatalogCoverageFamilyDigest(family CatalogCoverageFamily) string {
	return canonicalDigestEncode(
		canonicalCatalogCoverageKind(family.Kind),
		string(family.State),
		family.ReasonCode,
		canonicalDigestTime(family.StaleUntil),
	)
}

func canonicalProviderDigest(provider ProviderSpec) string {
	authorization := provider.ParameterAuthorization
	return canonicalDigestEncode(
		strings.TrimSpace(provider.AdapterName),
		string(provider.Classification.normalized()),
		strings.TrimSpace(provider.Binding.Kind),
		strings.TrimSpace(provider.Binding.ProviderID),
		strings.TrimSpace(provider.Binding.ImplementationID),
		strings.TrimSpace(provider.Binding.SchemaDigest),
		fmt.Sprintf("%d", provider.Binding.CatalogGeneration),
		fmt.Sprintf("%t", provider.Ready),
		canonicalDigestTime(provider.ReadyUntil),
		// Channel scopes are matched case-insensitively by the planner, so the
		// identity follows the same semantic normalization.
		canonicalDigestNormalizedStringList(provider.ChannelScopes, true),
		strings.TrimSpace(authorization.Digest),
		strings.TrimSpace(authorization.CanonicalizerVer),
		canonicalDigestNormalizedStringList(authorization.AllowedFields, false),
		canonicalDigestNormalizedStringList(authorization.AllowedTargets, false),
		canonicalDigestNormalizedStringList(authorization.AllowedArtifactIDs, false),
		canonicalDigestNormalizedStringList(effectStrings(provider.Effects), false),
		canonicalDigestProvisionList(provider.Provides),
		canonicalDigestArtifactList(provider.Consumes),
		canonicalDigestArtifactList(provider.Produces),
	)
}

func effectStrings(effects []EffectClass) []string {
	values := make([]string, len(effects))
	for index, effect := range effects {
		values[index] = string(effect)
	}
	return values
}

func canonicalDigestProvisionList(provisions []CapabilityProvision) string {
	values := make([]string, 0, len(provisions))
	for _, provision := range provisions {
		values = append(values, canonicalDigestEncode(
			string(provision.Capability),
			canonicalDigestStringMap(provision.Qualifiers),
			fmt.Sprintf("%.9g", provision.Quality),
		))
	}
	sort.Strings(values)
	parts := make([]string, 0, len(values)+1)
	parts = append(parts, fmt.Sprintf("%d", len(values)))
	parts = append(parts, values...)
	return canonicalDigestEncode(parts...)
}

func canonicalDigestArtifactList(contracts []ArtifactContract) string {
	values := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		values = append(values, canonicalDigestEncode(
			strings.ToLower(strings.TrimSpace(contract.Kind)),
			strings.ToLower(strings.TrimSpace(contract.MIMEType)),
			fmt.Sprintf("%t", contract.Required),
		))
	}
	sort.Strings(values)
	parts := make([]string, 0, len(values)+1)
	parts = append(parts, fmt.Sprintf("%d", len(values)))
	parts = append(parts, values...)
	return canonicalDigestEncode(parts...)
}

func canonicalDigestStringMap(values map[string]string) string {
	if len(values) == 0 {
		return canonicalDigestEncode("0")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	parts = append(parts, fmt.Sprintf("%d", len(keys)))
	for _, key := range keys {
		parts = append(parts, canonicalDigestEncode(key, values[key]))
	}
	return canonicalDigestEncode(parts...)
}
