package corelib

import "testing"

func TestApplyMemberUpstreamBodyMergesAndDrops(t *testing.T) {
	body := map[string]any{
		"model":    "keep-me",
		"messages": []any{},
		"stream":   true,
		"thinking": map[string]any{"type": "enabled"},
		"nested":   map[string]any{"keep": "downstream", "model": "inner"},
	}
	ApplyMemberUpstreamBody(body, map[string]any{
		"model":            "hijack",
		"reasoning_effort": "none",
		"thinking":         map[string]any{"type": "disabled"},
		"nested": map[string]any{
			"keep":  "member",
			"added": true,
			"model": "still-inner",
		},
	}, "", []string{"thinking", "model"})

	if body["model"] != "keep-me" || body["stream"] != true {
		t.Fatalf("protected keys changed: %#v", body)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("thinking survived the drop: %#v", body["thinking"])
	}
	if body["reasoning_effort"] != "none" {
		t.Fatalf("reasoning_effort = %#v", body["reasoning_effort"])
	}
	nested, _ := body["nested"].(map[string]any)
	if nested["keep"] != "member" || nested["added"] != true || nested["model"] != "inner" {
		t.Fatalf("nested = %#v", nested)
	}
}

func TestApplyMemberUpstreamBodyDownstreamKeepsExistingKeys(t *testing.T) {
	body := map[string]any{
		"reasoning_effort": "high",
		"nested":           map[string]any{"keep": "downstream"},
	}
	ApplyMemberUpstreamBody(body, map[string]any{
		"reasoning_effort": "none",
		"temperature":      0.2,
		"nested":           map[string]any{"keep": "member", "added": 1},
	}, "DOWNSTREAM", nil)
	if body["reasoning_effort"] != "high" || body["temperature"] != 0.2 {
		t.Fatalf("downstream merge = %#v", body)
	}
	nested, _ := body["nested"].(map[string]any)
	if nested["keep"] != "downstream" || nested["added"] != 1 {
		t.Fatalf("nested = %#v", nested)
	}
}

func TestApplyMemberUpstreamBodyLeavesSharedNestedMapAlone(t *testing.T) {
	nested := map[string]any{"keep": "downstream"}
	body := map[string]any{"nested": nested}
	ApplyMemberUpstreamBody(body, map[string]any{
		"nested": map[string]any{"added": true},
	}, "", nil)
	if _, ok := nested["added"]; ok {
		t.Fatalf("shared nested map changed: %#v", nested)
	}
	got, _ := body["nested"].(map[string]any)
	if got["keep"] != "downstream" || got["added"] != true {
		t.Fatalf("merged nested = %#v", got)
	}
}

func TestFitAnthropicThinkingBudgetUsesMemberJSONNumbers(t *testing.T) {
	body := map[string]any{
		"max_tokens": float64(128),
		"thinking":   map[string]any{"type": "enabled", "budget_tokens": float64(9000)},
	}
	FitAnthropicThinkingBudget(body)
	thinking, _ := body["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 127 || body["max_tokens"] != float64(128) {
		t.Fatalf("fitted body = %#v", body)
	}

	small := map[string]any{
		"max_tokens": 8,
		"thinking":   map[string]any{"type": "enabled", "budget_tokens": 7},
	}
	FitAnthropicThinkingBudget(small)
	kept, _ := small["thinking"].(map[string]any)
	if kept["budget_tokens"] != 7 {
		t.Fatalf("small budget changed: %#v", small["thinking"])
	}
}

func TestApplyMemberUpstreamBodyEmptyIsNoOp(t *testing.T) {
	body := map[string]any{"thinking": map[string]any{"type": "enabled"}}
	ApplyMemberUpstreamBody(body, nil, "", nil)
	ApplyMemberUpstreamBody(body, map[string]any{}, "member", []string{})
	thinking, _ := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || len(body) != 1 {
		t.Fatalf("empty override changed the body: %#v", body)
	}
}
