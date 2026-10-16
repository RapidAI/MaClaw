package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// P0-1 (0b-i) degraded-turn capability-gap self-recovery.
//
// When the unified intent classifier degrades (L3 endpoint failure), write
// tools stay fail-closed out of the tool table and the turn finishes with
// tools=0 — the request silently fails. This file implements the recovery
// state machine: tombstone the degraded cache scope, re-classify with the
// same message context, and on an authoritative Layer 3/4 verdict that
// activates the missing write tools, re-run the turn so the MODEL re-decides
// with the recovered tool table (the recovery never invokes write tools
// itself). Otherwise the user gets an explicit notice instead of silence.
// Secret-looking payloads re-run like any other: the 0b-ii execution-layer
// credential gate (payload scan + confirmation card) is their pause point.

// degradedRecoveryRequestIDPrefix marks continuation runs. A turn running
// under this prefix must never spawn another recovery (retry-loop guard).
const degradedRecoveryRequestIDPrefix = "degraded-recovery-"

// degradedReclassificationTimeout bounds the async reclassification wait.
const degradedReclassificationTimeout = 60 * time.Second

const degradedRecoveryNoticeUnavailable = "分类服务暂时不可用，请稍后重发。"

// degradedRecoveryWriteToolPrefixes are the write-tool families whose absence
// from a degraded turn's tool table marks a recoverable capability gap.
var degradedRecoveryWriteToolPrefixes = []string{"knowledge_save_", "knowledge_import_"}

// AIContinuationEvent is the payload of the "ai-continuation" Wails event.
// Kind is "token" (streamed delta), "final" (authoritative replacement),
// "notice" (standalone assistant message, e.g. recovery failure hints), or
// "confirmation" (a mid-loop confirmation card, e.g. the P0-4 credential
// write gate — rendered like a normal confirmation card with action buttons).
type AIContinuationEvent struct {
	RequestID  string `json:"request_id"`
	SessionKey string `json:"session_key,omitempty"`
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	// Confirmation and Actions carry the credential card payload for
	// Kind == "confirmation". They reuse the IM confirmation card protocol
	// (__confirm_execution__ / __cancel_execution__ commands).
	Confirmation *IMResponseConfirmation `json:"confirmation,omitempty"`
	Actions      []IMResponseAction      `json:"actions,omitempty"`
}

// emitAIContinuationEvent pushes one continuation event to the frontend.
func (a *App) emitAIContinuationEvent(event AIContinuationEvent) {
	if a == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	a.emitEvent("ai-continuation", string(payload))
}

// ---------------------------------------------------------------------------
// Recovery state machine
// ---------------------------------------------------------------------------

// degradedRecoveryPlan is an immutable snapshot of the degraded turn handed
// to the async recovery goroutine.
type degradedRecoveryPlan struct {
	userID     string
	userText   string
	requestID  string // original turn request id (dedup key + notice origin)
	platform   string
	lang       string
	message    intent.MessageContext // exact classification scope of this turn
	generation uint64                // per-user generation captured at turn end
	turnTools  []map[string]interface{}
}

// degradedTurnRecoveryManager holds the in-memory recovery state: per-user
// generations (a new inbound message invalidates every pending recovery for
// that user) and the dedup set of recovery attempts. Nothing here survives a
// restart — by design, no recovery is replayed after startup.
type degradedTurnRecoveryManager struct {
	mu         sync.Mutex
	generation map[string]uint64
	attempts   map[string]time.Time
}

// degradedRecoveryAttemptTTL bounds the dedup window for recovery attempts.
const degradedRecoveryAttemptTTL = 15 * time.Minute

func newDegradedTurnRecoveryManager() *degradedTurnRecoveryManager {
	return &degradedTurnRecoveryManager{
		generation: make(map[string]uint64),
		attempts:   make(map[string]time.Time),
	}
}

// generationFor returns the current per-user generation.
func (m *degradedTurnRecoveryManager) generationFor(userID string) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation[userID]
}

// bumpGeneration invalidates every pending recovery for the user (new inbound
// message or cancel) and returns the new generation.
func (m *degradedTurnRecoveryManager) bumpGeneration(userID string) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generation[userID]++
	return m.generation[userID]
}

