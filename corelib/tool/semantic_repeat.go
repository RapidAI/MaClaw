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

// RepeatSiblingBudgetLimit caps how many invocations one need publishes up
// front. The bound exists because the budget materializes as real plan nodes:
// a rule with a runaway count would inflate every plan, revision, and audit
// record built from it.
const RepeatSiblingBudgetLimit = 32

// MaxRepeatFamilyInvocations is the absolute ceiling on nodes one family may
// hold. For a budgeted family the published wave is the budget. After it is
// spent the host may open one continuation sibling, and that sibling does not
// raise the budget again. A note that says "call this tool again" must not
// mint the next node forever (production 2026-10-08: bash #02 through #23,
// each success echoing a petition for knowledge_save_text). An iterative
// local file write is not that promise: the next edit is listed because the
// write succeeded, and it may continue until this ceiling.
const MaxRepeatFamilyInvocations = 256

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

// RepeatFamilyKey is the need identity shared by every spelling of one
// repeat family. Selection ids are "selection:" plus that need, and a
// sibling suffix is not part of the key. Grouping by the raw selection id
// treats those spellings as different families, so one turn can hold two
// live grants and AppendRepeatSibling can mint a third spelling.
func RepeatFamilyKey(id string) string {
	return strings.TrimPrefix(RepeatFamilyID(id), "selection:")
}

// SelectionRepeatFamily is the family key of one planned node. The need id
// is the authority. A node stored only as its selection id still joins the
// need it was minted from.
func SelectionRepeatFamily(selection PlannedSelection) string {
	if family := RepeatFamilyKey(selection.NeedID); family != "" {
		return family
	}
	return RepeatFamilyKey(selection.ID)
}

