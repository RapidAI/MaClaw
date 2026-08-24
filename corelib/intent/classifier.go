package intent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// DefaultFusionTreeDeadline is how long ClassifyContext waits for L3 after an
// ambiguous L2 result, and how long dual-channel fusion waits on the tree
// channel. Reasoning models often exceed this with a thinking phase; that is
// intentional — an unconfirmed embedding guess must not block the control
// path for a full LLM timeout.
const DefaultFusionTreeDeadline = 5 * time.Second

// DefaultLLMTimeout is the outer budget for tree-only classification (no L2).
// Kept longer than fusion wait so pure-LLM mode can still complete on remote
// chat models when embedding is unavailable.
const DefaultLLMTimeout = 30 * time.Second

// Config holds initialization parameters for the UnifiedIntentClassifier.
type Config struct {
	Embedder       embedding.Embedder
	LLMFunc        LLMClassifyFunc // optional compatibility callback
	LLMContextFunc LLMClassifyContextFunc
	LLMTimeout     time.Duration // tree-only path; 0 -> DefaultLLMTimeout (30s)
	// FusionTreeDeadline caps how long classifyWithFusion waits on the tree
	// channel when embedding is also available. 0 -> DefaultFusionTreeDeadline.
	// Never exceeds LLMTimeout when both are set.
	FusionTreeDeadline time.Duration
}

// UnifiedIntentClassifier is the single entry point for all user-intent
// classification. It implements a dual-channel fusion pipeline inspired by
// intent-fusion (https://github.com/Liyuan1992/intent-fusion):
//
//	Layer 2: embedding cosine similarity (~5ms) — parallel with Layer 3
//	Layer 3: LLM intent tree reasoning (~2-8s) — parallel with Layer 2
//	Fusion:  α·emb + (1-α)·tree → three-state verdict (CLEAR/AMBIGUOUS/LOW)
//
// When both L2 and L3 are available, they run in parallel and their results
// are fused using a weighted formula. When only one channel is available,
// the system degrades gracefully (α forced to 0 or 1).
type UnifiedIntentClassifier struct {
	affinity           *ToolAffinityRegistry
	embedder           embedding.Embedder
	anchors            []intentAnchor
	llmFunc            LLMClassifyFunc
	llmContextFunc     LLMClassifyContextFunc
	llmTimeout         time.Duration
	fusionTreeDeadline time.Duration

	// Intent Tree text for Layer 3 tree reasoning (pre-built from definitions).
	treeText string
	// Flat LLM prompt for Layer 3 single-channel fallback (pre-built from definitions).
	llmPrompt string
	fusionCfg FusionConfig

	// Per-message cache: cleared after each message processing cycle.
	cache       sync.Map // map[string]*ClassificationResult
	cacheEpoch  atomic.Uint64
	fusionCache sync.Map // map[string]FusionResult — stores fusion details for diagnostics

	// Separate cache for ClassifyEmbeddingOnly results. Kept apart from the
	// main cache so lower-quality L2-only results never satisfy full Classify
	// calls. Bounded because per-message InvalidateCache is not guaranteed.
	embOnlyCache sync.Map // map[string]*ClassificationResult
	embOnlyCount atomic.Int64

	// workflowCandidates is the set of IntentLabels that may trigger a
	// multi-phase workflow, derived from IntentDefinition.MayTriggerWorkflow.
	// Pre-computed at construction time. Labels NOT in this set are
	// definitively non-workflow and can be fast-rejected by consumers.
	workflowCandidates map[IntentLabel]bool

	// embeddingGeneration advances whenever anchors are replaced. A warmup
	// goroutine may finish after a newer embedder is installed; its generation
	// check prevents stale vectors from re-enabling Layer 2 for the wrong model.
	embeddingGeneration uint64
	ready               bool         // set to true when the current anchor warmup completes
	mu                  sync.RWMutex // protects ready, embeddingGeneration, and LLM callbacks
}

// New creates a UnifiedIntentClassifier. Starts background anchor warmup
// if the embedder is not a NoopEmbedder.
func New(cfg Config) *UnifiedIntentClassifier {
	timeout := cfg.LLMTimeout
	if timeout == 0 {
		timeout = DefaultLLMTimeout
	}
	fusionTreeDeadline := cfg.FusionTreeDeadline
	if fusionTreeDeadline <= 0 {
		fusionTreeDeadline = DefaultFusionTreeDeadline
	}
	// Fusion wait never exceeds the outer LLM budget.
	if timeout > 0 && fusionTreeDeadline > timeout {
		fusionTreeDeadline = timeout
	}

	// Build intent tree text from unified definitions for Layer 3 tree reasoning.
	defs := DefaultDefinitions()
	treeText := BuildIntentTreeText(defs)

	u := &UnifiedIntentClassifier{
		affinity:           NewToolAffinityRegistryFromDefinitions(defs),
		embedder:           cfg.Embedder,
		anchors:            BuildAnchorsFromDefinitions(defs),
		llmFunc:            cfg.LLMFunc,
		llmContextFunc:     cfg.LLMContextFunc,
		llmTimeout:         timeout,
		fusionTreeDeadline: fusionTreeDeadline,
		treeText:           treeText,
		llmPrompt:          buildLLMSystemPrompt(defs),
		fusionCfg:          DefaultFusionConfigWithWorkflowTypes(defs),
		workflowCandidates: WorkflowCandidateLabels(defs),
	}

	// Determine available layers and log.
	isNoop := embedding.IsNoop(cfg.Embedder)
	var layers []string
	if !isNoop {
		layers = append(layers, "Layer 2 (embedding)")
	}
	if cfg.LLMFunc != nil || cfg.LLMContextFunc != nil {
		layers = append(layers, "Layer 3 (LLM)")
	}
	if len(layers) == 0 {
		layers = append(layers, "none (semantic classifiers unavailable)")
	}
	log.Printf("[UnifiedIntentClassifier] available layers: %v", layers)

	// Start background anchor warmup if embedder is real.
	if !isNoop {
		anchors := u.anchors     // immutable cold snapshot for goroutine
		embedder := cfg.Embedder // snapshot for goroutine
		u.embeddingGeneration++
		generation := u.embeddingGeneration
		go func() {
			warmed, err := warmupAnchors(embedder, anchors)
			if err != nil {
				log.Printf("[UnifiedIntentClassifier] Layer 2 remains unavailable: %v", err)
				return
			}
			u.mu.Lock()
			if u.embeddingGeneration != generation || !sameAnchorSnapshot(u.anchors, anchors) {
				u.mu.Unlock()
				log.Printf("[UnifiedIntentClassifier] discarded stale Layer 2 anchor warmup generation=%d", generation)
				return
			}
			u.anchors = warmed
			u.ready = true
			u.mu.Unlock()
		}()
	} else {
		// NoopEmbedder: mark ready immediately (Layer 2 won't be used).
		u.mu.Lock()
		u.ready = true
		u.mu.Unlock()
	}

	return u
}

