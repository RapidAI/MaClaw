package views

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	tea "github.com/charmbracelet/bubbletea"
)

// TestConfigFields_SingleSourceOfTruth verifies the mechanism-level invariant:
// every key that appears in the UI (allConfigFields) has both a Get and Set
// accessor, and round-trips correctly through AppConfig.
//
// This test makes it impossible to add a field to the UI without also wiring
// up its Get/Set — the three-way sync problem that previously caused silent
// bugs (field visible but doesn't save, or saves but doesn't load).
func TestConfigTabsFitNarrowTerminal(t *testing.T) {
	m := NewConfigModel("zh")
	m.width = 36
	tabs := m.renderTabs()
	if got := lipgloss.Width(tabs); got > m.width {
		t.Fatalf("config tab width = %d, want <= %d: %q", got, m.width, tabs)
	}
	if !strings.Contains(tabs, "1:") || !strings.Contains(tabs, "6:") {
		t.Fatalf("compact config tabs should keep numeric hints: %q", tabs)
	}
}

func TestConfigFields_SingleSourceOfTruth(t *testing.T) {
	seen := make(map[string]bool)
	for _, def := range allConfigFields {
		// No duplicate keys.
		if seen[def.Key] {
			t.Errorf("duplicate key in allConfigFields: %q", def.Key)
		}
		seen[def.Key] = true

		// Every field must have Get and Set (except ReadOnly which only needs Get).
		if def.Get == nil {
			t.Errorf("key %q: missing Get accessor", def.Key)
		}
		if !def.ReadOnly && def.Set == nil {
			t.Errorf("key %q: missing Set accessor (non-ReadOnly field)", def.Key)
		}

		// Tab must be valid.
		if def.Tab < 0 || def.Tab >= CfgTabCount {
			t.Errorf("key %q: invalid Tab %d", def.Key, def.Tab)
		}

		// DescKey must be non-empty.
		if def.DescKey == "" {
			t.Errorf("key %q: empty DescKey", def.Key)
		}
	}
}

// TestConfigFields_RoundTrip verifies that every non-ReadOnly field can be
// written to AppConfig via Set and read back via Get with the same value.
func TestConfigFields_RoundTrip(t *testing.T) {
	for _, def := range allConfigFields {
		if def.ReadOnly || def.Get == nil || def.Set == nil {
			continue
		}

		// Pick a test value based on field type.
		testVal := "test_value_123"
		if def.Options != nil && len(def.Options) > 0 {
			// Use the last option (different from typical default which is first).
			testVal = def.Options[len(def.Options)-1]
		}
		// intGet returns "" for zero, so use a numeric string for int fields.
		// Detect int fields by checking if the default Get on a zero-value config
		// returns "" (intGet behavior) vs "false" (boolGet) vs "" (strGet).
		// More robust: just try Set+Get and if it fails with a non-numeric string,
		// retry with a numeric one.
		cfg := corelib.AppConfig{}
		def.Set(&cfg, testVal)
		got := def.Get(&cfg)

		if got != testVal {
			// Might be an int field — retry with numeric value.
			numVal := "42"
			def.Set(&cfg, numVal)
			got = def.Get(&cfg)
			if got != numVal {
				t.Errorf("key %q: round-trip failed: Set(%q) then Get() = %q (also tried %q → %q)",
					def.Key, testVal, got, numVal, got)
			}
		}
	}
}

// TestConfigFields_ApplyAndLoad verifies the exported ApplyConfigValue and
// LoadConfigValue functions work for every registered key.
func TestConfigFields_ApplyAndLoad(t *testing.T) {
	for _, def := range allConfigFields {
		if def.ReadOnly {
			continue
		}

		testVal := "exported_api_test"
		if def.Options != nil && len(def.Options) > 0 {
			testVal = def.Options[0]
		}

		cfg := corelib.AppConfig{}
		ApplyConfigValue(&cfg, def.Key, testVal)
		got, ok := LoadConfigValue(&cfg, def.Key)
		if !ok {
			t.Errorf("key %q: LoadConfigValue returned ok=false", def.Key)
			continue
		}
		if got != testVal {
			// Might be an int field — retry with numeric value.
			numVal := "99"
			cfg2 := corelib.AppConfig{}
			ApplyConfigValue(&cfg2, def.Key, numVal)
			got2, _ := LoadConfigValue(&cfg2, def.Key)
			if got2 != numVal {
				t.Errorf("key %q: ApplyConfigValue(%q) then LoadConfigValue() = %q (also tried %q → %q)",
					def.Key, testVal, got, numVal, got2)
			}
		}
	}
}

// TestConfigFields_UIEntriesMatchDefs verifies that NewConfigModel produces
// entries that exactly match allConfigFields — no entries are lost or added
// outside the single source of truth.
func TestConfigFields_UIEntriesMatchDefs(t *testing.T) {
	m := NewConfigModel("zh")

	// Collect all UI keys.
	uiKeys := make(map[string]bool)
	for tab := 0; tab < CfgTabCount; tab++ {
		for _, e := range m.tabs[tab] {
			if uiKeys[e.Key] {
				t.Errorf("duplicate UI key: %q", e.Key)
			}
			uiKeys[e.Key] = true
		}
	}

	// Every def must appear in UI.
	for _, def := range allConfigFields {
		if !uiKeys[def.Key] {
			t.Errorf("key %q defined in allConfigFields but missing from UI", def.Key)
		}
	}

	// Every UI key must be in defs.
	defKeys := make(map[string]bool)
	for _, def := range allConfigFields {
		defKeys[def.Key] = true
	}
	for key := range uiKeys {
		if !defKeys[key] {
			t.Errorf("UI key %q not found in allConfigFields", key)
		}
	}
}

