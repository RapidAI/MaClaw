package guiapp

import "testing"

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
