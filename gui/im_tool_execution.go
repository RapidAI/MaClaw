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
}

func (h *IMMessageHandler) executeAgentLoopToolCall(opts agentLoopToolExecutionOptions) toolExecutionResult {
	tc := opts.ToolCall
	if opts.MilestoneTracker != nil {
		opts.MilestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, false)
	}
	if opts.Debug && opts.SendToolProgress != nil {
		opts.SendToolProgress(userFacingToolProgressText(tc.Function.Name))
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
		result = h.executeToolDetailed(tc.Function.Name, tc.Function.Arguments, filteredToolProgressCallback(tc.Function.Name, opts.OnProgress, opts.Debug))
	}

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
