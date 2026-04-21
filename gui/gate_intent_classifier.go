package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// gateContPhrases are known short continuation phrases that indicate the user
// wants to start or continue a previously discussed task. Used by the
// short-message continuation detection in Classify.
var gateContPhrases = []string{
	"继续", "开工", "开干", "动手", "搞起来", "搞起", "干吧", "做吧",
	"开始吧", "开始做", "开始干", "开始搞",
	"let's go", "start working", "go ahead", "continue",
	"start", "begin", "go", "ok", "好的", "嗯",
}

// gateClassifierSystemPrompt is the system prompt for the Layer 3 LLM
// refinement call. It instructs the LLM to classify user messages into one
// of five gate-specific categories with structured JSON output.
const gateClassifierSystemPrompt = `你是一个编程任务分类器，负责判断用户请求属于哪种编程工作流类别。

分类目标：
- new_project：创建新应用、新功能、新工具、新游戏、新系统（需要走需求→设计→任务分解流程）
- bug_fix：修复bug、调试、排查错误、解决崩溃/白屏/闪退/卡住等问题（直接修复，不需要三阶段流程）
- maintenance：重构代码、优化性能、清理代码、升级依赖、改善结构（直接执行，不需要三阶段流程）
- non_coding：翻译、整理资料、搜索论文、总结文章、生成报告等非编程任务
- continuation：用户想继续之前讨论的任务（如"开工"、"继续"、"动手"）

规则：
- 如果消息同时包含创建和修复信号（如"开发一个bug追踪系统"），判为 new_project（主要意图是创建）
- 如果消息同时包含修复和维护信号（如"修复bug然后重构"），判为 bug_fix（主要动作是修复）
- 如果消息同时包含编码和非编码信号（如"翻译代码注释"），判为 non_coding（主要动作是翻译）
- 信息不足时输出 unknown
- 只输出 JSON，不要输出任何额外解释

输出格式：
{"gate_intent": "...", "confidence": 0.0-1.0, "reason": "..."}`

// gateClassifierJSONSchema defines the expected JSON output schema for the
// LLM gate classification response. Used with OpenAI-compatible structured
// output (response_format).
var gateClassifierJSONSchema = map[string]interface{}{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]interface{}{
		"gate_intent": map[string]interface{}{
			"type": "string",
			"enum": []string{"new_project", "bug_fix", "maintenance",
				"non_coding", "continuation", "unknown"},
		},
		"confidence": map[string]interface{}{
			"type": "number", "minimum": 0, "maximum": 1,
		},
		"reason": map[string]interface{}{"type": "string"},
	},
	"required": []string{"gate_intent", "confidence", "reason"},
}

// ---------------------------------------------------------------------------
// Gate Intent Classifier — semantic, multi-layer classification for the
// Coding Tool Gate decision.
//
// Replaces the keyword-only approach with a three-layer pipeline:
//   Layer 1: keyword rules (fast path, <1ms)
//   Layer 2: embedding cosine similarity against gate-specific anchors (<500ms)
//   Layer 3: LLM refinement for ambiguous cases (3s timeout)
//
// Five gate-specific categories:
//   new_project   — activate three-phase workflow
//   bug_fix       — bypass gate, execute directly
//   maintenance   — bypass gate, execute directly
//   non_coding    — bypass gate, not a coding task
//   continuation  — bypass gate, continue previous task
//   unknown       — fallback / low confidence
// ---------------------------------------------------------------------------

// GateIntent represents the five-category classification result for the Gate.
type GateIntent string

const (
	GateIntentNewProject   GateIntent = "new_project"
	GateIntentBugFix       GateIntent = "bug_fix"
	GateIntentMaintenance  GateIntent = "maintenance"
	GateIntentNonCoding    GateIntent = "non_coding"
	GateIntentContinuation GateIntent = "continuation"
	GateIntentUnknown      GateIntent = "unknown"
)