// TestConfigFields_LoadFromAppConfig verifies that LoadFromAppConfig uses
// the Get accessors and populates all UI entries.
func TestConfigFields_LoadFromAppConfig(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteHubURL:             "https://hub.example.com",
		RemoteHubCenterURL:       "https://center.example.com",
		MaclawLLMModel:           "test-model",
		QQBotEnabled:             true,
		DefaultProxyEnabled:      true,
		SecurityPolicyMode:       "strict",
		SkillPurchaseMode:        "free_only",
		MaclawAgentMaxIterations: 42,
	}

	m := NewConfigModel("zh")
	m.LoadFromAppConfig(cfg)

	checks := map[string]string{
		"hub_url":               "https://hub.example.com",
		"hubcenter_url":         "https://center.example.com",
		"maclaw_llm_model":      "test-model",
		"qqbot_enabled":         "true",
		"default_proxy_enabled": "true",
		"security_policy_mode":  "strict",
		"skill_purchase_mode":   "free_only",
		"max_iterations":        "42",
	}

	for key, want := range checks {
		found := false
		for tab := 0; tab < CfgTabCount; tab++ {
			for _, e := range m.tabs[tab] {
				if e.Key == key {
					if e.Value != want {
						t.Errorf("LoadFromAppConfig: key %q = %q, want %q", key, e.Value, want)
					}
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("LoadFromAppConfig: key %q not found in UI", key)
		}
	}
}

func TestConfigFields_LoadFromAppConfigAddsDynamicChoices(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMUrl:             "https://custom.example/v1",
		MaclawLLMModel:           "custom-model",
		MaclawLLMContextLength:   123456,
		DefaultProxyPort:         "9090",
		WorkingDirectory:         "/tmp/maclaw-work",
		LansengerGatewayURL:      "https://lan.example",
		WeixinBaseURL:            "https://wx.example",
		MaclawAgentMaxIterations: 77,
		RemoteHubCenterURL:       "https://center.example",
		RemoteHubCenterURLs:      []string{"https://backup-center.example"},
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "Saved Provider", URL: "https://saved.example/v1", Model: "saved-model"},
		},
	}

	m := NewConfigModel("zh")
	m.LoadFromAppConfig(cfg)

	_, _, provider := findConfigEntryForTest(t, m, "maclaw_llm_provider_preset")
	if !containsString(provider.Options, "Saved Provider") {
		t.Fatalf("provider options missing saved provider: %#v", provider.Options)
	}

	_, _, hubURL := findConfigEntryForTest(t, m, "hub_url")
	if !hubURL.ReadOnly {
		t.Fatal("hub_url should be read-only; it is selected by HubCenter during setup")
	}

	_, _, hubCenterURL := findConfigEntryForTest(t, m, "hubcenter_url")
	for _, want := range []string{"https://center.example", "https://backup-center.example", "https://hubs.maclaw.top"} {
		if !containsString(hubCenterURL.Suggestions, want) {
			t.Fatalf("hubcenter URL suggestions missing %q: %#v", want, hubCenterURL.Suggestions)
		}
	}

	_, _, ctx := findConfigEntryForTest(t, m, "maclaw_llm_context_length")
	if !containsString(ctx.Options, "123456") {
		t.Fatalf("context options missing current custom value: %#v", ctx.Options)
	}

	_, _, maxIterations := findConfigEntryForTest(t, m, "max_iterations")
	if !containsString(maxIterations.Options, "77") {
		t.Fatalf("max iteration options missing current custom value: %#v", maxIterations.Options)
	}

	_, _, proxyPort := findConfigEntryForTest(t, m, "default_proxy_port")
	if !containsString(proxyPort.Options, "9090") {
		t.Fatalf("proxy port options missing current custom value: %#v", proxyPort.Options)
	}

	_, _, llmURL := findConfigEntryForTest(t, m, "maclaw_llm_url")
	for _, want := range []string{"https://custom.example/v1", "https://saved.example/v1", "http://localhost:11434/v1"} {
		if !containsString(llmURL.Suggestions, want) {
			t.Fatalf("llm URL suggestions missing %q: %#v", want, llmURL.Suggestions)
		}
	}

	_, _, llmModel := findConfigEntryForTest(t, m, "maclaw_llm_model")
	for _, want := range []string{"custom-model", "saved-model", "auto"} {
		if !containsString(llmModel.Suggestions, want) {
			t.Fatalf("llm model suggestions missing %q: %#v", want, llmModel.Suggestions)
		}
	}

	_, _, weixinURL := findConfigEntryForTest(t, m, "weixin_base_url")
	if !containsString(weixinURL.Suggestions, "https://ilinkai.weixin.qq.com") {
		t.Fatalf("weixin URL suggestions missing default endpoint: %#v", weixinURL.Suggestions)
	}

	_, _, lansengerURL := findConfigEntryForTest(t, m, "lansenger_gateway_url")
	if !containsString(lansengerURL.Suggestions, "https://apigw.lx.qianxin.com") {
		t.Fatalf("lansenger gateway suggestions missing default endpoint: %#v", lansengerURL.Suggestions)
	}
}

