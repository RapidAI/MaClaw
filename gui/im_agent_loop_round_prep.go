package main

import (
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

type agentLoopRoundPrepOptions struct {
	Context                 *LoopContext
	UserID                  string
	UserText                string
	Iteration               int
	EffectiveMax            int
	MinIterations           int
	ConfigMax               int
	ChatFinalizeGrace       int
	Config                  corelib.MaclawLLMConfig
	HTTPClient              *http.Client
	Conversation            []interface{}
	Tools                   []map[string]interface{}
	ToolsTokenBudget        int
	BaseTools               []map[string]interface{}
	GateConfig              codingToolGateConfig
	SkipCodingGate          bool
	OrchestratorActive      func() bool
	DirectModeToolsFiltered bool
	EffectiveTokenLimit     int
	Phase                   *agentLoopPhase
	GoalAnchor              *GoalAnchor
	ProgressTracker         *HarnessProgressTracker
	TrialState              *trialReflectState
	DriftDetector           *DriftDetector
	MilestoneTracker        *progress.AgentProgressTracker
	LastInputTokens         int
	LastOutputTokens        int
	SendProgress            func(string)
	IsDebug                 func() bool
	RecordSystemMessages    func(int, []interface{})
}

type agentLoopRoundPrepResult struct {
	Conversation            []interface{}
	Tools                   []map[string]interface{}
	ToolsTokenBudget        int
	EffectiveMax            int
	EffectiveTokenLimit     int
	DirectModeToolsFiltered bool
	PrepElapsed             time.Duration
	Response                *IMAgentResponse
	Stop                    bool
}

func (h *IMMessageHandler) prepareAgentLoopRound(opts agentLoopRoundPrepOptions) agentLoopRoundPrepResult {
	ctx := opts.Context
	result := agentLoopRoundPrepResult{
		Conversation:            opts.Conversation,
		Tools:                   opts.Tools,
		ToolsTokenBudget:        opts.ToolsTokenBudget,
		EffectiveMax:            opts.EffectiveMax,
		EffectiveTokenLimit:     opts.EffectiveTokenLimit,
		DirectModeToolsFiltered: opts.DirectModeToolsFiltered,
	}
	effectiveMax := h.refreshAgentLoopEffectiveMax(ctx, opts.Iteration, opts.EffectiveMax, opts.MinIterations, opts.DriftDetector, opts.SendProgress)
	result.EffectiveMax = effectiveMax
	if resp, handled := handleBackgroundIterationPause(ctx, opts.Iteration, effectiveMax); handled {
		result.Response = resp
		return result
	}
	if cm := ctx.MaxIterations(); cm > 0 && cm != effectiveMax {
		effectiveMax = cm
		result.EffectiveMax = effectiveMax
	}
	if shouldStopForAgentLoopIterationLimit(ctx, opts.Iteration, effectiveMax, opts.ChatFinalizeGrace) {
		result.Stop = true
		return result
	}

	conversation, cancelled, injectedText := h.prepareAgentLoopIteration(
		ctx,
		opts.UserID,
		opts.UserText,
		opts.Iteration,
		effectiveMax,
		opts.ConfigMax,
		opts.Conversation,
		opts.SendProgress,
		opts.MilestoneTracker,
		opts.IsDebug,
	)
	if cancelled {
		result.Conversation = conversation
		result.Stop = true
		return result
	}

	// When a merge injection changes the task direction (e.g. user says "use
	// SSH to connect" while the loop was doing Nginx analysis), the tool list
	// computed at loop start may not include the newly needed tools. Re-route
	// with the injection text and augment the current tool set.
	tools := opts.Tools
	toolsTokenBudget := opts.ToolsTokenBudget
	if injectedText != "" {
		tools, toolsTokenBudget = h.augmentToolsFromInjection(injectedText, tools, opts.BaseTools, opts.GateConfig.active)
	}

	prepStartedAt := time.Now()
	conversation = autoCompressConversation(conversation, opts.Config, opts.HTTPClient)

	effectiveTokenLimit := calibratedAgentLoopTokenLimit(opts.Config, conversation, opts.LastInputTokens, opts.LastOutputTokens)
	conversation = trimConversation(conversation, effectiveTokenLimit, opts.ToolsTokenBudget, makeSummarizer(opts.Config, opts.HTTPClient))

	phase := derefAgentLoopPhase(opts.Phase)
	conversation, systemMessagesStart := h.injectAgentLoopHarnessPrompts(
		ctx,
		conversation,
		phase,
		opts.Iteration,
		effectiveMax,
		opts.GoalAnchor,
		opts.ProgressTracker,
		opts.TrialState,
	)
	recoverPromptResult := h.applyAgentLoopRecoverPrompt(
		ctx,
		opts.UserID,
		opts.Phase,
		conversation,
		tools,
		toolsTokenBudget,
		opts.BaseTools,
		opts.GateConfig,
		opts.SkipCodingGate,
		opts.OrchestratorActive,
	)
	conversation = recoverPromptResult.Conversation
	tools = recoverPromptResult.Tools
	toolsTokenBudget = recoverPromptResult.ToolsTokenBudget
	directModeToolsFiltered := opts.DirectModeToolsFiltered
	if recoverPromptResult.Applied {
		directModeToolsFiltered = recoverPromptResult.DirectModeToolsFiltered
	}

	orchestratorStep := h.applyAgentLoopTaskOrchestratorStep(opts.UserID, ctx, tools, conversation, directModeToolsFiltered)
	tools = orchestratorStep.Tools
	conversation = orchestratorStep.Conversation
	directModeToolsFiltered = orchestratorStep.DirectModeToolsFiltered

	if opts.RecordSystemMessages != nil {
		opts.RecordSystemMessages(systemMessagesStart, conversation)
		if convergePrompt := buildSkillPreferenceConvergePrompt(derefAgentLoopPhase(opts.Phase)); convergePrompt != "" {
			conversation = append(conversation, map[string]string{"role": "system", "content": convergePrompt})
			opts.RecordSystemMessages(len(conversation)-1, conversation)
		}
	}

	return agentLoopRoundPrepResult{
		Conversation:            conversation,
		Tools:                   tools,
		ToolsTokenBudget:        toolsTokenBudget,
		EffectiveMax:            effectiveMax,
		EffectiveTokenLimit:     effectiveTokenLimit,
		DirectModeToolsFiltered: directModeToolsFiltered,
		PrepElapsed:             time.Since(prepStartedAt),
	}
}
