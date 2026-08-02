package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
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
		// Connection is dead; clean up and reconnect.
		log.Printf("[browser] CDP connection is dead; reconnecting...")
		globalSession.client.Close()
		globalSession = nil
	}

	// Resolve CDP address (discover or launch).
	if addr == "" {
		discovered, err := DiscoverOrLaunch()
		if err != nil {
			return nil, fmt.Errorf("browser connection failed: %w", err)
		}
		addr = discovered
	}

	// Connect with retry; the browser may still be starting up or the port
	// may have changed after a restart.
	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 2 * time.Second // 2s, 4s
			log.Printf("[browser] CDP retry (%d/%d), waiting %v...", attempt+1, maxRetries, backoff)
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
	return nil, fmt.Errorf("CDP connection failed after %d retries: %w", maxRetries, lastErr)
}

// connectToAddr establishes a new CDP session to the given HTTP address.
func connectToAddr(addr string) (*Session, error) {
	targets, err := DiscoverTargets(addr)
	if err != nil {
		return nil, fmt.Errorf("get browser targets (%s): %w", addr, err)
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
			return nil, fmt.Errorf("browser connected but no debuggable page found")
		}
	}

	client, err := ConnectCDP(wsURL)
	if err != nil {
		return nil, fmt.Errorf("CDP WebSocket connection failed: %w", err)
	}

	// Enable Page and Runtime domains.
	if _, err := client.Send("Page.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Page.enable failed: %w", err)
	}
	if _, err := client.Send("Runtime.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Runtime.enable failed: %w", err)
	}
	if _, err := client.Send("Network.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Network.enable failed: %w", err)
	}
	if _, err := client.Send("Log.enable", nil, 5*time.Second); err != nil {
		client.Close()
		return nil, fmt.Errorf("CDP Log.enable failed: %w", err)
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
	if reused := s.switchToReusableNavigationTargetLocked(url); reused != "" {
		return reused, nil
	}

	result, err := s.client.Send("Page.navigate", map[string]interface{}{
		"url": url,
	}, NavTimeout)
	if err != nil {
		return "", err
	}

	// Wait for load event. Unlike Back(), navigations tolerate a slow
	// "complete" (SPAs with hanging subresources) before falling back to a
	// structural-stability wait.
	readyState := s.waitForLoad(NavTimeout, 5*time.Second)
	if readyState != "complete" {
		// SPA pages often reach "interactive" long before first render.
		// Give the page a short, non-fatal structural-stability window so
		// follow-up observe/click doesn't hit an empty DOM.
		if err := s.WaitForStable(3*time.Second, 300*time.Millisecond); err != nil {
			log.Printf("[browser] navigate %q: readyState=%q, stability wait: %v", url, readyState, err)
		}
	}

	return string(result), nil
}

func (s *Session) switchToReusableNavigationTarget(rawURL string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.switchToReusableNavigationTargetLocked(rawURL)
}

func (s *Session) switchToReusableNavigationTargetLocked(rawURL string) string {
	if s == nil || strings.TrimSpace(rawURL) == "" {
		return ""
	}
	targetKey := normalizeReusableNavigationURL(rawURL)
	if targetKey == "" || targetKey == "about:blank" || strings.HasPrefix(targetKey, "chrome://") {
		return ""
	}
	pages, err := s.ListPages()
	if err != nil {
		return ""
	}
	oldActiveID := s.activeTabID
	oldActiveBlank := false
	for _, page := range pages {
		if page.ID == oldActiveID && normalizeReusableNavigationURL(page.URL) == "about:blank" {
			oldActiveBlank = true
			break
		}
	}
	for _, page := range pages {
		if page.Type != "page" || page.ID == "" || page.WebSocketDebugURL == "" {
			continue
		}
		if normalizeReusableNavigationURL(page.URL) != targetKey {
			continue
		}
		if page.ID != s.activeTabID {
			if err := s.switchPageLocked(page.ID); err != nil {
				return ""
			}
			if oldActiveBlank && oldActiveID != "" && oldActiveID != page.ID {
				s.closeTargetBestEffortLocked(oldActiveID)
			}
		}
		payload := map[string]interface{}{"reused_target": true, "target_id": page.ID, "url": page.URL, "title": page.Title}
		encoded, _ := json.Marshal(payload)
		log.Printf("[browser] reused existing page target=%s url=%q", page.ID, page.URL)
		return string(encoded)
	}
	return ""
}

func (s *Session) closeTargetBestEffort(targetID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeTargetBestEffortLocked(targetID)
}

