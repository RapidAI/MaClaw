package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/views"
)

func TestGUITUINewMachineStartsOnboarding(t *testing.T) {
	root := views.NewRootModel("zh")

	configureGUITUIInitialTab(&root, corelib.AppConfig{})

	if got := root.ActiveTab(); got != views.TabOnboarding {
		t.Fatalf("active tab = %d, want onboarding", got)
	}
}

func TestGUITUIConfiguredLLMStartsChat(t *testing.T) {
	root := views.NewRootModel("zh")
	cfg := corelib.AppConfig{
		MaclawLLMUrl:             "https://api.example/v1",
		MaclawLLMKey:             "key",
		MaclawLLMModel:           "model",
		MaclawLLMCurrentProvider: "Custom",
	}

	configureGUITUIInitialTab(&root, cfg)

	if got := root.ActiveTab(); got != views.TabChat {
		t.Fatalf("active tab = %d, want chat", got)
	}
}

func TestGUITUIConfiguredLLMWithoutKeyStartsConfigKey(t *testing.T) {
	root := views.NewRootModel("zh")
	cfg := corelib.AppConfig{
		MaclawLLMUrl:             "https://api.example/v1",
		MaclawLLMModel:           "model",
		MaclawLLMCurrentProvider: "Custom",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "Custom",
			URL:      "https://api.example/v1",
			Model:    "model",
			AuthType: "bearer",
		}},
	}

	configureGUITUIInitialTab(&root, cfg)

	if got := root.ActiveTab(); got != views.TabConfig {
		t.Fatalf("active tab = %d, want config", got)
	}
}

func TestGUITUIHubReadyWithoutLLMStartsServiceRedeem(t *testing.T) {
	root := views.NewRootModel("zh")
	cfg := corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}

	configureGUITUIInitialTab(&root, cfg)

	if got := root.ActiveTab(); got != views.TabServiceRedeem {
		t.Fatalf("active tab = %d, want service redeem", got)
	}
}

func TestGUITUIOnboardingDoneWithoutLLMStartsConfig(t *testing.T) {
	root := views.NewRootModel("zh")

	configureGUITUIInitialTab(&root, corelib.AppConfig{OnboardingDone: true})

	if got := root.ActiveTab(); got != views.TabConfig {
		t.Fatalf("active tab = %d, want config", got)
	}
}

func TestGUITUIStartWeixinQRReportsEmptyResponse(t *testing.T) {
	app := &tuiModeApp{}

	msg := app.startWeixinQR()()

	result, ok := msg.(views.OnboardingWeixinQRMsg)
	if !ok {
		t.Fatalf("message = %T, want OnboardingWeixinQRMsg", msg)
	}
	if result.Success {
		t.Fatal("nil app should not report QR success")
	}
}

func TestGUITUIPollWeixinQRReportsEmptyApp(t *testing.T) {
	app := &tuiModeApp{}

	msg := app.pollWeixinQR("token")()

	result, ok := msg.(views.OnboardingWeixinPollResultMsg)
	if !ok {
		t.Fatalf("message = %T, want OnboardingWeixinPollResultMsg", msg)
	}
	if !result.Completed || result.Status != "error" {
		t.Fatalf("poll result = %#v, want completed error", result)
	}
}

func TestGUITUIPollWeixinQRReportsEmptyToken(t *testing.T) {
	app := &tuiModeApp{root: views.NewRootModel("en")}

	msg := app.pollWeixinQR("  ")()

	result, ok := msg.(views.OnboardingWeixinPollResultMsg)
	if !ok {
		t.Fatalf("message = %T, want OnboardingWeixinPollResultMsg", msg)
	}
	if !result.Completed || result.Status != "error" || !strings.Contains(result.Message, "incomplete") {
		t.Fatalf("poll result = %#v, want completed empty-token error", result)
	}
}

func TestGUITUIWeixinQRStatusMessageFallbacks(t *testing.T) {
	checks := map[string]string{
		"wait":    "等待微信扫码",
		"scanned": "已扫码",
		"scaned":  "已扫码",
		"expired": "已过期",
	}
	for status, want := range checks {
		if got := guiTUIWeixinQRStatusMessage("zh", status); !strings.Contains(got, want) {
			t.Fatalf("status %q message = %q, want %q", status, got, want)
		}
	}
}

