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
	if engine := h.getWorkflowEngine(); engine != nil {
		awaitingReview = engine.IsAwaitingReview(userID)
		phaseBlocked = engine.IsPhaseExecutionBlocked(userID)
	}
	return shouldSkipWorkflowToolExecutionGate(ctx.SkipNeedsConfirmGate, awaitingReview, ctx.WorkflowAgentLoop, phaseBlocked)
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

func (h *IMMessageHandler) executeAgentLoopToolCall(opts agentLoopToolExecutionOptions) toolExecutionResult {
	tc := opts.ToolCall

	var result toolExecutionResult
	if !opts.SkipWorkflowGate && !h.isWorkflowToolAllowed(opts.UserID, tc.Function.Name) {
		text := workflowPolicyToolRejectedText(tc.Function.Name)
		result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
		log.Printf("[agent-loop] rejected execution of workflow-blocked tool %q (iter=%d user=%s)", tc.Function.Name, opts.Iteration, opts.UserID)
	}
	if result.Text == "" && !opts.SkipWorkflowGate {
		allowed, reason := h.isWorkflowToolCallAllowed(opts.UserID, tc.Function.Name, tc.Function.Arguments)
		if !allowed {
			text := fmt.Sprintf("[system rejected] %s", reason)
			result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}
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
		result = h.executeToolDetailedWithUserText(tc.Function.Name, tc.Function.Arguments, opts.UserText, filteredToolProgressCallback(tc.Function.Name, opts.OnProgress, opts.Debug))
	}
	h.recordAdaptiveRetryToolFailure(opts.AdaptiveRetry, tc.Function.Name, result, opts.Iteration)

	if opts.MilestoneTracker != nil {
		opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, true)
	}
	return result
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
	request, _ := args["request"].(string)
	request = strings.TrimSpace(request)
	if request == "" {
		return toolExecutionResult{Text: "[system rejected] delegate_task(coding_workflow) requires a non-empty request.", ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailureMissingParameters}, true
	}
	if h == nil {
		return toolExecutionResult{Text: "[system rejected] CodingSubAgent host is unavailable.", ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailureExecutionPanic}, true
	}
	if allowed, reason := h.allowCodingWorkflowDelegate(request, opts); !allowed {
		return toolExecutionResult{Text: "[system rejected] delegate_task(coding_workflow) is only allowed for semantically confirmed coding work. " + reason, ToolName: "delegate_task", ToolKind: classifyAgentToolKind("delegate_task"), Outcome: toolOutcomeFailed, FailureKind: toolFailurePolicyRejected}, true
	}
	projectPath := firstCodingDelegateProjectPathArg(args)
	if projectPath == "" {
		projectPath = extractCodingDelegateProjectPath(request)
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

	httpClient := (*http.Client)(nil)
	if opts.Context != nil {
		httpClient = opts.Context.HTTPClient
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Duration(corelib.DefaultLLMTimeoutSec) * time.Second}
	}

	log.Printf("[delegate-task] executing coding_workflow via CodingSubAgent user=%s project=%s request=%q", opts.UserID, projectPath, truncateRunes(request, 120))
	task := &TaskItem{
		Index:       1,
		Title:       "Delegated coding task",
		Description: request,
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

func (h *IMMessageHandler) allowCodingWorkflowDelegate(request string, opts agentLoopToolExecutionOptions) (bool, string) {
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
		return false, fmt.Sprintf("classified as %s (confidence %.2f, layer %d): %s", result.Intent, result.Confidence, result.Layer, result.Reason)
	}
	return true, ""
}

func (h *IMMessageHandler) classifyCodingWorkflowDelegateIntent(text, userID string) (GateIntentResult, bool) {
	if h == nil {
		return GateIntentResult{}, false
	}
	if uic := h.getUnifiedClassifier(); uic != nil {
		return classifyUnifiedGateIntent(uic, text, userID)
	}
	if uic := unifiedClassifierPtr.Load(); uic != nil {
		return classifyUnifiedGateIntent(uic, text, userID)
	}
	return GateIntentResult{}, false
}