func TestConfigFields_SuggestionsOpenChooserBeforeManualEdit(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Custom")
	m := NewConfigModel("zh")
	m.LoadFromAppConfig(cfg)
	tab, idx, entry := findVisibleConfigEntryForTest(t, m, "maclaw_llm_url")
	if len(entry.Suggestions) == 0 {
		t.Fatal("expected maclaw_llm_url to have quick-fill suggestions")
	}

	m.activeTab = tab
	m.cursor = idx
	m, _ = m.updateNormal(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.selectMode || !m.selectSuggestions || m.editing {
		t.Fatalf("Enter on a suggested text field should open chooser, editing=%v selectMode=%v selectSuggestions=%v", m.editing, m.selectMode, m.selectSuggestions)
	}
}

func TestConfigFields_SpaceCyclesSuggestionsAndSaves(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Custom")
	m := NewConfigModel("zh")
	m.LoadFromAppConfig(cfg)
	tab, idx, entry := findVisibleConfigEntryForTest(t, m, "maclaw_llm_url")
	if len(entry.Suggestions) == 0 {
		t.Fatal("expected maclaw_llm_url to have quick-fill suggestions")
	}

	m.activeTab = tab
	m.cursor = idx
	m, cmd := m.updateNormal(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("Space on a suggested field should return a save command")
	}
	if got := m.tabs[tab][idx].Value; got != entry.Suggestions[0] {
		t.Fatalf("Space set value %q, want first suggestion %q", got, entry.Suggestions[0])
	}
	msg := cmd()
	save, ok := msg.(ConfigSaveMsg)
	if !ok {
		t.Fatalf("Space command returned %T, want ConfigSaveMsg", msg)
	}
	if save.Key != "maclaw_llm_url" || save.Value != entry.Suggestions[0] {
		t.Fatalf("save message = %#v, want key maclaw_llm_url value %q", save, entry.Suggestions[0])
	}
}

func TestConfigFields_IMChannelProfileSelectsOneChannel(t *testing.T) {
	cfg := corelib.AppConfig{WeixinEnabled: true, TelegramBotEnabled: true, QQBotEnabled: true, LansengerEnabled: true}
	ApplyConfigValue(&cfg, "im_channel_profile", "telegram")

	if cfg.WeixinEnabled || cfg.QQBotEnabled || cfg.LansengerEnabled {
		t.Fatalf("selecting telegram should disable other IM channels: %#v", cfg)
	}
	if !cfg.TelegramBotEnabled {
		t.Fatal("selecting telegram should enable Telegram")
	}
	got, _ := LoadConfigValue(&cfg, "im_channel_profile")
	if got != "telegram" {
		t.Fatalf("im_channel_profile = %q", got)
	}
}

func TestConfigFields_IMChannelProfileDetectsCustomMultiChannel(t *testing.T) {
	cfg := corelib.AppConfig{WeixinEnabled: true, TelegramBotEnabled: true}
	got, _ := LoadConfigValue(&cfg, "im_channel_profile")
	if got != "custom" {
		t.Fatalf("im_channel_profile = %q, want custom", got)
	}
}

func TestConfigFields_IMTabHidesDisabledChannelsByDefault(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabIM
	m.cursor = 99
	m.LoadFromAppConfig(corelib.AppConfig{})

	entries := m.currentEntries()
	if !visibleConfigEntryExists(entries, "im_channel_profile") {
		t.Fatal("IM profile selector should remain visible")
	}
	for _, hidden := range []string{"qqbot_enabled", "telegram_bot_token", "weixin_base_url", "lansenger_gateway_url"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden when IM profile is off", hidden)
		}
	}
	if m.cursor >= len(entries) {
		t.Fatalf("cursor should be clamped into visible IM entries, cursor=%d entries=%d", m.cursor, len(entries))
	}
}

func TestConfigFields_IMTabShowsSelectedChannelOnly(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabIM
	m.LoadFromAppConfig(corelib.AppConfig{TelegramBotEnabled: true})

	entries := m.currentEntries()
	for _, visible := range []string{"im_channel_profile", "telegram_bot_enabled", "telegram_bot_token"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should be visible for telegram IM profile", visible)
		}
	}
	for _, hidden := range []string{"qqbot_app_id", "weixin_base_url", "lansenger_gateway_url"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should stay hidden for telegram IM profile", hidden)
		}
	}
}

func TestConfigFields_IMTabHidesEmptyReadOnlyWeixinToken(t *testing.T) {
	m := NewConfigModel("en")
	m.activeTab = CfgTabIM
	m.LoadFromAppConfig(corelib.AppConfig{WeixinEnabled: true})

	entries := m.currentEntries()
	for _, visible := range []string{"im_channel_profile", "weixin_enabled", "weixin_base_url"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should remain visible for WeChat setup", visible)
		}
	}
	if visibleConfigEntryExists(entries, "weixin_token") {
		t.Fatalf("empty read-only weixin_token should be hidden until QR binding creates a token: %#v", entries)
	}

	m.LoadFromAppConfig(corelib.AppConfig{WeixinEnabled: true, WeixinToken: "wx-token"})
	if !visibleConfigEntryExists(m.currentEntries(), "weixin_token") {
		t.Fatal("weixin_token should be visible once a token exists")
	}
}

func TestConfigFields_IMTabShowsAllChannelsForCustomProfile(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabIM
	m.LoadFromAppConfig(corelib.AppConfig{WeixinEnabled: true, TelegramBotEnabled: true})

	entries := m.currentEntries()
	for _, visible := range []string{"qqbot_enabled", "telegram_bot_enabled", "weixin_enabled", "lansenger_enabled"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should be visible for custom IM profile", visible)
		}
	}
	_, _, profile := findConfigEntryForTest(t, m, "im_channel_profile")
	if !containsString(profile.Options, "custom") {
		t.Fatalf("custom should be shown as current dynamic IM profile option: %#v", profile.Options)
	}
}

func selectConfigOptionForTest(t *testing.T, m ConfigModel, value string) ConfigModel {
	t.Helper()
	entries := m.currentEntries()
	if m.cursor >= len(entries) {
		t.Fatalf("cursor %d outside entries %d", m.cursor, len(entries))
	}
	for i, opt := range entries[m.cursor].Options {
		if opt == value {
			m.selectCursor = i
			return m
		}
	}
	t.Fatalf("option %q not found for %s: %#v", value, entries[m.cursor].Key, entries[m.cursor].Options)
	return m
}

func visibleConfigEntryExists(entries []ConfigEntry, key string) bool {
	for _, entry := range entries {
		if entry.Key == key {
			return true
		}
	}
	return false
}

