package views

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestToolStatus_SubTabSwitch_Key1(t *testing.T) {
	m := NewToolStatusModel("zh")
	if m.subTab != ToolSubSkill {
		t.Fatalf("expected initial subTab=ToolSubSkill, got %d", m.subTab)
	}

	// Switch to MCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'2'}}))
	if m.subTab != ToolSubMCP {
		t.Fatalf("expected subTab=ToolSubMCP after pressing '2', got %d", m.subTab)
	}

	// Switch back to Skill with '1'
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'1'}}))
	if m.subTab != ToolSubSkill {
		t.Fatalf("expected subTab=ToolSubSkill after pressing '1', got %d", m.subTab)
	}
}

func TestToolStatus_SkillSearch_Focus(t *testing.T) {
	m := NewToolStatusModel("zh")

	// Press 's' to focus search
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'s'}}))
	if !m.skillSearch.Focused() {
		t.Fatal("expected skillSearch to be focused after pressing 's'")
	}

	// Type a query
	for _, r := range "pdf" {
		m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{r}}))
	}
	if m.skillSearch.Value() != "pdf" {
		t.Fatalf("expected search value 'pdf', got %q", m.skillSearch.Value())
	}

	// Press Enter to trigger search
	m, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("expected non-nil cmd after pressing Enter with query")
	}
	if m.skillSearch.Focused() {
		t.Fatal("expected skillSearch to be blurred after Enter")
	}
	if !m.skillSearching {
		t.Fatal("expected skillSearching=true after Enter")
	}

	// Execute the cmd to get the message
	msg := cmd()
	searchMsg, ok := msg.(ToolSkillSearchMsg)
	if !ok {
		t.Fatalf("expected ToolSkillSearchMsg, got %T", msg)
	}
	if searchMsg.Query != "pdf" {
		t.Fatalf("expected query 'pdf', got %q", searchMsg.Query)
	}
}

func TestToolStatus_SkillSearch_Esc(t *testing.T) {
	m := NewToolStatusModel("zh")

	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'s'}}))
	if !m.skillSearch.Focused() {
		t.Fatal("expected focused")
	}

	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}))
	if m.skillSearch.Focused() {
		t.Fatal("expected blurred after Esc")
	}
}

func TestToolStatus_SubTabSwitch_BlockedDuringSearch(t *testing.T) {
	m := NewToolStatusModel("zh")

	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'s'}}))

	// '2' should type into search, not switch sub-tab
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'2'}}))
	if m.subTab != ToolSubSkill {
		t.Fatal("sub-tab should not switch while search is focused")
	}
	if m.skillSearch.Value() != "2" {
		t.Fatalf("expected '2' typed into search, got %q", m.skillSearch.Value())
	}
}

// TestRootModel_ToolsTab_Key1_RoutesToSubTab verifies that pressing '1'
// on the Tools tab reaches ToolStatusModel for sub-tab switching.
func TestRootModel_ToolsTab_Key1_RoutesToSubTab(t *testing.T) {
	root := NewRootModel("zh")
	root.SetTab(TabTools)
	root.Tools.subTab = ToolSubMCP

	root, _ = root.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'1'}}))
	if root.tab != TabTools {
		t.Fatalf("expected to stay on TabTools, got %d", root.tab)
	}
	if root.Tools.subTab != ToolSubSkill {
		t.Fatalf("expected sub-tab=ToolSubSkill, got %d", root.Tools.subTab)
	}
}

// TestRootModel_TasksTab_Key2_RoutesToSubTab verifies that pressing '2'
// on the Tasks tab reaches TaskModel for sub-tab switching.
func TestRootModel_TasksTab_Key2_RoutesToSubTab(t *testing.T) {
	root := NewRootModel("zh")
	root.SetTab(TabTasks)

	root, _ = root.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'2'}}))
	if root.tab != TabTasks {
		t.Fatalf("expected to stay on TabTasks, got %d", root.tab)
	}
}

// TestRootModel_ToolsTab_SearchFlow verifies the full search flow through RootModel.
func TestRootModel_ToolsTab_SearchFlow(t *testing.T) {
	root := NewRootModel("zh")
	root.SetTab(TabTools)

	// Press 's' to focus search
	root, _ = root.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'s'}}))
	if !root.Tools.skillSearch.Focused() {
		t.Fatal("expected skillSearch focused after 's'")
	}
	if !root.Tools.IsEditing() {
		t.Fatal("expected IsEditing()=true when search is focused")
	}

	// Type "pdf"
	for _, r := range "pdf" {
		root, _ = root.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{r}}))
	}
	if root.Tools.skillSearch.Value() != "pdf" {
		t.Fatalf("expected search value 'pdf', got %q", root.Tools.skillSearch.Value())
	}

	// Press Enter to trigger search
	root, cmd := root.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("expected non-nil cmd after Enter")
	}
	if root.Tools.skillSearch.Focused() {
		t.Fatal("expected search blurred after Enter")
	}
	if !root.Tools.skillSearching {
		t.Fatal("expected skillSearching=true")
	}
}

// TestRootModel_NumericKeys_PassThroughOnAllTabs verifies that numeric keys
// always reach the active tab (root never intercepts them).
// Tab labels show "1:Chat", "2:Tools" etc. as visual hints, but the root
// does not handle numeric key switching — keys pass through to the active view.
func TestRootModel_NumericKeys_PassThroughOnAllTabs(t *testing.T) {
	tabs := []struct {
		name string
		tab  int
	}{
		{"Chat (blurred)", TabChat},
		{"Tools", TabTools},
		{"Tasks", TabTasks},
		{"Memory", TabMemory},
		{"Audit", TabAudit},
		{"Config", TabConfig},
	}
	for _, tc := range tabs {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRootModel("zh")
			root.Chat.input.Blur() // ensure chat doesn't eat the key
			root.SetTab(tc.tab)

			// Press '3' — should NOT change main tab
			root, _ = root.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'3'}}))
			if root.tab != tc.tab {
				t.Fatalf("expected to stay on tab %d, got %d", tc.tab, root.tab)
			}
		})
	}
}
