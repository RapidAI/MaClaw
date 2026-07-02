package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

var (
	claudeReleasesBase = "https://storage.googleapis.com/claude-code-dist-86c565f3-f756-42ad-8dfa-d59b1c096819/claude-code-releases"
	httpGet            = http.Get
)

type ToolStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path"`
}

type ToolManager struct {
	app *App
}

// toolStatusCache caches GetToolStatus results to avoid spawning child
// processes (e.g. "claude --version") on every call. The cache is
// invalidated by calling InvalidateToolStatusCache (after install/uninstall).
var (
	toolStatusCacheMu     sync.RWMutex
	toolStatusCacheMap    = make(map[string]toolStatusCacheEntry)
	toolStatusCacheTTL    = 10 * time.Minute // positive results (tool installed)
	toolStatusCacheNegTTL = 1 * time.Minute  // negative results (tool not found)
)

type toolStatusCacheEntry struct {
	status   ToolStatus
	cachedAt time.Time
}

// InvalidateToolStatusCache clears the cached tool status for the given tool,
// or all tools if name is empty. Call this after installing or uninstalling a tool.
func InvalidateToolStatusCache(name string) {
	toolStatusCacheMu.Lock()
	defer toolStatusCacheMu.Unlock()
	if name == "" {
		toolStatusCacheMap = make(map[string]toolStatusCacheEntry)
	} else {
		prefix := remote.NormalizeRemoteToolName(name) + "\x00"
		for key := range toolStatusCacheMap {
			if key == remote.NormalizeRemoteToolName(name) || strings.HasPrefix(key, prefix) {
				delete(toolStatusCacheMap, key)
			}
		}
	}
}

func NewToolManager(app *App) *ToolManager {
	return &ToolManager{app: app}
}

func privateToolsDirForApp(app *App) string {
	if app != nil {
		return filepath.Join(app.GetDataDir(), "tools")
	}
	return remote.ToolsDir()
}

func (tm *ToolManager) privateToolsDir() string {
	return privateToolsDirForApp(tm.app)
}

func (tm *ToolManager) GetToolStatus(name string) ToolStatus {
	normalized := remote.NormalizeRemoteToolName(name)
	status := ToolStatus{Name: name}
	toolsDir := tm.privateToolsDir()
	if strings.TrimSpace(toolsDir) == "" {
		return status
	}
	cacheKey := normalized + "\x00" + toolsDir

	// Check cache first — avoids spawning child processes on every UI tab switch.
	toolStatusCacheMu.RLock()
	if entry, ok := toolStatusCacheMap[cacheKey]; ok {
		ttl := toolStatusCacheTTL
		if !entry.status.Installed {
			ttl = toolStatusCacheNegTTL
		}
		if time.Since(entry.cachedAt) < ttl {
			toolStatusCacheMu.RUnlock()
			return entry.status
		}
	}
	toolStatusCacheMu.RUnlock()

	tm.app.log(fmt.Sprintf("GetToolStatus: Checking tool '%s'", name))

	path, found := remote.ResolveToolPathInDir(name, toolsDir)
	if !found {
		tm.app.log(fmt.Sprintf("GetToolStatus: Tool '%s' NOT found", name))
		toolStatusCacheMu.Lock()
		toolStatusCacheMap[cacheKey] = toolStatusCacheEntry{status: status, cachedAt: time.Now()}
		toolStatusCacheMu.Unlock()
		return status
	}

	tm.app.log(fmt.Sprintf("GetToolStatus: Tool '%s' found at: %s", name, path))
	status.Installed = true
	status.Path = path

	version, err := tm.getToolVersion(name, path)
	if err == nil {
		status.Version = version
	}

	// Cache positive result.
	toolStatusCacheMu.Lock()
	toolStatusCacheMap[cacheKey] = toolStatusCacheEntry{status: status, cachedAt: time.Now()}
	toolStatusCacheMu.Unlock()

	return status
}