// sameAnchorSnapshot reports whether two anchor slices still refer to the same
// backing snapshot. Besides SetEmbedder, package-level integration tests and
// specialized hosts can replace anchors directly before the initial warmup
// completes; the initial goroutine must never publish over that replacement.
func sameAnchorSnapshot(current, snapshot []intentAnchor) bool {
	if len(current) != len(snapshot) {
		return false
	}
	if len(current) == 0 {
		return true
	}
	return &current[0] == &snapshot[0]
}

// Classify returns the ClassificationResult for the given message.
//
// New request-processing code should use ClassifyContext so tree reasoning
// inherits the lifetime of the inbound turn. This compatibility entry point is
// retained for callers that genuinely have no request context.
// Results are cached per message text plus recent-history context; subsequent
// calls with the same semantic input return the cached result without recomputation.
//
// Execution model:
//  1. Run L2 embedding first. A high-confidence, well-separated result, or
//     a reviewed declared composite whose two semantic halves independently
//     meet their thresholds, is returned immediately without any LLM request.
//  2. Escalate only ambiguous L2 results to L3 tree reasoning, bounded by
//     FusionTreeDeadline when L2 already ran (tree-only still uses LLMTimeout).
//     A short, sub-floor search/live_data guess does not pay that latency:
//     「北京天所」 is not a confirmed lookup, and lexical markers still recover
//     short lookup requests.
//  3. If L3 fails after that escalation, return conservative unknown. Do not
//     promote the unconfirmed L2 capability label: downstream treats Primary
//     as a governed identity.
//  4. If only one channel is available, use it; if neither is available,
//     return conservative unknown.
//
// Local keyword rules are not used as a degraded decision path. Callers that
// gate workflow transitions should fail closed or ask for clarification when
// semantic classifiers are unavailable.
func (u *UnifiedIntentClassifier) Classify(msg MessageContext) ClassificationResult {
	return u.ClassifyContext(context.Background(), msg)
}

