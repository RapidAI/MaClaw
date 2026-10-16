package guiapp

// coding_scope_admission.go contains the scope-mode admission fence for
// dynamic Skill/MCP discovery.  A scope selector is only a projection of a
// host decision: it may render bindings present in the immutable planner plan
// or in an explicitly host-admitted binding set.  It must never turn the
// connected registry into an implicit allow-list.

import (
	"encoding/json"
	"log"
	"sort"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingSubAgentSkillScopeSchemaDigest mirrors the reviewed dynamic Skill
// invocation schema projection closely enough to compare a locally observed
// entry with a plan that carries the same observed digest. If a host plan
// uses a different/stronger composite binding digest, the comparison remains
// fail-closed rather than silently accepting the entry.
func codingSubAgentSkillScopeSchemaDigest(def NLSkillDefinition) string {
	properties := make(map[string]interface{}, len(def.Params))
	required := make([]string, 0, len(def.Params))
	for _, param := range def.Params {
		name := strings.TrimSpace(param.Name)
		if name == "" || codingScopeSkillReservedParam(name) {
			continue
		}
		kind := strings.TrimSpace(param.Type)
		switch kind {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			kind = "string"
		}
		properties[name] = map[string]interface{}{"type": kind}
		if param.Required {
			required = append(required, name)
		}
	}
	schema := map[string]interface{}{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	encoded, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	return coretool.SchemaDigest([]byte(strings.Join([]string{
		strings.TrimSpace(def.HubVersion),
		codingSubAgentSkillContentDigest(def),
		coretool.SchemaDigest(encoded),
	}, "\x00")))
}

func codingScopeSkillReservedParam(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "name", "skill", "skill_id", "provider", "provider_id", "selection_id", "action", "credential", "credentials", "artifact_id", "artifact_ref":
		return true
	default:
		return false
	}
}

type codingScopeDynamicAdmission struct {
	kind             string
	providerID       string
	implementationID string
	schemaDigest     string
	version          string
	contentDigest    string
	contractDigest   string
}

// setHostAdmittedDynamicBindings is the only callback-local API that may
// provide a dynamic scope without a planner selection.  The caller is a
// trusted host adapter; model text, scores and registry scans never call it.
// An empty pair is meaningful: it explicitly says that this scope has no
// dynamic providers.  Omitting the call means admission is unavailable and
// scope discovery fails closed.
func (c *codingSubAgentCallbacks) setHostAdmittedDynamicBindings(skills []codingSubAgentSkillMatch, mcpTools []codingSubAgentMCPToolMatch) {
	if c == nil {
		return
	}
	c.toolScopeMu.Lock()
	c.hostAdmittedSkills = cloneCodingScopeSkillMatches(skills)
	c.hostAdmittedMCPTools = cloneCodingScopeMCPMatches(mcpTools)
	c.hostDynamicBindingsAdmitted = true
	// A new host admission is a new selection input. Do not retain a previous
	// local result or its fallback snapshot across that boundary.
	c.matchedSkills = nil
	c.matchedMCPTools = nil
	c.matchedSkillsSelected = false
	c.matchedMCPToolsSelected = false
	c.toolSnapshotID = ""
	c.toolSnapshotPlannerDigest = false
	c.toolScopeMu.Unlock()
}

func cloneCodingScopeSkillMatches(values []codingSubAgentSkillMatch) []codingSubAgentSkillMatch {
	if len(values) == 0 {
		return nil
	}
	out := make([]codingSubAgentSkillMatch, len(values))
	for i, value := range values {
		out[i] = value
		out[i].RequiredArgs = append([]string(nil), value.RequiredArgs...)
	}
	return out
}

func cloneCodingScopeMCPMatches(values []codingSubAgentMCPToolMatch) []codingSubAgentMCPToolMatch {
	if len(values) == 0 {
		return nil
	}
	out := make([]codingSubAgentMCPToolMatch, len(values))
	for i, value := range values {
		out[i] = value
		out[i].RequiredArgs = append([]string(nil), value.RequiredArgs...)
		out[i].ArgumentHints = append([]string(nil), value.ArgumentHints...)
	}
	return out
}

// codingScopeDynamicAdmissions returns the exact provider identities admitted
// for one family.  Planner and host admissions are intersected when both are
// present; a disagreement is a fail-closed error rather than an opportunity
// to widen the set.  The bool reports whether an authoritative source exists.
func (c *codingSubAgentCallbacks) codingScopeDynamicAdmissions(kind string) ([]codingScopeDynamicAdmission, bool, string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if c == nil || (kind != "skill" && kind != "mcp") {
		return nil, false, "invalid_kind"
	}
	if !c.scopeBasedSelection {
		return nil, true, "legacy"
	}
	if !c.ensureCodingPlannerSnapshotAdopted() {
		return nil, false, "planner_conflict"
	}

	// Capture callback-owned admission state under its lock. staticShadowPlan
	// is host-owned and immutable after publication; pointer identity is used
	// as its generation fence by ensureCodingPlannerSnapshotAdopted.
	c.toolScopeMu.RLock()
	hostAdmitted := c.hostDynamicBindingsAdmitted
	hostSkills := cloneCodingScopeSkillMatches(c.hostAdmittedSkills)
	hostMCP := cloneCodingScopeMCPMatches(c.hostAdmittedMCPTools)
	plannerConflict := c.toolSnapshotPlannerConflict
	plannerInvalidated := c.toolSnapshotPlannerInvalidated
	invalidatedPlan := c.toolSnapshotPlannerInvalidatedPlan
	c.toolScopeMu.RUnlock()
	if plannerConflict {
		return nil, false, "planner_conflict"
	}
	var staticPlan *codingStaticPlanPreparation
	staticPlan = codingStaticShadowPlanOf(c.subagent)
	if plannerInvalidated && invalidatedPlan == staticPlan {
		// The old preparation remains attached (or no replacement is attached).
		// It is deliberately not an admission source after invalidation.
		staticPlan = nil
	}

	var host []codingScopeDynamicAdmission
	var hostPresent bool
	if hostAdmitted {
		hostPresent = true
		if kind == "skill" {
			for _, value := range hostSkills {
				admission, ok := codingScopeAdmissionFromSkill(value)
				if !ok {
					return nil, false, "host_binding_invalid"
				}
				host = append(host, admission)
			}
		} else {
			for _, value := range hostMCP {
				admission, ok := codingScopeAdmissionFromMCP(value)
				if !ok {
					return nil, false, "host_binding_invalid"
				}
				host = append(host, admission)
			}
		}
		host = dedupeCodingScopeAdmissions(host)
	}

	planner, plannerPresent, plannerReason := codingScopePlanAdmissions(staticPlan, kind)
	if staticPlan != nil && !plannerPresent && plannerReason != "plan_no_dynamic_selection" {
		return nil, false, plannerReason
	}
	if staticPlan != nil && plannerReason == "plan_no_dynamic_selection" && hostPresent && len(host) > 0 {
		// A host list cannot widen a valid planner that explicitly selected no
		// dynamic providers for this scope.
		return nil, false, "scope_admission_conflict"
	}
	if plannerPresent {
		planner = dedupeCodingScopeAdmissions(planner)
	}

	var result []codingScopeDynamicAdmission
	switch {
	case hostPresent && plannerPresent:
		// Require a one-to-one intersection. This prevents a host list from
		// overgranting a plan and prevents a plan from selecting a binding the
		// host did not admit.
		used := make([]bool, len(planner))
		for _, h := range host {
			found := -1
			for i, p := range planner {
				if used[i] || !codingScopeAdmissionCompatible(h, p) {
					continue
				}
				if found >= 0 {
					return nil, false, "scope_admission_ambiguous"
				}
				found = i
			}
			if found < 0 {
				return nil, false, "scope_admission_conflict"
			}
			used[found] = true
			result = append(result, mergeCodingScopeAdmissions(h, planner[found]))
		}
		if len(result) != len(planner) {
			return nil, false, "scope_admission_conflict"
		}
	case hostPresent:
		result = host
	case plannerPresent:
		result = planner
	default:
		// A valid static plan with no dynamic selections is an explicit empty
		// dynamic surface. A missing plan/admission is also closed, but carries
		// a distinct diagnostic reason for operators.
		if staticPlan != nil {
			return nil, true, "plan_no_dynamic_selection"
		}
		return nil, false, "scope_admission_missing"
	}
	if len(result) == 0 {
		return nil, true, "scope_admission_empty"
	}
	sort.SliceStable(result, func(i, j int) bool {
		return codingScopeAdmissionKey(result[i]) < codingScopeAdmissionKey(result[j])
	})
	return result, true, "scope_admission_ready"
}

func codingScopePlanAdmissions(prepared *codingStaticPlanPreparation, kind string) ([]codingScopeDynamicAdmission, bool, string) {
	if prepared == nil {
		return nil, false, "plan_missing"
	}
	plan := prepared.Plan
	if strings.TrimSpace(plan.ID) == "" || strings.TrimSpace(plan.RootTaskID) == "" || strings.TrimSpace(plan.SnapshotDigest) == "" {
		return nil, false, "plan_invalid_identity"
	}
	if len(plan.Unmet) > 0 {
		// A plan with required/unresolved needs is only a planning artifact; a
		// partial selection set must never be promoted into a model surface.
		return nil, false, "plan_unmet"
	}
	result := make([]codingScopeDynamicAdmission, 0)
	for _, selection := range plan.Selections {
		providerKind := strings.ToLower(strings.TrimSpace(selection.Provider.Kind))
		if (providerKind == "skill" || providerKind == "mcp") &&
			(strings.TrimSpace(selection.Provider.ProviderID) == "" || strings.TrimSpace(selection.Provider.ImplementationID) == "") {
			return nil, false, "plan_binding_invalid"
		}
		if providerKind != kind {
			continue
		}
		providerID := strings.TrimSpace(selection.Provider.ProviderID)
		implementationID := strings.TrimSpace(selection.Provider.ImplementationID)
		if providerID == "" || implementationID == "" {
			return nil, false, "plan_binding_invalid"
		}
		result = append(result, codingScopeDynamicAdmission{
			kind: kind, providerID: providerID, implementationID: implementationID,
			schemaDigest: strings.TrimSpace(selection.Provider.SchemaDigest),
		})
	}
	if len(result) == 0 {
		return nil, false, "plan_no_dynamic_selection"
	}
	return result, true, "planner"
}

func codingScopeAdmissionFromSkill(value codingSubAgentSkillMatch) (codingScopeDynamicAdmission, bool) {
	providerID := strings.TrimSpace(value.StableID)
	if providerID == "" {
		providerID = strings.TrimSpace(value.QualifiedID)
	}
	if providerID == "" || strings.TrimSpace(value.Name) == "" {
		return codingScopeDynamicAdmission{}, false
	}
	return codingScopeDynamicAdmission{
		kind: "skill", providerID: providerID, implementationID: strings.TrimSpace(value.Name),
		schemaDigest: strings.TrimSpace(value.SchemaDigest), version: strings.TrimSpace(value.Version),
		contentDigest: strings.TrimSpace(value.ContentDigest), contractDigest: strings.TrimSpace(value.ContractDigest),
	}, true
}

func codingScopeAdmissionFromMCP(value codingSubAgentMCPToolMatch) (codingScopeDynamicAdmission, bool) {
	if strings.TrimSpace(value.ServerID) == "" || strings.TrimSpace(value.ToolName) == "" {
		return codingScopeDynamicAdmission{}, false
	}
	return codingScopeDynamicAdmission{
		kind: "mcp", providerID: strings.TrimSpace(value.ServerID), implementationID: strings.TrimSpace(value.ToolName),
		schemaDigest: strings.TrimSpace(value.SchemaDigest), contractDigest: strings.TrimSpace(value.ContractDigest),
	}, true
}

func dedupeCodingScopeAdmissions(values []codingScopeDynamicAdmission) []codingScopeDynamicAdmission {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]codingScopeDynamicAdmission, 0, len(values))
	for _, value := range values {
		key := codingScopeAdmissionKey(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func codingScopeAdmissionKey(value codingScopeDynamicAdmission) string {
	return strings.Join([]string{value.kind, value.providerID, value.implementationID, value.schemaDigest, value.version, value.contentDigest, value.contractDigest}, "\x00")
}

func codingScopeAdmissionCompatible(left, right codingScopeDynamicAdmission) bool {
	if left.kind != right.kind || left.providerID != right.providerID || left.implementationID != right.implementationID {
		return false
	}
	// The planner side is authoritative. A host observation may be richer
	// than an old planner row, but it may not omit a concrete planner identity:
	// otherwise a changed schema/version could be rendered under an old grant.
	return codingScopeOptionalEqual(right.schemaDigest, left.schemaDigest) &&
		codingScopeOptionalEqual(right.version, left.version) &&
		codingScopeOptionalEqual(right.contentDigest, left.contentDigest) &&
		codingScopeOptionalEqual(right.contractDigest, left.contractDigest)
}

func codingScopeOptionalEqual(expected, observed string) bool {
	expected, observed = strings.TrimSpace(expected), strings.TrimSpace(observed)
	// Empty expected values represent a pre-digest legacy planner row and
	// therefore accept a concrete observation. A concrete expected identity
	// requires an equally concrete, exact observation.
	return expected == "" || (observed != "" && expected == observed)
}

func mergeCodingScopeAdmissions(host, planner codingScopeDynamicAdmission) codingScopeDynamicAdmission {
	result := planner
	if result.schemaDigest == "" {
		result.schemaDigest = host.schemaDigest
	}
	if result.version == "" {
		result.version = host.version
	}
	if result.contentDigest == "" {
		result.contentDigest = host.contentDigest
	}
	if result.contractDigest == "" {
		result.contractDigest = host.contractDigest
	}
	return result
}

func codingScopeSkillProviderIDMatches(admission codingScopeDynamicAdmission, value codingSubAgentSkillMatch) bool {
	want := admission.providerID
	if want == strings.TrimSpace(value.StableID) || want == strings.TrimSpace(value.QualifiedID) {
		return true
	}
	// Dynamic inventory uses this deterministic fallback for legacy skills.
	return want == "legacy:"+strings.ToLower(strings.TrimSpace(value.Name))
}

func codingScopeSkillMatches(admission codingScopeDynamicAdmission, value codingSubAgentSkillMatch) bool {
	if admission.kind != "skill" || !codingScopeSkillProviderIDMatches(admission, value) || strings.TrimSpace(value.Name) != admission.implementationID {
		return false
	}
	return codingScopeOptionalEqual(admission.schemaDigest, value.SchemaDigest) &&
		codingScopeOptionalEqual(admission.version, value.Version) &&
		codingScopeOptionalEqual(admission.contentDigest, value.ContentDigest) &&
		codingScopeOptionalEqual(admission.contractDigest, value.ContractDigest)
}

func codingScopeMCPMatches(admission codingScopeDynamicAdmission, value codingSubAgentMCPToolMatch) bool {
	if admission.kind != "mcp" || strings.TrimSpace(value.ServerID) != admission.providerID || strings.TrimSpace(value.ToolName) != admission.implementationID {
		return false
	}
	return codingScopeOptionalEqual(admission.schemaDigest, value.SchemaDigest) &&
		codingScopeOptionalEqual(admission.contractDigest, value.ContractDigest)
}

func (c *codingSubAgentCallbacks) filterCodingScopeSkills(values []codingSubAgentSkillMatch) []codingSubAgentSkillMatch {
	admissions, authoritative, reason := c.codingScopeDynamicAdmissions("skill")
	if !authoritative || len(admissions) == 0 {
		log.Printf("[coding-subagent] skill scope admission closed reason=%s candidates=%d", reason, len(values))
		return nil
	}
	result := make([]codingSubAgentSkillMatch, 0, len(admissions))
	for _, admission := range admissions {
		found := -1
		for i := range values {
			if !codingScopeSkillMatches(admission, values[i]) {
				continue
			}
			if found >= 0 {
				log.Printf("[coding-subagent] skill scope admission closed reason=ambiguous_binding provider=%s implementation=%s", admission.providerID, admission.implementationID)
				return nil
			}
			found = i
		}
		if found < 0 {
			log.Printf("[coding-subagent] skill scope admission closed reason=binding_missing provider=%s implementation=%s", admission.providerID, admission.implementationID)
			return nil
		}
		value := values[found]
		value.Score = 1
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(result[i].QualifiedID) + "\x00" + strings.TrimSpace(result[i].Name))
		right := strings.ToLower(strings.TrimSpace(result[j].QualifiedID) + "\x00" + strings.TrimSpace(result[j].Name))
		return left < right
	})
	return result
}