func (tm *ToolManager) claudeVersionEndpoint(target string) string {
	return fmt.Sprintf("%s/%s", claudeReleasesBase, target)
}

func (tm *ToolManager) fetchRemoteVersion(url string) (string, error) {
	resp, err := httpGet(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(body))
	if version == "" || len(version) > 64 || strings.Contains(version, "<") {
		return "", fmt.Errorf("invalid version response: %q", version)
	}
	return version, nil
}

func (tm *ToolManager) fetchLatestClaudeVersion(target string) (string, error) {
	if target == "" {
		target = "latest"
	}
	version, err := tm.fetchRemoteVersion(tm.claudeVersionEndpoint(target))
	if err != nil {
		return "", fmt.Errorf("failed to get %s version: %w", target, err)
	}
	return version, nil
}

func (tm *ToolManager) GetLatestVersion(name string) (string, error) {
	switch remote.NormalizeRemoteToolName(name) {
	case "claude":
		return tm.fetchLatestClaudeVersion("latest")
	default:
		packageName := tm.GetPackageName(name)
		if packageName == "" {
			return "", fmt.Errorf("automatic update checks are not supported for tool %s", name)
		}
		npmPath := tm.getNpmPath()
		if npmPath == "" {
			return "", fmt.Errorf("npm not found")
		}
		return tm.app.getLatestNpmVersion(npmPath, packageName)
	}
}

func (tm *ToolManager) NeedsUpdate(name, currentVersion string) (bool, string, error) {
	latest, err := tm.GetLatestVersion(name)
	if err != nil {
		return false, "", err
	}
	if strings.TrimSpace(latest) == "" {
		return false, "", fmt.Errorf("empty latest version for tool %s", name)
	}
	if strings.TrimSpace(currentVersion) == "" {
		return true, latest, nil
	}
	return compareVersions(currentVersion, latest) < 0, latest, nil
}

func (tm *ToolManager) getToolVersion(name, path string) (string, error) {
	var cmd *exec.Cmd
	cmd = createVersionCmd(path)

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	output := strings.TrimSpace(string(out))
	// Parse version based on tool output format
	if strings.Contains(name, "claude") {
		// Native format: "2.1.29 (Claude Code)"
		// NPM format: "claude-code/0.2.29 darwin-arm64 node-v22.12.0"
		if strings.Contains(output, "(Claude Code)") {
			// Native format
			parts := strings.Split(output, " ")
			if len(parts) > 0 {
				return parts[0], nil
			}
		} else {
			// NPM format
			parts := strings.Split(output, " ")
			if len(parts) > 0 {
				verParts := strings.Split(parts[0], "/")
				if len(verParts) == 2 {
					return verParts[1], nil
				}
			}
		}
	}

	return output, nil
}

