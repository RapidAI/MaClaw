package guiapp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

// desktopObservation is one look at the foreground window of the process
// this acceptance started. Image is the full executable path.
type desktopObservation struct {
	Image     string
	Title     string
	Class     string
	Labels    []string
	Focused   string
	FocusKind string
	Covers    bool
	// HasBounds is the foreground window rectangle in screen pixels.
	// A click outside it is not sent. Right and Bottom are exclusive.
	HasBounds bool
	Left      int
	Top       int
	Right     int
	Bottom    int
	// PasswordEdit is a real password control (ES_PASSWORD), not a caption.
	// The observation does not copy that control's text.
	PasswordEdit bool
}

// desktopAcceptanceSession drives one window program. Tests install a fake.
// Production sends input only after the image path matches the built exe.
type desktopAcceptanceSession interface {
	Start(ctx context.Context, exe, dir string) (pid int, kill func(), err error)
	Observe(ctx context.Context, pid int) (desktopObservation, error)
	Click(ctx context.Context, pid, x, y int) error
	Key(ctx context.Context, pid int, key string) error
}

type acceptancePage struct {
	URL   string
	Title string
	Text  string
	// Wall is login, captcha, password, or payment. It is a category, never
	// the control text or a typed value.
	Wall string
}

// acceptanceBrowserSession is an isolated browser. Tests install a fake and
// never start Chrome.
type acceptanceBrowserSession interface {
	Navigate(ctx context.Context, rawURL string) error
	Observe(ctx context.Context) (acceptancePage, error)
	Click(ctx context.Context, x, y int) error
	Press(ctx context.Context, key string) error
	HandToUser(ctx context.Context, reason, pageURL, title string) error
}

type acceptanceListen struct {
	Host     string
	Port     int
	Image    string
	URL      string
	artifact string
}

// acceptanceDisplayProof is how a remote coding turn may drive a UI.
// Proven is true only when this session's display is the user's cloud
// desktop, or a locally dialed loopback belongs to this turn.
type acceptanceDisplayProof struct {
	Proven   bool
	Surface  string
	CDPURL   string
	CDPToken string
}

// acceptanceDisplayBinding is the production identity of this turn's display.
// SameTurn is true only when that identity is the container. An SSH target
// is not the container, and desktopd OpenSession belongs to the desktop bot.
type acceptanceDisplayBinding struct {
	SameTurn bool
	Surface  string
	CDPURL   string
}

// acceptanceRemoteSession drives an in-container channel. It must not write
// the local console buffer or the local foreground.
type acceptanceRemoteSession interface {
	Drive(ctx context.Context, proof acceptanceDisplayProof) (surface string, err error)
}

func acceptanceComputerUseOn(app *App) bool {
	if acceptanceComputerUseForTest != nil {
		return *acceptanceComputerUseForTest
	}
	// A unit test has no config file. Nil means Computer Use is on, matching
	// the product default. An explicit override still wins.
	if app == nil || testing.Testing() {
		return true
	}
	return app.GetComputerUseEnabled()
}

func sessionForDesktopAcceptance() (desktopAcceptanceSession, string) {
	if acceptanceDesktopDriver != nil {
		return acceptanceDesktopDriver, ""
	}
	if testing.Testing() {
		return nil, acceptanceReasonTestNoDesktop
	}
	return platformDesktopAcceptanceSession()
}

func sessionForBrowserAcceptance(listen acceptanceListen) (acceptanceBrowserSession, string) {
	if acceptanceBrowserDriver != nil {
		return acceptanceBrowserDriver, ""
	}
	if testing.Testing() {
		return nil, acceptanceReasonTestNoBrowser
	}
	return platformBrowserAcceptanceSession(listen)
}

