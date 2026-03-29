package browser

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── managed browser process ──

var (
	managedBrowserMu   sync.Mutex
	managedBrowserProc *os.Process
)

// DiscoverCDPAddr tries to auto-discover a Chrome/Edge CDP endpoint.
// Priority: 1) DevToolsActivePort file  2) common ports (9222, 9229, 9333)
// Returns an HTTP address like "http://127.0.0.1:9222" or error.
func DiscoverCDPAddr() (string, error) {
	// 1. Try DevToolsActivePort file (works with chrome://inspect remote debugging).
	if port, _ := readDevToolsActivePort(); port > 0 {
		if probePort(port) {
			return fmt.Sprintf("http://127.0.0.1:%d", port), nil
		}
	}

	// 2. Scan common debug ports.
	for _, port := range []int{9222, 9229, 9333} {
		if probePort(port) {
			return fmt.Sprintf("http://127.0.0.1:%d", port), nil
		}
	}

	return "", fmt.Errorf("未发现 Chrome/Edge 调试端口")
}

// DiscoverOrLaunch tries DiscoverCDPAddr first. If that fails, it launches
// the user's default Chrome/Edge with --remote-debugging-port=0 using the
// user's default profile, waits for the DevToolsActivePort file, and returns
// the CDP address. This preserves login state because it reuses the real profile.
func DiscoverOrLaunch() (string, error) {
	// Fast path: already available.
	if addr, err := DiscoverCDPAddr(); err == nil {
		// Verify the port is actually serving CDP (not just TCP-open).
		if _, err2 := DiscoverTargets(addr); err2 == nil {
			return addr, nil
		}
		log.Printf("[browser] 端口可达但 CDP 无响应，将重新启动浏览器")
	}

	// Detect browser.
	bi := detectBrowser()
	if bi == nil {
		return "", fmt.Errorf("未找到 Chrome 或 Edge 浏览器，请安装后重试")
	}

	// Determine the user's default profile directory so we keep login state.
	userDataDir := defaultUserDataDir(bi.name)
	if userDataDir == "" {
		return "", fmt.Errorf("无法确定浏览器 profile 目录")
	}

	// Check if browser is already running (profile locked).
	if isBrowserRunning(bi.name) {
		// Browser is running but no debug port — we need to restart it.
		log.Printf("[browser] %s 正在运行但未开启调试端口，正在重启...", bi.name)
		killBrowserByName(bi.name)
		// Wait for processes to fully exit and release profile lock.
		waitForProfileUnlock(userDataDir, 12*time.Second)
		// Double-check: if processes are still lingering, force kill again.
		if isBrowserRunning(bi.name) {
			log.Printf("[browser] 浏览器进程仍在运行，再次强制终止...")
			killBrowserByName(bi.name)
			time.Sleep(2 * time.Second)
		}
	}

	// Clean stale DevToolsActivePort file before launch.
	dtapPath := filepath.Join(userDataDir, "DevToolsActivePort")
	os.Remove(dtapPath)
	// Remove lock files that may prevent the new instance from starting properly.
	// Linux/macOS uses SingletonLock; Windows uses lockfile.
	for _, lockName := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "lockfile"} {
		os.Remove(filepath.Join(userDataDir, lockName))
	}

	// Launch with --remote-debugging-port=0 (auto-pick free port).
	args := []string{
		"--remote-debugging-port=0",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + userDataDir,
	}
	cmd := exec.Command(bi.path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	log.Printf("[browser] 启动命令: %s %v", bi.path, args)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动浏览器失败: %w", err)
	}

	// Track the process so we can clean up later.
	managedBrowserMu.Lock()
	managedBrowserProc = cmd.Process
	managedBrowserMu.Unlock()

	// Channel to detect if the browser process exits prematurely.
	procExited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(procExited)
		managedBrowserMu.Lock()
		managedBrowserProc = nil
		managedBrowserMu.Unlock()
	}()

	// Give Chrome a moment to start — if it exits immediately, the profile
	// was likely locked or the args were wrong.
	select {
	case <-procExited:
		// Process already exited. This typically means Chrome delegated to an
		// existing instance (which has no debug port) or hit an error.
		log.Printf("[browser] 浏览器进程已立即退出 (可能转发给了已有实例)")

		// Check if DevToolsActivePort appeared anyway (unlikely but possible).
		time.Sleep(1 * time.Second)
		if fallbackPort, _ := readDevToolsActivePort(); fallbackPort > 0 && probePort(fallbackPort) {
			log.Printf("[browser] 进程退出但发现调试端口: %d", fallbackPort)
			return fmt.Sprintf("http://127.0.0.1:%d", fallbackPort), nil
		}

		// The existing instance (without debug port) swallowed our launch.
		// Force-kill everything and retry once.
		log.Printf("[browser] 强制终止所有浏览器实例并重试启动...")
		killBrowserByName(bi.name)
		waitForProfileUnlock(userDataDir, 10*time.Second)
		// Extra safety: kill again if anything lingers.
		if isBrowserRunning(bi.name) {
			killBrowserByName(bi.name)
			time.Sleep(3 * time.Second)
		}

		// Clean lock files again.
		os.Remove(dtapPath)
		for _, lockName := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie", "lockfile"} {
			os.Remove(filepath.Join(userDataDir, lockName))
		}

		// Retry launch.
		log.Printf("[browser] 重新启动浏览器...")
		cmd2 := exec.Command(bi.path, args...)
		cmd2.Stdout = nil
		cmd2.Stderr = nil
		if err := cmd2.Start(); err != nil {
			return "", fmt.Errorf("重试启动浏览器失败: %w", err)
		}
		managedBrowserMu.Lock()
		managedBrowserProc = cmd2.Process
		managedBrowserMu.Unlock()

		procExited2 := make(chan struct{})
		go func() {
			cmd2.Wait()
			close(procExited2)
			managedBrowserMu.Lock()
			managedBrowserProc = nil
			managedBrowserMu.Unlock()
		}()

		// Check if retry also exits immediately.
		select {
		case <-procExited2:
			log.Printf("[browser] 重试启动也立即退出")
			time.Sleep(1 * time.Second)
			if fp, _ := readDevToolsActivePort(); fp > 0 && probePort(fp) {
				return fmt.Sprintf("http://127.0.0.1:%d", fp), nil
			}
			return "", fmt.Errorf("浏览器两次启动均立即退出。请手动关闭所有浏览器窗口后重试")
		case <-time.After(3 * time.Second):
			// Good — retry process is alive.
		}

		port, err := waitForDevToolsActivePortWithExit(dtapPath, 20*time.Second, procExited2)
		if err != nil {
			if fp, _ := readDevToolsActivePort(); fp > 0 && probePort(fp) {
				return fmt.Sprintf("http://127.0.0.1:%d", fp), nil
			}
			return "", fmt.Errorf("重试启动浏览器后仍未获取调试端口: %w", err)
		}
		retryAddr := fmt.Sprintf("http://127.0.0.1:%d", port)
		log.Printf("[browser] 重试成功，调试端口: %d", port)
		return retryAddr, nil

	case <-time.After(2 * time.Second):
		// Process is still running after 2s — good, continue waiting for port.
	}

	// Wait for DevToolsActivePort file to appear (Chrome writes it after startup).
	port, err := waitForDevToolsActivePortWithExit(dtapPath, 20*time.Second, procExited)
	if err != nil {
		// Fallback: the browser may have delegated to an existing instance.
		// Try reading DevToolsActivePort from the default location one more time.
		if fallbackPort, _ := readDevToolsActivePort(); fallbackPort > 0 && probePort(fallbackPort) {
			log.Printf("[browser] 通过 fallback 发现调试端口: %d", fallbackPort)
			return fmt.Sprintf("http://127.0.0.1:%d", fallbackPort), nil
		}
		return "", fmt.Errorf("浏览器已启动但未能获取调试端口: %w", err)
	}

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	log.Printf("[browser] 已自动启动 %s，调试端口: %d", bi.name, port)
	return addr, nil
}

