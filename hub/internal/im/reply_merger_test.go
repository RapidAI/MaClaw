package im

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReplyMerger_SingleReply(t *testing.T) {
	rm := NewReplyMerger(func() *HubLLMConfig { return nil }, DefaultCircuitBreaker())
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: "hello"}},
	}
	resp, err := rm.MergeReplies(context.Background(), replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Body, "hello") {
		t.Fatalf("expected body to contain 'hello', got: %s", resp.Body)
	}
}

func TestReplyMerger_SingleReplyWithErrors(t *testing.T) {
	rm := NewReplyMerger(func() *HubLLMConfig { return nil }, DefaultCircuitBreaker())
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: "hello"}},
		{Name: "iMac", Err: fmt.Errorf("timeout")},
	}
	resp, err := rm.MergeReplies(context.Background(), replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Body, "1 台设备未回复") {
		t.Fatalf("expected error note, got: %s", resp.Body)
	}
}

func TestReplyMerger_SimilarReplies(t *testing.T) {
	rm := NewReplyMerger(func() *HubLLMConfig { return nil }, DefaultCircuitBreaker())
	body := strings.Repeat("same content ", 20)
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: body}},
		{Name: "iMac", Response: &GenericResponse{Body: body}},
	}
	resp, err := rm.MergeReplies(context.Background(), replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Body, "观点一致") {
		t.Fatalf("expected dedup message, got: %s", resp.Body)
	}
}

func TestReplyMerger_DifferentRepliesNoLLM(t *testing.T) {
	rm := NewReplyMerger(func() *HubLLMConfig { return nil }, DefaultCircuitBreaker())
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: "Go is great for servers"}},
		{Name: "iMac", Response: &GenericResponse{Body: "Python is better for ML"}},
	}
	resp, err := rm.MergeReplies(context.Background(), replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Body, "MacBook") || !strings.Contains(resp.Body, "iMac") {
		t.Fatalf("expected both device names in fallback format, got: %s", resp.Body)
	}
}

func TestReplyMerger_AllErrors(t *testing.T) {
	rm := NewReplyMerger(func() *HubLLMConfig { return nil }, DefaultCircuitBreaker())
	replies := []DeviceReply{
		{Name: "MacBook", Err: fmt.Errorf("err1")},
		{Name: "iMac", Err: fmt.Errorf("err2")},
	}
	resp, err := rm.MergeReplies(context.Background(), replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 503 {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestReplyMerger_DifferentRepliesWithLLM(t *testing.T) {
	srv := mockLLMServer("合并结果：Go适合服务端，Python适合ML")
	defer srv.Close()

	cfg := &HubLLMConfig{
		Enabled:  true,
		APIURL:   srv.URL,
		APIKey:   "test",
		Model:    "test",
		Protocol: "openai",
	}
	rm := NewReplyMerger(func() *HubLLMConfig { return cfg }, DefaultCircuitBreaker())

	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: "Go is great for servers"}},
		{Name: "iMac", Response: &GenericResponse{Body: "Python is better for ML"}},
	}
	resp, err := rm.MergeReplies(context.Background(), replies)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Body, "合并结果") {
		t.Fatalf("expected LLM merged content, got: %s", resp.Body)
	}
}
