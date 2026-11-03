package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PrivateHeadless is a preview-only Chrome or Edge process.
// It starts its own process and does not join the shared browser session.
type PrivateHeadless struct {
	cmd       *exec.Cmd
	dir       string
	session   *Session
	closeOnce sync.Once
}

// StartPrivateHeadless launches a new headless browser with a temporary
// profile and a system-assigned debugging port.
func StartPrivateHeadless() (*PrivateHeadless, error) {
	bi := detectBrowser()
	if bi == nil {
		return nil, fmt.Errorf("未找到 Chrome 或 Edge")
	}
	dir, err := os.MkdirTemp("", "maclaw-preview-chrome-")
	if err != nil {
		return nil, err
	}
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--remote-debugging-port=0",
		"--remote-allow-origins=*",
		"--user-data-dir=" + dir,
		"--window-size=1280,800",
		"about:blank",
	}
	cmd := exec.Command(bi.path, args...)
	stderr := &tailBuf{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("启动预览浏览器失败: %w", err)
	}
	proc := &PrivateHeadless{cmd: cmd, dir: dir}
	go func() { _ = cmd.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	addr, err := waitForPrivateDebugPort(ctx, dir)
	if err != nil {
		proc.Close()
		detail := strings.TrimSpace(stderr.tail())
		if len(detail) > 400 {
			detail = detail[len(detail)-400:]
		}
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	session, err := connectToAddr(addr)
	if err != nil {
		proc.Close()
		return nil, err
	}
	proc.session = session
	return proc, nil
}

func waitForPrivateDebugPort(ctx context.Context, dir string) (string, error) {
	for {
		if info, ok := readDevToolsActivePortFile(filepath.Join(dir, "DevToolsActivePort")); ok {
			addr := fmt.Sprintf("http://127.0.0.1:%d", info.Port)
			if cdpVersionReady(addr) {
				return addr, nil
			}
		}
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("预览浏览器未就绪: %w", err)
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", fmt.Errorf("预览浏览器未就绪: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func cdpVersionReady(addr string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(addr + "/json/version")
	if err != nil {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK && strings.Contains(string(body), "Browser")
}

// ScreenshotURL captures the viewport after the document finishes loading.
// Only loopback HTTP URLs are accepted. A page that does not reach
// readyState complete is an error, not a blank image.
func (p *PrivateHeadless) ScreenshotURL(raw string) (string, error) {
	if p == nil || p.session == nil || p.session.client == nil {
		return "", fmt.Errorf("预览浏览器未连接")
	}
	if !previewCaptureURLAllowed(raw) {
		return "", fmt.Errorf("预览截图只打开本地预览地址")
	}
	client := p.session.client
	_, _ = client.Send("Emulation.setDeviceMetricsOverride", map[string]interface{}{
		"width": 1280, "height": 800, "deviceScaleFactor": 1, "mobile": false,
	}, 5*time.Second)
	// The browser is reused for later edits. Opening the address it is already
	// showing does not load the new stylesheet, so that visit is reloaded
	// after the current document is marked.
	_, _ = client.Send("Network.setCacheDisabled", map[string]interface{}{"cacheDisabled": true}, 5*time.Second)
	current, _, _ := previewPageState(client)
	stale := ""
	if previewLocationMatches(current, raw) {
		stale = strconv.FormatInt(time.Now().UnixNano(), 10)
		_, _ = client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    fmt.Sprintf(`document.documentElement&&(document.documentElement.dataset.mcShot=%q)`, stale),
			"returnByValue": true,
		}, 2*time.Second)
		if _, err := client.Send("Page.reload", map[string]interface{}{"ignoreCache": true}, previewNavigateTimeout); err != nil {
			return "", fmt.Errorf("打开预览页失败: %w", err)
		}
	} else {
		navRaw, err := client.Send("Page.navigate", map[string]interface{}{"url": raw}, previewNavigateTimeout)
		if err != nil {
			return "", fmt.Errorf("打开预览页失败: %w", err)
		}
		var nav struct {
			ErrorText string `json:"errorText"`
		}
		if json.Unmarshal(navRaw, &nav) == nil && strings.TrimSpace(nav.ErrorText) != "" {
			return "", fmt.Errorf("打开预览页失败: %s", nav.ErrorText)
		}
	}
	if err := waitForPreviewLoad(client, raw, stale, 8*time.Second); err != nil {
		return "", err
	}
	shot, err := client.Send("Page.captureScreenshot", map[string]interface{}{
		"format": "png",
		"clip": map[string]interface{}{
			"x": 0, "y": 0, "width": 1280, "height": 800, "scale": 1,
		},
		"captureBeyondViewport": false,
	}, 15*time.Second)
	if err != nil {
		return "", fmt.Errorf("预览截图失败: %w", err)
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(shot, &resp); err != nil || strings.TrimSpace(resp.Data) == "" {
		return "", fmt.Errorf("预览截图为空")
	}
	return resp.Data, nil
}

const previewNavigateTimeout = 15 * time.Second

func waitForPreviewLoad(client *CDPClient, want, stale string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	expr := `(function(){ var href=""; if (document.readyState==="complete" && location.href && location.href!=="about:blank") href=location.href; var fonts=""; try { fonts=(document.fonts&&document.fonts.status)||""; } catch (e) {} var shot=""; try { shot=(document.documentElement&&document.documentElement.dataset.mcShot)||""; } catch (e) {} return href+"\n"+fonts+"\n"+shot; })()`
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("预览页未在时限内加载完成")
		}
		raw, err := client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, 2*time.Second)
		href, fonts, shot := splitPreviewLoadState(evaluateString(raw))
		if err == nil && previewCaptureReady(href, want, fonts, shot, stale, remaining) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func previewPageState(client *CDPClient) (href, fonts, shot string) {
	raw, err := client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    `(function(){ var href=""; try { href=location.href||""; } catch (e) {} var fonts=""; try { fonts=(document.fonts&&document.fonts.status)||""; } catch (e) {} var shot=""; try { shot=(document.documentElement&&document.documentElement.dataset.mcShot)||""; } catch (e) {} return href+"\n"+fonts+"\n"+shot; })()`,
		"returnByValue": true,
	}, 2*time.Second)
	if err != nil {
		return "", "", ""
	}
	return splitPreviewLoadState(evaluateString(raw))
}

