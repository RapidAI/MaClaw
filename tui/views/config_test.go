package views

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfigEnterOnSuggestedTextFieldOpensChooser(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	moveConfigCursorToKey(t, &m, "hubcenter_url")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening suggestion chooser should not save immediately")
	}
	if !m.selectMode || !m.selectSuggestions {
		t.Fatalf("expected suggestion selector, selectMode=%v selectSuggestions=%v", m.selectMode, m.selectSuggestions)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Manual input") {
		t.Fatalf("suggestion selector should include manual fallback:\n%s", view)
	}
}

func TestConfigSuggestionChooserSavesSelectedSuggestion(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	moveConfigCursorToKey(t, &m, "hubcenter_url")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.selectCursor = 0
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command for selected suggestion")
	}
	msg, ok := cmd().(ConfigSaveMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Key != "hubcenter_url" || msg.Value == "" {
		t.Fatalf("save message = %#v", msg)
	}
	if m.selectMode || m.editing {
		t.Fatalf("selector should close after saving, selectMode=%v editing=%v", m.selectMode, m.editing)
	}
}

func TestConfigSuggestionChooserCanOpenManualInput(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	moveConfigCursorToKey(t, &m, "hubcenter_url")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	entry := m.currentEntries()[m.cursor]
	m.selectCursor = len(entry.suggestionValues())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing || m.selectMode || m.selectSuggestions {
		t.Fatalf("manual fallback should open text input, editing=%v selectMode=%v selectSuggestions=%v", m.editing, m.selectMode, m.selectSuggestions)
	}
}

func TestConfigSuggestionChooserScrollsAroundCursor(t *testing.T) {
	m := NewConfigModel("en")
	m.height = 18
	m.LoadFromAppConfig(corelib.AppConfig{})
	m.setEntrySuggestions("hubcenter_url", []string{"/one", "/two", "/three", "/four", "/five", "/six", "/seven"})
	moveConfigCursorToKey(t, &m, "hubcenter_url")
	entry := m.currentEntries()[m.cursor]

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.selectCursor = len(entry.Suggestions)
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Manual input") || !strings.Contains(view, "↑") {
		t.Fatalf("suggestion chooser should scroll to the selected manual fallback:\n%s", view)
	}
	if strings.Contains(view, "/one") {
		t.Fatalf("suggestion chooser should not render the full long list at once:\n%s", view)
	}
}

func TestConfigOptionChooserUsesVerticalListOnNarrowTerminal(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	moveConfigCursorToKey(t, &m, "language")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening option chooser should not save immediately")
	}
	if !m.selectMode || m.selectSuggestions {
		t.Fatalf("expected option selector, selectMode=%v selectSuggestions=%v", m.selectMode, m.selectSuggestions)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "> Chinese") || !strings.Contains(view, "  English") {
		t.Fatalf("narrow option chooser should render a vertical language list:\n%s", view)
	}
	assertViewFitsWidth(t, view, 44)
}

func TestConfigBooleanOptionsAreLocalized(t *testing.T) {
	if got := configOptionDisplay("agentnet_enabled", "true", "zh"); got != "开启" {
		t.Fatalf("zh true label = %q", got)
	}
	if got := configOptionDisplay("agentnet_enabled", "false", "zh"); got != "关闭" {
		t.Fatalf("zh false label = %q", got)
	}
	if got := configOptionDisplay("agentnet_enabled", "true", "en"); got != "On" {
		t.Fatalf("en true label = %q", got)
	}

	m := NewConfigModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{AgentNetEnabled: true})
	moveConfigCursorToKey(t, &m, "agentnet_enabled")
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "● 开启") || strings.Contains(view, "ON") || strings.Contains(view, "true") {
		t.Fatalf("boolean display should be localized and hide raw bools:\n%s", view)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "关闭") || strings.Contains(view, "false") {
		t.Fatalf("boolean selector should use localized labels:\n%s", view)
	}
}