// ClassifyContext returns the ClassificationResult for a message while
// respecting the caller's cancellation boundary. A cancelled turn must not
// keep a tree-classification request alive and later make its result appear to
// belong to a replacement turn. The configured LLM timeout remains an upper
// bound; the supplied context may only make that bound stricter.
func (u *UnifiedIntentClassifier) ClassifyContext(ctx context.Context, msg MessageContext) ClassificationResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return cancelledClassificationResult(err)
	}
	epoch := u.cacheEpoch.Load()
	cacheKey := classificationCacheKey(epoch, msg)

	// Check cache first.
	if cached, ok := u.cache.Load(cacheKey); ok {
		return *cached.(*ClassificationResult)
	}

	// Snapshot mutable fields under lock to avoid racing with SetEmbedder/SetLLMFunc.
	u.mu.RLock()
	emb := u.embedder
	anchors := u.anchors
	hasLLM := u.llmFunc != nil || u.llmContextFunc != nil
	isReady := u.ready
	u.mu.RUnlock()

	// Determine which layers are available.
	isNoop := embedding.IsNoop(emb)
	canEmb := !isNoop && isReady
	canTree := hasLLM

	if !isNoop && !isReady {
		log.Printf("[UnifiedIntentClassifier] Layer 2 skipped: anchors not ready")
	}

	// Fast path: embedding is deterministic and local. Confident lookup
	// (search/live_data at EmbeddingLookupMinScore with EmbeddingLookupMinGap)
	// or the strict rule (EmbeddingConfidentMinScore / EmbeddingConfidentMinGap)
	// skip the remote LLM; only an unresolved result pays that latency.
	// A locally verified declared composite stays local so its dependency graph
	// cannot be collapsed or overwritten by an unavailable tree response.
	var l2Result ClassificationResult
	l2Ran := false
	skipTree := false
	if canEmb {
		var confident bool
		l2Result, confident = classifyByEmbedding(emb, anchors, msg.Text)
		l2Ran = true
		if confident {
			NormalizeDeclaredComposite(&l2Result)
			applyExecutionAffordances(msg.Text, &l2Result)
			l2Result.ToolNames = u.affinity.Resolve(l2Result.Primary, l2Result.Secondary)
			u.cacheAndLog(cacheKey, msg.Text, &l2Result)
			return l2Result
		}
		if skipTreeForShortAmbiguousLookup(msg.Text, l2Result) {
			// Policy skip is stable whether or not an LLM is configured.
			skipTree = true
			canTree = false
			log.Printf("[UnifiedIntentClassifier] Layer 2 short lookup skipped tree: text_len=%d primary=%s conf=%.2f", utf8.RuneCountInString(msg.Text), l2Result.Primary, l2Result.Confidence)
		} else if !canTree {
			// Embedding-only and unconfirmed: keep a lookup as an explicit
			// hint. Affordances and affinity would attach generate/tools and
			// make downstream treat the guess as a governed capability.
			result := lookupHintOrUnknownFromL2(l2Result, false)
			u.cacheAndLog(cacheKey, msg.Text, &result)
			return result
		} else {
			// Do not start an L3 call speculatively. An ambiguous embedding result is
			// useful evidence for logs, but the tree's semantic verdict is the route
			// authority after escalation.
			log.Printf("[UnifiedIntentClassifier] Layer 2 ambiguous; escalating to tree: text_len=%d primary=%s conf=%.2f", utf8.RuneCountInString(msg.Text), l2Result.Primary, l2Result.Confidence)
		}
	}

	// L3 is reached only when embedding is unavailable or cannot separate the
	// candidate intents with enough confidence.
	if canTree {
		u.mu.RLock()
		llmFn := u.llmFunc
		llmContextFn := u.llmContextFunc
		llmTimeout := u.llmTimeout
		treeDeadline := u.fusionTreeDeadline
		u.mu.RUnlock()
		// Ambiguous L2 already produced a guess. Waiting the tree-only 30s
		// budget (flash/reasoning models often stall) turns a four-character
		// typo into a hung turn. Fusion's deadline is the designed cap.
		if l2Ran {
			if treeDeadline <= 0 {
				treeDeadline = DefaultFusionTreeDeadline
			}
			llmTimeout = treeDeadline
		}

		candidates, err := classifyByTreeWithTimeout(ctx, llmContextFn, llmFn, u.treeText, msg.Text, llmTimeout)
		if err == nil && len(candidates) > 0 {
			top := candidates[0]
			bestResult := ClassificationResult{
				Primary:      top.Label,
				Confidence:   top.Score,
				Secondary:    secondaryTreeLabels(candidates),
				Layer:        3,
				Reason:       fmt.Sprintf("tree-after-embedding: %s (%.3f)", top.Label, top.Score),
				WorkflowType: top.WorkflowType,
			}
			if top.Label == LabelCoding && top.WorkflowType == "coding" {
				bestResult.CreationOriented = true
			}
			// Escalation path discards L2, but L2 may hold the complementary half
			// of a declared composite (e.g. L2=document_generate 0.79, tree=live_data/web_fetch 0.59).
			// Synthesize the composite without keyword heuristics so any
			// lookup+generate phrasing is recovered even when the tree returns a single label.
			if l2Ran && len(bestResult.Secondary) == 0 && l2Result.Primary != LabelUnknown && l2Result.Primary != LabelAmbiguous && l2Result.Primary != bestResult.Primary {
				if declaredCompositeIntentPair(l2Result.Primary, bestResult.Primary) {
					// Keep the lower confidence as composite confidence to avoid
					// over-promoting a weak half; execution profile will treat
					// the composite as managed mutating and exempt it from chat projection.
					compConf := bestResult.Confidence
					if l2Result.Confidence < compConf {
						compConf = l2Result.Confidence
					}
					// Build a synthetic composite and let canonical direction
					// (lookup Primary) be enforced by NormalizeDeclaredComposite.
					synth := ClassificationResult{
						Primary:    LabelDocumentGenerate,
						Secondary:  []IntentLabel{bestResult.Primary},
						Confidence: compConf,
						Layer:      3,
						Reason:     fmt.Sprintf("tree-after-embedding+synthesized composite: %s(%.3f)+%s(%.3f)", bestResult.Primary, bestResult.Confidence, l2Result.Primary, l2Result.Confidence),
					}
					if isLookupIntentLabel(bestResult.Primary) {
						synth.Primary = LabelDocumentGenerate
						synth.Secondary = []IntentLabel{bestResult.Primary}
					} else if isLookupIntentLabel(l2Result.Primary) {
						synth.Primary = LabelDocumentGenerate
						synth.Secondary = []IntentLabel{l2Result.Primary}
					} else {
						synth = ClassificationResult{}
					}
					if synth.Primary != "" {
						NormalizeDeclaredComposite(&synth)
						if len(synth.Secondary) > 0 && synth.Primary != LabelDocumentGenerate {
							bestResult = synth
							bestResult.WorkflowType = top.WorkflowType
						}
					}
				}
			}
			NormalizeDeclaredComposite(&bestResult)
			applyExecutionAffordances(msg.Text, &bestResult)
			bestResult.ToolNames = u.affinity.Resolve(bestResult.Primary, bestResult.Secondary)
			u.cacheAndLog(cacheKey, msg.Text, &bestResult)
			return bestResult
		}
		log.Printf("[UnifiedIntentClassifier] Layer 3 failed: %v; keeping L2 lookup as hint or collapsing non-lookup", err)
		if err := ctx.Err(); err != nil {
			return cancelledClassificationResult(err)
		}
		var protocolErr *TreeResponseProtocolError
		if errors.As(err, &protocolErr) {
			// A 200 response with prose instead of the classifier's contract is
			// not an unknown intent.  Preserve that distinction so the shared
			// loop can stop before its legacy router creates a tools=0 request.
			return ClassificationResult{
				Primary:             LabelUnknown,
				Confidence:          0.30,
				Layer:               3,
				Reason:              "intent classification structured-output protocol violation",
				Degraded:            true,
				ControlPlaneFailure: true,
			}
		}
	}

	// L3 is the route authority after an ambiguous L2 escalation. A search or
	// live_data guess stays a read-only hint: routing may chat (sub-floor) or
	// plan lookup (≥ 0.70) without HostReject. Other families still collapse
	// to unknown. Do not keep generate/coding secondaries or resolve tools.
	if l2Ran {
		result := lookupHintOrUnknownFromL2(l2Result, skipTree)
		if skipTree {
			// Policy skip is stable for this text, unlike an L3 timeout.
			// cacheAndLog refuses all Degraded results; store the hint here.
			u.cache.Store(cacheKey, &result)
		}
		u.cacheAndLog(cacheKey, msg.Text, &result)
		return result
	}

	// Degraded mode: neither L2 nor L3 is available (or L3 failed). Primary
	// intent remains unknown; only narrow execution affordances may add secondary
	// tool needs such as browser delivery to a named web platform.
	result := ClassificationResult{
		Primary:    LabelUnknown,
		Confidence: 0.30,
		Layer:      0,
		Reason:     "semantic classifiers unavailable",
		Degraded:   true,
	}
	applyExecutionAffordances(msg.Text, &result)
	result.ToolNames = u.affinity.Resolve(result.Primary, result.Secondary)
	u.cacheAndLog(cacheKey, msg.Text, &result)
	return result
}

