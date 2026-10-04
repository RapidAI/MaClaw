package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
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

func TestConsumeRuntimeSSEResponseForwardsActivityWithoutTranscript(t *testing.T) {
	s := platformAwareMachineSender{}
	body := strings.NewReader("data: {\"status\":\"accepted\"}\n\ndata: {\"status\":\"tool_start\",\"name\":\"knowledge_search\"}\n\ndata: {\"chunk\":\"Hi\"}\n\ndata: {\"status\":\"tool_done\",\"name\":\"knowledge_search\"}\n\ndata: {\"status\":\"not-a-phase\"}\n\ndata: {\"done\":true,\"content\":\"Hi\"}\n\n")
	var got []string
	reply, err := s.consumeRuntimeSSEResponse(body, "tenant", digitalEmployeeEntry{ID: "ve-1"}, time.Now(), nil, func(phase, name string) {
		got = append(got, phase+":"+name)
	})
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if reply != "Hi" {
		t.Fatalf("reply=%q, want Hi", reply)
	}
	want := "accepted:,tool_start:knowledge_search,tool_done:knowledge_search"
	if strings.Join(got, ",") != want {
		t.Fatalf("activity=%q, want %s", strings.Join(got, ","), want)
	}
}

func TestNormalizeRuntimeActivityRejectsUnknownPhase(t *testing.T) {
	if _, _, ok := normalizeRuntimeActivity("thinking", "bash"); ok {
		t.Fatal("unknown phase should be dropped")
	}
	phase, name, ok := normalizeRuntimeActivity("tool_start", "bash; rm")
	if !ok || phase != "tool_start" || name != "" {
		t.Fatalf("phase=%q name=%q ok=%v, dirty tool name must not be shown", phase, name, ok)
	}
	phase, name, ok = normalizeRuntimeActivity("tool_start", "knowledge_search")
	if !ok || phase != "tool_start" || name != "knowledge_search" {
		t.Fatalf("phase=%q name=%q ok=%v", phase, name, ok)
	}
}

func TestVEStreamChunkCoalescerPaintsFirstTokenThenBatches(t *testing.T) {
	var got []string
	var live veStreamChunkCoalescer
	push := func(chunk string) { got = append(got, chunk) }
	live.add("A", push)
	if len(got) != 1 || got[0] != "A" {
		t.Fatalf("first flush=%q, want the first token alone", got)
	}
	live.add("B", push)
	live.add("C", push)
	if len(got) != 1 {
		t.Fatalf("fast follow-up tokens were pushed immediately: %q", got)
	}
	live.add(strings.Repeat("x", veStreamPushMaxBytes), push)
	if len(got) != 2 || got[0] != "A" || got[1] != "BC"+strings.Repeat("x", veStreamPushMaxBytes) {
		t.Fatalf("full batch=%q, want A then the held tail plus the full batch", got)
	}
	live.flush(push)
	time.Sleep(veStreamPushFlushInterval + 30*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("timer flushed again after close: %q", got)
	}
	var tail veStreamChunkCoalescer
	var tailGot []string
	tail.add("A", func(chunk string) { tailGot = append(tailGot, chunk) })
	tail.add("Z", func(chunk string) { tailGot = append(tailGot, chunk) })
	tail.flush(func(chunk string) { tailGot = append(tailGot, chunk) })
	if strings.Join(tailGot, "") != "AZ" || len(tailGot) != 2 || tailGot[1] != "Z" {
		t.Fatalf("end flush=%q, want A then Z", tailGot)
	}
}

func TestVEStreamChunkCoalescerReleasesTailWhenTheModelPauses(t *testing.T) {
	var mu sync.Mutex
	var got []string
	var live veStreamChunkCoalescer
	push := func(chunk string) {
		mu.Lock()
		got = append(got, chunk)
		mu.Unlock()
	}
	live.add("A", push)
	live.add("B", push)
	live.add("C", push)
	time.Sleep(veStreamPushFlushInterval + 40*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, "") != "ABC" || len(got) != 2 || got[1] != "BC" {
		t.Fatalf("got %q, want A then BC without an explicit flush", got)
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