// GateIntentResult holds the classification output from GateIntentClassifier.
type GateIntentResult struct {
	Intent     GateIntent             // one of the GateIntent constants
	Confidence float64                // [0, 1] — higher means more certain
	Gap        float64                // score gap between top-1 and top-2 category
	Layer      int                    // 1=keyword, 2=embedding, 3=LLM
	Reason     string                 // human-readable explanation
	AllScores  map[GateIntent]float64 // diagnostic: scores for all five categories
}

// ConversationContextProvider abstracts access to recent conversation history
// for continuation detection. Implemented by IMMessageHandler.
type ConversationContextProvider interface {
	// RecentMessages returns the last N messages for the given user.
	RecentMessages(userID string, n int) []string
}

// gateAnchor groups anchor texts and their pre-computed embeddings for one
// gate intent category.
type gateAnchor struct {
	Intent GateIntent
	Texts  []string
	Vecs   [][]float32
}

// GateIntentClassifier performs three-layer gate intent classification:
//
//	Layer 1: keyword rules (fast path)
//	Layer 2: embedding cosine similarity against gate-specific anchors
//	Layer 3: LLM refinement for ambiguous cases
type GateIntentClassifier struct {
	embedder           embedding.Embedder
	anchors            []gateAnchor
	queryCache         *tool.QueryEmbeddingCache
	ctxProvider        ConversationContextProvider
	llmConfig          func() MaclawLLMConfig // lazy access to LLM config
	httpClient         *http.Client
	unifiedClassifier  *intent.UnifiedIntentClassifier
	ready              bool
	mu                 sync.RWMutex
}

// NewGateIntentClassifier creates a new gate intent classifier. If emb is nil
// or a NoopEmbedder, only Layer 1 (keyword rules) is available. A background
// goroutine pre-computes anchor embeddings; Ready() returns true once complete.
func NewGateIntentClassifier(emb embedding.Embedder) *GateIntentClassifier {
	g := &GateIntentClassifier{
		embedder: emb,
		anchors:  gateAnchors(),
	}
	if emb == nil || embedding.IsNoop(emb) {
		return g
	}
	g.queryCache = tool.NewQueryEmbeddingCache(emb, 64, 30*time.Second)
	go func() {
		for i := range g.anchors {
			vecs, err := emb.EmbedBatch(g.anchors[i].Texts)
			if err != nil {
				log.Printf("[GateIntentClassifier] warmup failed for %s: %v", g.anchors[i].Intent, err)
				return
			}
			g.anchors[i].Vecs = vecs
		}
		g.mu.Lock()
		g.ready = true
		g.mu.Unlock()
	}()
	return g
}

// SetContextProvider sets the conversation context provider for continuation
// detection.
func (g *GateIntentClassifier) SetContextProvider(p ConversationContextProvider) {
	g.ctxProvider = p
}

// SetLLMConfig sets the lazy LLM config accessor and HTTP client for Layer 3
// refinement.
func (g *GateIntentClassifier) SetLLMConfig(cfgFn func() MaclawLLMConfig, client *http.Client) {
	g.llmConfig = cfgFn
	g.httpClient = client
}

// Ready returns true when anchor embeddings have been pre-computed and the
// classifier can perform Layer 2 (embedding) classification.
func (g *GateIntentClassifier) Ready() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.ready
}

// SetUnifiedClassifier sets the UIC instance. When non-nil, Classify()
// delegates to the UIC and maps the result to GateIntentResult via
// ToGateIntent(), bypassing the local three-layer pipeline.
func (g *GateIntentClassifier) SetUnifiedClassifier(uic *intent.UnifiedIntentClassifier) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.unifiedClassifier = uic
}