func findAcceptanceListens(project string) []acceptanceListen {
	var found []acceptanceListen
	if acceptanceListenFinder != nil {
		if listen, ok := acceptanceListenFinder(project); ok {
			found = append(found, listen)
		}
	} else if !testing.Testing() {
		found = platformAcceptanceListens(project)
	}
	out := make([]acceptanceListen, 0, len(found))
	for _, listen := range found {
		listen, ok := normalizeAcceptanceListen(project, listen)
		if ok {
			out = append(out, listen)
		}
	}
	return out
}

func normalizeAcceptanceListen(project string, listen acceptanceListen) (acceptanceListen, bool) {
	listen.Host = strings.TrimSpace(listen.Host)
	if listen.Host == "" || listen.Port <= 0 {
		return acceptanceListen{}, false
	}
	if !acceptanceLoopbackHost(listen.Host) {
		return acceptanceListen{}, false
	}
	// A loopback port with no image is not this project's process. A test
	// executable is the harness, not the program under test.
	if listen.Image == "" || acceptanceImageIsMaClaw(listen.Image) || isTestExecutableBase(listen.Image) || !pathWithin(project, listen.Image) {
		return acceptanceListen{}, false
	}
	if listen.URL == "" {
		host := listen.Host
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		listen.URL = fmt.Sprintf("http://%s:%d/", host, listen.Port)
	}
	if listen.artifact == "" {
		listen.artifact = listen.URL + "|0|0"
	}
	return listen, true
}

func acceptanceLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func runDesktopAcceptance(ctx context.Context, sess desktopAcceptanceSession, exe, dir string, checks []codingAcceptanceCheck, onCover func()) CodingInteractiveAcceptance {
	acc := CodingInteractiveAcceptance{Kind: acceptanceKindDesktop, Launch: exe, Status: acceptanceStatusFailed}
	if sess == nil {
		acc.Status = acceptanceStatusPlatformBlocked
		acc.Reason = acceptanceReasonTestNoDesktop
		return acc
	}
	var kill func()
	defer func() {
		if kill != nil {
			kill()
		}
		clearAcceptanceDriving()
	}()
	if ctx.Err() != nil {
		acc.Status = acceptanceStatusCancelled
		acc.Reason = acceptanceReasonCancelled
		return acc
	}
	pid, nextKill, err := sess.Start(ctx, exe, dir)
	acc.Attempts = 1
	if err != nil || pid == 0 {
		acc.Status = acceptanceStatusPlatformBlocked
		acc.Reason = "CreateProcess 失败。没有发送键鼠。"
		if err != nil {
			acc.Reason = "CreateProcess 失败：" + err.Error() + "。没有发送键鼠。"
		}
		return acc
	}
	kill = nextKill
	setAcceptanceDriving(exe)
	if len(checks) == 0 {
		checks = defaultAcceptanceChecks()
	}
	if len(checks) > acceptanceDesktopMaxSteps {
		checks = checks[:acceptanceDesktopMaxSteps]
	}
	passed := true
	covered := false
	for _, check := range checks {
		if ctx.Err() != nil {
			acc.Status = acceptanceStatusCancelled
			acc.Reason = acceptanceReasonCancelled
			acc.Checks = append(acc.Checks, failedAcceptanceCheck(check, "cancelled"))
			return acc
		}
		obs, obsErr := sess.Observe(ctx, pid)
		if obsErr != nil {
			acc.Checks = append(acc.Checks, failedAcceptanceCheck(check, "observe_failed"))
			acc.Reason = "观察窗口失败：" + obsErr.Error() + "。没有发送键鼠。"
			passed = false
			break
		}
		if obs.Covers && !covered {
			covered = true
			if onCover != nil {
				onCover()
			}
		}
		result := CodingAcceptanceCheckResult{ID: check.ID, Expect: check.Expect}
		if len(check.Keys) == 1 {
			result.Key = check.Keys[0]
		}
		if acceptanceWindowIsMaClaw(obs) {
			result.Detail = "handoff_refused"
			acc.Checks = append(acc.Checks, result)
			acc.Reason = acceptanceReasonMaClaw
			passed = false
			break
		}
		if !acceptanceSameImage(obs.Image, exe) {
			result.Detail = "image_mismatch"
			acc.Checks = append(acc.Checks, result)
			acc.Reason = "前台映像路径与刚构建的程序不一致，没有点击也没有按键。"
			passed = false
			break
		}
		key := result.Key
		if reason := acceptanceDesktopRefusal(obs, key); reason != "" {
			result.Detail = reason
			acc.Checks = append(acc.Checks, result)
			acc.Reason = "秘密、支付或系统安全界面已拒绝。没有点击，没有按键，也没有等待确认。"
			passed = false
			break
		}
		// expect_rejected is a closed failure. A stored coordinate must not
		// turn that failure into a click, and one coordinate is never a click.
		partialClick := (check.ClickX != nil) != (check.ClickY != nil)
		if check.Expect == "expect_rejected" || partialClick {
			result.Detail = "expect_rejected"
			acc.Checks = append(acc.Checks, result)
			acc.Reason = "这一步不是允许的点击或按键。没有发送键鼠。"
			passed = false
			break
		}
		click := check.ClickX != nil && check.ClickY != nil && len(check.Keys) == 0
		if click {
			if why := acceptanceClickOutside(obs, *check.ClickX, *check.ClickY); why != "" {
				result.Detail = "click_outside"
				acc.Checks = append(acc.Checks, result)
				acc.Reason = why
				passed = false
				break
			}
			if err := sess.Click(ctx, pid, *check.ClickX, *check.ClickY); err != nil {
				result.Detail = "click_failed"
				acc.Checks = append(acc.Checks, result)
				acc.Reason = "点击失败：" + err.Error()
				passed = false
				break
			}
			if detail, why := acceptanceDesktopAfterAction(ctx, sess, pid, exe, key); detail != "" {
				result.Detail = detail
				acc.Checks = append(acc.Checks, result)
				acc.Reason = why
				if detail == "cancelled" {
					acc.Status = acceptanceStatusCancelled
					return acc
				}
				passed = false
				break
			}
			result.Passed = true
			result.Detail = "clicked"
			acc.Checks = append(acc.Checks, result)
			continue
		}
		if !acceptanceKeyAllowed(key) {
			result.Detail = "expect_rejected"
			acc.Checks = append(acc.Checks, result)
			passed = false
			break
		}
		if err := sess.Key(ctx, pid, key); err != nil {
			result.Detail = "key_failed"
			acc.Checks = append(acc.Checks, result)
			acc.Reason = "按键失败：" + err.Error()
			passed = false
			break
		}
		if detail, why := acceptanceDesktopAfterAction(ctx, sess, pid, exe, key); detail != "" {
			result.Detail = detail
			acc.Checks = append(acc.Checks, result)
			acc.Reason = why
			if detail == "cancelled" {
				acc.Status = acceptanceStatusCancelled
				return acc
			}
			passed = false
			break
		}
		result.Passed = true
		result.Detail = "keyed"
		acc.Checks = append(acc.Checks, result)
	}
	if acc.Status == acceptanceStatusCancelled {
		return acc
	}
	if passed {
		acc.Status = acceptanceStatusPassed
		acc.Reason = ""
	} else {
		acc.Status = acceptanceStatusFailed
	}
	return acc
}

// acceptanceDesktopAfterAction re-reads the foreground window after a click
// or key. A pay wall or a different image that appears because of that
// action does not pass, and no further input is sent.
func acceptanceDesktopAfterAction(ctx context.Context, sess desktopAcceptanceSession, pid int, exe, key string) (string, string) {
	if ctx.Err() != nil {
		return "cancelled", acceptanceReasonCancelled
	}
	obs, err := sess.Observe(ctx, pid)
	if err != nil {
		return "observe_failed", "观察窗口失败：" + err.Error() + "。没有继续发送键鼠。"
	}
	if acceptanceWindowIsMaClaw(obs) {
		return "handoff_refused", acceptanceReasonMaClaw
	}
	if !acceptanceSameImage(obs.Image, exe) {
		return "image_mismatch", "动作之后前台映像路径与刚构建的程序不一致。没有继续发送键鼠。"
	}
	if acceptanceDesktopRefusal(obs, key) != "" {
		return "handoff_refused", "动作之后出现秘密、支付或系统安全界面。没有继续发送键鼠，也没有等待确认。"
	}
	return "", ""
}

