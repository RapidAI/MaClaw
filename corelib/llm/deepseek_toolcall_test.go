package llm

// DeepSeek tool-call round-trip verification.
//
// This test calls the DeepSeek API with a simple tool definition, lets the
// model produce a tool_call, then sends the tool result back in a second
// request. The second request is where the HTTP 400 error occurs if the
// conversation history violates DeepSeek's strict message ordering rules:
//
//   "An assistant message with 'tool_calls' must be followed by tool
//    messages responding to each 'tool_call_id'."
//
// Run:  go test -v -run TestDeepSeek_ToolCallRoundTrip -timeout 120s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDeepSeek_ToolCallRoundTrip(t *testing.T) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set, skipping integration test")
	}
	cfg := corelib.MaclawLLMConfig{
		URL:   "https://api.deepseek.com/v1",
		Key:   apiKey,
		Model: "deepseek-reasoner",
	}

	client := &http.Client{Timeout: 120 * time.Second}
	ctx := context.Background()

	// ── Tool definition: a trivial "get_weather" function ──
	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "get_weather",
				"description": "Get the current weather for a city",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"city"},
				},
			},
		},
	}

	// ── Round 1: Ask a question that should trigger tool use ──
	messages := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": "You are a helpful assistant. When asked about weather, always use the get_weather tool. Reply in English.",
		},
		map[string]interface{}{
			"role":    "user",
			"content": "What's the weather in Beijing?",
		},
	}

	t.Log("=== Round 1: Sending initial request with tool definition ===")
	resp1, err := DoOpenAIRequest(ctx, cfg, messages, tools, client)
	if err != nil {
		t.Fatalf("Round 1 failed: %v", err)
	}

	if len(resp1.Choices) == 0 {
		t.Fatal("Round 1: no choices returned")
	}

	choice1 := resp1.Choices[0]
	t.Logf("Round 1 finish_reason: %s", choice1.FinishReason)
	t.Logf("Round 1 content: %q", choice1.Message.Content)
	t.Logf("Round 1 tool_calls count: %d", len(choice1.Message.ToolCalls))

	if len(choice1.Message.ToolCalls) == 0 {
		// Model chose not to use the tool — this is valid behavior for
		// deepseek-reasoner. Log and skip the round-trip test.
		t.Log("Model did not produce tool_calls (answered directly). Skipping round-trip test.")
		t.Log("This is expected for deepseek-reasoner which may not always use tools.")
		return
	}

	// ── Build Round 2 conversation: original + assistant(tool_calls) + tool results ──
	//
	// This is the critical part. DeepSeek requires:
	//   messages = [..., assistant{tool_calls}, tool{tool_call_id=X}, ...]
	// with NO gaps and NO extra messages between assistant and tool.

	// Build the assistant message exactly as the API returned it.
	assistantMsg := map[string]interface{}{
		"role":    "assistant",
		"content": choice1.Message.Content,
	}

	// Serialize tool_calls the way maclaw does it in the agent loop.
	tcJSON, _ := json.Marshal(choice1.Message.ToolCalls)
	var toolCallsRaw []interface{}
	json.Unmarshal(tcJSON, &toolCallsRaw)
	assistantMsg["tool_calls"] = toolCallsRaw

	messages2 := []interface{}{
		messages[0], // system
		messages[1], // user
		assistantMsg,
	}

	// Add a tool result for each tool_call.
	for _, tc := range choice1.Message.ToolCalls {
		t.Logf("  tool_call: id=%s name=%s args=%s", tc.ID, tc.Function.Name, tc.Function.Arguments)
		toolResultMsg := map[string]interface{}{
			"role":         "tool",
			"tool_call_id": tc.ID,
			"content":      `{"temperature": "22°C", "condition": "sunny", "city": "Beijing"}`,
		}
		messages2 = append(messages2, toolResultMsg)
	}

	t.Log("=== Round 2: Sending tool results back ===")
	t.Logf("Conversation has %d messages", len(messages2))
	for i, m := range messages2 {
		mm, _ := m.(map[string]interface{})
		role, _ := mm["role"].(string)
		hasTC := mm["tool_calls"] != nil
		tcID, _ := mm["tool_call_id"].(string)
		t.Logf("  [%d] role=%s has_tool_calls=%v tool_call_id=%s", i, role, hasTC, tcID)
	}

	resp2, err := DoOpenAIRequest(ctx, cfg, messages2, tools, client)
	if err != nil {
		t.Fatalf("Round 2 failed (THIS IS THE BUG): %v", err)
	}

	if len(resp2.Choices) == 0 {
		t.Fatal("Round 2: no choices returned")
	}

	choice2 := resp2.Choices[0]
	t.Logf("Round 2 finish_reason: %s", choice2.FinishReason)
	t.Logf("Round 2 content: %q", choice2.Message.Content)
	t.Log("=== Round-trip completed successfully ===")

	// ── Round 3 (regression): Simulate trimmed conversation where tool ──
	// ── messages are missing — this SHOULD fail with HTTP 400.         ──
	t.Log("=== Round 3: Intentionally broken conversation (missing tool results) ===")
	brokenMessages := []interface{}{
		messages[0], // system
		messages[1], // user
		assistantMsg, // assistant with tool_calls but NO following tool messages
		map[string]interface{}{
			"role":    "user",
			"content": "Thanks!",
		},
	}

	_, err3 := DoOpenAIRequest(ctx, cfg, brokenMessages, tools, client)
	if err3 != nil {
		t.Logf("Round 3 correctly failed: %v", err3)
		t.Log("This confirms DeepSeek rejects orphaned tool_calls messages.")
	} else {
		t.Log("Round 3 unexpectedly succeeded — DeepSeek may have relaxed validation.")
	}
}

