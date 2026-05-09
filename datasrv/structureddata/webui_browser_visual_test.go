package structureddata

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebConsoleRendersInHeadlessChromeDesktopAndMobile(t *testing.T) {
	chromePaths := findChromeCandidatesForVisualTest()
	if len(chromePaths) == 0 {
		t.Skip("Chrome or Edge executable not found; browser visual regression requires a real Chromium browser")
	}
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	server := httptest.NewServer(NewHTTPServer(NewService(store, "sqlite"), "", "visual-test").Handler())
	defer server.Close()

	var failures []string
	for _, chromePath := range chromePaths {
		if err := runBrowserVisualRegression(server.URL+"/", chromePath); err != nil {
			failures = append(failures, filepath.Base(chromePath)+": "+err.Error())
			continue
		}
		return
	}
	t.Fatalf("all Chromium visual regression candidates failed:\n%s", strings.Join(failures, "\n"))
}

func runBrowserVisualRegression(pageURL, chromePath string) (err error) {
	chrome, err := startChromeForDevTools(chromePath)
	if err != nil {
		return err
	}
	defer chrome.close()
	done := make(chan error, 1)
	go func() {
		desktop, err := chrome.capturePage(pageURL, 1440, 1000)
		if err != nil {
			done <- err
			return
		}
		mobile, err := chrome.capturePage(pageURL, 390, 900)
		if err != nil {
			done <- err
			return
		}
		if err := screenshotLooksRendered("desktop", desktop, 1440, 1000, 240); err != nil {
			done <- err
			return
		}
		if err := screenshotLooksRendered("mobile", mobile, 390, 900, 120); err != nil {
			done <- err
			return
		}
		if sameSampledPixels(desktop, mobile) {
			done <- fmt.Errorf("desktop and mobile screenshots are unexpectedly identical; responsive layout may not be exercised")
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(90 * time.Second):
		return fmt.Errorf("browser visual regression timed out")
	}
}

type chromeDevTools struct {
	port    int
	cmd     *exec.Cmd
	userDir string
	done    chan error
	stderr  *lockedBuffer
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startChromeForDevTools(chromePath string) (*chromeDevTools, error) {
	userData, err := os.MkdirTemp("", "datasrv-chrome-profile-*")
	if err != nil {
		return nil, fmt.Errorf("create chrome profile dir: %w", err)
	}
	port, err := freeLocalPort()
	if err != nil {
		removeChromeProfile(userData)
		return nil, err
	}
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-crash-reporter",
		"--disable-breakpad",
		"--disable-client-side-phishing-detection",
		"--disable-dev-shm-usage",
		"--disable-default-apps",
		"--disable-domain-reliability",
		"--disable-extensions",
		"--disable-sync",
		"--hide-scrollbars",
		"--metrics-recording-only",
		"--no-service-autorun",
		"--no-proxy-server",
		"--proxy-server=direct://",
		"--proxy-bypass-list=*",
		"--remote-allow-origins=*",
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=" + itoa(port),
		"--user-data-dir=" + userData,
		"about:blank",
	}
	cmd := exec.Command(chromePath, args...)
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		removeChromeProfile(userData)
		return nil, fmt.Errorf("start chrome: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if err := waitForDevToolsHTTP(port, done, 45*time.Second); err != nil {
		killChromeProcessTree(cmd.Process.Pid)
		removeChromeProfile(userData)
		return nil, fmt.Errorf("wait for DevTools endpoint: %w%s", err, chromeStderrSuffix(stderr.String()))
	}
	return &chromeDevTools{port: port, cmd: cmd, userDir: userData, done: done, stderr: stderr}, nil
}

func chromeStderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if len(stderr) > 4000 {
		stderr = stderr[len(stderr)-4000:]
	}
	return "\nchrome stderr:\n" + stderr
}

func freeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate devtools port: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func waitForDevToolsHTTP(port int, done <-chan error, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			return fmt.Errorf("chrome exited before DevTools endpoint became ready: %v", err)
		default:
		}
		resp, err := client.Get(endpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("DevTools endpoint not ready at %s", endpoint)
}

func (c *chromeDevTools) close() {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return
	}
	killChromeProcessTree(c.cmd.Process.Pid)
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
	}
	removeChromeProfile(c.userDir)
}

