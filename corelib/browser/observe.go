package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const browserObserveScript = `(function () {
  function shortText(input, limit) {
    const s = String(input || '').replace(/\s+/g, ' ').trim();
    return s.length > limit ? s.slice(0, limit) : s;
  }
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
    const v = String(value)
      .replace(/\\/g, '\\\\')
      .replace(/"/g, '\\"')
      .replace(/\n/g, '\\a ')
      .replace(/\r/g, '\\d ')
      .replace(/\f/g, '\\c ');
    return '[' + name + '="' + v + '"]';
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
  function anyIframeSrc(needle) {
    const n = String(needle || '').toLowerCase();
    function scan(d) {
      for (const f of queryIframes(d)) {
        try {
          if (String(f.src || '').toLowerCase().includes(n)) return true;
          if (f.contentDocument && scan(f.contentDocument)) return true;
        } catch (e) {}
      }
      return false;
    }
    return scan(document);
  }
  function isUnique(sel, doc) {
    try {
      return queryAllDeep(doc || document, sel).length === 1;
    } catch (e) {
      return false;
    }
  }
  function selectorCandidatesFor(el) {
    if (!el || !el.tagName) return [];
    const tag = el.tagName.toLowerCase();
    const doc = el.ownerDocument || document;
    const out = [];
    const seen = new Set();
    function pushRaw(sel) {
      if (!sel || seen.has(sel)) return;
      seen.add(sel);
      out.push(sel);
    }
    function pushUnique(sel) {
      if (!sel || seen.has(sel)) return false;
      if (!isUnique(sel, doc)) return false;
      seen.add(sel);
      out.push(sel);
      return true;
    }
    if (el.id) pushUnique('#' + cssEscape(el.id));
    let comboGuaranteed = false;
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
    const type = el.getAttribute && el.getAttribute('type');
    let combo = tag;
    if (type) combo += attrSel('type', type);
    if (el.name) combo += attrSel('name', el.name);
    if (role) combo += attrSel('role', role);
    if (testId) combo += attrSel(testIdAttr, testId);
    if (combo !== tag) {
      if (comboGuaranteed) pushRaw(combo); else pushUnique(combo);
    }
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
    if (combo !== tag) {
      if (comboGuaranteed || seen.has(combo)) pushRaw(combo + pos); else pushUnique(combo + pos);
    }
    const legacy = (combo !== tag ? combo : tag) + pos;
    if (isUnique(legacy, doc)) pushRaw(legacy);
    return out;
  }
  function isDisabled(el) {
    if (!el) return true;
    if (el.disabled) return true;
    if (el.getAttribute && el.getAttribute('aria-disabled') === 'true') return true;
    return false;
  }
  function isHidden(el) {
    if (!el) return true;
    if (el.getAttribute && el.getAttribute('aria-hidden') === 'true') return true;
    const style = (el.ownerDocument && el.ownerDocument.defaultView) ? el.ownerDocument.defaultView.getComputedStyle(el) : null;
    if (style && (style.display === 'none' || style.visibility === 'hidden')) return true;
    return false;
  }
  function collectFromRoot(root, frameId, refs, seen, idxHolder) {
    const selector = 'a,button,input,textarea,select,option,[role],summary,[onclick],[contenteditable="true"],[tabindex]';
    const candidates = queryAllDeep(root, selector);
    candidates.forEach((el) => {
      if (!el || seen.has(el)) return;
      seen.add(el);
      if (isHidden(el)) return;
      const rect = el.getBoundingClientRect();
      if ((rect.width <= 0 || rect.height <= 0) && !(el.matches && el.matches('option'))) return;
      const disabled = isDisabled(el);
      const inputType = String((el.getAttribute && (el.getAttribute('type') || el.type)) || '').toLowerCase();
      let role = (el.getAttribute && el.getAttribute('role')) || '';
      if (!role) {
        if (el.tagName === 'INPUT' && (inputType === 'submit' || inputType === 'button' || inputType === 'reset' || inputType === 'image')) role = 'button';
        else if (el.tagName === 'INPUT' && inputType === 'checkbox') role = 'checkbox';
        else if (el.tagName === 'INPUT' && inputType === 'radio') role = 'radio';
        else if (el.tagName === 'INPUT' && inputType === 'search') role = 'searchbox';
        else if (el.tagName === 'A') role = 'link';
        else role = el.tagName.toLowerCase();
      }
      const text = shortText(el.innerText || el.textContent || '', 120);
      const name = shortText(el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('placeholder') || el.getAttribute('name') || text, 120);
      let value = '';
      if (inputType !== 'password') {
        if (el.tagName === 'SELECT') {
          const opt = el.options && el.selectedIndex >= 0 ? el.options[el.selectedIndex] : null;
          value = shortText((opt && (opt.textContent || opt.label)) || el.value || '', 80);
        } else if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
          value = shortText(el.value || '', 40);
        }
      }
      const candidates = selectorCandidatesFor(el);
      const selector = candidates.length > 0 ? candidates[0] : '';
      const vw = (root.defaultView || window);
      const inViewport = rect.bottom > 0 && rect.right > 0 && rect.top < (vw.innerHeight || 0) && rect.left < (vw.innerWidth || 0);
      refs.push({
        ref: '@e' + idxHolder.n++,
        frame_id: frameId,
        tag: el.tagName.toLowerCase(),
        role: role,
        input_type: inputType,
        name: name,
        text: text,
        checked: !!el.checked,
        value: value,
        selector: selector,
        selector_candidates: candidates,
        stable_key: [el.tagName.toLowerCase(), el.id || '', el.getAttribute('name') || '', role || '', name || text || '', String(nthIndex(el))].join('|'),
        bounding_box: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
        stability_score: el.id ? 1 : (el.getAttribute('name') ? 0.92 : ((el.getAttribute('data-testid') || el.getAttribute('data-test') || el.getAttribute('data-qa')) ? 0.9 : 0.72)),
        disabled: disabled,
        visible: true,
        in_viewport: inViewport
      });
    });
  }
  const refs = [];
  const seen = new Set();
  const idxHolder = { n: 1 };
  collectFromRoot(document, 'main', refs, seen, idxHolder);
  const frames = [{ frame_id: 'main', url: location.href, name: '', title: document.title }];
  function collectFrames(doc, parentId) {
    queryIframes(doc).forEach((frame, i) => {
      const frameId = frame.name || frame.id || (parentId === 'main' ? ('iframe-' + i) : (parentId + '-' + i));
      frames.push({ frame_id: frameId, url: frame.src || '', name: frame.name || '', parent_frame_id: parentId });
      try {
        const child = frame.contentDocument;
        if (child) {
          collectFromRoot(child, frameId, refs, seen, idxHolder);
          collectFrames(child, frameId);
          if (child.defaultView && !child.defaultView.__maclawMut) {
            child.defaultView.__maclawMut = { n: 0 };
            try { new MutationObserver(function () { child.defaultView.__maclawMut.n++; }).observe(child, { subtree: true, childList: true, attributes: true, characterData: false }); } catch (e) {}
          }
        }
      } catch (e) {}
    });
  }
  collectFrames(document, 'main');
` + browserPageFlagsCollectJS + `
  if (!window.__maclawMut) {
    window.__maclawMut = { n: 0 };
    try {
      new MutationObserver(function () { window.__maclawMut.n++; }).observe(document, { subtree: true, childList: true, attributes: true, characterData: false });
    } catch (e) {}
  }
  return JSON.stringify({
    url: location.href,
    title: document.title,
    ready_state: document.readyState,
    frame_tree: frames,
    page_text_excerpt: shortText(fullText, excerptLimit),
    page_text_total: fullText.length,
    page_text_offset: 0,
    page_text_has_more: fullText.length > excerptLimit,
    page_flags: pageFlags,
    refs: refs
  });
})()`

