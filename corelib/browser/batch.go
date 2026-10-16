package browser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// Link clicks poll location.href until it changes or this cap expires.
	// Same-page controls do not wait on DOM quiet, so a noisy page cannot
	// stall every step for the whole cap.
	batchLinkURLWait        = 2 * time.Second
	batchURLPollInterval    = 40 * time.Millisecond
	batchURLUnchangedSettle = 200 * time.Millisecond
)

// actionFollow is the post-action settle/observe decision for one step.
type actionFollow struct {
	obs        *BrowserObservation
	blocked    *BrowserActionResult
	err        error
	stop       bool
	reason     string
	observeErr string
}

func (f actionFollow) fail() (*BrowserActionResult, error) {
	if f.blocked != nil || f.err != nil {
		return f.blocked, f.err
	}
	return nil, nil
}

func (f actionFollow) stamp(result *BrowserActionResult) *BrowserActionResult {
	if result != nil && f.stop {
		result.batchStop = true
		if result.batchStopReason == "" {
			result.batchStopReason = f.reason
		}
	}
	if result != nil && f.observeErr != "" {
		result.batchObserveErr = f.observeErr
	}
	return result
}

func (s *BrowserAgentSession) beginFastBatch() {
	if s == nil {
		return
	}
	sig, err := s.pageSignal()
	if sig.URL == "" && s.session != nil && err != nil {
		for i := 0; i < 2; i++ {
			time.Sleep(20 * time.Millisecond)
			sig, err = s.pageSignal()
			if sig.URL != "" || err == nil {
				break
			}
		}
	}
	s.mu.Lock()
	s.fastBatch = true
	s.fastBatchURL = sig.URL
	s.fastBatchTitle = sig.Title
	s.fastBatchMark = sig.Mark
	s.fastBatchLoading = strings.EqualFold(strings.TrimSpace(sig.Ready), "loading")
	s.mu.Unlock()
}

func (s *BrowserAgentSession) endFastBatch() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.fastBatch = false
	s.fastBatchURL = ""
	s.fastBatchTitle = ""
	s.fastBatchMark = ""
	s.fastBatchLoading = false
	s.mu.Unlock()
}

func (s *BrowserAgentSession) fastBatchActive() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fastBatch
}

func (s *BrowserAgentSession) pageURL() (string, error) {
	if s == nil || s.session == nil {
		return "", nil
	}
	raw, err := s.session.Eval("location.href")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}

type pageSignal struct {
	URL   string `json:"url"`
	Ready string `json:"ready"`
	Title string `json:"title"`
	Mark  string `json:"mark"`
}

func (s *BrowserAgentSession) pageSignal() (pageSignal, error) {
	if s == nil || s.session == nil {
		return pageSignal{}, nil
	}
	raw, err := s.session.Eval(`(function(){var main=document.querySelector("main, [role='main']");var h=main?main.querySelector("h1, h2"):document.querySelector("h1, h2");var heading="";try{heading=h?String(h.textContent||"").replace(/\s+/g," ").trim().slice(0,120):""}catch(e){}return JSON.stringify({url:location.href,ready:document.readyState,title:document.title||"",mark:heading})})()`)
	if err != nil {
		return pageSignal{}, err
	}
	var sig pageSignal
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &sig); err != nil {
		return pageSignal{}, err
	}
	sig.URL = strings.TrimSpace(sig.URL)
	return sig, nil
}

// batchURLWatchDecision stops early when the address changes. A document that
// stays complete keeps the batch moving after a short settle. readyState
// "loading" extends the wait up to timeout so a slow navigation can still stop
// the batch.
func batchURLWatchDecision(startURL, nowURL, ready string, alreadyLoading bool, elapsed, timeout, settle time.Duration) (changed, done bool) {
	if startURL != "" && nowURL != "" && !sameDocumentURL(startURL, nowURL) {
		return true, true
	}
	if elapsed >= timeout {
		return false, true
	}
	loading := strings.EqualFold(strings.TrimSpace(ready), "loading")
	// A document that was already loading did not start navigating because of
	// this click. Waiting out the cap would add seconds to every step.
	if loading && !alreadyLoading {
		return false, false
	}
	if elapsed >= settle {
		return false, true
	}
	return false, false
}

