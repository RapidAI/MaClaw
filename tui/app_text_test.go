package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTUITextServiceMessagesFollowLanguage(t *testing.T) {
	en := tuiText("en", "serviceRedeemSuccessChat")
	if !strings.Contains(en, "Service redeem succeeded") {
		t.Fatalf("English redeem text = %q", en)
	}
	if strings.Contains(en, "兑换") || strings.Contains(en, "默认") {
		t.Fatalf("English redeem text should not contain Chinese: %q", en)
	}

	zh := tuiText("zh", "serviceRedeemSuccessChat")
	if !strings.Contains(zh, "服务兑换成功") {
		t.Fatalf("Chinese redeem text = %q", zh)
	}
}

func TestTUISkillDownloadGuidanceFollowsLanguage(t *testing.T) {
	if got := tuiText("en", "skillBuiltInDownloadGuidance"); !strings.Contains(got, "prefer the built-in") || strings.Contains(got, "通用") {
		t.Fatalf("English download guidance = %q", got)
	}
	if got := tuiText("en", "skillGenericDownloadCaution"); !strings.Contains(got, "generic download Skill") || strings.Contains(got, "提示") {
		t.Fatalf("English download caution = %q", got)
	}
	if got := tuiText("zh", "skillBuiltInDownloadGuidance"); !strings.Contains(got, "download_file") || got == tuiText("en", "skillBuiltInDownloadGuidance") {
		t.Fatalf("Chinese download guidance = %q", got)
	}
}

func TestTUIOfficeReadProviderUsesPersistedPolicyAndRefreshesImmediately(t *testing.T) {
	for _, key := range []string{
		"MACLAW_OFFICE_READ_ENGINE",
		"MACLAW_OFFICE_READ_FORMATS",
		"MACLAW_OFFICE_READ_FALLBACK",
		"MACLAW_OFFICE_READ_EMIT_MARKDOWN",
	} {
		t.Setenv(key, "")
	}

	dataDir := t.TempDir()
	store := commands.NewFileConfigStore(dataDir)
	fallback := false
	emitMarkdown := true
	if err := store.SaveConfig(corelib.AppConfig{
		OfficeReadEngine:       "officeread",
		OfficeReadFormats:      []string{"doc", "docx"},
		OfficeReadFallback:     &fallback,
		OfficeReadEmitMarkdown: &emitMarkdown,
	}); err != nil {
		t.Fatalf("save initial OfficeRead policy: %v", err)
	}
	restore := installTUIOfficeReadConfigProvider(dataDir)
	defer restore()

	policy := agent.CurrentOfficeReadRuntimePolicy()
	if policy.Engine != agent.OfficeExtractEngineOfficeRead || !reflect.DeepEqual(policy.Formats, []string{"doc", "docx"}) || policy.Fallback || !policy.EmitMarkdown {
		t.Fatalf("initial TUI OfficeRead policy = %#v", policy)
	}

	if err := store.SaveConfig(corelib.AppConfig{OfficeReadEngine: "legacy", OfficeReadFormats: []string{"doc"}}); err != nil {
		t.Fatalf("save global rollback policy: %v", err)
	}
	policy = agent.CurrentOfficeReadRuntimePolicy()
	if policy.Engine != agent.OfficeExtractEngineLegacy || !reflect.DeepEqual(policy.Formats, []string{"doc"}) || !policy.Fallback || policy.EmitMarkdown {
		t.Fatalf("updated TUI OfficeRead policy = %#v", policy)
	}
}
func TestTUISearchResultInstalledUsesSourceAwareIdentity(t *testing.T) {
	tests := []struct {
		name      string
		result    skill.HubSearchResult
		installed corelib.NLSkillEntry
	}{
		{
			name:      "skillhub hub id",
			result:    skill.HubSearchResult{ID: "hub-123", Source: "skillhub"},
			installed: corelib.NLSkillEntry{Name: "Installed Hub", Source: "hub", HubSkillID: "hub-123"},
		},
		{
			name:      "clawhub slug trigger",
			result:    skill.HubSearchResult{ID: "weather", Name: "Weather Assistant", Source: "clawhub"},
			installed: corelib.NLSkillEntry{Name: "Weather Assistant", Source: "clawhub", Triggers: []string{"weather"}},
		},
		{
			name:      "github repo url",
			result:    skill.HubSearchResult{Name: "acme/weather · SKILL.md", Source: "github", RepoURL: "https://github.com/acme/weather"},
			installed: corelib.NLSkillEntry{Name: "Weather", Source: "github", SourceProject: "https://github.com/acme/weather"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, gotName := tuiSearchResultInstalled(tt.result, []corelib.NLSkillEntry{tt.installed})
			if !ok || gotName != tt.installed.Name {
				t.Fatalf("tuiSearchResultInstalled() = %v, %q; want true, %q", ok, gotName, tt.installed.Name)
			}
		})
	}
}

func TestTUISearchResultInstalledDoesNotMatchConflictingStableIdentity(t *testing.T) {
	tests := []struct {
		name      string
		result    skill.HubSearchResult
		installed corelib.NLSkillEntry
	}{
		{
			name:      "clawhub same name different slug",
			result:    skill.HubSearchResult{ID: "new-weather", Name: "Weather", Source: "clawhub"},
			installed: corelib.NLSkillEntry{Name: "Weather", Source: "clawhub", Triggers: []string{"old-weather"}},
		},
		{
			name:      "github same name different repo",
			result:    skill.HubSearchResult{Name: "Weather", Source: "github", RepoURL: "https://github.com/new/weather"},
			installed: corelib.NLSkillEntry{Name: "Weather", Source: "github", SourceProject: "https://github.com/old/weather"},
		},
		{
			name:      "skillhub conflicting id same skill id",
			result:    skill.HubSearchResult{ID: "new-id", SkillID: "acme.weather", Source: "skillhub"},
			installed: corelib.NLSkillEntry{Name: "Weather", Source: "hub", HubSkillID: "old-id", SkillID: "acme.weather"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ok, name := tuiSearchResultInstalled(tt.result, []corelib.NLSkillEntry{tt.installed}); ok {
				t.Fatalf("tuiSearchResultInstalled() unexpectedly matched %q", name)
			}
		})
	}
}

func TestTUIInstalledSkillForInstallRequestUsesSourceLocators(t *testing.T) {
	githubRef := `{"repo_url":"https://github.com/acme/weather"}`
	tests := []struct {
		name       string
		source     string
		skillID    string
		installRef string
		installed  corelib.NLSkillEntry
	}{
		{
			name: "clawhub slug", source: "clawhub", skillID: "weather",
			installed: corelib.NLSkillEntry{Name: "Weather Assistant", Source: "clawhub", Triggers: []string{"weather"}},
		},
		{
			name: "github repository", source: "github", skillID: "acme/weather", installRef: githubRef,
			installed: corelib.NLSkillEntry{Name: "Weather", Source: "github", SourceProject: "https://github.com/acme/weather"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ok := tuiInstalledSkillForInstallRequest(tt.source, tt.skillID, tt.installRef, []corelib.NLSkillEntry{tt.installed})
			if !ok || name != tt.installed.Name {
				t.Fatalf("tuiInstalledSkillForInstallRequest() = %q, %v; want %q, true", name, ok, tt.installed.Name)
			}
		})
	}
}

func TestTUIInstalledSkillEntryForInstallRequestUsesSourceLocators(t *testing.T) {
	githubRef := `{"repo_url":"https://github.com/acme/weather"}`
	installed := corelib.NLSkillEntry{Name: "Weather", Source: "github", SourceProject: "https://github.com/acme/weather"}

	got, ok := tuiInstalledSkillEntryForInstallRequest("github", "acme/weather", githubRef, []corelib.NLSkillEntry{installed})
	if !ok || got.Name != installed.Name {
		t.Fatalf("tuiInstalledSkillEntryForInstallRequest() = %#v, %v; want %#v, true", got, ok, installed)
	}
}

func TestToolOperationResultReachesToolView(t *testing.T) {
	m := &tuiModel{app: &TUIApp{appConfig: corelib.AppConfig{Language: "en"}}, root: views.NewRootModel("en")}
	updated, cmd := m.Update(views.ToolOperationResultMsg{Tab: views.ToolSubSkill, Success: false, Message: "install failed"})
	if cmd != nil {
		t.Fatalf("tool result command = %v, want nil", cmd)
	}
	got := updated.(*tuiModel)
	if !strings.Contains(got.root.Tools.View(), "install failed") {
		t.Fatalf("tool result was not rendered in tool view:\n%s", got.root.Tools.View())
	}
}

func TestHubMissingTextExplainsAutomaticHubSelection(t *testing.T) {
	en := tuiText("en", "hubURLMissing")
	if !strings.Contains(en, "Hub is not activated") || !strings.Contains(en, "selected automatically") {
		t.Fatalf("English Hub missing text should guide Setup, got %q", en)
	}
	zh := tuiText("zh", "hubURLMissing")
	if !strings.Contains(zh, "Hub 未激活") || !strings.Contains(zh, "自动选择") {
		t.Fatalf("Chinese Hub missing text should guide Setup, got %q", zh)
	}
}

func TestTUIConfigLangDefaultsToChinese(t *testing.T) {
	if got := tuiConfigLang(corelib.AppConfig{}); got != "zh" {
		t.Fatalf("default lang = %q", got)
	}
	if got := tuiConfigLang(corelib.AppConfig{Language: "en"}); got != "en" {
		t.Fatalf("configured lang = %q", got)
	}
}

func TestTUIFormatUsesLocalizedTemplate(t *testing.T) {
	got := tuiFormat("en", "hubActivationFailed", "bad token")
	if got != "Hub activation failed: bad token" {
		t.Fatalf("formatted text = %q", got)
	}
}

func TestTUIWeixinQRStatusMessageLocalizesScannedState(t *testing.T) {
	got := tuiWeixinQRStatusMessage("en", weixin.QRLoginStatusScanned, nil)
	if !strings.Contains(got, "Confirm on your phone") {
		t.Fatalf("English scanned message = %q", got)
	}
	zh := tuiWeixinQRStatusMessage("zh", weixin.QRLoginStatusScanned, nil)
	if zh == "scaned" || zh == got {
		t.Fatalf("Chinese scanned message should be localized, got %q", zh)
	}
}

func TestTUIWeixinQRStatusTerminalFailures(t *testing.T) {
	for _, status := range []string{"error", "failed", "cancelled"} {
		if !tuiWeixinQRStatusIsTerminal(status) {
			t.Fatalf("status %q should be terminal", status)
		}
	}
	if tuiWeixinQRStatusIsTerminal("wait") {
		t.Fatal("wait should not be terminal")
	}
}

func TestTUIWeixinQREmptyTextIsLocalized(t *testing.T) {
	if got := tuiText("en", "weixinQREmpty"); !strings.Contains(got, "incomplete") {
		t.Fatalf("English empty QR text = %q", got)
	}
	if got := tuiText("zh", "weixinQREmpty"); got == "weixinQREmpty" || got == tuiText("en", "weixinQREmpty") {
		t.Fatalf("Chinese empty QR text = %q", got)
	}
}

func TestTUIFormatInstalledFromUsesGoPositionalTemplate(t *testing.T) {
	got := tuiFormat("zh", "installedFrom", "Ai Coding Tools Full Suite", "ClawHub")
	if strings.Contains(got, "%!") {
		t.Fatalf("installedFrom should not leak fmt errors: %q", got)
	}
	if !strings.Contains(got, "ClawHub") || !strings.Contains(got, "Ai Coding Tools Full Suite") {
		t.Fatalf("installedFrom should include source and skill name: %q", got)
	}
}

func TestTUITextTemplatesUseGoFmtSyntax(t *testing.T) {
	unsupportedPositional := regexp.MustCompile(`%[0-9]+\$[a-zA-Z]`)
	for _, path := range []string{
		"app_text.go",
		filepath.Join("views", "tool_status.go"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if match := unsupportedPositional.FindIndex(data); match != nil {
			line := 1 + strings.Count(string(data[:match[0]]), "\n")
			t.Fatalf("%s:%d uses unsupported positional fmt syntax %q; use Go fmt form like %%[2]s", path, line, data[match[0]:match[1]])
		}
	}
}

func TestTUITextCoversAppKeys(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	re := regexp.MustCompile(`tui(?:Text|Format)\([^,\n]+,\s*"([^"]+)"`)
	keys := make(map[string]bool)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatalf("read %s: %v", file.Name(), err)
		}
		for _, match := range re.FindAllStringSubmatch(string(data), -1) {
			keys[match[1]] = true
		}
	}
	for key := range keys {
		if got := tuiText("en", key); got == key {
			t.Fatalf("English text missing for key %q", key)
		}
		if got := tuiText("zh", key); got == key {
			t.Fatalf("Chinese text missing for key %q", key)
		}
	}
}

func TestTUIRegistrationMethodMismatchTextsFollowLanguage(t *testing.T) {
	if got := tuiText("en", "regRequiresPhone"); !strings.Contains(strings.ToLower(got), "phone") {
		t.Fatalf("English phone-registration guidance = %q", got)
	}
	if got := tuiText("zh", "regRequiresPhone"); !strings.Contains(got, "手机") {
		t.Fatalf("Chinese phone-registration guidance = %q", got)
	}
}

