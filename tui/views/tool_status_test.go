package views

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestToolStatusFocusMCP(t *testing.T) {
	m := NewToolStatusModel("en")
	m.FocusMCP()
	if m.ActiveSubTab() != ToolSubMCP {
		t.Fatalf("FocusMCP subTab = %d, want MCP", m.ActiveSubTab())
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "No MCP servers") {
		t.Fatalf("FocusMCP should render the MCP sub-tab:\n%s", view)
	}
}

func TestToolStatusFocusTabAndSkill(t *testing.T) {
	m := NewToolStatusModel("en")
	m.FocusTab(ToolSubMCP)
	if m.ActiveSubTab() != ToolSubMCP {
		t.Fatalf("FocusTab subTab = %d, want MCP", m.ActiveSubTab())
	}
	m.FocusSkill()
	if m.ActiveSubTab() != ToolSubSkill {
		t.Fatalf("FocusSkill subTab = %d, want Skill", m.ActiveSubTab())
	}
	m.FocusTab(ToolSubCount + 1)
	if m.ActiveSubTab() != ToolSubSkill {
		t.Fatalf("invalid FocusTab should keep current subTab, got %d", m.ActiveSubTab())
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

func TestToolStatusIgnoresStaleSkillSearchResult(t *testing.T) {
	m := NewToolStatusModel("en")
	m.beginSkillSearch("Searching first")
	firstRequestID := m.skillSearchRequest
	m.beginSkillSearch("Searching second")
	secondRequestID := m.skillSearchRequest

	m, _ = m.Update(ToolSkillSearchResultMsg{RequestID: firstRequestID, Results: []SkillSearchResult{{Name: "Old result"}}})
	if len(m.skillResults) != 0 || !m.skillSearching {
		t.Fatalf("stale result changed active search: results=%#v searching=%v", m.skillResults, m.skillSearching)
	}

	m, _ = m.Update(ToolSkillSearchResultMsg{RequestID: secondRequestID, Results: []SkillSearchResult{{Name: "Current result"}}})
	if m.skillSearching || len(m.skillResults) != 1 || m.skillResults[0].Name != "Current result" {
		t.Fatalf("current result was not applied: results=%#v searching=%v", m.skillResults, m.skillSearching)
	}
}

func TestToolStatusEscDismissesInFlightSkillSearch(t *testing.T) {
	m := NewToolStatusModel("en")
	m.beginSkillSearch("Searching")
	requestID := m.skillSearchRequest

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.skillSearching || len(m.skillResults) != 0 || m.skillMessage != "" {
		t.Fatalf("Esc did not dismiss search: searching=%v results=%#v message=%q", m.skillSearching, m.skillResults, m.skillMessage)
	}

	m, _ = m.Update(ToolSkillSearchResultMsg{RequestID: requestID, Results: []SkillSearchResult{{Name: "Late result"}}})
	if len(m.skillResults) != 0 {
		t.Fatalf("late response restored dismissed search: %#v", m.skillResults)
	}
}

func TestToolStatusInstalledSearchResultDoesNotDispatchInstall(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(ToolSkillSearchResultMsg{Results: []SkillSearchResult{{
		ID: "hub-weather", Name: "Weather", Source: "skillhub", Installed: true, InstalledName: "Weather",
	}}})

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("installed result should not dispatch install: %v", cmd)
	}
	if m.skillConfirming {
		t.Fatal("installed result should not open install confirmation")
	}
	if !strings.Contains(m.skillMessage, "Installed") {
		t.Fatalf("skill message = %q, want installed state", m.skillMessage)
	}
}

func TestToolStatusGitHubResultWithoutInstallRefDoesNotConfirmOrDispatch(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(ToolSkillSearchResultMsg{Results: []SkillSearchResult{{
		ID: "acme/weather", Name: "GitHub Weather", Source: "github",
	}}})

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.skillConfirming {
		t.Fatalf("incomplete GitHub result should not confirm or dispatch: cmd=%v confirming=%v", cmd, m.skillConfirming)
	}
	if !strings.Contains(m.skillMessage, "metadata is missing") {
		t.Fatalf("skill message = %q", m.skillMessage)
	}
}

func TestToolStatusSuccessfulInstallMarksSelectedSearchResultInstalled(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(ToolSkillSearchResultMsg{Results: []SkillSearchResult{{
		ID: "weather", Name: "Weather", Source: "clawhub",
	}}})
	m, _ = m.Update(ToolOperationResultMsg{Tab: ToolSubSkill, Success: true, Message: "Installed", InstalledName: "Weather"})

	if !m.skillResults[0].Installed || m.skillResults[0].InstalledName != "Weather" {
		t.Fatalf("installed search result = %#v", m.skillResults[0])
	}
}

func TestToolStatusSuccessfulInstallMarksOriginalResultWhenCursorMoved(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(ToolSkillSearchResultMsg{Results: []SkillSearchResult{
		{ID: "weather", Name: "Weather", Source: "clawhub"},
		{ID: "calendar", Name: "Calendar", Source: "clawhub"},
	}})
	key := m.skillSearchResultKey(m.skillResults[0])
	m.skillResultCursor = 1
	m, _ = m.Update(ToolOperationResultMsg{Tab: ToolSubSkill, Success: true, Message: "Installed", InstalledName: "Weather", InstalledSearchResult: key})

	if !m.skillResults[0].Installed || m.skillResults[1].Installed {
		t.Fatalf("results were marked incorrectly: %#v", m.skillResults)
	}
}

func TestToolStatusDoesNotDispatchAnotherInstallWhileOneIsInFlight(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(ToolSkillSearchResultMsg{Results: []SkillSearchResult{{
		ID: "weather", Name: "Weather", Source: "clawhub",
	}}})

	// First Enter opens confirmation; the next accepts and dispatches exactly one install.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.skillInstalling {
		t.Fatalf("expected one in-flight install: cmd=%v installing=%v", cmd, m.skillInstalling)
	}
	msg := cmd()
	if _, ok := msg.(ToolSkillInstallMsg); !ok {
		t.Fatalf("install command message = %T, want ToolSkillInstallMsg", msg)
	}

	m, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if duplicate != nil || m.skillConfirming {
		t.Fatalf("duplicate install was not blocked: cmd=%v confirming=%v", duplicate, m.skillConfirming)
	}

	m, _ = m.Update(ToolOperationResultMsg{Tab: ToolSubSkill, Success: false, Message: "network failed"})
	if m.skillInstalling {
		t.Fatal("installing state should clear after its result")
	}
}

