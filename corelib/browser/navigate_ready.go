package browser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Navigate readiness and login/risk-wall detection.
//
// After a navigate the page must be loaded (document.readyState=complete) and
// show at least one interactive element before the step returns; a page that
// stays blank is reloaded once. A sign-in form is marked login_wall so the
// agent calls submit_secret. A captcha / "安全验证" check, or a page that is
// still blank after the wait, carries LoginWallMarker and hands the preview
// to the person.
//
// MACLAW_BROWSER_NAV_READY_TIMEOUT (seconds, default 18, 0 disables) bounds
// each wait; the blank-page threshold for a wall is MACLAW_BROWSER_BLANK_WALL
// (seconds, default 20, counted over both waits).

// LoginWallMarker is the stable token in a navigate result that names a
// login or risk-control wall. MaClawSrv maps it to failure class login_wall.
const LoginWallMarker = "[login_wall]"

const (
	navReadyTimeoutEnv = "MACLAW_BROWSER_NAV_READY_TIMEOUT"
	navBlankWallEnv    = "MACLAW_BROWSER_BLANK_WALL"
)

// navReadyPoll is the poll interval; tests shorten it.
var navReadyPoll = 400 * time.Millisecond

func navEnvSeconds(name string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return def
	}
	return time.Duration(f * float64(time.Second))
}

// PageReadyState is one readiness probe of the current page.
type PageReadyState struct {
	ReadyState  string `json:"rs"`
	Interactive int    `json:"n"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	TextLen     int    `json:"tl"`
	Wall        string `json:"wall"`
	// ContentType of the document: an image, video, PDF or plain-text
	// resource opened directly has no interactive element and no sign-in.
	ContentType string `json:"ct"`
}

// rawResource: the page is a raw file (picture, video, PDF, text), not a
// site page; it is never a login wall (papers 11-11: a raw.githubusercontent
// .com png was classed login_wall "blank").
func (st PageReadyState) rawResource() bool {
	ct := strings.ToLower(strings.TrimSpace(st.ContentType))
	if ct != "" && ct != "text/html" && !strings.Contains(ct, "xhtml") {
		return true
	}
	return rawResourceURLRe.MatchString(st.URL)
}

var rawResourceURLRe = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|svg|bmp|ico|avif|mp4|webm|pdf|txt|csv|json)(\?[^#]*)?(#.*)?$`)

// frequencyNoticeFnJS returns the page's own short refusal lines. A product
// description that mentions the words is not a refusal: the line has to be
// the notice itself. An inline fragment of a longer line, a hidden node, or
// an editor paragraph is not that notice. A container of many children is
// not one either. Readiness does not call this; it only scans the text
// already on the page.
const frequencyNoticeFnJS = `(() => {
  const re = /访问过于频繁|操作过于频繁|请求过于频繁|访问频繁|too many requests/i;
  const rate = /\brate limit\b/i;
  const cue = /exceed|try again|later|too many|slow down/i;
  const line = (t) => {
    t = String(t || '').replace(/\s+/g, ' ').trim();
    if (t.length < 4 || t.length > 80) return '';
    if (re.test(t)) return t;
    if (rate.test(t) && cue.test(t)) return t;
    return '';
  };
  const docs = [];
  const walk = (d, depth) => {
    if (!d || depth > 3) return;
    docs.push(d);
    let frames = [];
    try { frames = d.querySelectorAll('iframe,frame'); } catch (e) { frames = []; }
    for (const f of frames) {
      let cd = null;
      try { cd = f.contentDocument; } catch (e) {}
      if (cd) walk(cd, depth + 1);
    }
  };
  try { walk(document, 0); } catch (e) {}
  const vis = (e) => {
    const r = e.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    let vh = 0;
    try {
      const root = e.ownerDocument && e.ownerDocument.documentElement;
      if (root && root.clientHeight > 0) vh = root.clientHeight;
    } catch (err) {}
    if (!(vh > 0)) vh = innerHeight || 0;
    if (vh > 0 && (r.top >= vh || r.bottom <= 0)) return false;
    for (let x = e; x && x.nodeType === 1; x = x.parentElement) {
      let s = null;
      try { s = getComputedStyle(x); } catch (err) { return false; }
      if (!s || s.display === 'none' || s.visibility === 'hidden' || Number(s.opacity) === 0) return false;
    }
    return true;
  };
  const editor = (e) => {
    try { return !!e.closest('[contenteditable=true],[contenteditable=""],textarea'); } catch (err) { return false; }
  };
  const out = [];
  for (const d of docs) {
    if (!d.body) continue;
    let nodes = [];
    try { nodes = d.body.querySelectorAll('p,span,div,li,h1,h2,h3,h4,label,strong,[role=alert],[role=status]'); } catch (e) { continue; }
    const inline = (e) => {
      try { return e.matches('span,strong,em,a,label'); } catch (err) { return false; }
    };
    for (const e of nodes) {
      if (out.length >= 3) break;
      if (e.children.length > 3 || editor(e) || !vis(e)) continue;
      const t = line(e.innerText);
      if (!t || out.some(o => o.includes(t) || t.includes(o))) continue;
      if (inline(e) && e.parentElement) {
        const pt = String(e.parentElement.innerText || '').replace(/\s+/g, ' ').trim();
        if (pt !== t) continue;
      }
      out.push(t);
    }
  }
  return out;
})`

