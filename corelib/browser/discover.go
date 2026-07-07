package browser

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// ── managed browser process ──

var (
	managedBrowserMu    sync.Mutex
	managedBrowserProcs = map[string]*os.Process{}
)

type devToolsActivePortInfo struct {
	Port   int
	WSPath string
	Source string
}

type browserLaunchDiagnostic struct {
	BrowserName  string
	BrowserPath  string
	UserDataDir  string
	Port         int
	PortOccupied bool
	TCPReachable bool
	Managed      bool
	PortError    string
	JSONError    string
	StderrTail   string
	Stage        string
}

func (d browserLaunchDiagnostic) Summary() string {
	parts := make([]string, 0, 8)
	if d.BrowserName != "" {
		parts = append(parts, "browser="+d.BrowserName)
	}
	if d.BrowserPath != "" {
		parts = append(parts, "path="+d.BrowserPath)
	}
	if d.UserDataDir != "" {
		parts = append(parts, "user_data_dir="+d.UserDataDir)
	}
	if d.Port > 0 {
		parts = append(parts, fmt.Sprintf("port=%d", d.Port))
	}
	if d.Stage != "" {
		parts = append(parts, "stage="+d.Stage)
	}
	if d.PortOccupied {
		parts = append(parts, "port_occupied=true")
	}
	if d.TCPReachable {
		parts = append(parts, "tcp_connect=ok")
	}
	if d.Managed {
		parts = append(parts, "managed_browser=true")
	}
	if d.PortError != "" {
		parts = append(parts, "port_error="+d.PortError)
	}
	if d.JSONError != "" {
		parts = append(parts, "json_error="+d.JSONError)
	}
	if d.StderrTail != "" {
		parts = append(parts, "stderr="+d.StderrTail)
	}
	return strings.Join(parts, "; ")
}

// DiscoverCDPAddr tries to auto-discover a Chrome/Edge CDP endpoint.
// Priority: 1) DevToolsActivePort file  2) common ports (9222, 9229, 9333)
// Returns an HTTP address like "http://127.0.0.1:9222" or error.
func DiscoverCDPAddr() (string, error) {
	// 1. Try DevToolsActivePort file (works with chrome://inspect remote debugging).
	if info, ok := readDevToolsActivePort(); ok {
		if probePort(info.Port) {
			return fmt.Sprintf("http://127.0.0.1:%d", info.Port), nil
		}
		return "", fmt.Errorf("已找到 DevToolsActivePort 但端口不可达: source=%s port=%d", info.Source, info.Port)
	}

	// 2. Scan common debug ports.
	for _, port := range []int{9222, 9229, 9333} {
		if probePort(port) {
			return fmt.Sprintf("http://127.0.0.1:%d", port), nil
		}
	}

	return "", fmt.Errorf("未发现 Chrome/Edge 调试端口")
}

// debugProfileDir returns a dedicated user-data-dir for debug-mode Chrome/Edge.
// Using a separate profile avoids conflicts with the user's running browser
// (the root cause of "browser exits immediately" failures).
func debugProfileDir() string {
	base := corelib.MaclawBaseDir()
	if base == "" {
		return filepath.Join(os.TempDir(), "maclaw-chrome-debug-profile")
	}
	return filepath.Join(base, "browser-debug-profile")
}

// persistentProfileDir is the managed MaClaw browser profile. It keeps cookies
// and login state across automation runs without using the user's daily Chrome
// profile, which avoids profile locks and unstable CDP target reuse.
func persistentProfileDir() string {
	base := corelib.MaclawBaseDir()
	if base == "" {
		return filepath.Join(os.TempDir(), "maclaw", "browser-profile")
	}
	return filepath.Join(base, "browser-profile")
}

func browserProfileKind(userDataDir string) string {
	clean := filepath.Clean(userDataDir)
	switch clean {
	case filepath.Clean(persistentProfileDir()):
		return "persistent managed profile"
	case filepath.Clean(debugProfileDir()):
		return "isolated debug profile"
	default:
		return "managed profile"
	}
}

// debugPort is the fixed CDP port used for the isolated debug browser.
const debugPort = 9222