func (tm *ToolManager) InstallTool(name string) error {
	// Use native installation for Claude Code
	if name == "claude" {
		return tm.installClaudeNative("latest")
	}

	npmPath := tm.getNpmPath()
	if npmPath == "" {
		return fmt.Errorf("npm not found. Please ensure Node.js is installed.")
	}

	localNodeDir := tm.privateToolsDir()
	if strings.TrimSpace(localNodeDir) == "" {
		return fmt.Errorf("cannot determine private tools directory")
	}

	// Ensure the local node directory exists for prefix usage
	if err := os.MkdirAll(localNodeDir, 0755); err != nil {
		return fmt.Errorf("failed to create local node directory: %w", err)
	}

	// Get package name
	packageName := tm.GetPackageName(name)
	if packageName == "" {
		return fmt.Errorf("unknown tool: %s", name)
	}

	// Pre-clean: Remove existing installation if present to avoid ENOTEMPTY errors on Windows
	pkgDir := filepath.Join(localNodeDir, "node_modules", packageName)
	if _, err := os.Stat(pkgDir); err == nil {
		tm.app.log(tm.app.tr("Removing existing %s installation to ensure clean install...", name))
		// Remove wrapper scripts first
		if runtime.GOOS == "windows" {
			wrappers := []string{
				filepath.Join(localNodeDir, name+".cmd"),
				filepath.Join(localNodeDir, name+".ps1"),
				filepath.Join(localNodeDir, name),
			}
			for _, wrapper := range wrappers {
				os.Remove(wrapper) // Best effort
			}
		}
		// Remove package directory with retry for locked files
		for i := 0; i < 3; i++ {
			err = os.RemoveAll(pkgDir)
			if err == nil {
				break
			}
			if i < 2 {
				tm.app.log(tm.app.tr("Retry removing directory (attempt %d/3)...", i+2))
				time.Sleep(time.Second)
			}
		}
		if err != nil {
			tm.app.log(tm.app.tr("Warning: Failed to completely remove old installation: %v", err))
		}
	}

	// Use --prefix to install to our local folder, avoiding sudo/permission issues
	// This works with both system npm and local npm.

	// Add @latest to ensure latest version is installed
	packages := []string{packageName + "@latest"}
	if name == "opencode" && runtime.GOOS != "windows" {
		var platformPkg string
		if runtime.GOOS == "darwin" {
			if runtime.GOARCH == "arm64" {
				platformPkg = "opencode-darwin-arm64@latest"
			} else {
				platformPkg = "opencode-darwin-x64@latest"
			}
		} else if runtime.GOOS == "linux" {
			if runtime.GOARCH == "arm64" {
				platformPkg = "opencode-linux-arm64@latest"
			} else {
				platformPkg = "opencode-linux-x64@latest"
			}
		}

		if platformPkg != "" {
			packages = append(packages, platformPkg)
		}
	}

	// Use a local cache directory to avoid permission issues with system/user cache
	localCacheDir := tm.app.GetLocalCacheDir()
	if err := os.MkdirAll(localCacheDir, 0755); err != nil {
		tm.app.log(fmt.Sprintf("Warning: Failed to create local npm cache dir: %v", err))
	}

	args := []string{"install", "-g"}
	args = append(args, packages...)
	args = append(args, "--prefix", localNodeDir, "--cache", localCacheDir, "--loglevel", "info")

	// Use --force to avoid ENOTEMPTY and other file lock issues on Windows
	args = append(args, "--force")

	// Skip postinstall scripts for iflow due to missing postinstall-ripgrep.js
	if name == "iflow" {
		args = append(args, "--ignore-scripts")
	}

	if normalizeAppLanguageKind(tm.app.CurrentLanguage).IsChinese() {
		args = append(args, "--registry=https://registry.npmmirror.com")
	}

	var cmd *exec.Cmd
	cmd = createNpmInstallCmd(npmPath, args)

	// Set environment to include local node bin for the installation process
	localBinDir := filepath.Join(localNodeDir, "bin")
	if runtime.GOOS == "windows" {
		localBinDir = localNodeDir
	}

	env := os.Environ()
	pathFound := false
	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			env[i] = fmt.Sprintf("PATH=%s%c%s", localBinDir, os.PathListSeparator, e[5:])
			pathFound = true
			break
		}
	}
	if !pathFound {
		env = append(env, "PATH="+localBinDir)
	}
	cmd.Env = env

	tm.app.log(tm.app.tr("Running installation: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " ")))

	out, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(out)
		// Check for specific npm errors
		needsRetry := false
		if strings.Contains(outputStr, "EACCES") || strings.Contains(outputStr, "EEXIST") {
			tm.app.log(tm.app.tr("Detected npm cache permission issue. Attempting to clear cache..."))
			needsRetry = true
		} else if strings.Contains(outputStr, "ENOTEMPTY") {
			tm.app.log(tm.app.tr("Detected ENOTEMPTY error (file lock issue). Will retry with cleanup..."))
			// Clean up the problematic directory more aggressively
			time.Sleep(2 * time.Second) // Wait for file locks to release
			os.RemoveAll(pkgDir)        // Try to remove again
			needsRetry = true
		}

		if needsRetry {
			// Try to clean cache
			cleanArgs := []string{"cache", "clean", "--force", "--cache", localCacheDir}
			if normalizeAppLanguageKind(tm.app.CurrentLanguage).IsChinese() {
				cleanArgs = append(cleanArgs, "--registry=https://registry.npmmirror.com")
			}

			cleanCmd := createNpmInstallCmd(npmPath, cleanArgs)
			cleanCmd.Env = env
			cleanCmd.CombinedOutput() // Ignore error on clean

			tm.app.log(tm.app.tr("Retrying installation after cleanup..."))
			// Retry installation
			cmd = createNpmInstallCmd(npmPath, args)
			cmd.Env = env
			out, err = cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to install %s (retry): %v\nOutput: %s", name, err, string(out))
			}
		} else {
			return fmt.Errorf("failed to install %s: %v\nOutput: %s", name, err, string(out))
		}
	}

	// Post-installation verification
	tm.app.log(tm.app.tr("Verifying %s installation...", name))
	time.Sleep(500 * time.Millisecond) // Brief wait for file system sync

	InvalidateToolStatusCache(name)
	status := tm.GetToolStatus(name)
	if !status.Installed {
		return fmt.Errorf("installation completed but tool verification failed - %s not found", name)
	}

	tm.app.log(tm.app.tr("✓ %s installed and verified successfully (version: %s)", name, status.Version))
	return nil
}

