package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codegenproxy"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var runtimeGOOS = goruntime.GOOS

const (
	defaultListenAddress = "0.0.0.0:18086"
	defaultProxyAPIKey   = "tigerproxy-local-key"
)

type App struct {
	ctx             context.Context
	cancel          context.CancelFunc
	ssoCtx          context.Context
	ssoCancel       context.CancelFunc
	server          *codegenproxy.Server
	listen          string
	lastError       string
	mu              sync.Mutex
	shown           bool
	codexInstalling bool
}

type Settings struct {
	ListenAddress string        `json:"listen_address"`
	APIKey        string        `json:"api_key"`
	AccessToken   string        `json:"access_token,omitempty"`
	BaseURL       string        `json:"base_url"`
	ModelID       string        `json:"model_id,omitempty"`
	Email         string        `json:"email,omitempty"`
	UpdatedAt     string        `json:"updated_at,omitempty"`
	Models        []ModelOption `json:"models,omitempty"`
}

type ModelOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextWindow int    `json:"context_window,omitempty"`
}

type Status struct {
	Settings           Settings `json:"settings"`
	Running            bool     `json:"running"`
	LastError          string   `json:"last_error,omitempty"`
	OpenAIURL          string   `json:"openai_url"`
	AnthropicURL       string   `json:"anthropic_url"`
	HealthURL          string   `json:"health_url"`
	BindAddress        string   `json:"bind_address"`
	LANURLs            []string `json:"lan_urls"`
	LoggedIn           bool     `json:"logged_in"`
	AutoStartSupported bool     `json:"auto_start_supported"`
	AutoStartEnabled   bool     `json:"auto_start_enabled"`
}

type LoginStartResult struct {
	LoginURL    string `json:"login_url"`
	CallbackURL string `json:"callback_url"`
}

func NewApp() *App {
	return &App{shown: true}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startProxyFromDisk()
	// Asynchronously refresh model list from server if logged in.
	// This ensures vendor/ prefix models are properly listed after the
	// normalizeModelID fix (old cached settings may have stripped the prefix).
	go a.refreshModelsIfLoggedIn()
}

// refreshModelsIfLoggedIn fetches the current model list from CodeGen and
// updates settings.json. This is a no-op if the user is not logged in.
// After updating, it emits an event so the frontend can refresh the UI.
func (a *App) refreshModelsIfLoggedIn() {
	s, err := loadSettings()
	if err != nil || strings.TrimSpace(s.AccessToken) == "" {
		return
	}
	models, _, err := oauth.FetchCodeGenModels(s.AccessToken)
	if err != nil || len(models) == 0 {
		return
	}
	newModels := modelOptionsFromOAuth(models)
	// Skip write if the list hasn't actually changed (avoid unnecessary disk I/O and UI flicker)
	if modelsEqual(s.Models, newModels) {
		return
	}
	s.Models = newModels
	if err := writeSettings(s); err != nil {
		return
	}
	// Notify frontend to refresh the model dropdown
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "models-refreshed", nil)
	}
}

func modelsEqual(a, b []ModelOption) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

func (a *App) shutdown(ctx context.Context) {
	_ = ctx
	a.cancelSSOLogin()
	a.stopProxy()
}

func (a *App) LoadSettings() (Settings, error) {
	return loadSettings()
}