// pageReadyExpr counts visible interactive elements and looks for wall
// signals. It returns a JSON string so the Go side has one parse path.
// A frequency refusal here is a short line in the opening of the text
// already on the page. A line further down is not this document's refusal;
// observe still records a notice that is on screen. The element walk stays
// on observe; this probe runs every poll.
const pageReadyExpr = `(() => {
  const sel = 'a[href],button,input:not([type=hidden]),textarea,select,[contenteditable=""],[contenteditable=true],[role=button],[role=textbox],[role=link],[tabindex]:not([tabindex="-1"])';
  let n = 0;
  try {
    for (const el of document.querySelectorAll(sel)) {
      const r = el.getBoundingClientRect();
      if (r.width > 1 && r.height > 1) { n++; if (n >= 50) break; }
    }
  } catch (e) {}
  const body = document.body ? (document.body.innerText || '') : '';
  const head = body.slice(0, 4000);
  let wall = '';
  const freqOpen = 400;
  const freqRe = /访问过于频繁|操作过于频繁|请求过于频繁|访问频繁|too many requests/i;
  const freqRate = /\brate limit\b/i;
  const freqCue = /exceed|try again|later|too many|slow down/i;
  let at = 0;
  for (const raw of head.split('\n')) {
    if (at >= freqOpen) break;
    const t = String(raw || '').replace(/\s+/g, ' ').trim();
    if (t.length >= 4 && t.length <= 80 && (freqRe.test(t) || (freqRate.test(t) && freqCue.test(t)))) { wall = 'rate_limit'; break; }
    at += raw.length + 1;
  }
  if (!wall && /安全验证|请完成验证|人机验证|滑动验证|拖动滑块|captcha|are you a robot|verify you are human/i.test(head)) wall = 'captcha';
  else if (!wall && document.querySelector('input[type=password]') && /登录|注册|sign ?in|log ?in/i.test(head + ' ' + document.title)) wall = 'login_form';
  return JSON.stringify({rs: document.readyState, n, url: location.href, title: document.title || '', tl: body.length, wall, ct: document.contentType || ''});
})()`

// pageReady reads the readiness state of the current page.
func (s *Session) pageReady() (PageReadyState, error) {
	var st PageReadyState
	raw, err := s.evalIsolated(pageReadyExpr, false, 3*time.Second)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return st, fmt.Errorf("parse ready state: %w", err)
	}
	return st, nil
}

// loginWallURL reports a sign-in / passport / risk URL.
func loginWallURL(raw string) string {
	u := strings.ToLower(raw)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	for _, k := range []string{"/signin", "/signup", "/login", "/passport", "passport.", "/account/unhuman", "/unhuman", "captcha", "/verify?", "/security"} {
		if strings.Contains(u, k) {
			return k
		}
	}
	return ""
}

// waitPageReadyOnce polls until complete+interactive, a wall, or timeout.
func (s *Session) waitPageReadyOnce(timeout time.Duration) (PageReadyState, bool) {
	deadline := time.Now().Add(timeout)
	var last PageReadyState
	for {
		st, err := s.pageReady()
		if err == nil {
			last = st
			if st.Wall != "" || loginWallURL(st.URL) != "" {
				return st, true
			}
			if st.ReadyState == "complete" && (st.Interactive > 0 || st.rawResource()) {
				return st, true
			}
		}
		if !time.Now().Before(deadline) {
			return last, false
		}
		time.Sleep(navReadyPoll)
	}
}