func TestConfigFields_GeneralHidesEmptyRemoteInternals(t *testing.T) {
	m := NewConfigModel("en")
	m.activeTab = CfgTabGeneral
	m.LoadFromAppConfig(corelib.AppConfig{})

	entries := m.currentEntries()
	_, _, hubCenter := findVisibleConfigEntryForTest(t, m, "hubcenter_url")
	if got := configOptionDisplay(hubCenter.Key, hubCenter.Value, m.lang); got != remote.DefaultRemoteHubCenterURL {
		t.Fatalf("empty HubCenter should display default URL, got %q", got)
	}
	for _, visible := range []string{"setup_status", "hubcenter_url"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should stay visible on a fresh machine", visible)
		}
	}
	for _, hidden := range []string{"hub_url", "token"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden until setup has a value to show", hidden)
		}
	}

	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:       "https://selected-hub.example",
		RemoteMachineToken: "machine-token",
	})
	entries = m.currentEntries()
	for _, visible := range []string{"hub_url", "token"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should be visible once setup has a value to inspect", visible)
		}
	}
}

func TestConfigFields_SetupStatusGuidesNewMachine(t *testing.T) {
	cases := []struct {
		name string
		cfg  corelib.AppConfig
		want string
	}{
		{name: "empty", want: "needs_setup"},
		{name: "hub ready", cfg: corelib.AppConfig{RemoteHubURL: "https://hub.example", RemoteViewerToken: "viewer-token"}, want: "needs_redeem"},
		{name: "custom llm without mcp", cfg: corelib.AppConfig{MaclawLLMUrl: "http://localhost:11434/v1", MaclawLLMModel: "qwen2.5-coder:32b"}, want: "mcp_optional"},
		{name: "custom llm on lan without key", cfg: corelib.AppConfig{MaclawLLMUrl: "http://192.168.1.20:11434/v1", MaclawLLMModel: "qwen2.5-coder:32b", MaclawLLMCurrentProvider: "Custom"}, want: "mcp_optional"},
		{name: "custom llm on docker host without key", cfg: corelib.AppConfig{MaclawLLMUrl: "host.docker.internal:1234/v1", MaclawLLMModel: "local-model", MaclawLLMCurrentProvider: "Custom"}, want: "mcp_optional"},
		{name: "custom llm with mcp", cfg: corelib.AppConfig{MaclawLLMUrl: "http://localhost:11434/v1", MaclawLLMModel: "qwen2.5-coder:32b", LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}}}, want: "llm_ready"},
		{name: "keyed preset missing key", cfg: corelib.AppConfig{MaclawLLMUrl: "https://api.openai.com/v1", MaclawLLMModel: "gpt-4o", MaclawLLMCurrentProvider: "OpenAI API Key"}, want: "needs_llm_key"},
		{name: "keyed preset with key", cfg: corelib.AppConfig{MaclawLLMUrl: "https://api.openai.com/v1", MaclawLLMModel: "gpt-4o", MaclawLLMKey: "sk-test", MaclawLLMCurrentProvider: "OpenAI API Key"}, want: "mcp_optional"},
		{name: "saved cloud provider missing auth type without key", cfg: corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMModel: "cloud-model", MaclawLLMCurrentProvider: "Corp Gateway", MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: "Corp Gateway", URL: "https://llm.example/v1", Model: "cloud-model"}}}, want: "needs_llm_key"},
		{name: "saved lan provider missing auth type without key", cfg: corelib.AppConfig{MaclawLLMUrl: "http://10.0.0.2:1234/v1", MaclawLLMModel: "local-model", MaclawLLMCurrentProvider: "Local Gateway", MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: "Local Gateway", URL: "http://10.0.0.2:1234/v1", Model: "local-model"}}}, want: "mcp_optional"},
		{name: "saved cloud provider missing auth type with key", cfg: corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMModel: "cloud-model", MaclawLLMKey: "sk-test", MaclawLLMCurrentProvider: "Corp Gateway", MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: "Corp Gateway", URL: "https://llm.example/v1", Model: "cloud-model"}}}, want: "mcp_optional"},
		{name: "saved provider key fallback", cfg: corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMModel: "cloud-model", MaclawLLMCurrentProvider: "Corp Gateway", MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: "Corp Gateway", URL: "https://llm.example/v1", Key: "sk-provider", Model: "cloud-model", AuthType: "apikey"}}}, want: "mcp_optional"},
		{name: "official llm without mcp", cfg: corelib.AppConfig{RemoteViewerToken: "viewer-token", MaclawLLMUrl: "https://hub.example/api/llm", MaclawLLMModel: "auto", MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName, MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: serviceRedeemOfficialProviderName, IsHubService: true}}}, want: "mcp_optional"},
		{name: "official llm with mcp", cfg: corelib.AppConfig{RemoteViewerToken: "viewer-token", MaclawLLMUrl: "https://hub.example/api/llm", MaclawLLMModel: "auto", MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName, MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: serviceRedeemOfficialProviderName, IsHubService: true}}, MCPServers: []corelib.MCPServerEntry{{Name: "remote"}}}, want: "official_ready"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewConfigModel("zh")
			m.LoadFromAppConfig(tc.cfg)
			_, _, entry := findConfigEntryForTest(t, m, "setup_status")
			if entry.Value != tc.want {
				t.Fatalf("setup_status = %q, want %q", entry.Value, tc.want)
			}
			if !entry.ReadOnly {
				t.Fatal("setup_status should be read-only")
			}
		})
	}
}

func TestConfigFields_LLMSyncPreservesCurrentProviderKeyFallback(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMModel:           "cloud-model",
		MaclawLLMCurrentProvider: "Corp Gateway",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "Corp Gateway",
			URL:      "https://llm.example/v1",
			Key:      "sk-provider",
			Model:    "cloud-model",
			AuthType: "apikey",
		}},
	}

	ApplyConfigValue(&cfg, "maclaw_llm_model", "cloud-model-v2")

	if cfg.MaclawLLMKey != "sk-provider" {
		t.Fatalf("flat key = %q, want provider key fallback", cfg.MaclawLLMKey)
	}
	if got := cfg.MaclawLLMProviders[0].Key; got != "sk-provider" {
		t.Fatalf("provider key = %q, want preserved provider key", got)
	}
}

