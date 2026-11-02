package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PrivateHeadless is a preview-only Chrome or Edge process.
// It starts its own process and does not join the shared browser session.
type PrivateHeadless struct {
	cmd     *exec.Cmd
	dir     string
	session *Session
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
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
		detail := strings.TrimSpace(stderr.String())
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
	if _, err := client.Send("Page.navigate", map[string]interface{}{"url": raw}, NavTimeout); err != nil {
		return "", fmt.Errorf("打开预览页失败: %w", err)
	}
	if err := waitForPreviewLoad(client, raw, 8*time.Second); err != nil {
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

func waitForPreviewLoad(client *CDPClient, want string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	expr := `(function(){ if (document.readyState !== "complete") return ""; if (!location.href || location.href === "about:blank") return ""; return location.href; })()`
	for time.Now().Before(deadline) {
		raw, err := client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, 2*time.Second)
		if err == nil && previewLocationMatches(evaluateString(raw), want) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("预览页未在时限内加载完成")
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

// Close kills the private browser and removes its temporary profile.
func (p *PrivateHeadless) Close() {
	if p == nil {
		return
	}
	if p.session != nil {
		p.session.closeClient()
		p.session = nil
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	if p.dir != "" {
		killManagedBrowserProcessesByDir(p.dir)
		_ = os.RemoveAll(p.dir)
		p.dir = ""
	}
}
