package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// AuxiliaryConfig holds the configuration for a lightweight LLM used for
// background tasks: memory compression, skill repair, session search
// summarization, etc. When not configured, the main LLM config is used.
type AuxiliaryConfig struct {
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol,omitempty"` // "openai" (default) or "anthropic"
}

// IsConfigured returns true if the auxiliary LLM has a URL and key set.
func (c AuxiliaryConfig) IsConfigured() bool {
	return c.URL != "" && c.Key != ""
}

// AuxiliaryCaller wraps an AuxiliaryConfig and provides a simple ChatCall
// interface for background tasks. It implements the LLMChatCaller interface
// used by memory.Compressor and skill.LLMRepairer.
type AuxiliaryCaller struct {
	Config     AuxiliaryConfig
	HTTPClient *http.Client
}

// NewAuxiliaryCaller creates a caller with sensible defaults.
func NewAuxiliaryCaller(cfg AuxiliaryConfig) *AuxiliaryCaller {
	return &AuxiliaryCaller{
		Config: cfg,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// IsConfigured reports whether the auxiliary LLM backend is ready.
func (c *AuxiliaryCaller) IsConfigured() bool {
	return c.Config.IsConfigured()
}

// ChatCall sends a chat completion request and returns the assistant reply.
func (c *AuxiliaryCaller) ChatCall(messages []map[string]string) (string, error) {
	if !c.IsConfigured() {
		return "", fmt.Errorf("auxiliary LLM not configured")
	}

	// Convert to OpenAI format.
	var msgs []map[string]interface{}
	for _, m := range messages {
		msgs = append(msgs, map[string]interface{}{
			"role":    m["role"],
			"content": m["content"],
		})
	}

	body := map[string]interface{}{
		"model":    c.Config.Model,
		"messages": msgs,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.Config.URL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	url += "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Config.Key)
	corelib.SetCodeGenClientNameHeaderIfNeeded(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("HTTP %d: body_len=%d", resp.StatusCode, len(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}
