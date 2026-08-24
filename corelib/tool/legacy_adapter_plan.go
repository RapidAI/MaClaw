package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LegacyAdapterPlan is the immutable migration boundary between the old
// name-based recommender and an LLM-visible legacy surface. Router evidence is
// only one input to this object; definitions are rendered later from the
// selected reviewed provisions and the exact trusted definition snapshot.
//
// It deliberately has no exported mutable fields. In particular, callers
// cannot append a tool name to a plan after it was built. A new user turn,
// policy decision, or definition snapshot needs a new plan and full surface
// replacement.
//
// This is not a semantic ToolPlan: it has no grant or parameter authorization.
// Legacy execution remains constrained by the request surface snapshot and the
// reviewed provision catalog while each family migrates to the semantic
// planner.
type LegacyAdapterPlan struct {
	id            string
	policyDigest  string
	catalogDigest string
	searchQuery   string
	confidence    float64
	schemaTokens  int
	selections    []LegacyAdapterSelection
	evidence      []RoutingEvidence
	pruned        []RoutingEvidence
}

// LegacyAdapterSelection is the reviewed identity of one legacy definition.
// ProviderBinding is intentionally host-owned and derived from the provision;
// it is not a model-provided provider or gateway target.
type LegacyAdapterSelection struct {
	ID               string
	ToolName         string
	Capability       CapabilityID
	Owner            string
	AdapterContract  string
	ProviderBinding  string
	Effects          []EffectClass
	DefinitionDigest string
	SchemaTokens     int
	Reason           string
	Score            float64
}

// LegacyAdapterPlanInput contains only request-scoped, host-trusted values.
// PolicyDigest must identify the policy decision under which the plan is
// rendered. Definitions must be the immutable snapshot supplied to the model
// request, not a registry re-read after planning.
type LegacyAdapterPlanInput struct {
	Recommendation RoutingRecommendation
	Definitions    []map[string]interface{}
	PolicyDigest   string
	// SchemaTokenBudget is a host limit for this request. Zero means the
	// compatibility plan uses only the count guard while callers migrate.
	SchemaTokenBudget int
	Now               time.Time
}

// LegacyAdapterPlanError is deliberately machine-readable so an unmanaged
// host can fail closed as catalog_incomplete or plan_over_budget instead of
// falling back to its previous definition soup.
type LegacyAdapterPlanError struct {
	Code string
	Err  error
}

func (e *LegacyAdapterPlanError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *LegacyAdapterPlanError) Unwrap() error { return e.Err }

const (
	LegacyAdapterPlanCatalogIncomplete = "catalog_incomplete"
	LegacyAdapterPlanOverBudget        = "plan_over_budget"
	LegacyAdapterPlanInvalid           = "invalid_legacy_adapter_plan"
)

func legacyAdapterPlanError(code, format string, args ...interface{}) error {
	return &LegacyAdapterPlanError{Code: code, Err: fmt.Errorf(format, args...)}
}

func (p LegacyAdapterPlan) ID() string            { return p.id }
func (p LegacyAdapterPlan) PolicyDigest() string  { return p.policyDigest }
func (p LegacyAdapterPlan) CatalogDigest() string { return p.catalogDigest }
func (p LegacyAdapterPlan) SearchQuery() string   { return p.searchQuery }
func (p LegacyAdapterPlan) Confidence() float64   { return p.confidence }
func (p LegacyAdapterPlan) SchemaTokens() int     { return p.schemaTokens }

// Selections and Evidence return defensive copies. They are intentionally the
// only public route to the plan's collections.
func (p LegacyAdapterPlan) Selections() []LegacyAdapterSelection {
	out := make([]LegacyAdapterSelection, len(p.selections))
	for i, selection := range p.selections {
		out[i] = cloneLegacyAdapterSelection(selection)
	}
	return out
}

func (p LegacyAdapterPlan) Evidence() []RoutingEvidence {
	return append([]RoutingEvidence(nil), p.evidence...)
}

// PrunedOptionalEvidence records candidates removed to meet a schema-token
// budget. It is audit-only: a pruned candidate is not a selection and cannot
// be rendered or executed.
func (p LegacyAdapterPlan) PrunedOptionalEvidence() []RoutingEvidence {
	return append([]RoutingEvidence(nil), p.pruned...)
}

func cloneLegacyAdapterSelection(in LegacyAdapterSelection) LegacyAdapterSelection {
	out := in
	out.Effects = append([]EffectClass(nil), in.Effects...)
	return out
}