func (a *App) SaveSettings(s Settings) (Status, error) {
	cur, _ := loadSettings()
	if strings.TrimSpace(s.ListenAddress) == "" {
		s.ListenAddress = cur.ListenAddress
	}
	if strings.TrimSpace(s.AccessToken) == "" {
		s.AccessToken = cur.AccessToken
	}
	if strings.TrimSpace(s.BaseURL) == "" {
		s.BaseURL = cur.BaseURL
	}
	if strings.TrimSpace(s.ModelID) == "" {
		s.ModelID = cur.ModelID
	}
	if strings.TrimSpace(s.Email) == "" {
		s.Email = cur.Email
	}
	if len(s.Models) == 0 {
		s.Models = cur.Models
	}
	s = normalizeSettings(s)

	// Only restart proxy if settings that affect the running proxy have changed.
	// Model name, email, API key etc. are just persisted metadata or can be hot-updated.
	needsRestart := s.ListenAddress != cur.ListenAddress ||
		s.AccessToken != cur.AccessToken ||
		s.BaseURL != cur.BaseURL

	if needsRestart {
		if err := a.restartProxy(s); err != nil {
			return Status{}, err
		}
	} else if s.APIKey != cur.APIKey {
		// API key can be hot-updated without restart (same as GenerateAPIKey)
		a.mu.Lock()
		server := a.server
		a.mu.Unlock()
		if server != nil {
			server.SetClientAPIKey(s.APIKey)
		}
	}

	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) LoginSSO() (Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), oauth.CodeGenTimeout)
	defer cancel()

	result, err := oauth.RunCodeGenSSOFlowWithCallback(ctx)
	if err != nil {
		return Status{}, err
	}

	s, _ := loadSettings()
	s.AccessToken = result.AccessToken
	s.BaseURL = result.BaseURL
	s.Email = result.Email
	s.Models = modelOptionsFromOAuth(result.Models)
	// Preserve user's model selection if it still exists in the new model list.
	if !modelExistsInList(s.ModelID, s.Models) {
		s.ModelID = normalizeModelID(result.ModelID)
	}
	if strings.TrimSpace(s.APIKey) == "" {
		s.APIKey = defaultProxyAPIKey
	}
	s = normalizeSettings(s)
	if err := a.restartProxy(s); err != nil {
		return Status{}, err
	}
	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) StartSSOLogin() (LoginStartResult, error) {
	a.cancelSSOLogin()
	ctx, cancel := context.WithTimeout(context.Background(), oauth.CodeGenTimeout)
	a.mu.Lock()
	a.ssoCtx = ctx
	a.ssoCancel = cancel
	a.mu.Unlock()

	loginURL, callbackURL, err := oauth.StartCodeGenSSOCallbackServer(ctx)
	if err != nil {
		a.cancelSSOLogin()
		return LoginStartResult{}, err
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, loginURL)
	}
	return LoginStartResult{LoginURL: loginURL, CallbackURL: callbackURL}, nil
}

func (a *App) CompleteSSOLogin() (Status, error) {
	defer a.cancelSSOLogin()
	a.mu.Lock()
	ctx := a.ssoCtx
	a.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := oauth.WaitForCodeGenSSOCallbackContext(ctx, oauth.CodeGenTimeout)
	if err != nil {
		return Status{}, err
	}
	s, _ := loadSettings()
	s.AccessToken = result.AccessToken
	s.BaseURL = result.BaseURL
	s.Email = result.Email
	s.Models = modelOptionsFromOAuth(result.Models)
	// Preserve user's model selection if it still exists in the new model list.
	if !modelExistsInList(s.ModelID, s.Models) {
		s.ModelID = normalizeModelID(result.ModelID)
	}
	s = normalizeSettings(s)
	if err := a.restartProxy(s); err != nil {
		return Status{}, err
	}
	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) cancelSSOLogin() {
	a.mu.Lock()
	cancel := a.ssoCancel
	a.ssoCtx = nil
	a.ssoCancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) Logout() (Status, error) {
	s, _ := loadSettings()
	s.AccessToken = ""
	s.Email = ""
	s.ModelID = ""
	s.Models = nil
	s = normalizeSettings(s)
	if err := a.restartProxy(s); err != nil {
		return Status{}, err
	}
	if err := writeSettings(s); err != nil {
		return Status{}, err
	}
	return a.Status()
}

