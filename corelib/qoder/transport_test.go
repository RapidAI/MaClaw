package qoder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestTransportStreamingContract drives an OpenAI-shaped request through
// WrapClient against a fake gate that serves the real SSE envelope shape,
// and asserts the caller sees plain OpenAI chunks back.
func TestTransportStreamingContract(t *testing.T) {
	var gotPath, gotAuth string
	var gotEncodedBody bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		gotEncodedBody = !strings.Contains(string(buf[:n]), `"role":"user"`)
		w.Header().Set("Content-Type", "text/event-stream")
		writeChatEvent(w, `{"choices":[{"delta":{"content":"","role":"assistant"},"index":0}]}`)
		writeChatEvent(w, `{"choices":[{"delta":{"content":"你好"},"index":0}]}`)
		writeChatEvent(w, `{"choices":[{"delta":{"reasoning_content":"思考"},"index":0}]}`)
		writeChatEvent(w, `{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"credits":0.01},"index":0}`)
		_, _ = w.Write([]byte("data:[DONE]\n\n"))
		_, _ = w.Write([]byte("data:{\"firstTokenDuration\":123,\"totalDuration\":456}\n\n"))
	}))
	defer server.Close()

	transport := &Transport{
		Base:        http.DefaultTransport,
		alwaysAdapt: true,
		credentialFor: func(bearer string) (string, string, bool) {
			return "01a11487-c7fa-7aab-b1b4-0ec70fcd5743", "", true
		},
	}
	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/model/v1/chat/completions",
		strings.NewReader(`{"model":"Qwen3.8-Max","stream":true,"messages":[{"role":"user","content":"你好"}]}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer dtetesttoken")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	rawBodyBytes, _ := io.ReadAll(resp.Body)
	rawBody := string(rawBodyBytes)
	t.Logf("RAW TRANSLATED STREAM: %s", rawBody)
	resp.Body.Close()
	resp.Body = io.NopCloser(strings.NewReader(rawBody))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(gotPath, "/algo/api/v2/service/pro/sse/agent_chat_generation") {
		t.Fatalf("path %q, want signed chat route", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer COSY.") {
		t.Fatalf("Authorization %q, want COSY bearer", gotAuth)
	}
	if !gotEncodedBody {
		t.Fatalf("outgoing body must be wasm-encoded, not plaintext JSON")
	}

	// The caller must observe standard OpenAI chunks with the merged deltas.
	scanner := bufio.NewScanner(resp.Body)
	var content strings.Builder
	var reasoning strings.Builder
	sawDone := false
	sawFinish := false
	var usage any
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			sawDone = true
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					Reasoning string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage any `json:"usage"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			t.Fatalf("non-OpenAI chunk leaked to the caller: %q", payload)
		}
		if len(chunk.Choices) > 0 {
			content.WriteString(chunk.Choices[0].Delta.Content)
			reasoning.WriteString(chunk.Choices[0].Delta.Reasoning)
			if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason != "" {
				sawFinish = true
			}
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if content.String() != "你好" {
		t.Fatalf("content = %q, want 你好", content.String())
	}
	if reasoning.String() != "思考" {
		t.Fatalf("reasoning = %q", reasoning.String())
	}
	if !sawFinish || !sawDone {
		t.Fatalf("finish=%v done=%v", sawFinish, sawDone)
	}
	if usage == nil {
		t.Fatalf("usage lost")
	}
	_ = io.Discard
}

func TestCanonicalChatBodySignsVerifiedBytes(t *testing.T) {
	verified := `{"model":"qfmodel","stream":true,"tools":[{"type":"function","function":{"name":"read_file"}}],"request_id":"keep-me"}`
	req, err := http.NewRequest(http.MethodPost, ChatBase+"/chat/completions", strings.NewReader(verified))
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(context.WithValue(req.Context(), qoderWireMetaKey{}, qoderWireMeta{
		adapted:        true,
		originalStream: false,
	}))
	body, stream := (&Transport{}).canonicalChatBody(req, []byte(verified))
	if body != verified {
		t.Fatalf("encoder rewrote verified JSON: %s", body)
	}
	if stream {
		t.Fatal("originalStream was lost")
	}
}

