package guiapp

import (
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
	// with the injection text and augment the current tool set. A capability-
	// managed turn already has a closed grant surface; name-router augment
	// and discover_tool pins must not union soup tools onto it.
	tools := opts.Tools
	toolsTokenBudget := opts.ToolsTokenBudget
	phase := derefAgentLoopPhase(opts.Phase)
	if !loopContextBlocksLegacyToolRouter(ctx) {
		if injectedText != "" {
			tools, toolsTokenBudget = h.augmentToolsFromInjection(ctx, opts.UserID, injectedText, tools, opts.BaseTools, false, phase)
		}
		tools, toolsTokenBudget = h.augmentToolsFromSessionPins(ctx, opts.UserID, tools, toolsTokenBudget)
	}
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
		// Same guard as the shared loop: the latency budget only applies to a
		// turn that would otherwise exceed the provider window.
		beforeTokens := estimateConversationTokens(conversation) + toolsTokenBudget
		if limit, budgeted := firstRequestCompactionLimit(effectiveTokenLimit, beforeTokens, conversation, tools); budgeted {
			compactionTokenLimit = limit
			requestID, loopID := "", ""
			if ctx != nil {
				requestID, loopID = ctx.Runtime.RequestID, ctx.ID
			}
			log.Printf("[first-request-budget] request_id=%q loop=%q limit=%d normal_limit=%d tools=%d path=legacy",
				requestID, loopID, compactionTokenLimit, effectiveTokenLimit, len(tools))
		}
	}
	conversation = h.compactAgentLoopConversation(ctx, opts.UserID, conversation, tools, compactionTokenLimit, toolsTokenBudget)
	// A checkpoint handle is only useful if this request can page it. The
	// spill path adds the reader for eligibility; the rendered surface has
	// to carry the same definition or the model is told to call a tool that
	// is not listed.
	if conversationHasToolResultHandle(conversation) {
		tools = h.checkpointToolsWithReader(tools)
		toolsTokenBudget = estimateToolsTokens(tools)
	}

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

	// A floor tool call was policy_rejected on an earlier round: runtime proof
	// the request surface is under-scoped for this turn. Re-union the
	// invariant-11 floor tools from the base surface so the model's next
	// attempt can execute (see MissFloorToolsUnlock).
	if opts.Phase != nil && opts.Phase.MissFloorToolsUnlock {
		if !loopContextBlocksLegacyToolRouter(ctx) {
			tools = unionMissFloorToolsForSurface(tools, opts.BaseTools)
			// baseTools predates expert, group, and skill filters. The
			// direct-mode latch is already true on later rounds, so this
			// seal has to drop coding tools itself.
			inDirect := h.mainLoopInDirectMode(opts.UserID, ctx)
			if inDirect {
				directModeToolsFiltered = true
			}
			tools = h.sealClassifierTimeoutExecutionFloor(opts.UserID, ctx, tools, *opts.Phase, inDirect, boundFloorCatalog(opts.BaseTools))
			toolsTokenBudget = estimateToolsTokens(tools)
			if h.traceService != nil && ctx != nil && ctx.RunID != "" {
				h.appendTraceEvent(ctx, "surface.floor_unlocked", "warn", "Re-united floor tools after under-scoped surface", truncateTraceText(strings.Join(agentLoopToolNamesForLog(tools), ","), 220), "", "")
			}
		}
		opts.Phase.MissFloorToolsUnlock = false
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
	if opts.Phase != nil && opts.Phase.SkillMode == skillPreferenceAgentGuided {
		tools = h.finishAgentGuidedSurface(opts.UserID, ctx, tools, opts.BaseTools, *opts.Phase)
		if h.mainLoopInDirectMode(opts.UserID, ctx) {
			directModeToolsFiltered = true
		}
		toolsTokenBudget = estimateToolsTokens(tools)
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

func contextCheckpointMode() agent.ContextCheckpointMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_CONTEXT_CHECKPOINT"))) {
	case "off", "0", "false":
		return agent.ContextCheckpointOff
	case "shadow":
		return agent.ContextCheckpointShadow
	case "on", "1", "true":
		return agent.ContextCheckpointOn
	default:
		// Lossless checkpoints are on by default so long-running tasks (e.g.
		// large document processing) never silently lose earlier context.
		// Operators may force off/shadow with the env above.
		return agent.ContextCheckpointOn
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
		return string(agent.ContextCheckpointOn)
	}
}