func TestGUITUIWeixinQRStatusMessageFollowsLanguage(t *testing.T) {
	got := guiTUIWeixinQRStatusMessage("en", "scanned")
	if !strings.Contains(got, "Confirm on your phone") {
		t.Fatalf("English scanned status = %q", got)
	}
}

func TestGUITUINormalizeWeixinQRStatusKeepsError(t *testing.T) {
	if got := guiTUINormalizeWeixinQRStatus(" "); got != "error" {
		t.Fatalf("normalized empty status = %q", got)
	}
	if got := guiTUINormalizeWeixinQRStatus(" error "); got != "error" {
		t.Fatalf("normalized error status = %q", got)
	}
	if got := guiTUINormalizeWeixinQRStatus("scanned"); got != "scaned" {
		t.Fatalf("normalized scanned status = %q", got)
	}
	if got := guiTUINormalizeWeixinQRStatus("failed"); got != "failed" {
		t.Fatalf("normalized failed status = %q", got)
	}
}

func TestGUITUIWeixinQRStatusMessageHandlesFailure(t *testing.T) {
	got := guiTUIWeixinQRStatusMessage("en", "failed")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "try again") {
		t.Fatalf("English failed status = %q", got)
	}
}

func TestGUITUIFinishOnboardingUsesMessage(t *testing.T) {
	app := &tuiModeApp{}

	msg := app.finishOnboarding()()

	result, ok := msg.(guiTUIOnboardingFinishedMsg)
	if !ok {
		t.Fatalf("message = %T, want guiTUIOnboardingFinishedMsg", msg)
	}
	if result.Error == "" {
		t.Fatal("nil app should return an onboarding finish error")
	}
}

func TestGUITUIOnboardingFinishedRoutesBySetupStatus(t *testing.T) {
	app := &tuiModeApp{root: views.NewRootModel("en")}

	_, _ = app.Update(guiTUIOnboardingFinishedMsg{Config: corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}})
	if got := app.root.ActiveTab(); got != views.TabServiceRedeem {
		t.Fatalf("hub-ready tab = %d, want service redeem", got)
	}

	_, _ = app.Update(guiTUIOnboardingFinishedMsg{Config: corelib.AppConfig{
		MaclawLLMUrl:             "https://api.example/v1",
		MaclawLLMKey:             "key",
		MaclawLLMModel:           "model",
		MaclawLLMCurrentProvider: "Custom",
	}})
	if got := app.root.ActiveTab(); got != views.TabChat {
		t.Fatalf("llm-ready tab = %d, want chat", got)
	}
}

func TestGUITUIConfigSaveReportsEmptyApp(t *testing.T) {
	app := &tuiModeApp{}

	msg := app.saveConfig(views.ConfigSaveMsg{Key: "language", Value: "en"})()

	result, ok := msg.(views.ConfigSaveFailedMsg)
	if !ok {
		t.Fatalf("message = %T, want ConfigSaveFailedMsg", msg)
	}
	if result.Error == "" {
		t.Fatal("nil app should return a config save error")
	}
}

func TestGUITUIConfigSaveRejectsHubManagedSecurityKey(t *testing.T) {
	backend := &App{testHomeDir: t.TempDir()}
	cfg := corelib.AppConfig{Language: "en", HubSecurityCentralized: true, SecurityPolicyMode: "strict", SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}
	if err := backend.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app := &tuiModeApp{app: backend}

	msg := app.saveConfig(views.ConfigSaveMsg{Key: "security_policy_mode", Value: "developer"})()
	failed, ok := msg.(views.ConfigSaveFailedMsg)
	if !ok {
		t.Fatalf("message = %T, want ConfigSaveFailedMsg", msg)
	}
	if !strings.Contains(failed.Error, "Hub") {
		t.Fatalf("failure = %#v, want Hub-managed reason", failed)
	}
	loaded, err := backend.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.SecurityPolicyMode != "strict" || loaded.SandboxMode != "os" || loaded.NetworkLevel != "none" {
		t.Fatalf("managed security config changed: %#v", loaded)
	}
}