func (s *Session) closeTargetBestEffortLocked(targetID string) {
	if s == nil || s.client == nil || strings.TrimSpace(targetID) == "" {
		return
	}
	if _, err := s.client.Send("Target.closeTarget", map[string]interface{}{"targetId": targetID}, 3*time.Second); err != nil {
		log.Printf("[browser] close abandoned blank target failed target=%s err=%v", targetID, err)
	}
}

func normalizeReusableNavigationURL(rawURL string) string {
	return normalizePageURL(rawURL)
}

func normalizeDuplicatePageURL(rawURL string) string {
	return normalizePageURL(rawURL)
}

func normalizePageURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" {
		return normalizeLoosePageURL(rawURL)
	}
	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = normalizePageHost(parsed)
	if parsed.Path != "/" {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	return parsed.String()
}

func normalizeLoosePageURL(rawURL string) string {
	if idx := strings.Index(rawURL, "#"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	return strings.TrimRight(rawURL, "/")
}

func normalizePageHost(parsed *url.URL) string {
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" || (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		if strings.Contains(hostname, ":") {
			return "[" + hostname + "]"
		}
		return hostname
	}
	return net.JoinHostPort(hostname, port)
}

// Click clicks an element matching the CSS selector.
func (s *Session) Click(selector string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clickAtLocked(selector)
}

// Type types text into an element matching the CSS selector.
func (s *Session) Type(selector, text string) error {
	return s.TypeContent(selector, text, BrowserContentFormatPlain)
}

// TypeContent types plain text or rich content into an element matching the CSS selector.
func (s *Session) TypeContent(selector, text, contentFormat string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.prepareEditableLocked(selector); err != nil {
		return err
	}
	if normalizeBrowserContentFormat(contentFormat) == BrowserContentFormatMarkdown {
		return s.insertMarkdownLocked(text)
	}
	return s.insertTextLocked(text)
}

// TypeActive types text into the currently focused editable element.
func (s *Session) TypeActive(text string) error {
	return s.TypeActiveContent(text, BrowserContentFormatPlain)
}

// TypeActiveContent types plain text or rich content into the focused editable element.
func (s *Session) TypeActiveContent(text, contentFormat string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.prepareActiveEditableLocked(); err != nil {
		return err
	}
	if normalizeBrowserContentFormat(contentFormat) == BrowserContentFormatMarkdown {
		return s.insertMarkdownLocked(text)
	}
	return s.insertTextLocked(text)
}

func (s *Session) prepareEditableLocked(selector string) error {
	js := fmt.Sprintf(`
		(function() {
			let el = document.querySelector(%q);
			if (!el) return JSON.stringify({error: "element not found: " + %q});
			if (!(el.isContentEditable || el.tagName === "TEXTAREA" || el.tagName === "INPUT")) {
				el = el.querySelector('textarea,input,[contenteditable="true"],[contenteditable="plaintext-only"],[contenteditable=""]') || el;
			}
			return window.__maclawPrepareEditable ? window.__maclawPrepareEditable(el) : (function(el) {
				const editable = el.isContentEditable || el.tagName === "TEXTAREA" || el.tagName === "INPUT";
				if (!editable) return JSON.stringify({error: "element is not editable: " + el.tagName});
				el.focus();
				if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
					el.value = "";
				} else {
					const range = document.createRange();
					range.selectNodeContents(el);
					const sel = window.getSelection();
					sel.removeAllRanges();
					sel.addRange(range);
					document.execCommand("delete", false, null);
					if ((el.textContent || "") !== "") el.textContent = "";
				}
				el.dispatchEvent(new InputEvent("input", {bubbles: true, cancelable: true, inputType: "deleteContentBackward"}));
				return JSON.stringify({ok: true, tag: el.tagName, contentEditable: !!el.isContentEditable});
			})(el);
		})()
	`, selector, selector)
	return s.evalCheck(js)
}

