package main

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type agentLoopTelemetry struct {
	LoopStartedAt                         time.Time
	PreLLMConfigElapsed                   time.Duration
	PreLLMToolsElapsed                    time.Duration
	PreLLMConversationElapsed             time.Duration
	PreLLMIterationPrepElapsed            time.Duration
	FirstLLMRequestStartedAt              time.Time
	FirstLLMResponseAt                    time.Time
	FirstLLMRetryWaitElapsed              time.Duration
	FirstLLMRetryCount                    int
	FirstLLMRequestMarked                 bool
	StreamDoneAt                          time.Time
	PostStreamUsageDoneAt                 time.Time
	PostStreamLastReturnPrepAt            time.Time
	HandlerPostStreamUsageElapsed         time.Duration
	HandlerPostStreamResponseElapsed      time.Duration
	HandlerPostStreamToolExecElapsed      time.Duration
	HandlerPostStreamChoiceElapsed        time.Duration
	HandlerPostStreamAssistantMsgElapsed  time.Duration
	HandlerPostStreamHistoryAppendElapsed time.Duration
	HandlerPostStreamNoToolBranchElapsed  time.Duration
	LastLLMInputTokens                    int
	LastLLMOutputTokens                   int
	LastLLMCacheReadTokens                int
	LastLLMCacheWriteTokens               int
	// Full-loop accumulated token totals (sum of every LLM round in this turn).
	TotalLLMInputTokens      int
	TotalLLMOutputTokens     int
	TotalLLMCacheReadTokens  int
	TotalLLMCacheWriteTokens int
	FirstRequestMetrics      *llmFirstRequestMetrics
	// Route is the turn model-routing decision (initial + optional escalate).
	Route modelRouteDecision
	// PromptProfile is full|light adaptive system prompt thickness for this turn.
	PromptProfile string
	// Dual-build token estimates when light prompt was used (shadow savings).
	PromptFullTokens  int
	PromptLightTokens int
	// PromptUpgraded is true when light→full recovery ran mid-turn.
	PromptUpgraded bool
	// PromptABSample is true when quality A/B forced full on a light-eligible turn.
	PromptABSample bool
	// PromptSoftFull is true when SoftFullAgentIntent upgraded light→full.
	PromptSoftFull bool
}

func newAgentLoopTelemetry() *agentLoopTelemetry {
	return &agentLoopTelemetry{
		LoopStartedAt:       time.Now(),
		FirstRequestMetrics: &llmFirstRequestMetrics{},
	}
}