func TestTUIConfigNavigationTextsFollowLanguage(t *testing.T) {
	enSetup := tuiText("en", "configOpenSetup")
	if !strings.Contains(enSetup, "Opened Setup") {
		t.Fatalf("English setup navigation text = %q", enSetup)
	}
	if strings.Contains(enSetup, "初始化") || strings.Contains(enSetup, "服务兑换") {
		t.Fatalf("English setup navigation text should not contain Chinese: %q", enSetup)
	}

	zhSetup := tuiText("zh", "configOpenSetup")
	if zhSetup == "configOpenSetup" || !strings.Contains(zhSetup, "初始化") {
		t.Fatalf("Chinese setup navigation text = %q", zhSetup)
	}

	zhRedeem := tuiText("zh", "configOpenRedeem")
	if zhRedeem == "configOpenRedeem" || !strings.Contains(zhRedeem, "服务兑换") {
		t.Fatalf("Chinese redeem navigation text = %q", zhRedeem)
	}
}

func TestIncompleteRemoteActivationTextUsesTUISetup(t *testing.T) {
	en := tuiFormat("en", "incompleteRemoteActivate", "maclaw")
	if !strings.Contains(en, "maclaw-tui setup") || strings.Contains(en, "remote activate") {
		t.Fatalf("English incomplete remote text should guide to TUI setup, got %q", en)
	}
	zh := tuiFormat("zh", "incompleteRemoteActivate", "maclaw")
	if !strings.Contains(zh, "maclaw-tui setup") || strings.Contains(zh, "remote activate") {
		t.Fatalf("Chinese incomplete remote text should guide to TUI setup, got %q", zh)
	}
}

func TestTUIRemoteActivationIncompleteRequiresViewerToken(t *testing.T) {
	complete := corelib.AppConfig{
		RemoteEmail:        "user@example.com",
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "machine-token",
		RemoteViewerToken:  "viewer-token",
	}
	if tuiRemoteActivationIncomplete(complete) {
		t.Fatal("remote activation with viewer token should be complete")
	}
	missingViewer := complete
	missingViewer.RemoteViewerToken = ""
	if !tuiRemoteActivationIncomplete(missingViewer) {
		t.Fatal("remote activation without viewer token should be incomplete")
	}
	noEmail := missingViewer
	noEmail.RemoteEmail = ""
	if tuiRemoteActivationIncomplete(noEmail) {
		t.Fatal("empty remote email should not force incomplete setup")
	}
}

func TestTuiBtwSystemPromptFollowsLanguage(t *testing.T) {
	app := &TUIApp{appConfig: corelib.AppConfig{Language: "en"}}
	prompt := buildTuiBtwSystemPrompt(app, "status")
	if !strings.Contains(prompt, "/btw Side Query Mode") {
		t.Fatalf("English /btw prompt missing English mode text:\n%s", prompt)
	}
	if strings.Contains(prompt, "侧查询模式") || strings.Contains(prompt, "用户信息") {
		t.Fatalf("English /btw prompt should not contain Chinese UI prompt text:\n%s", prompt)
	}
	if strings.Contains(prompt, "自动召回") || strings.Contains(prompt, agent.KnowledgeAutoRecallHeader) {
		t.Fatalf("/btw must not dump warehouse bodies:\n%s", prompt)
	}
}

func TestTUIProviderDisplayNameLocalizesOfficialProvider(t *testing.T) {
	if got := tuiProviderDisplayName("en", tuiHubServiceProviderName); got != "MaClaw Official" {
		t.Fatalf("English provider display = %q", got)
	}
	if got := tuiProviderDisplayName("zh", tuiHubServiceProviderName); got != "MaClaw 官方" {
		t.Fatalf("Chinese provider display = %q", got)
	}
	if got := tuiModelDisplayLabel("en", tuiHubServiceProviderName, "auto"); got != "MaClaw Official / auto" {
		t.Fatalf("model label = %q", got)
	}
	if got := tuiModelDisplayLabel("en", tuiHubServiceProviderName, ""); got != "MaClaw Official" {
		t.Fatalf("empty-model label = %q", got)
	}
}

func TestApplyTUIHubLLMServiceStatusConfiguresOfficialDefault(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "Custom", URL: "http://localhost:11434/v1"},
			{Name: tuiHubServiceProviderName, URL: "https://old.example/v1"},
			{Name: tuiHubServiceProviderName, URL: "https://duplicate.example/v1"},
		},
	}

	applyTUIHubLLMServiceStatusToConfig(&cfg, tuiHubLLMServiceStatus{
		HubLLMBaseURL: "https://hub.example/api/llm/",
		DefaultModel:  "qwen-max",
	})

	if cfg.MaclawLLMCurrentProvider != tuiHubServiceProviderName {
		t.Fatalf("current provider = %q", cfg.MaclawLLMCurrentProvider)
	}
	if cfg.MaclawLLMUrl != "https://hub.example/api/llm" {
		t.Fatalf("LLM URL = %q", cfg.MaclawLLMUrl)
	}
	if cfg.MaclawLLMKey != "viewer-token" || cfg.MaclawLLMModel != "qwen-max" || cfg.MaclawLLMProtocol != "openai" {
		t.Fatalf("official LLM config not applied: key=%q model=%q protocol=%q", cfg.MaclawLLMKey, cfg.MaclawLLMModel, cfg.MaclawLLMProtocol)
	}
	if len(cfg.MaclawLLMProviders) != 2 || cfg.MaclawLLMProviders[0].Name != tuiHubServiceProviderName {
		t.Fatalf("providers should be official-first and deduplicated: %#v", cfg.MaclawLLMProviders)
	}
	if !cfg.MaclawLLMProviders[0].IsHubService {
		t.Fatal("official provider should be marked as Hub service")
	}
}

func TestBuildLLMConfigUsesViewerTokenForSavedOfficialService(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: tuiHubServiceProviderName,
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "qwen-max",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: tuiHubServiceProviderName, IsHubService: true}},
	}
	llm := buildLLMConfigFromAppConfig(cfg)
	if llm.Key != "viewer-token" {
		t.Fatalf("official LLM key = %q, want viewer token fallback", llm.Key)
	}
	if !tuiConfigLLMReady(cfg) {
		t.Fatal("official LLM with viewer token should be treated as ready")
	}
}

func TestBuildLLMConfigMigratesZhipuCodingDefault(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: corelib.ZhipuCodingProviderName,
		MaclawLLMUrl:             "https://open.bigmodel.cn/api/anthropic",
		MaclawLLMModel:           "GLM-5.2",
		MaclawLLMProtocol:        "anthropic",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: corelib.ZhipuCodingProviderName, URL: "https://open.bigmodel.cn/api/anthropic", Model: "GLM-5.2", Protocol: "anthropic",
		}},
	}
	llm := buildLLMConfigFromAppConfig(cfg)
	if llm.Model != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("model = %q, want migrated %q", llm.Model, corelib.ZhipuCodingDefaultModel)
	}

	cfg.MaclawLLMModel = ""
	llm = buildLLMConfigFromAppConfig(cfg)
	if llm.Model != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("empty flat model = %q, want migrated from provider %q", llm.Model, corelib.ZhipuCodingDefaultModel)
	}
}

func TestBuildLLMConfigCanonicalizesMojibakeOfficialProvider(t *testing.T) {
	mojibakeHubName := "MaClaw\u7039\u6a3b\u67df"
	cfg := corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: mojibakeHubName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: mojibakeHubName, URL: "https://hub.example/v1", Key: "", Model: "auto", IsHubService: true, TimeoutSec: 33},
		},
	}

	llm := buildLLMConfigFromAppConfig(cfg)
	if llm.ProviderName != tuiHubServiceProviderName {
		t.Fatalf("ProviderName = %q, want %q", llm.ProviderName, tuiHubServiceProviderName)
	}
	if llm.Key != "viewer-token" {
		t.Fatalf("Key = %q, want viewer token fallback", llm.Key)
	}
	if llm.TimeoutSec != 33 {
		t.Fatalf("TimeoutSec = %d, want provider-specific timeout", llm.TimeoutSec)
	}
}

func TestBuildLLMConfigDoesNotUseViewerTokenForNonCurrentOfficialProvider(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: "Corp Gateway",
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMModel:           "cloud-model",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: tuiHubServiceProviderName, IsHubService: true, Key: "viewer-token", Model: "auto"},
			{Name: "Corp Gateway", URL: "https://llm.example/v1", Key: "", Model: "cloud-model", TimeoutSec: 44},
		},
	}

	llm := buildLLMConfigFromAppConfig(cfg)
	if llm.ProviderName != "Corp Gateway" {
		t.Fatalf("ProviderName = %q, want Corp Gateway", llm.ProviderName)
	}
	if llm.Key != "" {
		t.Fatalf("Key = %q, want empty because current provider is not official", llm.Key)
	}
	if llm.TimeoutSec != 44 {
		t.Fatalf("TimeoutSec = %d, want current provider timeout", llm.TimeoutSec)
	}
}

func TestBuildLLMConfigUsesCurrentProviderKeyFallback(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "Corp Gateway",
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMModel:           "cloud-model",
		MaclawLLMTimeoutSec:      300,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:       "Corp Gateway",
			URL:        "https://llm.example/v1",
			Key:        "sk-provider",
			Model:      "cloud-model",
			AuthType:   "apikey",
			TimeoutSec: 510,
		}},
	}
	llm := buildLLMConfigFromAppConfig(cfg)
	if llm.Key != "sk-provider" {
		t.Fatalf("LLM key = %q, want provider key fallback", llm.Key)
	}
	if llm.TimeoutSec != 510 {
		t.Fatalf("LLM timeout = %d, want current provider timeout", llm.TimeoutSec)
	}
	if !tuiConfigLLMReady(cfg) {
		t.Fatal("provider key fallback should make the LLM ready")
	}
}

func TestBuildLLMConfigAppliesGlobalThinkingMode(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMThinkingMode: "disabled",
		MaclawLLMUrl:          "https://api.deepseek.com/v1",
		MaclawLLMKey:          "test-key",
		MaclawLLMModel:        "deepseek-reasoner",
	}
	if got := buildLLMConfigFromAppConfig(cfg).ThinkingMode; got != "disabled" {
		t.Fatalf("ThinkingMode = %q, want disabled", got)
	}
}

func TestBuildLLMConfigUsesJWTForLegacyCodexOAuthProvider(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "OpenAI",
		MaclawLLMKey:             "sk-legacy-platform-key",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:             "OpenAI",
			URL:              "https://chatgpt.com/backend-api/codex",
			Key:              "sk-legacy-platform-key",
			OAuthAccessToken: "eyJhbGciOiJub25lIn0.payload.sig",
			AuthType:         "oauth",
			Model:            "gpt-5.6-luna",
		}},
	}

	if got := buildLLMConfigFromAppConfig(cfg).Key; got != "eyJhbGciOiJub25lIn0.payload.sig" {
		t.Fatalf("LLM key = %q, want raw OAuth JWT", got)
	}
}

func TestSimpleChatMessagesFiltersAndLimitsHistory(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "tool", Content: "ignored"},
		{Role: "user", Content: "old"},
	}
	for i := 0; i < 22; i++ {
		history = append(history, agent.ConversationEntry{Role: "assistant", Content: fmt.Sprintf("reply-%02d", i)})
	}

	messages := simpleChatMessages(history, "now")
	if len(messages) != 21 {
		t.Fatalf("messages len = %d, want 21", len(messages))
	}
	first := messages[0].(map[string]interface{})
	if first["content"] != "reply-02" {
		t.Fatalf("first history content = %#v, want reply-02", first["content"])
	}
	last := messages[len(messages)-1].(map[string]interface{})
	if last["role"] != "user" || last["content"] != "now" {
		t.Fatalf("last message = %#v, want user now", last)
	}
}

func TestSlashHelpDoesNotAdvertiseMemoryDump(t *testing.T) {
	help := tuiText("en", "slashHelp")
	if strings.Contains(help, "/memory") {
		t.Fatalf("slash help should not advertise memory dump in simplified TUI:\n%s", help)
	}
	if !strings.Contains(help, "mcp shows the MCP list") || !strings.Contains(help, "template choices when empty") {
		t.Fatalf("slash help should describe MCP list/template behavior:\n%s", help)
	}
	for _, want := range []string{"/setup", "/redeem", "/chat", "/tools", "/mcp", "/skill", "/tasks", "/schedule", "/config", "/status", "/health", "/llm", "/security"} {
		if !strings.Contains(help, want) {
			t.Fatalf("slash help should advertise %s navigation:\n%s", want, help)
		}
	}
	if !strings.Contains(help, "/loop <cmd> <goal>") || !strings.Contains(help, "--max N") {
		t.Fatalf("slash help should advertise /loop usage:\n%s", help)
	}
}

func TestSlashHelpTextLocalizesLoopUsage(t *testing.T) {
	help := tuiText("zh", "slashHelp")
	if !strings.Contains(help, "/loop <命令> <目标>") || !strings.Contains(help, "目标驱动验证循环") || !strings.Contains(help, "--max 轮数") || !strings.Contains(help, "--dir 路径") {
		t.Fatalf("Chinese slash help should localize /loop usage:\n%s", help)
	}
	if strings.Contains(help, "/loop <cmd> <goal>") || strings.Contains(help, "Goal-driven verification loop") || strings.Contains(help, "--dir path") {
		t.Fatalf("Chinese slash help should not keep English /loop copy:\n%s", help)
	}
}

