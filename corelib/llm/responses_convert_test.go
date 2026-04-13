package llm

import (
	"encoding/json"
	"testing"
)

func TestConvertToResponsesInput_SystemMessage(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "system", "content": "You are helpful."},
		map[string]interface{}{"role": "user", "content": "Hi"},
	}
	got := ConvertToResponsesInput(msgs)
	if got.Instructions != "You are helpful." {
		t.Fatalf("Instructions = %q, want %q", got.Instructions, "You are helpful.")
	}
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
	item := got.Input[0].(map[string]interface{})
	if item["type"] != "message" || item["role"] != "user" {
		t.Fatalf("unexpected item: %v", item)
	}
}

func TestConvertToResponsesInput_MultipleSystemMessages(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "system", "content": "You are helpful."},
		map[string]interface{}{"role": "system", "content": "Be concise."},
		map[string]interface{}{"role": "user", "content": "Hi"},
	}
	got := ConvertToResponsesInput(msgs)
	want := "You are helpful.\nBe concise."
	if got.Instructions != want {
		t.Fatalf("Instructions = %q, want %q", got.Instructions, want)
	}
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
}

func TestConvertToResponsesInput_MapStringString(t *testing.T) {
	msgs := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": "hello"},
	}
	got := ConvertToResponsesInput(msgs)
	if got.Instructions != "sys" {
		t.Fatalf("Instructions = %q, want %q", got.Instructions, "sys")
	}
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
}

func TestConvertToResponsesInput_UserMessage(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "user", "content": "What is 2+2?"},
	}
	got := ConvertToResponsesInput(msgs)
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
	item := got.Input[0].(map[string]interface{})
	if item["type"] != "message" || item["role"] != "user" {
		t.Fatalf("unexpected type/role: %v", item)
	}
	content := item["content"].([]interface{})
	part := content[0].(map[string]interface{})
	if part["type"] != "input_text" || part["text"] != "What is 2+2?" {
		t.Fatalf("unexpected content part: %v", part)
	}
}

func TestConvertToResponsesInput_AssistantText(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "assistant", "content": "4"},
	}
	got := ConvertToResponsesInput(msgs)
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
	item := got.Input[0].(map[string]interface{})
	if item["type"] != "message" || item["role"] != "assistant" {
		t.Fatalf("unexpected type/role: %v", item)
	}
	content := item["content"].([]interface{})
	part := content[0].(map[string]interface{})
	if part["type"] != "output_text" || part["text"] != "4" {
		t.Fatalf("unexpected content part: %v", part)
	}
}

func TestConvertToResponsesInput_AssistantToolCalls_Typed(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []ToolCall{
				{
					ID:   "call_abc",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "bash", Arguments: `{"cmd":"ls"}`},
				},
			},
		},
	}
	got := ConvertToResponsesInput(msgs)
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
	item := got.Input[0].(map[string]interface{})
	if item["type"] != "function_call" {
		t.Fatalf("type = %v, want function_call", item["type"])
	}
	if item["call_id"] != "call_abc" {
		t.Fatalf("call_id = %v, want call_abc", item["call_id"])
	}
	if item["name"] != "bash" {
		t.Fatalf("name = %v, want bash", item["name"])
	}
	if item["arguments"] != `{"cmd":"ls"}` {
		t.Fatalf("arguments = %v, want {\"cmd\":\"ls\"}", item["arguments"])
	}
}

func TestConvertToResponsesInput_AssistantToolCalls_Untyped(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":   "call_xyz",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "read_file",
						"arguments": `{"path":"a.txt"}`,
					},
				},
			},
		},
	}
	got := ConvertToResponsesInput(msgs)
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
	item := got.Input[0].(map[string]interface{})
	if item["type"] != "function_call" {
		t.Fatalf("type = %v, want function_call", item["type"])
	}
	if item["call_id"] != "call_xyz" {
		t.Fatalf("call_id = %v, want call_xyz", item["call_id"])
	}
	if item["name"] != "read_file" {
		t.Fatalf("name = %v, want read_file", item["name"])
	}
}