var (
	observeOCRMu sync.Mutex
	observeOCR   OCRProvider
)

// SetObserveOCR installs the optional vision-on-empty OCR backend.
func SetObserveOCR(provider OCRProvider) {
	observeOCRMu.Lock()
	observeOCR = provider
	observeOCRMu.Unlock()
}

func getObserveOCR() OCRProvider {
	observeOCRMu.Lock()
	defer observeOCRMu.Unlock()
	return observeOCR
}

type observePayload struct {
	URL             string                 `json:"url"`
	Title           string                 `json:"title"`
	ReadyState      string                 `json:"ready_state"`
	FrameTree       []BrowserFrameSnapshot `json:"frame_tree"`
	PageTextExcerpt string                 `json:"page_text_excerpt"`
	PageTextTotal   int                    `json:"page_text_total"`
	PageTextOffset  int                    `json:"page_text_offset"`
	PageTextHasMore bool                   `json:"page_text_has_more"`
	PageFlags       BrowserPageFlags       `json:"page_flags"`
	Refs            []BrowserElementRef    `json:"refs"`
}

// Observe captures a structured browser snapshot with compact refs.
func (s *BrowserAgentSession) Observe(_ bool) (*BrowserObservation, error) {
	return s.ObserveFiltered("")
}

// ObserveFiltered captures a snapshot, optionally keeping refs whose name/text/role match query.
func (s *BrowserAgentSession) ObserveFiltered(query string) (*BrowserObservation, error) {
	if s == nil {
		return nil, fmt.Errorf("browser session is nil")
	}
	if !s.IsTargetAlive() {
		return nil, fmt.Errorf("browser target is gone (destroyed or detached); retry the operation — session will auto-recover")
	}
	s.mu.Lock()
	sess := s.session
	s.mu.Unlock()
	if sess == nil {
		return nil, fmt.Errorf("browser session not connected")
	}

	result, err := sess.Eval(browserObserveScript)
	if err != nil {
		return nil, err
	}
	var payload observePayload
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse browser observation: %w", err)
	}

	jsFrames := payload.FrameTree
	cdpFrames := sess.frameTree()
	if len(cdpFrames) > 0 {
		remapRefFrameIDs(payload.Refs, cdpFrames, jsFrames)
		payload.FrameTree = cdpFrames
	}
	axRefs := sess.axInteractiveRefs()
	payload.Refs = mergeAXRefs(payload.Refs, axRefs)
	crossRefs := sess.observeAttachedFrames(len(payload.Refs) + 1)
	payload.Refs = append(payload.Refs, crossRefs...)

	query = strings.TrimSpace(query)
	if query != "" {
		payload.Refs = filterRefsByQuery(payload.Refs, query)
	}

	now := time.Now().UnixMilli()
	snapshotID := "browser-snapshot-" + generateID()
	for i := range payload.Refs {
		payload.Refs[i].SnapshotID = snapshotID
		if payload.Refs[i].FrameID == "" {
			payload.Refs[i].FrameID = "main"
		}
		payload.Refs[i].Ref = fmt.Sprintf("@e%d", i+1)
	}
	kept, truncated := truncateRefs(payload.Refs, compactRefLimit)
	internalRefs := payload.Refs
	if truncated {
		// Keep full refs internally so stale-handle recovery still works.
		_ = kept
	} else {
		internalRefs = kept
	}

	visionExcerpt := ""
	if shouldUseVision(payload.PageFlags, payload.Refs) {
		if excerpt := sess.visionExcerptOnce(); excerpt != "" {
			visionExcerpt = excerpt
			payload.PageFlags.VisionUsed = true
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil, fmt.Errorf("browser session not connected")
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
		Refs:            internalRefs,
		PageTextExcerpt: payload.PageTextExcerpt,
		PageTextTotal:   payload.PageTextTotal,
		PageTextOffset:  payload.PageTextOffset,
		PageTextHasMore: payload.PageTextHasMore,
		ConsoleSummary:  consoleSummary,
		NetworkSummary:  networkSummary,
		Screenshot:      "",
		PageFlags:       payload.PageFlags,
		RefsTruncated:   truncated,
		VisionExcerpt:   visionExcerpt,
	}
	s.addSnapshot(snapshot)
	display := formatObserveDisplay(snapshot)
	data := observeDataFromSnapshot(snapshot)
	if excerpt := lastExpectExcerpt(s.lastExpect); excerpt != nil {
		data["last_expect"] = excerpt
	}
	obs := &BrowserObservation{
		Snapshot: snapshot,
		PageState: map[string]interface{}{
			"url":         snapshot.URL,
			"title":       snapshot.Title,
			"ready_state": snapshot.ReadyState,
		},
		Display: display,
		Data:    data,
	}
	s.recentTrace = appendCappedTrace(s.recentTrace, BrowserTraceEvent{Kind: "observe", Summary: display, CreatedAt: now}, browserAgentConsoleLimit)
	s.activityLog = appendCapped(s.activityLog, display, browserAgentConsoleLimit)
	return obs, nil
}

