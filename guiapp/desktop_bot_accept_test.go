package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDesktopBotAcceptToldKeepsOnlyTheModelsOwnSentence(t *testing.T) {
	in := "去小红书上给大家打个招呼，说maclaw bot来了:)"
	cases := []struct {
		name string
		u    DesktopBotUnderstanding
		text string
		want string
	}{
		{"natural told kept", DesktopBotUnderstanding{Told: "好嘞，这就去小红书打招呼。"}, in, "好嘞，这就去小红书打招呼。"},
		{"echo cleared", DesktopBotUnderstanding{Told: in}, in, ""},
		{"empty cleared", DesktopBotUnderstanding{}, in, ""},
		{"echo of card cleared", DesktopBotUnderstanding{Told: "目标：打招呼", Instruction: "目标：打招呼"}, in, ""},
		{"verbatim fallback cleared", DesktopBotUnderstanding{Told: "x", Fallback: "unread"}, in, ""},
		{"stop kept", DesktopBotUnderstanding{Told: "STOP_WORK"}, "停", "STOP_WORK"},
		{"files not named cleared", DesktopBotUnderstanding{Told: "好的，开始写摘要。"}, ackPDF, ""},
		{"files named kept", DesktopBotUnderstanding{Told: "拿《2608.06216-回答.pdf》做素材，这就写。"}, ackPDF, "拿《2608.06216-回答.pdf》做素材，这就写。"},
		{"generic clarification cleared", DesktopBotUnderstanding{Told: "我先确认一下这篇综述要发给谁、发在哪里，以及希望覆盖哪些内容"}, "找一篇最近的热门论文，发到技术社区做图文综述。", ""},
		{"question cleared", DesktopBotUnderstanding{Told: "你希望写成多长？"}, "找一篇最近的热门论文，发到技术社区做图文综述。", ""},
	}
	for _, c := range cases {
		if got := desktopBotAcceptTold(&c.u, c.text, "zh"); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestDesktopBotAcceptOptionsStayStructured(t *testing.T) {
	u := DesktopBotUnderstanding{Told: "先发笔记，想要别的点一下。", Options: []string{" 发一条新笔记 ", "在评论区留言", "发一条新笔记", "", "私信粉丝", "改个人简介"}}
	if got := desktopBotAcceptTold(&u, "去小红书打个招呼", "zh"); got != "先发笔记，想要别的点一下。" {
		t.Fatalf("told %q", got)
	}
	if len(u.Options) != 4 || u.Options[0] != "发一条新笔记" {
		t.Fatalf("options %v", u.Options)
	}
	one := DesktopBotUnderstanding{Options: []string{"only"}}
	desktopBotAcceptTold(&one, "x", "zh")
	if one.Options != nil {
		t.Fatalf("single option kept: %v", one.Options)
	}
	var parsed DesktopBotUnderstanding
	if err := json.Unmarshal([]byte(`{"told":"t","options":["A","B"]}`), &parsed); err != nil || len(parsed.Options) != 2 {
		t.Fatalf("parse %v %v", err, parsed.Options)
	}
}

func TestDesktopBotHasMaterials(t *testing.T) {
	yes := "用这些图发小红书\n\n[素材]\n- ~/素材/小红书助手/a.png\n- ~/素材/小红书助手/b.mp4"
	if !desktopBotHasMaterials(yes) || !desktopBotHasMaterials("[素材]\n- ~/素材/x/a.png") {
		t.Fatal("materials not seen")
	}
	for _, no := range []string{"普通消息", "x[素材]\n- ~/素材/a", "t\n[素材]\n- /etc/passwd", "t\n[素材]\nhello"} {
		if desktopBotHasMaterials(no) {
			t.Fatalf("false positive %q", no)
		}
	}
}

func TestWriteDesktopBotAckRetriesOnFallbackAndRejectsEcho(t *testing.T) {
	prev, prevAttempts, prevModels := desktopBotUnderstandLLM, desktopBotAckAttempts, desktopBotAckModels
	defer func() {
		desktopBotUnderstandLLM, desktopBotAckAttempts, desktopBotAckModels = prev, prevAttempts, prevModels
	}()
	desktopBotAckModels = func(*App) (corelib.MaclawLLMConfig, *corelib.MaclawLLMConfig) { return corelib.MaclawLLMConfig{}, nil }
	var mu sync.Mutex
	var seen []desktopBotUnderstandRequest
	// Every quick attempt parrots the message; only the person's model asked
	// again with its thinking on writes a real line.
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		if req.fallback {
			return "收到《2608.06216-回答.pdf》，拿来写知乎摘要。", nil
		}
		return "附件是文档。", nil
	}
	line, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "accept", Message: ackPDF, Lang: "zh-Hans", Goal: "写知乎摘要", Persona: "码卡龙"})
	if err != nil || line != "收到《2608.06216-回答.pdf》，拿来写知乎摘要。" {
		t.Fatalf("line %q err %v", line, err)
	}
	fallbackSeen, hubSeen := false, false
	for _, req := range seen {
		if req.tag != "desktop-bot-ack" || req.timeoutSec > 15 {
			t.Fatalf("attempt %+v", req)
		}
		fallbackSeen = fallbackSeen || req.fallback
		hubSeen = hubSeen || req.hub
		for _, want := range []string{"files: 2608.06216-回答.pdf", "goal: 写知乎摘要", "persona: 码卡龙", "kind: accept"} {
			if !strings.Contains(req.userPrompt, want) {
				t.Fatalf("prompt lacks %q:\n%s", want, req.userPrompt)
			}
		}
	}
	if !fallbackSeen || !hubSeen {
		t.Fatalf("attempts %+v", seen)
	}
	var tries []desktopBotUnderstandRequest
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		mu.Lock()
		tries = append(tries, req)
		mu.Unlock()
		return "", errors.New("timeout")
	}
	if _, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "supplement", Message: "x"}); err == nil {
		t.Fatal("all-failed ack returned no error")
	}
	if len(tries) < 3 || !tries[len(tries)-1].hub {
		t.Fatalf("hub must be the last ordered attempt: %+v", tries)
	}
}

