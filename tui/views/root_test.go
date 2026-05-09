package views

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRootDirectTabShortcutWorksInsideConfig(t *testing.T) {
	m := NewRootModel("zh")
	m.SetTab(TabConfig)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF3})
	if updated.ActiveTab() != TabTools {
		t.Fatalf("active tab = %d, want tools", updated.ActiveTab())
	}
}

func TestRootF3FromRedeemOpensMCPTemplateWhenOfficialReady(t *testing.T) {
	m := NewRootModel("en")
	m.SetTab(TabServiceRedeem)
	m.Service.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "auto",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF3})
	if updated.ActiveTab() != TabTools || updated.Tools.ActiveSubTab() != ToolSubMCP {
		t.Fatalf("F3 active tab/sub-tab = %d/%d, want tools/MCP", updated.ActiveTab(), updated.Tools.ActiveSubTab())
	}
	if updated.Tools.MCPAddMode() != "local" {
		t.Fatalf("F3 should open the local MCP template flow, got %q", updated.Tools.MCPAddMode())
	}
}

func TestRootDirectTabShortcutWorksWhileChatInputFocused(t *testing.T) {
	m := NewRootModel("zh")
	m.SetTab(TabChat)
	m.Chat.FocusInput()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	if updated.ActiveTab() != TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", updated.ActiveTab())
	}
}

func TestRootTabsShowFunctionKeyHintsOnWideTerminal(t *testing.T) {
	m := NewRootModel("zh")
	m.width = 100
	tabs := m.renderTabs()
	if !strings.Contains(tabs, "F1") || !strings.Contains(tabs, "F6") {
		t.Fatalf("tab bar should include function-key hints on wide terminals: %q", tabs)
	}
	if !strings.Contains(stripANSIForTest(tabs), "聊天") {
		t.Fatalf("chat tab should use the same user-facing term as /chat: %q", tabs)
	}
}

func TestRootTabsFitNarrowTerminal(t *testing.T) {
	m := NewRootModel("zh")
	m.width = 32
	tabs := m.renderTabs()
	if got := lipgloss.Width(tabs); got > m.width {
		t.Fatalf("tab bar width = %d, want <= %d: %q", got, m.width, tabs)
	}
	if !strings.Contains(tabs, "F1") || !strings.Contains(tabs, "F6") {
		t.Fatalf("compact tab bar should keep direct shortcut hints: %q", tabs)
	}
}

func TestRootPropagatesWindowSizeBeforeTabSwitch(t *testing.T) {
	m := NewRootModel("en")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	updated.SetTab(TabTools)

	view := stripANSIForTest(updated.View())
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 44 {
			t.Fatalf("line width = %d, want <= 44: %q\nview:\n%s", got, line, view)
		}
	}
}

func TestRootAllTabsFitNarrowTerminal(t *testing.T) {
	m := NewRootModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	m.Tasks.SetRemoteTasks([]RemoteTaskItem{{
		ID:        "remote-task-with-a-very-long-id",
		Host:      "very-long-hostname.example.internal",
		Status:    "running",
		StartedAt: "2026-05-05T10:00:00Z",
		Command:   "a very long command with many arguments",
	}})
	m.Tools.SetSkills([]SkillItem{{
		Name:        "very-long-skill-name-that-should-fit",
		Description: "a long skill description that should not overflow narrow terminals",
		Status:      "active",
		Source:      "local",
		Publisher:   "tester",
	}})
	m.Chat.messages = []ChatMessage{
		{Role: "system", Content: "ready"},
		{Role: "assistant", Content: "| Name | Value |\n| --- | --- |\n| very-long-key | very-long-value |"},
	}
	m.Chat.invalidateCache()

	for tab := 0; tab < TabCount; tab++ {
		m.SetTab(tab)
		view := stripANSIForTest(m.View())
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > 44 {
				t.Fatalf("tab %d line width = %d, want <= 44: %q\nview:\n%s", tab, got, line, view)
			}
		}
	}
}

func TestRootAllTabsDoNotPanicOnTinyTerminal(t *testing.T) {
	m := NewRootModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 2, Height: 6})

	for tab := 0; tab < TabCount; tab++ {
		m.SetTab(tab)
		_ = m.View()
	}
}

func TestRootAllTabsFitTinyTerminal(t *testing.T) {
	m := NewRootModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 8, Height: 8})

	for tab := 0; tab < TabCount; tab++ {
		m.SetTab(tab)
		view := stripANSIForTest(m.View())
		for _, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > 8 {
				t.Fatalf("tab %d tiny line width = %d, want <= 8: %q\nview:\n%s", tab, got, line, view)
			}
		}
	}
}