// BuildLegacyAdapterPlan converts reviewed Router evidence into a plan. It
// never derives a capability from a definition's name or description: every
// selected name must still resolve to a live reviewed provision. This is the
// P1 replacement boundary; Router.Route* remains only a compatibility API.
func BuildLegacyAdapterPlan(input LegacyAdapterPlanInput) (LegacyAdapterPlan, error) {
	policyDigest := strings.TrimSpace(input.PolicyDigest)
	if policyDigest == "" {
		return LegacyAdapterPlan{}, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "policy digest is required")
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	definitions, err := legacyDefinitionSnapshot(input.Definitions)
	if err != nil {
		return LegacyAdapterPlan{}, err
	}

	recommendation := input.Recommendation.clone()
	evidence := append([]RoutingEvidence(nil), recommendation.Evidence...)
	sort.SliceStable(evidence, func(i, j int) bool {
		pi, pj := legacyAdapterReasonPriority(evidence[i].Reason), legacyAdapterReasonPriority(evidence[j].Reason)
		if pi != pj {
			return pi < pj
		}
		if evidence[i].Score != evidence[j].Score {
			return evidence[i].Score > evidence[j].Score
		}
		return evidence[i].ToolName < evidence[j].ToolName
	})
	if len(evidence) > MaxToolBudget {
		return LegacyAdapterPlan{}, legacyAdapterPlanError(LegacyAdapterPlanOverBudget, "%d reviewed selections exceed legacy count guard %d", len(evidence), MaxToolBudget)
	}

	seenNames := make(map[string]bool, len(evidence))
	selections := make([]LegacyAdapterSelection, 0, len(evidence))
	for _, item := range evidence {
		name := strings.TrimSpace(item.ToolName)
		if name == "" || seenNames[name] {
			return LegacyAdapterPlan{}, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "duplicate or empty routing evidence tool name %q", name)
		}
		seenNames[name] = true
		provision, ok := LegacyAdapterProvisionForTool(name, now)
		if !ok {
			return LegacyAdapterPlan{}, legacyAdapterPlanError(LegacyAdapterPlanCatalogIncomplete, "%q has no live reviewed provision", name)
		}
		if item.Capability != provision.Capability || strings.TrimSpace(item.AdapterContract) != provision.AdapterContract {
			return LegacyAdapterPlan{}, legacyAdapterPlanError(LegacyAdapterPlanCatalogIncomplete, "%q routing evidence does not match reviewed provision", name)
		}
		definitionDigest, ok := definitions[name]
		if !ok {
			return LegacyAdapterPlan{}, legacyAdapterPlanError(LegacyAdapterPlanCatalogIncomplete, "%q is not present in the trusted definition snapshot", name)
		}
		selection := LegacyAdapterSelection{
			ToolName:         name,
			Capability:       provision.Capability,
			Owner:            provision.Owner,
			AdapterContract:  provision.AdapterContract,
			ProviderBinding:  legacyAdapterProviderBinding(provision),
			Effects:          append([]EffectClass(nil), provision.Effects...),
			DefinitionDigest: definitionDigest,
			SchemaTokens:     legacyDefinitionTokenEstimate(input.Definitions, name),
			Reason:           strings.TrimSpace(item.Reason),
			Score:            item.Score,
		}
		selection.ID = legacyAdapterSelectionID(selection)
		selections = append(selections, selection)
	}
	pruned := make([]RoutingEvidence, 0)
	if budget := input.SchemaTokenBudget; budget > 0 {
		// Evidence is priority-sorted above. Drop only optional retrieval
		// candidates, starting from the lowest score, and never silently remove
		// bootstrap/current-turn/host-policy/recovery selection.
		for legacySelectionsSchemaTokens(selections) > budget {
			index := legacyLowestPriorityOptionalSelection(selections)
			if index < 0 {
				return LegacyAdapterPlan{}, legacyAdapterPlanError(LegacyAdapterPlanOverBudget, "required legacy selections need %d schema tokens, budget is %d", legacySelectionsSchemaTokens(selections), budget)
			}
			pruned = append(pruned, evidence[index])
			selections = append(selections[:index], selections[index+1:]...)
			evidence = append(evidence[:index], evidence[index+1:]...)
		}
	}

	plan := LegacyAdapterPlan{
		policyDigest:  policyDigest,
		catalogDigest: legacyAdapterCatalogDigest(now),
		searchQuery:   strings.TrimSpace(recommendation.SearchQuery),
		confidence:    recommendation.Confidence,
		schemaTokens:  legacySelectionsSchemaTokens(selections),
		selections:    selections,
		evidence:      evidence,
		pruned:        pruned,
	}
	plan.id = legacyAdapterPlanID(plan)
	return plan, nil
}

func legacyDefinitionTokenEstimate(definitions []map[string]interface{}, name string) int {
	for _, definition := range definitions {
		if strings.TrimSpace(ExtractToolName(definition)) != name {
			continue
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			return 0
		}
		// A conservative local approximation used only by the legacy migration
		// guard. Managed surfaces use the provider-aware schema tokenizer.
		return (len(encoded) + 3) / 4
	}
	return 0
}

func legacySelectionsSchemaTokens(selections []LegacyAdapterSelection) int {
	total := 0
	for _, selection := range selections {
		total += selection.SchemaTokens
	}
	return total
}

func legacyLowestPriorityOptionalSelection(selections []LegacyAdapterSelection) int {
	best := -1
	for i, selection := range selections {
		if strings.TrimSpace(selection.Reason) != "retrieval_candidate" {
			continue
		}
		if best < 0 || selection.Score < selections[best].Score || (selection.Score == selections[best].Score && selection.ToolName > selections[best].ToolName) {
			best = i
		}
	}
	return best
}