func (s *Session) prepareActiveEditableLocked() error {
	js := `
		(function() {
			let el = document.activeElement;
			if (!el || el === document.body || el === document.documentElement) {
				return JSON.stringify({error: "no focused editable element"});
			}
			if (!(el.isContentEditable || el.tagName === "TEXTAREA" || el.tagName === "INPUT")) {
				el = el.querySelector('textarea,input,[contenteditable="true"],[contenteditable="plaintext-only"],[contenteditable=""]') || el;
			}
			const editable = el.isContentEditable || el.tagName === "TEXTAREA" || el.tagName === "INPUT";
			if (!editable) return JSON.stringify({error: "focused element is not editable: " + el.tagName});
			el.focus();
			if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
				el.value = "";
			} else {
				const range = document.createRange();
				range.selectNodeContents(el);
				const sel = window.getSelection();
				sel.removeAllRanges();
				sel.addRange(range);
				document.execCommand("delete", false, null);
				if ((el.textContent || "") !== "") el.textContent = "";
			}
			el.dispatchEvent(new InputEvent("input", {bubbles: true, cancelable: true, inputType: "deleteContentBackward"}));
			return JSON.stringify({ok: true, tag: el.tagName, contentEditable: !!el.isContentEditable});
		})()
	`
	return s.evalCheck(js)
}

func (s *Session) insertMarkdownLocked(markdown string) error {
	richHTML := browserMarkdownToHTML(markdown)
	if strings.TrimSpace(richHTML) == "" {
		return s.insertTextLocked(markdown)
	}
	plainJSON, _ := json.Marshal(markdown)
	htmlJSON, _ := json.Marshal(richHTML)
	js := fmt.Sprintf(`
		(function() {
			const el = document.activeElement;
			if (!el || el === document.body || el === document.documentElement) {
				return JSON.stringify({error: "no focused editable element"});
			}
			const plain = %s;
			const html = %s;
			if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
				el.value = plain;
				el.dispatchEvent(new InputEvent("input", {bubbles: true, cancelable: true, inputType: "insertText", data: plain}));
				return JSON.stringify({ok: true, mode: "plain-field"});
			}
			if (!el.isContentEditable) return JSON.stringify({error: "focused element is not contenteditable: " + el.tagName});
			el.focus();
			function clearEditable() {
				const range = document.createRange();
				range.selectNodeContents(el);
				const selection = window.getSelection();
				selection.removeAllRanges();
				selection.addRange(range);
				document.execCommand("delete", false, null);
				if ((el.textContent || "") !== "") el.textContent = "";
			}
			function rawMarkdownStillVisible() {
				const visible = (el.innerText || el.textContent || "").trim();
				const source = plain.trim();
				if (!visible || !source) return false;
				const lines = source.split(/\n+/).map(s => s.trim()).filter(Boolean);
				for (const line of lines) {
					if (/^#{1,6}\s+/.test(line) && visible.includes(line)) return true;
					if (/^[-*+]\s+/.test(line) && visible.includes(line)) return true;
					if (/^\d+[.)]\s+/.test(line) && visible.includes(line)) return true;
					if (/^>\s?/.test(line) && visible.includes(line)) return true;
				}
				if (/\*\*[^*]+\*\*/.test(source) && visible.includes("**")) return true;
				if (/\[[^\]]+\]\(https?:\/\//.test(source) && visible.includes("](")) return true;
				return false;
			}
			const before = el.textContent || "";
			let pasted = false;
			try {
				const data = new DataTransfer();
				data.setData("text/html", html);
				data.setData("text/plain", plain);
				const event = new ClipboardEvent("paste", {bubbles: true, cancelable: true, clipboardData: data});
				el.dispatchEvent(event);
				pasted = (el.textContent || "") !== before && !rawMarkdownStillVisible();
			} catch (e) {}
			if (!pasted || (el.textContent || "").trim() === "") {
				if ((el.textContent || "") !== "") clearEditable();
				document.execCommand("insertHTML", false, html);
			}
			if ((el.textContent || "").trim() === "") {
				return JSON.stringify({error: "rich markdown insert produced empty content"});
			}
			if (rawMarkdownStillVisible()) {
				return JSON.stringify({error: "rich markdown insert left raw markdown visible"});
			}
			el.dispatchEvent(new InputEvent("input", {bubbles: true, cancelable: true, inputType: "insertFromPaste", data: plain}));
			return JSON.stringify({ok: true, mode: "rich-paste"});
		})()
	`, string(plainJSON), string(htmlJSON))
	if err := s.evalCheck(js); err != nil {
		return err
	}
	return nil
}

