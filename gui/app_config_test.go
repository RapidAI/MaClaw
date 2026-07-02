package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreconfig "github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/user"
)

func TestLoadConfigConcurrentFirstRun(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}

	const workers = 12
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := app.LoadConfig(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read config.json error = %v", err)
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Unmarshal config.json error = %v", err)
	}
	if cfg.ActiveTool != "claude" {
		t.Fatalf("ActiveTool = %q, want %q", cfg.ActiveTool, "claude")
	}
	if !cfg.CheckUpdateOnStartup {
		t.Fatal("CheckUpdateOnStartup = false, want true for first-run default config")
	}
	if !cfg.VectorSearchEnabled {
		t.Fatal("VectorSearchEnabled = false, want true for first-run default config")
	}
	if !cfg.ASREnabled {
		t.Fatal("ASREnabled = false, want true for first-run default config")
	}
	if !cfg.TTSEnabled {
		t.Fatal("TTSEnabled = false, want true for first-run default config")
	}
	if cfg.ScreenParsingEnabled == nil || !*cfg.ScreenParsingEnabled {
		t.Fatal("ScreenParsingEnabled = false/nil, want true for first-run default config")
	}
	if !cfg.IsIMProgressNudgeEnabled() {
		t.Fatal("IMProgressNudgeEnabled = false, want true for first-run default config")
	}
	if cfg.ShowAppEntry {
		t.Fatal("ShowAppEntry = true, want false for first-run default config")
	}

	matches, err := filepath.Glob(configPath + ".tmp*")
	if err != nil {
		t.Fatalf("Glob temp files error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files remain: %v", matches)
	}
}

func TestSyncToCodeBuddySettingsNormalizesOpenAIChatEndpoint(t *testing.T) {
	tmpHome := t.TempDir()
	projectDir := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		CodeBuddy: corelib.ToolConfig{
			CurrentModel: "GLM",
			Models: []corelib.ModelConfig{{
				ModelName: "GLM",
				ModelId:   "glm-5.1",
				ModelUrl:  "https://open.bigmodel.cn/api/paas/v4",
				ApiKey:    "test-key",
				AgentType: "Kilo Code",
			}},
		},
	}

	if err := app.syncToCodeBuddySettings(cfg, projectDir); err != nil {
		t.Fatalf("syncToCodeBuddySettings() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projectDir, ".codebuddy", "models.json"))
	if err != nil {
		t.Fatalf("read CodeBuddy models.json: %v", err)
	}
	var out corelib.CodeBuddyFileConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal CodeBuddy models.json: %v", err)
	}
	if len(out.Models) != 1 {
		t.Fatalf("models len = %d, want 1: %#v", len(out.Models), out.Models)
	}
	if got, want := out.Models[0].Url, "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions"; got != want {
		t.Fatalf("CodeBuddy model URL = %q, want %q", got, want)
	}
}

func TestOpenAICompatibleToolConfigNormalizesGLMCodingPlanBaseURL(t *testing.T) {
	tmpHome := t.TempDir()
	projectDir := t.TempDir()
	app := &App{testHomeDir: tmpHome}

	opencodeCfg := corelib.AppConfig{
		Opencode: corelib.ToolConfig{
			CurrentModel: "GLM",
			Models: []corelib.ModelConfig{{
				ModelName: "GLM",
				ModelId:   "glm-5.1",
				ModelUrl:  "https://open.bigmodel.cn/api/paas/v4",
				ApiKey:    "test-key",
			}},
		},
	}
	if err := app.syncToOpencodeSettings(opencodeCfg, projectDir, "session-1"); err != nil {
		t.Fatalf("syncToOpencodeSettings() error = %v", err)
	}
	opencodeData, err := os.ReadFile(filepath.Join(projectDir, ".aicoder", "opencode", "session-1", "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode config: %v", err)
	}
	var opencodeOut map[string]interface{}
	if err := json.Unmarshal(opencodeData, &opencodeOut); err != nil {
		t.Fatalf("unmarshal opencode config: %v", err)
	}
	provider := opencodeOut["provider"].(map[string]interface{})["myprovider"].(map[string]interface{})
	options := provider["options"].(map[string]interface{})
	if got, want := options["baseURL"], "https://open.bigmodel.cn/api/coding/paas/v4"; got != want {
		t.Fatalf("OpenCode baseURL = %#v, want %q", got, want)
	}

	kiloCfg := corelib.AppConfig{
		Kilo: corelib.ToolConfig{
			CurrentModel: "GLM",
			Models: []corelib.ModelConfig{{
				ModelName: "GLM",
				ModelId:   "glm-5.1",
				ModelUrl:  "https://open.bigmodel.cn/api/paas/v4",
				ApiKey:    "test-key",
			}},
		},
	}
	if err := app.syncToKiloSettings(kiloCfg, projectDir, "session-2"); err != nil {
		t.Fatalf("syncToKiloSettings() error = %v", err)
	}
	kiloData, err := os.ReadFile(filepath.Join(projectDir, ".aicoder", "kilocode", "cli", "session-2", "config.json"))
	if err != nil {
		t.Fatalf("read kilo config: %v", err)
	}
	var kiloOut map[string]interface{}
	if err := json.Unmarshal(kiloData, &kiloOut); err != nil {
		t.Fatalf("unmarshal kilo config: %v", err)
	}
	providers := kiloOut["providers"].([]interface{})
	kiloProvider := providers[0].(map[string]interface{})
	if got, want := kiloProvider["openAiBaseUrl"], "https://open.bigmodel.cn/api/coding/paas/v4"; got != want {
		t.Fatalf("Kilo openAiBaseUrl = %#v, want %q", got, want)
	}
}

func TestLoadConfigDefaultsUseGLM51ForOpenAICompatibleTools(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	for name, toolCfg := range map[string]corelib.ToolConfig{
		"codex":     cfg.Codex,
		"opencode":  cfg.Opencode,
		"codebuddy": cfg.CodeBuddy,
		"iflow":     cfg.IFlow,
		"kilo":      cfg.Kilo,
	} {
		found := false
		for _, model := range toolCfg.Models {
			if model.ModelName != "GLM" {
				continue
			}
			found = true
			if model.ModelUrl != "https://open.bigmodel.cn/api/coding/paas/v4" {
				t.Fatalf("%s GLM URL = %q", name, model.ModelUrl)
			}
			if model.ModelId != "GLM-5.2" {
				t.Fatalf("%s GLM model = %q, want GLM-5.2", name, model.ModelId)
			}
		}
		if !found {
			t.Fatalf("%s GLM provider not found", name)
		}
	}
}

func TestLoadConfigNormalizesRemoteHeartbeatSec(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHeartbeatSec: 0}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RemoteHeartbeatSec != corelib.DefaultRemoteHeartbeatSec {
		t.Fatalf("RemoteHeartbeatSec = %d, want %d", cfg.RemoteHeartbeatSec, corelib.DefaultRemoteHeartbeatSec)
	}
}

func TestLoadConfigDefaultsLocalAIModelsWhenFieldsAbsent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".maclaw")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("MkdirAll config dir error = %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"active_tool":"message"}`), 0644); err != nil {
		t.Fatalf("Write config.json error = %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.VectorSearchEnabled {
		t.Fatal("absent vector_search_enabled should default to true")
	}
	if !cfg.ASREnabled {
		t.Fatal("absent asr_enabled should default to true")
	}
	if !cfg.TTSEnabled {
		t.Fatal("absent tts_enabled should default to true")
	}
	if cfg.ScreenParsingEnabled == nil || !*cfg.ScreenParsingEnabled {
		t.Fatal("absent screen_parsing_enabled should default to true")
	}
	if !cfg.IsIMProgressNudgeEnabled() {
		t.Fatal("absent im_progress_nudge_enabled should default to true")
	}
	if cfg.SubAgentConcurrency != corelib.DefaultSubAgentConcurrency {
		t.Fatalf("absent subagent_concurrency should default to %d, got %d", corelib.DefaultSubAgentConcurrency, cfg.SubAgentConcurrency)
	}
}

