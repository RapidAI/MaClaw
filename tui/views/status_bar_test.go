package views

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/charmbracelet/lipgloss"
)

func TestStatusBarFitsTerminalWidth(t *testing.T) {
	bar := NewStatusBarModel("en")
	bar.SetModelInfo(strings.Repeat("very-long-model-name-", 4))
	bar.SetMessage(strings.Repeat("activate Hub and redeem MaClaw official service ", 3))

	for _, width := range []int{12, 24, 40, 60, 120} {
		rendered := bar.View(width)
		plain := stripANSIForTest(rendered)
		if got := lipgloss.Width(plain); got > width {
			t.Fatalf("status bar width = %d, want <= %d: %q", got, width, plain)
		}
	}
}

func TestStatusBarKeepsHelpHintOnNarrowTerminal(t *testing.T) {
	bar := NewStatusBarModel("en")
	bar.SetMessage("Ready")

	plain := stripANSIForTest(bar.View(24))
	if !strings.Contains(plain, "?:help") {
		t.Fatalf("narrow status bar should keep help hint: %q", plain)
	}
}

func TestStatusBarKeepsTruncatedLongMessage(t *testing.T) {
	bar := NewStatusBarModel("en")
	bar.SetMessage("Opened Tools/MCP templates. Choose a local template with Left/Right, Enter opens it, or A opens remote templates.")

	plain := stripANSIForTest(bar.View(120))
	if !strings.Contains(plain, "Opened Tools/MCP templates") || !strings.Contains(plain, "Left/Right") {
		t.Fatalf("wide status bar should keep the useful message, got: %q", plain)
	}
	if !strings.Contains(plain, "?:help") {
		t.Fatalf("wide status bar should still keep help hint, got: %q", plain)
	}
	if got := lipgloss.Width(plain); got > 120 {
		t.Fatalf("status bar width = %d, want <= 120: %q", got, plain)
	}
}

func TestStatusBarUsesLocalizedTinyHelp(t *testing.T) {
	bar := NewStatusBarModel("zh")
	bar.SetMessage(strings.Repeat("状态消息", 8))

	plain := stripANSIForTest(bar.View(20))
	if !strings.Contains(plain, "?:帮助") {
		t.Fatalf("Chinese narrow status bar should keep localized help hint: %q", plain)
	}
}

func TestStatusBarSetLangRelocalizesReadyMessage(t *testing.T) {
	bar := NewStatusBarModel("zh")
	bar.SetLang("en")

	plain := stripANSIForTest(bar.View(80))
	if !strings.Contains(plain, i18n.T(i18n.MsgTUIReady, "en")) {
		t.Fatalf("ready message should switch to English: %q", plain)
	}
	if strings.Contains(plain, i18n.T(i18n.MsgTUIReady, "zh")) {
		t.Fatalf("ready message should not keep Chinese after language switch: %q", plain)
	}
}

func TestStatusBarSetLangRelocalizesOpenPageMessage(t *testing.T) {
	bar := NewStatusBarModel("zh")
	bar.SetMessage(statusBarText("zh", "slashOpenTools"))

	bar.SetLang("en")

	plain := stripANSIForTest(bar.View(120))
	if !strings.Contains(plain, statusBarText("en", "slashOpenTools")) {
		t.Fatalf("open-page message should switch to English: %q", plain)
	}
	if strings.Contains(plain, statusBarText("zh", "slashOpenTools")) {
		t.Fatalf("open-page message should not keep Chinese after language switch: %q", plain)
	}
}

func TestStatusBarSetLangRelocalizesTemplateMessages(t *testing.T) {
	bar := NewStatusBarModel("zh")
	bar.SetMessage(statusBarFormat("zh", "configSaveFailed", "语言", "磁盘满了"))

	bar.SetLang("en")

	plain := stripANSIForTest(bar.View(120))
	want := statusBarFormat("en", "configSaveFailed", "语言", "磁盘满了")
	if !strings.Contains(plain, want) {
		t.Fatalf("template message should switch to English: %q", plain)
	}
	if strings.Contains(plain, statusBarFormat("zh", "configSaveFailed", "语言", "磁盘满了")) {
		t.Fatalf("template message should not keep Chinese after language switch: %q", plain)
	}
}
