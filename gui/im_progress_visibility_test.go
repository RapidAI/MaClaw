package main

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestIMProgressVisibilityFilterDefaultAllowsAllProgress(t *testing.T) {
	filter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{})
	for i := 0; i < 3; i++ {
		if !filter.ShouldSend() {
			t.Fatalf("enabled filter suppressed progress at call %d", i+1)
		}
	}
}

func TestIMProgressVisibilityFilterDisabledAllowsOnlyFirstProgress(t *testing.T) {
	filter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(false)})
	if !filter.ShouldSend() {
		t.Fatal("disabled filter should allow the first progress message")
	}
	if filter.ShouldSend() {
		t.Fatal("disabled filter should suppress the second progress message")
	}
	if filter.ShouldSend() {
		t.Fatal("disabled filter should suppress later progress messages")
	}
}

func TestIMProgressVisibilityFilterLocalProgressRules(t *testing.T) {
	filter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(false)})
	if filter.ShouldSendProgress("") {
		t.Fatal("empty progress should not be sent")
	}
	if filter.ShouldSendProgress(imHeartbeatMsg) {
		t.Fatal("heartbeat should not be visible to local IM users")
	}
	if !filter.ShouldSendProgress("收到，正在处理") {
		t.Fatal("first real progress should be visible")
	}
	if filter.ShouldSendProgress("正在执行工具") {
		t.Fatal("later real progress should be hidden")
	}
}

func TestIMProgressVisibilityFilterRemoteKeepsHeartbeatAlive(t *testing.T) {
	filter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(false)})
	if text, ok := filter.ForwardProgressOrHeartbeat(""); ok || text != "" {
		t.Fatalf("empty progress forwarded as text=%q ok=%v", text, ok)
	}
	if text, ok := filter.ForwardProgressOrHeartbeat("收到，正在处理"); !ok || text != "收到，正在处理" {
		t.Fatalf("first progress = %q,%v; want visible text", text, ok)
	}
	if text, ok := filter.ForwardProgressOrHeartbeat("正在执行工具"); !ok || text != imHeartbeatMsg {
		t.Fatalf("suppressed progress = %q,%v; want heartbeat", text, ok)
	}
	if text, ok := filter.ForwardProgressOrHeartbeat(imHeartbeatMsg); !ok || text != imHeartbeatMsg {
		t.Fatalf("heartbeat = %q,%v; want heartbeat forwarded", text, ok)
	}
}

func TestIMProgressVisibilityFilterCommandStillRunningProgress(t *testing.T) {
	disabled := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(false)})
	stillRunning := "⏳ 命令仍在执行中（已 30s）: find / -name *.mp4"
	if disabled.ShouldSendProgress(stillRunning) {
		t.Fatal("command-still-running progress should be hidden when hints are disabled")
	}
	if !disabled.ShouldSendProgress("收到，正在处理") {
		t.Fatal("hidden command-still-running progress must not consume first visible progress")
	}
	if text, ok := disabled.ForwardProgressOrHeartbeat(stillRunning); !ok || text != imHeartbeatMsg {
		t.Fatalf("disabled remote command-still-running = %q,%v; want heartbeat", text, ok)
	}

	enabled := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(true)})
	if !enabled.ShouldSendProgress(stillRunning) {
		t.Fatal("first command-still-running progress should be visible when hints are enabled")
	}
	if enabled.ShouldSendProgress("⏳ 命令仍在执行中（已 60s）: find / -name *.mp4") {
		t.Fatal("later command-still-running progress should be hidden even when hints are enabled")
	}
}

func TestIMProgressVisibilityFilterCommandStillRunningEnglishProgress(t *testing.T) {
	filter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(true)})
	if !filter.ShouldSendProgress("command still running (30s): npm test") {
		t.Fatal("first English command-still-running progress should be visible")
	}
	if filter.ShouldSendProgress("command still running (60s): npm test") {
		t.Fatal("later English command-still-running progress should be hidden")
	}
}

func TestIMProgressVisibilityFilterCommandStillRunningConcurrent(t *testing.T) {
	filter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(true)})
	var sent int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if filter.ShouldSendProgress("command still running (30s): npm test") {
				atomic.AddInt32(&sent, 1)
			}
		}()
	}
	wg.Wait()
	if sent != 1 {
		t.Fatalf("concurrent command-still-running visible count = %d, want 1", sent)
	}
}

func TestIMProgressVisibilityFilterHeartbeatDoesNotConsumeFirstVisibleProgress(t *testing.T) {
	filter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(false)})
	if filter.ShouldSendProgress(imHeartbeatMsg) {
		t.Fatal("local heartbeat should stay invisible")
	}
	if !filter.ShouldSendProgress("收到，正在处理") {
		t.Fatal("heartbeat must not consume first visible progress")
	}

	remoteFilter := newIMProgressVisibilityFilterFromConfig(corelib.AppConfig{IMProgressNudgeEnabled: boolPtr(false)})
	if text, ok := remoteFilter.ForwardProgressOrHeartbeat(imHeartbeatMsg); !ok || text != imHeartbeatMsg {
		t.Fatalf("remote heartbeat = %q,%v; want heartbeat forwarded", text, ok)
	}
	if text, ok := remoteFilter.ForwardProgressOrHeartbeat("收到，正在处理"); !ok || text != "收到，正在处理" {
		t.Fatalf("heartbeat consumed first visible progress: %q,%v", text, ok)
	}
}
