package compute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TestResult holds the outcome of a provider connectivity test.
type TestResult struct {
	Success bool          `json:"success"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error,omitempty"`
	Model   string        `json:"model,omitempty"`
}

// ProviderTester sends a simple prompt to an LLM provider and reports
// whether the provider is reachable and responding correctly.
type ProviderTester struct {
	Client *http.Client
}

// NewProviderTester creates a ProviderTester with a 30-second timeout.
func NewProviderTester() *ProviderTester {
	return &ProviderTester{
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Test sends a simple "Hello" prompt to the given provider and returns the result.
func (t *ProviderTester) Test(p *ComputeProvider) TestResult {
	start := time.Now()

	req, err := t.buildRequest(p)
	if err != nil {
		return TestResult{Error: fmt.Sprintf("build request: %s", err)}
	}

	resp, err := t.Client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return TestResult{Error: fmt.Sprintf("request failed: %s", err), Latency: latency}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := string(body)
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return TestResult{
			Error:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg),
			Latency: latency,
		}
	}

	model := t.extractModel(p.Protocol, body)
	if model == "" {
		model = p.Model
	}

	return TestResult{
		Success: true,
		Latency: latency,
		Model:   model,
	}
}

// buildRequest constructs the appropriate HTTP request for the provider's protocol.
func (t *ProviderTester) buildRequest(p *ComputeProvider) (*http.Request, error) {
	switch p.Protocol {
	case ProtocolOpenAI:
		return t.buildOpenAIRequest(p)
	case ProtocolAnthropic:
		return t.buildAnthropicRequest(p)
	case ProtocolGemini:
		return t.buildGeminiRequest(p)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
}

func (t *ProviderTester) buildOpenAIRequest(p *ComputeProvider) (*http.Request, error) {
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	model := p.Model
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens": 5,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}
	return req, nil
}

func (t *ProviderTester) buildAnthropicRequest(p *ComputeProvider) (*http.Request, error) {
	url := strings.TrimRight(p.BaseURL, "/") + "/messages"
	model := p.Model
	if model == "" {
		model = "claude-3-haiku-20240307"
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens": 5,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}
	return req, nil
}

func (t *ProviderTester) buildGeminiRequest(p *ComputeProvider) (*http.Request, error) {
	model := p.Model
	if model == "" {
		model = "gemini-pro"
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/models/" + model + ":generateContent?key=" + p.APIKey
	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]string{
					{"text": "Hello"},
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}
	return req, nil
}

// extractModel tries to pull the model name from the response body.
func (t *ProviderTester) extractModel(protocol string, body []byte) string {
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}
	switch protocol {
	case ProtocolOpenAI:
		if m, ok := data["model"].(string); ok {
			return m
		}
	case ProtocolAnthropic:
		if m, ok := data["model"].(string); ok {
			return m
		}
	case ProtocolGemini:
		if m, ok := data["modelVersion"].(string); ok {
			return m
		}
	}
	return ""
}
