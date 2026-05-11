package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type agentLoopLLMRetryResult struct {
	Response         *llm.Response
	Err              error
	FirstResponseAt  time.Time
	RetryWaitElapsed time.Duration
	RetryCount       int
	Cancelled        bool
}

func (h *IMMessageHandler) retryAgentLoopLLMRequestAfterError(
	ctx *LoopContext,
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	conversation []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	onProgress tool.ProgressCallback,
	streamDoneCallback func(),
	adaptiveRetry *AdaptiveRetry,
	firstRequestMetrics *llmFirstRequestMetrics,
	firstRequestMarked bool,
	resp *llm.Response,
	err error,
	firstResponseAt time.Time,
) agentLoopLLMRetryResult {
	result := agentLoopLLMRetryResult{
		Response:        resp,
		Err:             err,
		FirstResponseAt: firstResponseAt,
	}
	if err == nil {
		return result
	}
	if ctx.IsCancelled() {
		ctx.SetLoopState(LoopStateStopped)
		result.Cancelled = true
		return result
	}
	if adaptiveRetry != nil {
		h.retryAgentLoopLLMRequestAdaptive(ctx, reqCtx, cfg, conversation, tools, httpClient, onToken, onProgress, streamDoneCallback, adaptiveRetry, firstRequestMetrics, firstRequestMarked, &result)
		return result
	}
	h.retryAgentLoopLLMRequestFallback(ctx, reqCtx, cfg, conversation, tools, httpClient, onToken, onProgress, streamDoneCallback, firstRequestMetrics, &result)
	return result
}

func (h *IMMessageHandler) retryAgentLoopLLMRequestAdaptive(
	ctx *LoopContext,
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	conversation []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	onProgress tool.ProgressCallback,
	streamDoneCallback func(),
	adaptiveRetry *AdaptiveRetry,
	firstRequestMetrics *llmFirstRequestMetrics,
	firstRequestMarked bool,
	result *agentLoopLLMRetryResult,
) {
	category := adaptiveRetry.Classify("llm_request", result.Err)
	for retryAttempt := 0; result.Err != nil && !ctx.IsCancelled(); retryAttempt++ {
		decision := adaptiveRetry.Decide("llm_request", category, retryAttempt)
		h.appendTraceEvent(ctx, "trial.retry_decided", "warn", "Adaptive retry decision", truncateTraceText(fmt.Sprintf("llm_request category=%s action=%s attempt=%d", category, decision.Action, decision.Attempt), 220), "", "")
		h.appendTraceEvidence(ctx, traceSourceKindAdaptiveRetry.String(), category.String(), "retry decision", truncateTraceText(firstNonEmptyTraceText(decision.ErrorContext, result.Err.Error()), 400), "", "llm_request")
		if decision.Action != RetryActionRetry {
			return
		}
		adaptiveRetry.RecordFailure("llm_request", category, decision)
		log.Printf("[LLM] AdaptiveRetry: %s error, retry after %v (%d): %v", string(category), decision.Delay, retryAttempt+1, result.Err)
		result.RetryWaitElapsed += decision.Delay
		result.RetryCount++
		reportLLMRetryWait(category == FailureTransient, onProgress, decision.Delay, retryAttempt+1, maxTransientRetries)
		if waitCancelled(ctx, decision.Delay) {
			result.Cancelled = true
			return
		}
		retryMetrics := &llmStreamMetrics{}
		result.Response, result.Err = h.doLLMRequestStream(reqCtx, cfg, conversation, tools, httpClient, onToken, retryMetrics)
		markLLMRetryResponse(result, retryMetrics)
		if !firstRequestMarked || firstRequestMetrics.RequestBuildElapsed == 0 {
			firstRequestMetrics.AddStreamMetrics(retryMetrics)
		} else {
			firstRequestMetrics.AddIdleMetrics(retryMetrics)
		}
		if result.Err == nil && streamDoneCallback != nil {
			streamDoneCallback()
		}
		if result.Err != nil {
			if newCategory := adaptiveRetry.Classify("llm_request", result.Err); newCategory != category {
				category = newCategory
			}
		}
	}
	if ctx.IsCancelled() {
		result.Cancelled = true
	}
}

func (h *IMMessageHandler) retryAgentLoopLLMRequestFallback(
	ctx *LoopContext,
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	conversation []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	onProgress tool.ProgressCallback,
	streamDoneCallback func(),
	firstRequestMetrics *llmFirstRequestMetrics,
	result *agentLoopLLMRetryResult,
) {
	retryKind := classifyLLMRetryError(result.Err)
	if !retryKind.Retryable() || ctx.IsCancelled() {
		return
	}
	isTransient := retryKind.TransientServer()
	retryDelay := 2 * time.Second
	retryMax := 1
	if isTransient {
		retryDelay = 5 * time.Second
		retryMax = 3
	}
	for retryAttempt := 0; retryAttempt < retryMax && result.Err != nil && !ctx.IsCancelled(); retryAttempt++ {
		log.Printf("[LLM] request failed, retry after %v (%d/%d): %v", retryDelay, retryAttempt+1, retryMax, result.Err)
		result.RetryWaitElapsed += retryDelay
		result.RetryCount++
		reportLLMRetryWait(isTransient, onProgress, retryDelay, retryAttempt+1, retryMax)
		if waitCancelled(ctx, retryDelay) {
			result.Cancelled = true
			return
		}
		retryMetrics := &llmStreamMetrics{}
		result.Response, result.Err = h.doLLMRequestStream(reqCtx, cfg, conversation, tools, httpClient, onToken, retryMetrics)
		markLLMRetryResponse(result, retryMetrics)
		firstRequestMetrics.AddStreamMetrics(retryMetrics)
		if result.Err == nil && streamDoneCallback != nil {
			streamDoneCallback()
		}
		if result.Err != nil {
			if newRetryKind := classifyLLMRetryError(result.Err); newRetryKind != retryKind {
				retryKind = newRetryKind
				isTransient = retryKind.TransientServer()
				if isTransient {
					retryDelay = 5 * time.Second
					retryMax = 3
				} else {
					retryDelay = 2 * time.Second
				}
			}
		}
		if isTransient {
			retryDelay *= 2
		}
	}
	if ctx.IsCancelled() {
		result.Cancelled = true
	}
}

func markLLMRetryResponse(result *agentLoopLLMRetryResult, metrics *llmStreamMetrics) {
	if result == nil || result.Err != nil || !result.FirstResponseAt.IsZero() {
		return
	}
	if metrics != nil && !metrics.FirstTokenAt.IsZero() {
		result.FirstResponseAt = metrics.FirstTokenAt
		return
	}
	result.FirstResponseAt = time.Now()
}

func waitCancelled(ctx *LoopContext, delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return ctx.IsCancelled()
	case <-ctx.CancelC:
		ctx.SetLoopState(LoopStateStopped)
		return true
	}
}

func reportLLMRetryWait(isTransient bool, onProgress tool.ProgressCallback, delay time.Duration, attempt, max int) {
	if !isTransient || onProgress == nil {
		return
	}
	onProgress(fmt.Sprintf("API is temporarily unavailable; retrying in %ds (%d/%d)...", int(delay.Seconds()), attempt, max))
}