func TestToolStatusSearchResultShowsSelectedDetailsAndCaution(t *testing.T) {
	m := NewToolStatusModel("en")
	m.SetSkills(nil)
	m, _ = m.Update(ToolSkillSearchResultMsg{Results: []SkillSearchResult{{
		ID: "download", Name: "Download Tool", Source: "clawhub", Description: "Fetches an HTTP URL.", Caution: "Prefer download_file for simple HTTP downloads.",
	}}})

	view := stripANSIForTest(m.View())
	for _, want := range []string{"Fetches an HTTP URL.", "Prefer download_file"} {
		if !strings.Contains(view, want) {
			t.Fatalf("search view missing %q:\n%s", want, view)
		}
	}
}

func TestToolStatus_SkillQuickSearchUsesPreset(t *testing.T) {
	m := NewToolStatusModel("en")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("expected quick search command")
	}
	if !m.skillSearching {
		t.Fatal("expected skillSearching after quick search")
	}
	if got := m.skillSearch.Value(); got != "coding" {
		t.Fatalf("quick search should set first preset query, got %q", got)
	}
	msg, ok := cmd().(ToolSkillSearchMsg)
	if !ok {
		t.Fatalf("expected ToolSkillSearchMsg, got %T", cmd())
	}
	if msg.Query != "coding" {
		t.Fatalf("quick search query = %q, want coding", msg.Query)
	}

	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("expected second quick search command")
	}
	msg = cmd().(ToolSkillSearchMsg)
	if msg.Query != "browser" {
		t.Fatalf("second quick search query = %q, want browser", msg.Query)
	}
}

func TestToolStatus_SkillEmptyStateShowsQuickSearch(t *testing.T) {
	m := NewToolStatusModel("en")
	m.SetSkills(nil)
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Left/Right chooses") || !strings.Contains(view, "Next quick search preset: coding") || !strings.Contains(view, "Quick presets: [coding]") {
		t.Fatalf("empty Skill view should advertise quick preset search:\n%s", view)
	}
}

