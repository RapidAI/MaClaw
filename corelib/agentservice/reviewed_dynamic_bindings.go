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
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
	}
}

// ReviewedMCPCapabilityContracts is the code-reviewed MCP tool-name map.
// work_log_query is the only attested read implementation that any observed
// server may publish. Mutation tool names are not guessed. A product-scoped
// tool such as web_search_prime is intentionally absent: publishing it here
// would admit the same name on every server.
func ReviewedMCPCapabilityContracts() map[string]DynamicCapabilityContract {
	return map[string]DynamicCapabilityContract{
		"work_log_query": reviewedWorklogReadContract(),
	}
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
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
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

func reviewedMCPContractShapeMatches(got, want DynamicCapabilityContract) bool {
	if len(got.Provisions) != len(want.Provisions) || len(got.Effects) != len(want.Effects) {
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
	used := make([]bool, len(want.Provisions))
	for _, provision := range got.Provisions {
		found := false
		for i, candidate := range want.Provisions {
			if used[i] || provision.Capability != candidate.Capability || provision.Quality != candidate.Quality || !reviewedMCPQualifiersEqual(provision.Qualifiers, candidate.Qualifiers) {
				continue
			}
			used[i] = true
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