// shortAmbiguousLookupMaxRunes is the longest utterance that may skip L3 when
// L2 only produced a sub-floor search/live_data guess. Longer weather/PDF
// requests still escalate; 「北京天所」 does not wait on a flash model.
const shortAmbiguousLookupMaxRunes = 6

func skipTreeForShortAmbiguousLookup(text string, result ClassificationResult) bool {
	if utf8.RuneCountInString(strings.TrimSpace(text)) > shortAmbiguousLookupMaxRunes {
		return false
	}
	switch result.Primary {
	case LabelSearch, LabelLiveData:
	default:
		return false
	}
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		if label != LabelSearch && label != LabelLiveData {
			return false
		}
	}
	return result.Confidence < EmbeddingLookupMinScore
}

// lookupHintOrUnknownFromL2 keeps an unconfirmed search/live_data primary as a
// degraded hint and collapses every other family to unknown. Secondary labels,
// workflow type, and tool names are cleared: a leftover document_generate or
// affinity pin would HostReject the turn.
func lookupHintOrUnknownFromL2(l2 ClassificationResult, skipTree bool) ClassificationResult {
	reason := fmt.Sprintf("embedding ambiguous; tree classification unavailable (l2=%s conf=%.2f)", l2.Primary, l2.Confidence)
	if skipTree {
		reason = fmt.Sprintf("embedding ambiguous; short lookup skipped tree (l2=%s conf=%.2f)", l2.Primary, l2.Confidence)
	}
	if l2.Primary != LabelSearch && l2.Primary != LabelLiveData {
		return ClassificationResult{
			Primary:    LabelUnknown,
			Confidence: 0.30,
			Layer:      2,
			Reason:     reason,
			Degraded:   true,
		}
	}
	return ClassificationResult{
		Primary:    l2.Primary,
		Confidence: l2.Confidence,
		Layer:      2,
		Reason:     reason,
		Degraded:   true,
	}
}

func classifyByTreeWithTimeout(parent context.Context, llmContextFn LLMClassifyContextFunc, llmFn LLMClassifyFunc, treeText, text string, timeout time.Duration) ([]TreeCandidate, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, fmt.Errorf("tree reasoning cancelled before start: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	type result struct {
		candidates []TreeCandidate
		err        error
	}
	ch := make(chan result, 1)
	go func() {
		candidates, err := ClassifyByTreeContext(ctx, llmContextFn, llmFn, treeText, text)
		ch <- result{candidates: candidates, err: err}
	}()
	select {
	case r := <-ch:
		// If a non-cooperative callback completes at the same instant as the
		// inbound turn is cancelled, cancellation still wins. Otherwise an old
		// turn can cache a successful verdict after its host has already replaced
		// or abandoned that turn.
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("tree reasoning cancelled: %w", err)
		}
		return r.candidates, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("tree reasoning LLM call timed out after %s: %w", timeout, ctx.Err())
	}
}

func cancelledClassificationResult(err error) ClassificationResult {
	return ClassificationResult{
		Primary: LabelUnknown, Confidence: 0.30, Layer: 0,
		Reason: "semantic classification cancelled: " + err.Error(), Degraded: true,
	}
}

func secondaryTreeLabels(candidates []TreeCandidate) []IntentLabel {
	if len(candidates) < 2 {
		return nil
	}
	topScore := candidates[0].Score
	seen := map[IntentLabel]bool{candidates[0].Label: true}
	var secondary []IntentLabel
	for _, candidate := range candidates[1:] {
		if candidate.Label == LabelUnknown || candidate.Label == LabelAmbiguous || seen[candidate.Label] {
			continue
		}
		isComposite := declaredCompositeIntentPair(candidates[0].Label, candidate.Label)
		threshold := 0.70
		if isComposite {
			threshold = 0.50
		}
		if candidate.Score < threshold {
			continue
		}
		if topScore-candidate.Score > 0.20 && !isComposite {
			continue
		}
		seen[candidate.Label] = true
		secondary = append(secondary, candidate.Label)
	}
	return secondary
}

