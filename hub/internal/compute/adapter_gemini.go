package compute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Gemini request/response types for the generateContent API.

type geminiRequest struct {
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	Contents          []geminiContent  `json:"contents"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsageMeta  `json:"usageMetadata,omitempty"`
	ModelVersion  string            `json:"modelVersion,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiUsageMeta struct {
	PromptTokenCount     int64 `json:"promptTokenCount"`
	CandidatesTokenCount int64 `json:"candidatesTokenCount"`
	TotalTokenCount      int64 `json:"totalTokenCount"`
}

// GeminiAdapter converts between OpenAI format and Gemini generateContent API.
type GeminiAdapter struct{}

// ConvertRequest builds an HTTP request for the Gemini generateContent API
// from an OpenAI-format chat request.
func (a *GeminiAdapter) ConvertRequest(req *OpenAIChatRequest, provider *ComputeProvider) (*http.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if provider == nil {
		return nil, fmt.Errorf("provider is nil")
	}

	// Extract system messages and build contents from non-system messages.
	var systemParts []geminiPart
	var contents []geminiContent
	for _, msg := range req.Messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" {
			if content != "" {
				systemParts = append(systemParts, geminiPart{Text: content})
			}
			continue
		}
		// Map OpenAI "assistant" role to Gemini "model" role.
		geminiRole := role
		if role == "assistant" {
			geminiRole = "model"
		}
		contents = append(contents, geminiContent{
			Role:  geminiRole,
			Parts: []geminiPart{{Text: content}},
		})
	}

	gemReq := geminiRequest{
		Contents: contents,
	}
	if len(systemParts) > 0 {
		gemReq.SystemInstruction = &geminiContent{
			Parts: systemParts,
		}
	}

	// Build generationConfig from OpenAI parameters.
	var genCfg geminiGenConfig
	hasGenCfg := false
	if req.MaxTokens != nil {
		genCfg.MaxOutputTokens = req.MaxTokens
		hasGenCfg = true
	}
	if req.Temperature != nil {
		genCfg.Temperature = req.Temperature
		hasGenCfg = true
	}
	if req.TopP != nil {
		genCfg.TopP = req.TopP
		hasGenCfg = true
	}
	if req.Stop != nil {
		switch v := req.Stop.(type) {
		case string:
			if v != "" {
				genCfg.StopSequences = []string{v}
				hasGenCfg = true
			}
		case []interface{}:
			for _, s := range v {
				if str, ok := s.(string); ok {
					genCfg.StopSequences = append(genCfg.StopSequences, str)
				}
			}
			if len(genCfg.StopSequences) > 0 {
				hasGenCfg = true
			}
		}
	}
	if hasGenCfg {
		gemReq.GenerationConfig = &genCfg
	}

	body, err := json.Marshal(gemReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// URL: base_url/models/{model}:generateContent?key=xxx
	model := req.Model
	if model == "" {
		model = provider.Model
	}
	url := strings.TrimRight(provider.BaseURL, "/") + "/models/" + model + ":generateContent"
	url += "?key=" + provider.APIKey

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if provider.UserAgent != "" {
		httpReq.Header.Set("User-Agent", provider.UserAgent)
	}

	return httpReq, nil
}

// ConvertResponse parses a Gemini generateContent response into OpenAI format.
func (a *GeminiAdapter) ConvertResponse(resp *http.Response) (*OpenAIChatResponse, error) {
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(body, &gemResp); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}

	result := &OpenAIChatResponse{
		Object: "chat.completion",
		Model:  gemResp.ModelVersion,
	}

	// Convert candidates to choices.
	for i, cand := range gemResp.Candidates {
		var content string
		if len(cand.Content.Parts) > 0 {
			content = cand.Content.Parts[0].Text
		}
		result.Choices = append(result.Choices, OpenAIChoice{
			Index:        i,
			Message:      OpenAIMessage{Role: "assistant", Content: content},
			FinishReason: mapGeminiFinishReason(cand.FinishReason),
		})
	}

	// Convert usage metadata.
	if gemResp.UsageMetadata != nil {
		result.Usage = &TokenUsage{
			InputTokens:  gemResp.UsageMetadata.PromptTokenCount,
			OutputTokens: gemResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  gemResp.UsageMetadata.PromptTokenCount + gemResp.UsageMetadata.CandidatesTokenCount,
		}
	}

	return result, nil
}

// ExtractUsage returns token usage from the converted OpenAI response.
func (a *GeminiAdapter) ExtractUsage(resp *OpenAIChatResponse) *TokenUsage {
	if resp == nil {
		return nil
	}
	return resp.Usage
}

// mapGeminiFinishReason converts Gemini finish reasons to OpenAI finish reasons.
func mapGeminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}
