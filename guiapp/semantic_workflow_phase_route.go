package guiapp

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// Named skill invocation is the main-assistant path: inject the skill into
// this conversation and let the agent run it. workflow_task is a workflow_v2
// panel start. Those two must not share a routing outcome.

// workflow_task inside a workflow agent loop.
//
// On a turn that is already a workflow phase, the label is not a statement
// about anything: the phase text describes the project the phase belongs to,
// so the classifier is restating the route the turn is on. Refusing there
// would let a workflow break itself from the inside.
//
// On an ordinary chat turn the label is a workflow-panel project only when
// the catalog corroborates it: the named template has to clear the catalog's
// own match, or, with no type named, the catalog match has to be decisive.
// The tree's old test — "design decisions, or the same input could come out
// differently" — also matches a video or animation the user wants made in
// this conversation. Ordinary chat never auto-starts a workflow, so refusing
// that label closes the only path left. A corroborated catalog project still
// refuses and points at /workflow.
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
	return classificationWithoutWorkflowTask(result)
}

// semanticReleaseUncataloguedWorkflowTask drops workflow_task when the
// catalog does not corroborate a panel project.
//
// WorkflowType is the primary label's template. It corroborates workflow_task
// only when that label is primary. Production 2026-09-27 set a type on a
// cartoon; the catalog did not support it, and refusing closed the only path
// left. A type that belongs to a surviving office or coding label is not
// evidence for keeping workflow_task.
func semanticReleaseUncataloguedWorkflowTask(userText string, result intent.ClassificationResult) intent.ClassificationResult {
	if !classificationHasLabel(result, intent.LabelWorkflowTask) {
		return result
	}
	text := semanticUserIntentText(userText)
	namedType := ""
	if result.Primary == intent.LabelWorkflowTask {
		namedType = result.WorkflowType
	}
	if v2.CatalogCorroboratesWorkflowProject(text, namedType) {
		return result
	}
	wasPrimary := result.Primary == intent.LabelWorkflowTask
	released := classificationWithoutWorkflowTask(result)
	// WorkflowType belongs to the top candidate. Clear it only when that
	// candidate was the label we dropped. A surviving office/coding primary
	// keeps its own type.
	if wasPrimary || released.Primary == "" {
		released.WorkflowType = ""
	}
	if released.RunnerUp == intent.LabelWorkflowTask {
		released.RunnerUp = ""
		released.RunnerUpScore = 0
	}
	const note = "workflow_task released: utterance is not a catalog workflow project"
	if released.Reason == "" {
		released.Reason = note
	} else if !strings.Contains(released.Reason, note) {
		released.Reason += "; " + note
	}
	log.Printf("[semantic-routing] %s text_len=%d", note, len([]rune(text)))
	return released
}

func classificationWithoutWorkflowTask(result intent.ClassificationResult) intent.ClassificationResult {
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
