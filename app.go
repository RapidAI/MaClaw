package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"
)

// App struct
type App struct {
	ctx               context.Context
	CurrentLanguage   string
	watcher           *fsnotify.Watcher
	testHomeDir       string // For testing purposes
	downloadCancelers map[string]context.CancelFunc
	downloadMutex     sync.Mutex
	IsInitMode        bool
	installingNode    bool      // Flag to prevent concurrent Node.js installation
	installingGit     bool      // Flag to prevent concurrent Git installation
	nodeInstallDone   chan bool // Channel to signal Node.js installation completion
	installMutex      sync.Mutex
	toolInstallLocks  map[string]bool // Track which tools are currently being installed
	toolLockMutex     sync.Mutex      // Mutex for toolInstallLocks map
	remoteSessions    *RemoteSessionManager
	powerStateMutex   sync.Mutex
	powerStateProcess *exec.Cmd
}

var OnConfigChanged func(AppConfig)
var UpdateTrayMenu func(string)
var UpdateTrayVisibility func(bool)

type ModelConfig struct {
	ModelName       string `json:"model_name"`
	ModelId         string `json:"model_id"`
	ModelUrl        string `json:"model_url"`
	ApiKey          string `json:"api_key"`
	WireApi         string `json:"wire_api"`
	IsCustom        bool   `json:"is_custom"`
	HasSubscription bool   `json:"has_subscription"`
}
type ProjectConfig struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	YoloMode      bool   `json:"yolo_mode"`
	AdminMode     bool   `json:"admin_mode"`
	PythonProject bool   `json:"python_project"` // Whether this is a Python project
	PythonEnv     string `json:"python_env"`     // Selected Python/Anaconda environment
	TeamMode      bool   `json:"team_mode"`      // Claude Code Agent Teams mode
	// Proxy settings (project-specific)
	UseProxy      bool   `json:"use_proxy"`
	ProxyHost     string `json:"proxy_host"`
	ProxyPort     string `json:"proxy_port"`
	ProxyUsername string `json:"proxy_username"`
	ProxyPassword string `json:"proxy_password"`
}
type PythonEnvironment struct {
	Name string `json:"name"` // Environment name (e.g.", "base", "myenv")
	Path string `json:"path"` // Full path to the environment
	Type string `json:"type"` // "conda", "venv", or "system"
}
type ToolConfig struct {
	CurrentModel string        `json:"current_model"`
	Models       []ModelConfig `json:"models"`
}
type CodeBuddyModel struct {
	Id               string `json:"id"`
	Name             string `json:"name"`
	Vendor           string `json:"vendor"`
	ApiKey           string `json:"apiKey"`
	MaxInputTokens   int    `json:"maxInputTokens"`
	MaxOutputTokens  int    `json:"maxOutputTokens"`
	Url              string `json:"url"`
	SupportsToolCall bool   `json:"supportsToolCall"`
	SupportsImages   bool   `json:"supportsImages"`
}
type CodeBuddyFileConfig struct {
	Models          []CodeBuddyModel `json:"Models"`
	AvailableModels []string         `json:"availableModels"`
}
type AppConfig struct {
	Claude               ToolConfig      `json:"claude"`
	Gemini               ToolConfig      `json:"gemini"`
	Codex                ToolConfig      `json:"codex"`
	Opencode             ToolConfig      `json:"opencode"`
	CodeBuddy            ToolConfig      `json:"codebuddy"`
	Qoder                ToolConfig      `json:"qoder"`
	IFlow                ToolConfig      `json:"iflow"`
	Kilo                 ToolConfig      `json:"kilo"`
	Kode                 ToolConfig      `json:"kode"`
	Cursor               ToolConfig      `json:"cursor"`
	Projects             []ProjectConfig `json:"projects"`
	CurrentProject       string          `json:"current_project"` // ID of the current project
	ActiveTool           string          `json:"active_tool"`     // "claude", "gemini", or "codex"
	HideStartupPopup     bool            `json:"hide_startup_popup"`
	ShowGemini           bool            `json:"show_gemini"`
	ShowCodex            bool            `json:"show_codex"`
	ShowOpenCode         bool            `json:"show_opencode"`
	ShowCodeBuddy        bool            `json:"show_codebuddy"`
	ShowQoder            bool            `json:"show_qoder"`
	ShowIFlow            bool            `json:"show_iflow"`
	ShowKilo             bool            `json:"show_kilo"`
	ShowKode             bool            `json:"show_kode"`
	ShowCursor           bool            `json:"show_cursor"`
	Language             string          `json:"language"`
	PowerOptimization    bool            `json:"power_optimization"`
	CheckUpdateOnStartup bool            `json:"check_update_on_startup"`
	// Environment check settings
	PauseEnvCheck    bool   `json:"pause_env_check"`     // Whether to skip environment checks
	EnvCheckDone     bool   `json:"env_check_done"`      // Whether the first environment check has been completed
	EnvCheckInterval int    `json:"env_check_interval"`  // Days between environment check reminders (2-30)
	LastEnvCheckTime string `json:"last_env_check_time"` // Last environment check time (RFC3339 format)
	// Proxy settings (global default)
	DefaultProxyHost     string `json:"default_proxy_host"`
	DefaultProxyPort     string `json:"default_proxy_port"`
	DefaultProxyUsername string `json:"default_proxy_username"`
	DefaultProxyPassword string `json:"default_proxy_password"`
	// Terminal settings (Windows only)
	UseWindowsTerminal bool   `json:"use_windows_terminal"` // Use Windows Terminal instead of cmd.exe
	RemoteEnabled      bool   `json:"remote_enabled"`
	RemoteHubURL       string `json:"remote_hub_url"`
	RemoteHubCenterURL string `json:"remote_hubcenter_url"`
	RemoteEmail        string `json:"remote_email"`
	RemoteSN           string `json:"remote_sn"`
	RemoteUserID       string `json:"remote_user_id"`
	RemoteMachineID    string `json:"remote_machine_id"`
	RemoteMachineToken string `json:"remote_machine_token"`
	RemoteHeartbeatSec int    `json:"remote_heartbeat_sec"`
	RemoteClientID     string `json:"remote_client_id"`
	DefaultLaunchMode  string `json:"default_launch_mode"` // "local" or "remote", default launch mode for tool area
}
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`      // "address" or "zip"
	Value       string `json:"value"`     // Address string or zip filename
	Installed   bool   `json:"installed"` // Whether this skill is already installed
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		downloadCancelers: make(map[string]context.CancelFunc),
		nodeInstallDone:   make(chan bool, 1), // Buffered channel to signal Node.js installation completion
		toolInstallLocks:  make(map[string]bool),
	}
}

// tryLockTool attempts to acquire a lock for installing a specific tool
// Returns true if lock acquired, false if tool is already being installed
func (a *App) tryLockTool(toolName string) bool {
	a.toolLockMutex.Lock()
	defer a.toolLockMutex.Unlock()

	if a.toolInstallLocks[toolName] {
		return false // Already being installed
	}
	a.toolInstallLocks[toolName] = true
	return true
}

// unlockTool releases the lock for a specific tool
func (a *App) unlockTool(toolName string) {
	a.toolLockMutex.Lock()
	defer a.toolLockMutex.Unlock()
	delete(a.toolInstallLocks, toolName)
}

// isToolLocked checks if a tool is currently being installed
func (a *App) isToolLocked(toolName string) bool {
	a.toolLockMutex.Lock()
	defer a.toolLockMutex.Unlock()
	return a.toolInstallLocks[toolName]
}

// IsToolBeingInstalled checks if a tool is currently being installed (exported for frontend)
func (a *App) IsToolBeingInstalled(toolName string) bool {
	return a.isToolLocked(toolName)
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Platform specific initialization
	a.platformStartup()
	a.startConfigWatcher()
	// Initialize CodeBuddy config in project directory
	if config, err := a.LoadConfig(); err == nil {
		// a.syncToCodeBuddySettings(config, ")
		if config.Language != "" {
			a.SetLanguage(config.Language)
		}
		if config.RemoteMachineID != "" && config.RemoteMachineToken != "" && config.RemoteHubURL != "" {
			a.remoteSessions = NewRemoteSessionManager(a)
			hubClient := NewRemoteHubClient(a, a.remoteSessions)
			a.remoteSessions.SetHubClient(hubClient)
			_ = hubClient.Connect()
		} else if config.RemoteEmail != "" && config.RemoteHubURL != "" {
			// Auto-register on startup: saved email + hub but no machine credentials yet
			go a.autoRegisterOnStartup(config)
		}
		a.refreshPowerOptimizationStateFromConfig(config)
		return
	}
	a.setPowerOptimizationEnabled(false)
}

// domReady is called after the frontend Dom has been loaded
func (a *App) domReady(ctx context.Context) {
	// Trigger environment check on startup
	// IsInitMode and PauseEnvCheck logic is handled inside CheckEnvironment
	a.CheckEnvironment(false)
}

func (a *App) shutdown(ctx context.Context) {
	a.platformShutdown()
}

func (a *App) refreshPowerOptimizationStateFromConfig(config AppConfig) {
	a.setPowerOptimizationEnabled(config.PowerOptimization && a.hasActiveRemoteTasks())
}

func (a *App) refreshPowerOptimizationState() {
	config, err := a.LoadConfig()
	if err != nil {
		a.setPowerOptimizationEnabled(false)
		return
	}
	a.refreshPowerOptimizationStateFromConfig(config)
}

func (a *App) hasActiveRemoteTasks() bool {
	if a.remoteSessions == nil {
		return false
	}
	return a.remoteSessions.HasActiveSessions()
}

func (a *App) resolveProjectProxyURL(config AppConfig, projectDir string) string {
	var proxyHost, proxyPort, proxyUsername, proxyPassword string

	var targetProj *ProjectConfig
	for i := range config.Projects {
		if config.Projects[i].Path == projectDir {
			targetProj = &config.Projects[i]
			break
		}
	}
	if targetProj == nil {
		for i := range config.Projects {
			if config.Projects[i].Id == config.CurrentProject {
				targetProj = &config.Projects[i]
				break
			}
		}
	}

	if targetProj != nil {
		proxyHost = targetProj.ProxyHost
		proxyPort = targetProj.ProxyPort
		proxyUsername = targetProj.ProxyUsername
		proxyPassword = targetProj.ProxyPassword
	}

	if proxyHost == "" {
		proxyHost = config.DefaultProxyHost
		proxyPort = config.DefaultProxyPort
		proxyUsername = config.DefaultProxyUsername
		proxyPassword = config.DefaultProxyPassword
	}

	if proxyHost == "" || proxyPort == "" {
		return ""
	}

	if proxyUsername != "" && proxyPassword != "" {
		return fmt.Sprintf("http://%s:%s@%s:%s", proxyUsername, proxyPassword, proxyHost, proxyPort)
	}
	return fmt.Sprintf("http://%s:%s", proxyHost, proxyPort)
}

func (a *App) buildClaudeLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected claude model is nil")
	}

	env := map[string]string{}
	env["CLAUDE_CODE_USE_COLORS"] = "true"
	env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = "64000"
	env["MAX_THINKING_TOKENS"] = "10000"

	if strings.ToLower(selectedModel.ModelName) != "original" {
		if selectedModel.ApiKey != "" {
			env["ANTHROPIC_AUTH_TOKEN"] = selectedModel.ApiKey
		}
		if selectedModel.ModelUrl != "" {
			env["ANTHROPIC_BASE_URL"] = selectedModel.ModelUrl
		}
		if selectedModel.ModelId != "" {
			env["ANTHROPIC_MODEL"] = selectedModel.ModelId
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		}
	}

	switch strings.ToLower(selectedModel.ModelName) {
	case "鐧惧害鍗冨竼", "百度千帆", "qianfan":
		modelID := selectedModel.ModelId
		if modelID == "" {
			modelID = "qianfan-code-latest"
		}
		env["ANTHROPIC_AUTH_TOKEN"] = selectedModel.ApiKey
		env["ANTHROPIC_BASE_URL"] = "https://qianfan.baidubce.com/anthropic/coding"
		env["ANTHROPIC_MODEL"] = modelID
		env["ANTHROPIC_SMALL_FAST_MODEL"] = modelID
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
		env["API_TIMEOUT_MS"] = "600000"
	}

	for _, proj := range config.Projects {
		if proj.Path == projectDir || proj.Id == config.CurrentProject {
			if proj.TeamMode {
				env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] = "1"
			}
			break
		}
	}

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	a.clearClaudeConfig()
	return env, nil
}

func (a *App) buildClaudeLaunchSpec(
	config AppConfig,
	yoloMode bool,
	adminMode bool,
	pythonEnv string,
	projectDir string,
	useProxy bool,
) (LaunchSpec, error) {
	return a.buildRemoteLaunchSpec("claude", config, yoloMode, adminMode, pythonEnv, projectDir, useProxy)
}

func (a *App) buildCodexLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected codex model is nil")
	}

	env := map[string]string{}
	if selectedModel.ApiKey != "" {
		env["OPENAI_API_KEY"] = selectedModel.ApiKey
	}
	if selectedModel.ModelUrl != "" {
		env["OPENAI_BASE_URL"] = selectedModel.ModelUrl
	}
	if selectedModel.ModelId != "" {
		env["OPENAI_MODEL"] = selectedModel.ModelId
	}
	wireAPI := strings.TrimSpace(selectedModel.WireApi)
	if wireAPI == "" {
		wireAPI = "responses"
	}
	env["WIRE_API"] = wireAPI

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	a.clearCodexConfig()
	return env, nil
}

func (a *App) buildOpencodeLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected opencode model is nil")
	}

	env := map[string]string{}
	if selectedModel.ApiKey != "" {
		env["OPENCODE_API_KEY"] = selectedModel.ApiKey
	}
	if selectedModel.ModelUrl != "" {
		env["OPENCODE_BASE_URL"] = selectedModel.ModelUrl
	}
	if selectedModel.ModelId != "" {
		env["OPENCODE_MODEL"] = selectedModel.ModelId
	}

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	return env, nil
}

func (a *App) buildIFlowLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected iflow model is nil")
	}

	env := map[string]string{}
	if selectedModel.ApiKey != "" {
		env["OPENAI_API_KEY"] = selectedModel.ApiKey
		env["IFLOW_API_KEY"] = selectedModel.ApiKey
	}
	if selectedModel.ModelUrl != "" {
		env["OPENAI_BASE_URL"] = selectedModel.ModelUrl
		env["IFLOW_BASE_URL"] = selectedModel.ModelUrl
	}
	if selectedModel.ModelId != "" {
		env["IFLOW_MODEL"] = selectedModel.ModelId
	}

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	return env, nil
}

func (a *App) buildKiloLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected kilo model is nil")
	}

	env := map[string]string{}
	if selectedModel.ApiKey != "" {
		env["OPENAI_API_KEY"] = selectedModel.ApiKey
		env["KILO_API_KEY"] = selectedModel.ApiKey
	}
	if selectedModel.ModelUrl != "" {
		env["OPENAI_BASE_URL"] = selectedModel.ModelUrl
		env["KILO_BASE_URL"] = selectedModel.ModelUrl
	}
	if selectedModel.ModelId != "" {
		env["KILO_MODEL"] = selectedModel.ModelId
	}

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	return env, nil
}

func (a *App) buildKodeLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected kode model is nil")
	}

	env := map[string]string{}
	if selectedModel.ApiKey != "" {
		env["OPENAI_API_KEY"] = selectedModel.ApiKey
	}
	if selectedModel.ModelUrl != "" {
		env["OPENAI_BASE_URL"] = selectedModel.ModelUrl
	}

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	return env, nil
}

func (a *App) buildGeminiLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected gemini model is nil")
	}

	env := map[string]string{}
	if selectedModel.ApiKey != "" {
		env["GEMINI_API_KEY"] = selectedModel.ApiKey
		env["GOOGLE_API_KEY"] = selectedModel.ApiKey
	}
	if selectedModel.ModelUrl != "" {
		env["GOOGLE_GEMINI_BASE_URL"] = selectedModel.ModelUrl
	}
	if selectedModel.ModelId != "" {
		env["GEMINI_MODEL"] = selectedModel.ModelId
	}

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	return env, nil
}

func (a *App) buildCursorLaunchEnv(
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	if selectedModel == nil {
		return nil, fmt.Errorf("selected cursor model is nil")
	}

	env := map[string]string{}
	if selectedModel.ApiKey != "" {
		env["CURSOR_API_KEY"] = selectedModel.ApiKey
	}
	if selectedModel.ModelUrl != "" {
		env["CURSOR_BASE_URL"] = selectedModel.ModelUrl
	}

	if useProxy {
		proxyURL := a.resolveProjectProxyURL(config, projectDir)
		if proxyURL != "" {
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
		}
	}

	return env, nil
}

func (a *App) buildRemoteLaunchEnvForTool(
	toolName string,
	config AppConfig,
	selectedModel *ModelConfig,
	projectDir string,
	useProxy bool,
) (map[string]string, error) {
	switch normalizeRemoteToolName(toolName) {
	case "claude":
		return a.buildClaudeLaunchEnv(config, selectedModel, projectDir, useProxy)
	case "codex":
		return a.buildCodexLaunchEnv(config, selectedModel, projectDir, useProxy)
	case "opencode":
		return a.buildOpencodeLaunchEnv(config, selectedModel, projectDir, useProxy)
	case "iflow":
		return a.buildIFlowLaunchEnv(config, selectedModel, projectDir, useProxy)
	case "kilo":
		return a.buildKiloLaunchEnv(config, selectedModel, projectDir, useProxy)
	case "kode":
		return a.buildKodeLaunchEnv(config, selectedModel, projectDir, useProxy)
	case "gemini":
		return a.buildGeminiLaunchEnv(config, selectedModel, projectDir, useProxy)
	case "cursor":
		return a.buildCursorLaunchEnv(config, selectedModel, projectDir, useProxy)
	default:
		return nil, fmt.Errorf("remote launch is not supported for tool: %s", toolName)
	}
}

func (a *App) buildRemoteLaunchSpec(
	toolName string,
	config AppConfig,
	yoloMode bool,
	adminMode bool,
	pythonEnv string,
	projectDir string,
	useProxy bool,
) (LaunchSpec, error) {
	tool := normalizeRemoteToolName(toolName)
	meta, err := getRemoteToolMetadata(tool)
	if err != nil {
		return LaunchSpec{}, err
	}
	toolCfg := meta.ConfigSelector(config)
	var selectedModel *ModelConfig
	for _, m := range toolCfg.Models {
		if m.ModelName == toolCfg.CurrentModel {
			model := m
			selectedModel = &model
			break
		}
	}
	if selectedModel == nil || toolCfg.CurrentModel == "" {
		return LaunchSpec{}, fmt.Errorf("no %s provider selected", tool)
	}

	if projectDir == "" {
		projectDir = a.GetCurrentProjectPath()
	}
	projectDir = filepath.Clean(projectDir)

	env, err := a.buildRemoteLaunchEnvForTool(tool, config, selectedModel, projectDir, useProxy)
	if err != nil {
		return LaunchSpec{}, err
	}

	title := filepath.Base(projectDir)
	if title == "" || title == "." || title == string(filepath.Separator) {
		title = meta.DefaultTitle
	}

	teamMode := false
	if tool == "claude" {
		for _, proj := range config.Projects {
			if proj.Path == projectDir || proj.Id == config.CurrentProject {
				teamMode = proj.TeamMode
				break
			}
		}
	}

	return LaunchSpec{
		Tool:         tool,
		ProjectPath:  projectDir,
		ModelName:    selectedModel.ModelName,
		ModelID:      selectedModel.ModelId,
		BinaryName:   meta.BinaryName,
		Title:        title,
		LaunchSource: RemoteLaunchSourceDesktop,
		YoloMode:     yoloMode,
		AdminMode:    adminMode,
		PythonEnv:    pythonEnv,
		UseProxy:     useProxy,
		TeamMode:     teamMode,
		Env:          env,
	}, nil
}

func (a *App) startConfigWatcher() {
	var err error
	a.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		a.log("Failed to create file watcher: " + err.Error())
		return
	}
	go func() {
		for {
			select {
			case event, ok := <-a.watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					a.log(a.tr("Config file modified: ") + event.Name)
					// Reload config and emit event
					// We use a debounce-like approach or just reload.
					// Since Wails events are async, it should be fine.
					// However, writing the config (SaveConfig) also triggers a write event.
					// We should probably check if the change was internal or external,
					// but that's hard. For now, simply reloading might be okay,
					// but it could cause a loop if we are not careful.
					// Actually, if we just emit 'config-updated', the frontend updates.
					// But if the frontend updates, it might save...
					// Let's assume for now this is for external edits.
					config, err := a.LoadConfig()
					if err == nil {
						a.refreshPowerOptimizationStateFromConfig(config)
						a.emitEvent("config-updated", config)
					}
				}
			case err, ok := <-a.watcher.Errors:
				if !ok {
					return
				}
				a.log("Watcher error: " + err.Error())
			}
		}
	}()
	configPath, err := a.getConfigPath()
	if err == nil {
		if err := a.watcher.Add(configPath); err != nil {
			a.log("Failed to watch config file: " + err.Error())
		} else {
			a.log("Watching config file: " + configPath)
		}
	}
}
func (a *App) SetLanguage(lang string) {
	a.CurrentLanguage = lang
	if UpdateTrayMenu != nil {
		UpdateTrayMenu(lang)
	}
}

// Greet returns a greeting for the given name
func (a *App) ResizeWindow(width, height int) {
	runtime.WindowSetSize(a.ctx, width, height)
	runtime.WindowCenter(a.ctx)
}
func (a *App) WindowHide() {
	runtime.WindowHide(a.ctx)
	if UpdateTrayVisibility != nil {
		UpdateTrayVisibility(false)
	}
}
func (a *App) SelectProjectDir() string {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Project Directory",
	})
	if err != nil {
		return ""
	}
	return selection
}
func (a *App) GetUserHomeDir() string {
	if a.testHomeDir != "" {
		return a.testHomeDir
	}
	home, _ := os.UserHomeDir()
	return home
}
func (a *App) GetLocalCacheDir() string {
	home := a.GetUserHomeDir()
	// Use shorter path to avoid Windows 260 character path limit
	// npm's _cacache directory structure can create very long paths
	return filepath.Join(home, ".cc", "cache")
}
func (a *App) GetCurrentProjectPath() string {
	config, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	for _, p := range config.Projects {
		if p.Id == config.CurrentProject {
			return p.Path
		}
	}
	if len(config.Projects) > 0 {
		return config.Projects[0].Path
	}
	home, _ := os.UserHomeDir()
	return home // Fallback
}
func (a *App) getClaudeConfigPaths(projectDir string, instanceID string) (string, string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "claude", instanceID)
		settings := filepath.Join(dir, "settings.json")
		legacy := filepath.Join(dir, "claude.json")
		return dir, settings, legacy
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude")
	settings := filepath.Join(dir, "settings.json")
	legacy := filepath.Join(home, ".claude.json")
	return dir, settings, legacy
}
func (a *App) getGeminiConfigPaths(projectDir string, instanceID string) (string, string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "gemini", instanceID)
		config := filepath.Join(dir, "settings.json")
		legacy := filepath.Join(dir, "geminirc")
		return dir, config, legacy
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".gemini")
	config := filepath.Join(dir, "settings.json")
	legacy := filepath.Join(home, ".geminirc")
	return dir, config, legacy
}
func (a *App) getCodexConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "codex", instanceID)
		auth := filepath.Join(dir, "auth.json")
		return dir, auth
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".codex")
	auth := filepath.Join(dir, "auth.json")
	return dir, auth
}
func (a *App) getOpencodeConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "opencode", instanceID)
		config := filepath.Join(dir, "opencode.json")
		return dir, config
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "opencode")
	config := filepath.Join(dir, "opencode.json")
	return dir, config
}
func (a *App) getIFlowConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "iflow", instanceID)
		config := filepath.Join(dir, "settings.json")
		return dir, config
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".iflow")
	config := filepath.Join(dir, "settings.json")
	return dir, config
}
func (a *App) clearClaudeConfig() {
	// Clear both project-specific and global configs
	home, _ := os.UserHomeDir()
	dir, _, legacy := a.getClaudeConfigPaths("", "")
	os.RemoveAll(dir)
	os.Remove(legacy)
	os.Remove(filepath.Join(home, ".claude.json.backup"))
	a.log("Cleared Claude configuration files")
}
func (a *App) clearGeminiConfig() {
	dir, _, legacy := a.getGeminiConfigPaths("", "")
	os.RemoveAll(dir)
	os.Remove(legacy)
	a.log("Cleared Gemini configuration files")
}
func (a *App) clearCodexConfig() {
	dir, _ := a.getCodexConfigPaths("", "")
	os.RemoveAll(dir)
	a.log("Cleared Codex configuration directory")
}
func (a *App) clearOpencodeConfig() {
	dir, _ := a.getOpencodeConfigPaths("", "")
	os.RemoveAll(dir)
	a.log("Cleared Opencode configuration directory")
}
func (a *App) clearIFlowConfig() {
	dir, _ := a.getIFlowConfigPaths("", "")
	os.RemoveAll(dir)
	a.log("Cleared iFlow configuration directory")
}
func (a *App) getKiloConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "kilocode", "cli", instanceID)
		config := filepath.Join(dir, "config.json")
		return dir, config
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".kilocode", "cli")
	config := filepath.Join(dir, "config.json")
	return dir, config
}
func (a *App) clearKiloConfig() {
	_, configPath := a.getKiloConfigPaths("", "")
	os.Remove(configPath)
	a.log("Cleared Kilo Code configuration file")
}
func (a *App) getKodeConfigPaths(projectDir string, instanceID string) (string, string) {
	// Use project-specific config directory with instance ID to avoid cross-contamination
	if projectDir != "" && instanceID != "" {
		dir := filepath.Join(projectDir, ".aicoder", "kode", "cli", instanceID)
		config := filepath.Join(dir, "config.json")
		return dir, config
	}
	// Fallback to home directory (for backward compatibility)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".kode", "cli")
	config := filepath.Join(dir, "config.json")
	return dir, config
}
func (a *App) clearKodeConfig() {
	_, configPath := a.getKodeConfigPaths("", "")
	os.Remove(configPath)
	a.log("Cleared Kode CLI configuration file")
}
func (a *App) clearEnvVars() {
	vars := []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "CLAUDE_CODE_MAX_OUTPUT_TOKENS",
		"MAX_THINKING_TOKENS", "API_TIMEOUT_MS",
		"OPENAI_API_KEY", "OPENAI_BASE_URL", "WIRE_API",
		"GEMINI_API_KEY", "GOOGLE_GEMINI_BASE_URL",
		"OPENCODE_API_KEY", "OPENCODE_BASE_URL",
		"CODEBUDDY_API_KEY", "CODEBUDDY_BASE_URL", "CODEBUDDY_CODE_MAX_OUTPUT_TOKENS",
		"QODER_PERSONAL_ACCESS_TOKEN", "QODER_BASE_URL",
		"IFLOW_API_KEY", "IFLOW_BASE_URL",
		"KILO_API_KEY", "KILO_BASE_URL", "KILO_MODEL",
	}
	for _, v := range vars {
		os.Unsetenv(v)
	}
}
func (a *App) syncToClaudeSettings(config AppConfig, projectDir string, instanceID string) error {
	var selectedModel *ModelConfig
	for _, m := range config.Claude.Models {
		if m.ModelName == config.Claude.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected model not found")
	}
	dir, settingsPath, legacyPath := a.getClaudeConfigPaths(projectDir, instanceID)
	if strings.ToLower(selectedModel.ModelName) == "original" {
		a.clearClaudeConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	settings := make(map[string]interface{})
	env := make(map[string]string)
	// Exclusively use AUTH_TOKEN for custom providers
	env["ANTHROPIC_AUTH_TOKEN"] = selectedModel.ApiKey
	env["CLAUDE_CODE_USE_COLORS"] = "true"
	env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = "64000"
	env["MAX_THINKING_TOKENS"] = "31999"
	switch strings.ToLower(selectedModel.ModelName) {
	case "kimi":
		env["ANTHROPIC_BASE_URL"] = "https://api.kimi.com/coding"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
	case "glm", "glm-4.7":
		env["ANTHROPIC_BASE_URL"] = "https://open.bigmodel.cn/api/anthropic"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
		settings["permissions"] = map[string]string{"defaultMode": "dontAsk"}
	case "doubao":
		env["ANTHROPIC_BASE_URL"] = "https://ark.cn-beijing.volces.com/api/coding"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
	case "讯飞星辰", "xfyun":
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "astron-code-latest"
		}
		env["ANTHROPIC_BASE_URL"] = "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case "minimax":
		env["ANTHROPIC_BASE_URL"] = "https://api.minimaxi.com/anthropic"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
		env["ANTHROPIC_SMALL_FAST_MODEL"] = selectedModel.ModelId
		env["API_TIMEOUT_MS"] = "3000000"
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
	case "deepseek":
		env["ANTHROPIC_BASE_URL"] = "https://api.deepseek.com/anthropic"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "deepseek-chat"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case "gaccode":
		env["ANTHROPIC_BASE_URL"] = "https://gaccode.com/claudecode"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "sonnet"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case "腾讯云", "tencent", "tencentcloud":
		env["ANTHROPIC_BASE_URL"] = "https://api.lkeap.cloud.tencent.com/coding/anthropic"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "glm-5"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case "阿里云", "aliyun":
		env["ANTHROPIC_BASE_URL"] = "https://coding.dashscope.aliyuncs.com/apps/anthropic"
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "glm-5"
		}
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
	case "百度千帆", "qianfan":
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "qianfan-code-latest"
		}
		env["ANTHROPIC_BASE_URL"] = "https://qianfan.baidubce.com/anthropic/coding"
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelId
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelId
		env["ANTHROPIC_MODEL"] = modelId
		env["ANTHROPIC_SMALL_FAST_MODEL"] = modelId
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
		env["API_TIMEOUT_MS"] = "600000"
		settings["permissions"] = map[string][]string{
			"allow": {},
			"deny":  {},
		}
	default:
		env["ANTHROPIC_BASE_URL"] = selectedModel.ModelUrl
		env["ANTHROPIC_MODEL"] = selectedModel.ModelId
	}
	settings["env"] = env
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	// Check if settings file needs update
	if existingData, err := os.ReadFile(settingsPath); err == nil {
		if bytes.Equal(existingData, data) {
			// Settings file is already up to date, skip main settings write
			// But still need to update .claude.json for API key responses
			goto updateLegacyJson
		}
	}
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return err
	}
	// 2. Sync to ~/.claude.json for customApiKeyResponses
updateLegacyJson:
	var claudeJson map[string]interface{}
	if jsonData, err := os.ReadFile(legacyPath); err == nil {
		json.Unmarshal(jsonData, &claudeJson)
	}
	if claudeJson == nil {
		claudeJson = make(map[string]interface{})
	}
	claudeJson["customApiKeyResponses"] = map[string]interface{}{
		"approved": []string{selectedModel.ApiKey},
		"rejected": []string{},
	}
	claudeJson["hasCompletedOnboarding"] = true
	data2, err := json.MarshalIndent(claudeJson, "", "  ")
	if err != nil {
		return err
	}
	// Check if legacy file needs update
	if existingData, err := os.ReadFile(legacyPath); err == nil {
		if bytes.Equal(existingData, data2) {
			return nil
		}
	}
	return os.WriteFile(legacyPath, data2, 0644)
}
func (a *App) syncToCodexSettings(config AppConfig, projectDir string, instanceID string) error {
	var selectedModel *ModelConfig
	for _, m := range config.Codex.Models {
		if m.ModelName == config.Codex.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected codex model not found")
	}
	dir, authPath := a.getCodexConfigPaths(projectDir, instanceID)
	if strings.ToLower(selectedModel.ModelName) == "original" {
		a.clearCodexConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Create auth.json
	authData := map[string]string{
		"OPENAI_API_KEY": selectedModel.ApiKey,
	}
	authJson, err := json.MarshalIndent(authData, "", "  ")
	if err != nil {
		return err
	}
	// Check if auth.json needs update
	if existingData, err := os.ReadFile(authPath); err == nil {
		if bytes.Equal(existingData, authJson) {
			// Auth file is already up to date, skip writing
			goto writeConfigToml
		}
	}
	if err := os.WriteFile(authPath, authJson, 0644); err != nil {
		return err
	}
	// Create config.toml
writeConfigToml:
	configPath := filepath.Join(dir, "config.toml")
	baseUrl := selectedModel.ModelUrl
	var configToml string
	if strings.ToLower(selectedModel.ModelName) == "aigocode" {
		if baseUrl == "" {
			baseUrl = "https://api.aigocode.com/openai"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "gpt-5.2-codex"
		}
		configToml = fmt.Sprintf(`model_provider = "aigocode"
