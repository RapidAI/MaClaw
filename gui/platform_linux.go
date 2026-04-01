//go:build linux
// +build linux

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
	wails_runtime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) platformStartup() {
}

func (a *App) platformShutdown() {
	a.setPowerOptimizationEnabled(false)
}

func (a *App) setPowerOptimizationEnabled(enabled bool) {
	a.powerStateMutex.Lock()
	defer a.powerStateMutex.Unlock()

	if !enabled {
		if a.powerStateProcess != nil && a.powerStateProcess.Process != nil {
			_ = a.powerStateProcess.Process.Kill()
			_, _ = a.powerStateProcess.Process.Wait()
		}
		a.powerStateProcess = nil
		return
	}

	if a.powerStateProcess != nil && a.powerStateProcess.Process != nil && a.powerStateProcess.ProcessState == nil {
		return
	}

	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		a.log("systemd-inhibit not found; power optimization is unavailable on this Linux system")
		return
	}

	cmd := exec.Command(
		"systemd-inhibit",
		"--what=sleep",
		"--why=AICoder remote task active",
		"sh",
		"-c",
		"while kill -0 "+strconv.Itoa(os.Getpid())+" 2>/dev/null; do sleep 60; done",
	)
	if err := cmd.Start(); err != nil {
		a.log("Failed to start systemd-inhibit: " + err.Error())
		return
	}
	a.powerStateProcess = cmd
}

// platformInitConsole is a no-op on Linux (console is already available)
func (a *App) platformInitConsole() {
	// No-op on Linux
}

// RunEnvironmentCheckCLI runs environment check in command-line mode
func (a *App) RunEnvironmentCheckCLI() {
	fmt.Println("Init mode not fully implemented for Linux yet.")
	// TODO: Port logic from CheckEnvironment
}