// acceptanceClickOutside refuses a point that is not inside the foreground
// window. A missing rectangle is refused too, so a click cannot land on
// whatever else is on the desktop.
func acceptanceClickOutside(obs desktopObservation, x, y int) string {
	if !obs.HasBounds || obs.Right <= obs.Left || obs.Bottom <= obs.Top {
		return "没有拿到刚构建窗口的范围。没有发送键鼠。"
	}
	if x < obs.Left || y < obs.Top || x >= obs.Right || y >= obs.Bottom {
		return "点击落在刚构建的窗口之外。没有发送键鼠。"
	}
	return ""
}

func acceptanceSameImage(got, want string) bool {
	if strings.TrimSpace(got) == "" || strings.TrimSpace(want) == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return acceptancePathKey(got) == acceptancePathKey(want)
	}
	return got == want
}

func acceptanceWindowIsMaClaw(obs desktopObservation) bool {
	if acceptanceImageIsMaClaw(obs.Image) {
		return true
	}
	class := strings.ToLower(strings.TrimSpace(obs.Class))
	return class == "wailswindow" || strings.Contains(class, "maclaw")
}

func acceptanceDesktopRefusal(obs desktopObservation, key string) string {
	if obs.PasswordEdit || browser.DesktopSecretSurfaceRefusal(obs.Title) != "" {
		return "handoff_refused"
	}
	policy := computeruse.NewPolicy(computeruse.Config{MaxSteps: acceptanceDesktopMaxSteps})
	if err := policy.AllowClickAt(0, 0, obs.Title); err != nil {
		return "handoff_refused"
	}
	if key != "" {
		if reason, _, _ := browser.DesktopPayKeyRefusal(key, obs.Focused, obs.FocusKind, obs.Labels); reason != "" {
			return "handoff_refused"
		}
	}
	// A visible pay or secret control stops the round even when the next
	// action is an arrow key. The person finishes that step; acceptance does not.
	if acceptanceLabelIsSecret(obs.Focused) {
		return "handoff_refused"
	}
	for _, label := range obs.Labels {
		if browser.DesktopPayClickRefusal(label) != "" || acceptanceLabelIsSecret(label) {
			return "handoff_refused"
		}
	}
	return ""
}

// acceptanceLabelIsSecret reports a password or verification caption. 密码学 is
// a subject title, so removing it must leave a real secret word.
func acceptanceLabelIsSecret(label string) bool {
	n := strings.ToLower(strings.TrimSpace(label))
	if n == "" {
		return false
	}
	n = strings.TrimSpace(strings.ReplaceAll(n, "密码学", " "))
	if n == "" {
		return false
	}
	if browser.DesktopSecretSurfaceRefusal(n) != "" {
		return true
	}
	return strings.Contains(n, "密码")
}