// Classify determines the gate intent for a user message. userID is used for
// conversation context lookup (continuation detection).
//
// Layer 1 (keyword rules) is attempted first. If it produces a high-confidence
// result (≥ 0.90), the result is returned immediately. Otherwise, the result
// falls through to Layer 2/3 (implemented in later tasks).
func (g *GateIntentClassifier) Classify(text string, userID string) GateIntentResult {
	// --- UIC delegation: when the Unified Intent Classifier is available,
	// delegate to it and map the result to GateIntentResult. ---
	g.mu.RLock()
	uic := g.unifiedClassifier
	g.mu.RUnlock()
	if uic != nil {
		uicResult := uic.Classify(intent.MessageContext{
			Text:   text,
			UserID: userID,
		})
		gateIntent, confidence, gap, layer, reason := uicResult.ToGateIntent()
		result := GateIntentResult{
			Intent:     GateIntent(gateIntent),
			Confidence: confidence,
			Gap:        gap,
			Layer:      layer,
			Reason:     reason,
		}
		log.Printf("[GateIntentClassifier] classify(%q): UIC delegation intent=%s conf=%.2f layer=%d",
			text, result.Intent, result.Confidence, result.Layer)
		return result
	}

	// --- Fallback: local three-layer pipeline when UIC is nil ---

	// --- Short-message continuation detection (before keyword classification) ---
	// Detect short messages (≤10 runes covers both ≤4 Chinese chars and ≤10
	// English chars since Chinese characters are 1 rune each) and check if
	// they match known continuation phrases with conversation context.
	trimmed := strings.TrimSpace(text)
	lowConfContinuation := false // tracks whether we saw a continuation phrase without coding context
	if utf8.RuneCountInString(trimmed) <= 10 && trimmed != "" {
		lower := strings.ToLower(trimmed)
		if matchesContinuationPhrase(lower) {
			// Check conversation context for coding signals.
			hasCodingCtx := false
			if g.ctxProvider != nil {
				msgs := g.ctxProvider.RecentMessages(userID, 10)
				hasCodingCtx = hasCodingSignals(msgs)
			}
			if hasCodingCtx {
				result := GateIntentResult{
					Intent:     GateIntentContinuation,
					Confidence: 0.65,
					Layer:      1,
					Reason:     "short continuation phrase with coding context in conversation history",
				}
				log.Printf("[GateIntentClassifier] continuation detected(%q): codingContext=true conf=%.2f", text, result.Confidence)
				return result
			}
			// No coding context (or no context provider) — low confidence.
			// Don't return immediately; fall through to Layer 2/3 so
			// embedding/LLM can attempt a better classification.
			// (Requirement 6.4: defer to embedding/LLM layers)
			lowConfContinuation = true
			log.Printf("[GateIntentClassifier] continuation detected(%q): codingContext=false conf=0.40, deferring to layer 2/3", text)
		}
		// Short message but not a continuation phrase — fall through to
		// keyword classification below.
	}

	// Layer 1: keyword-based classification (fast path).
	if result, ok := g.classifyByKeywords(text); ok {
		log.Printf("[GateIntentClassifier] classify(%q): layer=1 intent=%s conf=%.2f reason=%s",
			text, result.Intent, result.Confidence, result.Reason)
		return result
	}

	// Layer 2: embedding cosine similarity (when ready).
	var layer2Result GateIntentResult
	var hasLayer2 bool
	if g.Ready() {
		if result, ok := g.classifyByEmbedding(text); ok {
			log.Printf("[GateIntentClassifier] classify(%q): layer=2 intent=%s conf=%.2f gap=%.2f reason=%s",
				text, result.Intent, result.Confidence, result.Gap, result.Reason)
			logTop2Scores(text, result.AllScores)
			return result
		} else if result.Intent != "" {
			// Layer 2 returned an ambiguous result — save it for fallback.
			layer2Result = result
			hasLayer2 = true
			log.Printf("[GateIntentClassifier] classify(%q): layer=2 ambiguous intent=%s conf=%.2f gap=%.2f, escalating to layer 3",
				text, result.Intent, result.Confidence, result.Gap)
			logTop2Scores(text, result.AllScores)
		}
	}

	// Layer 3: LLM refinement for ambiguous cases.
	if g.llmConfig != nil && g.httpClient != nil {
		if llmResult, err := g.classifyGateIntentWithLLM(text); err == nil && llmResult.Confidence >= 0.60 {
			// Log when LLM result overrides Layer 2 result.
			if hasLayer2 && llmResult.Intent != layer2Result.Intent {
				log.Printf("[GateIntentClassifier] classify(%q): layer=3 overrides layer=2: semantic=%s(%.2f) -> llm=%s(%.2f)",
					text, layer2Result.Intent, layer2Result.Confidence, llmResult.Intent, llmResult.Confidence)
			} else {
				log.Printf("[GateIntentClassifier] classify(%q): layer=3 intent=%s conf=%.2f reason=%s",
					text, llmResult.Intent, llmResult.Confidence, llmResult.Reason)
			}
			return llmResult
		}
	}

	// Fall back to Layer 2 result if available and better than baseline.
	if hasLayer2 && layer2Result.Confidence > 0.30 {
		log.Printf("[GateIntentClassifier] classify(%q): fallback to layer=2 intent=%s conf=%.2f gap=%.2f",
			text, layer2Result.Intent, layer2Result.Confidence, layer2Result.Gap)
		return layer2Result
	}

	// No strong match from any layer.
	// If we detected a continuation phrase earlier but had no coding context,
	// return it as low-confidence continuation (Req 6.4) so the caller knows
	// the phrase was recognized but ambiguous.
	if lowConfContinuation {
		result := GateIntentResult{
			Intent:     GateIntentContinuation,
			Confidence: 0.40,
			Layer:      1,
			Reason:     "short continuation phrase, no coding context, layer 2/3 did not override",
		}
		log.Printf("[GateIntentClassifier] classify(%q): final fallback to low-conf continuation intent=%s conf=%.2f",
			text, result.Intent, result.Confidence)
		return result
	}

	// Truly unknown — no continuation phrase, no keyword match, no embedding/LLM result.
	result := GateIntentResult{
		Intent:     GateIntentUnknown,
		Confidence: 0.30,
		Layer:      1,
		Reason:     "no strong keyword match",
	}
	log.Printf("[GateIntentClassifier] classify(%q): layer=1 intent=%s conf=%.2f reason=%s",
		text, result.Intent, result.Confidence, result.Reason)
	return result
}