func formatObserveDisplay(snapshot BrowserSnapshot) string {
	display := fmt.Sprintf("observed page %s (%s), interactive elements: %d", firstNonEmpty(snapshot.Title, snapshot.URL), snapshot.URL, len(snapshot.Refs))
	if snapshot.RefsTruncated {
		display += fmt.Sprintf(" (showing %d)", compactRefLimit)
	}
	flags := []string{}
	if snapshot.PageFlags.CaptchaWidget {
		flags = append(flags, "captcha_widget")
	}
	if snapshot.PageFlags.Captcha {
		flags = append(flags, "captcha")
	}
	if snapshot.PageFlags.LoginWall {
		flags = append(flags, "login_wall")
	}
	if snapshot.PageFlags.MFA {
		flags = append(flags, "mfa")
	}
	if snapshot.PageFlags.Canvas {
		flags = append(flags, "canvas")
	}
	if snapshot.PageFlags.CaptchaWidget {
		display += "; page flags: " + strings.Join(flags, ",") + " — solve the captcha in the browser, then observe before clicking"
	} else if len(flags) > 0 {
		display += "; page flags: " + strings.Join(flags, ",")
	}
	if snapshot.VisionExcerpt != "" {
		display += "; vision excerpt available"
	}
	return display
}

func shouldUseVision(flags BrowserPageFlags, refs []BrowserElementRef) bool {
	if flags.CaptchaWidget {
		return false
	}
	if flags.Canvas {
		return true
	}
	interactive := 0
	for _, ref := range refs {
		if !ref.Disabled {
			interactive++
		}
	}
	return interactive < visionEmptyRefThreshold
}

