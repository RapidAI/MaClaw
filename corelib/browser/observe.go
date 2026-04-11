package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const browserObserveScript = `(function () {
  function shortText(input, limit) {
    const s = String(input || '').replace(/\s+/g, ' ').trim();
    return s.length > limit ? s.slice(0, limit) : s;
  }
  function pageText() {
    return String(document.body ? (document.body.innerText || document.body.textContent || '') : '').replace(/\s+/g, ' ').trim();
  }
  function cssEscape(value) {
    if (typeof CSS !== 'undefined' && CSS.escape) return CSS.escape(value);
    return String(value || '').replace(/([ #;?%&,.+*~\':"!^$\[\]()=>|\/\\@])/g, '\\$1');
  }
  function nthIndex(el) {
    const parent = el && el.parentElement;
    if (!parent) return 1;
    const siblings = Array.from(parent.children).filter((child) => child.tagName === el.tagName);
    return Math.max(1, siblings.indexOf(el) + 1);
  }
  function selectorFor(el) {
    if (!el || !el.tagName) return '';
    const tag = el.tagName.toLowerCase();
    if (el.id) return '#' + cssEscape(el.id);
    const parts = [tag];
    const type = el.getAttribute && el.getAttribute('type');
    if (type) parts.push('[type="' + String(type).replace(/"/g, '\\"') + '"]');
    if (el.name) parts.push('[name="' + String(el.name).replace(/"/g, '\\"') + '"]');
    if (el.getAttribute) {
      const role = el.getAttribute('role');
      if (role) parts.push('[role="' + String(role).replace(/"/g, '\\"') + '"]');
      const testId = el.getAttribute('data-testid') || el.getAttribute('data-test') || el.getAttribute('data-qa');
      if (testId) parts.push('[data-testid="' + String(testId).replace(/"/g, '\\"') + '"]');
    }
    const text = shortText(el.innerText || el.textContent || '', 40);
    if (text && text.length <= 30 && /^(a|button|summary)$/i.test(tag)) {
      parts.push(':nth-of-type(' + nthIndex(el) + ')');
    } else {
      parts.push(':nth-of-type(' + nthIndex(el) + ')');
    }
    return parts.join('');
  }
  const refs = [];
  const seen = new Set();
  const candidates = document.querySelectorAll('a,button,input,textarea,select,option,[role],summary,[onclick],[contenteditable="true"],[tabindex]');
  let idx = 1;
  candidates.forEach((el) => {
    if (!el || seen.has(el)) return;
    seen.add(el);
    const rect = el.getBoundingClientRect();
    if ((rect.width <= 0 || rect.height <= 0) && !el.matches('option')) return;
    const role = (el.getAttribute && el.getAttribute('role')) || el.tagName.toLowerCase();
    const text = shortText(el.innerText || el.textContent || '', 120);
    const name = shortText(el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('placeholder') || el.getAttribute('name') || text, 120);
    const selector = selectorFor(el);
    refs.push({
      ref: '@e' + idx++,
      frame_id: 'main',
      tag: el.tagName.toLowerCase(),
      role: role,
      name: name,
      text: text,
      selector: selector,
      selector_candidates: [selector].filter(Boolean),
      stable_key: [el.tagName.toLowerCase(), el.id || '', el.getAttribute('name') || '', role || '', name || text || '', String(nthIndex(el))].join('|'),
      bounding_box: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
      stability_score: el.id ? 1 : (el.getAttribute('name') ? 0.92 : (el.getAttribute('data-testid') ? 0.9 : 0.72)),
    });
  });
  const fullText = pageText();
  const excerptLimit = 1200;
  return JSON.stringify({
    url: location.href,
    title: document.title,
    ready_state: document.readyState,
    frame_tree: [{ frame_id: 'main', url: location.href, title: document.title }],
    page_text_excerpt: shortText(fullText, excerptLimit),
    page_text_total: fullText.length,
    page_text_offset: 0,
    page_text_has_more: fullText.length > excerptLimit,
    refs: refs,
  });
})()`