func TestToolStatus_SkillEmptyStateLeftRightChoosesQuickPreset(t *testing.T) {
	m := NewToolStatusModel("en")
	m.SetSkills(nil)

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil {
		t.Fatalf("choosing a quick preset should not start search immediately: %v", cmd)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Next quick search preset: browser") || !strings.Contains(view, "[browser]") {
		t.Fatalf("Right should choose the next quick preset:\n%s", view)
	}

	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should run the selected quick preset")
	}
	msg, ok := cmd().(ToolSkillSearchMsg)
	if !ok || msg.Query != "browser" {
		t.Fatalf("selected preset command = %#v, want browser", cmd())
	}
}

func TestToolStatus_SkillEmptyStateEnterStartsQuickSearch(t *testing.T) {
	m := NewToolStatusModel("en")
	m.SetSkills(nil)

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Enter on empty Skill list to start a preset search")
	}
	msg, ok := cmd().(ToolSkillSearchMsg)
	if !ok {
		t.Fatalf("command returned %T, want ToolSkillSearchMsg", cmd())
	}
	if msg.Query != "coding" || m.skillSearch.Value() != "coding" {
		t.Fatalf("quick search query/value = %q/%q, want coding", msg.Query, m.skillSearch.Value())
	}
}

func TestToolStatus_SkillInstalledListKeepsQuickPresetSearch(t *testing.T) {
	m := NewToolStatusModel("en")
	m.SetSkills([]SkillItem{{Name: "existing", Description: "already installed", Status: "active"}})

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "preset(coding)") || !strings.Contains(view, "Enter/Space:search") {
		t.Fatalf("installed Skill list should advertise the current quick preset:\n%s", view)
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil {
		t.Fatalf("cycling installed-list preset should not start search immediately: %v", cmd)
	}
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "preset(browser)") {
		t.Fatalf("Right should change the quick preset shown in the footer:\n%s", view)
	}

	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should run the selected quick preset even when installed Skills exist")
	}
	msg, ok := cmd().(ToolSkillSearchMsg)
	if !ok || msg.Query != "browser" {
		t.Fatalf("installed-list quick search command = %#v, want browser", cmd())
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
		{"Redeem", TabServiceRedeem},
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

func TestToolStatusEnglishViewLocalizesSkillAndMCP(t *testing.T) {
	m := NewToolStatusModel("en")
	m.SetSkills(nil)
	m.SetMCPServers(nil)

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Search Skill") || !strings.Contains(view, "No installed Skills") {
		t.Fatalf("Skill view should be English-localized:\n%s", view)
	}
	if strings.Contains(view, "搜索") || strings.Contains(view, "暂无") {
		t.Fatalf("Skill view should not show Chinese in English mode:\n%s", view)
	}

	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'2'}}))
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "No MCP servers") || !strings.Contains(view, "Local templates") {
		t.Fatalf("MCP view should be English-localized:\n%s", view)
	}
}

func TestToolStatusChineseSkillQuickPresetsUseLocalizedLabels(t *testing.T) {
	m := NewToolStatusModel("zh")
	m.SetSkills(nil)
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "常用预设") || !strings.Contains(view, "[编程]") || !strings.Contains(view, "数据库") {
		t.Fatalf("Chinese Skill empty state should show localized quick-preset choices:\n%s", view)
	}
	if strings.Contains(view, "Quick presets") || strings.Contains(view, "[coding]") {
		t.Fatalf("Chinese Skill empty state should not show English preset UI labels:\n%s", view)
	}
}

func TestToolStatusChineseInstalledSkillFooterUsesLocalizedPreset(t *testing.T) {
	m := NewToolStatusModel("zh")
	m.SetSkills([]SkillItem{{Name: "existing", Description: "已安装", Status: "active"}})

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "预设(编程)") || strings.Contains(view, "preset(coding)") {
		t.Fatalf("Chinese installed Skill footer should use localized preset label:\n%s", view)
	}
}

func TestToolStatusSetLangUpdatesSearchPlaceholder(t *testing.T) {
	m := NewToolStatusModel("zh")
	m.SetLang("en")
	if got := m.skillSearch.Placeholder; got != "Search Skill..." {
		t.Fatalf("placeholder = %q", got)
	}
}