// InvalidateCache clears the per-message cache. Called once per message
// processing cycle by the consumer (e.g., after IMMessageHandler finishes).
func (u *UnifiedIntentClassifier) InvalidateCache() {
	u.cache.Range(func(key, _ any) bool {
		u.cache.Delete(key)
		return true
	})
	u.fusionCache.Range(func(key, _ any) bool {
		u.fusionCache.Delete(key)
		return true
	})
	u.embOnlyCache.Range(func(key, _ any) bool {
		u.embOnlyCache.Delete(key)
		return true
	})
	u.embOnlyCount.Store(0)
}

// ClassifyEmbeddingOnly performs L2 embedding-only classification without
// triggering the L3 tree reasoning LLM call. This is significantly faster
// (~50-100ms vs LLM latency) and suitable for auxiliary checks where a rough
// intent signal is sufficient (e.g., checking if conversation history
// contains coding context).
//
// Results are cached in a dedicated bounded cache (separate from the main
// Classify cache, so L2-only results never satisfy full fusion calls). The
// same text within one message cycle is embedded only once across all
// consumers. Degraded results (embedding unavailable) are never cached.
func (u *UnifiedIntentClassifier) ClassifyEmbeddingOnly(msg MessageContext) ClassificationResult {
	cacheKey := classificationCacheKey(u.cacheEpoch.Load(), msg)
	if cached, ok := u.embOnlyCache.Load(cacheKey); ok {
		return *cached.(*ClassificationResult)
	}

	u.mu.RLock()
	emb := u.embedder
	anchors := u.anchors
	isReady := u.ready
	u.mu.RUnlock()

	isNoop := embedding.IsNoop(emb)
	if isNoop || !isReady {
		return ClassificationResult{
			Primary:    LabelUnknown,
			Confidence: 0.30,
			Layer:      0,
			Reason:     "embedding unavailable for fast classification",
			Degraded:   true,
		}
	}

	result, _ := classifyByEmbedding(emb, anchors, msg.Text)
	NormalizeDeclaredComposite(&result)
	applyExecutionAffordances(msg.Text, &result)
	result.ToolNames = u.affinity.Resolve(result.Primary, result.Secondary)
	if !result.Degraded {
		u.storeEmbOnly(cacheKey, &result)
	}
	return result
}

// ClassifyCached returns the full-fusion classification previously computed
// for msg in the current message cycle, if one exists. It is a pure cache
// read: it never starts a new embedding or LLM classification. Latency-bound
// consumers (e.g. tool routing) can reuse the main loop's earlier result
// instead of paying for their own tree/LLM call or failing closed when the
// embedding-only channel is degraded.
func (u *UnifiedIntentClassifier) ClassifyCached(msg MessageContext) (ClassificationResult, bool) {
	cacheKey := classificationCacheKey(u.cacheEpoch.Load(), msg)
	if cached, ok := u.cache.Load(cacheKey); ok {
		return *cached.(*ClassificationResult), true
	}
	return ClassificationResult{}, false
}

// embOnlyCacheMaxEntries bounds the embedding-only cache; when exceeded the
// whole map is dropped (cheap, and entries are per-message in practice).
const embOnlyCacheMaxEntries = 256

func (u *UnifiedIntentClassifier) storeEmbOnly(key string, res *ClassificationResult) {
	if u.embOnlyCount.Add(1) > embOnlyCacheMaxEntries {
		u.embOnlyCache.Range(func(k, _ any) bool {
			u.embOnlyCache.Delete(k)
			return true
		})
		u.embOnlyCount.Store(1)
	}
	u.embOnlyCache.Store(key, res)
}

// Ready returns true when Layer 2 anchor embeddings are warmed up.
func (u *UnifiedIntentClassifier) Ready() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.ready
}

// FusionTreeDeadline returns the dual-channel wait budget for the tree channel.
func (u *UnifiedIntentClassifier) FusionTreeDeadline() time.Duration {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.fusionTreeDeadline <= 0 {
		return DefaultFusionTreeDeadline
	}
	return u.fusionTreeDeadline
}

// SetFusionTreeDeadline updates how long dual-channel fusion waits for L3.
// Values <= 0 reset to DefaultFusionTreeDeadline. Capped by llmTimeout when set.
func (u *UnifiedIntentClassifier) SetFusionTreeDeadline(d time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if d <= 0 {
		d = DefaultFusionTreeDeadline
	}
	if u.llmTimeout > 0 && d > u.llmTimeout {
		d = u.llmTimeout
	}
	u.fusionTreeDeadline = d
}

// SetLLMFunc sets or replaces the Layer 3 LLM callback.
func (u *UnifiedIntentClassifier) SetLLMFunc(fn LLMClassifyFunc) {
	u.mu.Lock()
	u.llmFunc = fn
	u.cacheEpoch.Add(1)
	u.mu.Unlock()
	u.InvalidateCache()
}

// SetLLMContextFunc sets the cancellable Layer 3 callback.
func (u *UnifiedIntentClassifier) SetLLMContextFunc(fn LLMClassifyContextFunc) {
	u.mu.Lock()
	u.llmContextFunc = fn
	u.cacheEpoch.Add(1)
	u.mu.Unlock()
	u.InvalidateCache()
}

