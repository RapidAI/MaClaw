package tool

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A turn whose outcome is a single fact — one search, one screenshot, one
// commit — is fully described by one selection holding one one-time grant.
// An iterative turn is not: editing code means reading a file, changing it,
// running a check, then reading again. Re-arming a spent grant would dissolve
// the property the whole execution plane rests on, so a bounded repeat is
// expressed the other way around: the plan carries one sibling selection per
// permitted invocation, and the host exposes them one at a time.
//
// Every invariant therefore survives untouched. Each sibling is an immutable
// planned node, carries its own signed grant, and owns exactly one durable
// execution record. The budget is not a runtime counter that a caller could
// raise; it is the number of nodes the plan was published with, visible to
// review and to audit.
const repeatSiblingSeparator = "#"

// RepeatSiblingBudgetLimit caps how many invocations one need may claim. The
// bound exists because the budget materializes as real plan nodes: a rule with
// a runaway count would inflate every plan, revision, and audit record built
// from it.
const RepeatSiblingBudgetLimit = 32

// RepeatSiblingNeedID names the index-th invocation of a repeatable need.
// Index 0 returns the base identity unchanged, so a single-invocation need
// keeps the exact ID it had before repeats existed and no already-published
// plan, durable execution key, or stored grant shifts underneath.
func RepeatSiblingNeedID(baseID string, index int) string {
	baseID = strings.TrimSpace(baseID)
	if index <= 0 {
		return baseID
	}
	return baseID + repeatSiblingSeparator + fmt.Sprintf("%02d", index+1)
}

// RepeatFamilyID collapses a need or selection identity onto the family its
// siblings share. Hosts group by this to keep one live invocation per family;
// an identity without a sibling suffix is its own family of one.
func RepeatFamilyID(id string) string {
	id = strings.TrimSpace(id)
	cut := strings.LastIndex(id, repeatSiblingSeparator)
	if cut <= 0 {
		return id
	}
	if !repeatSiblingIndex(id[cut+len(repeatSiblingSeparator):]) {
		return id
	}
	return id[:cut]
}

// repeatSiblingIndex reports whether the suffix is one this package minted.
// A capability, adapter, or qualifier value is free to contain "#", so the
// suffix only splits a family when it is exactly the generated shape.
func repeatSiblingIndex(suffix string) bool {
	if len(suffix) != 2 {
		return false
	}
	value, err := strconv.Atoi(suffix)
	return err == nil && value >= 2
}

// RepeatSiblingBudget normalizes a declared budget. Zero and one both mean the
// historical single-invocation need, so a rule that says nothing about repeats
// plans exactly as it always has.
func RepeatSiblingBudget(maxInvocations int) int {
	if maxInvocations < 1 {
		return 1
	}
	if maxInvocations > RepeatSiblingBudgetLimit {
		return RepeatSiblingBudgetLimit
	}
	return maxInvocations
}

// RepeatSiblingRequired reports whether the index-th sibling of a required
// template is itself required. Only the first invocation is an obligation;
// later siblings are an exposure ceiling the model may spend.
func RepeatSiblingRequired(templateRequired bool, index int) bool {
	return templateRequired && index <= 0
}

// ExtendRepeatFamily mints additional optional siblings of an already-offered
// family so a later companion template can raise the exposure ceiling without
// minting a second family. from is the first index to emit (the current sibling
// count); budget is the desired RepeatSiblingBudget. Index 0 is never reminted:
// the family base stays the need that already exists. Empty output when the
// family is already at or above budget or the base identity is empty.
func ExtendRepeatFamily(base CapabilityNeed, from, budget int, confidence float64, evidenceIDs []string) []CapabilityNeed {
	budget = RepeatSiblingBudget(budget)
	if from < 1 {
		from = 1
	}
	if from >= budget {
		return nil
	}
	baseID := RepeatFamilyID(base.ID)
	if strings.TrimSpace(baseID) == "" {
		return nil
	}
	evidence := append([]string(nil), evidenceIDs...)
	out := make([]CapabilityNeed, 0, budget-from)
	for index := from; index < budget; index++ {
		sibling := CloneCapabilityNeed(base)
		sibling.ID = RepeatSiblingNeedID(baseID, index)
		sibling.Required = false
		sibling.Confidence = confidence
		sibling.EvidenceIDs = append([]string(nil), evidence...)
		out = append(out, sibling)
	}
	return out
}

// IsRepeatCeilingID reports whether id is a minted sibling other than the
// family base (need#02, selection:need#03). After-edges bind to the
// earliest remaining sibling of the family, which is the base when it
// is still in the plan.
func IsRepeatCeilingID(id string) bool {
	id = strings.TrimSpace(id)
	return id != "" && RepeatFamilyID(id) != id
}

