package intent

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Config holds initialization parameters for the UnifiedIntentClassifier.
type Config struct {
	Embedder   embedding.Embedder
	LLMFunc    LLMClassifyFunc   // optional, can be nil
	LLMTimeout time.Duration     // 0 → default 8s
}

// UnifiedIntentClassifier is the single entry point for all user-intent
// classification. It implements a dual-channel fusion pipeline inspired by
// intent-fusion (https://github.com/Liyuan1992/intent-fusion):
//
//	Layer 1: keyword rules (fast path, <1ms) — confident hits return immediately
//	Layer 2: embedding cosine similarity (~5ms) — parallel with Layer 3
//	Layer 3: LLM intent tree reasoning (~2-8s) — parallel with Layer 2
//	Fusion:  α·emb + (1-α)·tree → three-state verdict (CLEAR/AMBIGUOUS/LOW)
//
// When both L2 and L3 are available, they run in parallel and their results
// are fused using a weighted formula. When only one channel is available,
// the system degrades gracefully (α forced to 0 or 1).
type UnifiedIntentClassifier struct {
	registry   *KeywordRegistry
	affinity   *ToolAffinityRegistry
	embedder   embedding.Embedder
	anchors    []intentAnchor
	llmFunc    LLMClassifyFunc
	llmTimeout time.Duration

	// Intent Tree text for Layer 3 tree reasoning (pre-built from definitions).
	treeText  string
	// Flat LLM prompt for Layer 3 single-channel fallback (pre-built from definitions).
	llmPrompt string
	fusionCfg FusionConfig

	// Per-message cache: cleared after each message processing cycle.
	cache      sync.Map // map[string]*ClassificationResult
	fusionCache sync.Map // map[string]FusionResult — stores fusion details for diagnostics

	// workflowCandidates is the set of IntentLabels that may trigger a
	// multi-phase workflow, derived from IntentDefinition.MayTriggerWorkflow.
	// Pre-computed at construction time. Labels NOT in this set are
	// definitively non-workflow and can be fast-rejected by consumers.
	workflowCandidates map[IntentLabel]bool

	ready bool         // set to true when anchor warmup completes
	mu    sync.RWMutex // protects ready and llmFunc
}