// matchesContinuationPhrase returns true if the lowercased text matches any
// known continuation phrase (exact match or substring).
func matchesContinuationPhrase(lower string) bool {
	for _, phrase := range gateContPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// hasCodingSignals scans a list of recent messages for coding-related keywords.
// Returns true if any message contains a coding keyword from codingKeywords.
func hasCodingSignals(messages []string) bool {
	for _, msg := range messages {
		lower := strings.ToLower(msg)
		for _, kw := range codingKeywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

// gateMaintenanceKeywords are coding keywords specifically about
// refactoring/optimization, not creation or bug-fix. Used by Layer 1
// keyword classification to distinguish maintenance from other coding tasks.
var gateMaintenanceKeywords = []string{
	"重构", "refactor", "优化", "清理", "升级", "改善",
	"clean up", "optimize", "upgrade", "improve",
}

// classifyByKeywords performs Layer 1 keyword-based classification using the
// existing keyword lists from coding_tool_gate.go and im_tools_session.go.
//
// Returns (result, true) when a high-confidence match is found, or
// (zero, false) when no strong match exists and classification should
// fall through to Layer 2.
func (g *GateIntentClassifier) classifyByKeywords(text string) (GateIntentResult, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return GateIntentResult{}, false
	}

	// Count keyword matches for each category.
	nonCodingCount := countSubstringMatches(lower, nonCodingKeywords)
	codingCount := countSubstringMatches(lower, codingKeywords)
	creationCount := countSubstringMatches(lower, creationCodingKeywords)
	bugFixCount := countMapSubstringMatches(lower, bugFixKeywords)

	// Maintenance signals: coding keywords that are specifically about
	// refactoring/optimization, not creation or bug-fix.
	maintenanceCount := countSubstringMatches(lower, gateMaintenanceKeywords)

	hasNonCoding := nonCodingCount > 0
	hasCoding := codingCount > 0
	hasCreation := creationCount > 0
	hasBugFix := bugFixCount > 0
	hasMaintenance := maintenanceCount > 0

	// --- Mixed-intent dominance rules (Requirement 10) ---

	// Rule 1: Non-coding + no coding keywords → non_coding.
	// "翻译文档" → non_coding; "翻译这段代码的注释" → non_coding
	// (primary action is non-coding even if "代码" appears as object).
	if hasNonCoding && !hasCoding {
		return GateIntentResult{
			Intent:     GateIntentNonCoding,
			Confidence: 0.92,
			Layer:      1,
			Reason:     fmt.Sprintf("non-coding keywords matched (%d hits), no coding keywords", nonCodingCount),
		}, true
	}

	// Non-coding dominates coding when the primary action is non-coding.
	// e.g. "翻译这段代码的注释" — "翻译" is the verb/action, "代码" is the object.
	// We detect this by checking if a non-coding keyword appears before any
	// coding keyword in the text, indicating the primary action is non-coding.
	if hasNonCoding && hasCoding && !hasCreation && !hasBugFix {
		if primaryActionIsNonCoding(lower) {
			return GateIntentResult{
				Intent:     GateIntentNonCoding,
				Confidence: 0.92,
				Layer:      1,
				Reason:     fmt.Sprintf("non-coding primary action dominates coding context (%d non-coding, %d coding)", nonCodingCount, codingCount),
			}, true
		}
	}

	// Rule 2: Creation keywords → new_project (creation dominates bug-fix).
	// "开发一个bug追踪系统" → new_project (has creation "开发" + bug "bug").
	if hasCreation {
		return GateIntentResult{
			Intent:     GateIntentNewProject,
			Confidence: 0.92,
			Layer:      1,
			Reason:     fmt.Sprintf("creation keywords matched (%d hits), creation dominates", creationCount),
		}, true
	}

	// Rule 3: Bug-fix keywords without creation → bug_fix.
	// "修复这个bug然后重构代码" → bug_fix (bugfix dominates maintenance).
	if hasBugFix && !hasCreation {
		return GateIntentResult{
			Intent:     GateIntentBugFix,
			Confidence: 0.92,
			Layer:      1,
			Reason:     fmt.Sprintf("bug-fix keywords matched (%d hits), no creation keywords", bugFixCount),
		}, true
	}

	// Rule 4: Maintenance keywords without creation/bugfix → maintenance.
	if hasMaintenance && !hasCreation && !hasBugFix {
		return GateIntentResult{
			Intent:     GateIntentMaintenance,
			Confidence: 0.85,
			Layer:      1,
			Reason:     fmt.Sprintf("maintenance keywords matched (%d hits), catch-all coding", maintenanceCount),
		}, true
	}

	// Rule 5: General coding keywords (not creation, not bugfix, not maintenance)
	// → maintenance as catch-all for generic coding that isn't specific enough.
	if hasCoding && !hasCreation && !hasBugFix && !hasMaintenance {
		return GateIntentResult{
			Intent:     GateIntentMaintenance,
			Confidence: 0.85,
			Layer:      1,
			Reason:     fmt.Sprintf("general coding keywords matched (%d hits), no specific category", codingCount),
		}, true
	}

	// No strong keyword match — fall through to Layer 2.
	return GateIntentResult{}, false
}

// countSubstringMatches counts how many keywords from the list appear as
// substrings in the lowercased text.
func countSubstringMatches(lower string, keywords []string) int {
	count := 0
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			count++
		}
	}
	return count
}

