package main

import (
	"context"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopCompletionOptions struct {
	Context                *LoopContext
	RequestContext         context.Context
	UserID                 string
	UserText               string
	Config                 corelib.MaclawLLMConfig
	Conversation           []interface{}
	History                []agent.ConversationEntry
	Tools                  []map[string]interface{}
	HTTPClient             *http.Client
	EffectiveTokenLimit    int
	ToolsTokenBudget       int
	OnToken                llm.TokenCallback
	OnProgress             tool.ProgressCallback
	OnNewRound             NewRoundCallback
	StreamDoneCallback     StreamDoneCallback
	FirstRequestMetrics    *llmFirstRequestMetrics
	StreamDone             bool
	Phase                  *agentLoopPhase
	MilestoneTracker       *progress.AgentProgressTracker
	Recorder               *TrajectoryRecorder
	InFlightLifecycle      *imInFlightLifecycle
	RecordToolCall         func(string, string, string)
	RecordToolResult       func(string, interface{})
	AttachLLMTelemetry     func(*IMAgentResponse)
	AttachVisibleArtifacts func(*IMAgentResponse)
	SendProgress           func(string)
	Debug                  bool
	LastInputTokens        int
	LastOutputTokens       int
	LastCacheReadTokens    int
	LastCacheWriteTokens   int
	TotalToolCallsInLoop   int
	EffectiveMax           int
	ConfigMax              int
	LoopMaxOverride        int
	ChatFinalizeGrace      int
	ConversationStartedAt  time.Time
}

type agentLoopCompletionResult struct {
	Response         *IMAgentResponse
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	UsageElapsed     time.Duration
	UsageDone        bool
	ToolExecElapsed  time.Duration
}

func (h *IMMessageHandler) finishAgentLoopAndRecordTelemetry(opts agentLoopCompletionOptions, telemetry *agentLoopTelemetry) *IMAgentResponse {
	completion := h.finishAgentLoopAfterMainIterations(opts)
	if telemetry != nil {
		telemetry.LastLLMInputTokens = completion.InputTokens
		telemetry.LastLLMOutputTokens = completion.OutputTokens
		telemetry.LastLLMCacheReadTokens = completion.CacheReadTokens
		telemetry.LastLLMCacheWriteTokens = completion.CacheWriteTokens
		if completion.UsageDone {
			telemetry.HandlerPostStreamUsageElapsed += completion.UsageElapsed
			telemetry.PostStreamUsageDoneAt = time.Now()
		}
		telemetry.HandlerPostStreamToolExecElapsed += completion.ToolExecElapsed
	}
	return completion.Response
}

func (h *IMMessageHandler) finishAgentLoopAfterMainIterations(opts agentLoopCompletionOptions) agentLoopCompletionResult {
	result := agentLoopCompletionResult{
		InputTokens:      opts.LastInputTokens,
		OutputTokens:     opts.LastOutputTokens,
		CacheReadTokens:  opts.LastCacheReadTokens,
		CacheWriteTokens: opts.LastCacheWriteTokens,
	}
	if h.manager != nil && h.manager.HasActiveSessions() {
		bonusResult := h.runActiveSessionBonusRound(agentLoopBonusRoundOptions{
			UserID:                 opts.UserID,
			Config:                 opts.Config,
			RequestContext:         opts.RequestContext,
			Conversation:           opts.Conversation,
			History:                opts.History,
			Tools:                  opts.Tools,
			HTTPClient:             opts.HTTPClient,
			EffectiveTokenLimit:    opts.EffectiveTokenLimit,
			ToolsTokenBudget:       opts.ToolsTokenBudget,
			OnToken:                opts.OnToken,
			OnProgress:             opts.OnProgress,
			OnNewRound:             opts.OnNewRound,
			StreamDoneCallback:     opts.StreamDoneCallback,
			FirstRequestMetrics:    opts.FirstRequestMetrics,
			StreamDone:             opts.StreamDone,
			Phase:                  opts.Phase,
			MilestoneTracker:       opts.MilestoneTracker,
			Recorder:               opts.Recorder,
			InFlightLifecycle:      opts.InFlightLifecycle,
			RecordToolCall:         opts.RecordToolCall,
			RecordToolResult:       opts.RecordToolResult,
			AttachLLMTelemetry:     opts.AttachLLMTelemetry,
			AttachVisibleArtifacts: opts.AttachVisibleArtifacts,
			SendProgress:           opts.SendProgress,
			Debug:                  opts.Debug,
		})
		if bonusResult.InputTokens > 0 || bonusResult.OutputTokens > 0 || bonusResult.CacheReadTokens > 0 || bonusResult.CacheWriteTokens > 0 {
			result.InputTokens = bonusResult.InputTokens
			result.OutputTokens = bonusResult.OutputTokens
			result.CacheReadTokens = bonusResult.CacheReadTokens
			result.CacheWriteTokens = bonusResult.CacheWriteTokens
		}
		if bonusResult.UsageDone {
			result.UsageElapsed = bonusResult.UsageElapsed
			result.UsageDone = true
		}
		result.ToolExecElapsed = bonusResult.ToolExecElapsed
		result.Response = bonusResult.Response
		return result
	}

	phase := agentLoopPhase{}
	if opts.Phase != nil {
		phase = *opts.Phase
	}
	result.Response = h.maxRoundsAgentLoopExit(opts.Context, agentLoopMaxRoundsExitOptions{
		UserID:                 opts.UserID,
		UserText:               opts.UserText,
		History:                opts.History,
		Phase:                  phase,
		FinalIteration:         opts.Context.Iteration(),
		TotalToolCallsInLoop:   opts.TotalToolCallsInLoop,
		EffectiveMax:           opts.EffectiveMax,
		ConfigMax:              opts.ConfigMax,
		LoopMaxOverride:        opts.LoopMaxOverride,
		ChatFinalizeGrace:      opts.ChatFinalizeGrace,
		ConversationStartedAt:  opts.ConversationStartedAt,
		AttachLLMTelemetry:     opts.AttachLLMTelemetry,
		AttachVisibleArtifacts: opts.AttachVisibleArtifacts,
	})
	return result
}
