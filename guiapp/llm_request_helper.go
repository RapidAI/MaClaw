package guiapp

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// dumpLLMContext reports LLM HTTP failures without persisting prompt/request
// bodies. Those bodies may contain browser observations, screenshots OCR, or
// user secrets, so writing them under ~/.maclaw would leak private context.
func dumpLLMContext(statusCode int, respMsg string, requestBody []byte, tempDir string) error {
	_ = tempDir
	ctxLen := len(requestBody)
	if statusCode != http.StatusInternalServerError {
		return fmt.Errorf("HTTP %d: %s (context %d bytes)", statusCode, respMsg, ctxLen)
	}
	return fmt.Errorf("HTTP %d (context %d bytes, request body not dumped): %s", statusCode, ctxLen, respMsg)
}

// llmSimpleResponse is a minimal response from a simple (non-tool-calling) LLM request.
type llmSimpleResponse struct {
	Content string
}

// simpleLLMRequestOptions describes a control-plane request whose output must
// follow a machine-readable contract.  Ordinary simple requests intentionally
// retain their existing provider behaviour.
type simpleLLMRequestOptions struct {
	ResponseFormat         interface{}
	PreserveResponseFormat bool
	// DetachParentCtx, when set, is the caller's user/turn context for the
	// detached read: the background continuation derives from it instead of
	// the request ctx, so the read survives the scheduling deadline but still
	// aborts on user cancellation (P0-3).
	DetachParentCtx context.Context
	// OnDetachedComplete, when set, delivers the detached read's resolution
	// (nil on success). It fires from the detacher's background goroutine when
	// the read resolves, and from each successful adopter when the result is
	// adopted — so it may run more than once per logical request and receivers
	// must be idempotent (the endpoint-gate success signal is).
	OnDetachedComplete func(err error)
}

const (
	// detachedReadMaxGrace caps how long a detached read may keep its
	// connection alive past the scheduling deadline while waiting for
	// adoption (min(2×budget, 60s, keepalive cap)).
	detachedReadMaxGrace = 60 * time.Second
	// detachedReadKeepaliveCap bounds the orphan connection lifetime so a
	// detached read can never linger past this ceiling even for huge budgets.
	detachedReadKeepaliveCap = 90 * time.Second
)

// detachedReadCompletedRetention keeps a FINISHED detached-read entry in the
// registry briefly after its result published. A late duplicate (e.g. the
// late-verdict goroutine delayed by scheduler load until after the response
// arrived) must still adopt the completed result instead of paying a second
// upstream request — without retention the entry is gone the moment the
// response lands, and the adoption window closes exactly when loaded
// machines are most likely to miss it.
var detachedReadCompletedRetention = 5 * time.Second

func detachedReadGrace(budget time.Duration) time.Duration {
	grace := 2 * budget
	if grace > detachedReadMaxGrace {
		grace = detachedReadMaxGrace
	}
	if grace > detachedReadKeepaliveCap {
		grace = detachedReadKeepaliveCap
	}
	return grace
}

// llmBudgetFiredError marks a failure produced by the caller-side scheduling
// budget (the request reached its deadline and was detached, or the caller's
// own deadline fired). It is not endpoint evidence: the endpoint failure gate
// must never treat it as a network failure (P0-2 budgetFired semantics), and
// corelib late-verdict re-runs rely on it to distinguish "slow" from "broken".
type llmBudgetFiredError struct{ err error }

func (e *llmBudgetFiredError) Error() string { return e.err.Error() }
func (e *llmBudgetFiredError) Unwrap() error { return e.err }

func isLLMBudgetFiredError(err error) bool {
	var bf *llmBudgetFiredError
	return errors.As(err, &bf)
}

// detachedSimpleLLMRead is the adoption promise for a request that hit its
// scheduling budget and kept reading in the background. Waiters (late-verdict
// re-runs, P0-1 reclassifications) block on done; resp/err are published
// before done closes.
type detachedSimpleLLMRead struct {
	done chan struct{}
	resp *llmSimpleResponse
	err  error
}