func findVisibleConfigEntryForTest(t *testing.T, m ConfigModel, key string) (int, int, ConfigEntry) {
	t.Helper()
	for tab := 0; tab < CfgTabCount; tab++ {
		m.activeTab = tab
		for i, e := range m.currentEntries() {
			if e.Key == key {
				return tab, i, e
			}
		}
	}
	t.Fatalf("visible config entry %q not found", key)
	return 0, 0, ConfigEntry{}
}

func findConfigEntryForTest(t *testing.T, m ConfigModel, key string) (int, int, ConfigEntry) {
	t.Helper()
	for tab := 0; tab < CfgTabCount; tab++ {
		for i, e := range m.tabs[tab] {
			if e.Key == key {
				return tab, i, e
			}
		}
	}
	t.Fatalf("config entry %q not found", key)
	return 0, 0, ConfigEntry{}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestConfigFields_WorkDirProfileAppliesCommonPaths(t *testing.T) {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	cfg := corelib.AppConfig{WorkingDirectory: "/tmp/custom-workdir"}
	ApplyConfigValue(&cfg, "working_directory_profile", "default_workspace")
	if cfg.WorkingDirectory != "" {
		t.Fatalf("default workspace profile should clear custom path, got %q", cfg.WorkingDirectory)
	}
	got, _ := LoadConfigValue(&cfg, "working_directory_profile")
	if got != "default_workspace" {
		t.Fatalf("working_directory_profile = %q, want default_workspace", got)
	}

	ApplyConfigValue(&cfg, "working_directory_profile", "current_directory")
	if !sameFilePath(cfg.WorkingDirectory, cwd) {
		t.Fatalf("current directory profile = %q, want %q", cfg.WorkingDirectory, cwd)
	}
	got, _ = LoadConfigValue(&cfg, "working_directory_profile")
	if got != "current_directory" {
		t.Fatalf("working_directory_profile = %q, want current_directory", got)
	}

	ApplyConfigValue(&cfg, "working_directory_profile", "home_directory")
	if !sameFilePath(cfg.WorkingDirectory, home) {
		t.Fatalf("home directory profile = %q, want %q", cfg.WorkingDirectory, home)
	}
	got, _ = LoadConfigValue(&cfg, "working_directory_profile")
	if got != "home_directory" {
		t.Fatalf("working_directory_profile = %q, want home_directory", got)
	}
}

func TestConfigFields_WorkDirProfileHidesPathUntilCustom(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabGeneral
	m.LoadFromAppConfig(corelib.AppConfig{})

	entries := m.currentEntries()
	if !visibleConfigEntryExists(entries, "working_directory_profile") {
		t.Fatal("working directory profile selector should remain visible")
	}
	if visibleConfigEntryExists(entries, "working_directory") {
		t.Fatal("working_directory path should be hidden for default profile")
	}

	m.LoadFromAppConfig(corelib.AppConfig{WorkingDirectory: "/tmp/custom-workdir"})
	entries = m.currentEntries()
	if !visibleConfigEntryExists(entries, "working_directory") {
		t.Fatal("working_directory path should be visible for custom profile")
	}
}

func TestConfigFields_WorkDirProfileCustomShowsPathImmediately(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabGeneral
	m.LoadFromAppConfig(corelib.AppConfig{})
	_, idx, _ := findVisibleConfigEntryForTest(t, m, "working_directory_profile")
	m.cursor = idx
	m, _ = m.updateNormal(tea.KeyMsg{Type: tea.KeyEnter})
	m = selectConfigOptionForTest(t, m, "custom")
	m, cmd := m.updateSelect(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command after selecting custom workdir")
	}
	if !visibleConfigEntryExists(m.currentEntries(), "working_directory") {
		t.Fatal("custom workdir path should appear immediately")
	}
}

func TestConfigFields_DataDirDisplaysResolvedPath(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", "/tmp/maclaw-test-data")

	m := NewConfigModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{})

	_, _, dataDir := findConfigEntryForTest(t, m, "data_dir")
	if dataDir.Value != "/tmp/maclaw-test-data" {
		t.Fatalf("data_dir value = %q", dataDir.Value)
	}
	if !dataDir.ReadOnly {
		t.Fatal("data_dir should be read-only")
	}
}

func TestConfigFields_VisibilityRefreshesImmediatelyAfterSelectorChange(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabIM
	m.LoadFromAppConfig(corelib.AppConfig{})
	_, idx, _ := findVisibleConfigEntryForTest(t, m, "im_channel_profile")
	m.cursor = idx
	m, _ = m.updateNormal(tea.KeyMsg{Type: tea.KeyEnter})
	m = selectConfigOptionForTest(t, m, "telegram")
	m, cmd := m.updateSelect(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command after selecting telegram")
	}
	if !visibleConfigEntryExists(m.currentEntries(), "telegram_bot_token") {
		t.Fatal("telegram fields should appear immediately after selecting Telegram")
	}
	if visibleConfigEntryExists(m.currentEntries(), "weixin_base_url") {
		t.Fatal("unselected IM channel fields should remain hidden immediately")
	}

	m.activeTab = CfgTabProxy
	m.cursor = 0
	m.LoadFromAppConfig(corelib.AppConfig{})
	_, idx, _ = findVisibleConfigEntryForTest(t, m, "default_proxy_profile")
	m.cursor = idx
	m, _ = m.updateNormal(tea.KeyMsg{Type: tea.KeyEnter})
	m = selectConfigOptionForTest(t, m, "custom")
	m, cmd = m.updateSelect(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command after selecting custom proxy")
	}
	if !visibleConfigEntryExists(m.currentEntries(), "default_proxy_host") {
		t.Fatal("custom proxy fields should appear immediately")
	}
}

