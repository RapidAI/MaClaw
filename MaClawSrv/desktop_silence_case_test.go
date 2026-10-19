package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotSilenceCases(t *testing.T) {
	got := scheduledTaskPrompt("打开首页")
	if !strings.HasPrefix(got, desktopbot.RoutineQuietPrompt) || strings.Contains(got, "请直接执行以下操作并返回结果") || !strings.Contains(got, "打开首页") {
		t.Fatalf("schedule prompt = %q", got)
	}
	if strings.Contains(desktopbot.WorthSayingPrompt, "没有变化就结束") {
		t.Fatal("routine line entered the system prompt")
	}
	desktopbot.ResetProgressCopies()
	t.Cleanup(desktopbot.ResetProgressCopies)
	desktopbot.RememberProgress("sess-silence", desktopbot.Sections{
		WhereWeAre: "正在把登录后的第一屏存下来。", NextStep: "打开设置里的通知页。", OpenProblems: "无",
	}, desktopbot.TaskProgressPath("sess-silence"))
	body := desktopRunAPIBody("", &agentservice.Run{ID: "run-silence", SessionID: "sess-silence"})
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "打开设置里的通知页。") || strings.Contains(string(raw), `"send_to_user"`) {
		t.Fatalf("run view = %s", raw)
	}
}