func runWebAcceptance(ctx context.Context, sess acceptanceBrowserSession, listen acceptanceListen, checks []codingAcceptanceCheck) CodingInteractiveAcceptance {
	acc := CodingInteractiveAcceptance{Kind: acceptanceKindWeb, Launch: listen.URL, Status: acceptanceStatusFailed}
	if sess == nil {
		acc.Status = acceptanceStatusPlatformBlocked
		acc.Reason = acceptanceReasonTestNoBrowser
		return acc
	}
	defer clearAcceptanceDriving()
	if err := acceptanceNavigationAllowed(listen.Host, listen.Port, listen.URL); err != nil {
		acc.Reason = err.Error()
		return acc
	}
	if ctx.Err() != nil {
		acc.Status = acceptanceStatusCancelled
		acc.Reason = acceptanceReasonCancelled
		return acc
	}
	setAcceptanceDriving(listen.URL)
	browser.NotifyStage(browser.BrowserStageNotice{Kind: "session", URL: listen.URL, Host: listen.Host, Title: "正在验证刚构建的界面"})
	if len(checks) == 0 {
		checks = []codingAcceptanceCheck{{ID: "open", URL: listen.URL, Expect: "navigate"}}
	}
	passed := true
	opened := false
	for _, check := range checks {
		if ctx.Err() != nil {
			acc.Status = acceptanceStatusCancelled
			acc.Reason = acceptanceReasonCancelled
			return acc
		}
		target := listen.URL
		if strings.TrimSpace(check.URL) != "" {
			target = check.URL
		}
		result := CodingAcceptanceCheckResult{ID: check.ID, Expect: check.Expect}
		if err := acceptanceNavigationAllowed(listen.Host, listen.Port, target); err != nil {
			result.Detail = "navigation_refused"
			acc.Checks = append(acc.Checks, result)
			acc.Reason = err.Error()
			passed = false
			break
		}
		if !opened || target != listen.URL {
			if err := sess.Navigate(ctx, target); err != nil {
				result.Detail = "navigate_failed"
				acc.Checks = append(acc.Checks, result)
				acc.Reason = "隔离浏览器没有打开页面：" + err.Error() + "。没有打开 Computer Use。"
				passed = false
				break
			}
			opened = true
		}
		page, err := sess.Observe(ctx)
		if err != nil {
			result.Detail = "observe_failed"
			acc.Checks = append(acc.Checks, result)
			acc.Reason = "观察页面失败：" + err.Error()
			passed = false
			break
		}
		if detail, why, wall := acceptanceWebPageBlock(page, listen, false); detail != "" {
			if wall != "" {
				acceptanceHandWebToUser(ctx, sess, listen, page, wall)
			}
			result.Detail = detail
			acc.Checks = append(acc.Checks, result)
			acc.Reason = why
			passed = false
			break
		}
		// A rejected or half-specified step is not "the page opened". Do not
		// click, do not press, and do not mark the round passed.
		partialClick := (check.ClickX != nil) != (check.ClickY != nil)
		if check.Expect == "expect_rejected" || partialClick {
			result.Detail = "expect_rejected"
			acc.Checks = append(acc.Checks, result)
			acc.Reason = "网页验收拒绝了这一步。没有点击，也没有打开 Computer Use。"
			passed = false
			break
		}
		if check.ClickX != nil && check.ClickY != nil && len(check.Keys) == 0 {
			if err := sess.Click(ctx, *check.ClickX, *check.ClickY); err != nil {
				if wall, ok := acceptanceClickWall(err); ok {
					acceptanceHandWebToUser(ctx, sess, listen, page, wall)
					result.Detail = "handoff_refused"
					acc.Checks = append(acc.Checks, result)
					acc.Reason = "页面留给用户完成登录、验证码、密码或支付。没有代点，也没有打开 Computer Use。"
					passed = false
					break
				}
				result.Detail = "click_failed"
				acc.Checks = append(acc.Checks, result)
				passed = false
				break
			}
		} else if len(check.Keys) > 0 {
			if len(check.Keys) != 1 || !acceptanceKeyAllowed(check.Keys[0]) {
				result.Detail = "expect_rejected"
				acc.Checks = append(acc.Checks, result)
				acc.Reason = "网页验收不发送这个按键。没有打开 Computer Use。"
				passed = false
				break
			}
			if err := sess.Press(ctx, check.Keys[0]); err != nil {
				if wall, ok := acceptanceClickWall(err); ok {
					acceptanceHandWebToUser(ctx, sess, listen, page, wall)
					result.Detail = "handoff_refused"
					acc.Checks = append(acc.Checks, result)
					acc.Reason = "页面留给用户完成登录、验证码、密码或支付。没有代点，也没有打开 Computer Use。"
					passed = false
					break
				}
				result.Detail = "press_failed"
				acc.Checks = append(acc.Checks, result)
				passed = false
				break
			}
			result.Key = check.Keys[0]
		}
		acted := (check.ClickX != nil && check.ClickY != nil && len(check.Keys) == 0) || len(check.Keys) == 1
		if acted {
			if ctx.Err() != nil {
				acc.Status = acceptanceStatusCancelled
				acc.Reason = acceptanceReasonCancelled
				return acc
			}
			landed, err := sess.Observe(ctx)
			if err != nil {
				result.Detail = "observe_failed"
				acc.Checks = append(acc.Checks, result)
				acc.Reason = "观察页面失败：" + err.Error()
				passed = false
				break
			}
			if detail, why, wall := acceptanceWebPageBlock(landed, listen, true); detail != "" {
				if wall != "" {
					acceptanceHandWebToUser(ctx, sess, listen, landed, wall)
				}
				result.Detail = detail
				acc.Checks = append(acc.Checks, result)
				acc.Reason = why
				passed = false
				break
			}
		}
		result.Passed = true
		result.Detail = "opened"
		acc.Checks = append(acc.Checks, result)
	}
	if acc.Status == acceptanceStatusCancelled {
		return acc
	}
	if passed {
		acc.Status = acceptanceStatusPassed
		acc.Reason = ""
	} else {
		acc.Status = acceptanceStatusFailed
	}
	return acc
}

