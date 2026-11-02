package llm

import (
	"strings"
	"testing"
)

func TestPresentOpenCodeFreeMessageUsesCallerTools(t *testing.T) {
	msg := PresentOpenCodeFreeMessage([]string{"bash", "read_file", "web_fetch", "web_search"}, Message{
		Content:          "think",
		ReasoningContent: "think",
		ToolCalls: []ToolCall{
			{ID: "1", Function: ToolCallFunction{Name: "shell", Arguments: `{"command":"python gen.py","workdir":"D:\\oc-empty","timeout":120000}`}},
			{ID: "2", Function: ToolCallFunction{Name: "read", Arguments: `{"filePath":"a.txt","offset":10,"limit":20}`}},
			{ID: "3", Function: ToolCallFunction{Name: "webfetch", Arguments: `{"url":"https://example.com"}`}},
			{ID: "4", Function: ToolCallFunction{Name: "execute", Arguments: `{"code":"1"}`}},
		},
	})
	if msg.Content != "" || msg.ReasoningContent != "think" {
		t.Fatalf("content %q reasoning %q", msg.Content, msg.ReasoningContent)
	}
	if msg.ToolCalls[0].Function.Name != "bash" || !strings.Contains(msg.ToolCalls[0].Function.Arguments, `"working_dir"`) || !strings.Contains(msg.ToolCalls[0].Function.Arguments, `"timeout":120`) {
		t.Fatalf("shell = %s %s", msg.ToolCalls[0].Function.Name, msg.ToolCalls[0].Function.Arguments)
	}
	if msg.ToolCalls[1].Function.Name != "read_file" || strings.Contains(msg.ToolCalls[1].Function.Arguments, "filePath") || !strings.Contains(msg.ToolCalls[1].Function.Arguments, `"start_line":10`) {
		t.Fatalf("read = %s %s", msg.ToolCalls[1].Function.Name, msg.ToolCalls[1].Function.Arguments)
	}
	if msg.ToolCalls[2].Function.Name != "web_fetch" || msg.ToolCalls[3].Function.Name != "execute" {
		t.Fatalf("calls = %#v", msg.ToolCalls)
	}
}

func TestOpenCodeFreeVisibleTextDoesNotDuplicate(t *testing.T) {
	content, reasoning := OpenCodeFreeVisibleText("", "think", nil)
	if content != "think" || reasoning != "" {
		t.Fatalf("text answer content=%q reasoning=%q", content, reasoning)
	}
	content, reasoning = OpenCodeFreeVisibleText("", "think", []ToolCall{{}})
	if content != "" || reasoning != "think" {
		t.Fatalf("tool turn content=%q reasoning=%q", content, reasoning)
	}
	if got := OpenCodeFreeFinishReason("stop", []ToolCall{{}}); got != "tool_calls" {
		t.Fatalf("finish = %s", got)
	}
	if got := OpenCodeFreeFinishReason("length", []ToolCall{{}}); got != "length" {
		t.Fatalf("length finish = %s", got)
	}
	msg := PresentOpenCodeFreeMessage(nil, Message{ReasoningContent: "hello"})
	if msg.Content != "hello" || msg.ReasoningContent != "" {
		t.Fatalf("text reply content=%q reasoning=%q", msg.Content, msg.ReasoningContent)
	}
}

func TestNormalizeOpenCodeFreeChoiceKeepsResponsesTools(t *testing.T) {
	tool := NormalizeOpenCodeFreeChoice(Choice{
		FinishReason: "stop",
		Message: Message{
			Content:          "think",
			ReasoningContent: "think",
			ToolCalls:        []ToolCall{{Function: ToolCallFunction{Name: "shell", Arguments: `{"command":"ls","workdir":"/tmp"}`}}},
		},
	})
	if tool.FinishReason != "tool_calls" || tool.Message.Content != "" || tool.Message.ReasoningContent != "think" {
		t.Fatalf("tool choice = %+v", tool)
	}
	if tool.Message.ToolCalls[0].Function.Name != "shell" || !strings.Contains(tool.Message.ToolCalls[0].Function.Arguments, `"workdir"`) {
		t.Fatalf("responses tool was renamed: %+v", tool.Message.ToolCalls[0].Function)
	}
	text := NormalizeOpenCodeFreeChoice(Choice{Message: Message{ReasoningContent: "hello"}})
	if text.Message.Content != "hello" || text.Message.ReasoningContent != "" || text.FinishReason != "stop" {
		t.Fatalf("text choice = %+v", text.Message)
	}
}

func TestPresentOpenCodeFreeMessageKeepsDeclaredShell(t *testing.T) {
	msg := PresentOpenCodeFreeMessage([]string{"shell"}, Message{
		ToolCalls: []ToolCall{{
			Function: ToolCallFunction{Name: "shell", Arguments: `{"command":"ls","workdir":"/tmp","timeout":120000}`},
		}},
	})
	args := msg.ToolCalls[0].Function.Arguments
	if msg.ToolCalls[0].Function.Name != "shell" || !strings.Contains(args, `"workdir"`) || !strings.Contains(args, `"timeout":120000`) {
		t.Fatalf("shell was rewritten for a caller that declared it: %s %s", msg.ToolCalls[0].Function.Name, args)
	}
}
