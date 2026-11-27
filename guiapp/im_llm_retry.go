package guiapp

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
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
	// Thinking or answer text already reached the host. Another stream would
	// duplicate the visible turn and can wipe this prefix if the retry fails.
	// A reasoning-only outage, a truncated body, or an opened tool call with
	// no arguments has not produced an answer, so that failure can be resent.
	// Answer text and a call that already has arguments still stop the retry.
	if llmResponseBlocksStreamRetry(resp, err) {
		return result
	}
	if adaptiveRetry != nil {
		h.retryAgentLoopLLMRequestAdaptive(ctx, reqCtx, cfg, conversation, tools, httpClient, onToken, onProgress, streamDoneCallback, adaptiveRetry, firstRequestMetrics, firstRequestMarked, &result)
		return result
	}
	h.retryAgentLoopLLMRequestFallback(ctx, reqCtx, cfg, conversation, tools, httpClient, onToken, onProgress, streamDoneCallback, firstRequestMetrics, &result)
	return result
}

// llmResponseBlocksStreamRetry reports that this partial must not be resent.
// Ordinary visible output, including reasoning, blocks. A temporary Responses
// frame, a truncated body, an idle stall, or a timeout blocks only when an
// answer or a tool call with arguments is already on the response.
func llmResponseBlocksStreamRetry(resp *llm.Response, err error) bool {
	if !llmResponseHasVisibleOutput(resp) {
		return false
	}
	if llm.ResponseHasCommittedAnswer(resp) {
		return true
	}
	return !llm.UncommittedStreamCutCanRetry(err)
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
	didAuthRefresh := false
	for retryAttempt := 0; result.Err != nil && !ctx.IsCancelled(); retryAttempt++ {
		// If the request context is already cancelled (e.g. by RequestReplan),
		// retrying is pointless — the next request will immediately fail with
		// the same context.Canceled error. Exit early to avoid wasting the
		// retry delay (1s+) on a deliberately-cancelled operation.
		if reqCtx.Err() != nil {
			return
		}
		decision := adaptiveRetry.Decide("llm_request", category, retryAttempt)
		decision.ProviderName = strings.TrimSpace(cfg.ProviderName)
		decision.Model = strings.TrimSpace(cfg.Model)
		decision.WireAPI = strings.TrimSpace(cfg.WireAPI)
		h.appendTraceEvent(ctx, "trial.retry_decided", "warn", "Adaptive retry decision", truncateTraceText(fmt.Sprintf("llm_request category=%s action=%s attempt=%d", category, decision.Action, decision.Attempt), 220), "", "")
		h.appendTraceEvidence(ctx, traceSourceKindAdaptiveRetry.String(), category.String(), "retry decision", truncateTraceText(firstNonEmptyTraceText(decision.ErrorContext, result.Err.Error()), 400), "", "llm_request")
		if decision.Action != RetryActionRetry {
			return
		}
		result.RetryCount++
		cfg, rotated := h.maybeRefreshLLMAuthForRetry(reqCtx, cfg, result.Err, &didAuthRefresh)
		failovered := false
		if !rotated {
			before := cfg
			if next, ok := h.workerRoleFailoverLLMConfig(cfg, result.Err); ok {
				cfg = next
				rotated = true
				failovered = true
				reportLLMFailoverInFlight(onProgress, retryAttempt+1, before, cfg)
			}
		}
		if rotated {
			if failovered {
				log.Printf("[LLM] AdaptiveRetry: %s endpoint unavailable, switched to %q (%d): %v", cfg.Profile, cfg.Model, retryAttempt+1, result.Err)
			} else {
				log.Printf("[LLM] AdaptiveRetry: token-validation error, retrying immediately after credential refresh (%d): %v", retryAttempt+1, result.Err)
				reportLLMAuthRefresh(onProgress)
				reportLLMRetryInFlight(onProgress, retryAttempt+1)
			}
		} else {
			adaptiveRetry.RecordFailure("llm_request", category, decision)
			log.Printf("[LLM] AdaptiveRetry: %s error, retry after %v (%d): %v", string(category), decision.Delay, retryAttempt+1, result.Err)
			result.RetryWaitElapsed += decision.Delay
			announced := reportLLMRetryWait(category == FailureTransient, onProgress, decision.Delay, retryAttempt+1, maxTransientRetries)
			if waitCancelled(ctx, decision.Delay) {
				result.Cancelled = true
				return
			}
			if announced {
				reportLLMRetryInFlight(onProgress, retryAttempt+1)
			}
		}
		retryMetrics := &llmStreamMetrics{}
		result.Response, result.Err = h.doLLMRequestStream(reqCtx, cfg, llmEndpointCategoryMainStream, conversation, tools, httpClient, onToken, retryMetrics)
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
	if !retryKind.Retryable() || ctx.IsCancelled() || reqCtx.Err() != nil {
		return
	}
	isTransient := retryKind.TransientServer()
	retryDelay := 2 * time.Second
	retryMax := 1
	if isTransient {
		retryDelay = baseTransientDelay
		retryMax = maxTransientRetries
	}
	didAuthRefresh := false
	for retryAttempt := 0; retryAttempt < retryMax && result.Err != nil && !ctx.IsCancelled(); retryAttempt++ {
		result.RetryCount++
		cfg, rotated := h.maybeRefreshLLMAuthForRetry(reqCtx, cfg, result.Err, &didAuthRefresh)
		failovered := false
		if !rotated {
			before := cfg
			if next, ok := h.workerRoleFailoverLLMConfig(cfg, result.Err); ok {
				cfg = next
				rotated = true
				failovered = true
				reportLLMFailoverInFlight(onProgress, retryAttempt+1, before, cfg)
			}
		}
		if rotated {
			if failovered {
				log.Printf("[LLM] %s endpoint unavailable, switched to %q (%d/%d): %v", cfg.Profile, cfg.Model, retryAttempt+1, retryMax, result.Err)
			} else {
				log.Printf("[LLM] token-validation error, retrying immediately after credential refresh (%d/%d): %v", retryAttempt+1, retryMax, result.Err)
				reportLLMAuthRefresh(onProgress)
				reportLLMRetryInFlight(onProgress, retryAttempt+1)
			}
		} else {
			log.Printf("[LLM] request failed, retry after %v (%d/%d): %v", retryDelay, retryAttempt+1, retryMax, result.Err)
			result.RetryWaitElapsed += retryDelay
			announced := reportLLMRetryWait(isTransient, onProgress, retryDelay, retryAttempt+1, retryMax)
			if waitCancelled(ctx, retryDelay) {
				result.Cancelled = true
				return
			}
			if announced {
				reportLLMRetryInFlight(onProgress, retryAttempt+1)
			}
		}
		retryMetrics := &llmStreamMetrics{}
		result.Response, result.Err = h.doLLMRequestStream(reqCtx, cfg, llmEndpointCategoryMainStream, conversation, tools, httpClient, onToken, retryMetrics)
		markLLMRetryResponse(result, retryMetrics)
		firstRequestMetrics.AddStreamMetrics(retryMetrics)
		if result.Err == nil && streamDoneCallback != nil {
			streamDoneCallback()
		}
		if result.Err != nil {
			if newRetryKind := classifyLLMRetryError(result.Err); newRetryKind != retryKind {
				retryKind = newRetryKind
				isTransient = retryKind.TransientServer()
				if !retryKind.Retryable() {
					// Error category changed to non-retryable (e.g. permission, args).
					// Stop retrying immediately.
					return
				}
				if isTransient {
					retryDelay = baseTransientDelay
					retryMax = maxTransientRetries
				} else {
					retryDelay = 2 * time.Second
				}
			}
		}
		if isTransient {
			retryDelay *= 2
			if retryDelay > maxTransientDelay {
				retryDelay = maxTransientDelay
			}
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

func reportLLMRetryWait(isTransient bool, onProgress tool.ProgressCallback, delay time.Duration, attempt, max int) bool {
	if !isTransient || onProgress == nil {
		return false
	}
	onProgress(agent.LLMRetryWaitStatus(attempt, max, delay, false))
	return true
}

func reportLLMRetryInFlight(onProgress tool.ProgressCallback, attempt int) {
	if onProgress == nil {
		return
	}
	onProgress(agent.LLMRetryInFlightStatus(attempt))
}

// reportLLMFailoverInFlight keeps the named switch on the retry row. Emitting
// a generic in-flight line after it would overwrite the only explanation of
// why the model changed.
func reportLLMFailoverInFlight(onProgress tool.ProgressCallback, attempt int, from, to corelib.MaclawLLMConfig) {
	if status, ok := agent.LLMRetrySwitchedInFlightStatus(attempt, from, to); ok {
		if onProgress != nil {
			onProgress(status)
		}
		return
	}
	reportLLMRetryInFlight(onProgress, attempt)
}

func reportLLMAuthRefresh(onProgress tool.ProgressCallback) {
	if onProgress == nil {
		return
	}
	onProgress(agent.LLMRetryAuthRefreshStatus())
}

func (h *IMMessageHandler) maybeRefreshLLMAuthForRetry(ctx context.Context, cfg corelib.MaclawLLMConfig, err error, did *bool) (corelib.MaclawLLMConfig, bool) {
	if did == nil || *did || !llm.IsTransientTokenValidationError(err) {
		return cfg, false
	}
	*did = true
	prev := cfg
	cfg = h.refreshLLMConfigAfterTokenErrorCtx(ctx, cfg)
	return cfg, llmAuthKeyRotated(prev, cfg)
}
