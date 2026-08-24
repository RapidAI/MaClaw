package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const captchaAskQuestion = "页面出现人机验证（滑块/reCAPTCHA）。请在浏览器中完成验证后继续。继续之后必须先 observe，再 click/type。"

// browserPageFlagsCollectJS assumes pageText/anyIframeSrc/anySelector exist.
const browserPageFlagsCollectJS = `
  const fullText = pageText();
  const excerptLimit = 1200;
  const lower = fullText.toLowerCase();
  const widgetIframe = anyIframeSrc('recaptcha') || anyIframeSrc('hcaptcha') || anyIframeSrc('funcaptcha');
  const slider = /拖动滑块/.test(fullText);
  const classCaptcha = anySelector('[class*="captcha"], [id*="captcha"]');
  const genericCaptchaIframe = anyIframeSrc('captcha');
  const captchaWidget = widgetIframe || slider || (classCaptcha && genericCaptchaIframe);
  const pageFlags = {
    captcha_widget: captchaWidget,
    captcha: /captcha|verify you are human|unusual traffic|安全验证|人机验证/.test(lower) || genericCaptchaIframe || classCaptcha,
    login_wall: anySelector('input[type=password]') && /login|sign in|登录|密码/.test(lower),
    mfa: /verification code|2fa|one-time|otp|authenticator|验证码/.test(lower),
    canvas: anySelector('canvas')
  };
`

// browserPageFlagsScript is a flags-only peek: no SoM, no refs, no screenshots.
const browserPageFlagsScript = `(function () {
  function pageText() {
    const parts = [];
    function collect(root) {
      if (!root) return;
      try {
        if (root.body) parts.push(String(root.body.innerText || root.body.textContent || ''));
        else parts.push(String(root.innerText || root.textContent || ''));
      } catch (e) {}
      let all = [];
      try { all = root.querySelectorAll('*'); } catch (e) { return; }
      for (const el of all) {
        if (el.shadowRoot) collect(el.shadowRoot);
      }
    }
    function walk(d) {
      if (!d) return;
      collect(d);
      for (const f of queryIframes(d)) {
        try { if (f.contentDocument) walk(f.contentDocument); } catch (e) {}
      }
    }
    walk(document);
    return String(parts.join(' ')).replace(/\s+/g, ' ').trim();
  }
  function queryAllDeep(root, selector) {
    const out = [];
    function walk(node) {
      if (!node || !node.querySelectorAll) return;
      try { out.push.apply(out, node.querySelectorAll(selector)); } catch (e) {}
      let all = [];
      try { all = node.querySelectorAll('*'); } catch (e) { return; }
      for (const el of all) {
        if (el.shadowRoot) walk(el.shadowRoot);
      }
    }
    walk(root);
    return out;
  }
  function queryIframes(root) {
    const out = [];
    function walk(node) {
      if (!node) return;
      let children = [];
      try { children = node.children ? Array.from(node.children) : []; } catch (e) { return; }
      for (const el of children) {
        const tag = String(el.tagName || '').toLowerCase();
        if (tag === 'iframe' || tag === 'frame') out.push(el);
        if (el.shadowRoot) walk(el.shadowRoot);
        walk(el);
      }
    }
    walk(root.documentElement || root);
    if (root.shadowRoot) walk(root.shadowRoot);
    return out;
  }
  function anySelector(sel) {
    function scan(d) {
      try { if (queryAllDeep(d, sel).length) return true; } catch (e) {}
      for (const f of queryIframes(d)) {
        try { if (f.contentDocument && scan(f.contentDocument)) return true; } catch (e) {}
      }
      return false;
    }
    return scan(document);
  }
  function anyIframeSrc(needle) {
    const n = String(needle || '').toLowerCase();
    function scan(d) {
      for (const f of queryIframes(d)) {
        try {
          if (String(f.src || '').toLowerCase().includes(n)) return true;
          if (f.contentDocument && scan(f.contentDocument)) return true;
        } catch (e) {}
      }
      return false;
    }
    return scan(document);
  }
` + browserPageFlagsCollectJS + `
  return JSON.stringify({ page_flags: pageFlags });
})()`

func isCaptchaMutatingAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "click", "type", "press", "select", "set_files", "dialog":
		return true
	default:
		return false
	}
}

