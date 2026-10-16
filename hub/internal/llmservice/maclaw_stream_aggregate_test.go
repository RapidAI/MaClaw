package llmservice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOfficialStreamBodyForcesStreamWithoutTouchingQuotedShape(t *testing.T) {
	got := officialStreamBody([]byte(`{"model":"auto","stream":false}`))
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["stream"] != true {
		t.Fatalf("stream = %#v, want true", payload["stream"])
	}
	already := []byte(`{"model":"auto","stream":true}`)
	if string(officialStreamBody(already)) != string(already) {
		t.Fatal("an already-streaming body must stay byte-for-byte")
	}
	raw := []byte(`not-json`)
	if string(officialStreamBody(raw)) != string(raw) {
		t.Fatal("non-object body must stay unchanged")
	}
}

func TestAggregateOfficialEventStreamFoldsContentToolsAndReasoning(t *testing.T) {
	raw := strings.Join([]string{
		": ping",
		"",
		`data: {"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"想"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"一下","content":"重庆"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"晴","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"web_search","arguments":"{\"q\":"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"天气\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`,
		"data: [DONE]",
		"",
	}, "\n")
	body, status, err := aggregateOfficialEventStream([]byte(raw))
	if err != nil || status != 200 {
		t.Fatalf("status=%d err=%v body=%s", status, err, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out["object"] != "chat.completion" || out["model"] != "m" || out["id"] != "c1" {
		t.Fatalf("envelope = %#v", out)
	}
	choices, _ := out["choices"].([]any)
	choice, _ := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish = %#v", choice["finish_reason"])
	}
	message, _ := choice["message"].(map[string]any)
	if message["content"] != "重庆晴" || message["reasoning_content"] != "想一下" {
		t.Fatalf("message = %#v", message)
	}
	calls, _ := message["tool_calls"].([]any)
	call, _ := calls[0].(map[string]any)
	fn, _ := call["function"].(map[string]any)
	if call["id"] != "call-1" || fn["name"] != "web_search" || fn["arguments"] != `{"q":"天气"}` {
		t.Fatalf("tool call = %#v", call)
	}
	usage, _ := out["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(3) || usage["completion_tokens"] != float64(4) {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestAggregateOfficialEventStreamErrorWithoutOutput(t *testing.T) {
	raw := ": ping\n\ndata: {\"error\":{\"message\":\"upstream closed\",\"code\":504}}\n\ndata: [DONE]\n"
	body, status, err := aggregateOfficialEventStream([]byte(raw))
	if err != nil || status != 504 {
		t.Fatalf("status=%d err=%v body=%s", status, err, body)
	}
	if !strings.Contains(string(body), "upstream closed") {
		t.Fatalf("body = %s", body)
	}
}
