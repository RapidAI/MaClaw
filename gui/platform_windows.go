//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
)

// compareVersions compares two semantic version strings
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func (a *App) compareVersions(v1, v2 string) int {
	cleanVersion := func(v string) string {
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "v")
		v = strings.TrimPrefix(v, "V")
		if idx := strings.Index(v, " "); idx > 0 {
			v = v[:idx]
		}
		return v
	}

	v1 = cleanVersion(v1)
	v2 = cleanVersion(v2)

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			numStr := parts1[i]
			for j, c := range numStr {
				if c < '0' || c > '9' {
					numStr = numStr[:j]
					break
				}
			}
			if numStr != "" {
				n1, _ = strconv.Atoi(numStr)
			}
		}
		if i < len(parts2) {
			numStr := parts2[i]
			for j, c := range numStr {
				if c < '0' || c > '9' {
					numStr = numStr[:j]
					break
				}
			}
			if numStr != "" {
				n2, _ = strconv.Atoi(numStr)
			}
		}
		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}
	return 0
}

func (a *App) platformStartup() {
}

func (a *App) platformShutdown() {
	a.allowSystemSleep()
}

func (a *App) setPowerOptimizationEnabled(enabled bool) {
	if enabled {
		a.preventSystemSleep()
		return
	}
	a.allowSystemSleep()
}

func (a *App) preventSystemSleep() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setThreadExecutionState := kernel32.NewProc("SetThreadExecutionState")
	// ES_DISPLAY_REQUIRED keeps the display on, preventing DWM from suspending
	// desktop composition. Without it, screenshots return black after the
	// display auto-off timeout, even though the system stays awake.
	setThreadExecutionState.Call(uintptr(esContinuous | esSystemRequired | esDisplayRequired))
}

func (a *App) allowSystemSleep() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setThreadExecutionState := kernel32.NewProc("SetThreadExecutionState")
	setThreadExecutionState.Call(uintptr(esContinuous))
}

func (a *App) platformInitConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	allocConsole := kernel32.NewProc("AllocConsole")
	allocConsole.Call()

	setConsoleTitle := kernel32.NewProc("SetConsoleTitleW")
	title, _ := syscall.UTF16PtrFromString("MaClaw - Environment Setup")
	setConsoleTitle.Call(uintptr(unsafe.Pointer(title)))
}

// RunEnvironmentCheckCLI runs environment check in command-line mode (synchronous, no GUI events)
// Installation order: Node.js -Git -VC++ Runtime -AI Tools
func (a *App) RunEnvironmentCheckCLI() {
	fmt.Println("\n========================================")
	fmt.Println("Environment Setup - Step by Step")
	fmt.Println("========================================")

	// ===== STEP 1: Node.js Installation =====
	fmt.Println("\n[1/4] Step 1: Node.js Installation")
	fmt.Println("--------------------------------------")

	nodeVersion := ""
	nodeCmd := exec.Command("node", "--version")
	nodeCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	if out, err := nodeCmd.Output(); err == nil {
		nodeVersion = strings.TrimSpace(string(out))
		fmt.Printf("-Node.js is already installed: %s\n", nodeVersion)
	} else {
		fmt.Println("Node.js not found. Installing...")
		if err := a.installNodeJSCLI(); err != nil {
			fmt.Printf("-ERROR: Failed to install Node.js: %v\n", err)
			fmt.Println("\nEnvironment setup failed. Please install Node.js manually.")
			return
		}
		fmt.Println("Verifying Node.js installation...")
		a.updatePathForNode()
		time.Sleep(2 * time.Second)

		nodeCmd = exec.Command("node", "--version")
		nodeCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
		if out, err := nodeCmd.Output(); err == nil {
			nodeVersion = strings.TrimSpace(string(out))
			fmt.Printf("-Node.js installed and verified successfully: %s\n", nodeVersion)
		} else {
			fmt.Printf("-ERROR: Node.js installation verification failed: %v\n", err)
			return
		}
	}

	// Verify npm
	fmt.Println("Verifying npm availability...")
	a.updatePathForNode()

	var npmExec string
	var npmVersion string
	maxRetries := 10
	npmReady := false

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			fmt.Printf("  Retry %d/%d...\n", i+1, maxRetries)
			time.Sleep(2 * time.Second)
		}
		var err error
		npmExec, err = exec.LookPath("npm")
		if err != nil {
			npmExec, err = exec.LookPath("npm.cmd")
		}
		if err == nil && npmExec != "" {
			npmTestCmd := exec.Command(npmExec, "--version")
			npmTestCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
			if out, err := npmTestCmd.Output(); err == nil {
				npmVersion = strings.TrimSpace(string(out))
				fmt.Printf("-npm verified successfully: %s (version: %s)\n", npmExec, npmVersion)
				npmReady = true
				break
			}
		}
		a.updatePathForNode()
	}

	if !npmReady {
		fmt.Printf("-ERROR: npm not available after %d attempts\n", maxRetries)
		return
	}

	// ===== STEP 2: Git Installation =====
	fmt.Println("\n[2/4] Step 2: Git Installation")
	fmt.Println("--------------------------------------")

	gitInstalled := false
	gitVersion := ""

	if gitPath, err := exec.LookPath("git"); err == nil {
		gitCmd := exec.Command(gitPath, "--version")
		gitCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
		if out, err := gitCmd.Output(); err == nil {
			gitVersion = strings.TrimSpace(string(out))
			fmt.Printf("-Git is already installed: %s\n", gitVersion)
			gitInstalled = true
		}
	} else {
		if _, err := os.Stat(`C:\Program Files\Git\cmd\git.exe`); err == nil {
			a.updatePathForGit()
			fmt.Println("-Git found in standard location.")
			gitInstalled = true
		}
	}

	if !gitInstalled {
		fmt.Println("Git not found. Installing...")
		if err := a.installGitBashCLI(); err != nil {
			fmt.Printf("-ERROR: Failed to install Git: %v\n", err)
			fmt.Println("Git installation failed. AI tools will be installed, but some features may not work.")
		} else {
			fmt.Println("Verifying Git installation...")
			a.updatePathForGit()
			time.Sleep(2 * time.Second)

			if gitPath, err := exec.LookPath("git"); err == nil {
				gitCmd := exec.Command(gitPath, "--version")
				gitCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
				if out, err := gitCmd.Output(); err == nil {
					gitVersion = strings.TrimSpace(string(out))
					fmt.Printf("-Git installed and verified successfully: %s\n", gitVersion)
					gitInstalled = true
				}
			}
			if !gitInstalled {
				fmt.Println("-WARNING: Git installation verification failed.")
			}
		}
	}

	// ===== STEP 3: Visual C++ Redistributable =====
	fmt.Println("\n[3/4] Step 3: Visual C++ Redistributable")
	fmt.Println("--------------------------------------")
	fmt.Println("Checking Visual C++ Redistributable (required for codex)...")

	arch := os.Getenv("PROCESSOR_ARCHITECTURE")
	fmt.Printf("System Architecture: %s\n", arch)

	isInstalled := a.isVCRedistInstalled()
	fmt.Printf("Registry Check Result: %v\n", isInstalled)

	if isInstalled {
		fmt.Println("-Visual C++ Redistributable is already installed")
	} else {
		fmt.Println("Visual C++ Redistributable not found. Installing...")
		if err := a.installVCRedist(); err != nil {
			fmt.Printf("-WARNING: Failed to install VC Redistributable: %v\n", err)
			fmt.Println("  Some tools like codex may not work properly without it.")
		} else {
			fmt.Println("-Visual C++ Redistributable installed successfully")
		}
	}

	// ===== STEP 4: Local Node Environment Setup =====
	fmt.Println("\n[4/4] Step 4: Local Node.js Environment Setup")
	fmt.Println("--------------------------------------")
	a.ensureLocalNodeBinary()
	fmt.Println("-Local Node.js environment configured")

	// Base environment setup complete
	fmt.Println("\n========================================")
	fmt.Println("Base Environment Setup Complete")
	fmt.Println("========================================")
	fmt.Printf("Node.js: %s\n", nodeVersion)
	if gitInstalled {
		fmt.Printf("Git: %s\n", gitVersion)
	} else {
		fmt.Println("Git: Not installed (optional)")
	}

	// Update config
	if cfg, err := a.LoadConfig(); err == nil {
		cfg.EnvCheckDone = true
		cfg.PauseEnvCheck = true
		a.SaveConfig(cfg)
	}

	fmt.Println("\n-Base environment setup completed!")
	fmt.Println("AI tools will be installed in background when the application starts.")
}

