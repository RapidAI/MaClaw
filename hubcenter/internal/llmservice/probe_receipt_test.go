package llmservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestNewMemberProbeRequestReceipt(t *testing.T) {
	huge := int64(100000)
	small := int64(2000)
	cases := []struct {
		name    string
		cfg     corelib.MaclawLLMConfig
		budget  MemberProbeBudget
		stream  bool
		key     string
		value   int64
		clipped bool
		absent  string
		urlPart string
	}{
		{
			name:    "openai huge",
			cfg:     corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Key: "sk-live", Model: "typhoon", Protocol: "openai", MemberOutputTokenCap: 16384},
			budget:  MemberProbeBudget{MaxTokens: &huge, OutputCap: 16384, Stream: true},
			stream:  true,
			key:     "max_tokens",
			value:   16384,
			clipped: true,
			absent:  "max_completion_tokens",
		},
		{
			name:   "anthropic keeps one field",
			cfg:    corelib.MaclawLLMConfig{URL: "https://api.anthropic.com", Key: "sk-live", Model: "claude-test", Protocol: "anthropic", MemberOutputTokenCap: 16384},
			budget: MemberProbeBudget{MaxTokens: &huge, MaxCompletionTokens: &small, OutputCap: 16384, Stream: true},
			stream: true,
			key:    "max_tokens",
			value:  16384, clipped: true,
			absent: "max_completion_tokens",
		},
		{
			name:   "responses prefers completion",
			cfg:    corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Key: "sk-live", Model: "gpt-4o", Protocol: "openai", WireAPI: "responses", MemberOutputTokenCap: 16384},
			budget: MemberProbeBudget{MaxTokens: &huge, MaxCompletionTokens: &small, OutputCap: 16384},
			key:    "max_output_tokens",
			value:  2000,
		},
		{
			name:    "google native field and stream endpoint",
			cfg:     corelib.MaclawLLMConfig{URL: "https://generativelanguage.googleapis.com/v1beta", Key: "sk-live", Model: "gemini-3.8-flash", Protocol: "google", MemberOutputTokenCap: 16384},
			budget:  MemberProbeBudget{MaxTokens: &huge, OutputCap: 16384, Stream: true},
			stream:  true,
			key:     "maxOutputTokens",
			value:   16384,
			clipped: true,
			urlPart: "streamGenerateContent",
		},
		{
			name:    "google hop ceiling without a member cap",
			cfg:     corelib.MaclawLLMConfig{URL: "https://generativelanguage.googleapis.com/v1beta", Key: "sk-live", Model: "gemini-3.8-flash", Protocol: "google"},
			budget:  MemberProbeBudget{MaxTokens: &huge},
			key:     "maxOutputTokens",
			value:   32768,
			clipped: true,
			urlPart: ":generateContent",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, wire, err := NewMemberProbeRequest(context.Background(), tc.cfg, nil, nil, tc.budget)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "sk-live") {
				t.Fatal("wire contains the key")
			}
			var doc map[string]any
			if err := json.Unmarshal(wire, &doc); err != nil {
				t.Fatal(err)
			}
			if tc.urlPart != "" && !strings.Contains(req.URL.String(), tc.urlPart) {
				t.Fatalf("url=%s", req.URL)
			}
			accept := req.Header.Get("Accept")
			if tc.budget.Stream {
				if accept != "text/event-stream" {
					t.Fatalf("accept=%q", accept)
				}
			} else if accept == "text/event-stream" {
				t.Fatalf("non-stream accept=%q", accept)
			}
			if tc.cfg.Protocol != "google" {
				stream, _ := doc["stream"].(bool)
				if stream != tc.stream {
					t.Fatalf("stream=%v want %v", doc["stream"], tc.stream)
				}
			}
			kind := ProbeOutputKind(tc.cfg)
			out := ProjectProbeOutput(wire, ProbeOutputBaseline(kind, tc.cfg.MemberExtraBody, tc.cfg.MemberExtraBodyPriority, tc.cfg.MemberDropFields, tc.budget.MaxTokens, tc.budget.MaxCompletionTokens, false))
			field := out[tc.key]
			if field.Value != tc.value || field.Clipped != tc.clipped {
				t.Fatalf("%s=%#v want %d clipped=%v body=%s", tc.key, field, tc.value, tc.clipped, wire)
			}
			if tc.absent != "" {
				if _, ok := out[tc.absent]; ok {
					t.Fatalf("receipt has %s: %#v", tc.absent, out)
				}
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "sk-live") || strings.Contains(string(raw), "messages") {
				t.Fatalf("receipt=%s", raw)
			}
		})
	}
}

