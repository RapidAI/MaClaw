package tool

import (
	"fmt"
	"strings"
)

// ReplanFailureEligible is the closed lifecycle vocabulary for a single
// binding-invalidated child revision. Schema rejection, unknown external
// effects and awaiting receipts are intentionally excluded.
func ReplanFailureEligible(reasonCode string) bool {
	reasonCode = strings.TrimSpace(strings.ToLower(reasonCode))
	return reasonCode == "dynamic_binding_stale" ||
		strings.HasSuffix(reasonCode, "_binding_stale") ||
		strings.HasSuffix(reasonCode, "_bound_execution_unavailable")
}

// SelectionIsDynamic reports whether a planned provider is a Skill or MCP
// binding. Only those selections may be replaced on a restricted child.
func SelectionIsDynamic(selection PlannedSelection) bool {
	switch strings.ToLower(strings.TrimSpace(selection.Provider.Kind)) {
	case "mcp", "skill":
		return true
	default:
		return false
	}
}

// ValidateReplanSubset requires the child to keep the parent RootTaskID and
// the same need/effect/qualifier/phase/confirmation/parameter authority.
func ValidateReplanSubset(parent, child ToolPlan) error {
	if strings.TrimSpace(parent.RootTaskID) == "" || parent.RootTaskID != child.RootTaskID {
		return fmt.Errorf("semantic replan root task mismatch")
	}
	parentByNeed, ok := replanSelectionsByNeed(parent.Selections)
	if !ok {
		return fmt.Errorf("semantic replan parent needs are not unique")
	}
	childByNeed, ok := replanSelectionsByNeed(child.Selections)
	if !ok {
		return fmt.Errorf("semantic replan child needs are not unique")
	}
	for needID, selection := range childByNeed {
		prior, ok := parentByNeed[needID]
		if !ok || !sameReplanSelectionAuthority(prior, selection) {
			return fmt.Errorf("semantic replan expands authority")
		}
	}
	if len(childByNeed) != len(parentByNeed) || len(child.Unmet) > 0 {
		return fmt.Errorf("semantic replan does not preserve governed needs")
	}
	return nil
}

// ReplanIsBindingOnlyReplacement admits the one narrow difference 11.3
// allows: a selected dynamic implementation may change after its binding
// became stale. Need, effect, artifact and parameter authority may not grow.
func ReplanIsBindingOnlyReplacement(parent, child ToolPlan) bool {
	if strings.TrimSpace(parent.RootTaskID) == "" || parent.RootTaskID != child.RootTaskID || len(parent.Selections) != len(child.Selections) || len(child.Unmet) > 0 {
		return false
	}
	parentByNeed, ok := replanSelectionsByNeed(parent.Selections)
	if !ok {
		return false
	}
	childByNeed, ok := replanSelectionsByNeed(child.Selections)
	if !ok || len(childByNeed) != len(parentByNeed) {
		return false
	}
	for needID, replacement := range childByNeed {
		prior, ok := parentByNeed[needID]
		if !ok || !SelectionIsDynamic(prior) || !sameReplanSelectionAuthorityIgnoringProvider(prior, replacement) {
			return false
		}
	}
	return true
}

// replanSelectionsByNeed makes the need identity a one-to-one correspondence
// across revisions. A count comparison alone is insufficient: a child could
// otherwise repeat one valid need while silently omitting another one.
func replanSelectionsByNeed(selections []PlannedSelection) (map[string]PlannedSelection, bool) {
	byNeed := make(map[string]PlannedSelection, len(selections))
	for _, selection := range selections {
		needID := strings.TrimSpace(selection.NeedID)
		if needID == "" {
			return nil, false
		}
		if _, exists := byNeed[needID]; exists {
			return nil, false
		}
		byNeed[needID] = selection
	}
	return byNeed, true
}

func sameReplanSelectionAuthorityIgnoringProvider(parent, child PlannedSelection) bool {
	return parent.NeedID == child.NeedID &&
		parent.Phase == child.Phase &&
		parent.RequiresConfirm == child.RequiresConfirm &&
		parent.ConfirmationID == child.ConfirmationID &&
		replanParametersDoNotExpand(parent.ParameterAuthorization, child.ParameterAuthorization) &&
		parent.FitProof.MatchedCapability == child.FitProof.MatchedCapability &&
		sameReplanQualifiers(parent.FitProof.QualifierBindings, child.FitProof.QualifierBindings) &&
		sameReplanEffects(parent.Effects, child.Effects) &&
		sameReplanArtifactContracts(parent.Consumes, child.Consumes) &&
		sameReplanArtifactContracts(parent.Produces, child.Produces)
}

func replanParametersDoNotExpand(parent, child ParameterAuthorization) bool {
	if parent.CanonicalizerVer == "" || parent.CanonicalizerVer != child.CanonicalizerVer {
		return false
	}
	return authorizedReferencesSubset(parent.AllowedFields, child.AllowedFields) &&
		authorizedReferencesSubset(parent.AllowedTargets, child.AllowedTargets) &&
		authorizedReferencesSubset(parent.AllowedArtifactIDs, child.AllowedArtifactIDs)
}

func authorizedReferencesSubset(parent, child []string) bool {
	allowed := make(map[string]struct{}, len(parent))
	for _, value := range parent {
		allowed[value] = struct{}{}
	}
	for _, value := range child {
		if _, ok := allowed[value]; !ok {
			return false
		}
	}
	return true
}

func sameReplanSelectionAuthority(parent, child PlannedSelection) bool {
	return parent.NeedID == child.NeedID && parent.Phase == child.Phase && parent.RequiresConfirm == child.RequiresConfirm && parent.ConfirmationID == child.ConfirmationID && parameterAuthorizationsEqual(parent.ParameterAuthorization, child.ParameterAuthorization) && parent.FitProof.MatchedCapability == child.FitProof.MatchedCapability && sameReplanQualifiers(parent.FitProof.QualifierBindings, child.FitProof.QualifierBindings) && sameReplanEffects(parent.Effects, child.Effects) && sameReplanArtifactContracts(parent.Consumes, child.Consumes) && sameReplanArtifactContracts(parent.Produces, child.Produces)
}

func sameReplanQualifiers(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func sameReplanEffects(left, right []EffectClass) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[EffectClass]int, len(left))
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		seen[value]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}

func sameReplanArtifactContracts(left, right []ArtifactContract) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, value := range left {
		seen[value.Kind+"\x00"+value.MIMEType+fmt.Sprintf("\x00%t", value.Required)]++
	}
	for _, value := range right {
		seen[value.Kind+"\x00"+value.MIMEType+fmt.Sprintf("\x00%t", value.Required)]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}
