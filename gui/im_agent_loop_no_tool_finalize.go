package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const maxConsecutiveNoTool = 5

type agentLoopNoToolExitResult struct {
	Response                 *IMAgentResponse
	PostStreamReturnPrepTime bool
}

type agentLoopEmptyNoToolResult struct {
	Response     *IMAgentResponse
	ContinueLoop bool
}

type agentLoopNoToolRecoverOptions struct {
	Context                *LoopContext
	UserID                 string
	UserText               string
	MessageContent         string
	TrimmedVisibleContent  string
	ToolCalls              []llm.ToolCall
	Phase                  *agentLoopPhase
	Conversation           []interface{}
	History                []agent.ConversationEntry
	Iteration              int
	TotalToolCallsInLoop   int
	RequiresExecution      bool
	RecordSystemMessages   func(int, []interface{})
	AttachLLMTelemetry     func(*IMAgentResponse)
	AttachVisibleArtifacts func(*IMAgentResponse)
}

type agentLoopNoToolRecoverResult struct {
	Conversation []interface{}
	Response     *IMAgentResponse
	ContinueLoop bool
}

type agentLoopNoToolBranchOptions struct {
	Context                  *LoopContext
	UserID                   string
	UserText                 string
	Iteration                int
	Platform                 string
	MessageContent           string
	Choice                   llm.Choice
	Phase                    *agentLoopPhase
	Conversation             []interface{}
	Tools                    []map[string]interface{}
	BaseTools                []map[string]interface{}
	History                  []agent.ConversationEntry
	LengthContinuationBuffer *strings.Builder
	TotalToolCallsInLoop     int
	StreamDone               bool
	RecordSystemMessages     func(int, []interface{})
	AttachLLMTelemetry       func(*IMAgentResponse)
	AttachVisibleArtifacts   func(*IMAgentResponse)
}

type agentLoopNoToolBranchResult struct {
	Conversation             []interface{}
	Tools                    []map[string]interface{}
	MessageContent           string
	TrimmedVisibleContent    string
	Response                 *IMAgentResponse
	ContinueLoop             bool
	ReadyToFinalize          bool
	PostStreamReturnPrepTime bool
}

type agentLoopNoToolFinalizeOptions struct {
	Context                *LoopContext
	UserID                 string
	UserText               string
	Iteration              int
	TotalToolCallsInLoop   int
	MessageContent         string
	LengthContinuationText string
	TrimmedVisibleContent  string
	TruncatedToolCount     int
	Phase                  *agentLoopPhase
	History                []agent.ConversationEntry
	StreamDone             bool
	VoiceData              string
	VoiceFileName          string
	VoiceMimeType          string
	AttachLLMTelemetry     func(*IMAgentResponse)
	AttachVisibleArtifacts func(*IMAgentResponse)
}

type agentLoopNoToolFinalizeResult struct {
	Response                 *IMAgentResponse
	PostStreamReturnPrepTime bool
	ResponseElapsed          time.Duration
}

type agentLoopNoToolPathOptions struct {
	Context                  *LoopContext
	UserID                   string
	UserText                 string
	Iteration                int
	Platform                 string
	MessageContent           string
	Choice                   llm.Choice
	Phase                    *agentLoopPhase
	Conversation             []interface{}
	Tools                    []map[string]interface{}
	BaseTools                []map[string]interface{}
	History                  []agent.ConversationEntry
	LengthContinuationBuffer *strings.Builder
	TotalToolCallsInLoop     int
	StreamDone               bool
	VoiceData                string
	VoiceFileName            string
	VoiceMimeType            string
	RecordSystemMessages     func(int, []interface{})
	AttachLLMTelemetry       func(*IMAgentResponse)
	AttachVisibleArtifacts   func(*IMAgentResponse)
}

type agentLoopNoToolPathResult struct {
	Conversation              []interface{}
	Tools                     []map[string]interface{}
	MessageContent            string
	Response                  *IMAgentResponse
	ContinueLoop              bool
	PostStreamReturnPrepTime  bool
	PostStreamBranchElapsed   time.Duration
	PostStreamResponseElapsed time.Duration
}

