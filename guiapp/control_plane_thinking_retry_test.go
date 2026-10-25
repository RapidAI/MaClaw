package guiapp

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRequestIntentClassificationRetriesWhenThinkingOffIsRejected(t *testing.T) {
	const refusal = `{"error":{"message":"该模型始终思考，不支持关闭思考；请使用 low、high 或 max。"}}`
	const answer = `{"intent":"coding","confidence":0.9,"reason":"edit code","evidence":["fix"]}`
	for _, wire := range []string{"chat", "responses"} {
		wire := wire
		t.Run(wire, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := map[string]interface{}{}
				if !decodeJSONBody(t, w, r, &body) {
					return
				}
				n := calls.Add(1)
				thinking, _ := body["thinking"].(map[string]interface{})
				if n == 1 {
					if thinking["type"] != "disabled" {
						t.Errorf("first thinking = %#v, want disabled", body["thinking"])
					}
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(refusal))
					return
				}
				if thinking["type"] != "enabled" {
					t.Errorf("retry thinking = %#v, want enabled", body["thinking"])
				}
				w.Header().Set("Content-Type", "application/json")
				if wire == "responses" {
					_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":` + jsonString(answer) + `}]}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + jsonString(answer) + `}}]}`))
			}))
			defer srv.Close()

			cfg := corelib.MaclawLLMConfig{
				URL: srv.URL, Key: "test-key", Model: "intent-" + wire + "-thinker", ThinkingMode: "disabled",
			}
			if wire == "responses" {
				cfg.WireAPI = "responses"
			}
			got, err := (&IMMessageHandler{}).requestIntentClassification(cfg, "user-1", []interface{}{
				map[string]interface{}{"role": "user", "content": "fix the bug"},
			}, srv.Client())
			if err != nil {
				t.Fatalf("requestIntentClassification: %v", err)
			}
			if got.Intent != intentCoding || got.Confidence != 0.9 {
				t.Fatalf("classification = %#v", got)
			}
			if calls.Load() != 2 {
				t.Fatalf("calls = %d, want 2", calls.Load())
			}
		})
	}
}

func TestRequestIntentClassificationDoesNotRetryOrdinaryBadRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"unknown model"}}`))
	}))
	defer srv.Close()

	_, err := (&IMMessageHandler{}).requestIntentClassification(corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "intent-plain-bad", ThinkingMode: "disabled",
	}, "user-1", []interface{}{
		map[string]interface{}{"role": "user", "content": "fix the bug"},
	}, srv.Client())
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestGroupDiscussionAuthorizationRetriesWhenThinkingOffIsRejected(t *testing.T) {
	const refusal = `{"error":{"message":"该模型始终思考，不支持关闭思考；请使用 low、high 或 max。"}}`
	const answer = `{"decision":"approve","confidence":0.8,"reason":"explicit"}`
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]interface{}{}
		if !decodeJSONBody(t, w, r, &body) {
			return
		}
		n := calls.Add(1)
		thinking, _ := body["thinking"].(map[string]interface{})
		if n == 1 {
			if thinking["type"] != "disabled" {
				t.Errorf("first thinking = %#v, want disabled", body["thinking"])
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(refusal))
			return
		}
		if thinking["type"] != "enabled" {
			t.Errorf("retry thinking = %#v, want enabled", body["thinking"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + jsonString(answer) + `}}]}`))
	}))
	defer srv.Close()

	got, err := requestGroupDiscussionAuthorizationOpenAI(&IMMessageHandler{}, corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "group-chat-thinker", ThinkingMode: "disabled",
	}, []interface{}{
		map[string]interface{}{"role": "user", "content": "start the discussion"},
	}, srv.Client())
	if err != nil {
		t.Fatalf("authorization: %v", err)
	}
	if got.Decision != "approve" {
		t.Fatalf("decision = %q", got.Decision)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}