func TestDesktopBotAckLineCleansAnswers(t *testing.T) {
	req := DesktopBotAckRequest{Message: "做摘要"}
	cases := map[string]string{
		"“好，这就开始。”":              "好，这就开始。",
		"好的\n第二行":                "好的",
		`{"told":"x"}`:           "",
		"做摘要":                    "",
		strings.Repeat("长", 200): "",
	}
	for in, want := range cases {
		if got := desktopBotAckLine(in, req); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}

func TestDesktopBotAckUsesSpecifiedBotModelWithoutFastRace(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		RemoteUserID:             "alice",
		MaclawLLMCurrentProvider: "WorkBuddy",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub", Name: hubServiceProviderName, IsHubService: true, URL: "https://hub.example/api/llm/v1", Key: "viewer", Model: "auto", Protocol: "openai"},
			{ID: "wb", Name: "WorkBuddy", URL: "https://copilot.example/v2", Key: "wb-key", Model: "hy4flash-free", Protocol: "openai"},
			{ID: "zp", Name: "智谱编程", URL: "https://open.bigmodel.cn/api/paas/v4", Key: "zp-key", Model: "glm-5.3-flash", Protocol: "openai"},
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "wb", Model: "hy4flash-free"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	app.rememberDesktopBotLLM("alice", DesktopBotLLM{
		Mode: "custom", Choice: "custom", Stored: true,
		Roles: &DesktopBotLLMRoles{Foreground: DesktopBotLLMRole{Name: "智谱编程", Model: "glm-5.3-flash"}},
	})

	prev, prevModels := desktopBotUnderstandLLM, desktopBotAckModels
	defer func() { desktopBotUnderstandLLM, desktopBotAckModels = prev, prevModels }()
	desktopBotAckModels = func(*App) (corelib.MaclawLLMConfig, *corelib.MaclawLLMConfig) {
		fast := corelib.MaclawLLMConfig{URL: "https://hub.example/api/llm/v1", Key: "viewer", Model: "official-low", Protocol: "openai"}
		return corelib.MaclawLLMConfig{URL: "https://copilot.example/v2", Key: "wb-key", Model: "hy4flash-free", Protocol: "openai"}, &fast
	}
	var mu sync.Mutex
	var seen []desktopBotUnderstandRequest
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		// Give incorrect concurrent racers time to start too; the specified
		// model must be the only call, not merely the fastest one.
		time.Sleep(20 * time.Millisecond)
		return "我先打开页面处理。", nil
	}
	line, err := app.writeDesktopBotAck(context.Background(), DesktopBotAckRequest{Kind: "accept", Message: "打开 example.com", Lang: "zh-Hans"})
	if err != nil || line == "" {
		t.Fatalf("line %q err %v", line, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("specified model must be the only racer, got %+v", seen)
	}
	req := seen[0]
	if req.override == nil || req.override.Model != "glm-5.3-flash" || req.override.ProviderName != "智谱编程" {
		t.Fatalf("override %+v", req.override)
	}
	if req.hub {
		t.Fatalf("specified model must not be replaced by hub: %+v", req)
	}
}
