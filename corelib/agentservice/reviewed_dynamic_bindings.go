package agentservice

import (
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// Reviewed MCP and Skill bindings are the only way an observed implementation
// becomes a capability. Server-agnostic rows are keyed by an exact MCP tool
// name or an exact Skill stable ID. A second list is keyed by the installed
// server's stable capability identity plus an exact tool name. Descriptions,
// triggers, schemas, and marketplace metadata are not consulted and cannot
// add a row. An identity match does not invent provisions: the contract below
// is still the code-reviewed declaration.
//
// The declaration carries no ObservedBindingDigest. The lifecycle publisher
// computes that from the tool schema or the Skill content it just observed.

func reviewedWorklogReadContract() DynamicCapabilityContract {
	return DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{
			Capability: coretool.CapabilityRecordReadWorklog,
			Quality:    3,
		}},
		Effects:   []coretool.EffectClass{coretool.EffectReadOnly},
		TrustTier: coretool.TrustTierApproved,
	}
}

// reviewedWorklogUpdateContract is the append/update half of the external
// time-block work record. It is deliberately not a read: the mutation writes
// a system of record this process cannot read back, so the effect is
// EffectExternalEffect and every selection settles through the host's
// external-effect receipt boundary rather than the provider's own result.
// Quality is 2 so a host-owned managed adapter, if one is ever published,
// still outranks it.
func reviewedWorklogUpdateContract() DynamicCapabilityContract {
	return DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{
			Capability: coretool.CapabilityRecordUpdateWorklog,
			Quality:    2,
		}},
		Effects:   []coretool.EffectClass{coretool.EffectExternalEffect},
		TrustTier: coretool.TrustTierApproved,
	}
}

// ReviewedMCPCapabilityContracts is the code-reviewed MCP tool-name map.
// work_log_query and work_log_update are the only attested read and mutation
// implementations that any observed server may publish: the name is the
// reviewed contract, so a server installed later publishes through this row
// without a code change, and a differently named tool still has to be
// reviewed here before it can carry the capability. A product-scoped tool
// such as web_search_prime is intentionally absent: publishing it here would
// admit the same name on every server.
func ReviewedMCPCapabilityContracts() map[string]DynamicCapabilityContract {
	return map[string]DynamicCapabilityContract{
		"work_log_query":  reviewedWorklogReadContract(),
		"work_log_update": reviewedWorklogUpdateContract(),
	}
}

// ReviewedMCPOpenLookupContract is the reviewed declaration that makes an
// arbitrary installed MCP tool callable.
//
// A tools/list name is reviewed per capability, so a server nobody has
// reviewed would otherwise be invisible to the model forever. information.
// lookup is the one family the reviewed vocabulary keeps open: any observed
// server may carry it and the host does not pre-approve a name.
//
// Effect envelope: the open family declares BOTH EffectReadOnly and
// EffectExternalEffect. That is the capability's allowed envelope, not the
// effect of any one tool. The per-tool contract narrows it:
// openLookupEffectsForTool classifies each observed tool from its vendor
// annotations, so a read-only-annotated tool matches a read-only need and runs
// without a write boundary, while a write or unannotated tool is selected with
// EffectExternalEffect and settles through the host's external-effect receipt
// boundary — the same confirmation path a reviewed mutation uses. An arbitrary
// installed MCP can therefore be called to act on an external system, and that
// act is never silent.
//
// scope is required by the descriptor. An arbitrary tool is reference
// retrieval: it answers from whatever it is pointed at, and it is not a
// freshness-bearing current-facts source the way a web search is.
// Quality is 1, below every reviewed and host-owned provision, so a specific
// contract always wins over this open one.
func ReviewedMCPOpenLookupContract() DynamicCapabilityContract {
	return DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{
			Capability: CapabilityInformationLookup,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
			Quality:    1,
		}},
		Effects:   []coretool.EffectClass{coretool.EffectReadOnly, coretool.EffectExternalEffect},
		TrustTier: coretool.TrustTierUnreviewed,
	}
}

