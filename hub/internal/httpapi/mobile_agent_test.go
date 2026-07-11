package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

func TestMobileExecuteAgentToolWebSearch(t *testing.T) {
	previous := mobileWebSearch
	mobileWebSearch = func(context.Context, string, int) ([]websearch.SearchResult, error) {
		return []websearch.SearchResult{{
			Title:   "Example",
			URL:     "https://example.test",
			Snippet: "hello",
		}}, nil
	}
	t.Cleanup(func() { mobileWebSearch = previous })

	got := mobileExecuteAgentTool(context.Background(), "web_search", `{"query":"status","max_results":3}`)
	if !strings.Contains(got, "Example") || !strings.Contains(got, "https://example.test") {
		t.Fatalf("tool result = %q", got)
	}
}

func TestMobileExecuteAgentToolRejectsUnknown(t *testing.T) {
	got := mobileExecuteAgentTool(context.Background(), "bash", `{"cmd":"ls"}`)
	if !strings.Contains(got, "unsupported") {
		t.Fatalf("got %q", got)
	}
}

func TestMobileParseChatCompletionToolCalls(t *testing.T) {
	raw := []byte(`{
		"choices":[{
			"message":{
				"content":null,
				"tool_calls":[{
					"id":"call_1",
					"type":"function",
					"function":{"name":"web_search","arguments":"{\"query\":\"x\"}"}
				}]
			}
		}]
	}`)
	comp, err := mobileParseChatCompletion(raw, "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(comp.ToolCalls) != 1 || comp.ToolCalls[0].Name != "web_search" {
		t.Fatalf("comp = %#v", comp)
	}
	if comp.RequestID != "req-1" {
		t.Fatalf("request id = %q", comp.RequestID)
	}
}

func TestMobileRunAgentLoopUsesToolsThenAnswers(t *testing.T) {
	previous := mobileWebSearch
	mobileWebSearch = func(context.Context, string, int) ([]websearch.SearchResult, error) {
		return []websearch.SearchResult{{Title: "Src", URL: "https://example.test", Snippet: "ok"}}, nil
	}
	t.Cleanup(func() { mobileWebSearch = previous })

	calls := 0
	official := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("X-MaClaw-Request-ID", "agent-req")
		if calls == 1 {
			// First turn: request a tool.
			writeJSON(w, http.StatusOK, map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{
						"content": "",
						"tool_calls": []any{map[string]any{
							"id":   "call_search",
							"type": "function",
							"function": map[string]any{
								"name":      "web_search",
								"arguments": `{"query":"status"}`,
							},
						}},
					},
				}},
			})
			return
		}
		// Second turn: final answer after tool result is in messages.
		msgs, _ := body["messages"].([]any)
		hasToolRole := false
		for _, m := range msgs {
			mm, _ := m.(map[string]any)
			if mm["role"] == "tool" {
				hasToolRole = true
			}
		}
		if !hasToolRole {
			t.Fatalf("second call missing tool role messages: %#v", body["messages"])
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": "结论：来自工具。"},
			}},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", nil)
	events := []string{}
	// Force legacy path: core agentservice may not accept the test double LLM shape.
	// Call legacy directly for web-tool loop contract tests.
	answer, requestID, err := mobileRunLegacyAgentLoop(
		context.Background(),
		req,
		official,
		mobileLlmAuthorizationRecord{},
		false,
		[]map[string]string{
			{"role": "system", "content": "sys"},
			{"role": "user", "content": "status?"},
		},
		func(event string, data map[string]any) {
			events = append(events, event)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if requestID != "agent-req" {
		t.Fatalf("requestID=%q", requestID)
	}
	if !strings.Contains(answer, "结论") {
		t.Fatalf("answer=%q", answer)
	}
	joined := strings.Join(events, ",")
	if !strings.Contains(joined, "tool_call") || !strings.Contains(joined, "tool_result") {
		t.Fatalf("events=%v", events)
	}
	if calls < 2 {
		t.Fatalf("expected multi-round LLM calls, got %d", calls)
	}
}
