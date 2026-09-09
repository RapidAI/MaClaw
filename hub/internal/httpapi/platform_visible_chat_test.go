package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestConsumeRuntimeSSEResponseFallsBackWhenDoneIsSentinelOnly(t *testing.T) {
	s := platformAwareMachineSender{}
	body := strings.NewReader("data: {\"chunk\":\"Hello Kate\"}\n\ndata: {\"done\":true,\"content\":\"\\u0001\"}\n\n")
	got, err := s.consumeRuntimeSSEResponse(body, "tenant", digitalEmployeeEntry{ID: "ve-1"}, time.Now(), nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != "Hello Kate" {
		t.Fatalf("got %q, want streamed answer after tofu-only done", got)
	}
}

func TestConsumeRuntimeSSEResponseUsesDoneMessageWhenContentIsTofu(t *testing.T) {
	s := platformAwareMachineSender{}
	body := strings.NewReader("data: {\"done\":true,\"content\":\"\\u0001\",\"message\":{\"content\":\"Hello Kate\"}}\n\n")
	got, err := s.consumeRuntimeSSEResponse(body, "tenant", digitalEmployeeEntry{ID: "ve-1"}, time.Now(), nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != "Hello Kate" {
		t.Fatalf("got %q, want message.content after tofu-only done content", got)
	}
}

func TestConsumeRuntimeSSEResponseDropsReasoningChunks(t *testing.T) {
	s := platformAwareMachineSender{}
	body := strings.NewReader("data: {\"chunk\":\"\\u0001thinking\"}\n\ndata: {\"chunk\":\"answer\"}\n\ndata: {\"done\":true,\"content\":\"answer\"}\n\n")
	got, err := s.consumeRuntimeSSEResponse(body, "tenant", digitalEmployeeEntry{ID: "ve-1"}, time.Now(), nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got != "answer" {
		t.Fatalf("got %q, want answer without reasoning chunk", got)
	}
}

func TestConsumeRuntimeSSEResponsePreservesErrorReason(t *testing.T) {
	s := platformAwareMachineSender{}
	body := strings.NewReader("data: {\"error\":\"LLM provider returned 503: overloaded\"}\n\n")
	_, err := s.consumeRuntimeSSEResponse(body, "tenant", digitalEmployeeEntry{ID: "ve-1"}, time.Now(), nil)
	if err == nil || !strings.Contains(err.Error(), "LLM provider returned 503: overloaded") {
		t.Fatalf("error=%v, want runtime detail preserved", err)
	}
}

func TestMacLawSrvRuntimeFailureContentPreservesSanitizedReason(t *testing.T) {
	content := macLawSrvRuntimeFailureContent(errors.New("MaClawSrv runtime error for employee-secret: LLM provider returned 503"), "employee-secret")
	if !strings.Contains(content, "LLM provider returned 503") {
		t.Fatalf("content=%q, want actionable runtime reason", content)
	}
	if strings.Contains(content, "employee-secret") {
		t.Fatalf("content=%q leaked platform employee id", content)
	}
}

func TestRuntimeErrorResponseDetailExtractsStructuredReason(t *testing.T) {
	if got := runtimeErrorResponseDetail([]byte(`{"error":"LLM provider returned 503","stage":"execute"}`)); got != "LLM provider returned 503" {
		t.Fatalf("detail=%q, want error field", got)
	}
	if got := runtimeErrorResponseDetail([]byte(`{"error":{"message":"model unavailable"}}`)); got != "model unavailable" {
		t.Fatalf("detail=%q, want nested error message", got)
	}
	if got := runtimeErrorResponseDetail([]byte("data: {\"error\":\"runtime down\"}\n\n")); got != "runtime down" {
		t.Fatalf("detail=%q, want SSE error message", got)
	}
}

func TestTruncateRemoteResponseDetailKeepsUTF8Boundaries(t *testing.T) {
	detail := strings.Repeat("错误", 300)
	truncated := truncateRemoteResponseDetail(detail)
	if !strings.HasSuffix(truncated, "...") || !utf8.ValidString(truncated) {
		t.Fatalf("truncated detail is not valid UTF-8: %q", truncated)
	}
}

func TestMacLawSrvRuntimeReplyContentSanitizesMessage(t *testing.T) {
	body, err := json.Marshal(map[string]any{"message": map[string]any{"content": "\x01Hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := macLawSrvRuntimeReplyContent(body); got != "Hello" {
		t.Fatalf("message.content = %q, want Hello", got)
	}
	if got := macLawSrvRuntimeReplyContent([]byte("\x01")); got != "" {
		t.Fatalf("sentinel-only raw body = %q, want empty", got)
	}

	fallback, err := json.Marshal(map[string]any{"message": map[string]any{"content": "\x01"}, "content": "Hello Kate"})
	if err != nil {
		t.Fatal(err)
	}
	if got := macLawSrvRuntimeReplyContent(fallback); got != "Hello Kate" {
		t.Fatalf("tofu message.content hid root content: %q", got)
	}

	nullContent, err := json.Marshal(map[string]any{"message": map[string]any{"content": nil}, "content": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got := macLawSrvRuntimeReplyContent(nullContent); got != "Hello" {
		t.Fatalf("null message.content = %q, want Hello", got)
	}
}