func captchaAskUserRequest(ctx string) *agent.AskUserRequest {
	ctx = strings.TrimSpace(ctx)
	if ctx == "" {
		ctx = "browser captcha_widget"
	}
	return &agent.AskUserRequest{
		Question:  captchaAskQuestion,
		Options:   []string{"继续"},
		Context:   ctx,
		InputType: "confirm",
	}
}

func captchaWidgetFromSignals(iframeSrcs []string, pageText string, classOrIDCaptcha bool) bool {
	slider := strings.Contains(pageText, "拖动滑块")
	widgetIframe := false
	genericCaptchaIframe := false
	for _, src := range iframeSrcs {
		lower := strings.ToLower(src)
		if strings.Contains(lower, "recaptcha") || strings.Contains(lower, "hcaptcha") || strings.Contains(lower, "funcaptcha") {
			widgetIframe = true
		}
		if strings.Contains(lower, "captcha") {
			genericCaptchaIframe = true
		}
	}
	if widgetIframe || slider {
		return true
	}
	return classOrIDCaptcha && genericCaptchaIframe
}

func (s *BrowserAgentSession) lastSnapshotFlags() (BrowserPageFlags, bool) {
	if s == nil {
		return BrowserPageFlags{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastSnapshotID == "" || s.snapshots == nil {
		return BrowserPageFlags{}, false
	}
	snap := s.snapshots[s.lastSnapshotID]
	if snap == nil {
		return BrowserPageFlags{}, false
	}
	return snap.PageFlags, true
}

func (s *BrowserAgentSession) peekPageFlags() (BrowserPageFlags, error) {
	if s == nil || s.session == nil {
		return BrowserPageFlags{}, fmt.Errorf("browser session not connected")
	}
	raw, err := s.session.Eval(browserPageFlagsScript)
	if err != nil {
		return BrowserPageFlags{}, err
	}
	var payload observePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return BrowserPageFlags{}, fmt.Errorf("parse page flags: %w", err)
	}
	return payload.PageFlags, nil
}

func shouldAskCaptchaWidget(hasSnapshot, snapshotWidget, didPeek bool, peekedWidget bool, peekErr error) bool {
	if hasSnapshot && !snapshotWidget {
		return false
	}
	if didPeek {
		if peekErr != nil {
			return true
		}
		return peekedWidget
	}
	return hasSnapshot && snapshotWidget
}

func (s *BrowserAgentSession) captchaAskIfNeeded(action string) *BrowserActionResult {
	if !isCaptchaMutatingAction(action) {
		return nil
	}
	flags, hasSnapshot := s.lastSnapshotFlags()
	if hasSnapshot && !flags.CaptchaWidget {
		return nil
	}
	didPeek := false
	var peeked BrowserPageFlags
	var peekErr error
	if s != nil && s.session != nil && (!hasSnapshot || flags.CaptchaWidget) {
		didPeek = true
		peeked, peekErr = s.peekPageFlags()
	}
	if !shouldAskCaptchaWidget(hasSnapshot, flags.CaptchaWidget, didPeek, peeked.CaptchaWidget, peekErr) {
		return nil
	}
	return s.captchaAskResult(action)
}

func (s *BrowserAgentSession) captchaAskResult(action string) *BrowserActionResult {
	url := ""
	title := ""
	sessionID := ""
	if s != nil {
		s.mu.RLock()
		sessionID = s.ID
		if s.snapshots != nil && s.lastSnapshotID != "" {
			if snap := s.snapshots[s.lastSnapshotID]; snap != nil {
				url = snap.URL
				title = snap.Title
			}
		}
		s.mu.RUnlock()
	}
	ctx := strings.TrimSpace(strings.Join([]string{title, url}, " "))
	return &BrowserActionResult{
		SessionID: sessionID,
		Action:    "browser_" + strings.TrimPrefix(action, "browser_"),
		Status:    "ask",
		Display:   "page has a captcha challenge; solve it in the browser, then observe before clicking",
		Detail:    "captcha_widget",
		AskUser:   captchaAskUserRequest(ctx),
		Data: map[string]interface{}{
			"reason":     "captcha_widget",
			"page_flags": BrowserPageFlags{CaptchaWidget: true},
			"url":        url,
			"title":      title,
		},
	}
}

func (s *BrowserAgentSession) rememberSubmitClickIfOK(key string, result *BrowserActionResult) {
	if s == nil || result == nil || result.Status != "ok" {
		return
	}
	s.rememberSubmitClick(key)
}