func (a *App) Status() (Status, error) {
	s, err := loadSettings()
	if err != nil {
		return Status{}, err
	}
	hostURL := publicBaseURL(s.ListenAddress, "127.0.0.1")
	supported := autoStartSupported()
	autoStartEnabled := false
	if supported {
		autoStartEnabled, _ = isAutoStartEnabled()
	}
	return Status{
		Settings:           scrubSettings(s),
		Running:            a.isRunning(),
		LastError:          a.getLastError(),
		OpenAIURL:          hostURL + "/v1",
		AnthropicURL:       hostURL + "/anthropic/v1",
		HealthURL:          hostURL + "/health",
		BindAddress:        s.ListenAddress,
		LANURLs:            lanURLs(s.ListenAddress),
		LoggedIn:           strings.TrimSpace(s.AccessToken) != "",
		AutoStartSupported: supported,
		AutoStartEnabled:   autoStartEnabled,
	}, nil
}

func (a *App) SetAutoStartEnabled(enabled bool) (Status, error) {
	if !autoStartSupported() {
		return Status{}, fmt.Errorf("auto start is only supported on Windows")
	}
	if err := setAutoStartEnabled(enabled); err != nil {
		return Status{}, err
	}
	status, err := a.Status()
	if err != nil {
		return Status{}, err
	}
	actual, err := isAutoStartEnabled()
	if err != nil {
		return Status{}, err
	}
	status.AutoStartEnabled = actual
	return status, nil
}

func (a *App) GenerateAPIKey() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	newKey := "tp-" + hex.EncodeToString(b)

	// Persist to disk first — if this fails, the running proxy is unaffected
	// and the user can retry without ending up in a broken state.
	s, _ := loadSettings()
	s.APIKey = newKey
	if err := writeSettings(s); err != nil {
		return "", err
	}

	// Disk write succeeded — now hot-update the running proxy so the new key
	// takes effect immediately without restart.
	a.mu.Lock()
	server := a.server
	a.mu.Unlock()
	if server != nil {
		server.SetClientAPIKey(newKey)
	}

	return newKey, nil
}

