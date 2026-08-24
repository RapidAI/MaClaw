package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopToolBranchStartOptions struct {
	Context    *LoopContext
	Iteration  int
	Choice     llm.Choice
	Phase      *agentLoopPhase
	TrialState *trialReflectState
	UserID     string
}

func (h *IMMessageHandler) startAgentLoopToolBranch(opts agentLoopToolBranchStartOptions) int {
	if opts.Phase != nil {
		opts.Phase.Stage = agentStageExecute
		opts.Phase.ConsecutiveNoTool = 0
		resetAgentLoopTruncationRecoveryAfterToolCalls(opts.Phase, opts.Choice)
	}
	logAgentLoopPartialTruncation(opts.Choice)
	if opts.TrialState != nil && opts.TrialState.enabled && h.traceService != nil && opts.Context != nil && opts.Context.RunID != "" {
		h.appendTraceEvent(opts.Context, "trial.started", "info", "Trial iteration started", fmt.Sprintf("iteration=%d tool_calls=%d", opts.Iteration+1, len(opts.Choice.Message.ToolCalls)), "", "")
	}
	return len(opts.Choice.Message.ToolCalls)
}

type agentLoopToolPathOptions struct {
	Context                    *LoopContext
	UserID                     string
	UserText                   string
	TaskAnchor                 *taskIdentityAnchor
	Iteration                  int
	Platform                   string
	Config                     corelib.MaclawLLMConfig
	MessageContent             string
	LengthContinuationText     string
	Choice                     llm.Choice
	Phase                      *agentLoopPhase
	Tools                      []map[string]interface{}
	BaseTools                  []map[string]interface{}
	ClientToolNames            []string
	Conversation               []interface{}
	History                    []agent.ConversationEntry
	VisibleArtifacts           *pendingVisibleArtifacts
	DriftDetector              *DriftDetector
	TrialState                 *trialReflectState
	CodingIterCount            int
	TotalToolCallsInLoop       int
	ConsecutiveWriteFileErrors *int
	InFlightLifecycle          *imInFlightLifecycle
	OnProgress                 tool.ProgressCallback
	OnToken                    llm.TokenCallback
	SendToolProgress           func(string)
	MilestoneTracker           *progress.AgentProgressTracker
	Recorder                   *TrajectoryRecorder
	RecordToolCall             func(string, string, string)
	RecordToolResult           func(string, interface{}, string, string)
	RecordSystemMessages       func(int, []interface{})
	AdaptiveRetry              *AdaptiveRetry
	Debug                      bool
	StreamDone                 bool
	LastCompressionSummary     *string
	AttachLLMTelemetry         func(*IMAgentResponse)
	AttachVisibleArtifacts     func(*IMAgentResponse)
}

type agentLoopToolPathResult struct {
	Conversation             []interface{}
	History                  []agent.ConversationEntry
	Tools                    []map[string]interface{}
	MessageContent           string
	CodingIterCount          int
	TotalToolCallsInLoop     int
	VoiceData                string
	VoiceFileName            string
	VoiceMimeType            string
	ToolExecElapsed          time.Duration
	PostStreamReturnPrepTime bool
	Response                 *IMAgentResponse
	Continue                 bool
}