func TestCanonicalChatBodyAdaptsWhenGateHasNotVerified(t *testing.T) {
	raw := `{"model":"Qwen3.8-Flash","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	body, stream := (&Transport{}).canonicalChatBody(nil, []byte(raw))
	if stream {
		t.Fatal("stream flag")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatal(err)
	}
	if obj["model"] != "qfmodel" {
		t.Fatalf("model = %#v", obj["model"])
	}
	if _, ok := obj["request_id"]; !ok {
		t.Fatal("unverified body was not adapted")
	}
}

type recordingGate struct {
	next       http.RoundTripper
	body       []byte
	sawGetBody bool
}

func (g *recordingGate) RoundTrip(req *http.Request) (*http.Response, error) {
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	g.body = append([]byte(nil), payload...)
	g.sawGetBody = req.GetBody != nil
	req.Body = io.NopCloser(bytes.NewReader(g.body))
	req.ContentLength = int64(len(g.body))
	req.GetBody = nil
	return g.next.RoundTrip(req)
}

func (g *recordingGate) ToolSurfaceNext() http.RoundTripper { return g.next }

func (g *recordingGate) SetToolSurfaceNext(next http.RoundTripper) { g.next = next }

var _ corelib.ToolSurfaceWireGate = (*recordingGate)(nil)

func TestPrepareAgentChatBodyDoesNotHTMLEscape(t *testing.T) {
	body, stream := prepareAgentChatBody([]byte(`{"model":"qfmodel","stream":false,"messages":[{"role":"user","content":"a & b <c>"}]}`))
	if stream {
		t.Fatal("stream flag")
	}
	if !strings.Contains(body, "a & b <c>") {
		t.Fatalf("body = %s", body)
	}
	if strings.Contains(body, `\u0026`) || strings.Contains(body, `\u003c`) {
		t.Fatalf("HTML-escaped body: %s", body)
	}
}

func TestRewrapUpdatesPinnedEditionWithoutStacking(t *testing.T) {
	network := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
	})
	gate := &recordingGate{next: network}
	_ = WrapClientWithEdition(&http.Client{Transport: gate}, "", "")
	first, _ := gate.ToolSurfaceNext().(*Transport)
	if first == nil || first.uid != "" || first.edition != "" {
		t.Fatalf("first encoder = %+v", first)
	}
	_ = WrapClientWithEdition(&http.Client{Transport: gate}, "uid-2", StoreCN)
	second, _ := gate.ToolSurfaceNext().(*Transport)
	if second == nil || second == first {
		t.Fatal("edition wrap did not replace the unpinned encoder")
	}
	if second.uid != "uid-2" || second.edition != StoreCN {
		t.Fatalf("uid=%q edition=%q", second.uid, second.edition)
	}
	if _, ok := second.Base.(roundTripFunc); !ok {
		t.Fatalf("replaced encoder lost its network hop: %T", second.Base)
	}
	_ = WrapClientWithEdition(&http.Client{Transport: gate}, "", "")
	kept, _ := gate.ToolSurfaceNext().(*Transport)
	if kept != second || kept.uid != "uid-2" || kept.edition != StoreCN {
		t.Fatal("empty rewrap wiped the pinned identity")
	}
}

func TestAdaptedNonStreamAggregatesAcrossGate(t *testing.T) {
	var networkBody []byte
	network := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		networkBody, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
		var buf bytes.Buffer
		inner, _ := json.Marshal(`{"choices":[{"delta":{"content":"ok"},"index":0,"finish_reason":"stop"}]}`)
		buf.WriteString("data:{\"headers\":{},\"body\":")
		buf.Write(inner)
		buf.WriteString("}\n\ndata:[DONE]\n\n")
		header := make(http.Header)
		header.Set("Content-Type", "text/event-stream")
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(buf.Bytes()))}, nil
	})
	gate := &recordingGate{next: network}
	client := WrapClientWithEdition(&http.Client{Transport: gate}, "01a11487-c7fa-7aab-b1b4-0ec70fcd5743", StoreCN)
	payload := `{"model":"qfmodel","stream":false,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read_file","description":"Read & write","parameters":{"type":"object"}}}]}`
	req, err := http.NewRequest(http.MethodPost, ChatBase+"/chat/completions", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer dtetesttoken")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	callerBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if gate.sawGetBody {
		t.Fatal("adapted request stayed rewindable across the gate")
	}
	if !json.Valid(gate.body) || !strings.Contains(string(gate.body), `"read_file"`) || !strings.Contains(string(gate.body), "Read & write") {
		t.Fatalf("gate body = %s", gate.body)
	}
	var seen map[string]any
	if err := json.Unmarshal(gate.body, &seen); err != nil {
		t.Fatal(err)
	}
	if seen["stream"] != true {
		t.Fatalf("upstream stream = %#v", seen["stream"])
	}
	if _, ok := seen["request_id"]; !ok {
		t.Fatal("gate did not see the adapted agent_chat body")
	}
	if json.Valid(networkBody) {
		t.Fatalf("network saw JSON: %s", networkBody)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("non-stream caller received SSE: %s", callerBody)
	}
	if !json.Valid(callerBody) || !strings.Contains(string(callerBody), `"ok"`) {
		t.Fatalf("aggregated body = %s", callerBody)
	}
}

// TestGlobalEditionSignsAgainstAlgoGateway records the URL the global
// edition actually signs. The shared OpenAI front (api2-v2) does not serve
// /algo and answers that path with HTTP 404; the signed chat host is api3.
func TestGlobalEditionSignsAgainstAlgoGateway(t *testing.T) {
	var gotURL string
	transport := &Transport{
		Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "text/event-stream")
			writeChatEvent(rec, `{"choices":[{"delta":{"content":"ok"},"index":0}]}`)
			writeChatEvent(rec, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
			_, _ = rec.WriteString("data:[DONE]\n\n")
			return rec.Result(), nil
		}),
		uid:     "01a11487-c7fa-7aab-b1b4-0ec70fcd5743",
		edition: StoreGlobal,
	}
	req, err := http.NewRequest(http.MethodPost, ChatBase+"/chat/completions",
		strings.NewReader(`{"model":"qfmodel","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer dtetesttoken")
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	const want = "https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation"
	if !strings.HasPrefix(gotURL, want) {
		t.Fatalf("signed url %s, want %s", gotURL, want)
	}
	if strings.Contains(gotURL, "api2-v2.qoder.sh") {
		t.Fatalf("signed chat stayed on the OpenAI front: %s", gotURL)
	}
}

func TestWrapNestsEncoderUnderToolSurfaceGate(t *testing.T) {
	network := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: req}, nil
	})
	gate := &recordingGate{next: network}
	client := &http.Client{Transport: gate}
	wrapped := WrapClientWithEdition(client, "uid", StoreCN)
	if _, ok := wrapped.Transport.(*adaptTransport); !ok {
		t.Fatalf("outer transport = %T, want JSON adapt above the gate", wrapped.Transport)
	}
	encoder, ok := gate.ToolSurfaceNext().(*Transport)
	if !ok {
		t.Fatalf("gate next = %T, want COSY encoder", gate.ToolSurfaceNext())
	}
	if _, ok := encoder.Base.(roundTripFunc); !ok {
		t.Fatalf("encoder base = %T, want the previous hop", encoder.Base)
	}
	again := WrapClientWithEdition(wrapped, "other", StoreGlobal)
	if again.Transport != wrapped.Transport {
		t.Fatal("wrapping an adapted client replaced the outer rewrite")
	}
	if gate.ToolSurfaceNext() != encoder {
		t.Fatal("second wrap stacked another encoder under the gate")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestEnvelopeStreamPublishesOneCompletionID(t *testing.T) {
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	const upstreamID = "e490d351-4f4e-4afd-8ae4-f5319cd34700"
	bodies := []string{
		`{"id":"chatcmpl-2d3d1de2-1d4c-9ae0-a57a-9dc541f14472","choices":[{"index":0,"delta":{"content":"Reading the evidence now. "}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"build/ has artifacts. a < b","tool_calls":[{"index":0,"id":"` + upstreamID + `","type":"function","function":{"name":"bash","arguments":"{\"id\": \"` + upstreamID + `\"}"}}]},"finish_reason":"tool_calls"}],"id":"` + upstreamID + `","usage":{"prompt_tokens":9007199254740993}}`,
		`{"choices":[{"index":0,"delta":{}}]}`,
		`{"id":"` + upstreamID + `","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
	}
	for i, body := range bodies {
		payload, err := json.Marshal(map[string]any{"headers": map[string]any{}, "body": body})
		if err != nil {
			t.Fatal(err)
		}
		if !writer.line("data:" + string(payload)) {
			t.Fatal("envelope line stopped the completion")
		}
		if i == 0 && !strings.Contains(buf.String(), "data: "+body+"\n\n") {
			t.Fatalf("first chunk was re-encoded:\n%s", buf.String())
		}
	}

	raw := buf.String()
	var ids []string
	var contents []string
	var sawTool bool
	var promptTokens float64
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("translated chunk: %v\n%s", err, payload)
		}
		if id, _ := chunk["id"].(string); id != "" {
			ids = append(ids, id)
		}
		if strings.Contains(payload, `\u003c`) {
			t.Fatalf("rewritten chunk escaped text: %s", payload)
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			if usage, _ := chunk["usage"].(map[string]any); usage != nil {
				promptTokens, _ = usage["prompt_tokens"].(float64)
			}
			continue
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if content, _ := delta["content"].(string); content != "" {
			contents = append(contents, content)
		}
		if calls, _ := delta["tool_calls"].([]any); len(calls) > 0 {
			sawTool = true
			call, _ := calls[0].(map[string]any)
			fn, _ := call["function"].(map[string]any)
			if call["id"] != upstreamID {
				t.Fatalf("tool call id = %#v, want the provider call id", call["id"])
			}
			if fn["name"] != "bash" || fn["arguments"] != `{"id": "`+upstreamID+`"}` {
				t.Fatalf("tool call = %#v", fn)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id != "chatcmpl-2d3d1de2-1d4c-9ae0-a57a-9dc541f14472" {
			t.Fatalf("published ids = %#v, want one completion id", ids)
		}
	}
	if len(ids) != 3 {
		t.Fatalf("published ids = %#v, want the first id on every id-bearing chunk", ids)
	}
	if strings.Join(contents, "") != "Reading the evidence now. build/ has artifacts. a < b" {
		t.Fatalf("content = %#v", contents)
	}
	if !sawTool {
		t.Fatal("tool call was dropped while stabilizing the completion id")
	}
	if promptTokens != 3 {
		t.Fatalf("prompt_tokens = %v, want 3", promptTokens)
	}
	if !strings.Contains(raw, `"id":"`+upstreamID+`"`) {
		t.Fatalf("nested tool-call id was rewritten with the completion id:\n%s", raw)
	}
	if !strings.Contains(raw, `9007199254740993`) {
		t.Fatal("usage integer was re-encoded")
	}
	if strings.Contains(raw, `"id":"`+upstreamID+`","choices"`) || strings.Contains(raw, `"id":"`+upstreamID+`","usage"`) {
		t.Fatalf("top-level upstream id leaked:\n%s", raw)
	}
}

func TestEnvelopeErrorCodeIsNotForwardedAsCompletion(t *testing.T) {
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	body := `{"id":"chatcmpl-x","code":"provider_error","message":"nope","choices":[{"delta":{"content":"hidden"}}]}`
	payload, err := json.Marshal(map[string]any{"body": body})
	if err != nil {
		t.Fatal(err)
	}
	if writer.line("data:" + string(payload)) {
		t.Fatal("error chunk continued the completion")
	}
	raw := buf.String()
	if strings.Contains(raw, "hidden") || strings.Contains(raw, "chatcmpl-x") {
		t.Fatalf("error chunk was forwarded:\n%s", raw)
	}
	if !strings.Contains(raw, "nope") {
		t.Fatalf("error message missing:\n%s", raw)
	}

	// code after choices still matches the leading-id shape. It must not be
	// published as a completion delta.
	var after bytes.Buffer
	later := &envelopeWriter{w: &after}
	laterBody := `{"id":"chatcmpl-x","choices":[{"delta":{"content":"hidden"}}],"code":"provider_error","message":"later"}`
	laterPayload, err := json.Marshal(map[string]any{"body": laterBody})
	if err != nil {
		t.Fatal(err)
	}
	if later.line("data:" + string(laterPayload)) {
		t.Fatal("error after choices continued the completion")
	}
	if strings.Contains(after.String(), "hidden") {
		t.Fatalf("error after choices was forwarded:\n%s", after.String())
	}

	var shaped bytes.Buffer
	shapedWriter := &envelopeWriter{w: &shaped}
	shapedBody := `{"id":"chatcmpl-x","object":"chat.completion.chunk","model":"qfmodel","choices":[{"delta":{"content":"hidden"}}],"code":"provider_error","message":"later"}`
	shapedPayload, err := json.Marshal(map[string]any{"body": shapedBody})
	if err != nil {
		t.Fatal(err)
	}
	if shapedWriter.line("data:" + string(shapedPayload)) {
		t.Fatal("error on an openai-shaped chunk continued the completion")
	}
	if strings.Contains(shaped.String(), "hidden") {
		t.Fatalf("openai-shaped error was forwarded:\n%s", shaped.String())
	}
}

func TestEnvelopeForwardsNonStringCompletionID(t *testing.T) {
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	bodies := []string{
		`{"id":null,"choices":[{"delta":{"content":"first "}}]}`,
		`{"id":"chatcmpl-stable","choices":[{"delta":{"content":"keep "}}]}`,
		`{"id":null,"choices":[{"delta":{"content":"a < b"}}],"usage":{"prompt_tokens":9007199254740993}}`,
		`{"id":17,"choices":[{"delta":{"content":" and more"}}]}`,
	}
	for _, body := range bodies {
		payload, err := json.Marshal(map[string]any{"body": body})
		if err != nil {
			t.Fatal(err)
		}
		if !writer.line("data:" + string(payload)) {
			t.Fatal("envelope line stopped the completion")
		}
	}
	raw := buf.String()
	if !strings.Contains(raw, "first ") || !strings.Contains(raw, "keep ") || !strings.Contains(raw, "a < b") || !strings.Contains(raw, " and more") {
		t.Fatalf("non-string id dropped a delta:\n%s", raw)
	}
	if strings.Contains(raw, `\u003c`) || !strings.Contains(raw, "9007199254740993") {
		t.Fatalf("non-string id chunk was re-encoded:\n%s", raw)
	}
	if strings.Contains(raw, `"id":null`) || strings.Contains(raw, `"id":17`) {
		t.Fatalf("non-string id still published:\n%s", raw)
	}
	if strings.Count(raw, "chatcmpl-stable") != 3 {
		t.Fatalf("string completion id count:\n%s", raw)
	}
}

func TestStableCompletionBodyReplacesEveryTopLevelID(t *testing.T) {
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	bodies := []string{
		`{"id":"chatcmpl-stable","choices":[{"delta":{"content":"a"}}]}`,
		`{"id":"upstream-a","choices":[{"delta":{"content":"b"}}],"id":"upstream-b"}`,
	}
	for _, body := range bodies {
		payload, err := json.Marshal(map[string]any{"body": body})
		if err != nil {
			t.Fatal(err)
		}
		if !writer.line("data:" + string(payload)) {
			t.Fatal("envelope line stopped the completion")
		}
	}
	raw := buf.String()
	if strings.Contains(raw, "upstream-a") || strings.Contains(raw, "upstream-b") {
		t.Fatalf("a second top-level id survived:\n%s", raw)
	}
	if strings.Count(raw, "chatcmpl-stable") != 3 {
		t.Fatalf("published ids:\n%s", raw)
	}
}

func TestReplaceTopLevelJSONStringLeavesNestedFields(t *testing.T) {
	const upstream = "e490d351-4f4e-4afd-8ae4-f5319cd34700"
	body := `{"choices":[{"delta":{"tool_calls":[{"id":"` + upstream + `","function":{"arguments":"{\"id\": \"` + upstream + `\"}"}}]}}], "id" : "` + upstream + `", "id":"` + upstream + `"}`
	got, ok := replaceTopLevelJSONString(body, "id", "chatcmpl-stable")
	if !ok {
		t.Fatal("top-level id was not found")
	}
	if strings.Count(got, "chatcmpl-stable") != 2 {
		t.Fatalf("rewritten = %s", got)
	}
	if !strings.Contains(got, `"id" : "chatcmpl-stable"`) || !strings.Contains(got, `"id":"chatcmpl-stable"`) {
		t.Fatalf("whitespace around the id field changed: %s", got)
	}
	if !strings.Contains(got, `"id":"`+upstream+`"`) {
		t.Fatalf("nested id removed: %s", got)
	}
	if strings.Count(got, upstream) != 2 {
		t.Fatalf("nested id or argument text changed: %s", got)
	}
}

func TestEnvelopeIngestSplitsPartialLines(t *testing.T) {
	events := []string{
		`{"id":"chatcmpl-first","choices":[{"delta":{"content":"one"}}]}`,
		`{"id":"upstream-second","choices":[{"delta":{"content":"two"}}]}`,
	}
	var whole strings.Builder
	for _, body := range events {
		payload, err := json.Marshal(map[string]string{"body": body})
		if err != nil {
			t.Fatal(err)
		}
		whole.WriteString("data:")
		whole.Write(payload)
		whole.WriteString("\n")
	}
	raw := whole.String()

	var got bytes.Buffer
	writer := &envelopeWriter{w: &got}
	for i := 0; i < len(raw); i++ {
		if !writer.ingest(raw[i : i+1]) {
			t.Fatal("partial line stopped the stream")
		}
	}
	var want bytes.Buffer
	once := &envelopeWriter{w: &want}
	if !once.ingest(raw) {
		t.Fatal("whole buffer stopped the stream")
	}
	if got.String() != want.String() {
		t.Fatalf("split ingest =\n%s\nwant\n%s", got.String(), want.String())
	}
	if strings.Count(got.String(), `"id":"chatcmpl-first"`) != 2 {
		t.Fatalf("split stream did not keep one completion id:\n%s", got.String())
	}
}

func TestAggregateEnvelopeKeepsToolCallWhenCompletionIDChanges(t *testing.T) {
	const callID = "call_write_arch"
	const upstream = "f3199d9b-9a16-4c9d-b4d4-41f2c76a31d9"
	events := []string{
		`{"id":"chatcmpl-0e29264b-a54e-9a77-a013-2eeafa3c8df9","choices":[{"index":0,"delta":{"content":"空沙箱，我直接产出完整架构设计。先落盘文档再展示。","reasoning_content":"先写文件。"}}]}`,
		`{"id":"` + upstream + `","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_send","type":"function","function":{"name":"send_file","arguments":"{\"path\":\"arch.md\""}}]}}]}`,
		`{"id":"` + upstream + `","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"` + callID + `","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"arch.md\",\"content\":\"a < b"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"` + upstream + `","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"}"}}]}}]}`,
		`{"id":"` + upstream + `","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"}"}}]}}]}`,
		`{"id":"` + upstream + `","choices":[],"usage":{"prompt_tokens":9007199254740993,"completion_tokens":4}}`,
	}
	var raw strings.Builder
	for _, body := range events {
		payload, err := json.Marshal(map[string]string{"body": body})
		if err != nil {
			t.Fatal(err)
		}
		raw.WriteString("data:")
		raw.Write(payload)
		raw.WriteString("\n\n")
	}
	got, err := aggregateEnvelopeStream(strings.NewReader(raw.String()), "qfmodel")
	if err != nil {
		t.Fatal(err)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(got, &completion); err != nil {
		t.Fatalf("aggregated completion: %v\n%s", err, got)
	}
	if len(completion.Choices) != 1 {
		t.Fatalf("choices = %d", len(completion.Choices))
	}
	msg := completion.Choices[0].Message
	if msg.Content != "空沙箱，我直接产出完整架构设计。先落盘文档再展示。" {
		t.Fatalf("content = %q", msg.Content)
	}
	if msg.Reasoning != "先写文件。" {
		t.Fatalf("reasoning = %q", msg.Reasoning)
	}
	if completion.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q", completion.Choices[0].FinishReason)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("tool_calls = %#v", msg.ToolCalls)
	}
	writeCall := msg.ToolCalls[0]
	if writeCall.ID != callID || writeCall.Type != "function" || writeCall.Function.Name != "write_file" {
		t.Fatalf("tool call 0 = %#v", writeCall)
	}
	if writeCall.Function.Arguments != `{"path":"arch.md","content":"a < b"}` {
		t.Fatalf("write arguments = %q", writeCall.Function.Arguments)
	}
	sendCall := msg.ToolCalls[1]
	if sendCall.ID != "call_send" || sendCall.Function.Name != "send_file" || sendCall.Function.Arguments != `{"path":"arch.md"}` {
		t.Fatalf("tool call 1 = %#v", sendCall)
	}
	if strings.Contains(string(got), `\u003c`) {
		t.Fatalf("aggregated tool arguments were HTML-escaped:\n%s", got)
	}
	if !strings.Contains(string(got), "9007199254740993") {
		t.Fatalf("usage integer was rounded:\n%s", got)
	}
}

func TestAggregateEnvelopeReportsUpstreamError(t *testing.T) {
	body := `{"code":"provider_error","message":"nope"}`
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		t.Fatal(err)
	}
	_, err = aggregateEnvelopeStream(strings.NewReader("data:"+string(payload)+"\n\n"), "qfmodel")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v", err)
	}
}

