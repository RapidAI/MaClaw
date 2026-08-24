package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// observeWithRecovery attempts Observe, and if it fails due to target-gone
// (common after click-triggered navigations that destroy the old target),
// tries to recover by re-discovering page targets and switching to the new one.
// This prevents false-failure reports to the LLM when the action itself
// succeeded but the post-action observation fails.
func (s *BrowserAgentSession) observeWithRecovery() (*BrowserObservation, error) {
	obs, err := s.Observe(false)
	if err == nil {
		return obs, nil
	}
	// Only attempt recovery if the failure is target-gone.
	if s.IsTargetAlive() {
		return nil, err // different error, don't mask it
	}
	s.recoverMu.Lock()
	if s.IsTargetAlive() {
		s.recoverMu.Unlock()
		return s.Observe(false)
	}
	s.mu.RLock()
	session := s.session
	policy := s.Policy
	currentID := s.TargetID
	s.mu.RUnlock()
	if session == nil {
		s.recoverMu.Unlock()
		return nil, err
	}
	if attachErr := attachSessionToRecoverablePage(session, policy, currentID); attachErr != nil {
		s.recoverMu.Unlock()
		if isPolicyDenied(attachErr) {
			return nil, attachErr
		}
		return nil, err
	}
	newTargetID := activeTargetID(session)
	log.Printf("[browser] observeWithRecovery: target gone, switching to %s session=%s", newTargetID, s.ID)
	s.mu.Lock()
	s.TargetID = newTargetID
	s.snapshots = map[string]*BrowserSnapshot{}
	s.lastSnapshotID = ""
	s.resetTargetGone()
	s.mu.Unlock()
	s.recoverMu.Unlock()
	s.startEventPump()
	return s.Observe(false)
}

func (s *BrowserAgentSession) observeAfterAction(action string) (*BrowserObservation, *BrowserActionResult, error) {
	obs, err := s.observeWithRecovery()
	if err != nil {
		blocked, err := policyBlockResult(s, action, err)
		return nil, blocked, err
	}
	if obs == nil {
		return nil, nil, fmt.Errorf("empty observe result")
	}
	return obs, nil, nil
}

// Navigate performs a browser navigation under the agent session.
func (s *BrowserAgentSession) Navigate(url string) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("missing url")
	}
	if err := validateNavigationPolicy(s.Policy, url, currentDomainFromSession(s)); err != nil {
		return policyBlockResult(s, "browser_navigate", err)
	}
	if _, err := s.session.Navigate(url); err != nil {
		return policyBlockResult(s, "browser_navigate", err)
	}
	s.waitForActionSettle(3*time.Second, 300*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_navigate")
	if blocked != nil || err != nil {
		return blocked, err
	}
	s.appendActionTrace("navigate", fmt.Sprintf("navigate to %s", url))
	return s.completeAction("browser_navigate", fmt.Sprintf("navigated to %s", url), url, obs, map[string]interface{}{
		"url":   obs.Snapshot.URL,
		"title": obs.Snapshot.Title,
	}, true), nil
}

func (s *BrowserAgentSession) waitForActionSettle(timeout, quiet time.Duration) {
	if s == nil || s.session == nil {
		return
	}
	// Fast path: target already gone, no point starting a settle wait.
	if !s.IsTargetAlive() {
		return
	}
	// Capture session pointer at spawn time. Even if s.session is later replaced
	// by a reconnect, the goroutine safely uses the old (self-contained) Session.
	sess := s.session
	// Run WaitForStable in a goroutine so we can abort on target destruction.
	// The goroutine will self-terminate within 'timeout' even if we abandon it,
	// because WaitForStable respects its deadline.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = sess.WaitForStable(timeout, quiet)
	}()
	select {
	case <-done:
		// Settle completed normally.
	case <-s.TargetGone():
		// Target destroyed — no point waiting for stability.
		// The goroutine will exit on its own within 'timeout' (WaitForStable
		// respects its deadline). It holds no locks, so this is safe.
		s.appendActionTrace("settle", "page settle aborted: target gone")
	}
}

func (s *BrowserAgentSession) selectorCandidatesForAction(snapshotID, ref, selector string) ([]string, *BrowserElementRef, error) {
	return s.locatorCandidates(snapshotID, ref, selector)
}

func (s *BrowserAgentSession) gateCandidate(frameID, selector string) error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.rejectNonUniqueInFrame(frameID, selector)
}