// detachedSimpleLLMReads is the functional single-flight registry: a retry
// with identical endpoint + payload adopts the in-flight detached read
// instead of double-sending (and double-billing) the same upstream request.
var detachedSimpleLLMReads sync.Map // key string -> *detachedSimpleLLMRead

// detachedSimpleLLMReadKey builds the functional single-flight key. It relies
// on fmt %#v serialization, which is stable ONLY while every message is a
// map[string]string (fmt sorts map keys deterministically). Callers must NOT
// pass message structs containing pointer fields here: pointer addresses make
// the key unstable between the original request and its retry, and adoption
// would then silently never match (degrading to the legacy double-send).
func detachedSimpleLLMReadKey(cfg corelib.MaclawLLMConfig, messages []interface{}, structured bool) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(llmEndpointPrefix(cfg)))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(cfg.WireAPI))))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(cfg.Model))))
	if structured {
		_, _ = h.Write([]byte("|structured"))
	}
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(fmt.Sprintf("%#v", messages)))
	return fmt.Sprintf("%x", h.Sum64)
}

// attachLightweightHubHint marks classify/summary/intent helper calls so Hub
// L1 attributes them as P2 instead of falling through to body heuristics.
func attachLightweightHubHint(cfg corelib.MaclawLLMConfig, task llm.TaskType) corelib.MaclawLLMConfig {
	if !corelib.IsHubManagedLLMEndpoint(cfg.URL, cfg.Model) && !cfg.HubManaged {
		return cfg
	}
	return cfg.WithHubWorkloadHints(string(task), "", "")
}

// doSimpleLLMRequest sends a simple chat completion request (no tool calling)
// to the configured LLM, supporting both OpenAI and Anthropic protocols.
// It returns the text content of the assistant's reply.
func doSimpleLLMRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*llmSimpleResponse, error) {
	return doSimpleLLMRequestWithOptions(ctx, cfg, messages, client, timeout, simpleLLMRequestOptions{})
}

func doSimpleLLMRequestWithOptions(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, opts simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	ctx = llm.WithRequestTraceIfMissing(ctx, "simple_llm")
	if cfg.IsResponsesWebSocket() {
		// WS is a long-lived message stream: there is no "finish reading the
		// body" point, so detached continuation does not apply. Keep the
		// legacy to-point-of-budget behaviour; the late-verdict re-send is
		// the fallback for slow WS endpoints (P0-3 exception).
		return doSimpleLLMRequestWithOptionsLegacy(ctx, cfg, messages, client, timeout, opts)
	}
	return doSimpleLLMRequestDetachable(ctx, cfg, messages, client, timeout, opts)
}

// doSimpleLLMRequestWithOptionsLegacy keeps the pre-P0-3 to-point-of-budget
// behaviour for wire variants that cannot detach (Responses-WebSocket): the
// read is torn down at the scheduling budget and the late-verdict re-send
// remains the fallback. The budget is enforced HERE, not inside the variant:
// the variant's internal timer used to surface a bare deadline-exceeded while
// the caller's ctx was still live, which observe call sites could not
// distinguish from a transport timeout — and the endpoint gate recorded it as
// a network failure and banned the endpoint (2026-08-25 incident pattern). A
// budget fire now always returns llmBudgetFiredError (budgetFired semantics,
// never endpoint evidence), without detached continuation.
func doSimpleLLMRequestWithOptionsLegacy(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, opts simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)

	ch := make(chan simpleLLMVariantResult, 1)
	go func() {
		var r simpleLLMVariantResult
		// timeout=0: no internal variant deadline — the outer budget timer
		// below is the single authority on the scheduling budget.
		if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
			r.resp, r.err = doSimpleResponsesRequest(scheduledCtx, cfg, messages, client, 0, opts)
		} else if cfg.Protocol == "anthropic" {
			r.resp, r.err = doSimpleAnthropicRequest(scheduledCtx, cfg, messages, client, 0, opts)
		} else {
			r.resp, r.err = doSimpleOpenAIRequest(scheduledCtx, cfg, messages, client, 0, opts)
		}
		ch <- r
	}()

	var budgetC <-chan time.Time
	var budgetTimer *time.Timer
	if timeout > 0 {
		budgetTimer = time.NewTimer(timeout)
		budgetC = budgetTimer.C
		defer budgetTimer.Stop()
	}

	select {
	case r := <-ch:
		scheduledCancel()
		lease.Release()
		globalLLMScheduler.ObserveResult(trace, r.err)
		return r.resp, r.err
	case <-ctx.Done():
		return finishLegacyBudgetFired(lease, scheduledCancel, timeout, ctx.Err())
	case <-budgetC:
		return finishLegacyBudgetFired(lease, scheduledCancel, timeout, context.DeadlineExceeded)
	}
}