func TestPrintUsageStartsWithTUIQuickStart(t *testing.T) {
	oldStderr := os.Stderr
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	os.Stderr = f
	printUsage()
	os.Stderr = oldStderr
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek usage output: %v", err)
	}
	outBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read usage: %v", err)
	}
	_ = f.Close()
	out := string(outBytes)
	for _, want := range []string{"快速开始", "maclaw-tui tui", "maclaw-tui setup", "maclaw-tui onboarding", "maclaw-tui redeem", "maclaw-tui config", "maclaw-tui status"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage should include %q in quick start:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "tui [页面]   显式打开完整 TUI；可加 setup/redeem/config/tools/tasks/mcp 直达") {
		t.Fatalf("quick start should advertise explicit tui/ui routes:\n%s", out)
	}
	if !strings.Contains(out, "setup [邮箱] 首次设置：邮箱 + HubCenter 自动选择 Hub（可加 llm/mcp/security/redeem 直达）") {
		t.Fatalf("quick start setup should advertise direct setup targets:\n%s", out)
	}
	if !strings.Contains(out, "onboarding cli") {
		t.Fatalf("usage should keep the scripted onboarding escape hatch:\n%s", out)
	}
	if !strings.Contains(out, "无参数/setup 打开 TUI LLM 设置") || !strings.Contains(out, "setup cli") {
		t.Fatalf("usage should explain llm opens TUI settings and keeps a CLI escape hatch:\n%s", out)
	}
	if !strings.Contains(out, "maclaw-tui config       打开完整 TUI 设置页") {
		t.Fatalf("quick start should route config to full TUI settings:\n%s", out)
	}
	if !strings.Contains(out, "status       打开 TUI 状态总览") || !strings.Contains(out, "doctor/health") || !strings.Contains(out, "--text/--json") {
		t.Fatalf("usage should advertise TUI status/doctor aliases and script status output:\n%s", out)
	}
	if !strings.Contains(out, "无参数打开完整 TUI 设置") {
		t.Fatalf("config command help should explain no-arg full TUI settings:\n%s", out)
	}
	if !strings.Contains(out, "llm/security/proxy/im/advanced 直达设置页") {
		t.Fatalf("config command help should advertise direct TUI setting tabs:\n%s", out)
	}
	if !strings.Contains(out, "settings      打开完整 TUI 的设置页（可加 llm/security/proxy/im/advanced 直达）") {
		t.Fatalf("settings help should advertise direct setting tabs:\n%s", out)
	}
	if !strings.Contains(out, "tools         ") || !strings.Contains(out, "skill/mcp") {
		t.Fatalf("tools help should advertise direct tool sub-tabs:\n%s", out)
	}
	if !strings.Contains(out, "tasks         ") || !strings.Contains(out, "remote/background/schedule") {
		t.Fatalf("tasks help should advertise direct task sub-tabs:\n%s", out)
	}
	if !strings.Contains(out, "setup 打开初始化") || !strings.Contains(out, "ui 打开独立设置页") {
		t.Fatalf("config command help should distinguish setup onboarding from legacy config ui:\n%s", out)
	}
	if !strings.Contains(out, "setup         打开完整 TUI 的初始化页（可跟邮箱预填；可加 llm/mcp/security/redeem 直达）") {
		t.Fatalf("setup help should advertise direct setup targets:\n%s", out)
	}
	if !strings.Contains(out, "redeem        打开完整 TUI 的服务兑换页（可跟兑换码预填；别名：service；可加 setup/llm 跳到相关页）") {
		t.Fatalf("redeem help should advertise related direct routes:\n%s", out)
	}
	if !strings.Contains(out, "loop          后台任务管理（无参数打开 TUI 后台任务页") {
		t.Fatalf("loop help should route no-arg loop to TUI background tasks:\n%s", out)
	}
	if !strings.Contains(out, "mcp           MCP 管理（无配置时打开模板选择，已有配置时查看列表；remote 进入远程模板") {
		t.Fatalf("usage should route no-arg mcp to the TUI MCP template chooser:\n%s", out)
	}
	if !strings.Contains(out, "remote        远程模式管理（无参数打开 TUI 初始化") || !strings.Contains(out, "Hub URL 注册后自动选择") {
		t.Fatalf("remote help should explain no-arg TUI setup and display-only Hub URL:\n%s", out)
	}
	for _, want := range []string{
		"memory        兼容脚本记忆命令（无参数打开聊天页；记忆在 TUI 后台自动维护）",
		"schedule      定时任务管理（无参数打开 TUI 任务页",
		"audit         兼容审计命令（无参数打开服务兑换；list 查看本地审计日志）",
		"policy        安全策略配置（无参数打开 TUI 安全设置",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage should explain simplified no-arg local command routing, missing %q:\n%s", want, out)
		}
	}
	for _, want := range []string{
		"tool          工具管理（无参数打开 TUI Tools",
		"skill         技能管理（无参数打开 TUI Skill",
		"skillhub      SkillHub 市场（无参数打开 TUI Skill",
		"skillmarket   SkillMarket 商店（无参数打开 TUI Skill",
		"nlskill       NL 技能管理（无参数打开 TUI Skill",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage should route no-arg tool/skill commands to TUI Tools, missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "memory        记忆管理") || strings.Contains(out, "audit         审计日志查询") {
		t.Fatalf("usage should not present memory/audit as primary TUI pages:\n%s", out)
	}
	if strings.Index(out, "快速开始") > strings.Index(out, "daemon") {
		t.Fatalf("quick start should appear before advanced commands:\n%s", out)
	}
}

