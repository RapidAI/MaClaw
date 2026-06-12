package main

// Tool execution: dispatcher that routes tool calls to registered handlers.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	mcputil "github.com/RapidAI/CodeClaw/corelib/mcp"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func (h *IMMessageHandler) shouldSkipWorkflowToolExecutionGate(userID string, ctx *LoopContext) bool {
	if ctx == nil {
		return false
	}
	awaitingReview := false
	phaseBlocked := false
	activeWorkflow := false
	return shouldSkipWorkflowToolExecutionGate(ctx.SkipNeedsConfirmGate, awaitingReview, ctx.WorkflowAgentLoop, phaseBlocked, activeWorkflow)
}

type agentLoopToolExecutionOptions struct {
	Context          *LoopContext
	UserID           string
	UserText         string
	SkipWorkflowGate bool
	ToolCall         llm.ToolCall
	Iteration        int
	Phase            agentLoopPhase
	Debug            bool
	OnProgress       coretool.ProgressCallback
	OnToken          llm.TokenCallback
	SendToolProgress func(string)
	MilestoneTracker *progress.AgentProgressTracker
	RecordToolCall   func(string, string, string)
	AdaptiveRetry    *AdaptiveRetry
}

func (h *IMMessageHandler) executeAgentLoopToolCall(opts agentLoopToolExecutionOptions) (result toolExecutionResult) {
	tc := opts.ToolCall
	toolStartedAt := time.Now()
	requestID := ""
	loopID := ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	defer func() {
		log.Printf("[agent-loop tool] done owner=%q request_id=%q loop=%q iteration=%d tool=%q outcome=%s failure=%s duration=%s result_len=%d",
			opts.UserID, requestID, loopID, opts.Iteration, strings.TrimSpace(tc.Function.Name), result.Outcome.String(), string(result.FailureKind), time.Since(toolStartedAt).Round(time.Millisecond), len([]rune(result.Text)))
	}()
	if rewrittenName, rewrittenArgs, rewritten := rewriteInternalBrowserToolCallJSON(tc.Function.Name, tc.Function.Arguments); rewritten {
		tc.Function.Name = rewrittenName
		tc.Function.Arguments = rewrittenArgs
	}
	if reason := rejectUnstableBrowserToolCallJSON(tc.Function.Name, tc.Function.Arguments); reason != "" {
		return toolExecutionResult{Text: "[system rejected] " + reason, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	policyUserID := h.workflowPolicyOwnerID(opts.UserID, opts.Context)
	skipWorkflowGate := opts.SkipWorkflowGate

	if !skipWorkflowGate && !h.isWorkflowToolAllowedForOwner(policyUserID, tc.Function.Name) {
		text := workflowPolicyToolRejectedText(tc.Function.Name)
		result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
		h.appendToolPolicyTrace(opts.Context, policyUserID, tc.Function.Name, "workflow.tool", text)
		log.Printf("[agent-loop] rejected execution of workflow-blocked tool %q (iter=%d user=%s)", tc.Function.Name, opts.Iteration, opts.UserID)
	}
	if result.Text == "" && !skipWorkflowGate {
		allowed, reason := h.isWorkflowToolCallAllowedForOwner(policyUserID, tc.Function.Name, tc.Function.Arguments)
		if !allowed {
			text := fmt.Sprintf("[system rejected] %s", reason)
			result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
			h.appendToolPolicyTrace(opts.Context, policyUserID, tc.Function.Name, "workflow.call", reason)
			log.Printf("[agent-loop] rejected execution of workflow-blocked tool call %q (iter=%d user=%s reason=%s)", tc.Function.Name, opts.Iteration, opts.UserID, reason)
		}
	}
	if result.Text == "" {
		if opts.MilestoneTracker != nil {
			opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, false)
		}
		if opts.RecordToolCall != nil {
			opts.RecordToolCall(tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
	}
	// Emit user-facing tool progress only after policy gates pass. Otherwise a
	// workflow-blocked browser/tool call can still leak confusing progress text.
	if result.Text == "" && opts.SendToolProgress != nil {
		opts.SendToolProgress(userFacingToolProgressTextWithArgs(tc.Function.Name, tc.Function.Arguments))
	}
	if result.Text == "" && opts.Phase.TruncationBlockedTools[tc.Function.Name] {
		text := fmt.Sprintf("[system rejected] %s is temporarily blocked because its arguments were repeatedly truncated. Use another currently available tool path.", tc.Function.Name)
		result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailureTruncationBlocked}
		log.Printf("[agent-loop] rejected execution of truncation-blocked tool %q (iter=%d)", tc.Function.Name, opts.Iteration)
	}
	if result.Text == "" && coretool.IsCodingSessionTool(strings.TrimSpace(tc.Function.Name)) {
		text := fmt.Sprintf("[system rejected] %s is disabled for the agent. Coding tasks must run through the internal CodingSubAgent, not external coding sessions.", tc.Function.Name)
		result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
		log.Printf("[agent-loop] rejected external coding-session tool %q (iter=%d)", tc.Function.Name, opts.Iteration)
	}
	if result.Text == "" && strings.TrimSpace(tc.Function.Name) == "delegate_task" {
		if delegateResult, handled := h.executeCodingWorkflowDelegateTask(opts); handled {
			result = delegateResult
		}
	}
	if result.Text == "" && strings.TrimSpace(tc.Function.Name) == "set_nickname" && !isExplicitNicknameRequest(opts.UserText) {
		text := "[system rejected] set_nickname is only allowed when the user's current request explicitly asks to set or change your nickname. Do not invent a nickname yourself."
		result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
		log.Printf("[agent-loop] rejected unsolicited set_nickname tool call (iter=%d user=%s)", opts.Iteration, opts.UserID)
	}
	if result.Text == "" {
		if errResult := preCheckAgentLoopInlinePayloadLimit(tc.Function.Name, tc.Function.Arguments, opts.Iteration); errResult != nil {
			result = *errResult
		}
	}
	if result.Text == "" {
		// In agent loop context, intercept missing/invalid parameter errors BEFORE
		// executeToolDetailed. executeToolDetailed would emit an AgentView panel
		// (designed for user-manual tool invocations), which is wrong inside an
		// agent loop; the LLM should receive an error message and self-correct.
		if errResult := h.preCheckToolArgsForAgentLoop(tc.Function.Name, tc.Function.Arguments, opts.Iteration); errResult != nil {
			result = *errResult
		}
	}
	if result.Text == "" {
		result = h.executeToolDetailedWithRuntimeState(policyUserID, loopContextHasExplicitRuntimeOwner(opts.Context), runtimePlatformFromLoopContext(opts.Context), tc.Function.Name, tc.Function.Arguments, opts.UserText, filteredToolProgressCallback(tc.Function.Name, opts.OnProgress, opts.Debug))
	}
	h.recordAdaptiveRetryToolFailure(opts.AdaptiveRetry, tc.Function.Name, result, opts.Iteration)

	if opts.MilestoneTracker != nil {
		opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, true)
	}
	return result
}

func (h *IMMessageHandler) appendToolPolicyTrace(ctx *LoopContext, userID, toolName, layer, reason string) {
	if ctx == nil || h == nil || h.traceService == nil || ctx.RunID == "" {
		return
	}
	policyOwner := strings.TrimSpace(userID)
	if policyOwner == "" && ctx.Runtime.PolicyOwnerID != "" {
		policyOwner = ctx.Runtime.PolicyOwnerID
	}
	summary := fmt.Sprintf("layer=%s tool=%s policy_owner=%s request_id=%s session_key=%s reason=%s",
		strings.TrimSpace(layer), strings.TrimSpace(toolName), strings.TrimSpace(policyOwner), strings.TrimSpace(ctx.Runtime.RequestID), strings.TrimSpace(ctx.Runtime.Conversation.SessionKey), strings.TrimSpace(reason))
	h.appendTraceEvent(ctx, "tool.policy_denied", "warn", "Tool policy denied", summary, "", "")
}

func (h *IMMessageHandler) executeCodingWorkflowDelegateTask(opts agentLoopToolExecutionOptions) (toolExecutionResult, bool) {
	cleaned := coretool.CleanToolArguments(opts.ToolCall.Function.Arguments)
	if strings.TrimSpace(cleaned) == "" {
		return toolExecutionResult{}, false
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return toolExecutionResult{}, false
	}
	return h.executeCodingWorkflowDelegateArgs(args, opts)
}

func (h *IMMessageHandler) executeCodingWorkflowDelegateArgs(args map[string]interface{}, opts agentLoopToolExecutionOptions) (toolExecutionResult, bool) {
	agentName, _ := args["agent"].(string)
	if strings.TrimSpace(strings.ToLower(agentName)) != "coding_workflow" {
		return toolExecutionResult{}, false
	}
	policyUserID := h.workflowPolicyOwnerID(opts.UserID, opts.Context)
	if allowed, reason := h.workflowAllowsSubAgentExecutionForOwner(policyUserID); !allowed {
		return toolExecutionResult{Text: "[system rejected] delegate_task(coding_workflow) is not allowed by the current workflow phase. " + reason + ".", ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}, true
	}
	if policyUserID != "" {
		opts.UserID = policyUserID
	}
	request, _ := args["request"].(string)
	request = strings.TrimSpace(request)
	if request == "" {
		return toolExecutionResult{Text: "[system rejected] delegate_task(coding_workflow) requires a non-empty request.", ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailureMissingParameters}, true
	}
	if h == nil {
		return toolExecutionResult{Text: "[system rejected] CodingSubAgent host is unavailable.", ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailureExecutionPanic}, true
	}
	projectPath := firstCodingDelegateProjectPathArg(args)
	if projectPath == "" {
		projectPath = extractCodingDelegateProjectPath(request)
	}
	if projectPath == "" {
		projectPath = h.workflowStartProjectPathForOwner(policyUserID)
	}
	if projectPath == "" {
		projectPath = h.traceProjectPath()
	}
	if projectPath == "" {
		projectPath = corelib.EffectiveWorkspaceDir()
	}
	if projectPath == "" {
		projectPath = "."
	}
	if allowed, reason := h.allowCodingWorkflowDelegate(request, opts, projectPath); !allowed {
		return toolExecutionResult{Text: "[system rejected] delegate_task(coding_workflow) is only allowed for semantically confirmed coding work. " + reason, ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}, true
	}

	httpClient := (*http.Client)(nil)
	if opts.Context != nil {
		httpClient = opts.Context.HTTPClient
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Duration(corelib.DefaultLLMTimeoutSec) * time.Second}
	}

	log.Printf("[delegate-task] executing coding_workflow via CodingSubAgent user=%s project=%s request=%q", opts.UserID, projectPath, truncateRunes(request, 120))
	task := &TaskItem{
		Index:         0,
		DisplayNumber: 1,
		Title:         "Delegated coding task",
		Description:   request,
		AcceptanceCriteria: []string{
			"Requested files are created or modified in the target project path.",
			"The implementation is checked before reporting completion.",
		},
	}
	runner := runTaskWithSubAgent
	if runner == nil {
		runner = RunTaskWithSubAgent
	}
	codeSessionID := newCodingSubAgentCodeSessionID("delegate-task-coding-workflow", opts.UserID)
	emitCodingSubAgentCodeSessionStart(h.app, codeSessionID)
	defer emitCodingSubAgentCodeSessionEnd(h.app, codeSessionID)
	result := runner(
		h,
		h.getMaclawLLMConfig(),
		httpClient,
		task,
		projectPath,
		request,
		"Directly delegated coding task; user already requested implementation.",
		nil,
		opts.Context,
		opts.OnToken,
		func(text string) {
			if opts.OnProgress != nil {
				opts.OnProgress(text)
			}
		},
	)
	if result == nil {
		return toolExecutionResult{Text: "CodingSubAgent did not return a result.", ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailureExecutionPanic}, true
	}
	emitCodingSubAgentCodeFileEvents(h.app, codeSessionID, projectPath, result.FilesModified, result.FilesCreated)
	text := strings.TrimSpace(result.Summary)
	if text == "" {
		text = strings.TrimSpace(result.Error)
	}
	if text == "" {
		text = fmt.Sprintf("CodingSubAgent finished with status=%s.", result.Status)
	}
	if result.Status == TaskExecPassed {
		return toolExecutionResult{Text: text, ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeSucceeded}, true
	}
	if result.Error != "" && !strings.Contains(text, result.Error) {
		text += "\n\nError: " + result.Error
	}
	return toolExecutionResult{Text: text, ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailureHandlerReported}, true
}