func (s *BrowserAgentSession) clickWithCandidates(candidates []string, resolved *BrowserElementRef) (string, int, error) {
	frameID := ""
	if resolved != nil {
		frameID = resolved.FrameID
	}
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		if err := s.gateCandidate(frameID, candidate); err != nil {
			lastErr = err
			if isFrameGoneErr(err) {
				return "", attempts, err
			}
			continue
		}
		var err error
		if frameID != "" && frameID != "main" {
			err = s.session.ClickInFrame(frameID, candidate)
		} else {
			err = s.session.Click(candidate)
		}
		if err == nil {
			return candidate, attempts, nil
		}
		lastErr = err
	}
	if resolved != nil && resolved.BackendNodeID != 0 {
		attempts++
		sessionID := ""
		if s.session != nil {
			sessionID = s.session.frameSessionID(resolved.FrameID)
		}
		if err := s.session.clickBackendNodeOn(sessionID, resolved.BackendNodeID); err == nil {
			return resolved.Ref, attempts, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", attempts, lastErr
	}
	return "", attempts, fmt.Errorf("no usable selector candidates")
}

func (s *BrowserAgentSession) typeWithCandidates(candidates []string, text, contentFormat string, appendText bool, resolved *BrowserElementRef) (string, int, error) {
	frameID := ""
	if resolved != nil {
		frameID = resolved.FrameID
	}
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		if err := s.gateCandidate(frameID, candidate); err != nil {
			lastErr = err
			if isFrameGoneErr(err) {
				return "", attempts, err
			}
			continue
		}
		var err error
		if frameID != "" && frameID != "main" {
			err = s.session.TypeInFrame(frameID, candidate, text, contentFormat, appendText)
		} else if appendText {
			err = s.session.TypeContentMaybeAppend(candidate, text, contentFormat, true)
		} else {
			err = s.session.TypeContent(candidate, text, contentFormat)
		}
		if err == nil {
			return candidate, attempts, nil
		}
		lastErr = err
	}
	if resolved != nil && resolved.BackendNodeID != 0 {
		attempts++
		sessionID := ""
		if s.session != nil {
			sessionID = s.session.frameSessionID(resolved.FrameID)
		}
		if err := s.session.typeBackendNodeOn(sessionID, resolved.BackendNodeID, text, contentFormat, appendText); err == nil {
			return resolved.Ref, attempts, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", attempts, lastErr
	}
	return "", attempts, fmt.Errorf("no usable selector candidates")
}

func (s *BrowserAgentSession) waitWithCandidates(candidates []string, timeoutSec int, resolved *BrowserElementRef) (string, int, error) {
	frameID := ""
	if resolved != nil {
		frameID = resolved.FrameID
	}
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		if err := s.session.WaitForSelectorInFrame(frameID, candidate, timeoutSec); err == nil {
			return candidate, attempts, nil
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", attempts, lastErr
	}
	return "", attempts, fmt.Errorf("no usable selector candidates")
}

func (s *BrowserAgentSession) extractWithCandidates(candidates []string, resolved *BrowserElementRef) (string, string, int, error) {
	frameID := ""
	if resolved != nil {
		frameID = resolved.FrameID
	}
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		if err := s.gateCandidate(frameID, candidate); err != nil {
			lastErr = err
			if isFrameGoneErr(err) {
				return "", "", attempts, err
			}
			continue
		}
		text, err := s.session.GetTextInFrame(frameID, candidate)
		if err == nil {
			return candidate, text, attempts, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", "", attempts, lastErr
	}
	return "", "", attempts, fmt.Errorf("no usable selector candidates")
}

func (s *BrowserAgentSession) submitClickKey(ref *BrowserElementRef, selector, fallbackText string) string {
	label := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(fallbackText)), " "))
	if ref != nil {
		if name := strings.TrimSpace(ref.Name); name != "" {
			label = strings.ToLower(strings.Join(strings.Fields(name), " "))
		}
		if label == "" && strings.TrimSpace(ref.Text) != "" {
			label = strings.ToLower(strings.Join(strings.Fields(ref.Text), " "))
		}
	}
	selectorLower := strings.ToLower(strings.TrimSpace(selector))
	var info *PageInfo
	if s != nil && s.session != nil {
		info, _ = s.session.Info()
	}
	url := ""
	if info != nil {
		url = strings.ToLower(info.URL)
	}
	risky := false
	for _, marker := range []string{"publish", "post", "submit", "send", "发布", "发表", "发送", "提交", "确认发布"} {
		if strings.Contains(label, marker) || strings.Contains(selectorLower, marker) {
			risky = true
			break
		}
	}
	if !risky && containsSubmitClickMarker(label, selectorLower) {
		risky = true
	}
	if strings.Contains(url, "zhihu.com") && strings.Contains(selectorLower, "button--blue") {
		risky = true
		if label == "" {
			label = "zhihu-blue-submit"
		}
	}
	if !risky {
		return ""
	}
	if label == "" {
		label = selectorLower
	}
	return strings.TrimSpace(url + "|" + label)
}