func (h *IMMessageHandler) handleAgentLoopToolPath(opts agentLoopToolPathOptions) agentLoopToolPathResult {
	requestID, loopID := "", ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	logSlow := func(stage string, startedAt time.Time) {
		if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
			log.Printf("[agent-loop-tool-path] slow stage=%s owner=%q request_id=%q loop=%q iteration=%d elapsed=%s tool_calls=%d",
				stage, opts.UserID, requestID, loopID, opts.Iteration, elapsed.Round(time.Millisecond), len(opts.Choice.Message.ToolCalls))
		}
	}
	stageStartedAt := time.Now()
	totalToolCalls := opts.TotalToolCallsInLoop + h.startAgentLoopToolBranch(agentLoopToolBranchStartOptions{
		Context:    opts.Context,
		Iteration:  opts.Iteration,
		Choice:     opts.Choice,
		Phase:      opts.Phase,
		TrialState: opts.TrialState,
		UserID:     opts.UserID,
	})
	result := agentLoopToolPathResult{
		Conversation:         opts.Conversation,
		History:              opts.History,
		Tools:                opts.Tools,
		MessageContent:       opts.MessageContent,
		CodingIterCount:      opts.CodingIterCount,
		TotalToolCallsInLoop: totalToolCalls,
	}
	replanRevision := int64(0)
	if opts.Context != nil {
		replanRevision = opts.Context.ReplanRevision()
	}

	toolCallResult := h.executeAgentLoopToolCalls(agentLoopToolCallsOptions{
		Context:                    opts.Context,
		UserID:                     opts.UserID,
		UserText:                   opts.UserText,
		TaskAnchor:                 opts.TaskAnchor,
		Iteration:                  opts.Iteration,
		Platform:                   opts.Platform,
		Config:                     opts.Config,
		GateActive:                 false,
		MessageContent:             opts.MessageContent,
		ToolCalls:                  opts.Choice.Message.ToolCalls,
		ExposedTools:               opts.Tools,
		ClientToolNames:            opts.ClientToolNames,
		Phase:                      opts.Phase,
		Conversation:               opts.Conversation,
		History:                    opts.History,
		VisibleArtifacts:           opts.VisibleArtifacts,
		DriftDetector:              opts.DriftDetector,
		ConsecutiveWriteFileErrors: opts.ConsecutiveWriteFileErrors,
		InFlightLifecycle:          opts.InFlightLifecycle,
		OnProgress:                 opts.OnProgress,
		OnToken:                    opts.OnToken,
		Recorder:                   opts.Recorder,
		SendToolProgress:           opts.SendToolProgress,
		MilestoneTracker:           opts.MilestoneTracker,
		RecordToolCall:             opts.RecordToolCall,
		RecordToolResult:           opts.RecordToolResult,
		RecordSystemMessages:       opts.RecordSystemMessages,
		AdaptiveRetry:              opts.AdaptiveRetry,
		Debug:                      opts.Debug,
		StreamDone:                 opts.StreamDone,
	})
	logSlow("execute_tool_calls", stageStartedAt)
	result.Conversation = toolCallResult.Conversation
	result.History = toolCallResult.History
	result.Conversation, result.Tools = h.recoverNativePDFGenerationFailure(
		opts.Context,
		opts.UserID,
		opts.Phase,
		result.Conversation,
		result.Tools,
		opts.BaseTools,
		toolCallResult.ToolExecResults,
		opts.RecordSystemMessages,
	)
	toolCallResult.Conversation = result.Conversation
	result.ToolExecElapsed = toolCallResult.ToolExecElapsed
	result.VoiceData = toolCallResult.PendingArtifacts.VoiceData
	result.VoiceFileName = toolCallResult.PendingArtifacts.VoiceFileName
	result.VoiceMimeType = toolCallResult.PendingArtifacts.VoiceMimeType
	if toolCallResult.Response != nil {
		result.Response = toolCallResult.Response
		return result
	}
	if toolCallResult.Replanned {
		result.Conversation = toolCallResult.Conversation
		result.History = toolCallResult.History
		result.Continue = true
		return result
	}
	if opts.Context != nil && opts.Context.ReplanRequestedSince(replanRevision) {
		result.Conversation = stripTrailingBrokenConversationToolGroup(result.Conversation)
		result.History = stripTrailingBrokenToolGroup(result.History)
		result.Continue = true
		return result
	}

	stageStartedAt = time.Now()
	postToolResult := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		Context:                    opts.Context,
		UserID:                     opts.UserID,
		UserText:                   opts.UserText,
		Iteration:                  opts.Iteration,
		Platform:                   opts.Platform,
		MessageContent:             opts.MessageContent,
		AssistantHadVisibleContent: assistantMessageHasVisibleContent(opts.Choice.Message.Content),
		LengthContinuationText:     opts.LengthContinuationText,
		ToolCalls:                  opts.Choice.Message.ToolCalls,
		ToolResults:                toolCallResult.ToolResults,
		ToolOutcomes:               toolCallResult.ToolOutcomes,
		ToolExecResults:            toolCallResult.ToolExecResults,
		Conversation:               result.Conversation,
		History:                    result.History,
		Phase:                      opts.Phase,
		TrialState:                 opts.TrialState,
		CodingIterCount:            opts.CodingIterCount,
		TotalToolCallsInLoop:       totalToolCalls,
		PendingArtifacts:           toolCallResult.PendingArtifacts,
		VisibleArtifacts:           opts.VisibleArtifacts,
		StreamDone:                 opts.StreamDone,
		LastCompressionSummary:     opts.LastCompressionSummary,
		RecordSystemMessages:       opts.RecordSystemMessages,
		AttachLLMTelemetry:         opts.AttachLLMTelemetry,
		AttachVisibleArtifacts:     opts.AttachVisibleArtifacts,
	})
	logSlow("post_tool_branch", stageStartedAt)
	result.Conversation = postToolResult.Conversation
	result.History = postToolResult.History
	result.MessageContent = postToolResult.MessageContent
	result.CodingIterCount = postToolResult.CodingIterCount
	result.PostStreamReturnPrepTime = postToolResult.PostStreamReturnPrepTime
	result.Response = postToolResult.Response
	return result
}

