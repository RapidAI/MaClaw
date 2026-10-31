package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestDesktopBotSilenceCases(t *testing.T) {
	if !strings.Contains(desktopBotUnderstandSystem, desktopbot.WorthSayingPrompt) || strings.Contains(desktopBotUnderstandSystem, "没有变化就结束") {
		t.Fatal("the interruption rule is missing from the foreground prompt, or the routine line entered it")
	}
	if strings.Contains(desktopBotUnderstandSystem, "exactly "+desktopbot.SteerOnIt) || !strings.Contains(desktopBotUnderstandSystem, "我会发一篇图文并茂的帖子到csdn") {
		t.Fatal("a direction change must get a model-written first-person sentence, not a canned 在办")
	}
	relays := 0
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		return desktopBotRelayResult{Text: "无变化"}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	var payload string
	desktopBotReportSink = func(raw string) { payload = raw }
	t.Cleanup(func() { desktopBotReportSink = nil })
	_, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID: "sched-quiet", DesktopBotID: "bot_1", Action: "看首页。",
	})
	if err != nil || payload != "" || relays != 0 {
		t.Fatalf("GUI started an unchanged schedule payload=%q relays=%d err=%v", payload, relays, err)
	}
	if desktopScheduleReports("无变化", desktopBotRelayResult{Text: "无变化"}, nil) {
		t.Fatal("无变化 was worth a report")
	}
}
