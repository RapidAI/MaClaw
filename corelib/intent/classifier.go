package intent

import (
	"fmt"
	"log"
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
// classification. It implements a three-layer pipeline:
//
//	Layer 1: keyword rules (fast path, <1ms)
//	Layer 2: embedding cosine similarity (~5ms)
//	Layer 3: LLM refinement (up to LLMTimeout)
type UnifiedIntentClassifier struct {
	registry   *KeywordRegistry
	affinity   *ToolAffinityRegistry
	embedder   embedding.Embedder
	anchors    []intentAnchor
	llmFunc    LLMClassifyFunc
	llmTimeout time.Duration

	// Per-message cache: cleared after each message processing cycle.
	cache sync.Map // map[string]*ClassificationResult

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

	u := &UnifiedIntentClassifier{
		registry:   NewKeywordRegistry(),
		affinity:   NewToolAffinityRegistry(),
		embedder:   cfg.Embedder,
		anchors:    defaultAnchors(),
		llmFunc:    cfg.LLMFunc,
		llmTimeout: timeout,
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

	var bestResult ClassificationResult

	// Layer 1: keyword classification.
	l1Result, l1Confident := classifyByKeywords(u.registry, u.affinity, msg)
	bestResult = l1Result

	if l1Confident {
		u.cacheAndLog(msg.Text, &bestResult)
		return bestResult
	}

	// Log Layer 1 escalation.
	log.Printf("[UnifiedIntentClassifier] Layer 1 escalating: %s (conf=%.2f, reason=%s)",
		truncateText(msg.Text, 30), l1Result.Confidence, l1Result.Reason)

	// Layer 2: embedding cosine similarity.
	if !isNoop && isReady {
		l2Result, l2Confident := classifyByEmbedding(u.embedder, u.anchors, msg.Text)
		if l2Confident {
			bestResult = l2Result
			bestResult.ToolNames = u.affinity.Resolve(bestResult.Primary, bestResult.Secondary)
			u.cacheAndLog(msg.Text, &bestResult)
			return bestResult
		}
		// Keep the better result between L1 and L2.
		if l2Result.Confidence > bestResult.Confidence {
			bestResult = l2Result
		}
		// Log Layer 2 escalation.
		log.Printf("[UnifiedIntentClassifier] Layer 2 escalating: %s (conf=%.2f, reason=%s)",
			truncateText(msg.Text, 30), l2Result.Confidence, l2Result.Reason)
	} else if !isNoop && !isReady {
		log.Printf("[UnifiedIntentClassifier] Layer 2 skipped: anchors not ready")
	}

	// Layer 3: LLM refinement.
	if hasLLM {
		u.mu.RLock()
		llmFn := u.llmFunc
		u.mu.RUnlock()

		l3Result, err := classifyByLLM(llmFn, msg)
		if err == nil {
			// Log Layer 3 override if it differs from lower layer.
			if l3Result.Primary != bestResult.Primary {
				log.Printf("[UnifiedIntentClassifier] Layer 3 override: %s → %s (conf=%.2f)",
					bestResult.Primary, l3Result.Primary, l3Result.Confidence)
			}
			bestResult = l3Result
			bestResult.ToolNames = u.affinity.Resolve(bestResult.Primary, bestResult.Secondary)
			u.cacheAndLog(msg.Text, &bestResult)
			return bestResult
		}
		// LLM failed — fall back to best available lower-layer result.
		log.Printf("[UnifiedIntentClassifier] Layer 3 failed: %v, using lower-layer result", err)
	}

	// Fall back to best available lower-layer result.
	bestResult.ToolNames = u.affinity.Resolve(bestResult.Primary, bestResult.Secondary)
	u.cacheAndLog(msg.Text, &bestResult)
	return bestResult
}

// InvalidateCache clears the per-message cache. Called once per message
// processing cycle by the consumer (e.g., after IMMessageHandler finishes).
func (u *UnifiedIntentClassifier) InvalidateCache() {
	u.cache.Range(func(key, _ any) bool {
		u.cache.Delete(key)
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