// countMapSubstringMatches counts how many keys from the map appear as
// substrings in the lowercased text. This handles bugFixKeywords which is
// a map[string]bool — we do substring matching rather than exact map lookup
// because the map keys include multi-word phrases.
func countMapSubstringMatches(lower string, keywords map[string]bool) int {
	count := 0
	for kw := range keywords {
		if strings.Contains(lower, kw) {
			count++
		}
	}
	return count
}

// primaryActionIsNonCoding returns true when the first matching non-coding
// keyword appears before the first matching coding keyword in the text,
// indicating the primary action/verb is non-coding. For example:
//
//	"翻译这段代码的注释" → "翻译" at pos 0, "代码" at pos 9 → true
//	"写代码然后翻译注释" → "代码" at pos 3, "翻译" at pos 15 → false
func primaryActionIsNonCoding(lower string) bool {
	firstNonCoding := -1
	for _, kw := range nonCodingKeywords {
		if idx := strings.Index(lower, kw); idx >= 0 {
			if firstNonCoding < 0 || idx < firstNonCoding {
				firstNonCoding = idx
			}
		}
	}
	if firstNonCoding < 0 {
		return false
	}

	firstCoding := -1
	for _, kw := range codingKeywords {
		if idx := strings.Index(lower, kw); idx >= 0 {
			if firstCoding < 0 || idx < firstCoding {
				firstCoding = idx
			}
		}
	}
	if firstCoding < 0 {
		return true // non-coding found, no coding found
	}

	return firstNonCoding < firstCoding
}