func TestToolStatusSetLangUpdatesMCPFormPlaceholders(t *testing.T) {
	m := NewToolStatusModel("zh")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'a'}}))
	m.SetLang("en")
	if got := m.mcpInputs[0].Placeholder; got != "Name" {
		t.Fatalf("name placeholder = %q", got)
	}
	if got := m.mcpInputs[1].Placeholder; got != "Command (e.g. uvx, npx)" {
		t.Fatalf("command placeholder = %q", got)
	}
}

func TestToolStatusSetLangRelocalizesMessages(t *testing.T) {
	m := NewToolStatusModel("zh")
	m.skillMessage = toolStatusFormat("zh", "searchingPreset", "编程")
	m.mcpMessage = toolStatusText("zh", "remoteSecretRequired")

	m.SetLang("en")

	if got := m.skillMessage; got != ""+toolStatusFormat("en", "searchingPreset", "编程") {
		t.Fatalf("skill message = %q", got)
	}
	if got := m.mcpMessage; got != ""+toolStatusText("en", "remoteSecretRequired") {
		t.Fatalf("MCP message = %q", got)
	}
}

func TestToolStatusSetLangRelocalizesInstallConfirmationAndCounts(t *testing.T) {
	m := NewToolStatusModel("zh")
	m.skillMessage = toolStatusFormat("zh", "confirmInstall", "Playwright", "GitHub")
	m.mcpMessage = toolStatusFormat("zh", "foundSkillResults", 12)

	m.SetLang("en")

	if got := m.skillMessage; got != toolStatusFormat("en", "confirmInstall", "Playwright", "GitHub") {
		t.Fatalf("confirm message = %q", got)
	}
	if got := m.mcpMessage; got != ""+toolStatusFormat("en", "foundSkillResults", 12) {
		t.Fatalf("count message = %q", got)
	}
}

func TestToolStatusLocalMCPPresetSubmitsWithoutTyping(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'a'}}))

	if !m.mcpAdding || m.mcpAddType != 0 {
		t.Fatalf("expected local MCP add form")
	}
	if got := m.mcpInputs[0].Value(); got != "filesystem" {
		t.Fatalf("preset name = %q", got)
	}
	if got := m.mcpInputs[1].Value(); got != "npx" {
		t.Fatalf("preset command = %q", got)
	}

	m, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("expected add command from default preset")
	}
	msg, ok := cmd().(ToolMCPAddMsg)
	if !ok {
		t.Fatalf("expected ToolMCPAddMsg, got %T", cmd())
	}
	if msg.Entry.Name != "filesystem" || msg.Entry.Command != "npx" {
		t.Fatalf("unexpected MCP entry: %#v", msg.Entry)
	}
	if got := strings.Join(msg.Entry.Args, " "); got != "-y @modelcontextprotocol/server-filesystem ." {
		t.Fatalf("args = %q", got)
	}
	if m.mcpAdding {
		t.Fatal("form should close after submit")
	}
}

func TestToolStatusMCPPresetFormStartsAsQuickChoice(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'a'}}))

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Ready to add:") || !strings.Contains(view, "Enter confirms this preset") {
		t.Fatalf("local preset should start as a quick choice:\n%s", view)
	}
	if !strings.Contains(view, "Tab adjusts details if needed") || strings.Contains(view, "edits details") {
		t.Fatalf("local preset should frame text fields as optional adjustments:\n%s", view)
	}
	if strings.Contains(view, "Command:") || strings.Contains(view, "Args:") {
		t.Fatalf("local preset should hide editable fields until details are requested:\n%s", view)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "Command:") || !strings.Contains(view, "Args:") {
		t.Fatalf("Tab should expand local MCP editable details:\n%s", view)
	}
}

func TestToolStatusLocalMCPPresetCyclesAndUpdatesFields(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'a'}}))
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRight}))

	if got := m.mcpInputs[0].Value(); got != "playwright" {
		t.Fatalf("cycled preset name = %q", got)
	}
	if got := m.mcpInputs[2].Value(); !strings.Contains(got, "@playwright/mcp") {
		t.Fatalf("cycled args = %q", got)
	}

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Template:") || !strings.Contains(view, "browser automation") {
		t.Fatalf("local MCP form should show preset selector and description:\n%s", view)
	}
}

