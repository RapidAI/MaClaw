package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func followUpCandidates() []DesktopFollowUpCandidate {
	return []DesktopFollowUpCandidate{
		{Session: "bot_1#t-a", Title: "整理上周订单报表", Result: "已生成 订单报表.xlsx"},
		{Session: "bot_1#t-b", Title: "给供应商写催货邮件", Result: "邮件草稿已保存"},
	}
}

func stubFollowUpLLM(t *testing.T, fn func(ctx context.Context, req desktopBotUnderstandRequest) (string, error)) {
	t.Helper()
	prevLLM, prevModels := desktopBotUnderstandLLM, desktopBotAckModels
	t.Cleanup(func() { desktopBotUnderstandLLM, desktopBotAckModels = prevLLM, prevModels })
	desktopBotAckModels = func(*App) (corelib.MaclawLLMConfig, *corelib.MaclawLLMConfig) { return corelib.MaclawLLMConfig{}, nil }
	desktopBotUnderstandLLM = func(ctx context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		return fn(ctx, req)
	}
}

func TestFollowUpRoutesToOldThreadByContent(t *testing.T) {
	var prompt string
	stubFollowUpLLM(t, func(_ context.Context, req desktopBotUnderstandRequest) (string, error) {
		prompt = req.userPrompt
		return `{"task":1,"confidence":0.9,"reason":"refers to the report"}`, nil
	})
	got, err := (&App{}).ClassifyDesktopFollowUp("把报表里的金额列改成两位小数", followUpCandidates(), "zh-Hans")
	if err != nil || got.Session != "bot_1#t-a" || got.Title != "整理上周订单报表" {
		t.Fatalf("route = %+v %v", got, err)
	}
	if !strings.Contains(prompt, "订单报表.xlsx") || !strings.Contains(prompt, "给供应商写催货邮件") {
		t.Fatalf("candidates not offered: %s", prompt)
	}
}

func TestFollowUpUnsureGoesToMain(t *testing.T) {
	for _, raw := range []string{
		`{"task":2,"confidence":0.5}`,  // not confident
		`{"task":0,"confidence":0.95}`, // new matter
		`{"task":7,"confidence":0.99}`, // not a task we offered
		`not json`,
	} {
		raw := raw
		stubFollowUpLLM(t, func(context.Context, desktopBotUnderstandRequest) (string, error) { return raw, nil })
		got, _ := (&App{}).ClassifyDesktopFollowUp("帮我查下明天天气", followUpCandidates(), "zh-Hans")
		if got.Session != "" {
			t.Fatalf("%s -> %+v, want main", raw, got)
		}
	}
	// No candidates: no model call at all.
	stubFollowUpLLM(t, func(context.Context, desktopBotUnderstandRequest) (string, error) {
		t.Fatal("classifier called without candidates")
		return "", nil
	})
	if got, _ := (&App{}).ClassifyDesktopFollowUp("继续", nil, "zh-Hans"); got.Session != "" {
		t.Fatalf("no candidates -> %+v", got)
	}
}

func TestFollowUpTimeoutGoesToMain(t *testing.T) {
	prev := desktopFollowUpBudget
	desktopFollowUpBudget = 100 * time.Millisecond
	t.Cleanup(func() { desktopFollowUpBudget = prev })
	stubFollowUpLLM(t, func(ctx context.Context, _ desktopBotUnderstandRequest) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	start := time.Now()
	got, _ := (&App{}).ClassifyDesktopFollowUp("把报表改一下", followUpCandidates(), "zh-Hans")
	if got.Session != "" {
		t.Fatalf("timeout -> %+v, want main", got)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}