func TestConfigFields_SaveMessageCarriesMaterializedConfig(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Custom")
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(cfg)
	_, idx, _ := findVisibleConfigEntryForTest(t, m, "maclaw_llm_provider_preset")
	m.cursor = idx
	m, _ = m.updateNormal(tea.KeyMsg{Type: tea.KeyEnter})
	m = selectConfigOptionForTest(t, m, "Ollama Local")
	_, cmd := m.updateSelect(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg, ok := cmd().(ConfigSaveMsg)
	if !ok {
		t.Fatalf("save command returned %T", cmd())
	}
	if !msg.HasConfig {
		t.Fatal("save message should carry full config snapshot")
	}
	if msg.Config.MaclawLLMUrl != "http://localhost:11434/v1" || msg.Config.MaclawLLMModel != "qwen2.5-coder:32b" {
		t.Fatalf("snapshot did not include provider preset details: url=%q model=%q", msg.Config.MaclawLLMUrl, msg.Config.MaclawLLMModel)
	}
	if msg.Config.MaclawLLMKey != "" {
		t.Fatalf("local provider snapshot should clear key, got %q", msg.Config.MaclawLLMKey)
	}
}

func TestConfigFields_LLMVisibilityRefreshesImmediatelyAfterProviderChange(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Custom")
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(cfg)
	if !visibleConfigEntryExists(m.currentEntries(), "maclaw_llm_url") {
		t.Fatal("custom LLM URL should start visible")
	}

	_, idx, _ := findVisibleConfigEntryForTest(t, m, "maclaw_llm_provider_preset")
	m.cursor = idx
	m, _ = m.updateNormal(tea.KeyMsg{Type: tea.KeyEnter})
	m = selectConfigOptionForTest(t, m, "Ollama Local")
	m, cmd := m.updateSelect(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected save command after selecting local provider")
	}
	entries := m.currentEntries()
	if visibleConfigEntryExists(entries, "maclaw_llm_url") || visibleConfigEntryExists(entries, "maclaw_llm_key") {
		t.Fatalf("preset local provider should hide URL and key immediately: %#v", entries)
	}
	_, _, model := findConfigEntryForTest(t, m, "maclaw_llm_model")
	if model.Value != "qwen2.5-coder:32b" {
		t.Fatalf("local config model = %q", model.Value)
	}
}

func TestConfigFields_DefaultProviderKeyPopulatesEndpoint(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_key", "secret")
	if cfg.MaclawLLMUrl == "" || cfg.MaclawLLMModel == "" {
		t.Fatalf("saving key on empty config should materialize default provider endpoint/model: url=%q model=%q", cfg.MaclawLLMUrl, cfg.MaclawLLMModel)
	}
	if cfg.MaclawLLMCurrentProvider == "" || cfg.MaclawLLMCurrentProvider == "Custom" {
		t.Fatalf("current provider = %q, want concrete preset", cfg.MaclawLLMCurrentProvider)
	}
}

func TestConfigFields_LLMTabStartsEmptyConfigOnPresetPath(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(corelib.AppConfig{})

	_, _, provider := findConfigEntryForTest(t, m, "maclaw_llm_provider_preset")
	if provider.Value == "Custom" {
		t.Fatal("empty LLM config should start on a provider choice, not custom advanced setup")
	}
	entries := m.currentEntries()
	if !visibleConfigEntryExists(entries, "maclaw_llm_key") {
		t.Fatal("default API provider path should expose the key field")
	}
	if visibleConfigEntryExists(entries, "maclaw_llm_url") {
		t.Fatal("empty LLM config should not expose raw URL before Custom is selected")
	}
}

func TestConfigFields_LLMTabHidesAdvancedFieldsForPreset(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Ollama Local")
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(cfg)

	entries := m.currentEntries()
	for _, visible := range []string{"maclaw_llm_provider_preset", "maclaw_llm_model_choice"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should remain visible for preset LLM setup", visible)
		}
	}
	for _, hidden := range []string{"maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_protocol", "maclaw_llm_context_length"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden for local preset LLM setup", hidden)
		}
	}
}

func TestConfigFields_LLMTabShowsKeyForAPIKeyPreset(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Zhipu GLM Lobster")
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(cfg)

	entries := m.currentEntries()
	if !visibleConfigEntryExists(entries, "maclaw_llm_key") {
		t.Fatal("API-key provider should still expose the LLM key field")
	}
	for _, hidden := range []string{"maclaw_llm_url", "maclaw_llm_protocol", "maclaw_llm_context_length"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden for preset API provider", hidden)
		}
	}
}

func TestConfigFields_LLMTabShowsAdvancedFieldsForCustomProvider(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Custom")
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(cfg)

	entries := m.currentEntries()
	for _, visible := range []string{"maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model", "maclaw_llm_protocol", "maclaw_llm_context_length"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should be visible for custom LLM setup", visible)
		}
	}
}

func TestConfigFields_AuxLLMProfileHidesDetailsByDefault(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(corelib.AppConfig{})

	entries := m.currentEntries()
	if !visibleConfigEntryExists(entries, "aux_llm_profile") {
		t.Fatal("auxiliary LLM profile should remain visible")
	}
	for _, hidden := range []string{"aux_llm_url", "aux_llm_key", "aux_llm_model", "aux_llm_protocol"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden when auxiliary LLM profile is off", hidden)
		}
	}
}