func isSemanticCodingWorkflowDelegateResult(result GateIntentResult) bool {
	if !isTrustedSemanticGateResult(result) {
		return false
	}
	switch result.Intent {
	case GateIntentNewProject, GateIntentBugFix, GateIntentMaintenance:
		return true
	default:
		return false
	}
}

var windowsPathInTextPattern = regexp.MustCompile(`(?i)([a-z]:\\[^\s\r\n\t"'<>|*?,;\x{ff0c}\x{3002}\x{ff1b}\x{ff1a}\x{3001}\x{ff09}\x{3011}\x{300b}]+)`)

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
	engine := h.getWorkflowEngine()
	if engine == nil || strings.TrimSpace(userID) == "" {
		return true
	}
	if engine.IsPhaseExecutionBlocked(userID) {
		return false
	}
	return workflow.IsToolAllowedByPolicy(engine.GetActivePhaseToolFilter(userID), name)
}

func (h *IMMessageHandler) isWorkflowToolCallAllowed(userID, name, argsJSON string) (bool, string) {
	engine := h.getWorkflowEngine()
	if engine == nil || strings.TrimSpace(userID) == "" {
		return true, ""
	}
	if engine.IsPhaseExecutionBlocked(userID) {
		return false, "current workflow phase is waiting for required input or review; tool execution is paused"
	}
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		cleaned := coretool.CleanToolArguments(argsJSON)
		if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
			return false, fmt.Sprintf("invalid tool arguments: %v", err)
		}
	}
	if err := workflow.ValidateToolCallByPolicyWithApproval(engine.GetActivePhaseToolFilter(userID), strings.TrimSpace(name), args, engine.GetOpsApprovedCommands(userID)); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (h *IMMessageHandler) executeTool(name, argsJSON string, onProgress coretool.ProgressCallback) string {
	return h.executeToolDetailed(name, argsJSON, onProgress).Text
}

func (h *IMMessageHandler) executeToolDetailed(name, argsJSON string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
	return h.executeToolDetailedWithUserText(name, argsJSON, "", onProgress)
}

func (h *IMMessageHandler) executeToolDetailedWithUserText(name, argsJSON, userText string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
	name = strings.TrimSpace(name)
	kind := classifyAgentToolKind(name)
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
			if h.emitRegisteredToolAgentViewIfNeeded(name, args) {
				return toolExecutionResult{Text: "Tool parameters are incomplete. A task panel form has been opened on the right.", Outcome: toolOutcomeUncertain, FailureKind: toolFailureMissingParameters}
			}
			if validationIssues := registeredToolValidateArgIssues(*tool, args); len(validationIssues) > 0 {
				if h.app != nil {
					if view := buildRegisteredToolAgentView(*tool, args, nil); view != nil {
						applyRegisteredToolFieldIssues(view, validationIssues)
						h.app.emitAgentView(view)
					}
				}
				return toolExecutionResult{Text: "Tool parameters need correction. A task panel form has been opened on the right.", Outcome: toolOutcomeUncertain, FailureKind: toolFailureValidation}
			}
			securityCtx := &SecurityCallContext{SessionID: localSessionIDFromToolArgs(args)}
			if h.emitRegisteredToolApprovalAgentViewIfNeeded(name, args, securityCtx) {
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
	if name != "write_file" && name != "bash" {
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
	}
	valueRunes := len([]rune(value))
	if value == "" || valueRunes <= limit {
		return nil
	}
	text := fmt.Sprintf("Tool %s parameter %s is too large for one agent-loop call (%d runes, limit %d). %s", name, field, valueRunes, limit, agentLoopInlinePayloadLimitInstruction())
	if name == "write_file" {
		text += " Split the content into chunks: first call mode=overwrite, then mode=append for later chunks."
	} else {
		text += " Do not embed generated file bodies in bash commands; use write_file chunks or craft_tool."
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
	if _, alreadyMap := raw.(map[string]interface{}); alreadyMap {
		return args, nil
	}
	normalized := make(map[string]interface{}, len(args))
	for key, value := range args {
		normalized[key] = value
	}
	normalized["arguments"] = toolArgs
	return normalized, nil
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