func TestToolStatusRemoteMCPPresetSubmitsWithoutTyping(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'A'}}))

	if !m.mcpAdding || m.mcpAddType != 1 {
		t.Fatalf("expected remote MCP add form")
	}
	if got := m.mcpInputs[0].Value(); got != "local-http-mcp" {
		t.Fatalf("preset name = %q", got)
	}
	if got := m.mcpInputs[1].Value(); got != "http://127.0.0.1:3000/mcp" {
		t.Fatalf("preset URL = %q", got)
	}
	if got := mcpAuthTypes[m.mcpAuthIdx]; got != "none" {
		t.Fatalf("auth type = %q", got)
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected add command from default remote preset")
	}
	msg, ok := cmd().(ToolMCPAddRemoteMsg)
	if !ok {
		t.Fatalf("expected ToolMCPAddRemoteMsg, got %T", cmd())
	}
	if msg.Entry.Name != "local-http-mcp" || msg.Entry.EndpointURL != "http://127.0.0.1:3000/mcp" {
		t.Fatalf("unexpected remote MCP entry: %#v", msg.Entry)
	}
	if m.mcpAdding {
		t.Fatal("form should close after submit")
	}
}

func TestToolStatusMCPEmptyStateEnterStartsLocalTemplate(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m.SetMCPServers(nil)

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("opening the local MCP template should not submit immediately: %v", cmd)
	}
	if !m.mcpAdding || m.mcpAddType != 0 {
		t.Fatalf("Enter on empty MCP list should open local template form, adding=%v type=%d", m.mcpAdding, m.mcpAddType)
	}
	if got := m.mcpInputs[0].Value(); got != "filesystem" {
		t.Fatalf("default local template = %q, want filesystem", got)
	}
}

func TestToolStatusMCPEmptyStateShowsTemplateChoices(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 18})
	m.subTab = ToolSubMCP
	m.SetMCPServers(nil)

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Selected local template: filesystem") || !strings.Contains(view, "share the current directory") {
		t.Fatalf("MCP empty state should preview the selected local template:\n%s", view)
	}
	if !strings.Contains(view, "Local templates: [filesystem]") || !strings.Contains(view, "playwright") {
		t.Fatalf("MCP empty state should show selectable local templates:\n%s", view)
	}
	if !strings.Contains(view, "Left/Right") || !strings.Contains(view, "Enter/Space") || !strings.Contains(view, "remote templates") {
		t.Fatalf("MCP empty state should advertise choice-first actions:\n%s", view)
	}
}

func TestToolStatusMCPEmptyStateLeftRightChoosesTemplate(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m.SetMCPServers(nil)

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil {
		t.Fatalf("choosing an MCP template should not open the form immediately: %v", cmd)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Selected local template: playwright") || !strings.Contains(view, "[playwright]") {
		t.Fatalf("Right should choose the next local MCP template:\n%s", view)
	}

	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("opening selected MCP template should not submit immediately: %v", cmd)
	}
	if !m.mcpAdding || m.mcpAddType != 0 {
		t.Fatalf("Enter should open local MCP template form, adding=%v type=%d", m.mcpAdding, m.mcpAddType)
	}
	if got := m.mcpInputs[0].Value(); got != "playwright" {
		t.Fatalf("selected local template = %q, want playwright", got)
	}
}

func TestToolStatusChineseMCPEmptyStateUsesLocalizedTemplateHelp(t *testing.T) {
	m := NewToolStatusModel("zh")
	m.subTab = ToolSubMCP
	m.SetMCPServers(nil)

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "当前本地模板：filesystem") || !strings.Contains(view, "把当前目录提供给模型访问") {
		t.Fatalf("Chinese MCP empty state should preview selected local template:\n%s", view)
	}
	if !strings.Contains(view, "本地模板：[filesystem]") || !strings.Contains(view, "左右键选择本地模板") {
		t.Fatalf("Chinese MCP empty state should show localized choice help:\n%s", view)
	}
	if strings.Contains(view, "Selected local template") || strings.Contains(view, "Local templates") {
		t.Fatalf("Chinese MCP empty state should not show English labels:\n%s", view)
	}
}

