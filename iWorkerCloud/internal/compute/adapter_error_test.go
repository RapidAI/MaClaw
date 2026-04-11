package compute

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ConvertErrorResponse
// ---------------------------------------------------------------------------

func TestConvertErrorResponse_Basic(t *testing.T) {
	resp := &http.Response{
		StatusCode: 429,
		Body:       io.NopCloser(strings.NewReader(`{"error":"rate limit exceeded"}`)),
	}

	result, statusCode := ConvertErrorResponse(resp, "openai")

	if statusCode != 429 {
		t.Errorf("expected status 429, got %d", statusCode)
	}
	if result.Error == nil {
		t.Fatal("expected non-nil error payload")
	}
	if result.Error.Type != "upstream_openai_error" {
		t.Errorf("expected type 'upstream_openai_error', got %q", result.Error.Type)
	}
	if result.Error.Code != "HTTP_429" {
		t.Errorf("expected code 'HTTP_429', got %q", result.Error.Code)
	}
	if result.Error.Message != `{"error":"rate limit exceeded"}` {
		t.Errorf("unexpected message: %q", result.Error.Message)
	}
}

func TestConvertErrorResponse_Anthropic(t *testing.T) {
	resp := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("internal server error")),
	}

	result, statusCode := ConvertErrorResponse(resp, "anthropic")

	if statusCode != 500 {
		t.Errorf("expected status 500, got %d", statusCode)
	}
	if result.Error.Type != "upstream_anthropic_error" {
		t.Errorf("expected type 'upstream_anthropic_error', got %q", result.Error.Type)
	}
	if result.Error.Code != "HTTP_500" {
		t.Errorf("expected code 'HTTP_500', got %q", result.Error.Code)
	}
}

func TestConvertErrorResponse_Gemini(t *testing.T) {
	resp := &http.Response{
		StatusCode: 403,
		Body:       io.NopCloser(strings.NewReader("forbidden")),
	}

	result, statusCode := ConvertErrorResponse(resp, "gemini")

	if statusCode != 403 {
		t.Errorf("expected status 403, got %d", statusCode)
	}
	if result.Error.Type != "upstream_gemini_error" {
		t.Errorf("expected type 'upstream_gemini_error', got %q", result.Error.Type)
	}
}

func TestConvertErrorResponse_TruncatesLongBody(t *testing.T) {
	longBody := strings.Repeat("x", 1000)
	resp := &http.Response{
		StatusCode: 502,
		Body:       io.NopCloser(strings.NewReader(longBody)),
	}

	result, _ := ConvertErrorResponse(resp, "openai")

	if len(result.Error.Message) != 500 {
		t.Errorf("expected message truncated to 500 chars, got %d", len(result.Error.Message))
	}
}

func TestConvertErrorResponse_EmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 503,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	result, statusCode := ConvertErrorResponse(resp, "openai")

	if statusCode != 503 {
		t.Errorf("expected status 503, got %d", statusCode)
	}
	if result.Error.Message != "" {
		t.Errorf("expected empty message, got %q", result.Error.Message)
	}
}

func TestConvertErrorResponse_NilResponse(t *testing.T) {
	result, statusCode := ConvertErrorResponse(nil, "openai")

	if statusCode != 0 {
		t.Errorf("expected status 0 for nil response, got %d", statusCode)
	}
	if result.Error == nil {
		t.Fatal("expected non-nil error payload")
	}
	if result.Error.Message != "nil response" {
		t.Errorf("expected 'nil response', got %q", result.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// EstimateTokenUsage
// ---------------------------------------------------------------------------

func TestEstimateTokenUsage_Basic(t *testing.T) {
	// "Hello" = 5 chars → (5+3)/4 = 2 tokens
	// "World!" = 6 chars → (6+3)/4 = 2 tokens
	usage := EstimateTokenUsage("Hello", "World!")

	if usage.InputTokens != 2 {
		t.Errorf("expected 2 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 2 {
		t.Errorf("expected 2 output tokens, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 4 {
		t.Errorf("expected 4 total tokens, got %d", usage.TotalTokens)
	}
}

func TestEstimateTokenUsage_EmptyInput(t *testing.T) {
	usage := EstimateTokenUsage("", "response text")

	if usage.InputTokens != 0 {
		t.Errorf("expected 0 input tokens for empty input, got %d", usage.InputTokens)
	}
	if usage.OutputTokens <= 0 {
		t.Errorf("expected positive output tokens, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != usage.InputTokens+usage.OutputTokens {
		t.Errorf("total should equal input + output: %d != %d + %d",
			usage.TotalTokens, usage.InputTokens, usage.OutputTokens)
	}
}

func TestEstimateTokenUsage_EmptyBoth(t *testing.T) {
	usage := EstimateTokenUsage("", "")

	if usage.InputTokens != 0 {
		t.Errorf("expected 0 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 0 {
		t.Errorf("expected 0 output tokens, got %d", usage.OutputTokens)
	}
	if usage.TotalTokens != 0 {
		t.Errorf("expected 0 total tokens, got %d", usage.TotalTokens)
	}
}

func TestEstimateTokenUsage_SingleChar(t *testing.T) {
	// "a" = 1 char → (1+3)/4 = 1 token
	usage := EstimateTokenUsage("a", "")

	if usage.InputTokens != 1 {
		t.Errorf("expected 1 input token for single char, got %d", usage.InputTokens)
	}
}

func TestEstimateTokenUsage_ExactMultipleOf4(t *testing.T) {
	// "abcd" = 4 chars → (4+3)/4 = 1 token
	usage := EstimateTokenUsage("abcd", "")

	if usage.InputTokens != 1 {
		t.Errorf("expected 1 input token for 4 chars, got %d", usage.InputTokens)
	}
}

func TestEstimateTokenUsage_TotalEqualsSum(t *testing.T) {
	usage := EstimateTokenUsage("some input text here", "and some output text")

	if usage.TotalTokens != usage.InputTokens+usage.OutputTokens {
		t.Errorf("total should equal input + output: %d != %d + %d",
			usage.TotalTokens, usage.InputTokens, usage.OutputTokens)
	}
}

func TestEstimateTokenUsage_PositiveForNonEmpty(t *testing.T) {
	usage := EstimateTokenUsage("x", "y")

	if usage.InputTokens < 1 {
		t.Errorf("expected at least 1 input token, got %d", usage.InputTokens)
	}
	if usage.OutputTokens < 1 {
		t.Errorf("expected at least 1 output token, got %d", usage.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
// estimateTokens (internal helper)
// ---------------------------------------------------------------------------

func TestEstimateTokens_Empty(t *testing.T) {
	if got := estimateTokens(""); got != 0 {
		t.Errorf("expected 0 for empty string, got %d", got)
	}
}

func TestEstimateTokens_RoundsUp(t *testing.T) {
	// 5 chars → (5+3)/4 = 2
	if got := estimateTokens("hello"); got != 2 {
		t.Errorf("expected 2 for 5 chars, got %d", got)
	}
	// 7 chars → (7+3)/4 = 2
	if got := estimateTokens("abcdefg"); got != 2 {
		t.Errorf("expected 2 for 7 chars, got %d", got)
	}
	// 8 chars → (8+3)/4 = 2
	if got := estimateTokens("abcdefgh"); got != 2 {
		t.Errorf("expected 2 for 8 chars, got %d", got)
	}
	// 9 chars → (9+3)/4 = 3
	if got := estimateTokens("abcdefghi"); got != 3 {
		t.Errorf("expected 3 for 9 chars, got %d", got)
	}
}