func TestRootShortTerminalKeepsTabsAndStatusVisible(t *testing.T) {
	m := NewRootModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 10})
	m.Tasks.SetRemoteTasks([]RemoteTaskItem{{
		ID:      "remote-task-with-a-long-id",
		Host:    "host.example",
		Status:  "running",
		Command: "run a long command",
	}})

	for _, tab := range []int{TabOnboarding, TabTools, TabTasks, TabServiceRedeem, TabConfig} {
		m.SetTab(tab)
		view := stripANSIForTest(m.View())
		lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("tab %d short view should include tab bar and status, got:\n%s", tab, view)
		}
		if !strings.Contains(lines[0], "F1") || !strings.Contains(lines[0], "F6") {
			t.Fatalf("tab %d should keep top tab shortcuts visible:\n%s", tab, view)
		}
		if !strings.Contains(lines[len(lines)-1], "?:help") {
			t.Fatalf("tab %d should keep status/help visible at bottom:\n%s", tab, view)
		}
		for _, line := range lines {
			if got := lipgloss.Width(line); got > 46 {
				t.Fatalf("tab %d short line width = %d, want <= 46: %q\nview:\n%s", tab, got, line, view)
			}
		}
	}
}

func TestRootHelpFitsNarrowTerminal(t *testing.T) {
	m := NewRootModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	m.Help.Toggle()

	view := stripANSIForTest(m.View())
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 44 {
			t.Fatalf("help line width = %d, want <= 44: %q\nview:\n%s", got, line, view)
		}
	}
}

func TestRootQuestionMarkOpensHelpOnOnboardingActionRows(t *testing.T) {
	m := NewRootModel("en")
	m.SetTab(TabOnboarding)
	m.Onboarding.cursor = onboardingRowLanguage
	m.Onboarding.focusCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !updated.Help.IsVisible() {
		t.Fatal("question mark should open help on non-editing onboarding rows")
	}
}

func TestRootQuestionMarkDoesNotStealOnboardingEmailInput(t *testing.T) {
	m := NewRootModel("en")
	m.SetTab(TabOnboarding)
	m.Onboarding.cursor = onboardingRowEmail
	m.Onboarding.focusCursor()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if updated.Help.IsVisible() {
		t.Fatal("question mark should stay in the email input while it is focused")
	}
	if got := updated.Onboarding.emailInput.Value(); got != "?" {
		t.Fatalf("email input = %q, want typed question mark", got)
	}
}

func TestRootQuestionMarkOpensHelpOnConfigWhenNotEditing(t *testing.T) {
	m := NewRootModel("en")
	m.SetTab(TabConfig)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !updated.Help.IsVisible() {
		t.Fatal("question mark should open help when config is not editing")
	}
}

func TestRootTabNavigationWorksOnServiceRedeemInput(t *testing.T) {
	m := NewRootModel("zh")
	m.SetTab(TabServiceRedeem)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if updated.ActiveTab() != TabConfig {
		t.Fatalf("active tab = %d, want config", updated.ActiveTab())
	}
}

func TestHelpConfigEnterMatchesChooserUX(t *testing.T) {
	help := NewHelpModel("en")
	help.Toggle()
	view := stripANSIForTest(help.View())
	if !strings.Contains(view, "select from choices") {
		t.Fatalf("help should describe config Enter as selection-aware:\n%s", view)
	}
}

func TestHelpRedeemMentionsMCPNextStep(t *testing.T) {
	help := NewHelpModel("en")
	help.Toggle()
	view := stripANSIForTest(help.View())
	if !strings.Contains(view, "after official service is ready, open MCP templates") {
		t.Fatalf("help should connect service redeem to MCP templates:\n%s", view)
	}
}

func TestStatusBarMentionsDirectTabShortcuts(t *testing.T) {
	bar := NewStatusBarModel("en").View(120)
	if !strings.Contains(bar, "F1-F6") || !strings.Contains(bar, "Ctrl+Tab") || !strings.Contains(bar, "?:help") {
		t.Fatalf("status bar should advertise direct tab/help shortcuts: %s", bar)
	}
}

func TestRootHandlesTaskEmptyStateNavigationMessages(t *testing.T) {
	m := NewRootModel("en")
	m.SetTab(TabTasks)

	updated, _ := m.Update(TaskOpenToolsMsg{})
	if updated.ActiveTab() != TabTools {
		t.Fatalf("TaskOpenToolsMsg active tab = %d, want tools", updated.ActiveTab())
	}
	if updated.Tools.ActiveSubTab() != ToolSubMCP {
		t.Fatalf("TaskOpenToolsMsg tools sub-tab = %d, want MCP", updated.Tools.ActiveSubTab())
	}

	updated.SetTab(TabTasks)
	updated, _ = updated.Update(TaskOpenChatMsg{})
	if updated.ActiveTab() != TabChat {
		t.Fatalf("TaskOpenChatMsg active tab = %d, want chat", updated.ActiveTab())
	}
}