// openLookupEffectsForTool classifies one arbitrary, unreviewed MCP tool by
// the vendor-declared annotations observed at tools/list time. The host never
// trusts an unknown vendor tool to be side-effect-free:
//   - an explicit readOnlyHint == true  -> read-only, silent read path;
//   - anything else (readOnlyHint absent or false, a destructiveHint, or no
//     annotation at all) -> EffectExternalEffect, gated by the receipt boundary.
//
// The capability envelope (ReviewedMCPOpenLookupContract) already permits both
// effects, so the narrowed per-tool contract still satisfies the planner's
// effectsWithinCapability check; only the selected tool's effect class changes.
func openLookupEffectsForTool(tool ObservedMCPTool) []coretool.EffectClass {
	if tool.Annotations != nil && tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint {
		return []coretool.EffectClass{coretool.EffectReadOnly}
	}
	return []coretool.EffectClass{coretool.EffectExternalEffect}
}

// reviewedContractOnlyCapabilities names the capabilities that have no host
// attach site and are servable only through a published MCP/Skill contract.
// It is the reviewed answer to "where can this capability come from" for the
// families the host does not own, so a wiring test can assert both halves:
// the family is contract-only, and a reviewed binding still provisions it.
//
// It is also the graft's authority for "this need can only ever be answered by
// something a user installed". A family listed here has no host implementation
// on any instance, so failing it closed can never be a deployment question —
// it is always "nothing is installed that serves this".
//
// It is DERIVED from ReviewedContractOnlyCapabilityIDs: there is exactly one
// place to add a contract-only family. A drift between the two would fail
// silently — either a family that can never be grafted (the exact "installed
// but unusable" bug this file's graft fixes), or one that can never be wired.
// See contractOnlyCapabilityIdentifiers for how names map onto IDs, and
// TestContractOnlyListsShareOneSourceOfTruth, which fails on any mismatch.
var reviewedContractOnlyCapabilities = contractOnlyCapabilityIdentifiers()

// contractOnlyCapability binds a capability ID to the Go constant name the
// wiring test greps for. Both live on one row so they cannot drift apart.
type contractOnlyCapability struct {
	id   coretool.CapabilityID
	name string
}

// contractOnlyCapabilities is the single source of truth for the contract-only
// families. Adding one is a single edit here; both consumers below derive from
// this slice, so there is no second literal to forget.
//
// An earlier version kept the IDs in ReviewedContractOnlyCapabilityIDs and the
// names in a separate slice, and the struct's `id` field was never read — so the
// two lists could in fact drift while the comment claimed they could not. A
// comment is not a mechanism.
var contractOnlyCapabilities = []contractOnlyCapability{
	{CapabilityInformationLookup, "CapabilityInformationLookup"},
	{CapabilityRecordReadWorklog, "CapabilityRecordReadWorklog"},
	{CapabilityRecordUpdateWorklog, "CapabilityRecordUpdateWorklog"},
}

// ReviewedContractOnlyCapabilityIDs lists the contract-only capability IDs, in
// declaration order. The graft consumes this form.
func ReviewedContractOnlyCapabilityIDs() []coretool.CapabilityID {
	out := make([]coretool.CapabilityID, 0, len(contractOnlyCapabilities))
	for _, entry := range contractOnlyCapabilities {
		out = append(out, entry.id)
	}
	return out
}

// contractOnlyCapabilityIdentifiers lists the Go constant names, in the same
// order. The wiring test reads names out of source text, which is why strings
// exist at all; nothing in the runtime path should consume them.
func contractOnlyCapabilityIdentifiers() []string {
	out := make([]string, 0, len(contractOnlyCapabilities))
	for _, entry := range contractOnlyCapabilities {
		out = append(out, entry.name)
	}
	return out
}

// IsReviewedContractOnlyCapability reports whether capability has no host
// attach site and can be served only by a published dynamic contract.
//
// It walks the table directly rather than the ID accessor: this runs per need
// and per provision while planning, and the accessor allocates a fresh slice
// each call.
func IsReviewedContractOnlyCapability(capability coretool.CapabilityID) bool {
	for _, entry := range contractOnlyCapabilities {
		if entry.id == capability {
			return true
		}
	}
	return false
}