func TestConfigCommonEnumsAreLocalized(t *testing.T) {
	cases := []struct {
		key   string
		value string
		lang  string
		want  string
	}{
		{"security_policy_mode", "permissive", "zh", "宽松"},
		{"sandbox_mode", "os", "zh", "系统沙箱"},
		{"network_level", "full", "zh", "完整网络"},
		{"skill_purchase_mode", "free_only", "zh", "仅免费"},
		{"ui_mode", "lite", "zh", "简洁"},
		{"maclaw_llm_protocol", "openai", "zh", "OpenAI 兼容"},
		{"maclaw_llm_provider_preset", "Custom", "zh", "自定义"},
		{"maclaw_llm_provider_preset", "Ollama Local", "zh", "Ollama 本地"},
		{"maclaw_llm_provider_preset", "LM Studio Local", "zh", "LM Studio 本地"},
		{"maclaw_llm_provider_preset", "Zhipu GLM Lobster", "zh", "智谱 GLM 龙虾"},
		{"maclaw_llm_model_choice", "auto", "zh", "自动"},
		{"setup_status", "needs_llm_key", "en", "LLM key needed"},
		{"setup_status", "needs_llm_key", "zh", "需要 LLM 密钥"},
		{"network_level", "intranet", "en", "Intranet only"},
		{"ui_mode", "pro", "en", "Pro"},
	}
	for _, tc := range cases {
		if got := configOptionDisplay(tc.key, tc.value, tc.lang); got != tc.want {
			t.Fatalf("configOptionDisplay(%q, %q, %q) = %q, want %q", tc.key, tc.value, tc.lang, got, tc.want)
		}
	}

	m := NewConfigModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{UIMode: "lite"})
	m.activeTab = CfgTabAdvanced
	moveConfigCursorToKey(t, &m, "ui_mode")
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "简洁") || strings.Contains(view, "lite") {
		t.Fatalf("advanced enum display should be localized:\n%s", view)
	}
}

func TestConfigDisplayNameForLangLocalizesKnownKeys(t *testing.T) {
	if got := ConfigDisplayNameForLang("maclaw_llm_provider_preset", "zh"); got != "LLM 服务商" {
		t.Fatalf("Chinese config display name = %q", got)
	}
	if got := ConfigDisplayNameForLang("maclaw_llm_provider_preset", "en"); got != "LLM provider" {
		t.Fatalf("English config display name = %q", got)
	}
	if got := ConfigDisplayNameForLang("onboarding", "zh"); got != "初始化" {
		t.Fatalf("Chinese onboarding display name = %q", got)
	}
	if got := ConfigDisplayNameForLang("unknown_key", "en"); got != "unknown_key" {
		t.Fatalf("unknown key should pass through, got %q", got)
	}
}

func TestConfigViewDoesNotPanicOnTinyTerminal(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 2, Height: 6})

	_ = m.View()
}

func TestConfigValueWidthUsesNarrowTerminalSpace(t *testing.T) {
	if got := configNameWidth(44); got != 18 {
		t.Fatalf("narrow name width = %d, want compact label width", got)
	}
	if got := configValueWidth(44, configNameWidth(44)); got < 18 || got > 22 {
		t.Fatalf("narrow value width = %d, want a usable remainder", got)
	}
	if got := configValueWidth(140, configNameWidth(140)); got != 36 {
		t.Fatalf("wide value width = %d, want capped width", got)
	}
}

func TestConfigCompactViewKeepsFooterAndFocusedRowVisible(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 10})
	moveConfigCursorToKey(t, &m, "check_update_on_startup")

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Check update") {
		t.Fatalf("compact config should keep the focused row visible:\n%s", view)
	}
	if !strings.Contains(view, "Enter:choose") {
		t.Fatalf("compact config should keep the footer visible:\n%s", view)
	}
	if strings.Contains(view, "Config") || strings.Contains(view, "options:") {
		t.Fatalf("compact config should omit title/details to save vertical space:\n%s", view)
	}
	assertViewFitsWidth(t, view, 46)
}

