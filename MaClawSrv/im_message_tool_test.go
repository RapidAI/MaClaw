package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestSrvIMMessageHandlerUnknownAction(t *testing.T) {
	h := newSrvIMMessageHandler(nil)
	got := h(map[string]interface{}{"action": "nope"})
	if !strings.Contains(got, "未知") {
		t.Fatalf("got %q", got)
	}
}

func TestSrvIMMessageHandlerMissingText(t *testing.T) {
	h := newSrvIMMessageHandler(nil)
	got := h(map[string]interface{}{"action": "send", "group_id": "g1"})
	if !strings.Contains(got, "text") {
		t.Fatalf("got %q", got)
	}
}

func TestSrvIMMessageHandlerInfersSend(t *testing.T) {
	h := newSrvIMMessageHandler(nil)
	got := h(map[string]interface{}{
		"text":     "hi",
		"group_id": "g1",
		"channel":  "lansenger",
	})
	// Without credentials may fail send/setup, but must not reject as unknown action.
	if strings.Contains(got, "未知") || strings.Contains(got, "缺少 action") {
		t.Fatalf("got %q", got)
	}
	if scheduler.ResolveIMMessageAction(map[string]interface{}{"text": "hi", "group_id": "g1"}) != "send" {
		t.Fatal("resolve")
	}
}

func TestSrvIMMessageHandlerInfersSendFile(t *testing.T) {
	h := newSrvIMMessageHandler(nil)
	got := h(map[string]interface{}{
		"path":     "report.pdf",
		"group_id": "g1",
		"channel":  "lansenger",
	})
	// Without credentials may fail send/setup, but must not reject as unknown action.
	if strings.Contains(got, "未知") || strings.Contains(got, "缺少 action") {
		t.Fatalf("got %q", got)
	}
	if scheduler.ResolveIMMessageAction(map[string]interface{}{"path": "report.pdf", "group_id": "g1"}) != "send_file" {
		t.Fatal("resolve")
	}
}

func TestSrvToolIMMessageSendFileValidation(t *testing.T) {
	principal := agentservice.Principal{TenantID: "system", UserID: "scheduler"}

	out := srvToolIMMessageSendFileForPrincipal(context.Background(), nil, principal,
		map[string]interface{}{"group_id": "g"})
	if !strings.Contains(out, "缺少 path") {
		t.Fatalf("missing path: %q", out)
	}

	out = srvToolIMMessageSendFileForPrincipal(context.Background(), nil, principal,
		map[string]interface{}{"path": "a.pdf"})
	if !strings.Contains(out, "缺少投递目标") {
		t.Fatalf("missing target: %q", out)
	}
}
