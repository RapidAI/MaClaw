package browser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const pageScriptMaxBytes = 8000
const pageScriptTimeout = 20 * time.Second

// pageScriptDenied keeps a desktop page script on the controls it can see.
// A button labeled "Forgot password" is still allowed. Reading cookies,
// storage, or a password field, and calling the network, are not.
var pageScriptDenied = regexp.MustCompile(`(?i)(document\s*\.\s*cookie|\.cookie\b|localstorage|sessionstorage|indexeddb|navigator\s*\.\s*credentials|fetch\s*\(|xmlhttprequest|websocket|sendbeacon|eventsource|\beval\s*\(|new\s+function|import\s*\(|document\s*\.\s*write|inserttext|execcommand|getownpropertydescriptor|\.prototype\b|\bconstructor\b|\b__proto__\b|\bglobalthis\b|document\s*\[|window\s*\[|this\s*\[|type\s*=\s*['\"]password['\"]|\[\s*type\s*=\s*['\"]?password|autocomplete\s*=\s*['\"]cc-|chrome\s*\.\s*cookies|\blocation\s*(?:\.href)?\s*=(?:\s*[^=]|$)|\blocation\s*\.\s*(?:assign|replace)\s*\(|window\s*\.\s*open)`)

// ValidatePageScript rejects an empty script and one that reads secrets or
// leaves the page. The script itself is the sequence of clicks, typing,
// selects, keys, and scrolling.
func ValidatePageScript(script string) error {
	script = strings.TrimSpace(script)
	if script == "" {
		return fmt.Errorf("page script is empty")
	}
	if len(script) > pageScriptMaxBytes {
		return fmt.Errorf("page script is longer than %d bytes", pageScriptMaxBytes)
	}
	if hit := pageScriptDenied.FindString(script); hit != "" {
		if strings.Contains(strings.ToLower(hit), "inserttext") || strings.Contains(strings.ToLower(hit), "execcommand") || strings.Contains(strings.ToLower(hit), "getownpropertydescriptor") || strings.Contains(strings.ToLower(hit), "prototype") {
			return fmt.Errorf("page script refused %q: fill a field with the helper instead, e.g. await type('textarea[placeholder^=\"标题\"]', text), then await click(...) / press('Enter')", strings.TrimSpace(hit))
		}
		return fmt.Errorf("page script cannot read cookies, storage, or passwords, and cannot navigate or call the network (refused %q)", strings.TrimSpace(hit))
	}
	return nil
}

type pageScriptReport struct {
	OK       bool              `json:"ok"`
	Value    string            `json:"value"`
	URL      string            `json:"url"`
	StartURL string            `json:"start_url"`
	Title    string            `json:"title"`
	Clicked  []string          `json:"clicked"`
	Typed    []string          `json:"typed"`
	Selected []string          `json:"selected"`
	Cookie   bool              `json:"cookie"`
	Flags    pageScriptFlagSet `json:"flags"`
	Error    string            `json:"error"`
}

// pageScriptFlagSet is the person-handoff check made at the end of the same
// script, so a finished script does not walk the page a second time.
type pageScriptFlagSet struct {
	LoginWall      bool `json:"login_wall"`
	LoginQR        bool `json:"login_qr"`
	MFA            bool `json:"mfa"`
	CaptchaWidget  bool `json:"captcha_widget"`
	PaymentConfirm bool `json:"payment_confirm"`
	ConsentDialog  bool `json:"consent_dialog"`
}

func (f pageScriptFlagSet) browserFlags() BrowserPageFlags {
	return BrowserPageFlags{
		LoginWall:      f.LoginWall,
		LoginQR:        f.LoginQR,
		MFA:            f.MFA,
		CaptchaWidget:  f.CaptchaWidget,
		PaymentConfirm: f.PaymentConfirm,
		ConsentDialog:  f.ConsentDialog,
	}
}

// RunPageScript runs one page script after the controls are already known.
// It stops before the script when the page is waiting for a person.
func (s *BrowserAgentSession) RunPageScript(script string) (*BrowserActionResult, error) {
	return s.RunPageScriptForSite(script, "")
}