func removeChromeProfile(path string) {
	if path == "" {
		return
	}
	for i := 0; i < 20; i++ {
		if err := os.RemoveAll(path); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *chromeDevTools) capturePage(pageURL string, width, height int) (image.Image, error) {
	targetURL, err := c.newTarget()
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("connect devtools websocket: %w", err)
	}
	defer conn.Close()
	client := &cdpClient{conn: conn}
	if _, err := client.call("Page.enable", nil); err != nil {
		return nil, err
	}
	if _, err := client.call("Runtime.enable", nil); err != nil {
		return nil, err
	}
	if _, err := client.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             width,
		"height":            height,
		"deviceScaleFactor": 1,
		"mobile":            width < 600,
	}); err != nil {
		return nil, err
	}
	if _, err := client.call("Page.navigate", map[string]any{"url": pageURL}); err != nil {
		return nil, err
	}
	if err := client.waitFor(`document.readyState === "complete" &&
		!!document.querySelector('[data-testid="admin-setup-panel"]') &&
		!!document.querySelector('[data-testid="admin-password-policy"]') &&
		document.querySelector('[data-testid="admin-password-policy"]').textContent.includes('Password policy') &&
		document.querySelector('#serviceStatus').textContent.includes('Service online')`, 15*time.Second); err != nil {
		return nil, err
	}
	resp, err := client.call("Page.captureScreenshot", map[string]any{
		"format":                "png",
		"fromSurface":           true,
		"captureBeyondViewport": false,
	})
	if err != nil {
		return nil, err
	}
	raw, ok := resp["data"].(string)
	if !ok || raw == "" {
		return nil, fmt.Errorf("captureScreenshot returned no data: %#v", resp)
	}
	pngBytes, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode screenshot base64: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot png: %w", err)
	}
	return img, nil
}

func (c *chromeDevTools) newTarget() (string, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/new?%s", c.port, url.QueryEscape("about:blank"))
	req, err := http.NewRequest(http.MethodPut, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create target request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create target: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode target: %w", err)
	}
	if out.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("target missing webSocketDebuggerUrl: %#v", out)
	}
	return out.WebSocketDebuggerURL, nil
}

type cdpClient struct {
	conn *websocket.Conn
	next int
}

func (c *cdpClient) call(method string, params map[string]any) (map[string]any, error) {
	c.next++
	id := c.next
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := c.conn.WriteJSON(msg); err != nil {
		return nil, fmt.Errorf("cdp %s write: %w", method, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		_ = c.conn.SetReadDeadline(deadline)
		var raw map[string]any
		if err := c.conn.ReadJSON(&raw); err != nil {
			return nil, fmt.Errorf("cdp %s read: %w", method, err)
		}
		if rawID, ok := raw["id"].(float64); !ok || int(rawID) != id {
			continue
		}
		if rawErr, ok := raw["error"]; ok {
			return nil, fmt.Errorf("cdp %s error: %#v", method, rawErr)
		}
		if result, ok := raw["result"].(map[string]any); ok {
			return result, nil
		}
		return map[string]any{}, nil
	}
}

func (c *cdpClient) waitFor(expression string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		resp, err := c.call("Runtime.evaluate", map[string]any{
			"expression":    expression,
			"returnByValue": true,
		})
		if err != nil {
			return err
		}
		if result, ok := resp["result"].(map[string]any); ok {
			if value, ok := result["value"].(bool); ok && value {
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for browser expression: %s", expression)
}

func killChromeProcessTree(pid int) {
	if pid <= 0 {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", itoa(pid), "/T", "/F").Run()
		return
	}
	_ = exec.Command("kill", "-TERM", itoa(pid)).Run()
}

func screenshotLooksRendered(name string, img image.Image, width, height, minUnique int) error {
	bounds := img.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		return fmt.Errorf("%s screenshot size=%dx%d, want %dx%d", name, bounds.Dx(), bounds.Dy(), width, height)
	}
	unique := map[uint32]struct{}{}
	nonWhite := 0
	dark := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 4 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 4 {
			r, g, b, _ := img.At(x, y).RGBA()
			rr, gg, bb := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			unique[uint32(rr)<<16|uint32(gg)<<8|uint32(bb)] = struct{}{}
			if rr < 245 || gg < 245 || bb < 245 {
				nonWhite++
			}
			if rr < 80 && gg < 95 && bb < 110 {
				dark++
			}
		}
	}
	if len(unique) < minUnique {
		return fmt.Errorf("%s screenshot has too few sampled colors: got %d want >= %d", name, len(unique), minUnique)
	}
	if nonWhite < (bounds.Dx()*bounds.Dy())/400 {
		return fmt.Errorf("%s screenshot appears blank: sampled non-white pixels=%d", name, nonWhite)
	}
	if dark == 0 {
		return fmt.Errorf("%s screenshot is missing dark navigation/header pixels", name)
	}
	return nil
}

func sameSampledPixels(a, b image.Image) bool {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return false
	}
	for y := ab.Min.Y; y < ab.Max.Y; y += 16 {
		for x := ab.Min.X; x < ab.Max.X; x += 16 {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}

func findChromeCandidatesForVisualTest() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	if path := os.Getenv("CHROME_BIN"); path != "" {
		if _, err := os.Stat(path); err == nil {
			add(path)
		}
	}
	candidates := []string{"google-chrome", "chromium", "chromium-browser", "msedge"}
	if runtime.GOOS == "windows" {
		candidates = append([]string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		}, candidates...)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				add(candidate)
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			add(path)
		}
	}
	return out
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