func TestConfigFields_AuxLLMProfileSameAsPrimaryCopiesPrimary(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMUrl:      "https://llm.example/v1/",
		MaclawLLMKey:      "primary-key",
		MaclawLLMModel:    "primary-model",
		MaclawLLMProtocol: "anthropic",
	}
	ApplyConfigValue(&cfg, "aux_llm_profile", "same_as_primary")

	if cfg.AuxiliaryLLM.URL != "https://llm.example/v1" || cfg.AuxiliaryLLM.Key != "primary-key" || cfg.AuxiliaryLLM.Model != "primary-model" || cfg.AuxiliaryLLM.Protocol != "anthropic" {
		t.Fatalf("auxiliary LLM did not copy primary config: %#v", cfg.AuxiliaryLLM)
	}
	got, _ := LoadConfigValue(&cfg, "aux_llm_profile")
	if got != "same_as_primary" {
		t.Fatalf("aux_llm_profile = %q, want same_as_primary", got)
	}
}

func TestConfigFields_AuxLLMProfileSameAsPrimaryUsesProviderKeyFallback(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "Corp Gateway",
		MaclawLLMUrl:             "https://llm.example/v1/",
		MaclawLLMModel:           "primary-model",
		MaclawLLMProtocol:        "openai",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:  "Corp Gateway",
			URL:   "https://llm.example/v1",
			Key:   "provider-key",
			Model: "primary-model",
		}},
	}
	ApplyConfigValue(&cfg, "aux_llm_profile", "same_as_primary")

	if cfg.AuxiliaryLLM.Key != "provider-key" {
		t.Fatalf("auxiliary LLM key = %q, want provider key fallback", cfg.AuxiliaryLLM.Key)
	}
	got, _ := LoadConfigValue(&cfg, "aux_llm_profile")
	if got != "same_as_primary" {
		t.Fatalf("aux_llm_profile = %q, want same_as_primary", got)
	}
}

func TestConfigFields_AuxLLMProfileCustomShowsDetails(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabLLM
	m.LoadFromAppConfig(corelib.AppConfig{AuxiliaryLLM: corelib.AuxiliaryLLMConfig{URL: "https://aux.example/v1", Model: "aux-model", Protocol: "openai"}})

	entries := m.currentEntries()
	for _, visible := range []string{"aux_llm_profile", "aux_llm_url", "aux_llm_key", "aux_llm_model", "aux_llm_protocol"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should be visible for custom auxiliary LLM", visible)
		}
	}
}

func TestConfigFields_LocalLLMPresetsAreSelectable(t *testing.T) {
	m := NewConfigModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{})

	_, _, provider := findConfigEntryForTest(t, m, "maclaw_llm_provider_preset")
	for _, want := range []string{"Ollama Local", "LM Studio Local"} {
		if !containsString(provider.Options, want) {
			t.Fatalf("provider options missing %q: %#v", want, provider.Options)
		}
	}
}

func TestConfigFields_LocalLLMPresetFillsEndpointAndClearsKey(t *testing.T) {
	cfg := corelib.AppConfig{MaclawLLMKey: "old-key"}
	ApplyConfigValue(&cfg, "maclaw_llm_provider_preset", "Ollama Local")

	if cfg.MaclawLLMCurrentProvider != "Ollama Local" {
		t.Fatalf("current provider = %q", cfg.MaclawLLMCurrentProvider)
	}
	if cfg.MaclawLLMUrl != "http://localhost:11434/v1" {
		t.Fatalf("LLM URL = %q", cfg.MaclawLLMUrl)
	}
	if cfg.MaclawLLMModel != "qwen2.5-coder:32b" {
		t.Fatalf("LLM model = %q", cfg.MaclawLLMModel)
	}
	if cfg.MaclawLLMKey != "" {
		t.Fatalf("local preset should clear API key, got %q", cfg.MaclawLLMKey)
	}
	if cfg.MaclawLLMProtocol != "openai" {
		t.Fatalf("LLM protocol = %q", cfg.MaclawLLMProtocol)
	}
}

func TestConfigFields_ProxyProfileAppliesCommonLocalProxy(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "default_proxy_profile", "local_http_7890")

	if !cfg.DefaultProxyEnabled {
		t.Fatal("proxy should be enabled by local proxy profile")
	}
	if cfg.DefaultProxyProtocol != "http" || cfg.DefaultProxyHost != "127.0.0.1" || cfg.DefaultProxyPort != "7890" {
		t.Fatalf("proxy = %s://%s:%s", cfg.DefaultProxyProtocol, cfg.DefaultProxyHost, cfg.DefaultProxyPort)
	}
	if !cfg.DefaultProxyScopeMaclaw || !cfg.DefaultProxyScopeAgent {
		t.Fatal("local proxy profile should enable both LLM and Agent scopes")
	}
	got, _ := LoadConfigValue(&cfg, "default_proxy_profile")
	if got != "local_http_7890" {
		t.Fatalf("proxy profile = %q", got)
	}
}

func TestConfigFields_ProxyTabHidesDetailsWhenOffOrPreset(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabProxy
	m.LoadFromAppConfig(corelib.AppConfig{})
	entries := m.currentEntries()
	if !visibleConfigEntryExists(entries, "default_proxy_profile") {
		t.Fatal("proxy profile should remain visible")
	}
	for _, hidden := range []string{"default_proxy_host", "default_proxy_port", "default_proxy_password"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden when proxy profile is off", hidden)
		}
	}

	m.LoadFromAppConfig(corelib.AppConfig{DefaultProxyEnabled: true, DefaultProxyProtocol: "http", DefaultProxyHost: "127.0.0.1", DefaultProxyPort: "7890"})
	entries = m.currentEntries()
	for _, hidden := range []string{"default_proxy_host", "default_proxy_port", "default_proxy_password"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden for a common local proxy preset", hidden)
		}
	}
}