// repeatSiblingIndex reports whether the suffix is one this package minted.
// A capability, adapter, or qualifier value is free to contain "#", so the
// suffix only splits a family when it is exactly the generated shape: the
// zero-padded index RepeatSiblingNeedID emits, at most MaxRepeatFamilyInvocations
// (so two or three digits, value between 2 and that ceiling). Longer digit
// runs cannot be minted and must stay intact.
func repeatSiblingIndex(suffix string) bool {
	if len(suffix) != 2 && len(suffix) != 3 {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	value, err := strconv.Atoi(suffix)
	return err == nil && value >= 2 && value <= MaxRepeatFamilyInvocations
}

// AppendRepeatSibling adds one more optional invocation of a repeat family
// that has already published at least two siblings. One-shot tools stay
// unchanged. A local file write or a remote command continues from a single
// sibling: the session ceiling can publish just one, and that one is not a
// one-shot. The new node is ready on its own; the host issues it when the
// previous call settles. False when the family is missing, is not
// repeatable, or the turn cap is already reached.
func AppendRepeatSibling(plan ToolPlan, prototypeSelectionID string) (ToolPlan, string, bool) {
	prototypeSelectionID = strings.TrimSpace(prototypeSelectionID)
	var prototype PlannedSelection
	found := false
	for _, selection := range plan.Selections {
		if selection.ID == prototypeSelectionID {
			prototype = clonePlannedSelection(selection)
			found = true
			break
		}
	}
	if !found {
		return plan, "", false
	}
	family := SelectionRepeatFamily(prototype)
	if family == "" {
		return plan, "", false
	}
	count, nextIndex := repeatFamilyNextIndex(plan, family)
	// A published wave of one is a one-shot: screenshot, send, generate.
	// Local file write and remote command are not. A session ceiling can
	// leave a single node (production 2026-10-04: references.bib stopped
	// after one append; 2026-10-09: file-cn install stopped after a wave of
	// two ssh calls plus one continuation, and the model was told ssh had
	// reached this turn's usage limit). That node is still the iterative
	// capability, so the next call appends a sibling instead of ending the
	// task.
	// A continuation already in a budgeted non-iterative family is that one
	// extra call. Opening another would make the spent-budget note and this
	// append feed each other. File writes and remote commands do not take
	// that flag: each settled call lists the next one, up to the turn ceiling.
	// The host lists the node. It does not promise another call in prose and
	// then refuse it.
	iterative := IterativeRepeatCapability(prototype)
	// A companion family is the workspace floor, not the task. Its published
	// nodes are the whole allowance. Promising another call and then appending
	// it is how a knowledge-save turn keeps executing bash after the save tool
	// is already listed.
	if count < 1 || count >= MaxRepeatFamilyInvocations || nextIndex < 1 || (count < 2 && !iterative) || repeatFamilyIsCompanionOnly(plan, family) || (!iterative && repeatFamilyHasContinuation(plan, family)) {
		return plan, "", false
	}
	needID := RepeatSiblingNeedID(family, nextIndex)
	sibling := prototype
	sibling.ID = "selection:" + needID
	sibling.NeedID = needID
	// Continuation marks the one extra call of a budgeted family. An
	// iterative sibling is the next step of the same task, including when
	// its prototype was that one extra call. Leaving the flag set would
	// make later readers treat the family as already continued.
	if iterative {
		sibling.Continuation = false
	} else {
		sibling.Continuation = true
	}
	// The extra invocation is ready on its own. Keeping the prototype's
	// producer edges while clearing Requires makes the published plan fail
	// validation, so the 33rd download would never be stored.
	sibling.Requires = nil
	sibling.RequiresConfirm = false
	sibling.ConfirmationID = ""
	sibling.ArtifactDependencies = nil
	sibling.Consumes = nil
	for _, selection := range plan.Selections {
		if selection.ID == sibling.ID || selection.NeedID == sibling.NeedID {
			return plan, "", false
		}
	}
	plan.Selections = append(plan.Selections, sibling)
	return plan, sibling.ID, true
}

// repeatFamilyNextIndex reports how many selections belong to family and the
// next free sibling index. Counting alone collides when a suffix is missing
// or a selection is only recognizable by its selection ID, and AppendRepeatSibling
// then refuses the extra download.
func repeatFamilyNextIndex(plan ToolPlan, family string) (count, nextIndex int) {
	maxSuffix := 0
	for _, selection := range plan.Selections {
		if !SelectionInRepeatFamily(selection, family) {
			continue
		}
		count++
		for _, id := range []string{selection.NeedID, selection.ID} {
			if id == "" {
				continue
			}
			if suffix := repeatSiblingSuffixNumber(id); suffix > maxSuffix {
				maxSuffix = suffix
			}
		}
	}
	if maxSuffix < 1 && count > 0 {
		maxSuffix = 1
	}
	return count, maxSuffix
}

// IterativeRepeatCapability reports a selection the same user task keeps
// calling until the work is done. Local file mutation and remote command
// are that class. A download, search, or send is a published wave plus at
// most one continuation: another node after that would feed the spent-budget
// note. A recorded fit proof is the authority. The need id is only a
// fallback when that proof was not copied onto the selection.
func IterativeRepeatCapability(selection PlannedSelection) bool {
	return IterativeLocalFileWrite(selection) || IterativeRemoteCommand(selection)
}

// IterativeLocalFileWrite reports a selection whose capability is local
// file mutation. write_file and edit_file share fs.write.local. A recorded
// fit proof is the authority: a read or send whose id happens to contain
// the write capability text stays one-shot. The need id is only a fallback
// when that proof was not copied onto the selection.
func IterativeLocalFileWrite(selection PlannedSelection) bool {
	return iterativeCapability(selection, CapabilityFSWriteLocal)
}

// IterativeRemoteCommand reports a selection whose capability is a command
// on a remote host. ssh polls and install steps share shell.execute.remote_host.
// A local shell whose id happens to contain the remote capability text stays
// on the local family's rule.
func IterativeRemoteCommand(selection PlannedSelection) bool {
	return iterativeCapability(selection, CapabilityShellExecuteRemoteHost)
}

func iterativeCapability(selection PlannedSelection, capability CapabilityID) bool {
	if recorded := strings.TrimSpace(string(selection.FitProof.MatchedCapability)); recorded != "" {
		return selection.FitProof.MatchedCapability == capability
	}
	id := strings.TrimSpace(selection.NeedID)
	if id == "" {
		id = strings.TrimSpace(selection.ID)
	}
	return strings.Contains(id, string(capability))
}

func repeatSiblingSuffixNumber(id string) int {
	cut := strings.LastIndex(id, repeatSiblingSeparator)
	if cut <= 0 || !repeatSiblingIndex(id[cut+len(repeatSiblingSeparator):]) {
		return 1
	}
	value, _ := strconv.Atoi(id[cut+len(repeatSiblingSeparator):])
	if value < 2 {
		return 1
	}
	return value
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
		liveFamilies[RepeatFamilyKey(selectionID)] = true
	}
	families := make(map[string][]string, len(exposure.Ready))
	for _, selection := range exposure.Ready {
		// A completed node stays in the plan as immutable decision evidence.
		// It must not re-enter the exposure closure merely because nothing
		// depends on it.
		if exposure.Completed[selection.ID] {
			continue
		}
		family := SelectionRepeatFamily(selection)
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

// RepeatWaveListedMarker is the promise that a spent repeat family can run
// once more on the next model request in the same turn. Hosts that see it on
// a remote-command result open that call instead of ending the turn.
const RepeatWaveListedMarker = "another call will be listed"

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
	family := RepeatFamilyKey(selectionID)
	// The published wave may promise one more call. Once that continuation
	// exists, the promise is the node itself. Another note would instruct the
	// model to call the same tool again, and the host would treat the obedient
	// call as a reason to raise the budget.
	if repeatFamilyHasContinuation(plan, family) || repeatFamilyIsCompanionOnly(plan, family) {
		return ""
	}
	budget := 0
	capability := CapabilityID("")
	iterative := false
	for _, selection := range plan.Selections {
		if !SelectionInRepeatFamily(selection, family) {
			continue
		}
		budget++
		capability = selection.FitProof.MatchedCapability
		if IterativeRepeatCapability(selection) {
			iterative = true
		}
		if !materialized[selection.ID] {
			return ""
		}
	}
	// A file edit lists its next write, and a remote command lists its next
	// call, from the settled attempt. The budget note would tell the model
	// the turn is finished. Production 2026-10-09: after that note the model
	// wrote "稍后自动继续" and the loop ended an unfinished nginx install.
	if budget < 2 || iterative {
		return ""
	}
	for _, liveID := range liveSelectionIDs {
		if SelectionInRepeatFamily(PlannedSelection{ID: liveID}, family) {
			return ""
		}
	}
	return fmt.Sprintf("\n\n[system] Planned invocations for %s in this turn (%d) are complete. If this task is unfinished, call this tool again on the next request in this same turn; %s. Do not ask the user to send another message. Do not narrate tool limits.", capability, budget, RepeatWaveListedMarker)
}

// RepeatFamilyHasUnissuedSibling reports a same-family node, other than
// exceptID, that the host has not issued or completed. done is that check.
// A node still waiting to be issued is the next call; appending another
// would queue a command the model cannot see yet.
func RepeatFamilyHasUnissuedSibling(plan ToolPlan, prototype PlannedSelection, exceptID string, done func(selectionID string) bool) bool {
	family := SelectionRepeatFamily(prototype)
	if family == "" {
		return false
	}
	if done == nil {
		done = func(string) bool { return false }
	}
	for _, other := range plan.Selections {
		if other.ID == exceptID {
			continue
		}
		if !SelectionInRepeatFamily(other, family) {
			continue
		}
		if done(other.ID) {
			continue
		}
		return true
	}
	return false
}

// repeatFamilyHasContinuation reports that this family already holds the one
// sibling opened after its published wave. family is whichever identity the
// caller grouped by: a need id or a selection id.
func repeatFamilyHasContinuation(plan ToolPlan, family string) bool {
	family = strings.TrimSpace(family)
	if family == "" {
		return false
	}
	for _, selection := range plan.Selections {
		if !selection.Continuation {
			continue
		}
		if SelectionInRepeatFamily(selection, family) {
			return true
		}
	}
	return false
}

// RepeatSelectionIsCompanion reports a selection the planner added as a
// workspace floor or an archetype companion. The task's own need does not
// carry these marks, so a raised family that still contains that need stays
// repeatable.
func RepeatSelectionIsCompanion(selection PlannedSelection) bool {
	if strings.Contains(selection.NeedID, "zz-baseline:") || strings.Contains(selection.ID, "zz-baseline:") {
		return true
	}
	for _, evidence := range selection.EvidenceIDs {
		switch evidence {
		case "intent:baseline_workspace", "intent:archetype_bundle":
			return true
		}
	}
	return false
}

// repeatFamilyIsCompanionOnly reports that every node in the family is a
// companion. One task-owned node keeps the family's own repeat rule.
func repeatFamilyIsCompanionOnly(plan ToolPlan, family string) bool {
	seen := false
	for _, selection := range plan.Selections {
		if !SelectionInRepeatFamily(selection, family) {
			continue
		}
		seen = true
		if !RepeatSelectionIsCompanion(selection) {
			return false
		}
	}
	return seen
}

// SelectionInRepeatFamily reports whether selection belongs to family.
// family is a need id or a selection id. Both spellings, with or without
// the "selection:" prefix and with or without a sibling suffix, are one
// family. The hosts use this so an unissued node cannot be missed and
// then appended again under a second spelling.
func SelectionInRepeatFamily(selection PlannedSelection, family string) bool {
	family = RepeatFamilyKey(family)
	if family == "" {
		return false
	}
	for _, candidate := range []string{selection.NeedID, selection.ID} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if RepeatFamilyKey(candidate) == family {
			return true
		}
	}
	return false
}

// SettledIterativeListingAllowed reports that a finished local file write
// or remote command may list its next sibling. Unknown and awaiting-receipt
// outcomes may still be running. Binding recovery replaces the plan.
// Cancellation did not finish the command. Copying any of those into
// another sibling retries a dead binding or a command the turn already stopped.
func SettledIterativeListingAllowed(result SelectionExecutionResult) bool {
	if result.Unknown || result.AwaitingReceipt {
		return false
	}
	code := strings.TrimSpace(result.ReasonCode)
	return code != "dynamic_execution_cancelled" && !ReplanFailureEligible(code)
}
