package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const hopRunSpecVersion = "hop-run/v1"

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
	// another selection in the same repeat family did. The family need is
	// then settled; the step keeps its own evidence for audit but no longer
	// blocks the run.
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
		name := strings.TrimSpace(step.Capability)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}
