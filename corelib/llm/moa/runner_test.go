package moa

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestRunnerPartialFailureContinues(t *testing.T) {
	calls := 0
	r := &Runner{
		CallRef: func(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}) (*llm.Response, error) {
			calls++
			if cfg.Model == "bad" {
				return nil, context.DeadlineExceeded
			}
			return &llm.Response{
				Choices: []llm.Choice{{Message: llm.Message{Content: "advice from " + cfg.Model}}},
				Usage:   &llm.Usage{PromptTokens: 10, CompletionTokens: 5},
			}, nil
		},
	}
	preset := ResolvedPreset{
		Name:                "review",
		Enabled:             true,
		ReferenceTimeoutSec: 5,
		References: []ResolvedRef{
			{Label: "good", Config: corelib.MaclawLLMConfig{URL: "http://x", Model: "good", Key: "k"}},
			{Label: "bad", Config: corelib.MaclawLLMConfig{URL: "http://x", Model: "bad", Key: "k"}},
		},
	}
	conv := []interface{}{
		map[string]string{"role": "user", "content": "plan?"},
	}
	out := r.RunReferences(context.Background(), preset, conv)
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if out.Advice == "" {
		t.Fatal("expected advice block")
	}
	if len(out.Calls) != 1 {
		t.Fatalf("usage calls=%d", len(out.Calls))
	}
	ok := false
	for _, it := range out.Items {
		if it.Label == "good" && it.Content != "" {
			ok = true
		}
		if it.Label == "bad" && it.Error == "" {
			t.Fatal("expected error on bad")
		}
	}
	if !ok {
		t.Fatalf("items %#v", out.Items)
	}
	if out.RefOK != 1 || out.RefFail != 1 {
		t.Fatalf("ok=%d fail=%d", out.RefOK, out.RefFail)
	}
	if out.Duration <= 0 && out.Progress == "" {
		t.Fatal("expected duration/progress")
	}
}

func TestRunnerSkipsErrorPlaceholders(t *testing.T) {
	calls := 0
	r := &Runner{
		CallRef: func(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}) (*llm.Response, error) {
			calls++
			return &llm.Response{
				Choices: []llm.Choice{{Message: llm.Message{Content: "ok"}}},
			}, nil
		},
	}
	out := r.RunReferences(context.Background(), ResolvedPreset{
		References: []ResolvedRef{
			{Label: "bad", Config: corelib.MaclawLLMConfig{ProviderName: "error:nope"}},
			{Label: "good", Config: corelib.MaclawLLMConfig{URL: "http://x", Model: "m"}},
		},
		ReferenceTimeoutSec: 5,
	}, []interface{}{map[string]string{"role": "user", "content": "q"}})
	if calls != 1 {
		t.Fatalf("calls=%d want 1 (skip error placeholder)", calls)
	}
	if out.RefOK != 1 || out.RefFail != 1 {
		t.Fatalf("ok=%d fail=%d", out.RefOK, out.RefFail)
	}
}