func (t *agentLoopTelemetry) Attach(resp *IMAgentResponse) {
	if t == nil || resp == nil {
		return
	}
	if !t.FirstLLMRequestStartedAt.IsZero() {
		resp.PreLLMPrepNanos = t.FirstLLMRequestStartedAt.Sub(t.LoopStartedAt).Nanoseconds()
		resp.PreLLMConfigNanos = t.PreLLMConfigElapsed.Nanoseconds()
		resp.PreLLMToolsNanos = t.PreLLMToolsElapsed.Nanoseconds()
		resp.PreLLMConversationNanos = t.PreLLMConversationElapsed.Nanoseconds()
		resp.PreLLMIterationPrepNanos = t.PreLLMIterationPrepElapsed.Nanoseconds()
	}
	if !t.FirstLLMRequestStartedAt.IsZero() && !t.FirstLLMResponseAt.IsZero() {
		resp.FirstTokenWaitNanos = t.FirstLLMResponseAt.Sub(t.FirstLLMRequestStartedAt).Nanoseconds()
		resp.LLMRequestBuildNanos = t.FirstRequestMetrics.RequestBuildElapsed.Nanoseconds()
		resp.LLMHTTPDoNanos = t.FirstRequestMetrics.HTTPDoElapsed.Nanoseconds()
		resp.LLMFirstSSEWaitNanos = t.FirstRequestMetrics.FirstSSEWaitElapsed.Nanoseconds()
		resp.LLMRetryWaitNanos = t.FirstLLMRetryWaitElapsed.Nanoseconds()
		resp.LLMStreamMaxTokenGapNanos = t.FirstRequestMetrics.StreamMaxTokenGapElapsed.Nanoseconds()
		resp.LLMRetryCount = t.FirstLLMRetryCount
		resp.LLMIdleTimeoutCount = t.FirstRequestMetrics.IdleTimeoutCount
		resp.LLMIdleTimeoutAfterToken = t.FirstRequestMetrics.IdleTimeoutAfterToken
	}
	if !t.StreamDoneAt.IsZero() && resp.HandlerTailNanos == 0 {
		resp.HandlerTailNanos = time.Since(t.StreamDoneAt).Nanoseconds()
	}
	if t.LastLLMInputTokens > 0 || t.LastLLMOutputTokens > 0 || t.LastLLMCacheReadTokens > 0 || t.LastLLMCacheWriteTokens > 0 {
		resp.Fields = mergeIMResponseFields(resp.Fields, tokenUsageResponseFieldsWithCache(t.LastLLMInputTokens, t.LastLLMOutputTokens, t.LastLLMCacheReadTokens, t.LastLLMCacheWriteTokens))
		resp.InputTokens = t.LastLLMInputTokens
		resp.OutputTokens = t.LastLLMOutputTokens
		resp.TotalTokens = t.LastLLMInputTokens + t.LastLLMOutputTokens
		resp.CacheReadTokens = t.LastLLMCacheReadTokens
		resp.CacheWriteTokens = t.LastLLMCacheWriteTokens
		if resp.EstCostRMB <= 0 {
			_, _, totalCost := corelib.CalculateLLMCostRMB(
				int64(t.LastLLMInputTokens),
				int64(t.LastLLMOutputTokens),
				corelib.DefaultLLMInputPricePerMTokensRMB,
				corelib.DefaultLLMOutputPricePerMTokensRMB,
			)
			resp.EstCostRMB = totalCost
		}
	}
	if t.Route.Task != "" || t.Route.Model != "" {
		resp.RouteTask = t.Route.Task
		resp.RouteSource = t.Route.Source
		resp.RouteModel = t.Route.Model
		resp.RouteReason = t.Route.Reason
		resp.RouteEscalated = t.Route.Escalated
		// Verbose multi-field route (visible when show_ai_trace_entry / detail).
		resp.Fields = mergeIMResponseFields(resp.Fields, modelRouteResponseFields(t.Route))
	}
	if pp := strings.TrimSpace(t.PromptProfile); pp != "" {
		resp.PromptProfile = pp
	} else if resp.PromptProfile == "" {
		// Default observe-as-full when hosts did not set a profile.
		resp.PromptProfile = string(agent.PromptProfileFull)
	}
	if t.PromptUpgraded {
		resp.PromptUpgraded = true
		// Effective thickness after recovery is full.
		resp.PromptProfile = string(agent.PromptProfileFull)
	}
	if t.PromptABSample && !t.PromptUpgraded {
		resp.PromptABSample = true
		resp.PromptProfile = string(agent.PromptProfileFull)
	}
	if t.PromptSoftFull && !t.PromptUpgraded && !t.PromptABSample {
		resp.PromptSoftFull = true
		resp.PromptProfile = string(agent.PromptProfileFull)
	}
	// Process-local adaptive prompt hit rate + estimated token savings.
	// Prefer route task (model router) as classify-task breakdown key.
	// Skip re-record when this attach is only reflecting a mid-loop upgrade
	// already counted via RecordLightUpgrade (profile decision was at start).
	// Also skip when system-prompt path already recorded (stats live in agent package).
	// Hosts that build prompts outside Resolve still need this path; shared loop
	// may double-count slightly — acceptable for process-local hit-rate.
	if !t.PromptUpgraded {
		agent.RecordPromptProfileDecision(agent.PromptProfileDecision{
			Profile:     agent.NormalizePromptProfile(resp.PromptProfile),
			FullTokens:  t.PromptFullTokens,
			LightTokens: t.PromptLightTokens,
			Task:        strings.TrimSpace(t.Route.Task),
			Reason:      strings.TrimSpace(t.Route.Reason),
		})
	} else {
		// Still record the initial light decision metrics if dual-build known,
		// then note upgrade was already counted separately.
		agent.RecordPromptProfileDecision(agent.PromptProfileDecision{
			Profile:     agent.PromptProfileLight, // started light
			FullTokens:  t.PromptFullTokens,
			LightTokens: t.PromptLightTokens,
			Task:        strings.TrimSpace(t.Route.Task),
			Reason:      strings.TrimSpace(t.Route.Reason) + "; upgraded mid-loop",
		})
	}
	if t.PromptFullTokens > 0 || t.PromptLightTokens > 0 {
		resp.PromptFullTokens = t.PromptFullTokens
		resp.PromptLightTokens = t.PromptLightTokens
		if t.PromptFullTokens > t.PromptLightTokens {
			resp.PromptSavedTokens = t.PromptFullTokens - t.PromptLightTokens
		}
	}
	// Always-on compact Turn chip for chat UI (route + tokens + est. cost + prompt).
	resp.Fields = mergeIMResponseFields(resp.Fields, turnMetaResponseField(
		t.Route,
		resp.InputTokens,
		resp.OutputTokens,
		resp.CacheReadTokens,
		resp.EstCostRMB,
		resp.PromptProfile,
		resp.PromptSavedTokens,
		resp.PromptUpgraded,
		resp.PromptABSample,
		resp.PromptSoftFull,
	))
	resp.HandlerPostStreamUsageNanos = t.HandlerPostStreamUsageElapsed.Nanoseconds()
	resp.HandlerPostStreamResponseNanos = t.HandlerPostStreamResponseElapsed.Nanoseconds()
	resp.HandlerPostStreamToolExecNanos = t.HandlerPostStreamToolExecElapsed.Nanoseconds()
	resp.HandlerPostStreamChoiceNanos = t.HandlerPostStreamChoiceElapsed.Nanoseconds()
	resp.HandlerPostStreamAssistantMsgNanos = t.HandlerPostStreamAssistantMsgElapsed.Nanoseconds()
	resp.HandlerPostStreamHistoryAppendNanos = t.HandlerPostStreamHistoryAppendElapsed.Nanoseconds()
	resp.HandlerPostStreamNoToolBranchNanos = t.HandlerPostStreamNoToolBranchElapsed.Nanoseconds()
	if !t.StreamDoneAt.IsZero() && !t.PostStreamUsageDoneAt.IsZero() && t.PostStreamUsageDoneAt.After(t.StreamDoneAt) {
		resp.HandlerBlackholeAfterUsageNanos = time.Since(t.PostStreamUsageDoneAt).Nanoseconds() - resp.HandlerPostStreamResponseNanos - resp.MemorySaveNanos - resp.CapabilityGapNanos - resp.FileMaterializeNanos
		if resp.HandlerBlackholeAfterUsageNanos < 0 {
			resp.HandlerBlackholeAfterUsageNanos = 0
		}
	}
	if !t.StreamDoneAt.IsZero() && !t.PostStreamLastReturnPrepAt.IsZero() && t.PostStreamLastReturnPrepAt.After(t.StreamDoneAt) {
		resp.HandlerBlackholeBeforeReturnNanos = time.Since(t.PostStreamLastReturnPrepAt).Nanoseconds()
	}
}