func TestGUITUIConfigSnapshotPreservesHubManagedSecurity(t *testing.T) {
	backend := &App{testHomeDir: t.TempDir()}
	current := corelib.AppConfig{Language: "zh", HubSecurityCentralized: true, SecurityPolicyMode: "strict", SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: false, ImageOutboundEnabled: false}
	if err := backend.SaveConfig(current); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app := &tuiModeApp{app: backend}
	snapshot := current
	snapshot.Language = "en"
	snapshot.HubSecurityCentralized = false
	snapshot.SecurityPolicyMode = "developer"
	snapshot.SandboxMode = "none"
	snapshot.NetworkLevel = "full"
	snapshot.FileOutboundEnabled = true
	snapshot.ImageOutboundEnabled = true

	msg := app.saveConfig(views.ConfigSaveMsg{Key: "language", Value: "en", Config: snapshot, HasConfig: true})()
	if _, ok := msg.(views.ConfigSavedMsg); !ok {
		t.Fatalf("message = %T, want ConfigSavedMsg", msg)
	}
	loaded, err := backend.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Language != "en" {
		t.Fatalf("language = %q, want en", loaded.Language)
	}
	if !loaded.HubSecurityCentralized || loaded.SecurityPolicyMode != "strict" || loaded.SandboxMode != "os" || loaded.NetworkLevel != "none" || loaded.FileOutboundEnabled || loaded.ImageOutboundEnabled {
		t.Fatalf("managed security config was overwritten: %#v", loaded)
	}
}

func TestGUITUIConfigSavedLanguageUpdatesRoot(t *testing.T) {
	app := &tuiModeApp{root: views.NewRootModel("zh")}

	_, _ = app.Update(views.ConfigSavedMsg{Key: "language", Value: "en"})

	if got := app.root.Lang(); got != "en" {
		t.Fatalf("root language = %q, want en", got)
	}
}

func TestGUITUISendMessageReportsEmptyApp(t *testing.T) {
	app := &tuiModeApp{root: views.NewRootModel("en")}

	msg := app.sendMessage("hello")()

	result, ok := msg.(views.ChatResponseMsg)
	if !ok {
		t.Fatalf("message = %T, want ChatResponseMsg", msg)
	}
	if result.Error == "" {
		t.Fatal("nil app should return a chat error")
	}
}

func TestGUITUIInitDoesNotDelayFirstRender(t *testing.T) {
	app := &tuiModeApp{root: views.NewRootModel("en")}

	if cmd := app.Init(); cmd != nil {
		t.Fatal("Init should not delay the first TUI render")
	}
	if strings.Contains(app.View(), "Initializing") {
		t.Fatalf("ready TUI should render root view, got:\n%s", app.View())
	}
}

func TestGUITUIServiceExpiryAndCreditsPrefersEffectiveValues(t *testing.T) {
	expires, credits := guiTUIServiceExpiryAndCredits(HubLLMServiceStatus{
		EffectiveExpiresAt: "2026-01-02T00:00:00Z",
		NearestExpiresAt:   "2026-01-01T00:00:00Z",
		CreditsRemaining:   3,
		CreditsAvailable:   7,
	})

	if expires != "2026-01-02T00:00:00Z" || credits != 3 {
		t.Fatalf("expires=%q credits=%v, want effective/remaining", expires, credits)
	}
}

func TestGUITUIServiceSuccessResultCarriesConfig(t *testing.T) {
	cfg := corelib.AppConfig{RemoteHubURL: "https://hub.example"}
	result := guiTUIServiceSuccessResult("ready", HubLLMServiceStatus{
		Active:             true,
		HubLLMBaseURL:      "https://hub.example/v1",
		EffectiveExpiresAt: "2026-01-02T00:00:00Z",
		CreditsRemaining:   5,
	}, cfg, true)

	if !result.Success || !result.HasConfig || !result.FromRefresh {
		t.Fatalf("result flags = %#v, want successful refresh with config", result)
	}
	if result.Config.RemoteHubURL != cfg.RemoteHubURL || result.CreditsRemaining != 5 {
		t.Fatalf("result = %#v, want config and credits preserved", result)
	}
}