func (h *IMMessageHandler) recoverNativePDFGenerationFailure(
	ctx *LoopContext,
	userID string,
	phase *agentLoopPhase,
	conversation []interface{},
	tools []map[string]interface{},
	baseTools []map[string]interface{},
	execResults []toolExecutionResult,
	recordSystemMessages func(int, []interface{}),
) ([]interface{}, []map[string]interface{}) {
	if phase == nil || phase.NativePDFFallbackInjected || !hasNativePDFGenerationFailure(execResults) {
		return conversation, tools
	}
	phase.NativePDFFallbackInjected = true
	catalog := h.truncationFallbackToolCatalog(ctx, userID, phase, baseTools)
	if policyOwnerID, applyFilter := h.workflowToolFilterOwnerAndDecision(userID, ctx); applyFilter {
		catalog = h.applyWorkflowToolFilterWithCatalog(policyOwnerID, catalog, h.getTools())
	}
	if ctx != nil {
		catalog = filterToolsForExecutionProfile(catalog, ctx.Runtime.Execution)
	}
	catalog = h.filterToolsForExpertUser(userID, catalog)
	fallbackTools := ensureNativePDFFallbackTools(tools, catalog)
	if ctx != nil && ctx.LansengerGroupPermissions != nil {
		// PDF recovery adds a fresh subset to the current list. Apply the group
		// boundary after that merge too, since a future fallback list may evolve
		// beyond the currently safe craft_tool/bash pair.
		fallbackTools = h.ensureLansengerGroupMemoryRecallTool(userID, fallbackTools)
		if ctx.LansengerGroupPermissions.allowsKnowledge() {
			fallbackTools = h.ensureLansengerGroupKnowledgeSearchTool(userID, fallbackTools)
		}
		fallbackTools = filterToolsForLansengerGroupPermissions(fallbackTools, *ctx.LansengerGroupPermissions)
	}
	fallbackTools = filterComputerUseToolsForLocalFileWork(ctx, "", fallbackTools)
	start := len(conversation)
	conversation = append(conversation, map[string]string{
		"role": "system",
		"content": "[system recovery] The native PDF generator is unavailable for this request. " +
			"Immediately use an actually available fallback tool to complete the one-off conversion: prefer craft_tool; use bash only when it is available and needed. " +
			"Do not create or run manage_schedule, and do not use passthrough_task: they are not execution fallbacks. Do not claim a tool was run unless you emitted that tool call and received its result.",
	})
	if recordSystemMessages != nil {
		recordSystemMessages(start, conversation)
	}
	log.Printf("[agent-loop] native PDF generator failed; exposed safe execution fallbacks (tools=%s)", agentLoopToolNamesForLog(fallbackTools))
	return conversation, fallbackTools
}