func TestAggregateEnvelopeKeepsPartialWhenUpstreamErrors(t *testing.T) {
	events := []string{
		`{"id":"chatcmpl-stable","choices":[{"delta":{"content":"partial"}}]}`,
		`{"code":"provider_error","message":"nope"}`,
	}
	var raw strings.Builder
	for _, body := range events {
		payload, err := json.Marshal(map[string]string{"body": body})
		if err != nil {
			t.Fatal(err)
		}
		raw.WriteString("data:")
		raw.Write(payload)
		raw.WriteString("\n\n")
	}
	got, err := aggregateEnvelopeStream(strings.NewReader(raw.String()), "qfmodel")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "partial") {
		t.Fatalf("partial output dropped:\n%s", got)
	}
}

func TestAggregateEnvelopeStopsAfterUpstreamError(t *testing.T) {
	events := []string{
		`{"id":"chatcmpl-stable","choices":[{"delta":{"content":"partial"}}]}`,
		`{"code":"provider_error","message":"nope"}`,
		`{"id":"chatcmpl-stable","choices":[{"delta":{"content":"hidden"}}]}`,
	}
	var raw strings.Builder
	for _, body := range events {
		payload, err := json.Marshal(map[string]string{"body": body})
		if err != nil {
			t.Fatal(err)
		}
		raw.WriteString("data:")
		raw.Write(payload)
		raw.WriteString("\n\n")
	}
	got, err := aggregateEnvelopeStream(strings.NewReader(raw.String()), "qfmodel")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "partial") || strings.Contains(string(got), "hidden") {
		t.Fatalf("tokens after the error were folded in:\n%s", got)
	}
}

