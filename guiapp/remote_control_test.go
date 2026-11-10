package guiapp

import (
	"errors"
	"strings"
	"testing"
)

func TestExecuteRemoteControlRejectsUnknownOp(t *testing.T) {
	_, err := (&App{}).executeRemoteControl("drop_database", nil)
	if err == nil || !strings.Contains(err.Error(), "不支持") {
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
	if got := trimRemoteControlText("你好世界", 2); got != "你好" {
		t.Fatalf("got %q", got)
	}
}