func hasNativePDFGenerationFailure(results []toolExecutionResult) bool {
	for _, result := range results {
		if !result.IsFailure() {
			continue
		}
		switch result.ToolKind {
		case agentToolKindGeneratePDF, agentToolKindOffice:
			text := result.Text
			if strings.Contains(text, "未找到可用的中文字体") || strings.Contains(text, "无法生成 PDF") || strings.Contains(text, "PDF 生成失败:") {
				return true
			}
		}
	}
	return false
}

type agentLoopToolCallsOptions struct {
	Context        *LoopContext
	UserID         string
	UserText       string
	TaskAnchor     *taskIdentityAnchor
	Iteration      int
	Platform       string
	Config         corelib.MaclawLLMConfig
	GateActive     bool
	MessageContent string
	ToolCalls      []llm.ToolCall
	// ExposedTools is the exact replacement surface attached to the model
	// request that produced ToolCalls. It is intentionally passed as definitions
	// rather than reconstructed from history, pins, or the registry.
	ExposedTools []map[string]interface{}
	// ClientToolNames records which exposed definitions were dynamically bound
	// to this request. It is never reconstructed from the current LoopContext.
	ClientToolNames            []string
	Phase                      *agentLoopPhase
	Conversation               []interface{}
	History                    []agent.ConversationEntry
	VisibleArtifacts           *pendingVisibleArtifacts
	DriftDetector              *DriftDetector
	ConsecutiveWriteFileErrors *int
	InFlightLifecycle          *imInFlightLifecycle
	OnProgress                 tool.ProgressCallback
	OnToken                    llm.TokenCallback
	SendToolProgress           func(string)
	MilestoneTracker           *progress.AgentProgressTracker
	Recorder                   *TrajectoryRecorder
	RecordToolCall             func(string, string, string)
	RecordToolResult           func(string, interface{}, string, string)
	RecordSystemMessages       func(int, []interface{})
	AdaptiveRetry              *AdaptiveRetry
	Debug                      bool
	StreamDone                 bool
}

type agentLoopToolCallsResult struct {
	Conversation     []interface{}
	History          []agent.ConversationEntry
	ToolResults      []string
	ToolOutcomes     []toolOutcome
	ToolExecResults  []toolExecutionResult
	PendingArtifacts agentLoopPendingToolArtifacts
	ToolExecElapsed  time.Duration
	Response         *IMAgentResponse
	Cancelled        bool
	Replanned        bool
}

