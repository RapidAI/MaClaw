package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestProbeStreamFinishReasonWithoutDone(t *testing.T) {
	const typhoon = "data: {\"choices\":[{\"delta\":{\"content\":\"hello\",\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"ping\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, typhoon)
	}))
	defer upstream.Close()
	provider := &llmpool.ProviderConfig{
		APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"typhoon"},
		Protocol: "openai",
	}
	got := probeProviderChat(context.Background(), upstream.Client(), provider, ProviderChatTest{Model: "typhoon", Stream: true})
	if got.Err != "" || got.Reply != "hello" || !got.Stream || got.StreamEnd == nil || !*got.StreamEnd || len(got.Calls) != 1 || got.Calls[0].Name != "ping" {
		t.Fatalf("clean finish=%#v", got)
	}

	cut := &http.Client{Transport: finishCutTransport{}}
	cutProvider := &llmpool.ProviderConfig{
		APIURL: "http://upstream.example/v1", APIKey: "sk-live", Models: []string{"typhoon"},
		Protocol: "openai",
	}
	got = probeProviderChat(context.Background(), cut, cutProvider, ProviderChatTest{Model: "typhoon", Stream: true})
	if got.Err != "connection reset" || got.Reply != "" || len(got.Calls) != 0 || !got.Stream || got.StreamEnd == nil || *got.StreamEnd {
		t.Fatalf("cut finish=%#v", got)
	}
}

// finishCutTransport returns a 200 body that already contains a legal
// finish_reason and then fails the read. That is a broken stream, not a
// normal end.
type finishCutTransport struct{}

func (finishCutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		ContentLength: -1,
		Body: &finishCutBody{rest: []byte(
			"data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n",
		)},
		Request: req,
	}, nil
}

type finishCutBody struct {
	rest []byte
}

func (b *finishCutBody) Read(p []byte) (int, error) {
	if len(b.rest) == 0 {
		return 0, errors.New("connection reset")
	}
	n := copy(p, b.rest)
	b.rest = b.rest[n:]
	return n, nil
}

func (b *finishCutBody) Close() error { return nil }

// TestProbeRejectsAbnormalCutReadErrorAndFakeSSE locks the three ways a
// buffered finish_reason must not become a successful stream. A chunked
// close is an unexpected EOF. A read that returns bytes and an error
// together is a read error. A body that only resembles SSE is not a stream.
func TestProbeRejectsAbnormalCutReadErrorAndFakeSSE(t *testing.T) {
	const framed = "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n"
	provider := &llmpool.ProviderConfig{
		APIURL: "http://upstream.example/v1", APIKey: "sk-live", Models: []string{"typhoon"},
		Protocol: "openai",
	}
	assertCut := func(t *testing.T, reads []scriptedRead) {
		t.Helper()
		wantErr := reads[len(reads)-1].err.Error()
		data, truncated, err := ReadProbeBody(&scriptedBody{reads: append([]scriptedRead(nil), reads...)})
		if err == nil || err.Error() != wantErr || data != nil || truncated {
			t.Fatalf("read=%q truncated=%v err=%v", data, truncated, err)
		}
		got := probeProviderChat(context.Background(), &http.Client{Transport: staticBodyTransport{reads: reads}}, provider, ProviderChatTest{Model: "typhoon", Stream: true})
		if got.Err != wantErr || got.Reply != "" || len(got.Calls) != 0 || !got.Stream || got.StreamEnd == nil || *got.StreamEnd {
			t.Fatalf("probe=%#v", got)
		}
	}
	frame := []byte(framed)
	t.Run("unexpected eof", func(t *testing.T) {
		assertCut(t, []scriptedRead{{data: frame}, {err: io.ErrUnexpectedEOF}})
	})
	t.Run("read error with bytes", func(t *testing.T) {
		assertCut(t, []scriptedRead{{data: frame, err: errors.New("read failed")}})
	})
	t.Run("partial line", func(t *testing.T) {
		assertCut(t, []scriptedRead{{
			data: []byte(`data: {"choices":[{"delta":{"content":"po"},"finish_reason":"sto`),
			err:  errors.New("connection reset"),
		}})
	})

	// A body that is not SSE stays on the missing-marker failure. Text is kept
	// only when a real data line carried it. The marker string inside JSON,
	// a comment, a lowercase [done], and an OpenAI event name are not ends.
	fakes := []struct {
		name  string
		body  string
		reply string
	}{
		{name: "json completion", body: `{"choices":[{"message":{"content":"pong"},"finish_reason":"stop"}]}`},
		{name: "ndjson", body: "{\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n"},
		{name: "pretty json", body: "{\n  \"choices\": [\n    {\"message\": {\"content\": \"pong\"}, \"finish_reason\": \"stop\"}\n  ]\n}\n"},
		{name: "marker text inside json", body: `{"choices":[{"message":{"content":"data: [DONE]"},"finish_reason":"stop"}]}`},
		{name: "comment marker", reply: "pong", body: ": data: [DONE]\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n"},
		{name: "lowercase done", reply: "pong", body: "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [done]\n\n"},
		{name: "openai event name", reply: "pong", body: "event: message_stop\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n"},
	}
	for _, tc := range fakes {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			fakeProvider := &llmpool.ProviderConfig{
				APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"typhoon"},
				Protocol: "openai",
			}
			got := probeProviderChat(context.Background(), upstream.Client(), fakeProvider, ProviderChatTest{Model: "typhoon", Stream: true})
			if got.Err != "stream ended before the end marker" || got.Reply != tc.reply || len(got.Calls) != 0 || !got.Stream || got.StreamEnd == nil || *got.StreamEnd {
				t.Fatalf("fake=%#v", got)
			}
		})
	}
	// A later data: [DONE] is a real end. The raw JSON line before it is not
	// a frame, so the reply stays empty and the probe is still a failure.
	t.Run("json line then done", func(t *testing.T) {
		const body = "{\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, body)
		}))
		defer upstream.Close()
		fakeProvider := &llmpool.ProviderConfig{
			APIURL: upstream.URL + "/v1", APIKey: "sk-live", Models: []string{"typhoon"},
			Protocol: "openai",
		}
		got := probeProviderChat(context.Background(), upstream.Client(), fakeProvider, ProviderChatTest{Model: "typhoon", Stream: true})
		if got.Err != "model returned no completion content" || got.Reply != "" || len(got.Calls) != 0 || !got.Stream || got.StreamEnd == nil || !*got.StreamEnd {
			t.Fatalf("marker after raw json=%#v", got)
		}
	})
}

type scriptedRead struct {
	data []byte
	err  error
}

type scriptedBody struct {
	reads []scriptedRead
}

func (b *scriptedBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(b.reads) > 0 {
		next := &b.reads[0]
		if len(next.data) == 0 {
			err := next.err
			b.reads = b.reads[1:]
			if err != nil {
				return 0, err
			}
			continue
		}
		n := copy(p, next.data)
		next.data = next.data[n:]
		if len(next.data) > 0 {
			return n, nil
		}
		err := next.err
		b.reads = b.reads[1:]
		return n, err
	}
	return 0, io.EOF
}

func (b *scriptedBody) Close() error { return nil }

type staticBodyTransport struct {
	reads []scriptedRead
}

func (s staticBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		ContentLength: -1,
		Body:          &scriptedBody{reads: append([]scriptedRead(nil), s.reads...)},
		Request:       req,
	}, nil
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
