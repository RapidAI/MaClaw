package lobsterai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestBuildLoginURLFacts(t *testing.T) {
	redirect := "http://127.0.0.1:54321/auth/callback"
	url := BuildLoginURL(redirect, "statehex0001")
	if !strings.HasPrefix(url, PortalBase+LoginPath) {
		t.Fatalf("portal base wrong: %s", url)
	}
	if !strings.Contains(url, "source=electron") {
		t.Fatalf("source param missing: %s", url)
	}
	if !strings.Contains(url, "redirect_uri="+escape(redirect)) {
		t.Fatalf("redirect_uri not encoded: %s", url)
	}
	if !strings.Contains(url, "state=statehex0001") {
		t.Fatalf("state missing: %s", url)
	}
}

func escape(raw string) string {
	replacer := strings.NewReplacer(":", "%3A", "/", "%2F")
	return replacer.Replace(raw)
}

func TestPrepareBodyForcesStream(t *testing.T) {
	payload := []byte(`{"model":"glm-5.3","stream":false,"messages":[],"tool_choice":"none"}`)
	body, originalStream := prepareBody(payload)
	if originalStream {
		t.Fatal("original stream must stay false")
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["stream"] != true {
		t.Fatalf("stream must be forced: %v", obj["stream"])
	}
	if _, has := obj["tool_choice"]; has {
		t.Fatalf("none tool_choice must be dropped: %v", obj["tool_choice"])
	}
	if obj["model"] != "glm-5.3" {
		t.Fatalf("model must ride along: %v", obj["model"])
	}
}

func TestSSEErrorFrameDetection(t *testing.T) {
	stream := []byte("data: {\"error\":{\"type\":\"proxy_error\",\"message\":\"quota gone\",\"code\":40201}}\n\n")
	frame := sseErrorFrame(stream)
	if frame == "" {
		t.Fatal("error frame must be detected")
	}
	if !strings.Contains(frame, "40201") || !strings.Contains(frame, "quota gone") {
		t.Fatalf("frame detail wrong: %s", frame)
	}
	plain := []byte("data: {\"id\":\"1\",\"choices\":[]}\n\n")
	if sseErrorFrame(plain) != "" {
		t.Fatal("plain chunks must not be flagged")
	}
}

func TestAggregateSSEOpenAIShape(t *testing.T) {
	upstream := "" +
		"data: {\"id\":\"u1\",\"model\":\"glm-5.3\",\"created\":1700000000,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"}}]}\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n" +
		"data: [DONE]\n"
	raw, err := aggregateSSE(strings.NewReader(upstream))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Object != "chat.completion" || result.ID != "u1" || result.Model != "glm-5.3" {
		t.Fatalf("envelope wrong: %+v", result)
	}
	if result.Choices[0].Message.Content != "你好" {
		t.Fatalf("content wrong: %q", result.Choices[0].Message.Content)
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish wrong: %s", result.Choices[0].FinishReason)
	}
	if result.Usage["total_tokens"].(float64) != 5 {
		t.Fatalf("usage wrong: %v", result.Usage)
	}
}

func TestAggregateSSEErrorMidStream(t *testing.T) {
	upstream := "data: {\"error\":{\"message\":\"unknown model\",\"code\":40300}}\n\n"
	if _, err := aggregateSSE(strings.NewReader(upstream)); err == nil {
		t.Fatal("mid-stream error frame must fail the aggregate")
	}
}

func TestShouldRewritePaths(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, APIBase+"/v1/chat/completions", nil)
	if !shouldRewrite(req) {
		t.Fatal("API host chat path must rewrite")
	}
	req, _ = http.NewRequest(http.MethodPost, "https://api.example.com/other", nil)
	req.Header.Set(MarkerHeader, "1")
	if !shouldRewrite(req) {
		t.Fatal("marker must force the rewrite")
	}
	req, _ = http.NewRequest(http.MethodGet, APIBase+"/api/models/available", nil)
	if shouldRewrite(req) {
		t.Fatal("GET requests must not rewrite")
	}
	req, _ = http.NewRequest(http.MethodPost, "https://api.deepseek.com/v1/chat/completions", nil)
	if shouldRewrite(req) {
		t.Fatal("foreign hosts must not rewrite")
	}
}

func TestApplyChatHeadersFamily(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer abc")
	applyChatHeaders(h, h.Get("Authorization"))
	if got := h.Get("Authorization"); got != "Bearer abc" {
		t.Fatalf("bearer must stay: %q", got)
	}
	if h.Get("X-LobsterAI-Client-Capabilities") == "" || h.Get("X-LobsterAI-Client-Version") == "" {
		t.Fatalf("client headers missing: %v", h)
	}
	if h.Get("User-Agent") != UserAgent {
		t.Fatalf("user agent wrong: %q", h.Get("User-Agent"))
	}
}

func TestPostJSON5xxStaysRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream busy"))
	}))
	defer srv.Close()
	_, err := postJSON(context.Background(), srv.URL, map[string]any{})
	if err == nil {
		t.Fatal("5xx must return an error")
	}
	var stale *upstreamRejection
	if errors.As(err, &stale) {
		t.Fatalf("5xx must not be classified as an invalid credential: %v", err)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error should carry the status: %v", err)
	}
}

func TestPostJSON429StaysRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := postJSON(context.Background(), srv.URL, map[string]any{})
	if err == nil {
		t.Fatal("429 must return an error")
	}
	var stale *upstreamRejection
	if errors.As(err, &stale) {
		t.Fatalf("429 must stay retryable, not a re-login demand: %v", err)
	}
}

func TestPostJSON4xxIsDefinitive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":10101,"msg":"bad grant"}`))
	}))
	defer srv.Close()
	_, err := postJSON(context.Background(), srv.URL, map[string]any{})
	if err == nil {
		t.Fatal("4xx must return an error")
	}
	var stale *upstreamRejection
	if !errors.As(err, &stale) {
		t.Fatalf("4xx must be classified as a definitive refusal: %v", err)
	}
}

func TestCanonicalChatURLPinsBase(t *testing.T) {
	if got := CanonicalChatURL("https://lobsterai-server.youdao.com:443/"); got != APIBase {
		t.Fatalf("canonical url wrong: %s", got)
	}
	if got := CanonicalChatURL("https://api.example.com"); got != "https://api.example.com" {
		t.Fatalf("foreign url must stay: %s", got)
	}
}

func TestJWTExpiryReadsClaim(t *testing.T) {
	const exp = 1900000000
	payload := `{"exp":` + intToString(exp) + `}`
	token := "h." + base64RawURL([]byte(payload)) + ".sig"
	if got := jwtExpiry(token); got != exp {
		t.Fatalf("exp wrong: %d", got)
	}
	if got := jwtExpiry("not-a-jwt"); got != 0 {
		t.Fatalf("non-jwt must be 0: %d", got)
	}
}

func base64RawURL(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func intToString(v int) string {
	return strconv.Itoa(v)
}