// acceptanceWebPageBlock says whether this page is still the listened origin
// and still free of a login, captcha, password, or payment wall.
func acceptanceWebPageBlock(page acceptancePage, listen acceptanceListen, acted bool) (detail, reason, wall string) {
	if strings.TrimSpace(page.URL) == "" {
		return "navigation_refused", "隔离浏览器没有留下可核对的页面地址。没有点击，也没有打开 Computer Use。", ""
	}
	if err := acceptanceNavigationAllowed(listen.Host, listen.Port, page.URL); err != nil {
		if acted {
			return "navigation_refused", "动作之后页面离开了监听地址：" + err.Error() + "。没有继续操作，也没有打开 Computer Use。", ""
		}
		return "navigation_refused", "页面离开了监听地址：" + err.Error() + "。没有点击，也没有打开 Computer Use。", ""
	}
	wall = page.Wall
	if wall == "" {
		wall = acceptancePageNeedsUser(page)
	}
	if wall == "" {
		return "", "", ""
	}
	if acted {
		return "handoff_refused", "动作之后页面进入登录、验证码、密码或支付。没有继续操作，也没有打开 Computer Use。", wall
	}
	return "handoff_refused", "页面留给用户完成登录、验证码、密码或支付。没有代点，也没有打开 Computer Use。", wall
}