func splitPreviewLoadState(raw string) (href, fonts, shot string) {
	parts := strings.SplitN(raw, "\n", 3)
	if len(parts) > 0 {
		href = parts[0]
	}
	if len(parts) > 1 {
		fonts = parts[1]
	}
	if len(parts) > 2 {
		shot = parts[2]
	}
	return href, fonts, shot
}

// previewDocumentReady is true once the requested page has loaded. Fonts that
// are still loading get a short wait, then the capture proceeds.
func previewDocumentReady(href, want, fonts string, remaining time.Duration) bool {
	return previewCaptureReady(href, want, fonts, "", "", remaining)
}

// previewCaptureReady rejects the document that was on screen before a reload.
// A matching address and a finished readyState are not enough: that is the
// page from before the stylesheet changed.
func previewCaptureReady(href, want, fonts, shot, stale string, remaining time.Duration) bool {
	if !previewLocationMatches(href, want) {
		return false
	}
	if stale != "" && shot == stale {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(fonts), "loading") && remaining > 1500*time.Millisecond {
		return false
	}
	return true
}

func previewLocationMatches(got, want string) bool {
	if !previewCaptureURLAllowed(got) {
		return false
	}
	left, errL := url.Parse(got)
	right, errR := url.Parse(want)
	if errL != nil || errR != nil {
		return false
	}
	if !strings.EqualFold(left.Hostname(), right.Hostname()) || left.Port() != right.Port() {
		return false
	}
	return previewPathKey(left) == previewPathKey(right)
}

func previewPathKey(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

func evaluateString(raw json.RawMessage) string {
	var payload struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return payload.Result.Value
}

func previewCaptureURLAllowed(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "127.0.0.1" || host == "localhost"
}

// Close disconnects immediately. Killing the browser and deleting its profile
// continue after return so a finished turn does not wait on process cleanup.
func (p *PrivateHeadless) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		if p.session != nil {
			p.session.closeClient()
			p.session = nil
		}
		cmd := p.cmd
		dir := p.dir
		p.cmd = nil
		p.dir = ""
		if cmd == nil && dir == "" {
			return
		}
		go releasePrivateHeadless(cmd, dir)
	})
}

func releasePrivateHeadless(cmd *exec.Cmd, dir string) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if strings.TrimSpace(dir) == "" {
		return
	}
	killManagedBrowserProcessesByDir(dir)
	_ = os.RemoveAll(dir)
}

type tailBuf struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = append([]byte(nil), t.b[len(t.b)-4096:]...)
	}
	return len(p), nil
}

func (t *tailBuf) tail() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