// DiscoverOrLaunchPersistent launches/reuses the managed persistent browser.
// This is the stable default for agent automation: one controlled Chrome
// profile, durable cookies, and browser-session-* handles for all operations.
func DiscoverOrLaunchPersistent() (string, error) {
	profileDir := persistentProfileDir()
	if addr, ok := discoverCDPAddrFromManagedProcess(profileDir); ok {
		if _, err := DiscoverTargets(addr); err == nil {
			log.Printf("[browser] reused managed browser from process command line: %s", addr)
			return addr, nil
		}
	}
	if addr, ok := readCDPAddrFromUserDataDir(profileDir); ok {
		if _, err := DiscoverTargets(addr); err == nil {
			log.Printf("[browser] reused managed browser from DevToolsActivePort: %s", addr)
			return addr, nil
		}
	}
	// Persistent profile owns login/cookies. If a browser already has this profile
	// open, do not kill or relaunch it just because CDP is slow or temporarily
	// unreachable. Starting a second Chrome against the same profile causes process
	// churn and unstable sessions. Recovery must reconnect to the existing
	// profile process or ask the user to close it manually.
	if browserProcessExistsForDir(profileDir) {
		if addr, ok := waitForPersistentCDP(profileDir, 10*time.Second); ok {
			return addr, nil
		}
		return "", fmt.Errorf("persistent browser profile is already open but CDP is unavailable; not killing or relaunching profile=%s", profileDir)
	}

	bi := detectBrowser()
	if bi == nil {
		return "", fmt.Errorf("未找到 Chrome 或 Edge 浏览器，请安装后重试")
	}

	// Do not delete Singleton* lock files for the persistent profile. If an older
	// managed browser is still alive but CDP is temporarily unreachable, removing
	// the profile lock can start a second browser on the same profile and corrupt
	// state. Chrome/Edge can clean stale locks on its own after a crash.
	os.Remove(filepath.Join(profileDir, "DevToolsActivePort"))

	port, err := findFreeLocalPort()
	if err != nil {
		return "", err
	}
	return launchDebugBrowser(bi, profileDir, port)
}

func waitForPersistentCDP(profileDir string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if addr, ok := discoverCDPAddrFromManagedProcess(profileDir); ok {
			if _, err := DiscoverTargets(addr); err == nil {
				log.Printf("[browser] reused managed browser after CDP wait: %s", addr)
				return addr, true
			}
		}
		if addr, ok := readCDPAddrFromUserDataDir(profileDir); ok {
			if _, err := DiscoverTargets(addr); err == nil {
				log.Printf("[browser] reused managed browser from DevToolsActivePort after CDP wait: %s", addr)
				return addr, true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", false
}

// DiscoverOrLaunch tries DiscoverCDPAddr first. If that fails, it launches
// Chrome/Edge with an isolated debug profile and a fixed port (9222).
// This is the isolated mode path; stable automation uses
// DiscoverOrLaunchPersistent instead.
//
// For persistent login state, use DiscoverOrLaunchPersistent() instead.
func DiscoverOrLaunch() (string, error) {
	// Fast path: already available.
	if addr, err := DiscoverCDPAddr(); err == nil {
		// Verify the port is actually serving CDP (not just TCP-open).
		if _, err2 := DiscoverTargets(addr); err2 == nil {
			return addr, nil
		} else {
			log.Printf("[browser] 端口可达但 CDP 无响应: %v", err2)
		}
	}

	// Detect browser.
	bi := detectBrowser()
	if bi == nil {
		return "", fmt.Errorf("未找到 Chrome 或 Edge 浏览器，请安装后重试")
	}

	// Use an isolated debug profile to avoid conflicts with the user's browser.
	debugDir := debugProfileDir()
	addr := fmt.Sprintf("http://127.0.0.1:%d", debugPort)

	// Step 1: If port 9222 is occupied but not serving CDP, only clean up our own managed
	// instance. Avoid killing user browsers or unrelated processes holding the port.
	if probePort(debugPort) {
		if _, err := DiscoverTargets(addr); err != nil {
			log.Printf("[browser] 端口 %d 被占用但非 CDP: %v", debugPort, err)
			if killManagedBrowserForDir(debugDir) {
				waitForPortRelease(debugPort, 8*time.Second)
			} else {
				return "", fmt.Errorf("调试端口 %d 已被占用，但不是有效 CDP 端点；未自动结束未知进程以避免误杀。详情: %w", debugPort, err)
			}
		}
	}

	// Clean stale lock files in the debug profile.
	for _, lockName := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "lockfile"} {
		os.Remove(filepath.Join(debugDir, lockName))
	}
	os.Remove(filepath.Join(debugDir, "DevToolsActivePort"))

	// Step 2: Launch with isolated profile + fixed port.
	launchedAddr, err := launchDebugBrowser(bi, debugDir, debugPort)
	if err != nil {
		// Retry once: force-kill only our managed process and try again.
		log.Printf("[browser] 首次启动失败 (%v)，清理托管浏览器后重试...", err)
		killManagedBrowserForDir(debugDir)
		waitForPortRelease(debugPort, 10*time.Second)
		for _, lockName := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "lockfile"} {
			os.Remove(filepath.Join(debugDir, lockName))
		}
		launchedAddr, err = launchDebugBrowser(bi, debugDir, debugPort)
		if err != nil {
			return "", fmt.Errorf("浏览器两次启动均失败: %w", err)
		}
	}

	return launchedAddr, nil
}