model = "%s"
model_reasoning_effort = "high"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.aigocode]
name = "aigocode"
base_url = "%s"
wire_api = "responses"
requires_openai_auth = true
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "deepseek" {
		if baseUrl == "" {
			baseUrl = "https://api.deepseek.com/v1"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "deepseek-chat"
		}
		configToml = fmt.Sprintf(`model_provider = "deepseek"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.deepseek]
name = "deepseek"
base_url = "%s"
wire_api = "responses"
request_max_retries = 4
stream_max_retries = 8
stream_idle_timeout_ms = 120000
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "glm" {
		if baseUrl == "" {
			baseUrl = "https://open.bigmodel.cn/api/paas/v4"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "glm-4.7"
		}
		configToml = fmt.Sprintf(`model_provider = "glm"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.glm]
name = "glm"
base_url = "%s"
wire_api = "responses"
request_max_retries = 4
stream_max_retries = 8
stream_idle_timeout_ms = 120000
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "doubao" {
		if baseUrl == "" {
			baseUrl = "https://ark.cn-beijing.volces.com/api/coding/v3"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "doubao-seed-code-preview-latest"
		}
		configToml = fmt.Sprintf(`model_provider = "doubao"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.doubao]
name = "doubao"
base_url = "%s"
wire_api = "responses"
request_max_retries = 4
stream_max_retries = 8
stream_idle_timeout_ms = 120000
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "讯飞星辰" || strings.ToLower(selectedModel.ModelName) == "xfyun" {
		if baseUrl == "" {
			baseUrl = "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "astron-code-latest"
		}
		configToml = fmt.Sprintf(`model_provider = "xfyun"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.xfyun]
name = "xfyun"
base_url = "%s"
wire_api = "responses"
request_max_retries = 4
stream_max_retries = 8
stream_idle_timeout_ms = 120000
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "kimi" {
		if baseUrl == "" {
			baseUrl = "https://api.kimi.com/coding/v1"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "kimi-for-coding"
		}
		configToml = fmt.Sprintf(`model_provider = "kimi"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.kimi]
name = "kimi"
base_url = "%s"
wire_api = "responses"
request_max_retries = 4
stream_max_retries = 8
stream_idle_timeout_ms = 120000
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "minimax" {
		if baseUrl == "" {
			baseUrl = "https://api.minimaxi.com/v1"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "MiniMax-M2.1"
		}
		configToml = fmt.Sprintf(`model_provider = "minimax"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.minimax]
name = "minimax"
base_url = "%s"
wire_api = "responses"
request_max_retries = 4
stream_max_retries = 8
stream_idle_timeout_ms = 120000
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "coderelay" {
		if baseUrl == "" {
			baseUrl = "https://api.code-relay.com/v1"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "gpt-5.2-codex"
		}
		configToml = fmt.Sprintf(`model_provider = "coderelay"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.coderelay]
name = "coderelay"
base_url = "%s"
wire_api = "responses"
`, modelId, baseUrl)
	} else if strings.ToLower(selectedModel.ModelName) == "阿里云" || strings.ToLower(selectedModel.ModelName) == "aliyun" {
		if baseUrl == "" {
			baseUrl = "https://coding.dashscope.aliyuncs.com/v1"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "glm-5"
		}
		configToml = fmt.Sprintf(`model_provider = "aliyun"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.aliyun]
name = "aliyun"
base_url = "%s"
wire_api = "responses"
request_max_retries = 4
stream_max_retries = 8
stream_idle_timeout_ms = 120000
`, modelId, baseUrl)
	} else {
		if baseUrl == "" {
			baseUrl = "https://api.aicodemirror.com/api/codex/backend-api/codex"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "gpt-5.2-codex"
		}
		configToml = fmt.Sprintf(`model_provider = "aicodemirror"
model = "%s"
model_reasoning_effort = "xhigh"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.aicodemirror]
name = "aicodemirror"
base_url = "%s"
wire_api = "responses"
`, modelId, baseUrl)
	}
	if selectedModel.IsCustom || (strings.ToLower(selectedModel.ModelName) != "aigocode" &&
		strings.ToLower(selectedModel.ModelName) != "deepseek" &&
		strings.ToLower(selectedModel.ModelName) != "glm" &&
		strings.ToLower(selectedModel.ModelName) != "doubao" &&
		strings.ToLower(selectedModel.ModelName) != "讯飞星辰" &&
		strings.ToLower(selectedModel.ModelName) != "xfyun" &&
		strings.ToLower(selectedModel.ModelName) != "kimi" &&
		strings.ToLower(selectedModel.ModelName) != "minimax" &&
		strings.ToLower(selectedModel.ModelName) != "coderelay" &&
		strings.ToLower(selectedModel.ModelName) != "阿里云" &&
		strings.ToLower(selectedModel.ModelName) != "aliyun" &&
		strings.ToLower(selectedModel.ModelName) != "aicodemirror") {
		// --- CUSTOM OR OTHER PROVIDERS ---
		wireApi := selectedModel.WireApi
		if wireApi == "" {
			wireApi = "responses"
		}
		providerName := strings.ToLower(selectedModel.ModelName)
		if providerName == "" || providerName == "custom" {
			providerName = "custom"
		}
		modelId := selectedModel.ModelId
		if modelId == "" {
			modelId = "gpt-5.2-codex"
		}
		configToml = fmt.Sprintf(`model_provider = "%s"
model = "%s"
model_reasoning_effort = "high"
disable_response_storage = true
preferred_auth_method = "apikey"
[model_providers.%s]
name = "%s"
base_url = "%s"
wire_api = "%s"
`, providerName, modelId, providerName, providerName, baseUrl, wireApi)
	}
	configBytes := []byte(configToml)
	// Check if config.toml needs update
	if existingData, err := os.ReadFile(configPath); err == nil {
		if bytes.Equal(existingData, configBytes) {
			return nil
		}
	}
	return os.WriteFile(configPath, configBytes, 0644)
}
func (a *App) syncToOpencodeSettings(config AppConfig, projectDir string, instanceID string) error {
	var selectedModel *ModelConfig
	for _, m := range config.Opencode.Models {
		if m.ModelName == config.Opencode.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected opencode model not found")
	}
	dir, configPath := a.getOpencodeConfigPaths(projectDir, instanceID)
	if strings.ToLower(selectedModel.ModelName) == "original" {
		a.clearOpencodeConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	baseUrl := selectedModel.ModelUrl
	modelId := selectedModel.ModelId
	providerName := selectedModel.ModelName
	// Fallback logic for Opencode (align with Codex providers)
	if modelId == "" {
		switch strings.ToLower(providerName) {
		case "deepseek":
			modelId = "deepseek-chat"
			if baseUrl == "" {
				baseUrl = "https://api.deepseek.com/v1"
			}
		case "glm":
			modelId = "glm-4.7"
			if baseUrl == "" {
				baseUrl = "https://open.bigmodel.cn/api/paas/v4"
			}
		case "doubao":
			modelId = "doubao-seed-code-preview-latest"
			if baseUrl == "" {
				baseUrl = "https://ark.cn-beijing.volces.com/api/coding/v3"
			}
		case "讯飞星辰", "xfyun":
			modelId = "astron-code-latest"
			if baseUrl == "" {
				baseUrl = "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2"
			}
		case "kimi":
			modelId = "kimi-for-coding"
			if baseUrl == "" {
				baseUrl = "https://api.kimi.com/coding/v1"
			}
		case "minimax":
			modelId = "MiniMax-M2.1"
			if baseUrl == "" {
				baseUrl = "https://api.minimaxi.com/v1"
			}
		case "阿里云", "aliyun":
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://coding.dashscope.aliyuncs.com/apps/anthropic/v1"
			}
		case "腾讯云", "tencent", "tencentcloud":
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://api.lkeap.cloud.tencent.com/coding/v3"
			}
		default:
			modelId = "opencode-1.0"
			if baseUrl == "" {
				baseUrl = "https://api.aicodemirror.com/api/opencode/v1"
			}
		}
	}
	// Build the JSON structure
	opencodeJson := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]interface{}{
			"myprovider": map[string]interface{}{
				"npm":  "@ai-sdk/openai-compatible",
				"name": providerName,
				"options": map[string]interface{}{
					"baseURL":   baseUrl,
					"apiKey":    selectedModel.ApiKey,
					"maxTokens": 8192,
				},
				"models": map[string]interface{}{
					modelId: map[string]interface{}{
						"name": modelId,
						"limit": map[string]interface{}{
							"context": 8192,
							"output":  8192,
						},
					},
				},
			},
		},
	}
	data, err := json.MarshalIndent(opencodeJson, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
func (a *App) syncToGeminiSettings(config AppConfig, projectDir string, instanceID string) error {
	var selectedModel *ModelConfig
	for _, m := range config.Gemini.Models {
		if m.ModelName == config.Gemini.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected gemini model not found")
	}

	dir, configPath, _ := a.getGeminiConfigPaths(projectDir, instanceID)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var configData map[string]interface{}

	// If using original (Google official)
	if strings.ToLower(selectedModel.ModelName) == "original" {
		a.log("Gemini: Using Google account authentication (Original mode)")
		configData = map[string]interface{}{
			"security": map[string]interface{}{
				"auth": map[string]interface{}{
					"selectedType": "oauth-personal",
				},
			},
			"general": map[string]interface{}{
				"previewFeatures": true,
			},
		}
	} else {
		// Non-original mode: Configure to use environment variables (API Key)
		configData = map[string]interface{}{
			"security": map[string]interface{}{
				"auth": map[string]interface{}{
					"selectedType": "gemini-api-key",
				},
			},
			"general": map[string]interface{}{
				"previewFeatures": true,
			},
		}
		a.log(fmt.Sprintf("Gemini: Configured to use environment variables (API Key from env)"))
	}

	// Use compact JSON format for faster serialization
	configJson, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		return err
	}

	// Check if file exists and has same content to avoid unnecessary writes
	if existingData, err := os.ReadFile(configPath); err == nil {
		if bytes.Equal(existingData, configJson) {
			// File already has the correct content, skip writing
			return nil
		}
	}

	return os.WriteFile(configPath, configJson, 0644)
}
func (a *App) syncToIFlowSettings(config AppConfig, projectDir string, instanceID string) error {
	var selectedModel *ModelConfig
	for _, m := range config.IFlow.Models {
		if m.ModelName == config.IFlow.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected iflow model not found")
	}
	dir, configPath := a.getIFlowConfigPaths(projectDir, instanceID)
	if strings.ToLower(selectedModel.ModelName) == "original" {
		a.clearIFlowConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Prepare defaults
	baseUrl := selectedModel.ModelUrl
	modelId := selectedModel.ModelId
	providerName := strings.ToLower(selectedModel.ModelName)
	// Fallback logic for iFlow (align with Codex providers)
	if modelId == "" {
		switch providerName {
		case "deepseek":
			modelId = "deepseek-chat"
			if baseUrl == "" {
				baseUrl = "https://api.deepseek.com/v1"
			}
		case "glm":
			modelId = "glm-4.7"
			if baseUrl == "" {
				baseUrl = "https://open.bigmodel.cn/api/paas/v4"
			}
		case "doubao":
			modelId = "doubao-seed-code-preview-latest"
			if baseUrl == "" {
				baseUrl = "https://ark.cn-beijing.volces.com/api/coding/v3"
			}
		case "kimi":
			modelId = "kimi-for-coding"
			if baseUrl == "" {
				baseUrl = "https://api.kimi.com/coding/v1"
			}
		case "minimax":
			modelId = "MiniMax-M2.1"
			if baseUrl == "" {
				baseUrl = "https://api.minimaxi.com/v1"
			}
		case "阿里云", "aliyun":
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://coding.dashscope.aliyuncs.com/apps/anthropic/v1"
			}
		case "腾讯云", "tencent", "tencentcloud":
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://api.lkeap.cloud.tencent.com/coding/v3"
			}
		default:
			modelId = "gpt-4o"
		}
	}
	// Build the JSON structure for settings.json
	settings := map[string]string{
		"selectedAuthType": "openai-compatible",
		"apiKey":           selectedModel.ApiKey,
		"baseUrl":          baseUrl,
		"modelName":        modelId,
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
func (a *App) syncToKiloSettings(config AppConfig, projectDir string, instanceID string) error {
	var selectedModel *ModelConfig
	for _, m := range config.Kilo.Models {
		if m.ModelName == config.Kilo.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected kilo model not found")
	}
	dir, configPath := a.getKiloConfigPaths(projectDir, instanceID)
	if strings.ToLower(selectedModel.ModelName) == "original" {
		a.clearKiloConfig()
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Read existing config if it exists
	var kiloConfig map[string]interface{}
	existingData, err := os.ReadFile(configPath)
	if err == nil {
		// File exists, parse it
		if err := json.Unmarshal(existingData, &kiloConfig); err != nil {
			// If parsing fails, create new config
			kiloConfig = make(map[string]interface{})
		}
	} else {
		// File doesn't exist, create new config
		kiloConfig = make(map[string]interface{})
	}
	// Prepare provider configuration
	baseUrl := selectedModel.ModelUrl
	modelId := selectedModel.ModelId
	providerName := strings.ToLower(selectedModel.ModelName)
	// Fallback logic for common providers
	if modelId == "" {
		switch providerName {
		case "deepseek":
			modelId = "deepseek-chat"
			if baseUrl == "" {
				baseUrl = "https://api.deepseek.com/v1"
			}
		case "glm":
			modelId = "glm-4.7"
			if baseUrl == "" {
				baseUrl = "https://open.bigmodel.cn/api/paas/v4"
			}
		case "doubao":
			modelId = "doubao-seed-code-preview-latest"
			if baseUrl == "" {
				baseUrl = "https://ark.cn-beijing.volces.com/api/coding/v3"
			}
		case "kimi":
			modelId = "kimi-for-coding"
			if baseUrl == "" {
				baseUrl = "https://api.kimi.com/coding/v1"
			}
		case "minimax":
			modelId = "MiniMax-M2.1"
			if baseUrl == "" {
				baseUrl = "https://api.minimaxi.com/v1"
			}
		case "xiaomi":
			modelId = "mimo-v2-flash"
			if baseUrl == "" {
				baseUrl = "https://api.xiaomimimo.com/v1"
			}
		case "阿里云", "aliyun":
			modelId = "glm-5"
			if baseUrl == "" {
				baseUrl = "https://coding.dashscope.aliyuncs.com/apps/anthropic/v1"
			}
		default:
			modelId = "gpt-4o"
		}
	}
	// Build provider object
	provider := map[string]interface{}{
		"id":            "default",
		"provider":      "openai",
		"openAiApiKey":  selectedModel.ApiKey,
		"openAiModelId": modelId,
		"openAiBaseUrl": baseUrl,
	}
	// Update providers array
	kiloConfig["providers"] = []interface{}{provider}
	// Write config file
	data, err := json.MarshalIndent(kiloConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func (a *App) syncToKodeSettings(config AppConfig, projectDir string, instanceID string) error {
	// Kode CLI uses .kode.json configuration file
	var selectedModel *ModelConfig
	for _, m := range config.Kode.Models {
		if m.ModelName == config.Kode.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil {
		return fmt.Errorf("selected kode model not found")
	}

	var kodeConfigPath string
	if projectDir != "" && instanceID != "" {
		// Use project-specific config with instance ID
		kodeConfigPath = filepath.Join(projectDir, ".aicoder", "kode", instanceID, "kode.json")
	} else {
		// Fallback to home directory
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		kodeConfigPath = filepath.Join(home, ".kode.json")
	}

	if err := os.MkdirAll(filepath.Dir(kodeConfigPath), 0755); err != nil {
		return err
	}

	// Create model profile
	modelProfile := map[string]interface{}{
		"name":          fmt.Sprintf("Custom OpenAI-Compatible API %s", selectedModel.ModelId),
		"provider":      "custom-openai",
		"modelName":     selectedModel.ModelId,
		"baseURL":       selectedModel.ModelUrl,
		"apiKey":        selectedModel.ApiKey,
		"maxTokens":     4096,
		"contextLength": 128000,
		"createdAt":     time.Now().UnixMilli(),
		"isActive":      true,
	}

	// Create full config structure
	kodeConfig := map[string]interface{}{
		"modelProfiles": []interface{}{modelProfile},
		"modelPointers": map[string]string{
			"main":    selectedModel.ModelId,
			"task":    selectedModel.ModelId,
			"compact": selectedModel.ModelId,
			"quick":   selectedModel.ModelId,
		},
		"defaultModelName":       selectedModel.ModelId,
		"hasCompletedOnboarding": true,
		"lastOnboardingVersion":  "2.0.3",
	}

	// Write config file
	data, err := json.MarshalIndent(kodeConfig, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(kodeConfigPath, data, 0644)
}
func (a *App) syncToCodeBuddySettings(config AppConfig, projectPath string) error {
	if projectPath == "" {
		projectPath = a.GetCurrentProjectPath()
	}
	if projectPath == "" {
		return nil
	}
	cbDir := filepath.Join(projectPath, ".codebuddy")
	if err := os.MkdirAll(cbDir, 0755); err != nil {
		return err
	}
	cbFilePath := filepath.Join(cbDir, "models.json")
	var cbModels []CodeBuddyModel
	var availableModelIds []string
	for _, m := range config.CodeBuddy.Models {
		// Only sync the currently selected model
		if m.ModelName != config.CodeBuddy.CurrentModel {
			continue
		}
		if strings.ToLower(m.ModelName) == "original" {
			continue
		}
		vendor := strings.ToLower(m.ModelName)
		idStr := m.ModelId
		if idStr == "" {
			switch vendor {
			case "deepseek":
				idStr = "deepseek-chat"
			case "glm":
				idStr = "glm-4.7"
			case "doubao":
				idStr = "doubao-seed-code-preview-latest"
			case "kimi":
				idStr = "kimi-for-coding"
			case "minimax":
				idStr = "MiniMax-M2.1"
			default:
				idStr = vendor + "-model"
			}
		}
		modelIds := strings.Split(idStr, ",")
		modelUrl := m.ModelUrl
		if modelUrl != "" && !strings.HasSuffix(modelUrl, "/chat/completions") {
			if strings.HasSuffix(modelUrl, "/") {
				modelUrl += "chat/completions"
			} else {
				modelUrl += "/chat/completions"
			}
		}
		for _, id := range modelIds {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			availableModelIds = append(availableModelIds, id)
			cbModels = append(cbModels, CodeBuddyModel{
				Id:               id,
				Name:             id,
				Vendor:           vendor,
				ApiKey:           m.ApiKey,
				MaxInputTokens:   200000,
				MaxOutputTokens:  8192,
				Url:              modelUrl,
				SupportsToolCall: true,
				SupportsImages:   true,
			})
		}
	}
	cbConfig := CodeBuddyFileConfig{
		Models:          cbModels,
		AvailableModels: availableModelIds,
	}
	data, err := json.MarshalIndent(cbConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cbFilePath, data, 0644)
}
func (a *App) syncToQoderSettings(config AppConfig, projectPath string) error {
	if projectPath == "" {
		projectPath = a.GetCurrentProjectPath()
	}
	if projectPath == "" {
		return nil
	}
	qDir := filepath.Join(projectPath, ".qoder")
	if err := os.MkdirAll(qDir, 0755); err != nil {
		return err
	}
	qFilePath := filepath.Join(qDir, "models.json")
	var qModels []CodeBuddyModel
	var availableModelIds []string
	for _, m := range config.Qoder.Models {
		// Only sync the currently selected model
		if m.ModelName != config.Qoder.CurrentModel {
			continue
		}
		if strings.ToLower(m.ModelName) == "original" {
			continue
		}
		vendor := strings.ToLower(m.ModelName)
		idStr := m.ModelId
		if idStr == "" {
			switch vendor {
			case "deepseek":
				idStr = "deepseek-chat"
			case "glm":
				idStr = "glm-4.7"
			case "doubao":
				idStr = "doubao-seed-code-preview-latest"
			case "kimi":
				idStr = "kimi-for-coding"
			case "minimax":
				idStr = "MiniMax-M2.1"
			default:
				idStr = vendor + "-model"
			}
		}
		modelIds := strings.Split(idStr, ",")
		modelUrl := m.ModelUrl
		if modelUrl != "" && !strings.HasSuffix(modelUrl, "/chat/completions") {
			if strings.HasSuffix(modelUrl, "/") {
				modelUrl += "chat/completions"
			} else {
				modelUrl += "/chat/completions"
			}
		}
		for _, id := range modelIds {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			availableModelIds = append(availableModelIds, id)
			qModels = append(qModels, CodeBuddyModel{
				Id:               id,
				Name:             id,
				Vendor:           vendor,
				ApiKey:           m.ApiKey,
				MaxInputTokens:   200000,
				MaxOutputTokens:  8192,
				Url:              modelUrl,
				SupportsToolCall: true,
				SupportsImages:   true,
			})
		}
	}
	qConfig := CodeBuddyFileConfig{
		Models:          qModels,
		AvailableModels: availableModelIds,
	}
	data, err := json.MarshalIndent(qConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(qFilePath, data, 0644)
}
func getBaseUrl(selectedModel *ModelConfig) string {
	// If user provided a URL for the selected model, always prefer it.
	if selectedModel.ModelUrl != "" {
		return selectedModel.ModelUrl
	}
	// Otherwise, fall back to hardcoded defaults for known providers that have them.
	baseUrl := "" // Default to empty string
	switch strings.ToLower(selectedModel.ModelName) {
	case "kimi":
		baseUrl = "https://api.kimi.com/coding"
	case "glm", "glm-4.7":
		baseUrl = "https://open.bigmodel.cn/api/anthropic"
	case "doubao":
		baseUrl = "https://ark.cn-beijing.volces.com/api/coding"
	case "讯飞星辰", "xfyun":
		baseUrl = "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic"
	case "minimax":
		baseUrl = "https://api.minimaxi.com/anthropic"
	case "deepseek":
		baseUrl = "https://api.deepseek.com/anthropic"
	case "gaccode":
		baseUrl = "https://gaccode.com/claudecode"
	case "百度千帆", "qianfan":
		baseUrl = "https://qianfan.baidubce.com/anthropic/coding"
	}
	return baseUrl
}
func (a *App) LaunchTool(toolName string, yoloMode bool, adminMode bool, pythonProject bool, pythonEnv string, projectDir string, useProxy bool) {
	a.log(fmt.Sprintf("LaunchTool called: %s, yolo=%v, admin=%v, py=%v, pyenv=%s, dir=%s, proxy=%v",
		toolName, yoloMode, adminMode, pythonProject, pythonEnv, projectDir, useProxy))
	a.log(fmt.Sprintf("Launching %s...", toolName))

	// Generate unique instance ID for this launch (timestamp-based)
	instanceID := fmt.Sprintf("%d", time.Now().UnixNano())

	// Only process Python environment if pythonProject is true
	if pythonProject && pythonEnv != "" && pythonEnv != "None (Default)" {
		a.log(fmt.Sprintf("Python project: using Python environment: %s", pythonEnv))
	} else {
		// Clear pythonEnv if not a Python project
		pythonEnv = ""
	}
	if projectDir == "" {
		projectDir = a.GetCurrentProjectPath()
	}
	config, err := a.LoadConfig()
	if err != nil {
		a.log("Error loading config: " + err.Error())
		return
	}
	var toolCfg ToolConfig
	var envKey, envBaseUrl string
	var binaryName string
	switch strings.ToLower(toolName) {
	case "claude":
		toolCfg = config.Claude
		envKey = "ANTHROPIC_AUTH_TOKEN"
		envBaseUrl = "ANTHROPIC_BASE_URL"
		binaryName = "claude"
	case "gemini":
		toolCfg = config.Gemini
		envKey = "GEMINI_API_KEY"
		envBaseUrl = "GOOGLE_GEMINI_BASE_URL"
		binaryName = "gemini"
	case "codex":
		toolCfg = config.Codex
		envKey = "OPENAI_API_KEY"
		envBaseUrl = "OPENAI_BASE_URL"
		binaryName = "codex"
	case "iflow":
		toolCfg = config.IFlow
		envKey = "IFLOW_API_KEY"
		envBaseUrl = "IFLOW_BASE_URL"
		binaryName = "iflow"
	case "kilo":
		toolCfg = config.Kilo
		envKey = "KILO_API_KEY"
		envBaseUrl = "KILO_BASE_URL"
		binaryName = "kilo"
	case "kode":
		toolCfg = config.Kode
		envKey = "OPENAI_API_KEY"
		envBaseUrl = "OPENAI_BASE_URL"
		binaryName = "kode"
	case "opencode":
		toolCfg = config.Opencode
		envKey = "OPENCODE_API_KEY"
		envBaseUrl = "OPENCODE_BASE_URL"
		binaryName = "opencode"
	case "codebuddy":
		toolCfg = config.CodeBuddy
		envKey = "CODEBUDDY_API_KEY"
		envBaseUrl = "CODEBUDDY_BASE_URL"
		binaryName = "codebuddy"
	case "qoder":
		toolCfg = config.Qoder
		envKey = "QODER_PERSONAL_ACCESS_TOKEN"
		envBaseUrl = "QODER_BASE_URL"
		binaryName = "qoder"
	default:
		return
	}
	var selectedModel *ModelConfig
	for _, m := range toolCfg.Models {
		if m.ModelName == toolCfg.CurrentModel {
			selectedModel = &m
			break
		}
	}
	if selectedModel == nil || toolCfg.CurrentModel == "" {
		title := "提示"
		message := "请先选择一个服务商。"
		if a.CurrentLanguage == "en" {
			title = "Notice"
			message = "Please select a provider first."
		}
		a.ShowMessage(title, message)
		return
	}
	// Ensure ActiveTool is set correctly for syncToSystemEnv
	config.ActiveTool = strings.ToLower(toolName)
	a.syncToSystemEnv(config)
	// Create env map for passing to batch script
	env := make(map[string]string)
	// Proxy settings
	if useProxy && goruntime.GOOS != "windows" {
		var proxyHost, proxyPort, proxyUsername, proxyPassword string
		// Get proxy configuration (matching project path > global default)
		var targetProj *ProjectConfig
		for i := range config.Projects {
			if config.Projects[i].Path == projectDir {
				targetProj = &config.Projects[i]
				break
			}
		}
		// Fallback to CurrentProject if path match not found
		if targetProj == nil {
			for i := range config.Projects {
				if config.Projects[i].Id == config.CurrentProject {
					targetProj = &config.Projects[i]
					break
				}
			}
		}
		if targetProj != nil {
			proxyHost = targetProj.ProxyHost
			proxyPort = targetProj.ProxyPort
			proxyUsername = targetProj.ProxyUsername
			proxyPassword = targetProj.ProxyPassword
		}
		// Use global default if project not configured
		if proxyHost == "" {
			proxyHost = config.DefaultProxyHost
			proxyPort = config.DefaultProxyPort
			proxyUsername = config.DefaultProxyUsername
			proxyPassword = config.DefaultProxyPassword
		}
		if proxyHost != "" && proxyPort != "" {
			var proxyURL string
			if proxyUsername != "" && proxyPassword != "" {
				proxyURL = fmt.Sprintf("http://%s:%s@%s:%s",
					proxyUsername, proxyPassword, proxyHost, proxyPort)
			} else {
				proxyURL = fmt.Sprintf("http://%s:%s", proxyHost, proxyPort)
			}
			// Set proxy environment variables (only in env map, not main process)
			env["HTTP_PROXY"] = proxyURL
			env["HTTPS_PROXY"] = proxyURL
			env["http_proxy"] = proxyURL
			env["https_proxy"] = proxyURL
			a.log(fmt.Sprintf("Proxy enabled: %s:%s", proxyHost, proxyPort))
		}
	}
	if strings.ToLower(selectedModel.ModelName) != "original" {
		// --- OTHER PROVIDER MODE: WRITE CONFIG & SET ENV ---
		// Only add to env map, do NOT set in main process (to avoid cross-contamination)
		env[envKey] = selectedModel.ApiKey
		if selectedModel.ModelUrl != "" && envBaseUrl != "" {
			env[envBaseUrl] = selectedModel.ModelUrl
		}
		// Add CODEBUDDY_CODE_MAX_OUTPUT_TOKENS for DeepSeek
		if strings.ToLower(selectedModel.ModelName) == "deepseek" {
			env["CODEBUDDY_CODE_MAX_OUTPUT_TOKENS"] = "8192"
		}
		// Set generic model name env var if applicable
		if selectedModel.ModelId != "" {
			switch strings.ToLower(toolName) {
			case "claude":
				env["ANTHROPIC_MODEL"] = selectedModel.ModelId
			case "gemini":
				env["GOOGLE_GEMINI_MODEL"] = selectedModel.ModelId
			case "codex":
				env["OPENAI_MODEL"] = selectedModel.ModelId
			case "opencode":
				env["OPENCODE_MODEL"] = selectedModel.ModelId
			case "codebuddy":
				// env["CODEBUDDY_MODEL"] = selectedModel.ModelId
			case "qoder":
				// Qoder doesn't use model env var
			case "iflow":
				// iFlow uses settings.json, but maybe env var too?
				env["IFLOW_MODEL"] = selectedModel.ModelId
			case "kilo":
				env["KILO_MODEL"] = selectedModel.ModelId
			}
		}
		if strings.ToLower(toolName) == "claude" {
			switch strings.ToLower(selectedModel.ModelName) {
			case "百度千帆", "qianfan":
				modelId := selectedModel.ModelId
				if modelId == "" {
					modelId = "qianfan-code-latest"
				}
				env["ANTHROPIC_AUTH_TOKEN"] = selectedModel.ApiKey
				env["ANTHROPIC_BASE_URL"] = "https://qianfan.baidubce.com/anthropic/coding"
				env["ANTHROPIC_MODEL"] = modelId
				env["ANTHROPIC_SMALL_FAST_MODEL"] = modelId
				env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
				env["API_TIMEOUT_MS"] = "600000"
			}
		}
		// Tool-specific configurations
		// Tools that support pure env vars: clear old config files to avoid interference
		// Tools that need config files: use instanceID for isolation
		switch strings.ToLower(toolName) {
		case "claude":
			// Claude Code reads env vars directly, no config file needed
			// Clear old config to prevent interference with env vars
			a.clearClaudeConfig()
			a.log("Claude: Using environment variables only (cleared old config)")
		case "gemini":
			// Gemini reads env vars directly, no config file needed
			// Clear old config to prevent interference with env vars
			a.clearGeminiConfig()
			a.log("Gemini: Using environment variables only (cleared old config)")
		case "codex":
			env["WIRE_API"] = "responses"
			// Ensure OpenAI standard vars for Codex
			env["OPENAI_API_KEY"] = selectedModel.ApiKey
			if selectedModel.ModelUrl != "" {
				env["OPENAI_BASE_URL"] = selectedModel.ModelUrl
			}
			// Clear old config to prevent interference with env vars
			a.clearCodexConfig()
			a.log("Codex: Using environment variables only (cleared old config)")
		case "opencode":
			// Opencode needs config file - use instanceID for isolation
			a.syncToOpencodeSettings(config, projectDir, instanceID)
		case "codebuddy":
			// CodeBuddy may need config file
			// a.syncToCodeBuddySettings(config, projectDir, instanceID)
		case "qoder":
			// Qoder needs config file - keep the sync call
			a.syncToQoderSettings(config, projectDir)
		case "iflow":
			// iFlow needs config file - use instanceID for isolation
			// Ensure OpenAI standard vars for iFlow (compatibility)
			env["OPENAI_API_KEY"] = selectedModel.ApiKey
			if selectedModel.ModelUrl != "" {
				env["OPENAI_BASE_URL"] = selectedModel.ModelUrl
			}
			a.syncToIFlowSettings(config, projectDir, instanceID)
		case "kilo":
			// Kilo needs config file - use instanceID for isolation
			a.syncToKiloSettings(config, projectDir, instanceID)
		case "kode":
			// Kode needs config file - use instanceID for isolation
			a.syncToKodeSettings(config, projectDir, instanceID)
		}
	} else {
		// --- ORIGINAL MODE: CLEANUP SPECIFIC TOOL ONLY ---
		// Clear config files to ensure tools use original settings
		if strings.ToLower(toolName) == "claude" {
			a.clearClaudeConfig()
		} else if strings.ToLower(toolName) == "gemini" {
			a.clearGeminiConfig()
		} else if strings.ToLower(toolName) == "codex" {
			a.clearCodexConfig()
		} else if strings.ToLower(toolName) == "opencode" {
			a.clearOpencodeConfig()
		} else if strings.ToLower(toolName) == "codebuddy" {
			// Codebuddy might need cleanup too if we added a clear function
		} else if strings.ToLower(toolName) == "qoder" {
			// Qoder cleanup if needed
		} else if strings.ToLower(toolName) == "iflow" {
			a.clearIFlowConfig()
		} else if strings.ToLower(toolName) == "kilo" {
			a.clearKiloConfig()
		} else if strings.ToLower(toolName) == "kode" {
			a.clearKodeConfig()
		}
		a.log(fmt.Sprintf("Running %s in Original mode: Custom configurations cleared.", toolName))
	}

	// Claude Code Agent Teams mode
	if strings.ToLower(toolName) == "claude" {
		// Find the current project config to check team_mode
		for _, proj := range config.Projects {
			if proj.Path == projectDir || proj.Id == config.CurrentProject {
				if proj.TeamMode {
					env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] = "1"
					a.log("Claude Code Agent Teams mode enabled")
				}
				break
			}
		}
	}

	if config.RemoteEnabled && (strings.ToLower(toolName) == "claude" || strings.ToLower(toolName) == "codex" || strings.ToLower(toolName) == "opencode" || strings.ToLower(toolName) == "iflow" || strings.ToLower(toolName) == "kilo" || strings.ToLower(toolName) == "kode") {
		spec, err := a.buildRemoteLaunchSpec(toolName, config, yoloMode, adminMode, pythonEnv, projectDir, useProxy)
		if err != nil {
			a.log("build remote launch spec failed: " + err.Error())
			return
		}

		if a.remoteSessions == nil {
			a.remoteSessions = NewRemoteSessionManager(a)
			hubClient := NewRemoteHubClient(a, a.remoteSessions)
			a.remoteSessions.SetHubClient(hubClient)
			_ = hubClient.Connect()
		}

		_, err = a.remoteSessions.Create(spec)
		if err != nil {
			a.log("create remote session failed: " + err.Error())
		}
		return
	}

	// Platform specific launch
	a.platformLaunch(binaryName, yoloMode, adminMode, pythonEnv, projectDir, env, selectedModel.ModelId)
}
func (a *App) log(message string) {
	if a.IsInitMode {
		fmt.Println(message)
	}
	if a.ctx != nil {
		a.emitEvent("env-log", message)
	}
}
func (a *App) getConfigPath() (string, error) {
	if a.testHomeDir != "" {
		return filepath.Join(a.testHomeDir, ".aicoder_config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aicoder_config.json"), nil
}
func (a *App) LoadConfig() (AppConfig, error) {
	path, err := a.getConfigPath()
	if err != nil {
		return AppConfig{}, err
	}
	// Helper for default models
	defaultClaudeModels := []ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: ""},
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/anthropic", ApiKey: ""},
		{ModelName: "Kimi", ModelId: "kimi-k2-thinking", ModelUrl: "https://api.kimi.com/coding", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding", ApiKey: ""},
		{ModelName: "讯飞星辰", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic", ApiKey: "", HasSubscription: true},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/anthropic", ApiKey: ""},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/anthropic", ApiKey: ""},
		{ModelName: "百度千帆", ModelId: "qianfan-code-latest", ModelUrl: "https://qianfan.baidubce.com/anthropic/coding", ApiKey: "", HasSubscription: true},
		{ModelName: "ChatFire", ModelId: "sonnet", ModelUrl: "https://api.chatfire.cn", ApiKey: ""},
		{ModelName: "腾讯云", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/anthropic", ApiKey: "", HasSubscription: true},
		{ModelName: "摩尔线程", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net", ApiKey: "", HasSubscription: true},
		{ModelName: "快手", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/kat-coder-pro-v1/claude-code-proxy", ApiKey: "", HasSubscription: true},
		{ModelName: "阿里云", ModelId: "glm-5", ModelUrl: "https://coding.dashscope.aliyuncs.com/apps/anthropic", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultGeminiModels := []ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: ""},
		{ModelName: "ChatFire", ModelId: "gemini-2.5-pro", ModelUrl: "https://api.chatfire.cn/v1beta/models/gemini-2.5-pro:generateContent", ApiKey: ""},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultCodexModels := []ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: ""},
		{ModelName: "ChatFire", ModelId: "gpt-5.1-codex-mini", ModelUrl: "https://api.chatfire.cn/v1", ApiKey: "", WireApi: "responses"},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "讯飞星辰", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", WireApi: "responses", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "腾讯云", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "摩尔线程", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "快手", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultOpencodeModels := []ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: ""},
		{ModelName: "ChatFire", ModelId: "gpt-4o", ModelUrl: "https://api.chatfire.cn/v1", ApiKey: ""},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "讯飞星辰", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "腾讯云", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "摩尔线程", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "快手", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultQoderModels := []ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: ""},
		{ModelName: "Qoder", ModelId: "qoder-1.0", ModelUrl: "https://api.qoder.com/v1", ApiKey: ""},
	}
	defaultIFlowModels := []ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: ""},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "讯飞星辰", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "XiaoMi", ModelId: "mimo-v2-flash", ModelUrl: "https://api.xiaomimimo.com/v1", ApiKey: ""},
		{ModelName: "腾讯云", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "摩尔线程", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "快手", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultKiloModels := []ModelConfig{
		{ModelName: "Original", ModelId: "", ModelUrl: "", ApiKey: ""},
		{ModelName: "ChatFire", ModelId: "gpt-4o", ModelUrl: "https://api.chatfire.cn/v1", ApiKey: ""},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "讯飞星辰", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "XiaoMi", ModelId: "mimo-v2-flash", ModelUrl: "https://api.xiaomimimo.com/v1", ApiKey: ""},
		{ModelName: "腾讯云", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "摩尔线程", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "快手", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	defaultKodeModels := []ModelConfig{
		{ModelName: "ChatFire", ModelId: "gpt-4o", ModelUrl: "https://api.chatfire.cn/v1", ApiKey: ""},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1", ApiKey: ""},
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4", ApiKey: ""},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding/v3", ApiKey: ""},
		{ModelName: "讯飞星辰", ModelId: "astron-code-latest", ModelUrl: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", ApiKey: "", HasSubscription: true},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1", ApiKey: ""},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1", ApiKey: ""},
		{ModelName: "XiaoMi", ModelId: "mimo-v2-flash", ModelUrl: "https://api.xiaomimimo.com/v1", ApiKey: ""},
		{ModelName: "腾讯云", ModelId: "glm-5", ModelUrl: "https://api.lkeap.cloud.tencent.com/coding/v3", ApiKey: "", HasSubscription: true},
		{ModelName: "摩尔线程", ModelId: "GLM-4.7", ModelUrl: "https://coding-plan-endpoint.kuaecloud.net/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "快手", ModelId: "kat-coder-pro-v1", ModelUrl: "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", ApiKey: "", HasSubscription: true},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom1", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom2", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom3", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom4", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
		{ModelName: "Custom5", ModelId: "", ModelUrl: "", ApiKey: "", IsCustom: true},
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Check for old config file for migration
		home, _ := os.UserHomeDir()
		oldPath := filepath.Join(home, ".claude_model_config.json")
		if _, err := os.Stat(oldPath); err == nil {
			// Migrate old config
			data, err := os.ReadFile(oldPath)
			if err == nil {
				var oldConfig struct {
					CurrentModel string          `json:"current_model"`
					Models       []ModelConfig   `json:"models"`
					Projects     []ProjectConfig `json:"projects"`
					CurrentProj  string          `json:"current_project"`
				}
				if err := json.Unmarshal(data, &oldConfig); err == nil {
					config := AppConfig{
						Claude: ToolConfig{
							CurrentModel: oldConfig.CurrentModel,
							Models:       oldConfig.Models,
						},
						Gemini: ToolConfig{
							CurrentModel: "Gemini 1.5 Pro",
							Models:       defaultGeminiModels,
						},
						Codex: ToolConfig{
							CurrentModel: "Codex",
							Models:       defaultCodexModels,
						},
						Opencode: ToolConfig{
							CurrentModel: "Original",
							Models:       defaultOpencodeModels,
						},
						CodeBuddy: ToolConfig{
							CurrentModel: "Original",
							Models:       defaultOpencodeModels,
						},
						Qoder: ToolConfig{
							CurrentModel: "Original",
							Models:       defaultQoderModels,
						},
						IFlow: ToolConfig{
							CurrentModel: "Original",
							Models:       defaultIFlowModels,
						},
						Kilo: ToolConfig{
							CurrentModel: "Original",
							Models:       defaultKiloModels,
						},
						Kode: ToolConfig{
							CurrentModel: "ChatFire",
							Models:       defaultKodeModels,
						},
						Projects:           oldConfig.Projects,
						CurrentProject:     oldConfig.CurrentProj,
						ActiveTool:         "claude",
						ShowGemini:         true,
						ShowCodex:          true,
						ShowOpenCode:       true,
						ShowKode:           true,
						ShowCursor:         true,
						ShowCodeBuddy:      true,
						ShowQoder:          true,
						ShowIFlow:          true,
						ShowKilo:           true,
						PowerOptimization:  true,
						RemoteEnabled:      false,
						RemoteHubURL:       "",
						RemoteHubCenterURL: defaultRemoteHubCenterURL,
						RemoteEmail:        "",
						RemoteSN:           "",
						RemoteUserID:       "",
						RemoteMachineID:    "",
						RemoteMachineToken: "",
						RemoteHeartbeatSec: 10,
					}
					a.SaveConfig(config)
					// Optional: os.Remove(oldPath)
					return config, nil
				}
			}
		}
		// Create default config
		defaultConfig := AppConfig{
			Claude: ToolConfig{
				CurrentModel: "GLM",
				Models:       defaultClaudeModels,
			},
			Gemini: ToolConfig{
				CurrentModel: "Gemini 1.5 Pro",
				Models:       defaultGeminiModels,
			},
			Codex: ToolConfig{
				CurrentModel: "Codex",
				Models:       defaultCodexModels,
			},
			Opencode: ToolConfig{
				CurrentModel: "AiCodeMirror",
				Models:       defaultOpencodeModels,
			},
			CodeBuddy: ToolConfig{
				CurrentModel: "AiCodeMirror",
				Models:       defaultOpencodeModels,
			},
			Qoder: ToolConfig{
				CurrentModel: "Original",
				Models:       defaultQoderModels,
			},
			IFlow: ToolConfig{
				CurrentModel: "Original",
				Models:       defaultIFlowModels,
			},
			Kilo: ToolConfig{
				CurrentModel: "Original",
				Models:       defaultKiloModels,
			},
			Kode: ToolConfig{
				CurrentModel: "ChatFire",
				Models:       defaultKodeModels,
			},
			Projects: []ProjectConfig{
				{
					Id:       "default",
					Name:     "Project 1",
					Path:     home,
					YoloMode: false,
				},
			},
			CurrentProject:     "default",
			ActiveTool:         "claude",
			ShowGemini:         true,
			ShowCodex:          true,
			ShowOpenCode:       true,
			ShowCodeBuddy:      true,
			ShowQoder:          true,
			ShowIFlow:          true,
			ShowKilo:           true,
			ShowKode:           true,
			ShowCursor:         true,
			PowerOptimization:  true,
			EnvCheckInterval:   7,    // Default to 7 days
			UseWindowsTerminal: true, // Default to true, will only work if Windows Terminal is installed
			RemoteEnabled:      false,
			RemoteHubURL:       "",
			RemoteHubCenterURL: defaultRemoteHubCenterURL,
			RemoteEmail:        "",
			RemoteSN:           "",
			RemoteUserID:       "",
			RemoteMachineID:    "",
			RemoteMachineToken: "",
			RemoteHeartbeatSec: 10,
		}
		err = a.SaveConfig(defaultConfig)
		return defaultConfig, err
	}
	config := AppConfig{
		ShowGemini:         true,
		ShowCodex:          true,
		ShowOpenCode:       true,
		ShowCursor:         true,
		ShowCodeBuddy:      true,
		ShowQoder:          true,
		ShowIFlow:          true,
		ShowKilo:           true,
		ShowKode:           true,
		PowerOptimization:  true,
		RemoteHubCenterURL: defaultRemoteHubCenterURL,
		RemoteHeartbeatSec: 10,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}

	// Check if this is an old config without the new show_* fields
	// If show_kilo is not present in JSON, default it to true
	var rawConfig map[string]interface{}
	json.Unmarshal(data, &rawConfig)
	hasShowKilo := false
	if _, ok := rawConfig["show_kilo"]; ok {
		hasShowKilo = true
	}
	hasShowCursor := false
	if _, ok := rawConfig["show_cursor"]; ok {
		hasShowCursor = true
	}
	hasPowerOptimization := false
	if _, ok := rawConfig["power_optimization"]; ok {
		hasPowerOptimization = true
	}

	err = json.Unmarshal(data, &config)
	if err != nil {
		return config, err
	}

	// Set default values for new fields if not present in old configs
	if !hasShowKilo {
		config.ShowKilo = true
	}
	if !hasShowCursor {
		config.ShowCursor = true
	}
	if !hasPowerOptimization {
		config.PowerOptimization = true
	}
	if config.RemoteHeartbeatSec < 5 {
		config.RemoteHeartbeatSec = 10
	}

	// Set default values for new fields if not present or invalid
	if config.EnvCheckInterval < 2 || config.EnvCheckInterval > 30 {
		config.EnvCheckInterval = 7 // Default to 7 days
	}
	// Ensure defaults for new fields
	if config.Claude.CurrentModel == "" && len(config.Claude.Models) > 0 {
		config.Claude.CurrentModel = config.Claude.Models[0].ModelName
	}
	// Helper to ensure a model exists in the list
	ensureModel := func(models *[]ModelConfig, name, url, id, wireApi string, hasSubscription ...bool) {
		hasSub := false
		if len(hasSubscription) > 0 {
			hasSub = hasSubscription[0]
		}
		for i := range *models {
			if strings.EqualFold((*models)[i].ModelName, name) {
				(*models)[i].ModelName = name // Update to canonical casing
				if url != "" {
					(*models)[i].ModelUrl = url
				}
				// Only set ModelId if user hasn't customized it (empty means not set yet)
				if id != "" && (*models)[i].ModelId == "" {
					(*models)[i].ModelId = id
				}
				if wireApi != "" {
					(*models)[i].WireApi = wireApi
				}
				(*models)[i].HasSubscription = hasSub
				return
			}
		}
		*models = append(*models, ModelConfig{ModelName: name, ModelUrl: url, ModelId: id, WireApi: wireApi, ApiKey: "", HasSubscription: hasSub})
	}
	// Helper to remove a model from the list
	removeModel := func(models *[]ModelConfig, name string) {
		var newModels []ModelConfig
		for _, m := range *models {
			if !strings.EqualFold(m.ModelName, name) {
				newModels = append(newModels, m)
			}
		}
		*models = newModels
	}
	if config.Gemini.Models == nil || len(config.Gemini.Models) == 0 {
		config.Gemini.Models = defaultGeminiModels
		config.Gemini.CurrentModel = "Original"
	}
	if config.Codex.Models == nil || len(config.Codex.Models) == 0 {
		config.Codex.Models = defaultCodexModels
		config.Codex.CurrentModel = "Original"
	}
	if config.Opencode.Models == nil || len(config.Opencode.Models) == 0 {
		config.Opencode.Models = defaultOpencodeModels
		config.Opencode.CurrentModel = "Original"
	}
	if config.CodeBuddy.Models == nil || len(config.CodeBuddy.Models) == 0 {
		config.CodeBuddy.Models = defaultOpencodeModels
		config.CodeBuddy.CurrentModel = "Original"
	}
	if config.Qoder.Models == nil || len(config.Qoder.Models) == 0 {
		config.Qoder.Models = defaultQoderModels
		config.Qoder.CurrentModel = "Original"
	}
	if config.IFlow.Models == nil || len(config.IFlow.Models) == 0 {
		config.IFlow.Models = defaultIFlowModels
		config.IFlow.CurrentModel = "Original"
	}
	if config.Kilo.Models == nil || len(config.Kilo.Models) == 0 {
		config.Kilo.Models = defaultKiloModels
		config.Kilo.CurrentModel = "Original"
	}
	if config.Kode.Models == nil || len(config.Kode.Models) == 0 {
		config.Kode.Models = defaultKodeModels
		config.Kode.CurrentModel = "ChatFire"
	}
	ensureModel(&config.Claude.Models, "ChatFire", "https://api.chatfire.cn", "sonnet", "")
	ensureModel(&config.Claude.Models, "DeepSeek", "https://api.deepseek.com/anthropic", "deepseek-chat", "")
	ensureModel(&config.Claude.Models, "Kimi", "https://api.kimi.com/coding", "kimi-k2-thinking", "")
	ensureModel(&config.Claude.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.Claude.Models, "讯飞星辰", "https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic", "astron-code-latest", "", true)
	ensureModel(&config.Claude.Models, "GLM", "https://open.bigmodel.cn/api/anthropic", "glm-4.7", "")
	ensureModel(&config.Claude.Models, "MiniMax", "https://api.minimaxi.com/anthropic", "MiniMax-M2.1", "")
	ensureModel(&config.Claude.Models, "百度千帆", "https://qianfan.baidubce.com/anthropic/coding", "qianfan-code-latest", "", true)
	ensureModel(&config.Claude.Models, "XiaoMi", "https://api.xiaomimimo.com/anthropic", "mimo-v2-flash", "")
	ensureModel(&config.Claude.Models, "腾讯云", "https://api.lkeap.cloud.tencent.com/coding/anthropic", "glm-5", "", true)
	ensureModel(&config.Claude.Models, "摩尔线程", "https://coding-plan-endpoint.kuaecloud.net", "GLM-4.7", "", true)
	ensureModel(&config.Claude.Models, "快手", "https://wanqing.streamlakeapi.com/api/gateway/coding/kat-coder-pro-v1/claude-code-proxy", "kat-coder-pro-v1", "", true)
	ensureModel(&config.Claude.Models, "阿里云", "https://coding.dashscope.aliyuncs.com/apps/anthropic", "glm-5", "", true)
	ensureModel(&config.Gemini.Models, "ChatFire", "https://api.chatfire.cn/v1beta/models/gemini-2.5-pro:generateContent", "gemini-2.5-pro", "")
	ensureModel(&config.Codex.Models, "ChatFire", "https://api.chatfire.cn/v1", "gpt-5.1-codex-mini", "responses")
	ensureModel(&config.Codex.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.Codex.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "glm-4.7", "")
	ensureModel(&config.Codex.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.Codex.Models, "讯飞星辰", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "responses", true)
	ensureModel(&config.Codex.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.Codex.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.Codex.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.Codex.Models, "腾讯云", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.Codex.Models, "摩尔线程", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.Codex.Models, "快手", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.Opencode.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.Opencode.Models, "ChatFire", "https://api.chatfire.cn/v1", "gpt-4o", "")
	ensureModel(&config.Opencode.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "glm-4.7", "")
	ensureModel(&config.Opencode.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.Opencode.Models, "讯飞星辰", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.Opencode.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.Opencode.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.Opencode.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.Opencode.Models, "腾讯云", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.Opencode.Models, "摩尔线程", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.Opencode.Models, "快手", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.CodeBuddy.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.CodeBuddy.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "glm-4.7", "")
	ensureModel(&config.CodeBuddy.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.CodeBuddy.Models, "讯飞星辰", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.CodeBuddy.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.CodeBuddy.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.CodeBuddy.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.CodeBuddy.Models, "腾讯云", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.CodeBuddy.Models, "摩尔线程", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.CodeBuddy.Models, "快手", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.IFlow.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.IFlow.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "glm-4.7", "")
	ensureModel(&config.IFlow.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.IFlow.Models, "讯飞星辰", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.IFlow.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.IFlow.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.IFlow.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.IFlow.Models, "腾讯云", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.IFlow.Models, "摩尔线程", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.IFlow.Models, "快手", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.Kilo.Models, "ChatFire", "https://api.chatfire.cn/v1", "gpt-4o", "")
	ensureModel(&config.Kilo.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.Kilo.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "glm-4.7", "")
	ensureModel(&config.Kilo.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.Kilo.Models, "讯飞星辰", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.Kilo.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.Kilo.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.Kilo.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.Kilo.Models, "腾讯云", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.Kilo.Models, "摩尔线程", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.Kilo.Models, "快手", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)
	ensureModel(&config.Kode.Models, "ChatFire", "https://api.chatfire.cn/v1", "gpt-4o", "")
	ensureModel(&config.Kode.Models, "DeepSeek", "https://api.deepseek.com/v1", "deepseek-chat", "")
	ensureModel(&config.Kode.Models, "GLM", "https://open.bigmodel.cn/api/coding/paas/v4", "glm-4.7", "")
	ensureModel(&config.Kode.Models, "Doubao", "https://ark.cn-beijing.volces.com/api/coding/v3", "doubao-seed-code-preview-latest", "")
	ensureModel(&config.Kode.Models, "讯飞星辰", "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", "astron-code-latest", "", true)
	ensureModel(&config.Kode.Models, "Kimi", "https://api.kimi.com/coding/v1", "kimi-for-coding", "")
	ensureModel(&config.Kode.Models, "MiniMax", "https://api.minimaxi.com/v1", "MiniMax-M2.1", "")
	ensureModel(&config.Kode.Models, "XiaoMi", "https://api.xiaomimimo.com/v1", "mimo-v2-flash", "")
	ensureModel(&config.Kode.Models, "腾讯云", "https://api.lkeap.cloud.tencent.com/coding/v3", "glm-5", "", true)
	ensureModel(&config.Kode.Models, "摩尔线程", "https://coding-plan-endpoint.kuaecloud.net/v1", "GLM-4.7", "", true)
	ensureModel(&config.Kode.Models, "快手", "https://wanqing.streamlakeapi.com/api/gateway/coding/v1", "kat-coder-pro-v1", "", true)

	// Purge Aliyun from other tools if it exists
	removeModel(&config.Gemini.Models, "阿里云")
	removeModel(&config.Codex.Models, "阿里云")
	removeModel(&config.Opencode.Models, "阿里云")
	removeModel(&config.CodeBuddy.Models, "阿里云")
	removeModel(&config.Gemini.Models, "百度千帆")
	removeModel(&config.Codex.Models, "百度千帆")
	removeModel(&config.Opencode.Models, "百度千帆")
	removeModel(&config.CodeBuddy.Models, "百度千帆")
	removeModel(&config.IFlow.Models, "百度千帆")
	removeModel(&config.Kilo.Models, "百度千帆")
	removeModel(&config.Kode.Models, "百度千帆")
	removeModel(&config.IFlow.Models, "阿里云")
	removeModel(&config.Kilo.Models, "阿里云")
	removeModel(&config.Kode.Models, "阿里云")
	removeModel(&config.Gemini.Models, "aliyun")
	removeModel(&config.Codex.Models, "aliyun")
	removeModel(&config.Opencode.Models, "aliyun")
	removeModel(&config.CodeBuddy.Models, "aliyun")
	removeModel(&config.IFlow.Models, "aliyun")
	removeModel(&config.Kilo.Models, "aliyun")
	removeModel(&config.Kode.Models, "aliyun")
	// Ensure 'Original' is always present and first
	ensureOriginal := func(models *[]ModelConfig) {
		found := false
		for _, m := range *models {
			if m.ModelName == "Original" {
				found = true
				break
			}
		}
		if !found {
			*models = append([]ModelConfig{{ModelName: "Original", ModelUrl: "", ApiKey: ""}}, *models...)
		}
	}
	// Opencode does NOT use common relay providers
	cleanOpencodeModels := func(models *[]ModelConfig) {
		var newModels []ModelConfig
		for _, m := range *models {
			name := strings.ToLower(m.ModelName)
			if name != "aigocode" && name != "aicodemirror" && name != "coderelay" && name != "chatfire" {
				newModels = append(newModels, m)
			}
		}
		*models = newModels
	}
	ensureOriginal(&config.Claude.Models)
	ensureOriginal(&config.Gemini.Models)
	ensureOriginal(&config.Codex.Models)
	ensureOriginal(&config.Opencode.Models)
	ensureOriginal(&config.CodeBuddy.Models)
	ensureOriginal(&config.Qoder.Models)
	ensureOriginal(&config.IFlow.Models)
	ensureOriginal(&config.Kilo.Models)
	cleanOpencodeModels(&config.Opencode.Models)
	cleanOpencodeModels(&config.CodeBuddy.Models)
	cleanOpencodeModels(&config.IFlow.Models)
	// Ensure at least 2 custom models are always present, and at most 6
	// Custom models are identified by IsCustom flag, not by name
	ensureCustom := func(models *[]ModelConfig) {
		customCount := 0
		for _, m := range *models {
			if m.IsCustom {
				customCount++
			}
		}
		// Ensure at least 2 custom models exist
		for customCount < 2 {
			customCount++
			name := "Custom"
			if customCount > 1 {
				name = fmt.Sprintf("Custom%d", customCount-1)
			}
			*models = append(*models, ModelConfig{ModelName: name, ModelUrl: "", ApiKey: "", IsCustom: true})
		}
		// Ensure at most 6 custom models exist
		if customCount > 6 {
			var newModels []ModelConfig
			customAdded := 0
			for _, m := range *models {
				if m.IsCustom {
					if customAdded < 6 {
						newModels = append(newModels, m)
						customAdded++
					}
				} else {
					newModels = append(newModels, m)
				}
			}
			*models = newModels
		}
	}
	ensureCustom(&config.Claude.Models)
	ensureCustom(&config.Gemini.Models)
	ensureCustom(&config.Codex.Models)
	ensureCustom(&config.Opencode.Models)
	ensureCustom(&config.CodeBuddy.Models)
	ensureCustom(&config.IFlow.Models)
	ensureCustom(&config.Kilo.Models)
	ensureCustom(&config.Kode.Models)
	// Qoder only has Original and Qoder
	// Preserve existing Qoder key if present
	var existingQoderKey string
	for _, m := range config.Qoder.Models {
		if m.ModelName == "Qoder" {
			existingQoderKey = m.ApiKey
			break
		}
	}
	config.Qoder.Models = defaultQoderModels
	if existingQoderKey != "" {
		for i := range config.Qoder.Models {
			if config.Qoder.Models[i].ModelName == "Qoder" {
				config.Qoder.Models[i].ApiKey = existingQoderKey
				break
			}
		}
	}
	if !strings.EqualFold(config.Qoder.CurrentModel, "Original") && !strings.EqualFold(config.Qoder.CurrentModel, "Qoder") {
		config.Qoder.CurrentModel = "Original"
	}
	// Ensure custom models are always last for all tools
	// Custom models are identified by IsCustom flag, not by name
	moveCustomToLast := func(models *[]ModelConfig) {
		var customModels []ModelConfig
		var newModels []ModelConfig
		for _, m := range *models {
			if m.IsCustom {
				customModels = append(customModels, m)
			} else {
				newModels = append(newModels, m)
			}
		}
		// Append all custom models at the end
		*models = append(newModels, customModels...)
	}
	// Ensure 'Original' is always first for all tools
	ensureOriginalFirst := func(models *[]ModelConfig) {
		var originalModel *ModelConfig
		var newModels []ModelConfig
		for i := range *models {
			m := &(*models)[i]
			if m.ModelName == "Original" {
				originalModel = m
			} else {
				newModels = append(newModels, *m)
			}
		}
		if originalModel != nil {
			*models = append([]ModelConfig{*originalModel}, newModels...)
		}
	}
	moveCustomToLast(&config.Claude.Models)
	moveCustomToLast(&config.Gemini.Models)
	moveCustomToLast(&config.Codex.Models)
	moveCustomToLast(&config.Opencode.Models)
	moveCustomToLast(&config.CodeBuddy.Models)
	moveCustomToLast(&config.IFlow.Models)
	moveCustomToLast(&config.Kilo.Models)
	moveCustomToLast(&config.Kode.Models)
	ensureOriginalFirst(&config.Claude.Models)
	ensureOriginalFirst(&config.Gemini.Models)
	ensureOriginalFirst(&config.Codex.Models)
	ensureOriginalFirst(&config.Opencode.Models)
	ensureOriginalFirst(&config.CodeBuddy.Models)
	ensureOriginalFirst(&config.Qoder.Models)
	ensureOriginalFirst(&config.IFlow.Models)
	ensureOriginalFirst(&config.Kilo.Models)
	// Ensure CurrentModel is valid
	if config.Gemini.CurrentModel == "" {
		config.Gemini.CurrentModel = "Original"
	}
	if config.Codex.CurrentModel == "" {
		config.Codex.CurrentModel = "Original"
	}
	if config.Opencode.CurrentModel == "" {
		config.Opencode.CurrentModel = "Original"
	}
	if config.CodeBuddy.CurrentModel == "" {
		config.CodeBuddy.CurrentModel = "Original"
	}
	if config.Qoder.CurrentModel == "" {
		config.Qoder.CurrentModel = "Original"
	}
	if config.IFlow.CurrentModel == "" {
		config.IFlow.CurrentModel = "Original"
	}
	if config.Kilo.Models == nil || len(config.Kilo.Models) == 0 {
		config.Kilo.Models = defaultKiloModels
		config.Kilo.CurrentModel = "Original"
	}
	if config.Kilo.CurrentModel == "" {
		config.Kilo.CurrentModel = "Original"
	}
	if config.Kode.Models == nil || len(config.Kode.Models) == 0 {
		config.Kode.Models = defaultKodeModels
		config.Kode.CurrentModel = "ChatFire"
	}
	if config.Kode.CurrentModel == "" {
		config.Kode.CurrentModel = "ChatFire"
	}
	if config.ActiveTool == "" {
		config.ActiveTool = "message"
	}
	// Normalize CurrentModel casing for all tools
	normalizeCurrentModel := func(toolCfg *ToolConfig) {
		for _, m := range toolCfg.Models {
			if strings.EqualFold(m.ModelName, toolCfg.CurrentModel) {
				toolCfg.CurrentModel = m.ModelName
				break
			}
		}
	}
	normalizeCurrentModel(&config.Claude)
	normalizeCurrentModel(&config.Gemini)
	normalizeCurrentModel(&config.Codex)
	normalizeCurrentModel(&config.Opencode)
	normalizeCurrentModel(&config.CodeBuddy)
	normalizeCurrentModel(&config.Qoder)
	normalizeCurrentModel(&config.IFlow)
	normalizeCurrentModel(&config.Kilo)
	normalizeCurrentModel(&config.Kode)
	return config, nil
}

// getProviderModel gets the model for a specific provider name from a tool config
func getProviderModel(toolConfig *ToolConfig, providerName string) *ModelConfig {
	for i := range toolConfig.Models {
		if strings.EqualFold(toolConfig.Models[i].ModelName, providerName) {
			return &toolConfig.Models[i]
		}
	}
	return nil
}

// syncAllProviderApiKeys synchronizes apikeys of all providers (except 'Original' and 'Custom') across all tools
func syncAllProviderApiKeys(a *App, oldConfig, newConfig *AppConfig) {
	// Map of tools for easy access
	tools := map[string]*ToolConfig{
		"claude":    &newConfig.Claude,
		"gemini":    &newConfig.Gemini,
		"codex":     &newConfig.Codex,
		"opencode":  &newConfig.Opencode,
		"codebuddy": &newConfig.CodeBuddy,
		"qoder":     &newConfig.Qoder,
		"iflow":     &newConfig.IFlow,
		"kilo":      &newConfig.Kilo,
		"kode":      &newConfig.Kode,
	}
	oldTools := map[string]*ToolConfig{
		"claude":    &oldConfig.Claude,
		"gemini":    &oldConfig.Gemini,
		"codex":     &oldConfig.Codex,
		"opencode":  &oldConfig.Opencode,
		"codebuddy": &oldConfig.CodeBuddy,
		"qoder":     &oldConfig.Qoder,
		"iflow":     &oldConfig.IFlow,
		"kilo":      &oldConfig.Kilo,
		"kode":      &oldConfig.Kode,
	}
	// providerName (lower) -> intended API key
	intentions := make(map[string]string)
	activeToolName := strings.ToLower(newConfig.ActiveTool)
	// 1. Detect Intent from Active Tool (Highest Priority)
	if activeTool, ok := tools[activeToolName]; ok {
		oldActive := oldTools[activeToolName]
		if oldActive != nil {
			for _, m := range activeTool.Models {
				if strings.EqualFold(m.ModelName, "Original") || strings.EqualFold(m.ModelName, "Custom") || m.IsCustom {
					continue
				}
				oldM := getProviderModel(oldActive, m.ModelName)
				// If key changed or a new key was added where none existed
				if (oldM != nil && m.ApiKey != oldM.ApiKey) || (oldM == nil && m.ApiKey != "") {
					intentions[strings.ToLower(m.ModelName)] = m.ApiKey
					a.log(fmt.Sprintf("Sync: detected %s intent from active tool %s", m.ModelName, activeToolName))
				}
			}
		}
	}
	// 2. Detect Intent from other tools (if not already captured from active tool)
	for name, tool := range tools {
		if name == activeToolName {
			continue
		}
		oldTool := oldTools[name]
		if oldTool == nil {
			continue
		}
		for _, m := range tool.Models {
			if strings.EqualFold(m.ModelName, "Original") || strings.EqualFold(m.ModelName, "Custom") || m.IsCustom {
				continue
			}
			lowerName := strings.ToLower(m.ModelName)
			if _, handled := intentions[lowerName]; handled {
				continue
			}
			oldM := getProviderModel(oldTool, m.ModelName)
			if (oldM != nil && m.ApiKey != oldM.ApiKey) || (oldM == nil && m.ApiKey != "") {
				intentions[lowerName] = m.ApiKey
				a.log(fmt.Sprintf("Sync: detected %s intent from tool %s", m.ModelName, name))
			}
		}
	}
	// 3. Propagate all intentions to ALL tools
	for providerLower, targetKey := range intentions {
		for _, tool := range tools {
			for i := range tool.Models {
				if strings.ToLower(tool.Models[i].ModelName) == providerLower {
					if tool.Models[i].ApiKey != targetKey {
						tool.Models[i].ApiKey = targetKey
					}
				}
			}
		}
	}
}
func (a *App) SaveConfig(config AppConfig) error {
	// Sanitize: Ensure Custom models have a name (prevent empty tab button)
	sanitizeCustomNames := func(models []ModelConfig) {
		for i := range models {
			if models[i].IsCustom && strings.TrimSpace(models[i].ModelName) == "" {
				models[i].ModelName = "Custom"
			}
		}
	}
	sanitizeCustomNames(config.Claude.Models)
	sanitizeCustomNames(config.Gemini.Models)
	sanitizeCustomNames(config.Codex.Models)
	sanitizeCustomNames(config.Opencode.Models)
	sanitizeCustomNames(config.CodeBuddy.Models)
	sanitizeCustomNames(config.Qoder.Models)
	sanitizeCustomNames(config.IFlow.Models)
	sanitizeCustomNames(config.Kilo.Models)
	sanitizeCustomNames(config.Kode.Models)
	// Load old config to compare for sync logic
	var oldConfig AppConfig
	path, _ := a.getConfigPath()
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &oldConfig)
	}
	// Sync all apikeys across all tools before saving
	syncAllProviderApiKeys(a, &oldConfig, &config)
	if err := a.saveToPath(path, config); err != nil {
		return err
	}
	a.refreshPowerOptimizationStateFromConfig(config)
	if OnConfigChanged != nil {
		OnConfigChanged(config)
	}
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil && a.remoteSessions.hubClient.IsConnected() {
		go a.remoteSessions.hubClient.SyncLaunchProjects()
	}
	return nil
}
func (a *App) saveToPath(path string, config AppConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

type UpdateResult struct {
	HasUpdate     bool   `json:"has_update"`
	LatestVersion string `json:"latest_version"`
	ReleaseUrl    string `json:"release_url"`
	TagName       string `json:"tag_name"`
	DownloadUrl   string `json:"download_url"`
}

func (a *App) CheckUpdate(currentVersion string) (UpdateResult, error) {
	// Use GitHub API instead of web scraping
	// Updated URL: aicoder instead of cceasy
	url := "https://api.github.com/repos/RapidAI/MaClaw/releases/latest"
	a.log(a.tr("CheckUpdate: Starting check against %s", url))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		a.log(a.tr("CheckUpdate: Failed to create request: %v", err))
		return UpdateResult{LatestVersion: "检查失败", ReleaseUrl: ""}, err
	}
	req.Header.Set("User-Agent", "MaClaw")
	// Add GitHub token for authentication (helps avoid rate limiting)
	// Priority: 1) GITHUB_TOKEN environment variable, 2) Built-in default token (base64 encoded 3 times)
	const defaultGitHubTokenEncoded = "V2pKb2QxZ3hjREJPVmtZeVVXNXNUV0ZZVmtOaFZFSktWbXBuTWxsWVNrOVNhbWhYWTI1a1ZsRlVUbXBWZWtaUVlsWk9TR1IzUFQwPQ=="
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		// Decode the base64 encoded token (3 times)
		decoded := defaultGitHubTokenEncoded
		for i := 0; i < 3; i++ {
			decodedBytes, err := base64.StdEncoding.DecodeString(decoded)
			if err != nil {
				a.log(a.tr("CheckUpdate: Failed to decode token at iteration %d: %v", i+1, err))
				decoded = ""
				break
			}
			decoded = string(decodedBytes)
		}
		if decoded != "" {
			token = decoded
			a.log(a.tr("CheckUpdate: Using built-in GitHub token for authentication"))
		}
	} else {
		a.log(a.tr("CheckUpdate: Using custom GitHub token from environment variable"))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.log(a.tr("CheckUpdate: Failed to fetch GitHub API: %v", err))
		return UpdateResult{LatestVersion: "网络错误", ReleaseUrl: ""}, err
	}
	defer resp.Body.Close()
	a.log(a.tr("CheckUpdate: HTTP Status: %d", resp.StatusCode))
	// Log rate limit headers for debugging
	a.log(a.tr("CheckUpdate: Rate Limit: %s/%s, Reset: %s",
		resp.Header.Get("X-RateLimit-Remaining"),
		resp.Header.Get("X-RateLimit-Limit"),
		resp.Header.Get("X-RateLimit-Reset")))
	// Check HTTP status
	if resp.StatusCode != 200 {
		a.log(a.tr("CheckUpdate: GitHub API returned status %d", resp.StatusCode))
		bodyText, _ := io.ReadAll(resp.Body)
		a.log(a.tr("CheckUpdate: Response: %s", string(bodyText[:min(len(bodyText), 200)])))
		// Provide specific error message for rate limiting
		if resp.StatusCode == 403 {
			remaining := resp.Header.Get("X-RateLimit-Remaining")
			if remaining == "0" {
				resetTime := resp.Header.Get("X-RateLimit-Reset")
				a.log(a.tr("CheckUpdate: Rate limit exceeded, resets at: %s", resetTime))
				return UpdateResult{LatestVersion: "速率限制", ReleaseUrl: ""},
					fmt.Errorf("github api rate limit exceeded (resets at %s)", resetTime)
			}
			return UpdateResult{LatestVersion: "访问受限", ReleaseUrl: ""},
				fmt.Errorf("github api access forbidden (status 403)")
		}
		return UpdateResult{LatestVersion: "API错误", ReleaseUrl: ""}, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		a.log(a.tr("CheckUpdate: Failed to read response body: %v", err))
		return UpdateResult{LatestVersion: "读取失败", ReleaseUrl: ""}, err
	}
	// Log raw response for debugging
	a.log(a.tr("CheckUpdate: Raw response length: %d bytes", len(body)))
	a.log(a.tr("CheckUpdate: Response body: %s", string(body[:min(len(body), 500)])))
	// Parse JSON response
	var release map[string]interface{}
	if err := json.Unmarshal(body, &release); err != nil {
		a.log(a.tr("CheckUpdate: Failed to parse JSON: %v", err))
		a.log(a.tr("CheckUpdate: Response body: %s", string(body[:min(len(body), 500)])))
		return UpdateResult{LatestVersion: "解析失败", ReleaseUrl: ""}, err
	}
	// Log parsed keys
	a.log(a.tr("CheckUpdate: Parsed keys: %v", getMapKeys(release)))
	// Extract version from either 'name' or 'tag_name'
	var tagName string
	// Try 'tag_name' field first (e.g.", "v2.0.0.2")
	if tag, ok := release["tag_name"].(string); ok && tag != "" {
		tagName = tag
		a.log(a.tr("CheckUpdate: Found version in 'tag_name' field: %s", tagName))
	} else if name, ok := release["name"].(string); ok && name != "" {
		// Fallback to 'name' field (e.g.", "V2.0.0.2")
		tagName = name
		a.log(a.tr("CheckUpdate: Found version in 'name' field: %s", tagName))
	} else {
		a.log(a.tr("CheckUpdate: Neither 'name' nor 'tag_name' found. Available: %v", release))
		return UpdateResult{LatestVersion: "找不到版本号", ReleaseUrl: ""}, fmt.Errorf("no version found in release")
	}
	a.log(a.tr("CheckUpdate: Using version: %s", tagName))
	// Extract release URL
	htmlURL, _ := release["html_url"].(string)
	// Extract download URL from assets
	var downloadUrl string
	var targetFileName string
	if goruntime.GOOS == "darwin" {
		targetFileName = "MaClaw-Universal.pkg"
	} else {
		targetFileName = "MaClaw-Setup.exe"
	}
	// Parse assets array from GitHub API response
	if assets, ok := release["assets"].([]interface{}); ok && len(assets) > 0 {
		a.log(a.tr("CheckUpdate: Found %d assets in release", len(assets)))
		for _, assetInterface := range assets {
			if asset, ok := assetInterface.(map[string]interface{}); ok {
				if name, ok := asset["name"].(string); ok && name == targetFileName {
					if browserDownloadUrl, ok := asset["browser_download_url"].(string); ok {
						downloadUrl = browserDownloadUrl
						a.log(a.tr("CheckUpdate: Found download URL from assets: %s", downloadUrl))
						break
					}
				}
			}
		}
	}
	// Fallback: construct URL manually if not found in assets
	if downloadUrl == "" {
		downloadUrl = fmt.Sprintf("https://github.com/RapidAI/CodeClaw/releases/download/%s/%s", tagName, targetFileName)
		a.log(a.tr("CheckUpdate: Assets not found, using constructed URL: %s", downloadUrl))
	}
	// Keep original version with V prefix for display
	displayVersion := strings.TrimSpace(tagName)
	if !strings.HasPrefix(strings.ToUpper(displayVersion), "V") {
		displayVersion = "V" + displayVersion
	}
	// Clean version strings for comparison (lowercase, no V prefix)
	latestVersionForComparison := strings.TrimPrefix(strings.ToLower(tagName), "v")
	cleanCurrent := strings.TrimPrefix(strings.ToLower(currentVersion), "v")
	cleanCurrent = strings.Split(cleanCurrent, " ")[0]
	// Log for debugging
	a.log(a.tr("CheckUpdate: Latest version: %s, Current version: %s, Display version: %s", latestVersionForComparison, cleanCurrent, displayVersion))
	// Compare versions
	if compareVersions(latestVersionForComparison, cleanCurrent) > 0 {
		a.log(a.tr("CheckUpdate: Update available! %s > %s", latestVersionForComparison, cleanCurrent))
		return UpdateResult{
			HasUpdate:     true,
			LatestVersion: displayVersion,
			ReleaseUrl:    htmlURL,
			TagName:       tagName,
			DownloadUrl:   downloadUrl,
		}, nil
	}
	a.log(a.tr("CheckUpdate: Already on latest version"))
	return UpdateResult{
		HasUpdate:     false,
		LatestVersion: displayVersion,
		ReleaseUrl:    htmlURL,
		TagName:       tagName,
		DownloadUrl:   downloadUrl,
	}, nil
}

// Helper function to get map keys
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

type DownloadProgress struct {
	Percentage float64 `json:"percentage"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Status     string  `json:"status"` // "downloading", "completed", "error", "cancelled"
	Error      string  `json:"error,omitempty"`
}

func (a *App) DownloadUpdate(url string, fileName string) (string, error) {
	a.log(fmt.Sprintf("DownloadUpdate: Starting download from %s", url))
	downloadsDir, err := a.GetDownloadsFolder()
	if err != nil {
		return "", fmt.Errorf("failed to get downloads folder: %w", err)
	}
	destPath := filepath.Join(downloadsDir, fileName)
	// Create context with cancel for this download
	ctx, cancel := context.WithCancel(context.Background())
	downloadID := fileName
	a.downloadMutex.Lock()
	a.downloadCancelers[downloadID] = cancel
	a.downloadMutex.Unlock()
	defer func() {
		a.downloadMutex.Lock()
		delete(a.downloadCancelers, downloadID)
		a.downloadMutex.Unlock()
		cancel()
	}()
	// If file exists, try to remove it first
	if _, err := os.Stat(destPath); err == nil {
		os.Remove(destPath)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MaClaw-App")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status: %s", resp.Status)
	}
	// Validation Logic
	// 1. Check Content-Type
	contentType := resp.Header.Get("Content-Type")
	a.log(fmt.Sprintf("DownloadUpdate: Content-Type: %s", contentType))
	if !strings.Contains(strings.ToLower(contentType), "application/octet-stream") &&
		!strings.Contains(strings.ToLower(contentType), "application/x-msdownload") &&
		!strings.Contains(strings.ToLower(contentType), "application/x-dosexec") {
		// Just a warning for now, as some servers might send weird types
		a.log(fmt.Sprintf("Warning: Unexpected Content-Type: %s", contentType))
	}
	// 2. Check File Size (> 5MB)
	if resp.ContentLength < 5*1024*1024 {
		return "", fmt.Errorf("file too small (%d bytes), possibly an error page", resp.ContentLength)
	}
	// 3. Check Extension
	expectedExt := ".exe"
	if goruntime.GOOS == "darwin" {
		expectedExt = ".pkg"
	}
	if !strings.HasSuffix(strings.ToLower(fileName), expectedExt) {
		return "", fmt.Errorf("invalid file extension: %s (expected %s)", fileName, expectedExt)
	}
	size := resp.ContentLength
	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	var downloaded int64
	buffer := make([]byte, 64*1024)
	lastReport := time.Now()
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := out.Write(buffer[:n])
			if writeErr != nil {
				return "", writeErr
			}
			downloaded += int64(n)
			// Report progress every 100ms
			if time.Since(lastReport) > 100*time.Millisecond {
				percentage := 0.0
				if size > 0 {
					percentage = float64(downloaded) / float64(size) * 100
				}
				a.emitEvent("download-progress", DownloadProgress{
					Percentage: percentage,
					Downloaded: downloaded,
					Total:      size,
					Status:     "downloading",
				})
				lastReport = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() == context.Canceled {
				a.emitEvent("download-progress", DownloadProgress{
					Status: "cancelled",
				})
				return "", fmt.Errorf("download cancelled")
			}
			a.emitEvent("download-progress", DownloadProgress{
				Status: "error",
				Error:  err.Error(),
			})
			return "", err
		}
	}
	// Final progress report
	a.emitEvent("download-progress", DownloadProgress{
		Percentage: 100,
		Downloaded: downloaded,
		Total:      size,
		Status:     "completed",
	})
	return destPath, nil
}
func (a *App) CancelDownload(downloadID string) {
	a.downloadMutex.Lock()
	defer a.downloadMutex.Unlock()
	if cancel, ok := a.downloadCancelers[downloadID]; ok {
		cancel()
		delete(a.downloadCancelers, downloadID)
	}
}
func (a *App) RecoverCC() error {
	a.emitRecoverLog("Starting recovery process...")
	home, err := os.UserHomeDir()
	if err != nil {
		a.emitRecoverLog(fmt.Sprintf("Error getting home dir: %v", err))
		return err
	}
	// Remove ~/.claude directory
	claudeDir := filepath.Join(home, ".claude")
	a.emitRecoverLog(fmt.Sprintf("Checking directory: %s", claudeDir))
	if _, err := os.Stat(claudeDir); !os.IsNotExist(err) {
		a.emitRecoverLog("Found .claude directory. Removing...")
		if err := os.RemoveAll(claudeDir); err != nil {
			a.emitRecoverLog(fmt.Sprintf("Failed to remove .claude directory: %v", err))
			return fmt.Errorf("failed to remove .claude directory: %w", err)
		}
		a.emitRecoverLog("Successfully removed .claude directory.")
	} else {
		a.emitRecoverLog(".claude directory not found, skipping.")
	}
	// Remove ~/.claude.json file
	claudeJsonPath := filepath.Join(home, ".claude.json")
	a.emitRecoverLog(fmt.Sprintf("Checking file: %s", claudeJsonPath))
	if _, err := os.Stat(claudeJsonPath); !os.IsNotExist(err) {
		a.emitRecoverLog("Found .claude.json file. Removing...")
		if err := os.Remove(claudeJsonPath); err != nil && !os.IsNotExist(err) {
			a.emitRecoverLog(fmt.Sprintf("Failed to remove .claude.json file: %v", err))
			return fmt.Errorf("failed to remove .claude.json file: %w", err)
		}
		a.emitRecoverLog("Successfully removed .claude.json file.")
	} else {
		a.emitRecoverLog(".claude.json file not found, skipping.")
	}
	// Remove ~/.claude.json.backup file
	claudeJsonBackupPath := filepath.Join(home, ".claude.json.backup")
	a.emitRecoverLog(fmt.Sprintf("Checking file: %s", claudeJsonBackupPath))
	if _, err := os.Stat(claudeJsonBackupPath); !os.IsNotExist(err) {
		a.emitRecoverLog("Found .claude.json.backup file. Removing...")
		if err := os.Remove(claudeJsonBackupPath); err != nil && !os.IsNotExist(err) {
			a.emitRecoverLog(fmt.Sprintf("Failed to remove .claude.json.backup file: %v", err))
			return fmt.Errorf("failed to remove .claude.json.backup file: %w", err)
		}
		a.emitRecoverLog("Successfully removed .claude.json.backup file.")
	} else {
		a.emitRecoverLog(".claude.json.backup file not found, skipping.")
	}
	a.emitRecoverLog("Recovery process completed successfully.")
	return nil
}
func (a *App) emitRecoverLog(msg string) {
	a.emitEvent("recover-log", msg)
}
func (a *App) ShowMessage(title, message string) {
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
	})
}
func (a *App) emitEvent(name string, data ...interface{}) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data...)
	}
}
func (a *App) ClipboardGetText() (string, error) {
	// Try Wails runtime first
	if a.ctx != nil {
		text, err := runtime.ClipboardGetText(a.ctx)
		if err == nil && text != "" {
			return text, nil
		}
	}
	// Fallback for macOS: use pbpaste command
	cmd := exec.Command("pbpaste")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		return out.String(), nil
	}
	return "", nil
}
func (a *App) fetchRemoteMarkdown(repo, file string) (string, error) {
	// Use GitHub API with timestamp to bypass all caches
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=main&t=%d", repo, file, time.Now().UnixNano())
	// Create a new transport to avoid connection reuse caching
	transport := &http.Transport{
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
	}
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "Failed to create request: " + err.Error(), nil
	}
	// GitHub API headers - request raw content directly
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	req.Header.Set("User-Agent", "MaClaw-App")
	req.Header.Set("Cache-Control", "no-cache, no-store")
	req.Header.Set("Pragma", "no-cache")
	// Add GitHub token for authentication (helps avoid rate limiting)
	// Priority: 1) GITHUB_TOKEN environment variable, 2) Built-in default token (base64 encoded 3 times)
	const defaultGitHubTokenEncoded = "V2pKb2QxZ3hjREJPVmtZeVVXNXNUV0ZZVmtOaFZFSktWbXBuTWxsWVNrOVNhbWhYWTI1a1ZsRlVUbXBWZWtaUVlsWk9TR1IzUFQwPQ=="
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		// Decode the base64 encoded token (3 times)
		decoded := defaultGitHubTokenEncoded
		for i := 0; i < 3; i++ {
			decodedBytes, err := base64.StdEncoding.DecodeString(decoded)
			if err != nil {
				decoded = ""
				break
			}
			decoded = string(decodedBytes)
		}
		if decoded != "" {
			token = decoded
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "Failed to fetch remote message: " + err.Error(), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Remote content unavailable (Status: %d %s)", resp.StatusCode, resp.Status), nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "Error reading remote content: " + err.Error(), nil
	}
	return string(data), nil
}
func (a *App) ReadBBS() (string, error) {
	return a.fetchRemoteMarkdown("rapidaicoder/msg", "bbs.md")
}
func (a *App) ReadTutorial() (string, error) {
	return a.fetchRemoteMarkdown("rapidaicoder/msg", "tutorial.md")
}
func (a *App) ReadThanks() (string, error) {
	return a.fetchRemoteMarkdown("rapidaicoder/msg", "thanks.md")
}

// compareVersions returns 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	for i := 0; i < maxLen; i++ {
		val1 := 0
		if i < len(parts1) {
			fmt.Sscanf(parts1[i], "%d", &val1)
		}
		val2 := 0
		if i < len(parts2) {
			fmt.Sscanf(parts2[i], "%d", &val2)
		}
		if val1 > val2 {
			return 1
		}
		if val1 < val2 {
			return -1
		}
	}
	return 0
}
func (a *App) getInstalledClaudeVersion(claudePath string) (string, error) {
	// Check if the path exists
	if _, err := os.Stat(claudePath); err != nil {
		// If explicit path fails, try finding it in PATH if it's just "claude"
		if claudePath == "claude" {
			path, err := exec.LookPath("claude")
			if err != nil {
				return "", err
			}
			claudePath = path
		} else {
			return "", err
		}
	}
	var cmd *exec.Cmd
	cmd = createVersionCmd(claudePath)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output format example: claude-code/0.2.29 darwin-arm64 node-v22.12.0
	output := strings.TrimSpace(string(out))
	parts := strings.Split(output, " ")
	if len(parts) > 0 {
		// "claude-code/0.2.29"
		verParts := strings.Split(parts[0], "/")
		if len(verParts) == 2 {
			return verParts[1], nil
		}
		// If output is just the version (unlikely but possible)
		if len(parts) == 1 && strings.Contains(parts[0], ".") {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("unexpected output format: %s", output)
}
func (a *App) getLatestNpmVersion(npmPath string, packageName string) (string, error) {
	var cmd *exec.Cmd
	// Use npm view <package> version
	localCacheDir := a.GetLocalCacheDir()
	if err := os.MkdirAll(localCacheDir, 0755); err != nil {
		a.log(fmt.Sprintf("Warning: Failed to create local npm cache dir: %v", err))
	}
	args := []string{"view", packageName, "version", "--cache", localCacheDir}
	if strings.HasPrefix(strings.ToLower(a.CurrentLanguage), "zh") {
		args = append(args, "--registry=https://registry.npmmirror.com")
	}
	cmd = createNpmInstallCmd(npmPath, args) // Using createNpmInstallCmd as it's a general npm command runner
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ListPythonEnvironments returns a list of all available Python environments
func (a *App) ListPythonEnvironments() []PythonEnvironment {
	envs := []PythonEnvironment{}
	// Add default "None" option
	envs = append(envs, PythonEnvironment{
		Name: "None (Default)",
		Path: "",
		Type: "system",
	})
	// Detect Conda environments
	condaEnvs := a.detectCondaEnvironments()
	envs = append(envs, condaEnvs...)
	// Could add detection for virtualenv, venv, etc. here
	return envs
}

// detectCondaEnvironments finds all Anaconda/Miniconda environments
func (a *App) detectCondaEnvironments() []PythonEnvironment {
	envs := []PythonEnvironment{}
	envMap := make(map[string]PythonEnvironment)
	// Helper to add env
	addEnv := func(name, path string) {
		if name == "" || path == "" {
			return
		}
		if _, exists := envMap[name]; !exists {
			a.log(a.tr("Found conda environment: %s at %s", name, path))
			envMap[name] = PythonEnvironment{
				Name: name,
				Path: path,
				Type: "conda",
			}
		}
	}
	// 1. Try 'conda env list'
	condaCmd := a.findCondaCommand()
	if condaCmd != "" {
		a.log(a.tr("Using conda command: ") + condaCmd)
		var cmd *exec.Cmd
		if goruntime.GOOS == "windows" {
			// Use platform-specific function to create command with hidden window
			cmd = createCondaEnvListCmd(condaCmd)
		} else {
			cmd = exec.Command(condaCmd, "env", "list")
		}
		output, err := cmd.CombinedOutput()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				// Skip comments and empty lines
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) == 0 {
					continue
				}
				var name, path string
				// Handle parsing
				// Case 1: "* /path" (unnamed, active)
				// Case 2: "/path" (unnamed)
				// Case 3: "name * /path" (named, active)
				// Case 4: "name /path" (named)
				firstIsPath := strings.Contains(parts[0], "/") || strings.Contains(parts[0], "\\") || (goruntime.GOOS == "windows" && strings.Contains(parts[0], ":"))
				if parts[0] == "*" {
					// Case 1
					if len(parts) > 1 {
						path = strings.Join(parts[1:], " ")
						name = filepath.Base(path)
					}
				} else if firstIsPath {
					// Case 2
					path = strings.Join(parts, " ")
					name = filepath.Base(path)
				} else {
					// Case 3 or 4
					name = parts[0]
					if len(parts) > 1 {
						startIdx := 1
						if parts[1] == "*" {
							startIdx = 2
						}
						if startIdx < len(parts) {
							path = strings.Join(parts[startIdx:], " ")
						}
					}
				}
				addEnv(name, path)
			}
		} else {
			// Only log as info, not error - conda command failed but this is not critical
			a.log(a.tr("Note: Unable to list conda environments via command (conda may not be fully initialized): ") + err.Error())
		}
	}
	// 2. Scan common env directories (Fallback/Augment)
	roots := []string{}
	// Conda installation root envs
	condaRoot := a.getCondaRoot()
	if condaRoot != "" {
		roots = append(roots, filepath.Join(condaRoot, "envs"))
		// Also add root environment (base)
		addEnv("base", condaRoot)
	}
	// User .conda envs
	home, _ := os.UserHomeDir()
	roots = append(roots, filepath.Join(home, ".conda", "envs"))
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					addEnv(entry.Name(), filepath.Join(root, entry.Name()))
				}
			}
		}
	}
	// Convert map to slice
	for _, env := range envMap {
		envs = append(envs, env)
	}
	// Only log if conda environments were found
	if len(envs) > 0 {
		a.log(a.tr("Total conda environments found: %d", len(envs)))
	}
	return envs
}

// findCondaCommand tries to locate the conda executable
func (a *App) findCondaCommand() string {
	// Try common conda command names (include .bat for Windows)
	condaCmds := []string{"conda.exe", "conda.bat", "conda"}
	// First check CONDA_EXE environment variable
	if condaExe := os.Getenv("CONDA_EXE"); condaExe != "" {
		if _, err := os.Stat(condaExe); err == nil {
			a.log(a.tr("Found conda from CONDA_EXE: ") + condaExe)
			return condaExe
		}
	}
	for _, cmd := range condaCmds {
		// Check if command exists in PATH
		if path, err := exec.LookPath(cmd); err == nil {
			a.log(a.tr("Found conda in PATH: ") + path)
			return path
		}
	}
	// Try common installation paths
	commonPaths := a.getCommonCondaPaths()
	a.log(a.tr("Searching for conda in %d common paths...", len(commonPaths)))
	for _, basePath := range commonPaths {
		// Check if the base path exists first
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			continue
		}
		for _, cmd := range condaCmds {
			fullPath := filepath.Join(basePath, cmd)
			if _, err := os.Stat(fullPath); err == nil {
				a.log(a.tr("Found conda at: ") + fullPath)
				return fullPath
			}
			// Also check in Scripts subdirectory (Windows)
			scriptsPath := filepath.Join(basePath, "Scripts", cmd)
			if _, err := os.Stat(scriptsPath); err == nil {
				a.log(a.tr("Found conda at: ") + scriptsPath)
				return scriptsPath
			}
			// Check in condabin subdirectory (newer Anaconda installations)
			condabinPath := filepath.Join(basePath, "condabin", cmd)
			if _, err := os.Stat(condabinPath); err == nil {
				a.log(a.tr("Found conda at: ") + condabinPath)
				return condabinPath
			}
			// Check in bin subdirectory (Linux/macOS)
			binPath := filepath.Join(basePath, "bin", cmd)
			if _, err := os.Stat(binPath); err == nil {
				a.log(a.tr("Found conda at: ") + binPath)
				return binPath
			}
		}
	}
	// No need to log if conda not found - it's normal if user doesn't use conda
	return ""
}

// getCommonCondaPaths returns platform-specific common conda installation paths
func (a *App) getCommonCondaPaths() []string {
	paths := []string{}
	homeDir := a.GetUserHomeDir()
	// Check CONDA_PREFIX environment variable first
	if condaPrefix := os.Getenv("CONDA_PREFIX"); condaPrefix != "" {
		paths = append(paths, condaPrefix)
	}
	// Check CONDA_EXE environment variable
	if condaExe := os.Getenv("CONDA_EXE"); condaExe != "" {
		// CONDA_EXE points to the conda executable, go up to get root
		condaDir := filepath.Dir(condaExe)
		if strings.HasSuffix(strings.ToLower(condaDir), "scripts") ||
			strings.HasSuffix(strings.ToLower(condaDir), "library\\bin") {
			paths = append(paths, filepath.Dir(condaDir))
		} else {
			paths = append(paths, condaDir)
		}
	}
	// User home directory paths
	paths = append(paths,
		filepath.Join(homeDir, "anaconda3"),
		filepath.Join(homeDir, "miniconda3"),
		filepath.Join(homeDir, "Anaconda3"),
		filepath.Join(homeDir, "Miniconda3"),
	)
	// macOS common paths
	if goruntime.GOOS == "darwin" {
		paths = append(paths,
			"/opt/anaconda3",
			"/opt/miniconda3",
			"/usr/local/anaconda3",
			"/usr/local/miniconda3",
			"/opt/homebrew/anaconda3",
			"/opt/homebrew/miniconda3",
			"/opt/homebrew/Caskroom/miniconda/base",
			"/opt/homebrew/Caskroom/anaconda/base",
			"/usr/local/Caskroom/miniconda/base",
			"/usr/local/Caskroom/anaconda/base",
		)
	}
	// AppData Local paths (Windows common location)
	appDataLocal := os.Getenv("LOCALAPPDATA")
	if appDataLocal != "" {
		paths = append(paths,
			filepath.Join(appDataLocal, "anaconda3"),
			filepath.Join(appDataLocal, "miniconda3"),
			filepath.Join(appDataLocal, "Continuum", "anaconda3"),
			filepath.Join(appDataLocal, "Continuum", "miniconda3"),
		)
	}
	// ProgramData paths (all users installation)
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = "C:\\ProgramData"
	}
	paths = append(paths,
		filepath.Join(programData, "anaconda3"),
		filepath.Join(programData, "miniconda3"),
		filepath.Join(programData, "Anaconda3"),
		filepath.Join(programData, "Miniconda3"),
	)
	// Common drive root installations
	for _, drive := range []string{"C:", "D:", "E:"} {
		root := drive + string(filepath.Separator)
		paths = append(paths,
			filepath.Join(root, "anaconda3"),
			filepath.Join(root, "miniconda3"),
			filepath.Join(root, "Anaconda3"),
			filepath.Join(root, "Miniconda3"),
			filepath.Join(root, "ProgramData", "anaconda3"),
			filepath.Join(root, "ProgramData", "miniconda3"),
		)
	}
	return paths
}

// getCondaRoot finds the conda installation root directory
func (a *App) getCondaRoot() string {
	// First try to get from conda command location
	condaCmd := a.findCondaCommand()
	if condaCmd != "" {
		// If conda is in PATH or found directly, parse its path
		// Conda executable is usually in [root]/Scripts/conda.exe or [root]/bin/conda
		condaDir := filepath.Dir(condaCmd)
		// Check if we're in Scripts or bin directory
		if strings.HasSuffix(strings.ToLower(condaDir), "scripts") ||
			strings.HasSuffix(strings.ToLower(condaDir), "bin") {
			// Go up one level to get the root
			return filepath.Dir(condaDir)
		}
		// Otherwise, condaDir itself might be the root
		return condaDir
	}
	// If not found, try common installation paths
	commonPaths := a.getCommonCondaPaths()
	for _, path := range commonPaths {
		// Check if activate.bat exists (Windows) or activate exists (Unix)
		activateScript := filepath.Join(path, "Scripts", "activate.bat")
		if _, err := os.Stat(activateScript); err == nil {
			return path
		}
		activateScript = filepath.Join(path, "bin", "activate")
		if _, err := os.Stat(activateScript); err == nil {
			return path
		}
	}
	return ""
}

type SystemInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	OSVersion string `json:"os_version"`
}

func (a *App) GetSystemInfo() SystemInfo {
	return SystemInfo{
		OS:        goruntime.GOOS,
		Arch:      goruntime.GOARCH,
		OSVersion: a.getOSVersion(),
	}
}
func (a *App) getOSVersion() string {
	switch goruntime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	case "windows":
		// Use platform-specific function to hide window
		if ver := getWindowsVersionHidden(); ver != "" {
			return ver
		}
	case "linux":
		// Try /etc/os-release
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					return strings.Trim(line[12:], "\"")
				}
			}
		}
	}
	return "Unknown"
}
func (a *App) PackLog(logContent string) (string, error) {
	// Create a temp file for the zip
	timestamp := time.Now().Format("20060102_150405")
	fileName := fmt.Sprintf("maclaw_log_%s.zip", timestamp)
	tempDir := os.TempDir()
	zipPath := filepath.Join(tempDir, fileName)
	// Create the zip file
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to create zip file: %w", err)
	}
	defer zipFile.Close()
	// Initialize zip writer
	archive := zip.NewWriter(zipFile)
	defer archive.Close()
	// Create file inside zip
	f, err := archive.Create("install_log.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create file in zip: %w", err)
	}
	// Write content
	_, err = f.Write([]byte(logContent))
	if err != nil {
		return "", fmt.Errorf("failed to write content to zip: %w", err)
	}
	return zipPath, nil
}
func (a *App) ShowItemInFolder(path string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		path = filepath.FromSlash(path)
		cmd = exec.Command("explorer", "/select,", path)
	case "linux":
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	default:
		return fmt.Errorf("unsupported platform")
	}
	// Use Start instead of Run to avoid waiting for the process and ignoring exit codes (like 1 on Windows)
	return cmd.Start()
}
func (a *App) GetSkillsDir(toolName string) string {
	home, _ := os.UserHomeDir()
	baseDir := filepath.Join(home, ".cceasy", "skills")
	storageDir := filepath.Join(baseDir, "storage")

	// Migration: If storage doesn't exist but claude does, rename claude to storage
	// This ensures existing skills are preserved and shared
	if _, err := os.Stat(storageDir); os.IsNotExist(err) {
		oldDir := filepath.Join(baseDir, "claude")
		if _, err := os.Stat(oldDir); err == nil {
			os.Rename(oldDir, storageDir)
		}
	}

	return storageDir
}
func (a *App) SelectSkillFile() string {
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Skill Zip File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Zip Files", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return ""
	}
	return selection
}

// getInstalledSkillDirs returns a list of installed skill directory names for both user and project locations
func (a *App) getInstalledSkillDirs(toolName string, location string, projectPath string) []string {
	var installedDirs []string
	configDirName := getToolConfigDirName(toolName)

	var skillsDir string
	if location == "user" {
		home, err := os.UserHomeDir()
		if err != nil {
			return installedDirs
		}
		skillsDir = filepath.Join(home, configDirName, "skills")
	} else if location == "project" {
		if projectPath == "" {
			return installedDirs
		}
		skillsDir = filepath.Join(projectPath, configDirName, "skills")
	} else {
		return installedDirs
	}

	// Check if skills directory exists
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		return installedDirs
	}

	// Read all entries in the skills directory
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return installedDirs
	}

	// Collect directory names
	for _, entry := range entries {
		if entry.IsDir() {
			installedDirs = append(installedDirs, entry.Name())
		}
	}

	return installedDirs
}

func (a *App) ListSkills(toolName string) []Skill {
	skillsDir := a.GetSkillsDir(toolName)
	metadataPath := filepath.Join(skillsDir, "metadata.json")

	var defaultSkills []Skill
	// Add default skills for all tools
	defaultSkills = append(defaultSkills, Skill{
		Name:        "Claude Official Documentation Skill Package",
		Description: "Claude Official Documentation Skill Package",
		Type:        "address",
		Value:       "document-skills@anthropic-agent-skills",
	})
	defaultSkills = append(defaultSkills, Skill{
		Name:        "超能力技能包",
		Description: "包含各种方便技能，包括头脑风暴等。",
		Type:        "address",
		Value:       "superpowers@superpowers-marketplace",
	})

	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return defaultSkills
	}
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return defaultSkills
	}
	var skills []Skill
	json.Unmarshal(data, &skills)

	// Filter out duplicates of default skills if they exist in JSON
	// AND filter out 'address' type skills for gemini/codex
	isGeminiOrCodex := strings.ToLower(toolName) == "gemini" || strings.ToLower(toolName) == "codex"

	filteredSkills := defaultSkills
	for _, s := range skills {
		if isGeminiOrCodex && s.Type == "address" {
			continue
		}

		isDefault := false
		for _, ds := range defaultSkills {
			if s.Name == ds.Name {
				isDefault = true
				break
			}
		}
		if !isDefault {
			filteredSkills = append(filteredSkills, s)
		}
	}
	return filteredSkills
}

// ListSkillsWithInstallStatus returns skills list with installed status marked
func (a *App) ListSkillsWithInstallStatus(toolName string, location string, projectPath string) []Skill {
	// Get all available skills
	allSkills := a.ListSkills(toolName)

	// Get installed skill directories
	installedDirs := a.getInstalledSkillDirs(toolName, location, projectPath)
	installedMap := make(map[string]bool)
	for _, dir := range installedDirs {
		installedMap[dir] = true
	}

	// Also check enabledPlugins in ~/.claude/settings.json for address-type skills
	enabledPlugins := make(map[string]bool)
	home, _ := os.UserHomeDir()
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	if data, err := os.ReadFile(settingsFile); err == nil {
		var settings map[string]interface{}
		if err := json.Unmarshal(data, &settings); err == nil {
			if plugins, ok := settings["enabledPlugins"].(map[string]interface{}); ok {
				for k, v := range plugins {
					if enabled, ok := v.(bool); ok && enabled {
						enabledPlugins[k] = true
					}
				}
			}
		}
	}

	// Mark skills as installed based on their type
	for i := range allSkills {
		skill := &allSkills[i]

		if skill.Type == "zip" {
			// For zip skills, extract the skill directory name from the zip filename
			// The zip file should extract to a directory with the same base name
			zipName := filepath.Base(skill.Value)
			// Remove .zip extension
			dirName := strings.TrimSuffix(zipName, ".zip")
			dirName = strings.TrimSuffix(dirName, ".rar")

			// Check if this directory exists in installed dirs
			skill.Installed = installedMap[dirName]
		} else if skill.Type == "address" {
			// For address skills, check enabledPlugins in settings.json
			skill.Installed = enabledPlugins[skill.Value]
			// Fallback: also check skill directories
			if !skill.Installed {
				parts := strings.Split(skill.Value, "@")
				if len(parts) > 0 {
					dirName := parts[0]
					skill.Installed = installedMap[dirName]
				}
			}
		}
	}

	return allSkills
}

func (a *App) validateSkillZip(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("invalid zip file: %v", err)
	}
	defer r.Close()
	rootDirs := make(map[string]bool)
	hasSkillMd := make(map[string]bool)
	for _, f := range r.File {
		// Normalize separators
		name := strings.ToValidUTF8(f.Name, "") // Ensure valid UTF8
		name = filepath.ToSlash(name)
		parts := strings.Split(name, "/")
		// Ignore Mac/System junk
		if len(parts) > 0 && (strings.HasPrefix(parts[0], "__MACOSX") || strings.HasPrefix(parts[0], ".")) {
			continue
		}
		if len(parts) == 1 {
			if f.FileInfo().IsDir() {
				rootDirs[parts[0]] = true
			} else {
				return fmt.Errorf("skill package root must only contain directories. Found file: %s", parts[0])
			}
		} else {
			// It's inside a directory
			rootDirs[parts[0]] = true
			if len(parts) == 2 && strings.EqualFold(parts[1], "SKILL.md") {
				hasSkillMd[parts[0]] = true
			}
		}
	}
	if len(rootDirs) == 0 {
		return fmt.Errorf("skill package is empty or contains no valid directories")
	}
	for dir := range rootDirs {
		if !hasSkillMd[dir] {
			return fmt.Errorf("directory '%s' is missing SKILL.md", dir)
		}
	}
	return nil
}
func getToolConfigDirName(tool string) string {
	switch strings.ToLower(tool) {
	case "claude":
		return ".claude"
	case "gemini":
		return ".gemini"
	case "codex":
		return ".codex"
	case "opencode":
		return ".opencode"
	case "codebuddy":
		return ".codebuddy"
	case "qoder":
		return ".qoder"
	case "iflow":
		return ".iflow"
	case "kilo", "kilocode":
		return ".kilocode"
	default:
		return "." + strings.ToLower(tool)
	}
}
func (a *App) AddSkill(name, description, skillType, value, toolName string) error {
	// Prevent address skills for gemini/codex
	if (strings.ToLower(toolName) == "gemini" || strings.ToLower(toolName) == "codex") && skillType == "address" {
		return fmt.Errorf("gemini and codex only support zip package skills")
	}
	// Validate zip if applicable
	if skillType == "zip" && strings.Contains(value, string(os.PathSeparator)) {
		if err := a.validateSkillZip(value); err != nil {
			return err
		}
	}
	skillsDir := a.GetSkillsDir(toolName)
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return err
	}
	metadataPath := filepath.Join(skillsDir, "metadata.json")
	// Load existing
	var skills []Skill
	if data, err := os.ReadFile(metadataPath); err == nil {
		json.Unmarshal(data, &skills)
	}
	// Check for duplicate name - update if exists
	found := false
	for i, s := range skills {
		if s.Name == name {
			finalValue := value
			if skillType == "zip" {
				// If value is a path (contains separator)", assume it's a new file to copy
				if strings.Contains(value, string(os.PathSeparator)) {
					srcFile, err := os.Open(value)
					if err != nil {
						return err
					}
					defer srcFile.Close()
					fileName := filepath.Base(value)
					destPath := filepath.Join(skillsDir, fileName)
					destFile, err := os.Create(destPath)
					if err != nil {
						return err
					}
					defer destFile.Close()
					_, err = io.Copy(destFile, srcFile)
					if err != nil {
						return err
					}
					finalValue = fileName
				}
			}
			skills[i] = Skill{
				Name:        name,
				Description: description,
				Type:        skillType,
				Value:       finalValue,
			}
			found = true
			break
		}
	}
	if !found {
		finalValue := value
		if skillType == "zip" {
			// Copy file
			srcFile, err := os.Open(value)
			if err != nil {
				return err
			}
			defer srcFile.Close()
			fileName := filepath.Base(value)
			destPath := filepath.Join(skillsDir, fileName)
			destFile, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer destFile.Close()
			_, err = io.Copy(destFile, srcFile)
			if err != nil {
				return err
			}
			finalValue = fileName
		}
		newSkill := Skill{
			Name:        name,
			Description: description,
			Type:        skillType,
			Value:       finalValue,
		}
		skills = append(skills, newSkill)
	}
	// Save
	data, err := json.MarshalIndent(skills, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0644)
}
func (a *App) InstallDefaultMarketplace() error {
	home, _ := os.UserHomeDir()
	settingsFile := filepath.Join(home, ".claude", "settings.json")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %v", err)
	}

	// Read existing settings
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsFile); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			settings = make(map[string]interface{})
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Ensure extraKnownMarketplaces exists
	marketplaces, ok := settings["extraKnownMarketplaces"].(map[string]interface{})
	if !ok {
		marketplaces = make(map[string]interface{})
	}

	changed := false

	// Add anthropic-agent-skills marketplace (anthropics/skills repo)
	if _, exists := marketplaces["anthropic-agent-skills"]; !exists {
		marketplaces["anthropic-agent-skills"] = map[string]interface{}{
			"source": map[string]interface{}{
				"source": "github",
				"repo":   "anthropics/skills",
			},
		}
		changed = true
	}

	// Add superpowers-marketplace (obra/superpowers-marketplace repo)
	if _, exists := marketplaces["superpowers-marketplace"]; !exists {
		marketplaces["superpowers-marketplace"] = map[string]interface{}{
			"source": map[string]interface{}{
				"source": "github",
				"repo":   "obra/superpowers-marketplace",
			},
		}
		changed = true
	}

	if changed {
		settings["extraKnownMarketplaces"] = marketplaces
		data, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal settings: %v", err)
		}
		if err := os.WriteFile(settingsFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write settings: %v", err)
		}
		a.log("Default marketplaces added to ~/.claude/settings.json")
	}

	return nil
}
func (a *App) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	// 1. Identify root directories to clean up
	rootDirs := make(map[string]bool)
	for _, f := range r.File {
		path := filepath.ToSlash(f.Name)
		parts := strings.Split(path, "/")
		if len(parts) > 0 {
			rootDir := parts[0]
			if !strings.HasPrefix(rootDir, "__MACOSX") && !strings.HasPrefix(rootDir, ".") {
				rootDirs[rootDir] = true
			}
		}
	}
	// 2. Remove existing directories
	for dir := range rootDirs {
		destPath := filepath.Join(dest, dir)
		if err := os.RemoveAll(destPath); err != nil {
			return fmt.Errorf("failed to remove existing skill directory %s: %v", destPath, err)
		}
	}
	os.MkdirAll(dest, 0755)
	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func (a *App) InstallSkill(name, description, skillType, value, location, projectPath, toolName string) error {
	// 1. Validate
	if location == "project" && skillType == "address" {
		return fmt.Errorf("project installation only supports zip/rar files")
	}
	// For zip validation, we need to know if value is a path or filename
	var fullPath string
	if skillType == "zip" {
		if strings.Contains(value, string(os.PathSeparator)) {
			fullPath = value
		} else {
			fullPath = filepath.Join(a.GetSkillsDir(toolName), value)
		}
		if err := a.validateSkillZip(fullPath); err != nil {
			return err
		}
	}
	configDirName := getToolConfigDirName(toolName)
	// 2. Install to Tool
	if location == "user" {
		if skillType == "address" {
			// Skill ID installation
			if strings.ToLower(toolName) != "claude" {
				return fmt.Errorf("skill ID installation is currently only supported for Claude")
			}
			// Ensure default marketplaces are registered
			if err := a.InstallDefaultMarketplace(); err != nil {
				a.log(fmt.Sprintf("Warning: failed to ensure marketplaces: %v", err))
			}
			// Enable plugin in ~/.claude/settings.json
			home, _ := os.UserHomeDir()
			settingsFile := filepath.Join(home, ".claude", "settings.json")
			var settings map[string]interface{}
			if data, err := os.ReadFile(settingsFile); err == nil {
				if err := json.Unmarshal(data, &settings); err != nil {
					settings = make(map[string]interface{})
				}
			} else {
				settings = make(map[string]interface{})
			}
			enabledPlugins, ok := settings["enabledPlugins"].(map[string]interface{})
			if !ok {
				enabledPlugins = make(map[string]interface{})
			}
			enabledPlugins[value] = true
			settings["enabledPlugins"] = enabledPlugins
			data, err := json.MarshalIndent(settings, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal settings: %v", err)
			}
			if err := os.WriteFile(settingsFile, data, 0644); err != nil {
				return fmt.Errorf("failed to write settings: %v", err)
			}
			a.log(fmt.Sprintf("Plugin %s enabled in settings.json", value))
		} else {
			// Unzip to ~/.<tool>/skills
			home, _ := os.UserHomeDir()
			destDir := filepath.Join(home, configDirName, "skills")
			if err := a.unzip(fullPath, destDir); err != nil {
				return fmt.Errorf("unzip failed: %v", err)
			}
		}
	} else if location == "project" {
		if projectPath == "" {
			return fmt.Errorf("project path required")
		}
		destDir := filepath.Join(projectPath, configDirName, "skills")
		if err := a.unzip(fullPath, destDir); err != nil {
			return fmt.Errorf("unzip failed: %v", err)
		}
	}
	// 3. Add to App List
	return a.AddSkill(name, description, skillType, value, toolName)
}
func (a *App) DeleteSkill(name, toolName string) error {
	// Prevent deletion of the hardcoded skill
	if name == "Claude Official Documentation Skill Package" {
		return fmt.Errorf("cannot delete system skill package")
	}
	skillsDir := a.GetSkillsDir(toolName)
	metadataPath := filepath.Join(skillsDir, "metadata.json")
	var skills []Skill
	if data, err := os.ReadFile(metadataPath); err == nil {
		json.Unmarshal(data, &skills)
	}
	var newSkills []Skill
	for _, s := range skills {
		if s.Name == name {
			if s.Type == "zip" {
				os.Remove(filepath.Join(skillsDir, s.Value))
			}
		} else {
			newSkills = append(newSkills, s)
		}
	}
	data, err := json.MarshalIndent(newSkills, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath, data, 0644)
}

// Translation logic
var translations = map[string]map[string]string{
	"Checking Node.js installation...": {
		"zh-Hans": "正在检查 Node.js 安装...",
		"zh-Hant": "正在檢查 Node.js 安裝...",
	},
	"Initializing...": {
		"zh-Hans": "初始化中...",
		"zh-Hant": "初始化中...",
	},
	"Skipping environment check and installation.": {
		"zh-Hans": "跳过环境检测安装。",
		"zh-Hant": "跳過環境檢測安裝。",
	},
	"Manual environment check triggered.": {
		"zh-Hans": "手动触发环境检测。",
		"zh-Hant": "手動觸發環境檢測。",
	},
	"Detected missing .cceasy directory. Forcing environment check...": {
		"zh-Hans": "检测到缺失 .cceasy 目录。正在强制进行环境检...",
		"zh-Hant": "檢測到缺�?.cceasy 目錄。正在強制進行環境檢測...",
	},
	"Init mode: Forcing environment check (ignoring configuration).": {
		"zh-Hans": "初始化模式：正在强制进行环境检测（忽略配置）。",
		"zh-Hant": "初始化模式：正在強制進行環境檢測（忽略配置）。",
	},
	"Forced environment check triggered (ignoring configuration).": {
		"zh-Hans": "已触发强制环境检测（忽略配置）。",
		"zh-Hant": "已觸發強制環境檢測（忽略配置）。",
	},
	"Forced environment check triggered.": {
		"zh-Hans": "已触发强制环境检测。",
		"zh-Hant": "已觸發強制環境檢測。",
	},
	"Node.js not found. Downloading and installing...": {
		"zh-Hans": "未找到 Node.js。正在下载并安装...",
		"zh-Hant": "未找到 Node.js。正在下載並安裝...",
	},
	"Node.js not found. Attempting manual installation...": {
		"zh-Hans": "未找到 Node.js。尝试手动安...",
		"zh-Hant": "未找到 Node.js。嘗試手動安...",
	},
	"Node.js installed successfully.": {
		"zh-Hans": "Node.js 安装成功。",
		"zh-Hant": "Node.js 安裝成功。",
	},
	"Node.js is installed.": {
		"zh-Hans": "Node.js 已安装。",
		"zh-Hant": "Node.js 已安裝。",
	},
	"Node.js is already installed.": {
		"zh-Hans": "Node.js 已经安装。",
		"zh-Hant": "Node.js 已經安裝。",
	},
	"Node.js installation already in progress, waiting for completion...": {
		"zh-Hans": "Node.js 正在安装中，等待完成...",
		"zh-Hant": "Node.js 正在安裝中，等待完成...",
	},
	"Node.js installation completed by another process.": {
		"zh-Hans": "Node.js 安装已由另一个进程完成。",
		"zh-Hant": "Node.js 安裝已由另一個進程完成。",
	},
	"ERROR: Timeout waiting for Node.js installation to complete.": {
		"zh-Hans": "错误：等待 Node.js 安装完成超时。",
		"zh-Hant": "錯誤：等待 Node.js 安裝完成超時。",
	},
	"ERROR: Node.js is not available. Cannot proceed with AI tools installation.": {
		"zh-Hans": "错误：Node.js 不可用。无法继续安装 AI 工具。",
		"zh-Hant": "錯誤：Node.js 不可用。無法繼續安裝 AI 工具。",
	},
	"Retrying npm verification (attempt %d/%d)...": {
		"zh-Hans": "重试 npm 验证（第 %d/%d 次尝试）...",
		"zh-Hant": "重試 npm 驗證（第 %d/%d 次嘗試）...",
	},
	"npm not found in PATH, updating environment...": {
		"zh-Hans": "在 PATH 中未找到 npm，正在更新环境...",
		"zh-Hant": "在 PATH 中未找到 npm，正在更新環境...",
	},
	"npm command test failed: %v": {
		"zh-Hans": "npm 命令测试失败：%v",
		"zh-Hant": "npm 命令測試失敗：%v",
	},
	"ERROR: npm not found after %d attempts. Cannot install AI tools. Please ensure Node.js is properly installed.": {
		"zh-Hans": "错误：经过 %d 次尝试后仍未找到 npm。无法安装 AI 工具。请确保 Node.js 已正确安装。",
		"zh-Hant": "錯誤：經過 %d 次嘗試後仍未找到 npm。無法安裝 AI 工具。請確保 Node.js 已正確安裝。",
	},
	"Checking Git installation...": {
		"zh-Hans": "正在检查 Git 安装...",
		"zh-Hant": "正在檢查 Git 安裝...",
	},
	"Git found in standard location.": {
		"zh-Hans": "在标准位置找到 Git",
		"zh-Hant": "在標準位置找到 Git",
	},
	"Git not found. Downloading and installing...": {
		"zh-Hans": "未找到 Git。正在下载并安装...",
		"zh-Hant": "未找到?Git。正在下載並安裝...",
	},
	"Git installed successfully.": {
		"zh-Hans": "Git 安装成功",
		"zh-Hant": "Git 安裝成功",
	},
	"Git is installed.": {
		"zh-Hans": "Git 已安装",
		"zh-Hant": "Git 已安裝",
	},
	"Environment check complete.": {
		"zh-Hans": "环境检查完成",
		"zh-Hant": "環境檢查完成",
	},
	"npm not found.": {
		"zh-Hans": "未找到 npm",
		"zh-Hant": "未找到 npm",
	},
	// Templates
	"Checking %s...": {
		"zh-Hans": "正在检查 %s...",
		"zh-Hant": "正在檢查 %s...",
	},
	"%s not found. Attempting automatic installation...": {
		"zh-Hans": "未找到 %s。尝试自动安装...",
		"zh-Hant": "未找到?%s。嘗試自動安...",
	},
	"ERROR: Failed to install %s: %v": {
		"zh-Hans": "错误：安装 %s 失败: %v",
		"zh-Hant": "錯誤：安裝 %s 失敗: %v",
	},
	"%s installed successfully.": {
		"zh-Hans": "%s 安装成功",
		"zh-Hant": "%s 安裝成功",
	},
	"%s found at %s (version: %s).": {
		"zh-Hans": "发现 %s 位于 %s (版本: %s)",
		"zh-Hant": "發現 %s 位於 %s (版本: %s)",
	},
	"Checking for %s updates...": {
		"zh-Hans": "正在检查 %s 更新...",
		"zh-Hant": "正在檢查 %s 更新...",
	},
	"New version available for %s: %s (current: %s). Updating...": {
		"zh-Hans": "%s 有新版本可用: %s (当前: %s)。正在更...",
		"zh-Hant": "%s 有新版本可用: %s (當前: %s)。正在更...",
	},
	"ERROR: Failed to update %s: %v": {
		"zh-Hans": "错误：更新 %s 失败: %v",
		"zh-Hant": "錯誤：更新 %s 失敗: %v",
	},
	"%s updated successfully to %s.": {
		"zh-Hans": "%s 成功更新到 %s",
		"zh-Hant": "%s 成功更新到 %s",
	},
	"CheckUpdate: Starting check against %s": {
		"zh-Hans": "检查更新：正在从 %s 检查...",
		"zh-Hant": "檢查更新：正在從 %s 檢查...",
	},
	"CheckUpdate: Failed to create request: %v": {
		"zh-Hans": "检查更新：创建请求失败: %v",
		"zh-Hant": "檢查更新：建立請求失敗: %v",
	},
	"CheckUpdate: Failed to decode token at iteration %d: %v": {
		"zh-Hans": "检查更新：第 %d 次迭代解码令牌失败: %v",
		"zh-Hant": "檢查更新：第 %d 次迭代解碼令牌失敗: %v",
	},
	"CheckUpdate: HTTP Status: %d": {
		"zh-Hans": "检查更新：HTTP 状态码: %d",
		"zh-Hant": "檢查更新：HTTP 狀態碼: %d",
	},
	"CheckUpdate: Rate Limit: %s/%s, Reset: %s": {
		"zh-Hans": "检查更新：速率限制: %s/%s, 重置时间: %s",
		"zh-Hant": "檢查更新：速率限制: %s/%s, 重置時間: %s",
	},
	"CheckUpdate: Response: %s": {
		"zh-Hans": "检查更新：响应内容: %s",
		"zh-Hant": "檢查更新：響應內容: %s",
	},
	"CheckUpdate: Failed to read response body: %v": {
		"zh-Hans": "检查更新：读取响应体失败: %v",
		"zh-Hant": "檢查更新：讀取響應體失敗: %v",
	},
	"CheckUpdate: Raw response length: %d bytes": {
		"zh-Hans": "检查更新：原始响应长度: %d 字节",
		"zh-Hant": "檢查更新：原始響應長�? %d 位元",
	},
	"CheckUpdate: Response body: %s": {
		"zh-Hans": "检查更新：响应体: %s",
		"zh-Hant": "檢查更新：響應體: %s",
	},
	"CheckUpdate: Parsed keys: %v": {
		"zh-Hans": "检查更新：解析出的键: %v",
		"zh-Hant": "檢查更新：解析出的鍵: %v",
	},
	"CheckUpdate: Found version in 'tag_name' field: %s": {
		"zh-Hans": "检查更新：在 'tag_name' 字段中找到版本 %s",
		"zh-Hant": "檢查更新：在 'tag_name' 欄位中找到版�? %s",
	},
	"CheckUpdate: Found version in 'name' field: %s": {
		"zh-Hans": "检查更新：在 'name' 字段中找到版本 %s",
		"zh-Hant": "檢查更新：在 'name' 欄位中找到版�? %s",
	},
	"CheckUpdate: Neither 'name' nor 'tag_name' found. Available: %v": {
		"zh-Hans": "检查更新：未找到 'name' 或 'tag_name'。可用字段: %v",
		"zh-Hant": "檢查更新：未找到 'name' �?'tag_name'。可用欄�? %v",
	},
	"CheckUpdate: Using version: %s": {
		"zh-Hans": "检查更新：使用版本: %s",
		"zh-Hant": "檢查更新：使用版�? %s",
	},
	"CheckUpdate: Using built-in GitHub token for authentication": {
		"zh-Hans": "检查更新：使用内置 GitHub 令牌进行身份验证",
		"zh-Hant": "檢查更新：使用內�?GitHub 令牌進行身份驗證",
	},
	"CheckUpdate: Using custom GitHub token from environment variable": {
		"zh-Hans": "检查更新：使用环境变量中的自定义 GitHub 令牌",
		"zh-Hant": "檢查更新：使用環境變數中的自定義 GitHub 令牌",
	},
	"CheckUpdate: Already on latest version": {
		"zh-Hans": "检查更新：已是最新版",
		"zh-Hant": "檢查更新：已是最新版",
	},
	"CheckUpdate: Latest version: %s, Current version: %s, Display version: %s": {
		"zh-Hans": "检查更新：最新版本 %s, 当前版本: %s, 显示版本: %s",
		"zh-Hant": "檢查更新：最新版�? %s, 當前版本: %s, 顯示版本: %s",
	},
	"CheckUpdate: Update available! %s > %s": {
		"zh-Hans": "检查更新：发现新版本！ %s > %s",
		"zh-Hant": "檢查更新：發現新版本�?%s > %s",
	},
	"CheckUpdate: Failed to fetch GitHub API: %v": {
		"zh-Hans": "检查更新：获取 GitHub API 失败: %v",
		"zh-Hant": "檢查更新：獲�?GitHub API 失敗: %v",
	},
	"CheckUpdate: Rate limit exceeded, resets at: %s": {
		"zh-Hans": "检查更新：超出速率限制，重置时间 %s",
		"zh-Hant": "檢查更新：超出速率限制，重置時�? %s",
	},
	"CheckUpdate: Failed to parse JSON: %v": {
		"zh-Hans": "检查更新：解析 JSON 失败: %v",
		"zh-Hant": "檢查更新：解�?JSON 失敗: %v",
	},
	"CheckUpdate: GitHub API returned status %d": {
		"zh-Hans": "检查更新：GitHub API 返回状态 %d",
		"zh-Hant": "檢查更新：GitHub API 返回狀�?%d",
	},
	"Config file modified: ": {
		"zh-Hans": "配置文件已修改：",
		"zh-Hant": "配置文件已修改：",
	},
	"Updated PATH environment variable: ": {
		"zh-Hans": "已更新 PATH 环境变量",
		"zh-Hant": "已更�?PATH 環境變數",
	},
	"Updated PATH environment variable for Git.": {
		"zh-Hans": "已为 Git 更新 PATH 环境变量",
		"zh-Hant": "已為 Git 更新 PATH 環境變數",
	},
	"Installing Node.js (this may take a moment, please grant administrator permission if prompted)...": {
		"zh-Hans": "正在安装 Node.js (这可能需要一些时间，如果提示请授予管理员权限)...",
		"zh-Hant": "正在安裝 Node.js (這可能需要一些時間，如果提示請授予管理員權限)...",
	},
	"Installing Git (this may take a moment, please grant administrator permission if prompted)...": {
		"zh-Hans": "正在安装 Git (这可能需要一些时间，如果提示请授予管理员权限)...",
		"zh-Hant": "正在安裝 Git (這可能需要一些時間，如果提示請授予管理員權限)...",
	},
	"Downloading Node.js %s for %s...": {
		"zh-Hans": "正在下载 Node.js %s (%s)...",
		"zh-Hant": "正在下載 Node.js %s (%s)...",
	},
	"Downloading Node.js v%s from %s...": {
		"zh-Hans": "正在从 %s 下载 Node.js v%s...",
		"zh-Hant": "正在�?%s 下載 Node.js v%s...",
	},
	"Downloading Git %s...": {
		"zh-Hans": "正在下载 Git %s...",
		"zh-Hant": "正在下載 Git %s...",
	},
	"Downloading (%.1f%%): %d/%d bytes": {
		"zh-Hans": "正在下载 (%.1f%%): %d/%d 字节",
		"zh-Hant": "正在下載 (%.1f%%): %d/%d 字節",
	},
	"Node.js installer is not accessible (Status: %s). Please check your internet connection or mirror availability.": {
		"zh-Hans": "无法访问 Node.js 安装程序 (状态 %s)。请检查您的网络连接或镜像可用性",
		"zh-Hant": "無法訪問 Node.js 安裝程序 (狀�? %s)。請檢查您的網絡連接或鏡像可用性",
	},
	"Failed to install Node.js: ": {
		"zh-Hans": "安装 Node.js 失败: ",
		"zh-Hant": "安裝 Node.js 失敗: ",
	},
	"Node.js not found. Checking for Homebrew...": {
		"zh-Hans": "未找到 Node.js。正在检查 Homebrew...",
		"zh-Hant": "未找到 Node.js。正在檢查?Homebrew...",
	},
	"Installing Node.js via Homebrew...": {
		"zh-Hans": "正在通过 Homebrew 安装 Node.js...",
		"zh-Hant": "正在通過 Homebrew 安裝 Node.js...",
	},
	"Homebrew installation failed.": {
		"zh-Hans": "Homebrew 安装失败",
		"zh-Hant": "Homebrew 安裝失敗",
	},
	"Node.js installed via Homebrew.": {
		"zh-Hans": "Node.js 已通过 Homebrew 安装",
		"zh-Hant": "Node.js 已通過 Homebrew 安裝",
	},
	"Homebrew not found. Attempting manual installation...": {
		"zh-Hans": "未找到 Homebrew。尝试手动安装...",
		"zh-Hant": "未找到?Homebrew。嘗試手動安...",
	},
	"Manual installation failed: ": {
		"zh-Hans": "手动安装失败: ",
		"zh-Hant": "手動安裝失敗: ",
	},
	"Downloading Node.js from %s": {
		"zh-Hans": "正在从 %s 下载 Node.js",
		"zh-Hant": "正在�?%s 下載 Node.js",
	},
	"Extracting Node.js (this should be fast)...": {
		"zh-Hans": "正在解压 Node.js (这应该很....",
		"zh-Hant": "正在解壓 Node.js (這應該很....",
	},
	"Extracting Node.js...": {
		"zh-Hans": "正在解压 Node.js...",
		"zh-Hant": "正在解壓 Node.js...",
	},
	"Node.js manually installed to ": {
		"zh-Hans": "Node.js 已手动安装到 ",
		"zh-Hant": "Node.js 已手動安裝到 ",
	},
	"Verifying Node.js installation...": {
		"zh-Hans": "正在验证 Node.js 安装...",
		"zh-Hant": "正在驗證 Node.js 安裝...",
	},
	"Node.js installation completed but binary not found.": {
		"zh-Hans": "Node.js 安装完成但未找到二进制文件",
		"zh-Hant": "Node.js 安裝完成但未找到二進制文件",
	},
	"Node.js found at: ": {
		"zh-Hans": "Node.js 位于: ",
		"zh-Hant": "Node.js 位於: ",
	},
	"Updated PATH: ": {
		"zh-Hans": "已更新 PATH: ",
		"zh-Hant": "已更新 PATH: ",
	},
	"Running installation: %s %s": {
		"zh-Hans": "正在运行安装: %s %s",
		"zh-Hant": "正在運行安裝: %s %s",
	},
	"Detected npm cache permission issue. Attempting to clear cache...": {
		"zh-Hans": "检测到 npm 缓存权限问题。正在尝试清理缓...",
		"zh-Hant": "檢測�?npm 緩存權限問題。正在嘗試清理緩...",
	},
	"Retrying installation after cache clean...": {
		"zh-Hans": "清理缓存后重试安...",
		"zh-Hant": "清理緩存後重試安...",
	},
	"Running update: %s %s": {
		"zh-Hans": "正在运行更新: %s %s",
		"zh-Hant": "正在運行更新: %s %s",
	},
	"Warning: Failed to create local npm cache dir: %v": {
		"zh-Hans": "警告: 创建本地 npm 缓存目录失败: %v",
		"zh-Hant": "警告: 創建本地 npm 緩存目錄失敗: %v",
	},
	"Found conda environment: %s at %s": {
		"zh-Hans": "发现 conda 环境: %s 位于 %s",
		"zh-Hant": "發現 conda 環境: %s 位於 %s",
	},
	"Using conda command: ": {
		"zh-Hans": "使用 conda 命令: ",
		"zh-Hant": "使用 conda 命令: ",
	},
	"Note: Unable to list conda environments via command (conda may not be fully initialized): ": {
		"zh-Hans": "注意：无法通过命令列出 conda 环境（conda 可能未完全初始化）: ",
		"zh-Hant": "注意：無法通過命令列出 conda 環境（conda 可能未完全初始化）: ",
	},
	"Total conda environments found: %d": {
		"zh-Hans": "共发现 %d 个 conda 环境",
		"zh-Hant": "共發�?%d �?conda 環境",
	},
	"Found conda from CONDA_EXE: ": {
		"zh-Hans": "从 CONDA_EXE 发现 conda: ",
		"zh-Hant": "�?CONDA_EXE 發現 conda: ",
	},
	"Found conda in PATH: ": {
		"zh-Hans": "从 PATH 中发现 conda: ",
		"zh-Hant": "�?PATH 中發�?conda: ",
	},
	"Searching for conda in %d common paths...": {
		"zh-Hans": "正在 %d 个常用路径中搜索 conda...",
		"zh-Hant": "正在 %d 個常用路徑中搜索 conda...",
	},
	"Found conda at: ": {
		"zh-Hans": "发现 conda 位于: ",
		"zh-Hant": "發現 conda 位於: ",
	},
}

func (a *App) tr(key string, args ...interface{}) string {
	lang := strings.ToLower(a.CurrentLanguage)
	if strings.HasPrefix(lang, "zh-hans") || strings.HasPrefix(lang, "zh-cn") {
		lang = "zh-Hans"
	} else if strings.HasPrefix(lang, "zh-hant") || strings.HasPrefix(lang, "zh-tw") || strings.HasPrefix(lang, "zh-hk") {
		lang = "zh-Hant"
	} else {
		lang = "en"
	}
	var format string
	if dict, ok := translations[key]; ok {
		if val, ok := dict[lang]; ok {
			format = val
		}
	}
	if format == "" {
		format = key
	}
	if len(args) > 0 {
		return fmt.Sprintf(format, args...)
	}
	return format
}
func (a *App) OpenSystemUrl(url string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		a.log("Opening URL on macOS: " + url)
		cmd = exec.Command("open", url)
	case "windows":
		a.log("Opening URL on Windows: " + url)
		// Escape & to ^& for cmd.exe
		escapedUrl := strings.ReplaceAll(url, "&", "^&")
		cmd = exec.Command("cmd", "/c", "start", "", escapedUrl)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