// NavReadyReport is the outcome of waitPageReady.
type NavReadyReport struct {
	State    PageReadyState
	Ready    bool
	Reloaded bool
	Wall     string // "", login_url, login_form, captcha, blank, rate_limit
	Waited   time.Duration
}

// Note is the text appended to a navigate result.
func (r NavReadyReport) Note() string {
	var b strings.Builder
	if r.State.URL != "" {
		fmt.Fprintf(&b, "\nfinal URL: %s", r.State.URL)
	}
	fmt.Fprintf(&b, " (readyState=%s, interactive elements=%d, waited %.1fs", nz(r.State.ReadyState, "unknown"), r.State.Interactive, r.Waited.Seconds())
	if r.Reloaded {
		b.WriteString(", reloaded once")
	}
	b.WriteString(")")
	if r.Wall == "login_form" || r.Wall == "login_url" {
		fmt.Fprintf(&b, "\n%s %s: this is a sign-in page. Type the account name yourself. Call browser(action=\"submit_secret\", method, ref) for the password, email/SMS code, or authenticator code. method is password, otp, or authenticator. Do not pass the secret and do not ask for it in chat. A captcha, QR code, or passkey uses browser(action=\"request_takeover\").", LoginWallMarker, r.Wall)
	} else if r.Wall == "rate_limit" {
		b.WriteString("\n" + frequencyRefusalStopText(false))
	} else if r.Wall != "" {
		fmt.Fprintf(&b, "\n%s %s: this page needs the person (captcha/安全验证, or it stays blank). The person should take over the MaClaw preview. Do not ask for a password or code in chat. After they finish, probe before the next click or type.", LoginWallMarker, r.Wall)
	} else if !r.Ready {
		b.WriteString("\npage not fully ready yet; probe once more before acting.")
	}
	return b.String()
}

func nz(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// waitPageReady waits for the navigated page, reloads a blank page once, and
// detects login/risk walls. Disabled when the timeout env is 0.
func (s *Session) waitPageReady() (NavReadyReport, bool) {
	timeout := navEnvSeconds(navReadyTimeoutEnv, 18*time.Second)
	if s == nil || timeout <= 0 {
		return NavReadyReport{}, false
	}
	blankWall := navEnvSeconds(navBlankWallEnv, 20*time.Second)
	start := time.Now()
	st, ok := s.waitPageReadyOnce(timeout)
	rep := NavReadyReport{State: st, Ready: ok}
	if !ok || (st.Interactive == 0 && st.Wall == "" && loginWallURL(st.URL) == "" && !st.rawResource()) {
		s.mu.Lock()
		client := s.client
		s.mu.Unlock()
		if client != nil {
			if _, err := client.Send("Page.reload", map[string]interface{}{"ignoreCache": false}, NavTimeout); err == nil {
				rep.Reloaded = true
				st, ok = s.waitPageReadyOnce(timeout)
				rep.State, rep.Ready = st, ok
			}
		}
	}
	rep.Waited = time.Since(start)
	switch {
	case rep.State.Wall != "":
		rep.Wall = rep.State.Wall
	case loginWallURL(rep.State.URL) != "":
		rep.Wall = "login_url"
	case rep.State.Interactive == 0 && rep.State.TextLen < 20 && rep.Waited >= blankWall && !localPageURL(rep.State.URL) && !rep.State.rawResource():
		// A blank local page (an image preview, a dev server) has no
		// sign-in; only remote sites get the person called in (XHS-7).
		rep.Wall = "blank"
	}
	return rep, true
}

var localPageRe = regexp.MustCompile(`(?i)^(file:|data:|blob:|about:|https?://(localhost|127\.|0\.0\.0\.0|\[::1\])[:/]?)`)

func localPageURL(u string) bool { return localPageRe.MatchString(strings.TrimSpace(u)) }

// wallHandsPageOver is the navigate takeover. A normal sign-in form stays
// with the agent, which calls submit_secret for the password or code.
// Captcha text and a blank remote page still hand the preview to the person.
func wallHandsPageOver(wall string) (string, bool) {
	switch wall {
	case "captcha":
		return "captcha", true
	case "blank":
		return "attention", true
	default:
		// rate_limit has nothing for the person to solve. Handing the preview
		// over would wait out the hold while the refusal stays on the page.
		return "", false
	}
}

// frequencyRefusalTTL is how long a refused site stays refused. Another
// navigation to that site inside this window is not sent. The stamp is the
// observe that read the refusal, so a tight retry loop does not keep extending it.
const frequencyRefusalTTL = 15 * time.Minute

// frequencyRefusalStopText is the model-facing refusal. Reloading the same
// document is what keeps the site refusing the next request.
func frequencyRefusalStopText(notSent bool) string {
	if notSent {
		return "站点因访问频繁拒绝了这个站点，这次没有重新打开。不要刷新同一地址，也不要改开这个站点的其他页面，也不要隔一段时间再试。把这一步停下来告诉用户。"
	}
	return "站点因访问频繁拒绝了这次请求。页眉和页脚不是页面内容。不要刷新这个地址，也不要改开这个站点的其他页面，也不要隔一段时间再试。把这一步停下来告诉用户。"
}

// FrequencyRefusalStops reports a tool result that already tells the model
// to end this step. A later "probe and continue" would undo that.
func FrequencyRefusalStops(text string) bool {
	return strings.Contains(text, "把这一步停下来告诉用户")
}

func frequencyRefusalText(s string) bool {
	for _, part := range noticeParts(s) {
		if frequencyRefusalLine(part) {
			return true
		}
	}
	return false
}

// noticeParts splits the notice fields. FrequencyNotice joins lines with '|',
// and BlockingNotice is its own line. Sentence punctuation stays inside the
// line: the page excerpt is collapsed to spaces, and splitting it into
// sentences would treat a mention inside a product page as the notice.
func noticeParts(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '|'
	})
}