func TestConfigCompactViewUsesHeightBoundedRows(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 9})
	moveConfigCursorToKey(t, &m, "language")

	lines := strings.Split(strings.TrimRight(stripANSIForTest(m.View()), "\n"), "\n")
	if len(lines) > 5 {
		t.Fatalf("compact config should fit root content height, got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestConfigCompactSelectorKeepsFooterVisible(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 10})
	moveConfigCursorToKey(t, &m, "language")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("opening selector should not save immediately")
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "> Chinese") {
		t.Fatalf("compact selector should keep the selected choice visible:\n%s", view)
	}
	if !strings.Contains(view, "Enter:confirm") {
		t.Fatalf("compact selector should keep the selector footer visible:\n%s", view)
	}
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) > 6 {
		t.Fatalf("compact selector should fit root content height, got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestConfigSetupStatusEnterOpensSetup(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	moveConfigCursorToKey(t, &m, "setup_status")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected setup navigation command")
	}
	if _, ok := cmd().(ConfigOpenSetupMsg); !ok {
		t.Fatalf("command returned %T, want ConfigOpenSetupMsg", cmd())
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Setup needed") || !strings.Contains(view, "[Enter->Setup]") {
		t.Fatalf("setup status row should advertise Enter action:\n%s", view)
	}
	if strings.Contains(view, "needs_setup") {
		t.Fatalf("setup status details should not expose raw internal value:\n%s", view)
	}
}

func TestConfigSetupStatusDetailsShowFreshMachineChecklist(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 96
	m.LoadFromAppConfig(corelib.AppConfig{})
	moveConfigCursorToKey(t, &m, "setup_status")

	details := stripANSIForTest(m.renderDetails(m.currentEntries()[m.cursor]))
	for _, want := range []string{
		"[ ] Setup: open guided Setup",
		"[ ] Hub: email + HubCenter activation needed",
		"[ ] Remote machine: optional for remote tasks",
		"[ ] Official service: redeem code optional",
		"[ ] LLM: choose provider or redeem service",
		"[ ] MCP: optional templates available",
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("setup status details missing %q:\n%s", want, details)
		}
	}
	if strings.Contains(details, "needs_setup") || strings.Contains(details, "setup_status") {
		t.Fatalf("fresh-machine checklist should hide raw internal values:\n%s", details)
	}
}

func TestConfigSetupStatusDetailsShowMissingLLMKeyProgress(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 110
	m.LoadFromAppConfig(corelib.AppConfig{
		MaclawLLMUrl:             "https://api.openai.com/v1",
		MaclawLLMModel:           "gpt-4o",
		MaclawLLMCurrentProvider: "OpenAI API Key",
	})
	moveConfigCursorToKey(t, &m, "setup_status")

	details := stripANSIForTest(m.renderDetails(m.currentEntries()[m.cursor]))
	for _, want := range []string{
		"[x] Setup: LLM provider selected; key needed",
		"[ ] LLM: API key required for selected provider",
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("missing-key checklist missing %q:\n%s", want, details)
		}
	}
	if strings.Contains(details, "open guided Setup") {
		t.Fatalf("missing-key LLM path should not look like a fresh machine:\n%s", details)
	}
}

func TestConfigSetupStatusDetailsShowReadyChecklist(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 120
	m.LoadFromAppConfig(corelib.AppConfig{
		OnboardingDone:           true,
		RemoteHubURL:             "https://hub.example",
		RemoteMachineID:          "machine-1",
		RemoteMachineToken:       "machine-token",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "qwen-max",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MCPServers:               []corelib.MCPServerEntry{{Name: "remote"}},
		LocalMCPServers:          []corelib.LocalMCPServerEntry{{Name: "local"}},
	})
	moveConfigCursorToKey(t, &m, "setup_status")

	details := stripANSIForTest(m.renderDetails(m.currentEntries()[m.cursor]))
	for _, want := range []string{
		"[x] Setup: guided setup complete",
		"[x] Hub: activated; Hub auto-selected",
		"[x] Remote machine: activated for remote tasks",
		"[x] Official service: active; default LLM is MaClaw Official",
		"[x] LLM: MaClaw Official",
		"[x] MCP: 2 configured",
	} {
		if !strings.Contains(details, want) {
			t.Fatalf("ready checklist missing %q:\n%s", want, details)
		}
	}
}