func filterRefsByQuery(refs []BrowserElementRef, query string) []BrowserElementRef {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return refs
	}
	out := make([]BrowserElementRef, 0)
	for _, ref := range refs {
		hay := strings.ToLower(strings.Join([]string{ref.Name, ref.Text, ref.Role, ref.Tag}, " "))
		if strings.Contains(hay, q) {
			out = append(out, ref)
		}
	}
	return out
}

func remapRefFrameIDs(refs []BrowserElementRef, cdp, js []BrowserFrameSnapshot) {
	if len(refs) == 0 || len(cdp) == 0 {
		return
	}
	byURL := map[string]string{}
	byName := map[string]string{}
	cdpRoot := ""
	for _, frame := range cdp {
		if frame.ParentFrameID == "" {
			cdpRoot = frame.FrameID
		}
		if frame.URL != "" {
			byURL[strings.TrimRight(frame.URL, "/")] = frame.FrameID
		}
		if frame.Name != "" {
			byName[frame.Name] = frame.FrameID
		}
	}
	jsRoot := ""
	jsURL := map[string]string{}
	for _, frame := range js {
		if frame.ParentFrameID == "" {
			jsRoot = frame.FrameID
		}
		if frame.URL != "" {
			jsURL[frame.FrameID] = strings.TrimRight(frame.URL, "/")
		}
	}
	if jsRoot == "" {
		jsRoot = "main"
	}

	childrenOf := func(frames []BrowserFrameSnapshot) map[string][]BrowserFrameSnapshot {
		out := make(map[string][]BrowserFrameSnapshot)
		for _, frame := range frames {
			if frame.ParentFrameID == "" {
				continue
			}
			out[frame.ParentFrameID] = append(out[frame.ParentFrameID], frame)
		}
		return out
	}
	jsKids := childrenOf(js)
	cdpKids := childrenOf(cdp)

	jsToCDP := map[string]string{}
	usedCDP := map[string]bool{}
	if cdpRoot != "" {
		jsToCDP[jsRoot] = cdpRoot
		usedCDP[cdpRoot] = true
	}
	for _, frame := range js {
		if frame.FrameID == jsRoot {
			continue
		}
		if frame.Name != "" {
			if id, ok := byName[frame.Name]; ok && !usedCDP[id] {
				jsToCDP[frame.FrameID] = id
				usedCDP[id] = true
				continue
			}
		}
		if url := strings.TrimRight(frame.URL, "/"); url != "" {
			if id, ok := byURL[url]; ok && !usedCDP[id] {
				jsToCDP[frame.FrameID] = id
				usedCDP[id] = true
			}
		}
	}
	var walk func(jsParent string)
	walk = func(jsParent string) {
		cdpParent := jsToCDP[jsParent]
		if cdpParent == "" {
			return
		}
		remaining := make([]BrowserFrameSnapshot, 0)
		for _, child := range cdpKids[cdpParent] {
			if !usedCDP[child.FrameID] {
				remaining = append(remaining, child)
			}
		}
		ri := 0
		for _, child := range jsKids[jsParent] {
			if _, ok := jsToCDP[child.FrameID]; ok {
				walk(child.FrameID)
				continue
			}
			if ri >= len(remaining) {
				continue
			}
			jsToCDP[child.FrameID] = remaining[ri].FrameID
			usedCDP[remaining[ri].FrameID] = true
			ri++
			walk(child.FrameID)
		}
	}
	walk(jsRoot)

	for i := range refs {
		id := refs[i].FrameID
		if id == "" || id == "main" {
			continue
		}
		if mapped, ok := jsToCDP[id]; ok {
			refs[i].FrameID = mapped
			continue
		}
		if mapped, ok := byName[id]; ok {
			refs[i].FrameID = mapped
			continue
		}
		if url := jsURL[id]; url != "" {
			if mapped, ok := byURL[url]; ok {
				refs[i].FrameID = mapped
			}
		}
	}
}

func mergeAXRefs(existing, ax []BrowserElementRef) []BrowserElementRef {
	if len(ax) == 0 {
		return existing
	}
	seen := map[string]int{}
	for i, ref := range existing {
		key := strings.ToLower(ref.Role + "|" + firstNonEmpty(ref.Name, ref.Text))
		if key != "|" {
			seen[key] = i
		}
	}
	next := existing
	idx := len(existing) + 1
	for _, ref := range ax {
		key := strings.ToLower(ref.Role + "|" + firstNonEmpty(ref.Name, ref.Text))
		if key == "|" {
			continue
		}
		if i, ok := seen[key]; ok {
			if next[i].BackendNodeID == 0 && ref.BackendNodeID != 0 {
				next[i].BackendNodeID = ref.BackendNodeID
			}
			continue
		}
		seen[key] = len(next)
		ref.Ref = fmt.Sprintf("@e%d", idx)
		idx++
		next = append(next, ref)
	}
	return next
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