func (s *Session) insertTextLocked(text string) error {
	if _, err := s.client.Send("Input.insertText", map[string]interface{}{"text": text}, DefaultCmdTimeout); err == nil {
		if s.activeElementContainsTextLocked(text) {
			return nil
		}
		// insertText reported success but nothing landed — seen with CJK/IME
		// text on some contenteditable implementations. Fall through to JS.
		log.Printf("[browser] Input.insertText did not land (len=%d), trying JS fallback", len(text))
	}
	if err := s.insertTextViaJSLocked(text); err == nil {
		if s.activeElementContainsTextLocked(text) {
			return nil
		}
		// Verification disagrees with the JS success claim. Two cases:
		//  a) text landed but was normalized (whitespace etc.) — the field
		//     is non-empty; accept it. Retyping via the per-char path would
		//     clear the field first and then likely lose IME characters
		//     anyway, so it cannot improve on the JS result here;
		//  b) input sanitization swallowed the value (e.g. type=number
		//     rejecting non-numeric text) — the field is empty; fall
		//     through to the per-char path (it clears first, so no
		//     duplication).
		if !s.activeElementIsEmptyLocked() {
			log.Printf("[browser] JS text insert unverified but field non-empty (len=%d), accepting JS result", len(text))
			return nil
		}
		log.Printf("[browser] JS text insert left field empty (len=%d), trying per-char fallback", len(text))
	}
	// Last resort: per-character key events. Only reliable for ASCII; IME
	// characters generally produce nothing here, hence the verification
	// above. Clear first (best-effort) so we never append onto partial
	// content left by a half-failed CDP insertText.
	s.clearActiveEditableLocked()
	for _, ch := range text {
		if _, err := s.client.Send("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "keyDown",
			"text": string(ch),
		}, DefaultCmdTimeout); err != nil {
			return err
		}
		if _, err := s.client.Send("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "keyUp",
			"text": string(ch),
		}, DefaultCmdTimeout); err != nil {
			return err
		}
	}
	// Key events can also be swallowed (hidden page) or ignored (IME
	// characters). Nothing verified the result on this path — at least
	// surface it instead of silently reporting success.
	if !s.activeElementContainsTextLocked(text) {
		log.Printf("[browser] per-char key input unverified (len=%d); text may not have landed", len(text))
	}
	return nil
}

// activeElementIsEmptyLocked reports whether the focused editable element
// has no visible content (empty value / empty trimmed textContent).
func (s *Session) activeElementIsEmptyLocked() bool {
	js := `
		(function() {
			const el = document.activeElement;
			if (!el) return "true";
			const v = (el.tagName === "INPUT" || el.tagName === "TEXTAREA") ? (el.value || "") : (el.textContent || "");
			return v.trim() === "" ? "true" : "false";
		})()
	`
	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, 5*time.Second)
	if err != nil {
		return true // assume empty on eval failure: safest for the caller's fallback
	}
	return extractStringValue(result) == "true"
}

// clearActiveEditableLocked best-effort clears the focused editable element
// (native value setter for INPUT/TEXTAREA, select-all + delete for
// contenteditable). Errors are ignored — callers use it before a last-
// resort input path.
func (s *Session) clearActiveEditableLocked() {
	js := `
		(function() {
			const el = document.activeElement;
			if (!el || el === document.body || el === document.documentElement) {
				return JSON.stringify({error: "no focused editable element"});
			}
			if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
				const proto = el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
				const desc = Object.getOwnPropertyDescriptor(proto, "value");
				if (desc && desc.set) desc.set.call(el, ""); else el.value = "";
				el.dispatchEvent(new InputEvent("input", {bubbles: true, cancelable: true, inputType: "deleteContentBackward"}));
				return JSON.stringify({ok: true});
			}
			if (!el.isContentEditable) return JSON.stringify({error: "focused element is not editable: " + el.tagName});
			el.focus();
			const range = document.createRange();
			range.selectNodeContents(el);
			const selection = window.getSelection();
			selection.removeAllRanges();
			selection.addRange(range);
			document.execCommand("delete", false, null);
			if ((el.textContent || "") !== "") el.textContent = "";
			return JSON.stringify({ok: true});
		})()
	`
	_ = s.evalCheck(js)
}

// activeElementContainsTextLocked reports whether the focused element's
// value/textContent contains the expected text.
func (s *Session) activeElementContainsTextLocked(expected string) bool {
	expectedJSON, _ := json.Marshal(expected)
	js := fmt.Sprintf(`
		(function() {
			const el = document.activeElement;
			if (!el) return "false";
			const v = (el.tagName === "INPUT" || el.tagName === "TEXTAREA") ? (el.value || "") : (el.textContent || "");
			return v.indexOf(%s) >= 0 ? "true" : "false";
		})()
	`, string(expectedJSON))
	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, 5*time.Second)
	if err != nil {
		return false
	}
	return extractStringValue(result) == "true"
}