// launchDebugBrowser starts Chrome/Edge with the given user-data-dir and
// remote-debugging-port, waits for the port to become available, and returns
// the CDP HTTP address.
func launchDebugBrowser(bi *browserInfo, userDataDir string, port int) (string, error) {
	if port <= 0 {
		freePort, err := findFreeLocalPort()
		if err != nil {
			return "", err
		}
		port = freePort
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + userDataDir,
	}
	cmd := exec.Command(bi.path, args...)
	var stderr bytes.Buffer
	cmd.Stdout = nil
	cmd.Stderr = &stderr
	log.Printf("[browser] 启动命令: %s %v", bi.path, args)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动浏览器失败: %w (browser=%s path=%s user_data_dir=%s port=%d)", err, bi.name, bi.path, userDataDir, port)
	}
	success := false
	defer func() {
		if success {
			return
		}
		if cmd.Process != nil {
			log.Printf("[browser] 启动未得到可用 CDP，清理托管浏览器进程 (PID %d)", cmd.Process.Pid)
			_ = cmd.Process.Kill()
		}
	}()

	// Track the process for cleanup.
	managedKey := managedBrowserKey(userDataDir)
	managedBrowserMu.Lock()
	managedBrowserProcs[managedKey] = cmd.Process
	managedBrowserMu.Unlock()

	procExited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(procExited)
		managedBrowserMu.Lock()
		if managedBrowserProcs[managedKey] == cmd.Process {
			delete(managedBrowserProcs, managedKey)
		}
		managedBrowserMu.Unlock()
	}()

	diag := browserLaunchDiagnostic{
		BrowserName: bi.name,
		BrowserPath: bi.path,
		UserDataDir: userDataDir,
		Port:        port,
		Managed:     true,
		Stage:       "launch",
	}

	// Chrome on Windows may let the launcher process exit after handing off to a
	// browser child process. Do not treat that as fatal; keep probing CDP.
	select {
	case <-procExited:
		diag.Stage = "exited_early"
		diag.StderrTail = summarizeStderr(stderr.String())
	case <-time.After(2 * time.Second):
		// Process is still alive, or at least has not handed off yet.
	}

	// Wait for the fixed port to start serving CDP.
	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	procExitedCh := procExited
	for time.Now().Before(deadline) {
		select {
		case <-procExitedCh:
			diag.Stage = "launcher_exited_waiting_cdp"
			diag.StderrTail = summarizeStderr(stderr.String())
			procExitedCh = nil
		default:
		}
		diag.PortOccupied = probePort(port)
		diag.TCPReachable = diag.PortOccupied
		if !diag.PortOccupied {
			diag.Stage = "waiting_port"
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if _, err := DiscoverTargets(addr); err == nil {
			log.Printf("[browser] launched %s with CDP port %d (%s)", bi.name, port, browserProfileKind(userDataDir))
			success = true
			return addr, nil
		} else {
			diag.Stage = "waiting_json"
			diag.JSONError = truncateText(err.Error(), 240)
		}
		time.Sleep(500 * time.Millisecond)
	}

	diag.StderrTail = summarizeStderr(stderr.String())
	if !diag.PortOccupied {
		return "", fmt.Errorf("浏览器已启动但端口 %d 未监听。%s", port, diag.Summary())
	}
	if diag.JSONError != "" {
		return "", fmt.Errorf("浏览器已启动但 /json 未返回有效 CDP。%s", diag.Summary())
	}
	return "", fmt.Errorf("浏览器已启动但端口 %d 未响应 CDP。%s", port, diag.Summary())
}

func findFreeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to reserve browser debug port: %w", err)
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("failed to inspect reserved browser debug port")
	}
	return addr.Port, nil
}

// waitForPortRelease waits until the given TCP port is no longer listening.
func waitForPortRelease(port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !probePort(port) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("[browser] waitForPortRelease: 端口 %d 超时仍被占用", port)
}

func managedBrowserKey(userDataDir string) string {
	key := filepath.Clean(userDataDir)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

// killManagedBrowserForDir kills the browser process that we started for a profile.
// Returns true if a matching managed process was found and killed.
func killManagedBrowserForDir(userDataDir string) bool {
	managedBrowserMu.Lock()
	key := managedBrowserKey(userDataDir)
	proc := managedBrowserProcs[key]
	if proc != nil {
		delete(managedBrowserProcs, key)
	}
	managedBrowserMu.Unlock()
	if proc != nil {
		log.Printf("[browser] 终止托管浏览器进程 (PID %d)...", proc.Pid)
		_ = proc.Kill()
		// Give the OS a moment to release resources.
		time.Sleep(1 * time.Second)
		return true
	}
	return killManagedBrowserProcessesByDir(userDataDir)
}

func killManagedBrowserProcessesByDir(userDataDir string) bool {
	if runtime.GOOS != "windows" || strings.TrimSpace(userDataDir) == "" {
		return false
	}
	ps := browserProcessesByDirPowerShell(userDataDir, true)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	coretool.HideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[browser] 按 profile 清理托管浏览器失败: %v output=%s", err, summarizeStderr(string(out)))
		return false
	}
	pids := strings.TrimSpace(string(out))
	if pids == "" {
		return false
	}
	log.Printf("[browser] 按 profile 清理托管浏览器进程 user_data_dir=%s pids=%s", userDataDir, pids)
	time.Sleep(1 * time.Second)
	return true
}

// ── browser detection ──

func browserProcessExistsForDir(userDataDir string) bool {
	if strings.TrimSpace(userDataDir) == "" {
		return false
	}
	managedBrowserMu.Lock()
	proc := managedBrowserProcs[managedBrowserKey(userDataDir)]
	managedBrowserMu.Unlock()
	if proc != nil {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	cmd := exec.Command("powershell", "-NoProfile", "-Command", browserProcessesByDirPowerShell(userDataDir, false))
	coretool.HideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func browserProcessesByDirPowerShell(userDataDir string, kill bool) string {
	needle := strings.ReplaceAll(filepath.Clean(userDataDir), "'", "''")
	if kill {
		return fmt.Sprintf(`$needle = '%s'; $procs = Get-CimInstance Win32_Process -Filter "name='chrome.exe' OR name='msedge.exe'" | Where-Object { $_.CommandLine -and $_.CommandLine.IndexOf($needle, [StringComparison]::OrdinalIgnoreCase) -ge 0 }; $ids = @($procs | ForEach-Object { $_.ProcessId }); if ($ids.Count -gt 0) { $ids -join ','; $ids | ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue } }`, needle)
	}
	return fmt.Sprintf(`$needle = '%s'; $procs = Get-CimInstance Win32_Process -Filter "name='chrome.exe' OR name='msedge.exe'" | Where-Object { $_.CommandLine -and $_.CommandLine.IndexOf($needle, [StringComparison]::OrdinalIgnoreCase) -ge 0 }; $ids = @($procs | ForEach-Object { $_.ProcessId }); if ($ids.Count -gt 0) { $ids -join ',' }`, needle)
}

type browserInfo struct {
	path string // absolute path to executable
	name string // "chrome" or "edge"
}

func detectBrowser() *browserInfo {
	if p := findChromeExe(); p != "" {
		return &browserInfo{path: p, name: "chrome"}
	}
	if p := findEdgeExe(); p != "" {
		return &browserInfo{path: p, name: "edge"}
	}
	return nil
}

func findChromeExe() string {
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("LocalAppData"),
		} {
			if base == "" {
				continue
			}
			p := filepath.Join(base, `Google\Chrome\Application\chrome.exe`)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "darwin":
		p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	default:
		if p, err := exec.LookPath("google-chrome"); err == nil {
			return p
		}
		if p, err := exec.LookPath("google-chrome-stable"); err == nil {
			return p
		}
	}
	return ""
}

func findEdgeExe() string {
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("ProgramFiles"),
		} {
			if base == "" {
				continue
			}
			p := filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case "darwin":
		p := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	default:
		if p, err := exec.LookPath("microsoft-edge"); err == nil {
			return p
		}
	}
	return ""
}