// frequencyRefusalLine is one short notice. The same words inside a paragraph
// are the page talking about the limit, not the site refusing this document.
func frequencyRefusalLine(s string) bool {
	s = strings.Join(strings.Fields(s), " ")
	n := len([]rune(s))
	if n < 4 || n > 80 {
		return false
	}
	lower := strings.ToLower(s)
	for _, phrase := range []string{"访问过于频繁", "操作过于频繁", "请求过于频繁", "访问频繁", "too many requests"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	if !strings.Contains(lower, "rate limit") {
		return false
	}
	for _, cue := range []string{"exceed", "try again", "later", "too many", "slow down"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// emptyShellAfterRefusal is a list document that stayed on its load-failure
// placeholder. The prompt and a standalone zero badge are one cluster in the
// opening, not a header count plus a failure later on the page, and not a
// sentence further down. A zero buried in a price, a stock count, or a
// footer is not a shell.
func emptyShellAfterRefusal(text string) bool {
	head := strings.ToLower(firstRunes(text, 400))
	for _, phrase := range []string{"刷新看看", "刷新重试", "加载失败", "failed to load", "try refreshing"} {
		from := 0
		for from < len(head) {
			idx := strings.Index(head[from:], phrase)
			if idx < 0 {
				break
			}
			idx += from
			if runeIndex(head, idx) < 160 && zeroBadgeNear(head, idx, len(phrase), 40) {
				return true
			}
			next := idx + len(phrase)
			if next <= from {
				break
			}
			from = next
		}
	}
	return false
}

// zeroBadgeNear reports a standalone (0) or （0） within window runes of the phrase.
func zeroBadgeNear(s string, phraseAt, phraseBytes, window int) bool {
	if phraseAt < 0 || phraseBytes < 0 || phraseAt+phraseBytes > len(s) {
		return false
	}
	phraseStart := runeIndex(s, phraseAt)
	phraseEnd := phraseStart + utf8.RuneCountInString(s[phraseAt:phraseAt+phraseBytes])
	for _, token := range []string{"(0)", "（0）"} {
		from := 0
		for from < len(s) {
			i := strings.Index(s[from:], token)
			if i < 0 {
				break
			}
			i += from
			end := i + len(token)
			if badgeEdge(s, i, end) {
				b0 := runeIndex(s, i)
				b1 := b0 + utf8.RuneCountInString(token)
				gap := phraseStart - b1
				if b0 >= phraseEnd {
					gap = b0 - phraseEnd
				} else if b1 > phraseStart {
					gap = 0
				}
				if gap <= window {
					return true
				}
			}
			if end <= from {
				break
			}
			from = end
		}
	}
	return false
}

func badgeEdge(s string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:start])
		if r == utf8.RuneError || unicode.IsDigit(r) || r == '.' || r == '．' {
			return false
		}
	}
	if end < len(s) {
		r, _ := utf8.DecodeRuneInString(s[end:])
		if r == utf8.RuneError || unicode.IsDigit(r) || r == '.' || r == '．' {
			return false
		}
	}
	return true
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func runeIndex(s string, byteIdx int) int {
	if byteIdx <= 0 {
		return 0
	}
	if byteIdx > len(s) {
		byteIdx = len(s)
	}
	return utf8.RuneCountInString(s[:byteIdx])
}

func documentHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// documentSite is the site root shared by a site's hosts. search.jd.com and
// cart.jd.com are one site. A multi-part public suffix stays with its name,
// so shop.example.co.uk and other.co.uk are not the same site.
func documentSite(raw string) string {
	return loggedInSiteRoot(documentHost(raw))
}

// noteFrequencyRefusalLocked records a frequency refusal or the empty list
// shell that follows one. The caller holds s.mu. The returned sentence is
// appended to the observation the model reads.
func (s *BrowserAgentSession) noteFrequencyRefusalLocked(snap BrowserSnapshot) string {
	if s == nil {
		return ""
	}
	s.ensureFrequencyMapsLocked()
	now := time.Now()
	// The notice fields are the lines the walker already accepted. The excerpt
	// is the whole page collapsed into one block; scanning it again would
	// treat a mention in that block as the notice.
	text := snap.PageFlags.FrequencyNotice + "\n" + snap.PageFlags.BlockingNotice
	if frequencyRefusalText(text) {
		s.stampFrequencyLocked(snap.URL, now)
		return frequencyRefusalStopText(false)
	}
	host := documentSite(snap.URL)
	key := documentPathKey(snap.URL)
	if host == "" || key == "" || !emptyShellAfterRefusal(snap.PageTextExcerpt) {
		return ""
	}
	at, ok := s.frequencyHostAt[host]
	if !ok || now.Sub(at) > frequencyRefusalTTL {
		return ""
	}
	s.stampFrequencyLocked(snap.URL, now)
	return "这个列表没有加载出来，和刚才的访问频繁是同一次拒绝。" + frequencyRefusalStopText(false)
}

func (s *BrowserAgentSession) ensureFrequencyMapsLocked() {
	if s.frequencyPathAt == nil {
		s.frequencyPathAt = map[string]time.Time{}
	}
	if s.frequencyHostAt == nil {
		s.frequencyHostAt = map[string]time.Time{}
	}
}

// rememberFrequencyDocument records a document whose readiness probe already
// decided it is the refusal. The caller does not hold s.mu.
func (s *BrowserAgentSession) rememberFrequencyDocument(raws ...string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureFrequencyMapsLocked()
	now := time.Now()
	for _, raw := range raws {
		s.stampFrequencyLocked(raw, now)
	}
}

// stampFrequencyLocked records the document and its site. A stamp that is
// still inside the window stays where it is, so reading the page again does
// not keep the refusal alive. The caller holds s.mu.
func (s *BrowserAgentSession) stampFrequencyLocked(raw string, now time.Time) {
	s.ensureFrequencyMapsLocked()
	s.pruneFrequencyLocked(now)
	keepFrequency(s.frequencyPathAt, documentPathKey(raw), now)
	keepFrequency(s.frequencyHostAt, documentSite(raw), now)
}

func (s *BrowserAgentSession) pruneFrequencyLocked(now time.Time) {
	for _, m := range []map[string]time.Time{s.frequencyPathAt, s.frequencyHostAt} {
		for k, at := range m {
			if now.Sub(at) > frequencyRefusalTTL {
				delete(m, k)
			}
		}
	}
}

func keepFrequency(m map[string]time.Time, key string, now time.Time) {
	if m == nil || key == "" {
		return
	}
	if at, ok := m[key]; ok && now.Sub(at) <= frequencyRefusalTTL {
		return
	}
	m[key] = now
}

// frequencyCurrentDocumentBlocked reports that the page last observed is on
// a site this session already saw refuse. Another request from that page is
// not sent. The caller does not hold s.mu.
func (s *BrowserAgentSession) frequencyCurrentDocumentBlocked() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	href := ""
	if s.snapshots != nil && s.lastSnapshotID != "" {
		if snap := s.snapshots[s.lastSnapshotID]; snap != nil {
			href = snap.URL
		}
	}
	s.mu.RUnlock()
	if href == "" {
		return ""
	}
	return s.frequencyNavigationBlocked(href)
}