func TestConfigSetupStatusEnterOpensServiceRedeem(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	moveConfigCursorToKey(t, &m, "setup_status")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected redeem navigation command")
	}
	if _, ok := cmd().(ConfigOpenServiceRedeemMsg); !ok {
		t.Fatalf("command returned %T, want ConfigOpenServiceRedeemMsg", cmd())
	}
}

func TestConfigSetupStatusEnterOpensToolsWhenMCPOptional(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		MaclawLLMUrl:   "http://localhost:11434/v1",
		MaclawLLMModel: "qwen2.5-coder:32b",
	})
	moveConfigCursorToKey(t, &m, "setup_status")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected tools navigation command")
	}
	if _, ok := cmd().(ConfigOpenToolsMsg); !ok {
		t.Fatalf("command returned %T, want ConfigOpenToolsMsg", cmd())
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Add MCP templates") || !strings.Contains(view, "[Enter->Tools]") {
		t.Fatalf("MCP optional status should advertise Tools action:\n%s", view)
	}
}

func TestConfigSetupStatusEnterFocusesLLMKeyWhenMissingKey(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		MaclawLLMUrl:             "https://api.openai.com/v1",
		MaclawLLMModel:           "gpt-4o",
		MaclawLLMCurrentProvider: "OpenAI API Key",
	})
	moveConfigCursorToKey(t, &m, "setup_status")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	entries := m.currentEntries()
	key := ""
	if m.cursor < len(entries) {
		key = entries[m.cursor].Key
	}
	if m.activeTab != CfgTabLLM || key != "maclaw_llm_key" {
		t.Fatalf("active tab/key = %d/%q, want LLM/maclaw_llm_key", m.activeTab, key)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "LLM key") {
		t.Fatalf("missing-key status should guide to the key field:\n%s", view)
	}
}

func TestConfigSetupStatusEnterFocusesLLMWhenReady(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		MaclawLLMUrl:    "http://localhost:11434/v1",
		MaclawLLMModel:  "qwen2.5-coder:32b",
		LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
	})
	moveConfigCursorToKey(t, &m, "setup_status")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	if m.activeTab != CfgTabLLM || m.cursor != 0 {
		t.Fatalf("active tab/cursor = %d/%d, want LLM/0", m.activeTab, m.cursor)
	}
}

func TestConfigFocusSecurityConfig(t *testing.T) {
	m := NewConfigModel("en")
	m.FocusSecurityConfig()
	if m.activeTab != CfgTabSecurity || m.cursor != 0 {
		t.Fatalf("active tab/cursor = %d/%d, want Security/0", m.activeTab, m.cursor)
	}
}

func TestConfigFocusSetupStatus(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m.FocusSetupStatus()
	if m.activeTab != CfgTabGeneral {
		t.Fatalf("active tab = %d, want General", m.activeTab)
	}
	entries := m.currentEntries()
	if m.cursor >= len(entries) || entries[m.cursor].Key != "setup_status" {
		t.Fatalf("focused key = %q, want setup_status", entries[m.cursor].Key)
	}
	if !m.statusOverview {
		t.Fatal("FocusSetupStatus should enable compact status overview mode")
	}
}

