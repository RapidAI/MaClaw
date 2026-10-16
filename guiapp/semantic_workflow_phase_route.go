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
// the catalog's winning template is decisive and clearly ahead of the next
// one. A type the classifier named counts only when that template is the
// winner; another template's score does not confirm it.
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

// semanticReleaseOptedOutWorkflowTask drops workflow_task when the host
// already decided this message must not be intercepted as a workflow.
//
// That decision is NoWorkflowInterception, set by the new-task wizard when
// the draft is「无」. It is authoritative for this handoff message: a
// catalog template matching the text does not reopen the panel route, and
// the label must not reach the unmapped-capability rejection. The chat
// agent is the executor the opt-out left in place. A pure workflow_task
// therefore falls through the same way an uncatalogued one does.
func semanticReleaseOptedOutWorkflowTask(optOut bool, userText string, result intent.ClassificationResult) intent.ClassificationResult {
	if !optOut || !classificationHasLabel(result, intent.LabelWorkflowTask) {
		return result
	}
	return discardWorkflowTask(result, "workflow_task released: host opted out of workflow interception", semanticUserIntentText(userText))
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
	return discardWorkflowTask(result, "workflow_task released: utterance is not a catalog workflow project", text)
}

// semanticReleaseLatexExpertWorkflowTask drops workflow_task when this
// session is the built-in LaTeX paper expert and the label is that document.
//
// The template library opens the expert with the .tex already on disk.
// paper_writing here is that document. So is a type the catalog does not
// confirm. An unlabeled workflow_task yields to a decisive winner the model
// left blank, so a gaokao sentence with no type stays gaokao. Asking for
// edits is not a panel project: a phase chain is not part of the search
// document, so 修改建议 does not make contract review the winner. A named
// type the catalog confirms stays a panel project. The utterance is scored,
// not scanned for filenames. A surviving document_generate label is a PDF,
// so that sole label becomes the file edit.
func semanticReleaseLatexExpertWorkflowTask(userID, userText string, result intent.ClassificationResult) intent.ClassificationResult {
	if expertIDFromUserID(userID) != builtinLatexExpertID {
		return result
	}
	if !classificationHasLabel(result, intent.LabelWorkflowTask) {
		return result
	}
	if latexExpertKeepsPanel(semanticUserIntentText(userText), result.WorkflowType) {
		return result
	}
	released := discardWorkflowTask(result, "workflow_task released: latex paper expert already owns the document", semanticUserIntentText(userText))
	return projectSoleSurvivorAsDocumentEdit(released, "latex paper expert plans the document edit")
}

// latexExpertKeepsPanel reports that this workflow_task is some other project
// the catalog confirms. paper_writing is the document this expert owns.
func latexExpertKeepsPanel(text, named string) bool {
	if v2.WorkflowType(named) == v2.WorkflowPaperWriting {
		return false
	}
	if named != "" {
		return v2.CatalogCorroboratesWorkflowProject(text, named)
	}
	return v2.CatalogDecisiveWinner(text) != ""
}

// projectSoleSurvivorAsDocumentEdit plans a file edit when dropping
// workflow_task left no other capability, or left only a PDF. A surviving
// office, coding, or search label keeps its own plan. Read and shell still
// come from the managed baseline.
func projectSoleSurvivorAsDocumentEdit(released intent.ClassificationResult, note string) intent.ClassificationResult {
	if released.Primary != "" && !soleDocumentGenerate(released) {
		return released
	}
	released.Primary = intent.LabelFileWrite
	released.Secondary = nil
	released.WorkflowType = ""
	released.ToolNames = nil
	if strings.Contains(released.Reason, note) {
		return released
	}
	if released.Reason == "" {
		released.Reason = note
	} else {
		released.Reason += "; " + note
	}
	return released
}

// soleDocumentGenerate reports a classification whose only capability is
// making a PDF. Mixed with another family, that PDF label stays.
func soleDocumentGenerate(result intent.ClassificationResult) bool {
	return result.Primary == intent.LabelDocumentGenerate && len(result.Secondary) == 0
}

// projectStoredTurnIntent applies the releases the planner will apply, to the
// classification stored on the loop. The planner reads that stored value.
// Leaving workflow_task there makes the legacy router keep generate_pdf.
func projectStoredTurnIntent(userID, userText string, noWorkflowInterception bool, result *intent.ClassificationResult) *intent.ClassificationResult {
	if result == nil {
		return nil
	}
	projected := semanticReleaseLatexExpertWorkflowTask(userID, userText, *result)
	projected = semanticReleaseUncataloguedWorkflowTask(userText, projected)
	projected = semanticReleaseOptedOutWorkflowTask(noWorkflowInterception, userText, projected)
	return &projected
}

// discardWorkflowTask removes the workflow_task label and the template that
// belonged to it. A surviving office or coding primary keeps its own type.
// The definition's tool list (generate_pdf) belongs to that label when it
// was primary. Leaving the list on the stored intent is what makes the
// legacy router keep the renderer after the label is gone. A secondary
// workflow_task does not own the primary's tool list, so that list stays.
func discardWorkflowTask(result intent.ClassificationResult, note, loggedText string) intent.ClassificationResult {
	if !classificationHasLabel(result, intent.LabelWorkflowTask) {
		return result
	}
	wasPrimary := result.Primary == intent.LabelWorkflowTask
	released := classificationWithoutWorkflowTask(result)
	if wasPrimary || released.Primary == "" {
		released.WorkflowType = ""
		released.ToolNames = nil
	}
	if released.RunnerUp == intent.LabelWorkflowTask {
		released.RunnerUp = ""
		released.RunnerUpScore = 0
	}
	if released.Reason == "" {
		released.Reason = note
	} else if !strings.Contains(released.Reason, note) {
		released.Reason += "; " + note
	}
	log.Printf("[semantic-routing] %s text_len=%d", note, len([]rune(loggedText)))
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