// New creates a UnifiedIntentClassifier. Starts background anchor warmup
// if the embedder is not a NoopEmbedder.
func New(cfg Config) *UnifiedIntentClassifier {
	timeout := cfg.LLMTimeout
	if timeout == 0 {
		timeout = 8 * time.Second
	}

	// Build intent tree text from unified definitions for Layer 3 tree reasoning.
	defs := DefaultDefinitions()
	treeText := BuildIntentTreeText(defs)

	// Use FullDefinitions (with keywords populated) for definitions-based constructors.
	fullDefs := FullDefinitions()

	u := &UnifiedIntentClassifier{
		registry:           NewKeywordRegistryFromDefinitions(fullDefs),
		affinity:           NewToolAffinityRegistryFromDefinitions(fullDefs),
		embedder:           cfg.Embedder,
		anchors:            BuildAnchorsFromDefinitions(defs),
		llmFunc:            cfg.LLMFunc,
		llmTimeout:         timeout,
		treeText:           treeText,
		llmPrompt:          buildLLMSystemPrompt(defs),
		fusionCfg:          DefaultFusionConfig(),
		workflowCandidates: WorkflowCandidateLabels(defs),
	}

	// Determine available layers and log.
	isNoop := embedding.IsNoop(cfg.Embedder)
	layers := []string{"Layer 1 (keywords)"}
	if !isNoop {
		layers = append(layers, "Layer 2 (embedding)")
	}
	if cfg.LLMFunc != nil {
		layers = append(layers, "Layer 3 (LLM)")
	}
	log.Printf("[UnifiedIntentClassifier] available layers: %v", layers)

	// Start background anchor warmup if embedder is real.
	if !isNoop {
		go func() {
			warmupAnchors(u.embedder, u.anchors)
			u.mu.Lock()
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

// Classify returns the ClassificationResult for the given message.
// Results are cached per message text; subsequent calls with the same
// text return the cached result without recomputation.
//
// Execution model:
//  1. Layer 1 (keywords): always runs, provides prior signal for tool affinity
//     and CreationOriented detection. NEVER used for intent classification decisions.
//  2. If both L2 (embedding) and L3 (LLM) are available → run in parallel,
//     fuse results using α·emb + (1-α)·tree, apply three-state verdict.
//  3. If only L2 available → α forced to 1.0 (embedding only).
//  4. If only L3 available → α forced to 0.0 (tree only).
//  5. If neither available → return L1 result (degraded mode only).
//
// L1 keywords NEVER override fusion results. When fusion produces a low
// confidence verdict, the result is Ambiguous — not a fallback to L1.
// This prevents keyword misclassification from overriding correct semantic
// analysis (e.g., "基于文档生成PPT" being classified as non_coding by
// keywords because "基于" looks like content processing).
func (u *UnifiedIntentClassifier) Classify(msg MessageContext) ClassificationResult {
	// Check cache first.
	if cached, ok := u.cache.Load(msg.Text); ok {
		return *cached.(*ClassificationResult)
	}

	// Determine which layers are available.
	isNoop := embedding.IsNoop(u.embedder)
	u.mu.RLock()
	hasLLM := u.llmFunc != nil
	isReady := u.ready
	u.mu.RUnlock()

	canEmb := !isNoop && isReady
	canTree := hasLLM

	if !isNoop && !isReady {
		log.Printf("[UnifiedIntentClassifier] Layer 2 skipped: anchors not ready")
	}

	// Dual-channel parallel fusion when both channels are available.
	if canEmb && canTree {
		fusionResult := u.classifyWithFusion(msg.Text)
		u.fusionCache.Store(msg.Text, fusionResult) // store for diagnostics
		bestResult := u.fusionToClassification(fusionResult)
		bestResult.ToolNames = u.affinity.Resolve(bestResult.Primary, bestResult.Secondary)
		u.cacheAndLog(msg.Text, &bestResult)
		return bestResult
	}

	// Single-channel fallback: embedding only.
	if canEmb {
		l2Result, _ := classifyByEmbedding(u.embedder, u.anchors, msg.Text)
		l2Result.ToolNames = u.affinity.Resolve(l2Result.Primary, l2Result.Secondary)
		u.cacheAndLog(msg.Text, &l2Result)
		return l2Result
	}

	// Single-channel fallback: LLM only (tree reasoning).
	if canTree {
		u.mu.RLock()
		llmFn := u.llmFunc
		u.mu.RUnlock()

		candidates, err := ClassifyByTree(llmFn, u.treeText, msg.Text)
		if err == nil && len(candidates) > 0 {
			top := candidates[0]
			bestResult := ClassificationResult{
				Primary:      top.Label,
				Confidence:   top.Score,
				Layer:        3,
				Reason:       fmt.Sprintf("tree-only: %s (%.3f)", top.Label, top.Score),
				WorkflowType: top.WorkflowType,
			}
			if top.Label == LabelCoding && top.WorkflowType == "coding" {
				bestResult.CreationOriented = true
			}
			bestResult.ToolNames = u.affinity.Resolve(bestResult.Primary, bestResult.Secondary)
			u.cacheAndLog(msg.Text, &bestResult)
			return bestResult
		}
		log.Printf("[UnifiedIntentClassifier] Layer 3 failed: %v, falling back to L1 keywords (degraded)", err)
	}

	// Degraded mode: neither L2 nor L3 available (or L3 failed).
	// L1 keyword classification is the last resort.
	l1Result, _ := classifyByKeywords(u.registry, u.affinity, msg)
	l1Result.ToolNames = u.affinity.Resolve(l1Result.Primary, l1Result.Secondary)
	u.cacheAndLog(msg.Text, &l1Result)
	return l1Result
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
}

// Ready returns true when Layer 2 anchor embeddings are warmed up.
func (u *UnifiedIntentClassifier) Ready() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.ready
}

// SetLLMFunc sets or replaces the Layer 3 LLM callback.
func (u *UnifiedIntentClassifier) SetLLMFunc(fn LLMClassifyFunc) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.llmFunc = fn
}

// DiagnoseScores returns all Layer 2 scores for debugging.
// No side effects, no caching.
func (u *UnifiedIntentClassifier) DiagnoseScores(text string) map[IntentLabel]float64 {
	scores := make(map[IntentLabel]float64)

	if embedding.IsNoop(u.embedder) {
		return scores
	}

	u.mu.RLock()
	isReady := u.ready
	u.mu.RUnlock()

	if !isReady {
		return scores
	}

	queryVec, err := u.embedder.Embed(text)
	if err != nil || queryVec == nil {
		return scores
	}

	for _, anchor := range u.anchors {
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

// cacheAndLog stores the result in cache and logs the decision.
func (u *UnifiedIntentClassifier) cacheAndLog(text string, result *ClassificationResult) {
	u.cache.Store(text, result)
	log.Printf("[UnifiedIntentClassifier] result: text=%q primary=%s conf=%.2f layer=%d reason=%s",
		truncateText(text, 30), result.Primary, result.Confidence, result.Layer, result.Reason)
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
	queryVec, err := u.embedder.Embed(text)
	if err != nil || queryVec == nil {
		return nil
	}

	type scored struct {
		label IntentLabel
		score float64
	}
	var scores []scored

	for _, anchor := range u.anchors {
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

	// L3: tree reasoning channel (runs in goroutine).
	go func() {
		t := time.Now()
		candidates, err := ClassifyByTree(llmFn, u.treeText, text)
		treeCh <- treeResult{candidates: candidates, ms: float64(time.Since(t).Milliseconds()), err: err}
	}()

	// Wait for both channels.
	emb := <-embCh
	tree := <-treeCh

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
// L1 keyword results are NOT used as fallback — when fusion produces a low
// confidence result, we return Ambiguous rather than falling back to keyword
// classification. Keywords lack semantic understanding and can override correct
// fusion decisions (e.g., classifying "基于文档生成PPT" as non_coding because
// "基于" looks like content processing).
func (u *UnifiedIntentClassifier) fusionToClassification(fr FusionResult) ClassificationResult {
	// Both channels failed — return ambiguous.
	if fr.Verdict == VerdictLow && len(fr.ActiveChannels) == 0 {
		return ClassificationResult{
			Primary:    LabelAmbiguous,
			Confidence: 0,
			Layer:      1,
			Reason:     "fusion: both channels failed, no confident classification",
		}
	}

	result := ClassificationResult{
		Primary:      fr.Top.Label,
		Confidence:   fr.Top.FinalScore,
		Layer:        23, // indicates fusion of L2+L3
		WorkflowType: fr.Top.WorkflowType,
	}

	// CreationOriented is determined by the fusion result itself, not L1 keywords.
	// When the tree channel classifies as coding with workflow_type="coding",
	// that's a creation-oriented task. Bug-fix and maintenance intents from
	// the tree channel don't set CreationOriented.
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
		// Low confidence — return ambiguous. Do NOT fall back to L1 keywords.
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
	if cached, ok := u.fusionCache.Load(text); ok {
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