// insertTextViaJSLocked sets text directly on the focused element: native
// value setter for INPUT/TEXTAREA (so React/Vue see the change), execCommand
// insertText for contenteditable. Both paths dispatch input events.
func (s *Session) insertTextViaJSLocked(text string) error {
	textJSON, _ := json.Marshal(text)
	js := fmt.Sprintf(`
		(function() {
			const el = document.activeElement;
			if (!el || el === document.body || el === document.documentElement) {
				return JSON.stringify({error: "no focused editable element"});
			}
			const text = %s;
			if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") {
				const proto = el.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
				const desc = Object.getOwnPropertyDescriptor(proto, "value");
				if (desc && desc.set) desc.set.call(el, text); else el.value = text;
				el.dispatchEvent(new InputEvent("input", {bubbles: true, cancelable: true, inputType: "insertText", data: text}));
				return JSON.stringify({ok: true, mode: "js-value"});
			}
			if (!el.isContentEditable) return JSON.stringify({error: "focused element is not editable: " + el.tagName});
			el.focus();
			// Clear first: this fallback runs when CDP insertText landed
			// nothing (or partially); inserting at the cursor on top of
			// partial content would duplicate text.
			const range = document.createRange();
			range.selectNodeContents(el);
			const selection = window.getSelection();
			selection.removeAllRanges();
			selection.addRange(range);
			document.execCommand("delete", false, null);
			if ((el.textContent || "") !== "") el.textContent = "";
			if (!document.execCommand("insertText", false, text)) {
				return JSON.stringify({error: "execCommand insertText rejected"});
			}
			return JSON.stringify({ok: true, mode: "js-execcommand"});
		})()
	`, string(textJSON))
	return s.evalCheck(js)
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
			if (!el) return JSON.stringify({error: "element not found: " + %q});
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
				if (!el) return JSON.stringify({error: "element not found: " + %q});
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
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("wait for selector timed out (%ds): %s", timeoutSec, selector)
}

// pageVisibilityLocked returns document.visibilityState ("visible",
// "hidden", ...) or "" when the probe fails.
func (s *Session) pageVisibilityLocked() string {
	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    "document.visibilityState",
		"returnByValue": true,
	}, 5*time.Second)
	if err != nil {
		return ""
	}
	return extractStringValue(result)
}

// Scroll scrolls the page by the given delta (pixels). Positive = down.
func (s *Session) Scroll(deltaX, deltaY int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pageVisibilityLocked() == "hidden" {
		// Chrome swallows input events on hidden pages (background tab /
		// minimized window) — see clickAtLocked. Try foregrounding first.
		log.Printf("[browser] scroll: page hidden, bringing to front")
		_, _ = s.client.Send("Page.bringToFront", nil, DefaultCmdTimeout)
		if s.pageVisibilityLocked() == "hidden" {
			log.Printf("[browser] scroll: page still hidden, used JS scroll fallback")
			js := fmt.Sprintf(`(function() { window.scrollBy(%d, %d); return JSON.stringify({ok: true}); })()`, deltaX, deltaY)
			return s.evalCheck(js)
		}
	}

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
			if (!el) return JSON.stringify({error: "element not found: " + %q});
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

// PruneDuplicatePages closes exact duplicate page targets in the managed
// browser profile so retries keep one controlled tab instead of piling up tabs.
func (s *Session) PruneDuplicatePages() int {
	if s == nil || s.client == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pages, err := s.ListPages()
	if err != nil {
		return 0
	}
	activeURL := ""
	for _, page := range pages {
		if page.ID == s.activeTabID {
			activeURL = normalizeDuplicatePageURL(page.URL)
			break
		}
	}
	log.Printf("[browser] prune duplicate pages scan addr=%s active_tab=%s active_url=%q page_count=%d", s.addr, s.activeTabID, activeURL, len(pages))
	seen := map[string]string{}
	if activeURL != "" {
		seen[activeURL] = s.activeTabID
	}
	closed := 0
	for _, page := range pages {
		if page.Type != "page" || page.ID == "" {
			continue
		}
		if page.ID == s.activeTabID {
			continue
		}
		key := normalizeDuplicatePageURL(page.URL)
		if key == "" || key == "about:blank" || strings.HasPrefix(key, "chrome://") {
			continue
		}
		if _, ok := seen[key]; !ok {
			seen[key] = page.ID
			continue
		}
		log.Printf("[browser] closing duplicate page target=%s url=%q kept_target=%s", page.ID, page.URL, seen[key])
		if _, err := s.client.Send("Target.closeTarget", map[string]interface{}{"targetId": page.ID}, 3*time.Second); err == nil {
			closed++
		} else {
			log.Printf("[browser] close duplicate page failed target=%s url=%q err=%v", page.ID, page.URL, err)
		}
	}
	return closed
}

// clickCoordResult is the JSON payload returned by the click coordinate
// probe script in clickAtLocked.
type clickCoordResult struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Tag      string  `json:"tag"`
	Error    string  `json:"error"`
	Occluded bool    `json:"occluded"`
	JsClick  bool    `json:"jsClick"`
	Vis      string  `json:"vis"`
}