// defaultUserDataDir returns the default user-data-dir for the given browser.
func defaultUserDataDir(browserName string) string {
	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			return ""
		}
		if browserName == "chrome" {
			return filepath.Join(localAppData, "Google", "Chrome", "User Data")
		}
		return filepath.Join(localAppData, "Microsoft", "Edge", "User Data")
	case "darwin":
		home, _ := os.UserHomeDir()
		if home == "" {
			return ""
		}
		if browserName == "chrome" {
			return filepath.Join(home, "Library/Application Support/Google/Chrome")
		}
		return filepath.Join(home, "Library/Application Support/Microsoft Edge")
	default: // linux
		home, _ := os.UserHomeDir()
		if home == "" {
			return ""
		}
		if browserName == "chrome" {
			return filepath.Join(home, ".config/google-chrome")
		}
		return filepath.Join(home, ".config/microsoft-edge")
	}
}

// isBrowserRunning checks if Chrome/Edge processes are running.
func isBrowserRunning(browserName string) bool {
	switch runtime.GOOS {
	case "windows":
		var procName string
		if browserName == "chrome" {
			procName = "chrome.exe"
		} else {
			procName = "msedge.exe"
		}
		out, err := func() ([]byte, error) {
			cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", procName), "/NH")
			coretool.HideCommandWindow(cmd)
			return cmd.Output()
		}()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), procName)
	case "darwin":
		var appName string
		if browserName == "chrome" {
			appName = "Google Chrome"
		} else {
			appName = "Microsoft Edge"
		}
		out, _ := exec.Command("pgrep", "-f", appName).Output()
		return len(strings.TrimSpace(string(out))) > 0
	default:
		var procName string
		if browserName == "chrome" {
			procName = "chrome"
		} else {
			procName = "msedge"
		}
		out, _ := exec.Command("pgrep", "-f", procName).Output()
		return len(strings.TrimSpace(string(out))) > 0
	}
}

// readDevToolsActivePort reads the DevToolsActivePort file from known Chrome profile locations.
// Returns info when a valid DevToolsActivePort file is found.
func readDevToolsActivePort() (*devToolsActivePortInfo, bool) {
	candidates := devToolsActivePortCandidates()

	// Also check our isolated debug profile directory.
	debugDir := debugProfileDir()
	candidates = append(candidates, filepath.Join(debugDir, "DevToolsActivePort"))

	seen := make(map[string]struct{}, len(candidates))
	for _, p := range candidates {
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
		port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		wsPath := ""
		if len(lines) > 1 {
			wsPath = strings.TrimSpace(lines[1])
		}
		return &devToolsActivePortInfo{Port: port, WSPath: wsPath, Source: p}, true
	}
	return nil, false
}

func readCDPAddrFromUserDataDir(userDataDir string) (string, bool) {
	info, ok := readDevToolsActivePortFile(filepath.Join(userDataDir, "DevToolsActivePort"))
	if !ok || !probePort(info.Port) {
		return "", false
	}
	return fmt.Sprintf("http://127.0.0.1:%d", info.Port), true
}