// ReviewedMCPWebSearchPrimeGlobalKey is the stable marketplace identity of
// the reviewed Zhipu web-search MCP. Install-instance capability ids are not
// match keys. A version suffix is not part of this key, so a reviewed
// product stays addressable when its version key changes.
const ReviewedMCPWebSearchPrimeGlobalKey = "enterprise_hub:mcp:hubcenter:web-search-prime"

// ReviewedMCPWebSearchPrimeTool is the exact tools/list name of that product.
const ReviewedMCPWebSearchPrimeTool = "web_search_prime"

// MCPCapabilityIdentity is the capability ref stored on an installed server.
// It selects a reviewed binding. It is not itself a provision, effect, or
// quality, and an empty identity matches nothing.
type MCPCapabilityIdentity struct {
	GlobalKey    string
	CapabilityID string
}

// ReviewedMCPCapabilityBinding is one code-reviewed MCP implementation that
// is addressable only when the installed server presents this identity and
// the observation contains ToolName. CapabilityID, when set, is a stable
// product id. Install-instance ids must not be written here.
type ReviewedMCPCapabilityBinding struct {
	GlobalKey    string
	CapabilityID string
	ToolName     string
	Contract     DynamicCapabilityContract
	Invocation   ReviewedMCPModelInvocation
}

// ReviewedMCPModelInvocation is the model contract for a capability that
// already has a stable prompt spelling. Schema replaces the vendor tools/list
// schema. Fields maps each model argument onto the observed tool argument.
// An empty invocation leaves the observed schema and the opaque grant token.
type ReviewedMCPModelInvocation struct {
	Function string
	Schema   map[string]interface{}
	Fields   map[string]string
}

func (inv ReviewedMCPModelInvocation) declared() bool {
	return strings.TrimSpace(inv.Function) != "" && len(inv.Schema) > 0 && len(inv.Fields) > 0
}

func (id MCPCapabilityIdentity) matchesReviewedMCP(binding ReviewedMCPCapabilityBinding) bool {
	wantGlobal := strings.TrimSpace(binding.GlobalKey)
	wantID := strings.TrimSpace(binding.CapabilityID)
	if wantGlobal == "" && wantID == "" {
		return false
	}
	if wantGlobal != "" && strings.TrimSpace(id.GlobalKey) == wantGlobal {
		return true
	}
	if wantID != "" && strings.TrimSpace(id.CapabilityID) == wantID {
		return true
	}
	return false
}

// reviewedMCPWebSearchPrimeQuality outranks both host search implementations.
// The desktop managed adapter semantic_search_trusted_web is quality 2. The
// core host adapter is quality 1. bestProvider replaces a candidate only when
// quality is strictly higher, and a tie keeps the lower StableID. That ID
// starts with the provider kind, so "builtin" sorts before "mcp" and an equal
// rank leaves the desktop serper search selected.
const reviewedMCPWebSearchPrimeQuality = 3

func reviewedWebSearchPrimeContract() DynamicCapabilityContract {
	return DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessReference}, Quality: reviewedMCPWebSearchPrimeQuality},
			{Capability: CapabilityInformationSearchWeb, Qualifiers: map[string]string{QualifierSearchFreshness: SearchFreshnessCurrent}, Quality: reviewedMCPWebSearchPrimeQuality},
		},
		Effects:   []coretool.EffectClass{coretool.EffectReadOnly},
		TrustTier: coretool.TrustTierApproved,
	}
}

// ReviewedMCPCapabilityBindings is the code-reviewed identity join. The
// web-search row is selected only after this product is installed and a
// tools/list observation still contains web_search_prime. The model keeps
// calling web_search({query}); execution maps that argument onto the observed
// search_query field.
func ReviewedMCPCapabilityBindings() []ReviewedMCPCapabilityBinding {
	return []ReviewedMCPCapabilityBinding{{
		GlobalKey: ReviewedMCPWebSearchPrimeGlobalKey,
		ToolName:  ReviewedMCPWebSearchPrimeTool,
		Contract:  reviewedWebSearchPrimeContract(),
		Invocation: ReviewedMCPModelInvocation{
			Function: "web_search",
			Schema:   reviewedHostWebSearchInvocationSchema(),
			Fields:   map[string]string{"query": "search_query"},
		},
	}}
}