func (a *App) installToolOnDemandForOwner(ownerID, toolName string) error {
	toolName = strings.ToLower(strings.TrimSpace(toolName))
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(ownerID, "bash", map[string]interface{}{"command": "install tool " + toolName, "tool": toolName}); err != nil {
		return err
	}
	return a.installToolOnDemandUnchecked(toolName)
}

func (a *App) installToolOnDemandUnchecked(toolName string) error {
	toolName = normalizeRemoteToolName(toolName)
	if !remoteToolAutoInstallSupported(toolName) {
		return fmt.Errorf("automatic installation is not supported for %s", toolName)
	}

	// Try to acquire lock for this tool
	if !a.tryLockTool(toolName) {
		a.log(a.tr("On-demand installation: %s is already being installed in background, waiting...", toolName))
		// Wait for background installation to complete
		for i := 0; i < 60; i++ { // Wait up to 60 seconds
			time.Sleep(1 * time.Second)
			if !a.isToolLocked(toolName) {
				break
			}
		}
		// Check if tool is now installed
		tm := NewToolManager(a)
		status := tm.GetToolStatus(toolName)
		if status.Installed {
			a.log(a.tr("On-demand installation: %s was installed by background process.", toolName))
			return nil
		}
		// Try to acquire lock again
		if !a.tryLockTool(toolName) {
			return fmt.Errorf("tool %s is still being installed", toolName)
		}
	}
	defer a.unlockTool(toolName)

	tm := NewToolManager(a)
	status := tm.GetToolStatus(toolName)

	if status.Installed {
		needsUpdate, _, err := tm.NeedsUpdate(toolName, status.Version)
		if err != nil {
			a.log(a.tr("On-demand installation: Warning: failed to check updates for %s: %v", toolName, err))
			return nil
		}
		if !needsUpdate {
			return nil
		}
		a.log(a.tr("On-demand installation: Updating %s...", toolName))
		if err := tm.UpdateTool(toolName); err != nil {
			a.log(a.tr("On-demand installation: ERROR: Failed to update %s: %v", toolName, err))
			return err
		}
		a.log(a.tr("On-demand installation: %s updated successfully.", toolName))
		a.emitEvent("tool-updated", toolName)
		return nil
	}

	a.log(a.tr("On-demand installation: Installing %s...", toolName))
	if err := tm.InstallTool(toolName); err != nil {
		a.log(a.tr("On-demand installation: ERROR: Failed to install %s: %v", toolName, err))
		return err
	}

	// Update PATH to include newly installed tool
	a.updatePathForNode()

	a.log(a.tr("On-demand installation: %s installed successfully.", toolName))
	a.emitEvent("tool-installed", toolName)
	return nil
}

