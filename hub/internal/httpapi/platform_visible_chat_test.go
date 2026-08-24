package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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