func TestHopProbeOutputCollapsesSingleNumberProtocols(t *testing.T) {
	huge := int64(100000)
	small := int64(2000)
	chat := MemberProbeOutputBody(16384, &huge, &small, false)
	google := hopProbeOutput(&llmpool.ProviderConfig{Protocol: "google", MaxOutputTokensPerRequest: 16384}, chat, ProviderChatTest{MaxTokens: &huge, MaxCompletionTokens: &small}, false)
	if _, ok := google["maxOutputTokens"]; ok || google["max_tokens"].Value != 16384 || !google["max_tokens"].Clipped {
		t.Fatalf("google hop=%#v", google)
	}
	anthropic := hopProbeOutput(&llmpool.ProviderConfig{Protocol: "anthropic", MaxOutputTokensPerRequest: 16384}, chat, ProviderChatTest{MaxTokens: &huge, MaxCompletionTokens: &small}, false)
	if _, ok := anthropic["max_completion_tokens"]; ok || anthropic["max_tokens"].Value != 16384 || !anthropic["max_tokens"].Clipped {
		t.Fatalf("anthropic hop=%#v", anthropic)
	}
	responses := hopProbeOutput(&llmpool.ProviderConfig{Protocol: "openai", WireAPI: "responses", MaxOutputTokensPerRequest: 16384}, chat, ProviderChatTest{MaxTokens: &huge, MaxCompletionTokens: &small}, false)
	if _, ok := responses["max_tokens"]; ok || responses["max_output_tokens"].Value != 2000 || responses["max_output_tokens"].Clipped {
		t.Fatalf("responses hop=%#v", responses)
	}
}

func TestProbeProviderChatStreamReceipt(t *testing.T) {
	var saw map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&saw)
		if saw["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()
	huge := int64(100000)
	provider := &llmpool.ProviderConfig{
		APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"typhoon"},
		MaxOutputTokensPerRequest: 16384, Protocol: "openai",
	}
	streamed := probeProviderChat(context.Background(), upstream.Client(), provider, ProviderChatTest{Model: "typhoon", MaxTokens: &huge, Stream: true})
	if streamed.Err != "" || streamed.Reply != "pong" || !streamed.Stream || streamed.StreamEnd == nil || !*streamed.StreamEnd {
		t.Fatalf("stream=%#v", streamed)
	}
	if streamed.Output["max_tokens"].Value != 16384 || !streamed.Output["max_tokens"].Clipped || saw["stream"] != true {
		t.Fatalf("stream output=%#v saw=%#v", streamed.Output["max_tokens"], saw["stream"])
	}
	raw, _ := json.Marshal(streamed.Output)
	if strings.Contains(string(raw), "sk-live") || strings.Contains(string(raw), "messages") {
		t.Fatalf("output=%s", raw)
	}
	small := int64(128)
	quiet := probeProviderChat(context.Background(), upstream.Client(), provider, ProviderChatTest{Model: "typhoon", MaxTokens: &small})
	if quiet.Err != "" || quiet.Reply != "pong" || quiet.Stream || quiet.StreamEnd != nil || quiet.Output["max_tokens"].Value != 128 || quiet.Output["max_tokens"].Clipped {
		t.Fatalf("quiet=%#v", quiet)
	}
}

func TestDecodeHopMemberTestLeavesUnknownReceiptFieldsUnset(t *testing.T) {
	got := decodeHopMemberTest([]byte(`{"success":true,"reply":"pong","latency_ms":3}`), ProviderChatTestResult{Model: "typhoon"})
	if got.Err != "" || got.Reply != "pong" || got.Stream || got.StreamEnd != nil || got.Output != nil {
		t.Fatalf("old peer=%#v", got)
	}
	got = decodeHopMemberTest([]byte(`{"success":false,"error":"stream ended before the end marker","reply":"po","latency_ms":4,"stream":true,"stream_end":false,"output":{"max_tokens":{"value":32,"clipped":true}}}`), ProviderChatTestResult{})
	if got.Err != "stream ended before the end marker" || got.Reply != "po" || !got.Stream || got.StreamEnd == nil || *got.StreamEnd || got.Output["max_tokens"].Value != 32 || !got.Output["max_tokens"].Clipped {
		t.Fatalf("new peer=%#v", got)
	}
}
