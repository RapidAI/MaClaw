package guiapp

import (
	"strings"
	"testing"
)

func TestDesktopBotAckLineRejectsBriefEcho(t *testing.T) {
	zh := DesktopBotAckRequest{Kind: "idle", Lang: "zh-Hans", Message: "暂停任务"}
	for _, raw := range []string{
		"They asked to stop but nothing is running. Say plainly nothing was running.",
		"The user asked to stop but nothing was running. Say plainly in Chinese.",
		`idle" - they asked to stop`,
		"Nothing to stop right now.",
	} {
		if got := desktopBotAckLine(raw, zh); got != "" {
			t.Fatalf("%q accepted as %q", raw, got)
		}
	}
	if got := desktopBotAckLine("好，我先停下来了。", zh); got != "好，我先停下来了。" {
		t.Fatalf("natural line rejected: %q", got)
	}
	notice := DesktopBotAckRequest{Kind: "notice", Lang: "zh-Hans", Instruction: "目标已暂停"}
	if got := desktopBotAckLine("目标先停着，想继续就发 /goal resume。", notice); got == "" {
		t.Fatal("literal command in a Chinese line rejected")
	}
	en := DesktopBotAckRequest{Kind: "stopped", Lang: "en"}
	if got := desktopBotAckLine("Stopped the Zhihu post.", en); got == "" {
		t.Fatal("english line rejected for english")
	}
}

func TestDesktopBotAckLineCollapsesAndSkipsRepeats(t *testing.T) {
	req := DesktopBotAckRequest{Kind: "queued", Lang: "zh-Hans", Message: "继续"}
	twice := "现在的活还在跑，我会在它结束后马上接这件事。现在的活还在跑，我会在它结束后马上接这件事。"
	if got := desktopBotAckLine(twice, req); got != "现在的活还在跑，我会在它结束后马上接这件事。" {
		t.Fatalf("collapse = %q", got)
	}
	req.Recent = []string{"你: 继续", "我: 知乎那篇还在发，登录后已经接着做了。"}
	if got := desktopBotAckLine("知乎那篇还在发，登录后已经接着做了。", req); got != "" {
		t.Fatalf("repeat of an earlier line accepted: %q", got)
	}
}

func TestDesktopBotAckUserCarriesStatusAndRecent(t *testing.T) {
	user := desktopBotAckUser(DesktopBotAckRequest{Kind: "continuing", Lang: "zh-Hans", Message: "继续",
		Status: &DesktopBotStatus{ActiveRun: "发知乎摘要", CurrentStep: "正在填正文", Takeover: "handed_back"},
		Recent: []string{"你: 继续"}})
	for _, want := range []string{"active_run: 发知乎摘要", "current_step: 正在填正文", "takeover: handed_back", "recent chat:", "你: 继续"} {
		if !strings.Contains(user, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, user)
		}
	}
}

func TestDesktopBotAckMetaRejectsRawCauseForNotices(t *testing.T) {
	raw := "run interrupted: service restarted while the run was active"
	req := DesktopBotAckRequest{Kind: "interrupted", Lang: "en", Status: &DesktopBotStatus{StopReason: raw, LastTask: "Zhihu answer"}}
	if !desktopBotAckMeta(raw, req) {
		t.Fatalf("raw cause passed through as the notice")
	}
	if desktopBotAckMeta("The Zhihu answer got cut off when the desktop restarted. Want me to pick it up again?", req) {
		t.Fatalf("a rewritten notice was rejected")
	}
}
