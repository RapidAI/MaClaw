package llm

// Unified OpenAI-compatible LLM HTTP client.
// All packages (gui, tui, hub/corelib/agent) should use these functions
// instead of implementing their own request/response logic.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// DoOpenAIRequest sends a non-streaming OpenAI-compatible chat completion
// request. It handles provider quirks (e.g. MiniMax system-role merge)
// in one place so callers don't need to worry about them.
//
// The caller provides a context for cancellation/timeout control.
// tools may be nil for simple requests without tool calling.
func DoOpenAIRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"
	log.Printf("[LLM] POST %s model=%s protocol=%s", endpoint, cfg.Model, cfg.Protocol)

	// Provider-specific message adaptation
	if corelib.NeedsSystemMerge(cfg) {
		messages = corelib.MergeSystemIntoUser(messages)
	}

	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": messages,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	var result Response
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}
