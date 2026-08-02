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
  function attrSel(name, value) {
    // Escape backslash, quote, and control chars that are illegal bare in a
    // CSS string (\n → \a, \r → \d, \f → \c; trailing space terminates the
    // escape and is not part of the value).
    const v = String(value)
      .replace(/\\/g, '\\\\')
      .replace(/"/g, '\\"')
      .replace(/\n/g, '\\a ')
      .replace(/\r/g, '\\d ')
      .replace(/\f/g, '\\c ');
    return '[' + name + '="' + v + '"]';
  }
  function isUnique(sel) {
    try {
      return document.querySelectorAll(sel).length === 1;
    } catch (e) {
      return false;
    }
  }
  // selectorCandidatesFor builds an ordered, deduplicated candidate list from
  // most to least stable. Candidates are only included when they uniquely
  // match one element — a non-unique selector would silently click the FIRST
  // match in document order, which is worse than failing. The single legacy
  // positional selector is always appended last (unverified) so behaviour
  // never regresses below the old single-selector implementation.
  //
  // Uniqueness checks are full-document querySelectorAll scans, so we skip
  // them when a superset already proved unique (a subset of a unique selector
  // is unique by definition).
  function selectorCandidatesFor(el) {
    if (!el || !el.tagName) return [];
    const tag = el.tagName.toLowerCase();
    const out = [];
    const seen = new Set();
    function pushRaw(sel) {
      if (!sel || seen.has(sel)) return;
      seen.add(sel);
      out.push(sel);
    }
    function pushUnique(sel) {
      if (!sel || seen.has(sel)) return false;
      if (!isUnique(sel)) return false;
      seen.add(sel);
      out.push(sel);
      return true;
    }
    if (el.id) pushUnique('#' + cssEscape(el.id));
    // comboGuaranteed: a superset of "combo" proved unique, so combo and
    // combo+pos are unique by subset and skip the document scan.
    let comboGuaranteed = false;
    // Record WHICH test-id attribute is present — generating [data-testid]
    // from a data-test/data-qa value would match a different element.
    let testIdAttr = '';
    if (el.getAttribute) {
      if (el.hasAttribute('data-testid')) testIdAttr = 'data-testid';
      else if (el.hasAttribute('data-test')) testIdAttr = 'data-test';
      else if (el.hasAttribute('data-qa')) testIdAttr = 'data-qa';
    }
    const testId = testIdAttr ? el.getAttribute(testIdAttr) : '';
    if (testId) {
      const bare = attrSel(testIdAttr, testId);
      if (pushUnique(bare)) {
        pushRaw(tag + bare);
        comboGuaranteed = true;
      } else if (pushUnique(tag + bare)) {
        comboGuaranteed = true;
      }
    }
    if (el.name) {
      if (pushUnique(tag + attrSel('name', el.name))) comboGuaranteed = true;
    }
    const role = el.getAttribute && el.getAttribute('role');
    const ariaLabel = el.getAttribute && el.getAttribute('aria-label');
    if (role && ariaLabel) pushUnique(attrSel('role', role) + attrSel('aria-label', ariaLabel));
    // Attribute combo without position (often unique on forms/toolbars).
    const type = el.getAttribute && el.getAttribute('type');
    let combo = tag;
    if (type) combo += attrSel('type', type);
    if (el.name) combo += attrSel('name', el.name);
    if (role) combo += attrSel('role', role);
    if (testId) combo += attrSel(testIdAttr, testId);
    if (combo !== tag) {
      if (comboGuaranteed) pushRaw(combo); else pushUnique(combo);
    }
    // Two-level path with parent context — survives sibling reordering better
    // than a bare nth-of-type when the parent has a stable hook.
    const parent = el.parentElement;
    const pos = ':nth-of-type(' + nthIndex(el) + ')';
    if (parent && parent.tagName) {
      const ptag = parent.tagName.toLowerCase();
      let parentSel = '';
      if (parent.id) parentSel = '#' + cssEscape(parent.id);
      else if (parent.getAttribute && parent.getAttribute('data-testid')) parentSel = ptag + attrSel('data-testid', parent.getAttribute('data-testid'));
      else if (parent.getAttribute && parent.getAttribute('role')) parentSel = ptag + attrSel('role', parent.getAttribute('role'));
      else if (parent.name) parentSel = ptag + attrSel('name', parent.name);
      if (parentSel) pushUnique(parentSel + ' > ' + tag + pos);
    }
    // Positional variant of the attribute combo, unique-verified like the rest.
    if (combo !== tag) {
      if (comboGuaranteed || seen.has(combo)) pushRaw(combo + pos); else pushUnique(combo + pos);
    }
    // Last resort: the legacy positional selector, kept even when not unique
    // (matches old single-selector behaviour; retried last by actions.go).
    const legacy = (combo !== tag ? combo : tag) + pos;
    pushRaw(legacy);
    return out;
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
    const candidates = selectorCandidatesFor(el);
    const selector = candidates.length > 0 ? candidates[0] : '';
    refs.push({
      ref: '@e' + idx++,
      frame_id: 'main',
      tag: el.tagName.toLowerCase(),
      role: role,
      name: name,
      text: text,
      selector: selector,
      selector_candidates: candidates,
      stable_key: [el.tagName.toLowerCase(), el.id || '', el.getAttribute('name') || '', role || '', name || text || '', String(nthIndex(el))].join('|'),
      bounding_box: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
      stability_score: el.id ? 1 : (el.getAttribute('name') ? 0.92 : ((el.getAttribute('data-testid') || el.getAttribute('data-test') || el.getAttribute('data-qa')) ? 0.9 : 0.72)),
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
func (s *BrowserAgentSession) Observe(_ bool) (*BrowserObservation, error) {
	if s == nil {
		return nil, fmt.Errorf("browser session is nil")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
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
		Screenshot:      "",
	}
	s.addSnapshot(snapshot)
	display := fmt.Sprintf("observed page %s (%s), interactive elements: %d", firstNonEmpty(payload.Title, payload.URL), payload.URL, len(payload.Refs))
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