func TestConfigCommandRoutingPrefersFullTUI(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want configCommandMode
	}{
		{name: "no args", want: configCommandOpenSettings},
		{name: "llm tab", args: []string{"llm"}, want: configCommandOpenSettings},
		{name: "security tab", args: []string{"security"}, want: configCommandOpenSettings},
		{name: "proxy tab", args: []string{"proxy"}, want: configCommandOpenSettings},
		{name: "setup", args: []string{"setup"}, want: configCommandOpenSetup},
		{name: "legacy standalone ui", args: []string{"ui"}, want: configCommandOpenStandaloneUI},
		{name: "scripted get", args: []string{"get", "--local"}, want: configCommandRunCLI},
		{name: "scripted set", args: []string{"set", "--local", "llm", "model", "auto"}, want: configCommandRunCLI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyConfigCommand(tc.args); got != tc.want {
				t.Fatalf("classifyConfigCommand(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestConfigCommandInitialTabs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
		ok   bool
	}{
		{name: "no args", want: views.CfgTabGeneral, ok: true},
		{name: "general", args: []string{"general"}, want: views.CfgTabGeneral, ok: true},
		{name: "llm", args: []string{"llm"}, want: views.CfgTabLLM, ok: true},
		{name: "security", args: []string{"security"}, want: views.CfgTabSecurity, ok: true},
		{name: "policy alias", args: []string{"policy"}, want: views.CfgTabSecurity, ok: true},
		{name: "proxy", args: []string{"proxy"}, want: views.CfgTabProxy, ok: true},
		{name: "im", args: []string{"im"}, want: views.CfgTabIM, ok: true},
		{name: "advanced", args: []string{"advanced"}, want: views.CfgTabAdvanced, ok: true},
		{name: "scripted get", args: []string{"get"}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := configCommandInitialTab(tc.args)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("configCommandInitialTab(%v) = (%d, %v), want (%d, %v)", tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestConfigCLIActionKnownAvoidsHubTokenForTypos(t *testing.T) {
	for _, args := range [][]string{{"get"}, {"set"}, {"export"}, {"import"}, {"schema"}} {
		if !configCLIActionKnown(args) {
			t.Fatalf("configCLIActionKnown(%v) = false, want true", args)
		}
	}
	for _, args := range [][]string{nil, {}, {"typo"}, {"llm"}} {
		if configCLIActionKnown(args) {
			t.Fatalf("configCLIActionKnown(%v) = true, want false", args)
		}
	}
}

func TestConfigNeedsHubCredentialsOnlyForRemoteGetSet(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"get"}, want: true},
		{args: []string{"set", "foo", "bar"}, want: true},
		{args: []string{"get", "--local"}, want: false},
		{args: []string{"set", "--local", "llm", "model", "auto"}, want: false},
		{args: []string{"export"}, want: false},
		{args: []string{"schema"}, want: false},
		{args: []string{"typo"}, want: false},
	}
	for _, tc := range cases {
		if got := configNeedsHubCredentials(tc.args); got != tc.want {
			t.Fatalf("configNeedsHubCredentials(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestConfigUsesLocalStoreAvoidsHubForLocalScripts(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"export"}, want: true},
		{args: []string{"import", "-f", "config.json"}, want: true},
		{args: []string{"schema"}, want: true},
		{args: []string{"get", "--local"}, want: true},
		{args: []string{"set", "--local", "llm", "model", "auto"}, want: true},
		{args: []string{"get"}, want: false},
		{args: []string{"set", "foo", "bar"}, want: false},
		{args: []string{"typo"}, want: false},
	}
	for _, tc := range cases {
		if got := configUsesLocalStore(tc.args); got != tc.want {
			t.Fatalf("configUsesLocalStore(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestToolsPageInitialSubTabs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
		ok   bool
	}{
		{name: "no args", want: views.ToolSubSkill, ok: true},
		{name: "skill", args: []string{"skill"}, want: views.ToolSubSkill, ok: true},
		{name: "skillhub", args: []string{"skillhub"}, want: views.ToolSubSkill, ok: true},
		{name: "mcp", args: []string{"mcp"}, want: views.ToolSubMCP, ok: true},
		{name: "mcp remote", args: []string{"mcp", "remote"}, want: views.ToolSubMCP, ok: true},
		{name: "unknown falls back", args: []string{"unknown"}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toolsPageInitialSubTab(tc.args)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("toolsPageInitialSubTab(%v) = (%d, %v), want (%d, %v)", tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestTasksPageInitialSubTabs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
		ok   bool
	}{
		{name: "no args", want: views.TaskSubRemote, ok: true},
		{name: "remote", args: []string{"remote"}, want: views.TaskSubRemote, ok: true},
		{name: "background", args: []string{"background"}, want: views.TaskSubBackground, ok: true},
		{name: "loop alias", args: []string{"loop"}, want: views.TaskSubBackground, ok: true},
		{name: "schedule", args: []string{"schedule"}, want: views.TaskSubScheduled, ok: true},
		{name: "cron alias", args: []string{"cron"}, want: views.TaskSubScheduled, ok: true},
		{name: "unknown falls back", args: []string{"unknown"}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tasksPageInitialSubTab(tc.args)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("tasksPageInitialSubTab(%v) = (%d, %v), want (%d, %v)", tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestTUICommandStartupRoutes(t *testing.T) {
	cases := []struct {
		name            string
		args            []string
		wantOK          bool
		wantTab         []int
		email           string
		code            string
		mcpMode         string
		wantStatusFocus bool
	}{
		{name: "no args opens full tui", wantOK: true},
		{name: "setup email", args: []string{"setup", "USER@Example.COM"}, wantOK: true, wantTab: []int{views.TabOnboarding}, email: "user@example.com"},
		{name: "remote email", args: []string{"remote", "USER@Example.COM"}, wantOK: true, wantTab: []int{views.TabOnboarding}, email: "user@example.com"},
		{name: "redeem code", args: []string{"redeem", "ABC", "123"}, wantOK: true, wantTab: []int{views.TabServiceRedeem}, code: "ABC123"},
		{name: "config llm", args: []string{"config", "llm"}, wantOK: true, wantTab: []int{views.TabConfig, views.CfgTabLLM}},
		{name: "status", args: []string{"status"}, wantOK: true, wantTab: []int{views.TabConfig, views.CfgTabGeneral}, wantStatusFocus: true},
		{name: "doctor", args: []string{"doctor"}, wantOK: true, wantTab: []int{views.TabConfig, views.CfgTabGeneral}, wantStatusFocus: true},
		{name: "health", args: []string{"health"}, wantOK: true, wantTab: []int{views.TabConfig, views.CfgTabGeneral}, wantStatusFocus: true},
		{name: "security shortcut", args: []string{"security"}, wantOK: true, wantTab: []int{views.TabConfig, views.CfgTabSecurity}},
		{name: "setup mcp defaults local when empty", args: []string{"setup", "mcp"}, wantOK: true, wantTab: []int{views.TabTools, views.ToolSubMCP}, mcpMode: mcpAddModeAutoLocal},
		{name: "tools mcp defaults local when empty", args: []string{"tools", "mcp"}, wantOK: true, wantTab: []int{views.TabTools, views.ToolSubMCP}, mcpMode: mcpAddModeAutoLocal},
		{name: "tools mcp remote", args: []string{"tools", "mcp", "remote"}, wantOK: true, wantTab: []int{views.TabTools, views.ToolSubMCP}, mcpMode: "remote"},
		{name: "mcp defaults local when empty", args: []string{"mcp"}, wantOK: true, wantTab: []int{views.TabTools, views.ToolSubMCP}, mcpMode: mcpAddModeAutoLocal},
		{name: "mcp local", args: []string{"mcp", "local"}, wantOK: true, wantTab: []int{views.TabTools, views.ToolSubMCP}, mcpMode: "local"},
		{name: "tasks background", args: []string{"tasks", "background"}, wantOK: true, wantTab: []int{views.TabTasks, views.TaskSubBackground}},
		{name: "unknown falls back to full tui", args: []string{"not-a-page"}, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tuiCommandStartup(tc.args)
			if ok != tc.wantOK {
				t.Fatalf("tuiCommandStartup(%v) ok = %v, want %v", tc.args, ok, tc.wantOK)
			}
			if !sameIntSlice(got.forceInitialTab, tc.wantTab) {
				t.Fatalf("forceInitialTab = %#v, want %#v", got.forceInitialTab, tc.wantTab)
			}
			if got.onboardingEmail != tc.email || got.serviceRedeemCode != tc.code || got.mcpAddMode != tc.mcpMode {
				t.Fatalf("startup extras = email %q/code %q/mcp %q, want %q/%q/%q",
					got.onboardingEmail, got.serviceRedeemCode, got.mcpAddMode, tc.email, tc.code, tc.mcpMode)
			}
			if got.focusSetupStatus != tc.wantStatusFocus {
				t.Fatalf("focusSetupStatus = %v, want %v", got.focusSetupStatus, tc.wantStatusFocus)
			}
		})
	}
}

func TestStatusCommandKeepsTUIDefaultButAllowsScriptOutput(t *testing.T) {
	if statusCommandUsesCLI(nil) {
		t.Fatal("status with no flags should keep opening the TUI overview")
	}
	if !statusCommandUsesCLI([]string{"--text"}) || !statusCommandUsesCLI([]string{"--json"}) {
		t.Fatal("status --text/--json should use script output")
	}
	if statusCommandUsesCLI([]string{"setup"}) {
		t.Fatal("unrecognized status args should not silently switch away from the TUI default")
	}
}

func sameIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSetupInitialRoutes(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantTab int
		wantSub int
		ok      bool
	}{
		{name: "no args", wantTab: views.TabOnboarding, wantSub: -1, ok: true},
		{name: "hub", args: []string{"hub"}, wantTab: views.TabOnboarding, wantSub: -1, ok: true},
		{name: "redeem", args: []string{"redeem"}, wantTab: views.TabServiceRedeem, wantSub: -1, ok: true},
		{name: "llm", args: []string{"llm"}, wantTab: views.TabConfig, wantSub: views.CfgTabLLM, ok: true},
		{name: "security", args: []string{"security"}, wantTab: views.TabConfig, wantSub: views.CfgTabSecurity, ok: true},
		{name: "proxy", args: []string{"proxy"}, wantTab: views.TabConfig, wantSub: views.CfgTabProxy, ok: true},
		{name: "mcp", args: []string{"mcp"}, wantTab: views.TabTools, wantSub: views.ToolSubMCP, ok: true},
		{name: "skill", args: []string{"skill"}, wantTab: views.TabTools, wantSub: views.ToolSubSkill, ok: true},
		{name: "schedule", args: []string{"schedule"}, wantTab: views.TabTasks, wantSub: views.TaskSubScheduled, ok: true},
		{name: "unknown falls back", args: []string{"unknown"}, wantTab: views.TabOnboarding, wantSub: -1, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tab, sub, ok := setupInitialRoute(tc.args)
			if ok != tc.ok || tab != tc.wantTab || sub != tc.wantSub {
				t.Fatalf("setupInitialRoute(%v) = (%d, %d, %v), want (%d, %d, %v)",
					tc.args, tab, sub, ok, tc.wantTab, tc.wantSub, tc.ok)
			}
		})
	}
}

func TestOnboardingEmailArgsPrefillSetup(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{name: "valid email", args: []string{"USER@Example.COM"}, want: "user@example.com", ok: true},
		{name: "setup route", args: []string{"llm"}, ok: false},
		{name: "invalid email", args: []string{"not-an-email"}, ok: false},
		{name: "multiple args", args: []string{"user@example.com", "extra"}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := onboardingEmailFromArgs(tc.args)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("onboardingEmailFromArgs(%v) = (%q, %v), want (%q, %v)", tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSetupEmailArgsPrefillCommonAliases(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{name: "direct setup email", args: []string{"USER@Example.COM"}, want: "user@example.com", ok: true},
		{name: "setup alias email", args: []string{"setup", "USER@Example.COM"}, want: "user@example.com", ok: true},
		{name: "setup email flag", args: []string{"--email", "USER@Example.COM"}, want: "user@example.com", ok: true},
		{name: "setup alias email flag", args: []string{"setup", "--email", "USER@Example.COM"}, want: "user@example.com", ok: true},
		{name: "remote alias email", args: []string{"remote", "USER@Example.COM"}, want: "user@example.com", ok: true},
		{name: "hub alias email", args: []string{"hub", "USER@Example.COM"}, want: "user@example.com", ok: true},
		{name: "llm route stays route", args: []string{"llm", "USER@Example.COM"}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := setupEmailFromArgs(tc.args)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("setupEmailFromArgs(%v) = (%q, %v), want (%q, %v)", tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestConfigSetupEmailArgsPrefillSetup(t *testing.T) {
	got, ok := configSetupEmailFromArgs([]string{"setup", "USER@Example.COM"})
	if got != "user@example.com" || !ok {
		t.Fatalf("configSetupEmailFromArgs returned (%q, %v), want user@example.com/true", got, ok)
	}
	if got, ok := configSetupEmailFromArgs([]string{"llm", "USER@Example.COM"}); got != "" || ok {
		t.Fatalf("configSetupEmailFromArgs should ignore non-setup routes, got (%q, %v)", got, ok)
	}
}

func TestLoopNoArgOpensTUIBackgroundTasks(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{want: true},
		{args: []string{"tui"}, want: true},
		{args: []string{"background"}, want: true},
		{args: []string{"list"}, want: false},
		{args: []string{"stop"}, want: false},
	}
	for _, tc := range cases {
		if got := loopCommandOpensTUI(tc.args); got != tc.want {
			t.Fatalf("loopCommandOpensTUI(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestServiceCommandRoutesToRelatedTUIPage(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantTab int
		wantSub int
	}{
		{name: "no args", wantTab: views.TabServiceRedeem, wantSub: -1},
		{name: "status stays redeem", args: []string{"status"}, wantTab: views.TabServiceRedeem, wantSub: -1},
		{name: "redeem code stays redeem", args: []string{"ABC-123"}, wantTab: views.TabServiceRedeem, wantSub: -1},
		{name: "setup opens onboarding", args: []string{"setup"}, wantTab: views.TabOnboarding, wantSub: -1},
		{name: "llm opens config", args: []string{"llm"}, wantTab: views.TabConfig, wantSub: views.CfgTabLLM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tab, sub := serviceInitialRoute(tc.args)
			if tab != tc.wantTab || sub != tc.wantSub {
				t.Fatalf("serviceInitialRoute(%v) = (%d, %d), want (%d, %d)", tc.args, tab, sub, tc.wantTab, tc.wantSub)
			}
		})
	}
}

func TestServiceRedeemCodeArgsPrefillRedeemPage(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{name: "plain code", args: []string{"ABC-123"}, want: "ABC-123", ok: true},
		{name: "split code", args: []string{"ABC", "123"}, want: "ABC123", ok: true},
		{name: "code keyword", args: []string{"code", "ABC-123"}, want: "ABC-123", ok: true},
		{name: "code flag", args: []string{"--code", "ABC-123"}, want: "ABC-123", ok: true},
		{name: "redeem keyword", args: []string{"redeem", "ABC", "123"}, want: "ABC123", ok: true},
		{name: "setup is route", args: []string{"setup"}, ok: false},
		{name: "status is route", args: []string{"status"}, ok: false},
		{name: "empty code keyword", args: []string{"code"}, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := serviceRedeemCodeFromArgs(tc.args)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("serviceRedeemCodeFromArgs(%v) = (%q, %v), want (%q, %v)", tc.args, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestServiceSetupEmailArgsPrefillOnboarding(t *testing.T) {
	got, ok := serviceSetupEmailFromArgs([]string{"setup", "USER@Example.COM"})
	if got != "user@example.com" || !ok {
		t.Fatalf("serviceSetupEmailFromArgs returned (%q, %v), want user@example.com/true", got, ok)
	}
	got, ok = serviceSetupEmailFromArgs([]string{"setup", "--email", "USER@Example.COM"})
	if got != "user@example.com" || !ok {
		t.Fatalf("serviceSetupEmailFromArgs with flag returned (%q, %v), want user@example.com/true", got, ok)
	}
	if got, ok := serviceSetupEmailFromArgs([]string{"USER@Example.COM"}); got != "" || ok {
		t.Fatalf("plain redeem email-shaped value should not become setup prefill, got (%q, %v)", got, ok)
	}
}

func TestLocalNoArgCommandsOpenSimplifiedTUIRoutes(t *testing.T) {
	cases := []struct {
		name    string
		command string
		args    []string
		wantTab int
		wantSub int
		wantOK  bool
	}{
		{name: "memory no args opens chat", command: "memory", wantTab: views.TabChat, wantSub: -1, wantOK: true},
		{name: "memory tui opens chat", command: "memory", args: []string{"tui"}, wantTab: views.TabChat, wantSub: -1, wantOK: true},
		{name: "schedule no args opens scheduled tasks", command: "schedule", wantTab: views.TabTasks, wantSub: views.TaskSubScheduled, wantOK: true},
		{name: "schedule setup opens scheduled tasks", command: "schedule", args: []string{"setup"}, wantTab: views.TabTasks, wantSub: views.TaskSubScheduled, wantOK: true},
		{name: "audit no args opens redeem", command: "audit", wantTab: views.TabServiceRedeem, wantSub: -1, wantOK: true},
		{name: "audit redeem opens redeem", command: "audit", args: []string{"redeem"}, wantTab: views.TabServiceRedeem, wantSub: -1, wantOK: true},
		{name: "policy no args opens security config", command: "policy", wantTab: views.TabConfig, wantSub: views.CfgTabSecurity, wantOK: true},
		{name: "policy setup opens security config", command: "policy", args: []string{"setup"}, wantTab: views.TabConfig, wantSub: views.CfgTabSecurity, wantOK: true},
		{name: "memory list remains cli", command: "memory", args: []string{"list"}, wantOK: false},
		{name: "schedule create remains cli", command: "schedule", args: []string{"create"}, wantOK: false},
		{name: "audit list remains cli", command: "audit", args: []string{"list"}, wantOK: false},
		{name: "policy list remains cli", command: "policy", args: []string{"list"}, wantOK: false},
		{name: "project no args remains cli", command: "project", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tab, sub, ok := localCommandInitialTab(tc.command, tc.args)
			if ok != tc.wantOK {
				t.Fatalf("localCommandInitialTab(%q, %v) ok = %v, want %v", tc.command, tc.args, ok, tc.wantOK)
			}
			if ok && (tab != tc.wantTab || sub != tc.wantSub) {
				t.Fatalf("localCommandInitialTab(%q, %v) = (%d, %d, %v), want (%d, %d, %v)",
					tc.command, tc.args, tab, sub, ok, tc.wantTab, tc.wantSub, tc.wantOK)
			}
		})
	}
}

func TestOnboardingCommandRoutingPrefersFullTUI(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want onboardingCommandMode
	}{
		{name: "no args", want: onboardingCommandOpenSetup},
		{name: "explicit tui", args: []string{"tui"}, want: onboardingCommandOpenSetup},
		{name: "setup alias", args: []string{"setup"}, want: onboardingCommandOpenSetup},
		{name: "positional email prefill", args: []string{"USER@Example.COM"}, want: onboardingCommandOpenSetup},
		{name: "setup email prefill", args: []string{"setup", "USER@Example.COM"}, want: onboardingCommandOpenSetup},
		{name: "setup email flag prefill", args: []string{"setup", "--email", "USER@Example.COM"}, want: onboardingCommandOpenSetup},
		{name: "scripted cli", args: []string{"cli"}, want: onboardingCommandRunCLI},
		{name: "scripted cli flag", args: []string{"--cli", "--yes"}, want: onboardingCommandRunCLI},
		{name: "legacy email flag", args: []string{"--email", "user@example.com"}, want: onboardingCommandRunCLI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyOnboardingCommand(tc.args); got != tc.want {
				t.Fatalf("classifyOnboardingCommand(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestLLMSetupCommandRoutingPrefersFullTUI(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want llmCommandMode
	}{
		{name: "no args opens tui", want: llmCommandOpenConfig},
		{name: "setup opens tui", args: []string{"setup"}, want: llmCommandOpenConfig},
		{name: "explicit tui", args: []string{"setup", "tui"}, want: llmCommandOpenConfig},
		{name: "setup cli", args: []string{"setup", "cli"}, want: llmCommandRunCLI},
		{name: "setup cli flag", args: []string{"setup", "--cli"}, want: llmCommandRunCLI},
		{name: "status remains cli", args: []string{"status"}, want: llmCommandRunCLI},
		{name: "legacy setup flags remain cli", args: []string{"setup", "--provider", "custom"}, want: llmCommandRunCLI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyLLMCommand(tc.args); got != tc.want {
				t.Fatalf("classifyLLMCommand(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestRemoteCommandRoutingPrefersFullTUISetup(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want remoteCommandMode
	}{
		{name: "no args", want: remoteCommandOpenSetup},
		{name: "setup opens tui", args: []string{"setup"}, want: remoteCommandOpenSetup},
		{name: "explicit tui", args: []string{"tui"}, want: remoteCommandOpenSetup},
		{name: "positional email prefill", args: []string{"USER@Example.COM"}, want: remoteCommandOpenSetup},
		{name: "setup email prefill", args: []string{"setup", "USER@Example.COM"}, want: remoteCommandOpenSetup},
		{name: "setup email flag prefill", args: []string{"setup", "--email", "USER@Example.COM"}, want: remoteCommandOpenSetup},
		{name: "status remains cli", args: []string{"status"}, want: remoteCommandRunCLI},
		{name: "activate remains cli", args: []string{"activate", "--email", "user@example.com"}, want: remoteCommandRunCLI},
		{name: "set hubcenter remains cli", args: []string{"set-hubcenter", "https://center.example"}, want: remoteCommandRunCLI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRemoteCommand(tc.args); got != tc.want {
				t.Fatalf("classifyRemoteCommand(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestMCPCommandRoutingPrefersToolsTUI(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want mcpCommandMode
	}{
		{name: "no args", want: mcpCommandOpenTools},
		{name: "setup opens tui", args: []string{"setup"}, want: mcpCommandOpenTools},
		{name: "explicit tui", args: []string{"tui"}, want: mcpCommandOpenTools},
		{name: "remote template", args: []string{"remote"}, want: mcpCommandOpenTools},
		{name: "local template", args: []string{"local"}, want: mcpCommandOpenTools},
		{name: "add opens local template", args: []string{"add"}, want: mcpCommandOpenTools},
		{name: "add remote opens template", args: []string{"add", "remote"}, want: mcpCommandOpenTools},
		{name: "setup remote template", args: []string{"setup", "remote"}, want: mcpCommandOpenTools},
		{name: "list remains cli", args: []string{"list"}, want: mcpCommandRunCLI},
		{name: "add remains cli", args: []string{"add", "--name", "server"}, want: mcpCommandRunCLI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyMCPCommand(tc.args); got != tc.want {
				t.Fatalf("classifyMCPCommand(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestMCPAddModeFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"remote"}, want: "remote"},
		{args: []string{"add"}, want: "local"},
		{args: []string{"add", "remote"}, want: "remote"},
		{args: []string{"mcp", "add"}, want: "local"},
		{args: []string{"setup", "mcp", "add", "remote"}, want: "remote"},
		{args: []string{"setup", "remote"}, want: "remote"},
		{args: []string{"mcp", "add-remote"}, want: "remote"},
		{args: []string{"local"}, want: "local"},
		{args: []string{"mcp", "npx"}, want: "local"},
		{args: []string{"list"}, want: ""},
		{args: []string{"add", "--name", "server"}, want: ""},
		{args: []string{"mcp", "add", "--name", "server"}, want: ""},
	}
	for _, tc := range cases {
		if got := mcpAddModeFromArgs(tc.args); got != tc.want {
			t.Fatalf("mcpAddModeFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestMCPDefaultAddModeFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{want: mcpAddModeAutoLocal},
		{args: []string{"mcp"}, want: mcpAddModeAutoLocal},
		{args: []string{"setup"}, want: mcpAddModeAutoLocal},
		{args: []string{"setup", "mcp"}, want: mcpAddModeAutoLocal},
		{args: []string{"tui"}, want: mcpAddModeAutoLocal},
		{args: []string{"remote"}, want: "remote"},
		{args: []string{"add"}, want: "local"},
		{args: []string{"add", "remote"}, want: "remote"},
		{args: []string{"add", "--name", "server"}, want: ""},
		{args: []string{"mcp", "add", "--name", "server"}, want: ""},
		{args: []string{"list"}, want: ""},
	}
	for _, tc := range cases {
		if got := mcpDefaultAddModeFromArgs(tc.args); got != tc.want {
			t.Fatalf("mcpDefaultAddModeFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestToolsBackedCommandRoutingPrefersToolsTUI(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want toolsBackedCommandMode
	}{
		{name: "no args", want: toolsBackedCommandOpenTools},
		{name: "setup opens tui", args: []string{"setup"}, want: toolsBackedCommandOpenTools},
		{name: "explicit tui", args: []string{"tui"}, want: toolsBackedCommandOpenTools},
		{name: "tool status remains cli", args: []string{"status"}, want: toolsBackedCommandRunCLI},
		{name: "skill list remains cli", args: []string{"list"}, want: toolsBackedCommandRunCLI},
		{name: "skillhub search remains cli", args: []string{"search", "pdf"}, want: toolsBackedCommandRunCLI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyToolsBackedCommand(tc.args); got != tc.want {
				t.Fatalf("classifyToolsBackedCommand(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestLLMCLIArgsStripExplicitSetupMarker(t *testing.T) {
	got := llmCLIArgs([]string{"setup", "cli"})
	if strings.Join(got, " ") != "setup" {
		t.Fatalf("llmCLIArgs did not strip cli marker: %#v", got)
	}
	got = llmCLIArgs([]string{"setup", "--cli", "--provider", "custom"})
	if strings.Join(got, " ") != "setup --provider custom" {
		t.Fatalf("llmCLIArgs did not preserve setup args after marker: %#v", got)
	}
	got = llmCLIArgs([]string{"status"})
	if strings.Join(got, " ") != "status" {
		t.Fatalf("llmCLIArgs should leave non-setup commands intact: %#v", got)
	}
}

func TestOnboardingCLIArgsStripExplicitMarker(t *testing.T) {
	got := onboardingCLIArgs([]string{"cli", "--email", "user@example.com"})
	if strings.Join(got, " ") != "--email user@example.com" {
		t.Fatalf("onboardingCLIArgs did not strip marker: %#v", got)
	}
	got = onboardingCLIArgs([]string{"--email", "user@example.com"})
	if strings.Join(got, " ") != "--email user@example.com" {
		t.Fatalf("legacy onboarding flags should stay intact: %#v", got)
	}
}

func TestSlashNavigationCommandsSwitchTabs(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}

	m.handleSlashCommand("/setup")
	if m.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("/setup active tab = %d, want onboarding", m.root.ActiveTab())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "HubCenter") {
		t.Fatalf("/setup status should explain setup action: %s", m.root.StatusBar.View(120))
	}

	m.handleSlashCommand("/setup USER@Example.COM")
	if m.root.ActiveTab() != views.TabOnboarding || m.root.Onboarding.EmailValueForTest() != "user@example.com" {
		t.Fatalf("/setup EMAIL active tab/email = %d/%q, want onboarding/email", m.root.ActiveTab(), m.root.Onboarding.EmailValueForTest())
	}

	m.handleSlashCommand("/setup remote OTHER@Example.COM")
	if m.root.ActiveTab() != views.TabOnboarding || m.root.Onboarding.EmailValueForTest() != "other@example.com" {
		t.Fatalf("/setup remote EMAIL active tab/email = %d/%q, want onboarding/email", m.root.ActiveTab(), m.root.Onboarding.EmailValueForTest())
	}

	m.handleSlashCommand("/setup llm")
	if m.root.ActiveTab() != views.TabConfig || m.root.Config.ActiveTab() != views.CfgTabLLM {
		t.Fatalf("/setup llm active tab/config tab = %d/%d, want config/LLM", m.root.ActiveTab(), m.root.Config.ActiveTab())
	}

	m.handleSlashCommand("/setup mcp")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "" {
		t.Fatalf("/setup mcp active tab/sub/add = %d/%d/%q, want tools/MCP/template choices", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}

	m.handleSlashCommand("/redeem")
	if m.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("/redeem active tab = %d, want service redeem", m.root.ActiveTab())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "masked") {
		t.Fatalf("/redeem status should mention masked input: %s", m.root.StatusBar.View(120))
	}

	m.handleSlashCommand("/redeem ABC-123")
	if m.root.ActiveTab() != views.TabServiceRedeem || m.root.Service.CodeValueForTest() != "ABC-123" {
		t.Fatalf("/redeem CODE active tab/code = %d/%q, want service redeem/code", m.root.ActiveTab(), m.root.Service.CodeValueForTest())
	}

	m.handleSlashCommand("/redeem setup")
	if m.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("/redeem setup active tab = %d, want onboarding", m.root.ActiveTab())
	}

	m.handleSlashCommand("/redeem setup THIRD@Example.COM")
	if m.root.ActiveTab() != views.TabOnboarding || m.root.Onboarding.EmailValueForTest() != "third@example.com" {
		t.Fatalf("/redeem setup EMAIL active tab/email = %d/%q, want onboarding/email", m.root.ActiveTab(), m.root.Onboarding.EmailValueForTest())
	}

	m.handleSlashCommand("/service llm")
	if m.root.ActiveTab() != views.TabConfig || m.root.Config.ActiveTab() != views.CfgTabLLM {
		t.Fatalf("/service llm active tab/config tab = %d/%d, want config/LLM", m.root.ActiveTab(), m.root.Config.ActiveTab())
	}

	m.handleSlashCommand("/config")
	if m.root.ActiveTab() != views.TabConfig {
		t.Fatalf("/config active tab = %d, want config", m.root.ActiveTab())
	}

	m.handleSlashCommand("/config proxy")
	if m.root.ActiveTab() != views.TabConfig || m.root.Config.ActiveTab() != views.CfgTabProxy {
		t.Fatalf("/config proxy active tab/config tab = %d/%d, want config/Proxy", m.root.ActiveTab(), m.root.Config.ActiveTab())
	}

	m.handleSlashCommand("/status")
	if m.root.ActiveTab() != views.TabConfig || m.root.Config.ActiveTab() != views.CfgTabGeneral {
		t.Fatalf("/status active tab/config tab = %d/%d, want config/General", m.root.ActiveTab(), m.root.Config.ActiveTab())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "Status") {
		t.Fatalf("/status status bar should mention status: %s", m.root.StatusBar.View(120))
	}

	m.handleSlashCommand("/health")
	if m.root.ActiveTab() != views.TabConfig || m.root.Config.ActiveTab() != views.CfgTabGeneral {
		t.Fatalf("/health active tab/config tab = %d/%d, want config/General", m.root.ActiveTab(), m.root.Config.ActiveTab())
	}

	m.handleSlashCommand("/llm")
	if m.root.ActiveTab() != views.TabConfig || m.root.Config.ActiveTab() != views.CfgTabLLM {
		t.Fatalf("/llm active tab/config tab = %d/%d, want config/LLM", m.root.ActiveTab(), m.root.Config.ActiveTab())
	}

	m.handleSlashCommand("/security")
	if m.root.ActiveTab() != views.TabConfig || m.root.Config.ActiveTab() != views.CfgTabSecurity {
		t.Fatalf("/security active tab/config tab = %d/%d, want config/Security", m.root.ActiveTab(), m.root.Config.ActiveTab())
	}

	m.handleSlashCommand("/chat")
	if m.root.ActiveTab() != views.TabChat {
		t.Fatalf("/chat active tab = %d, want chat", m.root.ActiveTab())
	}

	m.handleSlashCommand("/tools")
	if m.root.ActiveTab() != views.TabTools {
		t.Fatalf("/tools active tab = %d, want tools", m.root.ActiveTab())
	}

	m.handleSlashCommand("/tools mcp")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "" {
		t.Fatalf("/tools mcp active tab/sub/add = %d/%d/%q, want tools/MCP/template choices", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "MCP templates") {
		t.Fatalf("/tools mcp status should explain template flow: %s", m.root.StatusBar.View(120))
	}

	m.handleSlashCommand("/tools mcp remote")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "remote" {
		t.Fatalf("/tools mcp remote active tab/sub/add = %d/%d/%q, want tools/MCP/remote", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}

	m.handleSlashCommand("/setup mcp local")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "local" {
		t.Fatalf("/setup mcp local active tab/sub/add = %d/%d/%q, want tools/MCP/local", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}

	m.handleSlashCommand("/mcp")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "" {
		t.Fatalf("/mcp active tab/sub/add = %d/%d/%q, want tools/MCP/template choices", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "MCP templates") {
		t.Fatalf("/mcp status should explain template flow: %s", m.root.StatusBar.View(120))
	}

	m.handleSlashCommand("/mcp remote")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "remote" {
		t.Fatalf("/mcp remote active tab/sub/add = %d/%d/%q, want tools/MCP/remote", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}

	m.handleSlashCommand("/mcp local")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "local" {
		t.Fatalf("/mcp local active tab/sub/add = %d/%d/%q, want tools/MCP/local", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}

	configured := &tuiModel{
		app: &TUIApp{appConfig: corelib.AppConfig{
			Language:        "en",
			LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
		}},
		root: views.NewRootModel("en"),
	}
	configured.handleSlashCommand("/mcp")
	if configured.root.ActiveTab() != views.TabTools || configured.root.Tools.ActiveSubTab() != views.ToolSubMCP {
		t.Fatalf("configured /mcp active tab/sub = %d/%d, want tools/MCP", configured.root.ActiveTab(), configured.root.Tools.ActiveSubTab())
	}
	if configured.root.Tools.MCPAddMode() != "" {
		t.Fatalf("configured /mcp should show list instead of add form, got %q", configured.root.Tools.MCPAddMode())
	}
	if strings.Contains(configured.root.StatusBar.View(120), "MCP templates") {
		t.Fatalf("configured /mcp status should not describe add-template flow: %s", configured.root.StatusBar.View(120))
	}
	if !strings.Contains(configured.root.StatusBar.View(120), "MCP list") {
		t.Fatalf("configured /mcp status should describe list flow: %s", configured.root.StatusBar.View(120))
	}

	configured.handleSlashCommand("/tools mcp")
	if configured.root.ActiveTab() != views.TabTools || configured.root.Tools.ActiveSubTab() != views.ToolSubMCP {
		t.Fatalf("configured /tools mcp active tab/sub = %d/%d, want tools/MCP", configured.root.ActiveTab(), configured.root.Tools.ActiveSubTab())
	}
	if configured.root.Tools.MCPAddMode() != "" {
		t.Fatalf("configured /tools mcp should show list instead of add form, got %q", configured.root.Tools.MCPAddMode())
	}
	if !strings.Contains(configured.root.StatusBar.View(120), "MCP list") {
		t.Fatalf("configured /tools mcp status should describe list flow: %s", configured.root.StatusBar.View(120))
	}

	m.handleSlashCommand("/skill")
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubSkill {
		t.Fatalf("/skill active tab/tools sub-tab = %d/%d, want tools/Skill", m.root.ActiveTab(), m.root.Tools.ActiveSubTab())
	}

	m.handleSlashCommand("/tasks")
	if m.root.ActiveTab() != views.TabTasks {
		t.Fatalf("/tasks active tab = %d, want tasks", m.root.ActiveTab())
	}

	m.handleSlashCommand("/tasks background")
	if m.root.ActiveTab() != views.TabTasks || m.root.Tasks.ActiveSubTab() != views.TaskSubBackground {
		t.Fatalf("/tasks background active tab/tasks sub-tab = %d/%d, want tasks/Background", m.root.ActiveTab(), m.root.Tasks.ActiveSubTab())
	}

	m.handleSlashCommand("/schedule")
	if m.root.ActiveTab() != views.TabTasks || m.root.Tasks.ActiveSubTab() != views.TaskSubScheduled {
		t.Fatalf("/schedule active tab/tasks sub-tab = %d/%d, want tasks/Scheduled", m.root.ActiveTab(), m.root.Tasks.ActiveSubTab())
	}
}

func TestSlashRedeemRefreshesServiceWhenHubReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}

	cmd := m.handleSlashCommand("/redeem")
	if cmd == nil {
		t.Fatal("/redeem should refresh service status when Hub is ready")
	}
	if m.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("/redeem active tab = %d, want service redeem", m.root.ActiveTab())
	}
}

func TestDirectRedeemTabShortcutRefreshesServiceWhenHubReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabChat)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	if cmd == nil {
		t.Fatal("F5 into Service Redeem should refresh service status when Hub is ready")
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
}

func TestMemoryCategorySummaryIsCompact(t *testing.T) {
	got := memoryCategorySummary(map[string]int{
		"":      1,
		"task":  3,
		"user":  2,
		"agent": 4,
		"extra": 5,
	})
	for _, want := range []string{"default:1", "agent:4", "+1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
}

func TestSlashMemoryCommandStaysSimplified(t *testing.T) {
	root := views.NewRootModel("en")
	root, _ = root.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: root,
	}

	m.handleSlashCommand("/memory")
	messages := m.root.Chat.GetMessages()
	got := messages[len(messages)-1].Content
	if !strings.Contains(got, "simplified TUI") || !strings.Contains(got, "no separate memory page") {
		t.Fatalf("/memory should show a compact simplified-TUI note, got: %q", got)
	}
	if strings.Contains(got, "Categories:") || strings.Contains(got, "maclaw-tui memory list") {
		t.Fatalf("/memory should not dump categories or route users to a memory browser: %q", got)
	}
}

func TestSlashHelpOpensHelpOverlay(t *testing.T) {
	root := views.NewRootModel("en")
	root, _ = root.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: root,
	}

	m.handleSlashCommand("/help")
	if !m.root.Help.IsVisible() {
		t.Fatal("/help should open the help overlay")
	}
	if !strings.Contains(m.root.StatusBar.View(100), "Esc closes") {
		t.Fatalf("/help status should explain closing help: %s", m.root.StatusBar.View(100))
	}
	messages := m.root.Chat.GetMessages()
	for _, msg := range messages {
		if strings.Contains(msg.Content, "Available commands:") {
			t.Fatalf("/help should not append long help text into chat: %#v", messages)
		}
	}
}

func TestSlashHelpUsesCurrentUILanguage(t *testing.T) {
	root := views.NewRootModel("en")
	root, _ = root.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: root,
	}

	m.root.SetLang("zh")
	m.handleSlashCommand("/help")

	view := stripANSIForTest(m.root.Help.View())
	if !strings.Contains(view, "斜杠命令") || !strings.Contains(view, "/loop <验证命令> <目标>") || !strings.Contains(view, "运行目标驱动验证循环") || !strings.Contains(view, "--timeout 秒") {
		t.Fatalf("/help overlay should use current UI language:\n%s", view)
	}
	if strings.Contains(view, "Slash commands") || strings.Contains(view, "goal-driven verification loop") || strings.Contains(view, "--dir path") {
		t.Fatalf("/help overlay should not keep English after UI language changes:\n%s", view)
	}
	if !strings.Contains(m.root.StatusBar.View(100), "已打开帮助") {
		t.Fatalf("/help status should use current UI language: %s", m.root.StatusBar.View(100))
	}
}

func TestConfigOpenSetupMessageSwitchesTab(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabConfig)

	updated, cmd := m.Update(views.ConfigOpenSetupMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("active tab = %d, want onboarding", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "activate Hub") {
		t.Fatalf("status bar did not explain setup action: %s", got.root.StatusBar.View(120))
	}
}

func TestConfigOpenServiceRedeemMessageSwitchesTab(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabConfig)

	updated, cmd := m.Update(views.ConfigOpenServiceRedeemMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "Service Redeem") {
		t.Fatalf("status bar did not explain redeem action: %s", got.root.StatusBar.View(120))
	}
}

func TestConfigOpenServiceRedeemRefreshesWhenHubReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabConfig)

	updated, cmd := m.Update(views.ConfigOpenServiceRedeemMsg{})
	if cmd == nil {
		t.Fatal("expected service refresh command when Hub is ready")
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
}

func TestConfigOpenToolsMessageSwitchesToMCPTemplates(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabConfig)

	updated, cmd := m.Update(views.ConfigOpenToolsMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabTools || got.root.Tools.ActiveSubTab() != views.ToolSubMCP {
		t.Fatalf("active tab/tools sub-tab = %d/%d, want tools/MCP", got.root.ActiveTab(), got.root.Tools.ActiveSubTab())
	}
	if got.root.Tools.MCPAddMode() != "" {
		t.Fatalf("MCP add mode = %q, want template choice list", got.root.Tools.MCPAddMode())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "Left/Right") || !strings.Contains(got.root.StatusBar.View(120), "A remote") {
		t.Fatalf("status bar should explain MCP template choices: %s", got.root.StatusBar.View(120))
	}
	if !strings.Contains(got.root.Tools.View(), "Local templates") {
		t.Fatalf("Tools view should show MCP template choices:\n%s", got.root.Tools.View())
	}
}

func TestMCPAddResultRefreshesConfigBackedViews(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	baseCfg := corelib.AppConfig{
		Language:                 "en",
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: tuiHubServiceProviderName,
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "auto",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         tuiHubServiceProviderName,
			IsHubService: true,
		}},
	}
	if err := store.SaveConfig(baseCfg); err != nil {
		t.Fatalf("save base config: %v", err)
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: baseCfg, llmConfig: buildLLMConfigFromAppConfig(baseCfg)},
		root: views.NewRootModel("en"),
	}
	m.root.Service.LoadFromAppConfig(baseCfg)
	if !m.root.Service.NeedsMCPNextStep() {
		t.Fatal("expected service redeem to request MCP before a server is configured")
	}

	withMCP := baseCfg
	withMCP.LocalMCPServers = []corelib.LocalMCPServerEntry{{Name: "filesystem", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "."}}}
	if err := store.SaveConfig(withMCP); err != nil {
		t.Fatalf("save MCP config: %v", err)
	}

	updated, cmd := m.Update(views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: true, Message: "added filesystem"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.Service.NeedsMCPNextStep() {
		t.Fatal("service redeem should stop prompting for MCP after config reload")
	}
	if len(got.app.appConfig.LocalMCPServers) != 1 {
		t.Fatalf("app config MCP count = %d, want 1", len(got.app.appConfig.LocalMCPServers))
	}
	if !strings.Contains(got.root.StatusBar.View(120), "F2") || !strings.Contains(got.root.StatusBar.View(120), "Chat") {
		t.Fatalf("MCP add status should guide back to chat: %s", got.root.StatusBar.View(120))
	}
	got.root.Tools.FocusMCP()
	if view := got.root.Tools.View(); !strings.Contains(view, "filesystem") {
		t.Fatalf("Tools MCP view should show reloaded server:\n%s", view)
	}
}

func TestMCPAddHonorsSecurityPolicy(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	cfg := corelib.AppConfig{Language: "en", HubSecurityCentralized: true, SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	m := &tuiModel{app: &TUIApp{appConfig: cfg}, root: views.NewRootModel("en")}

	localMsg := m.addLocalMCP(corelib.LocalMCPServerEntry{Name: "local", Command: "node", Args: []string{"server.js"}})()
	localResult, ok := localMsg.(views.ToolOperationResultMsg)
	if !ok {
		t.Fatalf("local message = %T, want ToolOperationResultMsg", localMsg)
	}
	if localResult.Success || !strings.Contains(localResult.Message, "sandbox") {
		t.Fatalf("local result = %#v, want sandbox rejection", localResult)
	}

	remoteMsg := m.addRemoteMCP(corelib.MCPServerEntry{Name: "remote", EndpointURL: "https://mcp.example/rpc"})()
	remoteResult, ok := remoteMsg.(views.ToolOperationResultMsg)
	if !ok {
		t.Fatalf("remote message = %T, want ToolOperationResultMsg", remoteMsg)
	}
	if remoteResult.Success || !strings.Contains(remoteResult.Message, "network") {
		t.Fatalf("remote result = %#v, want network rejection", remoteResult)
	}

	loaded, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loaded.LocalMCPServers) != 0 || len(loaded.MCPServers) != 0 {
		t.Fatalf("blocked MCP entries persisted: local=%d remote=%d", len(loaded.LocalMCPServers), len(loaded.MCPServers))
	}
}

func TestConfigSaveRejectsHubManagedSecurityKey(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	cfg := corelib.AppConfig{Language: "en", HubSecurityCentralized: true, SecurityPolicyMode: "strict", SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	m := &tuiModel{app: &TUIApp{appConfig: cfg}, root: views.NewRootModel("en")}

	msg := m.saveConfig(views.ConfigSaveMsg{Key: "security_policy_mode", Value: "developer"})()
	failed, ok := msg.(views.ConfigSaveFailedMsg)
	if !ok {
		t.Fatalf("message = %T, want ConfigSaveFailedMsg", msg)
	}
	if !strings.Contains(failed.Error, "Hub") {
		t.Fatalf("failure = %#v, want Hub-managed reason", failed)
	}
	loaded, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.SecurityPolicyMode != "strict" || loaded.SandboxMode != "os" || loaded.NetworkLevel != "none" {
		t.Fatalf("managed security config changed: %#v", loaded)
	}
}

func TestConfigSaveSnapshotPreservesHubManagedSecurity(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	current := corelib.AppConfig{Language: "zh", HubSecurityCentralized: true, SecurityPolicyMode: "strict", SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: false, ImageOutboundEnabled: false}
	if err := store.SaveConfig(current); err != nil {
		t.Fatalf("save config: %v", err)
	}
	m := &tuiModel{app: &TUIApp{appConfig: current}, root: views.NewRootModel("en")}
	snapshot := current
	snapshot.Language = "en"
	snapshot.HubSecurityCentralized = false
	snapshot.SecurityPolicyMode = "developer"
	snapshot.SandboxMode = "none"
	snapshot.NetworkLevel = "full"
	snapshot.FileOutboundEnabled = true
	snapshot.ImageOutboundEnabled = true

	msg := m.saveConfig(views.ConfigSaveMsg{Key: "language", Value: "en", Config: snapshot, HasConfig: true})()
	if _, ok := msg.(views.ConfigSavedMsg); !ok {
		t.Fatalf("message = %T, want ConfigSavedMsg", msg)
	}
	loaded, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Language != "en" {
		t.Fatalf("language = %q, want en", loaded.Language)
	}
	if !loaded.HubSecurityCentralized || loaded.SecurityPolicyMode != "strict" || loaded.SandboxMode != "os" || loaded.NetworkLevel != "none" || loaded.FileOutboundEnabled || loaded.ImageOutboundEnabled {
		t.Fatalf("managed security config was overwritten by snapshot: %#v", loaded)
	}
}

func TestF3OpensMCPTemplateWhenLLMReadyWithoutMCP(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:       "en",
		MaclawLLMUrl:   "http://localhost:11434/v1",
		MaclawLLMModel: "qwen2.5-coder:32b",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg, llmConfig: buildLLMConfigFromAppConfig(cfg)},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabChat)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF3})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabTools || got.root.Tools.ActiveSubTab() != views.ToolSubMCP {
		t.Fatalf("F3 active tab/sub-tab = %d/%d, want tools/MCP", got.root.ActiveTab(), got.root.Tools.ActiveSubTab())
	}
	if got.root.Tools.MCPAddMode() != "local" {
		t.Fatalf("F3 should open local MCP template, got %q", got.root.Tools.MCPAddMode())
	}
}

func TestF3KeepsNormalToolsShortcutWhenMCPConfigured(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:        "en",
		MaclawLLMUrl:    "http://localhost:11434/v1",
		MaclawLLMModel:  "qwen2.5-coder:32b",
		LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg, llmConfig: buildLLMConfigFromAppConfig(cfg)},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabChat)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF3})
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabTools {
		t.Fatalf("F3 active tab = %d, want tools", got.root.ActiveTab())
	}
	if got.root.Tools.MCPAddMode() != "" {
		t.Fatalf("F3 should not reopen MCP add flow once MCP is configured, got %q", got.root.Tools.MCPAddMode())
	}
}

func TestServiceRedeemOpenSetupMessageSwitchesTab(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabServiceRedeem)

	updated, cmd := m.Update(views.ServiceRedeemOpenSetupMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("active tab = %d, want onboarding", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "activate Hub") {
		t.Fatalf("status bar did not explain setup action: %s", got.root.StatusBar.View(120))
	}
}

func TestTaskOpenMessagesSwitchTabsAndExplainAction(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabTasks)

	updated, cmd := m.Update(views.TaskOpenToolsMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabTools {
		t.Fatalf("active tab = %d, want tools", got.root.ActiveTab())
	}
	if got.root.Tools.ActiveSubTab() != views.ToolSubMCP {
		t.Fatalf("tools sub-tab = %d, want MCP", got.root.Tools.ActiveSubTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "Left/Right") || !strings.Contains(got.root.Tools.View(), "Local templates") {
		t.Fatalf("status/tools view should explain MCP template choices: %s\n%s", got.root.StatusBar.View(120), got.root.Tools.View())
	}

	got.root.SetTab(views.TabTasks)
	updated, cmd = got.Update(views.TaskOpenChatMsg{})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got = updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabChat {
		t.Fatalf("active tab = %d, want chat", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "Type a message") {
		t.Fatalf("status bar should explain chat action: %s", got.root.StatusBar.View(120))
	}
}

func TestServiceRedeemResultKeepsRedeemTabVisible(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabServiceRedeem)

	updated, cmd := m.Update(views.ServiceRedeemResultMsg{Success: true, Message: "ok"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "ok") {
		t.Fatalf("status bar should show service result: %s", got.root.StatusBar.View(120))
	}
}

func TestServiceRedeemResultAppliesRuntimeLLMConfig(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:                 "en",
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: tuiHubServiceProviderName,
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMKey:             "viewer-token",
		MaclawLLMModel:           "qwen-max",
		MaclawLLMProtocol:        "openai",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         tuiHubServiceProviderName,
			IsHubService: true,
			URL:          "https://hub.example/api/llm",
			Key:          "viewer-token",
			Model:        "qwen-max",
			Protocol:     "openai",
		}},
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabServiceRedeem)

	updated, cmd := m.Update(views.ServiceRedeemResultMsg{
		Success:      true,
		Message:      "ready",
		ProviderName: tuiHubServiceProviderName,
		Config:       cfg,
		HasConfig:    true,
	})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.app.llmConfig.URL != "https://hub.example/api/llm" || got.app.llmConfig.Model != "qwen-max" {
		t.Fatalf("runtime LLM config was not updated: %#v", got.app.llmConfig)
	}
	if !strings.Contains(got.root.StatusBar.View(120), "qwen-max") {
		t.Fatalf("status bar should show refreshed official model: %s", got.root.StatusBar.View(120))
	}
}

func TestServiceRedeemFailureResultUpdatesStatusBar(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabServiceRedeem)

	updated, cmd := m.Update(views.ServiceRedeemResultMsg{Success: false, Message: "bad code"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "bad code") {
		t.Fatalf("status bar should show service failure: %s", got.root.StatusBar.View(120))
	}
}

func TestOnboardingRemoteSuccessWithoutCredentialsStaysIncomplete(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		Language:     "en",
		RemoteEmail:  "user@example.com",
		RemoteHubURL: "https://hub.example",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.SetTab(views.TabOnboarding)
	m.root.Onboarding.LoadFromAppConfig(corelib.AppConfig{Language: "en", RemoteEmail: "user@example.com"})

	updated, cmd := m.Update(views.OnboardingRemoteResultMsg{
		Success:   true,
		HubURL:    "https://hub.example",
		MachineID: "machine-1",
	})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.Onboarding.RemoteDoneForTest() {
		t.Fatal("remote onboarding should not complete without Hub service or machine credentials")
	}
	status := got.root.StatusBar.View(120)
	if strings.Contains(status, "succeeded") {
		t.Fatalf("status bar should not show success for incomplete credentials: %s", status)
	}
	if !strings.Contains(status, "viewer token") {
		t.Fatalf("status bar should explain missing viewer token: %s", status)
	}
}

func TestOnboardingLanguageChangeSavesConfigAndSwitchesRoot(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{Language: "zh"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "zh"}},
		root: views.NewRootModel("zh"),
	}
	updated, cmd := m.Update(views.OnboardingLanguageChangedMsg{Language: "en"})
	if cmd == nil {
		t.Fatal("expected save command")
	}
	got := updated.(*tuiModel)
	if got.root.Lang() != "en" {
		t.Fatalf("root lang = %q, want en", got.root.Lang())
	}
	msg, ok := cmd().(views.ConfigSavedMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Key != "language" || msg.Value != "en" {
		t.Fatalf("save msg = %#v", msg)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Language != "en" {
		t.Fatalf("saved language = %q", cfg.Language)
	}
}

func TestOnboardingLanguageChangeRelocalizesStatusBarModelInfoImmediately(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{Language: "en"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	llm := corelib.MaclawLLMConfig{
		ProviderName: tuiHubServiceProviderName,
		Model:        "qwen-max",
		URL:          "https://hub.example/api/llm",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}, llmConfig: llm},
		root: views.NewRootModel("en"),
	}
	m.root.StatusBar.SetModelInfo(tuiModelDisplayLabel("en", llm.ProviderName, llm.Model))

	updated, cmd := m.Update(views.OnboardingLanguageChangedMsg{Language: "zh"})
	if cmd == nil {
		t.Fatal("expected save command")
	}
	got := updated.(*tuiModel)
	wantProvider := tuiProviderDisplayName("zh", tuiHubServiceProviderName)
	bar := got.root.StatusBar.View(120)
	if !strings.Contains(bar, wantProvider) || strings.Contains(bar, "MaClaw Official") {
		t.Fatalf("status bar provider should switch immediately, want %q in %s", wantProvider, bar)
	}
}

func TestConfigLanguageSaveRelocalizesStatusBarModelInfo(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{Language: "zh"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	llm := corelib.MaclawLLMConfig{
		ProviderName: tuiHubServiceProviderName,
		Model:        "qwen-max",
		URL:          "https://hub.example/api/llm",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}, llmConfig: llm},
		root: views.NewRootModel("en"),
	}
	m.root.StatusBar.SetModelInfo(tuiModelDisplayLabel("en", llm.ProviderName, llm.Model))

	updated, cmd := m.Update(views.ConfigSavedMsg{Key: "language", Value: "zh"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	wantProvider := tuiProviderDisplayName("zh", tuiHubServiceProviderName)
	bar := got.root.StatusBar.View(120)
	if got.root.Lang() != "zh" {
		t.Fatalf("root lang = %q, want zh", got.root.Lang())
	}
	if !strings.Contains(bar, wantProvider) || strings.Contains(bar, "MaClaw Official") {
		t.Fatalf("status bar provider should follow language, want %q in %s", wantProvider, bar)
	}
}

func TestOnboardingSavedWithHubOpensServiceRedeem(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ConfigSavedMsg{Key: "onboarding", Value: "done"})
	if cmd == nil {
		t.Fatal("expected service status refresh command")
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "service code") {
		t.Fatalf("status bar should explain service redeem next step: %s", got.root.StatusBar.View(120))
	}
}

func TestOnboardingSavedWithPendingCodeSkipsServiceRefresh(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.root.Service.SetInitialCode("SERVICE-CODE")
	updated, cmd := m.Update(views.ConfigSavedMsg{Key: "onboarding", Value: "done"})
	if cmd != nil {
		t.Fatalf("unexpected service refresh command with pending code: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
	if got.root.Service.CodeValueForTest() != "SERVICE-CODE" {
		t.Fatalf("pending service code = %q, want SERVICE-CODE", got.root.Service.CodeValueForTest())
	}
}

func TestOnboardingSavedWithoutHubOpensLLMConfig(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		Language:       "en",
		OnboardingDone: true,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ConfigSavedMsg{Key: "onboarding", Value: "done"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabConfig {
		t.Fatalf("active tab = %d, want config", got.root.ActiveTab())
	}
}

func TestOnboardingSavedWithLLMOpensChat(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		Language:       "en",
		OnboardingDone: true,
		MaclawLLMUrl:   "http://localhost:11434/v1",
		MaclawLLMModel: "qwen2.5-coder:32b",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ConfigSavedMsg{Key: "onboarding", Value: "done"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabChat {
		t.Fatalf("active tab = %d, want chat", got.root.ActiveTab())
	}
}

func TestConfigureInitialTabStartsOnboardingForFreshConfig(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(corelib.AppConfig{Language: "en"}, false, "en")
	if m.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("active tab = %d, want onboarding", m.root.ActiveTab())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "HubCenter") {
		t.Fatalf("fresh setup status should explain the first action: %s", m.root.StatusBar.View(120))
	}
	if m.startupCmd != nil {
		t.Fatal("fresh onboarding should not start service refresh")
	}
}

func TestConfigureInitialTabRefreshesServiceWhenHubReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, false, "en")
	if m.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", m.root.ActiveTab())
	}
	if m.startupCmd == nil {
		t.Fatal("Hub-ready setup should refresh official service status on startup")
	}
	if !strings.Contains(m.root.StatusBar.View(120), "service code") {
		t.Fatalf("status bar should explain redeem path: %s", m.root.StatusBar.View(120))
	}
}

func TestConfigureInitialTabTreatsExistingHubAsSetupReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, false, "en")
	if m.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem for legacy Hub-ready config", m.root.ActiveTab())
	}
	if m.startupCmd == nil {
		t.Fatal("legacy Hub-ready config should refresh official service status on startup")
	}
}

func TestConfigureInitialTabTreatsServiceReadyWithSavedEmailAsRedeem(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		RemoteEmail:       "user@example.com",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, false, "en")
	if m.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem when Hub service credentials are ready", m.root.ActiveTab())
	}
	if m.startupCmd == nil {
		t.Fatal("service-ready config should refresh official service status on startup")
	}
}

func TestConfigureInitialTabTreatsExistingLLMAsSetupReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:       "en",
		MaclawLLMUrl:   "http://localhost:11434/v1",
		MaclawLLMModel: "qwen2.5-coder:32b",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, true, "en")
	if m.root.ActiveTab() != views.TabChat {
		t.Fatalf("active tab = %d, want chat for legacy LLM-ready config", m.root.ActiveTab())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "F3") || !strings.Contains(m.root.StatusBar.View(120), "MCP") {
		t.Fatalf("LLM-ready startup without MCP should advertise optional MCP templates: %s", m.root.StatusBar.View(120))
	}
}

func TestConfigureInitialTabFocusesLLMKeyForKeyedProviderWithoutKey(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:                 "en",
		MaclawLLMUrl:             "https://api.openai.com/v1",
		MaclawLLMModel:           "gpt-4o",
		MaclawLLMCurrentProvider: "OpenAI API Key",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, tuiConfigLLMReady(cfg), "en")
	if m.root.ActiveTab() != views.TabConfig {
		t.Fatalf("keyed provider without key should open config, active tab = %d", m.root.ActiveTab())
	}
	if m.root.Config.ActiveTab() != views.CfgTabLLM {
		t.Fatalf("config tab = %d, want LLM", m.root.Config.ActiveTab())
	}
	if !strings.Contains(m.root.StatusBar.View(120), "API key") {
		t.Fatalf("status should explain missing LLM key: %s", m.root.StatusBar.View(120))
	}
	if !m.llmMissing() {
		t.Fatal("keyed provider without key should be considered missing")
	}
}

func TestConfigureInitialTabDoesNotAdvertiseMCPWhenConfigured(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:        "en",
		MaclawLLMUrl:    "http://localhost:11434/v1",
		MaclawLLMModel:  "qwen2.5-coder:32b",
		LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, true, "en")
	if strings.Contains(m.root.StatusBar.View(120), "F3") || strings.Contains(m.root.StatusBar.View(120), "templates") {
		t.Fatalf("startup should not prompt for MCP after MCP is configured: %s", m.root.StatusBar.View(120))
	}
}

func TestOnboardingSavedWithLLMGuidesOptionalMCP(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := commands.NewFileConfigStore(dataDir)
	cfg := corelib.AppConfig{
		Language:       "en",
		OnboardingDone: true,
		MaclawLLMUrl:   "http://localhost:11434/v1",
		MaclawLLMModel: "qwen2.5-coder:32b",
	}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}

	updated, cmd := m.Update(views.ConfigSavedMsg{Key: "onboarding", Value: "done"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabChat {
		t.Fatalf("active tab = %d, want chat", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "F3") || !strings.Contains(got.root.StatusBar.View(120), "MCP") {
		t.Fatalf("onboarding completion should guide optional MCP templates: %s", got.root.StatusBar.View(120))
	}
}

func TestConfigureInitialTabOpensSetupForIncompleteRemoteWithoutLLM(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:       "en",
		OnboardingDone: true,
		RemoteEmail:    "user@example.com",
		RemoteHubURL:   "https://hub.example",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, false, "en")
	if m.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("active tab = %d, want onboarding", m.root.ActiveTab())
	}
	if m.startupCmd != nil {
		t.Fatal("incomplete remote setup should not start service refresh")
	}
	if !strings.Contains(m.root.StatusBar.View(120), "incomplete") {
		t.Fatalf("status bar should explain incomplete setup: %s", m.root.StatusBar.View(120))
	}
}

func TestForcedInitialTabClearsServiceRefreshOutsideRedeem(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, false, "en")
	if m.startupCmd == nil {
		t.Fatal("test setup expected service refresh command")
	}

	m.applyForcedInitialTab(views.TabConfig, cfg, false, "en")
	if m.root.ActiveTab() != views.TabConfig {
		t.Fatalf("active tab = %d, want config", m.root.ActiveTab())
	}
	if m.startupCmd != nil {
		t.Fatal("forced config startup should not keep service refresh running in the background")
	}
	if !strings.Contains(m.root.StatusBar.View(120), "Config") {
		t.Fatalf("forced config status should explain current page: %s", m.root.StatusBar.View(120))
	}
}

func TestForcedRedeemKeepsServiceRefreshWhenHubReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}

	m.applyForcedInitialTab(views.TabServiceRedeem, cfg, false, "en")
	if m.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", m.root.ActiveTab())
	}
	if m.startupCmd == nil {
		t.Fatal("forced redeem should refresh service status when Hub is ready")
	}
	if !strings.Contains(m.root.StatusBar.View(120), "Service Redeem") {
		t.Fatalf("forced redeem status should explain current page: %s", m.root.StatusBar.View(120))
	}
}

func TestForcedRedeemRefreshesEvenWhenLLMConfigured(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
		MaclawLLMUrl:      "http://localhost:11434/v1",
		MaclawLLMModel:    "qwen2.5-coder:32b",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}

	m.applyForcedInitialTab(views.TabServiceRedeem, cfg, true, "en")
	if m.startupCmd == nil {
		t.Fatal("maclaw-tui redeem should refresh service status even when another LLM is configured")
	}
}

func TestStartupPrefilledRedeemCodeSuppressesRefresh(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}

	m.configureInitialTab(cfg, false, "en")
	if m.startupCmd == nil {
		t.Fatal("test setup expected service refresh before code prefill")
	}
	m.applyForcedInitialTab(views.TabServiceRedeem, cfg, false, "en")
	m.applyStartupPrefills(tuiStartupOptions{serviceRedeemCode: " ABC 123 "})

	if m.startupCmd != nil {
		t.Fatal("prefilled service code should suppress startup refresh so the code stays ready")
	}
	if got := m.root.Service.CodeValueForTest(); got != "ABC123" {
		t.Fatalf("prefilled code = %q, want ABC123", got)
	}
}

func TestStartupMCPAddModeOpensTemplate(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	m.applyStartupPrefills(tuiStartupOptions{mcpAddMode: "remote"})
	if m.root.ActiveTab() != views.TabTools || m.root.Tools.ActiveSubTab() != views.ToolSubMCP || m.root.Tools.MCPAddMode() != "remote" {
		t.Fatalf("startup mcp mode active tab/sub/add = %d/%d/%q, want tools/MCP/remote", m.root.ActiveTab(), m.root.Tools.ActiveSubTab(), m.root.Tools.MCPAddMode())
	}
}

func TestStartupMCPAutoModeOpensTemplateChoicesWhenEmpty(t *testing.T) {
	empty := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	empty.applyStartupPrefills(tuiStartupOptions{mcpAddMode: mcpAddModeAutoLocal})
	if empty.root.ActiveTab() != views.TabTools || empty.root.Tools.ActiveSubTab() != views.ToolSubMCP || empty.root.Tools.MCPAddMode() != "" {
		t.Fatalf("empty auto mcp active tab/sub/add = %d/%d/%q, want tools/MCP/template choices", empty.root.ActiveTab(), empty.root.Tools.ActiveSubTab(), empty.root.Tools.MCPAddMode())
	}
	if !strings.Contains(empty.root.StatusBar.View(120), "MCP templates") {
		t.Fatalf("empty auto mcp status should describe template choices: %s", empty.root.StatusBar.View(120))
	}

	configured := &tuiModel{
		app: &TUIApp{appConfig: corelib.AppConfig{
			Language:        "en",
			LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
		}},
		root: views.NewRootModel("en"),
	}
	configured.applyStartupPrefills(tuiStartupOptions{mcpAddMode: mcpAddModeAutoLocal})
	if configured.root.ActiveTab() != views.TabTools || configured.root.Tools.ActiveSubTab() != views.ToolSubMCP {
		t.Fatalf("configured auto mcp active tab/sub = %d/%d, want tools/MCP", configured.root.ActiveTab(), configured.root.Tools.ActiveSubTab())
	}
	if configured.root.Tools.MCPAddMode() != "" {
		t.Fatalf("configured auto mcp should show list instead of add form, got %q", configured.root.Tools.MCPAddMode())
	}
	if !strings.Contains(configured.root.StatusBar.View(120), "MCP list") {
		t.Fatalf("configured auto mcp status should describe list flow: %s", configured.root.StatusBar.View(120))
	}
}

func TestConfigureInitialTabOpensLLMConfigWithoutHub(t *testing.T) {
	cfg := corelib.AppConfig{Language: "en", OnboardingDone: true}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	m.configureInitialTab(cfg, false, "en")
	if m.root.ActiveTab() != views.TabConfig {
		t.Fatalf("active tab = %d, want config", m.root.ActiveTab())
	}
	if m.root.Config.ActiveTab() != views.CfgTabLLM {
		t.Fatalf("config active tab = %d, want LLM", m.root.Config.ActiveTab())
	}
}

func TestChatSendWithoutLLMOpensSetup(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{Language: "en"}},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ChatSendMsg{Text: "hello"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("active tab = %d, want onboarding", got.root.ActiveTab())
	}
	msgs := got.root.Chat.GetMessages()
	if !strings.Contains(msgs[len(msgs)-1].Content, "I opened the next setup page") {
		t.Fatalf("missing chat guidance: %#v", msgs[len(msgs)-1])
	}
}

func TestChatSendWithoutLLMOpensServiceRedeemWhenHubReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		OnboardingDone:    true,
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ChatSendMsg{Text: "hello"})
	if cmd == nil {
		t.Fatal("expected service status refresh command")
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got.root.ActiveTab())
	}
}

func TestChatSendWithoutLLMUsesLegacyHubReadyConfig(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ChatSendMsg{Text: "hello"})
	if cmd == nil {
		t.Fatal("expected service status refresh command")
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem for legacy Hub-ready config", got.root.ActiveTab())
	}
}