// CheckEnvironment checks and installs base environment (Node.js)
// Tools are checked and updated in background after base environment is ready
func (a *App) CheckEnvironment(force bool) {
	go func() {
		// If in init mode, always force
		if a.IsInitMode {
			force = true
			a.log(a.tr("Init mode: Forcing environment check (ignoring configuration)."))
		}

		// If .maclaw/data directory doesn't exist, force environment check
		home := a.GetUserHomeDir()
		ccDir := filepath.Join(home, ".maclaw", "data")
		if _, err := os.Stat(ccDir); os.IsNotExist(err) {
			force = true
			a.log(a.tr("Detected missing .maclaw/data directory. Forcing environment check..."))
		}

		if force {
			a.log(a.tr("Forced environment check triggered (ignoring configuration)."))
		} else {
			// Check config first
			config, err := a.LoadConfig()
			if err == nil && config.PauseEnvCheck {
				a.log(a.tr("Skipping base environment check."))
				a.emitEvent("env-check-done")
				// Always start background tool check/update on every startup
				go a.installToolsInBackground()
				return
			}
		}

		a.log(a.tr("Checking base environment..."))

		home, _ = os.UserHomeDir()
		localNodeDir := filepath.Join(home, ".maclaw", "data", "tools")
		localBinDir := filepath.Join(localNodeDir, "bin")

		// 1. Setup PATH
		envPath := os.Getenv("PATH")
		commonPaths := []string{"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}

		// Add local node bin to PATH
		commonPaths = append([]string{localBinDir}, commonPaths...)

		newPathParts := strings.Split(envPath, ":")
		pathChanged := false
		for _, p := range commonPaths {
			if !contains(newPathParts, p) {
				newPathParts = append([]string{p}, newPathParts...) // Prepend for priority
				pathChanged = true
			}
		}

		if pathChanged {
			envPath = strings.Join(newPathParts, ":")
			os.Setenv("PATH", envPath)
			a.log(a.tr("Updated PATH: ") + envPath)
		}

		// 2. Search for Node.js
		a.log(a.tr("Checking Node.js..."))
		nodePath, err := exec.LookPath("node")
		if err != nil {
			for _, p := range commonPaths {
				fullPath := filepath.Join(p, "node")
				if _, err := os.Stat(fullPath); err == nil {
					nodePath = fullPath
					break
				}
			}
		}

		// 3. If still not found, try to install
		if nodePath == "" {
			a.log(a.tr("Node.js not found. Attempting manual installation..."))
			if err := a.installNodeJSManually(localNodeDir); err != nil {
				a.log(a.tr("Manual installation failed: ") + err.Error())
				wails_runtime.EventsEmit(a.ctx, "env-check-done")
				return
			}
			a.log(a.tr("Node.js manually installed to ") + localNodeDir)

			// Ensure the freshly installed bin dir is on PATH so npm lookup succeeds.
			a.updatePathForNode()

			// Re-check for node
			localNodePath := filepath.Join(localBinDir, "node")
			if _, err := os.Stat(localNodePath); err == nil {
				nodePath = localNodePath
			}

			if nodePath == "" {
				a.log(a.tr("Node.js installation completed but binary not found."))
				wails_runtime.EventsEmit(a.ctx, "env-check-done")
				return
			}

			// Verify the installed binary actually runs.
			verifyCmd := exec.Command(nodePath, "--version")
			if out, err := verifyCmd.Output(); err != nil {
				a.log(a.tr("Node.js binary exists but failed to run: ") + err.Error())
				// Remove the broken installation so next launch retries.
				os.RemoveAll(localNodeDir)
				wails_runtime.EventsEmit(a.ctx, "env-check-done")
				return
			} else {
				a.log(a.tr("✓ Node.js installed: %s (%s)", strings.TrimSpace(string(out)), nodePath))
			}
		} else {
			// Get Node.js version
			cmd := exec.Command(nodePath, "--version")
			if out, err := cmd.Output(); err == nil {
				a.log(a.tr("✓ Node.js found: %s (%s)", strings.TrimSpace(string(out)), nodePath))
			} else {
				a.log(a.tr("✓ Node.js found at: ") + nodePath)
			}
		}

		// 4. Check for npm
		a.log(a.tr("Checking npm..."))
		npmPath, err := exec.LookPath("npm")
		if err != nil {
			localNpmPath := filepath.Join(localBinDir, "npm")
			if _, err := os.Stat(localNpmPath); err == nil {
				npmPath = localNpmPath
			}
		}

		if npmPath == "" {
			a.log(a.tr("✗ npm not found. Check Node.js installation."))
			wails_runtime.EventsEmit(a.ctx, "env-check-done")
			return
		}

		// Get npm version
		npmCmd := exec.Command(npmPath, "--version")
		if out, err := npmCmd.Output(); err == nil {
			a.log(a.tr("✓ npm found: %s (%s)", strings.TrimSpace(string(out)), npmPath))
		} else {
			a.log(a.tr("✓ npm found at: ") + npmPath)
		}

		a.log(a.tr("✓ Base environment check complete."))

		// ===== Check and Install Python =====
		a.log(a.tr("Checking Python environment..."))
		pySt := pyenv.Detect()
		if pySt.Available {
			label := "system"
			if pySt.IsPrivate {
				label = "private"
			}
			a.log(a.tr("✓ Python found: v%s (%s) → %s", pySt.Version, label, pySt.PythonPath))
		} else {
			a.log(a.tr("Python >= 3.10 not found. Installing private Python + uv ..."))
			a.emitEvent("python-install-start")
			pySt = pyenv.EnsureEnvironment(func(stage string, pct int, msg string) {
				a.log(fmt.Sprintf("[python-env] [%s] %d%% %s", stage, pct, msg))
				a.emitEvent("python-install-progress", map[string]interface{}{
					"stage": stage, "pct": pct, "msg": msg,
				})
			})
			if pySt.Error != "" {
				a.log(a.tr("WARNING: Python environment setup failed: %s", pySt.Error))
			} else {
				a.log(a.tr("✓ Python %s installed with venv: %s", pySt.Version, pySt.VenvPath))
			}
			a.emitEvent("python-install-done", map[string]interface{}{
				"available": pySt.Available,
				"version":   pySt.Version,
				"error":     pySt.Error,
			})
		}

		// Update config to mark base env check done
		if cfg, err := a.LoadConfig(); err == nil {
			needsSave := false
			if !cfg.EnvCheckDone {
				cfg.EnvCheckDone = true
				cfg.PauseEnvCheck = true
				needsSave = true
			}
			if needsSave {
				a.SaveConfig(cfg)
			}
		}

		a.emitEvent("env-check-done")

		// Always start background tool check/update after base environment is ready
		go a.installToolsInBackground()
	}()
}

// installToolsInBackground checks, installs and updates AI tools in background
// This runs on every application startup
func (a *App) installToolsInBackground() {
	a.log(a.tr("Starting background tool check/update..."))

	home, _ := os.UserHomeDir()
	localBinDir := filepath.Join(home, ".maclaw", "data", "tools", "bin")

	// Find npm
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		localNpmPath := filepath.Join(localBinDir, "npm")
		if _, err := os.Stat(localNpmPath); err == nil {
			npmPath = localNpmPath
		}
	}

	if npmPath == "" {
		a.log(a.tr("npm not found. Cannot install tools in background."))
		return
	}

	tm := NewToolManager(a)
	tools := []string{"kilo", "claude", "gemini", "codex", "opencode", "codebuddy", "iflow", "cursor"}
	expectedPrefix := filepath.Join(home, ".maclaw", "data", "tools")

	for _, tool := range tools {
		// Try to acquire lock for this tool
		if !a.tryLockTool(tool) {
			a.log(a.tr("Background: %s is being installed by user, skipping...", tool))
			continue
		}

		a.log(a.tr("Background: Checking %s...", tool))
		a.emitEvent("tool-checking", tool)
		status := tm.GetToolStatus(tool)

		if !status.Installed {
			a.log(a.tr("Background: %s not found. Installing...", tool))
			a.emitEvent("tool-installing", tool)
			if err := tm.InstallTool(tool); err != nil {
				a.log(a.tr("Background: ERROR: Failed to install %s: %v", tool, err))
			} else {
				a.log(a.tr("Background: %s installed successfully.", tool))
				a.updatePathForNode()
				a.emitEvent("tool-installed", tool)
			}
		} else {
			if !strings.HasPrefix(status.Path, expectedPrefix) {
				a.log(a.tr("Background: WARNING: %s found at %s (not in private directory, skipping)", tool, status.Path))
				a.unlockTool(tool)
				continue
			}

			a.log(a.tr("Background: %s found at %s (version: %s).", tool, status.Path, status.Version))

			// Check for updates (skip for non-npm tools like claude and cursor)
			pkgName := tm.GetPackageName(tool)
			if pkgName == "" {
				// Non-npm tools (claude, cursor) manage their own updates;
				// skip automatic re-download on every startup to avoid unnecessary traffic.
				a.log(a.tr("Background: %s uses native installer, skipping automatic update check.", tool))
			} else {
				a.log(a.tr("Background: Checking for %s updates...", tool))
				latest, err := a.getLatestNpmVersion(npmPath, pkgName)
				if err == nil && latest != "" {
					needsUpdate := compareVersions(status.Version, latest) < 0
					if needsUpdate {
						a.log(a.tr("Background: New version available for %s: %s (current: %s). Updating...", tool, latest, status.Version))
						a.emitEvent("tool-updating", tool)
						if err := tm.UpdateTool(tool); err != nil {
							errStr := err.Error()
							if strings.Contains(errStr, "ripgrep") && strings.Contains(errStr, "403") {
								a.log(a.tr("Background: Warning: %s update completed with ripgrep download issue.", tool))
							} else {
								a.log(a.tr("Background: ERROR: Failed to update %s: %v", tool, err))
							}
						} else {
							a.log(a.tr("Background: %s updated successfully to %s.", tool, latest))
							a.emitEvent("tool-updated", tool)
						}
					} else {
						a.log(a.tr("Background: %s is already up to date (version: %s).", tool, status.Version))
					}
				}
			}
		}

		// Release lock for this tool
		a.unlockTool(tool)
	}

	a.log(a.tr("Background tool check/update complete."))
	a.emitEvent("tools-install-done")
}