// recordAttempt claims a dedup slot for one turn. It returns false when the
// same turn (request id + user text hash) already attempted a recovery within
// the TTL — one turn re-runs at most once.
func (m *degradedTurnRecoveryManager) recordAttempt(key string) bool {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if expiry, ok := m.attempts[key]; ok && now.Before(expiry) {
		return false
	}
	if len(m.attempts) > 512 {
		for k, expiry := range m.attempts {
			if !now.Before(expiry) {
				delete(m.attempts, k)
			}
		}
	}
	m.attempts[key] = now.Add(degradedRecoveryAttemptTTL)
	return true
}

// degradedRecoveryDedupKey is request_id + user-text hash.
func degradedRecoveryDedupKey(requestID, userText string) string {
	sum := sha256.Sum256([]byte(userText))
	return strings.TrimSpace(requestID) + "\x00" + fmt.Sprintf("%x", sum[:])
}

// degradedTurnRecovery returns the handler's recovery manager, lazily
// initialized so zero-value IMMessageHandler structs (tests) still work.
func (h *IMMessageHandler) degradedTurnRecovery() *degradedTurnRecoveryManager {
	if h == nil {
		return nil
	}
	h.degradedRecoveryInit.Do(func() {
		h.degradedRecovery = newDegradedTurnRecoveryManager()
	})
	return h.degradedRecovery
}

// ---------------------------------------------------------------------------
// Turn-end hook
// ---------------------------------------------------------------------------

// maybeRecoverDegradedTurn is the P0-1 end-of-turn hook (agent loop shared
// path). All three trigger conditions must hold:
//  1. this turn's SemanticIntent is Degraded (ControlPlaneFailure included);
//  2. the user text hits the narrow explicit-capability lexical trigger;
//  3. the turn's actual tool table contains no knowledge write tools.
func (h *IMMessageHandler) maybeRecoverDegradedTurn(ctx *LoopContext, userID, userText, requestID string, turnTools []map[string]interface{}, platform, lang string) {
	if h == nil || ctx == nil || strings.TrimSpace(userText) == "" {
		return
	}
	if loopContextIsVisionFallthrough(ctx) {
		return
	}
	si := ctx.Runtime.SemanticIntent
	if si == nil || !si.Degraded {
		return
	}
	if strings.HasPrefix(strings.TrimSpace(requestID), degradedRecoveryRequestIDPrefix) {
		return // a continuation run must never spawn another recovery
	}
	if degradedTurnToolTableHasWriteTools(turnTools) {
		return // the write capability was not lost on this turn
	}
	if !intent.ExplicitCapabilityRequestTrigger(userText) {
		return // trigger only, not authority — narrow on purpose
	}
	mgr := h.degradedTurnRecovery()
	if mgr == nil {
		return
	}
	if !mgr.recordAttempt(degradedRecoveryDedupKey(requestID, userText)) {
		return // this turn already attempted a recovery
	}
	plan := degradedRecoveryPlan{
		userID:     userID,
		userText:   userText,
		requestID:  strings.TrimSpace(requestID),
		platform:   platform,
		lang:       lang,
		message:    classificationCacheMessageForTurn(ctx, userID, userText, loopHistory(ctx)),
		generation: mgr.generationFor(userID),
		turnTools:  turnTools,
	}
	log.Printf("[degraded-recovery] scheduled owner=%q request_id=%q text_len=%d degraded_reason=%q",
		userID, plan.requestID, len([]rune(userText)), si.Reason)
	go h.runDegradedTurnRecovery(plan)
}

