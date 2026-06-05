package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopLLMRoundOptions struct {
	Context             *LoopContext
	RequestContext      context.Context
	Config              corelib.MaclawLLMConfig
	Conversation        []interface{}
	Tools               []map[string]interface{}
	HTTPClient          *http.Client
	OnToken             llm.TokenCallback
	OnProgress          tool.ProgressCallback
	StreamDoneCallback  func()
	AdaptiveRetry       *AdaptiveRetry
	FirstRequestMetrics *llmFirstRequestMetrics
	FirstRequestMarked  bool
	FirstResponseAt     time.Time
	StreamDone          bool
	UserID              string
	History             []agent.ConversationEntry
	UserText            string
	Iteration           int
	InFlightLifecycle   *imInFlightLifecycle
}

type agentLoopLLMRoundResult struct {
	Response         *llm.Response
	Err              error
	Conversation     []interface{}
	FirstResponseAt  time.Time
	RetryWaitElapsed time.Duration
	RetryCount       int
	Usage            llmUsageSnapshot
	UsageElapsed     time.Duration
	UsageDone        bool
	Exit             *IMAgentResponse
	Cancelled        bool
}

type agentLoopLLMDispatchOptions struct {
	Context             *LoopContext
	RequestContext      context.Context
	Config              corelib.MaclawLLMConfig
	Conversation        []interface{}
	Tools               []map[string]interface{}
	HTTPClient          *http.Client
	OnToken             llm.TokenCallback
	OnProgress          tool.ProgressCallback
	OnNewRound          NewRoundCallback
	StreamDoneCallback  func()
	AdaptiveRetry       *AdaptiveRetry
	FirstRequestMetrics *llmFirstRequestMetrics
	FirstRequestMarked  bool
	FirstRequestStarted time.Time
	FirstResponseAt     time.Time
	StreamDone          bool
	UserID              string
	History             []agent.ConversationEntry
	UserText            string
	Iteration           int
	InFlightLifecycle   *imInFlightLifecycle
}

type agentLoopLLMDispatchResult struct {
	Response                 *llm.Response
	Conversation             []interface{}
	FirstRequestMarked       bool
	FirstRequestStarted      time.Time
	FirstResponseAt          time.Time
	RetryWaitElapsed         time.Duration
	RetryCount               int
	InputTokens              int
	OutputTokens             int
	CacheReadTokens          int
	CacheWriteTokens         int
	UsageElapsed             time.Duration
	PostStreamUsageCompleted bool
	Exit                     *IMAgentResponse
	Cancelled                bool
}

func (h *IMMessageHandler) dispatchAgentLoopLLMRound(opts agentLoopLLMDispatchOptions) agentLoopLLMDispatchResult {
	result := agentLoopLLMDispatchResult{
		Conversation:        opts.Conversation,
		FirstRequestMarked:  opts.FirstRequestMarked,
		FirstRequestStarted: opts.FirstRequestStarted,
		FirstResponseAt:     opts.FirstResponseAt,
	}
	if opts.OnNewRound != nil && opts.Iteration > 0 {
		opts.OnNewRound()
	}
	llmCallStartedAt := time.Now()
	if !result.FirstRequestMarked {
		result.FirstRequestMarked = true
		result.FirstRequestStarted = llmCallStartedAt
	}
	llmRound := h.executeAgentLoopLLMRound(agentLoopLLMRoundOptions{
		Context:             opts.Context,
		RequestContext:      opts.RequestContext,
		Config:              opts.Config,
		Conversation:        opts.Conversation,
		Tools:               opts.Tools,
		HTTPClient:          opts.HTTPClient,
		OnToken:             opts.OnToken,
		OnProgress:          opts.OnProgress,
		StreamDoneCallback:  opts.StreamDoneCallback,
		AdaptiveRetry:       opts.AdaptiveRetry,
		FirstRequestMetrics: opts.FirstRequestMetrics,
		FirstRequestMarked:  result.FirstRequestMarked,
		FirstResponseAt:     opts.FirstResponseAt,
		StreamDone:          opts.StreamDone,
		UserID:              opts.UserID,
		History:             opts.History,
		UserText:            opts.UserText,
		Iteration:           opts.Iteration,
		InFlightLifecycle:   opts.InFlightLifecycle,
	})
	result.Response = llmRound.Response
	result.Conversation = llmRound.Conversation
	result.FirstResponseAt = llmRound.FirstResponseAt
	result.RetryWaitElapsed = llmRound.RetryWaitElapsed
	result.RetryCount = llmRound.RetryCount
	if llmRound.Usage.HasAny() {
		result.InputTokens = llmRound.Usage.Input
		result.OutputTokens = llmRound.Usage.Output
		result.CacheReadTokens = llmRound.Usage.CacheRead
		result.CacheWriteTokens = llmRound.Usage.CacheWrite
	}
	if llmRound.UsageDone {
		result.UsageElapsed = llmRound.UsageElapsed
		result.PostStreamUsageCompleted = true
	}
	result.Cancelled = llmRound.Cancelled
	result.Exit = llmRound.Exit
	return result
}

