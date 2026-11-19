package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDesktopDeliverablesParse(t *testing.T) {
	items, err := ParseDesktopDeliverables("```json\n{\"items\":[\"贪吃蛇运行时的截图，发到聊天\",\" \",\"贪吃蛇运行时的截图，发到聊天\",\"游戏代码文件\"]}\n```")
	if err != nil || len(items) != 2 || items[0] != "贪吃蛇运行时的截图，发到聊天" {
		t.Fatalf("%v %v", items, err)
	}
	if items, err := ParseDesktopDeliverables(`{"items":[]}`); err != nil || len(items) != 0 {
		t.Fatalf("empty: %v %v", items, err)
	}
	if _, err := ParseDesktopDeliverables("no"); err == nil {
		t.Fatal("not json accepted")
	}
	typed, err := ParseDesktopDeliverableContract(`{"items":[{"id":"d1","kind":"file","text":"报告"},{"kind":"post","text":"帖子"},{"id":"d1","kind":"text","text":"重复"}]}`)
	if err == nil {
		t.Fatal("repeated id accepted")
	}
	typed, err = ParseDesktopDeliverableContract(`{"items":[{"id":"report","kind":"file","text":"报告"},{"kind":"post","text":"帖子"}]}`)
	if err != nil || len(typed) != 2 || typed[0].ID != "report" || typed[0].Kind != "file" || typed[1].ID != "d2" || typed[1].Kind != "post" {
		t.Fatalf("typed = %#v %v", typed, err)
	}
	if _, err := ParseDesktopDeliverableContract(`{"items":[{"id":"d1","kind":"widget","text":"x"}]}`); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestDesktopDeliverablesInJudgeAndNote(t *testing.T) {
	items := []string{"运行截图，发到聊天", "知乎帖子链接"}
	content := desktopOutcomeUserContentChecklist(corelib.MaclawLLMConfig{}, "做个游戏，运行后截屏发我", items, "", "desktop: Delivered shot.png (1 bytes)", nil)
	text, _ := content.(string)
	for _, want := range []string{"交付清单", "1. 运行截图，发到聊天", "2. 知乎帖子链接", "不要再要求当前截图"} {
		if !strings.Contains(text, want) {
			t.Fatalf("judge content misses %q:\n%s", want, text)
		}
	}
	plain, _ := desktopOutcomeUserContentWithReply(corelib.MaclawLLMConfig{}, "x", "", "desktop: ok", nil).(string)
	if strings.Contains(plain, "交付清单") {
		t.Fatal("checklist shown without items")
	}
	if note := DesktopDeliverablesNote(items); !strings.Contains(note, "2. 知乎帖子链接") || !strings.Contains(note, "deliver") {
		t.Fatalf("note %q", note)
	}
	if DesktopDeliverablesNote(nil) != "" {
		t.Fatal("empty note")
	}
}
