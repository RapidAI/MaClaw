package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	DefaultCDPAddr    = "http://127.0.0.1:9222"
	DefaultCmdTimeout = 15 * time.Second
	NavTimeout        = 30 * time.Second
)

// Session holds the active CDP connection and page state.
type Session struct {
	mu     sync.Mutex
	client *CDPClient
	addr   string // e.g. "http://127.0.0.1:9222"

	activeTabID   string
	activeFrameID string
	recentNetwork []string
	recentErrors  []string
}

var (
	globalSession   *Session
	globalSessionMu sync.Mutex
)

// GetSession returns the global browser session, connecting if needed.
// If addr is empty, it auto-discovers the CDP port or automatically launches
// the user's browser with remote debugging enabled (preserving login state).
//
// Production-grade: automatically detects stale connections and reconnects
// transparently so callers always get a working session.
func GetSession(addr string) (*Session, error) {
	globalSessionMu.Lock()
	defer globalSessionMu.Unlock()

	// Fast path: existing session that is still alive.
	if globalSession != nil && globalSession.client != nil {
		if globalSession.client.IsAlive() {
			return globalSession, nil
		}
		// Connection is dead — clean up and reconnect.
		log.Printf("[browser] 检测到 CDP 连接已断开，正在自动重连...")
		globalSession.client.Close()
		globalSession = nil
	}

	// Resolve CDP address (discover or launch).
	if addr == "" {
		discovered, err := DiscoverOrLaunch()
		if err != nil {
			return nil, fmt.Errorf("浏览器连接失败: %w", err)
		}
		addr = discovered
	}

	// Connect with retry — the browser may still be starting up or the port
	// may have changed after a restart.
	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 2 * time.Second // 2s, 4s
			log.Printf("[browser] CDP 连接重试 (%d/%d), 等待 %v...", attempt+1, maxRetries, backoff)
			time.Sleep(backoff)
			// Re-discover in case the port changed (e.g. browser restarted with port=0).
			if newAddr, err := DiscoverCDPAddr(); err == nil {
				addr = newAddr
			}
		}

		session, err := connectToAddr(addr)
		if err != nil {
			lastErr = err
			continue
		}
		globalSession = session
		return globalSession, nil
	}
	return nil, fmt.Errorf("CDP 连接失败 (重试 %d 次): %w", maxRetries, lastErr)
}

// connectToAddr establishes a new CDP session to the given HTTP address.
func connectToAddr(addr string) (*Session, error) {
	targets, err := DiscoverTargets(addr)
	if err != nil {
		return nil, fmt.Errorf("无法获取浏览器页面列表 (%s): %w", addr, err)
	}

	// Find the first "page" target.
	var wsURL string
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebugURL != "" {
			wsURL = t.WebSocketDebugURL
			break
		}
	}
	if wsURL == "" {
		if len(targets) > 0 && targets[0].WebSocketDebugURL != "" {
			wsURL = targets[0].WebSocketDebugURL
		} else {
			return nil, fmt.Errorf("浏览器已连接但未找到可调试的页面")
		}
	}

	client, err := ConnectCDP(wsURL)
	if err != nil {
		return nil, fmt.Errorf("CDP WebSocket 连接失败: %w", err)
	}

	// Enable Page and Runtime domains.
	if _, err := client.Send("Page.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Page.enable 失败: %w", err)
	}
	if _, err := client.Send("Runtime.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Runtime.enable 失败: %w", err)
	}
	if _, err := client.Send("Network.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Network.enable 失败: %w", err)
	}
	if _, err := client.Send("Log.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Log.enable 失败: %w", err)
	}

	return &Session{client: client, addr: addr, activeTabID: activeTargetIDFromTargets(targets), activeFrameID: "main"}, nil
}

func activeTargetIDFromTargets(targets []TargetInfo) string {
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebugURL != "" {
			return t.ID
		}
	}
	if len(targets) > 0 {
		return targets[0].ID
	}
	return ""
}

// CloseSession disconnects the global session.
func CloseSession() {
	globalSessionMu.Lock()
	defer globalSessionMu.Unlock()
	if globalSession != nil && globalSession.client != nil {
		globalSession.client.Close()
		globalSession.client = nil
	}
	globalSession = nil
}