func (s *BrowserAgentSession) frequencyHoldResult(action, msg string) *BrowserActionResult {
	if msg == "" {
		return nil
	}
	id := ""
	if s != nil {
		id = s.ID
	}
	return &BrowserActionResult{
		SessionID:       id,
		Action:          action,
		Status:          "unchanged",
		Display:         msg,
		batchStop:       true,
		batchStopReason: "frequency_refusal",
	}
}

// frequencyReloadClickBlocked stops a click on a page the site already
// refused. The click is another request, whether it reloads this document
// or opens a different one. The label is not consulted: a link named 首页
// is the same request as a reload. A pay control is refused earlier by the
// checkout guard, so this hold does not replace that card.
func (s *BrowserAgentSession) frequencyReloadClickBlocked(_ *BrowserElementRef, _ string) string {
	return s.frequencyCurrentDocumentBlocked()
}

// frequencyClickUnresolved is the stop sentence when the click could not be
// aimed and this page was already refused. The miss would otherwise say to
// probe again, which is another step on the site that said to stop. A ref
// that did resolve still reaches the checkout guard first.
func (s *BrowserAgentSession) frequencyClickUnresolved(err error) (*BrowserActionResult, error) {
	if err == nil {
		return nil, nil
	}
	if msg := s.frequencyCurrentDocumentBlocked(); msg != "" {
		return s.frequencyHoldResult("browser_click", msg), nil
	}
	return nil, err
}