func TestToolStatusRemoteMCPPresetFormStartsAsQuickChoice(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'A'}}))

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Ready to add:") || !strings.Contains(view, "http://127.0.0.1:3000/mcp") {
		t.Fatalf("remote preset should start as a quick choice:\n%s", view)
	}
	if !strings.Contains(view, "Tab adjusts details if needed") || strings.Contains(view, "edits details") {
		t.Fatalf("remote preset should frame text fields as optional adjustments:\n%s", view)
	}
	if strings.Contains(view, "Secret/Token:") {
		t.Fatalf("remote preset should hide editable fields until details are requested:\n%s", view)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "Secret/Token:") || !strings.Contains(view, "Auth type:") {
		t.Fatalf("Tab should expand remote MCP editable details:\n%s", view)
	}
}

func TestToolStatusRemoteMCPPresetCyclesAndUpdatesFields(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'A'}}))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})

	if got := m.mcpInputs[0].Value(); got != "remote-bearer-mcp" {
		t.Fatalf("cycled preset name = %q", got)
	}
	if got := mcpAuthTypes[m.mcpAuthIdx]; got != "bearer" {
		t.Fatalf("cycled auth type = %q", got)
	}
	if got := m.mcpInputs[1].Value(); got != "" {
		t.Fatalf("remote bearer template should not use a fake URL, got %q", got)
	}

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Template:") || !strings.Contains(view, "bearer token") {
		t.Fatalf("remote MCP form should show preset selector and description:\n%s", view)
	}
}

func TestToolStatusRemoteMCPPresetMissingURLExpandsDetails(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'A'}}))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "URL:") || !strings.Contains(view, "Secret/Token:") {
		t.Fatalf("remote template without URL should expose required details:\n%s", view)
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("missing URL should not submit a remote MCP entry: %v", cmd)
	}
	if !m.mcpAdding || !strings.Contains(stripANSIForTest(m.View()), "name and URL are required") {
		t.Fatalf("missing URL should keep the user in the form with a clear message:\n%s", stripANSIForTest(m.View()))
	}
	if m.mcpFocused != 2 || !m.mcpInputs[1].Focused() {
		t.Fatalf("missing URL should focus the URL input, focused=%d inputFocused=%v", m.mcpFocused, m.mcpInputs[1].Focused())
	}
}

func TestToolStatusRemoteMCPAuthRequiresSecret(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'A'}}))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.mcpInputs[1].SetValue("https://mcp.example/mcp")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("missing secret should not submit a remote MCP entry: %v", cmd)
	}
	if !m.mcpAdding || m.mcpFocused != 4 || !m.mcpInputs[2].Focused() {
		t.Fatalf("missing secret should keep form open and focus secret, adding=%v focused=%d secretFocused=%v", m.mcpAdding, m.mcpFocused, m.mcpInputs[2].Focused())
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "needs a secret or token") {
		t.Fatalf("missing secret should show a clear error:\n%s", view)
	}

	m.mcpInputs[2].SetValue("secret-token")
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected remote MCP add command after filling secret")
	}
	msg, ok := cmd().(ToolMCPAddRemoteMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Entry.AuthType != "bearer" || msg.Entry.AuthSecret != "secret-token" {
		t.Fatalf("remote MCP auth = %q/%q", msg.Entry.AuthType, msg.Entry.AuthSecret)
	}
}

func TestToolStatusRemoteMCPAuthTypeLabelsAreReadable(t *testing.T) {
	if got := mcpAuthTypeDisplay("none", "zh"); got != "不认证" {
		t.Fatalf("zh none auth label = %q", got)
	}
	if got := mcpAuthTypeDisplay("api_key", "en"); got != "API Key" {
		t.Fatalf("api key label = %q", got)
	}
	if got := mcpAuthTypeDisplay("bearer", "en"); got != "Bearer Token" {
		t.Fatalf("bearer label = %q", got)
	}

	m := NewToolStatusModel("zh")
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'A'}}))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Bearer Token") || strings.Contains(view, "认证类型: bearer") {
		t.Fatalf("remote MCP auth should display readable labels:\n%s", view)
	}
	if got := m.mcpInputs[1].Placeholder; got != "端点 URL" {
		t.Fatalf("endpoint placeholder = %q", got)
	}
}

