package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestToolIMMessageMissingText(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	got := h.toolIMMessage(map[string]interface{}{
		"action":     "send",
		"group_name": "产品讨论群",
		"channel":    "lansenger",
	})
	if !strings.Contains(got, "text") {
		t.Fatalf("want missing text error, got %q", got)
	}
}

func TestToolIMMessageMissingTarget(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	got := h.toolIMMessage(map[string]interface{}{
		"action": "send",
		"text":   "hello weather",
	})
	if !strings.Contains(got, "投递目标") {
		t.Fatalf("want missing target error, got %q", got)
	}
}

func TestToolIMMessageUnknownAction(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.toolIMMessage(map[string]interface{}{"action": "explode"})
	if !strings.Contains(got, "未知") {
		t.Fatalf("got %q", got)
	}
}

func TestToolIMMessageInfersSendWithoutAction(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	// No action, but text+group → send path (fails on missing resolve/credentials, not unknown action).
	got := h.toolIMMessage(map[string]interface{}{
		"text":       "北京天气",
		"group_name": "研发学院_校友群",
		"channel":    "lansenger",
	})
	if strings.Contains(got, "未知") || strings.Contains(got, "缺少 action") {
		t.Fatalf("should infer send, got %q", got)
	}
	// Without live gateway, expect send failure or resolve failure — not list path.
	if strings.Contains(got, "查询投递目标") {
		t.Fatalf("should not list_targets: %q", got)
	}
}

func TestToolIMMessageInfersListWithoutAction(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	got := h.toolIMMessage(map[string]interface{}{
		"query":   "校友",
		"channel": "lansenger",
	})
	// list path; may fail without catalog, but must not require text.
	if strings.Contains(got, "缺少 text") {
		t.Fatalf("list should not require text: %q", got)
	}
	if strings.Contains(got, "未知") {
		t.Fatalf("should infer list_targets: %q", got)
	}
}

func TestParseScheduleDeliveryArgsForIMMessage(t *testing.T) {
	d, err := parseScheduleDeliveryArgs(map[string]interface{}{
		"group_name": "研发学院_校友群",
		"channel":    "lansenger",
		"text":       "ignored for parse",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || !d.Enabled || d.Channel != scheduler.DeliveryChannelLansenger {
		t.Fatalf("%#v", d)
	}
	if d.Targets[0].GroupName != "研发学院_校友群" {
		t.Fatalf("group_name=%q", d.Targets[0].GroupName)
	}
}