func (tm *ToolManager) UpdateTool(name string) error {
	// Use native update for Claude Code
	if name == "claude" {
		return tm.installClaudeNative("latest")
	}

	// Verify the tool is installed in our private directory first
	status := tm.GetToolStatus(name)
	if !status.Installed {
		return fmt.Errorf("tool %s is not installed", name)
	}

	expectedPrefix := filepath.Clean(tm.privateToolsDir())
	statusPath := filepath.Clean(status.Path)
	if expectedPrefix == "" || (statusPath != expectedPrefix && !strings.HasPrefix(statusPath, expectedPrefix+string(os.PathSeparator))) {
		return fmt.Errorf("tool %s is not installed in private directory (%s), cannot update. Only private installations can be updated.", name, status.Path)
	}

	// Use npm to update the package in private directory
	// This avoids calling the tool's own update command which might try to update global installations
	packageName := tm.GetPackageName(name)
	if packageName == "" {
		return fmt.Errorf("unknown package name for tool %s", name)
	}

	tm.app.log(tm.app.tr("Updating %s in private directory using npm...", name))

	// Find npm
	npmExec, err := exec.LookPath("npm")
	if err != nil {
		npmExec, err = exec.LookPath("npm.cmd")
		if err != nil {
			// Also check private npm
			npmExec = tm.getNpmPath()
			if npmExec == "" {
				return fmt.Errorf("npm not found")
			}
		}
	}

	// Set up npm prefix to private directory
	localToolsDir := expectedPrefix

	// Use npm install with latest version to update
	args := []string{"install", "-g", "--prefix", localToolsDir, packageName + "@latest", "--force"}

	// Add ignore-scripts for tools that have problematic postinstall scripts
	if name == "iflow" || name == "kilo" {
		args = append(args, "--ignore-scripts")
	}

	// Use local cache directory
	localCacheDir := tm.app.GetLocalCacheDir()
	if localCacheDir != "" {
		args = append(args, "--cache", localCacheDir)
	}

	if normalizeAppLanguageKind(tm.app.CurrentLanguage).IsChinese() {
		args = append(args, "--registry=https://registry.npmmirror.com")
	}

	cmd := createNpmInstallCmd(npmExec, args)

	tm.app.log(tm.app.tr("Running: npm %s", strings.Join(args, " ")))

	// Retry logic for Windows file locking issues
	maxRetries := 3
	var out []byte
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			tm.app.log(tm.app.tr("Retry attempt %d/%d after waiting...", i+1, maxRetries))
			time.Sleep(time.Duration(i*2) * time.Second) // Progressive backoff
		}

		out, err = cmd.CombinedOutput()
		if err == nil {
			break
		}

		// Check if it's a file locking error
		outputStr := string(out)
		isLockError := strings.Contains(outputStr, "EPERM") ||
			strings.Contains(outputStr, "EBUSY") ||
			strings.Contains(outputStr, "ENOTEMPTY") ||
			strings.Contains(outputStr, "operation not permitted") ||
			strings.Contains(outputStr, "resource busy or locked")

		if !isLockError || i == maxRetries-1 {
			// Not a lock error or final retry failed
			break
		}

		tm.app.log(tm.app.tr("Detected file lock issue, will retry..."))

		// Recreate command for retry
		cmd = createNpmInstallCmd(npmExec, args)
	}

	if err != nil {
		outputStr := string(out)
		// Check for specific errors that can be ignored
		if strings.Contains(outputStr, "403") && strings.Contains(outputStr, "ripgrep") {
			tm.app.log(tm.app.tr("Warning: ripgrep download failed (GitHub API limit), but %s may still work", name))
			// Don't fail the update for ripgrep download issues
			InvalidateToolStatusCache(name)
			return nil
		}

		return fmt.Errorf("failed to update %s: %v\nOutput: %s", name, err, string(out))
	}

	tm.app.log(tm.app.tr("Successfully updated %s in private directory", name))
	InvalidateToolStatusCache(name)
	return nil
}

