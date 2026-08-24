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

type attachedFrame struct {
	TargetID  string
	SessionID string
	URL       string
	Type      string
	OpenerID  string
}

type jsDialogState struct {
	Message string
	Type    string
	URL     string
}

// Session holds the active CDP connection and page state.
type Session struct {
	mu     sync.Mutex
	client *CDPClient
	addr   string // e.g. "http://127.0.0.1:9222"

	activeTabID   string
	activeFrameID string
	recentNetwork []string
	recentErrors  []string

	netMu         sync.Mutex
	inflight      map[string]struct{}
	attached      map[string]attachedFrame
	pendingDialog *jsDialogState
	visionOnce    bool
}

var (
	globalSession        *Session
	globalSessionMu      sync.Mutex
	globalSessionStartMu sync.Mutex
)

// GetSession returns the global browser session, connecting if needed.
// If addr is empty, it auto-discovers the CDP port or automatically launches
// the user's browser with remote debugging enabled (preserving login state).
//
// Production-grade: automatically detects stale connections and reconnects
// transparently so callers always get a working session.
func GetSession(addr string) (*Session, error) {
	if sess := liveGlobalSession(); sess != nil {
		return sess, nil
	}

	globalSessionStartMu.Lock()
	defer globalSessionStartMu.Unlock()
	if sess := liveGlobalSession(); sess != nil {
		return sess, nil
	}

	if _, client := globalSessionSnapshot(); client != nil {
		log.Printf("[browser] CDP connection is dead; reconnecting...")
		clearGlobalSessionIfClient(client)
	}

	if strings.TrimSpace(addr) == "" {
		discovered, err := DiscoverOrLaunch()
		if err != nil {
			return nil, fmt.Errorf("browser connection failed: %w", err)
		}
		addr = discovered
	}

	const maxRetries = 3
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 2 * time.Second
			log.Printf("[browser] CDP retry (%d/%d), waiting %v...", attempt+1, maxRetries, backoff)
			time.Sleep(backoff)
			if newAddr, err := DiscoverCDPAddr(); err == nil {
				addr = newAddr
			}
		}

		session, err := connectToAddr(addr)
		if err != nil {
			lastErr = err
			continue
		}
		if winner := installGlobalSession(session); winner != session {
			session.closeClient()
			return winner, nil
		}
		return session, nil
	}
	return nil, fmt.Errorf("CDP connection failed after %d retries: %w", maxRetries, lastErr)
}

func liveGlobalSession() *Session {
	sess, client := globalSessionSnapshot()
	if client != nil && client.IsAlive() {
		return sess
	}
	return nil
}

func globalSessionSnapshot() (*Session, *CDPClient) {
	globalSessionMu.Lock()
	sess := globalSession
	globalSessionMu.Unlock()
	if sess == nil {
		return nil, nil
	}
	sess.mu.Lock()
	client := sess.client
	sess.mu.Unlock()
	return sess, client
}

func clearGlobalSessionIfClient(client *CDPClient) {
	globalSessionMu.Lock()
	if globalSession == nil {
		globalSessionMu.Unlock()
		return
	}
	globalSession.mu.Lock()
	current := globalSession.client
	globalSession.mu.Unlock()
	clear := current == client || current == nil
	if clear {
		globalSession = nil
	}
	globalSessionMu.Unlock()
	if clear && client != nil {
		client.Close()
	}
}

func installGlobalSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	globalSessionMu.Lock()
	defer globalSessionMu.Unlock()
	if globalSession != nil {
		globalSession.mu.Lock()
		open := globalSession.client != nil && !globalSession.client.isClosed()
		globalSession.mu.Unlock()
		if open {
			return globalSession
		}
	}
	globalSession = session
	return session
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
	_, _ = client.Send("Accessibility.enable", nil, 5*time.Second)
	_, _ = client.Send("Target.setAutoAttach", map[string]interface{}{
		"autoAttach":             true,
		"waitForDebuggerOnStart": false,
		"flatten":                true,
	}, 5*time.Second)
	_, _ = client.Send("Target.setDiscoverTargets", map[string]interface{}{"discover": true}, 5*time.Second)

	return &Session{
		client:        client,
		addr:          addr,
		activeTabID:   activeTargetIDFromTargets(targets),
		activeFrameID: "main",
		inflight:      map[string]struct{}{},
		attached:      map[string]attachedFrame{},
	}, nil
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
	globalSessionStartMu.Lock()
	defer globalSessionStartMu.Unlock()
	globalSessionMu.Lock()
	sess := globalSession
	globalSession = nil
	globalSessionMu.Unlock()
	if sess == nil {
		return
	}
	sess.closeClient()
}

