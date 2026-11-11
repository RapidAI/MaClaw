package guiapp

import (
	"os"
	"strings"
	"testing"
)

// A steer's line is written by the model as a partner: the running job and
// how the new instruction changes it; the prompt forbids a bare 好的/收到.
func TestDesktopBotAckSupplementPromptConveysChange(t *testing.T) {
	for _, want := range []string{"kind supplement", "job already running", "how this changes", "do not invent requirements", "我按这个改"} {
		if !strings.Contains(desktopBotAckSystem, want) {
			t.Fatalf("supplement prompt lacks %q", want)
		}
	}
	user := desktopBotAckUser(DesktopBotAckRequest{Kind: "supplement", Message: "用公开中文有害文本数据集", RunningTask: "训练有害文本二分类模型", Lang: "zh-Hans"})
	if !strings.Contains(user, "job already running: 训练有害文本二分类模型") {
		t.Fatalf("running job missing: %s", user)
	}
}

// The queued receipt carries no stock sentence.
func TestDesktopBotSupplementReceiptHasNoStockSentence(t *testing.T) {
	src := readGUISourceD97(t, "desktop_bot_hub.go")
	if strings.Contains(src, "好，我按这个改") || strings.Contains(src, "Got it, I'll work that in") {
		t.Fatal("stock steer sentence still in the receipt")
	}
}

func readGUISourceD97(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
