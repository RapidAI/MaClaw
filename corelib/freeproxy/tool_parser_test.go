package freeproxy

import "testing"

func TestExtractToolCalls_CodeFence(t *testing.T) {
	content := "before\n```tool_call\n{\"name\":\"search\",\"arguments\":{\"q\":\"golang\"}}\n```\nafter"
	calls := extractToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Type != "function" {
		t.Fatalf("expected function type, got %q", calls[0].Type)
	}
	if calls[0].Function.Name != "search" {
		t.Fatalf("expected tool name search, got %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"q":"golang"}` {
		t.Fatalf("expected arguments JSON, got %q", calls[0].Function.Arguments)
	}
}

func TestExtractToolCalls_XML(t *testing.T) {
	content := "prefix\n<tool_call>{\"name\":\"lookup\",\"arguments\":{\"id\":123}}</tool_call>\nsuffix"
	calls := extractToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "lookup" {
		t.Fatalf("expected tool name lookup, got %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"id":123}` {
		t.Fatalf("expected arguments JSON, got %q", calls[0].Function.Arguments)
	}
}

func TestRemoveToolCallBlocks_RemovesBothFormats(t *testing.T) {
	content := "hello\n```tool_call\n{\"name\":\"search\",\"arguments\":{\"q\":\"go\"}}\n```\n<tool_call>{\"name\":\"lookup\",\"arguments\":{\"id\":1}}</tool_call>\nworld"
	got := removeToolCallBlocks(content)
	want := "hello\n\nworld"
	if got != want {
		t.Fatalf("removeToolCallBlocks() = %q, want %q", got, want)
	}
}

func TestExtractToolCalls_PrefersCodeFenceWhenBothPresent(t *testing.T) {
	content := "```tool_call\n{\"name\":\"search\",\"arguments\":{\"q\":\"go\"}}\n```\n<tool_call>{\"name\":\"lookup\",\"arguments\":{\"id\":1}}</tool_call>"
	calls := extractToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "search" {
		t.Fatalf("expected fenced tool call to win, got %q", calls[0].Function.Name)
	}
}

func TestExtractToolCalls_AllowsArrayArguments(t *testing.T) {
	content := "```tool_call\n{\"name\":\"search\",\"arguments\":[\"go\",\"json\"]}\n```"
	calls := extractToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Arguments != `["go","json"]` {
		t.Fatalf("expected array arguments JSON, got %q", calls[0].Function.Arguments)
	}
}

func TestExtractToolCalls_SkipsEmptyName(t *testing.T) {
	content := "```tool_call\n{\"name\":\"\",\"arguments\":{\"q\":\"go\"}}\n```"
	calls := extractToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %#v", calls)
	}
}
