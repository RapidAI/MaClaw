package browser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// cdpSend issues a CDP command on the session's current connection.
func (s *Session) cdpSend(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		return nil, fmt.Errorf("no active CDP connection")
	}
	return c.Send(method, params, timeout)
}

// ExportAuthHeadersForURL harvests cookies (and the live page's User-Agent)
// for rawURL from the most recently active live agent browser session. The
// downloader's L2 anti-bot escalation uses it after the user or agent passed
// an interactive challenge (e.g. Cloudflare clearance) in the browser: the
// persistent profile's cookie store then holds the clearance cookie.
//
// Only cookies the browser itself would send for rawURL are exported
// (Network.getCookies filters by URL). An error is returned when no live
// browser session exists or the session holds no cookies for the URL's host.
func ExportAuthHeadersForURL(rawURL string) (map[string]string, error) {
	sess := mostRecentLiveAgentSession()
	if sess == nil {
		return nil, fmt.Errorf("no live browser session (open the site with the browser tool first)")
	}

	sess.mu.RLock()
	s := sess.session
	sess.mu.RUnlock()
	if s == nil {
		return nil, fmt.Errorf("browser session %s has no active connection", sess.ID)
	}

	raw, err := s.cdpSend("Network.getCookies", map[string]interface{}{
		"urls": []string{rawURL},
	}, DefaultCmdTimeout)
	if err != nil {
		return nil, fmt.Errorf("read browser cookies failed: %w", err)
	}
	var payload struct {
		Cookies []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse browser cookies failed: %w", err)
	}
	if len(payload.Cookies) == 0 {
		return nil, fmt.Errorf("browser session has no cookies for %s", hostOf(rawURL))
	}

	parts := make([]string, 0, len(payload.Cookies))
	for _, c := range payload.Cookies {
		if c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("browser session has no usable cookies for %s", hostOf(rawURL))
	}

	headers := map[string]string{"Cookie": strings.Join(parts, "; ")}
	// Match the downloader's UA to the real browser so the clearance cookie
	// (bound to UA on some sites) stays valid.
	if ua, err := s.Eval("navigator.userAgent"); err == nil {
		if ua = strings.TrimSpace(ua); ua != "" {
			headers["User-Agent"] = ua
		}
	}
	return headers, nil
}

// mostRecentLiveAgentSession returns the live agent session with the most
// recent activity, preferring persistent sessions (their cookie store is the
// one users log into).
func mostRecentLiveAgentSession() *BrowserAgentSession {
	browserAgentMu.Lock()
	candidates := make([]*BrowserAgentSession, 0, len(browserAgentSessions))
	for _, sess := range browserAgentSessions {
		if sess != nil {
			candidates = append(candidates, sess)
		}
	}
	browserAgentMu.Unlock()
	var best *BrowserAgentSession
	for _, sess := range candidates {
		if !sess.cdpClient().IsAlive() {
			continue
		}
		if best == nil {
			best = sess
			continue
		}
		sess.mu.RLock()
		sessMode := sess.Mode
		sessLast := sess.LastActivityAt
		sess.mu.RUnlock()
		best.mu.RLock()
		bestPersistent := best.Mode == SessionModePersistent
		bestLast := best.LastActivityAt
		best.mu.RUnlock()
		sessPersistent := sessMode == SessionModePersistent
		if sessPersistent != bestPersistent {
			if sessPersistent {
				best = sess
			}
			continue
		}
		if sessLast.After(bestLast) {
			best = sess
		}
	}
	return best
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