func TestConfigStatusOverviewCompactPrioritizesChecklist(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 46, Height: 10})
	m.FocusSetupStatus()

	view := stripANSIForTest(m.View())
	for _, want := range []string{"Status:", "Setup:[ ]", "Hub:[ ]", "Machine:[ ]", "Official:[ ]", "LLM:[ ]", "MCP:[ ]", "Enter:choose"} {
		if !strings.Contains(view, want) {
			t.Fatalf("compact status overview missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Check update") {
		t.Fatalf("compact status overview should prioritize readiness checklist over normal config rows:\n%s", view)
	}
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) > 5 {
		t.Fatalf("compact status overview should fit short terminals, got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	assertViewFitsWidth(t, view, 46)
}

func TestConfigStatusOverviewClearsOnTabSwitch(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m.FocusSetupStatus()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if m.statusOverview {
		t.Fatal("manual tab switching should return to normal config browsing")
	}
	if m.activeTab != CfgTabLLM {
		t.Fatalf("active tab = %d, want LLM", m.activeTab)
	}
}

func TestConfigHubURLIsDisplayOnly(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:       "https://selected-hub.example",
		RemoteHubCenterURL: "https://center.example",
	})
	moveConfigCursorToKey(t, &m, "hub_url")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("read-only Hub URL should open setup")
	}
	msg := cmd()
	if _, ok := msg.(ConfigOpenSetupMsg); !ok {
		t.Fatalf("read-only Hub URL should open setup, got %T", msg)
	}
	if m.editing || m.selectMode {
		t.Fatalf("read-only Hub URL should not enter edit/select mode, editing=%v select=%v", m.editing, m.selectMode)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "read-only") || !strings.Contains(view, "https://selected-hub.example") || !strings.Contains(view, "Hub is selected automatically") || !strings.Contains(view, "[Enter->Setup]") {
		t.Fatalf("Hub URL should be displayed as read-only:\n%s", view)
	}
}

func TestConfigReadOnlyCredentialsOpenSetup(t *testing.T) {
	cases := []struct {
		name string
		key  string
		cfg  corelib.AppConfig
		tab  int
		hint string
	}{
		{name: "machine token", key: "token", cfg: corelib.AppConfig{RemoteMachineToken: "machine-token"}, tab: CfgTabGeneral, hint: "activate Hub"},
		{name: "weixin token", key: "weixin_token", cfg: corelib.AppConfig{WeixinEnabled: true, WeixinToken: "wx-token"}, tab: CfgTabIM, hint: "bind WeChat by QR code"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewConfigModel("en")
			m.activeTab = tc.tab
			m.LoadFromAppConfig(tc.cfg)
			moveConfigCursorToKey(t, &m, tc.key)

			m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatalf("%s should open setup", tc.key)
			}
			msg := cmd()
			if _, ok := msg.(ConfigOpenSetupMsg); !ok {
				t.Fatalf("%s should open setup, got %T", tc.key, msg)
			}
			if m.editing || m.selectMode {
				t.Fatalf("%s should not enter edit/select mode, editing=%v select=%v", tc.key, m.editing, m.selectMode)
			}
			view := stripANSIForTest(m.View())
			if !strings.Contains(view, "read-only") || !strings.Contains(view, tc.hint) || !strings.Contains(view, "[Enter->Setup]") {
				t.Fatalf("%s should show read-only setup hint:\n%s", tc.key, view)
			}
		})
	}
}

func TestConfigAdvancedDefaultsToLiteFields(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m.activeTab = CfgTabAdvanced

	keys := visibleConfigKeys(m.currentEntries())
	for _, hidden := range []string{"memory_auto_compress", "log_detail_enabled", "llm_trajectory_logging", "maclaw_debug_tool_calls"} {
		if containsString(keys, hidden) {
			t.Fatalf("lite advanced settings should hide %q: %#v", hidden, keys)
		}
	}
	for _, shown := range []string{"skill_purchase_mode", "ui_mode"} {
		if !containsString(keys, shown) {
			t.Fatalf("lite advanced settings should show %q: %#v", shown, keys)
		}
	}
}