// InstallToolOnDemand installs a specific tool when user clicks on it
// Returns error if installation fails
func (a *App) InstallToolOnDemand(toolName string) error {
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
		return nil // Already installed
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

func (a *App) updatePathForNode() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	localBinDir := filepath.Join(home, ".maclaw", "data", "tools", "bin")
	if _, err := os.Stat(localBinDir); err != nil {
		return
	}

	currentPath := os.Getenv("PATH")
	parts := strings.Split(currentPath, string(os.PathListSeparator))
	for _, part := range parts {
		if part == localBinDir {
			return
		}
	}

	newPath := localBinDir + string(os.PathListSeparator) + currentPath
	os.Setenv("PATH", newPath)
	a.log(a.tr("Updated PATH: ") + newPath)
}

func (a *App) installNodeJSManually(targetDir string) error {
	// Simple download and unpack for Linux (assuming x64 for now, or detect)
	nodeVersion := RequiredNodeVersion
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}

	fileName := fmt.Sprintf("node-v%s-linux-%s.tar.gz", nodeVersion, arch)
	url := fmt.Sprintf("https://nodejs.org/dist/v%s/%s", nodeVersion, fileName)
	if strings.HasPrefix(strings.ToLower(a.CurrentLanguage), "zh") {
		url = fmt.Sprintf("https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v%s/%s", nodeVersion, fileName)
	}

	a.log(a.tr("Downloading Node.js from %s...", url))

	tempDir := a.GetTempDir()
	tarPath := filepath.Join(tempDir, fileName)

	// Download
	if err := a.downloadFile(tarPath, url); err != nil {
		return err
	}
	defer os.Remove(tarPath)

	a.log(a.tr("Extracting Node.js..."))

	// Ensure target dir exists
	os.MkdirAll(targetDir, 0755)

	// Use tar command to extract
	cmd := exec.Command("tar", "-xzf", tarPath, "--strip-components=1", "-C", targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar failed: %s\n%s", err, string(output))
	}

	return nil
}

