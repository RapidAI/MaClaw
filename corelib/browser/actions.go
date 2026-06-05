package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Navigate performs a browser navigation under the agent session.
func (s *BrowserAgentSession) Navigate(url string) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("missing url")
	}
	if err := validateNavigationPolicy(s.Policy, url, currentDomainFromSession(s)); err != nil {
		return nil, err
	}
	if _, err := s.session.Navigate(url); err != nil {
		return nil, err
	}
	s.waitForActionSettle(3*time.Second, 300*time.Millisecond)
	obs, err := s.Observe(false)
	if err != nil {
		return nil, err
	}
	s.appendActionTrace("navigate", fmt.Sprintf("navigate to %s", url))
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: obs.Snapshot.SnapshotID,
		Action:     "browser_navigate",
		Status:     "ok",
		Detail:     url,
		Display:    fmt.Sprintf("navigated to %s", url),
		Data: map[string]interface{}{
			"url":         obs.Snapshot.URL,
			"title":       obs.Snapshot.Title,
			"snapshot_id": obs.Snapshot.SnapshotID,
		},
	}, nil
}

func (s *BrowserAgentSession) waitForActionSettle(timeout, quiet time.Duration) {
	if s == nil || s.session == nil {
		return
	}
	if err := s.session.WaitForStable(timeout, quiet); err != nil {
		s.appendActionTrace("settle", fmt.Sprintf("page settle skipped: %v", err))
	}
}

func (s *BrowserAgentSession) selectorCandidatesForAction(snapshotID, ref, selector string) ([]string, *BrowserElementRef, error) {
	selector = strings.TrimSpace(selector)
	ref = strings.TrimSpace(ref)
	if ref != "" {
		return s.selectorCandidatesForRef(snapshotID, ref)
	}
	if selector == "" {
		return nil, nil, fmt.Errorf("missing ref or selector")
	}
	return []string{selector}, nil, nil
}

