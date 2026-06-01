package main

import "testing"

func TestParseXMLContentToolCalls(t *testing.T) {
	content := "prefix\n<tool_call>{\"name\":\"lookup\",\"arguments\":{\"id\":123}}</tool_call>\nsuffix"
	calls := parseXMLContentToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Type != "function" {
		t.Fatalf("expected function type, got %q", calls[0].Type)
	}
	if calls[0].Function.Name != "lookup" {
		t.Fatalf("expected tool name lookup, got %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"id":123}` {
		t.Fatalf("expected arguments JSON, got %q", calls[0].Function.Arguments)
	}
}

func TestParseXMLContentToolCalls_Multiple(t *testing.T) {
	content := `<tool_call>{"name":"one","arguments":{"n":1}}</tool_call><tool_call>{"name":"two","arguments":{"n":2}}</tool_call>`
	calls := parseXMLContentToolCalls(content)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "one" || calls[1].Function.Name != "two" {
		t.Fatalf("unexpected calls: %#v", calls)
	}
}

func TestParseXMLContentToolCalls_AllowsArrayArguments(t *testing.T) {
	content := "<tool_call>{\"name\":\"search\",\"arguments\":[\"go\",\"json\"]}</tool_call>"
	calls := parseXMLContentToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Arguments != `["go","json"]` {
		t.Fatalf("expected array arguments JSON, got %q", calls[0].Function.Arguments)
	}
}

func TestParseXMLContentToolCalls_SkipsEmptyName(t *testing.T) {
	content := "<tool_call>{\"name\":\"\",\"arguments\":{\"q\":\"go\"}}</tool_call>"
	calls := parseXMLContentToolCalls(content)
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %#v", calls)
	}
}

func TestParseXMLContentToolCalls_IgnoresInvalidJSON(t *testing.T) {
	calls := parseXMLContentToolCalls("<tool_call>{bad json}</tool_call>")
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %#v", calls)
	}
}