// finishLegacyBudgetFired tears the in-flight read down (this variant has no
// detached continuation) and reports the budget-fired error semantics.
func finishLegacyBudgetFired(lease *llmSchedulerLease, scheduledCancel context.CancelFunc, budget time.Duration, cause error) (*llmSimpleResponse, error) {
	if cause == nil {
		cause = context.DeadlineExceeded
	}
	scheduledCancel()
	lease.Release()
	return nil, &llmBudgetFiredError{err: fmt.Errorf("simple LLM request reached its scheduling budget after %s: %w", budget, cause)}
}

type simpleLLMVariantResult struct {
	resp *llmSimpleResponse
	err  error
}

// doSimpleLLMRequestDetachable implements the P0-3 detached read: when the
// scheduling budget fires, the connection is NOT torn down. The scheduler
// lease is released (freeing the foreground slot and dropping the scheduler
// cancel handle) and the in-flight request keeps reading in the background
// for the grace window. A retry carrying the identical endpoint + payload
// (late-verdict re-run, P0-1 reclassification) adopts the detached result
// instead of double-sending; a user cancellation still aborts the read via
// the parent context.
func doSimpleLLMRequestDetachable(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, opts simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	key := detachedSimpleLLMReadKey(cfg, messages, opts.ResponseFormat != nil)
	if existing, ok := detachedSimpleLLMReads.Load(key); ok {
		entry := existing.(*detachedSimpleLLMRead)
		log.Printf("[LLM] detached read adoption wait key=%s", key)
		select {
		case <-entry.done:
			if entry.err == nil && entry.resp != nil {
				log.Printf("[LLM] detached read adopted key=%s", key)
				// Adoption is a success observe of the same endpoint and
				// payload: deliver the positive health signal for this
				// category too, not just the detacher's own resolution.
				if opts.OnDetachedComplete != nil {
					opts.OnDetachedComplete(nil)
				}
				return entry.resp, nil
			}
			log.Printf("[LLM] detached read resolved with error; falling through to a fresh request key=%s err=%v", key, entry.err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return doSimpleLLMRequestForeground(ctx, cfg, messages, client, timeout, opts, key)
}

// doSimpleLLMRequestForeground is the shared send tail: acquire the scheduler
// lease, run the protocol variant, enforce the budget, and detach at the
// deadline. Both the first attempt and fresh-request fall-throughs (after a
// failed adoption wait) run through here, so the lease/context lifecycle has
// exactly one owner.
func doSimpleLLMRequestForeground(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, opts simpleLLMRequestOptions, key string) (*llmSimpleResponse, error) {
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	// detachedCtx derives from the caller's user/turn context (or the request
	// ctx when none was supplied), NEVER from an internal budget ctx: it must
	// outlive the scheduling deadline yet still die on user cancellation.
	detachParent := opts.DetachParentCtx
	if detachParent == nil {
		detachParent = ctx
	}
	detachedCtx, detachCancel := context.WithCancel(detachParent)
	scheduledCtx, scheduledCancel := context.WithCancel(detachedCtx)
	lease.SetCancel(scheduledCancel)

	ch := make(chan simpleLLMVariantResult, 1)
	go func() {
		var r simpleLLMVariantResult
		switch {
		case cfg.IsResponsesAPI():
			// timeout=0: the variant must not install its own deadline — the
			// transport context has to stay alive past the budget for the
			// detached read. The helper enforces the budget below.
			r.resp, r.err = doSimpleResponsesRequest(scheduledCtx, cfg, messages, client, 0, opts)
		case cfg.Protocol == "anthropic":
			r.resp, r.err = doSimpleAnthropicRequest(scheduledCtx, cfg, messages, client, 0, opts)
		default:
			r.resp, r.err = doSimpleOpenAIRequest(scheduledCtx, cfg, messages, client, 0, opts)
		}
		ch <- r
	}()

	var budgetC <-chan time.Time
	var budgetTimer *time.Timer
	if timeout > 0 {
		budgetTimer = time.NewTimer(timeout)
		budgetC = budgetTimer.C
		defer budgetTimer.Stop()
	}

	select {
	case r := <-ch:
		// Completed within the budget: ordinary success/error path.
		scheduledCancel()
		detachCancel()
		lease.Release()
		globalLLMScheduler.ObserveResult(trace, r.err)
		return r.resp, r.err
	case <-ctx.Done():
		return beginDetachedSimpleLLMRead(ctx, cfg, messages, client, key, ch, lease, trace, scheduledCancel, detachCancel, timeout, ctx.Err(), opts)
	case <-budgetC:
		return beginDetachedSimpleLLMRead(ctx, cfg, messages, client, key, ch, lease, trace, scheduledCancel, detachCancel, timeout, context.DeadlineExceeded, opts)
	}
}

// beginDetachedSimpleLLMRead switches an in-flight request to background
// continuation: the caller is handed a budget-fired error while the request
// keeps reading. The scheduler lease is released first — the detached read
// must not occupy a foreground slot or stay killable by scheduler preemption.
// The read resolves into the shared registry so a duplicate retry adopts it
// (upstream request count stays 1); the grace window bounds how long the
// orphan connection may live.
func beginDetachedSimpleLLMRead(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, key string, ch chan simpleLLMVariantResult, lease *llmSchedulerLease, trace llm.RequestTrace, scheduledCancel, detachCancel context.CancelFunc, budget time.Duration, cause error, opts simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	if cause == nil {
		cause = context.DeadlineExceeded
	}
	grace := detachedReadGrace(budget)
	entry := &detachedSimpleLLMRead{done: make(chan struct{})}
	actual, loaded := detachedSimpleLLMReads.LoadOrStore(key, entry)
	if loaded {
		// A concurrent identical request detached first and owns the registry
		// slot. We lost the race: our duplicate read will never be consumed,
		// so tear it down immediately and free the scheduler slot instead of
		// holding a foreground slot for the whole wait. Then wait on the
		// existing promise with OUR OWN ctx as the bound — its grace window
		// started at the other request's detach time and may far outlive our
		// budget, so a synchronous caller must never block past its own
		// cancellation.
		existing := actual.(*detachedSimpleLLMRead)
		scheduledCancel()
		detachCancel()
		lease.Release()
		log.Printf("[LLM] detached read: concurrent duplicate detached first; waiting on existing entry key=%s", key)
		select {
		case <-existing.done:
			if existing.err == nil && existing.resp != nil {
				// Adopted: deliver the same positive health signal as the
				// top-level adoption path.
				if opts.OnDetachedComplete != nil {
					opts.OnDetachedComplete(nil)
				}
				return existing.resp, nil
			}
			// The existing read failed (e.g. its grace expired). Fall through
			// to a fresh request — the same rescue semantics as the top-level
			// adoption path — instead of propagating the failure, which would
			// let the late-verdict goroutine exit without re-sending.
			log.Printf("[LLM] existing detached read failed; falling through to a fresh request key=%s err=%v", key, existing.err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return doSimpleLLMRequestForeground(ctx, cfg, messages, client, budget, opts, key)
	}
	lease.Release()
	log.Printf("[LLM] detached read: scheduling budget fired; continuing in background key=%s grace=%s", key, grace)

	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		var r simpleLLMVariantResult
		select {
		case r = <-ch:
		case <-timer.C:
			// Grace expired with no adoption window left: abort the orphan
			// read and resolve waiters with an error so they fall through
			// to a fresh request (the legacy late-verdict re-send path).
			detachCancel()
			r = simpleLLMVariantResult{err: &llmBudgetFiredError{err: fmt.Errorf("detached read grace window %s expired: %w", grace, context.DeadlineExceeded)}}
		}
		entry.resp, entry.err = r.resp, r.err
		globalLLMScheduler.ObserveResult(trace, r.err)
		if opts.OnDetachedComplete != nil {
			opts.OnDetachedComplete(r.err)
		}
		// Publish the result before removing the registry entry so a lookup
		// that raced the removal still adopts instead of double-sending.
		// CompareAndDelete guards against deleting a NEWER live entry that a
		// concurrent same-key request registered after us. The deletion is
		// delayed by detachedReadCompletedRetention so a late duplicate that
		// missed the in-flight window still adopts the finished result.
		close(entry.done)
		scheduledCancel()
		detachCancel()
		time.AfterFunc(detachedReadCompletedRetention, func() {
			detachedSimpleLLMReads.CompareAndDelete(key, entry)
		})
	}()

	return nil, &llmBudgetFiredError{err: fmt.Errorf("simple LLM request reached its scheduling budget after %s and was detached: %w", budget, cause)}
}

func doSimpleOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, requestOpts ...simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	opts := simpleLLMRequestOptions{}
	if len(requestOpts) > 0 {
		opts = requestOpts[0]
	}
	// Preserve the mature simple-request path for ordinary callers.  Only the
	// control-plane caller below opts into a response contract, so this change
	// cannot silently alter compatibility retries for summaries, OCR, etc.
	if opts.ResponseFormat == nil {
		parsed, err := llm.DoOpenAIRequest(ctx, cfg, messages, nil, client)
		if err != nil {
			var httpErr *llm.HTTPStatusError
			if errors.As(err, &httpErr) && httpErr != nil && len(httpErr.Body) > 0 {
				body := strings.TrimSpace(string(httpErr.Body))
				if len(body) > 512 {
					body = body[:512]
				}
				return nil, fmt.Errorf("%w %s", err, body)
			}
			msg := err.Error()
			if strings.HasPrefix(msg, "HTTP 500:") {
				endpoint, data, buildErr := llm.BuildOpenAIChatRequestData(cfg, messages, llm.OpenAIChatRequestOptions{Stream: false})
				if buildErr != nil {
					return nil, err
				}
				_ = endpoint
				return nil, dumpLLMContext(http.StatusInternalServerError, "llm request failed", data, "")
			}
			return nil, err
		}
		if len(parsed.Choices) == 0 {
			return nil, fmt.Errorf("no response from model")
		}
		text := parsed.Choices[0].Message.Content
		if text == "" {
			text = parsed.Choices[0].Message.ReasoningContent
		}
		return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
	}

	req, data, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:                 false,
		ResponseFormat:         opts.ResponseFormat,
		PreserveResponseFormat: opts.PreserveResponseFormat,
	})
	if err != nil {
		return nil, err
	}
	traceFields := llm.RequestTraceLogFields(req.Context())
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM] POST %s model=%s configured_model=%s protocol=%s simple=true structured=%t %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, opts.ResponseFormat != nil, traceFields)
	startedAt := time.Now()
	if client == nil {
		client = http.DefaultClient
	}
	httpResp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s simple=true structured=%t status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, opts.ResponseFormat != nil, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer httpResp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 256*1024))
	if readErr != nil {
		return nil, readErr
	}
	log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s simple=true structured=%t status=%d elapsed=%s body_len=%d %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, opts.ResponseFormat != nil, httpResp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusInternalServerError {
			return nil, dumpLLMContext(httpResp.StatusCode, "llm request failed", data, "")
		}
		return nil, &llm.HTTPStatusError{StatusCode: httpResp.StatusCode, Body: append([]byte(nil), body...)}
	}
	parsed, err := llm.ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
}

func doSimpleResponsesRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, requestOpts ...simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	opts := simpleLLMRequestOptions{}
	if len(requestOpts) > 0 {
		opts = requestOpts[0]
	}
	if client == nil {
		client = http.DefaultClient
	}

	requestBody := map[string]interface{}(nil)
	if opts.ResponseFormat != nil {
		requestBody = map[string]interface{}{"response_format": opts.ResponseFormat}
	}
	req, data, endpoint, err := llm.NewResponsesAPIRequest(ctx, cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream:                 false,
		ExtraBody:              requestBody,
		PreserveResponseFormat: opts.PreserveResponseFormat,
	})
	if err != nil {
		return nil, err
	}
	traceFields := llm.RequestTraceLogFields(req.Context())
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM] POST %s model=%s configured_model=%s wire_api=responses simple=true %s", endpoint, upstreamModel, cfg.Model, traceFields)

	startedAt := time.Now()
	httpResp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses simple=true status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses simple=true status=%d elapsed=%s body_len=%d %s", endpoint, upstreamModel, cfg.Model, httpResp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
		if httpResp.StatusCode == http.StatusInternalServerError {
			return nil, dumpLLMContext(httpResp.StatusCode, classifyResponsesAPIHTTPError(httpResp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName), data, "")
		}
		return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(httpResp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName))
	}

	parsed, parseErr := llm.ParseNonStreamResponsesAPIResponse(httpResp)
	log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses simple=true status=%d elapsed=%s parse_err=%v %s", endpoint, upstreamModel, cfg.Model, httpResp.StatusCode, time.Since(startedAt).Round(time.Millisecond), parseErr, traceFields)
	if parseErr != nil {
		return nil, parseErr
	}
	if parsed == nil || len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	if text == "" {
		return nil, fmt.Errorf("no text response from model")
	}
	return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
}

func doSimpleAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, requestOpts ...simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if len(requestOpts) > 0 && requestOpts[0].ResponseFormat != nil {
		// Anthropic's Messages API has no response_format analogue.  The tree
		// parser remains the validator and reports a protocol failure if prose
		// is returned; this branch is intentionally not a lexical fallback.
		log.Printf("[LLM] structured simple request uses Anthropic prompt contract only")
	}

	traceFields := llm.RequestTraceLogFields(ctx)
	upstreamModel := cfg.UpstreamModel()
	endpoint := corelib.AnthropicMessagesEndpoint(cfg.URL)
	log.Printf("[LLM] POST %s model=%s configured_model=%s protocol=anthropic simple=true sdk=anthropic-sdk-go %s", endpoint, upstreamModel, cfg.Model, traceFields)
	startedAt := time.Now()
	resp, err := llm.DoAnthropicRequest(ctx, cfg, messages, nil, client)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s protocol=anthropic simple=true status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, err
	}
	log.Printf("[LLM] done %s model=%s configured_model=%s protocol=anthropic simple=true status=200 elapsed=%s %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), traceFields)
	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		text = resp.Choices[0].Message.ReasoningContent
	}
	if text == "" {
		return nil, fmt.Errorf("no text response from model")
	}
	return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
}
