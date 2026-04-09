package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const (
	simpleLLMMaxAttempts    = 5
	simpleLLMInitialBackoff = 200 * time.Millisecond
)

// thinkTagPattern matches <think>...</think> blocks produced by reasoning
// models (DeepSeek, Kimi, QwQ, etc.).
var thinkTagPattern = regexp.MustCompile(`(?si)<think>.*?</think>|<think>.*$`)

// StripThinkingTags removes <think>...</think> blocks from LLM output.
func StripThinkingTags(s string) string {
	if !strings.Contains(s, "<think>") {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(thinkTagPattern.ReplaceAllString(s, ""))
}

// LLMSimpleResponse is a minimal response from a simple LLM request.
type LLMSimpleResponse struct {
	Content string
}

type llmHTTPError struct {
	statusCode int
	message    string
}

func (e *llmHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.statusCode, e.message)
}

func (e *llmHTTPError) StatusCode() int {
	return e.statusCode
}

func newLLMHTTPError(statusCode int, message string) error {
	return &llmHTTPError{statusCode: statusCode, message: message}
}

func shouldRetrySimpleLLMError(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *llmHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.statusCode == http.StatusRequestTimeout || httpErr.statusCode == http.StatusTooManyRequests || httpErr.statusCode >= http.StatusInternalServerError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func waitSimpleLLMBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// dumpLLMContext saves the request body to a temp file on HTTP 500.
func dumpLLMContext(statusCode int, respMsg string, requestBody []byte) error {
	if statusCode != http.StatusInternalServerError {
		return newLLMHTTPError(statusCode, respMsg)
	}
	ctxLen := len(requestBody)

	// Use ~/.maclaw/temp if available, fallback to os.TempDir()
	tempDir := os.TempDir()
	if home, err := os.UserHomeDir(); err == nil {
		maclawTmp := filepath.Join(home, ".maclaw", "temp")
		if _, err := os.Stat(maclawTmp); err == nil {
			tempDir = maclawTmp
		} else {
			// Try to create it if .maclaw exists
			maclawDir := filepath.Join(home, ".maclaw")
			if _, err := os.Stat(maclawDir); err == nil {
				_ = os.MkdirAll(maclawTmp, 0o755)
				tempDir = maclawTmp
			}
		}
	}

	dumpFile := filepath.Join(tempDir, fmt.Sprintf("llm_context_%d.json", time.Now().UnixMilli()))
	if err := os.WriteFile(dumpFile, requestBody, 0644); err != nil {
		return fmt.Errorf("HTTP %d (context %d bytes, dump failed: %v): %s", statusCode, ctxLen, err, respMsg)
	}
	return fmt.Errorf("HTTP %d (context %d bytes, dumped to %s): %s", statusCode, ctxLen, dumpFile, respMsg)
}

// DoSimpleLLMRequest sends a simple chat completion request (no tool calling)
// supporting both OpenAI and Anthropic protocols.
func DoSimpleLLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*LLMSimpleResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	startedAt := time.Now()
	var lastErr error
	backoff := simpleLLMInitialBackoff
	for attempt := 1; attempt <= simpleLLMMaxAttempts; attempt++ {
		var (
			resp *LLMSimpleResponse
			err  error
		)
		if cfg.Protocol == "anthropic" {
			resp, err = doSimpleAnthropicRequest(ctx, cfg, messages, client)
		} else {
			resp, err = doSimpleOpenAIRequest(ctx, cfg, messages, client)
		}
		if err == nil {
			if attempt > 1 {
				log.Printf("[LLM Simple] succeeded on attempt %d/%d for model=%s protocol=%s after %s", attempt, simpleLLMMaxAttempts, cfg.Model, cfg.Protocol, time.Since(startedAt).Round(time.Millisecond))
			}
			return resp, nil
		}
		lastErr = err
		if !shouldRetrySimpleLLMError(err) || attempt == simpleLLMMaxAttempts {
			break
		}
		elapsed := time.Since(startedAt).Round(time.Millisecond)
		log.Printf("[LLM Simple] attempt %d/%d failed for model=%s protocol=%s after %s: %v; retrying in %s", attempt, simpleLLMMaxAttempts, cfg.Model, cfg.Protocol, elapsed, err, backoff)
		if err := waitSimpleLLMBackoff(ctx, backoff); err != nil {
			lastErr = err
			log.Printf("[LLM Simple] stop retrying for model=%s protocol=%s after %s: %v", cfg.Model, cfg.Protocol, time.Since(startedAt).Round(time.Millisecond), err)
			break
		}
		backoff *= 2
	}
	if shouldRetrySimpleLLMError(lastErr) {
		return nil, fmt.Errorf("simple LLM request failed after %d attempts: %w", simpleLLMMaxAttempts, lastErr)
	}
	return nil, lastErr
}

func doSimpleOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (*LLMSimpleResponse, error) {
	req, data, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream: true,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[LLM Simple] POST %s model=%s protocol=%s (stream=true)", endpoint, cfg.Model, cfg.Protocol)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, dumpLLMContext(resp.StatusCode, msg, data)
	}

	parsed, err := llm.ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("parse response: %w (body prefix: %s)", err, snippet)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	if text == "" {
		return nil, fmt.Errorf("no response from model")
	}
	return &LLMSimpleResponse{Content: StripThinkingTags(text)}, nil
}

func doSimpleAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (*LLMSimpleResponse, error) {
	endpoint := corelib.AnthropicMessagesEndpoint(cfg.URL)

	var systemText string
	var anthropicMsgs []interface{}
	for _, m := range messages {
		if mm, ok := m.(map[string]string); ok && mm["role"] == "system" {
			systemText = mm["content"]
			continue
		}
		if mm, ok := m.(map[string]interface{}); ok {
			if role, _ := mm["role"].(string); role == "system" {
				if content, _ := mm["content"].(string); content != "" {
					systemText = content
				}
				continue
			}
		}
		anthropicMsgs = append(anthropicMsgs, m)
	}

	reqBody := map[string]interface{}{
		"model":      cfg.Model,
		"messages":   anthropicMsgs,
		"max_tokens": 4096,
	}
	if systemText != "" {
		reqBody["system"] = systemText
	}
	data, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, cfg.Key)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, dumpLLMContext(resp.StatusCode, msg, data)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			return &LLMSimpleResponse{Content: StripThinkingTags(block.Text)}, nil
		}
	}
	return nil, fmt.Errorf("no text response from model")
}