func degradedTurnToolTableHasWriteTools(turnTools []map[string]interface{}) bool {
	for _, def := range turnTools {
		name := extractToolName(def)
		for _, prefix := range degradedRecoveryWriteToolPrefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

// degradedRecoveryActivatesMissingWriteTools reports which write tools an
// authoritative reclassification verdict activates that the degraded turn's
// tool table lacked. Non-authoritative results (still degraded, or not from
// Layer 3/4/23) activate nothing.
func degradedRecoveryActivatesMissingWriteTools(result intent.ClassificationResult, turnTools []map[string]interface{}) []string {
	if result.Degraded || (result.Layer != 3 && result.Layer != 4 && result.Layer != 23) {
		return nil
	}
	present := make(map[string]bool, len(turnTools))
	for _, def := range turnTools {
		if name := extractToolName(def); name != "" {
			present[name] = true
		}
	}
	var activated []string
	seen := make(map[string]bool)
	for _, name := range result.ToolNames {
		for _, prefix := range degradedRecoveryWriteToolPrefixes {
			if strings.HasPrefix(name, prefix) && !present[name] && !seen[name] {
				seen[name] = true
				activated = append(activated, name)
			}
		}
	}
	return activated
}

// ---------------------------------------------------------------------------
// Async recovery
// ---------------------------------------------------------------------------

// Overridable seams for tests; production behavior is the package defaults.
var (
	emitDegradedRecoveryNoticeFn = func(h *IMMessageHandler, userID, originRequestID, text string) {
		if h == nil || h.app == nil {
			log.Printf("[degraded-recovery] notice dropped without app owner=%q origin_request_id=%q text=%q", userID, originRequestID, text)
			return
		}
		h.app.emitAIContinuationEvent(AIContinuationEvent{
			RequestID:  originRequestID,
			SessionKey: userID,
			Kind:       "notice",
			Text:       text,
		})
	}
	runDegradedTurnContinuationFn = func(h *IMMessageHandler, plan degradedRecoveryPlan, activated []string) {
		if h == nil || h.app == nil {
			log.Printf("[degraded-recovery] continuation dropped without app owner=%q origin_request_id=%q activated=%v",
				plan.userID, plan.requestID, activated)
			return
		}
		h.app.runDegradedTurnContinuation(plan.userID, plan.userText, plan.platform, plan.lang, plan.requestID, plan.generation)
	}
)

// runDegradedTurnRecovery re-classifies the degraded turn's exact message
// scope and, on success, initiates the continuation run.
func (h *IMMessageHandler) runDegradedTurnRecovery(plan degradedRecoveryPlan) {
	uic := h.getUnifiedClassifier()
	if uic == nil {
		emitDegradedRecoveryNoticeFn(h, plan.userID, plan.requestID, degradedRecoveryNoticeUnavailable)
		return
	}
	// An authoritative tree verdict that landed before the tombstone (e.g. a
	// late tree verdict that beat this goroutine) is adopted directly —
	// tree-provenance cache entries are trusted without a new LLM call.
	if cached, ok := uic.ClassifyCached(plan.message); ok && !cached.Degraded {
		if source, _ := uic.CachedClassificationProvenance(plan.message); source == intent.CacheSourceTree {
			if activated := degradedRecoveryActivatesMissingWriteTools(cached, plan.turnTools); len(activated) > 0 {
				log.Printf("[degraded-recovery] adopted pre-landed tree verdict owner=%q origin_request_id=%q primary=%s activated=%v",
					plan.userID, plan.requestID, cached.Primary, activated)
				h.finishDegradedRecovery(plan, activated)
				return
			}
		}
	}
	// Per-key invalidation BEFORE reclassification: suppresses cache stores
	// for exactly this scope (degraded hint, racing late verdicts) so the
	// reclassification cannot be swallowed by a stale entry. Keep the returned
	// token: a concurrent recovery for the same text re-arms this scope, and
	// only this token may disarm our own tombstone later.
	tombstoneToken := uic.AddCacheTombstone(plan.message)
	ctx, cancel := context.WithTimeout(context.Background(), degradedReclassificationTimeout)
	result := uic.ClassifyContext(ctx, plan.message)
	cancel()
	activated := degradedRecoveryActivatesMissingWriteTools(result, plan.turnTools)
	if len(activated) == 0 {
		log.Printf("[degraded-recovery] reclassification did not recover a write capability owner=%q origin_request_id=%q layer=%d primary=%s degraded=%v",
			plan.userID, plan.requestID, result.Layer, result.Primary, result.Degraded)
		emitDegradedRecoveryNoticeFn(h, plan.userID, plan.requestID, degradedRecoveryNoticeUnavailable)
		return
	}
	// Lift the tombstone and store the fresh verdict. The reclassification's
	// own store was suppressed while the tombstone was armed; without this
	// the scope could not be cached (resends/adoptions would re-pay the L3
	// call) until the TTL expired. Remove BEFORE the store so the fresh
	// verdict is not swallowed by our own tombstone. The token makes the
	// remove conditional: if another recovery re-armed the scope meanwhile,
	// their guard stays up and our store below is suppressed instead.
	uic.RemoveCacheTombstone(plan.message, tombstoneToken)
	uic.StoreRecoveredClassification(plan.message, result)
	log.Printf("[degraded-recovery] reclassification recovered write capability owner=%q origin_request_id=%q primary=%s activated=%v",
		plan.userID, plan.requestID, result.Primary, activated)
	h.finishDegradedRecovery(plan, activated)
}

// finishDegradedRecovery applies the end-of-recovery policy: a newer user
// message invalidates the pending continuation immediately; otherwise the
// continuation run starts.
func (h *IMMessageHandler) finishDegradedRecovery(plan degradedRecoveryPlan, activated []string) {
	mgr := h.degradedTurnRecovery()
	if mgr != nil && mgr.generationFor(plan.userID) != plan.generation {
		log.Printf("[degraded-recovery] invalidated by newer user message owner=%q origin_request_id=%q", plan.userID, plan.requestID)
		return
	}
	runDegradedTurnContinuationFn(h, plan, activated)
}

// ---------------------------------------------------------------------------
// Continuation run (App side)
// ---------------------------------------------------------------------------

// runDegradedTurnContinuation re-dispatches the recovered request as a fresh
// turn: routing sees the fresh authoritative classification and assembles the
// tool table including the previously missing write tools, then the model
// re-decides execution. The result streams to the frontend on the dedicated
// "ai-continuation" channel — never on "ai-assistant-response" (no round owns
// it). The original assistant reply is left untouched.
//
// expectedGeneration is the per-user recovery generation captured when the
// recovery decided to continue. finishDegradedRecovery already checked it,
// but the dispatch crosses a goroutine boundary: a user message landing in
// that window would otherwise let a stale continuation run against a newer
// turn. The continuation goroutine re-validates the generation right before
// entering the handler pipeline and drops itself on mismatch.
// degradedContinuationDispatchFn enters the handler pipeline for a validated
// continuation run. It is a package-level seam so tests can assert the
// second-generation fence drops stale continuations BEFORE HandleIMMessage
// is invoked (the fence itself stays the real production check).
var degradedContinuationDispatchFn = func(handler *IMMessageHandler, msg IMUserMessage, emit func(kind, text string), requestID, originRequestID string) {
	resp := handler.HandleIMMessageWithProgressAndStream(msg, nil, func(delta string) {
		if delta != "" {
			emit("token", delta)
		}
	}, nil, nil)
	if resp == nil {
		emit("final", degradedRecoveryNoticeUnavailable)
		return
	}
	if resp.Error != "" && strings.TrimSpace(resp.Text) == "" {
		emit("final", "自动重试失败："+resp.Error)
		return
	}
	emit("final", strings.TrimSpace(resp.Text))
	log.Printf("[degraded-recovery] continuation done request_id=%s origin_request_id=%s", requestID, originRequestID)
}

func (a *App) runDegradedTurnContinuation(userID, userText, platform, lang, originRequestID string, expectedGeneration uint64) {
	if a == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if strings.TrimSpace(platform) == "" {
		platform = desktopPlatform
	}
	requestID := fmt.Sprintf("%s%d", degradedRecoveryRequestIDPrefix, time.Now().UnixNano())
	emit := func(kind, text string) {
		a.emitAIContinuationEvent(AIContinuationEvent{
			RequestID:  requestID,
			SessionKey: userID,
			Kind:       kind,
			Text:       text,
		})
	}
	log.Printf("[degraded-recovery] continuation start origin_request_id=%s request_id=%s owner=%q text_len=%d",
		originRequestID, requestID, userID, len([]rune(userText)))
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		emit("notice", "自动重试失败：AI 助手后端不可用，请稍后重发。")
		return
	}
	handler := hubClient.ensureIMHandler()
	msg := IMUserMessage{
		RequestID: requestID,
		UserID:    userID,
		Platform:  platform,
		Text:      userText,
		Lang:      lang,
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[degraded-recovery] continuation panic request_id=%s panic=%v", requestID, r)
				emit("final", degradedRecoveryNoticeUnavailable)
			}
		}()
		// Second-generation fence: between finishDegradedRecovery's check and
		// this goroutine actually entering the pipeline, the user may have
		// sent a newer message (bumped generation). Running the continuation
		// then would execute a stale turn against fresh context — drop it
		// BEFORE the dispatch seam so HandleIMMessage is never called.
		if mgr := handler.degradedTurnRecovery(); mgr != nil && mgr.generationFor(userID) != expectedGeneration {
			log.Printf("[degraded-recovery] continuation dropped by newer user message request_id=%s owner=%q origin_request_id=%s",
				requestID, userID, originRequestID)
			return
		}
		degradedContinuationDispatchFn(handler, msg, emit, requestID, originRequestID)
	}()
}
