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

func TestParseXMLContentToolCalls_CodexInvoke(t *testing.T) {
	content := `<turn: tool_call>
<invoke name="bash">
<parameter name="description" string="true">Check existing project directory</parameter>
<parameter name="command" string="true">if exist D:\gametest\15\ (dir /B D:\gametest\15\) else (echo DIRECTORY_NOT_EXIST)</parameter>
<parameter name="timeout" string="false">5000</parameter>
</invoke>
</turn>`
	calls, malformed := parseXMLContentToolCallsDetailed(content)
	if malformed {
		t.Fatalf("expected codex tool call to parse cleanly")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "bash" {
		t.Fatalf("expected tool name bash, got %q", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"command":"if exist D:\\gametest\\15\\ (dir /B D:\\gametest\\15\\) else (echo DIRECTORY_NOT_EXIST)","description":"Check existing project directory","timeout":5000}` {
		t.Fatalf("unexpected arguments JSON: %q", calls[0].Function.Arguments)
	}
}

func TestParseXMLContentToolCalls_CodexInvokeMalformed(t *testing.T) {
	calls, malformed := parseXMLContentToolCallsDetailed(`<turn: tool_call><invoke><parameter name="x">y</parameter></invoke></turn>`)
	if !malformed {
		t.Fatalf("expected malformed codex tool call")
	}
	if len(calls) != 0 {
		t.Fatalf("expected no parsed tool calls, got %#v", calls)
	}
}

func TestParseXMLContentToolCalls_CodexInvokeTruncated(t *testing.T) {
	calls, malformed := parseXMLContentToolCallsDetailed(`<turn: tool_call><invoke name="bash">`)
	if !malformed {
		t.Fatalf("expected truncated codex tool call to be reported malformed")
	}
	if len(calls) != 0 {
		t.Fatalf("expected no parsed tool calls, got %#v", calls)
	}
}
