package main

import (
	"strings"
	"testing"
)

func TestStartCodeGenProxyIsDisabled(t *testing.T) {
	app := &App{}

	msg, err := app.StartCodeGenProxy("https://codegen.qianxin-inc.cn/api/v1", "token")
	if err != nil {
		t.Fatalf("StartCodeGenProxy() error = %v", err)
	}
	if !strings.Contains(msg, "disabled") {
		t.Fatalf("StartCodeGenProxy() = %q, want disabled message", msg)
	}
	if app.IsCodeGenProxyRunning() {
		t.Fatal("CodeGen proxy should not be running")
	}
	if got := app.StopCodeGenProxy(); got != "stopped" {
		t.Fatalf("StopCodeGenProxy() = %q, want stopped", got)
	}
	if app.IsCodeGenProxyRunning() {
		t.Fatal("CodeGen proxy should still not be running after stop")
	}
}