// RunPageScriptForSite runs script and refuses to click when siteHost is set
// and the page is a different website. The check happens inside the script,
// before the first click.
func (s *BrowserAgentSession) RunPageScriptForSite(script, siteHost string) (*BrowserActionResult, error) {
	if err := ValidatePageScript(script); err != nil {
		return nil, err
	}
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	// One guard read. A probe from this turn already has the flags, so a
	// script that follows it does not walk the page before it clicks.
	// A failed read stops the script (it must not click through a page it
	// could not classify), but it is not a captcha: a slow or busy tab
	// (a large editor) fails the read too, and calling that a captcha handed
	// an unpublished Zhihu draft to a person (run_ab72be42).
	flags, guardErr := s.scriptGuardFlags()
	if guardErr != nil {
		return nil, fmt.Errorf("page script not run: the page did not answer the pre-script state check (%v). This is not a captcha; probe or screenshot the page, reload the tab if it stays unresponsive, then retry", guardErr)
	}
	if flags.CaptchaWidget {
		return s.captchaAskResult("script"), nil
	}
	if blocked := pageScriptPersonResult(flags); blocked != nil {
		return blocked, nil
	}
	s.mu.Lock()
	sess := s.session
	s.mu.Unlock()
	if sess == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	raw, err := sess.EvalAwait(wrapPageScript(script, siteHost), pageScriptTimeout)
	// The probe no longer describes this page. The next script must look again.
	s.markProbeFlagsStale()
	if err != nil {
		return nil, err
	}
	report, err := parsePageScriptReport(raw)
	if err != nil {
		return nil, err
	}
	if !report.OK {
		return pageScriptFailureResult(report)
	}
	cleanURL := pageScriptURL(report.URL)
	display := formatPageScriptDisplay(report, cleanURL)
	extra := map[string]interface{}{
		"url":   cleanURL,
		"title": strings.TrimSpace(report.Title),
	}
	if startURL := pageScriptURL(report.StartURL); startURL != "" {
		extra["start_url"] = startURL
	}
	// The script already classified the page it left behind. A consent
	// dialog is not a person handoff, but it is not a reusable batch.
	flags = report.Flags.browserFlags()
	if pageScriptPersonNames(flags) != nil || flags.ConsentDialog {
		extra["page_flags"] = flags
	}
	return s.completeAction("browser_script", display, display, nil, extra, false), nil
}

func (s *BrowserAgentSession) scriptGuardFlags() (BrowserPageFlags, error) {
	if flags, ok := s.recentSnapshotFlags(20 * time.Second); ok {
		return flags, nil
	}
	return s.peekPageFlags()
}

func pageScriptFailureResult(report pageScriptReport) (*BrowserActionResult, error) {
	if blocked := pageScriptPersonResult(report.Flags.browserFlags()); blocked != nil {
		if progress := pageScriptProgress(report); progress != "" {
			blocked.Display += " after " + progress
		}
		return blocked, nil
	}
	message := strings.TrimSpace(report.Error)
	if message == "" {
		message = "page script failed"
	}
	if progress := pageScriptProgress(report); progress != "" {
		message += " after " + progress
	}
	return nil, fmt.Errorf("page script: %s", message)
}

