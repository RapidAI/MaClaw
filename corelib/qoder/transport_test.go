package qoder

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func writeChatEvent(w http.ResponseWriter, innerBody string) {
	inner, _ := json.Marshal(innerBody)
	_, _ = w.Write([]byte("data:" + `{"headers":{"Content-Type":["application/json"]},"body":` + string(inner) + "}" + "\n\n"))
}