func TestLoadConfigPreservesExplicitLocalAIModelDisable(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".maclaw")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("MkdirAll config dir error = %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	raw := `{
		"active_tool": "message",
		"vector_search_enabled": false,
		"asr_enabled": false,
		"tts_enabled": false,
		"screen_parsing_enabled": false,
		"im_progress_nudge_enabled": false
	}`
	if err := os.WriteFile(configPath, []byte(raw), 0644); err != nil {
		t.Fatalf("Write config.json error = %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.VectorSearchEnabled {
		t.Fatal("explicit vector_search_enabled=false should be preserved")
	}
	if cfg.ASREnabled {
		t.Fatal("explicit asr_enabled=false should be preserved")
	}
	if cfg.TTSEnabled {
		t.Fatal("explicit tts_enabled=false should be preserved")
	}
	if cfg.ScreenParsingEnabled == nil || *cfg.ScreenParsingEnabled {
		t.Fatal("explicit screen_parsing_enabled=false should be preserved")
	}
	if cfg.IsIMProgressNudgeEnabled() {
		t.Fatal("explicit im_progress_nudge_enabled=false should be preserved")
	}
}

func TestUpdateToastTranslationUsesReadableText(t *testing.T) {
	app := &App{}

	app.SetLanguage("en")
	if got, want := app.tr("New version %s is available. Open About to download the update.", "V9.9.9"), "New version V9.9.9 is available. Open About to download the update."; got != want {
		t.Fatalf("en update toast = %q, want %q", got, want)
	}

	app.SetLanguage("zh-Hans")
	if got, want := app.tr("New version %s is available. Open About to download the update.", "V9.9.9"), "发现新版本 V9.9.9，可前往关于页面下载更新。"; got != want {
		t.Fatalf("zh-Hans update toast = %q, want %q", got, want)
	}

	app.SetLanguage("zh-Hant")
	if got, want := app.tr("New version %s is available. Open About to download the update.", "V9.9.9"), "發現新版本 V9.9.9，可前往關於頁面下載更新。"; got != want {
		t.Fatalf("zh-Hant update toast = %q, want %q", got, want)
	}
}

func TestLoadConfigConcurrentLegacyMigration(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	legacyConfig := []byte(`{"active_tool":"claude","current_project":"legacy-project"}`)
	legacyPath := filepath.Join(tmpHome, ".aicoder_config.json")
	if err := os.WriteFile(legacyPath, legacyConfig, 0644); err != nil {
		t.Fatalf("Write legacy config error = %v", err)
	}

	app := &App{testHomeDir: tmpHome}

	const workers = 10
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := app.LoadConfig(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read migrated config error = %v", err)
	}
	if string(data) != string(legacyConfig) {
		t.Fatalf("migrated config = %q, want %q", string(data), string(legacyConfig))
	}
	var migrated map[string]any
	if err := json.Unmarshal(data, &migrated); err != nil {
		t.Fatalf("Unmarshal migrated config error = %v", err)
	}

	matches, err := filepath.Glob(configPath + ".tmp*")
	if err != nil {
		t.Fatalf("Glob temp files error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files remain: %v", matches)
	}
}

func TestLoadConfigLegacyMigrationSanitizesRemovedCodingTools(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	legacyConfig := []byte(`{"active_tool":"cursor","default_tool":"gemini","current_project":"legacy-project"}`)
	legacyPath := filepath.Join(tmpHome, ".aicoder_config.json")
	if err := os.WriteFile(legacyPath, legacyConfig, 0644); err != nil {
		t.Fatalf("Write legacy config error = %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.ActiveTool != "claude" || cfg.DefaultTool != "claude" {
		t.Fatalf("config not sanitized: active=%q default=%q", cfg.ActiveTool, cfg.DefaultTool)
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read migrated config error = %v", err)
	}
	var migrated map[string]any
	if err := json.Unmarshal(data, &migrated); err != nil {
		t.Fatalf("Unmarshal migrated config error = %v", err)
	}
	if migrated["active_tool"] != "claude" || migrated["default_tool"] != "claude" {
		t.Fatalf("migrated tools not sanitized: %#v", migrated)
	}
	if migrated["current_project"] != "legacy-project" {
		t.Fatalf("current_project = %q, want legacy-project", migrated["current_project"])
	}
}

func TestLoadConfigCachesInMemoryUntilSave(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() seed error = %v", err)
	}

	cfg.RemoteEmail = "first@example.com"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	cached, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() cached error = %v", err)
	}
	if cached.RemoteEmail != "first@example.com" {
		t.Fatalf("cached RemoteEmail = %q, want %q", cached.RemoteEmail, "first@example.com")
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read config.json error = %v", err)
	}
	var diskCfg corelib.AppConfig
	if err := json.Unmarshal(data, &diskCfg); err != nil {
		t.Fatalf("Unmarshal config.json error = %v", err)
	}
	if diskCfg.RemoteEmail != "first@example.com" {
		t.Fatalf("disk RemoteEmail = %q, want %q", diskCfg.RemoteEmail, "first@example.com")
	}

	diskCfg.RemoteEmail = "external@example.com"
	updatedData, err := json.Marshal(diskCfg)
	if err != nil {
		t.Fatalf("Marshal external config error = %v", err)
	}
	if err := os.WriteFile(configPath, updatedData, 0644); err != nil {
		t.Fatalf("Write external config error = %v", err)
	}

	stale, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() stale error = %v", err)
	}
	if stale.RemoteEmail != "first@example.com" {
		t.Fatalf("stale RemoteEmail = %q, want cached %q before invalidation", stale.RemoteEmail, "first@example.com")
	}

	app.configMu.Lock()
	app.invalidateConfigCacheLocked()
	app.configMu.Unlock()

	fresh, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() fresh error = %v", err)
	}
	if fresh.RemoteEmail != "external@example.com" {
		t.Fatalf("fresh RemoteEmail = %q, want %q", fresh.RemoteEmail, "external@example.com")
	}
}

func TestLoadConfigRefreshesLogDetailGateAfterCacheInvalidation(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() seed error = %v", err)
	}
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() enable log detail error = %v", err)
	}
	if !corelib.IsLogDetailEnabled() {
		t.Fatal("expected runtime log detail gate to be enabled after SaveConfig")
	}

	cfg.LogDetailEnabled = false
	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	updatedData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal config error = %v", err)
	}
	if err := os.WriteFile(configPath, updatedData, 0644); err != nil {
		t.Fatalf("Write config.json error = %v", err)
	}

	app.configMu.Lock()
	app.invalidateConfigCacheLocked()
	app.configMu.Unlock()
	corelib.SetLogDetailEnabled(true)

	fresh, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() fresh error = %v", err)
	}
	if fresh.LogDetailEnabled {
		t.Fatal("expected fresh config to load log detail as disabled")
	}
	if corelib.IsLogDetailEnabled() {
		t.Fatal("expected runtime log detail gate to be disabled after non-cached LoadConfig")
	}
}

func TestSaveConfigPersistsLogDetailEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if !reloaded.LogDetailEnabled {
		t.Fatal("expected LogDetailEnabled to persist as true")
	}
}

func TestPatchConfigFieldsUpdatesOnlyRequestedGeneralFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	patched, err := app.PatchConfigFields(map[string]interface{}{
		"gossip_auto_publish": false,
	})
	if err != nil {
		t.Fatalf("PatchConfigFields() error = %v", err)
	}
	if patched.GossipAutoPublish {
		t.Fatal("GossipAutoPublish = true, want false")
	}
	if patched.RemoteEmail != "owner@example.com" {
		t.Fatalf("RemoteEmail = %q, want preserved owner@example.com", patched.RemoteEmail)
	}
	if !patched.LogDetailEnabled {
		t.Fatal("LogDetailEnabled = false, want preserved true")
	}
}

func TestPatchConfigFieldsAppliesRuntimeSideEffects(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	corelib.SetLogDetailEnabled(false)
	t.Cleanup(func() { corelib.SetLogDetailEnabled(false) })
	t.Cleanup(func() { corelib.SetWorkspaceDir("") })

	app := &App{testHomeDir: tmpHome}
	if _, err := app.PatchConfigFields(map[string]interface{}{
		"log_detail_enabled": true,
		"working_directory":  filepath.Join(tmpHome, "work"),
	}); err != nil {
		t.Fatalf("PatchConfigFields() error = %v", err)
	}
	if !corelib.IsLogDetailEnabled() {
		t.Fatal("expected runtime log detail gate to be enabled after PatchConfigFields")
	}
	if got := corelib.EffectiveWorkspaceDir(); got != filepath.Join(tmpHome, "work") {
		t.Fatalf("WorkspaceDir = %q, want patched working directory", got)
	}
}

func TestPatchConfigFieldsWorkflowEnabledToggle(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}

	// Default: workflowDisabled atomic zero-value is false (no config loaded yet)
	if app.workflowDisabled.Load() {
		t.Fatal("workflowDisabled should be false by default (atomic zero-value)")
	}

	// Turn off workflow
	patched, err := app.PatchConfigFields(map[string]interface{}{
		"workflow_enabled": false,
	})
	if err != nil {
		t.Fatalf("PatchConfigFields(workflow_enabled=false) error = %v", err)
	}
	if patched.IsWorkflowEnabled() {
		t.Fatal("patched config should have workflow_enabled=false")
	}
	if !app.workflowDisabled.Load() {
		t.Fatal("workflowDisabled atomic should be true after disabling workflow")
	}

	// Turn on workflow
	patched, err = app.PatchConfigFields(map[string]interface{}{
		"workflow_enabled": true,
	})
	if err != nil {
		t.Fatalf("PatchConfigFields(workflow_enabled=true) error = %v", err)
	}
	if !patched.IsWorkflowEnabled() {
		t.Fatal("patched config should have workflow_enabled=true")
	}
	if app.workflowDisabled.Load() {
		t.Fatal("workflowDisabled atomic should be false after re-enabling workflow")
	}

	// Verify persistence: reload from disk
	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !reloaded.IsWorkflowEnabled() {
		t.Fatal("workflow_enabled=true should survive reload from disk")
	}
}

func TestPatchConfigFieldsUpdatesExtendedScalarFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	patched, err := app.PatchConfigFields(map[string]interface{}{
		"pause_env_check":              true,
		"env_check_done":               true,
		"language":                     "zh-Hans",
		"active_tool":                  "codex",
		"current_project":              "p2",
		"favorite_employees":           []interface{}{"ve1", "ve2"},
		"favorite_employee_names":      map[string]interface{}{"ve1": "Reviewer"},
		"tool_current_model":           map[string]interface{}{"tool": "codex", "model": "Original"},
		"remote_enabled":               true,
		"remote_hub_url":               " https://hub.example.com/ ",
		"remote_hubcenter_url":         " https://hubs.example.com ",
		"remote_email":                 " owner@example.com ",
		"remote_mobile":                " 13800138000 ",
		"onboarding_done":              true,
		"default_launch_mode":          "remote",
		"security_policy_mode":         "strict",
		"sandbox_mode":                 "os",
		"network_level":                "allowlist",
		"network_allowlist":            []interface{}{"example.com"},
		"yolo_mode_allowed":            false,
		"smart_route_enabled":          true,
		"gossip_enabled":               true,
		"file_outbound_enabled":        false,
		"image_outbound_enabled":       false,
		"skill_sources_allowed":        []interface{}{"skillhub"},
		"maclaw_role_name":             " reviewer ",
		"maclaw_role_description":      " checks code ",
		"im_progress_nudge_enabled":    false,
		"qqbot_enabled":                true,
		"qqbot_app_id":                 " qq-app ",
		"qqbot_app_secret":             "qq-secret ",
		"telegram_bot_enabled":         true,
		"telegram_bot_token":           " tg-token ",
		"lansenger_enabled":            true,
		"lansenger_app_id":             " lx-app ",
		"lansenger_app_secret":         "lx-secret ",
		"lansenger_gateway_url":        " https://apigw.example.com ",
		"lansenger_wss_url":            " wss://ws.example.com ",
		"thirdparty_gateway_enabled":   true,
		"thirdparty_gateway_token":     " gateway-token ",
		"thirdparty_gateway_host":      " 0.0.0.0 ",
		"thirdparty_gateway_port":      float64(18888),
		"ui_zoom_factor":               float64(3.5),
		"chat_font_size":               float64(99),
		"env_check_interval":           float64(14),
		"last_env_check_time":          " 2026-06-05T12:00:00Z ",
		"vector_search_enabled":        false,
		"screen_parsing_enabled":       false,
		"asr_enabled":                  false,
		"tts_enabled":                  false,
		"tts_voice_id":                 " zm_yunxi ",
		"maclaw_agent_max_iterations":  float64(12),
		"subagent_concurrency":         float64(99),
		"trial_reflect_enabled":        true,
		"pet_enabled":                  true,
		"pet_skin":                     " mini-claw ",
		"pet_size":                     float64(92),
		"pet_motion_enabled":           true,
		"pet_motion_sound_enabled":     false,
		"pet_motion_sound_preset":      " soft ",
		"pet_text_interaction_enabled": true,
		"pet_voice_input_enabled":      true,
		"pet_voice_readback_enabled":   true,
		"pet_file_drop_enabled":        false,
		"pet_interaction_mode":         " active ",
		"pet_conversation_mode":        " continuous ",
		"pet_readback_mode":            " summary ",
		"pet_auto_retry_on_no_hear":    true,
		"pet_continuous_timeout_sec":   float64(45),
		"pet_quiet_mode":               true,
		"projects": []interface{}{
			map[string]interface{}{"id": "p1", "name": "One", "path": "D:/one", "yolo_mode": false},
			map[string]interface{}{"id": "p2", "name": "Two", "path": "D:/two", "yolo_mode": true},
		},
		"use_windows_terminal":       false,
		"show_ai_trace_entry":        true,
		"show_app_entry":             true,
		"show_coding_tool_entry":     true,
		"show_codex":                 false,
		"screen_dim_timeout_min":     float64(9),
		"remote_heartbeat_sec":       float64(3),
		"agent_response_timeout_sec": float64(300),
		"skill_runner_timeout_sec":   float64(3600),
		"maclaw_llm_timeout_sec":     float64(480),
		"audio_input_device_id":      " mic-1 ",
		"audio_output_device_id":     " speaker-1 ",
		"default_proxy_enabled":      true,
		"default_proxy_protocol":     " socks5 ",
		"default_proxy_host":         " proxy.example.com ",
		"default_proxy_port":         " 1080 ",
		"default_proxy_username":     "user",
		"default_proxy_password":     "pass",
		"default_proxy_bypass":       " localhost;127.0.0.1 ",
		"default_proxy_scope_agent":  true,
		"noise_floor_calibrated":     float64(0.012),
		"speech_level_calibrated":    float64(0.2),
		"llm_prompt_cache": map[string]interface{}{
			"enabled":     true,
			"ttl_seconds": float64(120),
		},
	})
	if err != nil {
		t.Fatalf("PatchConfigFields() error = %v", err)
	}
	if !patched.PauseEnvCheck || !patched.EnvCheckDone || patched.UseWindowsTerminal || !patched.ShowAITraceEntry || !patched.ShowAppEntry || !patched.ShowCodingToolEntry || patched.ShowCodex {
		t.Fatalf("boolean patch fields not applied: %#v", patched)
	}
	if patched.Language != "zh-Hans" || patched.ActiveTool != "codex" || patched.CurrentProject != "p2" || len(patched.Projects) != 2 || len(patched.FavoriteEmployees) != 2 || patched.FavoriteEmployeeNames["ve1"] != "Reviewer" {
		t.Fatalf("navigation/project/favorite fields not applied: %#v", patched)
	}
	if !patched.RemoteEnabled || patched.RemoteHubURL != "https://hub.example.com/" || patched.RemoteHubCenterURL != "https://hubs.example.com" || patched.RemoteEmail != "owner@example.com" || patched.RemoteMobile != "13800138000" || !patched.OnboardingDone || patched.DefaultLaunchMode != "remote" || patched.Codex.CurrentModel != "Original" {
		t.Fatalf("remote/provider fields not applied: %#v", patched)
	}
	if patched.SecurityPolicyMode != "strict" || patched.SandboxMode != "os" || patched.NetworkLevel != "allowlist" || len(patched.NetworkAllowlist) != 1 || patched.YoloModeAllowed || !patched.SmartRouteEnabled || !patched.GossipEnabled || patched.FileOutboundEnabled || patched.ImageOutboundEnabled || len(patched.SkillSourcesAllowed) != 1 {
		t.Fatalf("security fields not applied: %#v", patched)
	}
	if patched.MaclawRoleName != "reviewer" || patched.MaclawRoleDescription != "checks code" {
		t.Fatalf("maclaw role fields not applied: %#v", patched)
	}
	if patched.IMProgressNudgeEnabled == nil || *patched.IMProgressNudgeEnabled || !patched.QQBotEnabled || patched.QQBotAppID != "qq-app" || patched.QQBotAppSecret != "qq-secret " || !patched.TelegramBotEnabled || patched.TelegramBotToken != "tg-token" {
		t.Fatalf("IM bot fields not applied: %#v", patched)
	}
	if !patched.LansengerEnabled || patched.LansengerAppID != "lx-app" || patched.LansengerAppSecret != "lx-secret " || patched.LansengerGatewayURL != "https://apigw.example.com" || patched.LansengerWSSURL != "wss://ws.example.com" {
		t.Fatalf("Lansenger fields not applied: %#v", patched)
	}
	if !patched.ThirdPartyGatewayEnabled || patched.ThirdPartyGatewayToken != "gateway-token" || patched.ThirdPartyGatewayHost != "0.0.0.0" || patched.ThirdPartyGatewayPort != 18888 {
		t.Fatalf("third-party gateway fields not applied: %#v", patched)
	}
	if patched.UIZoomFactor != 2.0 || patched.ChatFontSize != 24 || patched.EnvCheckInterval != 14 || patched.LastEnvCheckTime != "2026-06-05T12:00:00Z" || patched.VectorSearchEnabled || patched.ScreenParsingEnabled == nil || *patched.ScreenParsingEnabled || patched.ASREnabled || patched.TTSEnabled || patched.TTSVoiceID != "zm_yunxi" || patched.MaclawAgentMaxIterations != 30 || patched.SubAgentConcurrency != corelib.MaxSubAgentConcurrency || !patched.TrialReflectEnabled {
		t.Fatalf("ui/vector/agent fields not applied with normalization: %#v", patched)
	}
	if !patched.PetEnabled || patched.PetSkin != "mini-claw" || patched.PetSize != 92 || patched.PetMotionEnabled == nil || !*patched.PetMotionEnabled || patched.PetMotionSound == nil || *patched.PetMotionSound || patched.PetMotionSoundPreset != "soft" || patched.PetTextInteraction == nil || !*patched.PetTextInteraction || !patched.PetVoiceInput || !patched.PetVoiceReadback || patched.PetFileDropEnabled == nil || *patched.PetFileDropEnabled || patched.PetInteractionMode != "active" || patched.PetConversationMode != "continuous" || patched.PetReadbackMode != "summary" || !patched.PetAutoRetryOnNoHear || patched.PetContinuousTimeout != 45 || !patched.PetQuietMode {
		t.Fatalf("pet fields not applied: %#v", patched)
	}
	if patched.ScreenDimTimeoutMin != 9 || patched.RemoteHeartbeatSec != 5 || patched.AgentResponseTimeoutSec != 300 || patched.SkillRunnerTimeoutSec != 3600 || patched.MaclawLLMTimeoutSec != 480 {
		t.Fatalf("numeric patch fields not applied: %#v", patched)
	}
	if patched.AudioInputDeviceID != "mic-1" || patched.AudioOutputDeviceID != "speaker-1" {
		t.Fatalf("audio device fields not trimmed/applied: %#v", patched)
	}
	if !patched.DefaultProxyEnabled || patched.DefaultProxyProtocol != "socks5" || patched.DefaultProxyHost != "proxy.example.com" || patched.DefaultProxyPort != "1080" || !patched.DefaultProxyScopeAgent {
		t.Fatalf("proxy fields not applied: %#v", patched)
	}
	if patched.NoiseFloorCalibrated != 0.012 || patched.SpeechLevelCalibrated != 0.2 {
		t.Fatalf("calibration fields not applied: %#v", patched)
	}
	if !patched.LLMPromptCache.Enabled || patched.LLMPromptCache.TTLSeconds != 120 || patched.LLMPromptCache.MemoryMaxEntries == 0 {
		t.Fatalf("LLM prompt cache field not applied with defaults: %#v", patched.LLMPromptCache)
	}
}

func TestPatchConfigFieldsRejectsLoopbackHubCenterURL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if _, err := app.PatchConfigFields(map[string]interface{}{
		"remote_hubcenter_url": "http://127.0.0.1:65140",
	}); err == nil {
		t.Fatal("PatchConfigFields accepted loopback remote_hubcenter_url")
	}
}

func TestPatchConfigFieldsRejectsUnsupportedFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if _, err := app.PatchConfigFields(map[string]interface{}{"unknown_field": "bad"}); err == nil {
		t.Fatal("PatchConfigFields(unknown_field) error = nil, want unsupported field error")
	}
}

func TestPatchConfigFieldsRemoteHeartbeatUsesSharedNormalization(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	patched, err := app.PatchConfigFields(map[string]interface{}{
		"remote_heartbeat_sec": float64(0),
	})
	if err != nil {
		t.Fatalf("PatchConfigFields() error = %v", err)
	}
	if patched.RemoteHeartbeatSec != corelib.DefaultRemoteHeartbeatSec {
		t.Fatalf("RemoteHeartbeatSec = %d, want %d", patched.RemoteHeartbeatSec, corelib.DefaultRemoteHeartbeatSec)
	}
}

func TestPatchConfigFieldsCoversRemotePanelAtomicWhitelist(t *testing.T) {
	guiDir := testGUIDir(t)
	backendFields := testPatchConfigBackendFields(t, guiDir)
	frontendSource, err := os.ReadFile(filepath.Join(guiDir, "frontend", "src", "components", "remote", "useRemotePanel.ts"))
	if err != nil {
		t.Fatalf("read useRemotePanel.ts: %v", err)
	}
	frontendText := string(frontendSource)
	whitelistStart := strings.Index(frontendText, "const atomicPatchFields")
	if whitelistStart < 0 {
		t.Fatalf("atomicPatchFields source bounds not found")
	}
	whitelistEndOffset := strings.Index(frontendText[whitelistStart:], "]);")
	if whitelistEndOffset < 0 {
		t.Fatalf("atomicPatchFields source bounds not found")
	}
	whitelistEnd := whitelistStart + whitelistEndOffset
	fieldRE := regexp.MustCompile(`'([a-z0-9_]+)'`)
	var missing []string
	for _, match := range fieldRE.FindAllStringSubmatch(frontendText[whitelistStart:whitelistEnd], -1) {
		if !backendFields[match[1]] {
			missing = append(missing, match[1])
		}
	}
	if len(missing) > 0 {
		t.Fatalf("atomicPatchFields missing backend PatchConfigFields cases: %v", missing)
	}
}

func TestPatchConfigFieldsCoversFrontendLiteralCallers(t *testing.T) {
	guiDir := testGUIDir(t)
	backendFields := testPatchConfigBackendFields(t, guiDir)
	srcDir := filepath.Join(guiDir, "frontend", "src")
	missing := map[string][]string{}
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__tests__" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".ts" && filepath.Ext(path) != ".tsx" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, key := range testFrontendPatchConfigLiteralKeys(string(data)) {
			if !backendFields[key] {
				rel, _ := filepath.Rel(guiDir, path)
				missing[key] = append(missing[key], rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk frontend src: %v", err)
	}
	if len(missing) > 0 {
		keys := make([]string, 0, len(missing))
		for key := range missing {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var parts []string
		for _, key := range keys {
			sort.Strings(missing[key])
			parts = append(parts, key+" in "+strings.Join(missing[key], ","))
		}
		t.Fatalf("frontend PatchConfigFields literal callers missing backend cases: %v", parts)
	}
}

func testGUIDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(currentFile)
}

func testPatchConfigBackendFields(t *testing.T, guiDir string) map[string]bool {
	t.Helper()
	appSource, err := os.ReadFile(filepath.Join(guiDir, "app.go"))
	if err != nil {
		t.Fatalf("read app.go: %v", err)
	}
	appText := string(appSource)
	patchStart := strings.Index(appText, "func (a *App) PatchConfigFields")
	patchEnd := strings.Index(appText, "// PatchConfig performs")
	if patchStart < 0 || patchEnd <= patchStart {
		t.Fatalf("PatchConfigFields source bounds not found")
	}
	caseRE := regexp.MustCompile(`case "([a-z0-9_]+)"`)
	backendFields := map[string]bool{}
	for _, match := range caseRE.FindAllStringSubmatch(appText[patchStart:patchEnd], -1) {
		backendFields[match[1]] = true
	}
	return backendFields
}

func testFrontendPatchConfigLiteralKeys(source string) []string {
	var keys []string
	for searchFrom := 0; searchFrom < len(source); {
		idx := strings.Index(source[searchFrom:], "PatchConfigFields")
		if idx < 0 {
			break
		}
		idx += searchFrom
		open := idx + len("PatchConfigFields")
		for open < len(source) && (source[open] == ' ' || source[open] == '\t' || source[open] == '\r' || source[open] == '\n') {
			open++
		}
		if open >= len(source) || source[open] != '(' {
			searchFrom = open
			continue
		}
		pos := open + 1
		for pos < len(source) && (source[pos] == ' ' || source[pos] == '\t' || source[pos] == '\r' || source[pos] == '\n') {
			pos++
		}
		if pos >= len(source) || source[pos] != '{' {
			searchFrom = pos
			continue
		}
		end := testFindMatchingObjectBrace(source, pos)
		if end < 0 {
			searchFrom = pos + 1
			continue
		}
		keys = append(keys, testTopLevelObjectKeys(source[pos:end+1])...)
		searchFrom = end + 1
	}
	return keys
}

func testFindMatchingObjectBrace(source string, start int) int {
	depth := 0
	quote := byte(0)
	escaped := false
	lineComment := false
	blockComment := false
	for i := start; i < len(source); i++ {
		ch := source[i]
		if lineComment {
			if ch == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if ch == '*' && i+1 < len(source) && source[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '/' && i+1 < len(source) && source[i+1] == '/' {
			lineComment = true
			i++
			continue
		}
		if ch == '/' && i+1 < len(source) && source[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func testTopLevelObjectKeys(objectSource string) []string {
	seen := map[string]bool{}
	var keys []string
	depth := 0
	quote := byte(0)
	escaped := false
	lineComment := false
	blockComment := false
	for i := 0; i < len(objectSource); i++ {
		ch := objectSource[i]
		if lineComment {
			if ch == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if ch == '*' && i+1 < len(objectSource) && objectSource[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '/' && i+1 < len(objectSource) && objectSource[i+1] == '/' {
			lineComment = true
			i++
			continue
		}
		if ch == '/' && i+1 < len(objectSource) && objectSource[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			continue
		}
		if depth != 1 || !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_') {
			continue
		}
		start := i
		for i+1 < len(objectSource) {
			next := objectSource[i+1]
			if !((next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || next == '_') {
				break
			}
			i++
		}
		key := objectSource[start : i+1]
		j := i + 1
		for j < len(objectSource) && (objectSource[j] == ' ' || objectSource[j] == '\t' || objectSource[j] == '\r' || objectSource[j] == '\n') {
			j++
		}
		if j < len(objectSource) && objectSource[j] == ':' && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func TestRestartUnconfiguredIMGatewaysReturnsDisconnected(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if got := app.RestartQQBot(); got != gatewayConnectionStatusDisconnected.String() {
		t.Fatalf("RestartQQBot() = %q, want disconnected", got)
	}
	if got := app.RestartTelegram(); got != gatewayConnectionStatusDisconnected.String() {
		t.Fatalf("RestartTelegram() = %q, want disconnected", got)
	}
	if got := app.RestartWeixin(); got != gatewayConnectionStatusDisconnected.String() {
		t.Fatalf("RestartWeixin() = %q, want disconnected", got)
	}
}

func TestLocalVoiceSettersPatchWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.SetASREnabled(false); err != nil {
		t.Fatalf("SetASREnabled(false) error = %v", err)
	}
	if err := app.SetTTSEnabled(false); err != nil {
		t.Fatalf("SetTTSEnabled(false) error = %v", err)
	}
	if err := app.SetTTSVoiceID("zm_yunyang"); err != nil {
		t.Fatalf("SetTTSVoiceID() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.RemoteEmail != "owner@example.com" || !reloaded.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by local voice setters: %#v", reloaded)
	}
	if reloaded.ASREnabled || reloaded.TTSEnabled || reloaded.TTSVoiceID != "zm_yunyang" {
		t.Fatalf("local voice settings not applied: %#v", reloaded)
	}
}

func TestSaveMISDataConfigPatchesWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.SaveMISDataConfig(corelib.MISDataConfig{
		Enabled:  true,
		Endpoint: " http://127.0.0.1:18180/ ",
		Token:    " token ",
		TenantID: " tenant ",
		UserID:   " user ",
		Role:     " DATA_ADMIN ",
	}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.RemoteEmail != "owner@example.com" || !reloaded.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by MIS data save: %#v", reloaded)
	}
	if !reloaded.MISData.Enabled || reloaded.MISData.Endpoint != "http://127.0.0.1:18180" || reloaded.MISData.Token != "token" || reloaded.MISData.TenantID != "tenant" || reloaded.MISData.UserID != "user" || reloaded.MISData.Role != "data_admin" {
		t.Fatalf("MIS data config not normalized/applied: %#v", reloaded.MISData)
	}
}

func TestSmallLocalSettersPatchWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.SetScreenParsingEnabled(false); err != nil {
		t.Fatalf("SetScreenParsingEnabled(false) error = %v", err)
	}
	if err := app.SetEnvCheckInterval(14); err != nil {
		t.Fatalf("SetEnvCheckInterval(14) error = %v", err)
	}
	app.UpdateLastEnvCheckTime()
	if err := app.SetVEAllowedDirectories([]string{`C:\Work`, `c:/work/`, `D:\Data`}); err != nil {
		t.Fatalf("SetVEAllowedDirectories() error = %v", err)
	}
	for name, setLocal := range map[string]func(bool) error{
		"qqbot":       app.SetQQBotLocalMode,
		"telegram":    app.SetTelegramLocalMode,
		"weixin":      app.SetWeixinLocalMode,
		"lansenger":   app.SetLansengerLocalMode,
		"third_party": app.SetThirdPartyGatewayLocalMode,
	} {
		if err := setLocal(true); err != nil {
			t.Fatalf("%s local mode setter error = %v", name, err)
		}
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.RemoteEmail != "owner@example.com" || !reloaded.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by small setters: %#v", reloaded)
	}
	if reloaded.ScreenParsingEnabled == nil || *reloaded.ScreenParsingEnabled {
		t.Fatalf("ScreenParsingEnabled not patched false: %#v", reloaded.ScreenParsingEnabled)
	}
	if reloaded.EnvCheckInterval != 14 || reloaded.LastEnvCheckTime == "" {
		t.Fatalf("env check fields not patched: interval=%d last=%q", reloaded.EnvCheckInterval, reloaded.LastEnvCheckTime)
	}
	if len(reloaded.VEAllowedDirectories) != 2 {
		t.Fatalf("VEAllowedDirectories = %#v, want 2 deduplicated entries", reloaded.VEAllowedDirectories)
	}
	if !reloaded.IsQQBotLocalMode() || !reloaded.IsTelegramLocalMode() || !reloaded.IsWeixinLocalMode() || !reloaded.IsLansengerLocalMode() || !reloaded.IsThirdPartyGatewayLocalMode() {
		t.Fatalf("gateway local mode fields not patched: %#v", reloaded)
	}
}

func TestSetDataDirPatchesWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(filepath.Join(tmpHome, ".maclaw")) })

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	dataDir := filepath.Join(tmpHome, "custom-data")
	if msg := app.SetDataDir(dataDir); msg != "" {
		t.Fatalf("SetDataDir() message = %q, want success", msg)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.DataDir != dataDir {
		t.Fatalf("DataDir = %q, want %q", reloaded.DataDir, dataDir)
	}
	if reloaded.RemoteEmail != "owner@example.com" || !reloaded.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by SetDataDir: %#v", reloaded)
	}
}

func TestSetDataDirRefreshesSkillAndAIModelDirs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(filepath.Join(tmpHome, ".maclaw")) })

	app := &App{testHomeDir: tmpHome}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	dataDir := filepath.Join(tmpHome, "custom-data")
	if msg := app.SetDataDir(dataDir); msg != "" {
		t.Fatalf("SetDataDir() message = %q, want success", msg)
	}

	skillsDir, err := skill.PrimarySkillsDir()
	if err != nil {
		t.Fatalf("PrimarySkillsDir() error = %v", err)
	}
	if want := filepath.Join(dataDir, "data", "skills"); skillsDir != want {
		t.Fatalf("PrimarySkillsDir() = %q, want %q", skillsDir, want)
	}
	if want := filepath.Join(dataDir, "models", embedding.DefaultModelFilename); embedding.DefaultModelPath() != want {
		t.Fatalf("DefaultModelPath() = %q, want %q", embedding.DefaultModelPath(), want)
	}
	if want := filepath.Join(dataDir, "data", "tools"); privateToolsDirForApp(app) != want {
		t.Fatalf("privateToolsDirForApp() = %q, want %q", privateToolsDirForApp(app), want)
	}
}

func TestSetDataDirResetsPathBoundState(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(filepath.Join(tmpHome, ".maclaw")) })

	app := &App{testHomeDir: tmpHome}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	app.aiConversationMemory = agent.NewConversationMemory()
	app.aiConfirmationStore = newAIConfirmationStore(filepath.Join(app.GetDataDir(), "ai_assistant_confirmation.json"))
	app.templateManager = &remote.SessionTemplateManager{}
	app.projectTabSessionPersist = &ProjectTabSessionPersist{}
	model, err := user.NewModel(filepath.Join(app.GetDataDir(), "user_model.json"))
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	app.userModel = model
	app.evidenceCollector = user.NewCollector(model)

	dataDir := filepath.Join(tmpHome, "custom-data")
	if msg := app.SetDataDir(dataDir); msg != "" {
		t.Fatalf("SetDataDir() message = %q, want success", msg)
	}

	if app.aiConversationMemory != nil || app.aiConfirmationStore != nil || app.templateManager != nil || app.projectTabSessionPersist != nil ||
		app.userModel != nil || app.evidenceCollector != nil {
		t.Fatalf("path-bound state was not reset after data dir change")
	}
	if want := filepath.Join(dataDir, "data"); app.GetDataDir() != want {
		t.Fatalf("GetDataDir() = %q, want %q", app.GetDataDir(), want)
	}
}

func TestProjectSearchSwitchPatchesWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	one := filepath.Join(tmpHome, "one")
	two := filepath.Join(tmpHome, "two")
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	cfg.CurrentProject = "p1"
	cfg.Projects = []corelib.ProjectConfig{
		{Id: "p1", Name: "One", Path: one},
		{Id: "p2", Name: "Two", Path: two},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	patched := app.switchCurrentProjectByPath(two)
	if patched == nil {
		t.Fatal("switchCurrentProjectByPath() returned nil")
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if saved.CurrentProject != "p2" {
		t.Fatalf("CurrentProject = %q, want p2", saved.CurrentProject)
	}
	if saved.RemoteEmail != "owner@example.com" || !saved.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by project search switch: %#v", saved)
	}
}

func TestSaveConfigSanitizesSubAgentConcurrency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.SubAgentConcurrency = corelib.MaxSubAgentConcurrency + 1
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.SubAgentConcurrency != corelib.MaxSubAgentConcurrency {
		t.Fatalf("SubAgentConcurrency = %d, want %d", reloaded.SubAgentConcurrency, corelib.MaxSubAgentConcurrency)
	}
}

func TestSaveConfigSanitizesRemovedCodingTools(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.ActiveTool = "cursor"
	cfg.DefaultTool = "gemini"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.ActiveTool != "claude" {
		t.Fatalf("ActiveTool = %q, want claude", reloaded.ActiveTool)
	}
	if reloaded.DefaultTool != "claude" {
		t.Fatalf("DefaultTool = %q, want claude", reloaded.DefaultTool)
	}
}

func TestSanitizeCodingToolSelectionCanonicalizesValidTools(t *testing.T) {
	cfg := corelib.AppConfig{ActiveTool: " CoDeX ", DefaultTool: " KILO "}
	sanitizeCodingToolSelection(&cfg)

	if cfg.ActiveTool != "codex" || cfg.DefaultTool != "kilo" {
		t.Fatalf("tools not canonicalized: active=%q default=%q", cfg.ActiveTool, cfg.DefaultTool)
	}
}

func TestPatchConfigSanitizesRemovedCodingTools(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ActiveTool = "cursor"
		cfg.DefaultTool = "gemini"
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.ActiveTool != "claude" || reloaded.DefaultTool != "claude" {
		t.Fatalf("removed tools not sanitized: active=%q default=%q", reloaded.ActiveTool, reloaded.DefaultTool)
	}
}

func TestSaveConfigRemovesChatFireFromCodingTools(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	chatFire := corelib.ModelConfig{ModelName: "ChatFire", ModelId: "gpt-4o", ModelUrl: "https://api.chatfire.cn/v1"}
	tools := []*corelib.ToolConfig{&cfg.Claude, &cfg.Codex, &cfg.Opencode, &cfg.CodeBuddy, &cfg.IFlow, &cfg.Kilo}
	for _, tool := range tools {
		tool.Models = append(tool.Models, chatFire)
		tool.CurrentModel = "ChatFire"
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	for name, tool := range map[string]corelib.ToolConfig{
		"claude":    reloaded.Claude,
		"codex":     reloaded.Codex,
		"opencode":  reloaded.Opencode,
		"codebuddy": reloaded.CodeBuddy,
		"iflow":     reloaded.IFlow,
		"kilo":      reloaded.Kilo,
	} {
		if strings.EqualFold(tool.CurrentModel, "ChatFire") {
			t.Fatalf("%s current_model still ChatFire", name)
		}
		for _, model := range tool.Models {
			if strings.EqualFold(model.ModelName, "ChatFire") {
				t.Fatalf("%s still contains ChatFire provider", name)
			}
		}
	}
}

func TestSaveConfigSanitizesAgentTimeouts(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.AgentResponseTimeoutSec = 120
	cfg.SkillRunnerTimeoutSec = 20000
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.AgentResponseTimeoutSec != corelib.MinAgentTimeoutSec {
		t.Fatalf("AgentResponseTimeoutSec = %d, want %d", reloaded.AgentResponseTimeoutSec, corelib.MinAgentTimeoutSec)
	}
	if reloaded.SkillRunnerTimeoutSec != corelib.MaxSkillRunnerTimeoutSec {
		t.Fatalf("SkillRunnerTimeoutSec = %d, want %d", reloaded.SkillRunnerTimeoutSec, corelib.MaxSkillRunnerTimeoutSec)
	}
}

func TestPatchConfigSanitizesAgentTimeouts(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.AgentResponseTimeoutSec = 120
		cfg.SkillRunnerTimeoutSec = 20000
		cfg.MaclawLLMTimeoutSec = 1200
		cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{Name: "slow", TimeoutSec: 120}}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.AgentResponseTimeoutSec != corelib.MinAgentTimeoutSec {
		t.Fatalf("AgentResponseTimeoutSec = %d, want %d", reloaded.AgentResponseTimeoutSec, corelib.MinAgentTimeoutSec)
	}
	if reloaded.SkillRunnerTimeoutSec != corelib.MaxSkillRunnerTimeoutSec {
		t.Fatalf("SkillRunnerTimeoutSec = %d, want %d", reloaded.SkillRunnerTimeoutSec, corelib.MaxSkillRunnerTimeoutSec)
	}
	if reloaded.MaclawLLMTimeoutSec != corelib.MaxAgentTimeoutSec {
		t.Fatalf("MaclawLLMTimeoutSec = %d, want %d", reloaded.MaclawLLMTimeoutSec, corelib.MaxAgentTimeoutSec)
	}
	if got := reloaded.MaclawLLMProviders[0].TimeoutSec; got != corelib.MinAgentTimeoutSec {
		t.Fatalf("provider TimeoutSec = %d, want %d", got, corelib.MinAgentTimeoutSec)
	}
}

func TestSetAuthRequestSoundConfigPatchesGroupDiscussionOnly(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteEmail: "owner@example.com"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.SetAuthRequestSoundConfig(" URGENT ", true); err != nil {
		t.Fatalf("SetAuthRequestSoundConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.RemoteEmail != "owner@example.com" {
		t.Fatalf("RemoteEmail = %q, want preserved owner@example.com", reloaded.RemoteEmail)
	}
	if reloaded.GroupDiscussion.AuthRequestSoundPreset != "urgent" {
		t.Fatalf("AuthRequestSoundPreset = %q, want urgent", reloaded.GroupDiscussion.AuthRequestSoundPreset)
	}
	if !reloaded.GroupDiscussion.AuthRequestSoundMuted {
		t.Fatal("AuthRequestSoundMuted = false, want true")
	}

	if err := app.SetAuthRequestSoundConfig("nope", false); err != nil {
		t.Fatalf("SetAuthRequestSoundConfig(invalid) error = %v", err)
	}
	reloaded, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() second reload error = %v", err)
	}
	if reloaded.GroupDiscussion.AuthRequestSoundPreset != "classic" {
		t.Fatalf("invalid preset normalized to %q, want classic", reloaded.GroupDiscussion.AuthRequestSoundPreset)
	}
	if reloaded.GroupDiscussion.AuthRequestSoundMuted {
		t.Fatal("AuthRequestSoundMuted = true, want false")
	}
}

func TestSaveConfigSanitizesPetSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.PetEnabled = true
	cfg.PetSkin = "unknown-skin"
	cfg.PetSize = 999
	cfg.PetInteractionMode = "noisy"
	cfg.PetConversationMode = "free-chat"
	cfg.PetReadbackMode = "everything"
	cfg.PetVoiceReadback = true
	cfg.PetMotionSoundPreset = "laser-horn"
	motionSound := false
	cfg.PetMotionSound = &motionSound
	cfg.PetContinuousTimeout = 1

	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.PetSkin != "clawmate" {
		t.Fatalf("PetSkin = %q, want clawmate", reloaded.PetSkin)
	}
	if reloaded.PetSize != 120 {
		t.Fatalf("PetSize = %d, want 120", reloaded.PetSize)
	}
	if reloaded.PetInteractionMode != "balanced" {
		t.Fatalf("PetInteractionMode = %q, want balanced", reloaded.PetInteractionMode)
	}
	if reloaded.PetConversationMode != "text-first" {
		t.Fatalf("PetConversationMode = %q, want text-first", reloaded.PetConversationMode)
	}
	if reloaded.PetReadbackMode != "summary" {
		t.Fatalf("PetReadbackMode = %q, want summary", reloaded.PetReadbackMode)
	}
	if !reloaded.PetVoiceReadback {
		t.Fatal("PetVoiceReadback = false, want true for summary readback")
	}
	if petMotionSoundEnabled(reloaded) {
		t.Fatal("PetMotionSound should remain disabled")
	}
	if reloaded.PetMotionSoundPreset != "classic" {
		t.Fatalf("PetMotionSoundPreset = %q, want classic", reloaded.PetMotionSoundPreset)
	}
	if reloaded.PetContinuousTimeout != 5 {
		t.Fatalf("PetContinuousTimeout = %d, want 5", reloaded.PetContinuousTimeout)
	}
}

func TestSaveConfigDefaultsPetSizeForClarity(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.PetEnabled = true
	cfg.PetSize = 0

	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	reloaded, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() reload error = %v", err)
	}
	if reloaded.PetSize != defaultPetSize {
		t.Fatalf("PetSize = %d, want %d", reloaded.PetSize, defaultPetSize)
	}
}

func TestFloatingAppearanceChangedIncludesPetRuntimeSettings(t *testing.T) {
	base := corelib.AppConfig{
		ShowAssistantEntry:   true,
		PetEnabled:           true,
		PetSkin:              "clawmate",
		PetSize:              defaultPetSize,
		PetInteractionMode:   "balanced",
		PetContinuousTimeout: 30,
	}

	motionOff := false
	withMotionOff := base
	withMotionOff.PetMotionEnabled = &motionOff
	if !floatingAppearanceChanged(base, withMotionOff) {
		t.Fatal("expected motion toggle to refresh floating window")
	}

	soundOff := false
	withSoundOff := base
	withSoundOff.PetMotionSound = &soundOff
	if floatingAppearanceChanged(base, withSoundOff) {
		t.Fatal("sound toggle should NOT trigger appearance refresh (uses lightweight UpdateSoundConfig)")
	}
	if !floatingSoundChanged(base, withSoundOff) {
		t.Fatal("expected sound toggle to trigger floatingSoundChanged")
	}

	withSoundPreset := base
	withSoundPreset.PetMotionSoundPreset = "chime"
	if floatingAppearanceChanged(base, withSoundPreset) {
		t.Fatal("sound preset change should NOT trigger appearance refresh")
	}
	if !floatingSoundChanged(base, withSoundPreset) {
		t.Fatal("expected sound preset change to trigger floatingSoundChanged")
	}

	withInvalidSoundPreset := base
	withInvalidSoundPreset.PetMotionSoundPreset = "laser-horn"
	if floatingSoundChanged(base, withInvalidSoundPreset) {
		t.Fatal("invalid motion sound preset should normalize to classic without triggering change")
	}

	withQuiet := base
	withQuiet.PetQuietMode = true
	if !floatingAppearanceChanged(base, withQuiet) {
		t.Fatal("expected quiet mode toggle to refresh floating window")
	}

	withMode := base
	withMode.PetInteractionMode = "active"
	if !floatingAppearanceChanged(base, withMode) {
		t.Fatal("expected interaction mode change to refresh floating window")
	}
}

func TestSaveConfigConcurrentWritesValidJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() seed error = %v", err)
	}

	const workers = 12
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			localCfg := cfg
			localCfg.RemoteEmail = "worker-" + string(rune('a'+i)) + "@example.com"
			if err := app.SaveConfig(localCfg); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("SaveConfig() error = %v", err)
		}
	}

	configPath := filepath.Join(tmpHome, ".maclaw", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Read config.json error = %v", err)
	}
	var diskCfg corelib.AppConfig
	if err := json.Unmarshal(data, &diskCfg); err != nil {
		t.Fatalf("Unmarshal config.json error = %v", err)
	}
	if diskCfg.RemoteEmail == "" {
		t.Fatal("expected RemoteEmail to be persisted")
	}

	matches, err := filepath.Glob(configPath + ".tmp*")
	if err != nil {
		t.Fatalf("Glob temp files error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files remain: %v", matches)
	}
}

func TestConfigManagerRemoteEnabledDoesNotChangeDefaultLaunchMode(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.DefaultLaunchMode = "local"
	cfg.RemoteEnabled = false
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	mgr := NewConfigManager(app)
	if _, err := mgr.UpdateConfig("remote", "remote_enabled", "true"); err != nil {
		t.Fatalf("UpdateConfig(remote_enabled) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false, want true")
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
}

func TestConfigManagerUpdatePatchesWithoutStaleOverwrite(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	mgr := NewConfigManager(app)
	if _, err := mgr.UpdateConfig("general", "language", "en"); err != nil {
		t.Fatalf("UpdateConfig(language) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if saved.Language != "en" {
		t.Fatalf("Language = %q, want en", saved.Language)
	}
	if saved.RemoteEmail != "owner@example.com" || !saved.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by ConfigManager update: %#v", saved)
	}
}

func TestConfigManagerExposesAndAppliesSkillRunnerTimeout(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	mgr := NewConfigManager(app)

	schemaJSON, err := mgr.SchemaJSON()
	if err != nil {
		t.Fatalf("SchemaJSON() error = %v", err)
	}
	if !strings.Contains(schemaJSON, "skill_runner_timeout_sec") {
		t.Fatalf("schema does not expose skill_runner_timeout_sec:\n%s", schemaJSON)
	}
	var schema []struct {
		Name string `json:"name"`
		Keys []struct {
			Key         string `json:"key"`
			Description string `json:"description"`
			Default     string `json:"default"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		t.Fatalf("schema JSON did not parse: %v\n%s", err, schemaJSON)
	}
	found := false
	for _, section := range schema {
		for _, key := range section.Keys {
			if key.Key != "skill_runner_timeout_sec" {
				continue
			}
			found = true
			if !strings.Contains(key.Description, "240-14400") || key.Default != "600" {
				t.Fatalf("schema exposes stale skill runner timeout bounds/default: %#v", key)
			}
		}
	}
	if !found {
		t.Fatalf("schema does not expose parsed skill_runner_timeout_sec:\n%s", schemaJSON)
	}
	if _, err := mgr.UpdateConfig("maclaw_llm", "skill_runner_timeout_sec", "20000"); err != nil {
		t.Fatalf("UpdateConfig(skill_runner_timeout_sec) error = %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if saved.SkillRunnerTimeoutSec != corelib.MaxSkillRunnerTimeoutSec {
		t.Fatalf("SkillRunnerTimeoutSec = %d, want %d", saved.SkillRunnerTimeoutSec, corelib.MaxSkillRunnerTimeoutSec)
	}
}

