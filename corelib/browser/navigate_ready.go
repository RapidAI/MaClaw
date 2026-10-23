package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Navigate readiness and login/risk-wall detection.
//
// After a navigate the page must be loaded (document.readyState=complete) and
// show at least one interactive element before the step returns; a page that
// stays blank is reloaded once. A page that is a sign-in form, a captcha /
// "安全验证" check, or is still blank after the wait is a login wall: the
// result carries LoginWallMarker so the caller classifies the step as
// login_wall and asks the person to take over instead of waiting blindly.
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
}

// pageReadyExpr counts visible interactive elements and looks for wall
// signals. It returns a JSON string so the Go side has one parse path.
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
  if (/安全验证|请完成验证|人机验证|滑动验证|拖动滑块|captcha|are you a robot|verify you are human/i.test(head)) wall = 'captcha';
  else if (document.querySelector('input[type=password]') && /登录|注册|sign ?in|log ?in/i.test(head + ' ' + document.title)) wall = 'login_form';
  return JSON.stringify({rs: document.readyState, n, url: location.href, title: document.title || '', tl: body.length, wall});
})()`

// pageReady reads the readiness state of the current page.
func (s *Session) pageReady() (PageReadyState, error) {
	var st PageReadyState
	raw, err := s.eval(pageReadyExpr, false, 3*time.Second)
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
			if st.ReadyState == "complete" && st.Interactive > 0 {
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
	Wall     string // "", login_url, login_form, captcha, blank
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
	if r.Wall != "" {
		fmt.Fprintf(&b, "\n%s %s: this page needs the person (sign-in, captcha/安全验证, or it stays blank). Do not wait or retry blindly: ask the person to take over the desktop and sign in / pass the check, then continue.", LoginWallMarker, r.Wall)
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
	if !ok || (st.Interactive == 0 && st.Wall == "" && loginWallURL(st.URL) == "") {
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
	case rep.State.Interactive == 0 && rep.State.TextLen < 20 && rep.Waited >= blankWall && !localPageURL(rep.State.URL):
		// A blank local page (an image preview, a dev server) has no
		// sign-in; only remote sites get the person called in (XHS-7).
		rep.Wall = "blank"
	}
	return rep, true
}

var localPageRe = regexp.MustCompile(`(?i)^(file:|data:|blob:|about:|https?://(localhost|127\.|0\.0\.0\.0|\[::1\])[:/]?)`)

func localPageURL(u string) bool { return localPageRe.MatchString(strings.TrimSpace(u)) }