// acceptanceNavigationAllowed is the acceptance policy. It refuses about:,
// file:, data:, an empty allowlist, a different port, and the other loopback
// name. 127.0.0.1 does not admit localhost. A URL that carries userinfo or a
// backslash is refused without echoing that text.
func acceptanceNavigationAllowed(host string, port int, rawURL string) error {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 {
		return fmt.Errorf("空的允许列表，拒绝导航")
	}
	rawURL = strings.TrimSpace(rawURL)
	if strings.ContainsAny(rawURL, "@\\\r\n\t") || acceptanceURLHasEncodedTrick(rawURL) {
		return fmt.Errorf("拒绝带用户信息或异常字符的导航")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("拒绝导航：%v", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https":
	case "about", "file", "data", "":
		return fmt.Errorf("拒绝 %s 导航", schemeName(scheme))
	default:
		return fmt.Errorf("拒绝 %s 导航", scheme)
	}
	gotHost := strings.ToLower(parsed.Hostname())
	if gotHost != strings.ToLower(host) {
		return fmt.Errorf("允许域是 %s，拒绝 %s", host, gotHost)
	}
	gotPort := parsed.Port()
	if gotPort == "" {
		if scheme == "https" {
			gotPort = "443"
		} else {
			gotPort = "80"
		}
	}
	if gotPort != strconv.Itoa(port) {
		return fmt.Errorf("允许端口是 %d，拒绝 %s", port, gotPort)
	}
	return nil
}

func acceptanceURLHasEncodedTrick(rawURL string) bool {
	lowered := strings.ToLower(rawURL)
	for _, token := range []string{"%5c", "%40", "%00", "%0a", "%0d", "%09"} {
		if strings.Contains(lowered, token) {
			return true
		}
	}
	return false
}

func schemeName(scheme string) string {
	if scheme == "" {
		return "空 scheme"
	}
	return scheme + ":"
}

func acceptancePageNeedsUser(page acceptancePage) string {
	blob := strings.ToLower(page.Title + "\n" + page.Text)
	// 密码学 is a document title, not a password dialog.
	blob = strings.ReplaceAll(blob, "密码学", " ")
	switch {
	case strings.Contains(blob, "验证码") || strings.Contains(blob, "captcha"):
		return "captcha"
	case strings.Contains(blob, "密码") || acceptanceSecretWord.MatchString(blob):
		return "password"
	case strings.Contains(blob, "支付") || strings.Contains(blob, "付款") || strings.Contains(blob, "checkout"):
		return "payment"
	case strings.Contains(blob, "登录") || acceptanceLoginWord.MatchString(blob):
		return "login"
	default:
		return ""
	}
}

func applyRemoteInteractiveAcceptance(summary string) (string, CodingInteractiveAcceptance) {
	proof := acceptanceDecideDisplay()
	if !proof.Proven {
		out := spliceRemoteInteractiveAcceptance(summary)
		if out == summary {
			return summary, CodingInteractiveAcceptance{}
		}
		return out, CodingInteractiveAcceptance{
			Kind:   acceptanceKindRemoteSkipped,
			Status: acceptanceStatusSkipped,
			Reason: acceptanceReasonRemoteSentence,
		}
	}
	surface, err := driveProvenRemote(context.Background(), proof)
	summary = stripAcceptanceExcuseClauses(summary)
	acc := CodingInteractiveAcceptance{Launch: "in-container:" + surface, Attempts: 1}
	if surface == "cdp" {
		acc.Kind = acceptanceKindWeb
	} else {
		acc.Kind = acceptanceKindDesktop
	}
	if err != nil || (surface != "cdp" && surface != "xdotool") {
		acc.Status = acceptanceStatusPlatformBlocked
		acc.Reason = "容器内通道没有接上。没有把本机的控制台输入或键鼠指向另一台机器。"
		if err != nil {
			acc.Reason = err.Error()
		}
		return joinAcceptanceSummary(summary, acc.Reason), acc
	}
	acc.Status = acceptanceStatusPassed
	acc.Launch = "in-container:" + surface
	line := "交互验证走容器内通道 " + surface + "。没有写本机控制台输入，也没有把键鼠指向另一台机器。"
	return joinAcceptanceSummary(summary, line), acc
}

// acceptanceDecideDisplay runs on every remote turn. A nil binding is not
// proven. The test hook is not required for that decision.
func acceptanceDecideDisplay() acceptanceDisplayProof {
	binding := acceptanceDisplayBinding{}
	if acceptanceDisplayBindingForTurn != nil {
		binding = acceptanceDisplayBindingForTurn()
	}
	if proof := acceptanceProofFromBinding(binding); proof.Proven {
		return proof
	}
	if acceptanceProveDisplay != nil {
		return acceptanceProveDisplay()
	}
	return acceptanceDisplayProof{}
}

func acceptanceProofFromBinding(binding acceptanceDisplayBinding) acceptanceDisplayProof {
	if !binding.SameTurn {
		return acceptanceDisplayProof{}
	}
	switch strings.TrimSpace(binding.Surface) {
	case "cdp":
		if !acceptanceCDPTargetAllowed(binding.CDPURL) {
			return acceptanceDisplayProof{}
		}
		return acceptanceDisplayProof{Proven: true, Surface: "cdp", CDPURL: binding.CDPURL}
	case "xdotool":
		return acceptanceDisplayProof{Proven: true, Surface: "xdotool"}
	default:
		return acceptanceDisplayProof{}
	}
}

// acceptanceCDPTargetAllowed admits a loopback debugger socket or the
// in-container tunnel paths. Any other host is not dialed.
func acceptanceCDPTargetAllowed(raw string) bool {
	dialURL, _ := browser.CDPDialTarget(raw)
	parsed, err := url.Parse(strings.TrimSpace(dialURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "ws", "wss", "http", "https":
	default:
		return false
	}
	path := parsed.EscapedPath()
	if strings.Contains(path, "/v1/desktops/tunnel/") || strings.Contains(path, "/api/v1/desktop-cdp/") {
		return true
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func acceptanceHandWebToUser(ctx context.Context, sess acceptanceBrowserSession, listen acceptanceListen, page acceptancePage, wall string) {
	if sess == nil || wall == "" {
		return
	}
	_ = sess.HandToUser(ctx, wall, page.URL, page.Title)
	browser.NotifyStage(browser.BrowserStageNotice{Kind: wall, URL: page.URL, Title: page.Title, Host: listen.Host, Hold: true, Reason: wall})
}

func acceptanceClickWall(err error) (string, bool) {
	var wall *acceptanceClickWallError
	if !errors.As(err, &wall) || wall == nil || wall.wall == "" {
		return "", false
	}
	return wall.wall, true
}

func acceptanceContainerChannelError(surface string) error {
	if testing.Testing() {
		return fmt.Errorf("测试环境没有安装容器内驱动，没有写本机输入")
	}
	return fmt.Errorf("容器内%s通道没有接上。没有把本机的控制台输入或键鼠指向另一台机器。", surface)
}

func driveProvenRemote(ctx context.Context, proof acceptanceDisplayProof) (string, error) {
	// Do not call acceptanceSSHReconnect. A remote UI uses the in-container
	// channel only. sshExecRuntimeBoundContext stays no-reconnect.
	surface := strings.TrimSpace(proof.Surface)
	if surface == "" {
		if strings.TrimSpace(proof.CDPURL) != "" {
			surface = "cdp"
		} else {
			surface = "xdotool"
		}
	}
	if surface != "cdp" && surface != "xdotool" {
		return surface, fmt.Errorf("远端显示证明没有容器内通道。没有把本机的控制台输入或键鼠指向另一台机器。")
	}
	if err := ctx.Err(); err != nil {
		return surface, err
	}
	switch surface {
	case "cdp":
		return "cdp", acceptanceDialContainerCDP(ctx, proof.CDPURL)
	default:
		return "xdotool", acceptanceRunContainerXdotool(ctx)
	}
}

func acceptanceDialContainerCDP(ctx context.Context, raw string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" || !acceptanceCDPTargetAllowed(raw) {
		return fmt.Errorf("容器内cdp通道没有接上。没有把本机的控制台输入或键鼠指向另一台机器。")
	}
	client, err := browser.ConnectCDP(raw)
	if err != nil || client == nil {
		return acceptanceContainerChannelError("cdp")
	}
	_ = client.Close()
	return nil
}

func acceptanceRunContainerXdotool(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if acceptanceInContainerExec == nil {
		return acceptanceContainerChannelError("xdotool")
	}
	return acceptanceInContainerExec(ctx, "xdotool")
}