func (s *BrowserAgentSession) clickWithCandidates(candidates []string) (string, int, error) {
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		if err := s.session.Click(candidate); err == nil {
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

func (s *BrowserAgentSession) typeWithCandidates(candidates []string, text, contentFormat string) (string, int, error) {
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		if err := s.session.TypeContent(candidate, text, contentFormat); err == nil {
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

func (s *BrowserAgentSession) waitWithCandidates(candidates []string, timeoutSec int) (string, int, error) {
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		if err := s.session.WaitForSelector(candidate, timeoutSec); err == nil {
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

func (s *BrowserAgentSession) extractWithCandidates(candidates []string) (string, string, int, error) {
	var lastErr error
	attempts := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		attempts++
		text, err := s.session.GetText(candidate)
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
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	ref = strings.TrimSpace(ref)
	selector = strings.TrimSpace(selector)
	var resolvedRef *BrowserElementRef
	var err error
	candidates, resolvedRef, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
	if err != nil {
		return nil, err
	}
	submitKey := s.submitClickKey(resolvedRef, selector, "")
	if err := s.guardSubmitClick(submitKey); err != nil {
		return nil, err
	}
	resolvedSelector, attempts, err := s.clickWithCandidates(candidates)
	if err != nil {
		if ref != "" {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs: %w", ref, err)
		}
		return nil, err
	}
	if attempts > 1 && ref != "" {
		s.appendActionTrace("retry", fmt.Sprintf("click fallback selector succeeded %s -> %s", ref, resolvedSelector))
	}
	s.rememberSubmitClick(submitKey)
	s.waitForActionSettle(2*time.Second, 250*time.Millisecond)
	obs, err := s.Observe(false)
	if err != nil {
		return nil, err
	}
	target := resolvedSelector
	if resolvedRef != nil {
		target = resolvedRef.Ref
	}
	s.appendActionTrace("click", fmt.Sprintf("click %s", target))
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: obs.Snapshot.SnapshotID,
		Action:     "browser_click",
		Status:     "ok",
		Detail:     target,
		Display:    fmt.Sprintf("clicked %s", target),
		Data: map[string]interface{}{
			"target":      target,
			"selector":    resolvedSelector,
			"snapshot_id": obs.Snapshot.SnapshotID,
		},
	}, nil
}

// ClickText clicks an element from the latest snapshot by visible text/name.
func (s *BrowserAgentSession) ClickText(snapshotID, text string) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("missing text")
	}
	candidates, resolvedRef, err := s.selectorCandidatesForText(snapshotID, text)
	if err != nil {
		return nil, err
	}
	submitKey := s.submitClickKey(resolvedRef, "", text)
	if err := s.guardSubmitClick(submitKey); err != nil {
		return nil, err
	}
	resolvedSelector, attempts, err := s.clickWithCandidates(candidates)
	if err != nil {
		return nil, fmt.Errorf("text %q is stale; run observe again to get fresh refs: %w", text, err)
	}
	if attempts > 1 {
		s.appendActionTrace("retry", fmt.Sprintf("click text fallback selector succeeded %s -> %s", text, resolvedSelector))
	}
	s.rememberSubmitClick(submitKey)
	s.waitForActionSettle(2*time.Second, 250*time.Millisecond)
	obs, err := s.Observe(false)
	if err != nil {
		return nil, err
	}
	target := text
	if resolvedRef != nil {
		target = resolvedRef.Ref
	}
	s.appendActionTrace("click", fmt.Sprintf("click text %s", text))
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: obs.Snapshot.SnapshotID,
		Action:     "browser_click",
		Status:     "ok",
		Detail:     target,
		Display:    fmt.Sprintf("clicked text %s", text),
		Data: map[string]interface{}{
			"target":      target,
			"selector":    resolvedSelector,
			"text":        text,
			"snapshot_id": obs.Snapshot.SnapshotID,
		},
	}, nil
}

// Type enters text into an element by ref or selector.
func (s *BrowserAgentSession) Type(snapshotID, ref, selector, text string) (*BrowserActionResult, error) {
	return s.TypeContent(snapshotID, ref, selector, text, BrowserContentFormatPlain)
}

// TypeContent enters plain text or rich markdown content into an element by ref or selector.
func (s *BrowserAgentSession) TypeContent(snapshotID, ref, selector, text, contentFormat string) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	ref = strings.TrimSpace(ref)
	selector = strings.TrimSpace(selector)
	contentFormat = normalizeBrowserContentFormat(contentFormat)
	if ref == "" && selector == "" {
		if err := s.session.TypeActiveContent(text, contentFormat); err != nil {
			return nil, err
		}
		s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
		obs, err := s.Observe(false)
		if err != nil {
			return nil, err
		}
		s.appendActionTrace("type", "typed into active element")
		return &BrowserActionResult{
			SessionID:  s.ID,
			SnapshotID: obs.Snapshot.SnapshotID,
			Action:     "browser_type",
			Status:     "ok",
			Detail:     "activeElement",
			Display:    fmt.Sprintf("typed %d chars into active element", len([]rune(text))),
			Data: map[string]interface{}{
				"target":         "activeElement",
				"text_length":    len([]rune(text)),
				"content_format": contentFormat,
				"snapshot_id":    obs.Snapshot.SnapshotID,
			},
		}, nil
	}
	var resolvedRef *BrowserElementRef
	var err error
	candidates, resolvedRef, err := s.selectorCandidatesForAction(snapshotID, ref, selector)
	if err != nil {
		return nil, err
	}
	resolvedSelector, attempts, err := s.typeWithCandidates(candidates, text, contentFormat)
	if err != nil {
		if ref != "" {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs: %w", ref, err)
		}
		return nil, err
	}
	if attempts > 1 && ref != "" {
		s.appendActionTrace("retry", fmt.Sprintf("type fallback selector succeeded %s -> %s", ref, resolvedSelector))
	}
	s.waitForActionSettle(1*time.Second, 200*time.Millisecond)
	obs, err := s.Observe(false)
	if err != nil {
		return nil, err
	}
	target := resolvedSelector
	if resolvedRef != nil {
		target = resolvedRef.Ref
	}
	s.appendActionTrace("type", fmt.Sprintf("type into %s", target))
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: obs.Snapshot.SnapshotID,
		Action:     "browser_type",
		Status:     "ok",
		Detail:     target,
		Display:    fmt.Sprintf("typed %d chars into %s", len([]rune(text)), target),
		Data: map[string]interface{}{
			"target":         target,
			"selector":       resolvedSelector,
			"text_length":    len([]rune(text)),
			"content_format": contentFormat,
			"snapshot_id":    obs.Snapshot.SnapshotID,
		},
	}, nil
}