func (h *IMMessageHandler) executeAgentLoopToolCalls(opts agentLoopToolCallsOptions) agentLoopToolCallsResult {
	requestID, loopID := "", ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	logSlow := func(stage string, startedAt time.Time, tc llm.ToolCall) {
		if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
			log.Printf("[agent-loop-tool-path] slow stage=%s owner=%q request_id=%q loop=%q iteration=%d tool=%q elapsed=%s",
				stage, opts.UserID, requestID, loopID, opts.Iteration, strings.TrimSpace(tc.Function.Name), elapsed.Round(time.Millisecond))
		}
	}
	result := agentLoopToolCallsResult{
		Conversation:    opts.Conversation,
		History:         opts.History,
		ToolResults:     make([]string, 0, len(opts.ToolCalls)),
		ToolOutcomes:    make([]toolOutcome, 0, len(opts.ToolCalls)),
		ToolExecResults: make([]toolExecutionResult, 0, len(opts.ToolCalls)),
	}
	legacySurface := newLegacyToolSurfaceWithClientTools(opts.ExposedTools, opts.ClientToolNames)
	if loopContextBlocksLegacyToolRouter(opts.Context) {
		// Managed turns admit through semantic grants and their SurfaceEpoch.
		// Do not treat their opaque adapter aliases as legacy registered names.
		legacySurface = legacyToolSurface{}
	}
	for tcIdx, tc := range opts.ToolCalls {
		if opts.Context != nil && opts.Context.IsCancelled() {
			opts.Context.SetLoopState(LoopStateStopped)
			// Close any tool calls that never received a result so trajectory pairs stay complete.
			if opts.Recorder != nil {
				opts.Recorder.CloseUnpairedToolCalls("cancelled")
			}
			result.Response = h.cancelledExitResponse(opts.UserID, result.History, opts.UserText)
			result.Cancelled = true
			return result
		}
		if argSize := len([]byte(tc.Function.Arguments)); argSize > guiMaxToolArgumentsBytes {
			toolName := strings.TrimSpace(tc.Function.Name)
			if toolName == "" {
				toolName = "unknown"
			}
			if opts.Context != nil {
				opts.Context.SetLoopState(LoopStateFailed)
			}
			result.Response = &IMAgentResponse{
				Error: fmt.Sprintf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, argSize, guiMaxToolArgumentsBytes),
			}
			return result
		}

		toolExecStartedAt := time.Now()
		replanRevision := int64(0)
		if opts.Context != nil {
			replanRevision = opts.Context.ReplanRevision()
		}
		execResult := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
			Context:          opts.Context,
			UserID:           opts.UserID,
			ContextTokens:    opts.Config.EffectiveContextTokens(),
			UserText:         opts.UserText,
			TaskAnchor:       opts.TaskAnchor,
			SkipWorkflowGate: h.shouldSkipWorkflowToolExecutionGate(opts.UserID, opts.Context),
			ToolCall:         tc,
			LegacySurface:    legacySurface,
			Iteration:        opts.Iteration,
			Phase:            derefAgentLoopPhase(opts.Phase),
			Debug:            opts.Debug,
			OnProgress:       opts.OnProgress,
			OnToken:          opts.OnToken,
			SendToolProgress: opts.SendToolProgress,
			MilestoneTracker: opts.MilestoneTracker,
			RecordToolCall:   opts.RecordToolCall,
			AdaptiveRetry:    opts.AdaptiveRetry,
		})
		logSlow("tool_exec", toolExecStartedAt, tc)
		if opts.Context != nil && opts.Context.ReplanRequestedSince(replanRevision) {
			result.Conversation = stripTrailingBrokenConversationToolGroup(result.Conversation)
			result.History = stripTrailingBrokenToolGroup(result.History)
			result.Replanned = true
			return result
		}
		rawResult := execResult.Text

		stageStartedAt := time.Now()
		if opts.VisibleArtifacts != nil && opts.VisibleArtifacts.QRCodeURL == "" {
			opts.VisibleArtifacts.QRCodeURL = extractWeixinQRCodeURLFromToolResult(rawResult)
		}

		askUserResult := h.handleAgentLoopAskUserToolResult(opts.UserID, opts.Platform, opts.MessageContent, rawResult, opts.GateActive, tc.ID, result.Conversation, result.History, result.ToolResults, opts.RecordToolResult)
		rawResult = askUserResult.Result
		result.Conversation = askUserResult.Conversation
		result.History = askUserResult.History
		result.ToolResults = askUserResult.ToolResults
		if askUserResult.Response != nil {
			// Interactive pause — remaining tools in this parallel batch never run.
			if opts.Recorder != nil {
				opts.Recorder.CloseUnpairedToolCalls("loop_paused")
			}
			result.Response = askUserResult.Response
			return result
		}

		recordAudioResult := h.handleAgentLoopRecordAudioToolResult(opts.UserID, opts.Platform, opts.MessageContent, rawResult, opts.GateActive, tc.ID, result.Conversation, result.History, result.ToolResults, opts.RecordToolResult)
		rawResult = recordAudioResult.Result
		result.Conversation = recordAudioResult.Conversation
		result.History = recordAudioResult.History
		result.ToolResults = recordAudioResult.ToolResults
		if recordAudioResult.Response != nil {
			// Interactive pause — close siblings that never executed (parity with shared).
			if opts.Recorder != nil {
				opts.Recorder.CloseUnpairedToolCalls("loop_paused")
			}
			result.Response = recordAudioResult.Response
			return result
		}

		if IsSubAgentContext(rawResult) {
			rawResult = ExtractSubAgentContext(rawResult)
		}
		h.pinConditionalToolAfterSuccess(opts.UserID, tc.Function.Name, execResult)
		logSlow("post_exec_pre_observation", stageStartedAt, tc)

		// Track files written during workflow agent loops for phase output capture.
		// When LLM writes documents to disk via write_file instead of outputting text,
		// the file path is recorded so post-loop capture can read the actual content.
		// We extract the resolved absolute path from the tool result (e.g. "已写入 F:\...\file.md（1234 字节）")
		// rather than from args, because toolWriteFile resolves relative paths.
		if opts.Context != nil && opts.Context.WorkflowAgentLoop &&
			execResult.Outcome == toolOutcomeSucceeded &&
			strings.TrimSpace(tc.Function.Name) == "write_file" {
			if writtenPath := extractWrittenPathFromResult(rawResult); writtenPath != "" {
				appendWorkflowWrittenFile(opts.Context, writtenPath)
			}
		}

		// Track files delivered via send_file / send_to_im during workflow agent loops.
		// Covers: LLM creates a file via bash then delivers it. Path from tool args
		// is the definitive "this is the phase output" signal.
		if opts.Context != nil && opts.Context.WorkflowAgentLoop &&
			execResult.Outcome == toolOutcomeSucceeded &&
			isSendFileFamilyTool(tc.Function.Name) {
			if sentPath := extractSendFileResolvedPath(tc, h, opts.Context); sentPath != "" {
				appendWorkflowWrittenFile(opts.Context, sentPath)
			}
		}

		// Capture generate_pdf content into WorkflowDocBuffer during workflow agent loops.
		// When LLM uses generate_pdf to produce the phase document (common in desktop panel
		// where the steering says "use generate_pdf for IM channels"), the content parameter
		// contains the full Markdown document. Without this capture, post-loop doc capture
		// finds only short commentary text in the buffer, and the preview panel has no content.
		if opts.Context != nil && opts.Context.WorkflowAgentLoop &&
			execResult.Outcome == toolOutcomeSucceeded &&
			(strings.TrimSpace(tc.Function.Name) == "generate_pdf" || strings.TrimSpace(tc.Function.Name) == "office") {
			if pdfContent := extractGeneratePDFContent(tc); pdfContent != "" {
				if opts.Context.WorkflowDocBuffer.Len() > 0 {
					opts.Context.WorkflowDocBuffer.WriteString("\n\n")
				}
				opts.Context.WorkflowDocBuffer.WriteString(pdfContent)
			}
		}

		stageStartedAt = time.Now()
		payloadObservation := parseToolPayloadResultForPlatformLang(rawResult, opts.Platform, h.imUILangOrZh())
		traceResult := payloadObservation.TraceResult
		toolContent := payloadObservation.ToolContent
		if opts.StreamDone {
			result.ToolExecElapsed += time.Since(toolExecStartedAt)
		}
		result.ToolResults = append(result.ToolResults, traceResult)
		result.ToolOutcomes = append(result.ToolOutcomes, execResult.Outcome)
		result.ToolExecResults = append(result.ToolExecResults, execResult)
		result.PendingArtifacts.ApplyObservation(payloadObservation)
		h.recordAgentLoopToolUsage(opts.Context, opts.UserText, tc, execResult.Outcome, agentLoopToolUsageFollowUp(tcIdx, opts.ToolCalls, execResult.Outcome))
		logSlow("record_usage", stageStartedAt, tc)

		// Skill operation recording: capture successful tool calls for skill generation.
		// Only record if the current agent loop's ownerID matches the recorder's ownerID
		// (per-tab isolation — avoids mixing operations from different tabs).
		if h.app != nil && h.app.skillRecorder != nil && h.app.skillRecorder.IsRecording() {
			recOwnerID := h.app.skillRecorder.OwnerID()
			currentOwnerID := h.currentRuntimeOrLegacyPolicyOwnerID()
			// recOwnerID=="" → backward compat (old Start without ownerID): record all.
			// currentOwnerID=="" → cannot determine attribution: skip (safe default).
			shouldRecord := recOwnerID == "" || (currentOwnerID != "" && recOwnerID == currentOwnerID)
			if shouldRecord {
				isSuccess := execResult.Outcome == toolOutcomeSucceeded
				toolName := strings.TrimSpace(tc.Function.Name)
				if isRecordableToolForSkill(toolName) {
					var argsMap map[string]interface{}
					if tc.Function.Arguments != "" {
						_ = json.Unmarshal([]byte(tc.Function.Arguments), &argsMap)
					}
					h.app.skillRecorder.Record(toolName, argsMap, truncateString(traceResult, 200), isSuccess)
					// Emit count update to frontend
					h.app.emitEvent("skill-recording-state-changed", map[string]interface{}{
						"recording": true,
						"count":     h.app.skillRecorder.EntryCount(),
						"tabId":     h.app.skillRecorder.TabID(),
					})
				}
			}
		}

		stageStartedAt = time.Now()
		h.recordAgentLoopToolTrace(opts.Context, tc, traceResult, rawResult, execResult)
		logSlow("trace_and_steering", stageStartedAt, tc)

		stageStartedAt = time.Now()
		projectionOwnerID := opts.UserID
		if h != nil {
			projectionOwnerID = h.workflowPolicyOwnerID(opts.UserID, opts.Context)
		}
		proj, err := agent.ProjectToolResultWithContext(tc.Function.Name, projectionOwnerID, toolContent, opts.Config.EffectiveContextTokens())
		truncated := proj.Preview
		if err != nil && truncated == "" {
			truncated = truncateToolResultForToolWithSession(tc.Function.Name, projectionOwnerID, toolContent)
		}
		// OpenHuman-inspired: check tool result for prompt injection attempts.
		// Only check external-source tools (web_fetch, web_search, read_file, bash)
		// to avoid wasting CPU on internal tools that return safe content.
		if isExternalSourceTool(tc.Function.Name) {
			if injectionWarning := h.checkToolResultInjection(tc.Function.Name, truncated); injectionWarning != "" {
				truncated = injectionWarning + truncated
			}
		}
		logSlow("truncate_and_injection_scan", stageStartedAt, tc)

		stageStartedAt = time.Now()
		commitResult := h.commitAgentLoopToolResult(agentLoopToolCommitOptions{
			UserID:                     opts.UserID,
			ToolCall:                   tc,
			TruncatedResult:            truncated,
			Execution:                  execResult,
			Conversation:               result.Conversation,
			History:                    result.History,
			Phase:                      opts.Phase,
			DriftDetector:              opts.DriftDetector,
			ConsecutiveWriteFileErrors: opts.ConsecutiveWriteFileErrors,
			InFlightLifecycle:          opts.InFlightLifecycle,
			Recorder:                   opts.Recorder,
			RecordToolResult:           opts.RecordToolResult,
			RecordSystemMessages:       opts.RecordSystemMessages,
			ParallelGroupIndex:         tcIdx,
			ParallelGroupTotal:         len(opts.ToolCalls),
		})
		logSlow("commit_tool_result", stageStartedAt, tc)
		result.Conversation = commitResult.Conversation
		result.History = commitResult.History
		if commitResult.Response != nil {
			result.Response = commitResult.Response
			return result
		}
	}
	return result
}