func TestConfigManagerDefaultLaunchModeDoesNotChangeRemoteEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("remote"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(remote) error = %v", err)
	}

	mgr := NewConfigManager(app)
	if _, err := mgr.UpdateConfig("remote", "default_launch_mode", "local"); err != nil {
		t.Fatalf("UpdateConfig(default_launch_mode) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false, want true")
	}
}

func TestSetDefaultLaunchMode(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("remote"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(remote) error = %v", err)
	}

	if err := app.SetDefaultLaunchMode(" local "); err != nil {
		t.Fatalf("SetDefaultLaunchMode(local) error = %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after local error = %v", err)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false after local mode, want true")
	}

	saved.RemoteEnabled = false
	if err := app.SaveConfig(saved); err != nil {
		t.Fatalf("SaveConfig(disable remote) error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("REMOTE"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(remote) error = %v", err)
	}
	saved, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after remote error = %v", err)
	}
	if saved.DefaultLaunchMode != "remote" {
		t.Fatalf("DefaultLaunchMode = %q, want remote", saved.DefaultLaunchMode)
	}
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled = false after remote mode, want true")
	}

	if err := app.SetDefaultLaunchMode("cloud"); err == nil {
		t.Fatal("SetDefaultLaunchMode(invalid) error = nil, want error")
	}
}

