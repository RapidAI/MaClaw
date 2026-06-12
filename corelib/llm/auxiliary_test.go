package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

type auxiliaryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f auxiliaryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAuxiliaryCallerUsesUnifiedOpenAIRequestBuilder(t *testing.T) {
	var gotPath string
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer aux-key" {
			t.Fatalf("Authorization = %q, want bearer key", got)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel, _ = body["model"].(string)
		if got := body["stream"]; got != false {
			t.Fatalf("stream = %#v, want false", got)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	caller := NewAuxiliaryCaller(AuxiliaryConfig{
		URL:   srv.URL,
		Key:   "aux-key",
		Model: "auto",
	})
	got, err := caller.ChatCall([]map[string]string{{"role": "user", "content": "hi"}})
	if err != nil {
		t.Fatalf("ChatCall returned error: %v", err)
	}
	if strings.TrimSpace(got) != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotModel != "auto" {
		t.Fatalf("model = %q, want auto for non-CodeGen URL", gotModel)
	}
}

func TestAuxiliaryCallerNormalizesCodeGenAutoModel(t *testing.T) {
	var gotModel string
	caller := NewAuxiliaryCaller(AuxiliaryConfig{
		URL:   "https://codegen.qianxin-inc.cn/api",
		Key:   "aux-key",
		Model: "auto",
	})
	caller.HTTPClient = &http.Client{Transport: auxiliaryRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel, _ = body["model"].(string)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)),
			Request:    r,
		}, nil
	})}

	if _, err := caller.ChatCall([]map[string]string{{"role": "user", "content": "hi"}}); err != nil {
		t.Fatalf("ChatCall returned error: %v", err)
	}
	if gotModel != corelib.CodeGenDefaultModelID {
		t.Fatalf("model = %q, want %q", gotModel, corelib.CodeGenDefaultModelID)
	}
}

func TestAuxiliaryCallerPreservesVersionedV4BaseURL(t *testing.T) {
	var gotPath string
	caller := NewAuxiliaryCaller(AuxiliaryConfig{
		URL:   "https://open.bigmodel.cn/api/paas/v4",
		Key:   "aux-key",
		Model: "glm-5.1",
	})
	caller.HTTPClient = &http.Client{Transport: auxiliaryRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)),
			Request:    r,
		}, nil
	})}

	if _, err := caller.ChatCall([]map[string]string{{"role": "user", "content": "hi"}}); err != nil {
		t.Fatalf("ChatCall returned error: %v", err)
	}
	if gotPath != "/api/paas/v4/chat/completions" {
		t.Fatalf("path = %q, want /api/paas/v4/chat/completions", gotPath)
	}
}