// Wait pauses or waits for a selector/ref to appear.
func (s *BrowserAgentSession) Wait(snapshotID, ref, selector string, durationMS int) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	var resolvedRef *BrowserElementRef
	var err error
	resolvedSelector := strings.TrimSpace(selector)
	if resolvedSelector != "" {
		candidates, _, err := s.selectorCandidatesForAction(snapshotID, "", resolvedSelector)
		if err != nil {
			return nil, err
		}
		timeoutSec := durationMS / 1000
		if timeoutSec <= 0 {
			timeoutSec = 10
		}
		resolvedSelector, _, err = s.waitWithCandidates(candidates, timeoutSec)
		if err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(ref) != "" {
		candidates, refInfo, err := s.selectorCandidatesForAction(snapshotID, ref, "")
		if err != nil {
			return nil, err
		}
		resolvedRef = refInfo
		timeoutSec := durationMS / 1000
		if timeoutSec <= 0 {
			timeoutSec = 10
		}
		resolvedSelector, attempts, err := s.waitWithCandidates(candidates, timeoutSec)
		if err != nil {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs: %w", ref, err)
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
	obs, err := s.Observe(false)
	if err != nil {
		return nil, err
	}
	s.appendActionTrace("wait", "wait for page stability")
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: obs.Snapshot.SnapshotID,
		Action:     "browser_wait",
		Status:     "ok",
		Display:    "wait complete",
		Data: map[string]interface{}{
			"selector":    resolvedSelector,
			"duration_ms": durationMS,
			"snapshot_id": obs.Snapshot.SnapshotID,
		},
	}, nil
}

// Refresh reloads the current page and refreshes snapshot state.
func (s *BrowserAgentSession) Refresh() (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	info, err := s.session.Info()
	if err != nil || info == nil || strings.TrimSpace(info.URL) == "" {
		return nil, fmt.Errorf("unable to determine current page for refresh")
	}
	result, err := s.Navigate(info.URL)
	if err != nil {
		return nil, err
	}
	result.Action = "browser_refresh"
	result.Display = fmt.Sprintf("refreshed %s", info.URL)
	result.Detail = info.URL
	s.appendActionTrace("refresh", fmt.Sprintf("refresh %s", info.URL))
	return result, nil
}

// Back performs browser back navigation and refreshes snapshot state.
func (s *BrowserAgentSession) Back() (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	if err := s.session.Back(); err != nil {
		return nil, err
	}
	s.waitForActionSettle(3*time.Second, 300*time.Millisecond)
	obs, err := s.Observe(false)
	if err != nil {
		return nil, err
	}
	s.appendActionTrace("back", "browser back")
	return &BrowserActionResult{
		SessionID:  s.ID,
		SnapshotID: obs.Snapshot.SnapshotID,
		Action:     "browser_back",
		Status:     "ok",
		Display:    "went back",
		Data: map[string]interface{}{
			"url":         obs.Snapshot.URL,
			"title":       obs.Snapshot.Title,
			"snapshot_id": obs.Snapshot.SnapshotID,
		},
	}, nil
}

// Extract returns text from a specific target or the page summary.
func (s *BrowserAgentSession) Extract(snapshotID, ref, selector, query, format string, offset, maxChars int) (*BrowserActionResult, error) {
	if s == nil || s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}
	query = strings.TrimSpace(query)
	value := ""
	resolvedSelector := strings.TrimSpace(selector)
	pageTotal := 0
	pageHasMore := false
	pageOffset := 0
	if resolvedSelector != "" {
		candidates, _, err := s.selectorCandidatesForAction(snapshotID, "", resolvedSelector)
		if err != nil {
			return nil, err
		}
		resolvedSelector, value, _, err = s.extractWithCandidates(candidates)
		if err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(ref) != "" {
		candidates, _, err := s.selectorCandidatesForAction(snapshotID, ref, "")
		if err != nil {
			return nil, err
		}
		var attempts int
		resolvedSelector, value, attempts, err = s.extractWithCandidates(candidates)
		if err != nil {
			return nil, fmt.Errorf("ref %s is stale; run observe again to get fresh refs: %w", ref, err)
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
		obs, err := s.Observe(false)
		if err != nil {
			return nil, err
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

func (s *BrowserAgentSession) extractPageTextWindow(offset, maxChars int) (string, int, bool, int, error) {
	if s == nil || s.session == nil {
		return "", 0, false, 0, fmt.Errorf("browser session not connected")
	}
	js := `(function() {
		const full = String(document.body ? (document.body.innerText || document.body.textContent || '') : '').replace(/\s+/g, ' ').trim();
		return JSON.stringify({text: full, total_chars: full.length});
	})()`
	raw, err := s.session.Eval(js)
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