func TestConvertToResponsesInput_AssistantContentAndToolCalls(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{
			"role":    "assistant",
			"content": "Let me check that.",
			"tool_calls": []ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "bash", Arguments: `{}`},
				},
			},
		},
	}
	got := ConvertToResponsesInput(msgs)
	// Should emit text message first, then function_call
	if len(got.Input) != 2 {
		t.Fatalf("len(Input) = %d, want 2", len(got.Input))
	}
	first := got.Input[0].(map[string]interface{})
	if first["type"] != "message" || first["role"] != "assistant" {
		t.Fatalf("first item should be assistant message, got %v", first)
	}
	second := got.Input[1].(map[string]interface{})
	if second["type"] != "function_call" {
		t.Fatalf("second item should be function_call, got %v", second)
	}
}

func TestConvertToResponsesInput_ToolResult(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{
			"role":         "tool",
			"tool_call_id": "call_abc",
			"content":      "file contents here",
		},
	}
	got := ConvertToResponsesInput(msgs)
	if len(got.Input) != 1 {
		t.Fatalf("len(Input) = %d, want 1", len(got.Input))
	}
	item := got.Input[0].(map[string]interface{})
	if item["type"] != "function_call_output" {
		t.Fatalf("type = %v, want function_call_output", item["type"])
	}
	if item["call_id"] != "call_abc" {
		t.Fatalf("call_id = %v, want call_abc", item["call_id"])
	}
	if item["output"] != "file contents here" {
		t.Fatalf("output = %v, want 'file contents here'", item["output"])
	}
}

func TestConvertToResponsesInput_MultiTurn(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{"role": "system", "content": "Be concise."},
		map[string]interface{}{"role": "user", "content": "List files"},
		map[string]interface{}{
			"role": "assistant", "content": "",
			"tool_calls": []ToolCall{{
				ID: "call_1", Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "bash", Arguments: `{"cmd":"ls"}`},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "a.txt\nb.txt"},
		map[string]interface{}{"role": "assistant", "content": "Found 2 files."},
		map[string]interface{}{"role": "user", "content": "Thanks"},
	}
	got := ConvertToResponsesInput(msgs)
	if got.Instructions != "Be concise." {
		t.Fatalf("Instructions = %q", got.Instructions)
	}
	// user + function_call + function_call_output + assistant_text + user = 5
	if len(got.Input) != 5 {
		t.Fatalf("len(Input) = %d, want 5", len(got.Input))
	}
}

func TestConvertToResponsesTools(t *testing.T) {
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "bash",
				"description": "Run a shell command",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"cmd": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	got := ConvertToResponsesTools(tools)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	flat := got[0]
	if flat["type"] != "function" {
		t.Fatalf("type = %v, want function", flat["type"])
	}
	if flat["name"] != "bash" {
		t.Fatalf("name = %v, want bash", flat["name"])
	}
	if flat["description"] != "Run a shell command" {
		t.Fatalf("description = %v", flat["description"])
	}
	// parameters should be preserved as-is
	params, _ := json.Marshal(flat["parameters"])
	if len(params) == 0 {
		t.Fatal("parameters should not be empty")
	}
}

func TestConvertToResponsesTools_NilFunction(t *testing.T) {
	tools := []map[string]interface{}{
		{"type": "function"}, // missing "function" key
	}
	got := ConvertToResponsesTools(tools)
	if len(got) != 0 {
		t.Fatalf("expected empty result for tool without function, got %d", len(got))
	}
}

func TestConvertToResponsesTools_Empty(t *testing.T) {
	got := ConvertToResponsesTools(nil)
	if got != nil {
		t.Fatalf("expected nil for empty tools, got %v", got)
	}
}