// Observe captures a structured browser snapshot with refs and summaries.
func (s *BrowserAgentSession) Observe(includeScreenshot bool) (*BrowserObservation, error) {
	if s == nil {
		return nil, fmt.Errorf("browser session is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
	}

	result, err := s.session.Eval(browserObserveScript)
	if err != nil {
		return nil, err
	}
	var payload struct {
		URL             string                 `json:"url"`
		Title           string                 `json:"title"`
		ReadyState      string                 `json:"ready_state"`
		FrameTree       []BrowserFrameSnapshot `json:"frame_tree"`
		PageTextExcerpt string                 `json:"page_text_excerpt"`
		PageTextTotal   int                    `json:"page_text_total"`
		PageTextOffset  int                    `json:"page_text_offset"`
		PageTextHasMore bool                   `json:"page_text_has_more"`
		Refs            []BrowserElementRef    `json:"refs"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse browser observation: %w", err)
	}
	now := time.Now().UnixMilli()
	snapshotID := "browser-snapshot-" + generateID()
	for i := range payload.Refs {
		payload.Refs[i].SnapshotID = snapshotID
		if payload.Refs[i].FrameID == "" {
			payload.Refs[i].FrameID = "main"
		}
	}
	screenshot := ""
	if includeScreenshot {
		if img, shotErr := s.session.Screenshot(false); shotErr == nil {
			screenshot = img
		}
	}
	consoleSummary := strings.Join(s.recentConsole, "\n")
	networkSummary := strings.Join(s.recentNetwork, "\n")
	snapshot := BrowserSnapshot{
		SnapshotID:      snapshotID,
		SessionID:       s.ID,
		TargetID:        s.TargetID,
		CreatedAt:       now,
		URL:             payload.URL,
		Title:           payload.Title,
		ReadyState:      payload.ReadyState,
		FrameTree:       payload.FrameTree,
		Refs:            payload.Refs,
		PageTextExcerpt: payload.PageTextExcerpt,
		PageTextTotal:   payload.PageTextTotal,
		PageTextOffset:  payload.PageTextOffset,
		PageTextHasMore: payload.PageTextHasMore,
		ConsoleSummary:  consoleSummary,
		NetworkSummary:  networkSummary,
		Screenshot:      screenshot,
	}
	s.addSnapshot(snapshot)
	display := fmt.Sprintf("观察到页面 %s (%s)，可交互元素 %d 个。", firstNonEmpty(payload.Title, payload.URL), payload.URL, len(payload.Refs))
	obs := &BrowserObservation{
		Snapshot: snapshot,
		PageState: map[string]interface{}{
			"url":         snapshot.URL,
			"title":       snapshot.Title,
			"ready_state": snapshot.ReadyState,
		},
		Display: display,
		Data: map[string]interface{}{
			"snapshot_id":        snapshot.SnapshotID,
			"url":                snapshot.URL,
			"title":              snapshot.Title,
			"tab_id":             snapshot.TargetID,
			"frame_tree":         snapshot.FrameTree,
			"refs":               snapshot.Refs,
			"screenshot":         snapshot.Screenshot,
			"console_summary":    snapshot.ConsoleSummary,
			"network_summary":    snapshot.NetworkSummary,
			"page_state":         map[string]interface{}{"ready_state": snapshot.ReadyState},
			"page_text_excerpt":  snapshot.PageTextExcerpt,
			"page_text_total":    snapshot.PageTextTotal,
			"page_text_offset":   snapshot.PageTextOffset,
			"page_text_has_more": snapshot.PageTextHasMore,
		},
	}
	s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "observe", Summary: display, CreatedAt: now}, browserAgentConsoleLimit)
	s.activityLog = appendCapped(s.activityLog, display, browserAgentConsoleLimit)
	return obs, nil
}

func appendCappedTrace(items []BrowserTraceEvent, item BrowserTraceEvent, limit int) []BrowserTraceEvent {
	items = append(items, item)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
