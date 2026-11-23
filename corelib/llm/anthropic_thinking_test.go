package llm

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestParseAnthropicResponseBodyCapturesThinking(t *testing.T) {
	body := []byte("{\"content\":[{\"type\":\"thinking\",\"thinking\":\"Check the request first.\",\"signature\":\"sig-parse\"},{\"type\":\"text\",\"text\":\"Done.\"}],\"stop_reason\":\"end_turn\"}")
	resp, err := parseAnthropicResponseBody(body)
	if err != nil {
		t.Fatalf("parseAnthropicResponseBody: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Check the request first."; got != want {
		t.Fatalf("reasoning = %q, want %q", got, want)
	}
	if got, want := resp.Choices[0].Message.Content, "Done."; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := resp.Choices[0].Message.ThoughtSignature, "sig-parse"; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestParseAnthropicSSEStreamForwardsThinkingDelta(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Check inputs. "}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Then answer."}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-stream"}}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Done."}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n"))
	var streamed, reasoning strings.Builder
	resp, err := parseAnthropicSSEStreamWithReasoning(body, func(delta string) {
		streamed.WriteString(delta)
	}, func(delta string) {
		reasoning.WriteString(delta)
	})
	if err != nil {
		t.Fatalf("parseAnthropicSSEStreamWithReasoning: %v", err)
	}
	if got, want := reasoning.String(), "Check inputs. Then answer."; got != want {
		t.Fatalf("streamed reasoning = %q, want %q", got, want)
	}
	if got, want := streamed.String(), "Done."; got != want {
		t.Fatalf("streamed text = %q, want %q", got, want)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Check inputs. Then answer."; got != want {
		t.Fatalf("reasoning = %q, want %q", got, want)
	}
	if got, want := resp.Choices[0].Message.Content, "Done."; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := resp.Choices[0].Message.ThoughtSignature, "sig-stream"; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestParseAnthropicSSEStreamAcceptsThinkingTextFallback(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","text":"Plan first."}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer."}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n"))
	resp, err := parseAnthropicSSEStream(body, nil)
	if err != nil {
		t.Fatalf("parseAnthropicSSEStream: %v", err)
	}
	if got, want := resp.Choices[0].Message.ReasoningContent, "Plan first."; got != want {
		t.Fatalf("reasoning = %q, want %q", got, want)
	}
	if got, want := resp.Choices[0].Message.Content, "Answer."; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestParseAnthropicSSEStreamRejectsConflictingProviderResponseIDs(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg-a"}}`,
		`data: {"type":"message_start","message":{"id":"msg-b"}}`,
		"",
	}, "\n"))
	if _, err := parseAnthropicSSEStream(body, nil); err == nil || !strings.Contains(err.Error(), "response ID changed") {
		t.Fatalf("conflicting Anthropic stream IDs error=%v", err)
	}
}

func TestBuildAnthropicMessagesRequestBodyAutoEnablesGLM53Thinking(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		AnthropicMessagesRequestOptions{MaxTokens: 8192},
	)
	thinking, _ := req["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("auto glm-5.3 thinking = %#v, want type=enabled", req["thinking"])
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("auto glm-5.3 must not send budget_tokens: %#v", thinking)
	}
}

func TestBuildAnthropicMessagesRequestBodyGLMFlashUsesLowEffort(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3-flash", ThinkingMode: "disabled"},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		AnthropicMessagesRequestOptions{MaxTokens: 900},
	)
	thinking, _ := req["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" || req["reasoning_effort"] != "low" {
		t.Fatalf("thinking = %#v effort = %#v, want enabled/low", req["thinking"], req["reasoning_effort"])
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("flash kept a budget: %#v", thinking)
	}
	if req["max_tokens"] != 900 {
		t.Fatalf("max_tokens = %#v, want 900", req["max_tokens"])
	}
}

func TestBuildAnthropicMessagesRequestBodyFitsMemberMaxTokens(t *testing.T) {
	req := BuildAnthropicMessagesRequestBody(
		corelib.MaclawLLMConfig{
			URL:             "https://api.anthropic.com",
			Model:           "claude-sonnet",
			ThinkingMode:    "enabled",
			MemberExtraBody: map[string]any{"max_tokens": float64(128)},
		},
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		AnthropicMessagesRequestOptions{MaxTokens: 8192},
	)
	if req["max_tokens"] != float64(128) {
		t.Fatalf("max_tokens = %#v, want the member cap", req["max_tokens"])
	}
	thinking, _ := req["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 127 {
		t.Fatalf("thinking = %#v, want budget 127", thinking)
	}
}

// parseAnthropicSSEStream shares the Chat Completions line scanner, so the same
// old 256 KiB limit aborted an Anthropic turn with "token too long"
// (BR-20261123-9FD249AF31B3). A large thinking delta inside the 4 MiB window
// must still stream.
func TestParseAnthropicSSEStreamAcceptsLinePastOldScanLimit(t *testing.T) {
	pad := strings.Repeat("h", 400*1024)
	body := "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"" + pad + "\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\n"
	if len(body) <= 256*1024 {
		t.Fatalf("fixture body is %d bytes, must exceed the old limit", len(body))
	}
	var thinking, text strings.Builder
	resp, err := parseAnthropicSSEStreamWithReasoning(strings.NewReader(body), func(d string) {
		text.WriteString(d)
	}, func(d string) {
		thinking.WriteString(d)
	})
	if err != nil {
		t.Fatalf("parseAnthropicSSEStreamWithReasoning: %v", err)
	}
	if resp == nil {
		t.Fatalf("nil response")
	}
	if got := thinking.Len(); got < 400*1024 {
		t.Fatalf("thinking length = %d, want at least %d", got, 400*1024)
	}
	if text.String() != "done" {
		t.Fatalf("text = %q", text.String())
	}
}

// A thinking line past the scan window must not erase the text that follows.
// Pairing the prefix with a read error makes the agent loop drop the turn.
func TestParseAnthropicSSEStreamKeepsTextWhenThinkingLineExceedsScanLimit(t *testing.T) {
	const maxLine = 8 * 1024
	saved := sseStreamMaxLineBytes
	sseStreamMaxLineBytes = maxLine
	t.Cleanup(func() { sseStreamMaxLineBytes = saved })

	body := "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"" + strings.Repeat("h", maxLine) + "\"}}\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"kept\"}}\n\n"
	var text strings.Builder
	resp, err := parseAnthropicSSEStreamWithReasoning(strings.NewReader(body), func(d string) {
		text.WriteString(d)
	}, nil)
	if err != nil {
		t.Fatalf("later text must not fail the turn: %v", err)
	}
	if resp == nil || text.String() != "kept" {
		t.Fatalf("text = %q resp = %#v", text.String(), resp)
	}
}