// SetEmbedder sets or replaces the Layer 2 embedder and triggers background
// anchor warmup. This enables late wiring: the UIC can be created with a
// noop embedder at startup (L1-only), then upgraded with a real embedder
// when the embedding model finishes loading.
func (u *UnifiedIntentClassifier) SetEmbedder(emb embedding.Embedder) {
	if emb == nil || embedding.IsNoop(emb) {
		return
	}

	// Rebuild anchors from definitions with the new embedder.
	defs := DefaultDefinitions()
	newAnchors := BuildAnchorsFromDefinitions(defs)

	u.mu.Lock()
	u.embedder = emb
	u.anchors = newAnchors
	u.ready = false // reset until new anchors are warmed up
	u.embeddingGeneration++
	generation := u.embeddingGeneration
	u.cacheEpoch.Add(1)
	u.mu.Unlock()
	u.InvalidateCache()

	go func() {
		warmed, err := warmupAnchors(emb, newAnchors)
		if err != nil {
			log.Printf("[UnifiedIntentClassifier] SetEmbedder: Layer 2 remains unavailable: %v", err)
			return
		}
		u.mu.Lock()
		if u.embeddingGeneration != generation {
			u.mu.Unlock()
			log.Printf("[UnifiedIntentClassifier] SetEmbedder: discarded stale anchor warmup generation=%d", generation)
			return
		}
		u.anchors = warmed
		u.ready = true
		u.cacheEpoch.Add(1)
		u.mu.Unlock()
		u.InvalidateCache()
		log.Println("[UnifiedIntentClassifier] SetEmbedder: anchor warmup complete, Layer 2 now available")
	}()
}

// DiagnoseScores returns all Layer 2 scores for debugging.
// No side effects, no caching.
func (u *UnifiedIntentClassifier) DiagnoseScores(text string) map[IntentLabel]float64 {
	scores := make(map[IntentLabel]float64)

	u.mu.RLock()
	emb := u.embedder
	anchors := u.anchors
	isReady := u.ready
	u.mu.RUnlock()

	if embedding.IsNoop(emb) || !isReady {
		return scores
	}

	queryVec, err := emb.Embed(text)
	if err != nil || queryVec == nil {
		return scores
	}

	for _, anchor := range anchors {
		if len(anchor.Vecs) == 0 {
			continue
		}
		maxSim := 0.0
		for _, vec := range anchor.Vecs {
			sim := cosineSimilarity(queryVec, vec)
			if sim > maxSim {
				maxSim = sim
			}
		}
		scores[anchor.Label] = maxSim
	}

	return scores
}

func classificationCacheKey(epoch uint64, msg MessageContext) string {
	// A classification result may eventually use conversation- or
	// principal-scoped semantic evidence. Keep cache scope aligned with that
	// boundary now, and length-prefix fields so user-controlled NUL bytes cannot
	// make distinct inputs collide.
	parts := append([]string{msg.UserID, msg.Text}, msg.RecentHistory...)
	var b strings.Builder
	fmt.Fprintf(&b, "%d", epoch)
	for _, part := range parts {
		fmt.Fprintf(&b, "\x00%d:", len(part))
		b.WriteString(part)
	}
	return b.String()
}

func fusionCacheKey(epoch uint64, text string) string {
	return fmt.Sprintf("%d\x00%s", epoch, text)
}

// cacheAndLog stores the result in cache and logs the decision.
func (u *UnifiedIntentClassifier) cacheAndLog(cacheKey, text string, result *ClassificationResult) {
	// Degraded results commonly represent cancellation, timeout, or a transient
	// provider outage. Caching them would let one aborted turn suppress a later
	// authoritative route for the same request scope.
	if !result.Degraded {
		u.cache.Store(cacheKey, result)
	}
	log.Printf("[UnifiedIntentClassifier] result: text_len=%d primary=%s conf=%.2f layer=%d reason=%s",
		len([]rune(text)), result.Primary, result.Confidence, result.Layer, result.Reason)
}

// truncateText truncates text to maxRunes for logging.
func truncateText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return fmt.Sprintf("%s...", string(runes[:maxRunes]))
}

// ---------------------------------------------------------------------------
// Dual-channel parallel fusion
// ---------------------------------------------------------------------------

// embeddingTopK returns the top-k embedding cosine similarity scores as
// labelScore pairs for the fusion pipeline. This is the raw signal from
// the embedding channel, without any confidence thresholding.
func (u *UnifiedIntentClassifier) embeddingTopK(text string, topK int) []labelScore {
	// Snapshot embedder and anchors under lock to avoid racing with SetEmbedder.
	u.mu.RLock()
	emb := u.embedder
	anchors := u.anchors
	u.mu.RUnlock()

	queryVec, err := emb.Embed(text)
	if err != nil || queryVec == nil {
		return nil
	}

	type scored struct {
		label IntentLabel
		score float64
	}
	var scores []scored

	for _, anchor := range anchors {
		if len(anchor.Vecs) == 0 {
			continue
		}
		maxSim := 0.0
		for _, vec := range anchor.Vecs {
			sim := cosineSimilarity(queryVec, vec)
			if sim > maxSim {
				maxSim = sim
			}
		}
		scores = append(scores, scored{anchor.Label, maxSim})
	}

	// Sort descending by score.
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Take top-k.
	if topK > 0 && len(scores) > topK {
		scores = scores[:topK]
	}

	result := make([]labelScore, len(scores))
	for i, s := range scores {
		result[i] = LabelScore(s.label, s.score)
	}
	return result
}