// CheckEnvironment checks and installs base environment (Node.js, Git, VC++ Runtime)
// Tools are checked and updated in background after base environment is ready
func (a *App) CheckEnvironment(force bool) {
	go func() {
		if a.IsInitMode {
			force = true
			a.log(a.tr("Init mode: Forcing environment check (ignoring configuration)."))
		}

		home := a.GetUserHomeDir()
		ccDir := filepath.Join(home, ".maclaw", "data")
		if _, err := os.Stat(ccDir); os.IsNotExist(err) {
			force = true
			a.log(a.tr("Detected missing .maclaw/data directory. Forcing environment check..."))
		}

		if force {
			a.log(a.tr("Forced environment check triggered (ignoring configuration)."))
			a.log(a.tr("Checking base environment..."))
		} else {
			config, err := a.LoadConfig()
			if err == nil {
				if config.PauseEnvCheck && config.EnvCheckDone {
					a.log(a.tr("Skipping base environment check."))
					a.emitEvent("env-check-done")
					// Always start background tool check/update on every startup
					go a.installToolsInBackground()
					return
				}
			}
		}

		// ===== Check and Install Visual C++ Redistributable =====
		a.log(a.tr("Checking Visual C++ Redistributable..."))
		if !a.isVCRedistInstalled() {
			a.log(a.tr("Visual C++ Redistributable not found. Installing..."))
			if err := a.installVCRedist(); err != nil {
				a.log(a.tr("WARNING: Failed to install VC Redistributable: %v", err))
			} else {
				a.log(a.tr("-Visual C++ Redistributable installed successfully."))
			}
		} else {
			a.log(a.tr("-Visual C++ Redistributable is already installed."))
		}

		a.log(a.tr("Checking Node.js..."))

		nodeCmd := exec.Command("node", "--version")
		nodeCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		nodeOutput, nodeErr := nodeCmd.Output()
		nodeInstalled := nodeErr == nil

		if !nodeInstalled {
			a.installMutex.Lock()
			if a.installingNode {
				a.log(a.tr("Node.js installation already in progress, waiting for completion..."))
				a.installMutex.Unlock()
				select {
				case <-a.nodeInstallDone:
					a.log(a.tr("Node.js installation completed by another process."))
					nodeInstalled = true
				case <-time.After(10 * time.Minute):
					a.log(a.tr("ERROR: Timeout waiting for Node.js installation to complete."))
					a.emitEvent("env-check-done")
					return
				}
			} else {
				a.installingNode = true
				a.installMutex.Unlock()

				a.log(a.tr("Node.js not found. Downloading and installing..."))
				if err := a.installNodeJS(); err != nil {
					a.log(a.tr("Failed to install Node.js: ") + err.Error())
					a.installMutex.Lock()
					a.installingNode = false
					a.installMutex.Unlock()
					a.emitEvent("env-check-done")
					return
				}
				a.log(a.tr("Node.js installed successfully."))

				a.installMutex.Lock()
				a.installingNode = false
				a.installMutex.Unlock()

				select {
				case a.nodeInstallDone <- true:
				default:
				}
				nodeInstalled = true
			}
		} else {
			a.log(a.tr("-Node.js found: %s", strings.TrimSpace(string(nodeOutput))))
			nodeInstalled = true
		}

		if !nodeInstalled {
			a.log(a.tr("ERROR: Node.js is not available. Cannot proceed."))
			a.emitEvent("env-check-done")
			return
		}

		a.updatePathForNode()

		// Check for Git
		a.log(a.tr("Checking Git..."))
		if gitPath, err := exec.LookPath("git"); err == nil {
			gitCmd := exec.Command(gitPath, "--version")
			gitCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if out, err := gitCmd.Output(); err == nil {
				a.log(a.tr("-Git found: %s", strings.TrimSpace(string(out))))
			} else {
				a.log(a.tr("-Git found at: %s", gitPath))
			}
		} else {
			gitFound := false
			if _, err := os.Stat(`C:\Program Files\Git\cmd\git.exe`); err == nil {
				gitFound = true
			}

			if gitFound {
				a.updatePathForGit()
				a.log(a.tr("-Git found in standard location."))
			} else {
				a.installMutex.Lock()
				if a.installingGit {
					a.log(a.tr("Git installation already in progress, skipping..."))
					a.installMutex.Unlock()
					time.Sleep(5 * time.Second)
				} else {
					a.installingGit = true
					a.installMutex.Unlock()

					a.log(a.tr("Git not found. Downloading and installing..."))
					if err := a.installGitBash(); err != nil {
						a.log("Failed to install Git: " + err.Error())
						a.installMutex.Lock()
						a.installingGit = false
						a.installMutex.Unlock()
					} else {
						a.log(a.tr("-Git installed successfully."))
						a.updatePathForGit()
						a.installMutex.Lock()
						a.installingGit = false
						a.installMutex.Unlock()
					}
				}
			}
		}

		a.ensureLocalNodeBinary()

		// ===== Check and Install Python =====
		a.log(a.tr("Checking Python environment..."))
		pySt := pyenv.Detect()
		if pySt.Available {
			label := "system"
			if pySt.IsPrivate {
				label = "private"
			}
			a.log(a.tr("-Python found: v%s (%s) → %s", pySt.Version, label, pySt.PythonPath))
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
				a.log(a.tr("-Python %s installed with venv: %s", pySt.Version, pySt.VenvPath))
			}
			a.emitEvent("python-install-done", map[string]interface{}{
				"available": pySt.Available,
				"version":   pySt.Version,
				"error":     pySt.Error,
			})
		}

		a.log(a.tr("-Base environment check complete."))

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

	// Verify npm is available
	a.log(a.tr("Verifying npm is available before installing AI tools..."))

	var npmExec string
	var npmReady bool
	maxRetries := 10
	retryDelay := 3 * time.Second

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			a.log(a.tr("Retrying npm verification (attempt %d/%d)...", i+1, maxRetries))
			time.Sleep(retryDelay)
		}

		var err error
		npmExec, err = exec.LookPath("npm")
		if err != nil {
			npmExec, err = exec.LookPath("npm.cmd")
		}

		if err != nil || npmExec == "" {
			a.log(a.tr("npm not found in PATH, updating environment..."))
			a.updatePathForNode()
			continue
		}

		npmTestCmd := exec.Command(npmExec, "--version")
		npmTestCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := npmTestCmd.Run(); err != nil {
			a.log(a.tr("npm command test failed: %v", err))
			continue
		}

		npmReady = true
		break
	}

	if !npmReady || npmExec == "" {
		a.log(a.tr("ERROR: npm not found after %d attempts. Cannot install AI tools.", maxRetries))
		return
	}

	a.log(a.tr("npm verified successfully: %s", npmExec))

	tm := NewToolManager(a)
	tools := []string{"kilo", "claude", "gemini", "codex", "opencode", "codebuddy", "iflow"}
	home, _ := os.UserHomeDir()
	expectedPrefix := filepath.Join(home, ".maclaw", "data", "tools")

	for _, tool := range tools {
		// Try to acquire lock for this tool
		if !a.tryLockTool(tool) {
			a.log(a.tr("Background: %s is being installed by user, skipping...", tool))
			continue
		}

		a.log(a.tr("Background: Checking %s in private directory...", tool))
		a.emitEvent("tool-checking", tool)
		status := tm.GetToolStatus(tool)

		if !status.Installed {
			a.log(a.tr("Background: %s not found in private directory. Installing...", tool))
			a.emitEvent("tool-installing", tool)
			if err := tm.InstallTool(tool); err != nil {
				a.log(a.tr("Background: ERROR: Failed to install %s: %v", tool, err))
			} else {
				a.log(a.tr("Background: %s installed successfully to private directory.", tool))
				a.updatePathForNode()
				a.emitEvent("tool-installed", tool)
			}
		} else {
			if !strings.HasPrefix(status.Path, expectedPrefix) {
				a.log(a.tr("Background: WARNING: %s found at %s (not in private directory, skipping)", tool, status.Path))
				a.unlockTool(tool)
				continue
			}

			a.log(a.tr("Background: %s found in private directory at %s (version: %s).", tool, status.Path, status.Version))

			// Check for updates (skip for non-npm tools like claude and cursor)
			pkgName := tm.GetPackageName(tool)
			if pkgName == "" {
				a.log(a.tr("Background: %s uses native installer, skipping automatic update check.", tool))
			} else {
				a.log(a.tr("Background: Checking for %s updates...", tool))
				latest, err := a.getLatestNpmVersion(npmExec, pkgName)
				if err == nil && latest != "" {
					needsUpdate := a.compareVersions(status.Version, latest) < 0
					if needsUpdate {
						a.log(a.tr("Background: New version available for %s: %s (current: %s). Updating...", tool, latest, status.Version))
						a.emitEvent("tool-updating", tool)
						if err := tm.UpdateTool(tool); err != nil {
							errStr := err.Error()
							if strings.Contains(errStr, "ripgrep") && strings.Contains(errStr, "403") {
								a.log(a.tr("Background: Warning: %s update completed with ripgrep download issue.", tool))
							} else if strings.Contains(errStr, "EPERM") || strings.Contains(errStr, "EBUSY") {
								a.log(a.tr("Background: Warning: %s update failed due to file lock.", tool))
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

func (a *App) installNodeJSCLI() error {
	arch := os.Getenv("PROCESSOR_ARCHITECTURE")
	nodeArch := "x64"
	if arch == "ARM64" || os.Getenv("PROCESSOR_ARCHITEW6432") == "ARM64" {
		nodeArch = "arm64"
	}

	nodeVersion := RequiredNodeVersion
	fileName := fmt.Sprintf("node-v%s-%s.msi", nodeVersion, nodeArch)
	downloadURL := fmt.Sprintf("https://nodejs.org/dist/v%s/%s", nodeVersion, fileName)
	fmt.Printf("  Downloading from: %s\n", downloadURL)

	client := &http.Client{Timeout: 10 * time.Second}
	headReq, _ := http.NewRequest("HEAD", downloadURL, nil)
	headReq.Header.Set("User-Agent", "Mozilla/5.0")
	headResp, err := client.Do(headReq)
	if err != nil || headResp.StatusCode != http.StatusOK {
		return fmt.Errorf("installer not accessible")
	}
	headResp.Body.Close()

	tempDir := a.GetTempDir()
	msiPath := filepath.Join(tempDir, fileName)

	fmt.Println("  Downloading Node.js installer...")
	if err := a.downloadFileCLI(msiPath, downloadURL); err != nil {
		return err
	}

	time.Sleep(500 * time.Millisecond)

	fmt.Println("  Installing Node.js (this may take a few minutes)...")
	fmt.Println("  You will be prompted for administrator permission. Please accept to continue.")

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb := syscall.StringToUTF16Ptr("runas")
	file := syscall.StringToUTF16Ptr("msiexec.exe")
	params := syscall.StringToUTF16Ptr(fmt.Sprintf("/i \"%s\" /qb ALLUSERS=1", msiPath))
	dir := syscall.StringToUTF16Ptr("")

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(syscall.SW_SHOW),
	)

	if ret <= 32 {
		return fmt.Errorf("failed to launch installer with admin privileges (error code: %d)", ret)
	}

	fmt.Println("  Waiting for installation to complete...")
	nodePath := `C:\Program Files\nodejs\node.exe`
	maxWaitTime := 5 * time.Minute
	checkInterval := 2 * time.Second
	elapsed := time.Duration(0)

	for elapsed < maxWaitTime {
		if _, err := os.Stat(nodePath); err == nil {
			fmt.Println("  Installation completed successfully.")
			fmt.Println("  Verifying npm availability...")
			npmReady := false
			npmWaitTime := 30 * time.Second
			npmCheckInterval := 1 * time.Second
			npmElapsed := time.Duration(0)

			for npmElapsed < npmWaitTime {
				npmCmd := exec.Command("npm", "--version")
				npmCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
				if err := npmCmd.Run(); err == nil {
					fmt.Println("  npm is ready.")
					npmReady = true
					break
				}
				time.Sleep(npmCheckInterval)
				npmElapsed += npmCheckInterval
			}

			if !npmReady {
				fmt.Println("  Warning: npm verification timed out, but continuing anyway...")
			}

			time.Sleep(2 * time.Second)
			go func() {
				time.Sleep(5 * time.Second)
				os.Remove(msiPath)
			}()
			return nil
		}
		time.Sleep(checkInterval)
		elapsed += checkInterval
	}

	fmt.Println("  Warning: Installation verification timed out.")
	os.Remove(msiPath)
	return nil
}

func (a *App) installGitBashCLI() error {
	gitVersion := "2.52.0"
	fullVersion := "v2.52.0.windows.1"
	fileName := fmt.Sprintf("Git-%s-64-bit.exe", gitVersion)

	downloadURL := fmt.Sprintf("https://github.com/git-for-windows/git/releases/download/%s/%s", fullVersion, fileName)
	fmt.Printf("  Downloading from: %s\n", downloadURL)

	tempDir := a.GetTempDir()
	exePath := filepath.Join(tempDir, fileName)

	if err := a.downloadFileCLI(exePath, downloadURL); err != nil {
		return err
	}

	time.Sleep(500 * time.Millisecond)

	fmt.Println("  Installing Git (this may take a few minutes)...")
	fmt.Println("  You will be prompted for administrator permission. Please accept to continue.")

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb := syscall.StringToUTF16Ptr("runas")
	file := syscall.StringToUTF16Ptr(exePath)
	params := syscall.StringToUTF16Ptr("/VERYSILENT /NORESTART /NOCANCEL /SP-")
	dir := syscall.StringToUTF16Ptr("")

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(syscall.SW_SHOW),
	)

	if ret <= 32 {
		return fmt.Errorf("failed to launch installer with admin privileges (error code: %d)", ret)
	}

	fmt.Println("  Waiting for installation to complete...")
	gitPath := `C:\Program Files\Git\cmd\git.exe`
	maxWaitTime := 5 * time.Minute
	checkInterval := 2 * time.Second
	elapsed := time.Duration(0)

	for elapsed < maxWaitTime {
		if _, err := os.Stat(gitPath); err == nil {
			fmt.Println("  Installation completed successfully.")
			time.Sleep(2 * time.Second)
			go func() {
				time.Sleep(5 * time.Second)
				os.Remove(exePath)
			}()
			return nil
		}
		time.Sleep(checkInterval)
		elapsed += checkInterval
	}

	fmt.Println("  Warning: Installation verification timed out.")
	os.Remove(exePath)
	return nil
}

func (a *App) downloadFileCLI(filepath string, url string) error {
	fmt.Printf("  Requesting URL: %s\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	transport := &http.Transport{
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DisableKeepAlives:     true,
	}

	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Request.URL.String() != url {
		fmt.Printf("  Redirected to: %s\n", resp.Request.URL.String())
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	size := resp.ContentLength
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	var downloaded int64
	buffer := make([]byte, 32768)
	lastReport := time.Now()

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			out.Write(buffer[:n])
			downloaded += int64(n)
			if size > 0 && time.Since(lastReport) > 1*time.Second {
				percent := float64(downloaded) / float64(size) * 100
				fmt.Printf("  Progress: %.1f%% (%d/%d bytes)\n", percent, downloaded, size)
				lastReport = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	out.Sync()
	fmt.Println("  Download complete.")
	return nil
}

func (a *App) updatePathForNode() {
	nodePath := `C:\Program Files\nodejs`
	npmPath := filepath.Join(os.Getenv("AppData"), "npm")
	home, _ := os.UserHomeDir()
	localToolPath := filepath.Join(home, ".maclaw", "data", "tools")
	oldToolPath := filepath.Join(home, ".cceasy", "node") // legacy path, keep for cleanup

	currentPath := os.Getenv("PATH")
	if strings.Contains(strings.ToLower(currentPath), strings.ToLower(oldToolPath)) {
		parts := strings.Split(currentPath, string(os.PathListSeparator))
		var newParts []string
		for _, part := range parts {
			if !strings.EqualFold(part, oldToolPath) {
				newParts = append(newParts, part)
			}
		}
		currentPath = strings.Join(newParts, string(os.PathListSeparator))
	}

	newPath := currentPath

	if _, err := os.Stat(nodePath); err == nil {
		if !strings.Contains(strings.ToLower(currentPath), strings.ToLower(nodePath)) {
			newPath = nodePath + string(os.PathListSeparator) + newPath
		}
	}

	if _, err := os.Stat(npmPath); err == nil {
		if !strings.Contains(strings.ToLower(currentPath), strings.ToLower(npmPath)) {
			newPath = npmPath + string(os.PathListSeparator) + newPath
		}
	}

	if _, err := os.Stat(localToolPath); err == nil {
		if !strings.Contains(strings.ToLower(currentPath), strings.ToLower(localToolPath)) {
			newPath = localToolPath + string(os.PathListSeparator) + newPath
		}
	}

	if newPath != currentPath {
		os.Setenv("PATH", newPath)
		a.log(a.tr("Updated PATH environment variable: ") + newPath)
	}
}

func (a *App) installNodeJS() error {
	arch := os.Getenv("PROCESSOR_ARCHITECTURE")
	nodeArch := "x64"
	if arch == "ARM64" || os.Getenv("PROCESSOR_ARCHITEW6432") == "ARM64" {
		nodeArch = "arm64"
	}

	nodeVersion := RequiredNodeVersion
	fileName := fmt.Sprintf("node-v%s-%s.msi", nodeVersion, nodeArch)

	officialURL := fmt.Sprintf("https://nodejs.org/dist/v%s/%s", nodeVersion, fileName)
	downloadURL := officialURL

	if strings.HasPrefix(strings.ToLower(a.CurrentLanguage), "zh") && nodeArch != "arm64" {
		mirrorURL := fmt.Sprintf("https://mirrors.tuna.tsinghua.edu.cn/nodejs-release/v%s/%s", nodeVersion, fileName)
		a.log(a.tr("Trying China mirror for faster download..."))

		client := &http.Client{Timeout: 10 * time.Second}
		headReq, _ := http.NewRequest("HEAD", mirrorURL, nil)
		headReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		headResp, err := client.Do(headReq)
		if err == nil && headResp.StatusCode == http.StatusOK {
			downloadURL = mirrorURL
			a.log(a.tr("Using China mirror: %s", mirrorURL))
		} else {
			a.log(a.tr("China mirror not available for this version, falling back to official source"))
		}
		if headResp != nil {
			headResp.Body.Close()
		}
	}

	a.log(a.tr("Downloading Node.js %s for %s...", nodeVersion, nodeArch))

	client := &http.Client{Timeout: 10 * time.Second}
	headReq, _ := http.NewRequest("HEAD", downloadURL, nil)
	headReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	headResp, err := client.Do(headReq)
	if err != nil || headResp.StatusCode != http.StatusOK {
		status := "Unknown"
		if headResp != nil {
			status = headResp.Status
		}
		return fmt.Errorf("%s", a.tr("Node.js installer is not accessible (Status: %s). Please check your internet connection or mirror availability.", status))
	}
	headResp.Body.Close()

	tempDir := a.GetTempDir()
	msiPath := filepath.Join(tempDir, fileName)

	if err := a.downloadFile(msiPath, downloadURL); err != nil {
		return fmt.Errorf("error downloading Node.js installer: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	a.log(a.tr("Installing Node.js (this may take a moment, please grant administrator permission if prompted)..."))

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb := syscall.StringToUTF16Ptr("runas")
	file := syscall.StringToUTF16Ptr("msiexec.exe")
	params := syscall.StringToUTF16Ptr(fmt.Sprintf("/i \"%s\" /qb ALLUSERS=1", msiPath))
	dir := syscall.StringToUTF16Ptr("")

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(syscall.SW_HIDE),
	)

	if ret <= 32 {
		return fmt.Errorf("failed to launch Node.js installer with admin privileges (error code: %d). Please run the application as administrator.", ret)
	}

	a.log(a.tr("Waiting for Node.js installation to complete..."))
	nodePath := `C:\Program Files\nodejs\node.exe`
	maxWaitTime := 5 * time.Minute
	checkInterval := 2 * time.Second
	elapsed := time.Duration(0)

	for elapsed < maxWaitTime {
		if _, err := os.Stat(nodePath); err == nil {
			a.log(a.tr("Node.js installation completed successfully."))

			a.log(a.tr("Verifying npm availability..."))
			npmReady := false
			npmWaitTime := 30 * time.Second
			npmCheckInterval := 1 * time.Second
			npmElapsed := time.Duration(0)

			for npmElapsed < npmWaitTime {
				npmCmd := exec.Command("npm", "--version")
				npmCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				if err := npmCmd.Run(); err == nil {
					a.log(a.tr("npm is ready."))
					npmReady = true
					break
				}
				time.Sleep(npmCheckInterval)
				npmElapsed += npmCheckInterval
			}

			if !npmReady {
				a.log(a.tr("Warning: npm verification timed out, but continuing anyway..."))
			}

			time.Sleep(2 * time.Second)

			go func() {
				time.Sleep(5 * time.Second)
				os.Remove(msiPath)
			}()

			return nil
		}
		time.Sleep(checkInterval)
		elapsed += checkInterval
	}

	a.log(a.tr("Warning: Node.js installation verification timed out. Please check if Node.js was installed correctly."))
	os.Remove(msiPath)
	return nil
}

func (a *App) updatePathForGit() {
	gitPaths := []string{
		`C:\Program Files\Git\cmd`,
		`C:\Program Files\Git\bin`,
	}

	currentPath := os.Getenv("PATH")
	newPath := currentPath

	for _, path := range gitPaths {
		if _, err := os.Stat(path); err == nil {
			if !strings.Contains(strings.ToLower(currentPath), strings.ToLower(path)) {
				newPath = path + string(os.PathListSeparator) + newPath
			}
		}
	}

	if newPath != currentPath {
		os.Setenv("PATH", newPath)
		a.log(a.tr("Updated PATH environment variable for Git."))
	}
}

func (a *App) isVCRedistInstalled() bool {
	arch := os.Getenv("PROCESSOR_ARCHITECTURE")
	var regPath string

	if arch == "ARM64" || os.Getenv("PROCESSOR_ARCHITEW6432") == "ARM64" {
		regPath = `SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\ARM64`
	} else {
		regPath = `SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64`
	}

	a.log(fmt.Sprintf("VC Redist check: Checking registry path: HKLM\\%s", regPath))

	cmd := exec.Command("reg", "query", fmt.Sprintf("HKLM\\%s", regPath), "/v", "Installed")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()

	if err != nil {
		a.log(fmt.Sprintf("VC Redist check: Registry key not found or error: %v", err))
		return false
	}

	outputStr := string(output)
	a.log(fmt.Sprintf("VC Redist check: Registry output: %s", outputStr))

	if strings.Contains(outputStr, "0x1") {
		a.log("VC Redist check: Found installed (0x1)")
		return true
	}

	a.log("VC Redist check: Not installed or value not 0x1")
	return false
}

func (a *App) installVCRedist() error {
	arch := os.Getenv("PROCESSOR_ARCHITECTURE")
	var downloadURL string
	var fileName string

	if arch == "ARM64" || os.Getenv("PROCESSOR_ARCHITEW6432") == "ARM64" {
		downloadURL = "https://aka.ms/vc14/vc_redist.arm64.exe"
		fileName = "vc_redist.arm64.exe"
	} else {
		downloadURL = "https://aka.ms/vc14/vc_redist.x64.exe"
		fileName = "vc_redist.x64.exe"
	}

	fmt.Printf("  -Downloading from: %s\n", downloadURL)
	a.log(a.tr("Downloading Visual C++ Redistributable..."))

	tempDir := a.GetTempDir()
	exePath := filepath.Join(tempDir, fileName)
	fmt.Printf("  -Download path: %s\n", exePath)

	if err := a.downloadFile(exePath, downloadURL); err != nil {
		errMsg := fmt.Sprintf("failed to download VC Redist: %v", err)
		fmt.Printf("  -%s\n", errMsg)
		return errors.New(errMsg)
	}

	fmt.Println("  -Download completed")
	time.Sleep(500 * time.Millisecond)

	if _, err := os.Stat(exePath); os.IsNotExist(err) {
		errMsg := "downloaded file not found"
		fmt.Printf("  -%s\n", errMsg)
		return errors.New(errMsg)
	}

	fmt.Println("  -Starting installation (requires admin privileges)...")
	a.log(a.tr("Installing Visual C++ Redistributable..."))

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb := syscall.StringToUTF16Ptr("runas")
	file := syscall.StringToUTF16Ptr(exePath)
	params := syscall.StringToUTF16Ptr("/install /quiet /norestart")
	dir := syscall.StringToUTF16Ptr("")

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(syscall.SW_SHOW),
	)

	if ret <= 32 {
		errMsg := fmt.Sprintf("failed to launch VC Redist installer with admin privileges (error code: %d)", ret)
		fmt.Printf("  -%s\n", errMsg)
		return errors.New(errMsg)
	}

	fmt.Println("  -Installer launched successfully")
	fmt.Println("  -Waiting for installation to complete...")
	a.log(a.tr("Waiting for installation to complete..."))

	maxRetries := 10
	installed := false
	for i := 0; i < maxRetries; i++ {
		time.Sleep(5 * time.Second)

		if a.isVCRedistInstalled() {
			installed = true
			fmt.Println("  -Installation verified successfully")
			a.log(a.tr("-Visual C++ Redistributable installed and verified successfully."))
			break
		}

		if i < maxRetries-1 {
			fmt.Printf("  -Still waiting... (%d/%d)\n", i+2, maxRetries)
		}
	}

	go func() {
		time.Sleep(5 * time.Second)
		os.Remove(exePath)
	}()

	if !installed {
		errMsg := fmt.Sprintf("VC Redistributable installation verification failed after %d attempts", maxRetries)
		fmt.Printf("  -%s\n", errMsg)
		return errors.New(errMsg)
	}

	return nil
}

func (a *App) installGitBash() error {
	gitVersion := "2.52.0"
	fullVersion := "v2.52.0.windows.1"
	fileName := fmt.Sprintf("Git-%s-64-bit.exe", gitVersion)

	downloadURL := fmt.Sprintf("https://github.com/git-for-windows/git/releases/download/%s/%s", fullVersion, fileName)
	if strings.HasPrefix(strings.ToLower(a.CurrentLanguage), "zh") {
		downloadURL = fmt.Sprintf("https://npmmirror.com/mirrors/git-for-windows/%s/%s", fullVersion, fileName)
	}

	a.log(a.tr("Downloading Git %s...", gitVersion))

	tempDir := a.GetTempDir()
	exePath := filepath.Join(tempDir, fileName)

	if err := a.downloadFile(exePath, downloadURL); err != nil {
		return fmt.Errorf("error downloading Git installer: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	a.log(a.tr("Installing Git (this may take a moment, please grant administrator permission if prompted)..."))

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb := syscall.StringToUTF16Ptr("runas")
	file := syscall.StringToUTF16Ptr(exePath)
	params := syscall.StringToUTF16Ptr(`/VERYSILENT /NORESTART /NOCANCEL /SP- /CLOSEAPPLICATIONS /RESTARTAPPLICATIONS /DIR="C:\Program Files\Git"`)
	dir := syscall.StringToUTF16Ptr("")

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		uintptr(syscall.SW_HIDE),
	)

	if ret <= 32 {
		var errMsg string
		switch ret {
		case 5:
			errMsg = "Access denied. Please ensure you have administrator privileges."
		case 8:
			errMsg = "Insufficient memory to complete the operation."
		case 31:
			errMsg = "No file association for installer executable."
		default:
			errMsg = fmt.Sprintf("Unknown error (code: %d). Please try installing Git manually from https://git-scm.com/", ret)
		}
		return fmt.Errorf("failed to launch Git installer: %s", errMsg)
	}

	a.log(a.tr("Waiting for Git installation to complete..."))
	gitPath := `C:\Program Files\Git\cmd\git.exe`
	maxWaitTime := 5 * time.Minute
	checkInterval := 2 * time.Second
	elapsed := time.Duration(0)

	for elapsed < maxWaitTime {
		if _, err := os.Stat(gitPath); err == nil {
			a.log(a.tr("Git installation completed successfully."))
			time.Sleep(2 * time.Second)

			go func() {
				time.Sleep(5 * time.Second)
				os.Remove(exePath)
			}()

			return nil
		}
		time.Sleep(checkInterval)
		elapsed += checkInterval
	}

	a.log(a.tr("Warning: Git installation verification timed out. Please check if Git was installed correctly."))
	os.Remove(exePath)
	return nil
}

func (a *App) downloadFile(filepath string, url string) error {
	a.log(fmt.Sprintf("Requesting URL: %s", url))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	transport := &http.Transport{
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		DisableKeepAlives:     true,
	}

	client := &http.Client{Transport: transport}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("network error during download: %v. Please check your internet connection or firewall settings.", err)
	}
	defer resp.Body.Close()

	if resp.Request.URL.String() != url {
		a.log(fmt.Sprintf("Redirected to: %s", resp.Request.URL.String()))
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s. The file might not be available on this server.", resp.Status)
	}

	size := resp.ContentLength
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer out.Close()

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
			return fmt.Errorf("interrupted download: %v. The connection was lost during data transfer.", err)
		}
	}

	out.Sync()
	return nil
}

func (a *App) restartApp() {
	executable, err := os.Executable()
	if err != nil {
		a.log("Failed to get executable path: " + err.Error())
		return
	}

	cmdLine := fmt.Sprintf(`cmd /c start "" "%s"`, executable)
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:    cmdLine,
		HideWindow: true,
	}
	if err := cmd.Start(); err != nil {
		a.log("Failed to restart: " + err.Error())
	} else {
		runtime.Quit(a.ctx)
	}
}

func (a *App) GetDownloadsFolder() (string, error) {
	if home := os.Getenv("USERPROFILE"); home != "" {
		downloads := filepath.Join(home, "Downloads")
		if _, err := os.Stat(downloads); err == nil {
			return downloads, nil
		}
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shGetKnownFolderPath := shell32.NewProc("SHGetKnownFolderPath")

	folderID := syscall.GUID{
		Data1: 0x374DE290,
		Data2: 0x123F,
		Data3: 0x4565,
		Data4: [8]byte{0x91, 0x64, 0x39, 0xC4, 0x92, 0x5E, 0x46, 0x7B},
	}

	var path *uint16
	res, _, _ := shGetKnownFolderPath.Call(
		uintptr(unsafe.Pointer(&folderID)),
		0,
		0,
		uintptr(unsafe.Pointer(&path)),
	)

	if res == 0 {
		defer syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(path)))
		return syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(path))[:]), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

func (a *App) findSh() string {
	path, err := exec.LookPath("sh")
	if err == nil {
		return path
	}

	commonPaths := []string{
		`C:\Program Files\Git\bin\sh.exe`,
		`C:\Program Files\Git\usr\bin\sh.exe`,
		`C:\Program Files (x86)\Git\bin\sh.exe`,
		`C:\Program Files (x86)\Git\usr\bin\sh.exe`,
	}
	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "sh"
}

func (a *App) platformLaunch(binaryName string, yoloMode bool, adminMode bool, pythonEnv string, projectDir string, env map[string]string, modelId string) {
	tm := NewToolManager(a)
	a.log(fmt.Sprintf("platformLaunch: Looking for tool '%s'", binaryName))
	status := tm.GetToolStatus(binaryName)

	a.log(fmt.Sprintf("Tool status - Installed: %v, Path: %s, Version: %s", status.Installed, status.Path, status.Version))

	binaryPath := ""
	if status.Installed {
		binaryPath = status.Path
	}

	if binaryPath == "" {
		// Tool not found, attempt automatic repair/installation
		a.log(fmt.Sprintf("Tool %s not found. Attempting automatic installation...", binaryName))

		// Emit event to show installation progress dialog
		runtime.EventsEmit(a.ctx, "tool-repair-start", binaryName)

		// Check if npm is available first
		npmPath := tm.getNpmPath()
		if npmPath == "" {
			runtime.EventsEmit(a.ctx, "tool-repair-failed", binaryName, a.tr("npm not found. Please run environment check first."))
			a.ShowMessage(a.tr("Installation Error"), a.tr("npm not found. Please run environment check first."))
			return
		}

		// Attempt to install the tool
		err := tm.InstallTool(binaryName)
		if err != nil {
			runtime.EventsEmit(a.ctx, "tool-repair-failed", binaryName, err.Error())
			a.ShowMessage(a.tr("Installation Error"), a.tr("Failed to install %s: %v", binaryName, err))
			return
		}

		// Re-check tool status after installation
		status = tm.GetToolStatus(binaryName)
		if !status.Installed {
			runtime.EventsEmit(a.ctx, "tool-repair-failed", binaryName, a.tr("Installation completed but tool not found"))
			a.ShowMessage(a.tr("Installation Error"), a.tr("Installation completed but %s still not found. Please try running environment check.", binaryName))
			return
		}

		binaryPath = status.Path
		runtime.EventsEmit(a.ctx, "tool-repair-success", binaryName, status.Version)
		a.log(fmt.Sprintf("Tool %s installed successfully. Version: %s", binaryName, status.Version))
	}
	a.log("Using binary at: " + binaryPath)

	projectDir = filepath.Clean(projectDir)
	binaryPath = filepath.Clean(binaryPath)

	cmdArgs := ""
	if binaryName == "codebuddy" && modelId != "" {
		cmdArgs += fmt.Sprintf(" --model %s", modelId)
	}

	if yoloMode {
		var flag string
		switch binaryName {
		case "claude":
			flag = "--dangerously-skip-permissions"
		case "gemini":
			flag = "--yolo"
		case "codex":
			flag = "--full-auto"
		case "codebuddy":
			flag = "-y"
		case "iflow":
			flag = "-y"
		case "kilo":
			flag = ""
		}
		if flag != "" {
			cmdArgs += " " + flag
		}
	}

	batchContent := "@echo off\r\n"
	batchContent += "setlocal\r\n"
	batchContent += "chcp 65001 > nul\r\n"
	batchContent += fmt.Sprintf("cd /d \"%s\"\r\n", projectDir)

	for k, v := range env {
		batchContent += fmt.Sprintf("set %s=%s\r\n", k, v)
	}

	home, _ := os.UserHomeDir()
	localToolPath := filepath.Join(home, ".maclaw", "data", "tools")
	nodePath := `C:\Program Files\nodejs`
	npmPath := filepath.Join(os.Getenv("AppData"), "npm")

	gitCmdPath := `C:\Program Files\Git\cmd`
	gitBinPath := `C:\Program Files\Git\bin`
	gitUsrBinPath := `C:\Program Files\Git\usr\bin`

	batchContent += fmt.Sprintf("set PATH=%s;%s;%s;%s;%s;%s;%%PATH%%\r\n",
		localToolPath, npmPath, nodePath, gitCmdPath, gitBinPath, gitUsrBinPath)

	if pythonEnv != "" && pythonEnv != "None (Default)" {
		condaRoot := a.getCondaRoot()
		if condaRoot != "" {
			activateScript := filepath.Join(condaRoot, "Scripts", "activate.bat")
			batchContent += fmt.Sprintf("echo Initializing Conda from: %s\r\n", condaRoot)
			batchContent += fmt.Sprintf("call \"%s\"\r\n", activateScript)
			batchContent += fmt.Sprintf("echo Activating Python environment: %s\r\n", pythonEnv)
			batchContent += fmt.Sprintf("call conda activate \"%s\"\r\n", pythonEnv)
			batchContent += "if errorlevel 1 (\r\n"
			batchContent += fmt.Sprintf("  echo Warning: Failed to activate conda environment '%s'. Continuing with base environment.\r\n", pythonEnv)
			batchContent += ")\r\n"
		} else {
			batchContent += "echo Warning: Conda installation not found. Cannot activate environment.\r\n"
		}
	}

	batchContent += fmt.Sprintf("echo Launching %s...\r\n", binaryName)
	batchContent += "echo.\r\n"

	ext := strings.ToLower(filepath.Ext(binaryPath))

	if ext == ".cmd" || ext == ".bat" {
		if strings.Contains(binaryPath, filepath.Join(home, ".maclaw", "data", "tools")) {
			var jsEntryPoint string
			packageName := tm.GetPackageName(binaryName)
			if packageName != "" {
				pkgDir := filepath.Join(home, ".maclaw", "data", "tools", "node_modules", packageName)

				possibleEntries := []string{
					filepath.Join(pkgDir, "index.js"),
					filepath.Join(pkgDir, "cli.js"),
					filepath.Join(pkgDir, "dist", "index.js"),
					filepath.Join(pkgDir, "bin", "index.js"),
					filepath.Join(pkgDir, "bin", binaryName+".js"),
					filepath.Join(pkgDir, "lib", "index.js"),
					filepath.Join(pkgDir, "src", "index.js"),
				}

				for _, entry := range possibleEntries {
					if _, err := os.Stat(entry); err == nil {
						jsEntryPoint = entry
						break
					}
				}
			}

			if jsEntryPoint != "" {
				a.log(fmt.Sprintf("Using direct node invocation with entry point: %s", jsEntryPoint))
				batchContent += fmt.Sprintf("node \"%s\"%s\r\n", jsEntryPoint, cmdArgs)
			} else {
				a.log(fmt.Sprintf("No JS entry point found, using wrapper script with 'call': %s", binaryPath))
				batchContent += fmt.Sprintf("call \"%s\"%s\r\n", binaryPath, cmdArgs)
			}
		} else {
			batchContent += fmt.Sprintf("call \"%s\"%s\r\n", binaryPath, cmdArgs)
		}
	} else if ext == ".ps1" {
		batchContent += fmt.Sprintf("powershell -ExecutionPolicy Bypass -File \"%s\"%s\r\n", binaryPath, cmdArgs)
	} else if ext == ".js" {
		batchContent += fmt.Sprintf("node \"%s\"%s\r\n", binaryPath, cmdArgs)
	} else if ext == "" {
		shPath := a.findSh()
		batchContent += fmt.Sprintf("\"%s\" \"%s\"%s\r\n", shPath, binaryPath, cmdArgs)
	} else {
		batchContent += fmt.Sprintf("\"%s\"%s\r\n", binaryPath, cmdArgs)
	}

	batchContent += "set TOOL_EXIT_CODE=%errorlevel%\r\n"
	batchContent += "echo.\r\n"
	batchContent += "if %TOOL_EXIT_CODE% neq 0 (\r\n"
	batchContent += "  echo ========================================\r\n"
	batchContent += fmt.Sprintf("  echo %s exited with error code %%TOOL_EXIT_CODE%%\r\n", binaryName)
	batchContent += "  echo ========================================\r\n"
	batchContent += "  echo.\r\n"
	batchContent += "  echo Press any key to close this window...\r\n"
	batchContent += "  pause >nul\r\n"
	batchContent += ") else (\r\n"
	batchContent += "  echo ========================================\r\n"
	batchContent += fmt.Sprintf("  echo %s completed successfully\r\n", binaryName)
	batchContent += "  echo ========================================\r\n"
	batchContent += ")\r\n"

	tempBatchPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("maclaw_launch_%d.bat", time.Now().UnixNano()))
	err := os.WriteFile(tempBatchPath, []byte(batchContent), 0644)
	if err != nil {
		a.log("Error creating batch file: " + err.Error())
		a.ShowMessage("Launch Error", "Failed to create temporary batch file")
		return
	}

	a.log(fmt.Sprintf("Created launch script: %s", tempBatchPath))

	go func() {
		time.Sleep(10 * time.Second)
		os.Remove(tempBatchPath)
	}()

	// Check if user prefers Windows Terminal
	config, _ := a.LoadConfig()
	useWT := config.UseWindowsTerminal && a.isWindowsTerminalAvailable()
	a.log(fmt.Sprintf("UseWindowsTerminal config: %v, isAvailable: %v, useWT: %v", config.UseWindowsTerminal, a.isWindowsTerminalAvailable(), useWT))

	if adminMode {
		shell32 := syscall.NewLazyDLL("shell32.dll")
		shellExecute := shell32.NewProc("ShellExecuteW")

		verb := syscall.StringToUTF16Ptr("runas")
		var file, params *uint16
		if useWT {
			wtPath := a.getWindowsTerminalPath()
			if wtPath == "" {
				a.log("Windows Terminal path not found, falling back to cmd")
				file = syscall.StringToUTF16Ptr("cmd.exe")
				params = syscall.StringToUTF16Ptr(fmt.Sprintf("/c \"%s\"", tempBatchPath))
			} else {
				file = syscall.StringToUTF16Ptr(wtPath)
				params = syscall.StringToUTF16Ptr(fmt.Sprintf("-w new -d \"%s\" cmd /k \"%s\"", projectDir, tempBatchPath))
			}
		} else {
			file = syscall.StringToUTF16Ptr("cmd.exe")
			params = syscall.StringToUTF16Ptr(fmt.Sprintf("/c \"%s\"", tempBatchPath))
		}
		dir := syscall.StringToUTF16Ptr(projectDir)

		ret, _, _ := shellExecute.Call(
			0,
			uintptr(unsafe.Pointer(verb)),
			uintptr(unsafe.Pointer(file)),
			uintptr(unsafe.Pointer(params)),
			uintptr(unsafe.Pointer(dir)),
			uintptr(syscall.SW_SHOW),
		)

		if ret <= 32 {
			a.log(fmt.Sprintf("ShellExecute failed with return value: %d", ret))
			a.ShowMessage("Launch Error", "Failed to launch with admin privileges.")
		}
	} else {
		if binaryName == "codex" || binaryName == "openai" {
			codexBatchContent := "@echo off\r\n"
			codexBatchContent += "setlocal\r\n"
			codexBatchContent += "chcp 65001 > nul\r\n"
			codexBatchContent += fmt.Sprintf("cd /d \"%s\"\r\n", projectDir)

			for k, v := range env {
				codexBatchContent += fmt.Sprintf("set %s=%s\r\n", k, v)
			}
			codexBatchContent += fmt.Sprintf("set PATH=%s;%%PATH%%\r\n", localToolPath)

			codexBatchContent += fmt.Sprintf("echo Launching %s...\r\n", binaryName)
			codexBatchContent += "echo.\r\n"
			// Bypass the .cmd shim and invoke node directly.
			// npm's .cmd shim uses "goto #_undefined_#" trick that breaks under "call".
			nodeExe := filepath.Join(filepath.Dir(binaryPath), "node.exe")
			codexJsPath := filepath.Join(filepath.Dir(binaryPath), "node_modules", "@openai", "codex", "bin", "codex.js")
			if _, err := os.Stat(nodeExe); err == nil {
				// Local node.exe bundled alongside the tool
				codexBatchContent += fmt.Sprintf("\"%s\" \"%s\"%s\r\n", nodeExe, codexJsPath, cmdArgs)
			} else {
				// Fall back to system node
				codexBatchContent += fmt.Sprintf("node \"%s\"%s\r\n", codexJsPath, cmdArgs)
			}

			codexBatchContent += "set TOOL_EXIT_CODE=%errorlevel%\r\n"
			codexBatchContent += "echo.\r\n"
			codexBatchContent += "if %TOOL_EXIT_CODE% neq 0 (\r\n"
			codexBatchContent += "  echo ========================================\r\n"
			codexBatchContent += fmt.Sprintf("  echo %s exited with error code %%TOOL_EXIT_CODE%%\r\n", binaryName)
			codexBatchContent += "  echo ========================================\r\n"
			codexBatchContent += "  echo.\r\n"
			codexBatchContent += "  echo Press any key to close this window...\r\n"
			codexBatchContent += "  pause >nul\r\n"
			codexBatchContent += ") else (\r\n"
			codexBatchContent += "  echo ========================================\r\n"
			codexBatchContent += fmt.Sprintf("  echo %s completed successfully\r\n", binaryName)
			codexBatchContent += "  echo ========================================\r\n"
			codexBatchContent += ")\r\n"

			codexBatchPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("maclaw_codex_%d.bat", time.Now().UnixNano()))
			if err := os.WriteFile(codexBatchPath, []byte(codexBatchContent), 0644); err != nil {
				a.log("Error creating codex batch file: " + err.Error())
				a.ShowMessage("Launch Error", "Failed to create temporary batch file")
				return
			}

			a.log(fmt.Sprintf("Launching %s with TTY batch mode", binaryName))

			go func() {
				time.Sleep(10 * time.Second)
				os.Remove(codexBatchPath)
			}()

			var cmdLine string
			if useWT {
				wtPath := a.getWindowsTerminalPath()
				if wtPath == "" {
					a.log("Windows Terminal path not found, falling back to cmd")
					cmdLine = fmt.Sprintf(`cmd /c start "MaClaw - %s" /d "%s" cmd /k "%s"`,
						binaryName, projectDir, codexBatchPath)
				} else {
					// Use start command to properly handle paths with spaces
					cmdLine = fmt.Sprintf(`cmd /c start "" "%s" -w new -d "%s" --title "MaClaw - %s" cmd /k "%s"`,
						wtPath, projectDir, binaryName, codexBatchPath)
				}
			} else {
				cmdLine = fmt.Sprintf(`cmd /c start "MaClaw - %s" /d "%s" cmd /k "%s"`,
					binaryName, projectDir, codexBatchPath)
			}

			cmd := exec.Command("cmd")
			cmd.SysProcAttr = &syscall.SysProcAttr{
				CmdLine:    cmdLine,
				HideWindow: true,
			}

			if err := cmd.Start(); err != nil {
				a.log("Error launching tool: " + err.Error())
				a.ShowMessage("Launch Error", "Failed to start process: "+err.Error())
			}
		} else {
			var cmdLine string
			if useWT {
				wtPath := a.getWindowsTerminalPath()
				if wtPath == "" {
					a.log("Windows Terminal path not found, falling back to cmd")
					cmdLine = fmt.Sprintf(`cmd /c start "MaClaw - %s" /d "%s" cmd /k "%s"`, binaryName, projectDir, tempBatchPath)
				} else {
					// Use start command to properly handle paths with spaces
					cmdLine = fmt.Sprintf(`cmd /c start "" "%s" -w new -d "%s" --title "MaClaw - %s" cmd /k "%s"`,
						wtPath, projectDir, binaryName, tempBatchPath)
				}
			} else {
				cmdLine = fmt.Sprintf(`cmd /c start "MaClaw - %s" /d "%s" cmd /k "%s"`, binaryName, projectDir, tempBatchPath)
			}

			cmd := exec.Command("cmd")
			cmd.SysProcAttr = &syscall.SysProcAttr{
				CmdLine:    cmdLine,
				HideWindow: true,
			}

			if err := cmd.Start(); err != nil {
				a.log("Error launching tool: " + err.Error())
				a.ShowMessage("Launch Error", "Failed to start process: "+err.Error())
			}
		}
	}
}

func (a *App) syncToSystemEnv(config AppConfig) {
	// On Windows, we don't set environment variables in the main process
	// to avoid cross-contamination between multiple tool instances.
	// Environment variables are set in the batch scripts instead.
	// This function is kept for compatibility but does nothing on Windows.
}

func createVersionCmd(path string) *exec.Cmd {
	cmd := exec.Command(path, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func createNpmInstallCmd(npmPath string, args []string) *exec.Cmd {
	cmd := exec.Command(npmPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func createCondaEnvListCmd(condaCmd string) *exec.Cmd {
	cmd := exec.Command("cmd", "/c", condaCmd, "env", "list")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func (a *App) ensureLocalNodeBinary() {
	home, _ := os.UserHomeDir()
	localNodeDir := filepath.Join(home, ".maclaw", "data", "tools")

	if err := os.MkdirAll(localNodeDir, 0755); err != nil {
		a.log("Failed to create local tools dir: " + err.Error())
		return
	}

	localNodeExe := filepath.Join(localNodeDir, "node.exe")

	if _, err := os.Stat(localNodeExe); err == nil {
		return
	}

	systemNode, err := exec.LookPath("node")
	if err != nil {
		commonPaths := []string{
			`C:\Program Files\nodejs\node.exe`,
			filepath.Join(os.Getenv("AppData"), "npm", "node.exe"),
		}
		for _, p := range commonPaths {
			if _, err := os.Stat(p); err == nil {
				systemNode = p
				break
			}
		}
	}

	if systemNode == "" {
		a.log("Warning: Could not find system node.exe to copy to local tool dir.")
		return
	}

	a.log(fmt.Sprintf("Copying node.exe from %s to %s to ensure wrapper compatibility...", systemNode, localNodeExe))

	input, err := os.ReadFile(systemNode)
	if err != nil {
		a.log("Failed to read system node.exe: " + err.Error())
		return
	}

	if err := os.WriteFile(localNodeExe, input, 0755); err != nil {
		a.log("Failed to write local node.exe: " + err.Error())
		return
	}

	a.log("Successfully copied node.exe to local directory.")
}

func (a *App) LaunchInstallerAndExit(installerPath string) error {
	a.log(fmt.Sprintf("Launching installer: %s", installerPath))

	cmd := exec.Command("cmd", "/c", "start", "", installerPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to launch installer: %w", err)
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		runtime.Quit(a.ctx)
	}()

	return nil
}

func getWindowsVersionHidden() string {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:    `cmd /c ver`,
		HideWindow: true,
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	verStr := string(out)
	safeVer := ""
	for _, r := range verStr {
		if r >= 32 && r <= 126 {
			safeVer += string(r)
		}
	}
	return strings.TrimSpace(safeVer)
}

func createUpdateCmd(path string) *exec.Cmd {
	cmd := exec.Command(path, "update")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func createHiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// isWindowsTerminalAvailable checks if Windows Terminal (wt.exe) is installed and available
func (a *App) isWindowsTerminalAvailable() bool {
	wtPath := a.getWindowsTerminalPath()
	return wtPath != ""
}

// getWindowsTerminalPath returns the full path to wt.exe if available, empty string otherwise
func (a *App) getWindowsTerminalPath() string {
	// Check if wt.exe is in PATH
	if wtPath, err := exec.LookPath("wt.exe"); err == nil {
		a.log(fmt.Sprintf("Windows Terminal found in PATH: %s", wtPath))
		return wtPath
	}

	// Check common installation paths
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		// Windows Terminal from Microsoft Store
		wtPath := filepath.Join(localAppData, "Microsoft", "WindowsApps", "wt.exe")
		if _, err := os.Stat(wtPath); err == nil {
			a.log(fmt.Sprintf("Windows Terminal found at: %s", wtPath))
			return wtPath
		}
	}

	// Check Program Files paths (for manual installations)
	programFiles := os.Getenv("ProgramFiles")
	if programFiles != "" {
		wtPath := filepath.Join(programFiles, "WindowsApps", "Microsoft.WindowsTerminal_*", "wt.exe")
		matches, _ := filepath.Glob(wtPath)
		if len(matches) > 0 {
			a.log(fmt.Sprintf("Windows Terminal found at: %s", matches[0]))
			return matches[0]
		}
	}

	a.log("Windows Terminal not found")
	return ""
}

// IsWindowsTerminalAvailable is exported for frontend to check availability
func (a *App) IsWindowsTerminalAvailable() bool {
	return a.isWindowsTerminalAvailable()
}
