package main

import "github.com/RapidAI/CodeClaw/corelib/intent"

// Named skill invocation is the main-assistant path: inject the skill into
// this conversation and let the agent run it. workflow_task is a workflow_v2
// panel start. Those two must not share a routing outcome.

// workflow_task inside a workflow agent loop.
//
// The label means "this reads like a multi-phase project". On an ordinary chat
// turn that is a statement about work the managed surface does not serve, and
// the turn is refused with a pointer to the workflow entry points. On a turn
// that is already a workflow phase, it is not a statement about anything: the
// phase text describes the project the phase belongs to, so the classifier is
// restating the route the turn is on. Refusing there would let a workflow break
// itself from the inside, mid-run, for the crime of still sounding like the
// workflow it is.
//
// The phase keeps whatever else it was classified as. Only the redundant label
// is dropped, so a phase that is also a coding turn is still planned as one; a
// phase that was nothing but workflow_task falls through to the legacy pipeline,
// which is where document_generate phases already go
// (imSemanticIntentIsManagedForLoop).

// semanticClassificationForWorkflowLoop removes the redundant workflow_task
// label from a phase turn's classification. Non-workflow turns and turns
// without the label are returned unchanged.
//
// This runs before coverage is computed rather than at the managed-for-loop
// gate, because an unmapped capability label is refused before that gate is
// ever reached.
func semanticClassificationForWorkflowLoop(workflowAgentLoop bool, result intent.ClassificationResult) intent.ClassificationResult {
	if !workflowAgentLoop || !classificationHasLabel(result, intent.LabelWorkflowTask) {
		return result
	}
	remaining := make([]intent.IntentLabel, 0, len(result.Secondary))
	for _, label := range result.Labels() {
		if label != intent.LabelWorkflowTask {
			remaining = append(remaining, label)
		}
	}
	trimmed := result
	if len(remaining) == 0 {
		// Nothing else was claimed, so the turn carries no capability at all.
		// An empty Primary reads as "no governed family" everywhere downstream,
		// which is the fallthrough this phase wants.
		trimmed.Primary = ""
		trimmed.Secondary = nil
		return trimmed
	}
	trimmed.Primary = remaining[0]
	trimmed.Secondary = remaining[1:]
	return trimmed
}