// RenderLegacyAdapterPlan is the only legacy-plan renderer. It starts from an
// empty list and includes exactly the selected, digest-bound definitions. It
// cannot union a previous surface, a registry listing, session history, or an
// injection list into the output.
func RenderLegacyAdapterPlan(plan LegacyAdapterPlan, definitions []map[string]interface{}, now time.Time) ([]map[string]interface{}, error) {
	if strings.TrimSpace(plan.id) == "" || strings.TrimSpace(plan.policyDigest) == "" {
		return nil, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "plan identity or policy digest is missing")
	}
	if len(plan.selections) > MaxToolBudget {
		return nil, legacyAdapterPlanError(LegacyAdapterPlanOverBudget, "%d selections exceed legacy count guard %d", len(plan.selections), MaxToolBudget)
	}
	snapshot, err := legacyDefinitionMap(definitions)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]map[string]interface{}, 0, len(plan.selections))
	for _, selection := range plan.selections {
		provision, ok := LegacyAdapterProvisionForTool(selection.ToolName, now)
		if !ok || provision.Capability != selection.Capability || provision.AdapterContract != selection.AdapterContract || legacyAdapterProviderBinding(provision) != selection.ProviderBinding {
			return nil, legacyAdapterPlanError(LegacyAdapterPlanCatalogIncomplete, "%q provision changed or expired after plan creation", selection.ToolName)
		}
		definition, ok := snapshot[selection.ToolName]
		if !ok {
			return nil, legacyAdapterPlanError(LegacyAdapterPlanCatalogIncomplete, "%q definition is absent from renderer snapshot", selection.ToolName)
		}
		digest, err := legacyToolDefinitionDigest(definition)
		if err != nil || digest != selection.DefinitionDigest {
			return nil, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "%q definition digest changed after plan creation", selection.ToolName)
		}
		cloned, ok := cloneJSONValue(definition).(map[string]interface{})
		if !ok {
			return nil, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "%q definition cannot be cloned", selection.ToolName)
		}
		out = append(out, cloned)
	}
	return out, nil
}

func legacyDefinitionSnapshot(definitions []map[string]interface{}) (map[string]string, error) {
	byName, err := legacyDefinitionMap(definitions)
	if err != nil {
		return nil, err
	}
	digests := make(map[string]string, len(byName))
	for name, definition := range byName {
		digest, err := legacyToolDefinitionDigest(definition)
		if err != nil {
			return nil, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "%q definition digest: %v", name, err)
		}
		digests[name] = digest
	}
	return digests, nil
}

func legacyDefinitionMap(definitions []map[string]interface{}) (map[string]map[string]interface{}, error) {
	byName := make(map[string]map[string]interface{}, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(ExtractToolName(definition))
		if name == "" {
			return nil, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "definition has no function name")
		}
		if _, exists := byName[name]; exists {
			return nil, legacyAdapterPlanError(LegacyAdapterPlanInvalid, "duplicate definition %q", name)
		}
		byName[name] = definition
	}
	return byName, nil
}

func legacyToolDefinitionDigest(definition map[string]interface{}) (string, error) {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func legacyAdapterProviderBinding(provision LegacyAdapterProvision) string {
	return "legacy/" + provision.Owner + "/" + provision.AdapterContract + "/" + provision.ToolName
}

func legacyAdapterReasonPriority(reason string) int {
	switch strings.TrimSpace(reason) {
	case "bootstrap":
		return 0
	case "current_turn_required":
		return 1
	case "compatibility_fallback":
		return 2
	case "host_policy_required":
		return 3
	default:
		return 4
	}
}

func legacyAdapterSelectionID(selection LegacyAdapterSelection) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		selection.ToolName, string(selection.Capability), selection.AdapterContract,
		selection.ProviderBinding, selection.DefinitionDigest,
	}, "\n")))
	return "legacy-selection-" + hex.EncodeToString(sum[:12])
}

func legacyAdapterPlanID(plan LegacyAdapterPlan) string {
	parts := []string{plan.policyDigest, plan.catalogDigest, plan.searchQuery, fmt.Sprintf("%.8f", plan.confidence)}
	for _, selection := range plan.selections {
		parts = append(parts, selection.ID)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "legacy-plan-" + hex.EncodeToString(sum[:16])
}

func legacyAdapterCatalogDigest(_ time.Time) string {
	entries := LegacyAdapterProvisions()
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, strings.Join([]string{
			entry.ToolName, string(entry.Capability), entry.Owner, entry.AdapterContract,
			strings.Join(effectClassStrings(entry.Effects), ","), entry.DeleteAfter.UTC().Format(time.RFC3339),
		}, "|"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func effectClassStrings(effects []EffectClass) []string {
	out := make([]string, len(effects))
	for i, effect := range effects {
		out[i] = string(effect)
	}
	sort.Strings(out)
	return out
}