// Navigate navigates the current page to the given URL.
func (s *Session) Navigate(url string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.client.Send("Page.navigate", map[string]interface{}{
		"url": url,
	}, NavTimeout)
	if err != nil {
		return "", err
	}

	// Wait for load event.
	s.waitForLoad(NavTimeout)

	return string(result), nil
}

// Click clicks an element matching the CSS selector.
func (s *Session) Click(selector string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	js := fmt.Sprintf(`
		(function() {
			const el = document.querySelector(%q);
			if (!el) return JSON.stringify({error: "元素未找到: " + %q});
			el.scrollIntoView({block: "center"});
			el.click();
			return JSON.stringify({ok: true, tag: el.tagName, text: (el.textContent||"").substring(0,100)});
		})()
	`, selector, selector)

	return s.evalCheck(js)
}

// Type types text into an element matching the CSS selector.
func (s *Session) Type(selector, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Focus the element first.
	focusJS := fmt.Sprintf(`
		(function() {
			const el = document.querySelector(%q);
			if (!el) return JSON.stringify({error: "元素未找到: " + %q});
			el.focus();
			el.value = "";
			return JSON.stringify({ok: true});
		})()
	`, selector, selector)
	if err := s.evalCheck(focusJS); err != nil {
		return err
	}

	// Use Input.dispatchKeyEvent for each character to trigger JS event handlers.
	for _, ch := range text {
		s.client.Send("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "keyDown",
			"text": string(ch),
		}, DefaultCmdTimeout)
		s.client.Send("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "keyUp",
			"text": string(ch),
		}, DefaultCmdTimeout)
	}
	return nil
}

// Screenshot captures a screenshot of the current page, returns base64 PNG.
func (s *Session) Screenshot(fullPage bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	params := map[string]interface{}{
		"format":  "png",
		"quality": 80,
	}
	if fullPage {
		// Get full page metrics.
		metrics, err := s.client.Send("Page.getLayoutMetrics", nil, DefaultCmdTimeout)
		if err == nil {
			var m struct {
				ContentSize struct {
					Width  float64 `json:"width"`
					Height float64 `json:"height"`
				} `json:"contentSize"`
			}
			if json.Unmarshal(metrics, &m) == nil && m.ContentSize.Width > 0 {
				params["clip"] = map[string]interface{}{
					"x":      0,
					"y":      0,
					"width":  m.ContentSize.Width,
					"height": m.ContentSize.Height,
					"scale":  1,
				}
			}
		}
	}

	result, err := s.client.Send("Page.captureScreenshot", params, DefaultCmdTimeout)
	if err != nil {
		return "", fmt.Errorf("screenshot: %w", err)
	}

	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse screenshot: %w", err)
	}
	return resp.Data, nil
}

// GetText returns the text content of an element matching the CSS selector.
func (s *Session) GetText(selector string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	js := fmt.Sprintf(`
		(function() {
			const el = document.querySelector(%q);
			if (!el) return JSON.stringify({error: "元素未找到: " + %q});
			return JSON.stringify({ok: true, text: el.innerText || el.textContent || ""});
		})()
	`, selector, selector)

	return s.evalString(js, "text")
}

// GetHTML returns the outer HTML of an element, or the full page if selector is empty.
func (s *Session) GetHTML(selector string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var js string
	if selector == "" {
		js = `JSON.stringify({ok: true, html: document.documentElement.outerHTML.substring(0, 50000)})`
	} else {
		js = fmt.Sprintf(`
			(function() {
				const el = document.querySelector(%q);
				if (!el) return JSON.stringify({error: "元素未找到: " + %q});
				return JSON.stringify({ok: true, html: el.outerHTML.substring(0, 50000)});
			})()
		`, selector, selector)
	}

	return s.evalString(js, "html")
}