func TestPlainLeadingCompletionIDShape(t *testing.T) {
	id, ok := plainLeadingCompletionID(`{"id":"chatcmpl-x","choices":[{"delta":{"content":"identity"}}]}`)
	if !ok || id != "chatcmpl-x" {
		t.Fatalf("plain id = %q ok=%v", id, ok)
	}
	if _, ok := plainLeadingCompletionID(`{"id":"chatcmpl-x","choices":[{"delta":{"content":"id"}}]}`); ok {
		t.Fatal("a string value of id must use the structural path")
	}
	if _, ok := plainLeadingCompletionID(`{"id":"chatcmpl-x"},`); ok {
		t.Fatal("trailing junk matched the fast path")
	}
	if _, ok := plainLeadingCompletionID(`{"id":"chatcmpl-x","choices":[],"ID":"other"}`); ok {
		t.Fatal("case-insensitive id must use the structural path")
	}
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	for _, folded := range []string{
		`{"id":"chatcmpl-stable","choices":[{"delta":{"content":"a"}}]}`,
		`{"id":"chatcmpl-stable","choices":[{"delta":{"content":"b"}}],"ID":"upstream-b"}`,
	} {
		payload, err := json.Marshal(map[string]string{"body": folded})
		if err != nil {
			t.Fatal(err)
		}
		if !writer.line("data:" + string(payload)) {
			t.Fatal("folded id stopped the stream")
		}
	}
	if strings.Contains(buf.String(), "upstream-b") {
		t.Fatalf("case-insensitive id leaked:\n%s", buf.String())
	}
	if id, ok := plainLeadingCompletionID("{\"id\":\"chatcmpl-x\"} \n"); !ok || id != "chatcmpl-x" {
		t.Fatalf("closing whitespace id = %q ok=%v", id, ok)
	}
	full := `{"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"qfmodel","choices":[{"delta":{"content":"hi"}}]}`
	if id, ok := plainLeadingCompletionID(full); !ok || id != "chatcmpl-x" {
		t.Fatalf("openai chunk id = %q ok=%v", id, ok)
	}
}

