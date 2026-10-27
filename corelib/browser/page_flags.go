package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const captchaAskQuestion = "页面出现人机验证（滑块/reCAPTCHA）。请在浏览器中完成验证后继续。继续之后必须先 probe，再 click/type。"

// browserPageFlagsCollectJS assumes pageText/anyIframeSrc/anySelector exist.
const browserPageFlagsCollectJS = `
  const fullText = pageText();
  const excerptLimit = 1200;
  const lower = fullText.toLowerCase();
  // A captcha counts only with visible, interactive challenge evidence: a
  // sized challenge frame, a slider prompt, or a visible captcha box holding
  // something to act on. A class name alone is not one: Bilibili puts
  // "risk-captcha-adapt" on <body> of every creator page.
  function __capVisible(el, minW, minH) {
    try {
      const r = el.getBoundingClientRect();
      if (r.width < minW || r.height < minH) return false;
      const view = (el.ownerDocument && el.ownerDocument.defaultView) || window;
      for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
        const st = view.getComputedStyle(n);
        if (st.display === 'none' || st.visibility === 'hidden' || Number(st.opacity || 1) < 0.05) return false;
      }
      return true;
    } catch (e) { return false; }
  }
  function __capFrame(re) {
    function scan(d) {
      for (const f of queryIframes(d)) {
        try {
          const src = String(f.src || '').toLowerCase();
          if (re.test(src) && !/size=invisible/.test(src) && __capVisible(f, 60, 40)) return true;
          if (f.contentDocument && scan(f.contentDocument)) return true;
        } catch (e) {}
      }
      return false;
    }
    return scan(document);
  }
  function __capBox() {
    function scan(d) {
      let els = [];
      try { els = queryAllDeep(d, '[class*="captcha" i], [id*="captcha" i]'); } catch (e) {}
      for (const el of els) {
        const tag = String(el.tagName || '').toLowerCase();
        if (tag === 'html' || tag === 'body') continue;
        if (!__capVisible(el, 100, 40)) continue;
        let act = null;
        try { act = el.matches('iframe,canvas') ? el : el.querySelector('iframe,canvas,img,input:not([type=hidden]),[role=slider],[draggable=true]'); } catch (e) {}
        if (act && __capVisible(act, 20, 16)) return true;
      }
      for (const f of queryIframes(d)) {
        try { if (f.contentDocument && scan(f.contentDocument)) return true; } catch (e) {}
      }
      return false;
    }
    return scan(document);
  }
  const widgetIframe = __capFrame(/recaptcha|hcaptcha|funcaptcha/);
  const slider = /拖动滑块/.test(fullText);
  const classCaptcha = __capBox();
  const genericCaptchaIframe = __capFrame(/captcha/);
  const captchaWidget = widgetIframe || slider || classCaptcha;
  const qrLogin = /扫码登录|扫描二维码|扫一扫登录|请用.{0,8}扫码|scan (the )?qr code|qr code to (log in|sign in)/i.test(fullText);
  const signedInChrome = /退出登录|已登录/.test(fullText);
  const phoneEntry = anySelector('input[type=tel], input[autocomplete="tel"], input[name*="phone" i], input[name*="mobile" i], input[id*="phone" i], input[id*="mobile" i]');
  const typeableLogin = (anySelector('input[type=password]') && /login|sign in|登录|密码/.test(lower)) || (phoneEntry && /登录|login|sign in|手机/.test(lower));
  let blockingNotice = '';
  try { blockingNotice = ` + blockingNoticeFnJS + `().join(' | '); } catch (e) {}
  const pageFlags = {
    blocking_notice: blockingNotice,
    captcha_widget: captchaWidget,
    captcha: /captcha|verify you are human|unusual traffic|安全验证|人机验证/.test(lower) || genericCaptchaIframe || classCaptcha,
    login_wall: !signedInChrome && typeableLogin,
    login_qr: !signedInChrome && qrLogin && !typeableLogin,
    mfa: /verification code|2fa|one-time|otp|authenticator|验证码/.test(lower),
    canvas: anySelector('canvas'),
    payment_confirm: anySelector('input[autocomplete="cc-number"], input[name*="cardnumber" i], input[id*="cardnumber" i], input[autocomplete="cc-exp"]') || anyIframeSrc('checkout', true) || anyIframeSrc('js.stripe.com', true) || anyIframeSrc('paypal.com', true),
    consent_dialog: anySelector('[role="dialog"], [aria-modal="true"]') && /consent|隐私政策|privacy policy|同意并继续/.test(fullText) && !(anySelector('[class*="cookie"], [id*="cookie"]') && /accept cookies|接受.*cookie|同意.*cookie/.test(lower))
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
  function anyIframeSrc(needle, pay) {
    const n = String(needle || '').toLowerCase();
    function scan(d) {
      for (const f of queryIframes(d)) {
        try {
          const src = String(f.src || '').toLowerCase();
          // Stripe.js loads hidden fraud/metrics frames on every page that
          // includes it (huggingface.co); they are not a checkout.
          if (pay && (/js\.stripe\.com\/v3\/(m-outer|m-inner|controller|metrics)/.test(src) || (f.offsetWidth < 3 && f.offsetHeight < 3))) continue;
          if (src.includes(n)) return true;
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
	case "click", "type", "press", "select", "set_files", "dialog", "script":
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

// captchaWidgetFromSignals mirrors the page script. iframeSrcs are visible,
// sized frames only; visibleChallenge is a visible captcha box with something
// to act on (a class or id substring alone never sets it).
func captchaWidgetFromSignals(iframeSrcs []string, pageText string, visibleChallenge bool) bool {
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
	_ = genericCaptchaIframe // a bare captcha frame is the soft "captcha" flag only
	return widgetIframe || slider || visibleChallenge
}

// paymentConfirmFromSignals is true only for a card field or an external
// checkout frame. A pay button, a price, or the word 支付 is not enough.
func paymentConfirmFromSignals(hasCard, checkoutIframe bool) bool {
	return hasCard || checkoutIframe
}

// consentDialogFromSignals ignores a cookie accept. A separate consent
// dialog still counts.
func consentDialogFromSignals(cookieAccept, consentDialog bool) bool {
	if cookieAccept {
		return false
	}
	return consentDialog
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
	if s != nil && s.peekFlags != nil {
		return s.peekFlags()
	}
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
		Display:   "page has a captcha challenge; solve it in the browser, then probe before clicking",
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
	if s == nil || result == nil || key == "" {
		return
	}
	// "unchanged" still means the click was sent. Remember it so a fast batch
	// stop does not turn into an immediate second submit.
	if result.Status != "ok" && result.Status != "unchanged" {
		return
	}
	s.rememberSubmitClick(key)
}
