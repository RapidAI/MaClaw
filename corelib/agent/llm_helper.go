package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// thinkTagPattern matches <think>...</think> blocks produced by reasoning
// models (DeepSeek, Kimi, QwQ, etc.).
var thinkTagPattern = regexp.MustCompile(`(?si)<think>.*?</think>|<think>.*$`)

// StripThinkingTags removes <think>...</think> blocks from LLM output.
func StripThinkingTags(s string) string {
	if !strings.Contains(s, "<think>") {
		return s
	}
	return strings.TrimSpace(thinkTagPattern.ReplaceAllString(s, ""))
}

// LLMSimpleResponse is a minimal response from a simple LLM request.
type LLMSimpleResponse struct {
	Content string
}

// dumpLLMContext saves the request body to a temp file on HTTP 500.
func dumpLLMContext(statusCode int, respMsg string, requestBody []byte) error {
	if statusCode != http.StatusInternalServerError {
		return fmt.Errorf("HTTP %d: %s", statusCode, respMsg)
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
	if cfg.Protocol == "anthropic" {
		return doSimpleAnthropicRequest(cfg, messages, client, timeout)
	}
	return doSimpleOpenAIRequest(cfg, messages, client, timeout)
}

func doSimpleOpenAIRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*LLMSimpleResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"
	log.Printf("[LLM Simple] POST %s model=%s protocol=%s (stream=true)", endpoint, cfg.Model, cfg.Protocol)
	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": messages,
		"stream":   true,
	}
	data, _ := json.Marshal(reqBody)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
		return nil, err
	}
	defer resp.Body.Close()

	// Peek the first bytes to detect SSE vs plain JSON.
	peek := make([]byte, 64)
	n, _ := resp.Body.Read(peek)
	peek = peek[:n]
	bodyReader := io.MultiReader(bytes.NewReader(peek), resp.Body)

	if resp.StatusCode != http.StatusOK {
		rest, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		full := append(peek, rest...)
		msg := string(full)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, dumpLLMContext(resp.StatusCode, msg, data)
	}

	// Detect SSE: Content-Type or body prefix.
	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	if !isSSE {
		trimmed := bytes.TrimLeft(peek, " \t\r\n")
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			isSSE = true
		}
	}

	if !isSSE {
		// Fallback: server returned plain JSON despite stream=true.
		body, _ := io.ReadAll(io.LimitReader(bodyReader, 256*1024))
		var result struct {
			Choices []struct {
				Message struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			snippet := string(body)
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			return nil, fmt.Errorf("parse response: %w (body prefix: %s)", err, snippet)
		}
		if len(result.Choices) == 0 {
			return nil, fmt.Errorf("no response from model")
		}
		text := result.Choices[0].Message.Content
		if text == "" {
			text = result.Choices[0].Message.ReasoningContent
		}
		return &LLMSimpleResponse{Content: StripThinkingTags(text)}, nil
	}

	// Parse SSE stream and accumulate content.
	var contentBuf, reasoningBuf strings.Builder
	scanner := bufio.NewScanner(bodyReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		contentBuf.WriteString(chunk.Choices[0].Delta.Content)
		reasoningBuf.WriteString(chunk.Choices[0].Delta.ReasoningContent)
	}

	text := contentBuf.String()
	if text == "" {
		text = reasoningBuf.String()
	}
	if text == "" {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("stream read error: %w", err)
		}
		return nil, fmt.Errorf("no response from model (stream)")
	}
	return &LLMSimpleResponse{Content: StripThinkingTags(text)}, nil
}

func doSimpleAnthropicRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*LLMSimpleResponse, error) {
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

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