// classifyByEmbedding performs Layer 2 embedding-based classification using
// cosine similarity between the user message embedding and pre-computed anchor
// vectors for each gate intent category.
//
// Returns (result, true) when a high-confidence match is found (confidence ≥ 0.78
// and gap ≥ 0.10), or (result, false) when the result is ambiguous and should
// fall through to Layer 3.
func (g *GateIntentClassifier) classifyByEmbedding(text string) (GateIntentResult, bool) {
	// 1. Get query embedding from cache.
	queryVec, err := g.queryCache.Get(text)
	if err != nil || queryVec == nil {
		return GateIntentResult{}, false
	}

	// 2. Compute max cosine similarity for each category.
	allScores := make(map[GateIntent]float64)
	g.mu.RLock()
	for _, anchor := range g.anchors {
		if len(anchor.Vecs) == 0 {
			continue // anchor not yet warmed up
		}
		maxSim := 0.0
		for _, vec := range anchor.Vecs {
			sim := tool.CosineSimilarity(queryVec, vec)
			if sim > maxSim {
				maxSim = sim
			}
		}
		allScores[anchor.Intent] = maxSim
	}
	g.mu.RUnlock()

	// 3. Find top-1 and top-2 categories.
	var top1Intent GateIntent
	var top1Score, top2Score float64
	for intent, score := range allScores {
		if score > top1Score {
			top2Score = top1Score
			top1Score = score
			top1Intent = intent
		} else if score > top2Score {
			top2Score = score
		}
	}

	gap := top1Score - top2Score

	// 4. Decision thresholds.
	if top1Score >= 0.78 && gap >= 0.10 {
		return GateIntentResult{
			Intent:     top1Intent,
			Confidence: top1Score,
			Gap:        gap,
			Layer:      2,
			Reason:     fmt.Sprintf("embedding: top=%s (%.3f), gap=%.3f", top1Intent, top1Score, gap),
			AllScores:  allScores,
		}, true
	}

	// Ambiguous — fall through to Layer 3 (or return as-is if Layer 3 not available).
	return GateIntentResult{
		Intent:     top1Intent,
		Confidence: top1Score,
		Gap:        gap,
		Layer:      2,
		Reason:     fmt.Sprintf("embedding ambiguous: top=%s (%.3f), gap=%.3f", top1Intent, top1Score, gap),
		AllScores:  allScores,
	}, false
}

