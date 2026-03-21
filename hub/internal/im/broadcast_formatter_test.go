package im

import (
	"fmt"
	"strings"
	"testing"
)

func TestBroadcastFormatter_MultipleDistinct(t *testing.T) {
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: "Go is great"}},
		{Name: "iMac", Response: &GenericResponse{Body: "Python is better"}},
	}
	result := FormatBroadcastReply(replies)
	if !strings.Contains(result, "━━ MacBook ━━") {
		t.Fatal("expected MacBook separator")
	}
	if !strings.Contains(result, "━━ iMac ━━") {
		t.Fatal("expected iMac separator")
	}
	if !strings.Contains(result, "成功: 2") {
		t.Fatalf("expected success count 2, got: %s", result)
	}
}

func TestBroadcastFormatter_SimilarDedup(t *testing.T) {
	body := strings.Repeat("same content here ", 10)
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: body}},
		{Name: "iMac", Response: &GenericResponse{Body: body}},
		{Name: "Mini", Response: &GenericResponse{Body: body}},
	}
	result := FormatBroadcastReply(replies)
	if !strings.Contains(result, "其他 2 台设备观点一致") {
		t.Fatalf("expected dedup message, got: %s", result)
	}
}

func TestBroadcastFormatter_WithErrors(t *testing.T) {
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: "ok"}},
		{Name: "iMac", Err: fmt.Errorf("timeout")},
	}
	result := FormatBroadcastReply(replies)
	if !strings.Contains(result, "⚠️ 异常设备") {
		t.Fatal("expected error section")
	}
	if !strings.Contains(result, "成功: 1") || !strings.Contains(result, "失败: 1") {
		t.Fatalf("expected stats, got: %s", result)
	}
}

func TestBroadcastFormatter_AllErrors(t *testing.T) {
	replies := []DeviceReply{
		{Name: "MacBook", Err: fmt.Errorf("err1")},
		{Name: "iMac", Response: nil},
	}
	result := FormatBroadcastReply(replies)
	if !strings.Contains(result, "失败: 2") {
		t.Fatalf("expected 2 failures, got: %s", result)
	}
}

func TestBroadcastFormatter_SingleReply(t *testing.T) {
	replies := []DeviceReply{
		{Name: "MacBook", Response: &GenericResponse{Body: "hello"}},
	}
	result := FormatBroadcastReply(replies)
	if !strings.Contains(result, "hello") {
		t.Fatal("expected reply body")
	}
	if !strings.Contains(result, "成功: 1") {
		t.Fatalf("expected success 1, got: %s", result)
	}
}
