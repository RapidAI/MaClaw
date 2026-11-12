package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// A supplement the run took in shows in that run's status line (never
// silent), names a change of direction as such, and fades after a while.
func TestDesktopSupplementAppliedShowsInStatusLine(t *testing.T) {
	now := time.Date(2026, 11, 12, 8, 0, 0, 0, time.Local)
	prev := desktopSupplementNow
	desktopSupplementNow = func() time.Time { return now }
	t.Cleanup(func() { desktopSupplementNow = prev })

	noteDesktopSupplementApplied("bot", "run_sup1", []agentservice.RunSupplement{{ID: "sup_a", Content: "只要前5条"}})
	line := desktopRunStatusLine("run_sup1")
	if !strings.Contains(line, "08:00 已按你的补充调整：只要前5条") {
		t.Fatalf("status line does not show the applied supplement: %q", line)
	}
	noteDesktopSupplementApplied("bot", "run_sup1", []agentservice.RunSupplement{{ID: "sup_b", Content: "【改变做法】改发CSDN"}})
	if got := desktopSupplementStatusPhrase("run_sup1"); !strings.Contains(got, "已按你的新要求改做法（共 2 条）：改发CSDN") {
		t.Fatalf("change of direction not named: %q", got)
	}
	if got := desktopSupplementStatusPhrase("run_other"); got != "" {
		t.Fatalf("another run shows this run's supplement: %q", got)
	}
	now = now.Add(desktopSupplementShownFor + time.Minute)
	if got := desktopSupplementStatusPhrase("run_sup1"); got != "" {
		t.Fatalf("note did not fade: %q", got)
	}
}

// Rule 2: a say that only narrates what the run is doing is not sent; live
// progress is the status line's. A say that carries a result, a question, a
// blocker or a failure still goes out.
func TestDesktopSayProgressOnly(t *testing.T) {
	for _, text := range []string{"正在整理表格", "我正在打开网站，请稍等。", "I'm now opening the site", "处理中"} {
		if !desktopSayProgressOnly(text) {
			t.Fatalf("%q was let through as a chat message", text)
		}
	}
	for _, text := range []string{"周报整理好了，附在这条消息里。", "正在训练，第 3 轮 dev F1 0.91", "正在发布时卡住了：需要你扫码登录", "正在上传，但失败了", "要发知乎还是 CSDN？", "正在 https://example.com 上找"} {
		if desktopSayProgressOnly(text) {
			t.Fatalf("%q was blocked as progress", text)
		}
	}
	if !strings.Contains(desktopSayHint(), "status line") {
		t.Fatal("the say hint does not send progress to the status line")
	}
}