func (h *IMMessageHandler) allowCodingWorkflowDelegate(request string, opts agentLoopToolExecutionOptions, projectPath string) (bool, string) {
	text := strings.TrimSpace(opts.UserText)
	if text == "" {
		text = strings.TrimSpace(request)
	}
	if text == "" {
		return false, "empty request cannot be classified."
	}
	result, ok := h.classifyCodingWorkflowDelegateIntent(text, opts.UserID)
	if !ok {
		return false, "semantic intent classifier is unavailable."
	}
	if !isSemanticCodingWorkflowDelegateResult(result) {
		gateIntent, confidence, _, layer, reason := result.ToGateIntent()
		if strings.TrimSpace(reason) != "" {
			return false, fmt.Sprintf("classified as %s (confidence %.2f, layer %d): %s", gateIntent, confidence, layer, reason)
		}
		return false, fmt.Sprintf("classified as %s (confidence %.2f, layer %d).", gateIntent, confidence, layer)
	}
	gateIntent, _, _, _, _ := result.ToGateIntent()
	interactionContinuation := opts.Context != nil && opts.Context.IsAskUserResponse && gateIntent == "bug_fix"
	if reason := h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
		Text:                        text,
		OwnerID:                     opts.UserID,
		ProjectPath:                 projectPath,
		InteractionContinuation:     interactionContinuation,
		RequireExistingCodeEvidence: gateIntent == "bug_fix",
	}); reason != "" {
		return false, reason + "."
	}
	return true, ""
}