func (s *Session) closeClient() {
	if s == nil {
		return
	}
	s.mu.Lock()
	client := s.client
	s.client = nil
	s.mu.Unlock()
	if client != nil {
		client.Close()
	}
}

// Navigate navigates the current page to the given URL.
func (s *Session) Navigate(url string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	if err := validateNavigationPolicy(BrowserPolicy{AllowCrossOriginNavigation: true}, url, ""); err != nil {
		return "", err
	}
	if reused := s.switchToReusableNavigationTarget(url); reused != "" {
		return reused, nil
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return "", fmt.Errorf("browser session not connected")
	}

	result, err := client.Send("Page.navigate", map[string]interface{}{
		"url": url,
	}, NavTimeout)
	if err != nil {
		return "", err
	}

	// Wait for load without holding s.mu so other session ops are not blocked
	// for the full navigation timeout.
	readyState := waitForLoadOn(client, NavTimeout, 5*time.Second)
	if readyState != "complete" {
		// SPA pages often reach "interactive" long before first render.
		// Give the page a short, non-fatal structural-stability window so
		// follow-up observe/click doesn't hit an empty DOM.
		if err := s.waitForStableOn(client, 3*time.Second, 300*time.Millisecond); err != nil {
			log.Printf("[browser] navigate %q: readyState=%q, stability wait: %v", url, readyState, err)
		}
	}

	return string(result), nil
}

