package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestSrvIMFileHandlerSendsExactTargetDirectly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(path, []byte("pdf-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := newSrvIMFileHandlerWithSender(nil, func(_ context.Context, _ *agentservice.Service, _ agentservice.Principal, channel string, target scheduler.DeliveryTarget, data []byte, fileName, mimeType, caption string) error {
		calls++
		if channel != scheduler.DeliveryChannelLansenger || target.GroupID != "g-9" || target.Kind != scheduler.DeliveryKindGroup {
			t.Fatalf("channel=%q target=%#v", channel, target)
		}
		if string(data) != "pdf-data" || fileName != "report.pdf" || mimeType != "application/pdf" || caption != "研发报告" {
			t.Fatalf("file args data=%q name=%q mime=%q caption=%q", data, fileName, mimeType, caption)
		}
		return nil
	})
	result := h(map[string]interface{}{
		"path": path, "destination": "im", "channel": "lansenger", "group_id": "g-9", "message": "研发报告",
	})
	if calls != 1 {
		t.Fatalf("sender calls=%d", calls)
	}
	if strings.Contains(result, "[file_base64|") || !strings.Contains(result, "sent") {
		t.Fatalf("result=%q; exact delivery must not fall back to artifact envelope", result)
	}
}

func TestSrvIMFileHandlerContextForwardsRequestPrincipal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}
	h := newSrvIMFileHandlerContextWithSender(nil, func(_ context.Context, _ *agentservice.Service, got agentservice.Principal, _ string, _ scheduler.DeliveryTarget, _ []byte, _, _, _ string) error {
		if got.TenantID != want.TenantID || got.UserID != want.UserID {
			t.Fatalf("principal=%#v, want %#v", got, want)
		}
		return nil
	})
	result := h(context.Background(), want, map[string]interface{}{
		"path": path, "channel": "lansenger", "group_id": "g-9",
	})
	if strings.HasPrefix(result, "Error:") {
		t.Fatalf("result=%q", result)
	}
}

func TestSrvIMFileHandlerExactTargetFailureDoesNotReturnArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newSrvIMFileHandlerWithSender(nil, func(context.Context, *agentservice.Service, agentservice.Principal, string, scheduler.DeliveryTarget, []byte, string, string, string) error {
		return errors.New("offline")
	})
	result := h(map[string]interface{}{
		"path": path, "channel": "lansenger", "group_id": "g-9",
	})
	if !strings.HasPrefix(result, "Error:") || !strings.Contains(result, "offline") || strings.Contains(result, "[file_base64|") {
		t.Fatalf("result=%q", result)
	}
}

func TestSrvIMFileHandlerUntargetedKeepsArtifactEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newSrvIMFileHandlerWithSender(nil, nil)
	result := h(map[string]interface{}{"path": path, "destination": "im"})
	if !strings.HasPrefix(result, "[file_base64|report.txt|") || !strings.Contains(result, "|im]") {
		t.Fatalf("result=%q", result)
	}
}
