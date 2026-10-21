package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
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
	prev, prevAttempts := desktopBotUnderstandLLM, desktopBotAckAttempts
	defer func() { desktopBotUnderstandLLM, desktopBotAckAttempts = prev, prevAttempts }()
	var seen []desktopBotUnderstandRequest
	answers := []string{"附件是文档。", "收到《2608.06216-回答.pdf》，拿来写知乎摘要。"}
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		seen = append(seen, req)
		out := answers[0]
		answers = answers[1:]
		return out, nil
	}
	line, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "accept", Message: ackPDF, Lang: "zh-Hans", Goal: "写知乎摘要", Persona: "码卡龙"})
	if err != nil || line != "收到《2608.06216-回答.pdf》，拿来写知乎摘要。" {
		t.Fatalf("line %q err %v", line, err)
	}
	if len(seen) != 2 || seen[0].fallback || seen[0].hub || !seen[1].fallback || seen[1].hub || seen[0].tag != "desktop-bot-ack" || seen[0].timeoutSec > 10 || seen[1].timeoutSec > 12 {
		t.Fatalf("attempts %+v", seen)
	}
	for _, want := range []string{"files: 2608.06216-回答.pdf", "goal: 写知乎摘要", "persona: 码卡龙", "kind: accept"} {
		if !strings.Contains(seen[0].userPrompt, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, seen[0].userPrompt)
		}
	}
	var tries []desktopBotUnderstandRequest
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		tries = append(tries, req)
		return "", errors.New("timeout")
	}
	if _, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "supplement", Message: "x"}); err == nil {
		t.Fatal("all-failed ack returned no error")
	}
	if len(tries) != 3 || !tries[2].hub {
		t.Fatalf("hub must be the last attempt only: %+v", tries)
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