func TestChatSendWithoutLLMUsesServiceReadyWithSavedEmail(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:          "en",
		RemoteEmail:       "user@example.com",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ChatSendMsg{Text: "hello"})
	if cmd == nil {
		t.Fatal("expected service status refresh command")
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem when Hub service credentials are ready", got.root.ActiveTab())
	}
}

func TestChatSendWithoutLLMOpensSetupForIncompleteRemote(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:       "en",
		OnboardingDone: true,
		RemoteEmail:    "user@example.com",
		RemoteHubURL:   "https://hub.example",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ChatSendMsg{Text: "hello"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabOnboarding {
		t.Fatalf("active tab = %d, want onboarding", got.root.ActiveTab())
	}
	if !strings.Contains(got.root.StatusBar.View(120), "incomplete") {
		t.Fatalf("status bar should explain incomplete setup: %s", got.root.StatusBar.View(120))
	}
}

func TestChatSendWithoutLLMOpensConfigWhenOnboardedWithoutHub(t *testing.T) {
	cfg := corelib.AppConfig{Language: "en", OnboardingDone: true}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ChatSendMsg{Text: "hello"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabConfig {
		t.Fatalf("active tab = %d, want config", got.root.ActiveTab())
	}
	if got.root.Config.ActiveTab() != views.CfgTabLLM {
		t.Fatalf("config active tab = %d, want LLM", got.root.Config.ActiveTab())
	}
}

func TestChatSendWithoutLLMUsesLegacyLLMConfigAsSetupReady(t *testing.T) {
	cfg := corelib.AppConfig{
		Language:       "en",
		MaclawLLMUrl:   "http://localhost:11434/v1",
		MaclawLLMModel: "qwen2.5-coder:32b",
	}
	m := &tuiModel{
		app:  &TUIApp{appConfig: cfg},
		root: views.NewRootModel("en"),
	}
	updated, cmd := m.Update(views.ChatSendMsg{Text: "hello"})
	if cmd != nil {
		t.Fatalf("unexpected command: %v", cmd)
	}
	got := updated.(*tuiModel)
	if got.root.ActiveTab() != views.TabConfig {
		t.Fatalf("active tab = %d, want config for legacy LLM-ready config", got.root.ActiveTab())
	}
	if got.root.Config.ActiveTab() != views.CfgTabLLM {
		t.Fatalf("config active tab = %d, want LLM", got.root.Config.ActiveTab())
	}
}

func TestExtractCodeGenSSOTokenInput(t *testing.T) {
	cases := map[string]string{
		"raw-token":                                        "raw-token",
		"https://callback.example/?token=abc123":           "abc123",
		"https://callback.example/?access_token=xyz":       "xyz",
		"https://callback.example/#access_token=fragment":  "fragment",
		"https://callback.example/#header.payload.sig":     "header.payload.sig",
		"http://127.0.0.1:12345/?header.payload.signature": "header.payload.signature",
	}
	for input, want := range cases {
		if got := extractCodeGenSSOTokenInput(input); got != want {
			t.Fatalf("extractCodeGenSSOTokenInput(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestApplyCodeGenSSOResultToConfigSetsUsableProviderDefaults(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{Name: "Custom", TimeoutSec: 300}},
	}
	applyCodeGenSSOResultToConfig(&cfg, oauth.CodeGenSSOResult{
		AccessToken:   "sso-token",
		BaseURL:       "https://codegen.example/api/v1",
		ModelID:       "codegen-model",
		ContextLength: 220000,
		Models: []oauth.CodeGenModel{
			{ID: "codegen-model"},
			{ID: "codegen-next"},
		},
	})

	if cfg.MaclawLLMCurrentProvider != "CodeGen" || len(cfg.MaclawLLMProviders) != 2 {
		t.Fatalf("CodeGen provider not selected/prepended: current=%q providers=%#v", cfg.MaclawLLMCurrentProvider, cfg.MaclawLLMProviders)
	}
	provider := cfg.MaclawLLMProviders[0]
	if provider.Name != "CodeGen" || provider.AuthType != "sso" || provider.Key != "sso-token" || provider.Model != "codegen-model" {
		t.Fatalf("CodeGen provider fields = %#v", provider)
	}
	if provider.TimeoutSec != corelib.DefaultLLMTimeoutSec || cfg.MaclawLLMTimeoutSec != corelib.DefaultLLMTimeoutSec {
		t.Fatalf("CodeGen timeout = provider %d config %d, want %d", provider.TimeoutSec, cfg.MaclawLLMTimeoutSec, corelib.DefaultLLMTimeoutSec)
	}
	if got := strings.Join(provider.Models, ","); got != "codegen-model,codegen-next" {
		t.Fatalf("CodeGen provider models = %q", got)
	}
	if provider.ContextLength != 220000 || cfg.MaclawLLMContextLength != 220000 {
		t.Fatalf("CodeGen context = provider %d config %d", provider.ContextLength, cfg.MaclawLLMContextLength)
	}
	if cfg.MaclawLLMUrl != "https://codegen.example/api/v1" || cfg.MaclawLLMKey != "sso-token" || cfg.MaclawLLMModel != "codegen-model" {
		t.Fatalf("top-level LLM config not synced: url=%q key=%q model=%q", cfg.MaclawLLMUrl, cfg.MaclawLLMKey, cfg.MaclawLLMModel)
	}
}

func TestCancelCodeGenSSOFlowInvokesRegisteredCancel(t *testing.T) {
	m := &tuiModel{}
	canceled := false
	entry := m.registerCodeGenSSOCancel("flow-1", func() { canceled = true })

	m.cancelCodeGenSSOFlow("flow-1")
	if !canceled {
		t.Fatal("cancelCodeGenSSOFlow should invoke registered cancel")
	}
	select {
	case <-entry.done:
	default:
		t.Fatal("cancelCodeGenSSOFlow should close done channel")
	}

	canceled = false
	m.cancelCodeGenSSOFlow("flow-1")
	if canceled {
		t.Fatal("cancelCodeGenSSOFlow should remove cancel after first use")
	}
}

func TestRegisterCodeGenSSOCancelClosesPreviousDone(t *testing.T) {
	m := &tuiModel{}
	canceled := false
	first := m.registerCodeGenSSOCancel("flow-1", func() { canceled = true })
	second := m.registerCodeGenSSOCancel("flow-1", func() {})
	if second == nil {
		t.Fatal("second register returned nil")
	}
	if !canceled {
		t.Fatal("re-registering a flow should cancel the previous callback")
	}
	select {
	case <-first.done:
	default:
		t.Fatal("re-registering a flow should close the previous done channel")
	}
	select {
	case <-second.done:
		t.Fatal("new done channel should remain open")
	default:
	}
}

func TestStaleCodeGenSSOSuccessDoesNotReloadConfig(t *testing.T) {
	m := &tuiModel{
		app:  &TUIApp{appConfig: corelib.AppConfig{MaclawLLMCurrentProvider: "before-sentinel"}},
		root: views.NewRootModel("en"),
	}
	m.root.StatusBar.SetMessage("before")

	updated, cmd := m.Update(views.OnboardingSSOResultMsg{FlowID: "stale-flow", Success: true, ModelID: "new-model"})
	if cmd != nil {
		t.Fatalf("stale success command = %v, want nil", cmd)
	}
	got := updated.(*tuiModel)
	if got.app.appConfig.MaclawLLMCurrentProvider != "before-sentinel" {
		t.Fatalf("stale SSO success reloaded config: provider=%q", got.app.appConfig.MaclawLLMCurrentProvider)
	}
	if strings.Contains(got.root.View(), tuiText("en", "codeGenSSOSuccess")) {
		t.Fatal("stale SSO success should not show success status")
	}
}

func TestCodeGenSSOParamsLifecycle(t *testing.T) {
	m := &tuiModel{}
	params := &oauth.HeadlessOAuthParams{AuthURL: "https://example.com", RedirectURI: "http://localhost/callback", Verifier: "verifier"}
	m.storeCodeGenSSOParams("flow-1", params)
	if got := m.codeGenSSOParams("flow-1"); got != params {
		t.Fatalf("codeGenSSOParams() = %#v, want %#v", got, params)
	}
	m.clearCodeGenSSOParams("flow-1")
	if got := m.codeGenSSOParams("flow-1"); got != nil {
		t.Fatalf("codeGenSSOParams() after clear = %#v, want nil", got)
	}
}