func (s *Session) switchToReusableNavigationTarget(rawURL string) string {
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
	s.mu.Lock()
	oldActiveID := s.activeTabID
	s.mu.Unlock()
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
		if page.ID != oldActiveID {
			if err := s.SwitchPage(page.ID); err != nil {
				return ""
			}
			if oldActiveBlank && oldActiveID != "" && oldActiveID != page.ID {
				s.closeTargetBestEffort(oldActiveID)
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
	if s == nil || strings.TrimSpace(targetID) == "" {
		return
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return
	}
	if _, err := client.Send("Target.closeTarget", map[string]interface{}{"targetId": targetID}, 3*time.Second); err != nil {
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
	return s.clickAtLockedIn(selector, frameScope{})
}

// Type types text into an element matching the CSS selector.
func (s *Session) Type(selector, text string) error {
	return s.TypeContent(selector, text, BrowserContentFormatPlain)
}

// TypeContent types plain text or rich content into an element matching the CSS selector.
func (s *Session) TypeContent(selector, text, contentFormat string) error {
	return s.TypeContentMaybeAppend(selector, text, contentFormat, false)
}

func (s *Session) TypeContentMaybeAppend(selector, text, contentFormat string, appendText bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.typeContentLocked(selector, text, contentFormat, appendText)
}

func (s *Session) typeContentLocked(selector, text, contentFormat string, appendText bool) error {
	return s.typeContentLockedIn(selector, frameScope{}, text, contentFormat, appendText)
}

func (s *Session) typeContentLockedIn(selector string, scope frameScope, text, contentFormat string, appendText bool) error {
	if appendText {
		if err := s.focusEditableLockedIn(selector, scope); err != nil {
			return err
		}
	} else if err := s.prepareEditableLockedIn(selector, scope); err != nil {
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
	return s.prepareEditableLockedIn(selector, frameScope{})
}

func (s *Session) prepareEditableLockedIn(selector string, scope frameScope) error {
	return s.evalCheck(prepareEditableJSIn(selector, scope))
}

func prepareEditableJSIn(selector string, scope frameScope) string {
	return fmt.Sprintf(`
		(function() {
			%s
			let el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
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
	`, pierceFindJS, scopedFindCall(selector, scope))
}

func (s *Session) focusEditableLocked(selector string) error {
	return s.focusEditableLockedIn(selector, frameScope{})
}

func (s *Session) focusEditableLockedIn(selector string, scope frameScope) error {
	return s.evalCheck(focusEditableJSIn(selector, scope))
}

func focusEditableJSIn(selector string, scope frameScope) string {
	return fmt.Sprintf(`
		(function() {
			%s
			const el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
			el.focus();
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, scopedFindCall(selector, scope))
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

func (s *Session) insertMarkdownOnLocked(sessionID, markdown string) error {
	if sessionID == "" {
		return s.insertMarkdownLocked(markdown)
	}
	richHTML := browserMarkdownToHTML(markdown)
	if strings.TrimSpace(richHTML) == "" {
		_, err := s.client.SendOn(sessionID, "Input.insertText", map[string]interface{}{"text": markdown}, DefaultCmdTimeout)
		return err
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
			try {
				const data = new DataTransfer();
				data.setData("text/html", html);
				data.setData("text/plain", plain);
				el.dispatchEvent(new ClipboardEvent("paste", {bubbles: true, cancelable: true, clipboardData: data}));
			} catch (e) {}
			if ((el.textContent || "").trim() === "") {
				document.execCommand("insertHTML", false, html);
			}
			el.dispatchEvent(new InputEvent("input", {bubbles: true, cancelable: true, inputType: "insertFromPaste", data: plain}));
			return JSON.stringify({ok: true, mode: "rich-paste"});
		})()
	`, string(plainJSON), string(htmlJSON))
	return s.evalCheckOnLocked(sessionID, js)
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
	return s.activeElementContainsTextOnLocked("", expected)
}

func (s *Session) activeElementContainsTextOnLocked(sessionID, expected string) bool {
	expectedJSON, _ := json.Marshal(expected)
	js := fmt.Sprintf(`
		(function() {
			const el = document.activeElement;
			if (!el) return "false";
			const v = (el.tagName === "INPUT" || el.tagName === "TEXTAREA") ? (el.value || "") : (el.textContent || "");
			return v.indexOf(%s) >= 0 ? "true" : "false";
		})()
	`, string(expectedJSON))
	var (
		result json.RawMessage
		err    error
	)
	if sessionID == "" {
		result, err = s.client.Send("Runtime.evaluate", map[string]interface{}{
			"expression": js, "returnByValue": true,
		}, 5*time.Second)
	} else {
		result, err = s.client.SendOn(sessionID, "Runtime.evaluate", map[string]interface{}{
			"expression": js, "returnByValue": true,
		}, 5*time.Second)
	}
	if err != nil {
		return false
	}
	return extractStringValue(result) == "true"
}

func insertTextViaJS(text string) string {
	textJSON, _ := json.Marshal(text)
	return fmt.Sprintf(`
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
}

func (s *Session) insertTextViaJSLocked(text string) error {
	return s.evalCheck(insertTextViaJS(text))
}

// Screenshot captures a screenshot of the current page, returns base64 PNG.
func (s *Session) Screenshot(fullPage bool) (string, error) {
	if s == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return "", fmt.Errorf("browser session not connected")
	}

	params := map[string]interface{}{
		"format":  "png",
		"quality": 80,
	}
	if fullPage {
		metrics, err := client.Send("Page.getLayoutMetrics", nil, DefaultCmdTimeout)
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

	result, err := client.Send("Page.captureScreenshot", params, DefaultCmdTimeout)
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
	return s.GetTextInFrame("", selector)
}

func getTextJS(selector string) string {
	return getTextJSIn(selector, frameScope{})
}

func getTextJSIn(selector string, scope frameScope) string {
	return fmt.Sprintf(`
		(function() {
			%s
			const el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
			return JSON.stringify({ok: true, text: el.innerText || el.textContent || ""});
		})()
	`, pierceFindJS, scopedFindCall(selector, scope))
}

// GetHTML returns the outer HTML of an element, or the full page if selector is empty.
func (s *Session) GetHTML(selector string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("browser session not connected")
	}

	var js string
	if selector == "" {
		js = pageHTMLJS()
	} else {
		selJSON, _ := json.Marshal(selector)
		js = fmt.Sprintf(`
			(function() {
				%s
				const el = findInFrames(document, %s);
				if (!el) return JSON.stringify({error: "element not found"});
				return JSON.stringify({ok: true, html: el.outerHTML.substring(0, 50000)});
			})()
		`, pierceFindJS, string(selJSON))
	}

	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	return evalStringOn(client, js, "html")
}

func pageHTMLJS() string {
	return fmt.Sprintf(`(function() {
		%s
		const parts = [];
		function collectHTML(doc) {
			if (!doc) return;
			try { parts.push(doc.documentElement ? doc.documentElement.outerHTML : ''); } catch (e) {}
			function shadows(root) {
				if (!root) return;
				let all = [];
				try { all = root.querySelectorAll('*'); } catch (e) { return; }
				for (const el of all) {
					if (el.shadowRoot) {
						try { parts.push(el.shadowRoot.innerHTML); } catch (e) {}
						shadows(el.shadowRoot);
					}
				}
			}
			shadows(doc);
			for (const f of queryIframes(doc)) {
				try { if (f.contentDocument) collectHTML(f.contentDocument); } catch (e) {}
			}
		}
		collectHTML(document);
		return JSON.stringify({ok: true, html: parts.join('\n').substring(0, 50000)});
	})()`, pierceFindJS)
}

// Eval executes arbitrary JavaScript and returns the result.
func (s *Session) Eval(expression string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return "", fmt.Errorf("browser session not connected")
	}

	result, err := client.Send("Runtime.evaluate", map[string]interface{}{
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

func (s *Session) countSelector(selector string) (int, error) {
	if s == nil || s.client == nil {
		return -1, fmt.Errorf("browser session not connected")
	}
	raw, err := s.Eval(countSelectorJS(selector))
	if err != nil {
		return -1, err
	}
	return parseSelectorCount(raw), nil
}

func (s *Session) countMatchesInFrame(frameID, selector string) (int, error) {
	if s == nil || s.client == nil {
		return -1, fmt.Errorf("browser session not connected")
	}
	sessionID := s.frameSessionID(frameID)
	var (
		raw string
		err error
	)
	switch {
	case sessionID != "":
		raw, err = s.EvalOn(frameID, countSelectorJS(selector))
	case frameID == "":
		raw, err = s.Eval(countSelectorJS(selector))
	case frameID == "main":
		raw, err = s.Eval(countInDocJS(selector))
	default:
		scope, ok := s.scopeFor(frameID)
		if !ok {
			return 0, errFrameGone()
		}
		raw, err = s.Eval(countScopedJS(selector, scope))
	}
	if err != nil {
		return -1, err
	}
	return parseSelectorCount(raw), nil
}

func (s *Session) rejectNonUniqueInFrame(frameID, selector string) error {
	n, err := s.countMatchesInFrame(frameID, selector)
	if err != nil {
		return err
	}
	if n < 0 {
		return nil
	}
	if n > 1 {
		return fmt.Errorf("selector matches %d elements; run observe and click by ref", n)
	}
	if n == 0 {
		return fmt.Errorf("element not found")
	}
	return nil
}

func countSelectorJS(selector string) string {
	selJSON, _ := json.Marshal(selector)
	return fmt.Sprintf(`(function() {
		%s
		try { return JSON.stringify({n: countDeepFrames(document, %s)}); }
		catch (e) { return JSON.stringify({n: -1}); }
	})()`, pierceFindJS, string(selJSON))
}

func countInDocJS(selector string) string {
	selJSON, _ := json.Marshal(selector)
	return fmt.Sprintf(`(function() {
		%s
		try { return JSON.stringify({n: queryAllDeep(document, %s).length}); }
		catch (e) { return JSON.stringify({n: -1}); }
	})()`, pierceFindJS, string(selJSON))
}

func countScopedJS(selector string, scope frameScope) string {
	selJSON, _ := json.Marshal(selector)
	nameJSON, _ := json.Marshal(scope.Name)
	urlJSON, _ := json.Marshal(scope.URL)
	path := scope.Path
	if path == nil {
		path = []int{}
	}
	pathJSON, _ := json.Marshal(path)
	return fmt.Sprintf(`(function() {
		%s
		try { return JSON.stringify({n: countScoped(%s, %s, %s, %s)}); }
		catch (e) { return JSON.stringify({n: -1}); }
	})()`, pierceFindJS, string(selJSON), nameJSON, urlJSON, string(pathJSON))
}

func parseSelectorCount(raw string) int {
	var payload struct {
		N int `json:"n"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return -1
	}
	return payload.N
}

func waitSelectorJS(selector string) string {
	return waitSelectorJSIn(selector, frameScope{})
}

func waitSelectorJSIn(selector string, scope frameScope) string {
	return fmt.Sprintf(`(function() {
		%s
		return %s ? "1" : "0";
	})()`, pierceFindJS, scopedFindCall(selector, scope))
}

// WaitForSelector waits until an element matching the selector appears (up to timeout).
func (s *Session) WaitForSelector(selector string, timeoutSec int) error {
	return s.WaitForSelectorInFrame("", selector, timeoutSec)
}

func (s *Session) WaitForSelectorInFrame(frameID, selector string, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		sessionID := s.frameSessionID(frameID)
		js := waitSelectorJS(selector)
		if sessionID == "" && frameID != "" && frameID != "main" {
			scope, ok := s.scopeFor(frameID)
			if !ok {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			js = waitSelectorJSIn(selector, scope)
		}
		var result string
		var err error
		if sessionID == "" {
			result, err = s.Eval(js)
		} else {
			result, err = s.EvalOn(frameID, waitSelectorJS(selector))
		}
		if err == nil && result == "1" {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("wait for selector timed out (%ds)", timeoutSec)
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

// Select selects an option in a <select> element by value or visible label.
func (s *Session) Select(selector, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evalCheck(selectOptionJS(selector, value))
}

func selectOptionJS(selector, value string) string {
	return selectOptionJSIn(selector, value, frameScope{})
}

func selectOptionJSIn(selector, value string, scope frameScope) string {
	valJSON, _ := json.Marshal(value)
	return fmt.Sprintf(`
		(function() {
			%s
			function applySelect(el, value) {
				const want = String(value == null ? "" : value).trim();
				if (!el) return false;
				if (el.tagName !== "SELECT") {
					el.value = want;
					el.dispatchEvent(new Event("change", {bubbles: true}));
					return true;
				}
				el.value = want;
				if (String(el.value) === want) {
					el.dispatchEvent(new Event("change", {bubbles: true}));
					return true;
				}
				const needle = want.toLowerCase();
				for (const opt of Array.from(el.options || [])) {
					const label = String(opt.textContent || opt.label || "").replace(/\s+/g, " ").trim();
					if (label.toLowerCase() === needle || String(opt.value).toLowerCase() === needle) {
						el.value = opt.value;
						el.dispatchEvent(new Event("change", {bubbles: true}));
						return true;
					}
				}
				return false;
			}
			const el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
			const value = %s;
			if (!applySelect(el, value)) return JSON.stringify({error: "option not found"});
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, scopedFindCall(selector, scope), string(valJSON))
}

// ListPages returns all page targets from the CDP endpoint.
func (s *Session) ListPages() ([]TargetInfo, error) {
	if s == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	return DiscoverTargets(s.addr)
}

type cdpTargetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	OpenerID string `json:"openerId"`
}

func (s *Session) getTargetInfos() ([]cdpTargetInfo, error) {
	if s == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	raw, err := client.Send("Target.getTargets", nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var payload struct {
		TargetInfos []cdpTargetInfo `json:"targetInfos"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse target infos: %w", err)
	}
	return payload.TargetInfos, nil
}

func (s *Session) hydratePopupTargetsFrom(infos []cdpTargetInfo) {
	for _, info := range infos {
		if strings.TrimSpace(info.OpenerID) == "" {
			continue
		}
		s.notePopupTarget(info.TargetID, info.OpenerID, info.Type, info.URL)
	}
}

func pageTargetsFromInfos(infos []cdpTargetInfo) []TargetInfo {
	out := make([]TargetInfo, 0, len(infos))
	for _, info := range infos {
		id := strings.TrimSpace(info.TargetID)
		if id == "" {
			continue
		}
		out = append(out, TargetInfo{ID: id, Type: info.Type, Title: info.Title, URL: info.URL})
	}
	return out
}

// PruneDuplicatePages closes exact duplicate page targets in the managed
// browser profile so retries keep one controlled tab instead of piling up tabs.
func (s *Session) PruneDuplicatePages() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	client := s.client
	activeTabID := s.activeTabID
	addr := s.addr
	s.mu.Unlock()
	if client == nil {
		return 0
	}
	pages, err := DiscoverTargets(addr)
	if err != nil {
		return 0
	}
	activeURL := ""
	for _, page := range pages {
		if page.ID == activeTabID {
			activeURL = normalizeDuplicatePageURL(page.URL)
			break
		}
	}
	log.Printf("[browser] prune duplicate pages scan addr=%s active_tab=%s active_url=%q page_count=%d", addr, activeTabID, activeURL, len(pages))
	seen := map[string]string{}
	if activeURL != "" {
		seen[activeURL] = activeTabID
	}
	closed := 0
	for _, page := range pages {
		if page.Type != "page" || page.ID == "" {
			continue
		}
		if page.ID == activeTabID {
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
		if _, err := client.Send("Target.closeTarget", map[string]interface{}{"targetId": page.ID}, 3*time.Second); err == nil {
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
	return s.jsClickLockedIn(selector, frameScope{})
}

func (s *Session) jsClickLockedIn(selector string, scope frameScope) error {
	return s.jsClickLockedOn("", selector, scope)
}

func (s *Session) jsClickLockedOn(sessionID, selector string, scope frameScope) error {
	js := fmt.Sprintf(`
		(function() {
			%s
			const el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
			el.click();
			if (!el.isConnected) return JSON.stringify({error: "element detached before click took effect"});
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, scopedFindCall(selector, scope))
	if sessionID == "" {
		return s.evalCheck(js)
	}
	return s.evalCheckOnLocked(sessionID, js)
}

func (s *Session) clickAtLocked(selector string) error {
	return s.clickAtLockedIn(selector, frameScope{})
}

func (s *Session) clickAtLockedIn(selector string, scope frameScope) error {
	return s.clickAtLockedOn("", selector, scope)
}

func (s *Session) clickAtLockedOn(sessionID, selector string, scope frameScope) error {
	send := func(method string, params map[string]interface{}) (json.RawMessage, error) {
		if sessionID == "" {
			return s.client.Send(method, params, DefaultCmdTimeout)
		}
		return s.client.SendOn(sessionID, method, params, DefaultCmdTimeout)
	}
	js := fmt.Sprintf(`
		(async function() {
			%s
			const found = %s;
			if (!found) return JSON.stringify({error: "element not found"});
			const el = found.el;
			el.scrollIntoView({block: "center", behavior: "instant"});
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
				try { el.click(); jsClick = !!el.isConnected; } catch (e) {}
			}
			return JSON.stringify({x: x, y: y, tag: el.tagName, occluded: occluded, jsClick: jsClick, vis: document.visibilityState});
		})()
	`, pierceFindJS, scopedLocatedCall(selector, scope))

	evalCoords := func() (*clickCoordResult, error) {
		result, err := send("Runtime.evaluate", map[string]interface{}{
			"expression":    js,
			"returnByValue": true,
			"awaitPromise":  true,
		})
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
		log.Printf("[browser] click %q: page hidden, bringing to front", selector)
		if _, err := send("Page.bringToFront", nil); err == nil {
			if retried, rerr := evalCoords(); rerr == nil {
				coord = retried
			}
		}
	}
	if coord.Vis == "hidden" && !coord.Occluded {
		log.Printf("[browser] click %q: page still hidden, used JS click fallback", selector)
		return s.jsClickLockedOn(sessionID, selector, scope)
	}
	if coord.Occluded {
		if coord.JsClick {
			log.Printf("[browser] click %q: element occluded, used JS click fallback", selector)
			return nil
		}
		return fmt.Errorf("element occluded and JS click fallback failed")
	}

	if _, err := send("Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mouseMoved", "x": coord.X, "y": coord.Y,
	}); err != nil {
		_ = err
	}
	if _, err := send("Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mousePressed", "x": coord.X, "y": coord.Y,
		"button": "left", "clickCount": 1,
	}); err != nil {
		return fmt.Errorf("mousePressed: %w", err)
	}
	_, err = send("Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mouseReleased", "x": coord.X, "y": coord.Y,
		"button": "left", "clickCount": 1,
	})
	return err
}

// SetFiles sets local file paths on a file input element, bypassing the file dialog.
// Uses DOM.setFileInputFiles CDP command.
func (s *Session) SetFiles(selector string, files []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setFilesLocked(selector, frameScope{}, files)
}

func (s *Session) setFilesLocked(selector string, scope frameScope, files []string) error {
	if _, err := s.client.Send("DOM.enable", nil, 5*time.Second); err != nil {
		return err
	}
	js := fmt.Sprintf(`(function(){ %s return %s || null; })()`, pierceFindJS, scopedFindCall(selector, scope))
	result, err := s.client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": false,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	objectID := extractObjectID(result)
	if objectID == "" {
		return fmt.Errorf("element not found")
	}
	desc, err := s.client.Send("DOM.describeNode", map[string]interface{}{"objectId": objectID}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	var node struct {
		Node struct {
			BackendNodeID int `json:"backendNodeId"`
		} `json:"node"`
	}
	if json.Unmarshal(desc, &node) != nil || node.Node.BackendNodeID == 0 {
		return fmt.Errorf("element not found")
	}
	if _, err := s.client.Send("DOM.setFileInputFiles", map[string]interface{}{
		"backendNodeId": node.Node.BackendNodeID,
		"files":         files,
	}, DefaultCmdTimeout); err != nil {
		return fmt.Errorf("setFileInputFiles: %w", err)
	}
	return nil
}

// Back navigates back in browser history and waits for load.
func (s *Session) Back() error {
	if s == nil {
		return fmt.Errorf("browser session not connected")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return fmt.Errorf("browser session not connected")
	}

	_, err := client.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    "history.back()",
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	// Accept "interactive" immediately (tolerance 0) — Back() keeps the old
	// behaviour; the longer SPA-tolerant wait only pays off on Navigate.
	waitForLoadOn(client, NavTimeout, 0)
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
	if s == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("browser session not connected")
	}

	result, err := client.Send("Runtime.evaluate", map[string]interface{}{
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
	if s == nil {
		return fmt.Errorf("browser session not connected")
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return fmt.Errorf("missing target id")
	}
	s.mu.Lock()
	addr := s.addr
	from := s.activeTabID
	s.mu.Unlock()
	log.Printf("[browser] switch page requested addr=%s from=%s to=%s", addr, from, targetID)

	client, err := connectPageTarget(addr, from, targetID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	oldTabID := s.activeTabID
	old := s.client
	s.client = client
	s.activeTabID = targetID
	s.activeFrameID = "main"
	s.mu.Unlock()
	if old != nil && old != client {
		log.Printf("[browser] switch page closing old CDP client addr=%s old=%s new=%s", addr, oldTabID, targetID)
		old.Close()
	}
	log.Printf("[browser] switch page complete addr=%s from=%s to=%s", addr, oldTabID, targetID)
	return nil
}

func connectPageTarget(addr, from, targetID string) (*CDPClient, error) {
	targets, err := DiscoverTargets(addr)
	if err != nil {
		log.Printf("[browser] switch page discover failed addr=%s from=%s to=%s err=%v", addr, from, targetID, err)
		return nil, err
	}
	var wsURL string
	for _, t := range targets {
		if t.ID == targetID {
			wsURL = t.WebSocketDebugURL
			break
		}
	}
	if wsURL == "" {
		log.Printf("[browser] switch page target not found addr=%s from=%s to=%s targets=%d", addr, from, targetID, len(targets))
		return nil, fmt.Errorf("target %s not found", targetID)
	}

	client, err := ConnectCDP(wsURL)
	if err != nil {
		log.Printf("[browser] switch page CDP connect failed addr=%s from=%s to=%s err=%v", addr, from, targetID, err)
		return nil, fmt.Errorf("switch page CDP connection failed: %w", err)
	}
	if _, err := client.Send("Page.enable", nil, 5*time.Second); err != nil {
		log.Printf("[browser] switch page Page.enable failed addr=%s from=%s to=%s err=%v", addr, from, targetID, err)
		client.Close()
		return nil, fmt.Errorf("switch page Page.enable failed: %w", err)
	}
	if _, err := client.Send("Runtime.enable", nil, 5*time.Second); err != nil {
		log.Printf("[browser] switch page Runtime.enable failed addr=%s from=%s to=%s err=%v", addr, from, targetID, err)
		client.Close()
		return nil, fmt.Errorf("switch page Runtime.enable failed: %w", err)
	}
	return client, nil
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

func (s *Session) noteRecentNetwork(line string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.recentNetwork = appendCapped(s.recentNetwork, line, browserAgentNetworkLimit)
	s.mu.Unlock()
}

func (s *Session) noteRecentError(line string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.recentErrors = appendCapped(s.recentErrors, line, browserAgentErrorLimit)
	s.mu.Unlock()
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
	if s == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	return evalStringOn(s.client, js, field)
}

func evalStringOn(client *CDPClient, js, field string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	result, err := client.Send("Runtime.evaluate", map[string]interface{}{
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

func extractObjectID(raw json.RawMessage) string {
	var resp struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return ""
	}
	return resp.Result.ObjectID
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
	if s == nil {
		return ""
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	return waitForLoadOn(client, timeout, interactiveTolerance)
}

func waitForLoadOn(client *CDPClient, timeout, interactiveTolerance time.Duration) string {
	if client == nil {
		return ""
	}
	deadline := time.Now().Add(timeout)
	interactiveSince := time.Time{}
	last := ""
	for time.Now().Before(deadline) {
		result, err := client.Send("Runtime.evaluate", map[string]interface{}{
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
	if s == nil {
		return fmt.Errorf("browser session not connected")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	return s.waitForStableOn(client, timeout, quiet)
}

func (s *Session) waitForStableOn(client *CDPClient, timeout, quiet time.Duration) error {
	if client == nil {
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
	lastMut := -1
	consecutiveErrors := 0
	for time.Now().Before(deadline) {
		result, err := client.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    stabilityProbeJS,
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
		ready, url, _, mut := parseStabilitySignature(sig)
		isReady := ready == "complete" || ready == "interactive"
		structurallyStable := isReady &&
			ready == lastReady &&
			url == lastURL &&
			mut == lastMut &&
			s.inflightCount() == 0
		if structurallyStable {
			if stableSince.IsZero() {
				stableSince = time.Now()
			} else if time.Since(stableSince) >= quiet {
				return nil
			}
		} else {
			lastReady = ready
			lastURL = url
			lastMut = mut
			stableSince = time.Time{}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastReady == "complete" || lastReady == "interactive" {
		log.Printf("[browser] page partially stable ready=%s inflight=%d within %v", lastReady, s.inflightCount(), timeout)
		return nil
	}
	return fmt.Errorf("page not stable within %v (ready=%s)", timeout, lastReady)
}

const stabilityProbeJS = `(function(){
	function ensureMut(doc) {
		if (!doc || !doc.defaultView) return 0;
		const w = doc.defaultView;
		if (!w.__maclawMut) {
			w.__maclawMut = { n: 0 };
			try { new MutationObserver(function () { w.__maclawMut.n++; }).observe(doc, { subtree: true, childList: true, attributes: true, characterData: false }); } catch (e) {}
		}
		return w.__maclawMut.n || 0;
	}
	function walk(doc) {
		let n = ensureMut(doc);
		for (const f of queryIframes(doc)) {
			try { if (f.contentDocument) n += walk(f.contentDocument); } catch (e) {}
		}
		return n;
	}
	function queryIframes(root) {
		const out = [];
		function walkNode(node) {
			if (!node) return;
			let children = [];
			try { children = node.children ? Array.from(node.children) : []; } catch (e) { return; }
			for (const el of children) {
				const tag = String(el.tagName || '').toLowerCase();
				if (tag === 'iframe' || tag === 'frame') out.push(el);
				if (el.shadowRoot) walkNode(el.shadowRoot);
				walkNode(el);
			}
		}
		walkNode(root.documentElement || root);
		if (root.shadowRoot) walkNode(root.shadowRoot);
		return out;
	}
	return JSON.stringify({ready: document.readyState, url: location.href, mut: walk(document)});
})()`

// parseStabilitySignature extracts ready, url, and text length from the
// JSON signature produced by the stability polling expression.
func parseStabilitySignature(sig string) (ready, url string, textLen, mut int) {
	var payload struct {
		Ready string `json:"ready"`
		URL   string `json:"url"`
		Text  int    `json:"text"`
		Mut   int    `json:"mut"`
	}
	if err := json.Unmarshal([]byte(sig), &payload); err == nil {
		return payload.Ready, payload.URL, payload.Text, payload.Mut
	}
	return "", "", 0, 0
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
