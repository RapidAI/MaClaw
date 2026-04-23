package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// dumpLLMContext saves the request body to a temp file when an HTTP 500 error
// occurs, and returns an enriched error message containing the context length
// (in bytes) and the dump file path.
func dumpLLMContext(statusCode int, respMsg string, requestBody []byte, tempDir string) error {
	if statusCode != http.StatusInternalServerError {
		return fmt.Errorf("HTTP %d: %s", statusCode, respMsg)
	}
	ctxLen := len(requestBody)
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	dumpFile := filepath.Join(tempDir, fmt.Sprintf("llm_context_%d.json", time.Now().UnixMilli()))
	if err := os.WriteFile(dumpFile, requestBody, 0644); err != nil {
		return fmt.Errorf("HTTP %d (context %d bytes, dump failed: %v): %s", statusCode, ctxLen, err, respMsg)
	}
	return fmt.Errorf("HTTP %d (context %d bytes, dumped to %s): %s", statusCode, ctxLen, dumpFile, respMsg)
}

// llmSimpleResponse is a minimal response from a simple (non-tool-calling) LLM request.
type llmSimpleResponse struct {
	Content string
}

// doSimpleLLMRequest sends a simple chat completion request (no tool calling)
// to the configured LLM, supporting both OpenAI and Anthropic protocols.
// It returns the text content of the assistant's reply.
func doSimpleLLMRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*llmSimpleResponse, error) {
	if cfg.Protocol == "anthropic" {
		return doSimpleAnthropicRequest(ctx, cfg, messages, client, timeout)
	}
	return doSimpleOpenAIRequest(ctx, cfg, messages, client, timeout)
}

func doSimpleOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*llmSimpleResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	parsed, err := llm.DoOpenAIRequest(ctx, cfg, messages, nil, client)
	if err != nil {
		msg := err.Error()
		if strings.HasPrefix(msg, "HTTP 500:") {
			if len(msg) > 512 {
				msg = msg[:512] + "..."
			}
			endpoint, data, buildErr := llm.BuildOpenAIChatRequestData(cfg, messages, llm.OpenAIChatRequestOptions{
				Stream: false,
			})
			if buildErr != nil {
				return nil, err
			}
			_ = endpoint
			return nil, dumpLLMContext(http.StatusInternalServerError, msg, data, "")
		}
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
}

func doSimpleAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*llmSimpleResponse, error) {
	endpoint := corelib.AnthropicMessagesEndpoint(cfg.URL)

	// Separate system message from user/assistant messages
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

	ctx, cancel := context.WithTimeout(ctx, timeout)
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
		return nil, dumpLLMContext(resp.StatusCode, msg, data, "")
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
			return &llmSimpleResponse{Content: stripThinkingTags(block.Text)}, nil
		}
	}
	return nil, fmt.Errorf("no text response from model")
}
