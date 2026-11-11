package guiapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestExecuteRemoteControlRecoveredWithoutApp(t *testing.T) {
	_, err := (&RemoteHubClient{}).executeRemoteControlRecovered("list_tasks", nil)
	if err == nil || !strings.Contains(err.Error(), "尚未就绪") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteRemoteControlRejectsUnknownOp(t *testing.T) {
	_, err := (&App{}).executeRemoteControl("drop_database", nil)
	if err == nil || !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteRemoteControlCreateTaskRequiresName(t *testing.T) {
	_, err := (&App{}).executeRemoteControl("create_task", json.RawMessage(`{"name":"  "}`))
	if err == nil || !strings.Contains(err.Error(), "任务名称") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecuteRemoteControlCreateTaskRejectsSSHMode(t *testing.T) {
	// SSH targets cannot be configured through remote control; the whitelist
	// rejects before CreateTaskWithMode runs.
	_, err := (&App{}).executeRemoteControl("create_task", json.RawMessage(`{"name":"x","mode":"ssh_host"}`))
	if err == nil || !strings.Contains(err.Error(), "任务模式") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteControlStopTreatsIdleSessionAsSuccess(t *testing.T) {
	if !remoteControlStopIsIdle(errors.New("no active session to cancel")) {
		t.Fatal("idle stop should be success")
	}
	if remoteControlStopIsIdle(errors.New("AI assistant not initialized")) {
		t.Fatal("a real stop failure must stay a failure")
	}
}

func TestRemoteControlKnownTaskRejectsEmptyPath(t *testing.T) {
	if _, err := (&App{}).remoteControlKnownTask("  "); err == nil {
		t.Fatal("expected missing task")
	}
}

func TestRemoteControlMessageRejectsDesktopCommands(t *testing.T) {
	if err := remoteControlMessageError("继续看一下"); err != nil {
		t.Fatal(err)
	}
	if err := remoteControlMessageError("/exec whoami"); err == nil {
		t.Fatal("slash command should be rejected")
	}
	if err := remoteControlMessageError("__agent_view_submit__ {}"); err == nil {
		t.Fatal("task panel action should be rejected")
	}
}

func TestTrimRemoteControlTextKeepsShortMessages(t *testing.T) {
	if trimRemoteControlText("你好", 4) != "你好" {
		t.Fatal("short text changed")
	}
	if got := trimRemoteControlText("你好世界", 2); got != "你好…" {
		t.Fatalf("got %q", got)
	}
}
