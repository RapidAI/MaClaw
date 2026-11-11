package botmgmt

import (
	"strings"
	"testing"
)

func TestDesktopRunStatusLineIsKeptForTheRunningRun(t *testing.T) {
	s := &Service{desktopAdmits: map[string]*desktopAdmitState{}}
	key := desktopAdmitKey("t", "u", "bot_a", "run_1")
	state := &desktopAdmitState{}
	s.desktopAdmits[key] = state
	s.rememberDesktopRunStatusLine(state, "单号 run_1 ·\n 第 3 步 · 后台任务运行中，下次查看 21:40 "+strings.Repeat("x", 400))
	got := s.DesktopRunStatusLine("t", "u", "bot_a", "run_1")
	if !strings.HasPrefix(got, "单号 run_1 · 第 3 步 · 后台任务运行中，下次查看 21:40") || len([]rune(got)) != desktopRunStatusLineMax {
		t.Fatalf("status line %q", got)
	}
	state.done = true
	if s.DesktopRunStatusLine("t", "u", "bot_a", "run_1") != "" {
		t.Fatal("finished run keeps a status line")
	}
	if s.DesktopRunStatusLine("t", "u", "bot_b", "run_1") != "" {
		t.Fatal("other bot sees the status line")
	}
}