func (a *App) OpenURL(url string) error {
	if a.ctx == nil {
		return nil
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// ConfigureCodex writes the TigerProxy local forwarding address, API key, and
// selected model into ~/.codex/auth.json and ~/.codex/config.toml so Codex
// can use TigerProxy as its LLM backend.
// Returns a success message including a warning if Codex binary is not detected.
func (a *App) ConfigureCodex() (string, error) {
	s, err := loadSettings()
	if err != nil {
		return "", fmt.Errorf("load settings: %w", err)
	}
	if strings.TrimSpace(s.APIKey) == "" {
		return "", fmt.Errorf("API Key 未配置，请先设置 API Key")
	}
	modelID := s.ModelID
	if modelID == "" {
		modelID = "gpt-5.4"
	}

	// Use the local OpenAI-compatible endpoint as base URL for Codex
	baseURL := publicBaseURL(s.ListenAddress, "127.0.0.1") + "/v1"
	apiKey := s.APIKey
	providerName := "tigerproxy"
	wireApi := "responses"

	if err := configfile.WriteCodexConfig(apiKey, baseURL, modelID, providerName, wireApi); err != nil {
		return "", err
	}

	if !a.IsCodexInstalled() {
		return "配置已写入，但未检测到 Codex Desktop，请先安装。", nil
	}
	return "Codex 配置已写入 ~/.codex/", nil
}

// IsCodexInstalled checks whether Codex Desktop is available on the system.
// Checks both the CLI in PATH and common Desktop app installation paths.
// Avoids slow external commands (winget) — only uses fast filesystem checks.
func (a *App) IsCodexInstalled() bool {
	// Check CLI in PATH first
	if _, err := exec.LookPath("codex"); err == nil {
		return true
	}
	// Check platform-specific Desktop app locations (filesystem only, fast)
	switch goRuntime() {
	case "windows":
		if a.isCodexInstalledWindows() {
			return true
		}
	case "darwin":
		if _, err := os.Stat("/Applications/Codex.app"); err == nil {
			return true
		}
		if home, err := os.UserHomeDir(); err == nil {
			if _, err := os.Stat(filepath.Join(home, "Applications", "Codex.app")); err == nil {
				return true
			}
		}
	}
	return false
}

// isCodexInstalledWindows checks all known Windows installation paths for Codex Desktop.
func (a *App) isCodexInstalledWindows() bool {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		// Traditional installer paths (non-Store)
		candidates := []string{
			filepath.Join(localAppData, "Programs", "Codex", "codex.exe"),
			filepath.Join(localAppData, "Codex", "codex.exe"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}

		// Microsoft Store App Execution Alias directory.
		// When apps are installed from Store, Windows creates app aliases here.
		// The alias may be directly at WindowsApps\codex.exe or under a package
		// family name subdirectory like WindowsApps\OpenAI.Codex_<hash>\codex.exe
		appAliasDir := filepath.Join(localAppData, "Microsoft", "WindowsApps")
		if _, err := os.Stat(filepath.Join(appAliasDir, "codex.exe")); err == nil {
			return true
		}
		// Check package family name subdirectories (Store apps may use this layout)
		if findCodexInDir(appAliasDir, "OpenAI.Codex") {
			return true
		}

		// WinGet packages directory (older winget versions put packages here)
		msixBase := filepath.Join(localAppData, "Microsoft", "WinGet", "Packages")
		if findCodexInDir(msixBase, "OpenAI.Codex") {
			return true
		}
	}

	// Program Files — traditional installer
	programFiles := os.Getenv("ProgramFiles")
	if programFiles != "" {
		if _, err := os.Stat(filepath.Join(programFiles, "Codex", "codex.exe")); err == nil {
			return true
		}
	}

	// Microsoft Store / MSIX WindowsApps directory.
	// C:\Program Files\WindowsApps\ has restrictive ACLs (TrustedInstaller only).
	// os.ReadDir will fail for normal user processes. But os.Stat on a known full path
	// may succeed if the file exists (Windows allows stat through restricted dirs in some cases).
	// We attempt it as a last resort — if ACL blocks us, findCodexInDir returns false gracefully.
	windowsApps := filepath.Join(os.Getenv("ProgramFiles"), "WindowsApps")
	if findCodexInDir(windowsApps, "OpenAI.Codex") {
		return true
	}

	return false
}

// findCodexInDir scans a directory for subdirectories matching the given prefix
// and checks if codex.exe exists anywhere inside (up to 2 levels deep).
func findCodexInDir(baseDir, prefix string) bool {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		pkgDir := filepath.Join(baseDir, e.Name())
		// Check common locations within the package
		codexPaths := []string{
			filepath.Join(pkgDir, "codex.exe"),                          // root
			filepath.Join(pkgDir, "app", "resources", "codex.exe"),      // Store MSIX layout
			filepath.Join(pkgDir, "app", "codex.exe"),                   // alternative layout
			filepath.Join(pkgDir, "resources", "codex.exe"),             // another variant
		}
		for _, p := range codexPaths {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
	}
	return false
}

// codexDesktopWindowsStoreID is the Microsoft Store product ID for Codex Desktop.
const codexDesktopWindowsStoreID = "9PLM9XGG6VKS"

// codexDesktopStoreURL opens the MS Store page in a browser as fallback.
const codexDesktopStoreURL = "https://apps.microsoft.com/detail/" + codexDesktopWindowsStoreID
const codexDesktopMacURL = "https://persistent.oaistatic.com/codex-app-prod/Codex.dmg"

// InstallCodexDesktop installs Codex Desktop with progress reporting.
// Windows: uses winget synchronously with progress feedback, fallback to MS Store page.
// macOS: downloads .dmg and copies to /Applications.
func (a *App) InstallCodexDesktop() error {
	switch goRuntime() {
	case "windows":
		return a.installCodexWindows()
	case "darwin":
		return a.installCodexMac()
	default:
		return fmt.Errorf("当前平台不支持自动安装 Codex Desktop，请手动安装")
	}
}

// codexInstallProgress is emitted to the frontend via Wails events.
type codexInstallProgress struct {
	Phase   string  `json:"phase"`   // "downloading" | "installing" | "done" | "error" | "fallback"
	Percent float64 `json:"percent"` // 0-100
	Message string  `json:"message"` // human-readable status
}

func (a *App) emitCodexProgress(p codexInstallProgress) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "codex-install-progress", p)
	}
}