func (s *BrowserAgentSession) watchBatchURL(timeout, settle time.Duration) (bool, string) {
	if timeout <= 0 || s == nil || s.session == nil {
		return false, ""
	}
	s.mu.RLock()
	start := s.fastBatchURL
	startTitle := s.fastBatchTitle
	startMark := s.fastBatchMark
	alreadyLoading := s.fastBatchLoading
	s.mu.RUnlock()
	if start == "" {
		return false, ""
	}
	started := time.Now()
	for {
		if !s.IsTargetAlive() {
			return true, "target_gone"
		}
		sig, err := s.pageSignal()
		elapsed := time.Since(started)
		if err == nil {
			// A page that was already loading can still be filling in its
			// heading. That is not a click changing the view.
			if !alreadyLoading && viewChanged(startTitle, sig.Title, startMark, sig.Mark) {
				return true, "view_changed"
			}
			changed, done := batchURLWatchDecision(start, sig.URL, sig.Ready, alreadyLoading, elapsed, timeout, settle)
			if done {
				if changed {
					return true, "url_changed"
				}
				return false, ""
			}
		} else if !s.IsTargetAlive() {
			return true, "target_gone"
		} else if elapsed >= timeout {
			return false, ""
		}
		remaining := timeout - elapsed
		if remaining <= 0 {
			return false, ""
		}
		step := batchURLPollInterval
		if remaining < step {
			step = remaining
		}
		time.Sleep(step)
	}
}

func viewToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			continue
		}
		b.WriteRune(r)
	}
	token := strings.Join(strings.Fields(b.String()), " ")
	token = strings.NewReplacer("(", "", ")", "", "[", "", "]", "").Replace(token)
	return strings.Join(strings.Fields(token), " ")
}

func viewChanged(startTitle, nowTitle, startMark, nowMark string) bool {
	// A stable heading means the view did not change. Title badges and
	// notification counts must not stop a form fill.
	startMark = viewToken(startMark)
	nowMark = viewToken(nowMark)
	if startMark != "" || nowMark != "" {
		return startMark != "" && nowMark != "" && startMark != nowMark
	}
	startTitle = viewToken(startTitle)
	nowTitle = viewToken(nowTitle)
	return startTitle != "" && nowTitle != "" && startTitle != nowTitle
}

func sameDocumentURL(a, b string) bool {
	return canonicalBrowserURL(a) == canonicalBrowserURL(b)
}

// canonicalBrowserURL ignores fragment, host case, default ports, and trailing
// slashes. /a, /a/, and /a/// are the same document.
func canonicalBrowserURL(raw string) string {
	raw = stripURLHash(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return raw
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" && !((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
		host += ":" + port
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	for len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	out := scheme + "://"
	if parsed.User != nil {
		out += parsed.User.String() + "@"
	}
	out += host + path
	if parsed.RawQuery != "" {
		out += "?" + parsed.RawQuery
	}
	return out
}

func stripURLHash(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		return raw[:i]
	}
	return raw
}

// refMayNavigate reports whether a click target is a link. A real <a> selector
// counts. Tags that merely start with "a", such as article, do not.
func refMayNavigate(ref *BrowserElementRef, selector string) bool {
	if ref != nil {
		role := strings.ToLower(strings.TrimSpace(ref.Role))
		tag := strings.ToLower(strings.TrimSpace(ref.Tag))
		return role == "link" || tag == "a"
	}
	return selectorLooksLikeAnchor(selector)
}

func selectorHasRoleLink(sel string) bool {
	for _, marker := range []string{`role=link`, `role="link"`, `role='link'`} {
		idx := 0
		for {
			at := strings.Index(sel[idx:], marker)
			if at < 0 {
				break
			}
			at += idx
			end := at + len(marker)
			if end >= len(sel) {
				return true
			}
			next := sel[end]
			if next == '-' || next == '_' || (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
				idx = end
				continue
			}
			return true
		}
	}
	return false
}

func selectorLooksLikeAnchor(selector string) bool {
	sel := strings.ToLower(strings.TrimSpace(selector))
	if sel == "" {
		return false
	}
	if selectorHasRoleLink(sel) {
		return true
	}
	for i := 0; i < len(sel); i++ {
		if sel[i] != 'a' {
			continue
		}
		if i > 0 {
			switch sel[i-1] {
			case ' ', '>', '+', '~', ',':
			default:
				continue
			}
		}
		if i+1 == len(sel) {
			return true
		}
		switch sel[i+1] {
		case ' ', '.', '#', '[', ':', '>', '+', '~', ',':
			return true
		}
	}
	return false
}

