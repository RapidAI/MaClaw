package llmservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// Mistral's hosted models stream content as a block array. The probe folds
// the blocks, flags content_normalized, and still finishes on the marker or
// a legal finish_reason — a string-typed field used to fail the whole chunk.
func TestParseProbeStreamOpenAIArrayContent(t *testing.T) {
	t.Parallel()
	body := "" +
		`data: {"choices":[{"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"why "}]}]}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"content":[{"type":"text","text":"po"}]}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"content":[{"type":"refusal","text":"skip"},{"type":"text","text":"ng"}]},"finish_reason":"stop"}]}` + "\n\n"
	got := ParseProbeStream([]byte(body), "openai")
	if !got.End || got.Reply != "pong" || !got.ContentNormalized {
		t.Fatalf("array delta=%#v", got)
	}
	if got.Failure() != "" {
		t.Fatalf("failure=%q", got.Failure())
	}

	toolBody := "" +
		`data: {"choices":[{"delta":{"content":[],"tool_calls":[{"index":0,"function":{"name":"ping","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"
	got = ParseProbeStream([]byte(toolBody), "openai")
	if !got.End || got.Reply != "" || len(got.Calls) != 1 || got.Calls[0].Name != "ping" || !got.ContentNormalized {
		t.Fatalf("array delta with tools=%#v", got)
	}
	if got.Failure() != "" {
		t.Fatalf("tool failure=%q", got.Failure())
	}

	// A string delta reports no normalization.
	got = ParseProbeStream([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n"), "openai")
	if !got.End || got.Reply != "pong" || got.ContentNormalized {
		t.Fatalf("string delta=%#v", got)
	}

	// A thinking-only array answers from the reasoning lane.
	got = ParseProbeStream([]byte(`data: {"choices":[{"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"pong"}]}]},"finish_reason":"stop"}]}`+"\n\n"), "openai")
	if !got.End || got.Reply != "pong" || !got.ContentNormalized {
		t.Fatalf("thinking-only=%#v", got)
	}
}

func TestProviderTestContentNormalized(t *testing.T) {
	t.Parallel()
	if !ProviderTestContentNormalized([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":"pong"}]}}]}`)) {
		t.Fatal("array content not detected")
	}
	if ProviderTestContentNormalized([]byte(`{"choices":[{"message":{"content":"pong"}}]}`)) {
		t.Fatal("string content flagged")
	}
	// Anthropic's block array is its native shape, not a normalization.
	if ProviderTestContentNormalized([]byte(`{"content":[{"type":"text","text":"pong"}]}`)) {
		t.Fatal("anthropic content flagged")
	}
	if ProviderTestContentNormalized([]byte(`{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`)) {
		t.Fatal("google candidates flagged")
	}
}

// drop_fields must run after the gateway fills output budgets: a member that
// drops max_tokens still gets the 65536 stream fill and the token-bank cap
// fill upstream of it, and both must be gone on the wire.
func TestMemberDropFieldsRunsAfterGatewayBudgetFill(t *testing.T) {
	var mu sync.Mutex
	var saw []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream body: %v", err)
			return
		}
		mu.Lock()
		saw = append(saw, body)
		mu.Unlock()
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer srv.Close()
	provider := &llmpool.ProviderConfig{
		ID:                        "mistral",
		Name:                      "Mistral",
		APIURL:                    srv.URL + "/v1",
		APIKey:                    "sk-test",
		Protocol:                  "openai",
		DropFields:                []string{"max_tokens", "max_completion_tokens"},
		MaxOutputTokensPerRequest: 16384,
	}
	body := map[string]any{
		"model":    "mistral-large-latest",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}

	dst := newLockedResponseRecorder()
	streamed, err := streamProviderToWriter(context.Background(), srv.Client(), provider, body, "mistral-large-latest", "mistral-large-latest", dst)
	if err != nil || streamed == nil || streamed.statusCode != http.StatusOK {
		t.Fatalf("stream err=%v result=%#v body=%s", err, streamed, dst.BodyString())
	}
	resp, err := forwardToProvider(context.Background(), srv.Client(), provider, map[string]any{
		"model":    "mistral-large-latest",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}, "mistral-large-latest", "mistral-large-latest")
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("forward err=%v resp=%#v", err, resp)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(saw) != 2 {
		t.Fatalf("captured %d bodies, want 2", len(saw))
	}
	for i, got := range saw {
		if _, ok := got["max_tokens"]; ok {
			t.Fatalf("body %d kept max_tokens after drop_fields: %#v", i, got["max_tokens"])
		}
		if _, ok := got["max_completion_tokens"]; ok {
			t.Fatalf("body %d kept max_completion_tokens after drop_fields: %#v", i, got["max_completion_tokens"])
		}
	}
}

// The probe request builder has the same ordering guarantee: an injected
// default budget is removed when the member drops that field.
func TestMemberProbeRequestDropFieldsAfterDefaultBudget(t *testing.T) {
	t.Parallel()
	cfg := corelib.MaclawLLMConfig{
		URL:              "https://api.example/v1",
		Key:              "sk-test",
		Model:            "mistral-large-latest",
		MemberDropFields: []string{"max_tokens"},
	}
	req, wire, err := NewMemberProbeRequest(context.Background(), cfg, nil, nil, MemberProbeBudget{Stream: true})
	if err != nil {
		t.Fatalf("NewMemberProbeRequest: %v", err)
	}
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	var body map[string]any
	if err := json.Unmarshal(wire, &body); err != nil {
		t.Fatalf("wire body: %v", err)
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("probe kept max_tokens after drop_fields: %#v", body["max_tokens"])
	}
	if body["stream"] != true {
		t.Fatalf("probe lost stream flag: %#v", body)
	}
}
