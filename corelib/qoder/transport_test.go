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

func writeChatEvent(w http.ResponseWriter, innerBody string) {
	inner, _ := json.Marshal(innerBody)
	_, _ = w.Write([]byte("data:" + `{"headers":{"Content-Type":["application/json"]},"body":` + string(inner) + "}" + "\n\n"))
}