// TestDeepSeek_ToolCallRoundTrip_StructuredToolCalls tests with the exact
// []llm.ToolCall type that maclaw uses internally (not JSON-roundtripped).
func TestDeepSeek_ToolCallRoundTrip_StructuredToolCalls(t *testing.T) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set, skipping integration test")
	}
	cfg := corelib.MaclawLLMConfig{
		URL:   "https://api.deepseek.com/v1",
		Key:   apiKey,
		Model: "deepseek-reasoner",
	}

	client := &http.Client{Timeout: 120 * time.Second}
	ctx := context.Background()

	tools := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "get_time",
				"description": "Get the current time in a timezone",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"timezone": map[string]interface{}{
							"type":        "string",
							"description": "Timezone name like Asia/Shanghai",
						},
					},
					"required": []string{"timezone"},
				},
			},
		},
	}

	messages := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": "You are a helpful assistant. When asked about time, always use the get_time tool.",
		},
		map[string]interface{}{
			"role":    "user",
			"content": "What time is it in Shanghai?",
		},
	}

	t.Log("=== Structured test: Round 1 ===")
	resp1, err := DoOpenAIRequest(ctx, cfg, messages, tools, client)
	if err != nil {
		t.Fatalf("Round 1 failed: %v", err)
	}
	if len(resp1.Choices) == 0 {
		t.Fatal("Round 1: no choices")
	}

	choice1 := resp1.Choices[0]
	t.Logf("Round 1: finish=%s content=%q tool_calls=%d",
		choice1.FinishReason, choice1.Message.Content, len(choice1.Message.ToolCalls))

	if len(choice1.Message.ToolCalls) == 0 {
		t.Log("Model answered directly without tools. Skipping.")
		return
	}

	// Build assistant message using the NATIVE []ToolCall type — this is
	// how maclaw's agent loop stores it in the conversation slice.
	assistantMsg := map[string]interface{}{
		"role":       "assistant",
		"content":    choice1.Message.Content,
		"tool_calls": choice1.Message.ToolCalls, // []ToolCall, not []interface{}
	}

	messages2 := []interface{}{
		messages[0],
		messages[1],
		assistantMsg,
	}

	for _, tc := range choice1.Message.ToolCalls {
		messages2 = append(messages2, map[string]interface{}{
			"role":         "tool",
			"tool_call_id": tc.ID,
			"content":      fmt.Sprintf(`{"time": "%s", "timezone": "Asia/Shanghai"}`, time.Now().Format("15:04:05")),
		})
	}

	t.Log("=== Structured test: Round 2 ===")
	resp2, err := DoOpenAIRequest(ctx, cfg, messages2, tools, client)
	if err != nil {
		t.Fatalf("Round 2 failed: %v", err)
	}
	if len(resp2.Choices) == 0 {
		t.Fatal("Round 2: no choices")
	}
	t.Logf("Round 2: finish=%s content=%q", resp2.Choices[0].FinishReason, resp2.Choices[0].Message.Content)
	t.Log("=== Structured round-trip OK ===")
}
