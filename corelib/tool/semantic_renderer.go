package tool

import (
	"fmt"
	"sort"
	"strings"
)

// CatalogRenderer is the only component that turns an approved selection into
// an LLM-visible function definition. It deliberately accepts schemas from a
// trusted host registry, never from dynamic provider descriptions.
type CatalogRenderer struct {
	registry *CapabilityRegistry
}

func NewCatalogRenderer(registry *CapabilityRegistry) *CatalogRenderer {
	return &CatalogRenderer{registry: registry}
}

// RenderedTool connects one host-owned model function name to the immutable
// grant and selection which the executor must validate. Definition contains
// only the OpenAI-compatible function surface; no provider identity is
// exposed there. FunctionName is stable across turns; Grant.Token is not.
type RenderedTool struct {
	FunctionName string
	Definition   map[string]interface{}
	Grant        InvocationGrant
	Selection    PlannedSelection
}

// Render creates a stable ordering for the grants supplied by the issuer. A
// source schema is looked up by the trusted AdapterName of a selection, not by
// any model-controlled identifier. The map values must be normal OpenAI
// function definitions produced by the host's registered-tool builder.
func (r *CatalogRenderer) Render(plan ToolPlan, grants []InvocationGrant, sourceByAdapter map[string]map[string]interface{}) ([]RenderedTool, error) {
	return r.RenderReady(plan, grants, sourceByAdapter, nil)
}

// RenderReady materializes only selections in the current exposure closure.
// `satisfied` contains executor-verified selection/artifact/confirmation
// dependencies; it is never derived from model prose or call ordering.
func (r *CatalogRenderer) RenderReady(plan ToolPlan, grants []InvocationGrant, sourceByAdapter map[string]map[string]interface{}, satisfied map[string]bool) ([]RenderedTool, error) {
	if r == nil || r.registry == nil {
		return nil, fmt.Errorf("catalog renderer requires a capability registry")
	}
	if strings.TrimSpace(plan.ID) == "" {
		return nil, fmt.Errorf("plan identity is required")
	}
	grantBySelection := make(map[string]InvocationGrant, len(grants))
	for _, grant := range grants {
		if strings.TrimSpace(grant.Token) == "" || strings.TrimSpace(grant.SelectionID) == "" {
			return nil, fmt.Errorf("invocation grant is incomplete")
		}
		if grant.Scope.PlanID != plan.ID || grant.CatalogGeneration != plan.CatalogGeneration {
			return nil, fmt.Errorf("invocation grant does not match plan")
		}
		if _, exists := grantBySelection[grant.SelectionID]; exists {
			return nil, fmt.Errorf("duplicate invocation grant for selection %q", grant.SelectionID)
		}
		grantBySelection[grant.SelectionID] = grant
	}
	selections := plan.ReadySelections(satisfied)
	sort.Slice(selections, func(i, j int) bool { return selections[i].ID < selections[j].ID })
	seenNames := make(map[string]struct{}, len(selections))
	out := make([]RenderedTool, 0, len(selections))
	for _, selection := range selections {
		grant, ok := grantBySelection[selection.ID]
		if !ok {
			return nil, fmt.Errorf("missing invocation grant for selection %q", selection.ID)
		}
		if selection.Provider.CatalogGeneration != plan.CatalogGeneration || grant.ProviderBinding != selection.Provider.StableID() || grant.FitProofDigest != selection.FitProof.Digest || !parameterAuthorizationsEqual(selection.ParameterAuthorization, grant.ParameterAuthorization) {
			return nil, fmt.Errorf("selection %q binding does not match grant", selection.ID)
		}
		provider, ok := sourceByAdapter[selection.AdapterName]
		if !ok {
			return nil, fmt.Errorf("no trusted schema for adapter %q", selection.AdapterName)
		}
		function, ok := provider["function"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("trusted schema for adapter %q has no function", selection.AdapterName)
		}
		params, ok := function["parameters"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("trusted schema for adapter %q has no parameters", selection.AdapterName)
		}
		if selection.ParameterAuthorization.Digest != "" {
			if err := ValidateParameterAuthorization(params, selection.ParameterAuthorization); err != nil {
				return nil, fmt.Errorf("selection %q parameter authorization: %w", selection.ID, err)
			}
		}
		functionName := RenderedSemanticFunctionName(selection.AdapterName, grant.Token)
		if !validRenderedFunctionName(functionName) {
			return nil, fmt.Errorf("adapter %q has no valid model function name", selection.AdapterName)
		}
		if _, exists := seenNames[functionName]; exists {
			return nil, fmt.Errorf("function-name collision for selection %q", selection.ID)
		}
		seenNames[functionName] = struct{}{}
		definition, err := r.renderDefinition(selection, functionName, provider)
		if err != nil {
			return nil, err
		}
		out = append(out, RenderedTool{FunctionName: functionName, Definition: definition, Grant: grant, Selection: selection})
	}
	return out, nil
}

