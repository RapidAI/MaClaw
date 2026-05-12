package memory

import (
	"strings"
	"testing"
)

func TestExtractJSONFromLLMResponse_CleanArray(t *testing.T) {
	var result []knowledgePoint
	input := `[{"content":"Go uses goroutines","category":"project_knowledge"}]`
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].Content != "Go uses goroutines" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_MarkdownFenced(t *testing.T) {
	var result []knowledgePoint
	input := "```json\n[{\"content\":\"test fact\",\"category\":\"instruction\"}]\n```"
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].Content != "test fact" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_MarkdownFencedNoLang(t *testing.T) {
	var result []knowledgePoint
	input := "```\n[{\"content\":\"no lang\",\"category\":\"project_knowledge\"}]\n```"
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].Content != "no lang" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_ProseWrapped(t *testing.T) {
	var result []knowledgePoint
	input := `Here are the extracted knowledge points:

[{"content":"SSH uses port 22","category":"project_knowledge"}]

I hope this helps!`
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].Content != "SSH uses port 22" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_HTMLError(t *testing.T) {
	var result []knowledgePoint
	input := `<html><head><title>502 Bad Gateway</title></head><body><center><h1>502 Bad Gateway</h1></center></body></html>`
	err := extractJSONFromLLMResponse(input, &result)
	if err == nil {
		t.Fatal("expected error for HTML response")
	}
	if !strings.Contains(err.Error(), "HTML instead of JSON") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestExtractJSONFromLLMResponse_HTMLDoctype(t *testing.T) {
	var result []knowledgePoint
	input := `<!DOCTYPE html><html><body><h1>504 Gateway Time-out</h1></body></html>`
	err := extractJSONFromLLMResponse(input, &result)
	if err == nil {
		t.Fatal("expected error for HTML response")
	}
	if !strings.Contains(err.Error(), "HTML instead of JSON") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestExtractJSONFromLLMResponse_Empty(t *testing.T) {
	var result []knowledgePoint
	err := extractJSONFromLLMResponse("", &result)
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty LLM response") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestExtractJSONFromLLMResponse_EmptyArray(t *testing.T) {
	var result []knowledgePoint
	if err := extractJSONFromLLMResponse("[]", &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty array, got: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_Object(t *testing.T) {
	type classifyResult struct {
		Operation string `json:"operation"`
		TargetID  string `json:"target_id"`
	}
	var result classifyResult
	input := `{"operation":"add","target_id":""}`
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Operation != "add" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_ObjectInProse(t *testing.T) {
	type classifyResult struct {
		Operation string `json:"operation"`
		Reason    string `json:"reason"`
	}
	var result classifyResult
	input := `Based on my analysis, here is the classification:
{"operation":"update","reason":"same topic, new details"}
This should merge the information.`
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Operation != "update" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_GarbageText(t *testing.T) {
	var result []knowledgePoint
	input := `I cannot extract any knowledge from this conversation because it was too short.`
	err := extractJSONFromLLMResponse(input, &result)
	if err == nil {
		t.Fatal("expected error for garbage text")
	}
	if !strings.Contains(err.Error(), "cannot parse LLM response as JSON") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestExtractJSONFromLLMResponse_NestedBrackets(t *testing.T) {
	var result []knowledgePoint
	input := `[{"content":"Use config[\"key\"] for access","category":"project_knowledge"}]`
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestLooksLikeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"<html><body>error</body></html>", true},
		{"<!DOCTYPE html><html>", true},
		{"<HTML><HEAD>", true},
		{"<head><title>Error</title></head>", true},
		{"[{\"content\":\"test\"}]", false},
		{"Here is the result: [{...}]", false},
		{"<think>reasoning</think>", false},                                  // single tag pair, not HTML doc
		{"<think>let me analyze this</think>\n[{\"content\":\"x\"}]", false}, // reasoning model prefix
	}
	for _, tt := range tests {
		got := looksLikeHTML(tt.input)
		if got != tt.want {
			t.Errorf("looksLikeHTML(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestExtractJSONFromLLMResponse_ThinkTagPrefix(t *testing.T) {
	// Reasoning models (deepseek-reasoner) wrap output in <think>...</think> tags.
	var result []knowledgePoint
	input := "<think>Let me analyze the conversation for knowledge points.</think>\n[{\"content\":\"Docker uses port 2375\",\"category\":\"project_knowledge\"}]"
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].Content != "Docker uses port 2375" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExtractJSONFromLLMResponse_BackticksInsideJSON(t *testing.T) {
	// JSON content contains triple backticks — the closing fence must be on its own line.
	var result []knowledgePoint
	input := "```json\n[{\"content\":\"use ```bash``` for shell commands\",\"category\":\"project_knowledge\"}]\n```"
	if err := extractJSONFromLLMResponse(input, &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].Content != "use ```bash``` for shell commands" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestStripMarkdownCodeFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no fence",
			input: `[{"x":1}]`,
			want:  `[{"x":1}]`,
		},
		{
			name:  "json fence",
			input: "```json\n[{\"x\":1}]\n```",
			want:  `[{"x":1}]`,
		},
		{
			name:  "fence no lang",
			input: "```\n{\"op\":\"add\"}\n```",
			want:  `{"op":"add"}`,
		},
		{
			name:  "backticks in content",
			input: "```json\n[{\"content\":\"use ```code``` here\"}]\n```",
			want:  "[{\"content\":\"use ```code``` here\"}]",
		},
		{
			name:  "no closing fence",
			input: "```json\n[{\"x\":1}]",
			want:  `[{"x":1}]`,
		},
		{
			name:  "fence with trailing newline",
			input: "```json\n{\"a\":\"b\"}\n```\n",
			want:  `{"a":"b"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdownCodeFence(tt.input)
			if got != tt.want {
				t.Errorf("stripMarkdownCodeFence():\n  got:  %q\n  want: %q", got, tt.want)
			}
		})
	}
}