// ClaudeManifest represents the manifest.json structure from Claude Code releases
type ClaudeManifest struct {
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
	Platforms map[string]struct {
		Checksum string `json:"checksum"`
		Size     int64  `json:"size"`
	} `json:"platforms"`
}

// installClaudeNative installs Claude Code using the native binary installer
func (tm *ToolManager) installClaudeNative(target string) error {
	home := tm.app.GetUserHomeDir()
	installDir := tm.privateToolsDir()
	if strings.TrimSpace(installDir) == "" {
		return fmt.Errorf("cannot determine private tools directory")
	}
	downloadDir := filepath.Join(home, ".claude", "downloads")

	// Determine platform
	var platform string
	switch runtime.GOOS {
	case "windows":
		platform = "win32-x64"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			platform = "darwin-arm64"
		} else {
			platform = "darwin-x64"
		}
	case "linux":
		if runtime.GOARCH == "arm64" {
			platform = "linux-arm64"
		} else {
			platform = "linux-x64"
		}
	default:
		return fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Create directories
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	tm.app.log(tm.app.tr("Fetching latest Claude Code version..."))

	// Get version
	var version string
	if target == "latest" || target == "stable" {
		var err error
		version, err = tm.fetchLatestClaudeVersion(target)
		if err != nil {
			return err
		}
	} else {
		version = target
	}

	tm.app.log(tm.app.tr("Installing Claude Code version: %s", version))

	// Get manifest
	manifestURL := fmt.Sprintf("%s/%s/manifest.json", claudeReleasesBase, version)
	resp, err := http.Get(manifestURL)
	if err != nil {
		return fmt.Errorf("failed to get manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get manifest: HTTP %s (url: %s)", resp.Status, manifestURL)
	}

	var manifest ClaudeManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	platformInfo, ok := manifest.Platforms[platform]
	if !ok {
		return fmt.Errorf("platform %s not found in manifest", platform)
	}

	expectedChecksum := platformInfo.Checksum
	expectedSize := platformInfo.Size

	tm.app.log(tm.app.tr("Expected size: %.2f MB", float64(expectedSize)/1024/1024))

	// Determine binary name and download URL
	var binaryName string
	if runtime.GOOS == "windows" {
		binaryName = "claude.exe"
	} else {
		binaryName = "claude"
	}

	downloadURL := fmt.Sprintf("%s/%s/%s/%s", claudeReleasesBase, version, platform, binaryName)
	downloadPath := filepath.Join(downloadDir, fmt.Sprintf("claude-%s-%s%s", version, platform, filepath.Ext(binaryName)))

	tm.app.log(tm.app.tr("Downloading from: %s", downloadURL))

	// Download binary
	resp, err = http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download binary: HTTP %s (url: %s)", resp.Status, downloadURL)
	}

	outFile, err := os.Create(downloadPath)
	if err != nil {
		return fmt.Errorf("failed to create download file: %w", err)
	}

	// Calculate checksum while downloading
	hasher := sha256.New()
	writer := io.MultiWriter(outFile, hasher)

	_, err = io.Copy(writer, resp.Body)
	outFile.Sync()
	outFile.Close()
	if err != nil {
		os.Remove(downloadPath)
		return fmt.Errorf("failed to download binary: %w", err)
	}

	// Verify checksum
	tm.app.log(tm.app.tr("Verifying checksum..."))
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))

	if actualChecksum != expectedChecksum {
		os.Remove(downloadPath)
		return fmt.Errorf("checksum verification failed!\nExpected: %s\nActual: %s", expectedChecksum, actualChecksum)
	}

	tm.app.log(tm.app.tr("Checksum verified successfully."))

	// Install to private directory
	targetPath := filepath.Join(installDir, binaryName)

	// Remove old version if exists
	if _, err := os.Stat(targetPath); err == nil {
		tm.app.log(tm.app.tr("Removing old version..."))
		// On Windows, retry removal in case file is locked
		for i := 0; i < 3; i++ {
			err = os.Remove(targetPath)
			if err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if err != nil {
			os.Remove(downloadPath)
			return fmt.Errorf("failed to remove old version: %w", err)
		}
	}

	// Copy to install directory
	tm.app.log(tm.app.tr("Installing to: %s", installDir))

	srcFile, err := os.Open(downloadPath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create target file: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		return fmt.Errorf("failed to copy binary: %w", err)
	}
	// Sync and close before verification to ensure the binary is fully on disk.
	dstFile.Sync()
	dstFile.Close()

	// Create wrapper scripts (Windows only)
	if runtime.GOOS == "windows" {
		quotedTarget := targetPath
		cmdWrapper := fmt.Sprintf("@echo off\n\"%s\" %%*\n", quotedTarget)
		ps1Wrapper := fmt.Sprintf("& %q @args", quotedTarget)

		os.WriteFile(filepath.Join(installDir, "claude.cmd"), []byte(cmdWrapper), 0755)
		os.WriteFile(filepath.Join(installDir, "claude.ps1"), []byte(ps1Wrapper), 0755)
	}

	// Clean up download
	os.Remove(downloadPath)

	// Verify installation
	tm.app.log(tm.app.tr("Verifying installation..."))
	time.Sleep(500 * time.Millisecond)

	InvalidateToolStatusCache("claude")
	status := tm.GetToolStatus("claude")
	if !status.Installed {
		return fmt.Errorf("installation completed but verification failed - claude not found at expected path")
	}

	// Run the binary to confirm it's functional (not a truncated download).
	verifyCmd := exec.Command(status.Path, "--version")
	verifyCmd.Env = os.Environ()
	hideCommandWindow(verifyCmd)
	if out, err := verifyCmd.Output(); err != nil {
		// Binary exists but doesn't run — remove it so next attempt retries.
		os.Remove(status.Path)
		return fmt.Errorf("claude binary exists but failed to execute: %v", err)
	} else {
		tm.app.log(tm.app.tr("✓ Claude Code %s installed successfully!", strings.TrimSpace(string(out))))
	}

	return nil
}