// jsClickLocked clicks an element in-page (main document or same-origin
// iframe) without dispatching mouse events. Used when real input events
// cannot reach the page (e.g. hidden tab/window).
func (s *Session) jsClickLocked(selector string) error {
	js := fmt.Sprintf(`
		(function() {
			function findInFrames(doc, sel) {
				const el = doc.querySelector(sel);
				if (el) return el;
				for (const f of doc.querySelectorAll('iframe')) {
					let child = null;
					try { child = f.contentDocument; } catch (e) {}
					if (!child) continue;
					const found = findInFrames(child, sel);
					if (found) return found;
				}
				return null;
			}
			const el = findInFrames(document, %q);
			if (!el) return JSON.stringify({error: "element not found: " + %q});
			el.click();
			if (!el.isConnected) return JSON.stringify({error: "element detached before click took effect: " + %q});
			return JSON.stringify({ok: true});
		})()
	`, selector, selector, selector)
	return s.evalCheck(js)
}

func (s *Session) clickAtLocked(selector string) error {
	// Get element coordinates via JS. The script:
	//  1. searches the main document and same-origin iframes (frame offsets
	//     are accumulated so coordinates land in the top-frame viewport);
	//  2. scrolls instantly and waits two animation frames so smooth-scroll
	//     animations don't leave us with a stale bounding rect;
	//  3. checks occlusion via elementFromPoint — a covered element falls
	//     back to a JS el.click() instead of clicking the overlay on top.
	js := fmt.Sprintf(`
		(async function() {
			function findInFrames(doc, sel, chain) {
				const el = doc.querySelector(sel);
				if (el) return {el: el, chain: chain};
				const iframes = doc.querySelectorAll('iframe');
				for (const f of iframes) {
					let child = null;
					try { child = f.contentDocument; } catch (e) {}
					if (!child) continue;
					const found = findInFrames(child, sel, chain.concat([f]));
					if (found) return found;
				}
				return null;
			}
			const found = findInFrames(document, %q, []);
			if (!found) return JSON.stringify({error: "element not found: " + %q});
			const el = found.el;
			el.scrollIntoView({block: "center", behavior: "instant"});
			// Wait up to two animation frames for layout/scroll to settle.
			// Skip entirely on hidden pages: rAF never fires there and
			// Chrome throttles timers on long-hidden pages (down to once
			// per minute), which could hang this eval past the CDP
			// timeout — and a hidden page has no rendering to settle.
			if (document.visibilityState !== "hidden") {
				await new Promise(function(resolve) {
					let done = false;
					const finish = function() { if (!done) { done = true; resolve(); } };
					requestAnimationFrame(function() { requestAnimationFrame(finish); });
					setTimeout(finish, 250);
				});
			}
			const rect = el.getBoundingClientRect();
			let x = rect.x + rect.width / 2;
			let y = rect.y + rect.height / 2;
			for (const frame of found.chain) {
				const fr = frame.getBoundingClientRect();
				x += fr.x;
				y += fr.y;
			}
			let occluded = false;
			let jsClick = false;
			const hit = el.ownerDocument.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
			if (hit && hit !== el && !el.contains(hit) && !(hit.contains && hit.contains(el))) {
				occluded = true;
			}
			// For elements inside iframes, also check the TOP document at the
			// accumulated viewport coordinates: a top-level overlay covering
			// the whole iframe is invisible to the in-frame check above.
			if (!occluded && found.chain.length > 0) {
				const topHit = document.elementFromPoint(x, y);
				if (topHit) {
					let inChain = false;
					for (const frame of found.chain) {
						if (topHit === frame || (frame.contains && frame.contains(topHit))) { inChain = true; break; }
					}
					if (!inChain) occluded = true;
				}
			}
			if (occluded) {
				// el.click() on a detached element does not throw — it just
				// fires on a node nobody sees. Only count it as clicked
				// when the element is still in the document.
				try { el.click(); jsClick = !!el.isConnected; } catch (e) {}
			}
			return JSON.stringify({x: x, y: y, tag: el.tagName, occluded: occluded, jsClick: jsClick, vis: document.visibilityState});
		})()
	`, selector, selector)

	evalCoords := func() (*clickCoordResult, error) {
		result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    js,
			"returnByValue": true,
			"awaitPromise":  true,
		}, DefaultCmdTimeout)
		if err != nil {
			return nil, err
		}
		str := extractStringValue(result)
		var coord clickCoordResult
		if err := json.Unmarshal([]byte(str), &coord); err != nil {
			return nil, fmt.Errorf("parse coordinates: %w", err)
		}
		if coord.Error != "" {
			return nil, fmt.Errorf("%s", coord.Error)
		}
		return &coord, nil
	}

	coord, err := evalCoords()
	if err != nil {
		return err
	}
	if coord.Vis == "hidden" && !coord.Occluded {
		// Chrome swallows Input.dispatchMouseEvent on hidden pages
		// (background tab / minimized window): the first click just
		// activates the window and the events never reach the renderer.
		// Bring the tab to the front and re-measure once.
		log.Printf("[browser] click %q: page hidden, bringing to front", selector)
		if _, err := s.client.Send("Page.bringToFront", nil, DefaultCmdTimeout); err == nil {
			if retried, rerr := evalCoords(); rerr == nil {
				coord = retried
			}
		}
	}
	if coord.Vis == "hidden" && !coord.Occluded {
		// Still hidden (e.g. window could not be foregrounded) — real mouse
		// events would be swallowed, so click in-page instead.
		log.Printf("[browser] click %q: page still hidden, used JS click fallback", selector)
		return s.jsClickLocked(selector)
	}
	if coord.Occluded {
		if coord.JsClick {
			// Element was covered by an overlay; a coordinate click would
			// have hit the overlay, so the in-page el.click() fallback
			// already did the job.
			log.Printf("[browser] click %q: element occluded, used JS click fallback", selector)
			return nil
		}
		// Occluded and the JS fallback failed (e.g. element detached
		// mid-eval) — a coordinate click here would hit the overlay, i.e.
		// the WRONG element. Fail instead of misclicking.
		return fmt.Errorf("element occluded and JS click fallback failed: %s", selector)
	}

	// Dispatch real mouse events.
	// Move the mouse to the target first — React/Vue SPA frameworks rely on
	// mousemove/mouseover to update internal hover state before processing
	// click. Without this, some components ignore mousePressed entirely.
	if _, err := s.client.Send("Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mouseMoved", "x": coord.X, "y": coord.Y,
	}, DefaultCmdTimeout); err != nil {
		// Non-fatal: some CDP targets don't accept mouseMoved pre-click.
		_ = err
	}
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
		return fmt.Errorf("element not found: %s", selector)
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
	// Accept "interactive" immediately (tolerance 0) — Back() keeps the old
	// behaviour; the longer SPA-tolerant wait only pays off on Navigate.
	s.waitForLoad(NavTimeout, 0)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.switchPageLocked(targetID)
}

