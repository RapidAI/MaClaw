package guiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResponsesConnectionProbeRetriesWhenThinkingOffIsRejected(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		n := calls.Add(1)
		thinking, _ := body["thinking"].(map[string]interface{})
		if n == 1 {
			if thinking["type"] != "disabled" {
				t.Errorf("first thinking = %#v, want disabled", body["thinking"])
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"该模型始终思考，不支持关闭思考；请使用 low、high 或 max。"}}`))
			return
		}
		if thinking["type"] != "enabled" {
			t.Errorf("retry thinking = %#v, want enabled", body["thinking"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()

	app := &App{}
	text, err := app.testResponsesAPILLM(corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "custom-thinker", ThinkingMode: "disabled",
	})
	if err != nil {
		t.Fatalf("testResponsesAPILLM: %v", err)
	}
	if text != "ok" {
		t.Fatalf("text = %q, want ok", text)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestClassifyVisionProbeIgnoresThinkingOffRefusal(t *testing.T) {
	body := []byte(`{"error":{"message":"该模型始终思考，不支持关闭思考；请使用 low、high 或 max。","param":"input[0].content[1].image_url"}}`)
	if got := classifyVisionProbeHTTPFailure(http.StatusBadRequest, body); got != visionProbeInconclusive {
		t.Fatalf("classify = %q, want inconclusive", got)
	}
	imageOnly := []byte(`{"error":{"message":"This model does not support image inputs"}}`)
	if got := classifyVisionProbeHTTPFailure(http.StatusBadRequest, imageOnly); got != visionProbeUnsupported {
		t.Fatalf("classify image rejection = %q, want unsupported", got)
	}
}

func TestResponsesVisionProbeRetriesWhenThinkingOffIsRejected(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		n := calls.Add(1)
		thinking, _ := body["thinking"].(map[string]interface{})
		if n == 1 {
			if thinking["type"] != "disabled" {
				t.Errorf("first thinking = %#v, want disabled", body["thinking"])
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"该模型始终思考，不支持关闭思考；请使用 low、high 或 max。","param":"input[0].content[1].image_url"}}`))
			return
		}
		if thinking["type"] != "enabled" {
			t.Errorf("retry thinking = %#v, want enabled", body["thinking"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"red"}]}]}`))
	}))
	defer srv.Close()

	got := probeVisionResponsesAPIResult(corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "vision-custom-thinker", WireAPI: "responses", ThinkingMode: "disabled",
	})
	if got != visionProbeSupported {
		t.Fatalf("vision = %q, want supported", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestResponsesVisionProbeDoesNotRetryImageRejection(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"This model does not support image inputs"}}`))
	}))
	defer srv.Close()

	got := probeVisionResponsesAPIResult(corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "custom-thinker", WireAPI: "responses", ThinkingMode: "disabled",
	})
	if got != visionProbeUnsupported {
		t.Fatalf("vision = %q, want unsupported", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}
