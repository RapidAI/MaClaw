package guiapp

import (
	"strings"
	"testing"
)

const ackPDF = "附件是文档。\n\n[素材]\n- ~/素材/Bot 2/2608.06216-回答.pdf"

func TestDesktopBotMaterialNames(t *testing.T) {
	names := desktopBotMaterialNames(ackPDF)
	if len(names) != 1 || names[0] != "2608.06216-回答.pdf" {
		t.Fatalf("names %v", names)
	}
	if desktopBotWordsBeforeMaterials(ackPDF) != "附件是文档。" || desktopBotMaterialNames("普通消息") != nil {
		t.Fatal("split")
	}
	many := []string{"a.png", "b.png", "c.png", "d.png"}
	if got := desktopBotFileList(many, false); got != "《a.png》、《b.png》等 4 个文件" {
		t.Fatalf("list %q", got)
	}
}

func TestDesktopBotMaterialAcceptTold(t *testing.T) {
	names := []string{"2608.06216-回答.pdf"}
	u := &DesktopBotUnderstanding{Goal: "知乎摘要"}
	if got := desktopBotMaterialAcceptTold(u, names, false, "好的，我这就去知乎摘要，有进展告诉你。"); got != "收到《2608.06216-回答.pdf》，我拿它当知乎摘要的素材，这就开始。" {
		t.Fatalf("goal %q", got)
	}
	if got := desktopBotMaterialAcceptTold(&DesktopBotUnderstanding{}, names, false, "好的"); got != "收到《2608.06216-回答.pdf》，这就用它开始。" {
		t.Fatalf("no goal %q", got)
	}
	keep := "收到《2608.06216-回答.pdf》，我先读一遍再写摘要。"
	if got := desktopBotMaterialAcceptTold(u, names, false, keep); got != keep {
		t.Fatalf("model reply naming the file was replaced: %q", got)
	}
	if got := desktopBotMaterialAcceptTold(&DesktopBotUnderstanding{Fallback: "unread"}, names, false, ""); !strings.HasPrefix(got, "收到《2608.06216-回答.pdf》，我先照你的原话") {
		t.Fatalf("fallback %q", got)
	}
	opt := &DesktopBotUnderstanding{Options: []string{"A", "B"}}
	if got := desktopBotMaterialAcceptTold(opt, names, false, "好的，我先按「A」去做。"); got != "收到《2608.06216-回答.pdf》。好的，我先按「A」去做。" {
		t.Fatalf("options %q", got)
	}
	if got := desktopBotMaterialAcceptTold(&DesktopBotUnderstanding{Goal: "summarize it"}, []string{"a.pdf"}, true, "ok"); got != "Got \"a.pdf\". I'll use it for summarize it and start now." {
		t.Fatalf("en %q", got)
	}
}

func TestDesktopBotMaterialKeepAndSupplement(t *testing.T) {
	names := []string{"封面.png"}
	if got := desktopBotMaterialKeepTold("好的", names, false); got != "收到《封面.png》，先放在桌面的 ~/素材 里，要用的时候说一声。" {
		t.Fatalf("keep %q", got)
	}
	if got := desktopBotMaterialSupplementAck(names, "做今天的dailypapers摘要，发到知乎上。", false); got != "收到附件《封面.png》，接下来会用它来做今天的dailypapers摘要，发到知乎上。" {
		t.Fatalf("supplement %q", got)
	}
	if got := desktopBotMaterialSupplementAck(names, strings.Repeat("很长的任务", 10), false); got != "收到附件《封面.png》，我会用在手上这件事里。" {
		t.Fatalf("long task %q", got)
	}
	if got := desktopBotMaterialSupplementAck(names, "目标：整理论文摘要\n约束：…", false); got != "收到附件《封面.png》，接下来会用它来整理论文摘要。" {
		t.Fatalf("card %q", got)
	}
}