// classifyWithFusion runs L2 (embedding) and L3 (tree reasoning) in parallel,
// then fuses their results using the weighted formula from intent-fusion.
//
// This is the core dual-channel fusion pipeline:
//
//	L2 embedding (5ms)  ──┐
//	                      ├── MergeAndScore → Decide → FusionResult
//	L3 tree LLM (2-8s) ──┘
func (u *UnifiedIntentClassifier) classifyWithFusion(text string) FusionResult {
	t0 := time.Now()

	// Snapshot fusion config under lock (SetFusionConfig may be called concurrently).
	u.mu.RLock()
	fusionCfg := u.fusionCfg
	llmFn := u.llmFunc
	llmContextFn := u.llmContextFunc
	treeDeadline := u.fusionTreeDeadline
	u.mu.RUnlock()

	type embResult struct {
		scores []labelScore
		ms     float64
	}
	type treeResult struct {
		candidates []TreeCandidate
		ms         float64
		err        error
	}

	embCh := make(chan embResult, 1)
	treeCh := make(chan treeResult, 1)

	// L2: embedding channel (runs in goroutine).
	go func() {
		t := time.Now()
		scores := u.embeddingTopK(text, 5)
		embCh <- embResult{scores: scores, ms: float64(time.Since(t).Milliseconds())}
	}()

	// L3: tree reasoning channel (runs in goroutine). Cancelling this context
	// on the fusion deadline releases the HTTP request instead of leaving a
	// slow classifier occupying an LLM scheduler slot in the background.
	treeCtx, cancelTree := context.WithCancel(context.Background())
	defer cancelTree()
	go func() {
		t := time.Now()
		candidates, err := ClassifyByTreeContext(treeCtx, llmContextFn, llmFn, u.treeText, text)
		treeCh <- treeResult{candidates: candidates, ms: float64(time.Since(t).Milliseconds()), err: err}
	}()

	// Wait for embedding (fast, <100ms typically).
	emb := <-embCh

	// Wait for tree channel with the configurable fusion-only deadline (default 5s), NOT the
	// full 30s LLM timeout. Dual-channel fusion already has embedding; waiting for a
	// slow reasoning model only delays the control path. On timeout we degrade to
	// embedding-only (designed path) and cancel the context-aware transport.
	if treeDeadline <= 0 {
		treeDeadline = DefaultFusionTreeDeadline
	}
	var tree treeResult
	treeTimer := time.NewTimer(treeDeadline)
	select {
	case tree = <-treeCh:
		// Tree responded within deadline.
		if !treeTimer.Stop() {
			select {
			case <-treeTimer.C:
			default:
			}
		}
	case <-treeTimer.C:
		cancelTree()
		// Tree too slow — proceed with embedding only.
		tree = treeResult{err: fmt.Errorf("tree channel deadline exceeded (%s)", treeDeadline)}
	}

	embOK := len(emb.scores) > 0
	treeOK := tree.err == nil && len(tree.candidates) > 0

	if !embOK && !treeOK {
		log.Printf("[UnifiedIntentClassifier] fusion: both channels failed")
		return FusionResult{
			Verdict:        VerdictLow,
			TotalMs:        float64(time.Since(t0).Milliseconds()),
			Degraded:       true,
			ActiveChannels: []string{},
		}
	}

	// Determine effective alpha based on channel availability.
	alpha := fusionCfg.Alpha
	var activeChannels []string
	degraded := false

	if embOK && treeOK {
		activeChannels = []string{"embedding", "tree"}
	} else if embOK {
		alpha = 1.0
		activeChannels = []string{"embedding"}
		degraded = true
		log.Printf("[UnifiedIntentClassifier] fusion degraded: tree channel failed: %v", tree.err)
	} else {
		alpha = 0.0
		activeChannels = []string{"tree"}
		degraded = true
		log.Printf("[UnifiedIntentClassifier] fusion degraded: embedding channel returned no scores")
	}

	// Convert tree candidates to labelScore pairs and extract workflow types.
	var treeScores []labelScore
	var treeWorkflowTypes map[IntentLabel]string
	for _, c := range tree.candidates {
		treeScores = append(treeScores, LabelScore(c.Label, c.Score))
		if c.WorkflowType != "" {
			if treeWorkflowTypes == nil {
				treeWorkflowTypes = make(map[IntentLabel]string)
			}
			treeWorkflowTypes[c.Label] = c.WorkflowType
		}
	}

	// Merge and score.
	candidates := MergeAndScore(emb.scores, treeScores, alpha, treeWorkflowTypes)
	result := Decide(candidates, fusionCfg)
	result.EmbMs = emb.ms
	result.TreeMs = tree.ms
	result.TotalMs = float64(time.Since(t0).Milliseconds())
	result.Degraded = degraded
	result.ActiveChannels = activeChannels

	if len(candidates) > 0 {
		runnerName := "-"
		runnerScore := 0.0
		if result.RunnerUp != nil {
			runnerName = string(result.RunnerUp.Label)
			runnerScore = result.RunnerUp.FinalScore
		}
		log.Printf("[UnifiedIntentClassifier] fusion: verdict=%s top=%s(%.3f) runner=%s(%.3f) "+
			"channels=%v emb=%.0fms tree=%.0fms total=%.0fms",
			result.Verdict, result.Top.Label, result.Top.FinalScore,
			runnerName, runnerScore,
			activeChannels, emb.ms, tree.ms, result.TotalMs)
	}

	return result
}

