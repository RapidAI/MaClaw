package views

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestChatWelcomeFitsNarrowTerminal(t *testing.T) {
	m := NewChatModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "MaClaw") {
		t.Fatalf("compact welcome should keep product name:\n%s", view)
	}
	assertViewFitsWidth(t, view, 44)
}

func TestChatMessagesFitNarrowTerminal(t *testing.T) {
	m := NewChatModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	m.messages = []ChatMessage{
		{Role: "system", Content: "ready"},
		{Role: "user", Content: "please summarize this very long command output with a very-long-unbroken-token-that-must-not-overflow"},
		{Role: "assistant", Content: "| File | Status | Details |\n| --- | --- | --- |\n| very-long-file-name-without-breaks.go | changed | very-long-detail-without-breaks-and-more-text |"},
	}
	m.invalidateCache()

	assertViewFitsWidth(t, stripANSIForTest(m.View()), 44)
}

func TestChatSetLangRelocalizesSystemMessages(t *testing.T) {
	m := NewChatModel("zh")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m.messages = append(m.messages, ChatMessage{Role: "user", Content: "hi"})
	_ = m.getLines()

	m.SetLang("en")
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "AI assistant is ready") {
		t.Fatalf("chat system message should switch to English:\n%s", view)
	}
	if strings.Contains(view, i18n.T(i18n.MsgTUIChatSystemReady, "zh")) {
		t.Fatalf("chat system message should not keep Chinese after language switch:\n%s", view)
	}
}

func TestRenderMarkdownFitsSmallWidth(t *testing.T) {
	lines := RenderMarkdown("| File | Status | Details |\n| --- | --- | --- |\n| very-long-file-name-without-breaks.go | changed | very-long-detail-without-breaks |", 16)
	for _, line := range lines {
		if got := lipgloss.Width(stripANSIForTest(line)); got > 16 {
			t.Fatalf("markdown line width = %d, want <= 16: %q", got, stripANSIForTest(line))
		}
	}
}

func assertViewFitsWidth(t *testing.T, view string, width int) {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line width = %d, want <= %d: %q\nview:\n%s", got, width, line, view)
		}
	}
}