func (t *agentLoopTelemetry) StreamDone() bool {
	return t != nil && !t.StreamDoneAt.IsZero()
}

func (t *agentLoopTelemetry) WrapStreamDoneCallback(onStreamDone StreamDoneCallback) StreamDoneCallback {
	if onStreamDone == nil {
		return nil
	}
	return func() {
		if t != nil && t.StreamDoneAt.IsZero() {
			t.StreamDoneAt = time.Now()
		}
		onStreamDone()
	}
}

func (t *agentLoopTelemetry) ApplyLLMDispatch(result agentLoopLLMDispatchResult) {
	if t == nil {
		return
	}
	t.FirstLLMRequestMarked = result.FirstRequestMarked
	t.FirstLLMRequestStartedAt = result.FirstRequestStarted
	t.FirstLLMResponseAt = result.FirstResponseAt
	t.FirstLLMRetryWaitElapsed += result.RetryWaitElapsed
	t.FirstLLMRetryCount += result.RetryCount
	if result.InputTokens > 0 || result.OutputTokens > 0 || result.CacheReadTokens > 0 || result.CacheWriteTokens > 0 {
		t.LastLLMInputTokens = result.InputTokens
		t.LastLLMOutputTokens = result.OutputTokens
		t.LastLLMCacheReadTokens = result.CacheReadTokens
		t.LastLLMCacheWriteTokens = result.CacheWriteTokens
		t.TotalLLMInputTokens += result.InputTokens
		t.TotalLLMOutputTokens += result.OutputTokens
		t.TotalLLMCacheReadTokens += result.CacheReadTokens
		t.TotalLLMCacheWriteTokens += result.CacheWriteTokens
	}
	if result.PostStreamUsageCompleted {
		t.HandlerPostStreamUsageElapsed += result.UsageElapsed
		t.PostStreamUsageDoneAt = time.Now()
	}
}

