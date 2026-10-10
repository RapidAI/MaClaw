package guiapp

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) handleBackgroundIMRoute(msg IMUserMessage, providedLoopCtx *LoopContext, httpClient *http.Client, onProgress tool.ProgressCallback) (*IMAgentResponse, bool) {
	if !msg.IsBackground || h.bgManager == nil || providedLoopCtx != nil {
		return nil, false
	}
	slotKind := parseSlotKind(msg.BackgroundSlotKind)
	maxIter := h.getMaclawAgentMaxIterations()
	if msg.MinIterations > maxIter {
		maxIter = msg.MinIterations
	}

	loopCtx, waitC := h.bgManager.SpawnOrQueue(slotKind, msg.UserID, msg.Text, maxIter)
	if loopCtx == nil && waitC != nil {
		// Wait for the slot to become available, but with a timeout to prevent
		// goroutine leaks when the previous task never completes.
		select {
		case loopCtx = <-waitC:
			// slot acquired
		case <-time.After(2 * time.Minute):
			// Remove the pending task from the queue to prevent an orphan
			// LoopContext from being created when the slot eventually frees.
			h.bgManager.CancelPending(slotKind, waitC)
			return &IMAgentResponse{Error: "Background task slot occupied by a long-running task. Skipping this execution."}, true
		}
	}
	if loopCtx == nil {
		return &IMAgentResponse{Error: "Background task failed to start: unable to acquire execution slot."}, true
	}
	// Background work is a first-class IM ingress, not a legacy escape hatch.
	// In particular, runAgentLoop selects the managed owner from Runtime.SemanticIntent
	// before it consults the shared-loop strangler.  Creating a background context
	// and jumping straight to runAgentLoop used to leave that field empty, so a
	// governed request could silently re-enter the legacy name router whenever the
	// strangler was off.  Reuse the normal inbound envelope setup, then classify
	// below before any prompt/tool surface is built.
	loopCtx = h.prepareIMLoopContext(loopCtx, msg, httpClient, true, false)
	// Same per-message copy as the foreground entry. projectStoredTurnIntent
	// cleans the classification about to be stored; the planner reads the
	// loop field after a session continuation may have put the label back.
	loopCtx.NoWorkflowInterception = msg.NoWorkflowInterception
	if h.traceService != nil && loopCtx.RunID == "" {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, msg.Text, msg.Platform, msg.UserID, h.traceProjectPath())
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "Background task accepted", truncateTraceText(msg.Text, 180), "", "")
	}

	var systemPrompt string
	history := h.memory.Load(msg.UserID)
	turnGeneration := loopCtx.SemanticTurnGeneration()
	classificationCtx, cancelClassification, turnCurrent := loopCtx.SemanticTurnContext(turnGeneration)
	if !turnCurrent {
		cancelClassification()
		h.bgManager.Complete(loopCtx.ID)
		return &IMAgentResponse{Error: "semantic_turn_replaced", ResponseSource: "ingress_replacement"}, true
	}
	executionProfile, semanticIntent := h.classifyIMExecutionProfileAndSemanticContext(
		classificationCtx,
		msg,
		false,
		false,
		recentHistoryTexts(history, 6),
	)
	if err := semanticRoutingRequestErr(classificationCtx); err != nil || !loopCtx.SemanticTurnCurrent(turnGeneration) {
		cancelClassification()
		h.bgManager.Complete(loopCtx.ID)
		return &IMAgentResponse{Error: "semantic_turn_replaced", ResponseSource: "ingress_replacement"}, true
	}
	cancelClassification()
	if rewritten := semanticExplicitKnowledgeWrite(semanticIntent, msg.Text); rewritten != nil {
		semanticIntent = rewritten
		executionProfile = executionProfileFromSemanticIntent(semanticIntent, h.executionContractForRegisteredToolName)
		log.Printf("[semantic-routing] explicit knowledge-write request user=%q", msg.UserID)
	}
	semanticIntent = projectStoredTurnIntent(msg.UserID, msg.Text, msg.NoWorkflowInterception, semanticIntent)
	loopCtx.Runtime.Execution = h.continuationKeepsParentExecution(executionProfile, msg.UserID, msg.Text, semanticIntent)
	markAnswerOnlySocialTurn(loopCtx, loopCtx.Runtime.Execution, msg.Text)
	loopCtx.Runtime.ClassificationMessage = classificationMessage(msg.UserID, msg.Text, history)
	bindLoopSemanticIntent(loopCtx, semanticIntent)
	applyStagedImageUnderstandRuntime(loopCtx, msg.Text, msg.Attachments)
	intentText := semanticUserIntentText(msg.Text)
	promptMessage := agent.CompactQueryForEmbedding(intentText)
	if strings.TrimSpace(promptMessage) == "" {
		promptMessage = intentText
	}
	if h.memoryStore != nil {
		systemPrompt = h.buildSystemPromptWithMemory(promptMessage, len(history) == 0, loopCtx)
	} else if fileCompanionTurnWithoutTools(msg.UserID, msg.Platform) {
		// Same base prompt as buildSystemPrompt, but with this loop so the
		// coding-document contract is omitted. Companion is not a background
		// task today; this keeps a later background turn on the same rule.
		systemPrompt = h.buildSystemPromptWithMemory(promptMessage, false, loopCtx)
	} else {
		systemPrompt = h.buildSystemPrompt()
	}
	if activeSlot := h.memory.ActiveUnfinishedSlot(msg.UserID); activeSlot != nil {
		systemPrompt += buildUnfinishedSlotResumeContextWithLang(activeSlot, msg.Lang)
		systemPrompt += h.buildTraceEvidencePrompt(msg.UserID, activeSlot.LastTask)
	} else {
		systemPrompt += h.buildTraceEvidencePrompt(msg.UserID, promptMessage)
	}
	systemPrompt += workflowDocDeliverySection(msg.UserID, msg.Platform)
	if fileCompanionTurnWithoutTools(msg.UserID, msg.Platform) {
		if bindingPrompt := buildAssistantBindingPrompt(msg.AssistantBinding); bindingPrompt != "" {
			systemPrompt += "\n\n" + bindingPrompt
		}
	}

	result := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, msg.Attachments, onProgress, nil, nil, nil, msg.MinIterations, msg.Platform)
	h.runEvidenceCollection(msg.UserID, promptMessage)

	if result != nil && result.Error != "" {
		loopCtx.SetLoopState(LoopStateFailed)
	} else {
		loopCtx.SetLoopState(LoopStateCompleted)
	}
	h.recordAgentLoopTerminalExperience(loopCtx, result)
	summaryText := ""
	errText := ""
	if result != nil {
		summaryText = firstNonEmptyTraceText(result.Text, result.TraceSummary)
		errText = result.Error
	}
	result = h.finalizeTraceResult(loopCtx, result, summaryText, errText)
	h.bgManager.Complete(loopCtx.ID)
	return result, true
}