// classifyGateIntentWithLLM performs Layer 3 LLM-based gate classification.
// It sends the user message to the configured LLM with a gate-specific system
// prompt and parses the structured JSON response. A 3-second timeout is
// enforced via context.WithTimeout.
func (g *GateIntentClassifier) classifyGateIntentWithLLM(text string) (GateIntentResult, error) {
	if g.llmConfig == nil || g.httpClient == nil {
		return GateIntentResult{}, fmt.Errorf("LLM config or HTTP client not available")
	}

	cfg := g.llmConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return GateIntentResult{}, fmt.Errorf("LLM config incomplete: URL or model empty")
	}

	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": gateClassifierSystemPrompt},
		map[string]interface{}{"role": "user", "content": strings.TrimSpace(text)},
	}

	// Use a 3-second timeout for the LLM call (Requirement 4.3).
	const gateLLMTimeout = 3 * time.Second

	// Create a child context with the 3-second timeout. DoSimpleLLMRequest
	// creates its own internal context, so we wrap the call in a goroutine
	// and select on our timeout context.
	ctx, cancel := context.WithTimeout(context.Background(), gateLLMTimeout)
	defer cancel()

	type llmResult struct {
		resp *agent.LLMSimpleResponse
		err  error
	}
	ch := make(chan llmResult, 1)
	go func() {
		resp, err := agent.DoSimpleLLMRequest(cfg, messages, g.httpClient, gateLLMTimeout)
		ch <- llmResult{resp, err}
	}()

	select {
	case <-ctx.Done():
		log.Printf("[GateIntentClassifier] LLM call timed out after %s", gateLLMTimeout)
		return GateIntentResult{}, fmt.Errorf("LLM call timed out: %w", ctx.Err())
	case result := <-ch:
		if result.err != nil {
			log.Printf("[GateIntentClassifier] LLM call failed: %v", result.err)
			return GateIntentResult{}, fmt.Errorf("LLM call failed: %w", result.err)
		}
		if result.resp == nil || strings.TrimSpace(result.resp.Content) == "" {
			return GateIntentResult{}, fmt.Errorf("LLM returned empty response")
		}

		parsed, err := parseGateLLMResponse(result.resp.Content)
		if err != nil {
			log.Printf("[GateIntentClassifier] LLM response parse failed: %v (body: %s)", err, result.resp.Content)
			return GateIntentResult{}, fmt.Errorf("parse LLM response: %w", err)
		}

		log.Printf("[GateIntentClassifier] LLM result: intent=%s confidence=%.2f reason=%s",
			parsed.Intent, parsed.Confidence, parsed.Reason)
		return parsed, nil
	}
}