func (h *IMMessageHandler) handleAgentLoopNoToolPath(opts agentLoopNoToolPathOptions) agentLoopNoToolPathResult {
	result := agentLoopNoToolPathResult{
		Conversation:   opts.Conversation,
		Tools:          opts.Tools,
		MessageContent: opts.MessageContent,
	}
	var branchStartedAt time.Time
	if opts.StreamDone {
		branchStartedAt = time.Now()
		defer func() {
			result.PostStreamBranchElapsed += time.Since(branchStartedAt)
		}()
	}

	noToolBranch := h.handleAgentLoopNoToolBranch(agentLoopNoToolBranchOptions{
		Context:                  opts.Context,
		UserID:                   opts.UserID,
		UserText:                 opts.UserText,
		Iteration:                opts.Iteration,
		Platform:                 opts.Platform,
		MessageContent:           opts.MessageContent,
		Choice:                   opts.Choice,
		Phase:                    opts.Phase,
		Conversation:             opts.Conversation,
		Tools:                    opts.Tools,
		BaseTools:                opts.BaseTools,
		History:                  opts.History,
		LengthContinuationBuffer: opts.LengthContinuationBuffer,
		TotalToolCallsInLoop:     opts.TotalToolCallsInLoop,
		StreamDone:               opts.StreamDone,
		RecordSystemMessages:     opts.RecordSystemMessages,
		AttachLLMTelemetry:       opts.AttachLLMTelemetry,
		AttachVisibleArtifacts:   opts.AttachVisibleArtifacts,
	})
	result.Conversation = noToolBranch.Conversation
	result.Tools = noToolBranch.Tools
	result.MessageContent = noToolBranch.MessageContent
	if noToolBranch.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
	}
	if noToolBranch.Response != nil {
		result.Response = noToolBranch.Response
		return result
	}
	if noToolBranch.ContinueLoop {
		result.ContinueLoop = true
		return result
	}
	if h.shouldContinueForPendingGuideReference(opts.UserID) {
		result.ContinueLoop = true
		return result
	}
	noToolFinalize := h.finalizeAgentLoopNoToolBranch(agentLoopNoToolFinalizeOptions{
		Context:                opts.Context,
		UserID:                 opts.UserID,
		UserText:               opts.UserText,
		Iteration:              opts.Iteration,
		TotalToolCallsInLoop:   opts.TotalToolCallsInLoop,
		MessageContent:         result.MessageContent,
		LengthContinuationText: opts.LengthContinuationBuffer.String(),
		TrimmedVisibleContent:  noToolBranch.TrimmedVisibleContent,
		TruncatedToolCount:     len(opts.Choice.TruncatedToolNames),
		Phase:                  opts.Phase,
		History:                opts.History,
		StreamDone:             opts.StreamDone,
		VoiceData:              opts.VoiceData,
		VoiceFileName:          opts.VoiceFileName,
		VoiceMimeType:          opts.VoiceMimeType,
		AttachLLMTelemetry:     opts.AttachLLMTelemetry,
		AttachVisibleArtifacts: opts.AttachVisibleArtifacts,
	})
	if noToolFinalize.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
		result.PostStreamResponseElapsed += noToolFinalize.ResponseElapsed
	}
	result.Response = noToolFinalize.Response
	return result
}

func (h *IMMessageHandler) finalizeAgentLoopNoToolBranch(opts agentLoopNoToolFinalizeOptions) agentLoopNoToolFinalizeResult {
	result := agentLoopNoToolFinalizeResult{}
	h.maybeStartAsyncCapabilityGapSearch(opts.Context, opts.Iteration, opts.TrimmedVisibleContent, opts.MessageContent, opts.TruncatedToolCount, opts.UserText, opts.UserID, opts.TotalToolCallsInLoop, opts.Phase)
	phase := agentLoopPhase{}
	if opts.Phase != nil {
		opts.Phase.Stage = agentStageFinalize
		phase = *opts.Phase
	}
	finalText := assembleNoToolFinalText(opts.MessageContent, opts.LengthContinuationText, phase)
	if opts.Context != nil {
		finalText = appendPendingBackgroundTaskFinalHint(finalText, h.pendingBackgroundTaskHint(opts.Context.StartedAt))
	}
	finalResp := &IMAgentResponse{Text: stripThinkingTags(finalText)}
	BrowserDiagCP7_FinalOutput(finalResp.Text, "msgContent")
	if opts.StreamDone {
		result.PostStreamReturnPrepTime = true
		responseStartedAt := time.Now()
		defer func() {
			result.ResponseElapsed += time.Since(responseStartedAt)
		}()
	}
	result.Response = h.finalizeAgentLoopNoToolResponse(agentLoopNoToolFinalResponseOptions{
		UserID:                 opts.UserID,
		UserText:               opts.UserText,
		Iteration:              opts.Iteration,
		TotalToolCallsInLoop:   opts.TotalToolCallsInLoop,
		Phase:                  phase,
		History:                opts.History,
		Response:               finalResp,
		VoiceData:              opts.VoiceData,
		VoiceFileName:          opts.VoiceFileName,
		VoiceMimeType:          opts.VoiceMimeType,
		AttachLLMTelemetry:     opts.AttachLLMTelemetry,
		AttachVisibleArtifacts: opts.AttachVisibleArtifacts,
	})
	return result
}