func containsSubmitClickMarker(label, selector string) bool {
	for _, marker := range []string{"\u53d1\u5e03", "\u53d1\u8868", "\u53d1\u9001", "\u63d0\u4ea4", "\u786e\u8ba4\u53d1\u5e03"} {
		if strings.Contains(label, marker) || strings.Contains(selector, marker) {
			return true
		}
	}
	return false
}

func (s *BrowserAgentSession) guardSubmitClick(key string) error {
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.submitClickRecentLocked(key, time.Now()) {
		return fmt.Errorf("non-idempotent browser click was already attempted recently; observe/verify page state before retrying")
	}
	return nil
}

func (s *BrowserAgentSession) forgetSubmitClick(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recentSubmitClicks, key)
}

func (s *BrowserAgentSession) rememberSubmitClick(key string) {
	if key == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recentSubmitClicks == nil {
		s.recentSubmitClicks = map[string]time.Time{}
	}
	for k, ts := range s.recentSubmitClicks {
		if now.Sub(ts) > 3*time.Minute {
			delete(s.recentSubmitClicks, k)
		}
	}
	s.recentSubmitClicks[key] = now
}

func (s *BrowserAgentSession) submitClickRecentLocked(key string, now time.Time) bool {
	if s.recentSubmitClicks == nil {
		return false
	}
	for k, ts := range s.recentSubmitClicks {
		if now.Sub(ts) > 3*time.Minute {
			delete(s.recentSubmitClicks, k)
		}
	}
	ts, ok := s.recentSubmitClicks[key]
	return ok && now.Sub(ts) <= 3*time.Minute
}

