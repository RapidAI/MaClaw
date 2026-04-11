package compute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ProviderSource provides the list of active compute providers.
type ProviderSource interface {
	ActiveProviders() []ProviderConfig
}

// ProviderConfig is a minimal provider configuration used by the proxy.
type ProviderConfig struct {
	Name                 string  `json:"name"`
	BaseURL              string  `json:"base_url"`
	APIKey               string  `json:"api_key"`
	Protocol             string  `json:"protocol"` // openai | anthropic | gemini
	UserAgent            string  `json:"user_agent"`
	Model                string  `json:"model"`
	Enabled              bool    `json:"enabled"`
	Priority             int     `json:"priority"`
	InputPricePerMToken  float64 `json:"input_price_per_mtoken"`
	OutputPricePerMToken float64 `json:"output_price_per_mtoken"`
}

// ChatRequest is a minimal OpenAI-compatible chat completion request.
type ChatRequest struct {
	Model    string                   `json:"model"`
	Messages []map[string]interface{} `json:"messages"`
	Stream   bool                     `json:"stream,omitempty"`
}

// ChatResponse is a minimal OpenAI-compatible chat completion response.
type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *UsageInfo   `json:"usage,omitempty"`
	Error   *ErrorInfo   `json:"error,omitempty"`
}

// ChatChoice is a single choice in the response.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatMessage is a message in the response.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// UsageInfo holds token counts.
type UsageInfo struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// ErrorInfo is an OpenAI-format error.
type ErrorInfo struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// LLMProxy forwards DiWorker LLM requests to the appropriate provider and
// records token usage locally.
type LLMProxy struct {
	source     ProviderSource
	usageStore *UsageStore
	client     *http.Client
}

// NewLLMProxy creates a new LLM proxy.
func NewLLMProxy(source ProviderSource, usageStore *UsageStore) *LLMProxy {
	return &LLMProxy{
		source:     source,
		usageStore: usageStore,
		client:     &http.Client{Timeout: 120 * time.Second},
	}
}

// HandleChatCompletions returns an http.HandlerFunc for POST /v1/chat/completions.
// It selects a provider, forwards the request, records usage, and returns the response.
func (p *LLMProxy) HandleChatCompletions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Read request body.
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
			return
		}

		var chatReq ChatRequest
		if err := json.Unmarshal(body, &chatReq); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}

		// Extract diworker ID from header or auth.
		diworkerID := r.Header.Get("X-DiWorker-ID")

		// Select provider.
		providers := p.source.ActiveProviders()
		if len(providers) == 0 {
			writeErr(w, http.StatusBadGateway, "no providers available")
			return
		}

		provider := providers[0]
		for _, prov := range providers {
			if prov.Model != "" && prov.Model == chatReq.Model {
				provider = prov
				break
			}
		}

		if chatReq.Model == "" && provider.Model != "" {
			chatReq.Model = provider.Model
		}

		// Build upstream request based on protocol.
		upstreamReq, err := p.buildUpstreamRequest(body, &chatReq, &provider)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "build request: "+err.Error())
			return
		}

		// Forward to upstream.
		upstreamResp, err := p.client.Do(upstreamReq)
		if err != nil {
			writeErr(w, http.StatusBadGateway, "upstream error: "+err.Error())
			return
		}
		defer upstreamResp.Body.Close()

		// Read upstream response.
		respBody, err := io.ReadAll(io.LimitReader(upstreamResp.Body, 10*1024*1024))
		if err != nil {
			writeErr(w, http.StatusBadGateway, "read upstream: "+err.Error())
			return
		}

		// Parse response to extract usage.
		var chatResp ChatResponse
		_ = json.Unmarshal(respBody, &chatResp)

		// Record usage.
		estimated := false
		var inputTokens, outputTokens int64
		if chatResp.Usage != nil {
			inputTokens = chatResp.Usage.PromptTokens
			outputTokens = chatResp.Usage.CompletionTokens
		} else {
			// Estimate from content.
			inputTokens = estimateTokens(extractInputText(&chatReq))
			outputTokens = estimateTokens(extractOutputText(&chatResp))
			estimated = true
		}

		if p.usageStore != nil {
			model := chatResp.Model
			if model == "" {
				model = chatReq.Model
			}
			rec := TokenUsageRecord{
				DiWorkerID:   diworkerID,
				ProviderName: provider.Name,
				Model:        model,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				TotalTokens:  inputTokens + outputTokens,
				Estimated:    estimated,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
			}
			if err := p.usageStore.RecordUsage(ctx, rec); err != nil {
				log.Printf("[compute-proxy] record usage error: %v", err)
			}
		}

		// Forward the response as-is.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstreamResp.StatusCode)
		w.Write(respBody)
	}
}

// buildUpstreamRequest constructs the HTTP request for the upstream provider.
// For OpenAI protocol, it's a passthrough. For others, the body is forwarded
// as-is since the Center-side protocol conversion is handled separately.
func (p *LLMProxy) buildUpstreamRequest(rawBody []byte, req *ChatRequest, provider *ProviderConfig) (*http.Request, error) {
	url := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(rawBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	if provider.UserAgent != "" {
		httpReq.Header.Set("User-Agent", provider.UserAgent)
	}

	return httpReq, nil
}

func extractInputText(req *ChatRequest) string {
	if req == nil {
		return ""
	}
	var parts []string
	for _, msg := range req.Messages {
		if c, ok := msg["content"].(string); ok {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, "\n")
}

func extractOutputText(resp *ChatResponse) string {
	if resp == nil {
		return ""
	}
	var parts []string
	for _, c := range resp.Choices {
		if c.Message.Content != "" {
			parts = append(parts, c.Message.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// estimateTokens approximates token count from text using ~4 chars per token.
func estimateTokens(text string) int64 {
	n := int64(len(text)) / 4
	if n < 1 && len(text) > 0 {
		n = 1
	}
	return n
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
