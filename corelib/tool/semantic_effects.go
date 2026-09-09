package tool

import "strings"

// SelectionRequiresReceipt reports whether a planned selection has an effect
// that cannot be safely inferred from a plain model/tool response.  This is a
// transport-neutral policy primitive shared by GUI and headless hosts.
//
// Local mutations are included deliberately: a host may opt into the more
// specific HostLocalMutationSelection helper when it has an authoritative
// same-process receipt.  Unknown effect sets fail closed and return false
// here; callers should reject unknown plans before reaching this projection.
func SelectionRequiresReceipt(selection PlannedSelection) bool {
	for _, effect := range selection.Effects {
		switch effect {
		case EffectExternalEffect, EffectSensitive, EffectLocalMutation:
			return true
		}
	}
	return false
}

// SelectionRequiresExternalReceipt is the dynamic-provider variant of
// SelectionRequiresReceipt.  Dynamic host adapters treat a local mutation as
// its own authoritative receipt, while external and sensitive effects still
// require a receipt coordinator unless a more specific host observation rule
// exempts them.
func SelectionRequiresExternalReceipt(selection PlannedSelection) bool {
	for _, effect := range selection.Effects {
		if effect == EffectExternalEffect || effect == EffectSensitive {
			return true
		}
	}
	return false
}

// HostLocalMutationSelection identifies a provider-owned local mutation whose
// result is observed by the same host process. Provider kind is part of the
// contract so a Skill/MCP adapter cannot claim the builtin host receipt rule.
func HostLocalMutationSelection(selection PlannedSelection, providerKind string) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), strings.TrimSpace(providerKind)) || strings.TrimSpace(providerKind) == "" {
		return false
	}
	local := false
	for _, effect := range selection.Effects {
		switch effect {
		case EffectSensitive, EffectLocalMutation:
			local = true
		case EffectExternalEffect:
			return false
		}
	}
	return local
}

// HostObservedExternalSelection identifies a host-owned adapter that waits
// for or reads back an external effect before returning. The adapter allowlist
// is supplied by the host because adapter names are host contract data, not a
// property of the planner.
func HostObservedExternalSelection(selection PlannedSelection, providerKind string, adapters ...string) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), strings.TrimSpace(providerKind)) || strings.TrimSpace(providerKind) == "" {
		return false
	}
	external := false
	for _, effect := range selection.Effects {
		if effect == EffectExternalEffect {
			external = true
			break
		}
	}
	if !external {
		return false
	}
	name := strings.TrimSpace(selection.AdapterName)
	if name == "" {
		return false
	}
	for _, adapter := range adapters {
		if name == strings.TrimSpace(adapter) {
			return true
		}
	}
	return false
}

// PlanHasCapability is the shared immutable-plan lookup used by host
// petition/expansion paths. Keeping it beside PlannedSelection prevents GUI
// and srv from reimplementing capability scans with subtly different rules.
func PlanHasCapability(plan ToolPlan, capability CapabilityID) bool {
	if capability == "" {
		return false
	}
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == capability {
			return true
		}
	}
	return false
}

// IsLookupSelection reports whether a planned selection belongs to the
// evidence-producing lookup family.  Selection-level callers should use this
// helper instead of reading FitProof.MatchedCapability directly; keeping the
// projection beside IsLookupCapability ensures GUI, headless and future TUI
// hosts agree when a new lookup capability is added.
func IsLookupSelection(selection PlannedSelection) bool {
	return IsLookupCapability(selection.FitProof.MatchedCapability)
}

// CapabilityNeedHasSideEffect reports whether replaying this need would redo
// a local mutation or external effect. A registry descriptor's Effects win
// when present: empty or any non-read-only effect is a side effect. Unknown
// capabilities default to side-effect so a later mutation family is not
// dropped from continuation. Empty capability IDs are not replayable.
func CapabilityNeedHasSideEffect(registry *CapabilityRegistry, need CapabilityNeed) bool {
	capability := strings.TrimSpace(string(need.Capability))
	if capability == "" {
		return false
	}
	if registry != nil {
		if descriptor, ok := registry.Lookup(need.Capability); ok {
			if len(descriptor.Effects) == 0 {
				return true
			}
			for _, effect := range descriptor.Effects {
				if effect != EffectReadOnly {
					return true
				}
			}
			return false
		}
	}
	return !readOnlySessionGovernedCapability(capability)
}

// CapabilityNeedsHaveSideEffect reports whether any granted need is a
// mutation or external effect. GUI session-governed replay and headless
// SessionGovernedTaskStore share this so a succeeded lookup cannot drift
// into an unfinished mutation on one host only.
func CapabilityNeedsHaveSideEffect(registry *CapabilityRegistry, needs []CapabilityNeed) bool {
	for _, need := range needs {
		if CapabilityNeedHasSideEffect(registry, need) {
			return true
		}
	}
	return false
}

func readOnlySessionGovernedCapability(capability string) bool {
	switch {
	case capability == "information.lookup",
		capability == "information.current_time",
		capability == string(CapabilityInteractionAskUser),
		capability == string(CapabilityMemoryRecallAgent),
		capability == string(CapabilityBusinessDataRead):
		return true
	case strings.HasPrefix(capability, "information.search."),
		strings.HasPrefix(capability, "information.fetch."),
		strings.HasPrefix(capability, "document.read."),
		strings.HasPrefix(capability, "fs.read."),
		strings.HasPrefix(capability, "repo.inspect."),
		strings.HasPrefix(capability, "knowledge.read."),
		strings.HasPrefix(capability, "security.audit."),
		strings.HasPrefix(capability, "audio.transcribe."),
		strings.HasPrefix(capability, "governance.inspect."):
		return true
	default:
		return false
	}
}

// PlanWithSelections returns a copy of plan containing only the explicitly
// allowed selection IDs. The immutable plan metadata and all non-selection
// diagnostics are retained, while selection order remains planner order.
func PlanWithSelections(plan ToolPlan, allowed map[string]bool) ToolPlan {
	filtered := plan
	filtered.Selections = make([]PlannedSelection, 0, len(allowed))
	for _, selection := range plan.Selections {
		if allowed != nil && allowed[selection.ID] {
			filtered.Selections = append(filtered.Selections, selection)
		}
	}
	return filtered
}

// PlanSelectionByID returns the immutable selection identified by ID. It is
// shared by GUI and headless execution paths so stale-plan lookups do not
// accidentally gain different trim or matching semantics.
func PlanSelectionByID(plan ToolPlan, selectionID string) (PlannedSelection, bool) {
	for _, selection := range plan.Selections {
		if selection.ID == selectionID {
			return selection, true
		}
	}
	return PlannedSelection{}, false
}