func TestPlainLeadingIDRewritesOpenAIChunk(t *testing.T) {
	first := `{"id":"chatcmpl-stable","object":"chat.completion.chunk","model":"qfmodel","choices":[{"delta":{"content":"a"}}]}`
	second := `{"id":"upstream-b","object":"chat.completion.chunk","created":1,"model":"qfmodel","choices":[{"delta":{"content":"a < b"}}],"usage":{"prompt_tokens":9007199254740993}}`
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	for _, body := range []string{first, second} {
		payload, err := json.Marshal(map[string]string{"body": body})
		if err != nil {
			t.Fatal(err)
		}
		if !writer.line("data:" + string(payload)) {
			t.Fatal("openai chunk stopped the stream")
		}
	}
	raw := buf.String()
	if strings.Contains(raw, "upstream-b") || strings.Contains(raw, `\u003c`) || !strings.Contains(raw, "9007199254740993") {
		t.Fatalf("openai chunk rewrite:\n%s", raw)
	}
	if strings.Count(raw, `"id":"chatcmpl-stable"`) != 2 || !strings.Contains(raw, `"object":"chat.completion.chunk"`) || !strings.Contains(raw, `"content":"a < b"`) {
		t.Fatalf("openai fields changed:\n%s", raw)
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("rewritten chunk is not JSON: %v\n%s", err, payload)
		}
		if chunk.ID != "chatcmpl-stable" {
			t.Fatalf("published id = %q", chunk.ID)
		}
	}
}