func (a *App) downloadFile(dlPath string, url string) error {
	a.log(fmt.Sprintf("Requesting URL: %s", url))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	transport := &http.Transport{
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error during download: %v", err)
	}
	defer resp.Body.Close()

	if resp.Request.URL.String() != url {
		a.log(fmt.Sprintf("Redirected to: %s", resp.Request.URL.String()))
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	out, err := os.Create(dlPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer out.Close()

	size := resp.ContentLength
	var downloaded int64
	buffer := make([]byte, 32768)
	lastReport := time.Now()

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			out.Write(buffer[:n])
			downloaded += int64(n)
			if size > 0 && time.Since(lastReport) > 500*time.Millisecond {
				percent := float64(downloaded) / float64(size) * 100
				a.log(a.tr("Downloading (%.1f%%): %d/%d bytes", percent, downloaded, size))
				lastReport = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("interrupted download: %v", err)
		}
	}

	out.Sync()
	return nil
}

func (a *App) restartApp() {
	// Basic restart for linux
	executable, err := os.Executable()
	if err != nil {
		return
	}
	exec.Command(executable).Start()
	wails_runtime.Quit(a.ctx)
}

func (a *App) GetDownloadsFolder() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

func (a *App) platformLaunch(binaryName string, yoloMode bool, adminMode bool, pythonEnv string, projectDir string, env map[string]string, modelId string) {
	// Linux launch implementation
	tm := NewToolManager(a)
	status := tm.GetToolStatus(binaryName)
	if !status.Installed {
		// Tool not found, attempt automatic repair/installation
		a.log(fmt.Sprintf("Tool %s not found. Attempting automatic installation...", binaryName))

		// Emit event to show installation progress dialog
		wails_runtime.EventsEmit(a.ctx, "tool-repair-start", binaryName)

		// Check if npm is available first
		npmPath := tm.getNpmPath()
		if npmPath == "" {
			wails_runtime.EventsEmit(a.ctx, "tool-repair-failed", binaryName, a.tr("npm not found. Please run environment check first."))
			a.ShowMessage(a.tr("Installation Error"), a.tr("npm not found. Please run environment check first."))
			return
		}

		// Attempt to install the tool
		err := tm.InstallTool(binaryName)
		if err != nil {
			wails_runtime.EventsEmit(a.ctx, "tool-repair-failed", binaryName, err.Error())
			a.ShowMessage(a.tr("Installation Error"), a.tr("Failed to install %s: %v", binaryName, err))
			return
		}

		// Re-check tool status after installation
		status = tm.GetToolStatus(binaryName)
		if !status.Installed {
			wails_runtime.EventsEmit(a.ctx, "tool-repair-failed", binaryName, a.tr("Installation completed but tool not found"))
			a.ShowMessage(a.tr("Installation Error"), a.tr("Installation completed but %s still not found. Please try running environment check.", binaryName))
			return
		}

		wails_runtime.EventsEmit(a.ctx, "tool-repair-success", binaryName, status.Version)
		a.log(fmt.Sprintf("Tool %s installed successfully. Version: %s", binaryName, status.Version))
	}

	cmdArgs := []string{}
	if binaryName == "codebuddy" && modelId != "" {
		cmdArgs = append(cmdArgs, "--model", modelId)
	}

	if yoloMode {
		switch binaryName {
		case "claude":
			cmdArgs = append(cmdArgs, "--dangerously-skip-permissions")
		case "gemini":
			cmdArgs = append(cmdArgs, "--yolo")
		case "codex":
			cmdArgs = append(cmdArgs, "--full-auto")
		case "codebuddy":
			cmdArgs = append(cmdArgs, "-y")
		case "iflow":
			cmdArgs = append(cmdArgs, "-y")
		}
	}

	// Create shell script wrapper
	scriptPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("maclaw_launch_%d.sh", time.Now().UnixNano()))
	scriptContent := "#!/bin/bash\n"
	scriptContent += fmt.Sprintf("cd \"%s\"\n", projectDir)
	for k, v := range env {
		scriptContent += fmt.Sprintf("export %s=\"%s\"\n", k, v)
	}

	// Add local node to PATH
	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".maclaw", "data", "tools", "bin")
	scriptContent += fmt.Sprintf("export PATH=\"%s:$PATH\"\n", localBin)

	scriptContent += fmt.Sprintf("\"%s\" %s\n", status.Path, strings.Join(cmdArgs, " "))
	scriptContent += "echo 'Press Enter to close...'\nread\n"

	os.WriteFile(scriptPath, []byte(scriptContent), 0755)

	// Try to open terminal
	terminals := []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xterm"}
	var cmd *exec.Cmd
	for _, t := range terminals {
		if _, err := exec.LookPath(t); err == nil {
			if t == "gnome-terminal" {
				cmd = exec.Command(t, "--", scriptPath)
			} else {
				cmd = exec.Command(t, "-e", scriptPath)
			}
			break
		}
	}

	if cmd != nil {
		cmd.Start()
	} else {
		a.log("No supported terminal emulator found.")
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func createVersionCmd(path string) *exec.Cmd {
	return exec.Command(path, "--version")
}

func createHiddenCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func createNpmInstallCmd(npmPath string, args []string) *exec.Cmd {
	return exec.Command(npmPath, args...)
}

func (a *App) LaunchInstallerAndExit(installerPath string) error {
	cmd := exec.Command("xdg-open", installerPath)
	if err := cmd.Start(); err != nil {
		return err
	}
	wails_runtime.Quit(a.ctx)
	return nil
}

func createCondaEnvListCmd(condaPath string) *exec.Cmd {
	return exec.Command(condaPath, "env", "list")
}

func getWindowsVersionHidden() string {
	return ""
}

// isWindowsTerminalAvailable returns false on Linux (Windows Terminal is Windows-only)
func (a *App) syncToSystemEnv(config AppConfig) {
}

func (a *App) isWindowsTerminalAvailable() bool {
	return false
}

// IsWindowsTerminalAvailable is exported for frontend to check availability
func (a *App) IsWindowsTerminalAvailable() bool {
	return false
}