func (h *IMMessageHandler) classifyCodingWorkflowDelegateIntent(text, userID string) (intent.ClassificationResult, bool) {
	if h == nil {
		return intent.ClassificationResult{}, false
	}
	if uic := h.getUnifiedClassifier(); uic != nil {
		return uic.Classify(intent.MessageContext{Text: text, UserID: userID}), true
	}
	if uic := unifiedClassifierPtr.Load(); uic != nil {
		return uic.Classify(intent.MessageContext{Text: text, UserID: userID}), true
	}
	return intent.ClassificationResult{}, false
}

func isSemanticCodingWorkflowDelegateResult(result intent.ClassificationResult) bool {
	if result.Degraded || result.Confidence < 0.6 {
		return false
	}
	gateIntent, _, _, layer, _ := result.ToGateIntent()
	if layer != 3 && layer != 23 {
		return false
	}
	switch gateIntent {
	case "new_project", "bug_fix", "maintenance":
		return true
	default:
		return false
	}
}

var windowsPathInTextPattern = regexp.MustCompile(`(?i)([a-z]:[\\/][^\s\r\n\t"'<>|*?,;\x{ff0c}\x{3002}\x{ff1b}\x{ff1a}\x{3001}\x{ff09}\x{3011}\x{300b}]+)`)

func firstCodingDelegateProjectPathArg(args map[string]interface{}) string {
	for _, key := range []string{"project_path", "projectPath", "path", "working_dir", "workingDir", "cwd"} {
		value := strings.TrimSpace(nonEmptyStringFromAny(args[key]))
		if value != "" {
			return filepath.Clean(value)
		}
	}
	return ""
}

func extractCodingDelegateProjectPath(text string) string {
	match := windowsPathInTextPattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	path := strings.TrimRight(strings.TrimSpace(match[1]), " .,:;)]}")
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func workflowPolicyToolRejectedText(name string) string {
	if isRolePrefixRiskToolName(name) {
		return "[system rejected] requested tool is not allowed by the current workflow tool policy."
	}
	return fmt.Sprintf("[system rejected] %s is not allowed by the current workflow tool policy.", name)
}

func isRolePrefixRiskToolName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "browser" || strings.HasPrefix(name, "browser_")
}

func (h *IMMessageHandler) isWorkflowToolAllowed(userID, name string) bool {
	return h.isWorkflowToolAllowedForOwner(h.workflowPolicyUserID(userID), name)
}

func (h *IMMessageHandler) isWorkflowToolCallAllowed(userID, name, argsJSON string) (bool, string) {
	return h.isWorkflowToolCallAllowedForOwner(h.workflowPolicyUserID(userID), name, argsJSON)
}

func (h *IMMessageHandler) isWorkflowToolAllowedForOwner(policyUserID, name string) bool {
	policyUserID = strings.TrimSpace(policyUserID)
	if policyUserID == "" {
		return true
	}
	if h.app != nil && h.app.workflowEngine != nil && h.app.workflowEngine.IsPhaseExecutionBlocked(policyUserID) {
		return false
	}
	_, policy, apply := h.workflowToolFilterOwnerPolicyAndDecision(policyUserID, nil)
	if !apply {
		return true
	}
	if policy == workflow.ToolFilterNone {
		return false
	}
	if h.shouldConstrainCodingWorkflowImplementationMainLoop(policyUserID) {
		return isCodingWorkflowImplementationMainLoopToolAllowed(name)
	}
	return workflow.IsToolAllowedByPolicy(policy, name)
}