func (s *BrowserAgentSession) recentSnapshotFlags(maxAge time.Duration) (BrowserPageFlags, bool) {
	if s == nil || maxAge <= 0 {
		return BrowserPageFlags{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshots == nil || s.lastSnapshotID == "" {
		return BrowserPageFlags{}, false
	}
	snap := s.snapshots[s.lastSnapshotID]
	if snap == nil || snap.CreatedAt == 0 {
		return BrowserPageFlags{}, false
	}
	if time.Since(time.UnixMilli(snap.CreatedAt)) > maxAge || s.probeFlagsStale {
		return BrowserPageFlags{}, false
	}
	return snap.PageFlags, true
}

func (s *BrowserAgentSession) markProbeFlagsStale() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.probeFlagsStale = true
	s.mu.Unlock()
}

func pageScriptPersonResult(flags BrowserPageFlags) *BrowserActionResult {
	names := pageScriptPersonNames(flags)
	if len(names) == 0 {
		return nil
	}
	return &BrowserActionResult{
		Action:  "browser_script",
		Status:  "ok",
		Display: "page flags: " + strings.Join(names, ","),
		Data:    map[string]interface{}{"page_flags": flags},
	}
}

func pageScriptPersonNames(flags BrowserPageFlags) []string {
	var names []string
	if flags.LoginQR {
		names = append(names, "login_qr")
	}
	if flags.LoginWall {
		names = append(names, "login_wall")
	}
	if flags.MFA {
		names = append(names, "mfa")
	}
	if flags.CaptchaWidget {
		names = append(names, "captcha_widget")
	}
	if flags.PaymentConfirm {
		names = append(names, "payment_confirm")
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func normalizeSiteHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if strings.HasPrefix(host, "www.") && strings.Contains(host[4:], ".") {
		return host[4:]
	}
	return host
}

// NormalizePageScriptHost is the site key stored with a saved page script.
// www is dropped only when a real registrable suffix remains.
func NormalizePageScriptHost(host string) string {
	return normalizeSiteHost(host)
}

// PageScriptCanSave reports whether a finished script is worth keeping.
// A login wall, captcha, or payment page is not a reusable batch.
func PageScriptCanSave(result *BrowserActionResult) bool {
	if result == nil || result.Status != "ok" {
		return false
	}
	if strings.Contains(result.Display, "page flags:") {
		return false
	}
	if result.Data == nil {
		return true
	}
	raw, exists := result.Data["page_flags"]
	if !exists || raw == nil {
		return true
	}
	flags, ok := raw.(BrowserPageFlags)
	if !ok {
		return false
	}
	return !flags.LoginQR && !flags.LoginWall && !flags.MFA && !flags.CaptchaWidget && !flags.PaymentConfirm && !flags.ConsentDialog
}

func pageScriptURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func parsePageScriptReport(raw string) (pageScriptReport, error) {
	var report pageScriptReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &report); err != nil {
		return report, fmt.Errorf("page script returned %s", trimPageScriptText(raw, 180))
	}
	report.Value = trimPageScriptText(report.Value, 240)
	report.Error = trimPageScriptText(report.Error, 240)
	report.Clicked = trimPageScriptList(report.Clicked)
	report.Typed = trimPageScriptList(report.Typed)
	report.Selected = trimPageScriptList(report.Selected)
	return report, nil
}

func pageScriptProgress(report pageScriptReport) string {
	var parts []string
	if len(report.Clicked) > 0 {
		parts = append(parts, "clicked "+strings.Join(report.Clicked, ", "))
	}
	if len(report.Typed) > 0 {
		parts = append(parts, "typed "+strings.Join(report.Typed, ", "))
	}
	if len(report.Selected) > 0 {
		parts = append(parts, "selected "+strings.Join(report.Selected, ", "))
	}
	return strings.Join(parts, "; ")
}

func formatPageScriptDisplay(report pageScriptReport, cleanURL string) string {
	var parts []string
	if progress := pageScriptProgress(report); progress != "" {
		parts = append(parts, progress)
	}
	if cleanURL != "" {
		parts = append(parts, "url "+cleanURL)
	}
	if title := strings.TrimSpace(report.Title); title != "" {
		parts = append(parts, "title "+trimPageScriptText(title, 80))
	}
	if len(parts) == 0 {
		parts = append(parts, "done")
	}
	display := "script ok: " + strings.Join(parts, "; ")
	if report.Value != "" {
		display += "\n" + report.Value
	}
	return display
}

func trimPageScriptList(items []string) []string {
	if len(items) > 12 {
		items = items[:12]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = trimPageScriptText(item, 80)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func trimPageScriptText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func wrapPageScript(script, siteHost string) string {
	encoded, _ := json.Marshal(script)
	encodedHost, _ := json.Marshal(normalizeSiteHost(siteHost))
	return `(async () => {
  const source = ` + string(encoded) + `;
  const expectedHost = ` + string(encodedHost) + `;
  const startedURL = location.href;
  function siteHost(raw) {
    const host = String(raw || '').toLowerCase();
    if (host.startsWith('www.') && host.slice(4).indexOf('.') >= 0) return host.slice(4);
    return host;
  }
  if (expectedHost) {
    const host = siteHost(location.hostname);
    const want = siteHost(expectedHost);
    if (!host || host !== want) {
      return JSON.stringify({ok: false, error: 'browser skill belongs to ' + want + ' and this page is ' + (host || 'unknown'), url: location.href, start_url: startedURL, title: document.title || '', clicked: [], typed: [], selected: [], flags: {}});
    }
  }
  const did = {clicked: [], typed: [], selected: []};
  function note(list, value) {
    const text = norm(value).slice(0, 80);
    if (!text || list.length >= 12) return;
    list.push(text);
  }
  function norm(s) {
    return String(s || '').replace(/\s+/g, ' ').trim();
  }
  function visible(el) {
    if (!el || !el.getBoundingClientRect) return false;
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return false;
    const view = el.ownerDocument && el.ownerDocument.defaultView;
    const st = view && view.getComputedStyle ? view.getComputedStyle(el) : null;
    if (st && (st.display === 'none' || st.visibility === 'hidden')) return false;
    if (el.getAttribute && el.getAttribute('aria-hidden') === 'true') return false;
    return true;
  }
  function enabled(el) {
    if (!el || el.disabled) return false;
    if (el.getAttribute && el.getAttribute('aria-disabled') === 'true') return false;
    return true;
  }
  function secret(el) {
    if (!el) return false;
    const type = String((el.getAttribute && (el.getAttribute('type') || el.type)) || '').toLowerCase();
    if (type === 'password' || type === 'hidden') return true;
    const ac = String(el.getAttribute && el.getAttribute('autocomplete') || '').toLowerCase();
    if (ac.indexOf('password') >= 0 || ac.indexOf('cc-') === 0) return true;
    const name = String((el.getAttribute && (el.getAttribute('name') || el.getAttribute('id'))) || '').toLowerCase();
    return name.indexOf('password') >= 0 || name.indexOf('passwd') >= 0 || name.indexOf('cardnumber') >= 0 || name.indexOf('card-number') >= 0;
  }
  function pageText() {
    const parts = [];
    function read(body) {
      if (!body || !body.cloneNode) return;
      const root = body.cloneNode(true);
      try { root.querySelectorAll('script,style,noscript').forEach(el => el.remove()); } catch (e) {}
      parts.push(String(root.textContent || root.innerText || ''));
    }
    try { read(document.body); } catch (e) {}
    for (const frame of queryAll('iframe,frame')) {
      try { read(frame.contentDocument && frame.contentDocument.body); } catch (e) {}
    }
    return norm(parts.join(' ')).slice(0, 4000).toLowerCase();
  }
  function acceptCookies() {
    const buttons = queryAll('button,a,[role="button"]').filter(el => visible(el) && enabled(el));
    for (const el of buttons) {
      const label = labelOf(el).toLowerCase();
      const idc = String((el.id || '') + ' ' + (typeof el.className === 'string' ? el.className : '')).toLowerCase();
      const tag = String(el.tagName || '').toLowerCase();
      if (tag === 'a') {
        const href = String((el.getAttribute && el.getAttribute('href')) || '');
        if (href && href.charAt(0) !== '#' && href.indexOf('javascript:') !== 0) continue;
      }
      const cookieish = idc.indexOf('cookie') >= 0 || label.indexOf('cookie') >= 0;
      if (/accept all cookies|accept cookies|接受.*cookie|同意.*cookie|允许.*cookie/.test(label) || (cookieish && /^(accept|agree|allow|ok|同意|接受|允许|知道了)$/.test(label))) {
        try { el.click(); } catch (e) {}
        return true;
      }
    }
    return false;
  }
  function focusEntry() {
    let best = null;
    let score = 0;
    for (const el of queryAll('input,textarea')) {
      if (!visible(el) || !enabled(el)) continue;
      const type = String((el.getAttribute && (el.getAttribute('type') || el.type)) || '').toLowerCase();
      const blob = String((el.getAttribute && ((el.getAttribute('name') || '') + ' ' + (el.getAttribute('id') || '') + ' ' + (el.getAttribute('placeholder') || '') + ' ' + (el.getAttribute('autocomplete') || ''))) || '').toLowerCase();
      let next = 0;
      if (type === 'password') next = 3;
      else if (type === 'tel' || /phone|mobile|手机|电话/.test(blob)) next = 2;
      else if (/one-time-code|otp|验证码/.test(blob)) next = 1;
      if (next > score) { score = next; best = el; }
    }
    if (best) { try { best.focus(); } catch (e) {} }
  }
  function labelOf(el) {
    if (!el) return '';
    return norm(el.innerText || el.textContent || (el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('placeholder') || el.getAttribute('name'))) || '');
  }
  function queryAll(selector) {
    const out = [];
    const seen = typeof WeakSet === 'function' ? new WeakSet() : null;
    function walk(node, frames) {
      if (!node || !node.querySelectorAll) return;
      try { out.push.apply(out, Array.from(node.querySelectorAll(selector))); } catch (e) {}
      let all = [];
      try { all = node.querySelectorAll('*'); } catch (e) { return; }
      for (const el of all) {
        const tag = String(el.tagName || '').toLowerCase();
        if (tag === 'iframe' || tag === 'frame') frames.push(el);
        if (el.shadowRoot) walk(el.shadowRoot, frames);
      }
    }
    function scan(doc) {
      if (!doc || (seen && seen.has(doc))) return;
      if (seen) seen.add(doc);
      const frames = [];
      walk(doc, frames);
      for (const frame of frames) {
        try { if (frame.contentDocument) scan(frame.contentDocument); } catch (e) {}
      }
    }
    scan(document);
    return out;
  }
  function looksLikeSelector(raw) {
    if (/^[#.[]/.test(raw)) return true;
    return /^(button|a|input|select|textarea|label|div|span|li|option|form|section|ul|ol|p|h[1-6])([\s.#\[]|[\s]*[>~+])/i.test(raw);
  }
  function collect(target) {
    const raw = norm(target);
    if (!raw) return [];
    if (looksLikeSelector(raw)) {
      try {
        const found = queryAll(raw).filter(el => visible(el) && enabled(el));
        if (found.length) return found;
      } catch (e) {}
    }
    const controls = queryAll('button,a,input,select,textarea,summary,[role="button"],[role="link"],[role="menuitem"],[role="tab"],[role="option"],[onclick],[contenteditable="true"]').filter(el => visible(el) && enabled(el));
    const want = raw.toLowerCase();
    const exact = controls.filter(el => labelOf(el).toLowerCase() === want);
    if (exact.length) return exact;
    return controls.filter(el => labelOf(el).toLowerCase().indexOf(want) >= 0);
  }
  function preferInnermost(found) {
    if (found.length < 2) return found;
    return found.filter(el => !found.some(other => other !== el && el.contains && el.contains(other)));
  }
  function one(target) {
    const found = preferInnermost(collect(target));
    if (found.length === 1) return found[0];
    if (found.length === 0) return null;
    const names = found.slice(0, 4).map(labelOf).filter(Boolean).join(', ');
    throw new Error('several controls match ' + target + ' (' + found.length + (names ? ': ' + names : '') + ')');
  }
  async function waitEl(target, ms) {
    const limit = Math.min(Math.max(Number(ms) || 2500, 200), 8000);
    const start = Date.now();
    while (Date.now() - start < limit) {
      let el = null;
      try { el = one(target); }
      catch (e) { throw new Error(String(e && e.message || e)); }
      if (el) return el;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error('not found: ' + target);
  }
  function setValue(el, text) {
    const tag = String(el.tagName || '').toLowerCase();
    if (el.isContentEditable) {
      el.focus();
      el.textContent = text;
      el.dispatchEvent(new Event('input', {bubbles: true}));
      return;
    }
    el.focus();
    const proto = tag === 'textarea' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const desc = Object.getOwnPropertyDescriptor(proto, 'value');
    if (desc && desc.set) desc.set.call(el, text);
    else el.value = text;
    el.dispatchEvent(new Event('input', {bubbles: true}));
    el.dispatchEvent(new Event('change', {bubbles: true}));
  }
  async function click(target) {
    const el = await waitEl(target, 2500);
    try { el.scrollIntoView({block: 'center', inline: 'center'}); } catch (e) {}
    el.click();
    note(did.clicked, labelOf(el) || target);
    await new Promise(resolve => {
      let done = false;
      const finish = () => { if (!done) { done = true; resolve(); } };
      const view = (el.ownerDocument && el.ownerDocument.defaultView) || window;
      if (view.requestAnimationFrame) view.requestAnimationFrame(finish);
      setTimeout(finish, 32);
    });
    return labelOf(el) || norm(target);
  }
  async function type(target, text) {
    const value = String(text == null ? '' : text);
    if (value.length > 4000) throw new Error('text is too long');
    const el = await waitEl(target, 2500);
    if (secret(el)) throw new Error('refusing a password or card field');
    setValue(el, value);
    note(did.typed, target);
    return norm(target);
  }
  async function select(target, label) {
    const el = await waitEl(target, 2500);
    const box = String(el.tagName || '').toLowerCase() === 'select' ? el : (el.querySelector ? el.querySelector('select') : null);
    if (!box) throw new Error('not a select: ' + target);
    const want = norm(label).toLowerCase();
    let match = null;
    for (const opt of Array.from(box.options || [])) {
      const text = norm(opt.textContent || opt.label || opt.value).toLowerCase();
      if (text === want || String(opt.value || '').toLowerCase() === want) { match = opt; break; }
    }
    if (!match) {
      const partial = Array.from(box.options || []).filter(opt => norm(opt.textContent || opt.label || opt.value).toLowerCase().indexOf(want) >= 0);
      if (partial.length === 1) match = partial[0];
    }
    if (!match) throw new Error('option not found: ' + label);
    box.value = match.value;
    box.dispatchEvent(new Event('input', {bubbles: true}));
    box.dispatchEvent(new Event('change', {bubbles: true}));
    note(did.selected, norm(label));
    return norm(label);
  }
  async function press(key) {
    const name = norm(key);
    if (!name || name.length > 20) throw new Error('press needs one key');
    const el = document.activeElement || document.body;
    if (secret(el)) throw new Error('refusing a password or card field');
    if (/^enter$/i.test(name) && el && el.form && el.form.querySelector && el.form.querySelector('input[type="password"],input[autocomplete^="cc-"]')) {
      throw new Error('refusing a password or card field');
    }
    if (/^enter$/i.test(name) && el && el.form && typeof el.form.requestSubmit === 'function') {
      el.form.requestSubmit();
      return name;
    }
    const view = (el.ownerDocument && el.ownerDocument.defaultView) || window;
    const init = {key: name, bubbles: true, cancelable: true};
    el.dispatchEvent(new view.KeyboardEvent('keydown', init));
    el.dispatchEvent(new view.KeyboardEvent('keyup', init));
    return name;
  }
  async function scroll(dy) {
    const amount = Number(dy);
    if (!Number.isFinite(amount)) {
      const el = await waitEl(dy, 2000);
      try { el.scrollIntoView({block: 'center'}); } catch (e) {}
      return norm(dy);
    }
    window.scrollBy(0, Math.max(-4000, Math.min(4000, amount)));
    return String(amount);
  }
  async function waitFor(target, ms) {
    await waitEl(target, ms);
    return norm(target);
  }
  function pageSignals() {
    const text = pageText();
    const nodes = queryAll('input,iframe,frame,[role="dialog"],[aria-modal="true"],[class*="captcha"],[id*="captcha"],[class*="cookie"],[id*="cookie"]');
    let password = false, card = false, captchaEl = false, dialog = false;
    const srcs = [];
    for (const el of nodes) {
      const tag = String(el.tagName || '').toLowerCase();
      if (tag === 'iframe' || tag === 'frame') { srcs.push(String(el.src || '').toLowerCase()); continue; }
      if (tag === 'input') {
        const type = String((el.getAttribute && (el.getAttribute('type') || el.type)) || '').toLowerCase();
        const ac = String((el.getAttribute && el.getAttribute('autocomplete')) || '').toLowerCase();
        const name = String((el.getAttribute && (el.getAttribute('name') || el.getAttribute('id'))) || '').toLowerCase();
        if (type === 'password' && visible(el)) password = true;
        if (ac === 'cc-number' || ac === 'cc-exp' || name.indexOf('cardnumber') >= 0) card = true;
        continue;
      }
      const role = String((el.getAttribute && el.getAttribute('role')) || '').toLowerCase();
      if ((role === 'dialog' || (el.getAttribute && el.getAttribute('aria-modal') === 'true')) && visible(el)) dialog = true;
      const idc = String((el.id || '') + ' ' + (typeof el.className === 'string' ? el.className : '')).toLowerCase();
      if (idc.indexOf('captcha') >= 0) captchaEl = true;
    }
    const frameSrc = srcs.join(' ');
    const payFrameSrc = srcs.filter(u => !/js\.stripe\.com\/v3\/(m-outer|m-inner|controller|metrics)/.test(String(u).toLowerCase())).join(' ');
    const signedIn = text.indexOf('退出登录') >= 0 || text.indexOf('已登录') >= 0;
    const qr = /扫码登录|扫描二维码|扫一扫登录|qr code/.test(text);
    const phone = nodes.some(el => {
      const type = String((el.getAttribute && (el.getAttribute('type') || el.type)) || '').toLowerCase();
      const blob = String((el.getAttribute && ((el.getAttribute('name') || '') + ' ' + (el.getAttribute('id') || '') + ' ' + (el.getAttribute('autocomplete') || ''))) || '').toLowerCase();
      return visible(el) && (type === 'tel' || /phone|mobile|手机|电话/.test(blob));
    });
    const typeable = (password && /login|sign in|登录|密码/.test(text)) || (phone && /登录|login|sign in|手机/.test(text));
    const cookieAccept = nodes.some(el => {
      const idc = String((el.id || '') + ' ' + (typeof el.className === 'string' ? el.className : '')).toLowerCase();
      return idc.indexOf('cookie') >= 0;
    }) && /accept cookies|接受.*cookie|同意.*cookie/.test(text);
    return {
      login_wall: !signedIn && typeable,
      login_qr: !signedIn && qr && !typeable,
      mfa: /verification code|2fa|one-time|otp|authenticator|验证码/.test(text),
      captcha_widget: text.indexOf('拖动滑块') >= 0 || /recaptcha|hcaptcha|funcaptcha/.test(frameSrc) || (captchaEl && frameSrc.indexOf('captcha') >= 0),
      payment_confirm: card || /js\.stripe\.com|paypal\.com/.test(payFrameSrc) || payFrameSrc.indexOf('checkout') >= 0,
      consent_dialog: dialog && /consent|隐私政策|privacy policy|同意并继续/.test(text) && !cookieAccept
    };
  }
  const sealed = [];
  function sealSecrets() {
    let nodes = [];
    try { nodes = queryAll('input,textarea'); } catch (e) { return; }
    for (const el of nodes) {
      if (!secret(el) || String((el.getAttribute && (el.getAttribute('type') || el.type)) || '').toLowerCase() === 'hidden') continue;
      const proto = String(el.tagName || '').toLowerCase() === 'textarea' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const desc = Object.getOwnPropertyDescriptor(proto, 'value');
      if (!desc || !desc.get || !desc.set) continue;
      try {
        Object.defineProperty(el, 'value', {
          configurable: true,
          get() { return ''; },
          set() { throw new Error('refusing a password or card field'); }
        });
        sealed.push(el);
      } catch (e) {}
    }
  }
  function unseal() {
    for (const el of sealed) {
      try { delete el.value; } catch (e) {}
    }
    sealed.length = 0;
  }
  try {
    const cookiesAccepted = acceptCookies();
    sealSecrets();
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
    const fn = new AsyncFunction('click', 'type', 'select', 'press', 'scroll', 'waitFor', '"use strict";\n' + source);
    const value = await Promise.race([
      fn.call(undefined, click, type, select, press, scroll, waitFor),
      new Promise((_, reject) => setTimeout(() => reject(new Error('script timed out')), 12000))
    ]);
    const flags = pageSignals();
    if (flags.login_wall || flags.mfa) focusEntry();
    return JSON.stringify({ok: true, value: value == null ? '' : String(value).slice(0, 240), url: location.href, start_url: startedURL, title: document.title || '', clicked: did.clicked, typed: did.typed, selected: did.selected, cookie: cookiesAccepted, flags: flags});
  } catch (e) {
    const flags = pageSignals();
    if (flags.login_wall || flags.mfa) focusEntry();
    return JSON.stringify({ok: false, error: String(e && e.message || e).slice(0, 240), url: location.href, start_url: startedURL, title: document.title || '', clicked: did.clicked, typed: did.typed, selected: did.selected, cookie: false, flags: flags});
  } finally {
    unseal();
  }
})()`
}
