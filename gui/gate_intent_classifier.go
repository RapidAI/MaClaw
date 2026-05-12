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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// gateClassifierSystemPrompt is the system prompt for the Layer 3 LLM
// refinement call. It instructs the LLM to classify user messages into one
// of five gate-specific categories with structured JSON output.
const gateClassifierSystemPrompt = `You are a coding workflow gate intent classifier.

Classify the user's request into exactly one gate_intent:
- new_project: create a new app, feature, tool, game, script, API, or system.
- bug_fix: debug, repair, troubleshoot, or fix a failing existing codebase.
- maintenance: refactor, optimize, clean up, upgrade, or improve existing code.
- non_coding: translation, summarization, research, reports, documents, presentations, or other non-coding work.
- continuation: continue a previously discussed coding task.
- unknown: not enough information or no safe classification.

Use semantic intent. Do not rely on keyword matching. If the request is ambiguous, return unknown with low confidence.
Return only one JSON object matching this shape:
{"gate_intent":"new_project|bug_fix|maintenance|non_coding|continuation|unknown","confidence":0.0,"reason":"short reason"}`

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

// Gate Intent Classifier performs semantic, multi-layer classification for the
// coding tool gate. It uses UIC first, then embedding and LLM classifiers.
// It returns unknown when semantic classifiers are unavailable or inconclusive.

// GateIntentResult holds the classification output from GateIntentClassifier.
type GateIntentResult struct {
	Intent     GateIntent             // one of the GateIntent constants
	Confidence float64                // [0, 1], higher means more certain
	Gap        float64                // score gap between top-1 and top-2 category
	Layer      int                    // classifier layer reported by UIC/embedding/LLM
	Reason     string                 // human-readable explanation
	AllScores  map[GateIntent]float64 // diagnostic: scores for all five categories
	Degraded   bool                   // true when UIC fusion was in degraded mode (one channel failed)
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

// GateIntentClassifier performs semantic gate intent classification.
type GateIntentClassifier struct {
	embedder          embedding.Embedder
	anchors           []gateAnchor
	queryCache        *tool.QueryEmbeddingCache
	ctxProvider       ConversationContextProvider
	llmConfig         func() corelib.MaclawLLMConfig // lazy access to LLM config
	httpClient        *http.Client
	unifiedClassifier *intent.UnifiedIntentClassifier
	ready             bool
	mu                sync.RWMutex
}

// NewGateIntentClassifier creates a new gate intent classifier. If emb is nil
// or a NoopEmbedder, local embedding classification is unavailable and callers
// receive unknown unless UIC or LLM classification is configured.
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
func (g *GateIntentClassifier) SetLLMConfig(cfgFn func() corelib.MaclawLLMConfig, client *http.Client) {
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
// conversation context lookup.
//
// Classification uses UIC first, then semantic embedding and LLM classifiers.
// If semantic classifiers are unavailable or inconclusive, it returns unknown.
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
			Degraded:   uicResult.Degraded,
		}
		if shouldAcceptGateResult(result) {
			log.Printf("[GateIntentClassifier] classify(%q): UIC delegation intent=%s conf=%.2f layer=%d",
				text, result.Intent, result.Confidence, result.Layer)
			return result
		}
		log.Printf("[GateIntentClassifier] classify(%q): UIC inconclusive intent=%s conf=%.2f degraded=%v; escalating",
			text, result.Intent, result.Confidence, result.Degraded)
	}

	// --- Fallback: local semantic pipeline when UIC is nil ---
	// Layer 2: embedding cosine similarity (when ready).
	var layer2Result GateIntentResult
	var hasLayer2 bool
	if g.Ready() {
		if result, ok := g.classifyByEmbedding(text); ok {
			log.Printf("[GateIntentClassifier] classify(%q): layer=2 intent=%s conf=%.2f gap=%.2f reason=%s",
				text, result.Intent, result.Confidence, result.Gap, result.Reason)
			logTop2Scores(text, result.AllScores)
			return result
		} else if result.Intent.IsKnown() {
			// Layer 2 returned an ambiguous result; save it for fallback.
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

	// Truly unknown: no semantic classifier result.
	result := GateIntentResult{
		Intent:     GateIntentUnknown,
		Confidence: 0.30,
		Layer:      1,
		Reason:     "semantic classifiers unavailable or inconclusive",
	}
	log.Printf("[GateIntentClassifier] classify(%q): layer=semantic-unavailable intent=%s conf=%.2f reason=%s",
		text, result.Intent, result.Confidence, result.Reason)
	return result
}

func shouldAcceptGateResult(result GateIntentResult) bool {
	// Accept degraded results when the intent is clearly non-coding.
	// The gate's purpose is to decide "is this a coding task that needs
	// three-phase workflow?" — for non-coding intents (non_coding, bug_fix,
	// maintenance, continuation), even a degraded embedding-only result is
	// sufficient to make this decision. Only escalate to LLM when the intent
	// is new_project/unknown (where the three-phase decision matters) AND
	// confidence is low.
	if result.Degraded {
		switch result.Intent {
		case GateIntentNonCoding, GateIntentBugFix, GateIntentMaintenance, GateIntentContinuation:
			// Non-coding / non-creation intents: accept with moderate confidence.
			// This saves 3s of LLM call for the common case (simple operations).
			return result.Confidence >= 0.50
		case GateIntentNewProject:
			// New project intent needs higher confidence when degraded.
			return result.Confidence >= 0.75
		default:
			// Unknown: escalate to LLM for clarification.
			return false
		}
	}
	if result.Intent == GateIntentUnknown {
		return false
	}
	return result.Confidence >= 0.60
}

// classifyByEmbedding performs Layer 2 embedding-based classification using
// cosine similarity between the user message embedding and pre-computed anchor
// vectors for each gate intent category.
//
// Returns (result, true) when a high-confidence match is found (confidence >= 0.78
// and gap >= 0.10), or (result, false) when the result is ambiguous and should
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

	// Ambiguous: fall through to Layer 3 (or return as-is if Layer 3 is not available).
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
		GateIntentNonCoding, GateIntentContinuation, GateIntentUnknown:
		// valid; keep as-is
	default:
		return GateIntentResult{}, fmt.Errorf("invalid gate_intent %q", resp.GateIntent)
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
				"fix the loading issue",
				"debug this crash",
				"the app keeps crashing on startup",
				"fix the authentication error",
				"there is a bug in the payment flow",
				"troubleshoot the memory leak",
				"resolve the null pointer exception",
			},
		},
		{
			Intent: GateIntentMaintenance,
			Texts: []string{
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
				"continue the previous implementation",
				"start working on the discussed task",
				"go ahead with the plan",
				"proceed with the coding task",
			},
		},
	}
}
