package compute

import (
	"fmt"
	"io"
	"net/http"
)

// ConvertErrorResponse reads the body of a non-200 HTTP response and converts
// it into an OpenAI-format error response. It returns the converted response
// and the original HTTP status code. The body is limited to 64 KB and the
// error message is truncated to 500 characters.
func ConvertErrorResponse(resp *http.Response, protocol string) (*OpenAIChatResponse, int) {
	if resp == nil {
		return &OpenAIChatResponse{
			Error: &OpenAIErrorPayload{
				Message: "nil response",
				Type:    "upstream_error",
				Code:    "HTTP_0",
			},
		}, 0
	}

	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	msg := string(body)
	if len(msg) > 500 {
		msg = msg[:500]
	}

	return &OpenAIChatResponse{
		Error: &OpenAIErrorPayload{
			Message: msg,
			Type:    fmt.Sprintf("upstream_%s_error", protocol),
			Code:    fmt.Sprintf("HTTP_%d", resp.StatusCode),
		},
	}, resp.StatusCode
}

// EstimateTokenUsage estimates token counts from input and output text using
// a simple character-based heuristic (~4 characters per token). The caller
// should mark the resulting TokenUsageRecord with Estimated: true.
func EstimateTokenUsage(inputText, outputText string) *TokenUsage {
	inputTokens := estimateTokens(inputText)
	outputTokens := estimateTokens(outputText)
	return &TokenUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
	}
}

// estimateTokens returns an approximate token count for the given text.
// Heuristic: ~4 characters per token, rounded up, minimum 1 for non-empty text.
func estimateTokens(text string) int64 {
	if len(text) == 0 {
		return 0
	}
	tokens := (len(text) + 3) / 4
	if tokens < 1 {
		tokens = 1
	}
	return int64(tokens)
}