// agentLoopCompactionSummarizer builds the LLM summarizer used when
// trimConversation drops older history in the agent loop.
//
// Without this, dropped history was replaced by a static placeholder and the
// model lost the task goal, file paths and prior decisions — long-running
// tasks (e.g. large document processing) derailed a few rounds after the
// first compaction.
func (h *IMMessageHandler) agentLoopCompactionSummarizer() func(string) string {
	if h == nil {
		return nil
	}
	return guardedCompactionSummarizer(h.getMaclawLLMConfig(), h.client)
}

func (h *IMMessageHandler) compactAgentLoopConversation(ctx *LoopContext, userID string, conversation []interface{}, tools []map[string]interface{}, effectiveTokenLimit, toolsTokenBudget int) []interface{} {
	// The caller may pass a smaller first-request limit. That budget only
	// decides how much of the transcript is inlined. Dropped history still
	// goes through the lossless checkpoint: the exact JSON stays behind
	// read_tool_result, and the preview tells the model to page it before
	// guessing. Skipping the checkpoint and substituting "history was
	// omitted" is what made a resumed task's first message deny its own
	// context. The summarizer remains the fallback when a checkpoint cannot
	// be stored (mode off, no reader, invalid groups).
	// The summarizer is an LLM call used only when the checkpoint cannot
	// store what it drops. Resolving it loads the app config, so a turn
	// whose transcript already fits must not pay that before the decision.
	summarizer := h.agentLoopCompactionSummarizer
	sessionKey := userID
	if h != nil {
		sessionKey = h.workflowPolicyOwnerID(userID, ctx)
	}
	mode := contextCheckpointMode()
	if mode == agent.ContextCheckpointOff {
		// Checkpoint is the only pass that folds old desktop screenshots.
		// Trim still has to see that folded transcript, or an image counted
		// as 85 tokens stays in the request as the original bytes.
		return trimConversation(agent.FoldComputerUseObserves(conversation), effectiveTokenLimit, toolsTokenBudget, summarizer())
	}
	// Short transcripts never reach a checkpoint. Attaching the reader only
	// when one might apply keeps a lookup turn from building tool defs.
	if len(conversation) > 3 {
		tools = h.checkpointToolsWithReader(tools)
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
	// Folding older desktop screenshots happens before the fit check. Keep
	// that folded transcript: the token count treats an image as a flat 85,
	// so handing the original bytes to trim would leave them inline.
	folded := checkpoint.Conversation
	switch checkpoint.Reason {
	case "below_threshold", "protected_window", "no_savings", "opaque_content", "nothing_to_drop":
		// below_threshold and protected_window already fit. no_savings means
		// the checkpoint preview was larger than the prefix it would replace,
		// so the spill did not shrink the prompt. opaque_content and
		// nothing_to_drop mean an image (or an empty drop set) stayed inline
		// on purpose. Rewriting that transcript into the omission line is
		// what made a resumed turn deny its own history. Keep the folded
		// transcript.
		return folded
	}
	return trimConversation(folded, effectiveTokenLimit, toolsTokenBudget, summarizer())
}

// checkpointToolsWithReader makes the lossless checkpoint eligible on a
// surface that did not already list the reader. CheckpointConversation is
// fail-closed without read_tool_result: it would otherwise fall through to
// the omission placeholder. The definition is the host builtin, not a new grant.
func (h *IMMessageHandler) checkpointToolsWithReader(tools []map[string]interface{}) []map[string]interface{} {
	const name = "read_tool_result"
	if h == nil || toolsIncludeName(tools, name) {
		return tools
	}
	// Core schema, not the live catalog. continuationHostDefinitionByName
	// rebuilds every host tool when the registry misses the name, and this
	// runs on the compaction path of every over-window turn.
	def := toolDefFromCore(name, "", nil)
	if extractToolName(def) != name {
		return tools
	}
	out := make([]map[string]interface{}, len(tools), len(tools)+1)
	copy(out, tools)
	return append(out, def)
}

func toolsIncludeName(tools []map[string]interface{}, name string) bool {
	for _, def := range tools {
		if extractToolName(def) == name {
			return true
		}
	}
	return false
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