func discoverCDPAddrFromManagedProcess(userDataDir string) (string, bool) {
	if runtime.GOOS != "windows" || strings.TrimSpace(userDataDir) == "" {
		return "", false
	}
	needle := strings.ReplaceAll(filepath.Clean(userDataDir), "'", "''")
	ps := fmt.Sprintf(`$needle = '%s'; Get-CimInstance Win32_Process -Filter "name='chrome.exe' OR name='msedge.exe'" | Where-Object { $_.CommandLine -and $_.CommandLine.IndexOf($needle, [StringComparison]::OrdinalIgnoreCase) -ge 0 -and $_.CommandLine.IndexOf('--remote-debugging-port', [StringComparison]::OrdinalIgnoreCase) -ge 0 } | ForEach-Object { $_.CommandLine }`, needle)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	coretool.HideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		port, ok := remoteDebuggingPortFromCommandLine(line)
		if !ok || !probePort(port) {
			continue
		}
		return fmt.Sprintf("http://127.0.0.1:%d", port), true
	}
	return "", false
}

func remoteDebuggingPortFromCommandLine(commandLine string) (int, bool) {
	lower := strings.ToLower(commandLine)
	idx := strings.Index(lower, "--remote-debugging-port")
	if idx < 0 {
		return 0, false
	}
	start := idx + len("--remote-debugging-port")
	for start < len(commandLine) && (commandLine[start] == '=' || commandLine[start] == ' ' || commandLine[start] == '\t') {
		start++
	}
	if start < len(commandLine) && (commandLine[start] == '"' || commandLine[start] == '\'') {
		start++
	}
	end := start
	for end < len(commandLine) && commandLine[end] >= '0' && commandLine[end] <= '9' {
		end++
	}
	if end == start {
		return 0, false
	}
	port, err := strconv.Atoi(commandLine[start:end])
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

func readDevToolsActivePortFile(path string) (*devToolsActivePortInfo, bool) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, false
	}
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port <= 0 || port > 65535 {
		return nil, false
	}
	wsPath := ""
	if len(lines) > 1 {
		wsPath = strings.TrimSpace(lines[1])
	}
	return &devToolsActivePortInfo{Port: port, WSPath: wsPath, Source: path}, true
}

func devToolsActivePortCandidates() []string {
	var candidates []string

	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, "Library/Application Support/Google/Chrome/DevToolsActivePort"),
				filepath.Join(home, "Library/Application Support/Google/Chrome Canary/DevToolsActivePort"),
				filepath.Join(home, "Library/Application Support/Chromium/DevToolsActivePort"),
				filepath.Join(home, "Library/Application Support/Microsoft Edge/DevToolsActivePort"),
			)
		}
	case "linux":
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				filepath.Join(home, ".config/google-chrome/DevToolsActivePort"),
				filepath.Join(home, ".config/chromium/DevToolsActivePort"),
				filepath.Join(home, ".config/microsoft-edge/DevToolsActivePort"),
			)
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			for _, userDataDir := range []string{
				filepath.Join(localAppData, "Google", "Chrome", "User Data"),
				filepath.Join(localAppData, "Chromium", "User Data"),
				filepath.Join(localAppData, "Microsoft", "Edge", "User Data"),
			} {
				candidates = append(candidates, windowsProfileDevToolsCandidates(userDataDir)...)
			}
		}
	}
	return candidates
}

func windowsProfileDevToolsCandidates(userDataDir string) []string {
	if strings.TrimSpace(userDataDir) == "" {
		return nil
	}
	candidates := []string{filepath.Join(userDataDir, "DevToolsActivePort")}
	entries, err := os.ReadDir(userDataDir)
	if err != nil {
		return candidates
	}
	profileDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "Default" || strings.HasPrefix(name, "Profile ") {
			profileDirs = append(profileDirs, name)
		}
	}
	sort.Strings(profileDirs)
	for _, name := range profileDirs {
		candidates = append(candidates, filepath.Join(userDataDir, name, "DevToolsActivePort"))
	}
	return candidates
}

func summarizeStderr(text string) string {
	return truncateText(strings.Join(strings.Fields(strings.TrimSpace(text)), " "), 240)
}

func truncateText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

// probePort checks if a TCP port is listening on localhost (2s timeout).
func probePort(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