func TestToolStatusListsFitNarrowTerminal(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 16})
	m.SetSkills([]SkillItem{{
		Name:        "very-long-skill-name-for-terminal",
		Description: "a very long description that should be clipped for narrow SSH terminals",
		Status:      "active",
		Publisher:   "publisher-with-long-name",
	}})
	assertRenderedLinesFit(t, m.View(), 44)

	m.subTab = ToolSubMCP
	m.SetMCPServers([]MCPItem{{
		Name:     "remote-server-with-very-long-name",
		Type:     "remote",
		Status:   "running",
		Endpoint: "https://example.invalid/very/long/path/that/should/not/overflow/the/window",
	}})
	assertRenderedLinesFit(t, m.View(), 44)
}

func TestToolStatusMCPFormsFitNarrowTerminal(t *testing.T) {
	for _, key := range []rune{'a', 'A'} {
		m := NewToolStatusModel("en")
		m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 16})
		m.subTab = ToolSubMCP
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if !m.mcpAdding {
			t.Fatalf("expected MCP form for key %q", key)
		}
		assertRenderedLinesFit(t, m.View(), 44)
	}
}

func TestToolStatusMCPCompactFormKeepsPresetAndFooterVisible(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  rune
		want string
	}{
		{name: "local", key: 'a', want: "filesystem"},
		{name: "remote", key: 'A', want: "local-http-mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewToolStatusModel("en")
			m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 10})
			m.subTab = ToolSubMCP
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})

			view := stripANSIForTest(m.View())
			if !strings.Contains(view, tc.want) || !strings.Contains(view, "Ready to add:") {
				t.Fatalf("compact MCP form should keep preset summary visible:\n%s", view)
			}
			if !strings.Contains(view, "Enter confirms") {
				t.Fatalf("compact MCP form should keep confirm footer visible:\n%s", view)
			}
			if strings.Contains(view, "Command:") || strings.Contains(view, "Secret/Token:") {
				t.Fatalf("compact quick-choice form should keep details hidden until requested:\n%s", view)
			}
			lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
			if len(lines) > 6 {
				t.Fatalf("compact MCP form should fit root content height, got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
			}
			assertRenderedLinesFit(t, view, 46)
		})
	}
}

func TestToolStatusMCPCompactDetailsShowsOnlyFocusedField(t *testing.T) {
	m := NewToolStatusModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 10})
	m.subTab = ToolSubMCP
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Name:") {
		t.Fatalf("compact detail mode should show the focused field:\n%s", view)
	}
	if strings.Contains(view, "Command:") || strings.Contains(view, "Args:") {
		t.Fatalf("compact detail mode should not show every text field at once:\n%s", view)
	}
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) > 6 {
		t.Fatalf("compact detail form should fit root content height, got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestToolStatusLocalMCPEnvIsMaskedInPresetSummary(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m.mcpAdding = true
	m.mcpAddType = 0
	m.mcpLocalTemplateIdx = 0
	m.mcpFocused = 0
	m.mcpInputs = buildMCPLocalInputs("en")
	m.mcpInputs[0].SetValue("secret-env")
	m.mcpInputs[1].SetValue("npx")
	m.mcpInputs[2].SetValue("-y server")
	m.mcpInputs[3].SetValue("API_KEY=super-secret DEBUG=1")

	if m.mcpInputs[3].EchoMode != textinput.EchoPassword {
		t.Fatalf("local MCP env input should use password echo, got %v", m.mcpInputs[3].EchoMode)
	}
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "super-secret") || strings.Contains(view, "DEBUG=1") {
		t.Fatalf("MCP env summary should mask values:\n%s", view)
	}
	if !strings.Contains(view, "API_KEY=********") || !strings.Contains(view, "DEBUG=********") {
		t.Fatalf("MCP env summary should keep names with masked values:\n%s", view)
	}
}

func TestToolStatusRemoteMCPSecretInputIsMasked(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m.mcpAdding = true
	m.mcpAddType = 1
	m.mcpRemoteTemplateIdx = len(mcpRemoteTemplates) - 1
	m.mcpFocused = 4
	m.mcpInputs = buildMCPRemoteInputs("en")
	m.mcpInputs[0].SetValue("remote")
	m.mcpInputs[1].SetValue("https://mcp.example")
	m.mcpInputs[2].SetValue("remote-secret")
	m.focusMCPRemoteFocusedInput()

	if m.mcpInputs[2].EchoMode != textinput.EchoPassword {
		t.Fatalf("remote MCP secret input should use password echo, got %v", m.mcpInputs[2].EchoMode)
	}
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "remote-secret") {
		t.Fatalf("remote MCP secret should not be visible:\n%s", view)
	}
}