func TestConfigFields_ProxyTabShowsDetailsForCustomProxy(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabProxy
	m.LoadFromAppConfig(corelib.AppConfig{DefaultProxyEnabled: true, DefaultProxyProtocol: "https", DefaultProxyHost: "proxy.example", DefaultProxyPort: "8443"})
	entries := m.currentEntries()
	for _, visible := range []string{"default_proxy_profile", "default_proxy_protocol", "default_proxy_host", "default_proxy_port"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should be visible for custom proxy", visible)
		}
	}
	_, _, profile := findConfigEntryForTest(t, m, "default_proxy_profile")
	if !containsString(profile.Options, "custom") {
		t.Fatalf("custom should be available for existing custom proxy: %#v", profile.Options)
	}
}

func TestConfigFields_ProxyProfileCustomEnablesManualFields(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "default_proxy_profile", "custom")
	if !cfg.DefaultProxyEnabled {
		t.Fatal("custom proxy profile should enable proxy editing")
	}
	got, _ := LoadConfigValue(&cfg, "default_proxy_profile")
	if got != "custom" {
		t.Fatalf("proxy profile = %q, want custom", got)
	}
}

func TestConfigFields_LLMModelQuickPickIncludesSavedModels(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMModel: "current-model",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "Saved", Model: "saved-model"},
		},
	}
	m := NewConfigModel("zh")
	m.LoadFromAppConfig(cfg)

	_, _, entry := findConfigEntryForTest(t, m, "maclaw_llm_model_choice")
	for _, want := range []string{"auto", "current-model", "saved-model", "qwen2.5-coder:32b"} {
		if !containsString(entry.Options, want) {
			t.Fatalf("model quick-pick options missing %q: %#v", want, entry.Options)
		}
	}
}

func TestConfigFields_LLMModelQuickPickUpdatesCurrentProvider(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "Custom",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:  "Custom",
			URL:   "https://llm.example/v1",
			Model: "old-model",
		}},
	}
	ApplyConfigValue(&cfg, "maclaw_llm_model_choice", "deepseek-coder-v2")

	if cfg.MaclawLLMModel != "deepseek-coder-v2" {
		t.Fatalf("MaclawLLMModel = %q", cfg.MaclawLLMModel)
	}
	if cfg.MaclawLLMProviders[0].Model != "deepseek-coder-v2" {
		t.Fatalf("provider model = %q", cfg.MaclawLLMProviders[0].Model)
	}
}

func TestConfigFields_LoadFromAppConfigMaterializesStandardSecurityDefaults(t *testing.T) {
	m := NewConfigModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{})

	if m.cfg.SecurityPolicyMode != "standard" || m.cfg.SandboxMode != "none" || m.cfg.NetworkLevel != "full" {
		t.Fatalf("implicit standard security defaults not materialized: %#v", m.cfg)
	}
	if !m.cfg.YoloModeAllowed || !m.cfg.FileOutboundEnabled || !m.cfg.ImageOutboundEnabled {
		t.Fatalf("implicit standard security booleans not materialized: %#v", m.cfg)
	}
	_, _, profile := findConfigEntryForTest(t, m, "security_profile")
	if profile.Value != "standard" {
		t.Fatalf("security_profile = %q, want standard", profile.Value)
	}
}

func TestConfigFields_SecurityProfileAppliesStrictAndHidesDetails(t *testing.T) {
	cfg := corelib.AppConfig{}
	ApplyConfigValue(&cfg, "security_profile", "strict")

	if cfg.SecurityPolicyMode != "strict" || cfg.SandboxMode != "os" || cfg.NetworkLevel != "intranet" {
		t.Fatalf("strict security profile not applied: %#v", cfg)
	}
	if cfg.YoloModeAllowed || cfg.FileOutboundEnabled || cfg.ImageOutboundEnabled {
		t.Fatalf("strict security profile should disable risky outbound flags: %#v", cfg)
	}
	got, _ := LoadConfigValue(&cfg, "security_profile")
	if got != "strict" {
		t.Fatalf("security_profile = %q, want strict", got)
	}

	m := NewConfigModel("zh")
	m.activeTab = CfgTabSecurity
	m.LoadFromAppConfig(cfg)
	entries := m.currentEntries()
	if !visibleConfigEntryExists(entries, "security_profile") {
		t.Fatal("security profile selector should remain visible")
	}
	for _, hidden := range []string{"security_policy_mode", "sandbox_mode", "network_level", "file_outbound_enabled"} {
		if visibleConfigEntryExists(entries, hidden) {
			t.Fatalf("%s should be hidden for strict security profile", hidden)
		}
	}
}

func TestConfigFields_SecurityProfileCustomShowsDetails(t *testing.T) {
	m := NewConfigModel("zh")
	m.activeTab = CfgTabSecurity
	m.LoadFromAppConfig(corelib.AppConfig{SecurityPolicyMode: "standard", SandboxMode: "docker", NetworkLevel: "full", YoloModeAllowed: true, FileOutboundEnabled: true, ImageOutboundEnabled: true})

	entries := m.currentEntries()
	for _, visible := range []string{"security_profile", "security_policy_mode", "sandbox_mode", "network_level", "yolo_mode_allowed", "file_outbound_enabled", "image_outbound_enabled"} {
		if !visibleConfigEntryExists(entries, visible) {
			t.Fatalf("%s should be visible for custom security profile", visible)
		}
	}
}

func TestConfigViewShowsSetupHint(t *testing.T) {
	m := NewConfigModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{})

	view := m.View()
	if !strings.Contains(view, "下一步") {
		t.Fatalf("config view should show setup next-step hint, got: %q", view)
	}
}

func TestConfigOptionDisplayLocalizesOfficialProvider(t *testing.T) {
	if got := configOptionDisplay("maclaw_llm_provider_preset", serviceRedeemOfficialProviderName, "en"); got != "MaClaw Official" {
		t.Fatalf("English provider display = %q", got)
	}
	if got := configOptionDisplay("maclaw_llm_provider_preset", serviceRedeemOfficialProviderName, "zh"); got != "MaClaw 官方" {
		t.Fatalf("Chinese provider display = %q", got)
	}
}