func (a *App) installCodexWindows() error {
	// Prevent concurrent install attempts
	a.mu.Lock()
	if a.codexInstalling {
		a.mu.Unlock()
		return fmt.Errorf("安装已在进行中，请稍候")
	}
	a.codexInstalling = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.codexInstalling = false
		a.mu.Unlock()
	}()

	a.emitCodexProgress(codexInstallProgress{Phase: "installing", Percent: 10, Message: "正在检查 winget..."})

	// Check if winget is available
	wingetPath, err := exec.LookPath("winget")
	if err != nil {
		// No winget — open MS Store page as fallback
		return a.codexFallbackToStore("系统未安装 winget，正在打开 Microsoft Store...")
	}

	a.emitCodexProgress(codexInstallProgress{Phase: "installing", Percent: 20, Message: "正在通过 winget 安装 Codex Desktop（可能需要 1-3 分钟）..."})

	// Run winget with a timeout to avoid indefinite hang.
	// winget MS Store installs can take 1-5 minutes depending on network.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, wingetPath, "install",
		"--name", "Codex",
		"--source", "msstore",
		"--accept-package-agreements",
		"--accept-source-agreements",
		"--silent",
	)
	// Hide the console window on Windows
	hideConsoleWindow(cmd)

	// Capture output for error diagnosis
	output, cmdErr := cmd.CombinedOutput()
	outputStr := string(output)

	// Check if already installed (winget may return non-zero but indicate "already installed")
	if isWingetAlreadyInstalled(outputStr) {
		a.emitCodexProgress(codexInstallProgress{Phase: "done", Percent: 100, Message: "Codex Desktop 已安装"})
		return nil
	}

	if cmdErr != nil {
		// Distinguish timeout from other failures
		if ctx.Err() == context.DeadlineExceeded {
			a.emitCodexProgress(codexInstallProgress{Phase: "installing", Percent: 50, Message: "winget 超时，尝试备用方案..."})
			return a.codexFallbackToStore("winget 安装超时（5 分钟），请在 Microsoft Store 中手动安装")
		}

		// winget failed — try fallback
		detail := strings.TrimSpace(outputStr)
		if len(detail) > 200 {
			detail = detail[:200]
		}
		a.emitCodexProgress(codexInstallProgress{Phase: "installing", Percent: 50, Message: "winget 安装未成功，尝试备用方案..."})
		return a.codexFallbackToStore(fmt.Sprintf("winget 安装失败: %s", detail))
	}

	// winget succeeded — verify installation
	a.emitCodexProgress(codexInstallProgress{Phase: "installing", Percent: 90, Message: "安装完成，正在验证..."})

	// Give the system a moment to register the app
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Second)
		if a.IsCodexInstalled() {
			a.emitCodexProgress(codexInstallProgress{Phase: "done", Percent: 100, Message: "Codex Desktop 安装成功！"})
			return nil
		}
	}

	// winget said success but we can't find it — might need a moment
	a.emitCodexProgress(codexInstallProgress{Phase: "done", Percent: 100, Message: "安装程序已完成，Codex 可能需要重启后生效。"})
	return nil
}