// frequencyPressHitsSite is a key that reloads the page or activates the
// focused control. A letter does not, so typing into a field is not itself
// a new request.
func frequencyPressHitsSite(key string) bool {
	return reloadKey(key) || checkoutKeySubmits(key) || pressActivatesFocusedControl(key)
}

// reloadControlLabel is the control that reloads this document. The label is
// that action, or a few characters in front of the refresh phrase
// ("去购物刷新看看"). The same word inside another action ("刷新价格",
// "preload") is not a reload.
func reloadControlLabel(label string) bool {
	label = strings.TrimRight(strings.Join(strings.Fields(strings.ToLower(label)), " "), " ~～.。!！?？")
	if label == "" || len([]rune(label)) > 16 {
		return false
	}
	for _, phrase := range []string{"刷新看看", "刷新重试", "重新加载", "刷新", "重试", "try refreshing", "try again", "reload", "refresh"} {
		if label == phrase {
			return true
		}
	}
	for _, phrase := range []string{"刷新看看", "刷新重试", "重新加载", "try refreshing", "try again"} {
		if !strings.HasSuffix(label, phrase) {
			continue
		}
		prefix := strings.TrimSpace(strings.TrimSuffix(label, phrase))
		n := len([]rune(prefix))
		if n > 0 && n <= 4 {
			return true
		}
	}
	return false
}

func reloadKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), " ", ""))
	switch k {
	case "f5", "ctrl+r", "control+r", "ctrl+shift+r", "control+shift+r",
		"meta+r", "cmd+r", "command+r", "meta+shift+r", "cmd+shift+r":
		return true
	default:
		return false
	}
}

// frequencyNavigationBlocked reports a site this session already saw refused.
// The refusal is the site's decision about this browser, so another path on
// the same site is not sent either. The stamp is not refreshed here, so the
// window stays the one the page itself was read.
func (s *BrowserAgentSession) frequencyNavigationBlocked(raw string) string {
	if s == nil {
		return ""
	}
	key := documentPathKey(raw)
	site := documentSite(raw)
	if key == "" && site == "" {
		return ""
	}
	s.mu.RLock()
	at, ok := s.frequencyStampLocked(key, site)
	s.mu.RUnlock()
	if !ok || time.Since(at) > frequencyRefusalTTL {
		return ""
	}
	return frequencyRefusalStopText(true)
}

// frequencyStampLocked is the live path stamp, or the site stamp when this
// path itself was not the page that was read. The caller holds s.mu.
func (s *BrowserAgentSession) frequencyStampLocked(key, site string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	if key != "" && s.frequencyPathAt != nil {
		if at, ok := s.frequencyPathAt[key]; ok && time.Since(at) <= frequencyRefusalTTL {
			return at, true
		}
	}
	if site != "" && s.frequencyHostAt != nil {
		if at, ok := s.frequencyHostAt[site]; ok && time.Since(at) <= frequencyRefusalTTL {
			return at, true
		}
	}
	return time.Time{}, false
}

// documentPathKey is the document, not the exact request string. A trailing
// slash, a dot-segment, and the query are the same page the site just refused.
func documentPathKey(raw string) string {
	host := documentHost(raw)
	if host == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	p = path.Clean(p)
	if p != "/" {
		p = strings.TrimRight(p, "/")
	}
	return host + p
}
