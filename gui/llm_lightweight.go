package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Lightweight LLM helper — minimal-context, no-tool, single-shot LLM calls
// for classification, intent detection, and other auxiliary tasks.
//
// Usage:
//
//	result, err := h.LLMClassify(ctx, LLMClassifyRequest{
//	    SystemPrompt: "Classify intent. Reply: confirm, modify, or other.",
//	    UserMessage:  "开工",
//	    TimeoutSec:   10,
//	})
//
// Token budget: ~100-300 input + ~10-20 output
// vs full agent loop: ~55000 input + ~500-4000 output
// ---------------------------------------------------------------------------

// LLMClassifyRequest holds the parameters for a lightweight LLM call.
type LLMClassifyRequest struct {
	// SystemPrompt is the system-level instruction for the LLM.
	// Keep it short and focused on the classification task.
	SystemPrompt string

	// UserMessage is the user input to classify.
	UserMessage string

	// TimeoutSec is the request timeout in seconds. Default: 15.
	TimeoutSec int

	// Tag is a short label for logging (e.g. "workflow-confirm", "intent-detect").
	Tag string
}

// LLMClassifyResult holds the response from a lightweight LLM call.
type LLMClassifyResult struct {
	// Text is the raw LLM response, trimmed of whitespace.
	Text string

	// InputTokens and OutputTokens track usage for cost monitoring.
	InputTokens  int
	OutputTokens int

	// Latency is the wall-clock time of the LLM call.
	Latency time.Duration
}

// LLMClassify makes a minimal-context LLM call with no tools, no conversation
// history, and no streaming. Designed for classification and intent detection
// tasks where the full agent loop would waste tokens.
//
// The call uses the same LLM provider as the main agent loop but with:
//   - Only 2 messages (system + user), typically ~100-300 tokens input
//   - No tool definitions (saves ~2000-5000 tokens)
//   - No conversation history (saves ~10000-50000 tokens)
//   - No streaming (simpler, lower overhead for short responses)
//   - Dedicated timeout (default 15s)
//
// Reusable for any classification task: workflow confirm/modify detection,
// intent classification, content categorization, etc.
func (h *IMMessageHandler) LLMClassify(ctx context.Context, req LLMClassifyRequest) (*LLMClassifyResult, error) {
	if h.app == nil {
		return nil, fmt.Errorf("app not initialized")
	}

	// Defaults.
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 15
	}
	if req.Tag == "" {
		req.Tag = "llm-classify"
	}

	cfg := h.app.GetMaclawLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("LLM not configured")
	}

	// Build minimal message list: system + user only.
	messages := []interface{}{
		map[string]string{"role": "system", "content": req.SystemPrompt},
		map[string]string{"role": "user", "content": req.UserMessage},
	}

	// No tools.
	var tools []map[string]interface{}

	// Create a dedicated short-lived HTTP client with tight timeout.
	// Don't reuse h.client which has a longer timeout for streaming.
	client := &http.Client{
		Timeout: time.Duration(req.TimeoutSec) * time.Second,
	}

	// Create a timeout context.
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSec)*time.Second)
	defer cancel()

	startedAt := time.Now()
	metrics := &llmStreamMetrics{}
	resp, err := h.doLLMRequestStream(timeoutCtx, cfg, messages, tools, client, nil, metrics)
	latency := time.Since(startedAt)

	if err != nil {
		log.Printf("[%s] LLM call failed (%.1fs): %v", req.Tag, latency.Seconds(), err)
		return nil, fmt.Errorf("%s LLM call failed: %w", req.Tag, err)
	}

	text := ""
	inputTokens := 0
	outputTokens := 0
	if resp != nil && len(resp.Choices) > 0 {
		text = strings.TrimSpace(resp.Choices[0].Message.Content)
	}
	if resp != nil && resp.Usage != nil {
		inputTokens = resp.Usage.PromptTokens
		outputTokens = resp.Usage.CompletionTokens
	}

	log.Printf("[%s] result=%q input=%d output=%d latency=%.1fs",
		req.Tag, truncateForLogGUI(text, 60), inputTokens, outputTokens, latency.Seconds())

	return &LLMClassifyResult{
		Text:         text,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Latency:      latency,
	}, nil
}

// truncateForLogGUI truncates a string for log output (GUI package version).
func truncateForLogGUI(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