func (h *IMMessageHandler) handleAgentLoopNoToolBranch(opts agentLoopNoToolBranchOptions) agentLoopNoToolBranchResult {
	result := agentLoopNoToolBranchResult{
		Conversation:   opts.Conversation,
		Tools:          opts.Tools,
		MessageContent: opts.MessageContent,
	}
	phase := opts.Phase
	if phase == nil {
		return result
	}
	phase.Stage = agentStageConverge
	phase.ConsecutiveNoTool++

	hallucinationCorrection := ""
	if h.shouldUseCodingWorkflowImplementationNoToolRecovery(opts.Context, opts.UserID) {
		hallucinationCorrection = buildCodingWorkflowImplementationToolAvailabilityCorrection()
	}
	hallucinationResult := handleAgentLoopToolAvailabilityHallucination(opts.Iteration, result.MessageContent, result.Tools, phase, result.Conversation, hallucinationCorrection)
	result.Conversation = hallucinationResult.Conversation
	if hallucinationResult.ContinueLoop {
		result.ContinueLoop = true
		return result
	}

	truncationRecovery := h.handleAgentLoopTruncatedToolCalls(
		opts.Iteration,
		opts.Choice,
		phase,
		result.Conversation,
		result.Tools,
		h.truncationFallbackToolCatalog(opts.Context, opts.UserID, phase, opts.BaseTools),
		opts.RecordSystemMessages,
	)
	result.Conversation = truncationRecovery.Conversation
	result.Tools = truncationRecovery.Tools
	if truncationRecovery.ContinueLoop {
		result.ContinueLoop = true
		return result
	}

	textContinuation := handleAgentLoopTextContinuation(opts.Iteration, opts.Choice.FinishReason, result.MessageContent, phase, opts.LengthContinuationBuffer, result.Conversation, opts.RecordSystemMessages)
	result.Conversation = textContinuation.Conversation
	if textContinuation.ContinueLoop {
		result.ContinueLoop = true
		return result
	}
	localStoredInfoNeedsFirstLookup := tool.IsLocalStoredInfoQuery(opts.UserText) && opts.TotalToolCallsInLoop == 0
	if intent, ok := classifyAgentNoToolReplyByHeuristic(result.MessageContent); ok && intent == agentNoToolReplyComplete && !localStoredInfoNeedsFirstLookup {
		result.TrimmedVisibleContent = strings.TrimSpace(stripThinkingTags(result.MessageContent))
		result.ReadyToFinalize = true
		return result
	}
	if h.shouldContinueForPendingGuideReference(opts.UserID) {
		result.ContinueLoop = true
		return result
	}

	needsConfirmResult := h.applyAgentLoopNeedsConfirmGate(opts.Context, opts.UserID, opts.Iteration, opts.Platform, result.MessageContent, opts.LengthContinuationBuffer.String(), phase, opts.History, opts.StreamDone, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
	result.MessageContent = needsConfirmResult.MsgContent
	if needsConfirmResult.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
	}
	if needsConfirmResult.Response != nil {
		result.Response = needsConfirmResult.Response
		return result
	}

	result.TrimmedVisibleContent = strings.TrimSpace(stripThinkingTags(result.MessageContent))
	if recovered := h.recoverForPendingBackgroundTaskNoToolReply(opts.Context, opts.MessageContent, phase); recovered {
		result.ContinueLoop = true
		return result
	}

	hardCapResult := h.maybeExitAgentLoopForNoToolHardCap(opts.Context, opts.UserID, result.MessageContent, opts.LengthContinuationBuffer.String(), phase, opts.History, opts.StreamDone, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
	if hardCapResult.PostStreamReturnPrepTime {
		result.PostStreamReturnPrepTime = true
	}
	if hardCapResult.Response != nil {
		result.Response = hardCapResult.Response
		return result
	}

	docNoToolRequiresExecution := workflowDocNoToolRequiresExecution(opts.Context, result.TrimmedVisibleContent)
	noToolRecover := h.handleAgentLoopNoToolRecover(agentLoopNoToolRecoverOptions{
		Context:                opts.Context,
		UserID:                 opts.UserID,
		UserText:               opts.UserText,
		MessageContent:         result.MessageContent,
		TrimmedVisibleContent:  result.TrimmedVisibleContent,
		ToolCalls:              opts.Choice.Message.ToolCalls,
		Phase:                  phase,
		Conversation:           result.Conversation,
		History:                opts.History,
		Iteration:              opts.Iteration,
		TotalToolCallsInLoop:   opts.TotalToolCallsInLoop,
		RequiresExecution:      noToolBranchRequiresExecution(opts.Context, phase) || docNoToolRequiresExecution || (opts.TotalToolCallsInLoop == 0 && userRequestRequiresToolExecution(opts.UserText)),
		RecordSystemMessages:   opts.RecordSystemMessages,
		AttachLLMTelemetry:     opts.AttachLLMTelemetry,
		AttachVisibleArtifacts: opts.AttachVisibleArtifacts,
	})
	result.Conversation = noToolRecover.Conversation
	if noToolRecover.Response != nil {
		result.Response = noToolRecover.Response
		return result
	}
	if noToolRecover.ContinueLoop {
		result.ContinueLoop = true
		return result
	}

	result.ReadyToFinalize = true
	return result
}

func (h *IMMessageHandler) shouldContinueForPendingGuideReference(userID string) bool {
	if !h.hasPendingGuideReferenceInjection(userID) {
		return false
	}
	log.Printf("[inject-guide-reference] user=%s pending guide reference detected before no-tool finalization; continuing loop", userID)
	return true
}

func handleAgentLoopToolAvailabilityHallucination(iteration int, msgContent string, tools []map[string]interface{}, phase *agentLoopPhase, conversation []interface{}, correctionOverride string) agentLoopNoToolRecoverResult {
	result := agentLoopNoToolRecoverResult{Conversation: conversation}
	if phase == nil || phase.ToolHallucinationCorrected {
		return result
	}
	correction := detectToolAvailabilityHallucination(msgContent, tools)
	if correction == "" {
		return result
	}
	if strings.TrimSpace(correctionOverride) != "" {
		correction = strings.TrimSpace(correctionOverride)
	}
	phase.ToolHallucinationCorrected = true
	phase.ConsecutiveNoTool = 0
	log.Printf("[agent-loop] tool availability hallucination detected, injecting correction (iter=%d)", iteration)
	result.Conversation = append(result.Conversation, map[string]interface{}{
		"role":    "user",
		"content": correction,
	})
	result.ContinueLoop = true
	return result
}

func (h *IMMessageHandler) handleAgentLoopNoToolRecover(opts agentLoopNoToolRecoverOptions) agentLoopNoToolRecoverResult {
	result := agentLoopNoToolRecoverResult{Conversation: opts.Conversation}
	phase := opts.Phase
	if phase == nil {
		return result
	}

	emptyVisibleResult := opts.TrimmedVisibleContent == ""
	intent, intentOK := h.classifyAgentNoToolReply(context.Background(), opts.MessageContent)
	codingWorkflowImplementationRecover := h.shouldUseCodingWorkflowImplementationNoToolRecovery(opts.Context, opts.UserID)
	promiseOnlyDeliverable := intentOK && intent == agentNoToolReplyPromise && len(opts.ToolCalls) == 0
	if codingWorkflowImplementationRecover {
		promiseOnlyDeliverable = false
	}
	promiseOnlyDeliverable = suppressPostToolPromiseOnlyDeliverable(promiseOnlyDeliverable, *phase, opts.TotalToolCallsInLoop, opts.Iteration)
	noToolStall := emptyVisibleResult || promiseOnlyDeliverable || (intentOK && intent == agentNoToolReplyStall)
	hasPendingSkillRun := strings.TrimSpace(phase.PreferredSkillRunID) != ""
	preferSkill := phase.ForceSkillPreference && phase.PreferredSkillName != ""
	effectiveNoToolRecoverThreshold := stalledNoToolRecoverThreshold
	if hasPendingSkillRun || phase.SkillFailed || phase.Stage == agentStageRecover {
		// Lower threshold to 1 for quick recovery — EXCEPT when tools were
		// blocked due to truncation. In that case the LLM needs normal iterations
		// to adapt to the new constraint (use bash instead of blocked tool).
		// Lowering to 1 would immediately kill the LLM's first attempt to comply.
		if len(phase.TruncationBlockedTools) == 0 {
			effectiveNoToolRecoverThreshold = 1
		}
	}

	if hasPendingSkillRun {
		enterRecoverPhase(phase, agentRecoverPendingSkillRunNoTool, buildNoToolStallRecoverPrompt(phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
		result.ContinueLoop = true
		return result
	}
	if emptyVisibleResult && phase.SkillFailed {
		enterRecoverPhase(phase, agentRecoverNoToolStall, buildNoToolStallRecoverPrompt(phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
		result.ContinueLoop = true
		return result
	}
	if emptyVisibleResult {
		emptyResult := h.handleAgentLoopEmptyNoToolResponse(opts.Context, opts.UserID, phase, opts.History, opts.AttachLLMTelemetry, opts.AttachVisibleArtifacts)
		result.Response = emptyResult.Response
		result.ContinueLoop = emptyResult.ContinueLoop
		return result
	}

	phase.ConsecutiveEmptyResponses = 0
	if promiseOnlyDeliverable {
		phase.DeliverableRecoverCount++
		if phase.DeliverableRecoverCount >= effectiveNoToolRecoverThreshold {
			enterRecoverPhase(phase, agentRecoverNoToolStall, h.buildNoToolStallRecoverPromptForContext(codingWorkflowImplementationRecover, phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
			result.ContinueLoop = true
			return result
		}
		enterRecoverPhase(phase, agentRecoverDeliverablePending, buildDeliverableRecoverPrompt(phase.PreferredSkillName, preferSkill, phase.PreferredSkillRunID))
		if shouldBypassSkillPreference(opts.ToolCalls) {
			phase.ForceSkillPreference = false
		}
		if h.traceService != nil && opts.Context != nil && opts.Context.RunID != "" {
			h.appendTraceEvent(opts.Context, "delivery.nudged", "warn", "Forced deliverable follow-up", truncateTraceText(opts.MessageContent, 220), "", "")
		}
		result.ContinueLoop = true
		return result
	}

	phase.DeliverableRecoverCount = 0
	if tool.IsLocalStoredInfoQuery(opts.UserText) && len(opts.ToolCalls) == 0 && opts.TotalToolCallsInLoop == 0 && !phase.LocalInfoRecallPrompted {
		systemMessagesStart := len(result.Conversation)
		result.Conversation = append(result.Conversation, map[string]string{
			"role":    "system",
			"content": buildLocalStoredInfoRecallPrompt(),
		})
		if opts.RecordSystemMessages != nil {
			opts.RecordSystemMessages(systemMessagesStart, result.Conversation)
		}
		phase.LocalInfoRecallPrompted = true
		result.ContinueLoop = true
		return result
	}

	noToolPrompt := h.buildNoToolActionPromptForContext(codingWorkflowImplementationRecover, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID)
	if shouldRestrictToSkillSearch(*phase) {
		noToolPrompt = buildRemoteSkillSearchPrompt()
	}
	shouldPromptForAction := opts.RequiresExecution && !(intentOK && intent == agentNoToolReplyComplete) && !phase.NoToolActionPrompted
	if phase.ConsecutiveNoTool == 1 && ((intentOK && intent == agentNoToolReplyStall) || hasPendingSkillRun || (phase.ForceSkillPreference && !phase.SkillAttempted) || shouldPromptForAction) {
		systemMessagesStart := len(result.Conversation)
		result.Conversation = append(result.Conversation, map[string]string{
			"role":    "system",
			"content": noToolPrompt,
		})
		if opts.RecordSystemMessages != nil {
			opts.RecordSystemMessages(systemMessagesStart, result.Conversation)
		}
		if phase.ForceSkillPreference {
			phase.SkillAttempted = true
		}
		if shouldPromptForAction {
			phase.NoToolActionPrompted = true
		}
		result.ContinueLoop = true
		return result
	}
	if opts.RequiresExecution && phase.NoToolActionPrompted && phase.TotalRecoverInjections == 0 && phase.ConsecutiveNoTool >= effectiveNoToolRecoverThreshold {
		enterRecoverPhase(phase, agentRecoverNoToolStall, h.buildNoToolStallRecoverPromptForContext(codingWorkflowImplementationRecover, phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
		result.ContinueLoop = true
		return result
	}
	if noToolStall && (phase.ConsecutiveNoTool >= effectiveNoToolRecoverThreshold || phase.DeliverableRecoverCount >= effectiveNoToolRecoverThreshold || (phase.SkillFailed && phase.ConsecutiveNoTool >= 1)) {
		enterRecoverPhase(phase, agentRecoverNoToolStall, h.buildNoToolStallRecoverPromptForContext(codingWorkflowImplementationRecover, phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
		result.ContinueLoop = true
		return result
	}

	return result
}

func buildLocalStoredInfoRecallPrompt() string {
	return "[Local stored info retrieval required]\n" +
		"The user is asking whether saved/local information is known. Do not answer from memory wording alone. Immediately call memory(action=\"recall\") with focused query terms from the user request. If knowledge_search or knowledge_context_pack is available, call knowledge_search with the same focused query before giving a final answer. Only after tool results may you say whether local memory or the knowledge base has the requested information.\n" +
		"[/Local stored info retrieval required]"
}

func (h *IMMessageHandler) shouldUseCodingWorkflowImplementationNoToolRecovery(ctx *LoopContext, userID string) bool {
	return ctx != nil && ctx.WorkflowAgentLoop && h != nil && h.shouldConstrainCodingWorkflowImplementationMainLoop(userID)
}

func (h *IMMessageHandler) buildNoToolActionPromptForContext(codingWorkflowImplementation bool, preferSkill bool, skillName, runID string) string {
	if codingWorkflowImplementation {
		return buildCodingWorkflowImplementationNoToolActionPrompt()
	}
	return buildNoToolActionPrompt(preferSkill, skillName, runID)
}

func (h *IMMessageHandler) buildNoToolStallRecoverPromptForContext(codingWorkflowImplementation bool, consecutive int, preferSkill bool, skillName, runID string) string {
	if codingWorkflowImplementation {
		return buildCodingWorkflowImplementationNoToolStallRecoverPrompt(consecutive)
	}
	return buildNoToolStallRecoverPrompt(consecutive, preferSkill, skillName, runID)
}

func workflowDocNoToolRequiresExecution(ctx *LoopContext, text string) bool {
	if ctx == nil || !ctx.WorkflowAgentLoop || !ctx.WorkflowDocPhase {
		return false
	}
	if len(ctx.WorkflowWrittenFiles) > 0 {
		return false
	}
	return !workflowDocTextLooksComplete(ctx.WorkflowPhaseID, text)
}

func workflowDocTextLooksComplete(phaseID string, text string) bool {
	cleaned := strings.TrimSpace(stripThinkingTags(text))
	if cleaned == "" || len([]rune(cleaned)) < 200 {
		return false
	}
	lower := strings.ToLower(cleaned)
	docSignals := 0
	for _, marker := range []string{
		"# ", "## ", "### ", "\n# ", "\n## ", "\n### ",
		"\n- ", "\n* ", "\n1. ", "\n2. ", "|",
		"一、", "二、", "三、", "四、",
		"第1页", "第 1 页", "第2页", "第 2 页",
		"逐页", "脚本", "视觉", "版式", "大纲", "目标", "受众",
		"slide", "speaker notes", "visual",
	} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			docSignals++
		}
	}
	processSignals := 0
	for _, marker := range []string{
		"我现在", "我来", "让我", "先创建", "先生成", "再生成", "然后", "接下来",
		"稍后", "正在", "不支持 &&", "powershell 不支持",
		"i will", "i'll", "let me", "first", "then", "now create", "now generate",
	} {
		if strings.Contains(lower, marker) {
			processSignals++
		}
	}
	if processSignals >= 2 && docSignals < 3 {
		return false
	}
	switch strings.TrimSpace(phaseID) {
	case "outline", "content_outline":
		return docSignals >= 2 && (strings.Contains(cleaned, "大纲") || strings.Contains(cleaned, "章节") || strings.Contains(cleaned, "结构") || strings.Contains(lower, "outline"))
	case "slide_scripting":
		return docSignals >= 2 && (strings.Contains(cleaned, "脚本") || strings.Contains(cleaned, "第1页") || strings.Contains(cleaned, "第 1 页") || strings.Contains(lower, "slide"))
	case "synthesis", "conclusion", "action_plan", "risk_assessment", "risk_plan",
		"proposal", "polishing", "opinion", "contingency", "assembly",
		"analysis_plan", "report":
		// Final deliverable phases: less strict — any document with 2+ signals
		// and >= 500 runes is considered complete (these produce comprehensive reports).
		return docSignals >= 2 && len([]rune(cleaned)) >= 500
	}
	// For artifact generation phases (ppt_generation, etc.) with NeedsConfirm=false,
	// this function's result doesn't drive any force-return decision. Return true
	// unconditionally to avoid needing per-phase signal word lists.
	return docSignals >= 2
}

func noToolBranchRequiresExecution(ctx *LoopContext, phase *agentLoopPhase) bool {
	if phase != nil && phase.ForceSkillPreference {
		return true
	}
	if ctx != nil && ctx.WorkflowAgentLoop {
		if ctx.WorkflowDocPhase {
			log.Printf("[noToolBranchRequiresExecution] WorkflowDocPhase=true, returning false")
			return false
		}
		log.Printf("[noToolBranchRequiresExecution] WorkflowAgentLoop=true but WorkflowDocPhase=false, returning true")
		return true
	}
	return false
}

func userRequestRequiresToolExecution(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	chineseActionMarkers := []string{
		"\u751f\u6210", "\u521b\u5efa", "\u5236\u4f5c", "\u53d1\u6211", "\u53d1\u9001", "\u4e0a\u4f20", "\u4e0b\u8f7d", "\u5bfc\u51fa", "\u4fdd\u5b58", "\u5199\u5165", "\u4fee\u6539", "\u4fee\u590d", "\u8fd0\u884c", "\u6267\u884c", "\u68c0\u67e5", "\u67e5\u770b", "\u8bfb\u53d6", "\u67e5\u8be2", "\u67e5\u65e5\u5fd7", "\u8fde\u63a5", "\u90e8\u7f72", "\u5b89\u88c5",
	}
	for _, marker := range chineseActionMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	englishActionMarkers := []string{
		"generate", "create", "make", "send", "upload", "download", "export", "save", "write", "edit", "fix", "run", "execute", "inspect", "check", "read", "query", "connect", "deploy", "install",
	}
	for _, marker := range englishActionMarkers {
		if containsASCIIWord(text, marker) {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) handleAgentLoopEmptyNoToolResponse(
	ctx *LoopContext,
	userID string,
	phase *agentLoopPhase,
	history []agent.ConversationEntry,
	attachLLMTelemetry func(*IMAgentResponse),
	attachPendingVisibleArtifacts func(*IMAgentResponse),
) agentLoopEmptyNoToolResult {
	if phase == nil {
		return agentLoopEmptyNoToolResult{}
	}

	phase.ConsecutiveEmptyResponses++
	taskHint := ""
	if ctx != nil {
		taskHint = h.pendingBackgroundTaskHint(ctx.StartedAt)
	}

	if phase.ConsecutiveEmptyResponses >= maxConsecutiveEmptyResponses ||
		phase.TotalRecoverInjections >= maxTotalRecoverInjections {
		log.Printf("[agent-loop] hard exit: %d consecutive empty responses, %d total recovers; returning best available result",
			phase.ConsecutiveEmptyResponses, phase.TotalRecoverInjections)
		phase.Stage = agentStageFinalize
		var fallbackText string
		if h.shouldUseCodingWorkflowImplementationNoToolRecovery(ctx, userID) {
			fallbackText = buildCodingWorkflowImplementationToolingFailureText("the model returned empty responses instead of delegating to CodingSubAgent", taskHint)
		} else {
			fallbackText = findLastAssistantContent(history)
			if fallbackText == "" {
				fallbackText = "Sorry, the model returned empty responses several times and cannot continue. Please simplify the request or send it again."
			}
			if taskHint != "" {
				fallbackText += "\n\n" + taskHint + "\nYou can ask to check background task progress later."
			}
		}
		finalResp := &IMAgentResponse{Text: fallbackText, HardExit: true}
		attachLLMTelemetry(finalResp)
		attachPendingVisibleArtifacts(finalResp)
		h.saveConversationHistoryTimed(userID, history, finalResp)
		return agentLoopEmptyNoToolResult{Response: finalResp}
	}

	recoverPrompt := buildEmptyResultRecoverPromptWithTasks(taskHint)
	if h.shouldUseCodingWorkflowImplementationNoToolRecovery(ctx, userID) {
		recoverPrompt = buildCodingWorkflowImplementationEmptyResultRecoverPrompt(taskHint)
	}
	enterRecoverPhase(phase, agentRecoverEmptyFinalResponse, recoverPrompt)
	if taskHint != "" {
		log.Printf("[agent-loop] empty-response recover: injected pending background task hint")
	}
	return agentLoopEmptyNoToolResult{ContinueLoop: true}
}

func suppressPostToolPromiseOnlyDeliverable(promiseOnlyDeliverable bool, phase agentLoopPhase, totalToolCallsInLoop int, iteration int) bool {
	if promiseOnlyDeliverable && phase.ConsecutiveNoTool == 1 && totalToolCallsInLoop > 0 {
		log.Printf("[agent-loop] suppressed promiseOnlyDeliverable: post-tool summary detected (ConsecutiveNoTool=1, totalToolCalls=%d, iter=%d)", totalToolCallsInLoop, iteration)
		return false
	}
	return promiseOnlyDeliverable
}

func (h *IMMessageHandler) recoverForPendingBackgroundTaskNoToolReply(ctx *LoopContext, msgContent string, phase *agentLoopPhase) bool {
	if ctx == nil || phase == nil || phase.TotalRecoverInjections >= maxTotalRecoverInjections {
		return false
	}
	taskHint := h.pendingBackgroundTaskHint(ctx.StartedAt)
	if !shouldRecoverForPendingBackgroundTaskNoToolReply(msgContent, taskHint) {
		return false
	}
	log.Printf("[agent-loop] pending background task active before no-tool finalization; forcing status check")
	enterRecoverPhase(phase, agentRecoverBackgroundTaskPending, buildPendingBackgroundTaskRecoverPrompt(taskHint))
	return true
}

func shouldRecoverForPendingBackgroundTaskNoToolReply(msgContent string, taskHint string) bool {
	return strings.TrimSpace(taskHint) != "" && strings.TrimSpace(stripThinkingTags(msgContent)) != ""
}

func (h *IMMessageHandler) maybeExitAgentLoopForNoToolHardCap(
	ctx *LoopContext,
	userID string,
	msgContent string,
	lengthContinuationText string,
	phase *agentLoopPhase,
	history []agent.ConversationEntry,
	streamDone bool,
	attachLLMTelemetry func(*IMAgentResponse),
	attachPendingVisibleArtifacts func(*IMAgentResponse),
) agentLoopNoToolExitResult {
	result := agentLoopNoToolExitResult{}
	trimmedVisibleContent := strings.TrimSpace(stripThinkingTags(msgContent))
	if phase == nil || phase.ConsecutiveNoTool <= maxConsecutiveNoTool || trimmedVisibleContent == "" {
		return result
	}
	log.Printf("[agent-loop] hard cap: %d consecutive no-tool iterations, force-returning response", phase.ConsecutiveNoTool)
	phase.Stage = agentStageFinalize
	var finalText string
	if ctx != nil {
		taskHint := h.pendingBackgroundTaskHint(ctx.StartedAt)
		if h.shouldUseCodingWorkflowImplementationNoToolRecovery(ctx, userID) {
			finalText = buildCodingWorkflowImplementationToolingFailureText("the model repeatedly responded without calling delegate_task(agent=\"coding_workflow\")", taskHint)
		} else {
			finalText = appendPendingBackgroundTaskFinalHint(stripThinkingTags(lengthContinuationText+msgContent), taskHint)
		}
	} else {
		finalText = stripThinkingTags(lengthContinuationText + msgContent)
	}
	finalResp := &IMAgentResponse{Text: finalText}
	result.PostStreamReturnPrepTime = streamDone
	attachLLMTelemetry(finalResp)
	attachPendingVisibleArtifacts(finalResp)
	h.saveConversationHistoryTimed(userID, history, finalResp)
	result.Response = finalResp
	return result
}

func appendPendingBackgroundTaskFinalHint(finalText string, taskHint string) string {
	taskHint = strings.TrimSpace(taskHint)
	if taskHint == "" {
		return finalText
	}
	return strings.TrimSpace(finalText) + "\n\n" + taskHint + "\nBackground task is still active; ask me to check progress later."
}

func assembleNoToolFinalText(msgContent string, lengthContinuationText string, phase agentLoopPhase) string {
	finalText := lengthContinuationText + msgContent
	if lengthContinuationText != "" {
		log.Printf("[agent-loop] assembled %d continuation chunks into final response (totalLen=%d)", phase.LengthContinuations+1, len(finalText))
	}
	if len(phase.TruncationBlockedTools) == 0 {
		return finalText
	}
	blockedNames := make([]string, 0, len(phase.TruncationBlockedTools))
	for tn := range phase.TruncationBlockedTools {
		blockedNames = append(blockedNames, tn)
	}
	finalText += "\n\nTools " + strings.Join(blockedNames, ", ") + " were blocked after repeated argument truncation; switched to alternate paths."
	log.Printf("[agent-loop] finalize with blocked tools: %v", blockedNames)
	return finalText
}

type agentLoopNoToolFinalResponseOptions struct {
	UserID                 string
	UserText               string
	Iteration              int
	TotalToolCallsInLoop   int
	Phase                  agentLoopPhase
	History                []agent.ConversationEntry
	Response               *IMAgentResponse
	VoiceData              string
	VoiceFileName          string
	VoiceMimeType          string
	AttachLLMTelemetry     func(*IMAgentResponse)
	AttachVisibleArtifacts func(*IMAgentResponse)
}

func (h *IMMessageHandler) finalizeAgentLoopNoToolResponse(opts agentLoopNoToolFinalResponseOptions) *IMAgentResponse {
	resp := opts.Response
	if resp == nil {
		resp = &IMAgentResponse{}
	}

	if opts.Phase.FailedSkillName != "" && h.app != nil && h.getSkillRunner() != nil {
		h.getSkillRunner().RecordWorkaround(opts.Phase.FailedSkillName, opts.Phase.FailedSkillError)
		log.Printf("[skill-workaround] skill %q failure classified as workaround; LLM resolved task via alternative tools", opts.Phase.FailedSkillName)
	}

	history := h.injectNudgeMessages(opts.History, opts.Iteration, opts.TotalToolCallsInLoop, opts.Phase, opts.UserText)
	if opts.AttachLLMTelemetry != nil {
		opts.AttachLLMTelemetry(resp)
	}
	if opts.AttachVisibleArtifacts != nil {
		opts.AttachVisibleArtifacts(resp)
	}
	attachVoiceArtifact(resp, opts.VoiceData, opts.VoiceFileName, opts.VoiceMimeType)
	h.saveConversationHistoryTimed(opts.UserID, history, resp)
	h.memory.DismissUnfinishedSlot(opts.UserID, "")
	return resp
}