func TestSaveConfigPreservesDefaultLaunchModeFromStaleSnapshot(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("local"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(local) error = %v", err)
	}

	stale := cfg
	stale.DefaultLaunchMode = "remote"
	stale.RemoteEmail = "stale@example.com"
	if err := app.SaveConfig(stale); err != nil {
		t.Fatalf("SaveConfig(stale) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after stale save error = %v", err)
	}
	if saved.RemoteEmail != "stale@example.com" {
		t.Fatalf("RemoteEmail = %q, want stale@example.com", saved.RemoteEmail)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
}

func TestSaveConfigPreservesHubManagedSecurityFromStaleSnapshot(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	current := corelib.AppConfig{
		Language:               "zh",
		HubSecurityCentralized: true,
		SecurityPolicyMode:     "strict",
		SandboxMode:            "os",
		NetworkLevel:           "none",
		NetworkAllowlist:       []string{"api.example.com"},
		YoloModeAllowed:        false,
		SmartRouteEnabled:      false,
		GossipEnabled:          false,
		FileOutboundEnabled:    false,
		ImageOutboundEnabled:   false,
		SkillSourcesAllowed:    []string{"skillhub"},
	}
	if err := app.SaveConfig(current); err != nil {
		t.Fatalf("SaveConfig(current) error = %v", err)
	}

	stale := current
	stale.Language = "en"
	stale.HubSecurityCentralized = false
	stale.SecurityPolicyMode = "developer"
	stale.SandboxMode = "none"
	stale.NetworkLevel = "full"
	stale.NetworkAllowlist = []string{"evil.example"}
	stale.YoloModeAllowed = true
	stale.SmartRouteEnabled = true
	stale.GossipEnabled = true
	stale.FileOutboundEnabled = true
	stale.ImageOutboundEnabled = true
	stale.SkillSourcesAllowed = []string{"github"}
	if err := app.SaveConfig(stale); err != nil {
		t.Fatalf("SaveConfig(stale) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.Language != "en" {
		t.Fatalf("Language = %q, want en", saved.Language)
	}
	if !saved.HubSecurityCentralized || saved.SecurityPolicyMode != "strict" || saved.SandboxMode != "os" || saved.NetworkLevel != "none" {
		t.Fatalf("managed security scalar fields overwritten: %#v", saved)
	}
	if len(saved.NetworkAllowlist) != 1 || saved.NetworkAllowlist[0] != "api.example.com" || len(saved.SkillSourcesAllowed) != 1 || saved.SkillSourcesAllowed[0] != "skillhub" {
		t.Fatalf("managed security slices overwritten: allow=%v sources=%v", saved.NetworkAllowlist, saved.SkillSourcesAllowed)
	}
	if saved.YoloModeAllowed || saved.SmartRouteEnabled || saved.GossipEnabled || saved.FileOutboundEnabled || saved.ImageOutboundEnabled {
		t.Fatalf("managed security bool fields overwritten: %#v", saved)
	}
}

func TestPatchConfigPreservesHubManagedSecurityUnlessExplicitBypass(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	current := corelib.AppConfig{HubSecurityCentralized: true, SecurityPolicyMode: "strict", SandboxMode: "os", NetworkLevel: "none", FileOutboundEnabled: false, ImageOutboundEnabled: false}
	if err := app.SaveConfig(current); err != nil {
		t.Fatalf("SaveConfig(current) error = %v", err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.Language = "en"
		cfg.HubSecurityCentralized = false
		cfg.SecurityPolicyMode = "developer"
		cfg.SandboxMode = "none"
		cfg.NetworkLevel = "full"
		cfg.FileOutboundEnabled = true
		cfg.ImageOutboundEnabled = true
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.Language != "en" || !saved.HubSecurityCentralized || saved.SecurityPolicyMode != "strict" || saved.SandboxMode != "os" || saved.NetworkLevel != "none" || saved.FileOutboundEnabled || saved.ImageOutboundEnabled {
		t.Fatalf("PatchConfig did not preserve managed security: %#v", saved)
	}

	if err := app.patchConfig(func(cfg *corelib.AppConfig) {
		cfg.HubSecurityCentralized = false
		cfg.SecurityPolicyMode = "developer"
		cfg.SandboxMode = "none"
		cfg.NetworkLevel = "full"
		cfg.FileOutboundEnabled = true
		cfg.ImageOutboundEnabled = true
	}, true); err != nil {
		t.Fatalf("patchConfig bypass error = %v", err)
	}
	saved, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after bypass error = %v", err)
	}
	if saved.HubSecurityCentralized || saved.SecurityPolicyMode != "developer" || saved.SandboxMode != "none" || saved.NetworkLevel != "full" || !saved.FileOutboundEnabled || !saved.ImageOutboundEnabled {
		t.Fatalf("patchConfig bypass did not update managed security: %#v", saved)
	}
}

func TestSaveConfigClearsStaleHubManagedSecurityWhenHubDisablesCentralizedPolicy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	current := corelib.AppConfig{HubSecurityCentralized: true, SecurityPolicyMode: "strict", SandboxMode: "os", NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com"}, FileOutboundEnabled: false, ImageOutboundEnabled: false}
	if err := app.SaveConfig(current); err != nil {
		t.Fatalf("SaveConfig(current) error = %v", err)
	}
	app.hubSecurityCache.update(json.RawMessage(`{"security_policy":{"centralized_security":false}}`))

	next := current
	next.SecurityPolicyMode = "developer"
	next.SandboxMode = "none"
	next.NetworkLevel = "full"
	next.NetworkAllowlist = nil
	next.FileOutboundEnabled = true
	next.ImageOutboundEnabled = true
	if err := app.SaveConfig(next); err != nil {
		t.Fatalf("SaveConfig(next) error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.HubSecurityCentralized || saved.SecurityPolicyMode != "developer" || saved.SandboxMode != "none" || saved.NetworkLevel != "full" || len(saved.NetworkAllowlist) != 0 || !saved.FileOutboundEnabled || !saved.ImageOutboundEnabled {
		t.Fatalf("stale Hub-managed security was preserved after centralized=false: %#v", saved)
	}
}

func TestPatchConfigClearsStaleHubManagedSecurityWhenHubDisablesCentralizedPolicy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	current := corelib.AppConfig{HubSecurityCentralized: true, SecurityPolicyMode: "strict", SandboxMode: "os", NetworkLevel: "allowlist", NetworkAllowlist: []string{"api.example.com"}, FileOutboundEnabled: false, ImageOutboundEnabled: false}
	if err := app.SaveConfig(current); err != nil {
		t.Fatalf("SaveConfig(current) error = %v", err)
	}
	app.hubSecurityCache.update(json.RawMessage(`{"security_policy":{"centralized_security":false}}`))

	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SecurityPolicyMode = "developer"
		cfg.SandboxMode = "none"
		cfg.NetworkLevel = "full"
		cfg.NetworkAllowlist = nil
		cfg.FileOutboundEnabled = true
		cfg.ImageOutboundEnabled = true
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.HubSecurityCentralized || saved.SecurityPolicyMode != "developer" || saved.SandboxMode != "none" || saved.NetworkLevel != "full" || len(saved.NetworkAllowlist) != 0 || !saved.FileOutboundEnabled || !saved.ImageOutboundEnabled {
		t.Fatalf("stale Hub-managed security was preserved after centralized=false: %#v", saved)
	}
}

func TestCoreConfigManagerDefaultLaunchModeUsesFieldSetter(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.SetDefaultLaunchMode("local"); err != nil {
		t.Fatalf("SetDefaultLaunchMode(local) error = %v", err)
	}

	mgr := coreconfig.NewManager(app)
	if _, err := mgr.UpdateConfig("remote", "default_launch_mode", "remote"); err != nil {
		t.Fatalf("UpdateConfig(default_launch_mode) error = %v", err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after update error = %v", err)
	}
	if saved.DefaultLaunchMode != "remote" {
		t.Fatalf("DefaultLaunchMode = %q, want remote", saved.DefaultLaunchMode)
	}

	if err := mgr.BatchUpdate([]coreconfig.ConfigChange{
		{Section: "remote", Key: "remote_enabled", Value: "true"},
		{Section: "remote", Key: "default_launch_mode", Value: "local"},
	}); err != nil {
		t.Fatalf("BatchUpdate(default_launch_mode) error = %v", err)
	}
	saved, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after batch update error = %v", err)
	}
	if saved.DefaultLaunchMode != "local" {
		t.Fatalf("DefaultLaunchMode = %q, want local", saved.DefaultLaunchMode)
	}
}