// batchClickURLWatch returns how long to poll, and how long an unchanged
// document may sit before the batch continues. Links keep a longer settle
// because the address can update late. Other clicks take one extra sample so
// a same-URL view change can land, then continue.
func batchClickURLWatch(ref *BrowserElementRef, selector string) (timeout, settle time.Duration) {
	if refMayNavigate(ref, selector) {
		return batchLinkURLWait, batchURLUnchangedSettle
	}
	return batchLinkURLWait, batchURLPollInterval
}

// followAction settles and observes unless a fast batch is running and this
// step is not expected to change the page. pageChanging forces the full path
// and stops the batch. urlWait polls location.href and stops the batch when
// it changes. A zero urlWait skips that poll.
func (s *BrowserAgentSession) followAction(action string, settleFor, quiet time.Duration, pageChanging bool, urlWait, urlSettle time.Duration, reason string) actionFollow {
	if s != nil && s.fastBatchActive() && !pageChanging {
		if stopWatch, why := s.watchBatchURL(urlWait, urlSettle); stopWatch {
			obs, blocked, err := s.observeAfterAction(action)
			if blocked != nil {
				return actionFollow{blocked: blocked}
			}
			if why == "" {
				why = "url_changed"
			}
			if err != nil {
				// The click already changed the page. Failing the step makes the
				// model repeat it. Stop the batch and report the read failure.
				return actionFollow{stop: true, reason: why, observeErr: err.Error()}
			}
			return actionFollow{obs: obs, stop: true, reason: why}
		}
		return actionFollow{}
	}
	if s != nil && s.fastBatchActive() && pageChanging {
		// Navigate already waited for load. Submit and dialog should not sit
		// out a full DOM-quiet window on a noisy page; poll the URL, then read.
		if action != "browser_navigate" {
			_, _ = s.watchBatchURL(batchLinkURLWait, batchURLUnchangedSettle)
		}
		obs, blocked, err := s.observeAfterAction(action)
		if blocked != nil {
			return actionFollow{blocked: blocked}
		}
		if reason == "" {
			reason = "url_changed"
		}
		if err != nil {
			return actionFollow{stop: true, reason: reason, observeErr: err.Error()}
		}
		return actionFollow{obs: obs, stop: true, reason: reason}
	}
	if s != nil {
		s.waitForActionSettle(settleFor, quiet)
	}
	obs, blocked, err := s.observeAfterAction(action)
	return actionFollow{obs: obs, blocked: blocked, err: err}
}

// bindLatestSnapshotRefs fills snapshot_id from the latest probe when the
// step names a ref that snapshot actually contains. Unknown refs are left
// alone so captcha handling still runs before a missing-ref error.
func (s *BrowserAgentSession) bindLatestSnapshotRefs(steps []StepSpec) {
	if s == nil {
		return
	}
	s.mu.RLock()
	snapshotID := s.lastSnapshotID
	var snap *BrowserSnapshot
	if s.snapshots != nil {
		snap = s.snapshots[snapshotID]
	}
	s.mu.RUnlock()
	if snap == nil || strings.TrimSpace(snapshotID) == "" {
		return
	}
	known := map[string]bool{}
	for _, ref := range snap.Refs {
		known[strings.ToLower(strings.TrimSpace(ref.Ref))] = true
	}
	for i := range steps {
		if steps[i].Params == nil {
			continue
		}
		ref := strings.TrimSpace(steps[i].Params["ref"])
		if ref == "" || strings.TrimSpace(steps[i].Params["snapshot_id"]) != "" {
			continue
		}
		if !known[strings.ToLower(ref)] {
			continue
		}
		steps[i].Params["snapshot_id"] = snapshotID
	}
}

// preflightBatchRefs rejects steps whose snapshot ref is already missing.
// Steps without snapshot_id are left to the action, so captcha-ask and legacy
// calls keep their current order.
func (s *BrowserAgentSession) preflightBatchRefs(steps []StepSpec) error {
	if s == nil {
		return fmt.Errorf("browser session is nil")
	}
	for i, step := range steps {
		if step.Params == nil {
			continue
		}
		ref := strings.TrimSpace(step.Params["ref"])
		snapshotID := strings.TrimSpace(step.Params["snapshot_id"])
		if ref == "" || snapshotID == "" {
			continue
		}
		if _, _, err := s.selectorCandidatesForAction(snapshotID, ref, step.Params["selector"]); err != nil {
			return fmt.Errorf("step %d ref %s: %w", i+1, ref, err)
		}
	}
	return nil
}
