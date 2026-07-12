package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type llmRequestPriority string

const (
	llmPriorityForeground llmRequestPriority = "foreground"
	llmPriorityBackground llmRequestPriority = "background"
)

const (
	// defaultForegroundLLMConcurrency is sized to support multiple concurrent
	// foreground agent loops (one per desktop tab) without serialization. Each
	// tab may issue up to 2 LLM requests per agent loop iteration (main LLM +
	// lightweight intent/task-context call). 6 slots covers ~3 concurrent tabs
	// with headroom while Hub-side user rate-limit queue absorbs short bursts.
	// Under API pressure (429), ObserveResult drops to degradedForeground/background
	// for llmSchedulerDegradedCooldown so the client self-paces while Hub's queue drains.
	defaultForegroundLLMConcurrency  = 6
	defaultBackgroundLLMConcurrency  = 2
	degradedForegroundLLMConcurrency = 2
	degradedBackgroundLLMConcurrency = 0
	llmSchedulerDegradedCooldown     = 45 * time.Second
	llmSchedulerWaitLogInterval      = 5 * time.Second
)

type llmConcurrencyScheduler struct {
	mu             sync.Mutex
	activeFG       int
	activeBG       int
	degradedUntil  time.Time
	bgPausedUntil  time.Time
	recoveryTimer  *time.Timer
	waitSeq        int64
	queue          []*llmSchedulerWaiter
	activeBGLease  map[*llmSchedulerLease]struct{}
	foregroundWork func() int64
	now            func() time.Time
}

type llmSchedulerWaiter struct {
	id       int64
	priority llmRequestPriority
	trace    llm.RequestTrace
	ready    chan struct{}
	lease    *llmSchedulerLease
	granted  bool
	enqueued time.Time
}

type llmSchedulerLease struct {
	scheduler *llmConcurrencyScheduler
	priority  llmRequestPriority
	trace     llm.RequestTrace
	mu        sync.Mutex
	cancel    context.CancelFunc
	released  bool
}

var globalLLMScheduler = newLLMConcurrencyScheduler()

func newLLMConcurrencyScheduler() *llmConcurrencyScheduler {
	return &llmConcurrencyScheduler{now: time.Now, foregroundWork: activeForegroundAgentWork}
}

