package llm

import (
	"strings"
	"testing"
)

func TestUsageUnmarshalKeepsSettledCredits(t *testing.T) {
	var usage Usage
	if err := usage.UnmarshalJSON([]byte(`{"prompt_tokens":10,"completion_tokens":2,"credits_deducted":0.5}`)); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 2 {
		t.Fatalf("tokens = %+v", usage)
	}
	if usage.CreditsDeducted == nil || *usage.CreditsDeducted != 0.5 {
		t.Fatalf("credits = %#v", usage.CreditsDeducted)
	}
	var zero Usage
	if err := zero.UnmarshalJSON([]byte(`{"prompt_tokens":1,"credits_deducted":0}`)); err != nil {
		t.Fatal(err)
	}
	if zero.CreditsDeducted == nil || *zero.CreditsDeducted != 0 {
		t.Fatalf("settled zero = %#v", zero.CreditsDeducted)
	}
}

func TestParseSSEStreamKeepsCreditsOnStopChunk(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"credits_deducted":1.25}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp, err := parseSSEStreamWithReasoning(strings.NewReader(body), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Usage == nil || resp.Usage.CreditsDeducted == nil || *resp.Usage.CreditsDeducted != 1.25 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	if resp.Usage.PromptTokens != 12 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestMergeSSEUsageKeepsCreditsWhenLaterChunkHasOnlyTokens(t *testing.T) {
	credits := 0.5
	prev := &Usage{PromptTokens: 8, CompletionTokens: 2, CreditsDeducted: &credits}
	next := &Usage{PromptTokens: 9, CompletionTokens: 3}
	merged := mergeSSEUsage(prev, next)
	if merged.PromptTokens != 9 || merged.CreditsDeducted == nil || *merged.CreditsDeducted != 0.5 {
		t.Fatalf("merged = %+v", merged)
	}
}