// isWingetAlreadyInstalled checks winget output for various "already installed" indicators
// across different system languages.
func isWingetAlreadyInstalled(output string) bool {
	lower := strings.ToLower(output)
	indicators := []string{
		"already installed",
		"已安装",
		"no applicable update",
		"no newer package versions",
		"found an existing package",
	}
	for _, s := range indicators {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// codexFallbackToStore opens the Microsoft Store page for manual installation.
func (a *App) codexFallbackToStore(reason string) error {
	a.emitCodexProgress(codexInstallProgress{Phase: "fallback", Percent: 50, Message: reason})

	// Try ms-windows-store:// protocol first (opens Store app directly)
	storeProtocol := fmt.Sprintf("ms-windows-store://pdp/?productid=%s", codexDesktopWindowsStoreID)
	cmd := exec.Command("cmd", "/c", "start", "", storeProtocol)
	hideConsoleWindow(cmd)
	if err := cmd.Run(); err != nil {
		// Fallback to browser
		if a.ctx != nil {
			runtime.BrowserOpenURL(a.ctx, codexDesktopStoreURL)
		}
	}

	a.emitCodexProgress(codexInstallProgress{Phase: "fallback", Percent: 100, Message: "已打开 Microsoft Store，请在商店中点击「获取」安装 Codex Desktop。"})

	// Poll for installation in background
	go func() {
		for i := 0; i < 60; i++ {
			time.Sleep(5 * time.Second)
			// Stop polling if app is shutting down
			if a.ctx == nil || a.ctx.Err() != nil {
				return
			}
			if a.IsCodexInstalled() {
				a.emitCodexProgress(codexInstallProgress{Phase: "done", Percent: 100, Message: "Codex Desktop 安装成功！"})
				return
			}
		}
		// Polling expired without detecting install — reset UI so user can retry
		if a.ctx != nil && a.ctx.Err() == nil {
			a.emitCodexProgress(codexInstallProgress{Phase: "error", Percent: 0, Message: "未检测到安装完成，请点击按钮重试。"})
		}
	}()

	return nil
}

func (a *App) installCodexMac() error {
	tmpDir := os.TempDir()
	installerPath := filepath.Join(tmpDir, "Codex.dmg")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Get(codexDesktopMacURL)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	outFile, err := os.Create(installerPath)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	if _, err := io.Copy(outFile, resp.Body); err != nil {
		outFile.Close()
		os.Remove(installerPath)
		return fmt.Errorf("写入安装包失败: %w", err)
	}
	outFile.Close()

	// Mount the DMG with -nobrowse to avoid Finder auto-opening
	mountOut, err := exec.Command("hdiutil", "attach", installerPath, "-nobrowse").Output()
	if err != nil {
		os.Remove(installerPath)
		return fmt.Errorf("挂载 DMG 失败: %w", err)
	}
	// Parse actual mount point from hdiutil output
	volumePath := parseDMGMountPoint(string(mountOut))
	if volumePath == "" {
		volumePath = "/Volumes/Codex"
	}

	// Try to copy .app to /Applications automatically
	appPath := filepath.Join(volumePath, "Codex.app")
	if _, statErr := os.Stat(appPath); statErr == nil {
		cpCmd := exec.Command("cp", "-R", appPath, "/Applications/")
		if cpErr := cpCmd.Run(); cpErr == nil {
			_ = exec.Command("hdiutil", "detach", volumePath, "-quiet").Run()
			go func() { time.Sleep(5 * time.Second); os.Remove(installerPath) }()
			return nil
		}
	}
	// Fallback: open the volume for manual drag-to-Applications
	_ = exec.Command("open", volumePath).Start()
	go func() {
		time.Sleep(60 * time.Second)
		_ = exec.Command("hdiutil", "detach", volumePath, "-quiet").Run()
		os.Remove(installerPath)
	}()
	return nil
}

// parseDMGMountPoint extracts the mount point from hdiutil attach output.
// Output format: "/dev/diskN\tApple_HFS\t/Volumes/VolumeName"
func parseDMGMountPoint(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if idx := strings.LastIndex(line, "/Volumes/"); idx >= 0 {
			return strings.TrimSpace(line[idx:])
		}
	}
	return ""
}

// goRuntime returns the GOOS value at runtime.
func goRuntime() string {
	return runtimeGOOS
}

func (a *App) ShowMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	a.setShown(true)
}
func (a *App) WindowHide() {
	if a.ctx == nil {
		return
	}
	runtime.WindowHide(a.ctx)
	a.setShown(false)
}