func (s *llmConcurrencyScheduler) Acquire(ctx context.Context, trace llm.RequestTrace) (*llmSchedulerLease, error) {
	if s == nil {
		return &llmSchedulerLease{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	priority := classifyLLMRequestPriority(trace)
	waiter := &llmSchedulerWaiter{priority: priority, trace: trace, ready: make(chan struct{}), enqueued: s.now()}

	s.mu.Lock()
	s.waitSeq++
	waiter.id = s.waitSeq
	s.queue = append(s.queue, waiter)
	if priority == llmPriorityForeground {
		s.cancelActiveBackgroundLocked("foreground-enqueued")
	}
	fgLimit, bgLimit, fgDegraded, bgPaused := s.limitsLocked()
	queueDepth := len(s.queue)
	if priority == llmPriorityForeground && strings.TrimSpace(trace.OwnerID) == "" {
		note := "foreground LLM request has no owner trace"
		if strings.EqualFold(strings.TrimSpace(trace.Caller), "simple_llm") {
			note = "unowned simple_llm treated as foreground to avoid foreground self-block"
		}
		log.Printf("[llm-scheduler] trace_gap caller=%q owner=%q request_id=%q loop=%q iteration=%d priority=%s note=%q", trace.Caller, trace.OwnerID, trace.RequestID, trace.LoopID, trace.Iteration, priority, note)
	}
	log.Printf("[llm-scheduler] enqueue priority=%s caller=%q owner=%q request_id=%q loop=%q iteration=%d queue_depth=%d active_fg=%d active_bg=%d limit_fg=%d limit_bg=%d mode=%s", priority, trace.Caller, trace.OwnerID, trace.RequestID, trace.LoopID, trace.Iteration, queueDepth, s.activeFG, s.activeBG, fgLimit, bgLimit, llmSchedulerMode(fgDegraded, bgPaused))
	s.dispatchLocked()
	s.mu.Unlock()

	waitLogTicker := time.NewTicker(llmSchedulerWaitLogInterval)
	defer waitLogTicker.Stop()
	for {
		select {
		case <-waiter.ready:
			return waiter.lease, nil
		case <-waitLogTicker.C:
			s.logWaitStill(waiter)
		case <-ctx.Done():
			s.mu.Lock()
			removed := s.removeWaiterLocked(waiter)
			if removed {
				s.dispatchLocked()
			}
			s.mu.Unlock()
			if !removed {
				<-waiter.ready
				return waiter.lease, nil
			}
			log.Printf("[llm-scheduler] wait_cancel priority=%s caller=%q owner=%q request_id=%q loop=%q iteration=%d waited=%s err=%v", priority, trace.Caller, trace.OwnerID, trace.RequestID, trace.LoopID, trace.Iteration, s.now().Sub(waiter.enqueued).Round(time.Millisecond), ctx.Err())
			return nil, ctx.Err()
		}
	}
}

func (s *llmConcurrencyScheduler) logWaitStill(waiter *llmSchedulerWaiter) {
	if s == nil || waiter == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if waiter.granted {
		return
	}
	position := 0
	for i, queued := range s.queue {
		if queued == waiter {
			position = i + 1
			break
		}
	}
	if position == 0 {
		return
	}
	fgLimit, bgLimit, fgDegraded, bgPaused := s.limitsLocked()
	foregroundWork := s.foregroundWorkLocked()
	owners := ""
	if foregroundWork > 0 {
		owners = foregroundAgentOwnersSnapshot()
	}
	log.Printf("[llm-scheduler] wait_still priority=%s caller=%q owner=%q request_id=%q loop=%q iteration=%d waited=%s queue_pos=%d queue_depth=%d active_fg=%d active_bg=%d foreground_work=%d foreground_owners=%q limit_fg=%d limit_bg=%d mode=%s", waiter.priority, waiter.trace.Caller, waiter.trace.OwnerID, waiter.trace.RequestID, waiter.trace.LoopID, waiter.trace.Iteration, s.now().Sub(waiter.enqueued).Round(time.Millisecond), position, len(s.queue), s.activeFG, s.activeBG, foregroundWork, owners, fgLimit, bgLimit, llmSchedulerMode(fgDegraded, bgPaused))
}

func (s *llmConcurrencyScheduler) Dispatch() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.dispatchLocked()
	s.mu.Unlock()
}

// IsDegraded reports whether the scheduler is in fg-degraded mode (foreground
// slot limit is reduced due to provider pressure). Used by callers that want
// to preempt background LLM work only when foreground slots are scarce.
//
// Note: bgPaused (background slots reduced due to background-only 429) is NOT
// considered degraded from the foreground perspective — fg slots remain at the
// default 4, so there is no need to preempt background work.
func (s *llmConcurrencyScheduler) IsForegroundDegraded() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, fgDegraded, _ := s.limitsLocked()
	return fgDegraded
}

func (s *llmConcurrencyScheduler) CancelActiveBackground(reason string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	cancelled := s.cancelActiveBackgroundLocked(reason)
	s.mu.Unlock()
	return cancelled
}

func (l *llmSchedulerLease) Release() {
	if l == nil || l.scheduler == nil {
		return
	}
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return
	}
	l.released = true
	l.cancel = nil
	l.mu.Unlock()
	s := l.scheduler
	s.mu.Lock()
	if l.priority == llmPriorityForeground {
		if s.activeFG > 0 {
			s.activeFG--
		}
	} else {
		delete(s.activeBGLease, l)
		if s.activeBG > 0 {
			s.activeBG--
		}
	}
	activeFG, activeBG := s.activeFG, s.activeBG
	_, _, fgDegraded, bgPaused := s.limitsLocked()
	s.dispatchLocked()
	s.mu.Unlock()
	log.Printf("[llm-scheduler] released priority=%s caller=%q owner=%q request_id=%q loop=%q iteration=%d active_fg=%d active_bg=%d mode=%s", l.priority, l.trace.Caller, l.trace.OwnerID, l.trace.RequestID, l.trace.LoopID, l.trace.Iteration, activeFG, activeBG, llmSchedulerMode(fgDegraded, bgPaused))
}

func (l *llmSchedulerLease) SetCancel(cancel context.CancelFunc) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		if cancel != nil {
			cancel()
		}
		return
	}
	l.cancel = cancel
}

