package browser

import (
	"fmt"
	"log"
	"strings"
)

// SessionMode determines how the browser session connects.
type SessionMode string

const (
	// SessionModeAuto tries to connect to the user's running Chrome first,
	// then falls back to launching Chrome with the user's real profile.
	SessionModeAuto SessionMode = "auto"

	// SessionModeConnectUser only connects to the user's running Chrome.
	// If not available, returns an error with guidance instead of launching.
	SessionModeConnectUser SessionMode = "connect_user"

	// SessionModeIsolated launches Chrome with an isolated debug profile
	// (no user cookies/passwords). Used for testing/crawling.
	SessionModeIsolated SessionMode = "isolated"
)

// ConnectUserChrome connects to the user's already-running Chrome/Edge instance
// by reading the DevToolsActivePort file. It does NOT launch a new browser.
//
// Prerequisites: user must have enabled remote debugging in
// chrome://inspect/#remote-debugging (Chrome 146+).
//
// Returns the CDP HTTP address (e.g. "http://127.0.0.1:9222") on success.
func ConnectUserChrome() (string, error) {
	info, ok := readDevToolsActivePort()
	if !ok {
		return "", fmt.Errorf("未找到 DevToolsActivePort 文件。%s", UserChromeGuideMessage())
	}

	addr := fmt.Sprintf("http://127.0.0.1:%d", info.Port)

	// Verify the port is actually serving CDP.
	if !probePort(info.Port) {
		return "", fmt.Errorf("DevToolsActivePort 文件存在 (source=%s) 但端口 %d 不可达。Chrome 可能已关闭。", info.Source, info.Port)
	}

	// Verify CDP is responding (not just TCP open).
	if _, err := DiscoverTargets(addr); err != nil {
		return "", fmt.Errorf("端口 %d 可达但 CDP 未响应: %v。Chrome 可能未启用远程调试。", info.Port, err)
	}

	log.Printf("[browser] 已连接用户 Chrome (port=%d, source=%s)", info.Port, info.Source)
	return addr, nil
}

// IsUserChromeAvailable checks if the user's Chrome is available for direct connection.
// Returns (available, description).
func IsUserChromeAvailable() (bool, string) {
	info, ok := readDevToolsActivePort()
	if !ok {
		return false, "DevToolsActivePort 文件不存在，远程调试未启用"
	}
	if !probePort(info.Port) {
		return false, fmt.Sprintf("DevToolsActivePort 存在但端口 %d 不可达", info.Port)
	}
	return true, fmt.Sprintf("用户 Chrome 可连接 (port=%d)", info.Port)
}

// UserChromeGuideMessage returns a user-friendly guide for enabling remote debugging.
func UserChromeGuideMessage() string {
	return strings.TrimSpace(`
请在 Chrome 中启用远程调试：
1. 确保 Chrome 版本 ≥ 146
2. 打开 chrome://inspect/#remote-debugging
3. 勾选"允许远程调试"（Allow remote debugging）
4. 完成后重试操作

注：Edge 浏览器同样支持，打开 edge://inspect/#remote-debugging 即可。
`)
}

// DiscoverOrLaunchUserProfile tries to connect to the user's running Chrome first.
// If that fails, launches Chrome with the user's REAL profile (preserving cookies,
// passwords, extensions) and remote debugging enabled.
//
// This is the default connection strategy (SessionModeAuto):
//   1. ConnectUserChrome() — connect to running instance
//   2. Launch with user's real user-data-dir + --remote-debugging-port
//   3. If user-data-dir is locked (Chrome already running without debug),
//      return error with guidance
func DiscoverOrLaunchUserProfile() (string, error) {
	// Step 1: Try connecting to already-running Chrome.
	if addr, err := ConnectUserChrome(); err == nil {
		return addr, nil
	}

	// Step 2: Try launching with user's real profile.
	bi := detectBrowser()
	if bi == nil {
		return "", fmt.Errorf("未找到 Chrome 或 Edge 浏览器，请安装后重试")
	}

	userDataDir := defaultUserDataDir(bi.name)
	if userDataDir == "" {
		// Fallback to isolated profile if we can't determine user data dir.
		log.Printf("[browser] 无法确定用户数据目录，回退到隔离 profile")
		return DiscoverOrLaunch()
	}

	// Check if the browser is already running (profile will be locked).
	if isBrowserRunning(bi.name) {
		return "", fmt.Errorf(
			"%s 正在运行但未启用远程调试。\n\n解决方法（二选一）：\n"+
				"方法一：在浏览器中启用远程调试\n%s\n\n"+
				"方法二：关闭 %s 后重试（系统将用你的 profile 启动带调试功能的 %s）",
			bi.name, UserChromeGuideMessage(), bi.name, bi.name,
		)
	}

	// Browser not running — launch with user's real profile.
	log.Printf("[browser] 用户 %s 未运行，使用用户 profile 启动: %s", bi.name, userDataDir)
	addr, err := launchDebugBrowser(bi, userDataDir, debugPort)
	if err != nil {
		// Profile lock error — another instance may have started between our check and launch.
		if classifyBrowserLaunchError(err).IsProfileLockLikely() {
			return "", fmt.Errorf(
				"使用用户 profile 启动 %s 失败（profile 可能被锁定）。\n\n"+
					"请关闭所有 %s 窗口后重试，或在已运行的 %s 中启用远程调试：\n%s",
				bi.name, bi.name, bi.name, UserChromeGuideMessage(),
			)
		}
		return "", err
	}

	return addr, nil
}

// isUserChromeSession checks if the session is connected to the user's Chrome
// (as opposed to an isolated instance). Used for display purposes.
func isUserChromeSession(sess *BrowserAgentSession) bool {
	if sess == nil {
		return false
	}
	// In auto mode, if we didn't use the isolated debug profile dir, it's the user's Chrome.
	// Simple heuristic: check if the addr was NOT from our managed debug profile.
	return sess.Mode != SessionModeIsolated
}