func TestGUITUIHubServiceReadyRequiresActiveBaseURL(t *testing.T) {
	if guiTUIHubServiceReady(HubLLMServiceStatus{Active: true}) {
		t.Fatal("active service without base URL should not be ready")
	}
	if guiTUIHubServiceReady(HubLLMServiceStatus{HubLLMBaseURL: "https://hub.example/v1"}) {
		t.Fatal("base URL without active status should not be ready")
	}
	if !guiTUIHubServiceReady(HubLLMServiceStatus{Active: true, HubLLMBaseURL: "https://hub.example/v1"}) {
		t.Fatal("active service with base URL should be ready")
	}
}

func TestGUITUIToolDataLoadsMCPServers(t *testing.T) {
	root := views.NewRootModel("en")
	cfg := corelib.AppConfig{
		LocalMCPServers: []corelib.LocalMCPServerEntry{{
			ID:      "local-1",
			Name:    "Filesystem",
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
		}},
		MCPServers: []corelib.MCPServerEntry{{
			ID:          "remote-1",
			Name:        "Remote",
			EndpointURL: "https://mcp.example/sse",
		}},
	}

	loadGUITUIToolData(&root, cfg)

	root.SetTab(views.TabTools)
	root.Tools.FocusMCP()
	view := root.View()
	if !strings.Contains(view, "Filesystem") || !strings.Contains(view, "Remote") {
		t.Fatalf("MCP servers missing from view:\n%s", view)
	}
}

func TestGUITUIMCPIDSanitizesName(t *testing.T) {
	now := time.UnixMilli(1700000000123)

	got := guiTUIMCPID("local", " File System! ", now)

	if got != "local_File_System_1700000000123" {
		t.Fatalf("id = %q, want sanitized local id", got)
	}
}

func TestGUITUIAddMCPRejectsMissingFieldsBeforeAppUse(t *testing.T) {
	app := &tuiModeApp{}

	localMsg := app.addLocalMCP(corelib.LocalMCPServerEntry{Name: "  "})()
	localResult, ok := localMsg.(views.ToolOperationResultMsg)
	if !ok {
		t.Fatalf("local message = %T, want ToolOperationResultMsg", localMsg)
	}
	if localResult.Success || localResult.Message == "" {
		t.Fatalf("local result = %#v, want validation failure", localResult)
	}

	remoteMsg := app.addRemoteMCP(corelib.MCPServerEntry{Name: "remote"})()
	remoteResult, ok := remoteMsg.(views.ToolOperationResultMsg)
	if !ok {
		t.Fatalf("remote message = %T, want ToolOperationResultMsg", remoteMsg)
	}
	if remoteResult.Success || remoteResult.Message == "" {
		t.Fatalf("remote result = %#v, want validation failure", remoteResult)
	}
}

func TestGUITUIAddMCPHonorsHubSecurityPolicy(t *testing.T) {
	tmpHome := t.TempDir()
	backend := &App{testHomeDir: tmpHome}
	if err := backend.SaveConfig(corelib.AppConfig{HubSecurityCentralized: true, SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: true, ImageOutboundEnabled: true}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app := &tuiModeApp{app: backend}

	localMsg := app.addLocalMCP(corelib.LocalMCPServerEntry{Name: "local", Command: "node", Args: []string{"server.js"}})()
	localResult, ok := localMsg.(views.ToolOperationResultMsg)
	if !ok {
		t.Fatalf("local message = %T, want ToolOperationResultMsg", localMsg)
	}
	if localResult.Success || !strings.Contains(localResult.Message, "sandbox") {
		t.Fatalf("local result = %#v, want sandbox rejection", localResult)
	}

	remoteMsg := app.addRemoteMCP(corelib.MCPServerEntry{Name: "remote", EndpointURL: "https://mcp.example/rpc"})()
	remoteResult, ok := remoteMsg.(views.ToolOperationResultMsg)
	if !ok {
		t.Fatalf("remote message = %T, want ToolOperationResultMsg", remoteMsg)
	}
	if remoteResult.Success || !strings.Contains(remoteResult.Message, "network") {
		t.Fatalf("remote result = %#v, want network rejection", remoteResult)
	}

	loaded, err := backend.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loaded.LocalMCPServers) != 0 || len(loaded.MCPServers) != 0 {
		t.Fatalf("blocked MCP entries persisted: local=%d remote=%d", len(loaded.LocalMCPServers), len(loaded.MCPServers))
	}
}