func TestCompletionIDTextKeepsPublishedBytes(t *testing.T) {
	if got := completionIDText(json.RawMessage(`" chatcmpl-x "`)); got != " chatcmpl-x " {
		t.Fatalf("id = %q", got)
	}
	if completionIDText(json.RawMessage(`"   "`)) != "" {
		t.Fatal("blank id should be empty")
	}
}

func TestOmitTopLevelIDDropsForeignID(t *testing.T) {
	body := `{"id":"upstream","choices":[{"delta":{"content":"a < b"}}],"usage":{"prompt_tokens":9007199254740993}}`
	got, ok := omitTopLevelID(body)
	if !ok {
		t.Fatal("omit failed")
	}
	if strings.Contains(got, "upstream") || strings.Contains(got, `\u003c`) || !strings.Contains(got, "9007199254740993") || !strings.Contains(got, "a < b") {
		t.Fatalf("got %s", got)
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(got), &chunk); err != nil {
		t.Fatal(err)
	}
	if _, exists := chunk["id"]; exists {
		t.Fatalf("id still present: %s", got)
	}
}

func TestEnvelopeBillingCodeStaysOnTheErrorFrame(t *testing.T) {
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	payload, err := json.Marshal(map[string]any{
		"body": `{"code":"110","message":"Billing daily count exceeded"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if writer.line("data:" + string(payload)) {
		t.Fatal("billing denial continued the stream")
	}
	frame := buf.String()
	if !strings.Contains(frame, `"code":"110"`) || !strings.Contains(frame, "Billing daily count exceeded") || !strings.Contains(frame, "上游错误 (110)") {
		t.Fatalf("error frame dropped the billing code:\n%s", frame)
	}

	buf.Reset()
	writer = &envelopeWriter{w: &buf}
	numeric, err := json.Marshal(map[string]any{
		"body": `{"code":110,"message":"Billing daily count exceeded"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if writer.line("data:" + string(numeric)) {
		t.Fatal("numeric billing code continued the stream")
	}
	if !strings.Contains(buf.String(), `"code":"110"`) {
		t.Fatalf("numeric code was dropped:\n%s", buf.String())
	}
}

func TestEnvelopeFailureTailIsAnError(t *testing.T) {
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	payload := `{"success":false,"msgCode":500,"message":"Internal Server Error"}`
	if writer.line("data:" + payload) {
		t.Fatal("failure tail continued the stream")
	}
	if !strings.Contains(buf.String(), "Internal Server Error") || !strings.Contains(buf.String(), "500") {
		t.Fatalf("error missing:\n%s", buf.String())
	}

	withBody, err := json.Marshal(map[string]any{
		"msgCode": 502,
		"message": "nope",
		"body":    `{"id":"chatcmpl-x","choices":[{"delta":{"content":"hidden"}}]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	writer = &envelopeWriter{w: &buf}
	if writer.line("data:" + string(withBody)) {
		t.Fatal("msgCode with a body continued the stream")
	}
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "nope") {
		t.Fatalf("body survived the error tail:\n%s", buf.String())
	}
}

func TestEnvelopeBodyMatchesJSON(t *testing.T) {
	bodies := []string{
		`{"id":"x","choices":[{"delta":{"content":"你好"}}]}`,
		`{"id":"x","choices":[{"delta":{"content":"a < b & c"}}]}`,
		"{\"id\":\"x\",\"choices\":[{\"delta\":{\"content\":\"line\\n\\t\\\"q\\\" \\\\ /\"}}]}",
		`{"id":"x","choices":[{"delta":{"content":"中"}}]}`,
		`{"id":"x"}`,
		``,
	}
	var writer envelopeWriter
	for _, body := range bodies {
		payload, err := json.Marshal(map[string]any{
			"headers": map[string]any{"Content-Type": []string{"application/json"}},
			"body":    body,
		})
		if err != nil {
			t.Fatal(err)
		}
		got, ok := writer.envelopeBody(string(payload))
		var want struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(payload, &want); err != nil {
			t.Fatal(err)
		}
		if !ok || got != want.Body {
			t.Fatalf("body %q\ngot %q ok=%v\nwant %q", body, got, ok, want.Body)
		}
	}
	// Body before headers, plus surrogate pairs encoding/json would decode.
	// An invalid pair is two replacement characters, not one.
	for _, manual := range []string{
		`{"body":"\uD83D\uDE00","headers":{"n":1,"s":"a\"b"}}`,
		`{"body":"\uD800\uD800"}`,
		`{"body":"\uDE00\uD800"}`,
		`{"body":"\uD800\u0041"}`,
		`{"body":"\uD800A"}`,
	} {
		got, ok := writer.envelopeBody(manual)
		var want struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal([]byte(manual), &want); err != nil {
			t.Fatal(err)
		}
		if !ok || got != want.Body {
			t.Fatalf("surrogate %s\ngot %q ok=%v want %q", manual, got, ok, want.Body)
		}
	}
	if _, ok := writer.envelopeBody(`{"success":false,"msgCode":500}`); ok {
		t.Fatal("failure tail was treated as an envelope body")
	}
	if _, ok := writer.envelopeBody(`{"body":"x"}{"extra":true}`); ok {
		t.Fatal("trailing junk was accepted as an envelope")
	}
}

func TestFlushPartialDataLine(t *testing.T) {
	body := `{"id":"chatcmpl-x","choices":[{"delta":{"content":"tail"}}]}`
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	writer := &envelopeWriter{w: &buf}
	if !writer.ingest("data:" + string(payload)) {
		t.Fatal("partial ingest stopped")
	}
	if buf.Len() != 0 {
		t.Fatalf("incomplete line was forwarded:\n%s", buf.String())
	}
	if !writer.flushPartial() {
		t.Fatal("flush stopped")
	}
	if !strings.Contains(buf.String(), `"content":"tail"`) {
		t.Fatalf("flushed %s", buf.String())
	}
}

func writeChatEvent(w http.ResponseWriter, innerBody string) {
	inner, _ := json.Marshal(innerBody)
	_, _ = w.Write([]byte("data:" + `{"headers":{"Content-Type":["application/json"]},"body":` + string(inner) + "}" + "\n\n"))
}