func agentLoopToolUsageFollowUp(index int, toolCalls []llm.ToolCall, outcome toolOutcome) toolUsageFollowUp {
	if outcome != toolOutcomeFailed || index < 0 || index >= len(toolCalls) {
		return toolUsageFollowUpContinue
	}
	name := strings.TrimSpace(toolCalls[index].Function.Name)
	for i := index + 1; i < len(toolCalls); i++ {
		if strings.TrimSpace(toolCalls[i].Function.Name) == name {
			return toolUsageFollowUpRetry
		}
	}
	return toolUsageFollowUpContinue
}

func derefAgentLoopPhase(phase *agentLoopPhase) agentLoopPhase {
	if phase == nil {
		return agentLoopPhase{}
	}
	return *phase
}

// extractWrittenPathFromResult extracts the resolved absolute file path from
// a successful write_file tool result string. The result format is one of:
// appendWorkflowWrittenFile appends a file path to the workflow written files
// list with deduplication. This is the single append point for both write_file
// and send_file tracking — avoids repeating the dedup loop.
func appendWorkflowWrittenFile(ctx *LoopContext, path string) {
	if ctx == nil || path == "" {
		return
	}
	for _, existing := range ctx.WorkflowWrittenFiles {
		if existing == path {
			return
		}
	}
	ctx.WorkflowWrittenFiles = append(ctx.WorkflowWrittenFiles, path)
}

