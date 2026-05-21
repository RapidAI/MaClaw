package main

import "time"

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
	FirstRequestMetrics                   *llmFirstRequestMetrics
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
	}
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