func (h *IMMessageHandler) executeAgentLoopLLMRound(opts agentLoopLLMRoundOptions) agentLoopLLMRoundResult {
	result := agentLoopLLMRoundResult{
		Conversation:    opts.Conversation,
		FirstResponseAt: opts.FirstResponseAt,
	}

	streamMetrics := &llmStreamMetrics{}
	requestID := ""
	loopID := ""
	if opts.Context != nil {
		requestID = opts.Context.Runtime.RequestID
		loopID = opts.Context.ID
	}
	reqCtx := llm.WithRequestTrace(opts.RequestContext, llm.RequestTrace{
		Caller:    agentLoopLLMCallerFromContext(opts.RequestContext),
		OwnerID:   opts.UserID,
		RequestID: requestID,
		LoopID:    loopID,
		Iteration: opts.Iteration,
	})
	resp, err := h.doLLMRequestStream(reqCtx, opts.Config, opts.Conversation, opts.Tools, opts.HTTPClient, opts.OnToken, streamMetrics)
	if !opts.FirstRequestMarked || opts.FirstRequestMetrics.RequestBuildElapsed == 0 {
		opts.FirstRequestMetrics.AddStreamMetrics(streamMetrics)
	}
	if err == nil && result.FirstResponseAt.IsZero() {
		if !streamMetrics.FirstTokenAt.IsZero() {
			result.FirstResponseAt = streamMetrics.FirstTokenAt
		} else {
			result.FirstResponseAt = time.Now()
		}
	}
	if err == nil && opts.StreamDoneCallback != nil {
		opts.StreamDoneCallback()
	}
	if err != nil {
		retryResult := h.retryAgentLoopLLMRequestAfterError(opts.Context, reqCtx, opts.Config, opts.Conversation, opts.Tools, opts.HTTPClient, opts.OnToken, opts.OnProgress, opts.StreamDoneCallback, opts.AdaptiveRetry, opts.FirstRequestMetrics, opts.FirstRequestMarked, resp, err, result.FirstResponseAt)
		resp = retryResult.Response
		err = retryResult.Err
		result.FirstResponseAt = retryResult.FirstResponseAt
		result.RetryWaitElapsed = retryResult.RetryWaitElapsed
		result.RetryCount = retryResult.RetryCount
		if retryResult.Cancelled {
			result.Cancelled = true
			return result
		}
	}

	if resp != nil {
		usageStartedAt := time.Now()
		result.Usage = h.recordLLMUsageSnapshot("main_round", resp, result.Conversation)
		if opts.StreamDone {
			result.UsageElapsed = time.Since(usageStartedAt)
			result.UsageDone = true
		}
	}

	guardResult := h.guardAgentLoopLLMResponse(opts.Context, reqCtx, opts.Config, result.Conversation, opts.Tools, opts.HTTPClient, opts.OnToken, opts.StreamDoneCallback, resp, err, opts.UserID, opts.History, opts.UserText, opts.InFlightLifecycle)
	result.Response = guardResult.Response
	result.Err = guardResult.Err
	result.Conversation = guardResult.Conversation
	if guardResult.Usage.HasAny() {
		result.Usage = guardResult.Usage
	}
	result.Exit = guardResult.Exit
	return result
}

func agentLoopLLMCallerFromContext(ctx context.Context) string {
	if trace, ok := llm.RequestTraceFromContext(ctx); ok && strings.TrimSpace(trace.Caller) != "" {
		return strings.TrimSpace(trace.Caller)
	}
	return "agent_loop"
}