// RepeatExposure is what a host currently knows about its plan's selections,
// stated in the terms every host already keeps.
type RepeatExposure struct {
	// Ready is the plan's currently ready selections.
	Ready []PlannedSelection
	// Completed marks selections with a durable success.
	Completed map[string]bool
	// Granted marks selections already handed a grant, whether or not that
	// grant is still live.
	Granted map[string]bool
	// Live marks selections whose grant has not been consumed yet.
	Live map[string]bool
	// Unsettled optionally reports that a spent selection has no settled
	// outcome. Hosts back it with the durable execution record; leaving it nil
	// means the host cannot tell, and no family is held back.
	Unsettled func(selectionID string) bool
}

// GrantSelectionIDs projects a host grant table onto the Live/Granted
// identity set NextRepeatSelections consumes. GUI and srv both name this
// map differently; the projection stays here so they cannot drift.
func GrantSelectionIDs(grants map[string]InvocationGrant) map[string]bool {
	ids := make(map[string]bool, len(grants))
	for _, grant := range grants {
		if id := strings.TrimSpace(grant.SelectionID); id != "" {
			ids[id] = true
		}
	}
	return ids
}

// LiveGrantNames returns the model-visible names currently holding a grant.
func LiveGrantNames(grants map[string]InvocationGrant) map[string]bool {
	if len(grants) == 0 {
		return nil
	}
	out := make(map[string]bool, len(grants))
	for name := range grants {
		if strings.TrimSpace(name) != "" {
			out[name] = true
		}
	}
	return out
}

// SoleLiveGrantName returns the only live grant name, or empty when the
// surface is holding zero or more than one grant.
func SoleLiveGrantName(grants map[string]InvocationGrant) string {
	if len(grants) != 1 {
		return ""
	}
	for name := range grants {
		return name
	}
	return ""
}

// SoleLiveGrantByAdapter returns the unique live grant bound to adapter.
// Zero or more than one match yields an empty name so a host cannot guess.
func SoleLiveGrantByAdapter(grants map[string]InvocationGrant, adapter string) (string, InvocationGrant) {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		return "", InvocationGrant{}
	}
	var name string
	var grant InvocationGrant
	for grantName, item := range grants {
		if item.AdapterName != adapter {
			continue
		}
		if name != "" {
			return "", InvocationGrant{}
		}
		name = grantName
		grant = item
	}
	return name, grant
}

// LiveGrantNameForCapability returns one live grant whose selection matches
// capability. Empty capability fails closed. Two matches still return one
// name because a petition only needs any already-issued surface for that
// capability; SoleLiveGrantByAdapter remains the unique-adapter probe.
func LiveGrantNameForCapability(plan ToolPlan, grants map[string]InvocationGrant, capability CapabilityID) string {
	if capability == "" {
		return ""
	}
	for name, grant := range grants {
		selection, ok := PlanSelectionByID(plan, grant.SelectionID)
		if ok && selection.FitProof.MatchedCapability == capability {
			return name
		}
	}
	return ""
}

// HasRetiredGrantByAdapter reports a retired grant whose table key or
// AdapterName matches adapter. Recovery uses this so a spent generate_pdf
// or host_document_generate_file cannot be re-issued by name lookup.
func HasRetiredGrantByAdapter(retired map[string]InvocationGrant, adapter string) bool {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		return false
	}
	if _, ok := retired[adapter]; ok {
		return true
	}
	for _, grant := range retired {
		if grant.AdapterName == adapter {
			return true
		}
	}
	return false
}

// HasLiveGrant reports whether name currently holds a live grant.
func HasLiveGrant(grants map[string]InvocationGrant, name string) bool {
	_, ok := grants[strings.TrimSpace(name)]
	return ok
}

// HasKnownGrant reports whether name is live or already retired on this surface.
func HasKnownGrant(live, retired map[string]InvocationGrant, name string) bool {
	name = strings.TrimSpace(name)
	if _, ok := live[name]; ok {
		return true
	}
	_, ok := retired[name]
	return ok
}

// SpentBudgetNoteForGrants is the host-table adapter for RepeatFamilySpentBudgetNote.
func SpentBudgetNoteForGrants(plan ToolPlan, selectionID string, materialized map[string]bool, grants map[string]InvocationGrant) string {
	live := make([]string, 0, len(grants))
	for _, grant := range grants {
		if id := strings.TrimSpace(grant.SelectionID); id != "" {
			live = append(live, id)
		}
	}
	return RepeatFamilySpentBudgetNote(plan, selectionID, materialized, live)
}

// AppendSpentBudgetNote concatenates the spent-budget system note onto a
// successful tool result when the family has no remaining live sibling.
func AppendSpentBudgetNote(result string, plan ToolPlan, selectionID string, materialized map[string]bool, grants map[string]InvocationGrant) string {
	note := SpentBudgetNoteForGrants(plan, selectionID, materialized, grants)
	if note == "" {
		return result
	}
	return result + note
}