func (s *llmConcurrencyScheduler) ObserveResult(trace llm.RequestTrace, err error) {
	if s == nil || err == nil || !isProviderPressureLLMError(err) {
		return
	}
	now := s.now()
	until := now.Add(llmSchedulerDegradedCooldown)
	priority := classifyLLMRequestPriority(trace)
	throttleForeground := isForegroundLLMThrottleError(err)
	s.mu.Lock()
	changed := false
	if priority == llmPriorityBackground || !throttleForeground {
		if until.After(s.bgPausedUntil) {
			s.bgPausedUntil = until
			changed = true
		}
	} else if until.After(s.degradedUntil) {
		s.degradedUntil = until
		changed = true
	}
	if changed {
		s.scheduleRecoveryWakeLocked(now)
	}
	fgDegradedFor := s.degradedUntil.Sub(now)
	bgPausedFor := s.bgPausedUntil.Sub(now)
	s.dispatchLocked()
	s.mu.Unlock()
	log.Printf("[llm-scheduler] pressure priority=%s caller=%q owner=%q request_id=%q loop=%q iteration=%d throttle_foreground=%v fg_cooldown=%s bg_pause=%s err=%v", priority, trace.Caller, trace.OwnerID, trace.RequestID, trace.LoopID, trace.Iteration, throttleForeground, nonNegativeDuration(fgDegradedFor).Round(time.Second), nonNegativeDuration(bgPausedFor).Round(time.Second), err)
}

func (s *llmConcurrencyScheduler) dispatchLocked() {
	for {
		idx := s.nextDispatchIndexLocked()
		if idx < 0 {
			return
		}
		waiter := s.queue[idx]
		s.queue = append(s.queue[:idx], s.queue[idx+1:]...)
		waiter.granted = true
		if waiter.priority == llmPriorityForeground {
			s.activeFG++
		} else {
			s.activeBG++
		}
		lease := &llmSchedulerLease{scheduler: s, priority: waiter.priority, trace: waiter.trace}
		waiter.lease = lease
		if waiter.priority == llmPriorityBackground {
			if s.activeBGLease == nil {
				s.activeBGLease = make(map[*llmSchedulerLease]struct{})
			}
			s.activeBGLease[lease] = struct{}{}
		}
		fgLimit, bgLimit, fgDegraded, bgPaused := s.limitsLocked()
		log.Printf("[llm-scheduler] acquired priority=%s caller=%q owner=%q request_id=%q loop=%q iteration=%d waited=%s queue_depth=%d active_fg=%d active_bg=%d limit_fg=%d limit_bg=%d mode=%s", waiter.priority, waiter.trace.Caller, waiter.trace.OwnerID, waiter.trace.RequestID, waiter.trace.LoopID, waiter.trace.Iteration, s.now().Sub(waiter.enqueued).Round(time.Millisecond), len(s.queue), s.activeFG, s.activeBG, fgLimit, bgLimit, llmSchedulerMode(fgDegraded, bgPaused))
		close(waiter.ready)
	}
}

func (s *llmConcurrencyScheduler) cancelActiveBackgroundLocked(reason string) int {
	cancelled := 0
	for lease := range s.activeBGLease {
		lease.mu.Lock()
		cancel := lease.cancel
		lease.mu.Unlock()
		if cancel != nil {
			cancel()
			cancelled++
		}
	}
	if cancelled > 0 {
		log.Printf("[llm-scheduler] preempt_background reason=%s cancelled=%d active_bg=%d", strings.TrimSpace(reason), cancelled, s.activeBG)
	}
	return cancelled
}

func (s *llmConcurrencyScheduler) nextDispatchIndexLocked() int {
	fgLimit, bgLimit, _, _ := s.limitsLocked()
	if s.activeFG < fgLimit {
		for i, waiter := range s.queue {
			if waiter.priority == llmPriorityForeground {
				return i
			}
		}
	}
	// Background LLM requests may run only when:
	//   - no active foreground LLM request (activeFG == 0): avoids API pressure
	//     from BG competing with in-flight FG calls
	//   - no foreground LLM request queued: FG gets strict priority
	//   - within the bg slot limit and not degraded/paused
	//
	// NOTE: we deliberately do NOT check foregroundWorkLocked() here. The
	// global foreground-agent-work counter tracks which desktop tabs have an
	// active agent loop, not whether a LLM request is in-flight. Blocking BG
	// LLM on foregroundWork > 0 would starve background processing (memory
	// extraction, knowledge archival) whenever ANY tab is running, which in
	// a multi-tab session means background tasks never run. The activeFG == 0
	// guard is sufficient: it ensures BG slots are only filled when no FG LLM
	// request is competing for bandwidth right now.
	if bgLimit > 0 && s.activeFG == 0 && s.activeBG < bgLimit && !s.hasQueuedForegroundLocked() {
		for i, waiter := range s.queue {
			if waiter.priority == llmPriorityBackground {
				return i
			}
		}
	}
	return -1
}

func (s *llmConcurrencyScheduler) foregroundWorkLocked() int64 {
	if s == nil || s.foregroundWork == nil {
		return 0
	}
	// Used only for diagnostic logging (logWaitStill). Not part of dispatch
	// decisions — see nextDispatchIndexLocked for the rationale.
	return s.foregroundWork()
}