// Click clicks an element by ref or selector.
func (s *BrowserAgentSession) Click(snapshotID, ref, selector string) (*BrowserActionResult, error) {
	if ask := s.captchaAskIfNeeded("click"); ask != nil {
		return ask, nil
	}
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	ref = strings.TrimSpace(ref)
	selector = strings.TrimSpace(selector)
	candidates, resolvedRef, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
	if err != nil {
		return nil, err
	}
	if err := rejectDisabledRef(resolvedRef); err != nil {
		return nil, err
	}
	submitKey := s.submitClickKey(resolvedRef, selector, "")
	if err := s.guardSubmitClick(submitKey); err != nil {
		return nil, err
	}
	resolvedSelector, attempts, err := s.clickWithCandidates(candidates, resolvedRef)
	if err != nil {
		if ref != "" {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs", ref)
		}
		return nil, err
	}
	if attempts > 1 && ref != "" {
		s.appendActionTrace("retry", fmt.Sprintf("click fallback selector succeeded %s -> %s", ref, resolvedSelector))
	}
	s.waitForActionSettle(2*time.Second, 250*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_click")
	if blocked != nil || err != nil {
		return blocked, err
	}
	target := resolvedSelector
	if resolvedRef != nil {
		target = resolvedRef.Ref
	}
	s.appendActionTrace("click", fmt.Sprintf("click %s", target))
	result := s.completeAction("browser_click", fmt.Sprintf("clicked %s", target), target, obs, map[string]interface{}{
		"target": target,
	}, activatingRef(resolvedRef))
	if result != nil {
		result.GoalClass = submitKey != ""
		result.submitRememberKey = submitKey
	}
	return result, nil
}

// ClickText clicks an element from the latest snapshot by visible text/name.
func (s *BrowserAgentSession) ClickText(snapshotID, text string) (*BrowserActionResult, error) {
	if ask := s.captchaAskIfNeeded("click"); ask != nil {
		return ask, nil
	}
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("missing text")
	}
	candidates, resolvedRef, err := s.selectorCandidatesForText(snapshotID, text)
	if err != nil {
		return nil, err
	}
	if err := rejectDisabledRef(resolvedRef); err != nil {
		return nil, err
	}
	submitKey := s.submitClickKey(resolvedRef, "", text)
	if err := s.guardSubmitClick(submitKey); err != nil {
		return nil, err
	}
	resolvedSelector, attempts, err := s.clickWithCandidates(candidates, resolvedRef)
	if err != nil {
		return nil, fmt.Errorf("text %q is stale; run observe again to get fresh refs", text)
	}
	if attempts > 1 {
		s.appendActionTrace("retry", fmt.Sprintf("click text fallback selector succeeded %s -> %s", text, resolvedSelector))
	}
	s.waitForActionSettle(2*time.Second, 250*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_click")
	if blocked != nil || err != nil {
		return blocked, err
	}
	target := text
	if resolvedRef != nil {
		target = resolvedRef.Ref
	}
	s.appendActionTrace("click", fmt.Sprintf("click text %s", text))
	result := s.completeAction("browser_click", fmt.Sprintf("clicked text %s", text), target, obs, map[string]interface{}{
		"target": target,
		"text":   text,
	}, activatingRef(resolvedRef))
	if result != nil {
		result.GoalClass = submitKey != ""
		result.submitRememberKey = submitKey
	}
	return result, nil
}

// Type enters text into an element by ref or selector.
func (s *BrowserAgentSession) Type(snapshotID, ref, selector, text string) (*BrowserActionResult, error) {
	return s.TypeContentAppend(snapshotID, ref, selector, text, BrowserContentFormatPlain, false)
}

// TypeContent enters plain text or rich markdown content into an element by ref or selector.
func (s *BrowserAgentSession) TypeContent(snapshotID, ref, selector, text, contentFormat string) (*BrowserActionResult, error) {
	return s.TypeContentAppend(snapshotID, ref, selector, text, contentFormat, false)
}

// TypeContentAppend types into a ref/selector, optionally appending instead of replacing.
func (s *BrowserAgentSession) TypeContentAppend(snapshotID, ref, selector, text, contentFormat string, appendText bool) (*BrowserActionResult, error) {
	if ask := s.captchaAskIfNeeded("type"); ask != nil {
		return ask, nil
	}
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	ref = strings.TrimSpace(ref)
	selector = strings.TrimSpace(selector)
	contentFormat = normalizeBrowserContentFormat(contentFormat)
	if ref == "" && selector == "" {
		if err := s.session.TypeActiveContent(text, contentFormat); err != nil {
			return nil, err
		}
		s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
		obs, blocked, err := s.observeAfterAction("browser_type")
		if blocked != nil || err != nil {
			return blocked, err
		}
		s.appendActionTrace("type", "typed into active element")
		return s.completeAction("browser_type", fmt.Sprintf("typed %d chars into active element", len([]rune(text))), "activeElement", obs, map[string]interface{}{
			"target":         "activeElement",
			"text_length":    len([]rune(text)),
			"content_format": contentFormat,
		}, false), nil
	}
	var resolvedRef *BrowserElementRef
	var err error
	candidates, resolvedRef, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
	if err != nil {
		return nil, err
	}
	if err := rejectDisabledRef(resolvedRef); err != nil {
		return nil, err
	}
	resolvedSelector, attempts, err := s.typeWithCandidates(candidates, text, contentFormat, appendText, resolvedRef)
	if err != nil {
		if ref != "" {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs", ref)
		}
		return nil, err
	}
	if attempts > 1 && ref != "" {
		s.appendActionTrace("retry", fmt.Sprintf("type fallback selector succeeded %s -> %s", ref, resolvedSelector))
	}
	s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_type")
	if blocked != nil || err != nil {
		return blocked, err
	}
	target := resolvedSelector
	if resolvedRef != nil {
		target = resolvedRef.Ref
	}
	s.appendActionTrace("type", fmt.Sprintf("type into %s", target))
	return s.completeAction("browser_type", fmt.Sprintf("typed %d chars into %s", len([]rune(text)), target), target, obs, map[string]interface{}{
		"target":         target,
		"text_length":    len([]rune(text)),
		"content_format": contentFormat,
	}, false), nil
}

func waitTimeoutSec(durationMS int) int {
	if durationMS < 1000 {
		return 10
	}
	return (durationMS + 999) / 1000
}

// Wait pauses or waits for a selector/ref to appear.
func (s *BrowserAgentSession) Wait(snapshotID, ref, selector string, durationMS int) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	var resolvedRef *BrowserElementRef
	var err error
	resolvedSelector := strings.TrimSpace(selector)
	if resolvedSelector != "" {
		candidates, resolved, err := s.selectorCandidatesForAction(snapshotID, "", resolvedSelector)
		if err != nil {
			return nil, err
		}
		resolvedSelector, _, err = s.waitWithCandidates(candidates, waitTimeoutSec(durationMS), resolved)
		if err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(ref) != "" {
		candidates, refInfo, err := s.selectorCandidatesForAction(snapshotID, ref, "")
		if err != nil {
			return nil, err
		}
		resolvedRef = refInfo
		resolvedSelector, attempts, err := s.waitWithCandidates(candidates, waitTimeoutSec(durationMS), resolvedRef)
		if err != nil {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs", ref)
		}
		if attempts > 1 {
			s.appendActionTrace("retry", fmt.Sprintf("wait fallback selector succeeded %s -> %s", ref, resolvedSelector))
		}
		_ = resolvedRef
	} else {
		if durationMS <= 0 {
			durationMS = 1000
		}
		time.Sleep(time.Duration(durationMS) * time.Millisecond)
	}
	s.waitForActionSettle(2*time.Second, 250*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_wait")
	if blocked != nil || err != nil {
		return blocked, err
	}
	s.appendActionTrace("wait", "wait for page stability")
	return s.completeAction("browser_wait", "wait complete", "", obs, map[string]interface{}{
		"duration_ms": durationMS,
	}, false), nil
}

// Refresh reloads the current page and refreshes snapshot state.
func (s *BrowserAgentSession) Refresh() (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	info, err := s.session.Info()
	if err != nil || info == nil || strings.TrimSpace(info.URL) == "" {
		return nil, fmt.Errorf("unable to determine current page for refresh")
	}
	result, err := s.Navigate(info.URL)
	if err != nil {
		return nil, err
	}
	if result != nil && result.Status != "blocked" && result.Status != "ask" {
		result.Action = "browser_refresh"
		result.Display = fmt.Sprintf("refreshed %s", info.URL)
		result.Detail = info.URL
		s.appendActionTrace("refresh", fmt.Sprintf("refresh %s", info.URL))
	}
	return result, nil
}

// Back performs browser back navigation and refreshes snapshot state.
func (s *BrowserAgentSession) Back() (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	if err := s.session.Back(); err != nil {
		return nil, err
	}
	s.waitForActionSettle(3*time.Second, 300*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_back")
	if blocked != nil || err != nil {
		return blocked, err
	}
	s.appendActionTrace("back", "browser back")
	return s.completeAction("browser_back", "went back", "", obs, map[string]interface{}{
		"url":   obs.Snapshot.URL,
		"title": obs.Snapshot.Title,
	}, true), nil
}

// Extract returns text from a specific target or the page summary.
func (s *BrowserAgentSession) Extract(snapshotID, ref, selector, query, format string, offset, maxChars int) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	query = strings.TrimSpace(query)
	value := ""
	resolvedSelector := strings.TrimSpace(selector)
	pageTotal := 0
	pageHasMore := false
	pageOffset := 0
	if resolvedSelector != "" {
		candidates, resolved, err := s.selectorCandidatesForAction(snapshotID, "", resolvedSelector)
		if err != nil {
			return nil, err
		}
		resolvedSelector, value, _, err = s.extractWithCandidates(candidates, resolved)
		if err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(ref) != "" {
		candidates, resolved, err := s.selectorCandidatesForAction(snapshotID, ref, "")
		if err != nil {
			return nil, err
		}
		var attempts int
		resolvedSelector, value, attempts, err = s.extractWithCandidates(candidates, resolved)
		if err != nil {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs", ref)
		}
		if attempts > 1 {
			s.appendActionTrace("retry", fmt.Sprintf("extract fallback selector succeeded %s -> %s", ref, resolvedSelector))
		}
	} else if snapshotID != "" {
		snap, ok := s.GetSnapshot(snapshotID)
		if !ok {
			return nil, fmt.Errorf("browser snapshot not found: %s", snapshotID)
		}
		value = snap.PageTextExcerpt
		pageTotal = snap.PageTextTotal
		pageHasMore = snap.PageTextHasMore
		pageOffset = snap.PageTextOffset
		var err error
		value, pageTotal, pageHasMore, pageOffset, err = s.extractPageTextWindow(offset, maxChars)
		if err != nil {
			return nil, err
		}
	} else {
		obs, blocked, err := s.observeAfterAction("browser_extract")
		if blocked != nil || err != nil {
			return blocked, err
		}
		value = obs.Snapshot.PageTextExcerpt
		snapshotID = obs.Snapshot.SnapshotID
		pageTotal = obs.Snapshot.PageTextTotal
		pageHasMore = obs.Snapshot.PageTextHasMore
		pageOffset = obs.Snapshot.PageTextOffset
		value, pageTotal, pageHasMore, pageOffset, err = s.extractPageTextWindow(offset, maxChars)
		if err != nil {
			return nil, err
		}
	}
	value = strings.TrimSpace(value)
	detail := strings.TrimSpace(query)
	if detail == "" {
		detail = "page content"
	}
	s.appendActionTrace("extract", fmt.Sprintf("extract %s", detail))
	data := map[string]interface{}{
		"query":       query,
		"format":      format,
		"content":     value,
		"snapshot_id": snapshotID,
	}
	if pageTotal > 0 {
		nextOffset := 0
		if pageHasMore {
			nextOffset = pageOffset + len([]rune(value))
		}
		data["total_chars"] = pageTotal
		data["offset"] = pageOffset
		data["has_more"] = pageHasMore
		data["next_offset"] = nextOffset
	}
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: snapshotID,
		Action:     "browser_extract",
		Status:     "ok",
		Display:    fmt.Sprintf("extracted %s", detail),
		Data:       data,
	}, nil
}

func extractPageTextExpr() string {
	return fmt.Sprintf(`(function() {
		%s
		const full = pageTextFrom(document);
		return JSON.stringify({text: full, total_chars: (full || '').length});
	})()`, pierceFindJS)
}

func (s *BrowserAgentSession) extractPageTextWindow(offset, maxChars int) (string, int, bool, int, error) {
	if s == nil || s.session == nil {
		return "", 0, false, 0, fmt.Errorf("browser session not connected")
	}
	raw, err := s.session.Eval(extractPageTextExpr())
	if err != nil {
		return "", 0, false, 0, err
	}
	var payload struct {
		Text       string `json:"text"`
		TotalChars int    `json:"total_chars"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", 0, false, 0, fmt.Errorf("parse page text: %w", err)
	}
	if offset < 0 {
		offset = 0
	}
	runes := []rune(payload.Text)
	total := len(runes)
	if payload.TotalChars > 0 {
		total = payload.TotalChars
	}
	if offset > len(runes) {
		offset = len(runes)
	}
	end := len(runes)
	if maxChars > 0 {
		end = min(len(runes), offset+maxChars)
	}
	hasMore := end < len(runes)
	nextOffset := offset
	if hasMore {
		nextOffset = end
	}
	_ = nextOffset
	return strings.TrimSpace(string(runes[offset:end])), total, hasMore, offset, nil
}

const unchangedDisplaySuffix = "; page did not change — observe and verify before retrying"

func (s *BrowserAgentSession) completeAction(action, display, detail string, obs *BrowserObservation, extra map[string]interface{}, requireChange bool) *BrowserActionResult {
	snapshotID := ""
	if obs != nil {
		snapshotID = obs.Snapshot.SnapshotID
	}
	status := "ok"
	data := compactActionData(obs, extra)
	if requireChange && obs != nil {
		s.mu.RLock()
		prior := s.lastFingerprint
		sess := s.session
		s.mu.RUnlock()
		if prior != "" && prior == snapshotFingerprint(obs.Snapshot) && (sess == nil || !sess.hasPendingDialog()) {
			status = "unchanged"
			display = display + unchangedDisplaySuffix
			data["delta"] = expectDelta(obs, fmt.Errorf("page did not change after %s", action))
		}
	}
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: snapshotID,
		Action:     action,
		Status:     status,
		Detail:     detail,
		Display:    display,
		Data:       data,
	}
}

func activatingRef(ref *BrowserElementRef) bool {
	if ref == nil {
		return true
	}
	role := strings.ToLower(strings.TrimSpace(ref.Role))
	tag := strings.ToLower(strings.TrimSpace(ref.Tag))
	typ := strings.ToLower(strings.TrimSpace(ref.InputType))
	switch role {
	case "textbox", "searchbox":
		return false
	case "button", "link", "checkbox", "radio", "switch", "menuitem", "tab", "option", "combobox":
		return true
	}
	if tag == "textarea" {
		return false
	}
	if tag == "button" || tag == "a" || tag == "select" {
		return true
	}
	if tag == "input" {
		switch typ {
		case "submit", "button", "reset", "image", "checkbox", "radio", "file", "range", "color":
			return true
		default:
			return false
		}
	}
	return true
}

func snapshotFingerprint(snap BrowserSnapshot) string {
	var b strings.Builder
	b.WriteString(snap.URL)
	b.WriteByte('|')
	b.WriteString(snap.Title)
	b.WriteByte('|')
	for _, ref := range snap.Refs {
		b.WriteString(ref.Role)
		b.WriteByte(':')
		b.WriteString(ref.Tag)
		b.WriteByte(':')
		b.WriteString(firstNonEmpty(ref.Name, ref.Text))
		if ref.Disabled {
			b.WriteByte('!')
		}
		if ref.Checked {
			b.WriteByte('*')
		}
		if v := strings.TrimSpace(ref.Value); v != "" && !strings.EqualFold(ref.InputType, "password") {
			b.WriteByte('=')
			b.WriteString(v)
		}
		b.WriteByte(';')
	}
	return b.String()
}

func (s *BrowserAgentSession) requireLiveSession() error {
	if s == nil || s.session == nil {
		return fmt.Errorf("browser session not connected")
	}
	if !s.IsTargetAlive() {
		return fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	return nil
}

func (s *BrowserAgentSession) Hover(snapshotID, ref, selector string) (*BrowserActionResult, error) {
	if err := s.requireLiveSession(); err != nil {
		return nil, err
	}
	candidates, resolved, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
	if err != nil {
		return nil, err
	}
	if err := rejectDisabledRef(resolved); err != nil {
		return nil, err
	}
	var lastErr error
	used := ""
	frameID := ""
	if resolved != nil {
		frameID = resolved.FrameID
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if err := s.gateCandidate(frameID, candidate); err != nil {
			lastErr = err
			if isFrameGoneErr(err) {
				return nil, err
			}
			continue
		}
		if err := s.session.HoverInFrame(frameID, candidate); err == nil {
			used = candidate
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if used == "" && resolved != nil && resolved.BackendNodeID != 0 {
		sessionID := ""
		if s.session != nil {
			sessionID = s.session.frameSessionID(resolved.FrameID)
		}
		if err := s.session.hoverBackendNodeOn(sessionID, resolved.BackendNodeID); err == nil {
			used = resolved.Ref
			lastErr = nil
		} else {
			lastErr = err
		}
	}
	if used == "" {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("missing ref or selector")
	}
	s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_hover")
	if blocked != nil || err != nil {
		return blocked, err
	}
	target := used
	if resolved != nil {
		target = resolved.Ref
	}
	s.appendActionTrace("hover", fmt.Sprintf("hover %s", target))
	return s.completeAction("browser_hover", fmt.Sprintf("hovered %s", target), target, obs, map[string]interface{}{
		"target": target,
	}, false), nil
}

func (s *BrowserAgentSession) Press(key string) (*BrowserActionResult, error) {
	if ask := s.captchaAskIfNeeded("press"); ask != nil {
		return ask, nil
	}
	if err := s.requireLiveSession(); err != nil {
		return nil, err
	}
	if err := s.session.Press(key); err != nil {
		return nil, err
	}
	s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_press")
	if blocked != nil || err != nil {
		return blocked, err
	}
	s.appendActionTrace("press", "press "+key)
	return s.completeAction("browser_press", fmt.Sprintf("pressed %s", key), key, obs, map[string]interface{}{
		"key": key,
	}, false), nil
}

func (s *BrowserAgentSession) HandleDialog(accept bool, promptText string) (*BrowserActionResult, error) {
	if ask := s.captchaAskIfNeeded("dialog"); ask != nil {
		return ask, nil
	}
	if err := s.requireLiveSession(); err != nil {
		return nil, err
	}
	if err := s.session.HandleDialog(accept, promptText); err != nil {
		return nil, err
	}
	s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_dialog")
	if blocked != nil || err != nil {
		return blocked, err
	}
	action := "dismiss"
	if accept {
		action = "accept"
	}
	s.appendActionTrace("dialog", action)
	return s.completeAction("browser_dialog", "dialog "+action, action, obs, map[string]interface{}{
		"accept": accept,
	}, false), nil
}

func (s *BrowserAgentSession) SelectOption(snapshotID, ref, selector, value string) (*BrowserActionResult, error) {
	if ask := s.captchaAskIfNeeded("select"); ask != nil {
		return ask, nil
	}
	if err := s.requireLiveSession(); err != nil {
		return nil, err
	}
	candidates, resolved, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
	if err != nil {
		return nil, err
	}
	if err := rejectDisabledRef(resolved); err != nil {
		return nil, err
	}
	frameID := ""
	if resolved != nil {
		frameID = resolved.FrameID
	}
	var lastErr error
	used := ""
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if err := s.gateCandidate(frameID, candidate); err != nil {
			lastErr = err
			if isFrameGoneErr(err) {
				return nil, err
			}
			continue
		}
		if err := s.session.SelectInFrame(frameID, candidate, value); err == nil {
			used = candidate
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if used == "" {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("missing ref or selector")
	}
	s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_select")
	if blocked != nil || err != nil {
		return blocked, err
	}
	target := used
	if resolved != nil {
		target = resolved.Ref
	}
	s.appendActionTrace("select", fmt.Sprintf("select %s = %s", target, value))
	return s.completeAction("browser_select", fmt.Sprintf("selected %s on %s", value, target), target, obs, map[string]interface{}{
		"target": target,
		"value":  value,
	}, true), nil
}

func (s *BrowserAgentSession) ScrollBy(snapshotID, ref, selector string, deltaX, deltaY int) (*BrowserActionResult, error) {
	if err := s.requireLiveSession(); err != nil {
		return nil, err
	}
	used := strings.TrimSpace(selector)
	frameID := ""
	if strings.TrimSpace(ref) != "" || used != "" {
		candidates, resolved, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
		if err != nil {
			return nil, err
		}
		if resolved != nil {
			frameID = resolved.FrameID
		}
		var lastErr error
		used = ""
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if err := s.gateCandidate(frameID, candidate); err != nil {
				lastErr = err
				if isFrameGoneErr(err) {
					return nil, err
				}
				continue
			}
			if err := s.session.ScrollElementInFrame(frameID, candidate, deltaX, deltaY); err == nil {
				used = candidate
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		if used == "" && lastErr != nil {
			return nil, lastErr
		}
	} else if err := s.session.Scroll(deltaX, deltaY); err != nil {
		return nil, err
	}
	s.waitForActionSettle(800*time.Millisecond, 150*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_scroll")
	if blocked != nil || err != nil {
		return blocked, err
	}
	s.appendActionTrace("scroll", fmt.Sprintf("scroll dx=%d dy=%d", deltaX, deltaY))
	return s.completeAction("browser_scroll", fmt.Sprintf("scrolled dx=%d dy=%d", deltaX, deltaY), "", obs, map[string]interface{}{
		"delta_x": deltaX,
		"delta_y": deltaY,
	}, false), nil
}

func (s *BrowserAgentSession) SetFilesOn(snapshotID, ref, selector string, files []string) (*BrowserActionResult, error) {
	if ask := s.captchaAskIfNeeded("set_files"); ask != nil {
		return ask, nil
	}
	if err := s.requireLiveSession(); err != nil {
		return nil, err
	}
	if err := validateUploadPolicy(s.Policy); err != nil {
		return policyBlockResult(s, "browser_set_files", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("missing files")
	}
	candidates, resolved, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
	if err != nil {
		return nil, err
	}
	if err := rejectDisabledRef(resolved); err != nil {
		return nil, err
	}
	frameID := ""
	if resolved != nil {
		frameID = resolved.FrameID
	}
	var lastErr error
	used := ""
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if err := s.gateCandidate(frameID, candidate); err != nil {
			lastErr = err
			if isFrameGoneErr(err) {
				return nil, err
			}
			continue
		}
		if err := s.session.SetFilesInFrame(frameID, candidate, files); err == nil {
			used = candidate
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if used == "" {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("missing ref or selector")
	}
	s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
	obs, blocked, err := s.observeAfterAction("browser_set_files")
	if blocked != nil || err != nil {
		return blocked, err
	}
	target := used
	if resolved != nil {
		target = resolved.Ref
	}
	s.appendActionTrace("set_files", fmt.Sprintf("set %d files on %s", len(files), target))
	return s.completeAction("browser_set_files", fmt.Sprintf("set %d files on %s", len(files), target), target, obs, map[string]interface{}{
		"target": target,
		"files":  len(files),
	}, false), nil
}
