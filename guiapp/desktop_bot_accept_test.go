package guiapp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDesktopBotAcceptTold(t *testing.T) {
	in := "去小红书上给大家打个招呼，说maclaw bot来了:)"
	cases := []struct {
		name string
		u    DesktopBotUnderstanding
		want string
	}{
		{"clear goal, echo told", DesktopBotUnderstanding{Told: in, Goal: "去小红书打个招呼"}, "好的，我这就去小红书打个招呼，有进展告诉你。"},
		{"clear goal, empty told", DesktopBotUnderstanding{Goal: "小红书打个招呼"}, "好的，我这就去小红书打个招呼，有进展告诉你。"},
		{"unclear", DesktopBotUnderstanding{Told: in, Unclear: "发笔记还是评论", Assumption: "发一条新笔记"}, "好的，我先按「发一条新笔记」去做；发笔记还是评论这点我不太确定，不对的话随时告诉我。"},
		{"model failed", DesktopBotUnderstanding{Instruction: in, Fallback: "unreachable"}, "好的，我先照你的原话去试试，哪里理解偏了随时叫停我。"},
		{"natural told kept", DesktopBotUnderstanding{Told: "好的，这就去小红书打个招呼。", Goal: "x"}, "好的，这就去小红书打个招呼。"},
		{"nothing", DesktopBotUnderstanding{Told: in}, "好的，我这就开始弄，弄完告诉你。"},
	}
	for _, c := range cases {
		got := desktopBotAcceptTold(&c.u, in, "zh")
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
		if strings.Contains(got, "maclaw bot来了") {
			t.Errorf("%s: echoed raw text", c.name)
		}
	}
	if got := desktopBotAcceptTold(&DesktopBotUnderstanding{Fallback: "unread"}, "post hi", "en"); !strings.HasPrefix(got, "OK, I'll try") {
		t.Errorf("en fallback: %q", got)
	}
}

func TestDesktopBotAcceptOptions(t *testing.T) {
	in := "去小红书上给大家打个招呼"
	u := DesktopBotUnderstanding{Told: in, Options: []string{" 发一条新笔记 ", "在评论区留言", "发一条新笔记", "", "私信粉丝", "改个人简介"}, Unclear: "x", Assumption: "y"}
	got := desktopBotAcceptTold(&u, in, "zh")
	want := "好的，我先按「发一条新笔记」去做。你要的是不是：发一条新笔记 / 在评论区留言 / 私信粉丝 / 改个人简介？点一下，或选「其它」写给我，我按你说的改。"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if len(u.Options) != 4 || u.Options[0] != "发一条新笔记" {
		t.Fatalf("options %v", u.Options)
	}
	one := DesktopBotUnderstanding{Goal: "小红书打个招呼", Options: []string{"only"}}
	if got := desktopBotAcceptTold(&one, in, "zh"); got != "好的，我这就去小红书打个招呼，有进展告诉你。" || one.Options != nil {
		t.Fatalf("single option should be dropped: %q %v", got, one.Options)
	}
	var parsed DesktopBotUnderstanding
	if err := json.Unmarshal([]byte(`{"told":"t","options":["A","B"]}`), &parsed); err != nil || len(parsed.Options) != 2 {
		t.Fatalf("parse %v %v", err, parsed.Options)
	}
}