func (tm *ToolManager) GetPackageName(name string) string {
	return remote.PackageName(name)
}

func (tm *ToolManager) getNpmPath() string {
	// 1. Check local node environment first
	toolsDir := tm.privateToolsDir()
	var localNpm string
	if runtime.GOOS == "windows" {
		localNpm = filepath.Join(toolsDir, "npm.cmd")
	} else {
		localNpm = filepath.Join(toolsDir, "bin", "npm")
	}

	if _, err := os.Stat(localNpm); err == nil {
		return localNpm
	}

	// 2. Fallback to system npm
	path, err := exec.LookPath("npm")
	if err == nil {
		return path
	}

	return ""
}

func (a *App) InstallTool(name string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": "install tool " + strings.TrimSpace(name), "tool": strings.TrimSpace(name)}); err != nil {
		return err
	}
	tm := NewToolManager(a)
	return tm.InstallTool(name)
}

func (a *App) UpdateTool(name string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("bash", map[string]interface{}{"command": "update tool " + strings.TrimSpace(name), "tool": strings.TrimSpace(name)}); err != nil {
		return err
	}
	tm := NewToolManager(a)
	return tm.UpdateTool(name)
}

func (a *App) CheckToolsStatus() []ToolStatus {
	tm := NewToolManager(a)
	// Check kilo first, then other tools
	tools := []string{"kilo", "claude", "codex", "opencode", "codebuddy", "iflow"}
	statuses := make([]ToolStatus, len(tools))
	for i, name := range tools {
		statuses[i] = tm.GetToolStatus(name)
	}
	return statuses
}