func (h *IMMessageHandler) isWorkflowToolCallAllowedForOwner(policyUserID, name, argsJSON string) (bool, string) {
	policyUserID = strings.TrimSpace(policyUserID)
	if policyUserID == "" {
		return true, ""
	}
	if h.app != nil && h.app.workflowEngine != nil && h.app.workflowEngine.IsPhaseExecutionBlocked(policyUserID) {
		return false, fmt.Sprintf("%s is not allowed while the current workflow phase is blocked", strings.TrimSpace(name))
	}
	_, policy, apply := h.workflowToolFilterOwnerPolicyAndDecision(policyUserID, nil)
	if !apply {
		return true, ""
	}
	if policy == workflow.ToolFilterNone {
		return false, fmt.Sprintf("%s is not allowed while the current workflow phase is blocked", strings.TrimSpace(name))
	}
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return false, fmt.Sprintf("%s arguments are invalid JSON: %v", strings.TrimSpace(name), err)
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	if h.shouldConstrainCodingWorkflowImplementationMainLoop(policyUserID) {
		if reason := validateCodingWorkflowImplementationMainLoopToolCall(name, args); reason != "" {
			return false, reason
		}
	}
	approved := []workflow.OpsApprovedCommand(nil)
	if policy == workflow.ToolFilterOpsControlled && h.app != nil && h.app.workflowEngine != nil {
		approved = h.app.workflowEngine.GetOpsApprovedCommands(policyUserID)
	}
	if err := workflow.ValidateToolCallByPolicyWithApproval(policy, name, args, approved); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func rewriteInternalBrowserToolCall(name string, args map[string]interface{}) (string, map[string]interface{}, bool) {
	name = strings.TrimSpace(name)
	if name == "browser" || !strings.HasPrefix(name, "browser_") {
		return name, args, false
	}
	action := strings.TrimPrefix(name, "browser_")
	if action == "" {
		return name, args, false
	}
	rewritten := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		rewritten[k] = v
	}
	rewritten["action"] = action
	return "browser", rewritten, true
}

func rewriteInternalBrowserToolCallJSON(name, argsJSON string) (string, string, bool) {
	name = strings.TrimSpace(name)
	if name == "browser" || !strings.HasPrefix(name, "browser_") {
		return name, argsJSON, false
	}
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(coretool.CleanToolArguments(argsJSON)), &args); err != nil {
			return name, argsJSON, false
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	rewrittenName, rewrittenArgs, rewritten := rewriteInternalBrowserToolCall(name, args)
	if !rewritten {
		return name, argsJSON, false
	}
	encoded, err := json.Marshal(rewrittenArgs)
	if err != nil {
		return name, argsJSON, false
	}
	return rewrittenName, string(encoded), true
}

func rejectUnstableBrowserToolCallJSON(name, argsJSON string) string {
	name = strings.TrimSpace(name)
	if name != "browser" && !strings.HasPrefix(name, "browser_") {
		return ""
	}
	action := normalizeBrowserToolAction(strings.TrimPrefix(name, "browser_"))
	if name == "browser" {
		var args map[string]interface{}
		if strings.TrimSpace(argsJSON) != "" {
			if err := json.Unmarshal([]byte(coretool.CleanToolArguments(argsJSON)), &args); err != nil {
				return ""
			}
		}
		if raw, ok := args["action"].(string); ok {
			action = normalizeBrowserToolAction(raw)
		}
	}
	if browserActionUnsupportedInMerged(action) {
		return fmt.Sprintf("browser action %s is disabled in the stable browser mechanism; use session_start plus observe/click/type/wait/extract/task_run", action)
	}
	return ""
}

func (h *IMMessageHandler) executeTool(name, argsJSON string, onProgress coretool.ProgressCallback) string {
	return h.executeToolDetailed(name, argsJSON, onProgress).Text
}

func (h *IMMessageHandler) executeToolDetailed(name, argsJSON string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
	return h.executeToolDetailedWithUserText(name, argsJSON, "", onProgress)
}

func (h *IMMessageHandler) executeToolDetailedWithUserText(name, argsJSON, userText string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
	return h.executeToolDetailedWithPolicyUserText("", name, argsJSON, userText, onProgress)
}

func (h *IMMessageHandler) executeToolDetailedWithPolicyUserText(policyUserID, name, argsJSON, userText string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
	return h.executeToolDetailedWithRuntimeState(policyUserID, strings.TrimSpace(policyUserID) != "", "", name, argsJSON, userText, onProgress)
}

func (h *IMMessageHandler) executeToolDetailedWithRuntime(policyUserID, runtimePlatform, name, argsJSON, userText string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
	return h.executeToolDetailedWithRuntimeState(policyUserID, strings.TrimSpace(policyUserID) != "", runtimePlatform, name, argsJSON, userText, onProgress)
}