func (r *CatalogRenderer) renderDefinition(selection PlannedSelection, functionName string, source map[string]interface{}) (map[string]interface{}, error) {
	function, ok := source["function"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("trusted schema for adapter %q has no function", selection.AdapterName)
	}
	params, ok := function["parameters"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("trusted schema for adapter %q has no parameters", selection.AdapterName)
	}
	capability, ok := r.registry.Lookup(selection.FitProof.MatchedCapability)
	if !ok || capability.Deprecated {
		return nil, fmt.Errorf("selection %q references unavailable capability %q", selection.ID, selection.FitProof.MatchedCapability)
	}
	description := strings.TrimSpace(capability.Summary)
	if description == "" {
		description = "Perform the approved " + string(capability.ID) + " capability."
	}
	description += semanticRenderedReuseCue(capability.ID)
	if semanticSchemaAdmitsNoArguments(params) {
		// Closed empty-schema adapters (current-channel delivery and similar)
		// bind content and destination host-side. Models habitually invent a
		// path field out of legacy send_file memory; admission rejects any
		// argument as parameter_schema_invalid and the rejection retires the
		// one-shot grant, so a guessed field costs the delivery itself
		// (2026-08-25 weather-PDF turn). Say so up front.
		description += " It takes no arguments: content and destination are bound by the host. Call it with an empty arguments object; any field (for example path) is rejected."
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        functionName,
			"description": description,
			"parameters":  cloneJSONValue(params).(map[string]interface{}),
		},
	}, nil
}

const semanticListedCallCue = " Call it whenever it is listed; after a successful call this name may briefly leave the list and later reappear for the next authorized step."

// semanticRenderedReuseCue is the host-authored call policy appended to every
// governed description. Delivery and generate tools must be called when listed.
// Optional URL tools must not: production 2026-08-29 burned download_file on
// https://example.invalid/skip because a force-call cue taught the model to
// probe a skip token.
func semanticRenderedReuseCue(id CapabilityID) string {
	switch id {
	case CapabilityArtifactAcquireRemote, CapabilityInformationFetchWeb:
		return " This step is optional when listed with other tools. Call only with a real HTTP(S) URL; never placeholder or reserved hosts such as example.invalid. Skipping it does not block the other listed tools."
	case CapabilityID("information.search.web"):
		return " This step is optional when listed with other tools. Call only with a real search query. Skipping it does not block the other listed tools."
	case CapabilityDocumentWriteOffice:
		return semanticListedCallCue + " Presentation slides accept native editable charts on slides[].charts (bar/column/bar_h/line/radar/pie/area)."
	default:
		return semanticListedCallCue
	}
}

// semanticSchemaAdmitsNoArguments reports a closed schema with no declared
// properties. The delivery adapters use exactly this shape: every value is
// host-bound, so the only valid invocation is an empty arguments object.
func semanticSchemaAdmitsNoArguments(params map[string]interface{}) bool {
	props, _ := params["properties"].(map[string]interface{})
	if len(props) > 0 {
		return false
	}
	additional, declared := params["additionalProperties"].(bool)
	return declared && !additional
}

func validRenderedFunctionName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func cloneJSONValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			out[key] = cloneJSONValue(child)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, child := range typed {
			out[i] = cloneJSONValue(child)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