// fusionToClassification converts a FusionResult to a ClassificationResult,
// maintaining backward compatibility with all existing consumers.
//
// WorkflowType is extracted from the winning candidate's tree channel data.
// This eliminates the need for a separate IUM LLM call to determine workflow type.
//
// Local lexical results are not used as fallback. When fusion produces a low
// confidence result, we return Ambiguous rather than guessing from wording.
func (u *UnifiedIntentClassifier) fusionToClassification(fr FusionResult) ClassificationResult {
	// Both channels failed; return ambiguous.
	if fr.Verdict == VerdictLow && len(fr.ActiveChannels) == 0 {
		return ClassificationResult{
			Primary:    LabelAmbiguous,
			Confidence: 0,
			Layer:      0,
			Reason:     "fusion: both channels failed, no confident classification",
		}
	}

	result := ClassificationResult{
		Primary:      fr.Top.Label,
		Confidence:   fr.Top.FinalScore,
		Layer:        23, // indicates fusion of L2+L3
		WorkflowType: fr.Top.WorkflowType,
		Degraded:     fr.Degraded,
	}

	// --- Degraded-mode WorkflowType inference ---
	// When the L3 tree channel fails (timeout/error), WorkflowType is empty
	// because it's only populated by tree reasoning. But the IntentDefinition
	// data already declares which labels map to which workflow types. If the
	// winning label has exactly one known workflow type, we can infer it from
	// the definition — no LLM needed.
	//
	// This is the key mechanism that prevents workflow bypass when LLMs are
	// unavailable: embedding-only mode can still produce WorkflowType="coding"
	// for a confident LabelCoding classification.
	//
	// Only applied when:
	//   1. WorkflowType is empty (tree didn't provide it)
	//   2. The tree channel was NOT active — i.e., it failed or was unavailable.
	//      When the tree channel IS active and returned empty WorkflowType,
	//      that's a deliberate decision (e.g., "修改函数返回值" is coding but
	//      not creation-oriented). We must not override the tree's judgment.
	//   3. Verdict is not LOW (we trust the label enough)
	//   4. The label has exactly one known workflow type (unambiguous mapping)
	if result.WorkflowType == "" && fr.Verdict != VerdictLow && !fr.Top.InTree {
		if wfType, ok := u.fusionCfg.WorkflowTypeMap[result.Primary]; ok {
			result.WorkflowType = wfType
			log.Printf("[UnifiedIntentClassifier] WorkflowType inferred from definition: "+
				"label=%s → workflow_type=%q (tree channel %s)",
				result.Primary, wfType, degradedReason(fr))
		}
	}

	// CreationOriented is determined by the fusion result itself.
	// When the tree channel classifies as coding with workflow_type="coding",
	// that's a creation-oriented task. Bug-fix and maintenance intents from
	// the tree channel don't set CreationOriented.
	//
	// Also set when WorkflowType was inferred from definition (degraded mode).
	if result.Primary == LabelCoding && result.WorkflowType == "coding" {
		result.CreationOriented = true
	}

	switch fr.Verdict {
	case VerdictClear:
		result.Reason = fmt.Sprintf("fusion-clear: %s (%.3f)", fr.Top.Label, fr.Top.FinalScore)
	case VerdictAmbiguous:
		if fr.RunnerUp != nil {
			result.Reason = fmt.Sprintf("fusion-ambiguous: %s (%.3f) vs %s (%.3f)",
				fr.Top.Label, fr.Top.FinalScore,
				fr.RunnerUp.Label, fr.RunnerUp.FinalScore)
			result.Secondary = []IntentLabel{fr.RunnerUp.Label}
		} else {
			result.Reason = fmt.Sprintf("fusion-ambiguous: %s (%.3f)", fr.Top.Label, fr.Top.FinalScore)
		}
	case VerdictLow:
		// Low confidence — return ambiguous. Do not fall back to local rules.
		result.Primary = LabelAmbiguous
		result.WorkflowType = "" // low confidence → don't trust workflow type
		result.Reason = fmt.Sprintf("fusion-low: top=%s (%.3f) below threshold", fr.Top.Label, fr.Top.FinalScore)
	}

	return result
}

// SetFusionConfig updates the fusion parameters (alpha, delta, lowThreshold).
// Useful for runtime calibration or A/B testing. Thread-safe.
func (u *UnifiedIntentClassifier) SetFusionConfig(cfg FusionConfig) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fusionCfg = cfg
}

// degradedReason returns a human-readable description of why the fusion was degraded.
func degradedReason(fr FusionResult) string {
	if !fr.Degraded {
		return "not degraded"
	}
	for _, ch := range []string{"embedding", "tree"} {
		found := false
		for _, active := range fr.ActiveChannels {
			if active == ch {
				found = true
				break
			}
		}
		if !found {
			return ch + " failed"
		}
	}
	return "degraded"
}

// GetFusionConfig returns the current fusion parameters. Thread-safe.
func (u *UnifiedIntentClassifier) GetFusionConfig() FusionConfig {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.fusionCfg
}

// LastFusionResult returns the FusionResult for the given text if it was
// classified via the fusion path. Returns nil if the text was classified
// via L1 fast path or is not in cache. Used for diagnostics.
func (u *UnifiedIntentClassifier) LastFusionResult(text string) *FusionResult {
	if cached, ok := u.fusionCache.Load(fusionCacheKey(u.cacheEpoch.Load(), text)); ok {
		fr := cached.(FusionResult)
		return &fr
	}
	return nil
}

// IsWorkflowCandidate returns true if the given label could potentially
// trigger a multi-phase workflow. Derived from IntentDefinition.MayTriggerWorkflow
// at construction time. Labels NOT in this set are definitively non-workflow.
//
// Used by the workflow interception chain to fast-reject non-workflow intents
// before calling IntentUnderstandingManager (which makes an expensive LLM call).
func (u *UnifiedIntentClassifier) IsWorkflowCandidate(label IntentLabel) bool {
	return u.workflowCandidates[label]
}

// GetWorkflowRejectThreshold returns the minimum UIC confidence required to
// fast-reject a non-workflow intent before calling IntentUnderstandingManager.
// Sourced from FusionConfig, tunable via SetFusionConfig or offline calibration.
func (u *UnifiedIntentClassifier) GetWorkflowRejectThreshold() float64 {
	u.mu.RLock()
	defer u.mu.RUnlock()
	t := u.fusionCfg.WorkflowRejectThreshold
	if t <= 0 {
		return DefaultWorkflowRejectThreshold
	}
	return t
}