func (s *llmConcurrencyScheduler) hasQueuedForegroundLocked() bool {
	for _, waiter := range s.queue {
		if waiter.priority == llmPriorityForeground {
			return true
		}
	}
	return false
}

func (s *llmConcurrencyScheduler) removeWaiterLocked(target *llmSchedulerWaiter) bool {
	for i, waiter := range s.queue {
		if waiter == target {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			return true
		}
	}
	return false
}

func (s *llmConcurrencyScheduler) limitsLocked() (int, int, bool, bool) {
	now := s.now()
	fgDegraded := now.Before(s.degradedUntil)
	bgPaused := now.Before(s.bgPausedUntil)
	if fgDegraded {
		return degradedForegroundLLMConcurrency, degradedBackgroundLLMConcurrency, true, bgPaused
	}
	if bgPaused {
		return defaultForegroundLLMConcurrency, degradedBackgroundLLMConcurrency, false, true
	}
	return defaultForegroundLLMConcurrency, defaultBackgroundLLMConcurrency, false, false
}

func (s *llmConcurrencyScheduler) scheduleRecoveryWakeLocked(now time.Time) {
	if s.degradedUntil.IsZero() && s.bgPausedUntil.IsZero() {
		return
	}
	until := s.degradedUntil
	if s.bgPausedUntil.After(until) {
		until = s.bgPausedUntil
	}
	delay := until.Sub(now)
	if delay < 0 {
		delay = 0
	}
	if s.recoveryTimer != nil {
		s.recoveryTimer.Stop()
	}
	s.recoveryTimer = time.AfterFunc(delay, func() {
		s.mu.Lock()
		fgDegraded := s.now().Before(s.degradedUntil)
		bgPaused := s.now().Before(s.bgPausedUntil)
		if !fgDegraded && !bgPaused {
			log.Printf("[llm-scheduler] recovered queue_depth=%d active_fg=%d active_bg=%d", len(s.queue), s.activeFG, s.activeBG)
			s.dispatchLocked()
		}
		s.mu.Unlock()
	})
}

func llmSchedulerMode(fgDegraded, bgPaused bool) string {
	if fgDegraded {
		return "degraded"
	}
	if bgPaused {
		return "background_paused"
	}
	return "healthy"
}

func nonNegativeDuration(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d
}

func classifyLLMRequestPriority(trace llm.RequestTrace) llmRequestPriority {
	caller := strings.ToLower(strings.TrimSpace(trace.Caller))
	if caller == "" {
		caller = "unknown"
	}
	if strings.Contains(caller, "background") || strings.Contains(caller, "memory") || strings.Contains(caller, "session-start") || strings.Contains(caller, "post-conversation") || strings.Contains(caller, "pending-reply") || strings.Contains(caller, "experience") || strings.Contains(caller, "probe") || strings.Contains(caller, "provider-test") || caller == "knowledge-card" || caller == "gossip-auto" {
		return llmPriorityBackground
	}
	return llmPriorityForeground
}

func acquireLLMSchedulerLease(ctx context.Context) (*llmSchedulerLease, llm.RequestTrace, error) {
	trace, ok := llm.RequestTraceFromContext(ctx)
	if !ok || strings.TrimSpace(trace.Caller) == "" {
		ctx = llm.WithRequestTraceIfMissing(ctx, "unknown")
		trace, _ = llm.RequestTraceFromContext(ctx)
	}
	lease, err := globalLLMScheduler.Acquire(ctx, trace)
	return lease, trace, err
}

func isProviderPressureLLMError(err error) bool {
	if err == nil || isHubPeriodLimitError(err) || isHubRateLimitWaitCanceledError(err) {
		// Period quota / canceled waits are not "pressure" that self-pacing can fix.
		return false
	}
	var httpErr *llm.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil {
		switch httpErr.StatusCode {
		case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "http 429") ||
		strings.Contains(s, "http 503") ||
		strings.Contains(s, "http 504") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		hasHubGatewayPressureMarker(s) ||
		strings.Contains(s, "overloaded") ||
		strings.Contains(s, "service unavailable") ||
		strings.Contains(s, "gateway timeout")
}

func isForegroundLLMThrottleError(err error) bool {
	if err == nil || isHubPeriodLimitError(err) || isHubRateLimitWaitCanceledError(err) {
		return false
	}
	var httpErr *llm.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil {
		return httpErr.StatusCode == http.StatusTooManyRequests
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "http 429") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		hasHubGatewayPressureMarker(s) ||
		strings.Contains(s, "overloaded")
}
