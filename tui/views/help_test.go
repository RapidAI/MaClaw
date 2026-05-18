package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpPanelScrollsInsideSmallViewport(t *testing.T) {
	help := NewHelpModel("en")
	help.Toggle()
	help.SetViewport(80, 8)

	first := stripANSIForTest(help.ViewWithSize(8, 80))
	if !strings.Contains(first, "scroll 1/") {
		t.Fatalf("small help view should show scroll footer:\n%s", first)
	}

	for i := 0; i < 20; i++ {
		help, _ = help.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	scrolled := stripANSIForTest(help.ViewWithSize(8, 80))
	if first == scrolled {
		t.Fatalf("help view did not change after scrolling")
	}
	if !strings.Contains(scrolled, "scroll ") {
		t.Fatalf("scrolled help view should keep scroll footer:\n%s", scrolled)
	}
}

func TestRootHelpUsesScrollableViewport(t *testing.T) {
	m := NewRootModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	m.Help.Toggle()

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "scroll 1/") {
		t.Fatalf("root help should include viewport scroll footer:\n%s", view)
	}
	if !strings.Contains(view, "F1") || !strings.Contains(view, "F6") {
		t.Fatalf("top tabs should remain visible while help is open:\n%s", view)
	}
}

func TestHelpSetupSpaceMentionsLanguage(t *testing.T) {
	help := NewHelpModel("en")
	view := stripANSIForTest(help.View())
	if !strings.Contains(view, "switch language") {
		t.Fatalf("setup help should mention language switching:\n%s", view)
	}
}

func TestHelpListsSlashNavigationCommands(t *testing.T) {
	help := NewHelpModel("en")
	view := stripANSIForTest(help.View())
	for _, want := range []string{"/loop", "/setup", "/redeem", "/chat", "/tools", "/mcp", "/skill", "/tasks", "/schedule", "/config", "/llm", "/security", "/help"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help should include slash command %s:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "goal-driven verification loop") || !strings.Contains(view, "--max N") || !strings.Contains(view, "--timeout N") || !strings.Contains(view, "--dir path") {
		t.Fatalf("help should explain /loop usage and options:\n%s", view)
	}
	if strings.Contains(view, "/memory") {
		t.Fatalf("simplified help should not advertise memory browsing:\n%s", view)
	}
	if !strings.Contains(view, "/mcp [remote]") || !strings.Contains(view, "template choices when empty") {
		t.Fatalf("help should explain bare /mcp opens template choices:\n%s", view)
	}
	if !strings.Contains(view, "mcp shows the MCP list") {
		t.Fatalf("help should explain /tools mcp shows the MCP list when configured:\n%s", view)
	}
}

func TestHelpLongSlashCommandsKeepDescriptionColumn(t *testing.T) {
	help := NewHelpModel("zh")
	view := stripANSIForTest(help.View())
	found := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "/loop <验证命令> <目标>") {
			found = true
			if !strings.Contains(line, "  运行目标驱动验证循环") || !strings.Contains(line, "--max 轮数") || !strings.Contains(line, "--dir 路径") {
				t.Fatalf("long /loop key should be padded before the description:\n%s", line)
			}
		}
	}
	if !found {
		t.Fatalf("help should include localized /loop usage:\n%s", view)
	}
}

func TestHelpListsToolQuickActions(t *testing.T) {
	help := NewHelpModel("en")
	view := stripANSIForTest(help.View())
	for _, want := range []string{"Tools", "Space", "common Skill preset", "a / A", "MCP"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help should include tool quick action %q:\n%s", want, view)
		}
	}
}

func TestHelpTaskEnterExplainsEmptyStateNavigation(t *testing.T) {
	help := NewHelpModel("en")
	view := stripANSIForTest(help.View())
	if !strings.Contains(view, "on empty lists") || !strings.Contains(view, "next useful page") {
		t.Fatalf("task help should explain Enter on empty task lists:\n%s", view)
	}
}
