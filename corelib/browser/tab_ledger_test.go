package browser

import (
	"testing"
	"time"
)

func TestRunTabsCloseOnlyWhatThatRunOpened(t *testing.T) {
	led := NewTabLedger(6)
	now := time.Date(2026, 11, 13, 16, 0, 0, 0, time.UTC)
	led.now = func() time.Time { return now }
	if extra := led.Opened("bot_a", "run_a", "tab_a"); len(extra) != 0 {
		t.Fatalf("extra %v", extra)
	}
	led.Opened("bot_b", "run_b", "tab_b")
	closed := led.EndRun("run_a")
	if len(closed) != 1 || closed[0] != "tab_a" {
		t.Fatalf("closed %v", closed)
	}
	if again := led.EndRun("run_b"); len(again) != 1 || again[0] != "tab_b" {
		t.Fatalf("other run %v", again)
	}
}

func TestEndRunLeavesAKeptResultPageOpen(t *testing.T) {
	led := NewTabLedger(6)
	led.Opened("bot_a", "run_a", "scratch")
	led.Opened("bot_a", "run_a", "result")
	led.Keep("result")
	closed := led.EndRun("run_a")
	if len(closed) != 1 || closed[0] != "scratch" {
		t.Fatalf("closed %v, want only the scratch tab", closed)
	}
	if again := led.EndRun("run_a"); len(again) != 0 {
		t.Fatalf("result page closed on a second end: %v", again)
	}
}

func TestBotTabCapDropsThatBotsOldestTab(t *testing.T) {
	led := NewTabLedger(2)
	t0 := time.Date(2026, 11, 13, 16, 0, 0, 0, time.UTC)
	led.now = func() time.Time { return t0 }
	led.Opened("bot_a", "run_a", "old")
	t0 = t0.Add(time.Minute)
	led.Opened("bot_a", "run_a", "mid")
	led.Opened("bot_b", "run_b", "other")
	t0 = t0.Add(time.Minute)
	extra := led.Opened("bot_a", "run_a", "new")
	if len(extra) != 1 || extra[0] != "old" {
		t.Fatalf("dropped %v", extra)
	}
	if left := led.EndRun("run_b"); len(left) != 1 || left[0] != "other" {
		t.Fatalf("other bot was touched: %v", left)
	}
	own := led.EndRun("run_a")
	if len(own) != 2 {
		t.Fatalf("remaining %v", own)
	}
}