// parseGateLLMResponse parses the JSON response from the gate classification
// LLM call into a GateIntentResult. It validates the intent string and clamps
// confidence to [0, 1]. Unknown or invalid intent strings are mapped to
// GateIntentUnknown.
func parseGateLLMResponse(body string) (GateIntentResult, error) {
	// Strip markdown code fences and whitespace that some models add.
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	// Extract JSON object if surrounded by extra text.
	if start := strings.Index(body, "{"); start >= 0 {
		if end := strings.LastIndex(body, "}"); end > start {
			body = body[start : end+1]
		}
	}

	var resp struct {
		GateIntent string  `json:"gate_intent"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return GateIntentResult{}, fmt.Errorf("parse gate LLM response: %w", err)
	}

	// Validate and normalize intent.
	intent := GateIntent(resp.GateIntent)
	switch intent {
	case GateIntentNewProject, GateIntentBugFix, GateIntentMaintenance,
		GateIntentNonCoding, GateIntentContinuation:
		// valid — keep as-is
	default:
		intent = GateIntentUnknown
	}

	// Clamp confidence to [0, 1].
	confidence := resp.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	return GateIntentResult{
		Intent:     intent,
		Confidence: confidence,
		Layer:      3,
		Reason:     strings.TrimSpace(resp.Reason),
	}, nil
}

// logTop2Scores logs the top-2 categories with their scores from the AllScores
// map. Used for observability in Layer 2 classification results.
func logTop2Scores(text string, allScores map[GateIntent]float64) {
	if len(allScores) == 0 {
		return
	}
	var top1Intent, top2Intent GateIntent
	var top1Score, top2Score float64
	for intent, score := range allScores {
		if score > top1Score {
			top2Intent = top1Intent
			top2Score = top1Score
			top1Score = score
			top1Intent = intent
		} else if score > top2Score {
			top2Score = score
			top2Intent = intent
		}
	}
	log.Printf("[GateIntentClassifier] scores(%q): top1=%s(%.3f) top2=%s(%.3f)",
		text, top1Intent, top1Score, top2Intent, top2Score)
}

// DiagnoseScores returns the full scoring breakdown for all five categories.
// It runs the embedding similarity computation (same as Layer 2) and returns
// a map[GateIntent]float64 with scores for each category. If the classifier
// is not ready (no embeddings) or the query cannot be embedded, returns an
// empty map. Usable in tests and debugging tools.
func (g *GateIntentClassifier) DiagnoseScores(text string) map[GateIntent]float64 {
	if !g.Ready() || g.queryCache == nil {
		return map[GateIntent]float64{}
	}

	queryVec, err := g.queryCache.Get(text)
	if err != nil || queryVec == nil {
		return map[GateIntent]float64{}
	}

	scores := make(map[GateIntent]float64)
	g.mu.RLock()
	for _, anchor := range g.anchors {
		if len(anchor.Vecs) == 0 {
			continue // anchor not yet warmed up
		}
		maxSim := 0.0
		for _, vec := range anchor.Vecs {
			sim := tool.CosineSimilarity(queryVec, vec)
			if sim > maxSim {
				maxSim = sim
			}
		}
		scores[anchor.Intent] = maxSim
	}
	g.mu.RUnlock()

	return scores
}

// gateAnchors returns the anchor text sets for all five gate intent categories.
// Each category contains a balanced mix of Chinese and English example sentences
// used as reference points for embedding cosine similarity scoring.
func gateAnchors() []gateAnchor {
	return []gateAnchor{
		{
			Intent: GateIntentNewProject,
			Texts: []string{
				// Chinese (creation-oriented)
				"开发一个贪吃蛇游戏",
				"写一个爬虫程序",
				"帮我开发一个聊天应用",
				"实现一个REST API服务",
				"创建一个命令行工具",
				"写一个自动化脚本",
				"开发一个数据可视化面板",
				// English (creation-oriented)
				"build a web application",
				"create a CLI tool",
				"develop a REST API",
				"write a Python script for data processing",
				"implement a chat server",
				"build a game in JavaScript",
				"create a file upload service",
			},
		},
		{
			Intent: GateIntentBugFix,
			Texts: []string{
				// Chinese (fix/debug-oriented)
				"有bug，一直显示加载中",
				"修复崩溃问题",
				"页面白屏了",
				"程序闪退",
				"调试一下这个问题",
				"排查报错原因",
				"修复登录失败的bug",
				// English (fix/debug-oriented)
				"fix the loading issue",
				"debug this crash",
				"the app keeps crashing on startup",
				"fix the authentication error",
				"there's a bug in the payment flow",
				"troubleshoot the memory leak",
				"resolve the null pointer exception",
			},
		},
		{
			Intent: GateIntentMaintenance,
			Texts: []string{
				// Chinese (refactor/optimize)
				"重构这个函数",
				"优化性能",
				"清理无用代码",
				"升级依赖版本",
				"改善代码结构",
				"优化数据库查询速度",
				// English (refactor/optimize)
				"refactor the auth module",
				"clean up dead code",
				"optimize the database queries",
				"upgrade the dependencies",
				"improve code readability",
				"reduce technical debt in the codebase",
			},
		},
		{
			Intent: GateIntentNonCoding,
			Texts: []string{
				// Chinese (non-coding tasks)
				"翻译文档",
				"搜索论文",
				"总结这篇文章",
				"帮我整理资料",
				"生成PDF报告",
				"把这段话翻译成英文",
				// English (non-coding tasks)
				"summarize this article",
				"translate this document",
				"search for papers on AI",
				"organize these notes",
				"help me write a report",
				"draft a project proposal document",
			},
		},
		{
			Intent: GateIntentContinuation,
			Texts: []string{
				// Chinese (short action phrases)
				"继续",
				"开工",
				"开干",
				"动手",
				"搞起来",
				// English (short action phrases)
				"let's go",
				"start working",
				"go ahead",
				"continue",
			},
		},
	}
}