func (c *codingSubAgentCallbacks) filterCodingScopeMCP(values []codingSubAgentMCPToolMatch) []codingSubAgentMCPToolMatch {
	admissions, authoritative, reason := c.codingScopeDynamicAdmissions("mcp")
	if !authoritative || len(admissions) == 0 {
		log.Printf("[coding-subagent] MCP scope admission closed reason=%s candidates=%d", reason, len(values))
		return nil
	}
	result := make([]codingSubAgentMCPToolMatch, 0, len(admissions))
	for _, admission := range admissions {
		found := -1
		for i := range values {
			if !codingScopeMCPMatches(admission, values[i]) {
				continue
			}
			if found >= 0 {
				log.Printf("[coding-subagent] MCP scope admission closed reason=ambiguous_binding provider=%s implementation=%s", admission.providerID, admission.implementationID)
				return nil
			}
			found = i
		}
		if found < 0 {
			log.Printf("[coding-subagent] MCP scope admission closed reason=binding_missing provider=%s implementation=%s", admission.providerID, admission.implementationID)
			return nil
		}
		value := values[found]
		value.Score = 1
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(result[i].ServerID) + "\x00" + strings.TrimSpace(result[i].ToolName))
		right := strings.ToLower(strings.TrimSpace(result[j].ServerID) + "\x00" + strings.TrimSpace(result[j].ToolName))
		return left < right
	})
	return result
}