func (t *agentLoopTelemetry) ApplyPostLLMTurn(result agentLoopPostLLMTurnResult) {
	if t == nil {
		return
	}
	t.HandlerPostStreamChoiceElapsed += result.PostStreamChoiceElapsed
	t.HandlerPostStreamAssistantMsgElapsed += result.AssistantMessageElapsed
	t.HandlerPostStreamHistoryAppendElapsed += result.HistoryAppendElapsed
}

func (t *agentLoopTelemetry) ApplyNoToolPath(result agentLoopNoToolPathResult) {
	if t == nil {
		return
	}
	t.HandlerPostStreamNoToolBranchElapsed += result.PostStreamBranchElapsed
	if result.PostStreamReturnPrepTime {
		t.PostStreamLastReturnPrepAt = time.Now()
		t.HandlerPostStreamResponseElapsed += result.PostStreamResponseElapsed
	}
}

func (t *agentLoopTelemetry) ApplyToolPath(result agentLoopToolPathResult) {
	if t == nil {
		return
	}
	t.HandlerPostStreamToolExecElapsed += result.ToolExecElapsed
	if result.PostStreamReturnPrepTime {
		t.PostStreamLastReturnPrepAt = time.Now()
	}
}

type llmFirstRequestMetrics struct {
	RequestBuildElapsed      time.Duration
	HTTPDoElapsed            time.Duration
	FirstSSEWaitElapsed      time.Duration
	StreamMaxTokenGapElapsed time.Duration
	IdleTimeoutCount         int
	IdleTimeoutAfterToken    bool
}

func (m *llmFirstRequestMetrics) AddStreamMetrics(metrics *llmStreamMetrics) {
	if m == nil || metrics == nil {
		return
	}
	m.RequestBuildElapsed += time.Duration(metrics.RequestBuildNanos)
	m.HTTPDoElapsed += time.Duration(metrics.HTTPDoNanos)
	m.FirstSSEWaitElapsed += time.Duration(metrics.FirstSSEWaitNanos)
	m.StreamMaxTokenGapElapsed += time.Duration(metrics.MaxTokenGapNanos)
	m.IdleTimeoutCount += metrics.IdleTimeoutCount
	m.IdleTimeoutAfterToken = m.IdleTimeoutAfterToken || metrics.IdleTimeoutAfterToken
}

func (m *llmFirstRequestMetrics) AddIdleMetrics(metrics *llmStreamMetrics) {
	if m == nil || metrics == nil {
		return
	}
	m.IdleTimeoutCount += metrics.IdleTimeoutCount
	m.IdleTimeoutAfterToken = m.IdleTimeoutAfterToken || metrics.IdleTimeoutAfterToken
}