// reviewedMCPModelInvocation returns the model contract for an entry whose
// installed identity, tool name, and published provisions match one reviewed
// binding. A matching binding whose observed schema no longer contains the
// mapped argument fails closed so the vendor shape cannot be rendered as the
// capability's prompt tool. A non-matching entry keeps the observed schema.
func reviewedMCPModelInvocation(entry MCPToolEntry) (ReviewedMCPModelInvocation, bool, error) {
	identity := MCPCapabilityIdentity{GlobalKey: entry.CapabilityGlobalKey, CapabilityID: entry.InstalledCapabilityID}
	toolName := strings.TrimSpace(entry.ToolName)
	for _, binding := range ReviewedMCPCapabilityBindings() {
		if strings.TrimSpace(binding.ToolName) != toolName || !identity.matchesReviewedMCP(binding) {
			continue
		}
		if !binding.Invocation.declared() || !reviewedMCPContractShapeMatches(entry.Contract, binding.Contract) {
			return ReviewedMCPModelInvocation{}, false, nil
		}
		if err := observedMCPSchemaHasStringFields(entry.InputSchema, binding.Invocation.Fields); err != nil {
			return ReviewedMCPModelInvocation{}, false, err
		}
		return cloneReviewedMCPModelInvocation(binding.Invocation), true, nil
	}
	return ReviewedMCPModelInvocation{}, false, nil
}

func cloneReviewedMCPModelInvocation(in ReviewedMCPModelInvocation) ReviewedMCPModelInvocation {
	out := in
	if cloned, ok := cloneMCPJSONValue(in.Schema).(map[string]interface{}); ok {
		out.Schema = cloned
	}
	out.Fields = make(map[string]string, len(in.Fields))
	for key, value := range in.Fields {
		out.Fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func observedMCPSchemaHasStringFields(schema map[string]interface{}, fields map[string]string) error {
	properties, _ := schema["properties"].(map[string]interface{})
	for _, target := range fields {
		target = strings.TrimSpace(target)
		if target == "" {
			return fmt.Errorf("reviewed MCP argument target is empty")
		}
		raw, ok := properties[target]
		if !ok {
			return fmt.Errorf("observed MCP schema is missing reviewed argument %q", target)
		}
		property, ok := raw.(map[string]interface{})
		if !ok || strings.TrimSpace(fmt.Sprint(property["type"])) != "string" {
			return fmt.Errorf("observed MCP schema argument %q is not a string", target)
		}
	}
	return nil
}

// reviewedMCPContractShapeMatches reports that the published contract still
// declares the reviewed capability shape it was written from. Provisions are
// a multiset SUBSET check (want ⊆ got): the publisher legitimately appends
// host-issued and vendor-declared x: provisions onto the same contract, and
// those extras must not strip a reviewed tool of its invocation. Effects stay
// an exact multiset match — the effect envelope is the reviewed contract's
// own claim and is not allowed to grow with extra provisions.
func reviewedMCPContractShapeMatches(got, want DynamicCapabilityContract) bool {
	if len(got.Provisions) < len(want.Provisions) || len(got.Effects) != len(want.Effects) {
		return false
	}
	effects := make(map[coretool.EffectClass]int, len(want.Effects))
	for _, effect := range want.Effects {
		effects[effect]++
	}
	for _, effect := range got.Effects {
		if effects[effect] == 0 {
			return false
		}
		effects[effect]--
	}
	unmatched := make([]bool, len(got.Provisions))
	for _, candidate := range want.Provisions {
		found := false
		for i, provision := range got.Provisions {
			if unmatched[i] || provision.Capability != candidate.Capability || provision.Quality != candidate.Quality || !reviewedMCPQualifiersEqual(provision.Qualifiers, candidate.Qualifiers) {
				continue
			}
			unmatched[i] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func reviewedMCPQualifiersEqual(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for key, value := range want {
		if strings.TrimSpace(got[key]) != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

// ReviewedSkillCapabilityContracts is the code-reviewed Skill stable-ID map.
// It is empty until a specific stable ID is reviewed for one sealed outcome.
// Installed package names are not implied entries.
func ReviewedSkillCapabilityContracts() map[string]DynamicCapabilityContract {
	return map[string]DynamicCapabilityContract{}
}