func (a *App) startProxyFromDisk() {
	s, err := loadSettings()
	if err != nil {
		return
	}
	_ = a.restartProxy(s)
}

func (a *App) restartProxy(s Settings) error {
	s = normalizeSettings(s)
	if _, err := normalizeListenAddress(s.ListenAddress); err != nil {
		return err
	}

	a.mu.Lock()
	sameListenAddress := a.server != nil && a.listen == s.ListenAddress
	a.mu.Unlock()
	if sameListenAddress {
		a.stopProxy()
	}

	ctx, cancel := context.WithCancel(context.Background())
	server := codegenproxy.NewServer(s.ListenAddress)
	server.SetClientAPIKey(s.APIKey)
	if strings.TrimSpace(s.AccessToken) != "" && strings.TrimSpace(s.BaseURL) != "" {
		server.SetUpstreamWithClientName(s.BaseURL, s.AccessToken, corelib.CodeGenClientName)
	}
	listener, err := net.Listen("tcp", s.ListenAddress)
	if err != nil {
		cancel()
		return fmt.Errorf("listen %s: %w", s.ListenAddress, err)
	}

	if !sameListenAddress {
		a.stopProxy()
	}

	a.mu.Lock()
	a.cancel = cancel
	a.server = server
	a.listen = s.ListenAddress
	a.lastError = ""
	a.mu.Unlock()

	go func() {
		if err := server.Serve(ctx, listener); err != nil && ctx.Err() == nil {
			msg := fmt.Sprintf("TigerProxy server stopped: %v", err)
			fmt.Fprintln(os.Stderr, msg)
			a.mu.Lock()
			if a.server == server {
				a.server = nil
				a.cancel = nil
				a.listen = ""
				a.lastError = msg
			}
			a.mu.Unlock()
		}
	}()
	return nil
}

func (a *App) stopProxy() {
	a.mu.Lock()
	cancel := a.cancel
	server := a.server
	a.cancel = nil
	a.server = nil
	a.listen = ""
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if server != nil {
		server.Stop()
	}
}

func (a *App) isRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.server != nil
}

func (a *App) getLastError() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastError
}

func (a *App) setShown(v bool) {
	a.mu.Lock()
	a.shown = v
	a.mu.Unlock()
	UpdateTrayVisibility(v)
}

func (a *App) isShown() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.shown
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".tigerproxy"), nil
}

func settingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func loadSettings() (Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return Settings{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s := normalizeSettings(Settings{})
			_ = writeSettings(s)
			return s, nil
		}
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		// Primary file is corrupted (e.g. partial write from a crash).
		// Try the .tmp file which writeSettings creates before rename —
		// it may contain a more recent valid snapshot.
		if tmpData, tmpErr := os.ReadFile(path + ".tmp"); tmpErr == nil {
			if json.Unmarshal(tmpData, &s) == nil {
				// Recovered from tmp — persist it as the primary file.
				_ = os.Rename(path+".tmp", path)
				return normalizeSettings(s), nil
			}
		}
		// Both files are bad — reset to defaults rather than returning an error
		// that blocks the entire app from starting.
		s = normalizeSettings(Settings{})
		_ = writeSettings(s)
		return s, nil
	}
	return normalizeSettings(s), nil
}

func writeSettings(s Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	s.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(normalizeSettings(s), "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: write to temp file first, then rename over target.
	// This prevents data loss if the process is killed mid-write.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Rename can fail on Windows if target is locked; fall back to direct write.
		_ = os.Remove(tmp)
		return os.WriteFile(path, data, 0o600)
	}
	return nil
}

