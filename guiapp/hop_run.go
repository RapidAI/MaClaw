package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const hopRunSpecVersion = "hop-run/v1"

// hopCurrentChannelDeliveryCapability is the capability the planner records for
// a delivery into the host's own conversation. That step's receipt is this
// turn's response projection, not a transport worker's later callback.
const hopCurrentChannelDeliveryCapability = "artifact.deliver.current_channel"

// A light lookup admits search and fetch as two ways to get the same fact.
// Search is the required plan step. When every search engine times out, the
// model is told to read the page with fetch. A trusted fetch success is the
// fact the search step owed, so that failed search no longer blocks the turn.
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
	// repeat family succeeded, or a failed web search was recovered by a
	// trusted web fetch in the same turn. The step keeps its own evidence
	// for audit but no longer blocks the run.
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
	optional, optionalKnown := semanticSurfaceOptionalNeeds(surface)
	satisfiedFamilies := satisfiedHopFamilies(surface)
	fetchSettledSearch := informationFetchSucceeded(surface)
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
		required := hopSelectionRequired(selection, optional, optionalKnown)
		step := HopRunStep{
			SelectionID: selection.ID,
			NeedID:      selection.NeedID,
			Capability:  string(selection.FitProof.MatchedCapability),
			Required:    required,
			DependsOn:   append([]string(nil), selection.Requires...),
			Status:      HopStepOptional,
		}
		evidenceKey := hopEvidenceKey(surface, selection.ID)
		cached, cachedOK := surface.hopEvidence[evidenceKey]
		if cachedOK {
			step.Evidence = cached
		} else if surface.executor != nil {
			if record, err := surface.executor.Execution(surface.scope, selection.ID); err == nil {
				step.Evidence = HopStepEvidence{State: record.State, ResultDigest: record.ResultDigest, ReasonCode: record.ReasonCode}
				if record.State == tool.PlanExecutionSucceeded || record.State == tool.PlanExecutionFailed {
					if surface.hopEvidence == nil {
						surface.hopEvidence = map[string]HopStepEvidence{}
					}
					surface.hopEvidence[evidenceKey] = step.Evidence
				}
			}
		}
		if surface.completed[selection.ID] {
			digest := ""
			if step.Evidence.State == tool.PlanExecutionSucceeded {
				digest = step.Evidence.ResultDigest
			}
			step.Evidence = HopStepEvidence{State: tool.PlanExecutionSucceeded, ResultDigest: digest}
		}
		// A required step whose repeat family another selection settled no
		// longer blocks the run: the base attempt may fail while a later
		// sibling succeeds, and the family need is then satisfied. The step
		// keeps its own (failed/unsettled) evidence and is marked superseded.
		superseded := required && step.Evidence.State != tool.PlanExecutionSucceeded && satisfiedFamilies[tool.RepeatFamilyID(selection.NeedID)]
		if !superseded && fetchSettledSearch && required &&
			step.Evidence.State == tool.PlanExecutionFailed &&
			step.Capability == hopInformationSearchWeb {
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
		selectedNeeds[step.NeedID] = true
	}
	for _, unmet := range surface.plan.Unmet {
		required := optionalKnown == false || !optional[unmet.NeedID]
		if !required || selectedNeeds[unmet.NeedID] {
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

// informationFetchSucceeded reports a trusted information.fetch.web success
// in this plan. That success is the recovery a light lookup uses after
// information.search.web fails, so the failed search is no longer owed.
func informationFetchSucceeded(surface *semanticCallSurface) bool {
	if surface == nil {
		return false
	}
	for _, selection := range surface.plan.Selections {
		if string(selection.FitProof.MatchedCapability) != hopInformationFetchWeb {
			continue
		}
		if surface.completed[selection.ID] {
			return true
		}
		evidenceKey := hopEvidenceKey(surface, selection.ID)
		if cached, ok := surface.hopEvidence[evidenceKey]; ok && cached.State == tool.PlanExecutionSucceeded {
			return true
		}
		if surface.executor == nil {
			continue
		}
		record, err := surface.executor.Execution(surface.scope, selection.ID)
		if err != nil || record.State != tool.PlanExecutionSucceeded {
			continue
		}
		return true
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
// succeeded execution record (consulting the plan-scoped evidence cache
// before the store). A base attempt may fail while a later sibling
// succeeds; the family need is then satisfied and no completion gate may
// keep counting it as open.
func satisfiedHopFamilies(surface *semanticCallSurface) map[string]bool {
	satisfied := map[string]bool{}
	if surface == nil {
		return satisfied
	}
	for _, selection := range surface.plan.Selections {
		family := tool.RepeatFamilyID(selection.NeedID)
		if family == "" || satisfied[family] {
			continue
		}
		if surface.completed[selection.ID] {
			satisfied[family] = true
			continue
		}
		evidenceKey := hopEvidenceKey(surface, selection.ID)
		if cached, ok := surface.hopEvidence[evidenceKey]; ok {
			if cached.State == tool.PlanExecutionSucceeded {
				satisfied[family] = true
			}
			continue
		}
		if surface.executor != nil {
			if record, err := surface.executor.Execution(surface.scope, selection.ID); err == nil {
				if record.State == tool.PlanExecutionSucceeded || record.State == tool.PlanExecutionFailed {
					if surface.hopEvidence == nil {
						surface.hopEvidence = map[string]HopStepEvidence{}
					}
					surface.hopEvidence[evidenceKey] = HopStepEvidence{State: record.State, ResultDigest: record.ResultDigest, ReasonCode: record.ReasonCode}
				}
				if record.State == tool.PlanExecutionSucceeded {
					satisfied[family] = true
				}
			}
		}
	}
	return satisfied
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

func hopSelectionRequired(selection tool.PlannedSelection, optional map[string]bool, optionalKnown bool) bool {
	if optionalKnown && optional[selection.NeedID] {
		return false
	}
	needID := strings.TrimSpace(selection.NeedID)
	return needID == "" || tool.RepeatFamilyID(needID) == needID
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