func TestConfigAdvancedProShowsExpertFields(t *testing.T) {
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{UIMode: "pro"})
	m.activeTab = CfgTabAdvanced

	keys := visibleConfigKeys(m.currentEntries())
	for _, shown := range []string{"memory_auto_compress", "log_detail_enabled", "llm_trajectory_logging", "maclaw_debug_tool_calls"} {
		if !containsString(keys, shown) {
			t.Fatalf("pro advanced settings should show %q: %#v", shown, keys)
		}
	}
}

func TestConfigDetailsHideInternalKeyInLiteMode(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 80
	m.LoadFromAppConfig(corelib.AppConfig{})
	m.activeTab = CfgTabAdvanced
	moveConfigCursorToKey(t, &m, "ui_mode")

	details := stripANSIForTest(m.renderDetails(m.currentEntries()[m.cursor]))
	if strings.Contains(details, "key: ui_mode") {
		t.Fatalf("lite config details should not expose internal keys:\n%s", details)
	}
	if !strings.Contains(details, "options: ") {
		t.Fatalf("lite config details should still show available options:\n%s", details)
	}
}

func TestConfigDetailsShowInternalKeyInProMode(t *testing.T) {
	m := NewConfigModel("en")
	m.width = 80
	m.LoadFromAppConfig(corelib.AppConfig{UIMode: "pro"})
	m.activeTab = CfgTabAdvanced
	moveConfigCursorToKey(t, &m, "ui_mode")

	details := stripANSIForTest(m.renderDetails(m.currentEntries()[m.cursor]))
	if !strings.Contains(details, "key: ui_mode") {
		t.Fatalf("pro config details should show internal keys:\n%s", details)
	}
}

func TestConfigSensitiveValuesAreMaskedInViewAndEditing(t *testing.T) {
	const secret = "sk-live-super-secret"
	m := NewConfigModel("en")
	m.width = 100
	m.LoadFromAppConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: "Custom",
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMModel:           "model-x",
		MaclawLLMKey:             secret,
	})
	m.activeTab = CfgTabLLM
	moveConfigCursorToKey(t, &m, "maclaw_llm_key")

	view := stripANSIForTest(m.View())
	if strings.Contains(view, secret) {
		t.Fatalf("normal config view should mask sensitive values:\n%s", view)
	}
	if !strings.Contains(view, "********") {
		t.Fatalf("normal config view should show a masked placeholder:\n%s", view)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing || m.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("sensitive edit should use password echo, editing=%v echo=%v", m.editing, m.input.EchoMode)
	}
	editView := stripANSIForTest(m.View())
	if strings.Contains(editView, secret) {
		t.Fatalf("editing view should not reveal the old secret:\n%s", editView)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.editing || m.input.EchoMode != textinput.EchoNormal {
		t.Fatalf("cancel should restore normal echo, editing=%v echo=%v", m.editing, m.input.EchoMode)
	}
}

func TestConfigSensitiveSaveKeepsRealValueWhileMasked(t *testing.T) {
	const secret = "sk-new-secret"
	m := NewConfigModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{MaclawLLMCurrentProvider: "Custom"})
	m.activeTab = CfgTabLLM
	moveConfigCursorToKey(t, &m, "maclaw_llm_key")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.input.SetValue(secret)
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg, ok := cmd().(ConfigSaveMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Value != secret {
		t.Fatalf("save value = %q, want real secret", msg.Value)
	}
	if m.input.EchoMode != textinput.EchoNormal {
		t.Fatalf("save should restore normal echo, got %v", m.input.EchoMode)
	}
	view := stripANSIForTest(m.View())
	if strings.Contains(view, secret) {
		t.Fatalf("post-save config view should mask sensitive values:\n%s", view)
	}
}

func visibleConfigKeys(entries []ConfigEntry) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	return keys
}

func moveConfigCursorToKey(t *testing.T, m *ConfigModel, key string) {
	t.Helper()
	for i, entry := range m.currentEntries() {
		if entry.Key == key {
			m.cursor = i
			return
		}
	}
	t.Fatalf("config entry %q not visible", key)
}
