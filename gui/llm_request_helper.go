package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// dumpLLMContext reports LLM HTTP failures without persisting prompt/request
// bodies. Those bodies may contain browser observations, screenshots OCR, or
// user secrets, so writing them under ~/.maclaw would leak private context.
func dumpLLMContext(statusCode int, respMsg string, requestBody []byte, tempDir string) error {
	_ = tempDir
	ctxLen := len(requestBody)
	if statusCode != http.StatusInternalServerError {
		return fmt.Errorf("HTTP %d: %s (context %d bytes)", statusCode, respMsg, ctxLen)
	}
	return fmt.Errorf("HTTP %d (context %d bytes, request body not dumped): %s", statusCode, ctxLen, respMsg)
}

// llmSimpleResponse is a minimal response from a simple (non-tool-calling) LLM request.
type llmSimpleResponse struct {
	Content string
}

// doSimpleLLMRequest sends a simple chat completion request (no tool calling)
// to the configured LLM, supporting both OpenAI and Anthropic protocols.
// It returns the text content of the assistant's reply.
func doSimpleLLMRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*llmSimpleResponse, error) {
	ctx = llm.WithRequestTraceIfMissing(ctx, "simple_llm")
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()
	var resp *llmSimpleResponse
	var err error
	if cfg.Protocol == "anthropic" {
		resp, err = doSimpleAnthropicRequest(scheduledCtx, cfg, messages, client, timeout)
	} else {
		resp, err = doSimpleOpenAIRequest(scheduledCtx, cfg, messages, client, timeout)
	}
	globalLLMScheduler.ObserveResult(trace, err)
	return resp, err
}

func doSimpleOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*llmSimpleResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	parsed, err := llm.DoOpenAIRequest(ctx, cfg, messages, nil, client)
	if err != nil {
		msg := err.Error()
		if strings.HasPrefix(msg, "HTTP 500:") {
			endpoint, data, buildErr := llm.BuildOpenAIChatRequestData(cfg, messages, llm.OpenAIChatRequestOptions{
				Stream: false,
			})
			if buildErr != nil {
				return nil, err
			}
			_ = endpoint
			return nil, dumpLLMContext(http.StatusInternalServerError, "llm request failed", data, "")
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
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())

	traceFields := llm.RequestTraceLogFields(ctx)
	log.Printf("[LLM] POST %s model=%s protocol=anthropic simple=true %s", endpoint, cfg.Model, traceFields)
	startedAt := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM] done %s model=%s protocol=anthropic simple=true status=error elapsed=%s err=%v %s", endpoint, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	log.Printf("[LLM] done %s model=%s protocol=anthropic simple=true status=%d elapsed=%s body_len=%d %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("llm request failed body_len=%d", len(body))
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
