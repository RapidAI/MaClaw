package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDesktopOutcomePageReadNeedsNoScreenshot(t *testing.T) {
	ev := "desktop: rich_fill: pasted\ndesktop: script ok: url https://zhuanlan.zhihu.com/p/1; title X {\"h1\":\"T\",\"len\":3812}"
	if !desktopOutcomePageRead(ev) {
		t.Fatal("page read not recognised")
	}
	txt, _ := desktopOutcomeUserContent(corelib.MaclawLLMConfig{}, "发到知乎", ev, nil).(string)
	if strings.Contains(txt, "没有当前桌面截图") || !strings.Contains(txt, "不需要截图") {
		t.Fatalf("content: %s", txt)
	}
	if desktopOutcomePageRead("desktop: bash ok\n  script ok: url http://x") {
		t.Fatal("indented line counted")
	}
}
