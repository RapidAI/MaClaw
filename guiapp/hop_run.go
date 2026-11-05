package guiapp

import (
	"errors"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const hopRunSpecVersion = "hop-run/v1"

// hopCurrentChannelDeliveryCapability is the capability the planner records for
// a delivery into the host's own conversation. That step's receipt is this
// turn's response projection, not a transport worker's later callback.
const hopCurrentChannelDeliveryCapability = "artifact.deliver.current_channel"

// A light lookup admits search and fetch as two ways to get the same fact.
// Search is the required plan step. Fetch is the other admitted provider:
// the model uses it when search times out, and a resume replans that one
// search as a step this plan has not started. A trusted fetch success is
// that fact only when the plan has a single required search, and that
// search has not acquired its own record: it failed, or it was never
// started. A second required search is a different fact, failed or not.
// A search this plan has already acquired stays its own record.
const (
	hopInformationSearchWeb = "information.search.web"
	hopInformationFetchWeb  = "information.fetch.web"
)

type HopStepStatus string

const (
	HopStepPending   HopStepStatus = "pending"
	HopStepRunning   HopStepStatus = "running"
	HopStepCompleted HopStepStatus = "completed"
	HopStepFailed    HopStepStatus = "failed"
	HopStepUnsettled HopStepStatus = "unsettled"
	HopStepUnmet     HopStepStatus = "unmet"
	HopStepOptional  HopStepStatus = "optional"
)

type HopRunStep struct {
	SelectionID string        `json:"selection_id"`
	NeedID      string        `json:"need_id"`
	Capability  string        `json:"capability"`
	Required    bool          `json:"required"`
	DependsOn   []string      `json:"depends_on,omitempty"`
	Status      HopStepStatus `json:"status"`
	// Superseded is set when this step did not reach a trusted success but
	// the obligation was settled another way: a later sibling in the same
	// repeat family succeeded, or the web-search fact was delivered by a
	// trusted web fetch. The step keeps its own evidence for audit but no
	// longer blocks the run.
	Superseded bool            `json:"superseded,omitempty"`
	Evidence   HopStepEvidence `json:"evidence"`
}

type HopStepEvidence struct {
	State        tool.PlanExecutionState `json:"state,omitempty"`
	ResultDigest string                  `json:"result_digest,omitempty"`
	ReasonCode   string                  `json:"reason_code,omitempty"`
}

type HopRunSnapshot struct {
	SpecVersion string       `json:"spec_version"`
	RunID       string       `json:"run_id"`
	Goal        string       `json:"goal"`
	RootTaskID  string       `json:"root_task_id"`
	PlanID      string       `json:"plan_id"`
	Status      string       `json:"status"`
	Remaining   int          `json:"remaining"`
	Steps       []HopRunStep `json:"steps"`
}

func (c *sharedAgentLoopCallbacks) hopRunSnapshot() *HopRunSnapshot {
	if c == nil || c.semanticSurface == nil {
		return nil
	}
	surface := c.semanticSurface
	// A recorded plan's Optional bit is the obligation. Rebuilding the
	// classification map here only repeated work the planner already finished.
	var optional map[string]bool
	var optionalKnown bool
	if !surface.plan.ObligationsRecorded {
		optional, optionalKnown = semanticSurfaceOptionalNeeds(surface)
	}
	satisfiedFamilies := satisfiedHopFamilies(surface)
	fetchSettledSearch := informationFetchSucceeded(surface)
	soleSearch := soleRequiredWebSearch(surface, optional, optionalKnown)
	snapshot := &HopRunSnapshot{
		SpecVersion: hopRunSpecVersion,
		RunID:       c.checkpointRunID,
		Goal:        c.userText,
		RootTaskID:  surface.plan.RootTaskID,
		PlanID:      surface.plan.ID,
		Status:      "running",
		Steps:       make([]HopRunStep, 0, len(surface.plan.Selections)+len(surface.plan.Unmet)),
	}
	for _, selection := range surface.plan.Selections {
		required := hopSelectionRequired(selection, optional, optionalKnown, surface.plan.ObligationsRecorded)
		step := HopRunStep{
			SelectionID: selection.ID,
			NeedID:      selection.NeedID,
			Capability:  string(selection.FitProof.MatchedCapability),
			Required:    required,
			DependsOn:   append([]string(nil), selection.Requires...),
			Status:      HopStepOptional,
		}
		step.Evidence = hopSelectionEvidence(surface, selection.ID)
		// A required step whose repeat family another selection settled no
		// longer blocks the run: the base attempt may fail while a later
		// sibling succeeds, and the family need is then satisfied. The step
		// keeps its own (failed/unsettled) evidence and is marked superseded.
		superseded := required && step.Evidence.State != tool.PlanExecutionSucceeded && satisfiedFamilies[tool.RepeatFamilyID(selection.NeedID)]
		if !superseded && fetchSettledSearch && required && step.Capability == hopInformationSearchWeb &&
			webSearchRecoveredByFetch(step.Evidence.State, soleSearch) {
			superseded = true
		}
		if superseded {
			step.Superseded = true
		}
		// No execution record means the selection has not started. Repeat
		// slots stay optional until a call acquires them. A real running
		// record keeps that status; inventing one left unused slots looking
		// in-flight after a successful turn.
		switch step.Evidence.State {
		case tool.PlanExecutionSucceeded:
			step.Status = HopStepCompleted
		case tool.PlanExecutionFailed:
			step.Status = HopStepFailed
			if required && !superseded {
				snapshot.Remaining++
			}
		case tool.PlanExecutionUnknown:
			step.Status = HopStepUnsettled
			if required && !superseded {
				snapshot.Remaining++
			}
		case tool.PlanExecutionAwaitingReceipt, tool.PlanExecutionRunning:
			step.Status = HopStepRunning
			if required && !superseded {
				snapshot.Remaining++
			}
		default:
			if required {
				step.Status = HopStepPending
				if !superseded {
					snapshot.Remaining++
				}
			}
		}
		snapshot.Steps = append(snapshot.Steps, step)
	}
	selectedNeeds := make(map[string]bool, len(snapshot.Steps))
	for _, step := range snapshot.Steps {
		selectedNeeds[strings.TrimSpace(step.NeedID)] = true
	}
	for _, unmet := range surface.plan.Unmet {
		if !unmetStillOwed(unmet, selectedNeeds, optional, optionalKnown) {
			continue
		}
		snapshot.Remaining++
		snapshot.Steps = append(snapshot.Steps, HopRunStep{
			SelectionID: "unmet:" + unmet.NeedID,
			NeedID:      unmet.NeedID,
			Capability:  unmet.NeedID,
			Required:    true,
			Status:      HopStepUnmet,
			Evidence:    HopStepEvidence{State: tool.PlanExecutionUnknown, ReasonCode: unmet.ReasonCode},
		})
	}
	return snapshot
}

// requiredEffectUnsettled reports required work this turn never dispatched.
//
// A step that already owns an execution record is not the model's to finish:
// another model round cannot move it. That is why a file delivery the host has
// already handed to this request's own transport does not block the stop. The
// response projection (current channel) or the channel gateway (specified
// target) produces that receipt after the loop returns; counting it here made
// every file turn end in a required_effect_unsettled hard exit after two
// no-tool stops, and the kept recovery marker then refused the next message as
// an uncertain external send.
//
// Every other unsettled state still blocks, because it either has no record
// (pending), failed, is genuinely unknown, or still waits for a decision the
// model's next round must not skip (a mailbox confirmation). Those are exactly
// the states where ending the turn would report the effect as done.
func requiredEffectUnsettled(snapshot *HopRunSnapshot) bool {
	if snapshot == nil {
		return false
	}
	for _, step := range snapshot.Steps {
		if !step.Required || step.Superseded || step.Status == HopStepCompleted {
			continue
		}
		if hostTransportDeliveryStep(step) {
			continue
		}
		return true
	}
	return false
}

// hostTransportDeliveryStep identifies a file delivery whose receipt is
// produced by this request's own transport, not by a later model round:
//
//   - current channel: the response projection attaches the bytes to this
//     conversation, so the projection is the delivery act;
//   - specified target: the channel gateway sends the reply attachment after
//     the loop returns, so the send is already committed to this request.
//
// Both are dispatched work. A mailbox send waiting for confirmation is not:
// its receipt is a human decision the pause mechanism owns, so it keeps the
// run open.
func hostTransportDeliveryStep(step HopRunStep) bool {
	if step.Status != HopStepRunning || step.Evidence.State != tool.PlanExecutionAwaitingReceipt {
		return false
	}
	capability := strings.TrimSpace(step.Capability)
	return strings.EqualFold(capability, hopCurrentChannelDeliveryCapability) ||
		strings.EqualFold(capability, string(semanticSpecifiedTargetDeliveryCapability))
}

// webSearchRecoveredByFetch reports that a trusted fetch already delivered
// the fact this required search owed. That is true only for the plan's
// single required search, and only when that search failed or was never
// started. One page read cannot settle a second search. A search this plan
// has acquired — running, unknown, or awaiting a receipt — stays open.
func webSearchRecoveredByFetch(state tool.PlanExecutionState, soleRequired bool) bool {
	if !soleRequired {
		return false
	}
	switch state {
	case tool.PlanExecutionFailed, "":
		return true
	default:
		return false
	}
}

// soleRequiredWebSearch reports that the plan has exactly one required web
// search. Optional repeat slots are not separate obligations. A second
// required search is a different fact and stays owed after a fetch.
func soleRequiredWebSearch(surface *semanticCallSurface, optional map[string]bool, optionalKnown bool) bool {
	if surface == nil {
		return false
	}
	required := 0
	for _, selection := range surface.plan.Selections {
		if string(selection.FitProof.MatchedCapability) != hopInformationSearchWeb {
			continue
		}
		if !hopSelectionRequired(selection, optional, optionalKnown, surface.plan.ObligationsRecorded) {
			continue
		}
		required++
		if required > 1 {
			return false
		}
	}
	return required == 1
}

// hopSelectionEvidence is the execution fact the hop snapshot will show for
// a selection, before that snapshot copies it onto the step. A host-completed
// selection is a trusted success. A missing record is unstarted. A store read
// that fails is not the same fact: it stays unknown, and it is not cached,
// so a later read can still see the record. A terminal store record is
// cached, so a later read does not ask the store again.
func hopSelectionEvidence(surface *semanticCallSurface, selectionID string) HopStepEvidence {
	if surface == nil {
		return HopStepEvidence{}
	}
	evidenceKey := hopEvidenceKey(surface, selectionID)
	var evidence HopStepEvidence
	if cached, ok := surface.hopEvidence[evidenceKey]; ok {
		evidence = cached
	} else if surface.executor != nil {
		record, err := surface.executor.Execution(surface.scope, selectionID)
		switch {
		case err == nil:
			evidence = HopStepEvidence{State: record.State, ResultDigest: record.ResultDigest, ReasonCode: record.ReasonCode}
			if record.State == tool.PlanExecutionSucceeded || record.State == tool.PlanExecutionFailed {
				if surface.hopEvidence == nil {
					surface.hopEvidence = map[string]HopStepEvidence{}
				}
				surface.hopEvidence[evidenceKey] = evidence
			}
		case errors.Is(err, tool.ErrPlanExecutionNotFound):
		default:
			evidence = HopStepEvidence{State: tool.PlanExecutionUnknown, ReasonCode: "execution_unreadable"}
		}
	}
	if surface.completed[selectionID] {
		digest := ""
		if evidence.State == tool.PlanExecutionSucceeded {
			digest = evidence.ResultDigest
		}
		evidence = HopStepEvidence{State: tool.PlanExecutionSucceeded, ResultDigest: digest}
	}
	return evidence
}

// informationFetchSucceeded reports a trusted information.fetch.web success
// in this plan. The snapshot uses it only when one required search is the
// fact that page can stand for.
func informationFetchSucceeded(surface *semanticCallSurface) bool {
	if surface == nil {
		return false
	}
	for _, selection := range surface.plan.Selections {
		if string(selection.FitProof.MatchedCapability) != hopInformationFetchWeb {
			continue
		}
		if hopSelectionEvidence(surface, selection.ID).State == tool.PlanExecutionSucceeded {
			return true
		}
	}
	return false
}

func hopEvidenceKey(surface *semanticCallSurface, selectionID string) string {
	if surface == nil {
		return selectionID
	}
	return surface.plan.ID + "\x00" + selectionID
}

// satisfiedHopFamilies returns every repeat family whose obligation a
// trusted success settled: a selection the host completed, or a durable
// succeeded execution record. One scope query covers the store. A cached
// terminal record still wins over that query, matching hopSelectionEvidence,
// so a snapshot cannot call the store once per selection while deciding
// which families are done. A base attempt may fail while a later sibling
// succeeds; the family need is then satisfied.
func satisfiedHopFamilies(surface *semanticCallSurface) map[string]bool {
	satisfied := map[string]bool{}
	if surface == nil {
		return satisfied
	}
	var succeeded map[string]bool
	if surface.executor != nil {
		if ids, err := surface.executor.Completed(surface.scope); err == nil {
			succeeded = ids
		}
	}
	for _, selection := range surface.plan.Selections {
		family := tool.RepeatFamilyID(selection.NeedID)
		if family == "" || satisfied[family] {
			continue
		}
		if hopFamilySettled(surface, selection.ID, succeeded) {
			satisfied[family] = true
		}
	}
	return satisfied
}

func hopFamilySettled(surface *semanticCallSurface, selectionID string, succeeded map[string]bool) bool {
	if surface == nil {
		return false
	}
	if cached, ok := surface.hopEvidence[hopEvidenceKey(surface, selectionID)]; ok && !surface.completed[selectionID] {
		return cached.State == tool.PlanExecutionSucceeded
	}
	return surface.completed[selectionID] || succeeded[selectionID]
}

// completedWithSatisfiedFamilies overlays repeat families a trusted success
// settled onto the completed view. Gates that key off raw completed (file
// deliverable settlement, delivery-complete early stop) must see the failed
// base as done once a sibling satisfied the family need. The surface's own
// completed map is never mutated, and the overlay is dropped when no family
// settled so the common path costs nothing.
func completedWithSatisfiedFamilies(surface *semanticCallSurface) map[string]bool {
	if surface == nil {
		return nil
	}
	families := satisfiedHopFamilies(surface)
	if len(families) == 0 {
		return surface.completed
	}
	completed := make(map[string]bool, len(surface.completed)+len(surface.plan.Selections))
	for id, ok := range surface.completed {
		if ok {
			completed[id] = true
		}
	}
	for _, selection := range surface.plan.Selections {
		if families[tool.RepeatFamilyID(selection.NeedID)] {
			completed[selection.ID] = true
		}
	}
	return completed
}

// unmetStillOwed is the completion contract for one Unmet row.
// A selection that already carries the need owns that contract. Budget cuts
// and dropped installed MCP rows are the ones the host already admitted
// when it published the plan; the model has no call that can settle them.
// Any other authoring fault stays owed. An optional map entry releases a
// row only when it is not an authoring fault.
func unmetStillOwed(unmet tool.UnmetNeed, selected, optional map[string]bool, optionalKnown bool) bool {
	if selected[strings.TrimSpace(unmet.NeedID)] || semanticUnmetIsBudget(unmet) || semanticUnmetIsInstalledMCP(unmet) {
		return false
	}
	if !tool.UnmetIsAuthoringFault(unmet.ReasonCode) && optionalKnown && optional[strings.TrimSpace(unmet.NeedID)] {
		return false
	}
	return true
}

func hopSelectionRequired(selection tool.PlannedSelection, optional map[string]bool, optionalKnown, recorded bool) bool {
	// The plan records the obligation it accepted. A later re-resolve of the
	// classification does not see needs added after that resolve, and treating
	// the missing id as required is what held a finished turn open.
	// Optional bit, companion offer, or repeat ceiling. A failed optionality
	// recompute must not promote an offer into an obligation: that is how an
	// unused download bundle kept a finished document turn open.
	if tool.SelectionCallIsSkippable(selection) {
		return false
	}
	// Recorded plans do not consult the classification map. That map can
	// only name needs the resolve produced, and must not release a base the
	// planner accepted as required.
	if recorded {
		return true
	}
	needID := strings.TrimSpace(selection.NeedID)
	if optionalKnown && optional[needID] {
		return false
	}
	return true
}

func finalizeHopRun(snapshot *HopRunSnapshot, result agent.LoopResult) *HopRunSnapshot {
	if snapshot == nil {
		return nil
	}
	switch {
	case result.AskUser != nil || result.RecordAudio != nil:
		snapshot.Status = "paused"
	case result.Error == "cancelled":
		snapshot.Status = "cancelled"
	case result.Error != "" || snapshot.Remaining > 0:
		snapshot.Status = "failed"
	default:
		snapshot.Status = "completed"
	}
	return snapshot
}

// hopMissingRequiredEffects lists required effects still owed by this turn, for
// the iteration contract fed back to the model. A file delivery already handed
// to this request's transport is not a deliverable still to produce: telling
// the model it is missing invites a second attempt at a spent one-shot grant.
func hopMissingRequiredEffects(snapshot *HopRunSnapshot) []string {
	if snapshot == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, step := range snapshot.Steps {
		if !step.Required || step.Status == HopStepCompleted || step.Superseded {
			continue
		}
		if hostTransportDeliveryStep(step) {
			continue
		}
		name := strings.TrimSpace(step.Capability)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func ProjectHopRun(snapshot hopjit.EngineSnapshot) *HopRunSnapshot {
	status := "running"
	switch snapshot.TerminalState {
	case hopjit.TerminalCompleted:
		status = "completed"
	case hopjit.TerminalFailed:
		status = "failed"
	case hopjit.TerminalAborted:
		status = "cancelled"
	default:
		if snapshot.Pause != nil || snapshot.AdaptiveNeeded != "" {
			status = "paused"
		}
	}
	projected := &HopRunSnapshot{
		SpecVersion: hopRunSpecVersion,
		RunID:       snapshot.RunID,
		Goal:        snapshot.Spec.Goal,
		RootTaskID:  snapshot.Spec.ID,
		PlanID:      snapshot.Spec.ID,
		Status:      status,
		Steps:       []HopRunStep{},
	}
	for _, step := range snapshot.Spec.AllSteps() {
		state := snapshot.StepStates[step.ID]
		item := HopRunStep{
			SelectionID: step.ID,
			NeedID:      step.ID,
			Capability:  string(step.Type),
			Required:    true,
			Status:      HopStepPending,
		}
		switch state {
		case hopjit.StepRunning:
			item.Status = HopStepRunning
			item.Evidence.State = tool.PlanExecutionRunning
		case hopjit.StepDone:
			item.Status = HopStepCompleted
			item.Evidence.State = tool.PlanExecutionSucceeded
		case hopjit.StepFailed:
			item.Status = HopStepFailed
			item.Evidence.State = tool.PlanExecutionFailed
			item.Evidence.ReasonCode = snapshot.Failures[step.ID].Reason
		case hopjit.StepSkipped:
			item.Status = HopStepOptional
			item.Required = false
		default:
			item.Status = HopStepPending
		}
		if state != hopjit.StepDone && state != hopjit.StepSkipped {
			projected.Remaining++
		}
		projected.Steps = append(projected.Steps, item)
	}
	return projected
}
