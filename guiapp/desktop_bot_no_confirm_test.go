package guiapp

import "testing"

func TestDesktopBotNoBareConfirm(t *testing.T) {
	cases := []struct {
		name, text, told, order string
	}{
		{"weather lookup", "今天天气怎样？", "我现在去查一下你所在城市的今天天气，确认后按这个安排执行。", "查询所在城市今天天气并告诉用户"},
		{"long research", "帮我调研三家竞品写一份报告", "我会先查资料再写报告。", "调研竞品并写报告"},
		{"publish", "把这篇文章发布到知乎", "请确认：我会把文章发布到知乎。", "发布文章到知乎"},
	}
	for _, c := range cases {
		u := &DesktopBotUnderstanding{Pace: "confirm", Told: c.told, Instruction: c.order}
		desktopBotNoBareConfirm(u, "plan")
		if u.Pace != "direct" {
			t.Fatalf("%s: pace = %q, want direct", c.name, u.Pace)
		}
		if desktopBotConfirmAsk.MatchString(u.Told) {
			t.Fatalf("%s: told still asks to proceed: %q", c.name, u.Told)
		}
	}
	keep := &DesktopBotUnderstanding{Pace: "direct", Told: "我现在去查。", Instruction: "查天气"}
	desktopBotNoBareConfirm(keep, "plan")
	if keep.Told != "我现在去查。" {
		t.Fatalf("plain told changed: %q", keep.Told)
	}
	ask := &DesktopBotUnderstanding{Pace: "ask", Told: "发到哪个平台？", Options: []string{"知乎", "小红书"}}
	desktopBotNoBareConfirm(ask, "plan")
	if ask.Pace != "ask" {
		t.Fatalf("a real question must stay ask, got %q", ask.Pace)
	}
	if got := desktopBotPace("", "plan"); got != "direct" {
		t.Fatalf("missing pace = %q, want direct", got)
	}
}
