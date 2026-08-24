package llm

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestParseAnthropicResponseBodyCapturesThinking(t *testing.T) {
	body := []byte("{\"content\":[{\"type\":\"thinking\",\"thinking\":\"Check the request first.\"},{\"type\":\"text\",\"text\":\"Done.\"}],\"stop_reason\":\"end_turn\"}")
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
}

func TestParseAnthropicSSEStreamForwardsThinkingDelta(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Check inputs. "}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Then answer."}}`,
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
	if thinking["budget_tokens"] == nil {
		t.Fatalf("auto glm-5.3 thinking missing budget: %#v", thinking)
	}
}