//   - "已写入 /path/to/file（1234 字节）"
//   - "已追加到 /path/to/file（当前 1234 字节）"
//   - "已清空 /path/to/file（0 字节）"
//
// This is more reliable than parsing args because toolWriteFile resolves
// relative paths to absolute paths before writing.
func extractWrittenPathFromResult(result string) string {
	// Match Chinese write_file success patterns.
	// "已写入 PATH（N 字节）" or "已追加到 PATH（当前 N 字节）" or "已清空 PATH（N 字节）"
	for _, prefix := range []string{"已写入 ", "已追加到 ", "已清空 "} {
		if !strings.HasPrefix(result, prefix) {
			continue
		}
		rest := result[len(prefix):]
		// Path ends at the LAST "（" (full-width parenthesis) that is followed by
		// digits or "当前". We search from the end because paths may contain "（".
		// The toolWriteFile format always ends with "（N 字节）" or "（当前 N 字节）".
		if idx := strings.LastIndex(rest, "（"); idx > 0 {
			return strings.TrimSpace(rest[:idx])
		}
		// Fallback: last " (" (half-width, unlikely but defensive).
		if idx := strings.LastIndex(rest, " ("); idx > 0 {
			return strings.TrimSpace(rest[:idx])
		}
		// No size suffix found — return the rest as path (defensive).
		return strings.TrimSpace(rest)
	}
	return ""
}