// waitForDevToolsActivePort polls the DevToolsActivePort file until it appears
// and contains a valid port number, then verifies the port is actually listening.
func waitForDevToolsActivePort(path string, timeout time.Duration) (int, error) {
	return waitForDevToolsActivePortWithExit(path, timeout, nil)
}

// waitForDevToolsActivePortWithExit is like waitForDevToolsActivePort but also
// aborts early if the browser process exits (signalled via procExited channel).
func waitForDevToolsActivePortWithExit(path string, timeout time.Duration, procExited <-chan struct{}) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Check if the browser process has exited.
		if procExited != nil {
			select {
			case <-procExited:
				return 0, fmt.Errorf("浏览器进程在等待调试端口期间退出")
			default:
			}
		}

		data, err := os.ReadFile(path)
		if err == nil {
			lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
			if port, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil && port > 0 {
				if probePort(port) {
					return port, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("等待 DevToolsActivePort 超时 (%v)", timeout)
}

// ── browser detection (mirrors freeproxy/browser.go logic) ──

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
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", procName), "/NH").Output()
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

// killBrowserByName terminates all instances of the given browser.
func killBrowserByName(browserName string) {
	switch runtime.GOOS {
	case "windows":
		var procName string
		if browserName == "chrome" {
			procName = "chrome.exe"
		} else {
			procName = "msedge.exe"
		}
		// /T kills the entire process tree (GPU helper, crashpad, etc.)
		exec.Command("taskkill", "/F", "/T", "/IM", procName).Run()
		// Fallback: enumerate remaining PIDs via tasklist and kill individually.
		// tasklist is available on all Windows versions (unlike wmic which is deprecated).
		out, err := exec.Command("tasklist", "/FI",
			fmt.Sprintf("IMAGENAME eq %s", procName), "/FO", "CSV", "/NH").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				// CSV format: "chrome.exe","12345","Console","1","123,456 K"
				fields := strings.Split(line, ",")
				if len(fields) >= 2 {
					pid := strings.Trim(fields[1], "\" ")
					if pid != "" && pid != "0" {
						exec.Command("taskkill", "/F", "/T", "/PID", pid).Run()
					}
				}
			}
		}
	case "darwin":
		// On macOS, use killall which is more reliable than pkill for app bundles.
		var appName string
		if browserName == "chrome" {
			appName = "Google Chrome"
		} else {
			appName = "Microsoft Edge"
		}
		exec.Command("killall", appName).Run()
		// Also pkill helper processes.
		var helperName string
		if browserName == "chrome" {
			helperName = "Google Chrome Helper"
		} else {
			helperName = "Microsoft Edge Helper"
		}
		exec.Command("killall", helperName).Run()
	default:
		var procName string
		if browserName == "chrome" {
			procName = "chrome"
		} else {
			procName = "msedge"
		}
		exec.Command("pkill", "-f", procName).Run()
	}
}