func (s *Session) switchPageLocked(targetID string) error {
	log.Printf("[browser] switch page requested addr=%s from=%s to=%s", s.addr, s.activeTabID, targetID)
	targets, err := DiscoverTargets(s.addr)
	if err != nil {
		log.Printf("[browser] switch page discover failed addr=%s from=%s to=%s err=%v", s.addr, s.activeTabID, targetID, err)
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
		log.Printf("[browser] switch page target not found addr=%s from=%s to=%s targets=%d", s.addr, s.activeTabID, targetID, len(targets))
		return fmt.Errorf("target %s not found", targetID)
	}

	client, err := ConnectCDP(wsURL)
	if err != nil {
		log.Printf("[browser] switch page CDP connect failed addr=%s from=%s to=%s err=%v", s.addr, s.activeTabID, targetID, err)
		return fmt.Errorf("switch page CDP connection failed: %w", err)
	}
	if _, err := client.Send("Page.enable", nil, 5*time.Second); err != nil {
		log.Printf("[browser] switch page Page.enable failed addr=%s from=%s to=%s err=%v", s.addr, s.activeTabID, targetID, err)
		client.Close()
		return fmt.Errorf("switch page Page.enable failed: %w", err)
	}
	if _, err := client.Send("Runtime.enable", nil, 5*time.Second); err != nil {
		log.Printf("[browser] switch page Runtime.enable failed addr=%s from=%s to=%s err=%v", s.addr, s.activeTabID, targetID, err)
		client.Close()
		return fmt.Errorf("switch page Runtime.enable failed: %w", err)
	}

	oldTabID := s.activeTabID
	old := s.client
	s.client = client
	s.activeTabID = targetID
	s.activeFrameID = "main"

	if old != nil {
		log.Printf("[browser] switch page closing old CDP client addr=%s old=%s new=%s", s.addr, oldTabID, targetID)
		old.Close()
	}
	log.Printf("[browser] switch page complete addr=%s from=%s to=%s", s.addr, oldTabID, targetID)
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

// internal helpers
func (s *Session) evalCheck(js string) error {
	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	// Check for a structured error in the returned JSON.
	str := extractStringValue(result)
	var r struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(str), &r) == nil {
		if r.Error != "" {
			return fmt.Errorf("%s", r.Error)
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

// waitForLoad polls document.readyState until "complete" or the timeout
// expires. Pages with long-polling/hanging subresources may stay at
// "interactive" for a long time, so after interactiveTolerance stuck at
// "interactive" we accept it and return (0 = accept immediately, the old
// behaviour). It returns the last observed readyState so callers can tell
// a fully loaded page apart from an interactive-but-still-rendering SPA.
func (s *Session) waitForLoad(timeout, interactiveTolerance time.Duration) string {
	deadline := time.Now().Add(timeout)
	interactiveSince := time.Time{}
	last := ""
	for time.Now().Before(deadline) {
		result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    "document.readyState",
			"returnByValue": true,
		}, 3*time.Second)
		if err == nil {
			last = extractStringValue(result)
			switch last {
			case "complete":
				return last
			case "interactive":
				if interactiveSince.IsZero() {
					interactiveSince = time.Now()
				}
				if time.Since(interactiveSince) >= interactiveTolerance {
					return last
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return last
}

func (s *Session) WaitForStable(timeout, quiet time.Duration) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("browser session not connected")
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if quiet <= 0 {
		quiet = 250 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	stableSince := time.Time{}
	lastReady := ""
	lastURL := ""
	lastTextLen := 0
	consecutiveErrors := 0
	for time.Now().Before(deadline) {
		result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    `JSON.stringify({ready:document.readyState,url:location.href,text:(document.body&&(document.body.innerText||document.body.textContent)||'').length})`,
			"returnByValue": true,
		}, 2*time.Second)
		if err != nil {
			consecutiveErrors++
			// If CDP is consistently failing, bail early instead of polling
			// until deadline. 3 consecutive failures = connection is dead.
			if consecutiveErrors >= 3 {
				return fmt.Errorf("page stability check failed: %w", err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		consecutiveErrors = 0
		sig := extractStringValue(result)
		ready, url, textLen := parseStabilitySignature(sig)
		isReady := ready == "complete" || ready == "interactive"
		// Structural stability: readyState and URL must match exactly;
		// text length must be within ±5% (tolerates minor dynamic content
		// like clocks, counters, blinking cursors on SPA pages).
		structurallyStable := isReady &&
			ready == lastReady &&
			url == lastURL &&
			textLenWithinTolerance(textLen, lastTextLen, 0.05)
		if structurallyStable {
			if stableSince.IsZero() {
				stableSince = time.Now()
			} else if time.Since(stableSince) >= quiet {
				return nil
			}
		} else {
			lastReady = ready
			lastURL = url
			lastTextLen = textLen
			stableSince = time.Time{}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("page not stable within %v", timeout)
}

// parseStabilitySignature extracts ready, url, and text length from the
// JSON signature produced by the stability polling expression.
func parseStabilitySignature(sig string) (ready, url string, textLen int) {
	// Fast path: parse the simple JSON without full unmarshal.
	var payload struct {
		Ready string `json:"ready"`
		URL   string `json:"url"`
		Text  int    `json:"text"`
	}
	if err := json.Unmarshal([]byte(sig), &payload); err == nil {
		return payload.Ready, payload.URL, payload.Text
	}
	return "", "", 0
}

// textLenWithinTolerance returns true if current and previous text lengths
// are within the given fractional tolerance of each other.
func textLenWithinTolerance(current, previous int, tolerance float64) bool {
	if previous == 0 && current == 0 {
		return true
	}
	if previous == 0 {
		// First measurement — treat as "changed" so we record it.
		return false
	}
	diff := current - previous
	if diff < 0 {
		diff = -diff
	}
	return float64(diff) <= float64(previous)*tolerance
}
