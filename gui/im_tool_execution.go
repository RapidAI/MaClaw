package main

// Tool execution: dispatcher that routes tool calls to registered handlers.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

type agentLoopToolExecutionOptions struct {
	UserID           string
	SkipWorkflowGate bool
	ToolCall         llm.ToolCall
	Iteration        int
	Phase            agentLoopPhase
	Debug            bool
	OnProgress       coretool.ProgressCallback
	SendToolProgress func(string)
	MilestoneTracker *progress.AgentProgressTracker
	RecordToolCall   func(string, string, string)
	AdaptiveRetry    *AdaptiveRetry
}

func (h *IMMessageHandler) executeAgentLoopToolCall(opts agentLoopToolExecutionOptions) toolExecutionResult {
	tc := opts.ToolCall
	if opts.MilestoneTracker != nil {
		opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, false)
	}
	// Always send tool-specific progress to the frontend so users see which
	// tool is being executed (e.g. "🔍 正在搜索网络..." instead of generic
	// "正在执行工具..."). Previously gated behind opts.Debug.
	if opts.SendToolProgress != nil {
		opts.SendToolProgress(userFacingToolProgressTextWithArgs(tc.Function.Name, tc.Function.Arguments))
	}
	if opts.RecordToolCall != nil {
		opts.RecordToolCall(tc.ID, tc.Function.Name, tc.Function.Arguments)
	}

	var result toolExecutionResult
	if !opts.SkipWorkflowGate && !h.isWorkflowToolAllowed(opts.UserID, tc.Function.Name) {
		text := fmt.Sprintf("[system rejected] %s is not allowed by the current workflow tool policy.", tc.Function.Name)
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
	if result.Text == "" && opts.Phase.TruncationBlockedTools[tc.Function.Name] {
		text := fmt.Sprintf("[system rejected] %s is temporarily blocked because its arguments were repeatedly truncated. Use another currently available tool path.", tc.Function.Name)
		result = toolExecutionResult{Text: text, ToolName: tc.Function.Name, ToolKind: classifyAgentToolKind(tc.Function.Name), Outcome: toolOutcomeFailed, FailureKind: toolFailureTruncationBlocked}
		log.Printf("[agent-loop] rejected execution of truncation-blocked tool %q (iter=%d)", tc.Function.Name, opts.Iteration)
	}
	if result.Text == "" {
		// In agent loop context, intercept missing/invalid parameter errors BEFORE
		// executeToolDetailed. executeToolDetailed would emit an AgentView panel
		// (designed for user-manual tool invocations), which is wrong inside an
		// agent loop — the LLM should receive an error message and self-correct.
		if errResult := h.preCheckToolArgsForAgentLoop(tc.Function.Name, tc.Function.Arguments, opts.Iteration); errResult != nil {
			result = *errResult
		}
	}
	if result.Text == "" {
		result = h.executeToolDetailed(tc.Function.Name, tc.Function.Arguments, filteredToolProgressCallback(tc.Function.Name, opts.OnProgress, opts.Debug))
	}
	h.recordAdaptiveRetryToolFailure(opts.AdaptiveRetry, tc.Function.Name, result, opts.Iteration)

	if opts.MilestoneTracker != nil {
		opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, true)
	}
	return result
}

func (h *IMMessageHandler) isWorkflowToolAllowed(userID, name string) bool {
	engine := h.getWorkflowEngine()
	if engine == nil || strings.TrimSpace(userID) == "" {
		return true
	}
	return workflow.IsToolAllowedByPolicy(engine.GetPhaseToolFilter(userID), name)
}

func (h *IMMessageHandler) isWorkflowToolCallAllowed(userID, name, argsJSON string) (bool, string) {
	engine := h.getWorkflowEngine()
	if engine == nil || strings.TrimSpace(userID) == "" {
		return true, ""
	}
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		cleaned := coretool.CleanToolArguments(argsJSON)
		if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
			return false, fmt.Sprintf("invalid tool arguments: %v", err)
		}
	}
	if err := workflow.ValidateToolCallByPolicyWithApproval(engine.GetPhaseToolFilter(userID), strings.TrimSpace(name), args, engine.GetOpsApprovedCommands(userID)); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (h *IMMessageHandler) executeTool(name, argsJSON string, onProgress coretool.ProgressCallback) string {
	return h.executeToolDetailed(name, argsJSON, onProgress).Text
}

func (h *IMMessageHandler) executeToolDetailed(name, argsJSON string, onProgress coretool.ProgressCallback) (result toolExecutionResult) {
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

	h.trackSteeringFileFromArgs(name, args)

	if kind == agentToolKindSearchAndInstallSkill {
		installResult := h.toolSearchAndInstallSkillResult(args, onProgress)
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
				return toolExecutionResult{Text: text, Outcome: toolOutcomeUncertain, FailureKind: toolFailureNone}
			}
			if tool.Handler != nil {
				text := tool.Handler(args)
				return toolExecutionResult{Text: text, Outcome: toolOutcomeUncertain, FailureKind: toolFailureNone}
			}
		}
	}

	return toolExecutionResult{Text: fmt.Sprintf("Unknown tool: %s", name), Outcome: toolOutcomeFailed, FailureKind: toolFailureUnknownTool}
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
		_ = json.Unmarshal([]byte(cleaned), &args)
	}
	if args == nil {
		args = map[string]interface{}{}
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
	return nil
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