func (h *IMMessageHandler) executeToolDetailedWithRuntimeState(policyUserID string, hasRuntimeOwner bool, runtimePlatform, name, argsJSON, userText string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
	name = strings.TrimSpace(name)
	kind := classifyAgentToolKind(name)
	if reason := rejectUnstableBrowserToolCallJSON(name, argsJSON); reason != "" {
		return toolExecutionResult{Text: "[system rejected] " + reason, ToolName: name, ToolKind: kind, Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	if isDisabledExternalCodingSessionTool(name) {
		return toolExecutionResult{Text: disabledExternalCodingSessionToolText(name), ToolName: name, ToolKind: kind, Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	defer func() {
		if r := recover(); r != nil {
			result = toolExecutionResult{
				Text:        fmt.Sprintf("Tool execution panicked: %v", r),
				ToolName:    name,
				ToolKind:    kind,
				Outcome:     toolOutcomeFailed,
				FailureKind: toolFailureExecutionPanic,
			}
		}
		if result.ToolName == "" {
			result.ToolName = name
		}
		if result.ToolKind == agentToolKindUnknown {
			result.ToolKind = kind
		}
		result.Metadata = mergeToolResultMetadata(result.Metadata, inferToolResultMetadata(result.ToolKind, result.Text))
	}()

	var args map[string]interface{}
	if argsJSON != "" {
		cleaned := coretool.CleanToolArguments(argsJSON)
		if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
			errMsg := fmt.Sprintf("Argument parse failed: %s", err.Error())
			if hint := classifyToolArgumentError(err, argsJSON).Hint(); hint != "" {
				errMsg += "\n\n" + hint
			}
			return toolExecutionResult{Text: errMsg, Outcome: toolOutcomeFailed, FailureKind: toolFailureArgumentParse}
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	if rewrittenName, rewrittenArgs, rewritten := rewriteInternalBrowserToolCall(name, args); rewritten {
		name = rewrittenName
		args = rewrittenArgs
		kind = classifyAgentToolKind(name)
	}
	policyUserID = strings.TrimSpace(policyUserID)
	if policyUserID != "" {
		if !h.isWorkflowToolAllowedForOwner(policyUserID, name) {
			return toolExecutionResult{Text: workflowPolicyToolRejectedText(name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
		}
		argsForPolicy, reason := h.isWorkflowToolCallAllowedForOwner(policyUserID, name, argsJSON)
		if !argsForPolicy {
			return toolExecutionResult{Text: "[system rejected] " + reason, Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
		}
	}
	acceptsRuntimeOwner := h.registeredToolAcceptsRuntimePolicyOwnerArg(name)
	if acceptsRuntimeOwner {
		log.Printf("[tool-runtime] tool=%q owner=%q explicit_runtime=%v platform=%q", name, policyUserID, hasRuntimeOwner, strings.TrimSpace(runtimePlatform))
	}
	if hasRuntimeOwner && policyUserID == "" && acceptsRuntimeOwner {
		return toolExecutionResult{Text: fmt.Sprintf("%s failed: runtime owner is missing; isolated runtime will not fall back to desktop loop", name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	if hasRuntimeOwner && acceptsRuntimeOwner {
		args[registeredToolPolicyOwnerIDField] = policyUserID
	}
	if platform := strings.TrimSpace(runtimePlatform); platform != "" && h.registeredToolAcceptsRuntimePlatformArg(name) {
		args[registeredToolRuntimePlatformField] = platform
	}
	if name == "set_nickname" && strings.TrimSpace(userText) != "" {
		args["_user_text"] = userText
	}

	log.Printf("[tool-call] name=%q args_len=%d summary=%s", name, len(argsJSON), summarizeToolArgsForLog(name, args))

	h.trackSteeringFileFromArgs(name, args)

	if allowed, reason := h.enforceHubSecurityToolPolicy(name, args); !allowed {
		if reason == "" {
			reason = "blocked by Hub security policy"
		}
		return toolExecutionResult{Text: "[system rejected] " + reason, Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}

	if kind == agentToolKindSearchAndInstallSkill {
		installResult := h.executeSkillSearchInstall(args, onProgress)
		outcome := toolOutcomeFailed
		if installResult.Success {
			outcome = toolOutcomeSucceeded
		}
		return toolExecutionResult{Text: installResult.Text, Outcome: outcome, FailureKind: failureKindForOutcome(outcome)}
	}

	if h.registry != nil {
		if tool, ok := h.registry.Get(name); ok {
			if h.emitRegisteredToolAgentViewIfNeeded(name, args, policyUserID) {
				return toolExecutionResult{Text: "Tool parameters are incomplete. A task panel form has been opened on the right.", Outcome: toolOutcomeUncertain, FailureKind: toolFailureMissingParameters}
			}
			if validationIssues := registeredToolValidateArgIssues(*tool, args); len(validationIssues) > 0 {
				if h.app != nil {
					if view := buildRegisteredToolAgentView(*tool, h.attachRegisteredToolPolicyOwnerForOwner(args, policyUserID), nil); view != nil {
						applyRegisteredToolFieldIssues(view, validationIssues)
						h.app.emitAgentView(view)
					}
				}
				return toolExecutionResult{Text: "Tool parameters need correction. A task panel form has been opened on the right.", Outcome: toolOutcomeUncertain, FailureKind: toolFailureValidation}
			}
			securityCtx := &SecurityCallContext{SessionID: localSessionIDFromToolArgs(args)}
			if h.emitRegisteredToolApprovalAgentViewIfNeeded(name, args, securityCtx, policyUserID) {
				return toolExecutionResult{Text: "Tool execution needs approval. An approval panel has been opened on the right.", Outcome: toolOutcomeUncertain, FailureKind: toolFailureApprovalRequired}
			}
			if h.firewall != nil {
				allowed, reason := h.firewall.Check(name, args, securityCtx)
				if !allowed {
					return toolExecutionResult{Text: reason, Outcome: toolOutcomeFailed, FailureKind: toolFailureFirewallRejected}
				}
			}
			if tool.HandlerProg != nil {
				text := tool.HandlerProg(args, onProgress)
				return registeredToolExecutionResult(text)
			}
			if tool.Handler != nil {
				text := tool.Handler(args)
				return registeredToolExecutionResult(text)
			}
		}
	}

	return toolExecutionResult{Text: fmt.Sprintf("Unknown tool: %s", name), Outcome: toolOutcomeFailed, FailureKind: toolFailureUnknownTool}
}

func runtimePolicyOwnerIDFromToolArgs(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	return strings.TrimSpace(nonEmptyStringFromAny(args[registeredToolPolicyOwnerIDField]))
}

func runtimePolicyOwnerIDFromToolArgsWithPresence(args map[string]interface{}) (string, bool) {
	if args == nil {
		return "", false
	}
	value, ok := args[registeredToolPolicyOwnerIDField]
	return strings.TrimSpace(nonEmptyStringFromAny(value)), ok
}

func consumeRuntimePolicyOwnerIDFromToolArgs(args map[string]interface{}) string {
	ownerID, _ := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	return ownerID
}

func consumeRuntimePlatformFromToolArgs(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	value := strings.TrimSpace(nonEmptyStringFromAny(args[registeredToolRuntimePlatformField]))
	delete(args, registeredToolRuntimePlatformField)
	return value
}

func consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args map[string]interface{}) (string, bool) {
	ownerID, ok := runtimePolicyOwnerIDFromToolArgsWithPresence(args)
	if args != nil {
		delete(args, registeredToolPolicyOwnerIDField)
	}
	return ownerID, ok
}

func (h *IMMessageHandler) consumeRuntimePolicyOwnerIDFromToolArgsOrCurrent(args map[string]interface{}) string {
	ownerID, ok := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if ok {
		return ownerID
	}
	ownerID, _ = h.currentRuntimePolicyOwnerState()
	return ownerID
}

func (h *IMMessageHandler) consumeRuntimePolicyOwnerIDFromToolArgsOrCurrentState(args map[string]interface{}) (string, bool) {
	ownerID, ok := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if ok {
		return ownerID, true
	}
	return h.currentRuntimePolicyOwnerState()
}

func (h *IMMessageHandler) toolArgsOrCurrentRuntimePolicyOwnerState(args map[string]interface{}) (string, bool) {
	ownerID, ok := runtimePolicyOwnerIDFromToolArgsWithPresence(args)
	if ok {
		return ownerID, true
	}
	return h.currentRuntimePolicyOwnerState()
}

func (h *IMMessageHandler) currentRuntimePlatform() string {
	return ""
}

func (h *IMMessageHandler) runtimePlatformForOwnerOrCurrent(ownerID string, explicitRuntime bool) string {
	if loopCtx := h.runtimeLoopContextForOwner(ownerID); loopCtx != nil {
		if platform := runtimePlatformFromLoopContext(loopCtx); platform != "" {
			return platform
		}
	}
	return ""
}

func (h *IMMessageHandler) consumeRuntimePlatformFromToolArgsOrCurrent(args map[string]interface{}) string {
	if platform := consumeRuntimePlatformFromToolArgs(args); platform != "" {
		return platform
	}
	return ""
}

func (h *IMMessageHandler) registeredToolAcceptsRuntimePolicyOwnerArg(name string) bool {
	if h != nil && h.registry != nil {
		if tool, ok := h.registry.Get(strings.TrimSpace(name)); ok && tool != nil {
			return tool.RuntimePolicyOwnerArg
		}
	}
	return toolAcceptsRuntimePolicyOwnerArg(name)
}

func (h *IMMessageHandler) registeredToolAcceptsRuntimePlatformArg(name string) bool {
	if h != nil && h.registry != nil {
		if tool, ok := h.registry.Get(strings.TrimSpace(name)); ok && tool != nil {
			return tool.RuntimePlatformArg
		}
	}
	return toolAcceptsRuntimePlatformArg(name)
}

func toolAcceptsRuntimePolicyOwnerArg(name string) bool {
	switch strings.TrimSpace(name) {
	case "bash",
		"read_file", "write_file", "edit_file", "edit_lines", "list_directory", "send_file",
		"manage_skill", "run_skill", "install_skill_hub", "search_and_install_skill",
		"memory", "compress_context", "delegate_task", "agent_status", "async_wait", "set_max_iterations",
		"group_discussion", "screenshot", "call_mcp_tool",
		"browser", "browser_session_start", "browser_connect", "ssh", "tts":
		return true
	default:
		return false
	}
}

func toolAcceptsRuntimePlatformArg(name string) bool {
	switch strings.TrimSpace(name) {
	case "manage_skill", "install_skill_hub", "search_and_install_skill", "screenshot", "tts":
		return true
	default:
		return false
	}
}

func runtimePlatformFromLoopContext(ctx *LoopContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Platform)
}

func loopContextHasExplicitRuntimeOwner(ctx *LoopContext) bool {
	if ctx == nil {
		return false
	}
	if strings.TrimSpace(ctx.Runtime.RequestID) != "" {
		return true
	}
	return strings.TrimSpace(ctx.Runtime.PolicyOwnerID) != ""
}

func (h *IMMessageHandler) workflowPolicyOwnerID(userID string, ctx *LoopContext) string {
	if ctx != nil {
		if ownerID := strings.TrimSpace(ctx.Runtime.PolicyOwnerID); ownerID != "" {
			return ownerID
		}
	}
	return h.workflowPolicyUserID(userID)
}

func (h *IMMessageHandler) workflowPolicyUserID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID != "" {
		return userID
	}
	// Empty owner means caller did not provide an isolation boundary. Do not
	// guess from lastUserID, currentLoopCtx, or single active desktop workflow:
	// all three can point at another tab under concurrent recent-task agents.
	return ""
}

func registeredToolExecutionResult(text string) toolExecutionResult {
	outcome := inferRegisteredToolOutcome(text)
	return toolExecutionResult{Text: text, Outcome: outcome, FailureKind: failureKindForOutcome(outcome)}
}

func inferRegisteredToolOutcome(text string) toolOutcome {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return toolOutcomeUncertain
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{
		"error:", "[error]", "failed:", "failure:", "argument parse failed:",
		"unknown tool:", "tool execution panicked:", "\u9519\u8bef:", "[\u9519\u8bef]",
	} {
		if strings.HasPrefix(lower, prefix) {
			return toolOutcomeFailed
		}
	}
	if strings.Contains(trimmed, "\u6267\u884c\u5931\u8d25") || strings.Contains(trimmed, "\u5de5\u5177\u6267\u884c\u5f02\u5e38") || strings.Contains(trimmed, "\u53c2\u6570\u89e3\u6790\u5931\u8d25") {
		return toolOutcomeFailed
	}
	return toolOutcomeSucceeded
}

func preCheckAgentLoopInlinePayloadLimit(name, argsJSON string, iteration int) *toolExecutionResult {
	name = strings.TrimSpace(name)
	if name != "write_file" && name != "bash" && name != "ssh" {
		return nil
	}
	var args map[string]interface{}
	cleaned := coretool.CleanToolArguments(argsJSON)
	if strings.TrimSpace(cleaned) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return nil
	}
	var field string
	var value string
	var limit int
	switch name {
	case "write_file":
		field = "content"
		value, _ = args[field].(string)
		limit = maxAgentLoopInlineWriteFileContentRunes
	case "bash":
		field = "command"
		value, _ = args[field].(string)
		limit = maxAgentLoopInlineBashCommandRunes
	case "ssh":
		field = "command"
		value, _ = args[field].(string)
		limit = maxAgentLoopInlineSSHCommandRunes
	}
	valueRunes := len([]rune(value))
	if value == "" || valueRunes <= limit {
		return nil
	}
	text := fmt.Sprintf("Tool %s parameter %s is too large for one agent-loop call (%d runes, limit %d). %s", name, field, valueRunes, limit, agentLoopInlinePayloadLimitInstruction())
	if name == "write_file" {
		text += " Split the content into chunks: first call mode=overwrite, then mode=append for later chunks."
	} else {
		text += " Do not embed generated file bodies or long scripts in shell commands; write/upload a script file first, then execute that file."
	}
	log.Printf("[agent-loop] tool %s inline payload limit exceeded field=%s runes=%d limit=%d (iter=%d)", name, field, valueRunes, limit, iteration)
	return &toolExecutionResult{
		Text:        text,
		ToolName:    name,
		ToolKind:    classifyAgentToolKind(name),
		Outcome:     toolOutcomeFailed,
		FailureKind: toolFailureValidation,
	}
}

// preCheckToolArgsForAgentLoop validates tool arguments in agent loop context.
// When parameters are missing or invalid, it returns an error result for the LLM
// to self-correct, instead of emitting an AgentView panel (which is designed for
// user-manual tool invocations and would break the agent loop flow).
// Returns nil when arguments are valid and execution should proceed normally.
func (h *IMMessageHandler) preCheckToolArgsForAgentLoop(name, argsJSON string, iteration int) *toolExecutionResult {
	if h.registry == nil {
		return nil
	}
	tool, ok := h.registry.Get(strings.TrimSpace(name))
	if !ok || tool == nil {
		return nil
	}
	var args map[string]interface{}
	if cleaned := coretool.CleanToolArguments(argsJSON); cleaned != "" {
		if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
			msg := fmt.Sprintf("Tool %s arguments JSON parse failed: %s. Please correct and retry.", name, err.Error())
			log.Printf("[agent-loop] tool %s argument parse failed (iter=%d): %v", name, iteration, err)
			return &toolExecutionResult{
				Text:        msg,
				ToolName:    name,
				ToolKind:    classifyAgentToolKind(name),
				Outcome:     toolOutcomeFailed,
				FailureKind: toolFailureArgumentParse,
			}
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	if strings.TrimSpace(name) == "call_mcp_tool" {
		var normalizeErr error
		args, normalizeErr = normalizeMCPToolCallArgsForAgentLoop(args)
		if normalizeErr != nil {
			msg := fmt.Sprintf("Tool call_mcp_tool arguments JSON parse failed: %s. Please correct and retry.", normalizeErr.Error())
			log.Printf("[agent-loop] tool call_mcp_tool arguments normalization failed (iter=%d): %v", iteration, normalizeErr)
			return &toolExecutionResult{
				Text:        msg,
				ToolName:    name,
				ToolKind:    classifyAgentToolKind(name),
				Outcome:     toolOutcomeFailed,
				FailureKind: toolFailureArgumentParse,
			}
		}
	}
	// Check missing required parameters.
	if missing := registeredToolMissingRequired(tool, args); len(missing) > 0 {
		errMsg := fmt.Sprintf("Tool %s requires parameter(s): %s. Please provide them and retry.", name, strings.Join(missing, ", "))
		log.Printf("[agent-loop] tool %s missing required params %v, returning error to LLM (iter=%d)", name, missing, iteration)
		return &toolExecutionResult{
			Text:        errMsg,
			ToolName:    name,
			ToolKind:    classifyAgentToolKind(name),
			Outcome:     toolOutcomeFailed,
			FailureKind: toolFailureMissingParameters,
		}
	}
	// Check schema validation (type errors, range violations, etc.).
	if issues := registeredToolValidateArgIssues(*tool, args); len(issues) > 0 {
		msgs := registeredToolValidationMessages(issues)
		errMsg := fmt.Sprintf("Tool %s parameter validation failed: %s. Please correct and retry.", name, strings.Join(msgs, "; "))
		log.Printf("[agent-loop] tool %s validation issues: %v (iter=%d)", name, msgs, iteration)
		return &toolExecutionResult{
			Text:        errMsg,
			ToolName:    name,
			ToolKind:    classifyAgentToolKind(name),
			Outcome:     toolOutcomeFailed,
			FailureKind: toolFailureValidation,
		}
	}
	if strings.TrimSpace(name) == "call_mcp_tool" {
		return h.preCheckMCPToolArgsForAgentLoop(args, iteration)
	}
	return nil
}

func normalizeMCPToolCallArgsForAgentLoop(args map[string]interface{}) (map[string]interface{}, error) {
	raw, ok := args["arguments"]
	if !ok {
		return args, nil
	}
	toolArgs, err := mcpToolArgumentsFromAny(raw)
	if err != nil {
		return nil, err
	}
	normalized := make(map[string]interface{}, len(args))
	for key, value := range args {
		normalized[key] = value
	}
	promoteMCPRoutingFields(normalized, toolArgs)
	normalized["arguments"] = toolArgs
	return normalized, nil
}

func promoteMCPRoutingFields(args map[string]interface{}, toolArgs map[string]interface{}) {
	if args == nil || toolArgs == nil {
		return
	}
	for _, key := range []string{"server_id", "tool_name"} {
		if strings.TrimSpace(nonEmptyStringFromAny(args[key])) == "" {
			if value := strings.TrimSpace(nonEmptyStringFromAny(toolArgs[key])); value != "" {
				args[key] = value
			}
		}
		delete(toolArgs, key)
	}
}

func (h *IMMessageHandler) preCheckMCPToolArgsForAgentLoop(args map[string]interface{}, iteration int) *toolExecutionResult {
	if h == nil {
		return nil
	}
	serverRef := strings.TrimSpace(nonEmptyStringFromAny(args["server_id"]))
	toolName := strings.TrimSpace(nonEmptyStringFromAny(args["tool_name"]))
	if serverRef == "" || toolName == "" {
		return nil
	}
	if isDisabledExternalCodingSessionTool(toolName) {
		return &toolExecutionResult{Text: disabledExternalCodingSessionToolText(toolName), ToolName: "call_mcp_tool", ToolKind: classifyAgentToolKind("call_mcp_tool"), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
	}
	toolArgs, parseErr := mcpToolArgumentsFromAny(args["arguments"])
	if parseErr != nil {
		return &toolExecutionResult{
			Text:        fmt.Sprintf("Tool call_mcp_tool arguments JSON parse failed: %s. Please correct and retry.", parseErr.Error()),
			ToolName:    "call_mcp_tool",
			ToolKind:    classifyAgentToolKind("call_mcp_tool"),
			Outcome:     toolOutcomeFailed,
			FailureKind: toolFailureArgumentParse,
		}
	}
	resolvedID, isLocal, err := h.resolveMCPServerRef(serverRef)
	if err != nil {
		return nil
	}
	inputSchema := h.lookupMCPInputSchema(resolvedID, toolName, isLocal)
	if len(inputSchema) == 0 {
		return nil
	}
	validationErrs := mcputil.ValidateArgs(inputSchema, toolArgs)
	if len(validationErrs) == 0 {
		return nil
	}
	msg := mcpValidationErrorTextForAgentLoop(resolvedID, toolName, validationErrs)
	log.Printf("[agent-loop] MCP tool %s/%s validation issues: %s (iter=%d); returning error to LLM", resolvedID, toolName, summarizeMCPValidationErrors(validationErrs), iteration)
	return &toolExecutionResult{
		Text:        msg,
		ToolName:    "call_mcp_tool",
		ToolKind:    classifyAgentToolKind("call_mcp_tool"),
		Outcome:     toolOutcomeFailed,
		FailureKind: mcpValidationFailureKind(validationErrs),
	}
}

func mcpToolArgumentsFromAny(raw interface{}) (map[string]interface{}, error) {
	switch v := raw.(type) {
	case nil:
		return map[string]interface{}{}, nil
	case map[string]interface{}:
		return v, nil
	case string:
		cleaned := coretool.CleanToolArguments(v)
		if cleaned == "" {
			return map[string]interface{}{}, nil
		}
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
			return nil, err
		}
		if args == nil {
			args = map[string]interface{}{}
		}
		return args, nil
	default:
		return nil, fmt.Errorf("arguments must be an object or JSON object string, got %T", raw)
	}
}

func mcpValidationErrorTextForAgentLoop(resolvedID, toolName string, validationErrs []mcputil.ValidationError) string {
	msgs := make([]string, 0, len(validationErrs))
	missing := make([]string, 0, len(validationErrs))
	for _, ve := range validationErrs {
		if strings.TrimSpace(ve.Message) != "" {
			msgs = append(msgs, ve.Message)
		}
		if normalizeAgentViewValidationCodeKind(ve.Code).IsMissingRequired() && strings.TrimSpace(ve.Param) != "" {
			missing = append(missing, strings.TrimSpace(ve.Param))
		}
	}
	if len(msgs) == 0 {
		msgs = append(msgs, "MCP tool arguments failed schema validation")
	}
	stdErr := mcputil.NewStandardError(resolvedID, toolName, mcputil.ErrValidation, strings.Join(msgs, "; "))
	text := mcputil.FormatForLLM(nil, stdErr)
	if len(missing) > 0 {
		text += "\n\nMissing required MCP argument(s): " + strings.Join(missing, ", ") + ". Infer them from context, call a discovery/search tool first, or ask the user a focused question if they cannot be inferred. Keep this recoverable error inside the agent loop."
	}
	return text
}

func mcpValidationFailureKind(validationErrs []mcputil.ValidationError) toolFailureKind {
	for _, ve := range validationErrs {
		if normalizeAgentViewValidationCodeKind(ve.Code).IsMissingRequired() {
			return toolFailureMissingParameters
		}
	}
	return toolFailureValidation
}

func summarizeMCPValidationErrors(validationErrs []mcputil.ValidationError) string {
	if len(validationErrs) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(validationErrs))
	for _, ve := range validationErrs {
		code := strings.TrimSpace(ve.Code)
		if code == "" {
			code = "validation_error"
		}
		param := strings.TrimSpace(ve.Param)
		if param == "" {
			param = "<unknown>"
		}
		parts = append(parts, code+":"+param)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func (h *IMMessageHandler) recordAdaptiveRetryToolFailure(adaptiveRetry *AdaptiveRetry, toolName string, execResult toolExecutionResult, attempt int) {
	if adaptiveRetry == nil || !execResult.IsFailure() {
		return
	}
	category := adaptiveRetryCategoryForToolFailure(execResult)
	decision := adaptiveRetry.Decide(toolName, category, attempt)
	context := adaptiveRetryToolFailureContext(execResult)
	if decision.ErrorContext == "" {
		decision.ErrorContext = context
	} else if context != "" {
		decision.ErrorContext += "; " + context
	}
	adaptiveRetry.RecordFailure(toolName, category, decision)
}

func adaptiveRetryCategoryForToolFailure(execResult toolExecutionResult) FailureCategory {
	switch execResult.FailureKind {
	case toolFailureArgumentParse, toolFailureMissingParameters, toolFailureValidation:
		return FailureArgs
	case toolFailureApprovalRequired, toolFailureFirewallRejected, toolFailurePolicyRejected:
		return FailurePermission
	case toolFailureUnknownTool, toolFailureTruncationBlocked:
		return FailureLogic
	}
	return classifyAdaptiveRetryFailure(fmt.Errorf("%s", execResult.Text))
}

func adaptiveRetryToolFailureContext(execResult toolExecutionResult) string {
	if execResult.FailureKind != toolFailureNone {
		return fmt.Sprintf("tool failure kind=%s outcome=%s: %s", execResult.FailureKind, execResult.Outcome.String(), truncateRunes(execResult.Text, 240))
	}
	return fmt.Sprintf("tool outcome=%s: %s", execResult.Outcome.String(), truncateRunes(execResult.Text, 240))
}

func summarizeToolArgsForLog(toolName string, args map[string]interface{}) string {
	if len(args) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	summary := map[string]interface{}{"keys": keys}
	switch strings.TrimSpace(toolName) {
	case "bash":
		command := strings.TrimSpace(nonEmptyStringFromAny(args["command"]))
		if command != "" {
			summary["command_len"] = len(command)
			if _, rejected := coretool.RejectRawSSHCommand(command); rejected {
				summary["command_class"] = "raw_ssh_rejected"
			} else {
				summary["command_class"] = localCommandClassForLog(command)
			}
		}
	case "call_mcp_tool":
		summary["server_id"] = nonEmptyStringFromAny(args["server_id"])
		summary["tool_name"] = nonEmptyStringFromAny(args["tool_name"])
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return "<unmarshalable>"
	}
	return string(data)
}

func localCommandClassForLog(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "empty"
	}
	first := strings.ToLower(strings.TrimSuffix(filepath.Base(fields[0]), ".exe"))
	switch first {
	case "ssh", "scp", "sftp":
		return "raw_ssh"
	case "bash", "sh", "zsh", "powershell", "pwsh", "cmd":
		return "shell"
	default:
		return "local"
	}
}