func TestToolStatusLocalMCPSubmitKeepsRealEnvValues(t *testing.T) {
	m := NewToolStatusModel("en")
	m.mcpInputs = buildMCPLocalInputs("en")
	m.mcpInputs[0].SetValue("secret-env")
	m.mcpInputs[1].SetValue("npx")
	m.mcpInputs[2].SetValue("-y server")
	m.mcpInputs[3].SetValue("API_KEY=super-secret DEBUG=1")

	_, cmd := m.submitMCPLocal()
	if cmd == nil {
		t.Fatal("expected local MCP add command")
	}
	msg, ok := cmd().(ToolMCPAddMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Entry.Env["API_KEY"] != "super-secret" || msg.Entry.Env["DEBUG"] != "1" {
		t.Fatalf("real env values were not preserved: %#v", msg.Entry.Env)
	}
}

func TestToolStatusLocalMCPSubmitPreservesQuotedArgsAndEnv(t *testing.T) {
	m := NewToolStatusModel("en")
	m.mcpInputs = buildMCPLocalInputs("en")
	m.mcpInputs[0].SetValue("filesystem")
	m.mcpInputs[1].SetValue("npx")
	m.mcpInputs[2].SetValue(`-y server "/tmp/project with space" --flag`)
	m.mcpInputs[3].SetValue(`API_KEY="secret with space" DEBUG=1`)

	_, cmd := m.submitMCPLocal()
	if cmd == nil {
		t.Fatal("expected local MCP add command")
	}
	msg, ok := cmd().(ToolMCPAddMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	wantArgs := []string{"-y", "server", "/tmp/project with space", "--flag"}
	if len(msg.Entry.Args) != len(wantArgs) {
		t.Fatalf("args = %#v, want %#v", msg.Entry.Args, wantArgs)
	}
	for i := range wantArgs {
		if msg.Entry.Args[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", msg.Entry.Args, wantArgs)
		}
	}
	if msg.Entry.Env["API_KEY"] != "secret with space" || msg.Entry.Env["DEBUG"] != "1" {
		t.Fatalf("quoted env values were not preserved: %#v", msg.Entry.Env)
	}
}

func TestToolStatusLocalMCPInvalidQuoteStaysInForm(t *testing.T) {
	m := NewToolStatusModel("en")
	m.subTab = ToolSubMCP
	m.mcpAdding = true
	m.mcpAddType = 0
	m.mcpInputs = buildMCPLocalInputs("en")
	m.mcpInputs[0].SetValue("filesystem")
	m.mcpInputs[1].SetValue("npx")
	m.mcpInputs[2].SetValue(`-y "unfinished`)

	m, cmd := m.submitMCPLocal()
	if cmd != nil {
		t.Fatalf("invalid args should not submit: %v", cmd)
	}
	if !m.mcpAdding || m.mcpFocused != 3 || !m.mcpInputs[2].Focused() {
		t.Fatalf("invalid args should keep form open and focus args, adding=%v focused=%d argsFocused=%v", m.mcpAdding, m.mcpFocused, m.mcpInputs[2].Focused())
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "unfinished quote") {
		t.Fatalf("invalid args should show a clear error:\n%s", view)
	}
}

func TestMaskMCPEnvForDisplay(t *testing.T) {
	got := maskMCPEnvForDisplay("API_KEY=secret EMPTY= FLAG")
	if got != "API_KEY=******** EMPTY= ********" {
		t.Fatalf("masked env = %q", got)
	}
	got = maskMCPEnvForDisplay(`API_KEY="secret with space" DEBUG=1`)
	if got != "API_KEY=******** DEBUG=********" {
		t.Fatalf("quoted masked env = %q", got)
	}
}

func assertRenderedLinesFit(t *testing.T, rendered string, width int) {
	t.Helper()
	for _, line := range strings.Split(stripANSIForTest(rendered), "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line width = %d, want <= %d: %q", got, width, line)
		}
	}
}
