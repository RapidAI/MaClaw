package llm

import (
	"strings"
	"testing"
)

func TestAnswerFromReasoningOnlyKeepsThinkBody(t *testing.T) {
	got := AnswerFromReasoningOnly("<think>{\"name\":\"审稿\"}</think>")
	if got != `{"name":"审稿"}` {
		t.Fatalf("think-only = %q", got)
	}
	if got := AnswerFromReasoningOnly("可见答案"); got != "可见答案" {
		t.Fatalf("plain = %q", got)
	}
	if got := AnswerFromReasoningOnly("<think>草稿</think>\n{\"name\":\"审稿\"}"); !strings.Contains(got, `"name":"审稿"`) {
		t.Fatalf("visible after think = %q", got)
	}
}

func TestOpenAISDKChunkDeltaTextAcceptsBlockReasoning(t *testing.T) {
	raw := `{"choices":[{"delta":{"reasoning_content":[{"type":"thinking","thinking":"{\"name\":\"审稿\"}"}]}}]}`
	content, reasoning := openAISDKChunkDeltaText(raw)
	if content != "" {
		t.Fatalf("content = %q", content)
	}
	if !strings.Contains(reasoning, "审稿") {
		t.Fatalf("reasoning = %q", reasoning)
	}
	content, reasoning = openAISDKChunkDeltaText(`{"choices":[{"delta":{"content":"","reasoning_content":"plain"}}]}`)
	if content != "" || reasoning != "plain" {
		t.Fatalf("string delta content=%q reasoning=%q", content, reasoning)
	}
}

func TestOpenAISDKChunkDeltaTextAcceptsContentBlocks(t *testing.T) {
	raw := `{"choices":[{"delta":{"content":[{"type":"text","text":"{\"name\":\"审稿\"}"}]}}]}`
	content, reasoning := openAISDKChunkDeltaText(raw)
	if !strings.Contains(content, "审稿") {
		t.Fatalf("content = %q reasoning=%q", content, reasoning)
	}
}

func TestOpenAISDKChunkDeltaTextKeepsThinkingBesideVisibleText(t *testing.T) {
	raw := `{"choices":[{"delta":{"content":[{"type":"text","text":"OK"},{"type":"thinking","thinking":"{\"name\":\"审稿\"}"}]}}]}`
	content, reasoning := openAISDKChunkDeltaText(raw)
	if content != "OK" {
		t.Fatalf("content = %q", content)
	}
	if !strings.Contains(reasoning, "审稿") {
		t.Fatalf("thinking block dropped: %q", reasoning)
	}
	content, reasoning = openAISDKChunkDeltaText(`{"choices":[{"delta":{"content":{"type":"text","text":"{\"name\":\"审稿\"}"}}}]}`)
	if !strings.Contains(content, "审稿") {
		t.Fatalf("single block content = %q reasoning=%q", content, reasoning)
	}
	content, reasoning = openAISDKChunkDeltaText(`{"choices":[{"delta":{"content":1,"reasoning_content":2}}]}`)
	if content != "" || reasoning != "" {
		t.Fatalf("scalar delta content=%q reasoning=%q", content, reasoning)
	}
}

func TestFinishOpenAISDKStreamMessageKeepsThinkBody(t *testing.T) {
	msg := finishOpenAISDKStreamMessage("<think>{\"name\":\"审稿\"}</think>", "")
	if strings.TrimSpace(msg.Content) != "" {
		t.Fatalf("content = %q, want empty visible body", msg.Content)
	}
	if !strings.Contains(msg.ReasoningContent, "审稿") {
		t.Fatalf("reasoning dropped the think body: %q", msg.ReasoningContent)
	}
}