// extractSendFileResolvedPath extracts and resolves the file path from a
// send_file tool call's arguments using the same resolution logic as toolSendFile.
// This enables phase output capture when LLM creates files via bash (heredoc/Python)
// and then delivers them via send_file — the send_file path is the definitive
// delivery signal for the phase document.
func extractSendFileResolvedPath(tc llm.ToolCall, h *IMMessageHandler, ctx *LoopContext) string {
	if tc.Function.Arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return ""
	}
	p, _ := args["path"].(string)
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// Use the same owner-aware resolution as toolSendFile.
	ownerID := ""
	if ctx != nil {
		ownerID = ctx.Runtime.PolicyOwnerID
	}
	if h != nil {
		if resolved, err := h.resolveFileToolPathForOwner(p, ownerID); err == nil {
			return resolved
		}
	}
	// Fallback: if already absolute, use as-is.
	if filepath.IsAbs(p) {
		return p
	}
	// Last resort: resolve against CWD.
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// extractGeneratePDFContent extracts the content parameter from a generate_pdf
// or office(action=generate_pdf) tool call. This allows the workflow doc buffer
// to capture the full document text when LLM uses PDF generation instead of
// streaming text output.
func extractGeneratePDFContent(tc llm.ToolCall) string {
	if tc.Function.Arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return ""
	}
	// For "office" merged tool, check action is generate_pdf
	if tc.Function.Name == "office" {
		action, _ := args["action"].(string)
		if action != "generate_pdf" {
			return ""
		}
	}
	content, _ := args["content"].(string)
	return strings.TrimSpace(content)
}