// ApplySpentBudgetNote stamps the spent-budget note onto a selection result.
// Headless Execute and GUI result projection both attach the note this way.
func ApplySpentBudgetNote(result SelectionExecutionResult, plan ToolPlan, selectionID string, materialized map[string]bool, grants map[string]InvocationGrant) SelectionExecutionResult {
	result.Result = AppendSpentBudgetNote(result.Result, plan, selectionID, materialized, grants)
	return result
}

// NextExposedSelections is the host-table adapter for NextRepeatSelections.
// GUI refresh and headless Definitions both named this closure differently;
// MaterializeReadySurface computes it here so they cannot drift.
func NextExposedSelections(ready []PlannedSelection, completed, granted map[string]bool, grants map[string]InvocationGrant, unsettled func(string) bool) map[string]bool {
	return NextRepeatSelections(RepeatExposure{
		Ready:     ready,
		Completed: completed,
		Granted:   granted,
		Live:      GrantSelectionIDs(grants),
		Unsettled: unsettled,
	})
}

// NextRepeatSelections chooses which ready selections may hold a grant right
// now. It lives here rather than in either host because the two hosts kept
// separate copies of this closure, and that duplication is exactly how they
// drifted: a budget honored on one and ignored on the other would expose a
// whole invocation allowance at once.
//
// The model must see one call per family at a time. Handing over a whole
// budget would let a single round spend it, and would render one outcome
// repeatedly as if the copies were different actions.
//
// A family of exactly one sibling — every need that declares no budget —
// resolves to the same selection a host picked before repeats existed, which
// is why both hosts can adopt this without changing any existing family.
func NextRepeatSelections(exposure RepeatExposure) map[string]bool {
	liveFamilies := make(map[string]bool, len(exposure.Live))
	for selectionID := range exposure.Live {
		liveFamilies[RepeatFamilyID(selectionID)] = true
	}
	families := make(map[string][]string, len(exposure.Ready))
	for _, selection := range exposure.Ready {
		// A completed node stays in the plan as immutable decision evidence.
		// It must not re-enter the exposure closure merely because nothing
		// depends on it.
		if exposure.Completed[selection.ID] {
			continue
		}
		family := RepeatFamilyID(selection.ID)
		families[family] = append(families[family], selection.ID)
	}
	next := make(map[string]bool, len(families))
	for family, siblings := range families {
		if liveFamilies[family] || repeatFamilyIsUnsettled(exposure, siblings) {
			continue
		}
		sort.Strings(siblings)
		for _, selectionID := range siblings {
			if !exposure.Granted[selectionID] {
				next[selectionID] = true
				break
			}
		}
	}
	return next
}

// repeatFamilyIsUnsettled holds a family back while one of its spent siblings
// has no settled outcome. A failed attempt costs its budget and the family
// moves on, but an operation awaiting a transport receipt, still running, or
// whose result was lost must never be followed by another attempt: spending
// budget on a new call is not the same act as retrying an effect that may
// already have happened.
func repeatFamilyIsUnsettled(exposure RepeatExposure, siblings []string) bool {
	if exposure.Unsettled == nil {
		return false
	}
	for _, selectionID := range siblings {
		if !exposure.Granted[selectionID] || exposure.Completed[selectionID] {
			continue
		}
		if exposure.Unsettled(selectionID) {
			return true
		}
	}
	return false
}

// RepeatFamilySpentBudgetNote reports that the call which just succeeded spent
// the last invocation its family was budgeted for.
//
// A single-invocation family gets nothing. There the tool disappearing is the
// outcome itself, and that is how every family behaved before budgets existed.
// A budgeted family is different: the work was still in progress, so a tool
// vanishing with no explanation invites the model to assume the task is done.
//
// The note rides on the result of the call that spent the budget instead of
// arriving as a separate message. Hosts must compute it after retiring the
// just-spent grant so a still-live sibling suppresses the notice.
func RepeatFamilySpentBudgetNote(plan ToolPlan, selectionID string, materialized map[string]bool, liveSelectionIDs []string) string {
	family := RepeatFamilyID(selectionID)
	budget := 0
	capability := CapabilityID("")
	for _, selection := range plan.Selections {
		if RepeatFamilyID(selection.ID) != family {
			continue
		}
		budget++
		capability = selection.FitProof.MatchedCapability
		if !materialized[selection.ID] {
			return ""
		}
	}
	if budget < 2 {
		return ""
	}
	for _, liveID := range liveSelectionIDs {
		if RepeatFamilyID(liveID) == family {
			return ""
		}
	}
	return fmt.Sprintf("\n\n[system] Planned invocations for %s in this turn (%d) are complete. Continue with the next listed tool, or call office if the user still needs document work. Do not narrate tool limits.", capability, budget)
}