// Eval executes arbitrary JavaScript and returns the result.
func (s *Session) Eval(expression string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return "", err
	}

	var resp struct {
		Result struct {
			Value interface{} `json:"value"`
			Type  string      `json:"type"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse eval: %w", err)
	}
	if resp.ExceptionDetails != nil {
		return "", fmt.Errorf("JS error: %s", resp.ExceptionDetails.Text)
	}

	switch v := resp.Result.Value.(type) {
	case string:
		return v, nil
	default:
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

// WaitForSelector waits until an element matching the selector appears (up to timeout).
func (s *Session) WaitForSelector(selector string, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	js := fmt.Sprintf(`!!document.querySelector(%q)`, selector)

	for time.Now().Before(deadline) {
		result, err := s.Eval(js)
		if err == nil && result == "true" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("等待元素超时 (%ds): %s", timeoutSec, selector)
}

// Scroll scrolls the page by the given delta (pixels). Positive = down.
func (s *Session) Scroll(deltaX, deltaY int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.client.Send("Input.dispatchMouseEvent", map[string]interface{}{
		"type":   "mouseWheel",
		"x":      100,
		"y":      100,
		"deltaX": deltaX,
		"deltaY": deltaY,
	}, DefaultCmdTimeout)
	return err
}

// Select selects an option in a <select> element.
func (s *Session) Select(selector, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	js := fmt.Sprintf(`
		(function() {
			const el = document.querySelector(%q);
			if (!el) return JSON.stringify({error: "元素未找到: " + %q});
			el.value = %q;
			el.dispatchEvent(new Event('change', {bubbles: true}));
			return JSON.stringify({ok: true});
		})()
	`, selector, selector, value)

	return s.evalCheck(js)
}

// ListPages returns all page targets from the CDP endpoint.
func (s *Session) ListPages() ([]TargetInfo, error) {
	return DiscoverTargets(s.addr)
}

// ClickAt performs a real CDP-level mouse click (Input.dispatchMouseEvent) on an element.
// Unlike Click() which uses JS el.click(), this triggers user gesture events,
// can open file dialogs, and bypasses anti-automation detection.
func (s *Session) ClickAt(selector string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get element coordinates via JS.
	js := fmt.Sprintf(`
		(function() {
			const el = document.querySelector(%q);
			if (!el) return JSON.stringify({error: "元素未找到: " + %q});
			el.scrollIntoView({block: "center"});
			const rect = el.getBoundingClientRect();
			return JSON.stringify({x: rect.x + rect.width/2, y: rect.y + rect.height/2, tag: el.tagName});
		})()
	`, selector, selector)

	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}

	str := extractStringValue(result)
	var coord struct {
		X     float64 `json:"x"`
		Y     float64 `json:"y"`
		Tag   string  `json:"tag"`
		Error string  `json:"error"`
	}
	if err := json.Unmarshal([]byte(str), &coord); err != nil {
		return fmt.Errorf("parse coordinates: %w", err)
	}
	if coord.Error != "" {
		return fmt.Errorf("%s", coord.Error)
	}

	// Dispatch real mouse events.
	if _, err := s.client.Send("Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mousePressed", "x": coord.X, "y": coord.Y,
		"button": "left", "clickCount": 1,
	}, DefaultCmdTimeout); err != nil {
		return fmt.Errorf("mousePressed: %w", err)
	}
	_, err = s.client.Send("Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mouseReleased", "x": coord.X, "y": coord.Y,
		"button": "left", "clickCount": 1,
	}, DefaultCmdTimeout)
	return err
}

// SetFiles sets local file paths on a file input element, bypassing the file dialog.
// Uses DOM.setFileInputFiles CDP command.
func (s *Session) SetFiles(selector string, files []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Enable DOM domain.
	s.client.Send("DOM.enable", nil, 5*time.Second)

	// Get document root.
	docResult, err := s.client.Send("DOM.getDocument", nil, DefaultCmdTimeout)
	if err != nil {
		return fmt.Errorf("DOM.getDocument: %w", err)
	}
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(docResult, &doc); err != nil {
		return fmt.Errorf("parse document: %w", err)
	}

	// Find the file input node.
	nodeResult, err := s.client.Send("DOM.querySelector", map[string]interface{}{
		"nodeId":   doc.Root.NodeID,
		"selector": selector,
	}, DefaultCmdTimeout)
	if err != nil {
		return fmt.Errorf("DOM.querySelector: %w", err)
	}
	var node struct {
		NodeID int `json:"nodeId"`
	}
	if err := json.Unmarshal(nodeResult, &node); err != nil || node.NodeID == 0 {
		return fmt.Errorf("元素未找到: %s", selector)
	}

	// Set files.
	_, err = s.client.Send("DOM.setFileInputFiles", map[string]interface{}{
		"nodeId": node.NodeID,
		"files":  files,
	}, DefaultCmdTimeout)
	if err != nil {
		return fmt.Errorf("setFileInputFiles: %w", err)
	}
	return nil
}

// Back navigates back in browser history and waits for load.
func (s *Session) Back() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    "history.back()",
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	s.waitForLoad(NavTimeout)
	return nil
}

// PageInfo contains basic page information.
type PageInfo struct {
	Title      string `json:"title"`
	URL        string `json:"url"`
	ReadyState string `json:"ready_state"`
}

// Info returns the current page's title, URL, and readyState in one call.
func (s *Session) Info() (*PageInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    `JSON.stringify({title: document.title, url: location.href, ready_state: document.readyState})`,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return nil, err
	}

	str := extractStringValue(result)
	var info PageInfo
	if err := json.Unmarshal([]byte(str), &info); err != nil {
		return nil, fmt.Errorf("parse info: %w", err)
	}
	return &info, nil
}

// TabSnapshot returns the active tab snapshot when available.
func (s *Session) TabSnapshot() *BrowserTabSnapshot {
	if s == nil {
		return nil
	}
	pages, err := s.ListPages()
	if err != nil {
		return nil
	}
	for _, page := range pages {
		if page.ID == s.activeTabID || (s.activeTabID == "" && page.Type == "page") {
			return &BrowserTabSnapshot{TabID: page.ID, URL: page.URL, Title: page.Title, Type: page.Type, Active: true}
		}
	}
	return nil
}

// FrameSnapshots returns the current lightweight frame tree snapshot.
func (s *Session) FrameSnapshots() []BrowserFrameSnapshot {
	if s == nil {
		return nil
	}
	info, err := s.Info()
	if err != nil || info == nil {
		return nil
	}
	frameID := s.activeFrameID
	if frameID == "" {
		frameID = "main"
	}
	return []BrowserFrameSnapshot{{FrameID: frameID, URL: info.URL, Name: info.Title}}
}

// SwitchPage switches to a different page target by its target ID.
func (s *Session) SwitchPage(targetID string) error {
	targets, err := DiscoverTargets(s.addr)
	if err != nil {
		return err
	}
	var wsURL string
	for _, t := range targets {
		if t.ID == targetID {
			wsURL = t.WebSocketDebugURL
			break
		}
	}
	if wsURL == "" {
		return fmt.Errorf("目标 %s 未找到", targetID)
	}

	client, err := ConnectCDP(wsURL)
	if err != nil {
		return fmt.Errorf("切换页面 CDP 连接失败: %w", err)
	}
	if _, err := client.Send("Page.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return fmt.Errorf("切换页面 Page.enable 失败: %w", err)
	}
	if _, err := client.Send("Runtime.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return fmt.Errorf("切换页面 Runtime.enable 失败: %w", err)
	}

	// Swap client under lock.
	s.mu.Lock()
	old := s.client
	s.client = client
	s.activeTabID = targetID
	s.activeFrameID = "main"
	s.mu.Unlock()

	if old != nil {
		old.Close()
	}
	return nil
}

func (s *Session) lastNetworkLines() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recentNetwork...)
}

func (s *Session) lastErrorLines() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recentErrors...)
}

// ── internal helpers ──

func (s *Session) evalCheck(js string) error {
	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	// Check for error in the returned JSON.
	str := extractStringValue(result)
	if strings.Contains(str, `"error"`) {
		var r map[string]interface{}
		if json.Unmarshal([]byte(str), &r) == nil {
			if e, ok := r["error"].(string); ok {
				return fmt.Errorf("%s", e)
			}
		}
	}
	return nil
}

func (s *Session) evalString(js, field string) (string, error) {
	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return "", err
	}
	str := extractStringValue(result)
	var r map[string]interface{}
	if err := json.Unmarshal([]byte(str), &r); err != nil {
		return str, nil
	}
	if e, ok := r["error"].(string); ok {
		return "", fmt.Errorf("%s", e)
	}
	if v, ok := r[field].(string); ok {
		return v, nil
	}
	return str, nil
}

func extractStringValue(raw json.RawMessage) string {
	var resp struct {
		Result struct {
			Value interface{} `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &resp) == nil {
		if s, ok := resp.Result.Value.(string); ok {
			return s
		}
	}
	return string(raw)
}

func (s *Session) waitForLoad(timeout time.Duration) {
	// Simple: poll document.readyState.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    "document.readyState",
			"returnByValue": true,
		}, 3*time.Second)
		if err == nil {
			str := extractStringValue(result)
			if str == "complete" || str == "interactive" {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
}