func normalizeSettings(s Settings) Settings {
	if strings.TrimSpace(s.ListenAddress) == "" {
		s.ListenAddress = defaultListenAddress
	}
	if strings.TrimSpace(s.APIKey) == "" {
		s.APIKey = defaultProxyAPIKey
	}
	if strings.TrimSpace(s.BaseURL) == "" {
		s.BaseURL = oauth.CodeGenBaseURL
	}
	if normalized, err := normalizeListenAddress(s.ListenAddress); err == nil {
		s.ListenAddress = normalized
	}
	s.APIKey = strings.TrimSpace(s.APIKey)
	s.AccessToken = strings.TrimSpace(s.AccessToken)
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	s.ModelID = strings.TrimSpace(s.ModelID)
	s.Email = strings.TrimSpace(s.Email)
	s.Models = normalizeModelOptions(s.Models)
	return s
}

func normalizeListenAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = defaultListenAddress
	}
	if strings.HasPrefix(addr, ":") {
		addr = "0.0.0.0" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.Count(addr, ":") == 0 {
			return "0.0.0.0:" + addr, nil
		}
		return "", fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if strings.TrimSpace(port) == "" {
		return "", fmt.Errorf("invalid listen address %q: port is required", addr)
	}
	if strings.TrimSpace(host) == "" || strings.EqualFold(host, "localhost") || host == "*" {
		host = "0.0.0.0"
	} else if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
	}
	return net.JoinHostPort(host, port), nil
}

func scrubSettings(s Settings) Settings {
	if s.AccessToken != "" {
		s.AccessToken = "已保存"
	}
	return s
}

func publicBaseURL(listen, host string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		port = "18086"
	}
	return "http://" + host + ":" + port
}

func lanURLs(listen string) []string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		port = "18086"
	}
	var urls []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			urls = append(urls, "http://"+ip.String()+":"+port)
		}
	}
	sort.Strings(urls)
	return urls
}

func modelOptionsFromOAuth(models []oauth.CodeGenModel) []ModelOption {
	options := make([]ModelOption, 0, len(models))
	for _, model := range models {
		options = append(options, ModelOption{
			ID:            model.ID,
			Name:          model.Name,
			ContextWindow: model.ContextWindow,
		})
	}
	return normalizeModelOptions(options)
}

func normalizeModelOptions(models []ModelOption) []ModelOption {
	seen := map[string]bool{}
	out := make([]ModelOption, 0, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		model.Name = strings.TrimSpace(model.Name)
		model.ID = normalizeModelID(model.ID)
		if model.ID == "" || seen[model.ID] {
			continue
		}
		if model.Name == "" {
			model.Name = modelDisplayName(model.ID)
		}
		seen[model.ID] = true
		out = append(out, model)
	}
	return out
}

// modelDisplayName generates a user-friendly display name from a model ID.
// "vendor/GLM-5.1" → "GLM-5.1 (vendor)"
// "GLM-5.2" → "GLM-5.2"
func modelDisplayName(id string) string {
	if strings.HasPrefix(id, "vendor/") {
		bare := strings.TrimPrefix(id, "vendor/")
		if bare != "" {
			return bare + " (vendor)"
		}
	}
	return id
}

func normalizeModelID(id string) string {
	id = strings.TrimSpace(id)
	// Keep "vendor/" prefix — it has routing semantics (tells CodeGen to use a third-party vendor API).
	if strings.HasPrefix(id, "vendor/") {
		return id
	}
	if idx := strings.LastIndex(id, "/"); idx >= 0 && idx+1 < len(id) {
		return strings.TrimSpace(id[idx+1:])
	}
	return id
}

// modelExistsInList checks whether the user's selected model ID is still
// present in the (already-normalized) model list. Returns false for empty
// modelID so callers fall through to the server default.
func modelExistsInList(modelID string, models []ModelOption) bool {
	if strings.TrimSpace(modelID) == "" {
		return false
	}
	normalized := normalizeModelID(modelID)
	for _, m := range models {
		if m.ID == normalized {
			return true
		}
	}
	return false
}
