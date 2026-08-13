package main

import (
	"crypto/sha256"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

type agentLoopRoundPrepOptions struct {
	Context           *LoopContext
	UserID            string
	UserText          string
	Iteration         int
	EffectiveMax      int
	MinIterations     int
	ConfigMax         int
	ChatFinalizeGrace int
	Config            corelib.MaclawLLMConfig
	Conversation      []interface{}
	Tools             []map[string]interface{}
	ToolsTokenBudget  int
	BaseTools         []map[string]interface{}

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
	// FirstRequest is true until an LLM request has actually been dispatched.
	// A cancelled/replanned first request still counts as dispatched, so a
	// replacement request can use the normal context budget.
	FirstRequest         bool
	SendProgress         func(string)
	IsDebug              func() bool
	RecordSystemMessages func(int, []interface{})
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
	// Mid-loop budget gate (iteration 0 already checked at runAgentLoop entry;
	// re-check after prior rounds may have recorded cost).
	if opts.Iteration > 0 {
		if blocked, msg := h.checkDailyBudgetGate(); blocked {
			reqID := ""
			if ctx != nil {
				reqID = ctx.Runtime.RequestID
			}
			result.Response = &IMAgentResponse{
				Text:           msg,
				Error:          "daily_llm_budget_exceeded",
				RequestID:      reqID,
				ResponseSource: "budget_gate",
				HardExit:       true,
			}
			return result
		}
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
	effectiveMax = h.applyAgentLoopBoundaryExtensions(ctx, opts.UserID, opts.Iteration, effectiveMax)
	result.EffectiveMax = effectiveMax
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
		tools, toolsTokenBudget = h.augmentToolsFromInjection(ctx, opts.UserID, injectedText, tools, opts.BaseTools, false)
	}

	// When discover_tool session-pins a conditional tool mid-loop, the tool
	// definition may be missing from the current tool list (which was computed
	// at loop start based on the original user message). Augment with any
	// session-pinned tools that aren't already in the list.
	tools, toolsTokenBudget = h.augmentToolsFromSessionPins(ctx, opts.UserID, tools, toolsTokenBudget)
	forceLightFinalizeWithoutTools := shouldForceLightFinalizeWithoutTools(ctx, opts.Iteration, effectiveMax, opts.ChatFinalizeGrace)
	// An authorised group must retain knowledge_search until it has either
	// produced evidence or established a no-result fallback. Otherwise the
	// light-profile finalization step can make the mandatory first lookup
	// impossible and lead to a fabricated answer.
	keepGroupKnowledgeLookup := ctx != nil && ctx.LansengerGroupPermissions != nil &&
		ctx.LansengerGroupPermissions.requiresKnowledgeLookup()
	if forceLightFinalizeWithoutTools && !keepGroupKnowledgeLookup {
		tools = nil
		toolsTokenBudget = 0
		log.Printf("[exec-profile] light finalize without tools request_id=%q loop=%q iteration=%d effectiveMax=%d grace=%d",
			ctx.Runtime.RequestID, ctx.ID, opts.Iteration, effectiveMax, opts.ChatFinalizeGrace)
	}

	prepStartedAt := time.Now()

	// Keep the run state's effective limit derived from the provider and prior
	// usage. The first-request cap below is only a one-shot compaction target;
	// storing it here would accidentally constrain every later tool round.
	effectiveTokenLimit, _ := calibratedAgentLoopTokenLimit(opts.Config, conversation, opts.LastInputTokens, opts.LastOutputTokens)
	compactionTokenLimit := effectiveTokenLimit
	if opts.FirstRequest {
		normalLimit := effectiveTokenLimit
		compactionTokenLimit = firstAgentLoopRequestTokenLimit(effectiveTokenLimit, conversation, tools)
		if compactionTokenLimit < normalLimit {
			requestID, loopID := "", ""
			if ctx != nil {
				requestID, loopID = ctx.Runtime.RequestID, ctx.ID
			}
			log.Printf("[first-request-budget] request_id=%q loop=%q limit=%d normal_limit=%d tools=%d path=legacy",
				requestID, loopID, compactionTokenLimit, normalLimit, len(tools))
		}
	}
	conversation = h.compactAgentLoopConversation(ctx, opts.UserID, conversation, tools, compactionTokenLimit, toolsTokenBudget, opts.FirstRequest)

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
	)
	conversation = recoverPromptResult.Conversation
	tools = recoverPromptResult.Tools
	toolsTokenBudget = recoverPromptResult.ToolsTokenBudget
	directModeToolsFiltered := opts.DirectModeToolsFiltered
	if recoverPromptResult.Applied {
		directModeToolsFiltered = recoverPromptResult.DirectModeToolsFiltered
	}

	toolsBeforeOrchestrator := len(tools)
	orchestratorStep := h.applyAgentLoopTaskOrchestratorStep(opts.UserID, ctx, tools, conversation, directModeToolsFiltered)
	tools = orchestratorStep.Tools
	conversation = orchestratorStep.Conversation
	directModeToolsFiltered = orchestratorStep.DirectModeToolsFiltered
	if len(tools) != toolsBeforeOrchestrator {
		toolsTokenBudget = estimateToolsTokens(tools)
	}
	if forceLightFinalizeWithoutTools && !keepGroupKnowledgeLookup {
		tools = nil
		toolsTokenBudget = 0
	}

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

func contextCheckpointMode(sessionKey string) agent.ContextCheckpointMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_CONTEXT_CHECKPOINT"))) {
	case "off", "0", "false":
		return agent.ContextCheckpointOff
	case "shadow":
		return agent.ContextCheckpointShadow
	case "on", "1", "true":
		return agent.ContextCheckpointOn
	default:
		// Conservative process-independent rollout: 10% active, 90% shadow.
		// Stable owner hashing keeps one session on one behavior and requires no
		// user-facing setting. Operators may force off/shadow/on with the env above.
		sum := sha256.Sum256([]byte(strings.TrimSpace(sessionKey)))
		if sum[0] < 26 { // 26/256 ~= 10.2%
			return agent.ContextCheckpointOn
		}
		return agent.ContextCheckpointShadow
	}
}

func contextCheckpointStatusMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_CONTEXT_CHECKPOINT"))) {
	case "off", "0", "false":
		return string(agent.ContextCheckpointOff)
	case "shadow":
		return string(agent.ContextCheckpointShadow)
	case "on", "1", "true":
		return string(agent.ContextCheckpointOn)
	default:
		return "rollout_10pct"
	}
}

func (h *IMMessageHandler) compactAgentLoopConversation(ctx *LoopContext, userID string, conversation []interface{}, tools []map[string]interface{}, effectiveTokenLimit, toolsTokenBudget int, firstRequestLatencyBudget bool) []interface{} {
	// A first-response latency budget is intentionally smaller than the normal
	// context window. Checkpoints are lossless, but their file flush/spill work
	// is still avoidable local I/O on the critical path. For this one request,
	// use the fast structural compactor; subsequent rounds retain the normal
	// checkpoint rollout and its lossless handles.
	if firstRequestLatencyBudget {
		return trimConversation(conversation, effectiveTokenLimit, toolsTokenBudget, nil)
	}
	sessionKey := userID
	if h != nil {
		sessionKey = h.workflowPolicyOwnerID(userID, ctx)
	}
	mode := contextCheckpointMode(sessionKey)
	if mode == agent.ContextCheckpointOff {
		return trimConversation(conversation, effectiveTokenLimit, toolsTokenBudget, nil)
	}
	var flush func() error
	if h != nil && h.memoryStore != nil {
		flush = h.memoryStore.Flush
	}
	checkpoint := agent.CheckpointConversation(conversation, agent.ContextCheckpointOptions{
		ContextLimit:   effectiveTokenLimit,
		ToolsTokens:    toolsTokenBudget,
		SessionKey:     sessionKey,
		Tools:          tools,
		BeforeCompress: flush,
		DryRun:         mode == agent.ContextCheckpointShadow,
	})
	if checkpoint.WouldApply && mode == agent.ContextCheckpointShadow {
		log.Printf("[context-checkpoint] mode=shadow owner=%q before=%d after~=%d dropped=%d (no persistence)", sessionKey, checkpoint.BeforeTokens, checkpoint.AfterTokens, checkpoint.DroppedCount)
	}
	if checkpoint.Applied {
		log.Printf("[context-checkpoint] mode=%s owner=%q before=%d after=%d dropped=%d handle=%s", mode, sessionKey, checkpoint.BeforeTokens, checkpoint.AfterTokens, checkpoint.DroppedCount, checkpoint.Handle.ID)
		if mode == agent.ContextCheckpointOn {
			return checkpoint.Conversation
		}
	}
	return trimConversation(conversation, effectiveTokenLimit, toolsTokenBudget, nil)
}

func shouldForceLightFinalizeWithoutTools(ctx *LoopContext, iteration int, effectiveMax int, chatFinalizeGrace int) bool {
	if ctx == nil || !ctx.Runtime.Execution.IsLight() || chatFinalizeGrace <= 0 {
		return false
	}
	return effectiveMax > 0 && iteration >= effectiveMax
}

func extendEffectiveMaxForPendingGuideReference(iteration, effectiveMax int, hasPendingGuideReference bool) int {
	if hasPendingGuideReference && iteration >= effectiveMax {
		return iteration + 1
	}
	return effectiveMax
}

func extendEffectiveMaxForPendingBackgroundTask(iteration, effectiveMax int, hasPendingBackgroundTask bool) int {
	if hasPendingBackgroundTask && iteration == effectiveMax {
		return iteration + 1
	}
	return effectiveMax
}

func (h *IMMessageHandler) applyAgentLoopBoundaryExtensions(ctx *LoopContext, userID string, iteration, effectiveMax int) int {
	extendedMax := extendEffectiveMaxForPendingGuideReference(iteration, effectiveMax, h.hasPendingGuideReferenceInjection(userID))
	backgroundExtended := false
	backgroundTaskKey := h.pendingBackgroundTaskBoundaryKey(ctx)
	if backgroundTaskKey != "" {
		backgroundExtendedMax := extendEffectiveMaxForPendingBackgroundTask(iteration, effectiveMax, true)
		if backgroundExtendedMax > effectiveMax && (ctx == nil || ctx.MarkBackgroundTaskBoundaryExtended(backgroundTaskKey)) {
			backgroundExtended = true
			if backgroundExtendedMax > extendedMax {
				extendedMax = backgroundExtendedMax
			}
		}
	}
	if ctx != nil && backgroundExtended && extendedMax > effectiveMax {
		ctx.SetMaxIterations(extendedMax)
	}
	return extendedMax
}