// waitForProfileUnlock waits until the specified browser processes have fully exited.
// On Windows, Chrome uses named pipes for singleton detection. After processes exit,
// the OS needs extra time to clean up these handles.
func waitForProfileUnlock(userDataDir string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		chromeGone := !isBrowserRunning("chrome")
		edgeGone := !isBrowserRunning("edge")
		if chromeGone && edgeGone {
			// Processes are gone. On Windows, wait extra time for the OS to
			// release named pipes and file handles that Chrome uses for
			// singleton detection. 500ms is not enough — 3s is safe.
			grace := 3 * time.Second
			if runtime.GOOS != "windows" {
				grace = 1 * time.Second
			}
			log.Printf("[browser] 浏览器进程已退出，等待 %v 释放 profile 锁...", grace)
			time.Sleep(grace)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("[browser] waitForProfileUnlock 超时，继续尝试启动...")
}

// readDevToolsActivePort reads the DevToolsActivePort file from known Chrome profile locations.
// Returns (port, wsPath) where wsPath may be empty.
func readDevToolsActivePort() (int, string) {
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
			candidates = append(candidates,
				filepath.Join(localAppData, "Google", "Chrome", "User Data", "DevToolsActivePort"),
				filepath.Join(localAppData, "Chromium", "User Data", "DevToolsActivePort"),
				filepath.Join(localAppData, "Microsoft", "Edge", "User Data", "DevToolsActivePort"),
			)
		}
	}

	for _, p := range candidates {
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
		return port, wsPath
	}
	return 0, ""
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
